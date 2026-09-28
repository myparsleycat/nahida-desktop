package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

func (x *XXMI) StartGame(ctx context.Context, importer string) error {
	return x.StartGameWithCompatibility(ctx, importer, false)
}

func (x *XXMI) StartGameWithCompatibility(ctx context.Context, importer string, allowOldLibs bool) error {
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return err
	}
	return x.startBuiltinGame(ctx, importer, cfg, allowOldLibs)
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
