package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
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
			UpdateAvailable: updateAvailable(info.LatestVersion, info.DeployedVersion, info.SkippedVersion),
			CustomDLL:       cfg.Mode == RuntimeXXMI && usesCustomDLL(cfg.ImporterFolder)})
	}
	return out, nil
}

type Overview struct {
	LauncherMode LauncherMode `json:"launcherMode"`
	Configured   bool         `json:"configured"`
	Root         string       `json:"root"`
	// SharedLibsVersion is empty while importers following the shared version use the latest release.
	SharedLibsVersion string              `json:"sharedLibsVersion"`
	Importers         []EnabledImporter   `json:"importers"`
	LibsCache         []CachedLibs        `json:"libsCache"`
	LegacyRuntimes    []LegacyRuntimeInfo `json:"legacyRuntimes"`
	FPSVersions       []string            `json:"fpsVersions"`
	CacheIssues       []string            `json:"cacheIssues,omitempty"`
	ExternalLauncher  *ExternalLauncher   `json:"externalLauncher,omitempty"`
}

func (x *XXMI) GetOverview(ctx context.Context) (Overview, error) {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return Overview{}, errors.New("XXMI settings store is not configured")
	}
	mode, err := launcherMode(ctx, client)
	if err != nil {
		return Overview{}, err
	}
	if mode == LauncherExternal {
		return x.externalOverview(ctx)
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
	sharedLibs, err := client.Settings.GetValue(ctx, sharedLibsVersionKey)
	if err != nil {
		return Overview{}, err
	}
	importers, err := x.builtinEnabledImporters(ctx)
	if err != nil {
		return Overview{}, err
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	keys := make([]string, 0, len(importers))
	for _, importer := range importers {
		keys = append(keys, importer.Key)
	}
	running, err := x.runningImporters(ctx, keys)
	if err != nil {
		return Overview{}, err
	}
	for i := range importers {
		importers[i].Running = running[importers[i].Key]
	}
	overview := Overview{
		LauncherMode: LauncherBuiltin,
		Configured:   len(rows) > 0,
		Root:         filepath.Clean(rootPath),
		Importers:    importers,
	}
	if sharedLibs != nil {
		overview.SharedLibsVersion = normalizeVersion(*sharedLibs)
	}
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
		} else if !strings.Contains(err.Error(), "not found") {
			return Overview{}, err
		}
	}
	return overview, nil
}

// builtinSettingKeys are the setting rows owned by the built-in runtime. The launcher mode is not one of them.
var builtinSettingKeys = []string{
	"xxmi_root", "xxmi_auto_update", "xxmi_include_prereleases", sharedLibsVersionKey,
}

// ResetBuiltinRuntime returns the built-in runtime to its unconfigured state by forgetting importer configs,
// package update state, and the root and update preferences. Files stay on disk: importer folders hold user
// mods and may still belong to an external XXMI Launcher, and the shared package caches remain reusable.
func (x *XXMI) ResetBuiltinRuntime(ctx context.Context) (returnErr error) {
	stage := "acquire"
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.resetBuiltinRuntime", infra.Diagnostic{
			Operation: "reset-builtin-runtime", Stage: stage,
			Fields: map[string]any{"setting_keys": builtinSettingKeys},
		})
	}()
	client, err := x.settingsClient()
	if err != nil {
		return err
	}

	// Holding every importer blocks launches and package installs until the reset commits.
	x.packageMu.Lock()
	defer x.packageMu.Unlock()
	acquired := make([]string, 0, len(importerPackages))
	defer func() {
		for _, key := range acquired {
			x.releaseImporter(key)
		}
	}()
	for key := range importerPackages {
		if !x.acquireImporter(key) {
			return errors.New("XXMI_BUSY")
		}
		acquired = append(acquired, key)
	}

	stage = "delete-state"
	if err := client.XXMIImporters.Reset(ctx, builtinSettingKeys); err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info("Reset built-in XXMI runtime state", "XXMI.resetBuiltinRuntime")
	}
	return nil
}

func (x *XXMI) externalOverview(ctx context.Context) (Overview, error) {
	overview := Overview{LauncherMode: LauncherExternal, Importers: []EnabledImporter{}}
	launcher, err := x.loadExternalLauncher(ctx)
	if err != nil || launcher == nil {
		return overview, err
	}
	overview.Configured = true
	overview.Root = launcher.path
	overview.Importers, err = x.externalEnabledImporters(ctx)
	return overview, err
}
