package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestReconcileEmptyDatabaseCreatesElectronSchema(t *testing.T) {
	t.Parallel()

	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	assertElectronSchema(t, client)

	state, err := client.SchemaState.Get(ctx, SchemaKeyAppVersion)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if state == nil || state.Value != "4" {
		t.Fatalf("app_schema_version = %+v, want 4", state)
	}
}

func TestReconcileCreatesMissingTables(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "partial.db")
	client, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.db.Exec(`CREATE TABLE "setting" ("key" TEXT PRIMARY KEY NOT NULL, "value" TEXT)`); err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	assertElectronSchema(t, client)
}

func TestReconcileAddsNullableAndDefaultColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "addcol.db")
	client, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.db.Exec(`CREATE TABLE "setting" ("key" TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	if _, err := client.db.Exec(`INSERT INTO "setting" ("key") VALUES ('theme')`); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if _, err := client.db.Exec(`
CREATE TABLE "game_paths" (
  "game" TEXT PRIMARY KEY NOT NULL,
  "modFolderPath" TEXT NOT NULL,
  "importer" TEXT,
  "linkedModFolderPath" TEXT,
  "gameInstallPath" TEXT,
  "gameExecutablePath" TEXT
)`); err != nil {
		t.Fatalf("seed game_paths: %v", err)
	}
	if _, err := client.db.Exec(`INSERT INTO "game_paths" ("game", "modFolderPath") VALUES ('GI', 'C:/mods')`); err != nil {
		t.Fatalf("seed game row: %v", err)
	}

	sqlBefore := tableSQL(t, client, "setting")
	if strings.Contains(sqlBefore, `"value"`) {
		t.Fatalf("precondition: setting already has value: %s", sqlBefore)
	}

	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	sqlAfter := tableSQL(t, client, "setting")
	if !strings.Contains(strings.ToUpper(sqlAfter), "ALTER") && !columnExists(t, client, "setting", "value") {
		t.Fatalf("value column missing after reconcile")
	}
	if !columnExists(t, client, "setting", "value") {
		t.Fatalf("setting.value was not added")
	}
	if !columnExists(t, client, "game_paths", "nteLauncherPath") {
		t.Fatalf("game_paths.nteLauncherPath was not added")
	}
	if !columnExists(t, client, "game_paths", "order") {
		t.Fatalf("game_paths.order was not added")
	}

	// ADD COLUMN must keep the existing table identity, not rebuild it away.
	if !strings.Contains(tableSQL(t, client, "setting"), `"key" TEXT PRIMARY KEY`) &&
		!columnExists(t, client, "setting", "key") {
		t.Fatalf("setting key lost")
	}

	row, err := client.Settings.Get(context.Background(), "theme")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row == nil || row.Value != nil {
		t.Fatalf("existing setting row should survive with NULL value, got %+v", row)
	}

	gp, err := client.GamePaths.GetByGame(context.Background(), "GI")
	if err != nil {
		t.Fatalf("GetByGame: %v", err)
	}
	if gp == nil || gp.ModFolderPath != "C:/mods" {
		t.Fatalf("game_paths row lost: %+v", gp)
	}
	if gp.NteLauncherPath != nil {
		t.Fatalf("new nteLauncherPath should be NULL, got %+v", gp.NteLauncherPath)
	}
}

func TestSQLiteDistinguishesOmittedAndExplicitNullDefaults(t *testing.T) {
	client, err := New(filepath.Join(t.TempDir(), "null-defaults.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.db.Exec(`CREATE TABLE "null_defaults" ("omitted" TEXT, "explicit" TEXT DEFAULT NULL)`); err != nil {
		t.Fatal(err)
	}

	rows := tableInfo(t, client, "null_defaults")
	if len(rows) != 2 {
		t.Fatalf("table_info rows = %#v", rows)
	}
	for _, row := range rows {
		switch row.Name {
		case "omitted":
			if row.DfltValue.Valid {
				t.Fatalf("omitted dflt_value = %q, want SQL NULL", row.DfltValue.String)
			}
		case "explicit":
			if !row.DfltValue.Valid || row.DfltValue.String != "NULL" {
				t.Fatalf("explicit dflt_value = %#v, want NULL text", row.DfltValue)
			}
		}
	}
	explicitNull := "NULL"
	if defaultSQLEqual(nil, &explicitNull) {
		t.Fatal("omitted and explicit NULL defaults should remain distinguishable")
	}
}

func TestReconcileRebuildsAliasesBooleansAndExtraColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "rebuild.db")
	client, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = client.Close() }()

	stmts := []string{
		`CREATE TABLE "setting" ("key" TEXT PRIMARY KEY NOT NULL, "value" TEXT, "legacy" TEXT)`,
		`INSERT INTO "setting" ("key", "value", "legacy") VALUES ('keep', 'yes', 'drop-me')`,
		`CREATE TABLE "fix_tool" (
			"id" TEXT PRIMARY KEY NOT NULL,
			"name" TEXT NOT NULL,
			"source" BLOB NOT NULL,
			"is_src_zstd" TEXT NOT NULL,
			"type" TEXT NOT NULL,
			"size" INTEGER NOT NULL
		)`,
		`INSERT INTO "fix_tool" ("id", "name", "source", "is_src_zstd", "type", "size")
		 VALUES ('script-1', 'Fixer', x'010203', 'true', 'python', 3)`,
		`CREATE TABLE "fix_tool_preset" ("id" TEXT PRIMARY KEY NOT NULL, "name" TEXT NOT NULL)`,
		`INSERT INTO "fix_tool_preset" ("id", "name") VALUES ('preset-1', 'Main')`,
		`CREATE TABLE "fix_tool_preset_item" (
			"preset_id" TEXT NOT NULL,
			"tool_id" TEXT NOT NULL,
			"order" INTEGER NOT NULL,
			PRIMARY KEY ("preset_id", "tool_id")
		)`,
		`INSERT INTO "fix_tool_preset_item" ("preset_id", "tool_id", "order") VALUES ('preset-1', 'script-1', 1)`,
	}
	for _, stmt := range stmts {
		if _, err := client.db.Exec(stmt); err != nil {
			t.Fatalf("seed %s: %v", stmt, err)
		}
	}

	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if tableExists(t, client, "fix_tool") {
		t.Fatal("alias table fix_tool should have been rebuilt into script")
	}
	if tableExists(t, client, "fix_tool_preset") {
		t.Fatal("alias table fix_tool_preset should have been rebuilt")
	}
	if tableExists(t, client, "fix_tool_preset_item") {
		t.Fatal("alias table fix_tool_preset_item should have been rebuilt")
	}
	if !tableExists(t, client, "script") || !tableExists(t, client, "script_preset") || !tableExists(t, client, "script_preset_item") {
		t.Fatal("canonical script tables missing after rebuild")
	}
	if columnExists(t, client, "setting", "legacy") {
		t.Fatal("extra column legacy should have been dropped by rebuild")
	}
	if columnExists(t, client, "script_preset_item", "tool_id") {
		t.Fatal("tool_id alias should have become script_id")
	}
	if !columnExists(t, client, "script_preset_item", "script_id") {
		t.Fatal("script_id missing after alias rebuild")
	}

	setting, err := client.Settings.Get(context.Background(), "keep")
	if err != nil || setting == nil || setting.Value == nil || *setting.Value != "yes" {
		t.Fatalf("setting row not copied: %+v %v", setting, err)
	}

	script, err := client.Scripts.FindByID(context.Background(), "script-1")
	if err != nil || script == nil {
		t.Fatalf("script row missing: %v", err)
	}
	if !script.IsSrcZstd {
		t.Fatal("boolean 'true' was not normalized to 1")
	}
	if string(script.Source) != string([]byte{1, 2, 3}) {
		t.Fatalf("blob not copied: %v", script.Source)
	}
	if script.SHA256 != "" {
		t.Fatalf("missing sha256 should use default empty string, got %q", script.SHA256)
	}

	item, err := client.ScriptPresetItems.ListByPresetID(context.Background(), "preset-1")
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(item) != 1 || item[0].ScriptID != "script-1" || item[0].Order != 1 {
		t.Fatalf("tool_id was not copied onto script_id: %+v", item)
	}

	assertElectronSchema(t, client)
}

func TestReconcileAppliesNTEGamePathsMigrationOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nte.db")
	client, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// Clear the one-shot flag and plant NTE rows as an existing file would look
	// before this migration ran.
	if _, err := client.db.Exec(`DELETE FROM "_schema_state" WHERE "key" = ?`, SchemaKeyGamePathsNTELauncher); err != nil {
		t.Fatalf("clear flag: %v", err)
	}
	if _, err := client.db.Exec(`
INSERT INTO "game_paths"
("game", "modFolderPath", "importer", "linkedModFolderPath", "gameInstallPath", "gameExecutablePath", "nteLauncherPath", "order")
VALUES
('NTE-old', 'C:/nte', 'NTE', NULL, NULL, 'C:/games/launcher.exe', NULL, 1),
('NTE-keep', 'C:/nte2', 'NTE', NULL, NULL, 'C:/games/YuanShen/htgame.exe', NULL, 2),
('GI', 'C:/gi', 'XXMI', NULL, NULL, 'C:/gi/GenshinImpact.exe', NULL, 3)`); err != nil {
		t.Fatalf("seed nte rows: %v", err)
	}

	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("migrate reconcile: %v", err)
	}

	moved, err := client.GamePaths.GetByGame(context.Background(), "NTE-old")
	if err != nil || moved == nil {
		t.Fatalf("NTE-old missing: %v", err)
	}
	if moved.GameExecutablePath != nil {
		t.Fatalf("NTE launcher path should have cleared gameExecutablePath, got %+v", moved.GameExecutablePath)
	}
	if moved.NteLauncherPath == nil || *moved.NteLauncherPath != "C:/games/launcher.exe" {
		t.Fatalf("NTE launcher path not moved: %+v", moved.NteLauncherPath)
	}

	kept, err := client.GamePaths.GetByGame(context.Background(), "NTE-keep")
	if err != nil || kept == nil {
		t.Fatalf("NTE-keep missing: %v", err)
	}
	if kept.NteLauncherPath != nil {
		t.Fatalf("htgame.exe row should not move into nteLauncherPath: %+v", kept)
	}
	if kept.GameExecutablePath == nil || !strings.HasSuffix(strings.ToLower(*kept.GameExecutablePath), "htgame.exe") {
		t.Fatalf("htgame.exe should stay on gameExecutablePath: %+v", kept.GameExecutablePath)
	}

	other, err := client.GamePaths.GetByGame(context.Background(), "GI")
	if err != nil || other == nil {
		t.Fatalf("GI missing: %v", err)
	}
	if other.NteLauncherPath != nil {
		t.Fatalf("non-NTE row should not migrate: %+v", other)
	}

	flag, err := client.SchemaState.Get(context.Background(), SchemaKeyGamePathsNTELauncher)
	if err != nil || flag == nil || flag.Value != "1" {
		t.Fatalf("migration flag missing: %+v %v", flag, err)
	}

	// Second pass must be one-shot: put a new NTE exe path back and ensure it is left alone.
	if _, err := client.db.Exec(`UPDATE "game_paths" SET "gameExecutablePath" = 'C:/games/new-launcher.exe', "nteLauncherPath" = NULL WHERE "game" = 'NTE-old'`); err != nil {
		t.Fatalf("reset row: %v", err)
	}
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	again, err := client.GamePaths.GetByGame(context.Background(), "NTE-old")
	if err != nil || again == nil {
		t.Fatalf("NTE-old after second pass: %v", err)
	}
	if again.NteLauncherPath != nil {
		t.Fatalf("one-shot migration ran twice: %+v", again)
	}
	if again.GameExecutablePath == nil || *again.GameExecutablePath != "C:/games/new-launcher.exe" {
		t.Fatalf("second pass should leave the new exe path: %+v", again.GameExecutablePath)
	}
}

func TestReconcileDropsToggleViewerArtifactTable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "toggle.db")
	client, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	if _, err := client.db.Exec(`DELETE FROM "_schema_state" WHERE "key" = ?`, SchemaKeyToggleViewerArtifactDropped); err != nil {
		t.Fatalf("clear flag: %v", err)
	}
	if _, err := client.db.Exec(`
CREATE TABLE "toggle_viewer_artifact" (
	"id" TEXT PRIMARY KEY NOT NULL,
	"target_ini_path" TEXT NOT NULL
)`); err != nil {
		t.Fatalf("seed leftover table: %v", err)
	}
	enabled := "true"
	hotkey := "ctrl H"
	if err := client.Settings.Insert(ctx, SettingRow{Key: "xxmi_toggle_viewer_auto_generate", Value: &enabled}); err != nil {
		t.Fatalf("seed auto generate: %v", err)
	}
	if err := client.Settings.Insert(ctx, SettingRow{Key: "xxmi_toggle_viewer_hotkey", Value: &hotkey}); err != nil {
		t.Fatalf("seed hotkey: %v", err)
	}

	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("drop reconcile: %v", err)
	}

	if _, ok := userTables(t, client)["toggle_viewer_artifact"]; ok {
		t.Fatal("toggle_viewer_artifact should have been dropped")
	}
	if row, err := client.Settings.Get(ctx, "xxmi_toggle_viewer_auto_generate"); err != nil || row != nil {
		t.Fatalf("auto generate leftover = %+v %v", row, err)
	}
	if row, err := client.Settings.Get(ctx, "xxmi_toggle_viewer_hotkey"); err != nil || row != nil {
		t.Fatalf("hotkey leftover = %+v %v", row, err)
	}

	flag, err := client.SchemaState.Get(ctx, SchemaKeyToggleViewerArtifactDropped)
	if err != nil || flag == nil || flag.Value != "1" {
		t.Fatalf("drop flag = %+v %v", flag, err)
	}

	if _, err := client.db.Exec(`CREATE TABLE "toggle_viewer_artifact" ("id" TEXT PRIMARY KEY NOT NULL)`); err != nil {
		t.Fatalf("replant table: %v", err)
	}
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("second drop reconcile: %v", err)
	}
	if _, ok := userTables(t, client)["toggle_viewer_artifact"]; !ok {
		t.Fatal("one-shot drop should not run again")
	}
}

func TestGetReconcileActionAddColumnsAndRebuild(t *testing.T) {
	t.Parallel()

	setting, ok := specByName("setting")
	if !ok {
		t.Fatal("setting spec missing")
	}
	script, ok := specByName("script")
	if !ok {
		t.Fatal("script spec missing")
	}
	items, ok := specByName("script_preset_item")
	if !ok {
		t.Fatal("script_preset_item spec missing")
	}

	add := getReconcileAction(setting, &existingTableShape{
		tableName: "setting",
		columns: []tableInfoRow{
			{Name: "key", Type: "TEXT", NotNull: 1, PK: 1},
		},
	})
	if add.kind != "add-columns" || len(add.columns) != 1 || add.columns[0].Name != "value" {
		t.Fatalf("nullable missing column should ADD COLUMN, got %+v", add)
	}

	alias := getReconcileAction(script, &existingTableShape{
		tableName: "script",
		columns: []tableInfoRow{
			{Name: "id", Type: "TEXT", NotNull: 1, PK: 1},
			{Name: "name", Type: "TEXT", NotNull: 1},
			{Name: "source", Type: "BLOB", NotNull: 1},
			{Name: "is_src_zstd", Type: "INTEGER", NotNull: 1, DfltValue: sql.NullString{String: "0", Valid: true}},
			{Name: "type", Type: "TEXT", NotNull: 1},
			{Name: "size", Type: "INTEGER", NotNull: 1},
			{Name: "zstd_size", Type: "INTEGER", DfltValue: sql.NullString{String: "NULL", Valid: true}},
			{Name: "sha256", Type: "TEXT", NotNull: 1, DfltValue: sql.NullString{String: "''", Valid: true}},
			{Name: "zstd_sha256", Type: "TEXT", DfltValue: sql.NullString{String: "NULL", Valid: true}},
		},
	})
	if alias.kind != "noop" {
		t.Fatalf("matching script shape should be noop, got %+v", alias)
	}

	rebuild := getReconcileAction(items, &existingTableShape{
		tableName: "script_preset_item",
		columns: []tableInfoRow{
			{Name: "preset_id", Type: "TEXT", NotNull: 1, PK: 1},
			{Name: "tool_id", Type: "TEXT", NotNull: 1, PK: 2},
			{Name: "order", Type: "INTEGER", NotNull: 1},
		},
	})
	if rebuild.kind != "rebuild" {
		t.Fatalf("tool_id alias should rebuild, got %+v", rebuild)
	}

	renamed := getReconcileAction(script, &existingTableShape{
		tableName: "fix_tool",
		columns:   []tableInfoRow{{Name: "id", Type: "TEXT", NotNull: 1, PK: 1}},
	})
	if renamed.kind != "rebuild" {
		t.Fatalf("aliased table name should rebuild, got %+v", renamed)
	}
}

func specByName(name string) (TableSpec, bool) {
	for _, spec := range TableSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return TableSpec{}, false
}

func TestReconcileRebuildsTypeNullPKAndFKMismatch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "mismatch.db")
	client, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.db.Exec(`CREATE TABLE "setting" ("key" TEXT PRIMARY KEY NOT NULL, "value" INTEGER)`); err != nil {
		t.Fatalf("type mismatch seed: %v", err)
	}
	if _, err := client.db.Exec(`INSERT INTO "setting" ("key", "value") VALUES ('n', 7)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := client.db.Exec(`CREATE TABLE "app_state" ("key" TEXT PRIMARY KEY NOT NULL, "value" TEXT, "updated_at" TEXT NOT NULL)`); err != nil {
		t.Fatalf("null mismatch seed: %v", err)
	}
	if _, err := client.db.Exec(`INSERT INTO "app_state" ("key", "value", "updated_at") VALUES ('k', 'v', 't')`); err != nil {
		t.Fatalf("seed app_state: %v", err)
	}
	if _, err := client.db.Exec(`CREATE TABLE "game_paths" ("game" TEXT NOT NULL, "modFolderPath" TEXT NOT NULL, "importer" TEXT, "linkedModFolderPath" TEXT, "gameInstallPath" TEXT, "gameExecutablePath" TEXT, "nteLauncherPath" TEXT, "order" INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("pk mismatch seed: %v", err)
	}
	if _, err := client.db.Exec(`CREATE TABLE "mod_presets" (
		"id" TEXT PRIMARY KEY NOT NULL,
		"game" TEXT NOT NULL,
		"name" TEXT NOT NULL,
		"description" TEXT,
		"item_count" INTEGER NOT NULL DEFAULT 0,
		"created_at" TEXT NOT NULL DEFAULT '',
		"updated_at" TEXT NOT NULL DEFAULT '',
		"version" INTEGER NOT NULL DEFAULT 1
	)`); err != nil {
		t.Fatalf("fk mismatch seed: %v", err)
	}

	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	assertElectronSchema(t, client)

	setting, err := client.Settings.Get(context.Background(), "n")
	if err != nil || setting == nil || setting.Value == nil || *setting.Value != "7" {
		t.Fatalf("type-rebuilt setting not copied: %+v %v", setting, err)
	}
	state, err := client.AppState.Get(context.Background(), "k")
	if err != nil || state == nil || state.Value != "v" {
		t.Fatalf("null-rebuilt app_state not copied: %+v %v", state, err)
	}
}

func mustNewTemp(t *testing.T) *Client {
	t.Helper()
	client, err := New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func assertElectronSchema(t *testing.T, client *Client) {
	t.Helper()

	names := userTables(t, client)
	for _, spec := range TableSpecs {
		if _, ok := names[spec.Name]; !ok {
			t.Fatalf("missing table %s", spec.Name)
		}
		for _, alias := range spec.Aliases {
			if _, ok := names[alias]; ok {
				t.Fatalf("alias table %s should not remain beside %s", alias, spec.Name)
			}
		}

		cols := tableInfo(t, client, spec.Name)
		if len(cols) != len(spec.Columns) {
			t.Fatalf("%s column count = %d, want %d", spec.Name, len(cols), len(spec.Columns))
		}
		byName := map[string]tableInfoRow{}
		for _, col := range cols {
			byName[col.Name] = col
		}
		var actualPK []string
		pkOrdered := append([]tableInfoRow(nil), cols...)
		sort.Slice(pkOrdered, func(i, j int) bool { return pkOrdered[i].PK < pkOrdered[j].PK })
		for _, col := range pkOrdered {
			if col.PK > 0 {
				actualPK = append(actualPK, col.Name)
			}
		}

		for _, want := range spec.Columns {
			got, ok := byName[want.Name]
			if !ok {
				t.Fatalf("%s missing column %s", spec.Name, want.Name)
			}
			if normalizeType(got.Type) != normalizeType(string(want.Type)) {
				t.Fatalf("%s.%s type = %q, want %q", spec.Name, want.Name, got.Type, want.Type)
			}
			if (got.NotNull != 0) != want.NotNull {
				t.Fatalf("%s.%s notnull = %d, want %v", spec.Name, want.Name, got.NotNull, want.NotNull)
			}
			var existingDefault *string
			if got.DfltValue.Valid {
				v := got.DfltValue.String
				existingDefault = &v
			}
			if !defaultSQLEqual(existingDefault, want.DefaultSQL) {
				t.Fatalf("%s.%s default = %v, want %v", spec.Name, want.Name, existingDefault, want.DefaultSQL)
			}
			if len(spec.CompositePrimaryKey) == 0 && want.PrimaryKey != (got.PK > 0) {
				t.Fatalf("%s.%s pk = %d, want primaryKey=%v", spec.Name, want.Name, got.PK, want.PrimaryKey)
			}
		}

		if len(spec.CompositePrimaryKey) > 0 {
			if strings.Join(sortStrings(actualPK), ",") != strings.Join(sortStrings(spec.CompositePrimaryKey), ",") {
				t.Fatalf("%s composite pk = %v, want %v", spec.Name, actualPK, spec.CompositePrimaryKey)
			}
		}

		fks := foreignKeys(t, client, spec.Name)
		if len(fks) != len(spec.ForeignKeys) {
			t.Fatalf("%s fk count = %d, want %d (%+v)", spec.Name, len(fks), len(spec.ForeignKeys), fks)
		}
		wantFK := map[string]struct{}{}
		for _, fk := range spec.ForeignKeys {
			wantFK[normalizeForeignKeySignature(fk)] = struct{}{}
		}
		for _, fk := range fks {
			if _, ok := wantFK[normalizeForeignKeySignature(fk)]; !ok {
				t.Fatalf("%s unexpected fk %+v", spec.Name, fk)
			}
		}

		gotIndexes := userIndexes(t, client, spec.Name)
		for _, want := range spec.Indexes {
			sig := normalizeIndexSignature(want)
			found := false
			for _, got := range gotIndexes {
				if normalizeIndexSignature(got) == sig {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s missing index %+v (have %+v)", spec.Name, want, gotIndexes)
			}
		}
	}
}

func userTables(t *testing.T, client *Client) map[string]struct{} {
	t.Helper()
	rows, err := client.db.Query(`SELECT "name" FROM "sqlite_schema" WHERE "type" = 'table' AND "name" NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("sqlite_schema: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		out[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tables: %v", err)
	}
	return out
}

func tableSQL(t *testing.T, client *Client, name string) string {
	t.Helper()
	var sqlText sql.NullString
	if err := client.db.QueryRow(`SELECT "sql" FROM "sqlite_schema" WHERE "type" = 'table' AND "name" = ?`, name).Scan(&sqlText); err != nil {
		t.Fatalf("table sql %s: %v", name, err)
	}
	return sqlText.String
}

func tableExists(t *testing.T, client *Client, name string) bool {
	t.Helper()
	_, ok := userTables(t, client)[name]
	return ok
}

func columnExists(t *testing.T, client *Client, table, column string) bool {
	t.Helper()
	for _, col := range tableInfo(t, client, table) {
		if col.Name == column {
			return true
		}
	}
	return false
}

func tableInfo(t *testing.T, client *Client, table string) []tableInfoRow {
	t.Helper()
	rows, err := client.db.Query(`PRAGMA table_info(` + quoteIdent(table) + `)`)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var out []tableInfoRow
	for rows.Next() {
		var row tableInfoRow
		if err := rows.Scan(&row.CID, &row.Name, &row.Type, &row.NotNull, &row.DfltValue, &row.PK); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info: %v", err)
	}
	return out
}

func foreignKeys(t *testing.T, client *Client, table string) []ForeignKeySpec {
	t.Helper()
	rows, err := client.db.Query(`PRAGMA foreign_key_list(` + quoteIdent(table) + `)`)
	if err != nil {
		t.Fatalf("foreign_key_list %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var raw []foreignKeyRow
	for rows.Next() {
		var row foreignKeyRow
		if err := rows.Scan(&row.ID, &row.Seq, &row.Table, &row.From, &row.To, &row.OnUpdate, &row.OnDelete, &row.Match); err != nil {
			t.Fatalf("scan fk: %v", err)
		}
		raw = append(raw, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("fk: %v", err)
	}
	return groupForeignKeys(raw)
}

func userIndexes(t *testing.T, client *Client, table string) []IndexSpec {
	t.Helper()
	rows, err := client.db.Query(`PRAGMA index_list(` + quoteIdent(table) + `)`)
	if err != nil {
		t.Fatalf("index_list %s: %v", table, err)
	}
	var listed []indexListRow
	for rows.Next() {
		var row indexListRow
		if err := rows.Scan(&row.Seq, &row.Name, &row.Unique, &row.Origin, &row.Partial); err != nil {
			_ = rows.Close()
			t.Fatalf("scan index: %v", err)
		}
		if row.Origin == "pk" {
			continue
		}
		listed = append(listed, row)
	}
	iterationErr := rows.Err()
	closeErr := rows.Close()
	if iterationErr != nil {
		t.Fatalf("indexes: %v", iterationErr)
	}
	if closeErr != nil {
		t.Fatalf("close indexes: %v", closeErr)
	}

	// MaxOpenConns is 1; index_info must not run while index_list is still open.
	out := make([]IndexSpec, 0, len(listed))
	for _, row := range listed {
		info, err := client.pragmaIndexInfo(context.Background(), row.Name)
		if err != nil {
			t.Fatalf("index_info %s: %v", row.Name, err)
		}
		cols := make([]string, len(info))
		for i, item := range info {
			cols[i] = item.Name
		}
		out = append(out, IndexSpec{Name: row.Name, Columns: cols, Unique: row.Unique != 0})
	}
	return out
}
