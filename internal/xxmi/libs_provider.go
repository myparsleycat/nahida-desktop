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

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

const (
	defaultLibsProvider   = "spectrumqt"
	sharedLibsProviderKey = "xxmi_libs_provider"
	providerDLLHashesKey  = "xxmi_libs_provider_hashes"
	providerDLLMetadata   = "source.json"
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
}

var libsProviders = []libsProviderSpec{
	{id: defaultLibsProvider, repo: libsRepo},
	{
		id:             "myparsleycat",
		repo:           github.Repo{Owner: "myparsleycat", Name: "XXMI-Libs-Package-Forked"},
		overlayPackage: "xxmi-libs-myparsleycat",
		versionMark:    "-nhd.",
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
	data, err := x.readVerifiedProviderDLL(ctx, root, spec, version)
	if err != nil {
		return nil, fmt.Errorf("XXMI_RUNTIME_CORRUPTED: %w", err)
	}
	return &providerRuntimeDLL{source: spec.overlayPackage + "@" + version, data: data}, nil
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
	damaged := false
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s cache is not a regular directory", spec.overlayPackage)
		}
		if _, err := x.readVerifiedProviderDLL(ctx, root, spec, version); err == nil {
			return nil
		}
		damaged = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	releases, err := x.github.AllReleases(ctx, spec.repo)
	if err != nil {
		return err
	}
	var release *github.Release
	for i := range releases {
		if normalizeVersion(releases[i].TagName) == version && !releases[i].Draft {
			release = &releases[i]
			break
		}
	}
	if release == nil {
		return fmt.Errorf("%s release %s not found", spec.overlayPackage, version)
	}
	assetIndex := slices.IndexFunc(release.Assets, func(asset github.Asset) bool {
		return asset.Name == customDLLName && asset.BrowserDownloadURL != ""
	})
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
	metadata, err := json.MarshalIndent(providerDLLSource{
		Version: version, Tag: release.TagName, SHA256: hashBytes(data), Size: int64(len(data)),
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
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

	// A damaged entry is only a copy of the release that was just downloaded and verified again.
	if damaged {
		if err := os.RemoveAll(destination); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, destination); err != nil {
		return err
	}
	return x.recordProviderDLLHash(ctx, spec, version, hashBytes(data))
}

// providerDLLHashes returns the hashes of the provider DLLs this installation checked against their published
// digest, keyed "<overlay package>@<version>". They are stored outside the package cache, so a cache entry
// whose DLL and metadata were rewritten together does not pass for the verified download.
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
	// A stored JSON null decodes into a nil map, which recordProviderDLLHash could not add to.
	if hashes == nil {
		hashes = map[string]string{}
	}
	return hashes, nil
}

// recordProviderDLLHash remembers a downloaded and verified provider DLL. The caller must hold packageMu.
func (x *XXMI) recordProviderDLLHash(ctx context.Context, spec libsProviderSpec, version, hash string) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	hashes, err := x.providerDLLHashes(ctx)
	if err != nil {
		return err
	}
	hashes[spec.overlayPackage+"@"+version] = hash
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
) ([]byte, error) {
	source, data, err := readProviderDLL(root, spec, version)
	if err != nil {
		return nil, err
	}
	hashes, err := x.providerDLLHashes(ctx)
	if err != nil {
		return nil, err
	}
	if hashes[spec.overlayPackage+"@"+version] != source.SHA256 {
		return nil, fmt.Errorf("%s %s is not the verified download", spec.overlayPackage, version)
	}
	return data, nil
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
	_, err = x.readVerifiedProviderDLL(ctx, root, spec, version)
	return err == nil
}

// verifiedProviderDLLFolder returns the cache folder of a deployed provider d3d11.dll, named by the runtime
// manifest as "<overlay package>@<version>", after verifying the cached file.
func (x *XXMI) verifiedProviderDLLFolder(ctx context.Context, root, source string) (string, error) {
	pkg, version, ok := strings.Cut(source, "@")
	spec, known := lookupOverlayPackage(pkg)
	if !ok || !known || version == "" || version == "." || version == ".." ||
		strings.ContainsAny(version, `\/:*?"<>|`) {
		return "", fmt.Errorf("invalid libraries provider source %q", source)
	}
	if _, err := x.readVerifiedProviderDLL(ctx, root, spec, version); err != nil {
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
