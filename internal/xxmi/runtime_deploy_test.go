package xxmi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

// noGameProcess reports that no game is running, keeping deploy tests independent of the host process list.
func noGameProcess(context.Context, string) (int, error) { return 0, nil }

func TestBootstrapExternalRuntimeUsesVerifiedSignatures(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	for name, content := range map[string]string{
		"d3d11.dll": "externally deployed", "d3dcompiler_47.dll": "third-party",
	} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(data []byte) string {
		t.Helper()
		digest := sha256.Sum256(data)
		signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(signature)
	}
	root, err := openInstallRoot(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	manifest := runtimeManifest{Files: map[string]string{}}
	if err := bootstrapExternalRuntime(root, &manifest, map[string]string{
		"d3d11.dll":          sign([]byte("externally deployed")),
		"d3dcompiler_47.dll": sign([]byte("original compiler")),
	}, base64.StdEncoding.EncodeToString(der)); err != nil {
		t.Fatal(err)
	}
	if manifest.Files["d3d11.dll"] != hashBytes([]byte("externally deployed")) {
		t.Fatalf("signed external DLL was not recognized: %+v", manifest.Files)
	}
	if _, ok := manifest.Files["d3dcompiler_47.dll"]; ok {
		t.Fatal("third-party DLL was marked as externally deployed")
	}
}

func TestDeployRuntimeSwitchesModesWithoutReplacingContent(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	importer := filepath.Join(base, "GIMI")
	libs := filepath.Join(base, "libs")
	legacy := filepath.Join(base, "legacy")
	for _, folder := range []string{importer, libs, legacy} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"d3dx.ini": "[Loader]\n", "Mods/example.ini": "mod content",
	} {
		path := filepath.Join(importer, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"d3d11.dll": "xxmi", "d3dcompiler_47.dll": "compiler"} {
		if err := os.WriteFile(filepath.Join(libs, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"3DMigoto Loader.exe": "loader", "d3d11.dll": "legacy", "nvapi64.dll": "nvapi"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := LegacyRuntimeSource{Files: map[string]string{
		"3DMigoto Loader.exe": hashBytes([]byte("loader")),
		"d3d11.dll":           hashBytes([]byte("legacy")),
		"nvapi64.dll":         hashBytes([]byte("nvapi")),
	}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "source.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeXXMI}
	ctx := context.Background()
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess); err != nil {
		t.Fatal(err)
	}
	if err := validateDeployedRuntime(importer, RuntimeXXMI); err != nil {
		t.Fatal(err)
	}
	cfg.Mode = RuntimeLegacy
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, legacy, "legacy@abc", base, false, noGameProcess); err != nil {
		t.Fatal(err)
	}
	if err := validateDeployedRuntime(importer, RuntimeLegacy); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "legacy")
	assertFileContent(t, filepath.Join(importer, "Mods", "example.ini"), "mod content")
	if _, err := os.Stat(filepath.Join(importer, "d3dcompiler_47.dll")); !os.IsNotExist(err) {
		t.Fatalf("old compiler still deployed: %v", err)
	}
	cfg.Mode = RuntimeXXMI
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess); err != nil {
		t.Fatal(err)
	}
	if err := validateDeployedRuntime(importer, RuntimeXXMI); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "xxmi")
	if _, err := os.Stat(filepath.Join(importer, "nvapi64.dll")); !os.IsNotExist(err) {
		t.Fatalf("deprecated nvapi still deployed: %v", err)
	}
}

func TestWaitForGameProcesses(t *testing.T) {
	t.Run("exits before deadline", func(t *testing.T) {
		calls := 0
		err := waitForGameProcesses(context.Background(), []string{"Game.exe"}, time.Second,
			func(context.Context, string) (int, error) {
				calls++
				if calls == 1 {
					return 42, nil
				}
				return 0, nil
			})
		if err != nil || calls < 2 {
			t.Fatalf("wait result = %v after %d probes", err, calls)
		}
	})
	t.Run("still running", func(t *testing.T) {
		err := waitForGameProcesses(context.Background(), []string{"Game.exe"}, time.Millisecond,
			func(context.Context, string) (int, error) { return 42, nil })
		if err == nil || err.Error() != "XXMI_GAME_RUNNING" {
			t.Fatalf("wait result = %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := waitForGameProcesses(ctx, []string{"Game.exe"}, time.Second,
			func(ctx context.Context, _ string) (int, error) { return 0, ctx.Err() })
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait result = %v", err)
		}
	})
}

func TestRuntimeFileErrorClassifiesSharingViolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d3d11.dll")
	locked := runtimeFileError(syscall.Errno(32), path)
	if !strings.Contains(locked.Error(), "XXMI_RUNTIME_LOCKED") || !strings.Contains(locked.Error(), path) ||
		!errors.Is(locked, syscall.Errno(32)) {
		t.Fatalf("sharing violation = %v", locked)
	}
	denied := runtimeFileError(syscall.Errno(5), path)
	if strings.Contains(denied.Error(), "XXMI_RUNTIME_LOCKED") || !errors.Is(denied, syscall.Errno(5)) {
		t.Fatalf("access denied = %v", denied)
	}
}

func TestValidateDeployedRuntimeDetectsChangedFile(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	importer := filepath.Join(base, "GIMI")
	libs := filepath.Join(base, "libs")
	for _, folder := range []string{importer, libs} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(importer, "d3dx.ini"):       "[Loader]",
		filepath.Join(libs, "d3d11.dll"):          "xxmi",
		filepath.Join(libs, "d3dcompiler_47.dll"): "compiler",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeXXMI}
	if _, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importer, "d3d11.dll"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateDeployedRuntime(importer, RuntimeXXMI); err == nil {
		t.Fatal("changed DLL passed runtime validation")
	}
}

func TestValidateXXMIRuntimeFilesRejectsChangedDLLWithRewrittenManifest(t *testing.T) {
	t.Parallel()
	importer := t.TempDir()
	cache := t.TempDir()
	files := map[string]string{"d3d11.dll": "signed d3d11", "d3dcompiler_47.dll": "signed compiler"}
	manifest := runtimeManifest{Mode: RuntimeXXMI, Source: "xxmi-libs@1.0.0", Files: map[string]string{}}
	for name, content := range files {
		for _, folder := range []string{importer, cache} {
			if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		manifest.Files[name] = hashBytes([]byte(content))
	}
	writeManifest := func() {
		t.Helper()
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(importer, runtimeManifestName), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	if err := validateXXMIRuntimeFiles(importer, cache, false); err != nil {
		t.Fatal(err)
	}
	modified := []byte("modified d3d11")
	if err := os.WriteFile(filepath.Join(importer, "d3d11.dll"), modified, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest.Files["d3d11.dll"] = hashBytes(modified)
	writeManifest()
	if err := validateDeployedRuntime(importer, RuntimeXXMI); err != nil {
		t.Fatalf("rewritten manifest should pass its own hash check: %v", err)
	}
	if err := validateXXMIRuntimeFiles(importer, cache, false); err == nil {
		t.Fatal("modified DLL passed signed-cache comparison")
	}
	if err := validateXXMIRuntimeFiles(importer, cache, true); err != nil {
		t.Fatalf("unsafe mode rejected a user-managed DLL: %v", err)
	}
}

func TestValidateLegacyRuntimeFilesRejectsChangedDLLWithRewrittenManifest(t *testing.T) {
	t.Parallel()
	importer := t.TempDir()
	cache := t.TempDir()
	files := map[string]string{"3DMigoto Loader.exe": "loader", "d3d11.dll": "legacy DLL"}
	source := LegacyRuntimeSource{ZipSHA256: "test zip", Files: map[string]string{}}
	manifest := runtimeManifest{Mode: RuntimeLegacy, Source: "legacy@abcdef123456", Files: map[string]string{}}
	for name, content := range files {
		for _, folder := range []string{importer, cache} {
			if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		source.Files[name] = hashBytes([]byte(content))
		manifest.Files[name] = source.Files[name]
	}
	sourceData, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "source.json"), sourceData, 0o600); err != nil {
		t.Fatal(err)
	}
	writeManifest := func() {
		t.Helper()
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(importer, runtimeManifestName), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	if err := validateLegacyRuntimeFiles(importer, cache); err != nil {
		t.Fatal(err)
	}
	modified := []byte("modified legacy DLL")
	if err := os.WriteFile(filepath.Join(importer, "d3d11.dll"), modified, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest.Files["d3d11.dll"] = hashBytes(modified)
	writeManifest()
	if err := validateDeployedRuntime(importer, RuntimeLegacy); err != nil {
		t.Fatalf("rewritten manifest should pass its own hash check: %v", err)
	}
	if err := validateLegacyRuntimeFiles(importer, cache); err == nil {
		t.Fatal("modified DLL passed legacy source comparison")
	}
}

func TestDeployRuntimeUnsafePreservesThirdPartyDLL(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	importer := filepath.Join(base, "GIMI")
	libs := filepath.Join(base, "libs")
	for _, folder := range []string{importer, libs} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(importer, "d3dx.ini"): "[Loader]", filepath.Join(importer, "d3d11.dll"): "fixer",
		filepath.Join(libs, "d3d11.dll"): "xxmi", filepath.Join(libs, "d3dcompiler_47.dll"): "compiler",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeXXMI, Migoto: MigotoOptions{UnsafeMode: true}}
	warnings, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) == 0 {
		t.Fatal("missing third-party preservation warning")
	}
	if err := validateDeployedRuntime(importer, RuntimeXXMI); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer")
	if !usesCustomDLL(importer) {
		t.Fatal("preserved third-party DLL is not reported as custom")
	}
	warnings, err = deployRuntimeFiles(
		context.Background(), "GIMI", cfg, libs, "xxmi-libs@2", base, false, noGameProcess,
	)
	if err != nil || len(warnings) == 0 {
		t.Fatalf("repeat deployment warnings = %v, error = %v", warnings, err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer")
	cfg.Migoto.UnsafeMode = false
	if _, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, libs, "xxmi-libs@2", base, false, noGameProcess,
	); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "xxmi")
	if usesCustomDLL(importer) {
		t.Fatal("signed DLL is still reported as custom")
	}
}

func TestDeployRuntimeModeSwitchBacksUpUserManagedDLLs(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	importer := filepath.Join(base, "GIMI")
	libs := filepath.Join(base, "libs")
	legacy := filepath.Join(base, "legacy")
	for _, folder := range []string{importer, libs, legacy} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(importer, "d3dx.ini"):           "[Loader]",
		filepath.Join(importer, "d3d11.dll"):          "fixer",
		filepath.Join(importer, "d3dcompiler_47.dll"): "custom compiler",
		filepath.Join(libs, "d3d11.dll"):              "xxmi",
		filepath.Join(libs, "d3dcompiler_47.dll"):     "xxmi compiler",
		filepath.Join(legacy, "3DMigoto Loader.exe"):  "loader",
		filepath.Join(legacy, "d3d11.dll"):            "legacy",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := json.Marshal(LegacyRuntimeSource{Files: map[string]string{
		"3DMigoto Loader.exe": hashBytes([]byte("loader")), "d3d11.dll": hashBytes([]byte("legacy")),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "source.json"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeXXMI, Migoto: MigotoOptions{UnsafeMode: true}}
	if _, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess,
	); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer")
	cfg.Mode = RuntimeLegacy
	if _, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, legacy, "legacy@abc", base, false, noGameProcess,
	); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "legacy")
	if _, err := os.Stat(filepath.Join(importer, "d3dcompiler_47.dll")); !os.IsNotExist(err) {
		t.Fatalf("user-managed XXMI compiler remained in legacy mode: %v", err)
	}
	if err := validateDeployedRuntime(importer, RuntimeLegacy); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(base, "backups", "GIMI *"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("runtime backups = %v, err = %v", backups, err)
	}
	firstBackup := backups[0]
	assertFileContent(t, filepath.Join(firstBackup, "d3d11.dll"), "fixer")
	assertFileContent(t, filepath.Join(firstBackup, "d3dcompiler_47.dll"), "custom compiler")
	if err := os.WriteFile(filepath.Join(importer, "d3d11.dll"), []byte("legacy fixer"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Mode = RuntimeXXMI
	if _, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess,
	); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "xxmi")
	backups, err = filepath.Glob(filepath.Join(base, "backups", "GIMI *"))
	if err != nil || len(backups) != 2 {
		t.Fatalf("successive runtime backups = %v, err = %v", backups, err)
	}
	for _, folder := range backups {
		if folder == firstBackup {
			continue
		}
		assertFileContent(t, filepath.Join(folder, "d3d11.dll"), "legacy fixer")
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, err = %v, want %q", path, data, err, want)
	}
}

func TestNewestLegacyRuntimeIgnoresStagingDirectory(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"abcdef123456", "abcdef123456.tmp-in-progress"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got, err := newestLegacyRuntime(parent)
	if err != nil || got != "abcdef123456" {
		t.Fatalf("latest cache = %q, err = %v", got, err)
	}
}

func TestResolveLibsVersionFetchesLatestBeforeUpdateCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.Contains(request.URL.Path, "XXMI-Libs-Package") {
			t.Errorf("unexpected release request %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
			Body: io.NopCloser(strings.NewReader(`[{"tag_name":"v1.2.3"}]`)),
		}, nil
	})}
	x := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: httpClient, Status: infra.BackendOnline,
	})})
	x.UseClient(client)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	version, err := x.resolveLibsVersion(ctx, cfg)
	if err != nil || version != "1.2.3" {
		t.Fatalf("resolved version = %q, err = %v", version, err)
	}
}

func TestResolveLibsVersionKeepsDeployedRuntimeWithoutVerifiedUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := NewWithOptions(Options{})
	x.UseClient(client)
	folder := t.TempDir()
	cfg, err := DefaultImporterConfig("GIMI", folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(runtimeManifest{Mode: RuntimeXXMI, Source: "xxmi-libs@1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	latest := "2.0.0"
	if err := client.XXMIPackages.Upsert(
		ctx,
		db.XXMIPackageRow{Package: "xxmi-libs", LatestVersion: &latest},
	); err != nil {
		t.Fatal(err)
	}
	version, err := x.resolveLibsVersion(ctx, cfg)
	if err != nil || version != "1.0.0" {
		t.Fatalf("uncached update selected %q, err = %v", version, err)
	}
	if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
		Package: "xxmi-libs", LatestVersion: &latest, SkippedVersion: &latest,
	}); err != nil {
		t.Fatal(err)
	}
	version, err = x.resolveLibsVersion(ctx, cfg)
	if err != nil || version != "1.0.0" {
		t.Fatalf("skipped update selected %q, err = %v", version, err)
	}
	shared := "v1.5.0"
	if err := client.Settings.Upsert(ctx, sharedLibsVersionKey, &shared); err != nil {
		t.Fatal(err)
	}
	version, err = x.resolveLibsVersion(ctx, cfg)
	if err != nil || version != "1.5.0" {
		t.Fatalf("shared version selected %q, err = %v", version, err)
	}
	cfg.XXMIVersion = VersionPin{Pinned: "2.0.0"}
	version, err = x.resolveLibsVersion(ctx, cfg)
	if err != nil || version != "2.0.0" {
		t.Fatalf("pinned version selected %q, err = %v", version, err)
	}
}

func TestSelectLibsVersionRespectsSkippedAndVerifiedUpdates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, latest, skipped, deployed, want string
		verified                              bool
		wantChecks                            int
	}{
		{name: "verified update", latest: "2.0.0", deployed: "1.0.0", verified: true, want: "2.0.0", wantChecks: 1},
		{name: "failed update", latest: "2.0.0", deployed: "1.0.0", want: "1.0.0", wantChecks: 1},
		{name: "skipped cached update", latest: "2.0.0", skipped: "2.0.0", deployed: "1.0.0", verified: true, want: "1.0.0"},
		{name: "initial install", latest: "2.0.0", want: "2.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checks := 0
			got := selectLibsVersion(tc.latest, tc.skipped, tc.deployed, func(version string) bool {
				checks++
				if version != tc.latest {
					t.Fatalf("checked %q, want %q", version, tc.latest)
				}
				return tc.verified
			})
			if got != tc.want || checks != tc.wantChecks {
				t.Fatalf("selected %q with %d cache checks; want %q with %d", got, checks, tc.want, tc.wantChecks)
			}
		})
	}
}

func TestDeployRuntimeFilesRejectsCorruptManifest(t *testing.T) {
	t.Parallel()
	base, importer, legacy, corrupt := corruptLegacyRuntime(t)
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeLegacy}
	_, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, legacy, "legacy@abcdef123456", base, false, noGameProcess,
	)
	if err == nil || !strings.HasPrefix(err.Error(), "XXMI_RUNTIME_CORRUPTED:") {
		t.Fatalf("deploy = %v, want XXMI_RUNTIME_CORRUPTED first", err)
	}
	got, readErr := os.ReadFile(filepath.Join(importer, runtimeManifestName))
	if readErr != nil || !bytes.Equal(got, corrupt) {
		t.Fatalf("manifest = %q, err = %v, want the corrupt bytes left in place", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(importer, "d3d11.dll")); !os.IsNotExist(statErr) {
		t.Fatalf("dll deployed despite a corrupt manifest: %v", statErr)
	}
	backups, globErr := filepath.Glob(filepath.Join(base, "backups", "GIMI *", runtimeManifestName))
	if globErr != nil || len(backups) != 0 {
		t.Fatalf("strict deploy backups = %v, err = %v", backups, globErr)
	}
}

func TestDeployRuntimeFilesRepairsCorruptManifest(t *testing.T) {
	t.Parallel()
	base, importer, legacy, corrupt := corruptLegacyRuntime(t)
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeLegacy}
	warnings, err := deployRuntimeFiles(
		context.Background(), "GIMI", cfg, legacy, "legacy@abcdef123456", base, true, noGameProcess,
	)
	if err != nil {
		t.Fatal(err)
	}
	backedUp := false
	for _, warning := range warnings {
		if warning == "Backed up corrupt runtime manifest" {
			backedUp = true
		}
	}
	if !backedUp {
		t.Fatalf("warnings = %v, want the corrupt manifest backup", warnings)
	}
	backups, err := filepath.Glob(filepath.Join(base, "backups", "GIMI *", runtimeManifestName))
	if err != nil || len(backups) != 1 {
		t.Fatalf("corrupt manifest backups = %v, err = %v", backups, err)
	}
	assertFileContent(t, backups[0], string(corrupt))
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "legacy")
	assertFileContent(t, filepath.Join(importer, "3DMigoto Loader.exe"), "loader")

	data, err := os.ReadFile(filepath.Join(importer, runtimeManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest runtimeManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("rebuilt manifest: %v", err)
	}
	if manifest.Mode != RuntimeLegacy || manifest.Source != "legacy@abcdef123456" {
		t.Fatalf("manifest = %+v, want legacy@abcdef123456", manifest)
	}
	if manifest.Files["d3d11.dll"] != hashBytes([]byte("legacy")) {
		t.Fatalf("manifest files = %+v", manifest.Files)
	}
}

func corruptLegacyRuntime(t *testing.T) (base, importer, legacy string, corrupt []byte) {
	t.Helper()
	base = t.TempDir()
	importer = filepath.Join(base, "GIMI")
	legacy = filepath.Join(base, "legacy")
	for _, folder := range []string{importer, legacy} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(importer, "d3dx.ini"), []byte("[Loader]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	corrupt = []byte("{not-json")
	if err := os.WriteFile(filepath.Join(importer, runtimeManifestName), corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"3DMigoto Loader.exe": "loader", "d3d11.dll": "legacy"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := LegacyRuntimeSource{Files: map[string]string{
		"3DMigoto Loader.exe": hashBytes([]byte("loader")),
		"d3d11.dll":           hashBytes([]byte("legacy")),
	}}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "source.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return base, importer, legacy, corrupt
}
