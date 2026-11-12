package mod

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/mod/metadata"
)

type togglePresetTestSettings struct {
	testSettings
	store string
}

func (s *togglePresetTestSettings) GetTogglePresetStore(context.Context) (string, error) {
	return s.store, nil
}

func (s *togglePresetTestSettings) SetTogglePresetStore(_ context.Context, store string) error {
	s.store = store
	return nil
}

// Switching stores moves every preset and must neither lose one nor touch the rest of nhd.json.
func TestSetTogglePresetStoreMovesPresetsBothWays(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	mods := filepath.Join(root, "Mods")
	enabled := filepath.Join(mods, "Char", "Foo")
	disabled := filepath.Join(mods, "DISABLED Bar")
	for _, dir := range []string{enabled, disabled} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Genshin", mods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	fooPresets := []TogglePreset{
		{ID: "a", Name: "Summer", Source: "game", INIs: map[string]map[string]string{"foo.ini": {"Outfit": "1"}},
			CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z"},
		{ID: "b", Name: "Winter", Source: "ini", INIs: map[string]map[string]string{"sub/hat.ini": {"Hat": "0.5"}},
			CreatedAt: "2026-11-02T00:00:00Z", UpdatedAt: "2026-11-02T00:00:00Z"},
	}
	barPresets := []TogglePreset{{ID: "c", Name: "Default", Source: "viewer",
		INIs: map[string]map[string]string{"bar.ini": {"Glow": "0"}}}}
	writeNHD := func(dir string, document map[string]any) {
		t.Helper()
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := metadata.Write(dir, data); err != nil {
			t.Fatal(err)
		}
	}
	writeNHD(enabled, map[string]any{"preview": "cover.png", togglePresetsMetadataKey: fooPresets})
	writeNHD(disabled, map[string]any{togglePresetsMetadataKey: barPresets})
	orphan := db.ModTogglePresetRow{ID: "z", ModName: "Gone", Name: "Old", Source: "ini", INIs: `{"gone.ini":{}}`}
	if err := service.client.ModTogglePresets.ReplaceMod(ctx, "Genshin", "char/gone", []db.ModTogglePresetRow{
		orphan,
	}); err != nil {
		t.Fatal(err)
	}

	migration, err := service.SetTogglePresetStore(ctx, TogglePresetStoreDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if migration.Moved != 3 || migration.Mods != 2 || migration.Leftover != 0 || settings.store != "database" {
		t.Fatalf("metadata to database: %+v, store %q", migration, settings.store)
	}
	raw, err := metadata.Read(enabled)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document[togglePresetsMetadataKey]; ok || string(document["preview"]) != `"cover.png"` {
		t.Fatalf("nhd.json after the move: %s", raw)
	}
	enabledAgain := filepath.Join(mods, "Char", "DISABLED Foo")
	if err := os.Rename(enabled, enabledAgain); err != nil {
		t.Fatal(err)
	}
	list, err := service.GetTogglePresets(ctx, enabledAgain)
	if err != nil {
		t.Fatal(err)
	}
	if list.Store != TogglePresetStoreDatabase || !reflect.DeepEqual(list.Presets, fooPresets) {
		t.Fatalf("database presets of the disabled mod: %+v", list)
	}

	migration, err = service.SetTogglePresetStore(ctx, TogglePresetStoreMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if migration.Moved != 3 || !reflect.DeepEqual(migration.Orphaned, []string{"Genshin/char/gone"}) {
		t.Fatalf("database to metadata: %+v", migration)
	}
	rows, err := service.client.ModTogglePresets.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "z" {
		t.Fatalf("database rows after the move: %+v", rows)
	}
	for dir, want := range map[string][]TogglePreset{enabledAgain: fooPresets, disabled: barPresets} {
		list, err := service.GetTogglePresets(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(list.Presets, want) {
			t.Fatalf("presets of %s: %+v, want %+v", dir, list.Presets, want)
		}
	}

	if migration, err = service.SetTogglePresetStore(ctx, TogglePresetStoreMetadata); err != nil ||
		migration.Moved != 0 {
		t.Fatalf("switching to the current store: %+v, %v", migration, err)
	}
}

func writeTogglePresetFixture(t *testing.T, dir string, presets []TogglePreset) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"preview": "cover.png", togglePresetsMetadataKey: presets})
	if err != nil {
		t.Fatal(err)
	}
	if err := metadata.Write(dir, raw); err != nil {
		t.Fatal(err)
	}
}

func TestSetTogglePresetStorePreservesCopiedIDs(t *testing.T) {
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	mods := filepath.Join(root, "Mods")
	want := TogglePreset{ID: "copied", Name: "Summer", Source: "viewer",
		INIs:      map[string]map[string]string{"mod.ini": {"Outfit": "1"}},
		CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-02T00:00:00Z"}
	for _, name := range []string{"Foo", "Bar"} {
		writeTogglePresetFixture(t, filepath.Join(mods, name), []TogglePreset{want})
	}
	if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	migration, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if migration.Moved != 2 || migration.Mods != 2 || migration.Leftover != 0 {
		t.Fatalf("migration: %+v", migration)
	}
	ids := map[string]string{}
	for _, name := range []string{"Foo", "Bar"} {
		list, err := service.GetTogglePresets(t.Context(), filepath.Join(mods, name))
		if err != nil || len(list.Presets) != 1 {
			t.Fatalf("%s lost its preset: %+v, %v", name, list, err)
		}
		got := list.Presets[0]
		ids[name] = got.ID
		got.ID = want.ID
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s preset: %+v, want %+v", name, got, want)
		}
	}
	if ids["Foo"] == ids["Bar"] || (ids["Foo"] != want.ID && ids["Bar"] != want.ID) {
		t.Fatalf("expected one original and one reassigned ID: %v", ids)
	}
	for _, store := range []string{TogglePresetStoreMetadata, TogglePresetStoreDatabase, TogglePresetStoreMetadata} {
		if _, err := service.SetTogglePresetStore(t.Context(), store); err != nil {
			t.Fatal(err)
		}
		for name, id := range ids {
			list, err := service.GetTogglePresets(t.Context(), filepath.Join(mods, name))
			expected := want
			expected.ID = id
			if err != nil || !reflect.DeepEqual(list.Presets, []TogglePreset{expected}) {
				t.Fatalf("%s after round trip to %s: %+v, %v", name, store, list, err)
			}
		}
	}
}

func TestSetTogglePresetStoreIncludesJunctions(t *testing.T) {
	for _, linkedRoot := range []bool{false, true} {
		t.Run(map[bool]string{false: "mod junction", true: "root junction"}[linkedRoot], func(t *testing.T) {
			service, root := newTestMod(t, testSettings{})
			service.UseSettings(&togglePresetTestSettings{store: TogglePresetStoreMetadata})
			mods := filepath.Join(root, "Mods")
			target := filepath.Join(root, "external")
			want := TogglePreset{ID: "linked", Name: "Summer",
				INIs: map[string]map[string]string{"mod.ini": {"Outfit": "1"}}}
			path := filepath.Join(mods, "DISABLED Foo")
			if linkedRoot {
				writeTogglePresetFixture(t, filepath.Join(target, "DISABLED Foo"), []TogglePreset{want})
				createJunction(t, target, mods)
			} else {
				writeTogglePresetFixture(t, target, []TogglePreset{want})
				if err := os.MkdirAll(mods, 0o700); err != nil {
					t.Fatal(err)
				}
				createJunction(t, target, path)
			}
			if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			before, err := service.GetTogglePresets(t.Context(), path)
			if err != nil || !reflect.DeepEqual(before.Presets, []TogglePreset{want}) {
				t.Fatalf("before: %+v, %v", before, err)
			}
			migration, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
			if err != nil {
				t.Fatal(err)
			}
			after, err := service.GetTogglePresets(t.Context(), path)
			if err != nil || !reflect.DeepEqual(after.Presets, before.Presets) || migration.Moved != 1 {
				t.Fatalf("junction preset disappeared: migration %+v, list %+v, error %v", migration, after, err)
			}
			if _, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreMetadata); err != nil {
				t.Fatal(err)
			}
			after, err = service.GetTogglePresets(t.Context(), path)
			if err != nil || !reflect.DeepEqual(after.Presets, before.Presets) {
				t.Fatalf("junction round trip: %+v, %v", after, err)
			}
		})
	}
}

func TestSetTogglePresetStoreDeduplicatesLinkedRoots(t *testing.T) {
	service, root := newTestMod(t, testSettings{})
	service.UseSettings(&togglePresetTestSettings{store: TogglePresetStoreMetadata})
	mods, alias := filepath.Join(root, "Mods"), filepath.Join(root, "importer Mods")
	want := TogglePreset{ID: "a", Name: "Summer", INIs: map[string]map[string]string{}}
	path := filepath.Join(mods, "Foo Bar")
	writeTogglePresetFixture(t, path, []TogglePreset{want})
	createJunction(t, mods, alias)
	if err := service.client.GamePaths.Upsert(t.Context(), db.GamePathRow{
		Game: "Genshin", ModFolderPath: mods, LinkedModFolderPath: &alias,
	}); err != nil {
		t.Fatal(err)
	}
	migration, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err != nil || migration.Moved != 1 || migration.Mods != 1 {
		t.Fatalf("linked roots: %+v, %v", migration, err)
	}
	for _, dir := range []string{path, filepath.Join(alias, "Foo Bar")} {
		list, err := service.GetTogglePresets(t.Context(), dir)
		if err != nil || !reflect.DeepEqual(list.Presets, []TogglePreset{want}) {
			t.Fatalf("presets at %s: %+v, %v", dir, list, err)
		}
	}
}

func TestSetTogglePresetStoreRetriesRemappedIDsAfterReopen(t *testing.T) {
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	mods := filepath.Join(root, "Mods")
	want := TogglePreset{ID: "shared", Name: "Summer", Source: "ini",
		INIs: map[string]map[string]string{"mod.ini": {"Outfit": "1"}}}
	for _, name := range []string{"A", "Z"} {
		writeTogglePresetFixture(t, filepath.Join(mods, name), []TogglePreset{want})
	}
	if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	owner := db.ModTogglePresetRow{ID: want.ID, Name: "Owner", INIs: `{"original.ini":{"Outfit":"2"}}`}
	if err := service.client.ModTogglePresets.ReplaceMod(
		t.Context(),
		"Genshin",
		"owner",
		[]db.ModTogglePresetRow{owner},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := service.client.SQL().ExecContext(t.Context(), `CREATE TRIGGER fail_toggle_import
BEFORE INSERT ON mod_toggle_presets WHEN NEW.mod_key = 'z'
BEGIN SELECT RAISE(ABORT, 'copy failure'); END`); err != nil {
		t.Fatal(err)
	}
	failed, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err == nil || len(failed.Failed) != 1 || settings.store != TogglePresetStoreMetadata {
		t.Fatalf("failed migration: %+v, %v, store %s", failed, err, settings.store)
	}
	for _, name := range []string{"A", "Z"} {
		list, err := service.GetTogglePresets(t.Context(), filepath.Join(mods, name))
		if err != nil || !reflect.DeepEqual(list.Presets, []TogglePreset{want}) {
			t.Fatalf("source changed after failure: %+v, %v", list, err)
		}
	}
	first, err := service.client.ModTogglePresets.ListByMod(t.Context(), "Genshin", "a")
	if err != nil || len(first) != 1 || first[0].ID == want.ID {
		t.Fatalf("first copy: %+v, %v", first, err)
	}
	if err := service.client.Close(); err != nil {
		t.Fatal(err)
	}
	client, err := db.New(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	service.UseClient(client)
	if _, err := client.SQL().ExecContext(t.Context(), `DROP TRIGGER fail_toggle_import`); err != nil {
		t.Fatal(err)
	}
	migration, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err != nil || migration.Moved != 2 || migration.Leftover != 0 || settings.store != TogglePresetStoreDatabase {
		t.Fatalf("retry: %+v, %v, store %s", migration, err, settings.store)
	}
	again, err := client.ModTogglePresets.ListByMod(t.Context(), "Genshin", "a")
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("retry changed the copied ID or added duplicates: %+v, want %+v, %v", again, first, err)
	}
	rows, err := client.ModTogglePresets.ListAll(t.Context())
	if err != nil || len(rows) != 3 {
		t.Fatalf("destination rows: %+v, %v", rows, err)
	}
	ownerRows, err := client.ModTogglePresets.ListByMod(t.Context(), "Genshin", "owner")
	if err != nil || len(ownerRows) != 1 || ownerRows[0].ID != want.ID || ownerRows[0].INIs != owner.INIs {
		t.Fatalf("original owner changed: %+v, %v", ownerRows, err)
	}
	for _, name := range []string{"A", "Z"} {
		raw, err := metadata.Read(filepath.Join(mods, name))
		if err != nil {
			t.Fatal(err)
		}
		document, presets, err := decodeMetadataTogglePresets(raw)
		if err != nil || len(presets) != 0 || string(document["preview"]) != `"cover.png"` {
			t.Fatalf("source cleanup: %s, %v", raw, err)
		}
	}
}

func TestSetTogglePresetStoreVerifiesAllCopiesBeforeSwitch(t *testing.T) {
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	mods := filepath.Join(root, "Mods")
	want := TogglePreset{Name: "Summer", INIs: map[string]map[string]string{"mod.ini": {"Outfit": "1"}}}
	for _, name := range []string{"A", "Z"} {
		preset := want
		preset.ID = name
		writeTogglePresetFixture(t, filepath.Join(mods, name), []TogglePreset{preset})
	}
	if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.client.SQL().ExecContext(t.Context(), `CREATE TRIGGER alter_earlier_toggle_copy
AFTER INSERT ON mod_toggle_presets WHEN NEW.mod_key = 'z'
BEGIN UPDATE mod_toggle_presets SET inis = '{}' WHERE mod_key = 'a'; END`); err != nil {
		t.Fatal(err)
	}
	migration, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err == nil || len(migration.Failed) != 1 || settings.store != TogglePresetStoreMetadata {
		t.Fatalf("invalid earlier copy allowed switch: %+v, %v, store %s", migration, err, settings.store)
	}
	for _, name := range []string{"A", "Z"} {
		list, err := service.GetTogglePresets(t.Context(), filepath.Join(mods, name))
		preset := want
		preset.ID = name
		if err != nil || !reflect.DeepEqual(list.Presets, []TogglePreset{preset}) {
			t.Fatalf("source lost on verification failure: %+v, %v", list, err)
		}
	}
}

func TestSetTogglePresetStoreRejectsSharedMetadata(t *testing.T) {
	for _, store := range []string{TogglePresetStoreMetadata, TogglePresetStoreDatabase} {
		t.Run(store, func(t *testing.T) {
			service, root := newTestMod(t, testSettings{})
			settings := &togglePresetTestSettings{store: store}
			service.UseSettings(settings)
			mods, target := filepath.Join(root, "Mods"), filepath.Join(root, "external")
			want := TogglePreset{ID: "same", Name: "Summer", INIs: map[string]map[string]string{}}
			writeTogglePresetFixture(t, target, []TogglePreset{want})
			if err := os.MkdirAll(mods, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"A", "B"} {
				createJunction(t, target, filepath.Join(mods, name))
			}
			if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
			destination := TogglePresetStoreDatabase
			if store == TogglePresetStoreDatabase {
				destination = TogglePresetStoreMetadata
				for _, name := range []string{"a", "b"} {
					row := db.ModTogglePresetRow{ID: name, Name: name, INIs: `{}`}
					if err := service.client.ModTogglePresets.ReplaceMod(
						t.Context(),
						"Genshin",
						name,
						[]db.ModTogglePresetRow{row},
					); err != nil {
						t.Fatal(err)
					}
				}
			}
			_, err := service.SetTogglePresetStore(t.Context(), destination)
			if err == nil || !strings.Contains(err.Error(), "shared by") || settings.store != store {
				t.Fatalf("shared metadata accepted: %v, store %s", err, settings.store)
			}
			raw, err := metadata.Read(target)
			if err != nil {
				t.Fatal(err)
			}
			_, presets, err := decodeMetadataTogglePresets(raw)
			if err != nil || !reflect.DeepEqual(presets, []TogglePreset{want}) {
				t.Fatalf("shared metadata changed: %s, %v", raw, err)
			}
		})
	}
}

func TestMetadataDirsStopsAtAncestorJunctions(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "DISABLED Foo")
	writeTogglePresetFixture(t, root, nil)
	writeTogglePresetFixture(t, want, nil)
	writeTogglePresetFixture(t, filepath.Join(root, ".hidden", "Bar"), nil)
	createJunction(t, root, filepath.Join(want, "Loop"))
	dirs, err := metadataDirs(t.Context(), root)
	if err != nil || !reflect.DeepEqual(dirs, []string{want}) {
		t.Fatalf("metadata directories: %v, %v", dirs, err)
	}
}

func TestSetTogglePresetStoreRejectsUnavailableRoots(t *testing.T) {
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	mods := filepath.Join(root, "Mods")
	if err := os.MkdirAll(mods, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mods); err != nil {
		t.Fatal(err)
	}
	_, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err == nil || !strings.Contains(err.Error(), mods) || settings.store != TogglePresetStoreMetadata {
		t.Fatalf("unavailable root allowed switch: %v, store %s", err, settings.store)
	}
}

func TestSetTogglePresetStoreRejectsBrokenChildJunction(t *testing.T) {
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	mods, target := filepath.Join(root, "Mods"), filepath.Join(root, "external")
	want := TogglePreset{ID: "a", Name: "Summer", INIs: map[string]map[string]string{}}
	path := filepath.Join(mods, "A")
	writeTogglePresetFixture(t, path, []TogglePreset{want})
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(mods, "Z")
	createJunction(t, target, broken)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err := service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err == nil || !strings.Contains(err.Error(), broken) || settings.store != TogglePresetStoreMetadata {
		t.Fatalf("broken child junction allowed switch: %v, store %s", err, settings.store)
	}
	list, err := service.GetTogglePresets(t.Context(), path)
	if err != nil || !reflect.DeepEqual(list.Presets, []TogglePreset{want}) {
		t.Fatalf("source changed: %+v, %v", list, err)
	}
}

func TestSetTogglePresetStoreRejectsUnreadableChild(t *testing.T) {
	service, root := newTestMod(t, testSettings{})
	settings := &togglePresetTestSettings{store: TogglePresetStoreMetadata}
	service.UseSettings(settings)
	mods := filepath.Join(root, "Mods")
	path := filepath.Join(mods, "Foo")
	want := TogglePreset{ID: "a", Name: "Summer", INIs: map[string]map[string]string{}}
	writeTogglePresetFixture(t, path, []TogglePreset{want})
	if err := service.AddGame(t.Context(), "Genshin", mods, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	if _, err := os.ReadDir(path); err == nil {
		t.Fatal("exclusive directory handle did not prevent listing")
	}
	_, err = service.SetTogglePresetStore(t.Context(), TogglePresetStoreDatabase)
	if err == nil || !strings.Contains(err.Error(), path) || settings.store != TogglePresetStoreMetadata {
		t.Fatalf("unreadable child allowed switch: %v, store %s", err, settings.store)
	}
	raw, err := metadata.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	_, presets, err := decodeMetadataTogglePresets(raw)
	if err != nil || !reflect.DeepEqual(presets, []TogglePreset{want}) {
		t.Fatalf("source changed: %s, %v", raw, err)
	}
}
