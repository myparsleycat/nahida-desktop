package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
)

type InstallDLLVersionInput struct {
	Version string `json:"version"`
}

func (x *XXMI) InstallDLLVersion(ctx context.Context, input InstallDLLVersionInput) error {
	version := strings.TrimSpace(input.Version)
	if version == "" {
		return errors.New("invalid version: must be a non-empty string")
	}
	if strings.ContainsAny(version, "\r\n\x00") {
		return errors.New("invalid version")
	}
	if err := x.load(ctx); err != nil {
		return err
	}
	x.mu.Lock()
	if x.busy {
		x.mu.Unlock()
		return errors.New("XXMI is busy")
	}
	if x.path == nil {
		x.mu.Unlock()
		return errors.New("XXMI is not configured")
	}
	x.busy = true
	xxmiPath := *x.path
	download := x.download
	archive := x.archive
	httpClient := x.http
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		x.busy = false
		x.mu.Unlock()
	}()
	if download == nil || archive == nil || httpClient == nil {
		return errors.New("XXMI install services are not configured")
	}
	if err := ensureLauncherClosed(ctx); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "nahida-xxmi-dll-")
	if err != nil {
		return err
	}
	defer func() { x.reportCleanup(os.RemoveAll(workDir), "InstallDLLVersion") }()
	escaped := url.PathEscape(version)
	header := make(http.Header)
	header.Set("User-Agent", "nahida-desktop")
	header.Set("Referer", "https://github.com/SpectrumQT/XXMI-Libs-Package")
	zipPath := filepath.Join(workDir, "package.zip")
	packageURL := fmt.Sprintf(
		"https://github.com/SpectrumQT/XXMI-Libs-Package/releases/download/%s/XXMI-PACKAGE-%s.zip",
		escaped,
		escaped,
	)
	if err := download.File(
		ctx,
		infra.DownloadRequest{URL: packageURL, Destination: zipPath, Header: header},
	); err != nil {
		return fmt.Errorf("failed to download XXMI package: %w", err)
	}
	extractedPath, err := archive.Extract(
		ctx,
		zipPath,
		filepath.Join(workDir, "extracted"),
		infra.ExtractOptions{},
		nil,
	)
	if err != nil {
		return fmt.Errorf("extract XXMI package: %w", err)
	}
	stagingDir := filepath.Join(workDir, "staging")
	if err := copyTree(extractedPath, stagingDir); err != nil {
		return fmt.Errorf("stage XXMI package: %w", err)
	}
	manifestURL := fmt.Sprintf(
		"https://github.com/SpectrumQT/XXMI-Libs-Package/releases/download/%s/Manifest.json",
		escaped,
	)
	response, err := httpClient.Fetch(
		ctx,
		manifestURL,
		infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true},
	)
	if err != nil {
		return err
	}
	if response.Body == nil {
		return errors.New("failed to download XXMI manifest: empty response")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("failed to download XXMI manifest: %s", response.Status)
	}
	manifest, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(manifest) > 1024*1024 {
		return errors.New("XXMI manifest is too large")
	}
	var manifestData struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &manifestData); err != nil {
		return fmt.Errorf("decode XXMI manifest: %w", err)
	}
	if normalizeVersion(manifestData.Version) != normalizeVersion(version) || normalizeVersion(version) == "" {
		return fmt.Errorf("manifest version mismatch: expected %s, got %s", version, manifestData.Version)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "Manifest.json"), manifest, 0o644); err != nil {
		return err
	}
	configPath := filepath.Join(xxmiPath, xxmiConfigName)
	config, _, err := readAndValidateConfig(configPath)
	if err != nil {
		return err
	}
	launcher, ok := config["Launcher"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Launcher section")
	}
	launcher["auto_update"] = false
	if err := writeXXMIConfig(configPath, config); err != nil {
		return err
	}
	destination := filepath.Join(xxmiPath, "Resources", "Packages", "XXMI")
	if err := copyTree(stagingDir, destination); err != nil {
		return fmt.Errorf("install XXMI package: %w", err)
	}
	loaded, parsed, err := readAndValidateConfig(configPath)
	if err != nil {
		return err
	}
	x.mu.Lock()
	x.config = loaded
	x.parsed = parsed
	x.mu.Unlock()
	if x.log != nil {
		x.log.Info("Installed XXMI DLL version "+version+" to "+destination, "XXMI.installDllVersion")
	}
	return nil
}

type InstallImporterPackageInput struct {
	Importer string `json:"importer"`
	Version  string `json:"version"`
}

func (x *XXMI) InstallImporterPackage(ctx context.Context, input InstallImporterPackageInput) (returnErr error) {
	stage := "validate-input"
	rollbackState := "not-started"
	cleanupState := "not-started"
	fields := map[string]any{
		"service": "XXMI", "action": "InstallImporterPackage",
		"importer": installDiagnosticValue(input.Importer),
		"version":  installDiagnosticValue(input.Version),
	}
	defer func() {
		if returnErr == nil {
			return
		}
		fields["rollback"] = rollbackState
		fields["cleanup"] = cleanupState
		returnErr = infra.ReportError(
			x.log,
			returnErr,
			"XXMI.installImporterPackage",
			infra.Diagnostic{Operation: "install-importer-package", Stage: stage, Fields: fields},
		)
	}()

	version := strings.TrimSpace(input.Version)
	if version == "" {
		return errors.New("invalid version: must be a non-empty string")
	}
	if strings.ContainsAny(version, "\r\n\x00") {
		return errors.New("invalid version")
	}
	spec, ok := lookupImporterPackage(input.Importer)
	if !ok {
		return errors.New("unknown importer")
	}
	fileVersion := normalizeVersion(version)
	if fileVersion == "" {
		return errors.New("invalid version")
	}
	fields["importer"] = spec.key
	fields["version"] = version

	stage = "load-config"
	if err := x.load(ctx); err != nil {
		return err
	}
	x.mu.Lock()
	if x.busy {
		x.mu.Unlock()
		return errors.New("XXMI is busy")
	}
	if x.path == nil {
		x.mu.Unlock()
		return errors.New("XXMI is not configured")
	}
	if _, exists := x.parsed.Importers[spec.key]; !exists {
		x.mu.Unlock()
		return fmt.Errorf("importer %s not found", spec.key)
	}
	x.busy = true
	xxmiPath := *x.path
	importerFolder := x.importerFolderLocked(spec.key)
	overwriteINI := x.parsed.Importers[spec.key].Importer.OverwriteINI
	download := x.download
	archive := x.archive
	x.mu.Unlock()
	xxmiPath, err := filepath.Abs(xxmiPath)
	if err != nil {
		return err
	}
	importerFolder, err = filepath.Abs(importerFolder)
	if err != nil {
		return err
	}
	xxmiPath = filepath.Clean(xxmiPath)
	importerFolder = filepath.Clean(importerFolder)
	configPath := filepath.Join(xxmiPath, xxmiConfigName)
	fields["xxmi_path"] = xxmiPath
	fields["importer_path"] = importerFolder
	fields["config_path"] = configPath
	defer func() {
		x.mu.Lock()
		x.busy = false
		x.mu.Unlock()
	}()
	if download == nil || archive == nil {
		return errors.New("XXMI install services are not configured")
	}
	if strings.TrimSpace(importerFolder) == "" || filepath.Clean(importerFolder) == filepath.Clean(xxmiPath) {
		return errors.New("importer folder is not configured")
	}

	stage = "close-launcher"
	if err := ensureLauncherClosedAt(ctx, filepath.Join(xxmiPath, launcherImageName)); err != nil {
		return err
	}

	stage = "create-work-directory"
	workDir, err := os.MkdirTemp("", "nahida-xxmi-importer-")
	if err != nil {
		return err
	}
	fields["work_path"] = workDir
	cleanupState = "pending"
	defer func() {
		cleanupErr := os.RemoveAll(workDir)
		if cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			cleanupState = "failed"
		} else {
			cleanupState = "complete"
		}
		x.reportCleanup(cleanupErr, "InstallImporterPackage")
	}()
	escapedTag := url.PathEscape(version)
	assetName := fmt.Sprintf(spec.assetFormat, fileVersion)
	header := make(http.Header)
	header.Set("User-Agent", "nahida-desktop")
	header.Set("Referer", fmt.Sprintf("https://github.com/%s/%s", spec.owner, spec.repo))
	zipPath := filepath.Join(workDir, "package.zip")
	packageURL := fmt.Sprintf(
		"https://github.com/%s/%s/releases/download/%s/%s",
		spec.owner, spec.repo, escapedTag, url.PathEscape(assetName),
	)
	fields["package_url"] = infra.SanitizeLogURL(packageURL)
	stage = "download-package"
	if err := download.File(
		ctx,
		infra.DownloadRequest{URL: packageURL, Destination: zipPath, Header: header},
	); err != nil {
		return fmt.Errorf("failed to download %s package: %w", spec.key, err)
	}
	stage = "extract-package"
	extractedPath, err := archive.Extract(
		ctx,
		zipPath,
		filepath.Join(workDir, "extracted"),
		infra.ExtractOptions{},
		nil,
	)
	if err != nil {
		return fmt.Errorf("extract %s package: %w", spec.key, err)
	}
	stagingDir := filepath.Join(workDir, "staging")
	stage = "stage-package"
	if err := copyTreeContext(ctx, extractedPath, stagingDir); err != nil {
		return fmt.Errorf("stage %s package: %w", spec.key, err)
	}
	stage = "validate-package"
	stagedVersion := readImporterVersion(stagingDir, spec)
	if stagedVersion == nil || normalizeVersion(*stagedVersion) != fileVersion {
		got := ""
		if stagedVersion != nil {
			got = *stagedVersion
		}
		return fmt.Errorf("package version mismatch: expected %s, got %s", version, got)
	}

	stage = "recover-installation"
	transaction, err := beginImporterInstallTransaction(ctx, importerFolder, configPath, spec.key)
	if err != nil {
		return err
	}
	defer func() { x.reportCleanup(transaction.Close(), "InstallImporterPackage") }()

	config, _, err := parseAndValidateConfig(transaction.configRaw)
	if err != nil {
		return err
	}
	if err := setImporterDeployedVersion(config, spec.key, fileVersion); err != nil {
		return err
	}
	configJSON, err := marshalXXMIConfig(config)
	if err != nil {
		return err
	}

	stage = "prepare-transaction"
	prepared := false
	committed := false
	defer func() {
		if !prepared || committed {
			return
		}
		rollbackState = "rolling-back"
		if rollbackErr := transaction.rollback(); rollbackErr != nil {
			rollbackState = "rollback-failed"
			returnErr = infra.WithCause(returnErr, rollbackErr)
			return
		}
		rollbackState = "rolled-back"
	}()
	stageRoot, err := transaction.prepare(ctx)
	if err != nil {
		prepared = transaction.state != ""
		return err
	}
	prepared = true

	var iniBackup []byte
	if !overwriteINI {
		iniBackup, _, err = stageRoot.readFile("d3dx.ini")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = stageRoot.Close()
			return err
		}
	}

	stage = "pre-install"
	if err := executeXcmdDeletesRoot(ctx, stagingDir, stageRoot, "PreInstall"); err != nil {
		_ = stageRoot.Close()
		return fmt.Errorf("pre-install %s package: %w", spec.key, err)
	}
	stage = "copy-package"
	if err := copyTreeFilterToRoot(ctx, stagingDir, stageRoot, shouldSkipImporterMods); err != nil {
		_ = stageRoot.Close()
		return fmt.Errorf("install %s package: %w", spec.key, err)
	}
	stage = "post-install"
	if err := executeXcmdDeletesFromRoot(ctx, stageRoot, stageRoot, "PostInstall"); err != nil {
		_ = stageRoot.Close()
		return fmt.Errorf("post-install %s package: %w", spec.key, err)
	}
	if !overwriteINI && iniBackup != nil {
		if err := stageRoot.writeFileAtomic(
			ctx, "d3dx.ini", bytes.NewReader(iniBackup), 0o644, nil,
		); err != nil {
			_ = stageRoot.Close()
			return err
		}
	}
	if err := stageRoot.Close(); err != nil {
		return err
	}

	stage = "commit"
	rollbackState = "backup-retained"
	loaded, parsed, err := transaction.commit(ctx, configJSON)
	if err != nil {
		return err
	}
	committed = true
	rollbackState = "committed"
	x.reportCleanup(transaction.finish(), "InstallImporterPackage")
	x.mu.Lock()
	x.config = loaded
	x.parsed = parsed
	x.mu.Unlock()
	if x.log != nil {
		x.log.Info(
			"Installed "+spec.key+" package version "+version+" to "+importerFolder,
			"XXMI.installImporterPackage",
		)
	}
	return nil
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

func setImporterDeployedVersion(config map[string]any, key, version string) error {
	launcher, ok := config["Launcher"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Launcher section")
	}
	launcher["auto_update"] = false
	packagesSection, ok := config["Packages"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Packages section")
	}
	packages, ok := packagesSection["packages"].(map[string]any)
	if !ok {
		return errors.New("XXMI Launcher config is missing Packages.packages")
	}
	entry, _ := packages[key].(map[string]any)
	if entry == nil {
		entry = map[string]any{
			"latest_version": "", "skipped_version": "", "deployed_version": version,
			"update_check_time": 0, "latest_release_notes": "", "deployed_release_notes": "",
		}
		packages[key] = entry
		return nil
	}
	entry["deployed_version"] = version
	return nil
}

func writeXXMIConfig(path string, config map[string]any) error {
	configJSON, err := marshalXXMIConfig(config)
	if err != nil {
		return err
	}
	root, err := openInstallRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	_, info, err := root.readFile(filepath.Base(path))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return root.writeFileAtomic(
		context.Background(), filepath.Base(path), bytes.NewReader(configJSON), 0o644, info,
	)
}

func marshalXXMIConfig(config map[string]any) ([]byte, error) {
	configJSON, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return nil, err
	}
	return append(configJSON, '\n'), nil
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

func copyTree(source, destination string) error {
	return copyTreeContext(context.Background(), source, destination)
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
