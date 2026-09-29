package xxmi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"nahida.live/desktop/internal/infra"
)

const xxmiLaunchGuardWhere = "XXMI.launchGuard"

var (
	// The renderer keys the launch-guard dialogs off these literals. Keep them in sync with
	// frontend/src/hooks/use-launch-guard.tsx.
	errGimiDCREnabled      = errors.New("GIMI_DCR_ENABLED")
	errSmoothMotionEnabled = errors.New("NVIDIA_SMOOTH_MOTION_ENABLED")

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
// neither configures game settings nor loads the XXMI DLL, so Genshin's DCR setting is irrelevant.
func (x *XXMI) rejectLaunchBlockers(ctx context.Context, importer, exe string, checkDCR bool) error {
	return x.rejectLaunchBlockersFrom(ctx, importer, exe, checkDCR, x)
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
	if err := applyLaunchFixes(ctx, importer, gameExecutable, x); err != nil {
		if infra.IsReportedError(err) {
			return err
		}
		return x.reportLaunchGuard(err, "clear-launch-blockers", importer, gameExecutable)
	}
	return nil
}

func (x *XXMI) reportLaunchGuard(err error, operation, importer, exe string) error {
	return infra.ReportError(x.log, err, xxmiLaunchGuardWhere, infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: operation, Stage: "launch-guard",
		Fields: map[string]any{"importer": importer, "executable": exe},
	})
}
