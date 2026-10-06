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
	"time"

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
	if err := service.EnsureLibsProvider(ctx, "myparsleycat", ""); err != nil || downloads.Load() != 1 {
		t.Fatalf("repeat ensure downloaded %d times, err = %v", downloads.Load(), err)
	}
	writeTestFile(t, cached, []byte("damaged"))
	if err := service.EnsureLibsProvider(ctx, "myparsleycat", ""); err != nil || downloads.Load() != 2 {
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
	if err := service.EnsureLibsProvider(ctx, "myparsleycat", ""); err != nil || downloads.Load() != 3 {
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

func TestSetSharedLibsProviderReplacesSharedCustomDLL(t *testing.T) {
	ctx := context.Background()
	data := testCustomDLLImage("0.2.0")
	service, client, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v0.2.0", data: data, digest: providerTestDigest(data)},
	})
	service.findProcess = noGameProcess
	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableImporter(ctx, "GIMI", folder); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "d3d11.dll")
	writeTestFile(t, source, testCustomDLLImage("custom"))
	dll, err := service.ImportCustomDLL(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetSharedCustomDLL(ctx, dll.ID); err != nil {
		t.Fatal(err)
	}

	// The importer launches the shared custom DLL over signed libraries the provider has no release for.
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Migoto.UnsafeMode = true
	cfg.XXMIVersion = VersionPin{Pinned: "1.7.6"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	// A provider that cannot be selected leaves the custom DLL in place.
	if err := service.SetSharedLibsProvider(ctx, "nobody"); err == nil {
		t.Fatal("unknown provider was selected")
	}
	if stored, err := client.Settings.GetValue(ctx, sharedCustomDLLKey); err != nil || stored == nil ||
		*stored != dll.ID {
		t.Fatalf("shared custom DLL after a rejected provider = %v, err = %v", stored, err)
	}

	if err := service.SetSharedLibsProvider(ctx, "myparsleycat"); err != nil {
		t.Fatal(err)
	}
	if stored, err := client.Settings.GetValue(ctx, sharedCustomDLLKey); err != nil || stored == nil || *stored != "" {
		t.Fatalf("shared custom DLL after selecting a provider = %v, err = %v", stored, err)
	}
	cfg, err = service.GetImporterConfig(ctx, "GIMI")
	if err != nil || cfg.XXMIVersion != (VersionPin{Follow: "latest"}) {
		t.Fatalf("pin of the importer that launched the custom DLL = %+v, err = %v", cfg.XXMIVersion, err)
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
		version, err := service.resolveProviderDLLVersion(ctx, spec, "", t.TempDir())
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
	restored := officialDLLConfig(cfg, "")
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

	// The signed libraries the release deploys over cannot be downloaded here, so nothing is installed.
	if installed, err := service.InstallUpdates(
		ctx,
		"GIMI",
		[]string{providerTestPackage},
	); err == nil || len(installed) != 0 || downloads.Load() != 0 {
		t.Fatalf("installed = %v, downloads = %d, err = %v", installed, downloads.Load(), err)
	}
	spec, _ := lookupOverlayPackage(providerTestPackage)
	if err := service.ensureProviderDLL(ctx, spec, "0.2.0"); err != nil {
		t.Fatal(err)
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

func TestProviderReleasePinsSignedLibraries(t *testing.T) {
	ctx := context.Background()
	newest, first, old := testCustomDLLImage("1.2.2-nhd.2"), testCustomDLLImage("1.2.2-nhd.1"),
		testCustomDLLImage("1.2.0-nhd.1")
	service, client, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v1.2.2-nhd.2", data: newest, digest: providerTestDigest(newest)},
		{tag: "v1.2.2-nhd.1", data: first, digest: providerTestDigest(first)},
		{tag: "v1.2.0-nhd.1", data: old, digest: providerTestDigest(old)},
	})
	for version, signed := range map[string]string{
		"v1.2.2-nhd.1": "1.2.2", "1.2.2": "1.2.2", "1.3.0-rc1": "1.3.0-rc1", "": "",
	} {
		if got := signedLibsVersion(version); got != signed {
			t.Fatalf("signed libraries of %q = %q, want %q", version, got, signed)
		}
	}

	releases, err := service.GetLibsProviderReleases(ctx, "myparsleycat")
	if err != nil || len(releases) != 3 || releases[0].Version != "1.2.2-nhd.2" {
		t.Fatalf("fork releases = %+v, err = %v", releases, err)
	}
	if _, err := service.GetLibsProviderReleases(ctx, "nobody"); err == nil {
		t.Fatal("an unknown provider listed releases")
	}

	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.LibsProvider = true, "myparsleycat"
	for pin, want := range map[string][2]string{
		// A fork release deploys over the signed libraries it was built on.
		"1.2.0-nhd.1": {"1.2.0-nhd.1", "1.2.0"},
		// A signed version stands for the newest fork release built on it.
		"1.2.2": {"1.2.2-nhd.2", "1.2.2"},
		// Following the latest fork release keeps the signed libraries at its base.
		"": {"1.2.2-nhd.2", "1.2.2"},
	} {
		cfg.XXMIVersion = VersionPin{Pinned: pin}
		if pin == "" {
			cfg.XXMIVersion = VersionPin{Follow: "latest"}
		}
		spec, err := service.libsProvider(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		dll, err := service.resolveProviderDLLVersion(ctx, spec, pin, cfg.ImporterFolder)
		if err != nil || dll != want[0] {
			t.Fatalf("fork release for pin %q = %q, err = %v", pin, dll, err)
		}
		if signed, err := service.resolveLibsVersion(ctx, cfg); err != nil || signed != want[1] {
			t.Fatalf("signed libraries for pin %q = %q, err = %v", pin, signed, err)
		}
	}
	cfg.XXMIVersion = VersionPin{Pinned: "1.1.7"}
	if version, err := service.resolveLibsVersion(ctx, cfg); err == nil {
		t.Fatalf("signed libraries without a fork release resolved to %q", version)
	}

	// A pinned fork importer follows only the fork's releases, and its pin holds the update back.
	cfg.XXMIVersion = VersionPin{Pinned: "1.2.0-nhd.1"}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	statuses, err := service.CheckUpdates(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		switch status.Package {
		case "xxmi-libs":
			t.Fatalf("fork importer follows the signed libraries: %+v", status)
		case providerTestPackage:
			if !status.Pinned || status.Installed != "1.2.0-nhd.1" || status.LatestVersion != "1.2.2-nhd.2" {
				t.Fatalf("pinned fork status = %+v", status)
			}
		}
	}

	sharedVersion := func() string {
		t.Helper()
		stored, err := client.Settings.GetValue(ctx, sharedLibsVersionKey)
		if err != nil {
			t.Fatal(err)
		}
		if stored == nil {
			return ""
		}
		return *stored
	}
	selectProvider := func(pinned, provider string) string {
		t.Helper()
		if err := client.Settings.Upsert(ctx, sharedLibsVersionKey, &pinned); err != nil {
			t.Fatal(err)
		}
		if err := service.SetSharedLibsProvider(ctx, provider); err != nil {
			t.Fatal(err)
		}
		return sharedVersion()
	}

	// The shared version moves to the selected provider's release of the same signed libraries.
	if got := selectProvider("1.2.2", "myparsleycat"); got != "1.2.2-nhd.2" {
		t.Fatalf("shared version under the fork = %q", got)
	}
	if _, err := os.Stat(filepath.Join(providerTestCache(t, "1.2.2-nhd.2"), customDLLName)); err != nil {
		t.Fatalf("pinned fork release was not cached: %v", err)
	}
	if got := selectProvider("1.2.0-nhd.1", defaultLibsProvider); got != "1.2.0" {
		t.Fatalf("shared version under the signed provider = %q", got)
	}
	if got := selectProvider("1.1.7", "myparsleycat"); got != "" {
		t.Fatalf("shared version without a fork release = %q", got)
	}
}

func TestProviderUpdateKeepsPinWithoutSignedLibraries(t *testing.T) {
	ctx := context.Background()
	newer, pinned := testCustomDLLImage("1.2.2-nhd.1"), testCustomDLLImage("1.2.0-nhd.1")
	service, _, downloads := newProviderTestService(t, []providerTestRelease{
		{tag: "v1.2.2-nhd.1", data: newer, digest: providerTestDigest(newer)},
		{tag: "v1.2.0-nhd.1", data: pinned, digest: providerTestDigest(pinned)},
	})
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.LibsProvider = true, "myparsleycat"
	cfg.XXMIVersion = VersionPin{Pinned: "1.2.0-nhd.1", Notify: true}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	// The fake GitHub has no signed 1.2.2 libraries, which the newer fork release deploys over.
	if _, err := service.InstallUpdates(ctx, "GIMI", []string{providerTestPackage}); err == nil {
		t.Fatal("a fork update was installed without its signed libraries")
	}
	saved, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil || saved.XXMIVersion.Pinned != "1.2.0-nhd.1" || downloads.Load() != 0 {
		t.Fatalf("pin = %q, downloads = %d, err = %v", saved.XXMIVersion.Pinned, downloads.Load(), err)
	}
}

func TestPruneLibsCacheKeepsProviderSignedLibraries(t *testing.T) {
	ctx := context.Background()
	latest := testCustomDLLImage("1.2.2-nhd.1")
	service, client, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v1.2.2-nhd.1", data: latest, digest: providerTestDigest(latest)},
	})
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.LibsProvider = true, "myparsleycat"
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	// The importer still runs the signed 1.2.0 libraries, and the cached fork release moves the next launch
	// to 1.2.2 while the signed releases are already at 1.3.0.
	manifest, err := json.Marshal(runtimeManifest{Mode: RuntimeXXMI, Source: "xxmi-libs@1.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, runtimeManifestName), manifest)
	spec, _ := lookupOverlayPackage(providerTestPackage)
	if err := service.ensureProviderDLL(ctx, spec, "1.2.2-nhd.1"); err != nil {
		t.Fatal(err)
	}
	signedLatest, forkLatest := "1.3.0", "1.2.2-nhd.1"
	for pkg, version := range map[string]*string{"xxmi-libs": &signedLatest, providerTestPackage: &forkLatest} {
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{Package: pkg, LatestVersion: version}); err != nil {
			t.Fatal(err)
		}
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "packages", "xxmi-libs")
	for _, version := range []string{"1.1.7", "1.2.0", "1.2.2"} {
		if err := os.MkdirAll(filepath.Join(parent, version), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	_, referenced, err := service.libsCacheReferences(ctx)
	if err != nil || !referenced["1.2.2"].InUse || !referenced["1.2.0"].Referenced {
		t.Fatalf("references = %+v, err = %v", referenced, err)
	}
	removed, err := service.PruneLibsCache(ctx)
	if err != nil || !slices.Equal(removed, []string{"1.1.7"}) {
		t.Fatalf("pruned libraries = %v, err = %v", removed, err)
	}
}

func TestSharedProviderSelectionMovesImporterPins(t *testing.T) {
	ctx := context.Background()
	newest, first := testCustomDLLImage("1.2.2-nhd.2"), testCustomDLLImage("1.2.2-nhd.1")
	service, _, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v1.2.2-nhd.2", data: newest, digest: providerTestDigest(newest)},
		{tag: "v1.2.2-nhd.1", data: first, digest: providerTestDigest(first)},
	})
	for key, pin := range map[string]VersionPin{
		"GIMI": {Pinned: "1.2.2", Notify: true},
		"SRMI": {Pinned: "1.1.7"},
	} {
		cfg, err := DefaultImporterConfig(key, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cfg.Mode, cfg.XXMIVersion = RuntimeXXMI, pin
		if err := service.SaveImporterConfig(ctx, key, cfg); err != nil {
			t.Fatal(err)
		}
	}
	pin := func(key string) VersionPin {
		t.Helper()
		cfg, err := service.GetImporterConfig(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.XXMIVersion
	}

	// An importer's own pin moves with the shared provider it follows, like the shared version does.
	if err := service.SetSharedLibsProvider(ctx, "myparsleycat"); err != nil {
		t.Fatal(err)
	}
	if got := pin("GIMI"); got != (VersionPin{Pinned: "1.2.2-nhd.2", Notify: true}) {
		t.Fatalf("pin under the fork = %+v", got)
	}
	if got := pin("SRMI"); got != (VersionPin{Follow: "latest"}) {
		t.Fatalf("pin without a fork release = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(providerTestCache(t, "1.2.2-nhd.2"), customDLLName)); err != nil {
		t.Fatalf("moved pin's fork release was not cached: %v", err)
	}

	if err := service.SetSharedLibsProvider(ctx, defaultLibsProvider); err != nil {
		t.Fatal(err)
	}
	if got := pin("GIMI"); got != (VersionPin{Pinned: "1.2.2", Notify: true}) {
		t.Fatalf("pin under the signed provider = %+v", got)
	}
}

func TestSharedLibsVersionNeedsReleaseOfFollowingProviders(t *testing.T) {
	ctx := context.Background()
	latest := testCustomDLLImage("1.2.2-nhd.1")
	service, client, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v1.2.2-nhd.1", data: latest, digest: providerTestDigest(latest)},
	})
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.Mode, cfg.LibsProvider = true, RuntimeXXMI, "myparsleycat"
	cfg.XXMIVersion = VersionPin{Follow: followShared}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	// The shared provider is the signed one, but the importer deploys the fork over the shared version.
	err = service.SetSharedLibsVersion(ctx, "1.1.7")
	if err == nil || !strings.Contains(err.Error(), providerTestPackage) {
		t.Fatalf("shared version without a fork release: err = %v", err)
	}
	if stored, err := client.Settings.GetValue(ctx, sharedLibsVersionKey); err != nil || stored != nil {
		t.Fatalf("rejected shared version was stored: %v, err = %v", stored, err)
	}
}

func TestCheckUpdatesComparesSignedLibrariesOfProviderPin(t *testing.T) {
	ctx := context.Background()
	service, client, _ := newProviderTestService(t, nil)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled, cfg.Mode = true, RuntimeXXMI
	cfg.XXMIVersion = VersionPin{Pinned: "1.2.2-nhd.1", Notify: true}
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	latest := "1.2.2"
	if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
		Package: "xxmi-libs", LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	// A pin left from the fork deploys the signed 1.2.2 libraries under the signed provider.
	statuses, err := service.CheckUpdates(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(statuses, func(status UpdateStatus) bool { return status.Package == "xxmi-libs" })
	if index < 0 || statuses[index].Installed != "1.2.2" || statuses[index].Available {
		t.Fatalf("statuses = %+v", statuses)
	}
}
