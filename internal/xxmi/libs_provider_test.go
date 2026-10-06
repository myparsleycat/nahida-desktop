package xxmi

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

const providerTestPackage = "xxmi-libs-myparsleycat"

// providerTestRelease is a fork release as the fake GitHub serves it.
type providerTestRelease struct {
	tag  string
	data []byte
	// digest is the published asset digest; empty publishes none.
	digest  string
	noAsset bool
	// size is the published asset size; zero publishes the real one.
	size int64
	// started is closed when the asset download begins, which then waits for proceed to close.
	started, proceed chan struct{}
}

func providerTestDigest(data []byte) string {
	return "sha256:" + hashBytes(data)
}

func providerTestAssetURL(tag string) string {
	return "https://github.com/myparsleycat/XXMI-Libs-Package-Forked/releases/download/" + tag + "/d3d11.dll"
}

// newProviderTestService returns a built-in service whose GitHub serves releases for the fork and no
// releases for every other repository. downloads counts fetched fork assets.
func newProviderTestService(
	t *testing.T,
	releases []providerTestRelease,
) (service *XXMI, client *db.Client, downloads *atomic.Int32) {
	t.Helper()
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}

	listed := make([]map[string]any, 0, len(releases))
	assets := map[string]providerTestRelease{}
	for _, release := range releases {
		entry := map[string]any{"tag_name": release.tag, "assets": []map[string]any{}}
		if !release.noAsset {
			entry["assets"] = []map[string]any{{
				"name": "d3d11.dll", "browser_download_url": providerTestAssetURL(release.tag),
				"digest": release.digest, "size": cmp.Or(release.size, int64(len(release.data))),
			}}
			assets[providerTestAssetURL(release.tag)] = release
		}
		listed = append(listed, entry)
	}
	releaseList, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	downloads = &atomic.Int32{}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, []byte(`[]`)
		switch {
		case request.URL.Host == "api.github.com" &&
			strings.Contains(request.URL.Path, "/myparsleycat/XXMI-Libs-Package-Forked/"):
			body = releaseList
		case request.URL.Host == "api.github.com":
		default:
			asset, ok := assets[request.URL.String()]
			if !ok {
				t.Errorf("unexpected request %s", request.URL)
				status = http.StatusNotFound
			}
			downloads.Add(1)
			if asset.started != nil {
				close(asset.started)
				<-asset.proceed
			}
			body = asset.data
		}
		return &http.Response{
			StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(body)), Request: request,
		}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(infraClient)
	service = NewWithOptions(Options{HTTP: infraClient, Download: download, Archive: infra.NewArchive()})
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	return service, client, downloads
}

func providerTestCache(t *testing.T, version string) string {
	t.Helper()
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "packages", providerTestPackage, version)
}

func TestSetSharedLibsProviderCachesLatestVerifiedDLL(t *testing.T) {
	ctx := context.Background()
	latest, older := testCustomDLLImage("0.2.0"), testCustomDLLImage("0.1.0")
	service, client, downloads := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.2.0", data: latest, digest: providerTestDigest(latest)},
		{tag: "v0.1.0", data: older, digest: providerTestDigest(older)},
	})

	overview, err := service.GetOverview(ctx)
	if err != nil || overview.SharedLibsProvider != defaultLibsProvider {
		t.Fatalf("default provider = %q, err = %v", overview.SharedLibsProvider, err)
	}
	if !slices.Equal(overview.LibsProviders, []string{defaultLibsProvider, "myparsleycat"}) {
		t.Fatalf("selectable providers = %v", overview.LibsProviders)
	}
	if err := service.SetSharedLibsProvider(ctx, "myparsleycat"); err != nil {
		t.Fatal(err)
	}
	cached := filepath.Join(providerTestCache(t, "0.2.0"), customDLLName)
	if data, err := os.ReadFile(cached); err != nil || !bytes.Equal(data, latest) {
		t.Fatalf("cached DLL has %d bytes, err = %v", len(data), err)
	}
	if _, err := os.Stat(providerTestCache(t, "0.1.0")); !os.IsNotExist(err) {
		t.Fatalf("older release was cached: %v", err)
	}
	overview, err = service.GetOverview(ctx)
	if err != nil || overview.SharedLibsProvider != "myparsleycat" {
		t.Fatalf("selected provider = %q, err = %v", overview.SharedLibsProvider, err)
	}

	// A verified copy is reused, and a damaged one is replaced by the release it came from.
	if err := service.EnsureLibsProvider(ctx, "myparsleycat"); err != nil || downloads.Load() != 1 {
		t.Fatalf("repeat ensure downloaded %d times, err = %v", downloads.Load(), err)
	}
	writeTestFile(t, cached, []byte("damaged"))
	if err := service.EnsureLibsProvider(ctx, "myparsleycat"); err != nil || downloads.Load() != 2 {
		t.Fatalf("repair downloaded %d times, err = %v", downloads.Load(), err)
	}
	if data, err := os.ReadFile(cached); err != nil || !bytes.Equal(data, latest) {
		t.Fatalf("repaired DLL has %d bytes, err = %v", len(data), err)
	}

	// An entry whose DLL and metadata were rewritten together is not the verified download either.
	forged := testCustomDLLImage("forged")
	metadata, err := json.Marshal(providerDLLSource{Version: "0.2.0", SHA256: hashBytes(forged)})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, cached, forged)
	writeTestFile(t, filepath.Join(providerTestCache(t, "0.2.0"), providerDLLMetadata), metadata)
	if err := service.EnsureLibsProvider(ctx, "myparsleycat"); err != nil || downloads.Load() != 3 {
		t.Fatalf("forged entry downloaded %d times, err = %v", downloads.Load(), err)
	}
	if data, err := os.ReadFile(cached); err != nil || !bytes.Equal(data, latest) {
		t.Fatalf("replaced DLL has %d bytes, err = %v", len(data), err)
	}

	// The signed provider adds nothing to download.
	if err := service.SetSharedLibsProvider(ctx, defaultLibsProvider); err != nil || downloads.Load() != 3 {
		t.Fatalf("default provider downloaded %d times, err = %v", downloads.Load(), err)
	}
	if err := service.SetSharedLibsProvider(ctx, "myparsleycat"); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetBuiltinRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if stored, err := client.Settings.GetValue(ctx, sharedLibsProviderKey); err != nil || stored != nil {
		t.Fatalf("reset kept provider %v, err = %v", stored, err)
	}

	// A reset keeps the package cache and the hashes that vouch for it, so the cached DLL is reused.
	if err := service.SetSharedLibsProvider(ctx, "myparsleycat"); err != nil || downloads.Load() != 3 {
		t.Fatalf("reselect after reset downloaded %d times, err = %v", downloads.Load(), err)
	}
}

func TestProviderDLLHashesSurviveNullRecord(t *testing.T) {
	ctx := context.Background()
	data := testCustomDLLImage("0.2.0")
	service, client, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.2.0", data: data, digest: providerTestDigest(data)},
	})
	null := "null"
	if err := client.Settings.Upsert(ctx, providerDLLHashesKey, &null); err != nil {
		t.Fatal(err)
	}

	if err := service.SetSharedLibsProvider(ctx, "myparsleycat"); err != nil {
		t.Fatal(err)
	}
	if !service.verifiedProviderDLL(ctx, libsProviders[1], "0.2.0") {
		t.Fatal("downloaded DLL was not recorded as verified")
	}
}

func TestSetSharedLibsProviderRejectsUnverifiableReleases(t *testing.T) {
	image := testCustomDLLImage("fork")
	for _, tc := range []struct {
		name    string
		release providerTestRelease
		want    string
	}{
		{
			name:    "digest mismatch",
			release: providerTestRelease{tag: "v0.2.0", data: image, digest: providerTestDigest([]byte("other"))},
			want:    "digest mismatch",
		},
		{
			name:    "no digest",
			release: providerTestRelease{tag: "v0.2.0", data: image},
			want:    "no asset digest",
		},
		{
			name: "not a DLL",
			release: providerTestRelease{
				tag: "v0.2.0", data: []byte("plain text"), digest: providerTestDigest([]byte("plain text")),
			},
			want: "XXMI_CUSTOM_DLL_INVALID",
		},
		{
			name: "oversized asset",
			release: providerTestRelease{
				tag: "v0.2.0", data: image, digest: providerTestDigest(image), size: customDLLSizeLimit + 1,
			},
			want: "exceeds size limit",
		},
		{
			// The published size understates the file, so only the stream itself shows the excess.
			name: "oversized download",
			release: providerTestRelease{
				tag: "v0.2.0", data: make([]byte, customDLLSizeLimit+1), digest: providerTestDigest(image), size: 1,
			},
			want: "download exceeds size limit",
		},
		{
			name:    "no asset",
			release: providerTestRelease{tag: "v0.2.0", noAsset: true},
			want:    "has no d3d11.dll",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			service, client, downloads := newProviderTestService(t, []providerTestRelease{tc.release})
			err := service.SetSharedLibsProvider(ctx, "myparsleycat")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if tc.release.size > customDLLSizeLimit && downloads.Load() != 0 {
				t.Fatal("an asset published over the size limit was downloaded")
			}
			if stored, err := client.Settings.GetValue(ctx, sharedLibsProviderKey); err != nil || stored != nil {
				t.Fatalf("rejected provider was selected: %v, err = %v", stored, err)
			}
			if _, err := os.Stat(providerTestCache(t, "0.2.0")); !os.IsNotExist(err) {
				t.Fatalf("rejected release was cached: %v", err)
			}
		})
	}

	t.Run("unknown provider", func(t *testing.T) {
		service, _, _ := newProviderTestService(t, nil)
		if err := service.SetSharedLibsProvider(context.Background(), "nobody"); err == nil {
			t.Fatal("unknown provider was selected")
		}
	})
	t.Run("no releases", func(t *testing.T) {
		service, _, _ := newProviderTestService(t, nil)
		if err := service.SetSharedLibsProvider(context.Background(), "myparsleycat"); err == nil {
			t.Fatal("provider without releases was selected")
		}
	})
}

func TestLibsProviderFollowsSharedUnlessImporterChooses(t *testing.T) {
	ctx := context.Background()
	service, client, _ := newProviderTestService(t, nil)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(cfg ImporterConfig) string {
		t.Helper()
		spec, err := service.libsProvider(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return spec.id
	}
	if got := resolve(cfg); got != defaultLibsProvider {
		t.Fatalf("unset provider = %q", got)
	}

	fork := "myparsleycat"
	if err := client.Settings.Upsert(ctx, sharedLibsProviderKey, &fork); err != nil {
		t.Fatal(err)
	}
	if got := resolve(cfg); got != fork {
		t.Fatalf("shared provider = %q", got)
	}
	own := cfg
	own.LibsProvider = defaultLibsProvider
	if got := resolve(own); got != defaultLibsProvider {
		t.Fatalf("importer provider = %q", got)
	}
	legacy := cfg
	legacy.Mode = RuntimeLegacy
	if got := resolve(legacy); got != defaultLibsProvider {
		t.Fatalf("legacy runtime provider = %q", got)
	}

	// A provider this build does not know falls back to the signed libraries instead of failing launches.
	removed := "removed"
	if err := client.Settings.Upsert(ctx, sharedLibsProviderKey, &removed); err != nil {
		t.Fatal(err)
	}
	if got := resolve(cfg); got != defaultLibsProvider {
		t.Fatalf("unknown shared provider = %q", got)
	}
	cfg.LibsProvider = removed
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("unknown importer provider passed validation")
	}
}

func TestDeployRuntimeFilesWritesProviderDLL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := t.TempDir()
	importer := filepath.Join(base, "GIMI")
	libs := filepath.Join(base, "libs")
	fork := filepath.Join(base, "fork")
	for path, content := range map[string]string{
		filepath.Join(importer, "d3dx.ini"): "[Loader]",
		filepath.Join(libs, "d3d11.dll"):    "official", filepath.Join(libs, "d3dcompiler_47.dll"): "compiler",
		filepath.Join(fork, "d3d11.dll"): "fork",
	} {
		writeTestFile(t, path, []byte(content))
	}
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeXXMI}
	provider := &providerRuntimeDLL{source: providerTestPackage + "@0.2.0", data: []byte("fork")}
	deploy := func(cfg ImporterConfig, custom *customRuntimeDLL, provider *providerRuntimeDLL) runtimeManifest {
		t.Helper()
		if _, err := deployCustomRuntimeFiles(
			ctx, "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess, custom, provider,
		); err != nil {
			t.Fatal(err)
		}
		if err := validateDeployedRuntime(importer, RuntimeXXMI); err != nil {
			t.Fatal(err)
		}
		manifest, ok := readXXMIRuntimeManifest(importer)
		if !ok {
			t.Fatal("runtime manifest is missing")
		}
		return manifest
	}

	manifest := deploy(cfg, nil, provider)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fork")
	assertFileContent(t, filepath.Join(importer, "d3dcompiler_47.dll"), "compiler")
	if manifest.Provider != provider.source || manifest.Source != "xxmi-libs@1" ||
		manifest.Files["d3d11.dll"] != hashBytes([]byte("fork")) || len(manifest.UserManaged) != 0 {
		t.Fatalf("provider manifest = %+v", manifest)
	}
	spec, _ := lookupOverlayPackage(providerTestPackage)
	if got := deployedProviderVersion(importer, spec); got != "0.2.0" {
		t.Fatalf("deployed provider version = %q", got)
	}

	// The launch accepts the provider's DLL without unsafe mode, but only against the provider's cache.
	if err := validateXXMIRuntimeFiles(importer, libs, fork, false); err != nil {
		t.Fatal(err)
	}
	if err := validateXXMIRuntimeFiles(importer, libs, libs, false); err == nil {
		t.Fatal("provider DLL passed the signed-cache comparison")
	}
	writeTestFile(t, filepath.Join(importer, "d3d11.dll"), []byte("tampered"))
	if err := validateXXMIRuntimeFiles(importer, libs, fork, false); err == nil {
		t.Fatal("tampered provider DLL passed validation")
	}

	// Returning to the signed provider restores the signed DLL.
	manifest = deploy(cfg, nil, nil)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "official")
	if manifest.Provider != "" || deployedProviderVersion(importer, spec) != "" {
		t.Fatalf("signed manifest = %+v", manifest)
	}
	if err := validateXXMIRuntimeFiles(importer, libs, libs, false); err != nil {
		t.Fatal(err)
	}

	// A custom DLL is the user's explicit file and wins over the provider's.
	unsafe := cfg
	unsafe.Migoto.UnsafeMode = true
	manifest = deploy(unsafe, &customRuntimeDLL{id: "abcdef123456", data: []byte("custom")}, provider)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "custom")
	if manifest.Provider != "" || manifest.Custom != "abcdef123456" || manifest.Files["d3d11.dll"] != "" {
		t.Fatalf("custom manifest = %+v", manifest)
	}
}

func TestSetSharedLibsProviderKeepsSelectionOrder(t *testing.T) {
	ctx := context.Background()
	image := testCustomDLLImage("0.2.0")
	started, proceed := make(chan struct{}), make(chan struct{})
	service, _, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.2.0", data: image, digest: providerTestDigest(image), started: started, proceed: proceed},
	})

	// The fork is still downloading when the user goes back to the signed provider.
	results := make(chan error, 2)
	go func() { results <- service.SetSharedLibsProvider(ctx, "myparsleycat") }()
	<-started
	go func() { results <- service.SetSharedLibsProvider(ctx, defaultLibsProvider) }()
	close(proceed)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	overview, err := service.GetOverview(ctx)
	if err != nil || overview.SharedLibsProvider != defaultLibsProvider {
		t.Fatalf("selected provider = %q, err = %v", overview.SharedLibsProvider, err)
	}
}

func TestFirstProviderDeploymentKeepsCachedRelease(t *testing.T) {
	ctx := context.Background()
	cached, newer := testCustomDLLImage("0.2.0"), testCustomDLLImage("0.3.0")
	service, client, downloads := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.3.0", data: newer, digest: providerTestDigest(newer)},
		{tag: "v0.2.0", data: cached, digest: providerTestDigest(cached)},
	})
	spec, _ := lookupOverlayPackage(providerTestPackage)
	if err := service.ensureProviderDLL(ctx, spec, "0.2.0"); err != nil {
		t.Fatal(err)
	}
	resolve := func(skipped *string) string {
		t.Helper()
		latest := "0.3.0"
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: providerTestPackage, LatestVersion: &latest, SkippedVersion: skipped,
		}); err != nil {
			t.Fatal(err)
		}
		version, err := service.resolveProviderDLLVersion(ctx, spec, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return version
	}

	// Nothing of this provider is deployed yet, and the cached release still holds the newer one back.
	if got := resolve(nil); got != "0.2.0" {
		t.Fatalf("release before the update is installed = %q", got)
	}
	skipped := "0.3.0"
	if got := resolve(&skipped); got != "0.2.0" {
		t.Fatalf("release with the update skipped = %q", got)
	}
	if downloads.Load() != 1 {
		t.Fatalf("resolving downloaded %d times", downloads.Load())
	}
	if err := service.ensureProviderDLL(ctx, spec, "0.3.0"); err != nil {
		t.Fatal(err)
	}
	if got := resolve(nil); got != "0.3.0" {
		t.Fatalf("release after the update is installed = %q", got)
	}
}

func TestAdoptedDLLHidesProvider(t *testing.T) {
	ctx := context.Background()

	// The fork release cannot be verified, so any attempt to download it fails.
	service, client, downloads := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.2.0", data: testCustomDLLImage("0.2.0")},
	})
	fork := "myparsleycat"
	if err := client.Settings.Upsert(ctx, sharedLibsProviderKey, &fork); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), "GIMI")
	writeTestFile(t, filepath.Join(folder, customDLLName), []byte("user build"))
	if err := service.EnableImporter(ctx, "GIMI", folder); err != nil {
		t.Fatal(err)
	}
	if err := service.AdoptUserRuntime(ctx, "GIMI"); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}

	if provider, err := service.providerRuntimeDLL(ctx, cfg); err != nil || provider != nil {
		t.Fatalf("provider DLL for a kept user DLL = %v, err = %v", provider, err)
	}
	statuses, err := service.CheckUpdates(ctx, false)
	if err != nil || slices.ContainsFunc(statuses, func(status UpdateStatus) bool {
		return status.Package == providerTestPackage
	}) {
		t.Fatalf("a provider hidden by the kept DLL was tracked: %+v, err = %v", statuses, err)
	}
	if downloads.Load() != 0 {
		t.Fatalf("downloaded %d times", downloads.Load())
	}

	// Once the importer stops keeping the file, the provider's DLL is what the deployment needs.
	cfg.Migoto.UnsafeMode = false
	if _, err := service.providerRuntimeDLL(ctx, cfg); err == nil {
		t.Fatal("an unverifiable provider release was accepted")
	}
}

func TestCustomDLLHidesProviderUpdates(t *testing.T) {
	ctx := context.Background()

	// The fork release cannot be verified, so any attempt to install it fails.
	service, client, downloads := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.2.0", data: testCustomDLLImage("0.2.0")},
	})
	fork := "myparsleycat"
	if err := client.Settings.Upsert(ctx, sharedLibsProviderKey, &fork); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "d3d11.dll")
	writeTestFile(t, source, testCustomDLLImage("custom"))
	custom, err := service.ImportCustomDLL(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.CustomDLL, cfg.XXMIVersion = true, custom.ID, VersionPin{Follow: "latest"}
	cfg.Migoto.UnsafeMode = true
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	tracksProvider := func() bool {
		t.Helper()
		statuses, err := service.CheckUpdates(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		return slices.ContainsFunc(statuses, func(status UpdateStatus) bool {
			return status.Package == providerTestPackage
		})
	}

	if tracksProvider() {
		t.Fatal("a provider hidden by the custom DLL was tracked")
	}
	if err := service.autoUpdateForLaunch(ctx, "GIMI"); err != nil || downloads.Load() != 0 {
		t.Fatalf("launch update downloaded %d times, err = %v", downloads.Load(), err)
	}

	// Restoring the official DLL drops the custom DLL and keeps the fork from taking its place.
	restored := officialDLLConfig(cfg)
	if spec, err := service.libsProvider(ctx, restored); err != nil || spec.id != defaultLibsProvider {
		t.Fatalf("restored provider = %q, err = %v", spec.id, err)
	}
	if id, err := service.customDLLID(ctx, restored); err != nil || id != "" || restored.Migoto.UnsafeMode {
		t.Fatalf("restored custom DLL = %q, unsafe = %t, err = %v", id, restored.Migoto.UnsafeMode, err)
	}

	// Without the custom DLL the fork is what the importer deploys, and its updates count again.
	cfg.Migoto.UnsafeMode, cfg.CustomDLL = false, ""
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	if !tracksProvider() {
		t.Fatal("the deployed provider was not tracked")
	}
	if err := service.autoUpdateForLaunch(ctx, "GIMI"); err == nil {
		t.Fatal("an unverifiable provider release was installed")
	}
}

func TestCheckUpdatesTracksProviderDLL(t *testing.T) {
	ctx := context.Background()
	latest := testCustomDLLImage("0.2.0")
	service, client, downloads := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.2.0", data: latest, digest: providerTestDigest(latest)},
	})
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.LibsProvider = true, "myparsleycat"
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	providerStatus := func() UpdateStatus {
		t.Helper()
		statuses, err := service.CheckUpdates(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(statuses, func(status UpdateStatus) bool {
			return status.Package == providerTestPackage
		})
		if index < 0 {
			t.Fatalf("provider status is missing from %+v", statuses)
		}
		return statuses[index]
	}

	status := providerStatus()
	if status.Importer != "GIMI" || status.LatestVersion != "0.2.0" || status.Installed != "" ||
		!status.Available || status.Shared {
		t.Fatalf("status before install = %+v", status)
	}
	installed, err := service.InstallUpdates(ctx, "GIMI", []string{providerTestPackage})
	if err != nil || !slices.Contains(installed, providerTestPackage) || downloads.Load() != 1 {
		t.Fatalf("installed = %v, downloads = %d, err = %v", installed, downloads.Load(), err)
	}

	// The cached release is deployed by the next launch, so it is not announced again.
	status = providerStatus()
	if status.Installed != "0.2.0" || status.Available {
		t.Fatalf("status after install = %+v", status)
	}

	// An importer without its own choice reports the shared provider's update as shared.
	cfg.LibsProvider = ""
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	statuses, err := service.CheckUpdates(ctx, false)
	if err != nil || slices.ContainsFunc(statuses, func(status UpdateStatus) bool {
		return status.Package == providerTestPackage
	}) {
		t.Fatalf("signed provider reported the fork: %+v, err = %v", statuses, err)
	}
	fork := "myparsleycat"
	if err := client.Settings.Upsert(ctx, sharedLibsProviderKey, &fork); err != nil {
		t.Fatal(err)
	}
	if status := providerStatus(); !status.Shared || status.Available {
		t.Fatalf("shared status = %+v", status)
	}
	if err := service.SkipVersion(ctx, providerTestPackage, "0.2.0"); err != nil {
		t.Fatal(err)
	}
}
