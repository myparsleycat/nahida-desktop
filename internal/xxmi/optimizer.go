package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/xxmi/optimizer"
)

type OptimizeModsInput struct {
	Importer   string `json:"importer"`
	DryRun     bool   `json:"dryRun"`
	ResetCache bool   `json:"resetCache"`
}

func (x *XXMI) OptimizeMods(ctx context.Context, input OptimizeModsInput) (optimizer.Report, error) {
	cfg, err := x.GetImporterConfig(ctx, input.Importer)
	if err != nil {
		return optimizer.Report{}, err
	}
	if err := validateLocalFolder("importer folder", cfg.ImporterFolder, true); err != nil {
		return optimizer.Report{}, err
	}
	data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, "d3dx.ini"))
	if errors.Is(err, os.ErrNotExist) {
		return optimizer.Report{}, errors.New("XXMI_IMPORTER_NOT_INSTALLED")
	}
	if err != nil {
		return optimizer.Report{}, err
	}
	patterns := []string{"DISABLED*"}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(strings.Trim(line, "[]")))
			continue
		}
		if section != "include" || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "exclude_recursive") {
			patterns = strings.Fields(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
			break
		}
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return optimizer.Report{}, err
	}
	prefix := "DISABLED "
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client != nil {
		style, err := client.Settings.GetValue(ctx, "mod_disabled_prefix_style")
		if err != nil {
			return optimizer.Report{}, err
		}
		if style != nil && *style == "underscore" {
			prefix = "DISABLED_"
		}
	}
	return optimizer.Optimize(ctx, optimizer.Options{
		Importer: input.Importer, ImporterFolder: cfg.ImporterFolder,
		CachePath: filepath.Join(root, "cache", "ini-optimizer", input.Importer+".json"),
		Exclude:   patterns, Prefix: prefix, DryRun: input.DryRun,
		ResetCache: input.ResetCache || cfg.IniOptimizer.ResetCache,
	})
}
