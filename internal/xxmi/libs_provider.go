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

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	defaultLibsProvider   = "spectrumqt"
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
	// smoothMotionSince is the first release whose d3d11.dll renders alongside NVIDIA Smooth Motion, or ""
	// when Smooth Motion has to be off for mods to load.
	smoothMotionSince string
}

var libsProviders = []libsProviderSpec{
	{id: defaultLibsProvider, repo: libsRepo},
	{
		id:                "myparsleycat",
		repo:              github.Repo{Owner: "myparsleycat", Name: "XXMI-Libs-Package-Forked"},
		overlayPackage:    "xxmi-libs-myparsleycat",
		versionMark:       "-nhd.",
		smoothMotionSince: "1.2.2-nhd.3",
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
	// INIChecked marks an entry whose release was searched for a d3dx.ini. An entry without it was cached
	// before the app looked for one, so its release may ship one that was never downloaded.
	INIChecked bool `json:"iniChecked,omitempty"`
}

// providerRuntimeDLL is the provider d3d11.dll a deployment writes instead of the signed one.
type providerRuntimeDLL struct {
	// source is the manifest entry, "<overlay package>@<version>".
	source string
	data   []byte
}

func lookupLibsProvider(id string) (libsProviderSpec, bool) {
	for _, spec := range libsProviders {
		if spec.id == id {
			return spec, true
		}
	}
	return libsProviderSpec{}, false
}

// libsProviderIDs lists the selectable providers, the default one first.
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
	fallback, _ := lookupLibsProvider(defaultLibsProvider)
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
		fallback, _ := lookupLibsProvider(defaultLibsProvider)
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
	return &providerRuntimeDLL{source: spec.overlayPackage + "@" + version, data: data}, nil
}

// smoothMotionSupport is how the d3d11.dll an importer's next deployment writes gets along with NVIDIA Smooth
// Motion.
type smoothMotionSupport struct {
	supported bool
	// provider, pin and version name that d3d11.dll as far as they were resolved before the verdict.
	provider, pin, version string
	// since is the provider's first release that renders alongside Smooth Motion, or "".
	since string
	// reason is why the DLL counts as unsupported, and err the failure behind it, if any.
	reason string
	err    error
}

const (
	smoothMotionCustomDLL           = "custom-dll"
	smoothMotionProviderUnresolved  = "provider-unresolved"
	smoothMotionProviderUnsupported = "provider-unsupported"
	smoothMotionVersionUnresolved   = "version-unresolved"
	smoothMotionVersionOutdated     = "version-outdated"
)

// smoothMotionSupport reports whether the d3d11.dll the importer's next deployment writes renders alongside
// NVIDIA Smooth Motion. A release that cannot be resolved counts as unsupported; the deployment that follows
// reports why.
func (x *XXMI) smoothMotionSupport(ctx context.Context, cfg ImporterConfig) smoothMotionSupport {
	custom, err := x.launchesCustomDLL(ctx, cfg)
	if err != nil {
		return smoothMotionSupport{reason: smoothMotionProviderUnresolved, err: err}
	}
	if custom {
		return smoothMotionSupport{reason: smoothMotionCustomDLL}
	}
	spec, err := x.libsProvider(ctx, cfg)
	support := smoothMotionSupport{provider: spec.id, since: spec.smoothMotionSince}
	if err != nil {
		support.reason, support.err = smoothMotionProviderUnresolved, err
		return support
	}
	if spec.smoothMotionSince == "" {
		support.reason = smoothMotionProviderUnsupported
		return support
	}

	support.reason = smoothMotionVersionUnresolved
	if support.pin, _, support.err = x.libsPin(ctx, cfg); support.err != nil {
		return support
	}
	support.version, support.err = x.resolveProviderDLLVersion(ctx, spec, support.pin, cfg.ImporterFolder)
	if support.err != nil || !semver.IsValid("v"+support.version) {
		return support
	}
	support.supported = semver.Compare("v"+support.version, "v"+spec.smoothMotionSince) >= 0
	support.reason = smoothMotionVersionOutdated
	if support.supported {
		support.reason = ""
	}
	return support
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
	return x.ensureProviderDLLLocked(ctx, spec, version)
}

// ensureProviderDLLLocked downloads a provider d3d11.dll into the package cache unless a verified copy is
// already there. The caller must hold packageMu.
func (x *XXMI) ensureProviderDLLLocked(ctx context.Context, spec libsProviderSpec, version string) error {
	ctx = infra.WithGitHubOperation(ctx, "xxmi-install-provider-dll")
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
		if source, _, err := x.readVerifiedProviderDLL(ctx, root, spec, version); err == nil {
			if source.INIChecked {
				if _, err := x.readVerifiedProviderINI(ctx, root, spec, version); err == nil {
					return nil
				}
			}

			// The verified DLL still launches, so a d3dx.ini that cannot be fetched only leaves its options
			// out until a later launch reaches it.
			err := x.refreshProviderINI(ctx, spec, version, destination, source)
			if err == nil || infra.IsCancellationError(err) {
				return err
			}
			_ = infra.ReportError(x.log, err, "XXMI.ensureProviderDLL", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "ensure-libs-provider", Stage: "refresh-ini",
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
	metadata, err := json.MarshalIndent(providerDLLSource{
		Version: version, Tag: release.TagName, SHA256: hashBytes(data), Size: int64(len(data)),
		FetchedAt: time.Now().UTC().Format(time.RFC3339), INIChecked: true,
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
	return x.recordProviderHashes(ctx, spec, version, hashBytes(data), iniHash)
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

// refreshProviderINI brings the d3dx.ini of a cache entry in line with its release and leaves the verified DLL
// in place. The metadata is written last, so an entry that was only partly refreshed is refreshed again. The
// caller must hold packageMu.
func (x *XXMI) refreshProviderINI(
	ctx context.Context,
	spec libsProviderSpec,
	version, destination string,
	source providerDLLSource,
) error {
	release, err := x.findProviderRelease(ctx, spec, version)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(destination), version+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	iniHash, err := x.downloadProviderINI(ctx, spec, version, *release, staging)
	if err != nil {
		return err
	}
	source.INIChecked = true
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
	if err := x.recordProviderHashes(ctx, spec, version, source.SHA256, iniHash); err != nil {
		return err
	}
	cached := filepath.Join(destination, providerININame)
	if iniHash == "" {
		if err := os.Remove(cached); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := platform.ReplaceAtomic(filepath.Join(staging, providerININame), cached); err != nil {
		return err
	}
	return platform.ReplaceAtomic(
		filepath.Join(staging, providerDLLMetadata), filepath.Join(destination, providerDLLMetadata),
	)
}

func providerINIHashKey(spec libsProviderSpec, version string) string {
	return spec.overlayPackage + "@" + version + "/" + providerININame
}

// providerDLLHashes returns the hashes of the provider files this installation checked against their published
// digest, keyed "<overlay package>@<version>" for the DLL and providerINIHashKey for the d3dx.ini. They are
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

// recordProviderHashes remembers the downloaded and verified files of a provider release. An empty ini drops
// the record of a d3dx.ini the release no longer ships. The caller must hold packageMu.
func (x *XXMI) recordProviderHashes(
	ctx context.Context,
	spec libsProviderSpec,
	version, dll, ini string,
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
	if ini == "" {
		delete(hashes, providerINIHashKey(spec, version))
	} else {
		hashes[providerINIHashKey(spec, version)] = ini
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
// downloaded for it. A recorded file that is missing is an error like one that was rewritten.
func (x *XXMI) readVerifiedProviderINI(
	ctx context.Context,
	root string,
	spec libsProviderSpec,
	version string,
) ([]byte, error) {
	hashes, err := x.providerDLLHashes(ctx)
	if err != nil {
		return nil, err
	}
	want, recorded := hashes[providerINIHashKey(spec, version)]
	path := filepath.Join(root, "packages", spec.overlayPackage, version, providerININame)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) && !recorded {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > providerINISizeLimit {
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
	var source providerDLLSource
	if err := json.Unmarshal(metadata, &source); err != nil {
		return providerDLLSource{}, nil, fmt.Errorf("decode %s metadata: %w", spec.overlayPackage, err)
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
// manifest as "<overlay package>@<version>", after verifying the cached file.
func (x *XXMI) verifiedProviderDLLFolder(ctx context.Context, root, source string) (string, error) {
	spec, version, err := parseProviderSource(source)
	if err != nil {
		return "", err
	}
	if _, _, err := x.readVerifiedProviderDLL(ctx, root, spec, version); err != nil {
		return "", err
	}
	return filepath.Join(root, "packages", spec.overlayPackage, version), nil
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
