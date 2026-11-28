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
	"nahida.live/desktop/internal/elevated"
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
	// Downgrade marks an available release older than the installed one, as after a release is withdrawn.
	Downgrade bool `json:"downgrade"`
	// Shared marks the XXMI libraries of an importer that follows the shared version, whose update belongs to
	// the runtime as a whole rather than to that importer.
	Shared bool `json:"shared"`
}

// UpdateProgress is the xxmi:update-progress payload. An install of one or more updates reports "start" with the
// importers and packages it covers, "package" as it moves to each package, and "end" when it stops for any reason.
// Package downloads made during the install carry the same Run in their xxmi:package-progress payload. Installs
// run one at a time, so the events of two runs never interleave.
type UpdateProgress struct {
	Run       int64    `json:"run"`
	Stage     string   `json:"stage"`
	Importers []string `json:"importers,omitempty"`
	Packages  []string `json:"packages,omitempty"`
	Package   string   `json:"package,omitempty"`
	Index     int      `json:"index,omitempty"`
}

type updateRunKey struct{}

func updateRunOf(ctx context.Context) (int64, bool) {
	run, ok := ctx.Value(updateRunKey{}).(int64)
	return run, ok
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
	signedFollowed := false
	for _, row := range rows {
		cfg, err := x.GetImporterConfig(ctx, row.Key)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			continue
		}
		source, _ := PackageSource(row.Key, cfg)
		packages[ImporterPackageID(row.Key, source)] = struct{}{}
		provider, err := x.deployedLibsProvider(ctx, cfg)
		if err != nil {
			return nil, err
		}
		providers[row.Key] = provider
		// A provider release without the version mark deploys over the latest signed release, which is checked
		// here too but not announced apart from the provider's own update, nor allowed to fail the check.
		if followsSignedLibs(cfg, provider) || provider.overlayPackage != "" {
			packages["xxmi-libs"] = struct{}{}
			signedFollowed = signedFollowed || followsSignedLibs(cfg, provider)
		}
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
			if pkg == "xxmi-libs" && !signedFollowed {
				continue
			}
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
		source, _ := PackageSource(row.Key, cfg)
		pkg := ImporterPackageID(row.Key, source)
		status := updateStatus(row.Key, pkg, states[pkg])
		// A package from another source than the selected one reports no version, so the selected source's latest
		// release is offered in its place.
		if installed := selectedInstalledPackage(row.Key, cfg); installed != nil {
			status.Installed = *installed
		}
		status.Pinned = cfg.PackageVersion.Pinned != ""
		status.Available, status.Downgrade = updateDirection(
			status.LatestVersion,
			status.Installed,
			status.SkippedVersion,
		)
		statuses = append(statuses, status)

		pin, notify, err := x.libsPin(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if followsSignedLibs(cfg, providers[row.Key]) {
			libs := updateStatus(row.Key, "xxmi-libs", states["xxmi-libs"])
			libs.Pinned = pin != "" && !notify
			libs.Shared = cfg.XXMIVersion.Follow == followShared
			if cfg.Mode == RuntimeXXMI {
				libs.Installed, _ = deployedLibsVersion(cfg.ImporterFolder)
				if pin == "" {
					_, _, downgrade := packageVersions(states["xxmi-libs"])
					libs.Installed = cachedLibsVersion(
						libs.LatestVersion, libs.SkippedVersion, downgrade, libs.Installed, x.verifiedCachedLibsVersion,
					)
				}
			}

			// A runtime not deployed since migration, or an adopted custom DLL whose manifest has no source,
			// reports no version; the next launch deploys the selected cached libraries without downloading.
			if libs.Installed == "" && (cfg.Mode == RuntimeXXMI || legacyUsesXXMIInjector(cfg)) {
				if version := x.selectedLegacyInjectorVersion(
					ctx, cfg, pin,
				); version != "" &&
					x.verifiedCachedLibsVersion(version) {
					libs.Installed = version
				}
			}

			// A pin that still announces releases is compared by the pinned version itself, so moving the pin
			// clears the notice before the next launch deploys it.
			if notify {
				if spec, err := x.pinProvider(ctx, cfg); err == nil {
					if signed, err := x.signedLibsOf(ctx, spec, pin, cfg.ImporterFolder); err == nil {
						libs.Installed = signed
					}
				}
			}
			libs.Available, libs.Downgrade = updateDirection(libs.LatestVersion, libs.Installed, libs.SkippedVersion)
			statuses = append(statuses, libs)
		}

		// A provider d3d11.dll is tracked beside the signed libraries it is deployed over.
		if provider := providers[row.Key]; provider.overlayPackage != "" {
			dll := updateStatus(row.Key, provider.overlayPackage, states[provider.overlayPackage])
			dll.Pinned = pin != "" && !notify
			dll.Shared = cfg.LibsProvider == ""
			if pin == "" {
				_, _, downgrade := packageVersions(states[provider.overlayPackage])
				dll.Installed = cachedLibsVersion(
					dll.LatestVersion,
					dll.SkippedVersion,
					downgrade,
					x.providerDLLBaseVersion(cfg.ImporterFolder, provider),
					func(version string) bool { return x.verifiedProviderDLL(ctx, provider, version) },
				)
			} else if version, err := x.resolveProviderDLLVersion(ctx, provider, pin, cfg.ImporterFolder); err == nil {
				dll.Installed = version
			}
			dll.Available, dll.Downgrade = updateDirection(dll.LatestVersion, dll.Installed, dll.SkippedVersion)
			statuses = append(statuses, dll)
		}
		if row.Key == "GIMI" && cfg.GIMI != nil && cfg.GIMI.UnlockFPS {
			fps := updateStatus(row.Key, "gi-fps-unlocker", states["gi-fps-unlocker"])
			fps.Installed = x.cachedReleaseVersion(ctx, "gi-fps-unlocker", x.verifiedFPSUnlocker)
			fps.Available, fps.Downgrade = updateDirection(fps.LatestVersion, fps.Installed, fps.SkippedVersion)
			statuses = append(statuses, fps)
		}
	}

	// A downgrade is offered to leave a withdrawn release. A deployed prerelease is still published, but its
	// version may carry no prerelease suffix, as SpectrumQT tags them, so only the release list tells.
	// Importers share packages, so each installed release is looked up once.
	prereleases := make(map[[2]string]bool)
	for i := range statuses {
		status := &statuses[i]
		if !status.Downgrade {
			continue
		}
		key := [2]string{status.Package, status.Installed}
		prerelease, known := prereleases[key]
		if !known {
			var err error
			prerelease, err = x.publishedPrerelease(ctx, status.Package, status.Installed)
			if err != nil {
				_ = infra.ReportError(x.log, err, "XXMI.CheckUpdates", infra.Diagnostic{
					Severity: infra.DiagnosticWarn, Operation: "check-updates", Stage: "prerelease-lookup",
					Fields: map[string]any{"package": status.Package, "installed": status.Installed},
				})
			}
			prereleases[key] = prerelease
		}
		if prerelease {
			status.Available, status.Downgrade = false, false
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

// packageVersions returns the normalized latest, skipped and downgraded-to versions of a package state, which
// may be nil.
func packageVersions(state *db.XXMIPackageRow) (latest, skipped, downgrade string) {
	if state == nil {
		return "", "", ""
	}
	if state.LatestVersion != nil {
		latest = normalizeVersion(*state.LatestVersion)
	}
	if state.SkippedVersion != nil {
		skipped = normalizeVersion(*state.SkippedVersion)
	}
	if state.DowngradeVersion != nil {
		downgrade = normalizeVersion(*state.DowngradeVersion)
	}
	return latest, skipped, downgrade
}

// cachedLibsVersion reports libraries ready for the next launch. Downloads are installed in the shared
// cache before the next launch deploys them, so an older importer manifest must not announce them again.
func cachedLibsVersion(latest, skipped, downgrade, deployed string, cacheVerified func(string) bool) string {
	if deployed != "" {
		return selectLibsVersion(latest, skipped, downgrade, deployed, cacheVerified)
	}
	if latest != "" && cacheVerified(latest) {
		return latest
	}
	return ""
}

func (x *XXMI) newestCachedPackageVersion(pkg string) string {
	root, err := x.cacheRoot()
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

// cachedReleaseVersion returns the cached release of pkg used where none is deployed: the release the user last
// installed while it stays cached, otherwise the newest cached one. Older releases stay cached and the latest
// release changes with every update check, so neither alone means the user chose it.
func (x *XXMI) cachedReleaseVersion(ctx context.Context, pkg string, cacheVerified func(string) bool) string {
	newest := x.newestCachedPackageVersion(pkg)
	client, err := x.settingsClient()
	if err != nil {
		return newest
	}
	state, err := client.XXMIPackages.Get(ctx, pkg)
	if err != nil || state == nil || state.SelectedVersion == nil {
		return newest
	}
	if selected := normalizeVersion(*state.SelectedVersion); cacheVerified(selected) {
		return selected
	}
	return newest
}

// recordSelectedRelease makes version the release cachedReleaseVersion returns for pkg.
func (x *XXMI) recordSelectedRelease(ctx context.Context, pkg, version string) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	return client.XXMIPackages.SetSelectedVersion(ctx, pkg, normalizeVersion(version))
}

func (x *XXMI) selectedLegacyInjectorVersion(ctx context.Context, cfg ImporterConfig, pin string) string {
	if pin == "" {
		return x.cachedReleaseVersion(ctx, "xxmi-libs", x.verifiedCachedLibsVersion)
	}
	spec, err := x.pinProvider(ctx, cfg)
	if err != nil {
		return signedLibsVersion(pin)
	}
	if version, err := x.signedLibsOf(ctx, spec, pin, cfg.ImporterFolder); err == nil && version != "" {
		return version
	}
	return signedLibsVersion(pin)
}

// updateAvailable reports whether latest is newer than installed. The external launcher, which installs its
// own updates, uses it; the built-in launcher also offers downgrades through updateDirection.
func updateAvailable(latest, installed, skipped string) bool {
	available, downgrade := updateDirection(latest, installed, skipped)
	return available && !downgrade
}

// updateDirection reports whether latest is offered over installed and whether it is older than installed. An
// installed prerelease is kept over an older latest release, which hides prereleases unless they are enabled.
func updateDirection(latest, installed, skipped string) (available, downgrade bool) {
	if latest == "" || latest == installed || latest == skipped {
		return false, false
	}
	order, ok := compareReleaseVersions(latest, installed)
	if !ok {
		return true, false
	}
	switch {
	case order > 0:
		return true, false
	case order < 0 && semver.Prerelease("v"+installed) == "":
		return true, true
	}
	return false, false
}

// followsSignedLibs reports whether the importer's updates follow the signed libraries releases. Under a
// provider d3d11.dll the signed libraries are the ones that release was built on, so only the provider's
// releases are followed.
func followsSignedLibs(cfg ImporterConfig, provider libsProviderSpec) bool {
	return provider.overlayPackage == "" &&
		(cfg.Mode == RuntimeXXMI || cfg.XXMIVersion.Pinned != "" || legacyUsesXXMIInjector(cfg))
}

func legacyUsesXXMIInjector(cfg ImporterConfig) bool {
	return cfg.Mode == RuntimeLegacy && cfg.InjectionMethod != "Native" &&
		cfg.ExtraLibraries.Enabled && len(cfg.ExtraLibraries.Paths) > 0
}

func (x *XXMI) SkipVersion(ctx context.Context, pkg, version string) error {
	if _, overlay := lookupOverlayPackage(pkg); pkg != "xxmi-libs" && pkg != "gi-fps-unlocker" && !overlay {
		if _, ok := parseImporterPackageID(pkg); !ok {
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
	x.updateMu.Lock()
	defer x.updateMu.Unlock()
	return x.installUpdates(ctx, importer, targets, nil)
}

// installUpdates is InstallUpdates for a caller that holds updateMu.
func (x *XXMI) installUpdates(
	ctx context.Context, importer string, targets []string, lease *elevated.FileLease,
) ([]string, error) {
	// Each package install below times its own stages.
	watch := infra.NewStallWatch(x.log, "XXMI.InstallUpdates", "install-updates", map[string]any{"importer": importer})
	watch.Stage("check-updates")
	statuses, err := x.CheckUpdates(ctx, false)
	watch.Stop()
	if err != nil {
		return nil, err
	}
	requested := make(map[string]bool, len(targets))
	for _, target := range targets {
		requested[target] = true
	}
	installable := func(status UpdateStatus) bool {
		return requested[status.Package] && status.Available && !status.Pinned &&
			(importer == "" || status.Importer == importer)
	}
	var order, importers []string
	for _, status := range statuses {
		if !installable(status) {
			continue
		}
		if !slices.Contains(order, status.Package) {
			order = append(order, status.Package)
		}
		if !slices.Contains(importers, status.Importer) {
			importers = append(importers, status.Importer)
		}
	}

	run, reporting := int64(0), len(order) > 0 && x.eventEmit != nil
	if reporting {
		run = x.updateRunSeq.Add(1)
		ctx = context.WithValue(ctx, updateRunKey{}, run)
		x.eventEmit("xxmi:update-progress", UpdateProgress{
			Run: run, Stage: "start", Importers: importers, Packages: order,
		})
		defer x.eventEmit("xxmi:update-progress", UpdateProgress{Run: run, Stage: "end"})
	}
	var started []string
	installed := []string{}
	for _, status := range statuses {
		if err := ctx.Err(); err != nil {
			return installed, err
		}
		if !installable(status) {
			continue
		}
		// The shared libraries report one status per importer between the importers' own packages, so only the
		// first one reports the package; the later ones count toward the package reported last.
		if !slices.Contains(started, status.Package) {
			started = append(started, status.Package)
			if reporting {
				x.eventEmit("xxmi:update-progress", UpdateProgress{
					Run: run, Stage: "package", Package: status.Package, Index: len(started),
				})
			}
		}
		provider, overlay := lookupOverlayPackage(status.Package)
		switch {
		case overlay:
			err = x.installProviderUpdate(ctx, status.Importer, provider, status.LatestVersion)
		case strings.HasPrefix(status.Package, "importer:"):
			err = x.installImporterPackage(ctx, InstallImporterPackageInput{
				Importer: status.Importer, Version: status.LatestVersion,
			}, lease)
		case status.Package == "xxmi-libs":
			err = x.installLibsUpdate(ctx, status.Importer, status.LatestVersion)
		case status.Package == "gi-fps-unlocker":
			err = x.ensureFPSUnlockerVersion(ctx, status.LatestVersion)
		default:
			err = fmt.Errorf("unknown XXMI package %q", status.Package)
		}
		if err != nil {
			return installed, fmt.Errorf("install %s update: %w", status.Package, err)
		}
		if status.Downgrade && !strings.HasPrefix(status.Package, "importer:") {
			if err := x.recordDowngrade(ctx, status.Package, status.LatestVersion); err != nil {
				return installed, fmt.Errorf("record %s downgrade: %w", status.Package, err)
			}
		}
		if status.Package == "xxmi-libs" || status.Package == "gi-fps-unlocker" {
			if err := x.recordSelectedRelease(ctx, status.Package, status.LatestVersion); err != nil {
				return installed, fmt.Errorf("record %s selection: %w", status.Package, err)
			}
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

// recordDowngrade lets launches select a cached release below the deployed one; see selectLibsVersion. An
// importer package is installed into its folder and needs no record.
func (x *XXMI) recordDowngrade(ctx context.Context, pkg, version string) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	return client.XXMIPackages.SetDowngradeVersion(ctx, pkg, version)
}

// installLibsUpdate caches the release and moves the pin of an importer that announces updates for its own
// pinned version. Importers following a release pick the cached version up at their next launch.
func (x *XXMI) installLibsUpdate(ctx context.Context, importer, version string) error {
	if err := x.EnsureLibsVersion(ctx, version); err != nil {
		return err
	}
	return x.moveLibsPin(ctx, importer, version)
}

// installProviderUpdate is installLibsUpdate for a provider release, cached together with the signed libraries
// it deploys over. A release without the version mark brings the latest signed release along.
func (x *XXMI) installProviderUpdate(
	ctx context.Context,
	importer string,
	provider libsProviderSpec,
	version string,
) error {
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return err
	}
	signed, err := x.signedLibsToFetch(ctx, provider, version, cfg.ImporterFolder)
	if err != nil {
		return err
	}
	if err := x.EnsureLibsVersion(ctx, signed); err != nil {
		return err
	}
	if err := x.ensureProviderDLL(ctx, provider, version); err != nil {
		return err
	}
	return x.moveLibsPin(ctx, importer, version)
}

func (x *XXMI) moveLibsPin(ctx context.Context, importer, version string) error {
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

func (x *XXMI) autoUpdateForLaunch(ctx context.Context, importer string, lease *elevated.FileLease) error {
	mode, err := x.launchUpdateMode(ctx)
	if err != nil || mode != launchUpdateAuto {
		return err
	}
	x.updateMu.Lock()
	defer x.updateMu.Unlock()
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
		_, overlay := lookupOverlayPackage(status.Package)
		if (status.Package == "xxmi-libs" || overlay) && cfg.XXMIVersion.Pinned != "" {
			continue
		}
		if status.Importer == importer && status.Available && !status.Pinned {
			targets = append(targets, status.Package)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	_, err = x.installUpdates(ctx, importer, targets, lease)
	return err
}
