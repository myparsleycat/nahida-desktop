package fixer4001

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/xxmi"
)

func newBuiltinXXMI(t *testing.T, ctx context.Context) (*xxmi.XXMI, *db.Client) {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	runtime := xxmi.New()
	appData, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime.UseAppData(appData)
	runtime.UseClient(client)
	if err := runtime.SetLauncherMode(ctx, xxmi.LauncherBuiltin); err != nil {
		t.Fatal(err)
	}
	return runtime, client
}

func TestDiversifyRollsBackWhenRuntimeAdoptionFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runtime, client := newBuiltinXXMI(t, ctx)
	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(folder, targetD3D11DLL)
	original := "original PE fixture"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := xxmi.DefaultImporterConfig("GIMI", filepath.Dir(folder))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.Upsert(ctx, "GIMI", string(data)); err != nil {
		t.Fatal(err)
	}
	// An unreadable runtime manifest makes the adoption fail after the DLL was replaced.
	if err := os.WriteFile(filepath.Join(folder, ".nahida-runtime.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{XXMI: runtime, PEDiversifier: fakePEDiversifier{}})

	result := service.FourThousandOneFixerDiversifyDllPadding(
		ctx,
		Fixer4001ImporterInput{ImporterKey: "GIMI", ImporterPath: &folder},
	)
	if result.Success || result.BackupPath != nil {
		t.Fatalf("diversify result = %#v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != original {
		t.Fatalf("target = %q, %v; want the original DLL back", got, err)
	}
	backups, err := filepath.Glob(filepath.Join(folder, diversifierBackupPre+"*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("backups = %q, %v", backups, err)
	}
	stored, err := runtime.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.Migoto.UnsafeMode {
		t.Fatalf("unsafe mode = %t, %v", stored.Migoto.UnsafeMode, err)
	}
}

func TestEnsureXXMIModeRejectsUnmanagedBuiltinImporters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runtime, client := newBuiltinXXMI(t, ctx)
	service := NewWithOptions(Options{XXMI: runtime})
	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := service.ensureXXMIMode(ctx, "GIMI", folder)
	if !errors.Is(err, errImporterNotConfigured) {
		t.Fatalf("disabled importer error = %v", err)
	}
	if code := xxmiModeFailureCode(err, "XXMI_ERR_BUILD_FAILED"); code != "XXMI_ERR_IMPORTER_NOT_CONFIGURED" {
		t.Fatalf("disabled importer code = %q", code)
	}

	// Seed the fixture directly: runtime mode switches inspect real game processes.
	cfg, err := xxmi.DefaultImporterConfig("GIMI", filepath.Dir(folder))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.Upsert(ctx, "GIMI", string(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ensureXXMIMode(ctx, "GIMI", folder); err != nil {
		t.Fatalf("enabled importer error = %v", err)
	}
	other := filepath.Join(t.TempDir(), "Other")
	if _, err := service.ensureXXMIMode(ctx, "GIMI", other); !errors.Is(err, errImporterNotConfigured) {
		t.Fatalf("mismatched folder error = %v", err)
	}

	cfg.Mode = xxmi.RuntimeLegacy
	data, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.Upsert(ctx, "GIMI", string(data)); err != nil {
		t.Fatal(err)
	}
	_, err = service.ensureXXMIMode(ctx, "GIMI", folder)
	if code := xxmiModeFailureCode(err, "XXMI_ERR_BUILD_FAILED"); code != "XXMI_ERR_LEGACY_RUNTIME" {
		t.Fatalf("legacy importer code = %q, error = %v", code, err)
	}
	if code := xxmiModeFailureCode(errors.New("db"), "XXMI_ERR_BUILD_FAILED"); code != "XXMI_ERR_BUILD_FAILED" {
		t.Fatalf("fallback code = %q", code)
	}
}
