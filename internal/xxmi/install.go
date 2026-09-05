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
	packageURL := fmt.Sprintf("https://github.com/SpectrumQT/XXMI-Libs-Package/releases/download/%s/XXMI-PACKAGE-%s.zip", escaped, escaped)
	if err := download.File(ctx, infra.DownloadRequest{URL: packageURL, Destination: zipPath, Header: header}); err != nil {
		return fmt.Errorf("failed to download XXMI package: %w", err)
	}
	extractedPath, err := archive.Extract(ctx, zipPath, filepath.Join(workDir, "extracted"), infra.ExtractOptions{}, nil)
	if err != nil {
		return fmt.Errorf("extract XXMI package: %w", err)
	}
	stagingDir := filepath.Join(workDir, "staging")
	if err := copyTree(extractedPath, stagingDir); err != nil {
		return fmt.Errorf("stage XXMI package: %w", err)
	}
	manifestURL := fmt.Sprintf("https://github.com/SpectrumQT/XXMI-Libs-Package/releases/download/%s/Manifest.json", escaped)
	response, err := httpClient.Fetch(ctx, manifestURL, infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true})
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
	configJSON, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	configJSON = append(configJSON, '\n')
	if err := os.WriteFile(configPath, configJSON, 0o644); err != nil {
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

func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
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
