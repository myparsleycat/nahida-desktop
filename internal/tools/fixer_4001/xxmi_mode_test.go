package fixer4001

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/xxmi"
)

func TestEnsureXXMIModeRejectsUnmanagedBuiltinImporters(t *testing.T) {
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
	runtime := xxmi.New()
	runtime.UseClient(client)
	if err := runtime.SetLauncherMode(ctx, xxmi.LauncherBuiltin); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{XXMI: runtime})
	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}

	err = service.ensureXXMIMode(ctx, "GIMI", folder)
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
	if err := service.ensureXXMIMode(ctx, "GIMI", folder); err != nil {
		t.Fatalf("enabled importer error = %v", err)
	}
	other := filepath.Join(t.TempDir(), "Other")
	if err := service.ensureXXMIMode(ctx, "GIMI", other); !errors.Is(err, errImporterNotConfigured) {
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
	err = service.ensureXXMIMode(ctx, "GIMI", folder)
	if code := xxmiModeFailureCode(err, "XXMI_ERR_BUILD_FAILED"); code != "XXMI_ERR_LEGACY_RUNTIME" {
		t.Fatalf("legacy importer code = %q, error = %v", code, err)
	}
	if code := xxmiModeFailureCode(errors.New("db"), "XXMI_ERR_BUILD_FAILED"); code != "XXMI_ERR_BUILD_FAILED" {
		t.Fatalf("fallback code = %q", code)
	}
}
