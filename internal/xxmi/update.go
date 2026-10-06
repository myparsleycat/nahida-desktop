package xxmi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
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
	// Shared marks the XXMI libraries of an importer that follows the shared version, whose update belongs to
	// the runtime as a whole rather than to that importer.
	Shared bool `json:"shared"`
}

func (x *XXMI) CheckUpdates(ctx context.Context, force bool) ([]UpdateStatus, error) {
	ctx = infra.WithGitHubOperation(ctx, "xxmi-check-updates")
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil, errors.New("XXMI settings store is not configured")
	}
	// The external launcher tracks and installs its own package updates.
	if mode, err := launcherMode(ctx, client); err != nil || mode == LauncherExternal {
		return []UpdateStatus{}, err
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return nil, err
	}
	packages := map[string]struct{}{}
	providers := make(map[string]libsProviderSpec, len(rows))
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
		provider, err := x.deployedLibsProvider(ctx, cfg)
		if err != nil {
			return nil, err
		}
		providers[row.Key] = provider
		if provider.overlayPackage != "" {
			packages[provider.overlayPackage] = struct{}{}
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
		var responseInfo infra.GitHubResponseInfo
		checkCtx := infra.WithGitHubResponseInfo(ctx, &responseInfo)
		releases, err := x.listReleases(checkCtx, pkg, force)
		if err != nil {
			err = infra.ReportError(x.log, err, "XXMI.CheckUpdates", infra.Diagnostic{
				Operation: "check-updates", Stage: "release-metadata",
				Fields: map[string]any{"package": pkg, "force": force},
			})
			if !force && errors.Is(err, github.ErrRateLimited) {
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
		if !responseInfo.FetchedAt.IsZero() {
			state.UpdateCheckTime = responseInfo.FetchedAt.Unix()
		}
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
			pin, notify, err := x.libsPin(ctx, cfg)
			if err != nil {
				return nil, err
			}
			libs := updateStatus(row.Key, "xxmi-libs", states["xxmi-libs"])
			libs.Pinned = pin != "" && !notify
			libs.Shared = cfg.XXMIVersion.Follow == followShared
			if cfg.Mode == RuntimeXXMI {
				libs.Installed, _ = deployedLibsVersion(cfg.ImporterFolder)
				if pin == "" {
					libs.Installed = cachedLibsVersion(
						libs.LatestVersion, libs.SkippedVersion, libs.Installed, verifiedCachedLibsVersion,
					)
				}
			}

			// A runtime not deployed since migration, or an adopted custom DLL whose manifest has no source,
			// reports no version; the next launch deploys the selected cached libraries without downloading.
			if libs.Installed == "" && (cfg.Mode == RuntimeXXMI || legacyUsesXXMIInjector(cfg)) {
				if version := selectedLegacyInjectorVersion(pin); version != "" && verifiedCachedLibsVersion(version) {
					libs.Installed = version
				}
			}

			// A pin that still announces releases is compared by the pinned version itself, so moving the pin
			// clears the notice before the next launch deploys it.
			if notify {
				libs.Installed = pin
			}
			libs.Available = updateAvailable(libs.LatestVersion, libs.Installed, libs.SkippedVersion)
			statuses = append(statuses, libs)
		}

		// A provider d3d11.dll is tracked beside the signed libraries it is deployed over.
		if provider := providers[row.Key]; provider.overlayPackage != "" {
			dll := updateStatus(row.Key, provider.overlayPackage, states[provider.overlayPackage])
			dll.Shared = cfg.LibsProvider == ""
			dll.Installed = cachedLibsVersion(
				dll.LatestVersion, dll.SkippedVersion, providerDLLBaseVersion(cfg.ImporterFolder, provider),
				func(version string) bool { return x.verifiedProviderDLL(ctx, provider, version) },
			)
			dll.Available = updateAvailable(dll.LatestVersion, dll.Installed, dll.SkippedVersion)
			statuses = append(statuses, dll)
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

// cachedLibsVersion reports libraries ready for the next launch. Downloads are installed in the shared
// cache before the next launch deploys them, so an older importer manifest must not announce them again.
func cachedLibsVersion(latest, skipped, deployed string, cacheVerified func(string) bool) string {
	if deployed != "" {
		return selectLibsVersion(latest, skipped, deployed, cacheVerified)
	}
	if latest != "" && cacheVerified(latest) {
		return latest
	}
	return ""
}

func newestCachedPackageVersion(pkg string) string {
	root, err := xxmiCacheRoot()
	if err != nil {
		return ""
	}
	entries, err := os.ReadDir(filepath.Join(root, "packages", pkg))
	if err != nil {
		return ""
	}
	latest := ""
	for _, entry := range entries {
		version := entry.Name()
		if !entry.IsDir() || !semver.IsValid("v"+version) {
			continue
		}
		if latest == "" || semver.Compare("v"+version, "v"+latest) > 0 {
			latest = version
		}
	}
	return latest
}

func selectedLegacyInjectorVersion(pin string) string {
	if version := normalizeVersion(pin); version != "" {
		return version
	}
	return newestCachedPackageVersion("xxmi-libs")
}

func updateAvailable(latest, installed, skipped string) bool {
	if latest == "" || latest == installed || latest == skipped {
		return false
	}
	if semver.IsValid("v"+latest) && semver.IsValid("v"+installed) {
		return semver.Compare("v"+latest, "v"+installed) > 0
	}
	return true
}

func legacyUsesXXMIInjector(cfg ImporterConfig) bool {
	return cfg.Mode == RuntimeLegacy && cfg.InjectionMethod != "Native" &&
		cfg.ExtraLibraries.Enabled && len(cfg.ExtraLibraries.Paths) > 0
}

func (x *XXMI) SkipVersion(ctx context.Context, pkg, version string) error {
	if _, overlay := lookupOverlayPackage(pkg); pkg != "xxmi-libs" && pkg != "gi-fps-unlocker" && !overlay {
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

// InstallUpdates installs the requested packages' available updates. A non-empty importer limits the install
// to that importer's packages; an empty one covers every importer.
func (x *XXMI) InstallUpdates(ctx context.Context, importer string, targets []string) ([]string, error) {
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
		if !requested[status.Package] || !status.Available || status.Pinned ||
			importer != "" && status.Importer != importer {
			continue
		}
		provider, overlay := lookupOverlayPackage(status.Package)
		switch {
		case overlay:
			err = x.ensureProviderDLL(ctx, provider, status.LatestVersion)
		case strings.HasPrefix(status.Package, "importer:"):
			err = x.InstallImporterPackage(ctx, InstallImporterPackageInput{
				Importer: status.Importer, Version: status.LatestVersion,
			})
		case status.Package == "xxmi-libs":
			err = x.installLibsUpdate(ctx, status.Importer, status.LatestVersion)
		case status.Package == "gi-fps-unlocker":
			err = x.EnsureFPSUnlockerVersion(ctx, status.LatestVersion)
		default:
			err = fmt.Errorf("unknown XXMI package %q", status.Package)
		}
		if err != nil {
			return installed, fmt.Errorf("install %s update: %w", status.Package, err)
		}

		// The shared libraries report one status per importer, and each one may hold its own pin to move.
		if !slices.Contains(installed, status.Package) {
			installed = append(installed, status.Package)
		}
	}
	for pkg := range requested {
		if pkg != "" && !slices.Contains(installed, pkg) {
			return installed, fmt.Errorf("update target %s is not available", pkg)
		}
	}
	return installed, nil
}

// installLibsUpdate caches the release and moves the pin of an importer that announces updates for its own
// pinned version. Importers following a release pick the cached version up at their next launch.
func (x *XXMI) installLibsUpdate(ctx context.Context, importer, version string) error {
	if err := x.EnsureLibsVersion(ctx, version); err != nil {
		return err
	}
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil || cfg.XXMIVersion.Pinned == "" {
		return err
	}
	cfg.XXMIVersion.Pinned = version
	return x.SaveImporterConfig(ctx, importer, cfg)
}

// Launch update modes. Keep the stored values in sync with the xxmi.autoUpdate setting.
const (
	launchUpdateAuto   = "auto"
	launchUpdateNotify = "notify"
	launchUpdateOff    = "off"
)

// launchUpdateMode reports how a launch treats available updates: installing them silently, asking first,
// or launching without them. The setting used to be a boolean, so "false" still reads as off.
func (x *XXMI) launchUpdateMode(ctx context.Context) (string, error) {
	client, err := x.settingsClient()
	if err != nil {
		return "", err
	}
	value, err := client.Settings.GetValue(ctx, "xxmi_auto_update")
	if err != nil || value == nil {
		return launchUpdateAuto, err
	}
	switch *value {
	case launchUpdateNotify:
		return launchUpdateNotify, nil
	case launchUpdateOff, "false":
		return launchUpdateOff, nil
	}
	return launchUpdateAuto, nil
}

// LaunchUpdates returns the importer's updates that the user confirms before a launch. It is empty unless
// launches are set to ask, and leaves out packages held at a version without update notices.
func (x *XXMI) LaunchUpdates(ctx context.Context, importer string) ([]UpdateStatus, error) {
	mode, err := x.launchUpdateMode(ctx)
	if err != nil || mode != launchUpdateNotify {
		return []UpdateStatus{}, err
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil {
		return nil, err
	}
	pending := []UpdateStatus{}
	for _, status := range statuses {
		if status.Importer == importer && status.Available && !status.Pinned {
			pending = append(pending, status)
		}
	}
	return pending, nil
}

func (x *XXMI) autoUpdateForLaunch(ctx context.Context, importer string) error {
	mode, err := x.launchUpdateMode(ctx)
	if err != nil || mode != launchUpdateAuto {
		return err
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil {
		return err
	}
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return err
	}
	targets := []string{}
	for _, status := range statuses {
		// A pinned XXMI libraries version only announces its update; moving the pin stays a manual choice.
		if status.Package == "xxmi-libs" && cfg.XXMIVersion.Pinned != "" {
			continue
		}
		if status.Importer == importer && status.Available && !status.Pinned {
			targets = append(targets, status.Package)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	_, err = x.InstallUpdates(ctx, importer, targets)
	return err
}
