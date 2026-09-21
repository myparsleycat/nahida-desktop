package xxmi

import (
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

func (x *XXMI) InstallImporterPackage(ctx context.Context, input InstallImporterPackageInput) error {
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
	if err := ensureLauncherClosed(ctx); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "nahida-xxmi-importer-")
	if err != nil {
		return err
	}
	defer func() { x.reportCleanup(os.RemoveAll(workDir), "InstallImporterPackage") }()
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
	if err := download.File(
		ctx,
		infra.DownloadRequest{URL: packageURL, Destination: zipPath, Header: header},
	); err != nil {
		return fmt.Errorf("failed to download %s package: %w", spec.key, err)
	}
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
	if err := copyTree(extractedPath, stagingDir); err != nil {
		return fmt.Errorf("stage %s package: %w", spec.key, err)
	}
	stagedVersion := readImporterVersion(stagingDir, spec)
	if stagedVersion == nil || normalizeVersion(*stagedVersion) != fileVersion {
		got := ""
		if stagedVersion != nil {
			got = *stagedVersion
		}
		return fmt.Errorf("package version mismatch: expected %s, got %s", version, got)
	}

	iniPath := filepath.Join(importerFolder, "d3dx.ini")
	var iniBackup []byte
	if !overwriteINI {
		iniBackup, err = os.ReadFile(iniPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := executeXcmdDeletes(stagingDir, importerFolder, "PreInstall"); err != nil {
		return fmt.Errorf("pre-install %s package: %w", spec.key, err)
	}
	if err := copyImporterTree(stagingDir, importerFolder); err != nil {
		return fmt.Errorf("install %s package: %w", spec.key, err)
	}
	if err := executeXcmdDeletes(importerFolder, importerFolder, "PostInstall"); err != nil {
		return fmt.Errorf("post-install %s package: %w", spec.key, err)
	}
	if !overwriteINI && iniBackup != nil {
		if err := os.WriteFile(iniPath, iniBackup, 0o644); err != nil {
			return err
		}
	}

	configPath := filepath.Join(xxmiPath, xxmiConfigName)
	config, _, err := readAndValidateConfig(configPath)
	if err != nil {
		return err
	}
	if err := setImporterDeployedVersion(config, spec.key, fileVersion); err != nil {
		return err
	}
	if err := writeXXMIConfig(configPath, config); err != nil {
		return err
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
		x.log.Info(
			"Installed "+spec.key+" package version "+version+" to "+importerFolder,
			"XXMI.installImporterPackage",
		)
	}
	return nil
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
	configJSON, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	configJSON = append(configJSON, '\n')
	return os.WriteFile(path, configJSON, 0o644)
}

func executeXcmdDeletes(commandRoot, importerFolder, section string) error {
	raw, err := os.ReadFile(filepath.Join(commandRoot, "Core", "auto_update.xcmd"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, relative := range parseXcmdDeletes(string(raw), section) {
		target, err := resolveXcmdDeletePath(importerFolder, relative)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(target); err != nil {
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
	target := filepath.Join(append([]string{importerFolder}, filtered...)...)
	relative, err := filepath.Rel(importerFolder, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("delete path escapes importer folder")
	}
	return target, nil
}

func copyImporterTree(source, destination string) error {
	return copyTreeFilter(source, destination, shouldSkipImporterMods)
}

func shouldSkipImporterMods(relative string) bool {
	first, _, _ := strings.Cut(filepath.ToSlash(relative), "/")
	return strings.EqualFold(first, "Mods")
}

func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")
}

func copyTree(source, destination string) error {
	return copyTreeFilter(source, destination, nil)
}

func copyTreeFilter(source, destination string, skip func(string) bool) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if skip != nil && relative != "." && skip(relative) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeOutputErr := output.Close()
		closeInputErr := input.Close()
		return errors.Join(copyErr, closeOutputErr, closeInputErr)
	})
}
