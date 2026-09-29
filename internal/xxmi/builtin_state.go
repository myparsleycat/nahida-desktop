package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

func (x *XXMI) builtinEnabledImporters(ctx context.Context) ([]EnabledImporter, error) {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil, errors.New("XXMI settings store is not configured")
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EnabledImporter, 0, len(rows))
	for _, row := range rows {
		var cfg ImporterConfig
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			continue
		}
		var installed *string
		if spec, ok := lookupImporterPackage(row.Key); ok {
			installed = readImporterVersion(cfg.ImporterFolder, spec)
		}
		pkg, err := client.XXMIPackages.Get(ctx, "importer:"+row.Key)
		if err != nil {
			return nil, err
		}
		info := PackageInfo{}
		if pkg != nil {
			if pkg.LatestVersion != nil {
				info.LatestVersion = *pkg.LatestVersion
			}
			if pkg.LatestReleaseNotes != nil {
				info.LatestReleaseNotes = *pkg.LatestReleaseNotes
			}
			if pkg.SkippedVersion != nil {
				info.SkippedVersion = *pkg.SkippedVersion
			}
			info.UpdateCheckTime = float64(pkg.UpdateCheckTime)
		}
		if installed != nil {
			info.DeployedVersion = *installed
		}
		out = append(out, EnabledImporter{Key: row.Key, Mode: cfg.Mode, ImporterFolder: cfg.ImporterFolder,
			GameFolder: cfg.GameFolder, InstalledVersion: installed, PackageInfo: info,
			UpdateAvailable: updateAvailable(info.LatestVersion, info.DeployedVersion, info.SkippedVersion)})
	}
	return out, nil
}

type Overview struct {
	Configured       bool                `json:"configured"`
	Root             string              `json:"root"`
	Importers        []EnabledImporter   `json:"importers"`
	LibsCache        []CachedLibs        `json:"libsCache"`
	LegacyRuntimes   []LegacyRuntimeInfo `json:"legacyRuntimes"`
	FPSVersions      []string            `json:"fpsVersions"`
	CacheIssues      []string            `json:"cacheIssues,omitempty"`
	ExternalLauncher *ExternalLauncher   `json:"externalLauncher,omitempty"`
}

func (x *XXMI) GetOverview(ctx context.Context) (Overview, error) {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return Overview{}, errors.New("XXMI settings store is not configured")
	}
	root, err := client.Settings.GetValue(ctx, "xxmi_root")
	if err != nil {
		return Overview{}, err
	}
	rootPath := ""
	if root != nil {
		rootPath = *root
	}
	if rootPath == "" {
		rootPath, err = xxmiCacheRoot()
		if err != nil {
			return Overview{}, err
		}
	}
	importers, err := x.builtinEnabledImporters(ctx)
	if err != nil {
		return Overview{}, err
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	for i := range importers {
		x.mu.RLock()
		importers[i].Running = x.busy[importers[i].Key]
		x.mu.RUnlock()
		if importers[i].Running {
			continue
		}
		spec, _ := lookupImporterPackage(importers[i].Key)
		for _, name := range append(append([]string{}, spec.gameExeNames...), spec.processNames...) {
			pid, err := findProcessPID(ctx, name)
			if err != nil {
				return Overview{}, err
			}
			if pid != 0 {
				importers[i].Running = true
				break
			}
		}
	}
	overview := Overview{Configured: len(rows) > 0, Root: filepath.Clean(rootPath), Importers: importers}
	if overview.LibsCache, err = x.ListCachedLibs(ctx); err != nil {
		overview.CacheIssues = append(overview.CacheIssues, "XXMI libraries: "+err.Error())
	}
	if overview.LegacyRuntimes, err = x.GetLegacyRuntimes(ctx); err != nil {
		overview.CacheIssues = append(overview.CacheIssues, "legacy 3DMigoto: "+err.Error())
	}
	if overview.FPSVersions, err = x.ListCachedFPSUnlocker(ctx); err != nil {
		overview.CacheIssues = append(overview.CacheIssues, "GI FPS Unlocker: "+err.Error())
	}
	if err := ctx.Err(); err != nil {
		return Overview{}, err
	}
	if !overview.Configured {
		external, err := x.DetectExternalLauncher(ctx)
		if err == nil {
			overview.ExternalLauncher = external
			if external != nil && (root == nil || *root == "") {
				overview.Root = external.Path
			}
		} else if !strings.Contains(err.Error(), "not found") {
			return Overview{}, err
		}
	}
	return overview, nil
}
