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

func collectLaunchBlockers(ctx context.Context, importer, exe string, src launchChecker) ([]error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var blocked []error
	if strings.EqualFold(importer, gimiImporterKey) {
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
		return nil, fmt.Errorf("read nvidia smooth motion for %s: %w", exe, err)
	}
	if enabled {
		blocked = append(blocked, errSmoothMotionEnabled)
	}
	return blocked, nil
}

func applyLaunchFixes(ctx context.Context, importer, exe string, src launchFixer) error {
	blockers, err := collectLaunchBlockers(ctx, importer, exe, src)
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

func (x *XXMI) rejectLaunchBlockers(ctx context.Context, importer, exe string) error {
	blockers, err := collectLaunchBlockers(ctx, importer, exe, x)
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
	ready, err := x.prepareGameLaunch(ctx, importer)
	if err != nil {
		return err
	}
	if err := applyLaunchFixes(ctx, ready.importer, ready.exe, x); err != nil {
		if infra.IsReportedError(err) {
			return err
		}
		return x.reportLaunchGuard(err, "clear-launch-blockers", ready.importer, ready.exe)
	}
	return nil
}

func (x *XXMI) reportLaunchGuard(err error, operation, importer, exe string) error {
	return infra.ReportError(x.log, err, xxmiLaunchGuardWhere, infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: operation, Stage: "launch-guard",
		Fields: map[string]any{"importer": importer, "executable": exe},
	})
}
