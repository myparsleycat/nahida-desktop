package xxmi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

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
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, libs, "xxmi-libs@1", base); err != nil {
		t.Fatal(err)
	}
	if err := validateDeployedRuntime(importer, RuntimeXXMI); err != nil {
		t.Fatal(err)
	}
	cfg.Mode = RuntimeLegacy
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, legacy, "legacy@abc", base); err != nil {
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
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, libs, "xxmi-libs@1", base); err != nil {
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
	if _, err := deployRuntimeFiles(context.Background(), "GIMI", cfg, libs, "xxmi-libs@1", base); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importer, "d3d11.dll"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateDeployedRuntime(importer, RuntimeXXMI); err == nil {
		t.Fatal("changed DLL passed runtime validation")
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
	warnings, err := deployRuntimeFiles(context.Background(), "GIMI", cfg, libs, "xxmi-libs@1", base)
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
	warnings, err = deployRuntimeFiles(context.Background(), "GIMI", cfg, libs, "xxmi-libs@2", base)
	if err != nil || len(warnings) == 0 {
		t.Fatalf("repeat deployment warnings = %v, error = %v", warnings, err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer")
	cfg.Migoto.UnsafeMode = false
	if _, err := deployRuntimeFiles(context.Background(), "GIMI", cfg, libs, "xxmi-libs@2", base); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "xxmi")
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
