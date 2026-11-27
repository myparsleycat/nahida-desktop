package modbisect

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/xxmi"
)

func TestListBisectOrphansSkipsUnaddressableFolders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	orphan := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(bisectDisabledPath(orphan), []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	extended := `\\?\` + root
	if err := os.Mkdir(extended+`\Loading...`, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(extended) })

	paths, err := listBisectOrphans(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != orphan {
		t.Fatalf("orphans = %v", paths)
	}
}

func TestCancelledBisectRecoveryLeavesOrphansForNextLaunch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service, _, _ := newBisectTestService(t, root)
	original := filepath.Join(root, "mod.ini")
	disabled := bisectDisabledPath(original)
	if err := os.WriteFile(disabled, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listBisectOrphans(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled directory walk = %v", err)
	}
	if restored, _ := service.recoverINIs(ctx, []string{original}); restored != 0 {
		t.Fatal("cancelled recovery changed files")
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatal("cancelled recovery lost the pending INI")
	}
	if err := service.RecoverBisects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(original); err != nil || string(content) != "preserve" {
		t.Fatalf("next launch did not restore INI: %q, %v", content, err)
	}
}

func TestRecoverBisectsRestoresMarkedSessionAfterCrash(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBisectINIs(t, root, 4)
	crashed, client, _ := newBisectTestService(t, root)
	if err := client.AppState.Upsert(ctx, legacyScanDoneKey, "1", appStateTimestamp()); err != nil {
		t.Fatal(err)
	}
	started, err := crashed.BisectStart(ctx, bisectTestGameID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if marker, err := client.AppState.GetValue(ctx, sessionMarkerPrefix+bisectTestGameID); err != nil || marker == nil {
		t.Fatalf("session marker for %s = %v, %v", bisectTestGameID, marker, err)
	}
	if err := client.GamePaths.Rename(ctx, bisectTestGameID, "Renamed"); err != nil {
		t.Fatal(err)
	}

	next := NewWithOptions(Options{})
	next.UseClient(client)
	if err := next.RecoverBisects(ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range started.CurrentBatch {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("crashed session INI not restored: %s: %v", path, err)
		}
	}
	assertNoSessionMarker(t, next)
}

func TestRecoverD3dxBackupsAfterRename(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client := openToolsTestDB(t)
	runtime := xxmi.New()
	runtime.UseClient(client)
	mode := string(xxmi.LauncherBuiltin)
	if err := client.Settings.Upsert(ctx, "xxmi_launcher_mode", &mode); err != nil {
		t.Fatal(err)
	}
	importerKey := "GIMI"
	importerFolder := t.TempDir()
	config, err := json.Marshal(xxmi.ImporterConfig{Enabled: true, ImporterFolder: importerFolder})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.XXMIImporters.Upsert(ctx, importerKey, string(config)); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{XXMI: runtime})
	service.UseClient(client)
	appRoot := useToolsTestAppData(t, service, t.TempDir())
	t.Cleanup(func() {
		if err := service.stopD3dxGuardLocked(bisectTestGameID); err != nil {
			t.Error(err)
		}
	})

	legacyName := "Original/Name"
	game := db.GamePathRow{
		ID: bisectTestGameID, Game: legacyName, LegacyName: &legacyName, Importer: &importerKey,
		ModFolderPath: t.TempDir(),
	}
	if err := client.GamePaths.Insert(ctx, game); err != nil {
		t.Fatal(err)
	}
	game.Game = "Renamed"
	if err := client.GamePaths.Rename(ctx, game.ID, game.Game); err != nil {
		t.Fatal(err)
	}

	d3dxPath := filepath.Join(importerFolder, "d3dx_user.ini")
	backupDir := filepath.Join(appRoot, appdata.ModBisectDir)
	legacyPath := filepath.Join(backupDir, "Original_Name.d3dx_user.ini.bak")
	uuidDir := filepath.Join(backupDir, "by-id")
	uuidPath := filepath.Join(uuidDir, bisectTestGameID+".d3dx_user.ini.bak")
	if err := os.MkdirAll(uuidDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d3dxPath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy original"), 0o600); err != nil {
		t.Fatal(err)
	}

	locked, err := os.Open(d3dxPath)
	if err != nil {
		t.Fatal(err)
	}
	startErr := service.startD3dxGuardLocked(ctx, game)
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}
	if startErr == nil {
		t.Fatal("guard started with a legacy backup that could not be restored")
	}
	if content, err := os.ReadFile(legacyPath); err != nil || string(content) != "legacy original" {
		t.Fatalf("pending legacy backup = %q, %v", content, err)
	}
	if _, err := os.Lstat(uuidPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("UUID backup after failed legacy restoration: %v", err)
	}

	if err := service.RecoverBisects(ctx); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(d3dxPath); err != nil || string(content) != "legacy original" {
		t.Fatalf("legacy recovery restored %q, %v", content, err)
	}
	if _, err := os.Lstat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy backup after recovery: %v", err)
	}

	if err := os.WriteFile(uuidPath, []byte("UUID original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.startD3dxGuardLocked(ctx, game); err == nil {
		t.Fatal("guard started with both UUID and legacy backups pending")
	}
	for path, want := range map[string]string{legacyPath: "legacy original", uuidPath: "UUID original"} {
		if content, err := os.ReadFile(path); err != nil || string(content) != want {
			t.Fatalf("conflicting backup %q = %q, %v", path, content, err)
		}
	}
	if err := os.Remove(legacyPath); err != nil {
		t.Fatal(err)
	}
	if err := service.RecoverBisects(ctx); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(d3dxPath); err != nil || string(content) != "UUID original" {
		t.Fatalf("UUID recovery restored %q, %v", content, err)
	}
	if _, err := os.Lstat(uuidPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("UUID backup after recovery: %v", err)
	}
}

func TestD3dxLegacyBackupPathIgnoresUnaddressableNames(t *testing.T) {
	t.Parallel()

	service := New()
	useToolsTestAppData(t, service, t.TempDir())
	for _, name := range []string{"CON", "LPT1.backup", "control\x01", strings.Repeat("a", 255)} {
		if path, err := service.d3dxLegacyBackupPath(name); path != "" || err != nil {
			t.Fatalf("legacy backup for %q = %q, %v", name, path, err)
		}
	}
}

func TestRecoverBisectsSurvivesMissingModFolder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBisectINIs(t, root, 4)
	crashed, client, _ := newBisectTestService(t, root)
	missing := filepath.Join(t.TempDir(), "unplugged")
	goneID := "543fcc09-6549-41c9-b3a7-64cba952e805"
	if err := client.GamePaths.Insert(
		ctx,
		db.GamePathRow{ID: goneID, Game: "gone", ModFolderPath: missing},
	); err != nil {
		t.Fatal(err)
	}
	if err := crashed.writeSessionMarker(ctx, goneID, missing, nil); err != nil {
		t.Fatal(err)
	}
	started, err := crashed.BisectStart(ctx, bisectTestGameID, nil)
	if err != nil {
		t.Fatal(err)
	}

	next := NewWithOptions(Options{})
	next.UseClient(client)
	if err := next.RecoverBisects(ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range started.CurrentBatch {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("INI not restored past a missing folder: %s: %v", path, err)
		}
	}
	if done, err := client.AppState.GetValue(ctx, legacyScanDoneKey); err != nil || done == nil {
		t.Fatalf("legacy walk not recorded as done: %v, %v", done, err)
	}
	markers, err := next.sessionMarkers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 1 || markers[0].gameID != goneID {
		t.Fatalf("markers = %#v, want only the unreachable root kept", markers)
	}
}

func assertNoSessionMarker(t *testing.T, service *Service) {
	t.Helper()
	markers, err := service.sessionMarkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 0 {
		t.Fatalf("session markers left: %#v", markers)
	}
}

func TestBisectStartRejectedDuringRecovery(t *testing.T) {
	t.Parallel()
	service, _, _ := newBisectTestService(t, t.TempDir())
	service.bisectRecovering = true
	if _, err := service.BisectStart(context.Background(), bisectTestGameID, nil); err == nil {
		t.Fatal("bisect started while recovery was marked in progress")
	}
}
