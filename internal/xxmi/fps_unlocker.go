package xxmi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/github"
)

var fpsUnlockerRepo = github.Repo{Owner: "SpectrumQT", Name: "GI-FPS-Unlocker-Package"}

var fpsUnlockerFiles = []string{
	"unlockfps_nc.exe", "unlockfps_nc.dll", "unlockfps_nc.deps.json", "unlockfps_nc.runtimeconfig.json",
	"Microsoft.Extensions.DependencyInjection.Abstractions.dll", "Microsoft.Extensions.DependencyInjection.dll",
	"Newtonsoft.Json.dll", "fps_config_template.json",
}

func (x *XXMI) EnsureFPSUnlockerVersion(ctx context.Context, version string) error {
	version = normalizeVersion(strings.TrimSpace(version))
	if version == "" || strings.ContainsAny(version, `\/:*?"<>|`) {
		return errors.New("invalid GI FPS Unlocker version")
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	parent := filepath.Join(root, "packages", "gi-fps-unlocker")
	destination := filepath.Join(parent, version)
	if _, err := os.Stat(filepath.Join(destination, "Manifest.json")); err == nil {
		return verifyFPSUnlockerCache(destination, version)
	}
	releases, err := x.github.AllReleases(ctx, fpsUnlockerRepo)
	if err != nil {
		return err
	}
	var release *github.Release
	for i := range releases {
		if normalizeVersion(releases[i].TagName) == version && !releases[i].Draft {
			release = &releases[i]
			break
		}
	}
	if release == nil {
		return fmt.Errorf("GI FPS Unlocker release %s not found", version)
	}
	signature := releaseSignature(release.Body)
	if signature == "" {
		return fmt.Errorf("GI FPS Unlocker release %s is unsigned", version)
	}
	assetName := "GENSHIN-FPS-UNLOCK-PACKAGE-v" + version + ".zip"
	zipURL := releaseAssetURL(*release, assetName)
	if zipURL == "" {
		return fmt.Errorf("GI FPS Unlocker asset %s not found", assetName)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, version+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	zipPath := filepath.Join(staging, "package.zip")
	if err := x.downloadPackageFile(ctx, "gi-fps-unlocker", version, github.FileRequest{
		Repo: fpsUnlockerRepo, URL: zipURL, Destination: zipPath,
	}); err != nil {
		return fmt.Errorf("download GI FPS Unlocker %s: %w", version, err)
	}
	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return err
	}
	if !zipInfo.Mode().IsRegular() || zipInfo.Size() > 64<<20 {
		return errors.New("GI FPS Unlocker archive is not a regular file or exceeds size limit")
	}
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		return err
	}
	if err := verifyPackageSignature(spectrumPublicKey, signature, zipBytes); err != nil {
		return fmt.Errorf("GI FPS Unlocker %s: %w", version, err)
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	for _, entry := range reader.File {
		name := entry.Name
		if filepath.Base(name) != name ||
			!containsString(fpsUnlockerFiles, name) && name != "Manifest.json" && name != "unlockfps_nc.pdb" {
			return fmt.Errorf("unexpected GI FPS Unlocker archive entry %q", name)
		}
		if name == "unlockfps_nc.pdb" {
			continue
		}
		data, err := readZipEntry(entry, 16<<20)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(staging, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	}
	if err := reader.Close(); err != nil {
		return err
	}
	if err := os.Remove(zipPath); err != nil {
		return err
	}
	if err := verifyFPSUnlockerCache(staging, version); err != nil {
		return err
	}
	if err := os.Rename(staging, destination); err != nil {
		if _, statErr := os.Stat(destination); statErr == nil {
			return verifyFPSUnlockerCache(destination, version)
		}
		return err
	}
	return nil
}

func verifyFPSUnlockerCache(folder, version string) error {
	data, err := os.ReadFile(filepath.Join(folder, "Manifest.json"))
	if err != nil {
		return err
	}
	var manifest xxmiLibraryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode GI FPS Unlocker manifest: %w", err)
	}
	if normalizeVersion(manifest.Version) != version {
		return fmt.Errorf("GI FPS Unlocker manifest version mismatch: %q", manifest.Version)
	}
	for _, name := range fpsUnlockerFiles {
		data, err := os.ReadFile(filepath.Join(folder, name))
		if err != nil {
			return err
		}
		if err := verifyPackageSignature(spectrumPublicKey, manifest.Signatures[name], data); err != nil {
			return fmt.Errorf("GI FPS Unlocker %s: %w", name, err)
		}
	}
	return nil
}

func importExternalFPSUnlocker(externalRoot string) error {
	source := filepath.Join(externalRoot, "Resources", "Packages", "GI-FPS-Unlocker")
	sourceRoot, err := openInstallRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = sourceRoot.Close() }()
	manifestData, _, err := sourceRoot.readFile("Manifest.json")
	if err != nil {
		return err
	}
	if len(manifestData) > 1<<20 {
		return errors.New("GI FPS Unlocker manifest exceeds size limit")
	}
	var manifest xxmiLibraryManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return err
	}
	version := normalizeVersion(manifest.Version)
	if version == "" || strings.ContainsAny(version, `\/:*?"<>|`) {
		return errors.New("invalid external GI FPS Unlocker version")
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	parent := filepath.Join(root, "packages", "gi-fps-unlocker")
	destination := filepath.Join(parent, version)
	if _, err := os.Stat(destination); err == nil {
		return verifyFPSUnlockerCache(destination, version)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, version+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	for _, name := range append([]string{"Manifest.json", "fps_config.json"}, fpsUnlockerFiles...) {
		data, _, err := sourceRoot.readFile(name)
		if name == "fps_config.json" && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if len(data) > 16<<20 || name == "fps_config.json" && len(data) > 1<<20 {
			return fmt.Errorf("external GI FPS Unlocker file %s exceeds size limit", name)
		}
		if err := os.WriteFile(filepath.Join(staging, name), data, 0o600); err != nil {
			return err
		}
	}
	if err := verifyFPSUnlockerCache(staging, version); err != nil {
		return err
	}
	return os.Rename(staging, destination)
}

func fpsUnlockerFolder() (string, error) {
	root, err := xxmiCacheRoot()
	if err != nil {
		return "", err
	}
	parent := filepath.Join(root, "packages", "gi-fps-unlocker")
	version, err := newestLegacyRuntime(parent)
	if err != nil {
		return "", err
	}
	folder := filepath.Join(parent, version)
	if err := verifyFPSUnlockerCache(folder, version); err != nil {
		return "", fmt.Errorf("cached GI FPS Unlocker %s is corrupted: %w", version, err)
	}
	return folder, nil
}

func (x *XXMI) prepareFPSUnlocker(ctx context.Context, cfg ImporterConfig, gameExe string) error {
	folder, err := fpsUnlockerFolder()
	if err != nil {
		releases, listErr := x.ListReleases(ctx, "gi-fps-unlocker")
		if listErr != nil {
			return listErr
		}
		if len(releases) == 0 {
			return errors.New("GI FPS Unlocker has no available release")
		}
		if err := x.EnsureFPSUnlockerVersion(ctx, releases[0].Version); err != nil {
			return err
		}
		folder, err = fpsUnlockerFolder()
		if err != nil {
			return err
		}
	}
	executable := filepath.Join(folder, "unlockfps_nc.exe")
	pid, err := findProcessPID(ctx, executable)
	if err != nil {
		return err
	}
	if pid != 0 {
		if err := killProcessForExecutable(executable)(pid); err != nil {
			return fmt.Errorf("GIMI_FPS_UNLOCKER_RUNNING: %w", err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			pid, err = findProcessPID(ctx, executable)
			if err != nil {
				return err
			}
			if pid == 0 {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("GIMI_FPS_UNLOCKER_RUNNING")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	root, err := openInstallRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	data, info, err := root.readFile("fps_config.json")
	if errors.Is(err, os.ErrNotExist) {
		data, _, err = root.readFile("fps_config_template.json")
	}
	if err != nil {
		return err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("decode GI FPS Unlocker config: %w", err)
	}
	if settings == nil {
		return errors.New("GI FPS Unlocker config must be a JSON object")
	}
	priority := map[string]int{"Realtime": 0, "High": 1, "AboveNormal": 2, "Normal": 3, "BelowNormal": 4, "Low": 5}
	windowModes := map[string][3]bool{
		"Windowed": {false, false, false}, "Borderless": {true, false, false},
		"Fullscreen": {false, true, false}, "Exclusive Fullscreen": {false, true, true},
	}
	window, ok := windowModes[cfg.WindowMode]
	if !ok {
		return fmt.Errorf("invalid GI FPS Unlocker window mode %q", cfg.WindowMode)
	}
	if cfg.GIMI.UnlockFPSValue < 30 || cfg.GIMI.UnlockFPSValue > 1000 {
		return fmt.Errorf("invalid Genshin FPS target %d", cfg.GIMI.UnlockFPSValue)
	}
	settings["GamePath"] = gameExe
	settings["Priority"] = priority[cfg.ProcessPriority]
	settings["AdditionalCommandLine"] = ""
	if cfg.UseLaunchOptions {
		settings["AdditionalCommandLine"] = cfg.LaunchOptions
	}
	settings["PopupWindow"] = window[0]
	settings["Fullscreen"] = window[1]
	settings["IsExclusiveFullscreen"] = window[2]
	settings["FPSTarget"] = cfg.GIMI.UnlockFPSValue
	encoded, err := json.MarshalIndent(settings, "", "    ")
	if err != nil {
		return err
	}
	if bytes.Equal(data, encoded) {
		return nil
	}
	return root.writeFileAtomic(ctx, "fps_config.json", bytes.NewReader(encoded), 0o600, info)
}
