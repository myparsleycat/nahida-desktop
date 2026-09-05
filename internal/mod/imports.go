package mod

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nahida.live/desktop/internal/infra"
)

const maxPreviewBytes = 50 << 20

var previewNameRE = regexp.MustCompile(`(?i)^preview\.[^.]+$`)

func (m *Mod) UpdateToggleKey(
	ctx context.Context,
	modPath, iniFileName, sectionName, variable, value string,
) error {
	iniPath := iniFileName
	if !filepath.IsAbs(iniPath) {
		iniPath = filepath.Join(modPath, iniFileName)
	}
	if _, err := m.ownedPath(ctx, iniPath); err != nil {
		return err
	}
	content, err := os.ReadFile(iniPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")
	newLines := make([]string, 0, len(lines)+1)
	currentSection := ""
	foundVariable := false
	sectionStart := -1
	updated := false
	variableLine := variable + " = " + value
	insertCurrent := func() {
		if !strings.EqualFold(currentSection, sectionName) || foundVariable || value == "" {
			return
		}
		insertAt := len(newLines)
		for insertAt > sectionStart+1 && strings.TrimSpace(newLines[insertAt-1]) == "" {
			insertAt--
		}
		newLines = append(newLines, "")
		copy(newLines[insertAt+1:], newLines[insertAt:])
		newLines[insertAt] = variableLine
		updated, foundVariable = true, true
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			insertCurrent()
			currentSection = trimmed[1 : len(trimmed)-1]
			newLines = append(newLines, line)
			sectionStart = len(newLines) - 1
			foundVariable = false
			continue
		}
		lower := strings.ToLower(trimmed)
		lowerVariable := strings.ToLower(variable)
		isVariable := strings.HasPrefix(lower, lowerVariable+" =") ||
			strings.HasPrefix(lower, lowerVariable+"=")
		if strings.EqualFold(currentSection, sectionName) && isVariable {
			foundVariable, updated = true, true
			if value != "" {
				newLines = append(newLines, variableLine)
			}
			continue
		}
		newLines = append(newLines, line)
	}
	insertCurrent()
	if !updated {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return writeToggleKeyFile(iniPath, []byte(strings.Join(newLines, "\n")))
}

func writeToggleKeyFile(path string, content []byte) error {
	err := os.Chmod(path, 0o666)
	if err == nil {
		err = os.WriteFile(path, content, 0o666)
	}
	if err == nil || !errors.Is(err, os.ErrPermission) {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o666)
}

func (m *Mod) ExtractArchiveToGroup(
	ctx context.Context,
	archivePath, groupPath, mode string,
) (string, error) {
	if _, err := m.ownedPath(ctx, groupPath); err != nil {
		return "", err
	}
	if m.archive == nil {
		return "", errors.New("archive service is not configured")
	}
	if mode == "" && m.settings != nil {
		value, err := m.settings.GetArchiveExtractPathMode(ctx)
		if err != nil {
			return "", err
		}
		mode = value
	}
	if mode == "ask_every_time" {
		return "", errors.New("ARCHIVE_EXTRACT_MODE_PROMPT_REQUIRED")
	}
	if mode != "flatten_single_root" && mode != "keep_archive_root" {
		return "", errors.New("INVALID_ARCHIVE_EXTRACT_MODE")
	}
	flatten := mode != "keep_archive_root"
	target, err := m.archive.Extract(
		ctx, archivePath, groupPath, infra.ExtractOptions{FlattenSingleRoot: &flatten}, nil,
	)
	if err != nil {
		return "", err
	}
	m.queueFixInspection(target)
	deleteAfter := false
	if m.settings != nil {
		deleteAfter, err = m.settings.GetDeleteArchiveAfterExtract(ctx)
		if err != nil {
			return target, err
		}
	}
	if deleteAfter {
		if err := os.Remove(archivePath); err != nil {
			return target, err
		}
	}
	return target, nil
}

func (m *Mod) CopyFolderToGroup(
	ctx context.Context,
	folderPath, groupPath string,
) (string, error) {
	if _, err := m.ownedPath(ctx, groupPath); err != nil {
		return "", err
	}
	move := false
	if m.settings != nil {
		var err error
		move, err = m.settings.GetMoveFolderInsteadOfCopy(ctx)
		if err != nil {
			return "", err
		}
	}
	source, err := validDirectory(folderPath)
	if err != nil {
		return "", err
	}
	target := filepath.Join(groupPath, filepath.Base(source))
	if pathWithin(source, target) {
		return "", errors.New("COPY_TARGET_INSIDE_SOURCE")
	}
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("ALREADY_EXISTS:%s", filepath.Base(source))
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if move {
		if err := os.Rename(source, target); err == nil {
			m.queueFixInspection(target)
			return target, nil
		}
	}
	if err := copyDirectory(source, target); err != nil {
		m.reportCleanup(os.RemoveAll(target), "CopyFolderToGroup")
		return "", err
	}
	if move {
		if err := os.RemoveAll(source); err != nil {
			return target, err
		}
	} else if m.shaders != nil {
		if err := m.shaders.DeleteModManifest(target); err != nil {
			return target, err
		}
	}
	m.queueFixInspection(target)
	return target, nil
}

func (m *Mod) PastePreview(
	ctx context.Context,
	modPath, data, pasteType string,
	existingPreviewPath *string,
) (string, error) {
	if _, err := m.ownedPath(ctx, modPath); err != nil {
		return "", err
	}
	modPath, err := validDirectory(modPath)
	if err != nil {
		return "", err
	}
	content, extension, err := m.previewContent(ctx, data, pasteType)
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(modPath, "preview"+extension)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := atomicWriteFile(filePath, content, 0o644, func(err error) {
		_ = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{Operation: "paste-preview", Stage: "cleanup", Fields: map[string]any{"path": filePath}})
	}); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(modPath)
	if err != nil {
		return filePath, err
	}
	stale := map[string]struct{}{}
	for _, entry := range entries {
		path := filepath.Join(modPath, entry.Name())
		if !entry.IsDir() && previewNameRE.MatchString(entry.Name()) && !samePath(path, filePath) {
			stale[path] = struct{}{}
		}
	}
	if existingPreviewPath != nil && pathWithin(modPath, *existingPreviewPath) &&
		previewNameRE.MatchString(filepath.Base(*existingPreviewPath)) &&
		!samePath(*existingPreviewPath, filePath) {
		stale[*existingPreviewPath] = struct{}{}
	}
	for path := range stale {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return filePath, err
		}
	}
	return filePath, nil
}

func (m *Mod) GetGameBananaModID(ctx context.Context, modPath string) (*int64, error) {
	if _, err := m.ownedPath(ctx, modPath); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(modPath, "nhd.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeGameBananaModID(raw), nil
}

func decodeGameBananaModID(raw []byte) *int64 {
	var metadata struct {
		Source string `json:"source"`
		Mod    *struct {
			ID int64 `json:"id"`
		} `json:"mod"`
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.Source != "gamebanana" ||
		metadata.Mod == nil || metadata.Mod.ID <= 0 {
		return nil
	}
	return &metadata.Mod.ID
}

func (m *Mod) previewContent(ctx context.Context, data, pasteType string) ([]byte, string, error) {
	switch pasteType {
	case "url":
		if m.http == nil {
			return nil, "", errors.New("http service is not configured")
		}
		response, err := m.http.Fetch(ctx, data, infra.FetchOptions{Method: http.MethodGet})
		if err != nil {
			return nil, "", err
		}
		defer func() { _ = response.Body.Close() }()
		content, err := readBounded(response.Body, maxPreviewBytes)
		if err != nil {
			return nil, "", err
		}
		return content, previewExtension(response.Header.Get("Content-Type"), content), nil
	case "base64":
		extension := ".png"
		encoded := data
		if strings.HasPrefix(data, "data:image/") {
			separator := strings.Index(data, ";base64,")
			if separator < 0 {
				return nil, "", errors.New("INVALID_PREVIEW_BASE64")
			}
			extension = safePreviewExtension(data[len("data:image/"):separator])
			encoded = data[separator+len(";base64,"):]
		}
		content, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(content) > maxPreviewBytes {
			return nil, "", errors.New("INVALID_PREVIEW_BASE64")
		}
		return content, extension, nil
	case "path":
		path, err := validFile(data)
		if err != nil {
			return nil, "", err
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, "", err
		}
		defer func() { _ = file.Close() }()
		content, err := readBounded(file, maxPreviewBytes)
		return content, safePreviewExtension(filepath.Ext(path)), err
	default:
		return nil, "", errors.New("INVALID_PREVIEW_TYPE")
	}
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, errors.New("PREVIEW_TOO_LARGE")
	}
	return content, nil
}

func previewExtension(contentType string, content []byte) string {
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if extension, ok := map[string]string{
		"video/quicktime":  ".mov",
		"video/x-msvideo":  ".avi",
		"video/x-matroska": ".mkv",
		"video/ogg":        ".ogg",
		"audio/ogg":        ".ogg",
		"application/ogg":  ".ogg",
	}[contentType]; ok {
		return extension
	}
	if _, subtype, ok := strings.Cut(contentType, "/"); ok {
		return safePreviewExtension(subtype)
	}
	return ".png"
}

func safePreviewExtension(value string) string {
	extension := "." + strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
	if mediaExtensions[extension] {
		return extension
	}
	return ".png"
}

func atomicWriteFile(path string, content []byte, mode os.FileMode, reports ...func(error)) error {
	report := func(err error) {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			for _, callback := range reports {
				callback(err)
			}
		}
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".nhd-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { report(os.Remove(temporaryPath)) }()
	if _, err := temporary.Write(content); err != nil {
		return infra.WithCause(err, temporary.Close())
	}
	if err := temporary.Sync(); err != nil {
		return infra.WithCause(err, temporary.Close())
	}
	if err := temporary.Chmod(mode); err != nil {
		return infra.WithCause(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	backup := path + ".nhd-backup"
	report(os.Remove(backup))
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return infra.WithCause(err, infra.AnnotateError(os.Rename(backup, path), infra.Diagnostic{Stage: "rollback", Fields: map[string]any{"path": path, "backupPath": backup}}))
	}
	report(os.Remove(backup))
	return nil
}

func copyDirectory(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported: %s", path)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(destination, info.Mode().Perm())
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return closeErr
	})
}

func (m *Mod) reportCleanup(err error, operation string) {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{Operation: operation, Stage: "cleanup"})
}
