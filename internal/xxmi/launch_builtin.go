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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi/inject"
)

func (x *XXMI) launchBuiltinGameLocked(ctx context.Context, key string, cfg ImporterConfig) (returnErr error) {
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
	processName := launchProcessName(cfg, packageSpec, gameExe)
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
	guards, err := x.launchGuards(ctx)
	if err != nil {
		return err
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
	if x.elevated == nil {
		return errors.New("XXMI_ELEVATION_DENIED: elevated helper is unavailable")
	}
	if migotoDLLUsed {
		progress("xcmd-prelaunch")
		skipped, err := executeXcmdDeletes(cfg.ImporterFolder, cfg.ImporterFolder, "PreLaunch")
		if err != nil {
			return err
		}
		for _, target := range skipped {
			warn("Skipped auto_update.xcmd delete through a symbolic link or junction: " + target)
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
			if runtimeProvider != "" && !cfg.Migoto.UnsafeMode {
				d3d11Folder, err = x.verifiedProviderDLLFolder(ctx, cacheRoot, runtimeProvider)
				if err != nil {
					return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
				}
			}
			if err := validateXXMIRuntimeFiles(
				cfg.ImporterFolder, cacheFolder, d3d11Folder, cfg.Migoto.UnsafeMode,
			); err != nil {
				return fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
			}
			if runtimeProvider != "" {
				// The provider's d3dx.ini only adds options, so the launch goes on without one that cannot
				// be read.
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
		if cfg.IniOptimizer.Enabled {
			progress("ini-optimizer")
			report, err := x.OptimizeMods(
				ctx,
				OptimizeModsInput{Importer: key, ResetCache: cfg.IniOptimizer.ResetCache},
			)
			if err != nil {
				return fmt.Errorf("optimize %s INI files: %w", key, err)
			}
			if x.eventEmit != nil {
				x.eventEmit("xxmi:launch-progress", map[string]any{
					"importer": key, "stage": stage, "optimized": len(report.Changes),
				})
			}
		}
	}

	// One helper lease covers the launch, so game files that need administrator rights and the
	// launch itself share a single UAC prompt.
	lease := elevated.NewFileLease(x.elevated)
	defer lease.Release()

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
	launchSpec, err := x.builtinLaunchSpec(ctx, key, cfg, gameExe, processName, migotoDLLUsed)
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

// updateLaunchINI writes the options this launch decides into the importer's d3dx.ini. providerINI is the
// d3dx.ini of the deployed libraries provider, or nil; its options are added where the file has none.
func (x *XXMI) updateLaunchINI(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	processName string,
	providerINI []byte,
) error {
	root, err := openInstallRoot(cfg.ImporterFolder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	iniRoot, iniName, err := root.resolveUserFile("d3dx.ini")
	if err != nil {
		return err
	}
	if iniRoot == nil {
		iniRoot = root
	} else {
		defer func() { _ = iniRoot.Close() }()
	}
	data, info, err := iniRoot.readFile(iniName)
	if err != nil {
		return err
	}
	doc := parseINI(data)
	doc.SetOption("Loader", "target", processName, true)
	doc.SetOption("Loader", "module", "d3d11.dll", true)
	// 3DMigoto warns about an empty `launch =` line, and the helper starts the game itself.
	doc.RemoveOption("Loader", "launch")
	if cfg.Mode == RuntimeXXMI {
		doc.SetOption("Loader", "loader", x.elevated.HelperImageName(), true)
	} else {
		doc.RemoveOption("Loader", "loader")
	}

	// The options the launch decides are written after these, so they win over a stale override.
	applyD3DXOverrides(doc, cfg.D3DXOverrides)
	systemOptions, logLevel := migotoINISupport(cfg)
	if systemOptions {
		doc.SetOption("System", "dll_initialization_delay", strconv.Itoa(cfg.XXMIDLLInitDelay), true)
		user32 := syscall.NewLazyDLL("user32.dll")
		metric := user32.NewProc("GetSystemMetrics")
		width, _, _ := metric.Call(0)
		height, _, _ := metric.Call(1)
		doc.SetOption("System", "screen_width", strconv.FormatUint(uint64(width), 10), true)
		doc.SetOption("System", "screen_height", strconv.FormatUint(uint64(height), 10), true)
	}

	// Where the file has no hotkey the launch writes one, so a saved override takes the place of the default.
	migoto := cfg.Migoto
	toggleInput, _ := lookupD3DXOption(d3dxOverrideKey("Input", "toggle_input"))
	if value, ok := cfg.D3DXOverrides[d3dxOverrideKey("Input", "toggle_input")]; ok && toggleInput.accepts(value) {
		migoto.ToggleInput = value
	}
	applyMigotoINI(doc, key, migoto, logLevel)

	// The options above are already in place, so the provider's defaults never replace a launch setting.
	if len(providerINI) > 0 {
		// A saved override of an option the provider adds is written with it. A package update that drops
		// the option would otherwise leave this launch on the provider's default.
		added := map[string]string{}
		for id, value := range cfg.D3DXOverrides {
			spec, ok := lookupD3DXOption(id)
			if !ok {
				continue
			}
			if _, present := doc.Option(spec.section, spec.key); !present {
				added[id] = value
			}
		}
		doc.AddMissingOptions(parseINI(providerINI))
		applyD3DXOverrides(doc, added)
	}
	if !doc.Changed() {
		return nil
	}
	return iniRoot.writeFileAtomic(ctx, iniName, bytes.NewReader(doc.Bytes()), 0o600, info)
}

// migotoLogLevelVersion is the first XXMI libraries release that reads log_level, the [Input] switches,
// and clear_unknown_settings. Earlier releases and the legacy 3DMigoto runtime read only calls and debug.
const migotoLogLevelVersion = "v1.1.7"

// migotoINISupport reports which options the deployed d3d11.dll reads: the [System] options a launch sets,
// which every XXMI libraries release has, and the ones migotoLogLevelVersion names.
func migotoINISupport(cfg ImporterConfig) (systemOptions, logLevel bool) {
	var manifest runtimeManifest
	if data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName)); err == nil {
		_ = json.Unmarshal(data, &manifest)
	}
	if cfg.Mode == RuntimeXXMI && manifest.UserManaged[customDLLName] == "" {
		version, ok := deployedLibsVersion(cfg.ImporterFolder)
		return true, ok && semver.IsValid("v"+version) && semver.Compare("v"+version, migotoLogLevelVersion) >= 0
	}

	// Neither the libraries version nor the version resource describes a custom DLL, one unsafe mode kept,
	// or a legacy runtime: XXMI builds before 1.2.0 carry the description and version of stock 3DMigoto.
	// The option names a DLL reads are in its image, as UTF-16 in all but a few builds.
	path := filepath.Join(cfg.ImporterFolder, customDLLName)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > customDLLSizeLimit {
		return false, false
	}
	image, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	names := func(option string) bool {
		wide := make([]byte, 0, 2*len(option))
		for _, char := range []byte(option) {
			wide = append(wide, char, 0)
		}
		return bytes.Contains(image, wide) || bytes.Contains(image, []byte(option))
	}
	return names("dll_initialization_delay"), cfg.Mode == RuntimeXXMI && names("log_level")
}

func applyMigotoINI(doc *iniDocument, key string, options MigotoOptions, logLevel bool) {
	if options.EnforceRendering {
		values := enforcedRenderingOptions(key)
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
	if logLevel {
		doc.SetOption("Logging", "log_level", strings.ToLower(options.LogLevel), true)
		boolean("System", "clear_unknown_settings", options.ClearUnknownSettings, "1", "0")
		boolean("Input", "input", options.Input, "1", "0")
		doc.SetOption("Input", "input_disable_mode", strings.ToLower(options.InputDisableMode), true)

		// The hotkey is a default: one the user or the importer package already set in d3dx.ini wins.
		if len(doc.optionIndexes("Input", "toggle_input")) == 0 && options.ToggleInput != "" {
			doc.SetOption("Input", "toggle_input", options.ToggleInput, true)
		}
	} else {
		boolean("Logging", "calls", options.LogLevel == "Info" || options.LogLevel == "Debug", "1", "0")
		boolean("Logging", "debug", options.LogLevel == "Debug", "1", "0")
	}
	boolean("Logging", "show_warnings", options.MuteWarnings, "0", "1")
	boolean("Hunting", "hunting", options.EnableHunting, "2", "0")
	boolean("Hunting", "marking_actions", options.DumpShaders, "clipboard hlsl asm regex", "clipboard")
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
	gameExe, processName string,
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
	switch key {
	case "WWMI":
		if cfg.UseLaunchOptions && gameExe != "" {
			spec.StartExe = filepath.Join(
				filepath.Dir(gameExe), "Client", "Binaries", "Win64", "Client-Win64-Shipping.exe",
			)
			spec.WorkDir = filepath.Dir(spec.StartExe)
		}
		spec.StartArgs = append([]string{"-dx11"}, spec.StartArgs...)
		if tier := wwmiResourceTierArgument(key, cfg); tier != "" {
			spec.StartArgs = append(spec.StartArgs, tier)
		}
	case "EFMI":
		spec.StartArgs = append([]string{"-force-d3d11"}, spec.StartArgs...)
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
// official launchers, which Epic Games opens first, pick the renderer themselves.
func d3d11ModeNoticeRequired(key string, cfg ImporterConfig) bool {
	return cfg.GameLaunch == "Epic" && (key == "WWMI" || key == "EFMI") && !cfg.D3D11ModeNoticeShown
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
