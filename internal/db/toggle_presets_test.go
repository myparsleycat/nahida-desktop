package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestModTogglePresetsReplaceModRollsBackCollision(t *testing.T) {
	t.Parallel()

	for _, owner := range [][2]string{{"GI", "other"}, {"HSR", "mod"}} {
		t.Run(owner[0]+"/"+owner[1], func(t *testing.T) {
			t.Parallel()

			client := mustNewTemp(t)
			ctx := context.Background()
			if err := client.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			for _, game := range []string{"GI", "HSR"} {
				if err := client.GamePaths.Insert(ctx, GamePathRow{Game: game, ModFolderPath: game}); err != nil {
					t.Fatal(err)
				}
			}
			for _, seed := range []struct {
				game, modKey, id string
			}{{"GI", "mod", "original"}, {owner[0], owner[1], "occupied"}} {
				if err := client.ModTogglePresets.ReplaceMod(ctx, seed.game, seed.modKey,
					[]ModTogglePresetRow{togglePresetTestRow(seed.id)}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := client.ModTogglePresets.ListAll(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = client.ModTogglePresets.ReplaceMod(ctx, "GI", "mod", []ModTogglePresetRow{
				togglePresetTestRow("inserted-first"), togglePresetTestRow("occupied"),
			})
			if err == nil {
				t.Error("expected ID collision to reject the replacement")
			}
			after, err := client.ModTogglePresets.ListAll(ctx)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("rows after failed replacement = %+v, want %+v; error = %v", after, before, err)
			}
		})
	}
}

func togglePresetTestRow(id string) ModTogglePresetRow {
	return ModTogglePresetRow{
		ID: id, ModName: "Mod", Name: id, Source: "ini", INIs: "{}", CreatedAt: "before", UpdatedAt: "before",
	}
}

func TestModTogglePresetsDeleteByModIDs(t *testing.T) {
	t.Parallel()

	client, _ := newTogglePresetTestClient(t)
	ctx := context.Background()
	for _, seed := range []struct {
		game, modKey, id string
	}{{"GI", "mod", "delete"}, {"GI", "other", "other-mod"}, {"HSR", "mod", "other-game"}} {
		if err := client.ModTogglePresets.ReplaceMod(ctx, seed.game, seed.modKey,
			[]ModTogglePresetRow{togglePresetTestRow(seed.id)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.ModTogglePresets.DeleteByModIDs(ctx, "GI", "mod",
		[]string{"delete", "other-mod", "other-game", "missing", "delete"}); err != nil {
		t.Fatal(err)
	}
	if err := client.ModTogglePresets.DeleteByModIDs(ctx, "GI", "other", nil); err != nil {
		t.Fatal(err)
	}
	rows, err := client.ModTogglePresets.ListAll(ctx)
	if err != nil || len(rows) != 2 || rows[0].ID != "other-mod" || rows[1].ID != "other-game" {
		t.Fatalf("remaining rows = %+v, error = %v", rows, err)
	}
}

func TestModTogglePresetsResolveImportIDsReuseAcrossReopen(t *testing.T) {
	t.Parallel()

	client, path := newTogglePresetTestClient(t)
	ctx := context.Background()
	if err := client.ModTogglePresets.ReplaceMod(ctx, "HSR", "mod",
		[]ModTogglePresetRow{togglePresetTestRow("occupied")}); err != nil {
		t.Fatal(err)
	}
	if err := client.ModTogglePresets.ReplaceMod(ctx, "GI", "mod",
		[]ModTogglePresetRow{togglePresetTestRow("owned")}); err != nil {
		t.Fatal(err)
	}
	resolved, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod",
		[]string{"occupied", "unused", "owned", "occupied"})
	if err != nil {
		t.Fatal(err)
	}
	target := resolved["occupied"]
	if len(resolved) != 3 || resolved["unused"] != "unused" || resolved["owned"] != "owned" ||
		target == "occupied" {
		t.Fatalf("resolved IDs = %v", resolved)
	}
	if _, err := uuid.Parse(target); err != nil {
		t.Fatalf("new ID = %q: %v", target, err)
	}
	state, err := client.AppState.Get(ctx, togglePresetTestImportKey(t, "GI", "mod", "occupied"))
	if err != nil || state == nil || state.Value != target {
		t.Fatalf("persisted remap = %+v, error = %v", state, err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.ModTogglePresets.DeleteByModIDs(ctx, "HSR", "mod", []string{"occupied"}); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{"occupied"}, {target}, {"occupied"}} {
		got, err := reopened.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", ids)
		if err != nil || got[ids[0]] != target {
			t.Fatalf("resolve %v after reopen = %v, want %q; error = %v", ids, got, target, err)
		}
	}
	if err := reopened.ModTogglePresets.ReplaceMod(ctx, "GI", "mod",
		[]ModTogglePresetRow{togglePresetTestRow(target)}); err != nil {
		t.Fatal(err)
	}
	got, err := reopened.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", []string{target})
	if err != nil || got[target] != target {
		t.Fatalf("roundtrip destination = %v, error = %v", got, err)
	}
}

func TestModTogglePresetsResolveImportIDsReservesPendingMappings(t *testing.T) {
	t.Parallel()

	client, _ := newTogglePresetTestClient(t)
	ctx := context.Background()
	if err := client.ModTogglePresets.ReplaceMod(ctx, "GI", "owner",
		[]ModTogglePresetRow{togglePresetTestRow("source")}); err != nil {
		t.Fatal(err)
	}
	first, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "importer", []string{"source"})
	if err != nil {
		t.Fatal(err)
	}
	target := first["source"]
	for _, owner := range [][2]string{{"GI", "other"}, {"HSR", "importer"}} {
		got, err := client.ModTogglePresets.ResolveImportIDs(ctx, owner[0], owner[1], []string{target})
		if err != nil || got[target] == target || got[target] == "" {
			t.Fatalf("pending reservation stolen by %v: %v, error = %v", owner, got, err)
		}
	}
	again, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "importer", []string{"source"})
	if err != nil || !reflect.DeepEqual(again, first) {
		t.Fatalf("pending retry = %v, want %v; error = %v", again, first, err)
	}
}

func TestModTogglePresetsResolveImportIDsInvalidatedMapping(t *testing.T) {
	t.Parallel()

	for _, owner := range [][2]string{{"GI", "other"}, {"HSR", "mod"}} {
		t.Run(owner[0]+"/"+owner[1], func(t *testing.T) {
			t.Parallel()

			client, _ := newTogglePresetTestClient(t)
			ctx := context.Background()
			if err := client.ModTogglePresets.ReplaceMod(ctx, owner[0], owner[1],
				[]ModTogglePresetRow{togglePresetTestRow("stolen")}); err != nil {
				t.Fatal(err)
			}
			key := togglePresetTestImportKey(t, "GI", "mod", "now-free")
			if err := client.AppState.Upsert(ctx, key, "stolen", "before"); err != nil {
				t.Fatal(err)
			}
			got, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", []string{"now-free"})
			if err != nil {
				t.Fatal(err)
			}
			target := got["now-free"]
			if target == "now-free" || target == "stolen" {
				t.Fatalf("invalidated mapping reused old ID: %v", got)
			}
			if _, err := uuid.Parse(target); err != nil {
				t.Fatal(err)
			}
			again, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", []string{"now-free"})
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Fatalf("invalidated mapping retry = %v, want %v; error = %v", again, got, err)
			}
			rows, err := client.ModTogglePresets.ListByMod(ctx, owner[0], owner[1])
			if err != nil || len(rows) != 1 || rows[0].ID != "stolen" {
				t.Fatalf("other owner's rows = %+v, error = %v", rows, err)
			}
		})
	}
}

func TestModTogglePresetsResolveImportIDsRequestCollisions(t *testing.T) {
	t.Parallel()

	for _, ids := range [][]string{{"source", "destination", "source"}, {"destination", "source"}} {
		t.Run(ids[0], func(t *testing.T) {
			t.Parallel()

			client, _ := newTogglePresetTestClient(t)
			ctx := context.Background()
			key := togglePresetTestImportKey(t, "GI", "mod", "source")
			if err := client.AppState.Upsert(ctx, key, "destination", "before"); err != nil {
				t.Fatal(err)
			}
			got, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", ids)
			if err != nil || len(got) != 2 || got["destination"] != "destination" ||
				got["source"] == "destination" || got["source"] == "source" || got["source"] == "" {
				t.Fatalf("request collision IDs = %v, error = %v", got, err)
			}
			again, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", ids)
			if err != nil || !reflect.DeepEqual(again, got) {
				t.Fatalf("request collision retry = %v, want %v; error = %v", again, got, err)
			}
		})
	}

	t.Run("shared mapping target", func(t *testing.T) {
		t.Parallel()

		client, _ := newTogglePresetTestClient(t)
		ctx := context.Background()
		for _, id := range []string{"first", "second"} {
			if err := client.AppState.Upsert(ctx, togglePresetTestImportKey(t, "GI", "mod", id),
				"shared", "before"); err != nil {
				t.Fatal(err)
			}
		}
		got, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", []string{"first", "second"})
		if err != nil || got["first"] != "shared" || got["second"] == "shared" || got["second"] == "second" {
			t.Fatalf("assigned collision IDs = %v, error = %v", got, err)
		}
		again, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", []string{"second", "first"})
		if err != nil || !reflect.DeepEqual(again, got) {
			t.Fatalf("assigned collision retry = %v, want %v; error = %v", again, got, err)
		}
	})
}

func TestModTogglePresetsResolveImportIDsMappingRollback(t *testing.T) {
	t.Parallel()

	client, _ := newTogglePresetTestClient(t)
	ctx := context.Background()
	if err := client.ModTogglePresets.ReplaceMod(ctx, "GI", "other", []ModTogglePresetRow{
		togglePresetTestRow("stolen"), togglePresetTestRow("second"),
	}); err != nil {
		t.Fatal(err)
	}
	firstKey := togglePresetTestImportKey(t, "GI", "mod", "first")
	secondKey := togglePresetTestImportKey(t, "GI", "mod", "second")
	if err := client.AppState.Upsert(ctx, firstKey, "stolen", "before"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SQL().ExecContext(ctx, `CREATE TRIGGER fail_toggle_import
BEFORE INSERT ON "app_state" WHEN NEW."key" = '`+secondKey+`'
BEGIN SELECT RAISE(ABORT, 'mapping failure'); END`); err != nil {
		t.Fatal(err)
	}
	got, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", []string{"first", "second"})
	if err == nil || got != nil {
		t.Fatalf("failed mapping transaction = %v, error = %v", got, err)
	}
	state, err := client.AppState.Get(ctx, firstKey)
	if err != nil || state == nil || state.Value != "stolen" || state.UpdatedAt != "before" {
		t.Fatalf("updated mapping was not rolled back: %+v, error = %v", state, err)
	}
	if state, err := client.AppState.Get(ctx, secondKey); err != nil || state != nil {
		t.Fatalf("failed mapping persisted: %+v, error = %v", state, err)
	}
	if _, err := client.SQL().ExecContext(ctx, `DROP TRIGGER fail_toggle_import`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", []string{"first", "second"}); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func TestModTogglePresetsCanceledAndEmptyRequests(t *testing.T) {
	t.Parallel()

	client, _ := newTogglePresetTestClient(t)
	ctx := context.Background()
	if err := client.ModTogglePresets.ReplaceMod(ctx, "GI", "mod",
		[]ModTogglePresetRow{togglePresetTestRow("original")}); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := client.ModTogglePresets.ResolveImportIDs(
		canceled,
		"GI",
		"mod",
		[]string{"original"},
	); !errors.Is(err, context.Canceled) ||
		got != nil {
		t.Fatalf("canceled resolution = %v, error = %v", got, err)
	}
	if err := client.ModTogglePresets.ReplaceMod(canceled, "GI", "mod", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replacement = %v", err)
	}
	if err := client.ModTogglePresets.DeleteByModIDs(
		canceled,
		"GI",
		"mod",
		[]string{"original"},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled deletion = %v", err)
	}
	rows, err := client.ModTogglePresets.ListByMod(ctx, "GI", "mod")
	if err != nil || len(rows) != 1 || rows[0].ID != "original" {
		t.Fatalf("rows after cancellation = %+v, error = %v", rows, err)
	}
	state, err := client.AppState.ListByPrefix(ctx, "toggle-preset-import-id:")
	if err != nil || len(state) != 0 {
		t.Fatalf("state after cancellation = %+v, error = %v", state, err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := client.ModTogglePresets.ResolveImportIDs(ctx, "GI", "mod", nil); err != nil || len(got) != 0 {
		t.Fatalf("empty resolution accessed closed DB: %v, error = %v", got, err)
	}
	if err := client.ModTogglePresets.DeleteByModIDs(ctx, "GI", "mod", nil); err != nil {
		t.Fatalf("empty deletion accessed closed DB: %v", err)
	}
}

func newTogglePresetTestClient(t *testing.T) (*Client, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "toggle-presets.db")
	client, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, game := range []string{"GI", "HSR"} {
		if err := client.GamePaths.Insert(ctx, GamePathRow{Game: game, ModFolderPath: game}); err != nil {
			t.Fatal(err)
		}
	}
	return client, path
}

func togglePresetTestImportKey(t *testing.T, game, modKey, sourceID string) string {
	t.Helper()

	encoded, err := json.Marshal([]string{game, modKey, sourceID})
	if err != nil {
		t.Fatal(err)
	}
	return "toggle-preset-import-id:" + base64.RawURLEncoding.EncodeToString(encoded)
}
