package xxmi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi/inject"
)

func (x *XXMI) launchBuiltinGameLocked(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	options LaunchOptions,
) (returnErr error) {
	stage := "validate"
	runtimeSource := "xxmi-libs@latest"
	if cfg.Mode == RuntimeLegacy {
		runtimeSource = "legacy@latest"
		if cfg.LegacyRuntime != "" {
			runtimeSource = "legacy@" + cfg.LegacyRuntime
		}
	} else if cfg.XXMIVersion.Pinned != "" {
		runtimeSource = "xxmi-libs@" + normalizeVersion(cfg.XXMIVersion.Pinned)
	}
	runtimeProvider := ""
	rollbackState := "not-started"
	gameExe := ""
	defer func() {
		if returnErr != nil {
			if x.eventEmit != nil {
				x.eventEmit("xxmi:launch-progress", map[string]any{
					"importer": key, "stage": "failed", "detail": stage,
				})
			}
			returnErr = infra.ReportError(x.log, returnErr, "XXMI.StartGame", infra.Diagnostic{
				Operation: "launch-game", Stage: stage,
				Fields: map[string]any{
					"importer":        key,
					"mode":            cfg.Mode,
					"source":          runtimeSource,
					"provider":        runtimeProvider,
					"rollback":        rollbackState,
					"importerFolder":  cfg.ImporterFolder,
					"gameFolder":      cfg.GameFolder,
					"gameExe":         gameExe,
					"gameLaunch":      cfg.GameLaunch,
					"injectionMethod": cfg.InjectionMethod,
					"packageVersion":  cfg.PackageVersion.Pinned,
					"reshade":         cfg.ReShade.Enabled,
				},
			})
		}
	}()
	progress := func(next string) {
		stage = next
		if x.eventEmit != nil {
			x.eventEmit("xxmi:launch-progress", map[string]any{"importer": key, "stage": stage})
		}
	}
	warn := func(warning string) {
		if x.log != nil {
			x.log.Warn(map[string]any{"importer": key, "stage": stage, "warning": warning}, "XXMI.StartGame")
		}
		if x.eventEmit != nil {
			x.eventEmit("xxmi:launch-progress", map[string]any{
				"importer": key, "stage": stage, "warning": warning,
			})
		}
	}
	if !cfg.Enabled {
		return errors.New("XXMI_NOT_CONFIGURED")
	}
	if err := ValidateImporterSettings(key, cfg); err != nil {
		return err
	}
	if err := validateInstalledImporterPackage(key, cfg); err != nil {
		return err
	}

	// The question is asked before the pre-launch command so answering it does not run that command twice.
	if wwmiResourceTierArgument(key, cfg) != "" && !cfg.WWMI.ResourceTierDecided {
		return errWWMIResourceTierUndecided
	}
	if d3d11ModeNoticeRequired(key, cfg) {
		return errD3D11ModeNoticeRequired
	}

	// Like the reference launcher, the pre-launch command runs before any other launch step, so it can
	// prepare drives, folders, or mods that the later steps read.
	progress("pre-launch")
	hookDir := ""
	if info, err := os.Stat(cfg.GameFolder); err == nil && info.IsDir() {
		hookDir = cfg.GameFolder
	}
	if err := runLaunchHook(ctx, cfg.RunPreLaunch, hookDir); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return err
		}
		warn(fmt.Sprintf("Pre-launch command exited with code %d", exit.ExitCode()))
	}

	reshadeUsed := cfg.usesReShade()
	if cfg.ReShade.Enabled && !reshadeUsed {
		warn("ReShade needs native injection with the 3DMigoto runtime and was left out of this launch")
	}
	cfg, reshadeWarnings := withoutReplacedReShadeLibraries(cfg)
	for _, warning := range reshadeWarnings {
		warn(warning)
	}

	// One helper lease covers the launch, so importer and game files that need administrator rights
	// and the launch itself share a single UAC prompt.
	lease := elevated.NewFileLease(x.elevated)
	defer lease.Release()

	// Like the reference launcher, a launch that leaves the XXMI DLL out prepares none of the
	// importer's files, so it also does not need them installed.
	migotoDLLUsed, err := x.migotoDLLUsed(ctx, cfg)
	if err != nil {
		return err
	}
	if migotoDLLUsed {
		if _, err := os.Stat(filepath.Join(cfg.ImporterFolder, "d3dx.ini")); err != nil {
			return fmt.Errorf("XXMI_IMPORTER_NOT_INSTALLED: %w", err)
		}
	}
	if err := validateInstalledImporterPackage(key, cfg); err != nil {
		return err
	}
	if migotoDLLUsed {
		if err := os.MkdirAll(filepath.Join(cfg.ImporterFolder, "Mods"), 0o700); err != nil {
			return err
		}
	}
	packageSpec, ok := lookupImporterPackage(key)
	if !ok {
		return fmt.Errorf("unknown importer %q", key)
	}
	progress("resolve-game")
	usesPlatform := cfg.GameLaunch == "Steam" || cfg.GameLaunch == "Epic"
	var platform platformLaunch
	if usesPlatform {
		// A store client knows where it installed the game, so the configured game folder is not consulted.
		resolved, err := x.resolvePlatformLaunch(key, cfg)
		if err != nil {
			return err
		}
		platform, cfg.GameFolder = resolved, resolved.installDir
	}
	game, err := validateGameFolder(ctx, key, cfg.GameFolder, packageSpec)
	if err == nil {
		cfg.GameFolder = game.Path
		err = ValidateImporterSettings(key, cfg)
	}
	switch {
	case err == nil:
		gameExe = game.ExePath
	case cfg.GameLaunch == "Direct":
		return fmt.Errorf("XXMI_GAME_FOLDER_NOT_CONFIGURED: %w", err)
	default:
		// Only a direct launch runs the executable from the game folder. A store install with an
		// unexpected layout, or one that overlaps the importer folder, still launches; it just
		// skips the steps that edit game files.
		cfg.GameFolder = ""
	}
	parentExe, err := x.efmiParentExe(key, cfg, gameExe)
	if err != nil {
		return err
	}
	processName := launchProcessName(cfg, packageSpec, gameExe)
	progress("auto-update")
	if err := x.autoUpdateForLaunch(ctx, key, lease); err != nil {
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
	guards, err := x.launchGuards(ctx)
	if err != nil {
		return err
	}
	if options.IgnoreSmoothMotion {
		guards.smoothMotion = false
	}

	// Mods do not render with DCR on, so it is turned off without asking whenever the XXMI DLL is loaded.
	// Unlike the reference launcher, this does not depend on ConfigureGame: DCR is all that option
	// would control for GIMI, and leaving it on only breaks mods silently.
	checkDCR := key == "GIMI" && migotoDLLUsed && guards.dcr
	if checkDCR {
		err := x.launchSettings.disableGIMIDCR(ctx)
		if err != nil && !errors.Is(err, errGimiDCRUnreadable) {
			return err
		}
		if err != nil {
			x.notifyLaunchWarning(key, launchWarning{code: launchWarningGimiDCRUnreadable, err: err})
			// The guard would only fail the same read again and warn twice.
			checkDCR = false
		}
	}
	blockerTarget := gameExe
	if blockerTarget == "" {
		blockerTarget = processName
	}
	launchSettings := x.builtinLaunchSettings(cfg, migotoDLLUsed, guards)
	if err := x.rejectLaunchBlockersFrom(ctx, key, blockerTarget, checkDCR, launchSettings); err != nil {
		return err
	}
	if migotoDLLUsed {
		if warning := x.dx12Warning(ctx, cfg); warning != nil {
			x.notifyLaunchWarning(key, *warning)
		}
	}
	if x.elevated == nil {
		return errors.New("XXMI_ELEVATION_DENIED: elevated helper is unavailable")
	}
	if migotoDLLUsed {
		progress("xcmd-prelaunch")
		skipped, err := executeXcmdDeletes(cfg.ImporterFolder, cfg.ImporterFolder, "PreLaunch")
		if err != nil {
			return err
		}
		if len(skipped) > 0 {
			warn("Skipped auto_update.xcmd deletes through a symbolic link or junction: " + strings.Join(skipped, ", "))
		}
		progress("ensure-runtime")
		progress("deploy-runtime")
		rollbackState = "not-attempted"
		warnings, err := x.deployRuntime(ctx, key, cfg, false)
		if err != nil {
			return err
		}
		if data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName)); err == nil {
			var deployed runtimeManifest
			if json.Unmarshal(data, &deployed) == nil && deployed.Source != "" {
				runtimeSource = deployed.Source
				runtimeProvider = deployed.Provider
			}
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
		var providerINI []byte
		if cfg.Mode == RuntimeXXMI {
			version, ok := strings.CutPrefix(runtimeSource, "xxmi-libs@")
			if !ok || !semver.IsValid("v"+version) {
				return errors.New("XXMI_RUNTIME_CORRUPTED: invalid library source")
			}
			cacheRoot, err := xxmiCacheRoot()
			if err != nil {
				return err
			}
			cacheFolder := filepath.Join(cacheRoot, "packages", "xxmi-libs", version)
			if err := verifyXXMILibsCache(cacheFolder, version); err != nil {
				return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
			}
			d3d11Folder := cacheFolder
			var companions []string
			if runtimeProvider != "" && !cfg.Migoto.UnsafeMode {
				d3d11Folder, companions, err = x.verifiedProviderDLLFolder(ctx, cacheRoot, runtimeProvider)
				if err != nil {
					return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
				}
			}
			if err := validateXXMIRuntimeFiles(
				cfg.ImporterFolder, cacheFolder, d3d11Folder, companions, cfg.Migoto.UnsafeMode,
			); err != nil {
				return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
			}
			if runtimeProvider != "" {
				// Without the provider's d3dx.ini the file is only edited in place, so the launch goes on
				// without one that cannot be read.
				providerINI, err = x.verifiedProviderINI(ctx, cacheRoot, runtimeProvider)
				if err != nil {
					warn("Skipped the d3dx.ini options of " + runtimeProvider + ": " + err.Error())
				}
			}
		} else if !cfg.Migoto.UnsafeMode {
			id, ok := strings.CutPrefix(runtimeSource, "legacy@")
			if !ok || len(id) != 12 || strings.Trim(id, "0123456789abcdef") != "" {
				return errors.New("XXMI_RUNTIME_CORRUPTED: invalid legacy source")
			}
			cacheRoot, err := xxmiCacheRoot()
			if err != nil {
				return err
			}
			cacheFolder := filepath.Join(cacheRoot, "packages", "legacy-3dmigoto", id)
			if err := validateLegacyRuntimeFiles(cfg.ImporterFolder, cacheFolder); err != nil {
				return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
			}
		}
		progress("update-ini")
		if err := x.updateLaunchINI(ctx, key, cfg, processName, providerINI); err != nil {
			return err
		}

		// The runtimes are updated after the d3d11.dll that picks their variant is deployed, and before the
		// optimizer reads the Mods folder. A failure leaves the installed files for this launch.
		x.mu.RLock()
		updateModRuntimes := x.modRuntimeUpdate
		x.mu.RUnlock()
		if updateModRuntimes != nil && cfg.AutoUpdateModRuntimes {
			progress("mod-runtimes")
			if err := updateModRuntimes(ctx, key); err != nil {
				if infra.IsCancellationError(err) {
					return err
				}
				_ = infra.ReportError(x.log, err, "XXMI.StartGame", infra.Diagnostic{
					Severity: infra.DiagnosticWarn, Operation: "launch-game", Stage: stage,
					Fields: map[string]any{"importer": key},
				})
				if x.eventEmit != nil {
					x.eventEmit("xxmi:launch-progress", map[string]any{
						"importer": key, "stage": stage, "detail": err.Error(),
					})
				}
			}
		}
		if cfg.IniOptimizer.Enabled {
			progress("ini-optimizer")
			input := OptimizeModsInput{Importer: key, ResetCache: cfg.IniOptimizer.ResetCache}
			var unreadable []string
			skipped := func(path string, _ error) {
				if !slices.Contains(unreadable, path) {
					unreadable = append(unreadable, path)
				}
			}
			report, err := x.optimizeMods(ctx, input, skipped)
			var restorable, excluded []string
			if err == nil && len(unreadable) > 0 {
				restorable, excluded = filterImporterAccessPaths(cfg.ImporterFolder, unreadable)
				if len(excluded) > 0 && x.log != nil {
					x.log.Warn(map[string]any{
						"importer": key, "paths": excluded,
						"warning": "Paths could not be resolved within the importer or Mods folders",
					}, "XXMI.StartGame")
				}
			}

			// The launch asks for administrator rights anyway, so the same consent gives the files back and
			// the optimizer runs again over them. Without it they stay skipped. A restored library can add a
			// namespace that mods cached by earlier runs duplicate, so the rerun ignores the cache.
			if len(restorable) > 0 {
				restoreErr := x.restoreImporterAccess(ctx, lease, restorable)
				if restoreErr != nil && infra.IsCancellationError(restoreErr) {
					return restoreErr
				}
				if restoreErr != nil {
					_ = infra.ReportError(x.log, restoreErr, "XXMI.StartGame", infra.Diagnostic{
						Severity: infra.DiagnosticWarn, Operation: "restore-importer-access", Stage: stage,
						Fields: map[string]any{"importer": key, "paths": restorable},
					})
				} else {
					unreadable = excluded
					input.ResetCache = true
					retried, retryErr := x.optimizeMods(ctx, input, skipped)
					report.Changes = append(report.Changes, retried.Changes...)
					err = retryErr
				}
			}
			if err != nil {
				return fmt.Errorf("optimize %s INI files: %w", key, err)
			}
			if len(unreadable) > 0 {
				warn("INI optimizer skipped files this app is not allowed to read: " + strings.Join(unreadable, ", "))
			}
			if x.eventEmit != nil {
				x.eventEmit("xxmi:launch-progress", map[string]any{
					"importer": key, "stage": stage, "optimized": len(report.Changes),
				})
			}
		}
	}

	progress("game-tweaks")
	publish := &gameFilePublisher{
		apply: func(ctx context.Context, ops []elevated.FileOp) error {
			return elevationDenied(lease.Apply(ctx, ops))
		},
		reportCleanup: func(err error) { x.reportCleanup(err, "game-tweaks") },
	}
	if err := initializeGameLaunch(ctx, key, cfg, migotoDLLUsed, publish); err != nil {
		return err
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if key == "WWMI" && cfg.WWMI != nil && cfg.WWMI.RetiredEngineOptionsPending && migotoDLLUsed &&
		cfg.GameFolder != "" && cfg.GameLaunch != "Custom" && cfg.GameLaunch != "Manual" && client != nil {
		// initializeGameLaunch just removed the retired Engine.ini options; later launches keep
		// whatever the user adds there.
		if err := client.XXMIImporters.SetConfigFlag(
			ctx, key, "$.wwmi.retiredEngineOptionsPending", false,
		); err != nil {
			warn("Engine.ini cleanup was not recorded: " + err.Error())
		}
	}
	if usesFPSUnlocker(cfg) {
		if err := x.prepareFPSUnlocker(ctx, cfg, gameExe); err != nil {
			return fmt.Errorf("GIMI_FPS_UNLOCKER_CONFIG_FAILED: %w", err)
		}
	}
	reshadeModule := ""
	if reshadeUsed {
		progress("reshade")
		if x.reshade == nil {
			return errors.New("RESHADE_NOT_INSTALLED: ReShade is not configured")
		}
		reshadeModule, err = x.reshade.PrepareLaunch(ctx, key)
		if err != nil {
			return err
		}
	}
	if cfg.GameLaunch == "Steam" {
		progress("platform-options")
		options := platformCommandLine(key, cfg, gameExe)
		if err := x.prepareSteamLaunch(ctx, platform, options, cfg.ConfigurePlatformLaunchOptions); err != nil {
			return err
		}
	}
	progress("elevate")
	if err := elevationDenied(lease.Hold(ctx)); err != nil {
		return err
	}
	launchSpec, err := x.builtinLaunchSpec(ctx, key, cfg, gameExe, processName, parentExe, migotoDLLUsed)
	if err != nil {
		return err
	}
	if reshadeModule != "" {
		launchSpec.PreloadDLLs = []string{reshadeModule}

		// A window hook loads the XXMI DLL whenever the game first handles a message, which cannot be
		// ordered after ReShade.
		if launchSpec.InjectMode == "Hook" {
			launchSpec.InjectMode, launchSpec.UseHook = "Inject", false
		}
		if err := x.useExtraDLLInjector(ctx, cfg, &launchSpec); err != nil {
			return err
		}
	}
	if usesPlatform {
		platform.apply(&launchSpec, cfg)
	}
	if migotoDLLUsed {
		gameDir := ""
		if gameExe != "" {
			gameDir = filepath.Dir(gameExe)
		}
		err := x.recordDiagnosticsLaunch(ctx, key, launchSpec.WorkDir, gameDir, cfg.GameFolder, cfg.ImporterFolder)
		if err != nil {
			warn("Logging session launch was not recorded: " + err.Error())
		}
	}
	progress("inject-launch")
	var result inject.LaunchResult
	if cfg.GameLaunch == "Epic" {
		uri := platform.epicApp.LaunchURI(platformCommandLine(key, cfg, gameExe))
		result, err = x.launchWithURI(ctx, launchSpec, uri)
	} else {
		result, err = x.elevated.LaunchXXMI(ctx, launchSpec)
	}
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		warn(warning)
	}

	// The game is already running, so later failures are reported as warnings instead of a failed launch.
	progress("post-load")
	if gameExe != "" {
		hookDir = filepath.Dir(gameExe)
	}
	if err := runLaunchHook(ctx, cfg.RunPostLoad, hookDir); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			warn(fmt.Sprintf("Post-load command exited with code %d", exit.ExitCode()))
		} else {
			warn("Post-load command failed: " + err.Error())
		}
	}
	progress("finish")
	if client == nil {
		warn("Launch count was not updated: XXMI settings store is not configured")
	} else if err := client.XXMIImporters.IncrementLaunchCount(ctx, key); err != nil {
		warn("Launch count was not updated: " + err.Error())
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Started %s (PID: %d, verified: %t)", key, result.PID, result.InjectionVerified),
			"XXMI.StartGame")
	}
	return nil
}

func filterImporterAccessPaths(importerFolder string, paths []string) (allowed, excluded []string) {
	// Unresolved roots stay empty and cannot authorize a path.
	importer, _ := platform.FinalPath(importerFolder)
	mods, _ := platform.FinalPath(filepath.Join(importerFolder, "Mods"))
	for _, path := range paths {
		// Match restoreImporterAccess: the parent resolves even when the entry cannot be opened.
		// The elevated helper rejects leaf links and hard links before changing their permissions.
		folder, err := platform.FinalPath(filepath.Dir(path))
		if err != nil {
			excluded = append(excluded, path)
			continue
		}
		target := filepath.Join(folder, filepath.Base(path))
		if platform.SameOrChildPath(importer, target) || platform.SameOrChildPath(mods, target) {
			allowed = append(allowed, target)
		} else {
			excluded = append(excluded, path)
		}
	}
	return allowed, excluded
}

// elevationDenied gives a helper that could not be started the code the renderer explains, and
// leaves a file request the running helper refused as it is.
func elevationDenied(err error) error {
	var acquire *elevated.AcquireError
	if errors.As(err, &acquire) {
		return fmt.Errorf("XXMI_ELEVATION_DENIED: %w", err)
	}
	return err
}

func (x *XXMI) acquireImporter(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
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
	key = strings.ToUpper(strings.TrimSpace(key))
	x.mu.Lock()
	delete(x.busy, key)
	x.mu.Unlock()
}

// withoutReplacedReShadeLibraries returns cfg as a launch with the built-in ReShade runs it, along with a
// warning for each change. Whether the launch loads the XXMI DLL depends on the result.
func withoutReplacedReShadeLibraries(cfg ImporterConfig) (ImporterConfig, []string) {
	if !cfg.usesReShade() || !cfg.ExtraLibraries.Enabled {
		return cfg, nil
	}

	// Libraries left from a manual ReShade setup would load a second copy of it.
	var warnings []string
	kept := make([]string, 0, len(cfg.ExtraLibraries.Paths))
	wrapperSkipped := false
	for _, library := range cfg.ExtraLibraries.Paths {
		name := filepath.Base(library)
		wrapper := strings.EqualFold(name, "RabbitWrapper.dll")
		if !wrapper && !strings.EqualFold(name, "ReShade64.dll") {
			kept = append(kept, library)
			continue
		}
		wrapperSkipped = wrapperSkipped || wrapper
		warnings = append(warnings, "Skipped an extra library that the built-in ReShade replaces: "+library)
	}
	cfg.ExtraLibraries.Paths = kept

	// That setup bypasses XXMI DLL injection because the wrapper loads the DLL itself. Without the
	// wrapper nothing would, so this launch injects it.
	if wrapperSkipped && cfg.XXMIDLLInjectMode == "Bypass" {
		cfg.XXMIDLLInjectMode = "Inject"
		warnings = append(
			warnings, "XXMI DLL injection was switched from Bypass to Inject for this launch so mods still load",
		)
	}
	return cfg, warnings
}

func (x *XXMI) builtinLaunchSpec(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	gameExe, processName, parentExe string,
	migotoDLLUsed bool,
) (inject.LaunchSpec, error) {
	if _, ok := lookupImporterPackage(key); !ok {
		return inject.LaunchSpec{}, fmt.Errorf("unknown importer %q", key)
	}

	// Nothing is deployed for a launch that leaves the XXMI DLL out, so it has no manifest to read.
	var deployed runtimeManifest
	var err error
	if migotoDLLUsed {
		data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName))
		if err != nil {
			return inject.LaunchSpec{}, err
		}
		if err := json.Unmarshal(data, &deployed); err != nil {
			return inject.LaunchSpec{}, err
		}
	}
	// The helper needs a working directory even when it starts nothing itself.
	workDir := cfg.ImporterFolder
	if gameExe != "" && key != "ZZMI" {
		// Zenless Zone Zero crashes at the login screen when started from its own folder.
		workDir = filepath.Dir(gameExe)
	}
	if key == "SRMI" && cfg.GameLaunch == "Direct" && gameExe != "" && epicHoYoPlayInstall(gameExe) {
		// The Epic Games build of Honkai: Star Rail stops at the login screen when started from its own
		// folder.
		workDir = filepath.Dir(filepath.Dir(workDir))
	}
	spec := inject.LaunchSpec{
		Mode: inject.RuntimeMode(cfg.Mode), ProcessName: processName, StartExe: gameExe,
		WorkDir: workDir, StartMethod: cfg.ProcessStartMethod, Priority: cfg.ProcessPriority,
		InjectMode: cfg.XXMIDLLInjectMode, UseHook: cfg.XXMIDLLInjectMode == "Hook",
		TimeoutSeconds:  cfg.ProcessTimeout,
		InjectionMethod: cfg.InjectionMethod,
		ParentExe:       parentExe,
	}
	if migotoDLLUsed {
		spec.ModuleDLL = filepath.Join(cfg.ImporterFolder, "d3d11.dll")
	}
	if cfg.UseLaunchOptions {
		spec.StartArgs, err = splitLaunchOptions(cfg.LaunchOptions)
		if err != nil {
			return inject.LaunchSpec{}, fmt.Errorf("parse launch options: %w", err)
		}
	}
	if key == "WWMI" && cfg.UseLaunchOptions && gameExe != "" {
		spec.StartExe = filepath.Join(
			filepath.Dir(gameExe), "Client", "Binaries", "Win64", "Client-Win64-Shipping.exe",
		)
		spec.WorkDir = filepath.Dir(spec.StartExe)
	}
	if tier := wwmiResourceTierArgument(key, cfg); tier != "" {
		spec.StartArgs = append(spec.StartArgs, tier)
	}
	if renderer := rendererSwitch(key, cfg); renderer != "" {
		spec.StartArgs = append([]string{renderer}, spec.StartArgs...)
	}
	if usesFPSUnlocker(cfg) {
		folder, err := fpsUnlockerFolder()
		if err != nil {
			return inject.LaunchSpec{}, err
		}
		spec.StartExe = filepath.Join(folder, "unlockfps_nc.exe")
		spec.WorkDir = folder
		spec.StartArgs = nil
	}
	switch cfg.GameLaunch {
	case "Custom":
		spec.CustomLaunchCmd = cfg.CustomLaunch.Command
	case "Manual":
		// The helper starts nothing and waits for the user to launch the game.
		spec.StartMethod = "Manual"
	}
	if spec.InjectionMethod == "Native" {
		spec.UseHook = false
		if spec.InjectMode != "Bypass" {
			spec.InjectMode = "Inject"
		}
	}
	if cfg.ExtraLibraries.Enabled {
		spec.ExtraDLLs, err = x.resolveExtraDLLPaths(ctx, cfg.ExtraLibraries.Paths)
		if err != nil {
			return inject.LaunchSpec{}, err
		}
	}
	if spec.InjectionMethod == "Native" {
		return spec, nil
	}
	if cfg.Mode == RuntimeXXMI && migotoDLLUsed {
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
		return spec, nil
	}
	if cfg.Mode != RuntimeXXMI && spec.InjectMode != "Bypass" {
		spec.LegacyLoader, err = verifiedLaunchFile(filepath.Join(cfg.ImporterFolder, "3DMigoto Loader.exe"))
		if err != nil {
			return inject.LaunchSpec{}, err
		}
	}

	if err := x.useExtraDLLInjector(ctx, cfg, &spec); err != nil {
		return inject.LaunchSpec{}, err
	}
	return spec, nil
}

// epicHoYoPlayInstall reports whether the game was installed by the HoYoPlay launcher that the Epic Games
// Store ships, using the two marks the reference launcher looks for: launcher_epic.exe two folders above the
// game folder, or a config.ini beside the executable with a value naming Epic.
func epicHoYoPlayInstall(gameExe string) bool {
	folder := filepath.Dir(gameExe)
	if fileExists(filepath.Join(filepath.Dir(filepath.Dir(folder)), "launcher_epic.exe")) {
		return true
	}

	path := filepath.Join(folder, "config.ini")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(data)) {
		_, value, ok := strings.Cut(line, "=")
		if ok && !strings.Contains(value, "=") && strings.Contains(strings.ToLower(value), "epic") {
			return true
		}
	}
	return false
}

// useExtraDLLInjector names the XXMI injector for a launch that injects extra or preloaded DLLs
// without a deployed XXMI runtime to take it from, caching its libraries first when they are missing.
func (x *XXMI) useExtraDLLInjector(ctx context.Context, cfg ImporterConfig, spec *inject.LaunchSpec) error {
	if spec.InjectionMethod == "Native" || spec.LoaderDLL.Path != "" ||
		len(spec.ExtraDLLs)+len(spec.PreloadDLLs) == 0 {
		return nil
	}
	pin, _, err := x.libsPin(ctx, cfg)
	if err != nil {
		return err
	}
	version := selectedLegacyInjectorVersion(pin)
	if version == "" {
		// Nothing is cached until a launch deploys the XXMI runtime, which this one does not.
		version, err = x.resolveLibsVersion(ctx, cfg)
		if err != nil {
			return err
		}
	}
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	cacheFolder := filepath.Join(cacheRoot, "packages", "xxmi-libs", version)
	if err := x.EnsureLibsVersion(ctx, version); err != nil {
		if info, statErr := os.Stat(cacheFolder); statErr == nil && info.IsDir() {
			return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
		}
		return err
	}
	spec.LoaderDLL, err = verifiedLaunchFile(filepath.Join(cacheFolder, "3dmloader.dll"))
	return err
}

// errWWMIResourceTierUndecided stops the first direct WWMI launch until the user picks the resource
// quality. The renderer keys its dialog off this literal; keep it in sync with use-launch-guard.tsx.
var errWWMIResourceTierUndecided = errors.New("WWMI_RESOURCE_TIER_DECISION_REQUIRED")

// errD3D11ModeNoticeRequired stops the first Epic Games launch of a game that needs DirectX 11
// until the user has seen how to turn it on. The renderer keys its dialog off this literal; keep
// it in sync with use-launch-guard.tsx.
var errD3D11ModeNoticeRequired = errors.New("XXMI_D3D11_MODE_NOTICE_REQUIRED")

// d3d11ModeNoticeRequired reports whether the launch still owes the DirectX 11 reminder. The games'
// official launchers, which Epic Games opens first, pick the renderer themselves. The launcher of
// Zenless Zone Zero has no such choice, so ZZMI owes none.
func d3d11ModeNoticeRequired(key string, cfg ImporterConfig) bool {
	return cfg.GameLaunch == "Epic" && (key == "WWMI" || key == "EFMI") && !cfg.SkipD3D11Switch &&
		!cfg.D3D11ModeNoticeShown
}

// rendererSwitch returns the argument that picks the renderer of the importer's game: DirectX 11 unless
// the user turned that off, or "" when the game has no such argument or is left to pick for itself.
func rendererSwitch(key string, cfg ImporterConfig) string {
	switch {
	case key == "ZZMI" && cfg.SkipD3D11Switch:
		// Zenless Zone Zero picks DirectX 11 on its own, so leaving the argument out changes nothing.
		return "-use-d3d12"
	case cfg.SkipD3D11Switch:
		return ""
	case key == "WWMI":
		return "-dx11"
	case key == "EFMI" || key == "ZZMI":
		return "-force-d3d11"
	}
	return ""
}

// wwmiResourceTierArgument returns the resource quality argument Wuthering Waves needs to load, or ""
// when the launch does not pass one. Launch options that already name a quality are left alone.
func wwmiResourceTierArgument(key string, cfg ImporterConfig) string {
	if key != "WWMI" || cfg.WWMI == nil || cfg.GameLaunch != "Direct" {
		return ""
	}
	if cfg.UseLaunchOptions && strings.Contains(cfg.LaunchOptions, "krqlv") {
		return ""
	}
	return "-krqlv=" + strings.ToLower(cfg.WWMI.ResourceTier)
}

// usesFPSUnlocker reports whether the launch starts the game through the Genshin FPS unlocker,
// which can only wrap a game executable this app starts itself.
func usesFPSUnlocker(cfg ImporterConfig) bool {
	return cfg.GIMI != nil && cfg.GIMI.UnlockFPS && cfg.GameLaunch == "Direct"
}

// launchProcessName resolves the image name to inject into: the importer's process executable,
// the executable found in the game folder, and finally the importer's first game executable.
// Like the reference launcher, the override only applies while no game executable is located.
func launchProcessName(cfg ImporterConfig, spec importerPackageSpec, gameExe string) string {
	switch {
	case cfg.GameProcessExe != "" && gameExe == "":
		return cfg.GameProcessExe
	case len(spec.processNames) > 0:
		return spec.processNames[0]
	case gameExe != "":
		return filepath.Base(gameExe)
	case len(spec.gameExeNames) > 0:
		return spec.gameExeNames[0]
	}
	return ""
}

func (x *XXMI) resolveExtraDLLPaths(ctx context.Context, paths []string) ([]string, error) {
	resolved := append([]string(nil), paths...)
	var root *string
	for i, library := range resolved {
		if filepath.IsAbs(library) {
			continue
		}
		if root == nil {
			var err error
			root, err = x.GetXXMIPath(ctx)
			if err != nil {
				return nil, err
			}
		}
		resolved[i] = filepath.Join(*root, library)
	}
	return resolved, nil
}

func (x *XXMI) migotoDLLUsed(ctx context.Context, cfg ImporterConfig) (bool, error) {
	if cfg.XXMIDLLInjectMode != "Bypass" {
		return true, nil
	}
	if !cfg.ExtraLibraries.Enabled {
		return false, nil
	}
	paths, err := x.resolveExtraDLLPaths(ctx, cfg.ExtraLibraries.Paths)
	if err != nil {
		return false, err
	}
	moduleDLL := filepath.Join(cfg.ImporterFolder, "d3d11.dll")
	for _, path := range paths {
		if strings.EqualFold(filepath.Clean(path), moduleDLL) {
			return true, nil
		}
	}
	return false, nil
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
