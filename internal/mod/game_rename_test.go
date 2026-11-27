package mod

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestRenameGameWhenImporterAndModsFolderAreUnavailable(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	service, root := newTestMod(t, testSettings{})
	importerRoot := filepath.Join(root, "GIMI")
	otherRoot := filepath.Join(root, "Other")
	for _, dir := range []string{importerRoot, otherRoot} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	importer := "GIMI"
	service.xxmi = compressionImporterSource{{Key: importer, ImporterFolder: importerRoot}}
	if err := service.AddGame(ctx, "Game", "", &importer, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Other", otherRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	gameID := testGameID(t, service, "Game")
	modPath := filepath.Join(importerRoot, "Mods")
	if err := os.Remove(modPath); err != nil {
		t.Fatal(err)
	}
	service.xxmi = nil

	for _, tc := range []struct {
		name string
		want error
	}{
		{name: "  ", want: ErrInvalidGameName},
		{name: " Other ", want: ErrDuplicateGameName},
		{name: "Game"},
		{name: " game "},
	} {
		if err := service.RenameGame(ctx, gameID, tc.name); !errors.Is(err, tc.want) {
			t.Fatalf("rename to %q: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := service.RenameGame(ctx, "game", "Different"); !errors.Is(err, ErrGameNotFound) {
		t.Fatalf("rename with display name: %v", err)
	}
	row, err := service.client.GamePaths.GetByID(ctx, gameID)
	if err != nil || row == nil || row.Game != "game" || row.ModFolderPath != modPath ||
		row.Importer == nil || *row.Importer != importer {
		t.Fatalf("game after rename: %#v, %v", row, err)
	}
}

func TestRenameGameKeepsReferencesWhenPreviousNameIsReused(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	oldMods, newMods := filepath.Join(root, "Mods"), filepath.Join(root, "NewMods")
	group, newGroup := filepath.Join(oldMods, "Diluc"), filepath.Join(newMods, "Diluc")
	manual, newManual := filepath.Join(group, "Costume"), filepath.Join(newGroup, "Costume")
	modPath := filepath.Join(group, "Outfit")
	writeFile(t, filepath.Join(modPath, "mod.ini"), "[Constants]\nglobal persist $outfit = 1")
	for _, dir := range []string{manual, newManual} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", oldMods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	gameID := testGameID(t, service, "Game")
	if err := service.SetLastGame(ctx, gameID); err != nil {
		t.Fatal(err)
	}
	if err := service.SetManualSubGroup(ctx, manual, true); err != nil {
		t.Fatal(err)
	}
	preset, err := service.CreatePreset(ctx, gameID, "Default", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	classification, err := service.SaveClassification(
		ctx, gameID, nil, "Element", []ClassificationGroup{{Name: "Pyro"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	bucketID := classification.Groups[0].ID
	if err := service.SetCharacterClassification(ctx, manual, classification.ID, &bucketID); err != nil {
		t.Fatal(err)
	}
	toggle := TogglePreset{ID: "outfit", Name: "Outfit", INIs: map[string]map[string]string{"mod.ini": {"outfit": "1"}}}
	writeTogglePresetFixture(t, modPath, []TogglePreset{toggle})
	if result, err := service.SetTogglePresetStore(ctx, TogglePresetStoreDatabase); err != nil || result.Moved != 1 {
		t.Fatalf("store toggle preset: %+v, %v", result, err)
	}

	if err := service.RenameGame(ctx, gameID, " Renamed "); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GameIDByName(ctx, "Game"); !errors.Is(err, ErrGameNotFound) {
		t.Fatalf("previous name lookup after rename: %v", err)
	}
	if err := service.AddGame(ctx, "Game", newMods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	newGameID := testGameID(t, service, "Game")
	if newGameID == gameID {
		t.Fatalf("reusing the name reused ID %q", gameID)
	}

	if err := service.client.Close(); err != nil {
		t.Fatal(err)
	}
	client, err := db.New(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	service = NewWithOptions(Options{Settings: settings})
	service.UseClient(client)

	if got := testGameID(t, service, "Renamed"); got != gameID {
		t.Fatalf("renamed game ID = %q, want %q", got, gameID)
	}
	if got, err := service.GetLastGame(ctx); err != nil || got == nil || *got != gameID {
		t.Fatalf("last game = %v, %v", got, err)
	}
	if got, err := service.GetPresets(ctx, gameID); err != nil || len(got) != 1 ||
		got[0].ID != preset.ID || got[0].GameID != gameID || got[0].Game != "Renamed" {
		t.Fatalf("presets after restart = %+v, %v", got, err)
	}
	if applied, err := service.ApplyPreset(ctx, preset.ID); err != nil || len(applied.Skipped) != 1 {
		t.Fatalf("apply existing preset = %+v, %v", applied, err)
	}
	if got, err := service.GetClassifications(ctx, gameID); err != nil || len(got) != 1 ||
		got[0].ID != classification.ID || got[0].GameID != gameID || got[0].Game != "Renamed" {
		t.Fatalf("classifications after restart = %+v, %v", got, err)
	}
	if got, err := service.GetManualSubGroups(ctx, group, nil); err != nil || len(got) != 1 ||
		!samePath(got[0].Path, manual) || got[0].Classifications[classification.ID] != bucketID {
		t.Fatalf("manual subgroups after restart = %+v, %v", got, err)
	}
	if got, err := service.GetManualSubGroups(ctx, newGroup, nil); err != nil || len(got) != 0 {
		t.Fatalf("new game's manual subgroups = %+v, %v", got, err)
	}
	if got, err := service.GetTogglePresets(ctx, modPath); err != nil || len(got.Presets) != 1 ||
		got.Presets[0].ID != toggle.ID {
		t.Fatalf("toggle presets after restart = %+v, %v", got, err)
	}
	if target, err := service.ResolveDownloadTarget(ctx, "Diluc", &gameID); err != nil || target == nil ||
		target.GameID != gameID || target.Game != "Renamed" || !samePath(target.Group.Path, group) {
		t.Fatalf("download target after restart = %+v, %v", target, err)
	}
	if err := service.SetLastGame(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := service.GetLastGame(ctx); err != nil || (got != nil && *got != "") {
		t.Fatalf("cleared last game = %v, %v", got, err)
	}
}
