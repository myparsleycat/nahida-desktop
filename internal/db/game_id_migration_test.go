package db

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestReconcileMigratesGameIDsOnce(t *testing.T) {
	t.Parallel()
	client := seedLegacyGameIDs(t)
	ctx := t.Context()
	if err := client.exec(ctx, `PRAGMA foreign_keys = OFF;
INSERT INTO "mod_presets" ("id", "game", "name") VALUES ('orphan', 'Missing', 'Orphan');
INSERT INTO "mod_preset_items" VALUES ('orphan', 'mod', 'a', '', 'A', 1, 0), ('gone', 'mod', 'b', '', 'B', 1, 0);
INSERT INTO "mod_classification_groups" ("id", "classification_id", "name") VALUES ('stray', 'gone', 'Stray');
PRAGMA foreign_keys = ON;`); err != nil {
		t.Fatalf("seed orphaned rows: %v", err)
	}
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("migrate legacy games: %v", err)
	}
	var orphans int
	if err := client.db.QueryRowContext(ctx, `SELECT
    (SELECT COUNT(*) FROM "mod_presets" WHERE "id" = 'orphan') +
    (SELECT COUNT(*) FROM "mod_preset_items" WHERE "preset_id" IN ('orphan', 'gone')) +
    (SELECT COUNT(*) FROM "mod_classification_groups" WHERE "id" = 'stray')`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("orphaned rows after migration = %d, %v", orphans, err)
	}

	games, err := client.GamePaths.List(ctx)
	if err != nil || len(games) != 4 {
		t.Fatalf("migrated games = %+v, %v", games, err)
	}
	names := []string{"Other", "case", " Case ", "Case"}
	orders := []int64{5, 11, 11, 11}
	rowIDs := []int64{25, 2, 11, 17}
	ids := make(map[string]string, len(games))
	for i, row := range games {
		var rowID int64
		if err := client.db.QueryRowContext(ctx, `SELECT rowid FROM "game_paths" WHERE "id" = ?`, row.ID).
			Scan(&rowID); err != nil {
			t.Fatal(err)
		}
		if row.Game != names[i] || row.LegacyName == nil || *row.LegacyName != names[i] ||
			row.Order != orders[i] || rowID != rowIDs[i] {
			t.Fatalf("game %d = %+v (rowid %d), want %q (order %d, rowid %d)", i, row, rowID,
				names[i], orders[i], rowIDs[i])
		}
		if id, err := uuid.Parse(row.ID); err != nil || id.Version() != 4 {
			t.Fatalf("migrated game %q has invalid UUID %q: %v", row.Game, row.ID, err)
		}
		ids[row.Game] = row.ID
	}
	game := games[2]
	if game.ModFolderPath != "C:/mods" || game.Importer == nil || *game.Importer != "GIMI" ||
		game.LinkedModFolderPath == nil || *game.LinkedModFolderPath != "C:/linked" ||
		game.GameInstallPath == nil || *game.GameInstallPath != "C:/game" ||
		game.GameExecutablePath == nil || *game.GameExecutablePath != "C:/game/play.exe" {
		t.Fatalf("migrated game paths = %+v", game)
	}

	prefs, err := client.Settings.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pref := range prefs {
		if pref.Value == nil {
			t.Fatalf("migrated preference %q is NULL", pref.Key)
		}
		switch pref.Key {
		case legacyLastGameKey:
			if *pref.Value != game.ID {
				t.Fatalf("last game = %q, want %q", *pref.Value, game.ID)
			}
		case legacyManualSubGroupsKey:
			var groups map[string][]string
			if err := json.Unmarshal([]byte(*pref.Value), &groups); err != nil || len(groups) != 3 ||
				!slices.Equal(groups[game.ID], []string{"main/hero"}) ||
				!slices.Equal(groups[ids["Case"]], []string{"main/other"}) ||
				!slices.Equal(groups["Deleted"], []string{"old"}) {
				t.Fatalf("manual subgroup mapping = %v, %v", groups, err)
			}
		case legacyBackupExclusionsKey:
			var excluded []string
			if err := json.Unmarshal([]byte(*pref.Value), &excluded); err != nil ||
				!slices.Equal(excluded, []string{game.ID, ids["case"], "Deleted"}) {
				t.Fatalf("backup exclusions = %v, %v", excluded, err)
			}
		}
	}
	if len(prefs) != 3 {
		t.Fatalf("migrated preferences = %+v", prefs)
	}
	backup, err := client.BackupCommitted.All(ctx)
	if err != nil || len(backup) != 1 || backup[0].TargetKey != "game: Case " {
		t.Fatalf("committed backup keys = %+v, %v", backup, err)
	}

	const marker = `{"modRootPath":"C:/mods","paths":["a.ini"]}`
	for _, match := range []struct{ key, value, updatedAt string }{
		{legacyBisectMarkerPrefix + game.ID, marker, "mark"},
		{legacyBisectMarkerPrefix + "Deleted", marker, "orphan mark"},
		{legacyToggleImportPrefix + legacyImportReservation(game.ID, "mod", "source"), "destination", "reservation"},
		{legacyToggleImportPrefix + "?", "keep malformed base64", "bad base64"},
		{legacyToggleImportPrefix + base64.RawURLEncoding.EncodeToString([]byte(`[null,"mod","source"]`)),
			"keep malformed owner", "bad owner"},
	} {
		row, err := client.AppState.Get(ctx, match.key)
		if err != nil || row == nil || row.Value != match.value || row.UpdatedAt != match.updatedAt {
			t.Fatalf("app_state[%q] = %+v, %v", match.key, row, err)
		}
	}
	for _, key := range []string{
		legacyBisectMarkerPrefix + " Case ",
		legacyToggleImportPrefix + legacyImportReservation(" Case ", "mod", "source"),
		legacyToggleImportPrefix + legacyImportReservation("Deleted", "mod", "source"),
	} {
		if row, err := client.AppState.Get(ctx, key); err != nil || row != nil {
			t.Fatalf("obsolete app_state[%q] = %+v, %v", key, row, err)
		}
	}

	assertMigratedGameRelations(t, client, game.ID, 5)
	state, err := client.SchemaState.Get(ctx, SchemaKeyGameIDs)
	if err != nil || state == nil || state.Value != "1" {
		t.Fatalf("game ID migration state = %+v, %v", state, err)
	}
	if err := client.GamePaths.Rename(ctx, game.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	after, err := client.GamePaths.GetByGame(ctx, "Renamed")
	if err != nil || after == nil || after.ID != game.ID || after.LegacyName == nil || *after.LegacyName != game.Game {
		t.Fatalf("game after second reconcile = %+v, %v", after, err)
	}
	stateAfter, err := client.SchemaState.Get(ctx, SchemaKeyGameIDs)
	if err != nil || stateAfter == nil || *stateAfter != *state {
		t.Fatalf("migration state changed from %+v to %+v, %v", state, stateAfter, err)
	}
	resolved, err := client.ModTogglePresets.ResolveImportIDs(ctx, game.ID, "mod", []string{"source"})
	if err != nil || resolved["source"] != "destination" {
		t.Fatalf("retry imported toggle preset = %+v, %v", resolved, err)
	}

	assertMigratedGameRelations(t, client, game.ID, 5)
	if err := client.GamePaths.Delete(ctx, game.ID); err != nil {
		t.Fatal(err)
	}
	assertMigratedGameRelations(t, client, game.ID, 0)
}

func TestReconcileRollsBackGameIDMigration(t *testing.T) {
	t.Parallel()
	client := seedLegacyGameIDs(t)
	ctx := t.Context()
	if err := client.exec(ctx, `CREATE TRIGGER "reject_game_id_marker" BEFORE INSERT ON "_schema_state"
WHEN NEW."key" = 'game_ids' BEGIN SELECT RAISE(ABORT, 'test prevents the migration marker'); END`); err != nil {
		t.Fatal(err)
	}
	if err := client.Reconcile(ctx); err == nil || !strings.Contains(err.Error(), "record game ID migration") ||
		!strings.Contains(err.Error(), "test prevents the migration marker") {
		t.Fatalf("migration failure = %v", err)
	}

	if columnExists(t, client, "game_paths", "id") || columnExists(t, client, "mod_presets", "game_id") {
		t.Fatal("failed migration left a rebuilt table")
	}
	var game, childGame string
	if err := client.db.QueryRowContext(ctx, `SELECT g."game", p."game" FROM "game_paths" g
JOIN "mod_presets" p ON p."game" = g."game" WHERE p."id" = 'preset'`).Scan(&game, &childGame); err != nil ||
		game != " Case " || childGame != game {
		t.Fatalf("original relationship after rollback = %q, %q, %v", game, childGame, err)
	}
	if value, err := client.Settings.GetValue(ctx, legacyLastGameKey); err != nil || value == nil || *value != game {
		t.Fatalf("last game after rollback = %v, %v", value, err)
	}
	for _, key := range []string{
		legacyBisectMarkerPrefix + game,
		legacyToggleImportPrefix + legacyImportReservation(game, "mod", "source"),
		legacyToggleImportPrefix + legacyImportReservation("Deleted", "mod", "source"),
	} {
		if row, err := client.AppState.Get(ctx, key); err != nil || row == nil {
			t.Fatalf("app_state[%q] after rollback = %+v, %v", key, row, err)
		}
	}
	if state, err := client.SchemaState.Get(ctx, SchemaKeyGameIDs); err != nil || state != nil {
		t.Fatalf("migration state after rollback = %+v, %v", state, err)
	}
	var foreignKeys int
	if err := client.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys after rollback = %d, %v", foreignKeys, err)
	}

	if err := client.exec(ctx, `DROP TRIGGER "reject_game_id_marker"`); err != nil {
		t.Fatal(err)
	}
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("retry migration: %v", err)
	}
	row, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil || row == nil {
		t.Fatalf("game after retry = %+v, %v", row, err)
	}
	assertMigratedGameRelations(t, client, row.ID, 5)
}

// An older version rebuilds the tables without game IDs but keeps the migration marker a newer one recorded.
func TestReconcileMigratesGameIDsAfterOlderVersionRebuild(t *testing.T) {
	t.Parallel()
	client := seedLegacyGameIDs(t)
	ctx := t.Context()
	if err := client.exec(
		ctx,
		`INSERT INTO "_schema_state" VALUES ('game_ids', '1', '2026-11-28T05:22:44Z')`,
	); err != nil {
		t.Fatal(err)
	}

	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile legacy tables with a stale marker: %v", err)
	}
	row, err := client.GamePaths.GetByGame(ctx, " Case ")
	if err != nil || row == nil {
		t.Fatalf("game after reconcile = %+v, %v", row, err)
	}
	if _, err := uuid.Parse(row.ID); err != nil {
		t.Fatalf("game ID %q: %v", row.ID, err)
	}
	assertMigratedGameRelations(t, client, row.ID, 5)
}

func assertMigratedGameRelations(t *testing.T, client *Client, gameID string, want int) {
	t.Helper()
	var count int
	err := client.db.QueryRowContext(t.Context(), `SELECT
    (SELECT COUNT(*) FROM "mod_presets" WHERE "id" = 'preset' AND "game_id" = ?) +
    (SELECT COUNT(*) FROM "mod_preset_items" WHERE "preset_id" = 'preset' AND "mod_key" = 'mod') +
    (SELECT COUNT(*) FROM "mod_toggle_presets" WHERE "id" = 'toggle' AND "game_id" = ?) +
    (SELECT COUNT(*) FROM "mod_classifications" WHERE "id" = 'classification' AND "game_id" = ?) +
    (SELECT COUNT(*) FROM "mod_classification_groups" WHERE "id" = 'group' AND "classification_id" = 'classification')`,
		gameID, gameID, gameID).Scan(&count)
	if err != nil || count != want {
		t.Fatalf("linked game rows = %d, %v, want %d", count, err, want)
	}
}

func legacyImportReservation(game, modKey, sourceID string) string {
	encoded, _ := json.Marshal([3]string{game, modKey, sourceID})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func seedLegacyGameIDs(t *testing.T) *Client {
	t.Helper()
	client := mustNewTemp(t)
	ctx := t.Context()
	const schema = `
CREATE TABLE "game_paths" (
    "game" TEXT PRIMARY KEY NOT NULL, "modFolderPath" TEXT NOT NULL, "importer" TEXT, "linkedModFolderPath" TEXT,
    "gameInstallPath" TEXT, "gameExecutablePath" TEXT, "nteLauncherPath" TEXT, "order" INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE "mod_presets" (
    "id" TEXT PRIMARY KEY NOT NULL, "game" TEXT NOT NULL, "name" TEXT NOT NULL, "description" TEXT,
    "item_count" INTEGER NOT NULL DEFAULT 0, "created_at" TEXT NOT NULL DEFAULT '', "updated_at" TEXT NOT NULL DEFAULT '',
    "version" INTEGER NOT NULL DEFAULT 1, FOREIGN KEY ("game") REFERENCES "game_paths" ("game") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "mod_presets_game_name_idx" ON "mod_presets" ("game", "name");
CREATE TABLE "mod_preset_items" (
    "preset_id" TEXT NOT NULL, "mod_key" TEXT NOT NULL, "relative_path" TEXT NOT NULL, "group_relative_path" TEXT NOT NULL,
    "folder_name" TEXT NOT NULL, "is_enabled" INTEGER NOT NULL, "item_order" INTEGER NOT NULL,
    PRIMARY KEY ("preset_id", "mod_key"), FOREIGN KEY ("preset_id") REFERENCES "mod_presets" ("id") ON DELETE CASCADE
);
CREATE TABLE "mod_toggle_presets" (
    "id" TEXT PRIMARY KEY NOT NULL, "game" TEXT NOT NULL, "mod_key" TEXT NOT NULL, "mod_name" TEXT NOT NULL,
    "name" TEXT NOT NULL, "source" TEXT NOT NULL DEFAULT '', "inis" TEXT NOT NULL DEFAULT '{}',
    "created_at" TEXT NOT NULL DEFAULT '', "updated_at" TEXT NOT NULL DEFAULT '',
    FOREIGN KEY ("game") REFERENCES "game_paths" ("game") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "mod_toggle_presets_mod_name_idx" ON "mod_toggle_presets" ("game", "mod_key", "name");
CREATE TABLE "mod_classifications" (
    "id" TEXT PRIMARY KEY NOT NULL, "game" TEXT NOT NULL, "name" TEXT NOT NULL,
    "item_order" INTEGER NOT NULL DEFAULT 0, "is_active" INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY ("game") REFERENCES "game_paths" ("game") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "mod_classifications_game_name_idx" ON "mod_classifications" ("game", "name");
CREATE TABLE "mod_classification_groups" (
    "id" TEXT PRIMARY KEY NOT NULL, "classification_id" TEXT NOT NULL, "name" TEXT NOT NULL,
    "item_order" INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY ("classification_id") REFERENCES "mod_classifications" ("id") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "mod_classification_groups_classification_name_idx"
ON "mod_classification_groups" ("classification_id", "name");
CREATE TABLE "setting" ("key" TEXT PRIMARY KEY NOT NULL, "value" TEXT);
CREATE TABLE "app_state" ("key" TEXT PRIMARY KEY NOT NULL, "value" TEXT NOT NULL, "updated_at" TEXT NOT NULL);
CREATE TABLE "_schema_state" ("key" TEXT PRIMARY KEY NOT NULL, "value" TEXT NOT NULL, "updated_at" TEXT NOT NULL);
CREATE TABLE "backup_committed" (
    "target_key" TEXT NOT NULL, "rel_path" TEXT NOT NULL, "sha256" TEXT NOT NULL, "size" INTEGER, "mtime" INTEGER,
    PRIMARY KEY ("target_key", "rel_path")
);
INSERT INTO "game_paths" (rowid, "game", "modFolderPath", "order")
VALUES (2, 'case', 'C:/case', 11), (17, 'Case', 'C:/upper', 11), (25, 'Other', 'C:/other', 5);
INSERT INTO "game_paths" (rowid, "game", "modFolderPath", "importer", "linkedModFolderPath", "gameInstallPath",
                          "gameExecutablePath") VALUES (11, ' Case ', 'C:/mods', 'GIMI', 'C:/linked', 'C:/game', 'C:/game/play.exe');
INSERT INTO "mod_presets" ("id", "game", "name", "item_count") VALUES ('preset', ' Case ', 'Preset', 1);
INSERT INTO "mod_preset_items" VALUES ('preset', 'mod', 'main/hero', 'main', 'Hero', 1, 0);
INSERT INTO "mod_toggle_presets" ("id", "game", "mod_key", "mod_name", "name")
VALUES ('toggle', ' Case ', 'mod', 'Hero', 'Toggle');
INSERT INTO "mod_classifications" ("id", "game", "name", "is_active") VALUES ('classification', ' Case ', 'Class', 1);
INSERT INTO "mod_classification_groups" ("id", "classification_id", "name") VALUES ('group', 'classification', 'Group');
INSERT INTO "setting" VALUES
    ('last_game', ' Case '), ('manual_subgroups', '{" Case ":["main/hero"],"Case":["main/other"],"Deleted":["old"]}'),
    ('backup_excluded_games', '[" Case ","case","Deleted"]');
INSERT INTO "backup_committed" VALUES ('game: Case ', 'a.ini', 'hash', NULL, NULL);`
	if err := client.exec(ctx, schema); err != nil {
		t.Fatalf("seed legacy game schema: %v", err)
	}
	const marker = `{"modRootPath":"C:/mods","paths":["a.ini"]}`
	for _, row := range []AppStateRow{
		{Key: legacyBisectMarkerPrefix + " Case ", Value: marker, UpdatedAt: "mark"},
		{Key: legacyBisectMarkerPrefix + "Deleted", Value: marker, UpdatedAt: "orphan mark"},
		{Key: legacyToggleImportPrefix + legacyImportReservation(" Case ", "mod", "source"),
			Value: "destination", UpdatedAt: "reservation"},
		{Key: legacyToggleImportPrefix + legacyImportReservation("Deleted", "mod", "source"),
			Value: "orphan destination", UpdatedAt: "orphan reservation"},
		{Key: legacyToggleImportPrefix + "?", Value: "keep malformed base64", UpdatedAt: "bad base64"},
		{Key: legacyToggleImportPrefix + base64.RawURLEncoding.EncodeToString([]byte(`[null,"mod","source"]`)),
			Value: "keep malformed owner", UpdatedAt: "bad owner"},
	} {
		if err := client.AppState.Upsert(ctx, row.Key, row.Value, row.UpdatedAt); err != nil {
			t.Fatalf("seed legacy app state: %v", err)
		}
	}
	return client
}
