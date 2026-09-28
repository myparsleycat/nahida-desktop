package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	cfg.Mode = RuntimeLegacy
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, legacy, "legacy@abc", base); err != nil {
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
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "xxmi")
	if _, err := os.Stat(filepath.Join(importer, "nvapi64.dll")); !os.IsNotExist(err) {
		t.Fatalf("deprecated nvapi still deployed: %v", err)
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
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer")
	warnings, err = deployRuntimeFiles(context.Background(), "GIMI", cfg, libs, "xxmi-libs@2", base)
	if err != nil || len(warnings) == 0 {
		t.Fatalf("repeat deployment warnings = %v, error = %v", warnings, err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer")
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, err = %v, want %q", path, data, err, want)
	}
}
