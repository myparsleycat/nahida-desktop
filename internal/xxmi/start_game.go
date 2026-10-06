package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (x *XXMI) StartGame(ctx context.Context, importer string) error {
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
