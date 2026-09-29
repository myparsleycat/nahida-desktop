package xxmi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/github"
)

const updateCheckInterval = time.Hour

type UpdateStatus struct {
	Importer       string `json:"importer"`
	Package        string `json:"package"`
	LatestVersion  string `json:"latestVersion"`
	Installed      string `json:"installed"`
	SkippedVersion string `json:"skippedVersion"`
	Pinned         bool   `json:"pinned"`
	Available      bool   `json:"available"`
}

func (x *XXMI) CheckUpdates(ctx context.Context, force bool) ([]UpdateStatus, error) {
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
	packages := map[string]struct{}{}
	for _, row := range rows {
		cfg, err := x.GetImporterConfig(ctx, row.Key)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			continue
		}
		packages["importer:"+row.Key] = struct{}{}
		if cfg.Mode == RuntimeXXMI || cfg.XXMIVersion.Pinned != "" || legacyUsesXXMIInjector(cfg) {
			packages["xxmi-libs"] = struct{}{}
		}
		if row.Key == "GIMI" && cfg.GIMI != nil && cfg.GIMI.UnlockFPS {
			packages["gi-fps-unlocker"] = struct{}{}
		}
	}
	for pkg := range packages {
		state, err := client.XXMIPackages.Get(ctx, pkg)
		if err != nil {
			return nil, err
		}
		if !force && state != nil && time.Since(time.Unix(state.UpdateCheckTime, 0)) < updateCheckInterval {
			continue
		}
		releases, err := x.listReleases(ctx, pkg, true)
		if err != nil {
			if errors.Is(err, github.ErrRateLimited) {
				break
			}
			return nil, fmt.Errorf("check %s updates: %w", pkg, err)
		}
		if state == nil {
			state = &db.XXMIPackageRow{Package: pkg}
		}
		if len(releases) > 0 {
			state.LatestVersion = &releases[0].Version
			state.LatestReleaseNotes = &releases[0].Notes
		}
		state.UpdateCheckTime = time.Now().Unix()
		if err := client.XXMIPackages.Upsert(ctx, *state); err != nil {
			return nil, err
		}
	}
	states := make(map[string]*db.XXMIPackageRow, len(packages))
	for pkg := range packages {
		state, err := client.XXMIPackages.Get(ctx, pkg)
		if err != nil {
			return nil, err
		}
		states[pkg] = state
	}
	statuses := make([]UpdateStatus, 0, len(rows)*2)
	for _, row := range rows {
		cfg, err := x.GetImporterConfig(ctx, row.Key)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			continue
		}
		pkg := "importer:" + row.Key
		status := updateStatus(row.Key, pkg, states[pkg])
		if spec, ok := lookupImporterPackage(row.Key); ok {
			if installed := readImporterVersion(cfg.ImporterFolder, spec); installed != nil {
				status.Installed = *installed
			}
		}
		status.Pinned = cfg.PackageVersion.Pinned != ""
		status.Available = updateAvailable(status.LatestVersion, status.Installed, status.SkippedVersion)
		statuses = append(statuses, status)

		if cfg.Mode == RuntimeXXMI || cfg.XXMIVersion.Pinned != "" || legacyUsesXXMIInjector(cfg) {
			libs := updateStatus(row.Key, "xxmi-libs", states["xxmi-libs"])
			libs.Pinned = cfg.XXMIVersion.Pinned != ""
			if legacyUsesXXMIInjector(cfg) {
				version := selectedLegacyInjectorVersion(cfg)
				if cacheRoot, err := xxmiCacheRoot(); err == nil && version != "" {
					if verifyXXMILibsCache(filepath.Join(cacheRoot, "packages", "xxmi-libs", version), version) == nil {
						libs.Installed = version
					}
				}
			}
			if cfg.Mode == RuntimeXXMI {
				if data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName)); err == nil {
					var deployed runtimeManifest
					if json.Unmarshal(data, &deployed) == nil {
						if version, ok := strings.CutPrefix(deployed.Source, "xxmi-libs@"); ok {
							libs.Installed = version
						}
					}
				}
			}
			libs.Available = updateAvailable(libs.LatestVersion, libs.Installed, libs.SkippedVersion)
			statuses = append(statuses, libs)
		}
		if row.Key == "GIMI" && cfg.GIMI != nil && cfg.GIMI.UnlockFPS {
			fps := updateStatus(row.Key, "gi-fps-unlocker", states["gi-fps-unlocker"])
			fps.Installed = newestCachedPackageVersion("gi-fps-unlocker")
			fps.Available = updateAvailable(fps.LatestVersion, fps.Installed, fps.SkippedVersion)
			statuses = append(statuses, fps)
		}
	}
	slices.SortFunc(statuses, func(a, b UpdateStatus) int {
		if order := cmp.Compare(a.Importer, b.Importer); order != 0 {
			return order
		}
		return cmp.Compare(a.Package, b.Package)
	})
	if x.eventEmit != nil {
		x.eventEmit("xxmi:updates", statuses)
	}
	return statuses, nil
}

func updateStatus(importer, pkg string, state *db.XXMIPackageRow) UpdateStatus {
	status := UpdateStatus{Importer: importer, Package: pkg}
	if state != nil {
		if state.LatestVersion != nil {
			status.LatestVersion = *state.LatestVersion
		}
		if state.SkippedVersion != nil {
			status.SkippedVersion = *state.SkippedVersion
		}
	}
	return status
}

func newestCachedPackageVersion(pkg string) string {
	root, err := xxmiCacheRoot()
	if err != nil {
		return ""
	}
	version, err := newestLegacyRuntime(filepath.Join(root, "packages", pkg))
	if err != nil {
		return ""
	}
	return version
}

func selectedLegacyInjectorVersion(cfg ImporterConfig) string {
	if version := normalizeVersion(cfg.XXMIVersion.Pinned); version != "" {
		return version
	}
	return newestCachedPackageVersion("xxmi-libs")
}

func updateAvailable(latest, installed, skipped string) bool {
	return latest != "" && latest != installed && latest != skipped
}

func legacyUsesXXMIInjector(cfg ImporterConfig) bool {
	return cfg.Mode == RuntimeLegacy && cfg.ExtraLibraries.Enabled && len(cfg.ExtraLibraries.Paths) > 0
}

func (x *XXMI) SkipVersion(ctx context.Context, pkg, version string) error {
	if pkg != "xxmi-libs" && pkg != "gi-fps-unlocker" {
		key, ok := strings.CutPrefix(pkg, "importer:")
		if !ok {
			return errors.New("unknown XXMI package")
		}
		if _, ok := lookupImporterPackage(key); !ok {
			return errors.New("unknown XXMI package")
		}
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	state, err := client.XXMIPackages.Get(ctx, pkg)
	if err != nil {
		return err
	}
	if state == nil || state.LatestVersion == nil || *state.LatestVersion != version {
		return errors.New("version is not the current latest release")
	}
	state.SkippedVersion = &version
	return client.XXMIPackages.Upsert(ctx, *state)
}

func (x *XXMI) InstallUpdates(ctx context.Context, targets []string) ([]string, error) {
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]bool, len(targets))
	for _, target := range targets {
		requested[target] = true
	}
	installed := []string{}
	for _, status := range statuses {
		if err := ctx.Err(); err != nil {
			return installed, err
		}
		if !requested[status.Package] || !status.Available || status.Pinned {
			continue
		}
		switch {
		case strings.HasPrefix(status.Package, "importer:"):
			err = x.InstallImporterPackage(ctx, InstallImporterPackageInput{
				Importer: status.Importer, Version: status.LatestVersion,
			})
		case status.Package == "xxmi-libs":
			err = x.EnsureLibsVersion(ctx, status.LatestVersion)
		case status.Package == "gi-fps-unlocker":
			err = x.EnsureFPSUnlockerVersion(ctx, status.LatestVersion)
		default:
			err = fmt.Errorf("unknown XXMI package %q", status.Package)
		}
		if err != nil {
			return installed, fmt.Errorf("install %s update: %w", status.Package, err)
		}
		installed = append(installed, status.Package)
		delete(requested, status.Package)
	}
	for pkg := range requested {
		if pkg != "" {
			return installed, fmt.Errorf("update target %s is not available", pkg)
		}
	}
	return installed, nil
}

func (x *XXMI) autoUpdateForLaunch(ctx context.Context, importer string) error {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	enabled, err := client.Settings.GetValue(ctx, "xxmi_auto_update")
	if err != nil {
		return err
	}
	if enabled != nil && *enabled != "true" {
		return nil
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil {
		return err
	}
	targets := []string{}
	for _, status := range statuses {
		if status.Importer == importer && status.Available && !status.Pinned {
			targets = append(targets, status.Package)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	_, err = x.InstallUpdates(ctx, targets)
	return err
}
