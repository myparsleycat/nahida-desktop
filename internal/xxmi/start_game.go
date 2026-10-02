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

func (x *XXMI) StartGame(ctx context.Context, importer string) error {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	if !x.acquireImporter(importer) {
		return errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(importer)
	x.mu.RLock()
	prepare := x.namespaceLaunchPreparation
	x.mu.RUnlock()
	if prepare != nil {
		if err := prepare(ctx, importer); err != nil {
			return infra.ReportError(
				x.log,
				fmt.Errorf("prepare mod namespaces before launch: %w", err),
				"XXMI:StartGame",
				infra.Diagnostic{
					Operation: "start-game",
					Stage:     "namespace-preparation",
					Fields:    map[string]any{"importerKey": importer},
				},
			)
		}
	}

	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return err
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
