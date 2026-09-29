package xxmi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi/inject"
)

func (x *XXMI) launchBuiltinGameLocked(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	allowOldLibs bool,
) (returnErr error) {
	stage := "validate"
	defer func() {
		if returnErr != nil {
			if x.eventEmit != nil {
				x.eventEmit("xxmi:launch-progress", map[string]any{
					"importer": key, "stage": "failed", "detail": stage,
				})
			}
			if x.log != nil {
				_ = infra.ReportError(x.log, returnErr, "XXMI.StartGame", infra.Diagnostic{
					Operation: "launch-game", Stage: stage,
					Fields: map[string]any{"importer": key, "mode": cfg.Mode, "importerFolder": cfg.ImporterFolder,
						"gameFolder": cfg.GameFolder},
				})
			}
		}
	}()
	progress := func(next string) {
		stage = next
		if x.eventEmit != nil {
			x.eventEmit("xxmi:launch-progress", map[string]any{"importer": key, "stage": stage})
		}
	}
	if !cfg.Enabled {
		return errors.New("XXMI_NOT_CONFIGURED")
	}
	if err := ValidateImporterSettings(key, cfg); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(cfg.ImporterFolder, "d3dx.ini")); err != nil {
		return fmt.Errorf("XXMI_IMPORTER_NOT_INSTALLED: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.ImporterFolder, "Mods"), 0o700); err != nil {
		return err
	}
	packageSpec, ok := lookupImporterPackage(key)
	if !ok {
		return fmt.Errorf("unknown importer %q", key)
	}
	progress("resolve-game")
	game, err := validateGameFolder(ctx, key, cfg.GameFolder, packageSpec)
	if err != nil {
		return fmt.Errorf("XXMI_GAME_FOLDER_NOT_CONFIGURED: %w", err)
	}
	cfg.GameFolder = game.Path
	if err := ValidateImporterSettings(key, cfg); err != nil {
		return fmt.Errorf("XXMI_GAME_FOLDER_NOT_CONFIGURED: %w", err)
	}
	gameExe := game.ExePath
	processName := filepath.Base(gameExe)
	if len(packageSpec.processNames) > 0 {
		processName = packageSpec.processNames[0]
	}
	progress("auto-update")
	if err := x.autoUpdateForLaunch(ctx, key); err != nil {
		if x.log != nil {
			_ = infra.ReportError(x.log, err, "XXMI.StartGame", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "launch-game", Stage: stage,
				Fields: map[string]any{"importer": key},
			})
		}
		if x.eventEmit != nil {
			x.eventEmit("xxmi:launch-progress", map[string]any{
				"importer": key, "stage": stage, "detail": err.Error(),
			})
		}
	}

	progress("launch-guard")
	if key == "GIMI" && cfg.ConfigureGame && cfg.GIMI != nil && cfg.GIMI.DisableDCR {
		enabled, err := x.gimiDCREnabled(ctx)
		if err != nil {
			return err
		}
		if enabled {
			if err := x.disableGIMIDCR(ctx); err != nil {
				return err
			}
		}
	}
	if err := x.rejectLaunchBlockers(ctx, key, gameExe); err != nil {
		return err
	}
	progress("xcmd-prelaunch")
	if err := executeXcmdDeletes(cfg.ImporterFolder, cfg.ImporterFolder, "PreLaunch"); err != nil {
		return err
	}
	progress("ensure-runtime")
	if key == "EFMI" && cfg.Mode == RuntimeXXMI && !allowOldLibs {
		version, err := x.resolveLibsVersion(ctx, cfg)
		if err != nil {
			return err
		}
		if efmiNeedsNewerLibs(version) {
			return fmt.Errorf("XXMI_LIBS_TOO_OLD: EFMI requires XXMI libraries 1.7.5; selected %s", version)
		}
	}
	progress("deploy-runtime")
	warnings, err := x.deployRuntime(ctx, key, cfg)
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		if x.log != nil {
			x.log.Warn(map[string]any{"importer": key, "warning": warning}, "XXMI.StartGame")
		}
	}
	progress("validate-runtime")
	if err := validateDeployedRuntime(cfg.ImporterFolder, cfg.Mode); err != nil {
		return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
	}
	if x.elevated == nil {
		return errors.New("XXMI_ELEVATION_DENIED: elevated helper is unavailable")
	}
	progress("update-ini")
	if err := x.updateLaunchINI(ctx, key, cfg, processName); err != nil {
		return err
	}
	if cfg.IniOptimizer.Enabled {
		progress("ini-optimizer")
		report, err := x.OptimizeMods(ctx, OptimizeModsInput{Importer: key, ResetCache: cfg.IniOptimizer.ResetCache})
		if err != nil {
			return fmt.Errorf("optimize %s INI files: %w", key, err)
		}
		if x.eventEmit != nil {
			x.eventEmit("xxmi:launch-progress", map[string]any{
				"importer": key, "stage": stage, "optimized": len(report.Changes),
			})
		}
	}
	progress("game-tweaks")
	if err := initializeGameLaunch(ctx, key, cfg); err != nil {
		return err
	}
	if key == "GIMI" && cfg.GIMI != nil && cfg.GIMI.UnlockFPS {
		if err := x.prepareFPSUnlocker(ctx, cfg, gameExe); err != nil {
			return err
		}
	}
	progress("pre-launch")
	if err := runLaunchHook(ctx, cfg.RunPreLaunch, filepath.Dir(gameExe)); err != nil {
		return err
	}
	progress("elevate")
	release, err := x.elevated.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("XXMI_ELEVATION_DENIED: %w", err)
	}
	defer release()
	launchSpec, err := x.builtinLaunchSpec(key, cfg, gameExe, processName)
	if err != nil {
		return err
	}
	progress("inject-launch")
	result, err := x.elevated.LaunchXXMI(ctx, launchSpec)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		if x.log != nil {
			x.log.Warn(map[string]any{"importer": key, "warning": warning}, "XXMI.StartGame")
		}
		if x.eventEmit != nil {
			x.eventEmit("xxmi:launch-progress", map[string]any{
				"importer": key, "stage": stage, "warning": warning,
			})
		}
	}
	progress("post-load")
	if err := runLaunchHook(ctx, cfg.RunPostLoad, filepath.Dir(gameExe)); err != nil {
		return err
	}
	progress("finish")
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	if err := client.XXMIImporters.IncrementLaunchCount(ctx, key); err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Started %s (PID: %d, verified: %t)", key, result.PID, result.InjectionVerified),
			"XXMI.StartGame")
	}
	return nil
}

func efmiNeedsNewerLibs(version string) bool {
	return semver.IsValid("v"+version) && semver.Compare("v"+version, "v1.7.5") < 0
}

func (x *XXMI) acquireImporter(key string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.busy[key] {
		return false
	}
	if x.busy == nil {
		x.busy = make(map[string]bool)
	}
	x.busy[key] = true
	return true
}

func (x *XXMI) releaseImporter(key string) {
	x.mu.Lock()
	delete(x.busy, key)
	x.mu.Unlock()
}

func (x *XXMI) updateLaunchINI(ctx context.Context, key string, cfg ImporterConfig, processName string) error {
	root, err := openInstallRoot(cfg.ImporterFolder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	data, info, err := root.readFile("d3dx.ini")
	if err != nil {
		return err
	}
	doc := parseINI(data)
	doc.SetOption("Loader", "target", processName, true)
	doc.SetOption("Loader", "module", "d3d11.dll", true)
	doc.SetOption("Loader", "launch", "", true)
	if cfg.Mode == RuntimeXXMI {
		doc.SetOption("Loader", "loader", x.elevated.HelperImageName(), true)
	} else {
		doc.RemoveOption("Loader", "loader")
	}
	doc.SetOption("System", "dll_initialization_delay", strconv.Itoa(cfg.XXMIDLLInitDelay), true)
	user32 := syscall.NewLazyDLL("user32.dll")
	metric := user32.NewProc("GetSystemMetrics")
	width, _, _ := metric.Call(0)
	height, _, _ := metric.Call(1)
	doc.SetOption("System", "screen_width", strconv.FormatUint(uint64(width), 10), true)
	doc.SetOption("System", "screen_height", strconv.FormatUint(uint64(height), 10), true)
	applyMigotoINI(doc, key, cfg.Migoto)
	if !doc.Changed() {
		return nil
	}
	return root.writeFileAtomic(ctx, "d3dx.ini", bytes.NewReader(doc.Bytes()), 0o600, info)
}

func applyMigotoINI(doc *iniDocument, key string, options MigotoOptions) {
	if options.EnforceRendering {
		values := map[string]string{
			"texture_hash": "0", "track_texture_updates": "0", "track_region_hashes": "0",
			"allow_buffer_resize": "1",
		}
		switch key {
		case "WWMI":
			values["texture_hash"], values["track_texture_updates"] = "1", "1"
		case "SRMI":
			values["track_implicit_index_buffers"] = "1"
		case "EFMI":
			values["track_region_hashes"], values["track_implicit_index_buffers"] = "1", "1"
			values["allow_buffer_resize"] = "0"
		}
		for _, option := range []string{"texture_hash", "track_texture_updates", "track_region_hashes",
			"track_implicit_index_buffers", "allow_buffer_resize"} {
			if value, ok := values[option]; ok {
				doc.SetOption("Rendering", option, value, true)
			}
		}
	}
	boolean := func(section, option string, enabled bool, on, off string) {
		value := off
		if enabled {
			value = on
		}
		doc.SetOption(section, option, value, true)
	}
	boolean("Logging", "calls", options.CallsLogging, "1", "0")
	boolean("Logging", "debug", options.DebugLogging, "1", "0")
	boolean("Logging", "show_warnings", options.MuteWarnings, "0", "1")
	boolean("Hunting", "hunting", options.EnableHunting, "2", "0")
	boolean("Hunting", "marking_actions", options.DumpShaders, "clipboard hlsl asm regex", "clipboard")
}

func (x *XXMI) builtinLaunchSpec(
	key string,
	cfg ImporterConfig,
	gameExe, processName string,
) (inject.LaunchSpec, error) {
	packageSpec, ok := lookupImporterPackage(key)
	if !ok {
		return inject.LaunchSpec{}, fmt.Errorf("unknown importer %q", key)
	}
	data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName))
	if err != nil {
		return inject.LaunchSpec{}, err
	}
	var deployed runtimeManifest
	if err := json.Unmarshal(data, &deployed); err != nil {
		return inject.LaunchSpec{}, err
	}
	injectMode := "Inject"
	if packageSpec.useHook {
		injectMode = "Hook"
	}
	spec := inject.LaunchSpec{
		Mode: inject.RuntimeMode(cfg.Mode), ProcessName: processName, StartExe: gameExe,
		WorkDir: filepath.Dir(gameExe), StartMethod: cfg.ProcessStartMethod, Priority: cfg.ProcessPriority,
		InjectMode: injectMode, UseHook: packageSpec.useHook, TimeoutSeconds: cfg.ProcessTimeout,
		ModuleDLL: filepath.Join(cfg.ImporterFolder, "d3d11.dll"),
	}
	if cfg.UseLaunchOptions {
		spec.StartArgs, err = splitLaunchOptions(cfg.LaunchOptions)
		if err != nil {
			return inject.LaunchSpec{}, fmt.Errorf("parse launch options: %w", err)
		}
	}
	if strings.EqualFold(processName, "Client-Win64-Shipping.exe") {
		if cfg.UseLaunchOptions {
			spec.StartExe = filepath.Join(filepath.Dir(gameExe), "Client", "Binaries", "Win64", processName)
			spec.WorkDir = filepath.Dir(spec.StartExe)
		}
		spec.StartArgs = append([]string{"-dx11"}, spec.StartArgs...)
	}
	if strings.EqualFold(processName, "Endfield.exe") {
		spec.StartArgs = append([]string{"-force-d3d11"}, spec.StartArgs...)
	}
	if cfg.GIMI != nil && cfg.GIMI.UnlockFPS {
		folder, err := fpsUnlockerFolder()
		if err != nil {
			return inject.LaunchSpec{}, err
		}
		spec.StartExe = filepath.Join(folder, "unlockfps_nc.exe")
		spec.WorkDir = folder
		spec.StartArgs = nil
	}
	if cfg.CustomLaunch.Enabled {
		spec.CustomLaunchCmd = cfg.CustomLaunch.Command
		spec.InjectMode = cfg.CustomLaunch.InjectMode
	}
	if cfg.ExtraLibraries.Enabled {
		spec.ExtraDLLs = append([]string(nil), cfg.ExtraLibraries.Paths...)
	}
	if cfg.Mode == RuntimeXXMI {
		version := strings.TrimPrefix(deployed.Source, "xxmi-libs@")
		if version == deployed.Source || version == "" {
			return inject.LaunchSpec{}, errors.New("XXMI_RUNTIME_CORRUPTED: invalid library source")
		}
		cacheRoot, err := xxmiCacheRoot()
		if err != nil {
			return inject.LaunchSpec{}, err
		}
		spec.LoaderDLL, err = verifiedLaunchFile(
			filepath.Join(cacheRoot, "packages", "xxmi-libs", version, "3dmloader.dll"),
		)
		if err != nil {
			return inject.LaunchSpec{}, err
		}
	} else {
		spec.LegacyLoader, err = verifiedLaunchFile(filepath.Join(cfg.ImporterFolder, "3DMigoto Loader.exe"))
		if err != nil {
			return inject.LaunchSpec{}, err
		}
		if len(spec.ExtraDLLs) > 0 {
			version := cfg.XXMIVersion.Pinned
			if version == "" {
				version = newestCachedPackageVersion("xxmi-libs")
			}
			if version == "" {
				return inject.LaunchSpec{}, errors.New("XXMI_LOADER_TOO_OLD: extra DLLs require cached XXMI libraries")
			}
			cacheRoot, err := xxmiCacheRoot()
			if err != nil {
				return inject.LaunchSpec{}, err
			}
			if err := verifyXXMILibsCache(
				filepath.Join(cacheRoot, "packages", "xxmi-libs", version),
				version,
			); err != nil {
				return inject.LaunchSpec{}, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
			}
			spec.LoaderDLL, err = verifiedLaunchFile(
				filepath.Join(cacheRoot, "packages", "xxmi-libs", version, "3dmloader.dll"),
			)
			if err != nil {
				return inject.LaunchSpec{}, err
			}
		}
	}
	return spec, nil
}

func verifiedLaunchFile(path string) (inject.VerifiedFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return inject.VerifiedFile{}, err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return inject.VerifiedFile{}, err
	}
	return inject.VerifiedFile{Path: path, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
