package xxmi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	signedLibsProvider    = "spectrumqt"
	defaultLibsProvider   = "myparsleycat"
	sharedLibsProviderKey = "xxmi_libs_provider"
	providerDLLHashesKey  = "xxmi_libs_provider_hashes"
	providerDLLMetadata   = "source.json"

	// providerININame is the d3dx.ini a provider release may ship beside its d3d11.dll. It lists the options
	// that DLL reads, and a launch rebuilds the importer's d3dx.ini from it.
	providerININame      = "d3dx.ini"
	providerINISizeLimit = 1 << 20
)

// libsProviderSpec is a source of the XXMI libraries an importer can run.
type libsProviderSpec struct {
	id   string
	repo github.Repo
	// overlayPackage names the package of a provider that ships only d3d11.dll. It is deployed over the
	// signed SpectrumQT package, which still supplies the loader and the compiler.
	overlayPackage string
	// versionMark separates the signed libraries version a provider release is built on from the provider's
	// own revision, as in "1.2.2-nhd.1".
	versionMark string
	// smoothMotion is whether the provider's d3d11.dll renders alongside NVIDIA Smooth Motion. Without it,
	// Smooth Motion has to be off for mods to load.
	smoothMotion bool
	// dx12 is whether the provider's d3d11.dll applies mods to a game rendering with DirectX 12. Without it,
	// mods only load with DirectX 11.
	dx12 bool
	// companions are the libraries a provider release may ship for its d3d11.dll to load. They are deployed
	// beside it.
	companions []string
}

// releaseFiles names the files a provider release may ship beside its d3d11.dll. Adding one here is all it
// takes for the entries cached without it to be searched for it again.
func (spec libsProviderSpec) releaseFiles() []string {
	return append([]string{providerININame}, spec.companions...)
}

func providerFileSizeLimit(name string) int64 {
	if name == providerININame {
		return providerINISizeLimit
	}
	return customDLLSizeLimit
}

var libsProviders = []libsProviderSpec{
	{id: signedLibsProvider, repo: libsRepo},
	{
		id:             defaultLibsProvider,
		repo:           github.Repo{Owner: "myparsleycat", Name: "XXMI-Libs-Package-Forked"},
		overlayPackage: "xxmi-libs-myparsleycat",
		versionMark:    "-nhd.",
		smoothMotion:   true,
		dx12:           true,
		companions:     []string{"dxcompiler.dll", "dxil.dll"},
	},
}

// providerDLLSource records where a cached provider d3d11.dll came from. Its releases are not signed, so the
// hash GitHub published for the asset is what later reads check the file against.
type providerDLLSource struct {
	Version   string `json:"version"`
	Tag       string `json:"tag"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	FetchedAt string `json:"fetchedAt"`
	// Searched names the files beside the DLL the release was searched for. A build that did not know of a
	// file leaves its name out, so the release may ship one that was never downloaded.
	Searched []string `json:"searched,omitempty"`
}

// providerRuntimeDLL is the provider d3d11.dll a deployment writes instead of the signed one.
type providerRuntimeDLL struct {
	// source is the manifest entry, "<overlay package>@<version>".
	source string
	data   []byte
	// companions are the libraries the release ships for the DLL to load, by file name.
	companions map[string][]byte
}

func lookupLibsProvider(id string) (libsProviderSpec, bool) {
	for _, spec := range libsProviders {
		if spec.id == id {
			return spec, true
		}
	}
	return libsProviderSpec{}, false
}

// libsProviderIDs lists the selectable providers, the signed one first.
func libsProviderIDs() []string {
	ids := make([]string, 0, len(libsProviders))
	for _, spec := range libsProviders {
		ids = append(ids, spec.id)
	}
	return ids
}

func lookupOverlayPackage(pkg string) (libsProviderSpec, bool) {
	for _, spec := range libsProviders {
		if spec.overlayPackage != "" && spec.overlayPackage == pkg {
			return spec, true
		}
	}
	return libsProviderSpec{}, false
}

// signedLibsVersion returns the signed SpectrumQT libraries a libraries version deploys: the release a
// provider version is built on, or the version itself.
func signedLibsVersion(version string) string {
	version = normalizeVersion(version)
	for _, spec := range libsProviders {
		if spec.versionMark == "" {
			continue
		}
		if signed, _, ok := strings.Cut(version, spec.versionMark); ok && signed != "" {
			return signed
		}
	}
	return version
}

// GetLibsProviderReleases lists the libraries versions an importer of the provider can be held to.
func (x *XXMI) GetLibsProviderReleases(ctx context.Context, provider string) ([]ReleaseInfo, error) {
	spec, ok := lookupLibsProvider(strings.TrimSpace(provider))
	if !ok {
		return nil, fmt.Errorf("unknown XXMI libraries provider %q", provider)
	}
	return x.ListReleases(ctx, cmp.Or(spec.overlayPackage, "xxmi-libs"))
}

// SetSharedLibsProvider selects the XXMI libraries provider for importers that do not choose their own, in
// place of a shared custom DLL. The provider's libraries are cached first, so a provider that cannot be
// downloaded is not selected.
func (x *XXMI) SetSharedLibsProvider(ctx context.Context, provider string) (returnErr error) {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	provider = strings.TrimSpace(provider)

	// The download can take a while, and a later selection must not be overwritten when it finishes.
	x.libsProviderMu.Lock()
	defer x.libsProviderMu.Unlock()

	// The shared version names a release of the provider it was picked from, so it moves along with the
	// provider: to the release built on the same signed libraries, or back to following the latest one.
	stored, err := client.Settings.GetValue(ctx, sharedLibsVersionKey)
	if err != nil {
		return err
	}
	pinned, version := "", ""
	if stored != nil {
		pinned = normalizeVersion(*stored)
	}
	spec, known := lookupLibsProvider(provider)
	if known && pinned != "" {
		if version, err = x.providerPin(ctx, spec, pinned); err != nil {
			return err
		}
	}
	if err := x.EnsureLibsProvider(ctx, provider, version); err != nil {
		return err
	}

	// The shared custom DLL goes before the pins move, so the importers that launched it are moved with the
	// rest. A selection that fails afterwards puts it back.
	x.customDLLMu.Lock()
	customDLL, err := client.Settings.GetValue(ctx, sharedCustomDLLKey)
	if err == nil && customDLL != nil && strings.TrimSpace(*customDLL) != "" {
		err = client.Settings.Upsert(ctx, sharedCustomDLLKey, new(string))
		defer func() {
			x.customDLLMu.Lock()
			defer x.customDLLMu.Unlock()
			if returnErr == nil {
				x.reportCleanup(x.pruneCustomDLLsLocked(ctx), "prune-custom-dll")
				return
			}
			x.reportCleanup(
				client.Settings.Upsert(context.WithoutCancel(ctx), sharedCustomDLLKey, customDLL),
				"restore-shared-custom-dll",
			)
		}()
	}
	x.customDLLMu.Unlock()
	if err != nil {
		return err
	}

	// An importer that follows the shared provider but pins its own version holds a release of it as well.
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		cfg, err := x.GetImporterConfig(ctx, row.Key)
		if err != nil {
			return err
		}
		own := normalizeVersion(cfg.XXMIVersion.Pinned)
		if cfg.Mode != RuntimeXXMI || cfg.LibsProvider != "" || own == "" {
			continue
		}

		// A user-provided DLL deploys over the signed libraries, which a pin of either provider names.
		custom, err := x.launchesCustomDLL(ctx, cfg)
		if err != nil {
			return err
		}
		if custom {
			continue
		}
		moved, err := x.providerPin(ctx, spec, own)
		if err != nil {
			return err
		}
		if moved == own {
			continue
		}
		if err := x.EnsureLibsProvider(ctx, provider, moved); err != nil {
			return err
		}
		cfg.XXMIVersion.Pinned = moved
		if moved == "" {
			cfg.XXMIVersion = VersionPin{Follow: "latest"}
		}
		if err := x.SaveImporterConfig(ctx, row.Key, cfg); err != nil {
			return err
		}
	}
	if version != pinned {
		if err := client.Settings.Upsert(ctx, sharedLibsVersionKey, &version); err != nil {
			return err
		}
	}
	return client.Settings.Upsert(ctx, sharedLibsProviderKey, &provider)
}

// EnsureLibsProvider caches the libraries a provider adds to the signed package, so the next launch deploys
// them without downloading. An empty pin caches the release an importer following the latest one deploys.
func (x *XXMI) EnsureLibsProvider(ctx context.Context, provider, pin string) (returnErr error) {
	stage, version := "resolve-provider", ""
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.EnsureLibsProvider", infra.Diagnostic{
			Operation: "ensure-libs-provider", Stage: stage,
			Fields: map[string]any{"provider": provider, "pin": pin, "version": version},
		})
	}()
	spec, ok := lookupLibsProvider(provider)
	if !ok {
		return fmt.Errorf("unknown XXMI libraries provider %q", provider)
	}
	if spec.overlayPackage == "" {
		return nil
	}

	stage = "resolve-version"
	version, err := x.resolveProviderDLLVersion(ctx, spec, normalizeVersion(pin), "")
	if err != nil {
		return err
	}
	stage = "download"
	return x.ensureProviderDLL(ctx, spec, version)
}

func (x *XXMI) sharedLibsProvider(ctx context.Context) (libsProviderSpec, error) {
	fallback, _ := lookupLibsProvider(defaultLibsProvider)
	client, err := x.settingsClient()
	if err != nil {
		return fallback, err
	}
	stored, err := client.Settings.GetValue(ctx, sharedLibsProviderKey)
	if err != nil || stored == nil {
		return fallback, err
	}
	if spec, ok := lookupLibsProvider(strings.TrimSpace(*stored)); ok {
		return spec, nil
	}
	return fallback, nil
}

// libsProvider returns the provider whose libraries an importer deploys. Only the XXMI runtime has one; an
// importer without its own choice follows the shared provider.
func (x *XXMI) libsProvider(ctx context.Context, cfg ImporterConfig) (libsProviderSpec, error) {
	fallback, _ := lookupLibsProvider(signedLibsProvider)
	if cfg.Mode != RuntimeXXMI {
		return fallback, nil
	}
	if cfg.LibsProvider == "" {
		return x.sharedLibsProvider(ctx)
	}
	if spec, ok := lookupLibsProvider(cfg.LibsProvider); ok {
		return spec, nil
	}
	return fallback, nil
}

// deployedLibsProvider returns the provider whose d3d11.dll the importer's next deployment writes, which is
// also the one its updates follow. A user-provided DLL takes that file's place, so the provider it hides is
// neither downloaded nor allowed to hold back a launch.
func (x *XXMI) deployedLibsProvider(ctx context.Context, cfg ImporterConfig) (libsProviderSpec, error) {
	custom, err := x.launchesCustomDLL(ctx, cfg)
	if err != nil {
		return libsProviderSpec{}, err
	}
	if custom {
		fallback, _ := lookupLibsProvider(signedLibsProvider)
		return fallback, nil
	}
	return x.libsProvider(ctx, cfg)
}

// providerRuntimeDLL returns the cached d3d11.dll of the importer's provider, or nil when the provider
// ships the whole signed package.
func (x *XXMI) providerRuntimeDLL(ctx context.Context, cfg ImporterConfig) (*providerRuntimeDLL, error) {
	spec, err := x.deployedLibsProvider(ctx, cfg)
	if err != nil || spec.overlayPackage == "" {
		return nil, err
	}
	pin, _, err := x.libsPin(ctx, cfg)
	if err != nil {
		return nil, err
	}
	version, err := x.resolveProviderDLLVersion(ctx, spec, pin, cfg.ImporterFolder)
	if err != nil {
		return nil, err
	}
	if err := x.ensureProviderDLL(ctx, spec, version); err != nil {
		return nil, err
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return nil, err
	}
	_, data, err := x.readVerifiedProviderDLL(ctx, root, spec, version)
	if err != nil {
		return nil, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
	}
	companions, err := x.readVerifiedProviderCompanions(ctx, root, spec, version)
	if err != nil {
		return nil, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
	}
	return &providerRuntimeDLL{
		source: spec.overlayPackage + "@" + version, data: data, companions: companions,
	}, nil
}

// dllFeatureSupport is whether the d3d11.dll an importer's next deployment writes has a feature only some
// providers ship, such as rendering alongside NVIDIA Smooth Motion.
type dllFeatureSupport struct {
	supported bool
	provider  string
	// reason is why the DLL counts as unsupported, and err the failure behind it, if any.
	reason string
	err    error
}

const (
	dllFeatureCustomDLL           = "custom-dll"
	dllFeatureProviderUnresolved  = "provider-unresolved"
	dllFeatureProviderUnsupported = "provider-unsupported"
)

// dllFeatureSupport reports whether the d3d11.dll the importer's next deployment writes comes from a provider
// with the feature. A user-provided DLL cannot be told, so it counts as unsupported.
func (x *XXMI) dllFeatureSupport(
	ctx context.Context,
	cfg ImporterConfig,
	has func(libsProviderSpec) bool,
) dllFeatureSupport {
	custom, err := x.launchesCustomDLL(ctx, cfg)
	if err != nil {
		return dllFeatureSupport{reason: dllFeatureProviderUnresolved, err: err}
	}
	if custom {
		return dllFeatureSupport{reason: dllFeatureCustomDLL}
	}
	spec, err := x.libsProvider(ctx, cfg)
	if err != nil {
		return dllFeatureSupport{provider: spec.id, reason: dllFeatureProviderUnresolved, err: err}
	}
	if !has(spec) {
		return dllFeatureSupport{provider: spec.id, reason: dllFeatureProviderUnsupported}
	}
	return dllFeatureSupport{provider: spec.id, supported: true}
}

// providerPin moves a pinned libraries version to a provider: to the release built on the same signed
// libraries, or to "" when the provider has none and the pin goes back to following the latest release.
func (x *XXMI) providerPin(ctx context.Context, spec libsProviderSpec, pinned string) (string, error) {
	if spec.overlayPackage == "" {
		return signedLibsVersion(pinned), nil
	}
	return x.providerRelease(ctx, spec, pinned)
}

// providerRelease returns the provider release a pinned libraries version stands for: that release, or the
// newest one built on the signed libraries the pin names. It is "" when the provider has neither.
func (x *XXMI) providerRelease(ctx context.Context, spec libsProviderSpec, pin string) (string, error) {
	if spec.versionMark != "" && strings.Contains(pin, spec.versionMark) {
		return pin, nil
	}
	releases, err := x.ListReleases(ctx, spec.overlayPackage)
	if err != nil {
		return "", err
	}
	signed, built := signedLibsVersion(pin), ""
	for _, release := range releases {
		if release.Version == pin {
			return pin, nil
		}
		if built == "" && signedLibsVersion(release.Version) == signed {
			built = release.Version
		}
	}
	return built, nil
}

// resolveProviderDLLVersion picks the provider release an importer deploys: the pinned one, or else the
// latest release with the same rules as the signed libraries, where a skipped or not yet cached update keeps
// the deployed one.
func (x *XXMI) resolveProviderDLLVersion(
	ctx context.Context,
	spec libsProviderSpec,
	pin, importerFolder string,
) (string, error) {
	client, err := x.settingsClient()
	if err != nil {
		return "", err
	}
	if pin != "" {
		version, err := x.providerRelease(ctx, spec, pin)
		if err != nil {
			return "", fmt.Errorf("resolve %s for XXMI libraries %s: %w", spec.overlayPackage, pin, err)
		}
		if version == "" {
			return "", fmt.Errorf("%s has no release for XXMI libraries %s", spec.overlayPackage, pin)
		}
		return version, nil
	}
	deployed := providerDLLBaseVersion(importerFolder, spec)
	cacheVerified := func(version string) bool { return x.verifiedProviderDLL(ctx, spec, version) }
	pkg, err := client.XXMIPackages.Get(ctx, spec.overlayPackage)
	if err != nil {
		return "", err
	}
	if pkg == nil || pkg.LatestVersion == nil || strings.TrimSpace(*pkg.LatestVersion) == "" {
		releases, err := x.ListReleases(ctx, spec.overlayPackage)
		if err == nil && len(releases) > 0 {
			return selectLibsVersion(releases[0].Version, "", deployed, cacheVerified), nil
		}
		if deployed != "" {
			return deployed, nil
		}
		if err != nil {
			return "", fmt.Errorf("resolve latest %s: %w", spec.overlayPackage, err)
		}
		return "", fmt.Errorf("%s has no releases", spec.overlayPackage)
	}
	skipped := ""
	if pkg.SkippedVersion != nil {
		skipped = normalizeVersion(*pkg.SkippedVersion)
	}
	return selectLibsVersion(normalizeVersion(*pkg.LatestVersion), skipped, deployed, cacheVerified), nil
}

func (x *XXMI) ensureProviderDLL(ctx context.Context, spec libsProviderSpec, version string) error {
	x.packageMu.Lock()
	defer x.packageMu.Unlock()
	return x.ensureProviderDLLLocked(ctx, spec, version, false)
}

// redownloadProviderRelease replaces the cached provider release an importer deploys with a fresh download and
// returns its manifest entry, or "" for an importer that deploys none. The cached files are only ever held to
// the digests they were downloaded with, so a release published again under its version is not fetched
// otherwise. A download that fails leaves the cached entry as it was.
func (x *XXMI) redownloadProviderRelease(ctx context.Context, cfg ImporterConfig) (string, error) {
	spec, err := x.deployedLibsProvider(ctx, cfg)
	if err != nil || spec.overlayPackage == "" {
		return "", err
	}
	pin, _, err := x.libsPin(ctx, cfg)
	if err != nil {
		return "", err
	}
	version, err := x.resolveProviderDLLVersion(ctx, spec, pin, cfg.ImporterFolder)
	if err != nil {
		return "", err
	}

	x.packageMu.Lock()
	defer x.packageMu.Unlock()
	return spec.overlayPackage + "@" + version, x.ensureProviderDLLLocked(ctx, spec, version, true)
}

// ensureProviderDLLLocked downloads a provider d3d11.dll into the package cache unless a verified copy is
// already there. redownload fetches the release again even then, with its metadata read from GitHub instead of
// the cached listing. The caller must hold packageMu.
func (x *XXMI) ensureProviderDLLLocked(
	ctx context.Context,
	spec libsProviderSpec,
	version string,
	redownload bool,
) error {
	ctx = infra.WithGitHubOperation(ctx, "xxmi-install-provider-dll")
	if redownload {
		ctx = infra.WithGitHubRefresh(ctx, true)
	}
	version = normalizeVersion(strings.TrimSpace(version))
	if version == "" || version == "." || version == ".." || strings.ContainsAny(version, `\/:*?"<>|`) {
		return fmt.Errorf("invalid %s version", spec.overlayPackage)
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return err
	}
	parent := filepath.Join(root, "packages", spec.overlayPackage)
	destination := filepath.Join(parent, version)
	replace := false
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s cache is not a regular directory", spec.overlayPackage)
		}
		replace = true
		source, _, err := x.readVerifiedProviderDLL(ctx, root, spec, version)
		if err == nil && !redownload {
			stale := x.staleProviderFiles(ctx, root, spec, version, source)
			if len(stale) == 0 {
				return nil
			}

			err := x.refreshProviderFiles(ctx, spec, version, destination, source, stale)
			if err == nil || infra.IsCancellationError(err) {
				return err
			}

			// The DLL may not work without its companion libraries, and an entry that was never searched for
			// them reads as a release that ships none.
			if slices.ContainsFunc(stale, func(name string) bool { return name != providerININame }) {
				return fmt.Errorf("refresh %s %s: %w", spec.overlayPackage, version, err)
			}

			// The verified DLL still launches, so a refresh that could not run only leaves the options of
			// its d3dx.ini out until a later launch reaches it.
			_ = infra.ReportError(x.log, err, "XXMI.ensureProviderDLL", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "ensure-libs-provider", Stage: "refresh",
				Fields: map[string]any{"package": spec.overlayPackage, "version": version},
			})
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return x.downloadProviderRelease(ctx, spec, version, parent, replace)
}

// downloadProviderRelease downloads and verifies the files of a provider release and moves them into the
// package cache. replace drops the entry already there, which is only a copy of the same release. The caller
// must hold packageMu.
func (x *XXMI) downloadProviderRelease(
	ctx context.Context,
	spec libsProviderSpec,
	version, parent string,
	replace bool,
) error {
	destination := filepath.Join(parent, version)
	release, err := x.findProviderRelease(ctx, spec, version)
	if err != nil {
		return err
	}
	isDLL := func(asset github.Asset) bool {
		return asset.Name == customDLLName && asset.BrowserDownloadURL != ""
	}
	assetIndex := slices.IndexFunc(release.Assets, isDLL)

	// GitHub lists a release before its assets finish uploading, and a listing fetched in that window stays
	// cached for an hour.
	if assetIndex < 0 {
		release, err = x.findProviderRelease(infra.WithGitHubRefresh(ctx, true), spec, version)
		if err != nil {
			return err
		}
		assetIndex = slices.IndexFunc(release.Assets, isDLL)
	}
	if assetIndex < 0 {
		return fmt.Errorf("%s release %s has no %s", spec.overlayPackage, version, customDLLName)
	}
	asset := release.Assets[assetIndex]

	// The published size keeps an oversized asset from being downloaded at all. Metadata without one, or
	// with a wrong one, is still held to the limit while the file streams.
	if asset.Size > customDLLSizeLimit {
		return fmt.Errorf("%s %s exceeds size limit", spec.overlayPackage, version)
	}

	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, version+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	dllPath := filepath.Join(staging, customDLLName)
	if err := x.downloadPackageFile(
		ctx,
		spec.overlayPackage, version,
		github.FileRequest{
			Repo: spec.repo, URL: asset.BrowserDownloadURL, Destination: dllPath, MaxSize: customDLLSizeLimit,
		},
	); err != nil {
		return fmt.Errorf("download %s %s: %w", spec.overlayPackage, version, err)
	}
	info, err := os.Stat(dllPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > customDLLSizeLimit {
		return fmt.Errorf("%s %s is not a regular file or exceeds size limit", spec.overlayPackage, version)
	}
	data, err := os.ReadFile(dllPath)
	if err != nil {
		return err
	}

	// The release is unsigned, so a file GitHub published no digest for cannot be checked at all.
	verified, err := verifyReleaseDigest(*release, customDLLName, data)
	if err != nil {
		return fmt.Errorf("%s %s: %w", spec.overlayPackage, version, err)
	}
	if !verified {
		return fmt.Errorf("%s release %s has no asset digest", spec.overlayPackage, version)
	}
	if err := validateCustomDLLImage(data); err != nil {
		return fmt.Errorf("%s %s: %w", spec.overlayPackage, version, err)
	}
	iniHash, err := x.downloadProviderINI(ctx, spec, version, *release, staging)
	if err != nil {
		return err
	}
	companionHashes, err := x.downloadProviderCompanions(
		ctx,
		spec,
		version,
		*release,
		staging,
		spec.companions,
	)
	if err != nil {
		return err
	}
	metadata, err := json.MarshalIndent(providerDLLSource{
		Version: version, Tag: release.TagName, SHA256: hashBytes(data), Size: int64(len(data)),
		FetchedAt: time.Now().UTC().Format(time.RFC3339), Searched: spec.releaseFiles(),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, providerDLLMetadata), metadata, 0o600); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// The entry is moved aside instead of deleted. A folder with a file some process holds open refuses the
	// move as a whole, where deleting it would take the DLL and leave the rest. The name is not a version,
	// so a leftover is never read as a cached release.
	replaced := destination + "~replaced"
	if replace {
		if err := os.RemoveAll(replaced); err != nil {
			return err
		}
		if err := os.Rename(destination, replaced); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, destination); err != nil {
		if replace {
			err = errors.Join(err, os.Rename(replaced, destination))
		}
		return err
	}
	if replace {
		x.reportCleanup(os.RemoveAll(replaced), "replace-provider-release")
	}
	return x.recordProviderHashes(ctx, spec, version, hashBytes(data), iniHash, companionHashes)
}

func (x *XXMI) findProviderRelease(
	ctx context.Context,
	spec libsProviderSpec,
	version string,
) (*github.Release, error) {
	releases, err := x.github.AllReleases(ctx, spec.repo)
	if err != nil {
		return nil, err
	}
	for i := range releases {
		if normalizeVersion(releases[i].TagName) == version && !releases[i].Draft {
			return &releases[i], nil
		}
	}
	return nil, fmt.Errorf("%s release %s not found", spec.overlayPackage, version)
}

// downloadProviderINI downloads the d3dx.ini of a provider release into staging and returns its hash, or ""
// when the release ships none. Its options decide what the DLL loads, so it is held to its published digest
// like the DLL, and one that could not be checked is not downloaded at all.
func (x *XXMI) downloadProviderINI(
	ctx context.Context,
	spec libsProviderSpec,
	version string,
	release github.Release,
	staging string,
) (string, error) {
	index := slices.IndexFunc(release.Assets, func(asset github.Asset) bool {
		return asset.Name == providerININame && asset.BrowserDownloadURL != ""
	})
	if index < 0 {
		return "", nil
	}
	asset := release.Assets[index]
	if asset.Size > providerINISizeLimit {
		return "", fmt.Errorf("%s %s %s exceeds size limit", spec.overlayPackage, version, providerININame)
	}
	if asset.Digest == "" {
		return "", fmt.Errorf(
			"%s release %s has no asset digest for %s", spec.overlayPackage, version, providerININame,
		)
	}

	path := filepath.Join(staging, providerININame)
	if err := x.github.DownloadFile(ctx, github.FileRequest{
		Repo: spec.repo, URL: asset.BrowserDownloadURL, Destination: path, MaxSize: providerINISizeLimit,
	}); err != nil {
		return "", fmt.Errorf("download %s %s %s: %w", spec.overlayPackage, version, providerININame, err)
	}
	ini, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	verified, err := verifyReleaseDigest(release, providerININame, ini)
	if err == nil && !verified {
		err = errors.New("no asset digest")
	}
	if err != nil {
		return "", fmt.Errorf("%s %s %s: %w", spec.overlayPackage, version, providerININame, err)
	}
	return hashBytes(ini), nil
}

// downloadProviderCompanions downloads the named companion libraries of a provider release into staging and
// returns their hashes by file name; a release ships any of them or none. The game loads them like the DLL, so each is
// held to its published digest, and one that could not be checked is not downloaded at all.
func (x *XXMI) downloadProviderCompanions(
	ctx context.Context,
	spec libsProviderSpec,
	version string,
	release github.Release,
	staging string,
	names []string,
) (map[string]string, error) {
	hashes := map[string]string{}
	for _, name := range names {
		index := slices.IndexFunc(release.Assets, func(asset github.Asset) bool {
			return asset.Name == name && asset.BrowserDownloadURL != ""
		})
		if index < 0 {
			continue
		}
		asset := release.Assets[index]
		if asset.Size > customDLLSizeLimit {
			return nil, fmt.Errorf("%s %s %s exceeds size limit", spec.overlayPackage, version, name)
		}
		if asset.Digest == "" {
			return nil, fmt.Errorf("%s release %s has no asset digest for %s", spec.overlayPackage, version, name)
		}

		path := filepath.Join(staging, name)
		if err := x.downloadPackageFile(
			ctx,
			spec.overlayPackage, version,
			github.FileRequest{
				Repo: spec.repo, URL: asset.BrowserDownloadURL, Destination: path, MaxSize: customDLLSizeLimit,
			},
		); err != nil {
			return nil, fmt.Errorf("download %s %s %s: %w", spec.overlayPackage, version, name, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		verified, err := verifyReleaseDigest(release, name, data)
		if err == nil && !verified {
			err = errors.New("no asset digest")
		}
		if err == nil {
			err = validateCustomDLLImage(data)
		}
		if err != nil {
			return nil, fmt.Errorf("%s %s %s: %w", spec.overlayPackage, version, name, err)
		}
		hashes[name] = hashBytes(data)
	}
	return hashes, nil
}

// staleProviderFiles names the files beside a cached provider d3d11.dll that have to be fetched again: the ones
// the release was never searched for, and the ones whose cached copy is no longer the verified download.
func (x *XXMI) staleProviderFiles(
	ctx context.Context,
	root string,
	spec libsProviderSpec,
	version string,
	source providerDLLSource,
) []string {
	var stale []string
	for _, name := range spec.releaseFiles() {
		if !slices.Contains(source.Searched, name) {
			stale = append(stale, name)
			continue
		}
		if _, err := x.readVerifiedProviderFile(
			ctx, root, spec, version, name, providerFileSizeLimit(name),
		); err != nil {
			stale = append(stale, name)
		}
	}
	return stale
}

// refreshProviderFiles brings the stale files of a cache entry in line with its release and leaves the verified
// DLL and the other files in place. A d3dx.ini that cannot be fetched is reported and does not fail the refresh.
// The metadata is written last, so an entry that was only partly refreshed is refreshed again. The caller must
// hold packageMu.
func (x *XXMI) refreshProviderFiles(
	ctx context.Context,
	spec libsProviderSpec,
	version, destination string,
	source providerDLLSource,
	stale []string,
) error {
	release, err := x.findProviderRelease(ctx, spec, version)
	if err != nil {
		return err
	}
	recorded, err := x.providerDLLHashes(ctx)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(destination), version+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	// A file this refresh does not look for keeps its cached copy and its record.
	iniHash := recorded[providerFileHashKey(spec, version, providerININame)]
	companionHashes := map[string]string{}
	for _, name := range spec.companions {
		companionHashes[name] = recorded[providerFileHashKey(spec, version, name)]
	}

	// fetched tells whether the release ships each file this refresh looked for.
	fetched := map[string]bool{}
	staleCompanions := slices.DeleteFunc(slices.Clone(stale), func(name string) bool {
		return name == providerININame
	})
	downloaded, err := x.downloadProviderCompanions(ctx, spec, version, *release, staging, staleCompanions)
	if err != nil {
		return err
	}
	for _, name := range staleCompanions {
		companionHashes[name] = downloaded[name]
		fetched[name] = downloaded[name] != ""
	}
	searched := append(slices.Clone(source.Searched), staleCompanions...)

	// A launch works without the d3dx.ini, so one that cannot be fetched does not hold back the companion
	// libraries. The cached file and its record stay as they are, and the entry counts as never searched for
	// it so that the next refresh looks again.
	if slices.Contains(stale, providerININame) {
		hash, iniErr := x.downloadProviderINI(ctx, spec, version, *release, staging)
		switch {
		case iniErr == nil:
			iniHash, fetched[providerININame] = hash, hash != ""
			searched = append(searched, providerININame)
		case infra.IsCancellationError(iniErr):
			return iniErr
		default:
			_ = infra.ReportError(x.log, iniErr, "XXMI.ensureProviderDLL", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "ensure-libs-provider", Stage: "refresh-ini",
				Fields: map[string]any{"package": spec.overlayPackage, "version": version},
			})
			searched = slices.DeleteFunc(searched, func(name string) bool { return name == providerININame })
		}
	}
	slices.Sort(searched)
	source.Searched = slices.Compact(searched)
	metadata, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, providerDLLMetadata), metadata, 0o600); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// The record goes first: it is the step a cancellation can still fail, and failing it here leaves the
	// cached file and its old record in agreement.
	if err := x.recordProviderHashes(ctx, spec, version, source.SHA256, iniHash, companionHashes); err != nil {
		return err
	}
	for name, present := range fetched {
		cached := filepath.Join(destination, name)
		if !present {
			if err := os.Remove(cached); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else if err := platform.ReplaceAtomic(filepath.Join(staging, name), cached); err != nil {
			return err
		}
	}
	return platform.ReplaceAtomic(
		filepath.Join(staging, providerDLLMetadata), filepath.Join(destination, providerDLLMetadata),
	)
}

func providerFileHashKey(spec libsProviderSpec, version, name string) string {
	return spec.overlayPackage + "@" + version + "/" + name
}

// providerDLLHashes returns the hashes of the provider files this installation checked against their published
// digest, keyed "<overlay package>@<version>" for the DLL and providerFileHashKey for the files beside it. They are
// stored outside the package cache, so a cache entry whose DLL and metadata were rewritten together does not
// pass for the verified download.
func (x *XXMI) providerDLLHashes(ctx context.Context) (map[string]string, error) {
	client, err := x.settingsClient()
	if err != nil {
		return nil, err
	}
	stored, err := client.Settings.GetValue(ctx, providerDLLHashesKey)
	if err != nil {
		return nil, err
	}
	hashes := map[string]string{}
	if stored != nil {
		// An unreadable record verifies nothing, and the entries it covered are downloaded again.
		_ = json.Unmarshal([]byte(*stored), &hashes)
	}
	// A stored JSON null decodes into a nil map, which recordProviderHashes could not add to.
	if hashes == nil {
		hashes = map[string]string{}
	}
	return hashes, nil
}

// recordProviderHashes remembers the downloaded and verified files of a provider release. An empty ini or a
// companion library without a hash drops the record of a file the release no longer ships. The caller must
// hold packageMu.
func (x *XXMI) recordProviderHashes(
	ctx context.Context,
	spec libsProviderSpec,
	version, dll, ini string,
	companions map[string]string,
) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	hashes, err := x.providerDLLHashes(ctx)
	if err != nil {
		return err
	}
	hashes[spec.overlayPackage+"@"+version] = dll
	files := map[string]string{providerININame: ini}
	for _, name := range spec.companions {
		files[name] = companions[name]
	}
	for name, hash := range files {
		if hash == "" {
			delete(hashes, providerFileHashKey(spec, version, name))
		} else {
			hashes[providerFileHashKey(spec, version, name)] = hash
		}
	}
	encoded, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	value := string(encoded)
	return client.Settings.Upsert(ctx, providerDLLHashesKey, &value)
}

// readVerifiedProviderDLL returns a cached provider d3d11.dll that is still the file this installation
// downloaded and verified.
func (x *XXMI) readVerifiedProviderDLL(
	ctx context.Context,
	root string,
	spec libsProviderSpec,
	version string,
) (providerDLLSource, []byte, error) {
	source, data, err := readProviderDLL(root, spec, version)
	if err != nil {
		return providerDLLSource{}, nil, err
	}
	hashes, err := x.providerDLLHashes(ctx)
	if err != nil {
		return providerDLLSource{}, nil, err
	}
	if hashes[spec.overlayPackage+"@"+version] != source.SHA256 {
		return providerDLLSource{}, nil, fmt.Errorf(
			"%s %s is not the verified download", spec.overlayPackage, version,
		)
	}
	return source, data, nil
}

// verifiedProviderINI returns the d3dx.ini cached with a deployed provider d3d11.dll, named by the runtime
// manifest as "<overlay package>@<version>". It is nil when the release ships none.
func (x *XXMI) verifiedProviderINI(ctx context.Context, root, source string) ([]byte, error) {
	spec, version, err := parseProviderSource(source)
	if err != nil {
		return nil, err
	}
	return x.readVerifiedProviderINI(ctx, root, spec, version)
}

// readVerifiedProviderINI returns the cached d3dx.ini of a provider release, or nil when no d3dx.ini was
// downloaded for it.
func (x *XXMI) readVerifiedProviderINI(
	ctx context.Context,
	root string,
	spec libsProviderSpec,
	version string,
) ([]byte, error) {
	return x.readVerifiedProviderFile(ctx, root, spec, version, providerININame, providerINISizeLimit)
}

// readVerifiedProviderCompanions returns the cached companion libraries of a provider release by file name,
// each still the file this installation downloaded and verified.
func (x *XXMI) readVerifiedProviderCompanions(
	ctx context.Context,
	root string,
	spec libsProviderSpec,
	version string,
) (map[string][]byte, error) {
	companions := map[string][]byte{}
	for _, name := range spec.companions {
		data, err := x.readVerifiedProviderFile(ctx, root, spec, version, name, customDLLSizeLimit)
		if err != nil {
			return nil, err
		}
		if data != nil {
			companions[name] = data
		}
	}
	return companions, nil
}

// readVerifiedProviderFile returns a file cached beside a provider d3d11.dll, or nil when none was downloaded
// for the release. A recorded file that is missing is an error like one that was rewritten.
func (x *XXMI) readVerifiedProviderFile(
	ctx context.Context,
	root string,
	spec libsProviderSpec,
	version, name string,
	sizeLimit int64,
) ([]byte, error) {
	hashes, err := x.providerDLLHashes(ctx)
	if err != nil {
		return nil, err
	}
	want, recorded := hashes[providerFileHashKey(spec, version, name)]
	path := filepath.Join(root, "packages", spec.overlayPackage, version, name)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) && !recorded {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > sizeLimit {
		return nil, fmt.Errorf("%s is not a regular file or exceeds size limit", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !recorded || want != hashBytes(data) {
		return nil, fmt.Errorf("%s is not the verified download", path)
	}
	return data, nil
}

func parseProviderSource(source string) (libsProviderSpec, string, error) {
	pkg, version, ok := strings.Cut(source, "@")
	spec, known := lookupOverlayPackage(pkg)
	if !ok || !known || version == "" || version == "." || version == ".." ||
		strings.ContainsAny(version, `\/:*?"<>|`) {
		return libsProviderSpec{}, "", fmt.Errorf("invalid libraries provider source %q", source)
	}
	return spec, version, nil
}

// readProviderDLL returns a cached provider d3d11.dll after checking its bytes against its own metadata. That
// alone only finds damage; readVerifiedProviderDLL ties the entry to the verified download.
func readProviderDLL(root string, spec libsProviderSpec, version string) (providerDLLSource, []byte, error) {
	folder := filepath.Join(root, "packages", spec.overlayPackage, version)
	metadata, err := os.ReadFile(filepath.Join(folder, providerDLLMetadata))
	if err != nil {
		return providerDLLSource{}, nil, err
	}
	// Builds before Searched recorded a search as one flag per kind of file. The companion flag stands for the
	// two libraries those builds knew of, not for the ones added since.
	var stored struct {
		providerDLLSource
		INIChecked        bool `json:"iniChecked"`
		CompanionsChecked bool `json:"companionsChecked"`
	}
	if err := json.Unmarshal(metadata, &stored); err != nil {
		return providerDLLSource{}, nil, fmt.Errorf("decode %s metadata: %w", spec.overlayPackage, err)
	}
	source := stored.providerDLLSource
	var flagged []string
	if stored.INIChecked {
		flagged = append(flagged, providerININame)
	}
	if stored.CompanionsChecked {
		flagged = append(flagged, "dxcompiler.dll", "dxil.dll")
	}
	for _, name := range flagged {
		if !slices.Contains(source.Searched, name) {
			source.Searched = append(source.Searched, name)
		}
	}
	if source.Version != version || source.SHA256 == "" {
		return providerDLLSource{}, nil, fmt.Errorf("%s metadata mismatch", spec.overlayPackage)
	}
	data, err := os.ReadFile(filepath.Join(folder, customDLLName))
	if err != nil {
		return providerDLLSource{}, nil, err
	}
	if hashBytes(data) != source.SHA256 {
		return providerDLLSource{}, nil, fmt.Errorf("%s hash mismatch", spec.overlayPackage)
	}
	return source, data, nil
}

func (x *XXMI) verifiedProviderDLL(ctx context.Context, spec libsProviderSpec, version string) bool {
	root, err := xxmiCacheRoot()
	if err != nil {
		return false
	}
	_, _, err = x.readVerifiedProviderDLL(ctx, root, spec, version)
	return err == nil
}

// verifiedProviderDLLFolder returns the cache folder of a deployed provider d3d11.dll, named by the runtime
// manifest as "<overlay package>@<version>", and the names of the companion libraries cached in it, after
// verifying the cached files.
func (x *XXMI) verifiedProviderDLLFolder(ctx context.Context, root, source string) (string, []string, error) {
	spec, version, err := parseProviderSource(source)
	if err != nil {
		return "", nil, err
	}
	if _, _, err := x.readVerifiedProviderDLL(ctx, root, spec, version); err != nil {
		return "", nil, err
	}
	companions, err := x.readVerifiedProviderCompanions(ctx, root, spec, version)
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(root, "packages", spec.overlayPackage, version), slices.Collect(maps.Keys(companions)), nil
}

// deployedProviderVersion returns the provider d3d11.dll version the last deployment wrote, or "".
func deployedProviderVersion(folder string, spec libsProviderSpec) string {
	if folder == "" || spec.overlayPackage == "" {
		return ""
	}
	manifest, ok := readXXMIRuntimeManifest(folder)
	if !ok {
		return ""
	}
	version, _ := strings.CutPrefix(manifest.Provider, spec.overlayPackage+"@")
	if version == manifest.Provider {
		return ""
	}
	return version
}

// providerDLLBaseVersion returns the provider release an importer already has: the deployed one, or for an
// importer that has not deployed this provider yet the newest cached one. A skipped or not yet downloaded
// update is then held back for a first deployment as well.
func providerDLLBaseVersion(folder string, spec libsProviderSpec) string {
	if deployed := deployedProviderVersion(folder, spec); deployed != "" {
		return deployed
	}
	return newestCachedPackageVersion(spec.overlayPackage)
}
