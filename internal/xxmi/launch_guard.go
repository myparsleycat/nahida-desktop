package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
)

const xxmiLaunchGuardWhere = "XXMI.launchGuard"

var (
	// The renderer keys the launch-guard dialogs off these literals. Keep them in sync with
	// frontend/src/hooks/use-launch-guard.tsx.
	errGimiDCREnabled      = errors.New("GIMI_DCR_ENABLED")
	errSmoothMotionEnabled = errors.New("NVIDIA_SMOOTH_MOTION_ENABLED")
	// errLoggingEnabled asks before a launch with 3DMigoto logging on, which can slow the game down
	// severely. Unlike the blockers above, the user may launch anyway through StartGameWithLogging.
	errLoggingEnabled = errors.New("XXMI_LOGGING_ENABLED")

	// errSmoothMotionUnreadable marks a failed NVIDIA settings read. The reference launcher has no such check,
	// so a launch only warns about it instead of failing.
	errSmoothMotionUnreadable = errors.New("NVIDIA smooth motion setting is unreadable")
)

// launchChecker reads the settings that can block a game launch.
type launchChecker interface {
	gimiDCREnabled(context.Context) (bool, error)
	smoothMotionEnabled(context.Context, string) (bool, error)
}

// launchFixer turns off the settings reported by launchChecker.
type launchFixer interface {
	launchChecker
	disableGIMIDCR(context.Context) error
	disableSmoothMotion(context.Context, string) error
}

func collectLaunchBlockers(
	ctx context.Context,
	importer, exe string,
	checkDCR bool,
	src launchChecker,
) ([]error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var blocked []error
	if checkDCR && strings.EqualFold(importer, gimiImporterKey) {
		enabled, err := src.gimiDCREnabled(ctx)
		if err != nil {
			return nil, fmt.Errorf("read genshin dynamic character resolution: %w", err)
		}
		if enabled {
			blocked = append(blocked, errGimiDCREnabled)
		}
	}
	if exe == "" {
		return blocked, nil
	}
	enabled, err := src.smoothMotionEnabled(ctx, exe)
	if err != nil {
		return blocked, fmt.Errorf("read nvidia smooth motion for %s: %w: %w", exe, errSmoothMotionUnreadable, err)
	}
	if enabled {
		blocked = append(blocked, errSmoothMotionEnabled)
	}
	return blocked, nil
}

func applyLaunchFixes(ctx context.Context, importer, exe string, src launchFixer) error {
	blockers, err := collectLaunchBlockers(ctx, importer, exe, true, src)
	if err != nil {
		return err
	}
	for _, blocker := range blockers {
		switch {
		case errors.Is(blocker, errGimiDCREnabled):
			if err := src.disableGIMIDCR(ctx); err != nil {
				return err
			}
		case errors.Is(blocker, errSmoothMotionEnabled):
			if err := src.disableSmoothMotion(ctx, exe); err != nil {
				return err
			}
		}
	}
	return nil
}

func (x *XXMI) gimiDCREnabled(ctx context.Context) (bool, error) {
	return readGenshinDCR(ctx)
}

func (x *XXMI) disableGIMIDCR(ctx context.Context) error {
	return x.DisableGenshinDynamicCharacterResolution(ctx)
}

// rejectLaunchBlockers fails the launch while a blocker is active. checkDCR is false when the launch
// does not load the XXMI DLL, so Genshin's DCR setting is irrelevant.
func (x *XXMI) rejectLaunchBlockers(ctx context.Context, importer, exe string, checkDCR bool) error {
	return x.rejectLaunchBlockersFrom(ctx, importer, exe, checkDCR, x.launchSettings)
}

func (x *XXMI) rejectLaunchBlockersFrom(
	ctx context.Context,
	importer, exe string,
	checkDCR bool,
	src launchChecker,
) error {
	blockers, err := collectLaunchBlockers(ctx, importer, exe, checkDCR, src)
	if errors.Is(err, errSmoothMotionUnreadable) {
		if x.log != nil {
			x.log.Warn(map[string]any{"importer": importer, "executable": exe, "error": err.Error()},
				xxmiLaunchGuardWhere)
		}
		err = nil
	}
	if err != nil {
		return x.reportLaunchGuard(err, "start-game", importer, exe)
	}
	if len(blockers) == 0 {
		return nil
	}
	joined := errors.Join(blockers...)
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Rejected StartGame for importer %s: %s", importer, joined), xxmiLaunchGuardWhere)
	}
	return infra.AnnotateError(joined, infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "start-game", Stage: "launch-guard",
		Fields: map[string]any{"importer": importer, "executable": exe},
	})
}

// ClearLaunchBlockers turns off every launch blocker that is still active for importer.
// A game profile inherits global NVIDIA Smooth Motion unless that profile has its own value,
// and clearing writes the off value on the game profile only.
func (x *XXMI) ClearLaunchBlockers(ctx context.Context, importer string) error {
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return err
	}
	if external {
		gameExecutable, err := x.externalGameExecutable(ctx, importer)
		if err != nil {
			return err
		}
		return x.clearLaunchBlockers(ctx, importer, gameExecutable)
	}
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return err
	}
	spec, ok := lookupImporterPackage(importer)
	if !ok {
		return errors.New("unknown XXMI importer")
	}
	gameExecutable := configuredGameExecutable(cfg.GameFolder, spec.gameExeNames)
	if importer == "WWMI" && cfg.GameFolder != "" {
		game, err := x.ValidateGameFolder(ctx, importer, cfg.GameFolder)
		if err != nil {
			return x.reportLaunchGuard(err, "clear-launch-blockers", importer, cfg.GameFolder)
		}
		gameExecutable = game.ExePath
	}
	return x.clearLaunchBlockers(ctx, importer, gameExecutable)
}

func (x *XXMI) clearLaunchBlockers(ctx context.Context, importer, gameExecutable string) error {
	if err := applyLaunchFixes(ctx, importer, gameExecutable, x.launchSettings); err != nil {
		if infra.IsReportedError(err) {
			return err
		}
		return x.reportLaunchGuard(err, "clear-launch-blockers", importer, gameExecutable)
	}
	return nil
}

// rejectLogging fails the launch while 3DMigoto logging is on for importer.
func (x *XXMI) rejectLogging(ctx context.Context, importer string, external bool) error {
	enabled, err := x.loggingEnabled(ctx, importer, external)
	if err != nil || !enabled {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Rejected StartGame for importer %s: %s", importer, errLoggingEnabled),
			xxmiLaunchGuardWhere)
	}
	return infra.AnnotateError(errLoggingEnabled, infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "start-game", Stage: "launch-guard",
		Fields: map[string]any{"importer": importer, "external": external},
	})
}

// loggingEnabled reports whether launching importer loads the XXMI DLL with a 3DMigoto log level
// that slows the game down. Warnings alone do not, and releases before migotoLogLevelVersion write
// nothing at that level. An importer that cannot launch reports false, so the launch itself explains why.
func (x *XXMI) loggingEnabled(ctx context.Context, importer string, external bool) (bool, error) {
	if external {
		launcher, err := x.loadExternalLauncher(ctx)
		if err != nil || launcher == nil {
			return false, err
		}
		_, disabled := launcher.disabled[importer]
		return !disabled && externalLoggingEnabled(launcher.migoto(importer)), nil
	}
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return false, err
	}
	if !cfg.Enabled || cfg.Migoto.LogLevel != "Info" && cfg.Migoto.LogLevel != "Debug" {
		return false, nil
	}
	used, err := x.migotoDLLUsed(ctx, cfg)
	if err != nil || !used {
		return false, err
	}
	_, statErr := os.Stat(filepath.Join(cfg.ImporterFolder, "d3dx.ini"))
	return statErr == nil && validateInstalledImporterPackage(importer, cfg) == nil, nil
}

// migoto returns the importer's Migoto section of the launcher config, or nil when it has none.
func (l externalLauncher) migoto(importer string) map[string]any {
	importers, _ := l.config["Importers"].(map[string]any)
	section, _ := importers[importer].(map[string]any)
	migoto, _ := section["Migoto"].(map[string]any)
	return migoto
}

// externalLoggingEnabled reads log_level, or the two switches XXMI Launcher used before 2.3.
func externalLoggingEnabled(migoto map[string]any) bool {
	if level, ok := migoto["log_level"].(string); ok {
		return strings.EqualFold(level, "INFO") || strings.EqualFold(level, "DEBUG")
	}
	calls, _ := migoto["calls_logging"].(bool)
	debug, _ := migoto["debug_logging"].(bool)
	return calls || debug
}

// DisableLogging turns 3DMigoto logging off for importer in the launcher that starts its game.
func (x *XXMI) DisableLogging(ctx context.Context, importer string) error {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return err
	}
	if !external {
		cfg, err := x.GetImporterConfig(ctx, importer)
		if err != nil {
			return err
		}
		cfg.Migoto.LogLevel = "Disabled"
		return x.SaveImporterConfig(ctx, importer, cfg)
	}

	configPath := ""
	err = x.updateExternalConfig(ctx, func(launcher *externalLauncher) (bool, error) {
		configPath = launcher.configPath()
		migoto := launcher.migoto(importer)
		if migoto == nil {
			return false, fmt.Errorf("importer %s not found", importer)
		}
		if _, modern := migoto["log_level"]; modern {
			migoto["log_level"] = "DISABLED"
		} else {
			migoto["calls_logging"], migoto["debug_logging"] = false, false
		}
		return true, nil
	})
	if err != nil {
		return infra.ReportError(x.log, err, xxmiLaunchGuardWhere, infra.Diagnostic{
			Operation: "disable-logging", Stage: "update-config",
			Fields: map[string]any{"importer": importer, "configPath": configPath},
		})
	}
	return nil
}

func (x *XXMI) reportLaunchGuard(err error, operation, importer, exe string) error {
	return infra.ReportError(x.log, err, xxmiLaunchGuardWhere, infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: operation, Stage: "launch-guard",
		Fields: map[string]any{"importer": importer, "executable": exe},
	})
}
