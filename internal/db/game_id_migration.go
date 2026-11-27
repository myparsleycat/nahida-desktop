package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	legacyBisectMarkerPrefix  = "mod_bisect_session:"
	legacyToggleImportPrefix  = "toggle-preset-import-id:"
	legacyLastGameKey         = "last_game"
	legacyManualSubGroupsKey  = "manual_subgroups"
	legacyBackupExclusionsKey = "backup_excluded_games"
)

func (c *Client) migrateGameIDs(ctx context.Context) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var hasSchemaState bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM "sqlite_schema"
WHERE "type" = 'table' AND "name" = '_schema_state')`).Scan(&hasSchemaState); err != nil {
		return fmt.Errorf("find migration state: %w", err)
	}
	if hasSchemaState {
		var complete string
		err := tx.QueryRowContext(ctx, `SELECT "value" FROM "_schema_state" WHERE "key" = ?`, SchemaKeyGameIDs).
			Scan(&complete)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read migration state: %w", err)
		}
		if complete == "1" {
			return nil
		}
	}

	columns, err := pragmaTableInfoOn(ctx, tx, "game_paths")
	if err != nil || len(columns) == 0 || migrationHasColumn(columns, "id") {
		return err
	}
	ids, err := readLegacyGameIDs(ctx, tx)
	if err != nil {
		return fmt.Errorf("assign IDs to legacy games: %w", err)
	}
	if err := deleteGameIDMigrationOrphans(ctx, tx); err != nil {
		return fmt.Errorf("delete rows of missing games: %w", err)
	}
	if err := rebuildGameIDTables(ctx, tx, ids, hasSchemaState); err != nil {
		return err
	}
	if err := migrateGameIDSettings(ctx, tx, ids); err != nil {
		return fmt.Errorf("migrate game preferences: %w", err)
	}
	if err := migrateGameIDAppState(ctx, tx, ids); err != nil {
		return fmt.Errorf("migrate game app state: %w", err)
	}
	if err := checkGameIDMigrationForeignKeys(ctx, tx); err != nil {
		return fmt.Errorf("check foreign keys: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO "_schema_state" ("key", "value", "updated_at") VALUES (?, '1', ?)
ON CONFLICT("key") DO UPDATE SET "value" = excluded."value", "updated_at" = excluded."updated_at"`,
		SchemaKeyGameIDs, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record game ID migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit game ID migration: %w", err)
	}
	return nil
}

func readLegacyGameIDs(ctx context.Context, tx queryExec) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT "game_paths"."game" FROM "game_paths"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	ids := make(map[string]string)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if _, exists := ids[name]; exists {
			return nil, errors.New("duplicate legacy game name")
		}
		id, err := uuid.NewRandom()
		if err != nil {
			return nil, err
		}
		ids[name] = id.String()
	}
	return ids, rows.Err()
}

// deleteGameIDMigrationOrphans removes rows whose game or parent row no longer exists. They have no game ID to
// take, and foreign keys may have been off when their parent was deleted.
func deleteGameIDMigrationOrphans(ctx context.Context, tx queryExec) error {
	for _, link := range []struct{ table, column, parent, parentColumn string }{
		{"mod_presets", "game", "game_paths", "game"},
		{"mod_toggle_presets", "game", "game_paths", "game"},
		{"mod_classifications", "game", "game_paths", "game"},
		{"mod_preset_items", "preset_id", "mod_presets", "id"},
		{"mod_classification_groups", "classification_id", "mod_classifications", "id"},
	} {
		columns, err := pragmaTableInfoOn(ctx, tx, link.table)
		if err != nil {
			return err
		}
		if !migrationHasColumn(columns, link.column) {
			continue
		}
		parentColumns, err := pragmaTableInfoOn(ctx, tx, link.parent)
		if err != nil {
			return err
		}
		if !migrationHasColumn(parentColumns, link.parentColumn) {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DELETE FROM %s WHERE %s NOT IN (SELECT %s FROM %s)`,
			quoteIdent(link.table), quoteIdent(link.column), quoteIdent(link.parentColumn), quoteIdent(link.parent),
		)); err != nil {
			return fmt.Errorf("delete orphaned %s: %w", link.table, err)
		}
	}
	return nil
}

func rebuildGameIDTables(ctx context.Context, tx queryExec, ids map[string]string, hasSchemaState bool) error {
	if _, err := tx.ExecContext(
		ctx,
		`CREATE TEMP TABLE "__game_id_map" ("name" TEXT PRIMARY KEY, "id" TEXT NOT NULL)`,
	); err != nil {
		return fmt.Errorf("create game ID map: %w", err)
	}
	for name, id := range ids {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO "__game_id_map" ("name", "id") VALUES (?, ?)`,
			name,
			id,
		); err != nil {
			return fmt.Errorf("populate game ID map: %w", err)
		}
	}

	for _, spec := range TableSpecs {
		var expressions map[string]string
		game := quoteIdent(spec.Name) + `."game"`
		lookup := `(SELECT "id" FROM "__game_id_map" WHERE "name" = ` + game + `)`
		switch spec.Name {
		case "game_paths":
			// Game order uses rowid as a fallback and to break ties.
			expressions = map[string]string{"id": lookup, "legacy_name": game, "rowid": `"game_paths".rowid`}
		case "mod_presets", "mod_toggle_presets", "mod_classifications":
			expressions = map[string]string{"game_id": lookup}
		case "_schema_state":
			if !hasSchemaState {
				if _, err := tx.ExecContext(ctx, buildCreateTableSQL(spec, spec.Name)); err != nil {
					return fmt.Errorf("create migration state: %w", err)
				}
			}
			continue
		default:
			continue
		}

		columns, err := pragmaTableInfoOn(ctx, tx, spec.Name)
		if err != nil {
			return err
		}
		if migrationHasColumn(columns, "game") {
			shape := existingTableShape{tableName: spec.Name, columns: columns}
			if err := rebuildTableOn(ctx, tx, spec, &shape, expressions); err != nil {
				return err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `DROP TABLE "__game_id_map"`); err != nil {
		return fmt.Errorf("drop game ID map: %w", err)
	}
	return nil
}

func migrationHasColumn(columns []tableInfoRow, name string) bool {
	return slices.ContainsFunc(columns, func(column tableInfoRow) bool { return column.Name == name })
}

func migrateGameIDSettings(ctx context.Context, tx queryExec, ids map[string]string) error {
	columns, err := pragmaTableInfoOn(ctx, tx, "setting")
	if err != nil || !migrationHasColumn(columns, "key") || !migrationHasColumn(columns, "value") {
		return err
	}
	for _, key := range []string{legacyLastGameKey, legacyManualSubGroupsKey, legacyBackupExclusionsKey} {
		var value sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT "value" FROM "setting" WHERE "key" = ?`, key).Scan(&value)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read %s: %w", key, err)
		}
		if !value.Valid {
			continue
		}
		updated, err := remapGameIDSetting(key, value.String, ids)
		if err != nil {
			return fmt.Errorf("remap %s: %w", key, err)
		}
		if updated != value.String {
			if _, err := tx.ExecContext(
				ctx,
				`UPDATE "setting" SET "value" = ? WHERE "key" = ?`,
				updated,
				key,
			); err != nil {
				return fmt.Errorf("write %s: %w", key, err)
			}
		}
	}
	return nil
}

func remapGameIDSetting(key, value string, ids map[string]string) (string, error) {
	switch key {
	case legacyLastGameKey:
		if id, ok := ids[value]; ok {
			return id, nil
		}
	case legacyManualSubGroupsKey:
		var groups map[string]json.RawMessage
		if json.Unmarshal([]byte(value), &groups) != nil {
			return value, nil //nolint:nilerr // Preserve unreadable legacy preferences verbatim.
		}
		updated := make(map[string]json.RawMessage, len(groups))
		changed := false
		for name, paths := range groups {
			if id, ok := ids[name]; ok {
				name = id
				changed = true
			}
			updated[name] = paths
		}
		if changed {
			raw, err := json.Marshal(updated)
			return string(raw), err
		}
	case legacyBackupExclusionsKey:
		var excluded []json.RawMessage
		if json.Unmarshal([]byte(value), &excluded) != nil {
			return value, nil //nolint:nilerr // Preserve unreadable legacy preferences verbatim.
		}
		changed := false
		for i, raw := range excluded {
			var name *string
			if json.Unmarshal(raw, &name) != nil || name == nil {
				continue
			}
			if id, ok := ids[*name]; ok {
				encoded, err := json.Marshal(id)
				if err != nil {
					return "", err
				}
				excluded[i] = encoded
				changed = true
			}
		}
		if changed {
			raw, err := json.Marshal(excluded)
			return string(raw), err
		}
	}
	return value, nil
}

func migrateGameIDAppState(ctx context.Context, tx queryExec, ids map[string]string) error {
	columns, err := pragmaTableInfoOn(ctx, tx, "app_state")
	if err != nil || !migrationHasColumn(columns, "key") {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT "key" FROM "app_state" WHERE "key" GLOB ? OR "key" GLOB ?`,
		legacyBisectMarkerPrefix+"*", legacyToggleImportPrefix+"*")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, key := range keys {
		destination, err := remapGameIDAppStateKey(key, ids)
		if err != nil {
			return err
		}
		switch destination {
		case key:
			continue
		case "":
			if _, err := tx.ExecContext(ctx, `DELETE FROM "app_state" WHERE "key" = ?`, key); err != nil {
				return fmt.Errorf("delete orphaned toggle preset import ID reservation: %w", err)
			}
		default:
			if _, err := tx.ExecContext(
				ctx,
				`UPDATE "app_state" SET "key" = ? WHERE "key" = ?`,
				destination,
				key,
			); err != nil {
				return fmt.Errorf("move game app state key: %w", err)
			}
		}
	}
	return nil
}

func remapGameIDAppStateKey(key string, ids map[string]string) (string, error) {
	if name, ok := strings.CutPrefix(key, legacyBisectMarkerPrefix); ok {
		if id, ok := ids[name]; ok {
			return legacyBisectMarkerPrefix + id, nil
		}
		return key, nil
	}

	encoded, _ := strings.CutPrefix(key, legacyToggleImportPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	var owner []string
	if err != nil || json.Unmarshal(raw, &owner) != nil || len(owner) != 3 {
		return key, nil //nolint:nilerr // Preserve unrecognized state keys verbatim.
	}
	canonical, err := json.Marshal(owner)
	if err != nil {
		return "", err
	}
	// The reservation writer only uses the canonical encoding of three strings.
	if base64.RawURLEncoding.EncodeToString(canonical) != encoded {
		return key, nil
	}
	id, ok := ids[owner[0]]
	if !ok {
		return "", nil
	}
	owner[0] = id
	converted, err := json.Marshal(owner)
	if err != nil {
		return "", err
	}
	return legacyToggleImportPrefix + base64.RawURLEncoding.EncodeToString(converted), nil
}

func checkGameIDMigrationForeignKeys(ctx context.Context, tx queryExec) error {
	for _, name := range []string{
		"mod_presets", "mod_preset_items", "mod_toggle_presets", "mod_classifications", "mod_classification_groups",
	} {
		columns, err := pragmaTableInfoOn(ctx, tx, name)
		if err != nil {
			return err
		}
		if len(columns) == 0 {
			continue
		}
		var table, parent string
		var rowID sql.NullInt64
		var fkID int
		err = tx.QueryRowContext(ctx, `PRAGMA foreign_key_check(`+quoteIdent(name)+`)`).
			Scan(&table, &rowID, &parent, &fkID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		return fmt.Errorf("%s row %v violates foreign key %d referencing %s", table, rowID, fkID, parent)
	}
	return nil
}
