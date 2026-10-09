package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/reshade"
)

func (x *XXMI) StartGame(ctx context.Context, importer string) error {
	return x.startGame(ctx, importer, false)
}

// StartGameWithLogging starts the game without asking about 3DMigoto logging: the user chose to
// keep it on, or the caller has no dialog to ask with.
func (x *XXMI) StartGameWithLogging(ctx context.Context, importer string) error {
	return x.startGame(ctx, importer, true)
}

func (x *XXMI) startGame(ctx context.Context, importer string, keepLogging bool) error {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	if !x.acquireImporter(importer) {
		return errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(importer)
	x.setLaunching(importer, true)
	defer x.setLaunching(importer, false)

	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return err
	}
	if !keepLogging {
		if err := x.rejectLogging(ctx, importer, external); err != nil {
			return err
		}
	}
	if external {
		return x.startExternalGame(ctx, importer)
	}
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return err
	}
	return x.launchBuiltinGameLocked(ctx, importer, cfg)
}

// The legacy loader injects on its own schedule, so ReShade cannot be ordered ahead of it.
func (cfg ImporterConfig) usesReShade() bool {
	return cfg.ReShade.Enabled && (cfg.Mode != RuntimeLegacy || cfg.InjectionMethod == "Native")
}

// LaunchPresetEffects returns the effect packages the importer's ReShade preset needs and lacks. It
// is empty for a launch that does not inject ReShade.
func (x *XXMI) LaunchPresetEffects(ctx context.Context, importer string) (reshade.PresetEffects, error) {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	none := reshade.PresetEffects{Packages: []reshade.EffectPackage{}, Unknown: []string{}}
	if x.reshade == nil {
		return none, nil
	}
	external, err := x.usesExternalLauncher(ctx)
	if err != nil || external {
		return none, err
	}
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil || !cfg.usesReShade() {
		return none, err
	}

	effects, err := x.reshade.LaunchPresetEffects(ctx, importer)
	if err != nil {
		return none, infra.ReportError(x.log, err, "XXMI.LaunchPresetEffects", infra.Diagnostic{
			Operation: "launch-preset-effects", Fields: map[string]any{"importer": importer},
		})
	}
	return effects, nil
}

func configuredGameExecutable(folder string, configured []string) string {
	folder = strings.TrimSpace(folder)
	var first string
	for _, name := range configured {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		candidate := filepath.Clean(name)
		if !filepath.IsAbs(candidate) {
			if !filepath.IsAbs(folder) {
				continue
			}
			candidate = filepath.Join(folder, candidate)
		}
		if first == "" {
			first = candidate
		}
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return first
}
