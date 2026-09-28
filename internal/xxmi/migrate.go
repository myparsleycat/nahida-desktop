package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type ExternalLauncher struct {
	Path      string   `json:"path"`
	Importers []string `json:"importers"`
}

type ImportExternalLauncherInput struct {
	Path string `json:"path"`
	Root string `json:"root"`
}

func (x *XXMI) DetectExternalLauncher(ctx context.Context) (*ExternalLauncher, error) {
	path, err := x.externalLauncherPath(ctx)
	if err != nil {
		return nil, err
	}
	if path == nil || !isValidConfig(filepath.Join(*path, xxmiConfigName)) {
		path, err = x.findExternalLauncherPath(ctx)
		if err != nil || path == nil {
			return nil, err
		}
	}
	_, parsed, err := readAndValidateConfig(filepath.Join(*path, xxmiConfigName))
	if err != nil {
		return nil, err
	}
	out := &ExternalLauncher{Path: *path}
	for _, key := range []string{"GIMI", "SRMI", "ZZMI", "WWMI", "HIMI", "EFMI"} {
		folder := parsed.Importers[key].Importer.ImporterFolder
		if !filepath.IsAbs(folder) {
			folder = filepath.Join(*path, folder)
		}
		if parsed.Packages.Packages[key].LatestVersion != "" || fileExists(filepath.Join(folder, "d3dx.ini")) {
			out.Importers = append(out.Importers, key)
		}
	}
	return out, nil
}

func (x *XXMI) findExternalLauncherPath(ctx context.Context) (*string, error) {
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData != "" {
		candidate := filepath.Join(appData, "XXMI Launcher")
		if isValidConfig(filepath.Join(candidate, xxmiConfigName)) {
			return &candidate, nil
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	roots, err := x.searchRoots()
	if err != nil {
		return nil, err
	}
	var diagnostics infra.DiagnosticBatch
	defer diagnostics.Report(x.log, "XXMI", "find-config")
	result, err := findFileAcrossRoots(ctx, roots, xxmiConfigName, map[string]struct{}{"Backups": {}}, diagnostics.Add)
	if err != nil || result == nil {
		return nil, err
	}
	directory := filepath.Dir(*result)
	return &directory, nil
}

func isValidConfig(path string) bool {
	_, _, err := readAndValidateConfig(path)
	return err == nil
}

func (x *XXMI) ImportExternalLauncher(ctx context.Context, input ImportExternalLauncherInput) error {
	path := strings.TrimSpace(input.Path)
	if err := validateLocalFolder("external launcher", path, true); err != nil {
		return err
	}
	config, parsed, err := readAndValidateConfig(filepath.Join(path, xxmiConfigName))
	if err != nil {
		return err
	}
	root := strings.TrimSpace(input.Root)
	if root == "" {
		root = path
	}
	if err := validateLocalFolder("xxmi root", root, true); err != nil {
		return err
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	launcher, _ := config["Launcher"].(map[string]any)
	autoUpdate, _ := launcher["auto_update"].(bool)
	libsVersion := dllVersion(&path)
	if libsVersion != nil {
		if err := importExternalLibs(path, *libsVersion); err != nil {
			return fmt.Errorf("import XXMI libraries: %w", err)
		}
	}
	if fileExists(filepath.Join(path, "Resources", "Packages", "GI-FPS-Unlocker", "Manifest.json")) {
		if err := importExternalFPSUnlocker(path); err != nil {
			return fmt.Errorf("import GI FPS Unlocker: %w", err)
		}
	}
	for _, key := range []string{"GIMI", "SRMI", "ZZMI", "WWMI", "HIMI", "EFMI"} {
		info := parsed.Importers[key]
		folder := info.Importer.ImporterFolder
		if !filepath.IsAbs(folder) {
			folder = filepath.Join(path, folder)
		}
		if parsed.Packages.Packages[key].LatestVersion == "" && !fileExists(filepath.Join(folder, "d3dx.ini")) {
			continue
		}
		cfg, err := DefaultImporterConfig(key, root)
		if err != nil {
			return err
		}
		cfg.Enabled = true
		cfg.ImporterFolder = filepath.Clean(folder)
		cfg.GameFolder = info.Importer.GameFolder
		if cfg.GameFolder != "" && !filepath.IsAbs(cfg.GameFolder) {
			cfg.GameFolder = filepath.Join(path, cfg.GameFolder)
		}
		cfg.OverwriteINI = info.Importer.OverwriteINI
		cfg.Mode = RuntimeXXMI
		if !autoUpdate {
			if spec, ok := lookupImporterPackage(key); ok {
				if installed := readImporterVersion(cfg.ImporterFolder, spec); installed != nil {
					cfg.PackageVersion = VersionPin{Pinned: *installed}
				}
			}
		}
		if libsVersion != nil {
			cfg.XXMIVersion = VersionPin{Pinned: *libsVersion}
		}
		wrapper, _ := config["Importers"].(map[string]any)[key].(map[string]any)
		importer, _ := wrapper["Importer"].(map[string]any)
		migoto, _ := wrapper["Migoto"].(map[string]any)
		mapExternalImporterSettings(&cfg, importer, migoto)
		if err := ValidateImporterSettings(key, cfg); err != nil {
			return fmt.Errorf("import %s config: %w", key, err)
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := client.XXMIImporters.Upsert(ctx, key, string(data)); err != nil {
			return err
		}
		pkg := parsed.Packages.Packages[key]
		if pkg.LatestVersion != "" {
			latest := normalizeVersion(pkg.LatestVersion)
			if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
				Package: "importer:" + key, LatestVersion: &latest,
				LatestReleaseNotes: &pkg.LatestReleaseNotes, UpdateCheckTime: int64(pkg.UpdateCheckTime),
			}); err != nil {
				return err
			}
		}
	}
	return x.SetRoot(ctx, root)
}

func mapExternalImporterSettings(cfg *ImporterConfig, importer, migoto map[string]any) {
	getString := func(key string) string { value, _ := importer[key].(string); return value }
	getBool := func(key string, fallback bool) bool {
		value, ok := importer[key].(bool)
		if !ok {
			return fallback
		}
		return value
	}
	getInt := func(key string, fallback int) int {
		value, ok := importer[key].(float64)
		if !ok {
			return fallback
		}
		return int(value)
	}
	if value := getString("process_start_method"); value != "" {
		cfg.ProcessStartMethod = value
	}
	if value := getString("process_priority"); value != "" {
		cfg.ProcessPriority = value
	}
	if value := getString("window_mode"); value != "" {
		cfg.WindowMode = value
	}
	cfg.ProcessTimeout = getInt("process_timeout", cfg.ProcessTimeout)
	cfg.XXMIDLLInitDelay = getInt("xxmi_dll_init_delay", cfg.XXMIDLLInitDelay)
	cfg.UseLaunchOptions = getBool("use_launch_options", cfg.UseLaunchOptions)
	cfg.LaunchOptions = getString("launch_options")
	cfg.ConfigureGame = getBool("configure_game", cfg.ConfigureGame)
	cfg.LaunchCount = getInt("launch_count", cfg.LaunchCount)
	cfg.RunPreLaunch = CommandHook{
		Enabled: getBool("run_pre_launch_enabled", false),
		Command: getString("run_pre_launch"),
		Wait:    getBool("run_pre_launch_wait", true),
	}
	cfg.RunPostLoad = CommandHook{
		Enabled: getBool("run_post_load_enabled", false),
		Command: getString("run_post_load"),
		Wait:    getBool("run_post_load_wait", true),
	}
	cfg.CustomLaunch = CustomLaunch{
		Enabled:    getBool("custom_launch_enabled", false),
		Command:    getString("custom_launch"),
		InjectMode: getString("custom_launch_inject_mode"),
	}
	if cfg.CustomLaunch.InjectMode == "" {
		cfg.CustomLaunch.InjectMode = "Hook"
	}
	cfg.ExtraLibraries.Enabled = getBool("extra_libraries_enabled", false)
	for _, line := range strings.Split(getString("extra_libraries"), "\n") {
		if path := strings.TrimSpace(line); path != "" {
			cfg.ExtraLibraries.Paths = append(cfg.ExtraLibraries.Paths, path)
		}
	}
	for key, target := range map[string]*bool{
		"enforce_rendering": &cfg.Migoto.EnforceRendering, "enable_hunting": &cfg.Migoto.EnableHunting,
		"dump_shaders": &cfg.Migoto.DumpShaders, "mute_warnings": &cfg.Migoto.MuteWarnings,
		"calls_logging": &cfg.Migoto.CallsLogging, "debug_logging": &cfg.Migoto.DebugLogging,
		"unsafe_mode": &cfg.Migoto.UnsafeMode,
	} {
		if value, ok := migoto[key].(bool); ok {
			*target = value
		}
	}
	if cfg.GIMI != nil {
		cfg.GIMI.UnlockFPS = getBool("unlock_fps", cfg.GIMI.UnlockFPS)
		cfg.GIMI.UnlockFPSValue = getInt("unlock_fps_value", cfg.GIMI.UnlockFPSValue)
		cfg.GIMI.EnableHDR = getBool("enable_hdr", cfg.GIMI.EnableHDR)
		cfg.GIMI.DisableDCR = getBool("disable_dcr", cfg.GIMI.DisableDCR)
	}
	if cfg.SRMI != nil {
		cfg.SRMI.UnlockFPS = getBool("unlock_fps", cfg.SRMI.UnlockFPS)
	}
	if cfg.HIMI != nil {
		cfg.HIMI.UnlockFPS = getBool("unlock_fps", cfg.HIMI.UnlockFPS)
		cfg.HIMI.UnlockFPSValue = getInt("unlock_fps_value", cfg.HIMI.UnlockFPSValue)
	}
	if cfg.WWMI != nil {
		cfg.WWMI.UnlockFPS = getBool("unlock_fps", cfg.WWMI.UnlockFPS)
		cfg.WWMI.ApplyPerfTweaks = getBool("apply_perf_tweaks", cfg.WWMI.ApplyPerfTweaks)
		cfg.WWMI.ForceMaxLODBias = getBool("force_max_lod_bias", cfg.WWMI.ForceMaxLODBias)
		cfg.WWMI.DisableWoundedFX = getBool("disable_wounded_fx", cfg.WWMI.DisableWoundedFX)
		cfg.WoundedFXDecided = getBool("disable_wounded_fx_warned", cfg.WoundedFXDecided)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func importExternalLibs(externalRoot, version string) error {
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	parent := filepath.Join(cacheRoot, "packages", "xxmi-libs")
	destination := filepath.Join(parent, version)
	if _, err := os.Stat(destination); err == nil {
		return verifyXXMILibsCache(destination, version)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, version+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	source := filepath.Join(externalRoot, "Resources", "Packages", "XXMI")
	for _, name := range append([]string{"Manifest.json"}, xxmiLibraryFiles...) {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staging, name), data, 0o600); err != nil {
			return err
		}
	}
	if err := verifyXXMILibsCache(staging, version); err != nil {
		return err
	}
	return os.Rename(staging, destination)
}
