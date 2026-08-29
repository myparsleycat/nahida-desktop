package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
)

func useToolsTestAppData(t *testing.T, service *Tools, home string) string {
	t.Helper()
	data, err := appdata.Open(home)
	if err != nil {
		t.Fatalf("appdata.Open: %v", err)
	}
	service.UseAppData(data)
	return data.Root()
}

func openToolsTestDB(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	if err := client.Reconcile(context.Background()); err != nil {
		_ = client.Close()
		t.Fatalf("Reconcile: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestSaveScriptCompressesAndRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)

	scriptPath := filepath.Join(t.TempDir(), "fix.py")
	want := []byte("print('fixed')\n")
	if err := os.WriteFile(scriptPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.SaveScript(ctx, scriptPath); err != nil {
		t.Fatalf("SaveScript: %v", err)
	}

	scripts, err := service.GetScripts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 1 || scripts[0].Name != "fix.py" || scripts[0].Type != "python" || scripts[0].Size != int64(len(want)) {
		t.Fatalf("GetScripts = %#v", scripts)
	}
	stored, err := client.Scripts.FindByID(ctx, scripts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || !stored.IsSrcZstd || stored.ZstdSize == nil || stored.ZstdSHA256 == nil {
		t.Fatalf("stored script metadata = %#v", stored)
	}
	got, err := decompressZstd(stored.Source)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("decompressed = %q, want %q", got, want)
	}
	if err := service.SaveScript(ctx, scriptPath); err == nil || !strings.Contains(err.Error(), "same file") {
		t.Fatalf("duplicate SaveScript error = %v", err)
	}
}

func TestCreatePresetIsOrderedAndDeleteScriptProtectsReferences(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)

	for _, row := range []db.ScriptRow{
		{ID: "a", Name: "a.py", Source: []byte("a"), Type: db.ScriptTypePython, Size: 1, SHA256: "a"},
		{ID: "b", Name: "b.exe", Source: []byte("b"), Type: db.ScriptTypeExec, Size: 1, SHA256: "b"},
	} {
		if err := client.Scripts.Insert(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.CreatePreset(ctx, CreateScriptPresetInput{Name: "  repair  ", ScriptIDs: []string{"b", "a"}}); err != nil {
		t.Fatalf("CreatePreset: %v", err)
	}
	presets, err := service.GetPresets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(presets) != 1 || presets[0].Name != "repair" || len(presets[0].Scripts) != 2 {
		t.Fatalf("presets = %#v", presets)
	}
	if presets[0].Scripts[0].ScriptID != "b" || presets[0].Scripts[1].ScriptID != "a" {
		t.Fatalf("preset order = %#v", presets[0].Scripts)
	}
	if err := service.DeleteScript(ctx, "a"); err == nil || !strings.Contains(err.Error(), "repair") {
		t.Fatalf("DeleteScript referenced error = %v", err)
	}
	if err := service.DeletePreset(ctx, presets[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteScript(ctx, "a"); err != nil {
		t.Fatalf("DeleteScript after preset deletion: %v", err)
	}
}

func TestCreatePresetDoesNotLeavePartialPreset(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)

	err := service.CreatePreset(ctx, CreateScriptPresetInput{Name: "broken", ScriptIDs: []string{"missing"}})
	if err == nil {
		t.Fatal("CreatePreset unexpectedly succeeded")
	}
	presets, listErr := service.GetPresets(ctx)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(presets) != 0 {
		t.Fatalf("partial presets remain: %#v", presets)
	}
}
