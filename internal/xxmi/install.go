package xxmi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
)

type InstallImporterPackageInput struct {
	Importer      string `json:"importer"`
	Version       string `json:"version"`
	AllowUnsigned bool   `json:"allowUnsigned"`
}

func (x *XXMI) InstallImporterPackage(ctx context.Context, input InstallImporterPackageInput) (returnErr error) {
	x.packageMu.Lock()
	defer x.packageMu.Unlock()

	stage := "validate-input"
	defer func() {
		if returnErr != nil {
			returnErr = infra.ReportError(x.log, returnErr, "XXMI.installImporterPackage", infra.Diagnostic{
				Operation: "install-importer-package", Stage: stage,
				Fields: map[string]any{
					"importer": installDiagnosticValue(input.Importer),
					"version":  installDiagnosticValue(input.Version), "rollback": "not-started",
				},
			})
		}
	}()
	version := strings.TrimSpace(input.Version)
	if version == "" || strings.ContainsAny(version, "\r\n\x00") {
		return errors.New("invalid importer package version")
	}
	spec, ok := lookupImporterPackage(input.Importer)
	if !ok {
		return errors.New("unknown importer")
	}
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return err
	}
	if external {
		stage = "install-external-package"
		return x.installExternalImporterPackage(ctx, spec, input)
	}
	cfg, err := x.GetImporterConfig(ctx, spec.key)
	if err != nil {
		return err
	}
	stage = "install-package"
	return x.installBuiltinImporterPackage(ctx, spec, cfg, input)
}

func installDiagnosticValue(value string) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, strings.TrimSpace(value))
	const maximumLength = 256
	if len(value) > maximumLength {
		return value[:maximumLength]
	}
	return value
}

func executeXcmdDeletes(commandRoot, importerFolder, section string) error {
	targetRoot, err := openInstallRoot(importerFolder)
	if err != nil {
		return err
	}
	defer func() { _ = targetRoot.Close() }()
	return executeXcmdDeletesRoot(context.Background(), commandRoot, targetRoot, section)
}

func executeXcmdDeletesRoot(ctx context.Context, commandRoot string, targetRoot *installRoot, section string) error {
	root, err := openInstallRoot(commandRoot)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return executeXcmdDeletesFromRoot(ctx, root, targetRoot, section)
}

func executeXcmdDeletesFromRoot(
	ctx context.Context,
	commandRoot *installRoot,
	targetRoot *installRoot,
	section string,
) error {
	raw, _, err := commandRoot.readFile(filepath.Join("Core", "auto_update.xcmd"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, relative := range parseXcmdDeletes(string(raw), section) {
		if err := ctx.Err(); err != nil {
			return err
		}
		target, err := resolveXcmdDeleteRelative(relative)
		if err != nil {
			return err
		}
		if err := targetRoot.removeAll(target); err != nil {
			return err
		}
	}
	return nil
}

func parseXcmdDeletes(raw, section string) []string {
	current := ""
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if !strings.EqualFold(current, section) {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "delete") {
			continue
		}
		path := strings.TrimSpace(value)
		if path != "" {
			out = append(out, path)
		}
	}
	return out
}

func resolveXcmdDeletePath(importerFolder, raw string) (string, error) {
	relative, err := resolveXcmdDeleteRelative(raw)
	if err != nil {
		return "", err
	}
	target := filepath.Join(importerFolder, relative)
	resolved, err := filepath.Rel(importerFolder, target)
	if err != nil || resolved == ".." || strings.HasPrefix(resolved, ".."+string(os.PathSeparator)) {
		return "", errors.New("delete path escapes importer folder")
	}
	return target, nil
}

func resolveXcmdDeleteRelative(raw string) (string, error) {
	cleaned := strings.ReplaceAll(raw, "\\", "/")
	var filtered []string
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			continue
		}
		filtered = append(filtered, part)
	}
	if len(filtered) < 2 {
		return "", errors.New("explicit removal of entire Core or ShaderFixes folder is not allowed")
	}
	root := strings.ToLower(filtered[0])
	if root != "core" && root != "shaderfixes" {
		return "", errors.New("file or folder removal is allowed only from Core or ShaderFixes folder")
	}
	return filepath.Join(filtered...), nil
}

func shouldSkipImporterMods(relative string) bool {
	first, _, _ := strings.Cut(filepath.ToSlash(relative), "/")
	return strings.EqualFold(first, "Mods")
}

func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")
}

func copyTreeContext(ctx context.Context, source, destination string) error {
	return copyTreeFilterContext(ctx, source, destination, nil)
}

func copyTreeFilter(source, destination string, skip func(string) bool) error {
	return copyTreeFilterContext(context.Background(), source, destination, skip)
}

func copyTreeFilterContext(ctx context.Context, source, destination string, skip func(string) bool) error {
	targetRoot, err := ensureInstallRoot(destination)
	if err != nil {
		return err
	}
	defer func() { _ = targetRoot.Close() }()
	return copyTreeFilterToRoot(ctx, source, targetRoot, skip)
}

func copyTreeFilterToRoot(ctx context.Context, source string, destination *installRoot, skip func(string) bool) error {
	sourceRoot, err := openInstallRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = sourceRoot.Close() }()
	return copyTreeRoots(ctx, sourceRoot, destination, skip)
}

func copyTreeRoots(ctx context.Context, source, destination *installRoot, skip func(string) bool) error {
	return fs.WalkDir(source.root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := filepath.FromSlash(path)
		if skip != nil && relative != "." && skip(relative) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return destination.mkdirAll(relative, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		before, err := source.root.Lstat(relative)
		if err != nil {
			return err
		}
		if err := validateInstallFile(before, filepath.Join(source.path, relative)); err != nil {
			return err
		}
		input, err := source.root.Open(relative)
		if err != nil {
			return err
		}
		after, err := input.Stat()
		if err == nil && !os.SameFile(before, after) {
			err = fmt.Errorf("source file identity changed while copying %q", filepath.Join(source.path, relative))
		}
		if err == nil {
			err = destination.writeFileAtomic(ctx, relative, input, info.Mode().Perm(), nil)
		}
		closeInputErr := input.Close()
		return errors.Join(err, closeInputErr)
	})
}
