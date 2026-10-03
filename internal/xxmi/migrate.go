package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type ExternalLauncher struct {
	Path      string   `json:"path"`
	Importers []string `json:"importers"`
}

type ImportExternalLauncherInput struct {
	Path     string             `json:"path"`
	Root     string             `json:"root"`
	UserData ImportUserDataMode `json:"userData"`
}

// ImportedImporter maps an imported importer's external launcher folder to its new built-in folder.
type ImportedImporter struct {
	Key            string `json:"key"`
	PreviousFolder string `json:"previousFolder"`
	ImporterFolder string `json:"importerFolder"`
}

type importerImportPlan struct {
	spec      importerPackageSpec
	source    string
	cfg       ImporterConfig
	installed string
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

// ImportExternalLauncher imports an external XXMI Launcher into the built-in runtime. Each importer gets its own
// folder under root with a freshly installed package, and its user data is linked or moved there per
// input.UserData. Every filesystem change is undone when the import fails.
func (x *XXMI) ImportExternalLauncher(
	ctx context.Context, input ImportExternalLauncherInput,
) (imported []ImportedImporter, returnErr error) {
	path := strings.TrimSpace(input.Path)
	root := strings.TrimSpace(input.Root)
	stage := "validate-input"
	rollbackState := "not-started"
	defer func() {
		if returnErr != nil {
			returnErr = infra.ReportError(x.log, returnErr, "XXMI.importExternalLauncher", infra.Diagnostic{
				Operation: "import-external-launcher", Stage: stage,
				Fields: map[string]any{
					"external_path": path, "root": root, "user_data": string(input.UserData),
					"rollback": rollbackState,
				},
			})
		}
	}()
	if input.UserData != ImportUserDataKeep && input.UserData != ImportUserDataMove {
		return nil, fmt.Errorf("invalid user data mode %q", input.UserData)
	}
	if err := validateLocalFolder("external launcher", path, true); err != nil {
		return nil, err
	}
	config, parsed, err := readAndValidateConfig(filepath.Join(path, xxmiConfigName))
	if err != nil {
		return nil, err
	}
	if root == "" {
		if root, err = xxmiCacheRoot(); err != nil {
			return nil, err
		}
	}
	if err := validateLocalFolder("xxmi root", root, true); err != nil {
		return nil, err
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil, errors.New("XXMI settings store is not configured")
	}

	launcher, _ := config["Launcher"].(map[string]any)
	autoUpdate, _ := launcher["auto_update"].(bool)
	includePrereleases, _ := launcher["pre_release"].(bool)
	libsVersion := dllVersion(&path)
	packageRows := []db.XXMIPackageRow{}
	importPackageState := func(id string, pkg PackageInfo) {
		if pkg.LatestVersion == "" && pkg.SkippedVersion == "" {
			return
		}
		row := db.XXMIPackageRow{
			Package: id, LatestReleaseNotes: &pkg.LatestReleaseNotes,
			UpdateCheckTime: int64(pkg.UpdateCheckTime),
		}
		if pkg.LatestVersion != "" {
			latest := normalizeVersion(pkg.LatestVersion)
			row.LatestVersion = &latest
		}
		if pkg.SkippedVersion != "" {
			skipped := normalizeVersion(pkg.SkippedVersion)
			row.SkippedVersion = &skipped
		}
		packageRows = append(packageRows, row)
	}
	plans := []importerImportPlan{}
	for _, key := range []string{"GIMI", "SRMI", "ZZMI", "WWMI", "HIMI", "EFMI"} {
		info := parsed.Importers[key]
		folder := info.Importer.ImporterFolder
		if !filepath.IsAbs(folder) {
			folder = filepath.Join(path, folder)
		}
		if parsed.Packages.Packages[key].LatestVersion == "" && !fileExists(filepath.Join(folder, "d3dx.ini")) {
			continue
		}
		spec, _ := lookupImporterPackage(key)
		cfg, err := DefaultImporterConfig(key, root)
		if err != nil {
			return nil, err
		}
		cfg.Enabled = true
		cfg.GameFolder = info.Importer.GameFolder
		if cfg.GameFolder != "" && !filepath.IsAbs(cfg.GameFolder) {
			cfg.GameFolder = filepath.Join(path, cfg.GameFolder)
		}
		cfg.OverwriteINI = info.Importer.OverwriteINI
		cfg.Mode = RuntimeXXMI

		// The built-in folder starts empty, so an importer whose deployed version cannot be read falls back to the
		// launcher's latest known release; without either, the import would leave an enabled importer with no package.
		installed := readImporterVersion(folder, spec)
		if installed == nil && parsed.Packages.Packages[key].LatestVersion != "" {
			latest := normalizeVersion(parsed.Packages.Packages[key].LatestVersion)
			installed = &latest
		}
		if installed == nil {
			return nil, fmt.Errorf("%w: %s in %q", errImportVersionUnknown, key, folder)
		}
		if !autoUpdate {
			cfg.PackageVersion = VersionPin{Pinned: *installed}
		}
		wrapper, _ := config["Importers"].(map[string]any)[key].(map[string]any)
		importer, _ := wrapper["Importer"].(map[string]any)
		migoto, _ := wrapper["Migoto"].(map[string]any)
		mapExternalImporterSettings(&cfg, importer, migoto)
		for i, library := range cfg.ExtraLibraries.Paths {
			if !filepath.IsAbs(library) {
				cfg.ExtraLibraries.Paths[i] = filepath.Join(path, library)
			}
		}
		if err := ValidateImporterSettings(key, cfg); err != nil {
			return nil, fmt.Errorf("import %s config: %w", key, err)
		}
		plans = append(plans, importerImportPlan{
			spec: spec, source: filepath.Clean(folder), cfg: cfg, installed: *installed,
		})
		importPackageState("importer:"+key, parsed.Packages.Packages[key])
	}
	importPackageState("xxmi-libs", parsed.Packages.Packages["XXMI"])
	importPackageState("gi-fps-unlocker", parsed.Packages.Packages["GI-FPS-Unlocker"])

	x.packageMu.Lock()
	defer x.packageMu.Unlock()
	stage = "check-importer-folders"
	for _, plan := range plans {
		if err := checkImporterFolderMigration(
			input.UserData, plan.spec.key, plan.source, plan.cfg.ImporterFolder,
		); err != nil {
			return nil, err
		}
		for _, name := range append(slices.Clone(plan.spec.gameExeNames), plan.spec.processNames...) {
			pid, err := x.findProcess(ctx, name)
			if err != nil {
				return nil, err
			}
			if pid != 0 {
				return nil, errors.New("XXMI_GAME_RUNNING")
			}
		}
	}

	stage = "import-shared-packages"
	if libsVersion != nil {
		if err := importExternalLibs(path, *libsVersion); err != nil {
			return nil, fmt.Errorf("import XXMI libraries: %w", err)
		}
	}
	if fileExists(filepath.Join(path, "Resources", "Packages", "GI-FPS-Unlocker", "Manifest.json")) {
		if err := importExternalFPSUnlocker(path); err != nil {
			return nil, fmt.Errorf("import GI FPS Unlocker: %w", err)
		}
	}

	stage = "pause-importer-watchers"
	resume, err := x.beginImporterMaintenance(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rollbackState == "committed" && input.UserData == ImportUserDataMove {
			resume(imported)
			return
		}
		resume(nil)
	}()

	migration := &importerFolderMigration{mode: input.UserData}
	defer func() {
		if returnErr == nil || rollbackState == "not-started" {
			return
		}
		rollbackState = "rolling-back"
		if err := migration.rollback(); err != nil {
			rollbackState = "rollback-failed"
			returnErr = errors.Join(returnErr, err)
			return
		}
		rollbackState = "rolled-back"
	}()
	importRows := make([]db.XXMIImporterRow, 0, len(plans))
	for _, plan := range plans {
		rollbackState = "pending"
		stage = "prepare-" + plan.spec.key
		if err := migration.prepare(plan.source, plan.cfg.ImporterFolder, plan.cfg.OverwriteINI); err != nil {
			return nil, err
		}
		stage = "install-" + plan.spec.key
		if err := x.installImporter(ctx, plan.spec, plan.cfg, InstallImporterPackageInput{
			Importer: plan.spec.key, Version: plan.installed,
		}); err != nil {
			return nil, fmt.Errorf("install %s %s: %w", plan.spec.key, plan.installed, err)
		}
		data, err := json.Marshal(plan.cfg)
		if err != nil {
			return nil, err
		}
		importRows = append(importRows, db.XXMIImporterRow{Key: plan.spec.key, Config: string(data)})
		imported = append(imported, ImportedImporter{
			Key: plan.spec.key, PreviousFolder: plan.source, ImporterFolder: plan.cfg.ImporterFolder,
		})
	}

	stage = "save"
	rootValue := root
	autoUpdateValue := strconv.FormatBool(autoUpdate)
	prereleasesValue := strconv.FormatBool(includePrereleases)
	settings := map[string]*string{
		"xxmi_root":                &rootValue,
		"xxmi_auto_update":         &autoUpdateValue,
		"xxmi_include_prereleases": &prereleasesValue,
	}

	// Every imported importer follows the shared version, which stays on the launcher's libraries.
	if libsVersion != nil {
		settings[sharedLibsVersionKey] = libsVersion
	}
	if err := client.XXMIImporters.ApplyImport(ctx, importRows, packageRows, settings); err != nil {
		return nil, err
	}
	rollbackState = "committed"
	return imported, nil
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
	getFloat := func(key string, fallback float64) float64 {
		value, ok := importer[key].(float64)
		if !ok {
			return fallback
		}
		return value
	}
	if value := getString("process_start_method"); value != "" {
		cfg.ProcessStartMethod = value
	}
	if value := getString("process_priority"); value != "" {
		cfg.ProcessPriority = value
	}
	if value := getString("window_mode"); value != "" {
		switch strings.ToLower(strings.ReplaceAll(value, " ", "")) {
		case "windowed":
			cfg.WindowMode = "Windowed"
		case "borderless":
			cfg.WindowMode = "Borderless"
		case "fullscreen":
			cfg.WindowMode = "Fullscreen"
		case "exclusivefullscreen":
			cfg.WindowMode = "Exclusive Fullscreen"
		default:
			cfg.WindowMode = value
		}
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
	if signatures, ok := importer["deployed_migoto_signatures"].(map[string]any); ok {
		cfg.DeployedSignatures = make(map[string]string, len(signatures))
		for name, value := range signatures {
			if signature, ok := value.(string); ok {
				cfg.DeployedSignatures[name] = signature
			}
		}
	}
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
		cfg.WWMI.MeshLODDistanceBaseFOV = getInt("mesh_lod_distance_lod_base_fov", cfg.WWMI.MeshLODDistanceBaseFOV)
		cfg.WWMI.MeshLODDistanceScale = getFloat("mesh_lod_distance_scale", cfg.WWMI.MeshLODDistanceScale)
		cfg.WWMI.MeshLODDistanceOffset = getFloat("mesh_lod_distance_offset", cfg.WWMI.MeshLODDistanceOffset)
		cfg.WWMI.TextureStreamingBoost = getFloat("texture_streaming_boost", cfg.WWMI.TextureStreamingBoost)
		cfg.WWMI.TextureStreamingMinBoost = getFloat("texture_streaming_min_boost", cfg.WWMI.TextureStreamingMinBoost)
		cfg.WWMI.TextureStreamingUseAll = getBool("texture_streaming_use_all_mips", cfg.WWMI.TextureStreamingUseAll)
		cfg.WWMI.TextureStreamingPoolSize = getInt("texture_streaming_pool_size", cfg.WWMI.TextureStreamingPoolSize)
		cfg.WWMI.TextureStreamingLimitVRAM = getBool(
			"texture_streaming_limit_to_vram",
			cfg.WWMI.TextureStreamingLimitVRAM,
		)
		cfg.WWMI.TextureStreamingFixedPool = getBool(
			"texture_streaming_fixed_pool_size",
			cfg.WWMI.TextureStreamingFixedPool,
		)
		if perf, ok := importer["perf_tweaks"].(map[string]any); ok {
			if settings, ok := perf["SystemSettings"].(map[string]any); ok {
				cfg.WWMI.PerfTweaks = make(map[string]float64, len(settings))
				for name, value := range settings {
					if number, ok := value.(float64); ok {
						cfg.WWMI.PerfTweaks[name] = number
					}
				}
			}
		}
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func importExternalLibs(externalRoot, version string) error {
	version = normalizeVersion(strings.TrimSpace(version))
	if version == "" || version == "." || version == ".." || strings.ContainsAny(version, `\/:*?"<>|`) {
		return errors.New("invalid external XXMI libraries version")
	}
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	parent := filepath.Join(cacheRoot, "packages", "xxmi-libs")
	destination := filepath.Join(parent, version)
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("XXMI libraries cache is not a regular directory")
		}
		if verifyXXMILibsCache(destination, version) == nil {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
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
	return replaceCorruptLibsCache(cacheRoot, staging, destination, version)
}
