package db

import (
	"context"
	"database/sql"
	"strings"
)

type SettingsStore struct{ c *Client }

func (s SettingsStore) Get(ctx context.Context, key string) (*SettingRow, error) {
	var row SettingRow
	var value sql.NullString
	err := s.c.db.QueryRowContext(ctx, `SELECT "key", "value" FROM "setting" WHERE "key" = ? LIMIT 1`, key).Scan(&row.Key, &value)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.Value = ptrString(value)
	return &row, nil
}

func (s SettingsStore) GetValue(ctx context.Context, key string) (*string, error) {
	row, err := s.Get(ctx, key)
	if err != nil || row == nil {
		return nil, err
	}
	return row.Value, nil
}

func (s SettingsStore) List(ctx context.Context) ([]SettingRow, error) {
	rows, err := s.c.query(ctx, `SELECT "key", "value" FROM "setting" ORDER BY "key"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SettingRow
	for rows.Next() {
		var row SettingRow
		var value sql.NullString
		if err := rows.Scan(&row.Key, &value); err != nil {
			return nil, err
		}
		row.Value = ptrString(value)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s SettingsStore) Insert(ctx context.Context, row SettingRow) error {
	return s.c.exec(ctx, `INSERT INTO "setting" ("key", "value") VALUES (?, ?)`, row.Key, argString(row.Value))
}

func (s SettingsStore) Upsert(ctx context.Context, key string, value *string) error {
	return s.c.exec(ctx, `INSERT INTO "setting" ("key", "value") VALUES (?, ?)
                 ON CONFLICT("key") DO UPDATE SET "value" = excluded."value"`, key, argString(value))
}

func (s SettingsStore) UpsertMany(ctx context.Context, values map[string]*string) error {
	return s.c.withImmediate(ctx, func(q queryExec) error {
		for key, value := range values {
			if _, err := q.ExecContext(ctx, `INSERT INTO "setting" ("key", "value") VALUES (?, ?)
                 ON CONFLICT("key") DO UPDATE SET "value" = excluded."value"`, key, argString(value)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s SettingsStore) UpdateValue(ctx context.Context, key string, value *string) error {
	return s.c.exec(ctx, `UPDATE "setting" SET "value" = ? WHERE "key" = ?`, argString(value), key)
}

func (s SettingsStore) Delete(ctx context.Context, key string) error {
	return s.c.exec(ctx, `DELETE FROM "setting" WHERE "key" = ?`, key)
}

func (s SettingsStore) InsertIfMissing(ctx context.Context, key string, value *string) error {
	return s.c.exec(ctx, `INSERT OR IGNORE INTO "setting" ("key", "value") VALUES (?, ?)`, key, argString(value))
}

// MoveIfMissing moves a setting value to a new key without overwriting an
// already-written destination. The obsolete source is removed atomically.
func (s SettingsStore) MoveIfMissing(ctx context.Context, source, destination string) error {
	if source == destination {
		return nil
	}
	return s.c.withImmediate(ctx, func(q queryExec) error {
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO "setting" ("key", "value")
SELECT ?, "value" FROM "setting" WHERE "key" = ?`, destination, source); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `DELETE FROM "setting" WHERE "key" = ?`, source)
		return err
	})
}

type AppStateStore struct{ c *Client }

func (s AppStateStore) Get(ctx context.Context, key string) (*AppStateRow, error) {
	var row AppStateRow
	err := s.c.db.QueryRowContext(ctx, `SELECT "key", "value", "updated_at" FROM "app_state" WHERE "key" = ? LIMIT 1`, key).
		Scan(&row.Key, &row.Value, &row.UpdatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s AppStateStore) GetValue(ctx context.Context, key string) (*string, error) {
	row, err := s.Get(ctx, key)
	if err != nil || row == nil {
		return nil, err
	}
	v := row.Value
	return &v, nil
}

func (s AppStateStore) List(ctx context.Context) ([]AppStateRow, error) {
	return s.list(ctx, `SELECT "key", "value", "updated_at" FROM "app_state" ORDER BY "key"`)
}

func (s AppStateStore) ListByPrefix(ctx context.Context, prefix string) ([]AppStateRow, error) {
	return s.list(ctx, `SELECT "key", "value", "updated_at" FROM "app_state" WHERE "key" LIKE ? ORDER BY "key"`, prefix+"%")
}

func (s AppStateStore) list(ctx context.Context, query string, args ...any) ([]AppStateRow, error) {
	rows, err := s.c.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AppStateRow
	for rows.Next() {
		var row AppStateRow
		if err := rows.Scan(&row.Key, &row.Value, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s AppStateStore) Upsert(ctx context.Context, key, value, updatedAt string) error {
	return s.c.exec(ctx, `INSERT INTO "app_state" ("key", "value", "updated_at") VALUES (?, ?, ?)
                 ON CONFLICT("key") DO UPDATE
                 SET "value" = excluded."value", "updated_at" = excluded."updated_at"`, key, value, updatedAt)
}

func (s AppStateStore) Delete(ctx context.Context, key string) error {
	return s.c.exec(ctx, `DELETE FROM "app_state" WHERE "key" = ?`, key)
}

type GamePathsStore struct{ c *Client }

const gamePathSelectCols = `"game", "modFolderPath", "importer", "linkedModFolderPath", "gameInstallPath", "gameExecutablePath", "nteLauncherPath"`

func scanGamePath(scanner interface{ Scan(dest ...any) error }) (*GamePathRow, error) {
	var row GamePathRow
	var importer, linked, install, exe, nte sql.NullString
	dest := []any{
		&row.Game, &row.ModFolderPath, &importer, &linked, &install, &exe, &nte, &row.Order,
	}
	if err := scanner.Scan(dest...); err != nil {
		return nil, err
	}
	row.Importer = ptrString(importer)
	row.LinkedModFolderPath = ptrString(linked)
	row.GameInstallPath = ptrString(install)
	row.GameExecutablePath = ptrString(exe)
	row.NteLauncherPath = ptrString(nte)
	return &row, nil
}

func (s GamePathsStore) GetByGame(ctx context.Context, game string) (*GamePathRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT `+gamePathSelectCols+`,
       CASE WHEN "order" = 0 THEN rowid ELSE "order" END AS "order"
FROM "game_paths" WHERE "game" = ? LIMIT 1`, game)
	out, err := scanGamePath(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s GamePathsStore) List(ctx context.Context) ([]GamePathRow, error) {
	rows, err := s.c.query(ctx, `
SELECT `+gamePathSelectCols+`,
       CASE WHEN "order" = 0 THEN rowid ELSE "order" END AS "order"
FROM "game_paths"
ORDER BY
    CASE WHEN "order" = 0 THEN rowid ELSE "order" END,
    rowid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []GamePathRow
	for rows.Next() {
		row, err := scanGamePath(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func (s GamePathsStore) FindByGameOrModFolderPath(ctx context.Context, game, modFolderPath string) (*GamePathRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT `+gamePathSelectCols+`, "order"
FROM "game_paths"
WHERE "game" = ? OR "modFolderPath" = ? OR "linkedModFolderPath" = ?
LIMIT 1`, game, modFolderPath, modFolderPath)
	out, err := scanGamePath(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s GamePathsStore) FindByModFolderPathOtherGame(ctx context.Context, game, modFolderPath string) (*GamePathRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT `+gamePathSelectCols+`, "order"
FROM "game_paths"
WHERE ("modFolderPath" = ? OR "linkedModFolderPath" = ?) AND "game" <> ?
LIMIT 1`, modFolderPath, modFolderPath, game)
	out, err := scanGamePath(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s GamePathsStore) Insert(ctx context.Context, row GamePathRow) error {
	var maxOrder sql.NullInt64
	err := s.c.db.QueryRowContext(ctx, `
SELECT MAX(CASE WHEN "order" = 0 THEN rowid ELSE "order" END) FROM "game_paths"`).Scan(&maxOrder)
	if err != nil && !isNoRows(err) {
		return err
	}
	order := int64(1)
	if maxOrder.Valid {
		order = maxOrder.Int64 + 1
	}
	return s.c.exec(ctx, `
INSERT INTO "game_paths" ("game", "modFolderPath", "importer", "linkedModFolderPath", "gameInstallPath", "gameExecutablePath", "nteLauncherPath", "order")
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.Game, row.ModFolderPath, argString(row.Importer), argString(row.LinkedModFolderPath),
		argString(row.GameInstallPath), argString(row.GameExecutablePath), argString(row.NteLauncherPath), order)
}

func (s GamePathsStore) Upsert(ctx context.Context, row GamePathRow) error {
	return s.c.exec(ctx, `
INSERT INTO "game_paths" ("game", "modFolderPath", "importer", "linkedModFolderPath", "gameInstallPath", "gameExecutablePath", "nteLauncherPath", "order")
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT("game") DO UPDATE
SET "modFolderPath" = excluded."modFolderPath",
    "importer" = excluded."importer",
    "linkedModFolderPath" = excluded."linkedModFolderPath",
    "gameInstallPath" = excluded."gameInstallPath",
    "gameExecutablePath" = excluded."gameExecutablePath",
    "nteLauncherPath" = excluded."nteLauncherPath",
    "order" = excluded."order"`,
		row.Game, row.ModFolderPath, argString(row.Importer), argString(row.LinkedModFolderPath),
		argString(row.GameInstallPath), argString(row.GameExecutablePath), argString(row.NteLauncherPath), row.Order)
}

type GamePathUpdates struct {
	ModFolderPath       string
	Importer            *string
	LinkedModFolderPath *string
	GameInstallPath     *string
	GameExecutablePath  *string
}

func (s GamePathsStore) Update(ctx context.Context, game string, updates GamePathUpdates) error {
	return s.c.exec(ctx, `
UPDATE "game_paths"
SET "modFolderPath" = ?, "importer" = ?, "linkedModFolderPath" = ?, "gameInstallPath" = ?, "gameExecutablePath" = ?
WHERE "game" = ?`,
		updates.ModFolderPath, argString(updates.Importer), argString(updates.LinkedModFolderPath),
		argString(updates.GameInstallPath), argString(updates.GameExecutablePath), game)
}

func (s GamePathsStore) SetNteLauncherPath(ctx context.Context, game, nteLauncherPath string) error {
	return s.c.exec(ctx, `UPDATE "game_paths" SET "nteLauncherPath" = ? WHERE "game" = ?`, nteLauncherPath, game)
}

func (s GamePathsStore) Reorder(ctx context.Context, games []string) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		for i, game := range games {
			if _, err := tx.ExecContext(ctx, `UPDATE "game_paths" SET "order" = ? WHERE "game" = ?`, i+1, game); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s GamePathsStore) Delete(ctx context.Context, game string) error {
	return s.c.exec(ctx, `DELETE FROM "game_paths" WHERE "game" = ?`, game)
}

type ModPresetsStore struct{ c *Client }

func scanModPreset(scanner interface{ Scan(dest ...any) error }) (*ModPresetRow, error) {
	var row ModPresetRow
	var desc sql.NullString
	if err := scanner.Scan(&row.ID, &row.Game, &row.Name, &desc, &row.ItemCount, &row.CreatedAt, &row.UpdatedAt, &row.Version); err != nil {
		return nil, err
	}
	row.Description = ptrString(desc)
	return &row, nil
}

func (s ModPresetsStore) ListByGame(ctx context.Context, game string) ([]ModPresetRow, error) {
	rows, err := s.c.query(ctx, `
SELECT "id", "game", "name", "description", "item_count", "created_at", "updated_at", "version"
FROM "mod_presets" WHERE "game" = ? ORDER BY "name"`, game)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ModPresetRow
	for rows.Next() {
		row, err := scanModPreset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func (s ModPresetsStore) FindByID(ctx context.Context, id string) (*ModPresetRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT "id", "game", "name", "description", "item_count", "created_at", "updated_at", "version"
FROM "mod_presets" WHERE "id" = ? LIMIT 1`, id)
	out, err := scanModPreset(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s ModPresetsStore) FindByGameAndName(ctx context.Context, game, name string) (*ModPresetRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT "id", "game", "name", "description", "item_count", "created_at", "updated_at", "version"
FROM "mod_presets" WHERE "game" = ? AND "name" = ? LIMIT 1`, game, name)
	out, err := scanModPreset(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s ModPresetsStore) Insert(ctx context.Context, row ModPresetRow) error {
	return s.c.exec(ctx, `
INSERT INTO "mod_presets"
("id", "game", "name", "description", "item_count", "created_at", "updated_at", "version")
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Game, row.Name, argString(row.Description), row.ItemCount, row.CreatedAt, row.UpdatedAt, row.Version)
}

// InsertSnapshot persists a preset and all of its items in one immediate
// transaction so a partial snapshot can never become visible.
func (s ModPresetsStore) InsertSnapshot(
	ctx context.Context,
	preset ModPresetRow,
	items []ModPresetItemRow,
) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO "mod_presets"
("id", "game", "name", "description", "item_count", "created_at", "updated_at", "version")
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			preset.ID, preset.Game, preset.Name, argString(preset.Description), preset.ItemCount,
			preset.CreatedAt, preset.UpdatedAt, preset.Version); err != nil {
			return err
		}
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO "mod_preset_items"
("preset_id", "mod_key", "relative_path", "group_relative_path", "folder_name", "is_enabled", "item_order")
VALUES (?, ?, ?, ?, ?, ?, ?)`,
				item.PresetID, item.ModKey, item.RelativePath, item.GroupRelativePath,
				item.FolderName, boolToInt(item.IsEnabled), item.ItemOrder); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s ModPresetsStore) UpdateName(ctx context.Context, id, name, updatedAt string) error {
	return s.c.exec(ctx, `UPDATE "mod_presets" SET "name" = ?, "updated_at" = ? WHERE "id" = ?`, name, updatedAt, id)
}

func (s ModPresetsStore) Delete(ctx context.Context, id string) error {
	return s.c.exec(ctx, `DELETE FROM "mod_presets" WHERE "id" = ?`, id)
}

type ModPresetItemsStore struct{ c *Client }

func (s ModPresetItemsStore) ListByPresetID(ctx context.Context, presetID string) ([]ModPresetItemRow, error) {
	rows, err := s.c.query(ctx, `
SELECT "preset_id", "mod_key", "relative_path", "group_relative_path", "folder_name", "is_enabled", "item_order"
FROM "mod_preset_items" WHERE "preset_id" = ? ORDER BY "item_order"`, presetID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ModPresetItemRow
	for rows.Next() {
		var row ModPresetItemRow
		var enabled any
		if err := rows.Scan(&row.PresetID, &row.ModKey, &row.RelativePath, &row.GroupRelativePath, &row.FolderName, &enabled, &row.ItemOrder); err != nil {
			return nil, err
		}
		row.IsEnabled = toBool(enabled)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s ModPresetItemsStore) InsertMany(ctx context.Context, rows []ModPresetItemRow) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		for _, row := range rows {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO "mod_preset_items"
("preset_id", "mod_key", "relative_path", "group_relative_path", "folder_name", "is_enabled", "item_order")
VALUES (?, ?, ?, ?, ?, ?, ?)`,
				row.PresetID, row.ModKey, row.RelativePath, row.GroupRelativePath, row.FolderName, boolToInt(row.IsEnabled), row.ItemOrder); err != nil {
				return err
			}
		}
		return nil
	})
}

type ImageCacheStore struct{ c *Client }

func (s ImageCacheStore) GetByHash(ctx context.Context, hash string) (*ImageCacheRow, error) {
	var row ImageCacheRow
	err := s.c.db.QueryRowContext(ctx, `SELECT "hash", "image", "size" FROM "image_cache" WHERE "hash" = ? LIMIT 1`, hash).
		Scan(&row.Hash, &row.Image, &row.Size)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s ImageCacheStore) InsertIgnore(ctx context.Context, row ImageCacheRow) error {
	return s.c.exec(ctx, `INSERT OR IGNORE INTO "image_cache" ("hash", "image", "size") VALUES (?, ?, ?)`, row.Hash, row.Image, row.Size)
}

func (s ImageCacheStore) SumSize(ctx context.Context) (int64, error) {
	var total sql.NullInt64
	err := s.c.db.QueryRowContext(ctx, `SELECT SUM("size") FROM "image_cache"`).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Int64, nil
}

func (s ImageCacheStore) DeleteAll(ctx context.Context) error {
	return s.c.exec(ctx, `DELETE FROM "image_cache"`)
}

type TouchProfileVisionCacheStore struct{ c *Client }

func (s TouchProfileVisionCacheStore) Get(ctx context.Context, cacheKey string) (*TouchProfileVisionCacheRow, error) {
	var row TouchProfileVisionCacheRow
	err := s.c.db.QueryRowContext(ctx, `
SELECT "cache_key", "result", "updated_at"
FROM "touch_profile_vision_cache" WHERE "cache_key" = ? LIMIT 1`, cacheKey).
		Scan(&row.CacheKey, &row.Result, &row.UpdatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s TouchProfileVisionCacheStore) Upsert(ctx context.Context, row TouchProfileVisionCacheRow) error {
	return s.c.exec(ctx, `
INSERT INTO "touch_profile_vision_cache" ("cache_key", "result", "updated_at")
VALUES (?, ?, ?)
ON CONFLICT("cache_key") DO UPDATE
SET "result" = excluded."result",
    "updated_at" = excluded."updated_at"`, row.CacheKey, row.Result, row.UpdatedAt)
}

func (s TouchProfileVisionCacheStore) DeleteAll(ctx context.Context) error {
	return s.c.exec(ctx, `DELETE FROM "touch_profile_vision_cache"`)
}

type ModScanCacheStore struct{ c *Client }

const modScanCacheQueryChunk = 400

func (s ModScanCacheStore) Get(ctx context.Context, path string) (*ModScanCacheRow, error) {
	var row ModScanCacheRow
	err := s.c.db.QueryRowContext(ctx, `
SELECT "path", "mtime", "payload", "updated_at"
FROM "mod_scan_cache" WHERE "path" = ? LIMIT 1`, path).
		Scan(&row.Path, &row.Mtime, &row.Payload, &row.UpdatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s ModScanCacheStore) GetMany(ctx context.Context, paths []string) (map[string]ModScanCacheRow, error) {
	out := make(map[string]ModScanCacheRow, len(paths))
	if len(paths) == 0 {
		return out, nil
	}
	for i := 0; i < len(paths); i += modScanCacheQueryChunk {
		end := i + modScanCacheQueryChunk
		if end > len(paths) {
			end = len(paths)
		}
		chunk := paths[i:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(chunk)), ",")
		query := `SELECT "path", "mtime", "payload", "updated_at" FROM "mod_scan_cache" WHERE "path" IN (` + placeholders + `)`
		args := make([]any, len(chunk))
		for j, path := range chunk {
			args[j] = path
		}
		rows, err := s.c.query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row ModScanCacheRow
			if err := rows.Scan(&row.Path, &row.Mtime, &row.Payload, &row.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[row.Path] = row
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s ModScanCacheStore) Upsert(ctx context.Context, row ModScanCacheRow) error {
	return s.c.exec(ctx, `
INSERT INTO "mod_scan_cache" ("path", "mtime", "payload", "updated_at")
VALUES (?, ?, ?, ?)
ON CONFLICT("path") DO UPDATE
SET "mtime" = excluded."mtime",
    "payload" = excluded."payload",
    "updated_at" = excluded."updated_at"`, row.Path, row.Mtime, row.Payload, row.UpdatedAt)
}

func (s ModScanCacheStore) UpsertMany(ctx context.Context, rows []ModScanCacheRow) error {
	if len(rows) == 0 {
		return nil
	}
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		for _, row := range rows {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO "mod_scan_cache" ("path", "mtime", "payload", "updated_at")
VALUES (?, ?, ?, ?)
ON CONFLICT("path") DO UPDATE
SET "mtime" = excluded."mtime",
    "payload" = excluded."payload",
    "updated_at" = excluded."updated_at"`, row.Path, row.Mtime, row.Payload, row.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

type ScriptsStore struct{ c *Client }

func scanScript(scanner interface{ Scan(dest ...any) error }) (*ScriptRow, error) {
	var (
		id, name, typ, sha256 string
		source                []byte
		isSrcZstd             any
		size                  int64
		zstdSize              sql.NullInt64
		zstdSHA256            sql.NullString
	)
	if err := scanner.Scan(&id, &name, &source, &isSrcZstd, &typ, &size, &zstdSize, &sha256, &zstdSHA256); err != nil {
		return nil, err
	}
	return &ScriptRow{
		ID:         id,
		Name:       name,
		Source:     toBytes(source),
		IsSrcZstd:  toBool(isSrcZstd),
		Type:       ScriptType(typ),
		Size:       size,
		ZstdSize:   ptrInt64(zstdSize),
		SHA256:     sha256,
		ZstdSHA256: ptrString(zstdSHA256),
	}, nil
}

func (s ScriptsStore) FindByID(ctx context.Context, id string) (*ScriptRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT "id", "name", "source", "is_src_zstd", "type", "size", "zstd_size", "sha256", "zstd_sha256"
FROM "script" WHERE "id" = ? LIMIT 1`, id)
	out, err := scanScript(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s ScriptsStore) FindBySHA256OrName(ctx context.Context, sha256, name string) (*ScriptRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT "id", "name", "source", "is_src_zstd", "type", "size", "zstd_size", "sha256", "zstd_sha256"
FROM "script" WHERE "sha256" = ? OR "name" = ? LIMIT 1`, sha256, name)
	out, err := scanScript(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s ScriptsStore) ListBasic(ctx context.Context) ([]ScriptBasicRow, error) {
	rows, err := s.c.query(ctx, `SELECT "id", "name", "type", "size" FROM "script" ORDER BY "name"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ScriptBasicRow
	for rows.Next() {
		var row ScriptBasicRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Type, &row.Size); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s ScriptsStore) Insert(ctx context.Context, row ScriptRow) error {
	return s.c.exec(ctx, `
INSERT INTO "script"
("id", "name", "source", "is_src_zstd", "type", "size", "zstd_size", "sha256", "zstd_sha256")
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Name, row.Source, boolToInt(row.IsSrcZstd), string(row.Type), row.Size, argInt64(row.ZstdSize), row.SHA256, argString(row.ZstdSHA256))
}

func (s ScriptsStore) UpdateCompressedSource(ctx context.Context, id string, source []byte, zstdSHA256 string, zstdSize int64) error {
	return s.c.exec(ctx, `
UPDATE "script"
SET "source" = ?, "is_src_zstd" = 1, "zstd_sha256" = ?, "zstd_size" = ?
WHERE "id" = ?`, source, zstdSHA256, zstdSize, id)
}

func (s ScriptsStore) Delete(ctx context.Context, id string) error {
	return s.c.exec(ctx, `DELETE FROM "script" WHERE "id" = ?`, id)
}

type ScriptPresetsStore struct{ c *Client }

func (s ScriptPresetsStore) ListWithScripts(ctx context.Context) ([]ScriptPresetWithScripts, error) {
	rows, err := s.c.query(ctx, `SELECT "id", "name" FROM "script_preset" ORDER BY "name"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var presets []ScriptPresetRow
	for rows.Next() {
		var row ScriptPresetRow
		if err := rows.Scan(&row.ID, &row.Name); err != nil {
			return nil, err
		}
		presets = append(presets, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	itemRows, err := s.c.query(ctx, `SELECT "preset_id", "script_id", "order" FROM "script_preset_item" ORDER BY "preset_id", "order"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = itemRows.Close() }()
	var items []ScriptPresetItemRow
	for itemRows.Next() {
		var item ScriptPresetItemRow
		if err := itemRows.Scan(&item.PresetID, &item.ScriptID, &item.Order); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := itemRows.Err(); err != nil {
		return nil, err
	}

	out := make([]ScriptPresetWithScripts, 0, len(presets))
	for _, preset := range presets {
		var scripts []ScriptPresetItemRow
		for _, item := range items {
			if item.PresetID == preset.ID {
				scripts = append(scripts, item)
			}
		}
		out = append(out, ScriptPresetWithScripts{ScriptPresetRow: preset, Scripts: scripts})
	}
	return out, nil
}

func (s ScriptPresetsStore) FindByID(ctx context.Context, id string) (*ScriptPresetRow, error) {
	var row ScriptPresetRow
	err := s.c.db.QueryRowContext(ctx, `SELECT "id", "name" FROM "script_preset" WHERE "id" = ? LIMIT 1`, id).Scan(&row.ID, &row.Name)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s ScriptPresetsStore) FindByIDWithScripts(ctx context.Context, id string) (*ScriptPresetWithScripts, error) {
	preset, err := s.FindByID(ctx, id)
	if err != nil || preset == nil {
		return nil, err
	}
	scripts, err := s.c.ScriptPresetItems.ListByPresetID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ScriptPresetWithScripts{ScriptPresetRow: *preset, Scripts: scripts}, nil
}

func (s ScriptPresetsStore) FindByName(ctx context.Context, name string) (*ScriptPresetRow, error) {
	var row ScriptPresetRow
	err := s.c.db.QueryRowContext(ctx, `SELECT "id", "name" FROM "script_preset" WHERE "name" = ? LIMIT 1`, name).Scan(&row.ID, &row.Name)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s ScriptPresetsStore) Insert(ctx context.Context, row ScriptPresetRow) error {
	return s.c.exec(ctx, `INSERT INTO "script_preset" ("id", "name") VALUES (?, ?)`, row.ID, row.Name)
}

// InsertSnapshot stores a preset and its ordered items as one atomic unit.
// This prevents an interrupted create from leaving an empty preset behind.
func (s ScriptPresetsStore) InsertSnapshot(ctx context.Context, row ScriptPresetRow, items []ScriptPresetItemRow) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO "script_preset" ("id", "name") VALUES (?, ?)`, row.ID, row.Name); err != nil {
			return err
		}
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "script_preset_item" ("preset_id", "script_id", "order") VALUES (?, ?, ?)`,
				item.PresetID, item.ScriptID, item.Order); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s ScriptPresetsStore) Delete(ctx context.Context, id string) error {
	return s.c.exec(ctx, `DELETE FROM "script_preset" WHERE "id" = ?`, id)
}

type ScriptPresetItemsStore struct{ c *Client }

func (s ScriptPresetItemsStore) ListByPresetID(ctx context.Context, presetID string) ([]ScriptPresetItemRow, error) {
	rows, err := s.c.query(ctx, `
SELECT "preset_id", "script_id", "order"
FROM "script_preset_item" WHERE "preset_id" = ? ORDER BY "order"`, presetID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ScriptPresetItemRow
	for rows.Next() {
		var row ScriptPresetItemRow
		if err := rows.Scan(&row.PresetID, &row.ScriptID, &row.Order); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s ScriptPresetItemsStore) FindUsageByScriptID(ctx context.Context, scriptID string) (*ScriptPresetItemUsage, error) {
	var row ScriptPresetItemUsage
	err := s.c.db.QueryRowContext(ctx, `
SELECT spi."preset_id", spi."script_id", spi."order", sp."name"
FROM "script_preset_item" spi
INNER JOIN "script_preset" sp ON sp."id" = spi."preset_id"
WHERE spi."script_id" = ?
LIMIT 1`, scriptID).Scan(&row.PresetID, &row.ScriptID, &row.Order, &row.PresetName)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s ScriptPresetItemsStore) InsertMany(ctx context.Context, rows []ScriptPresetItemRow) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		for _, row := range rows {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "script_preset_item" ("preset_id", "script_id", "order") VALUES (?, ?, ?)`,
				row.PresetID, row.ScriptID, row.Order); err != nil {
				return err
			}
		}
		return nil
	})
}

type SchemaStateStore struct{ c *Client }

func (s SchemaStateStore) Get(ctx context.Context, key string) (*SchemaStateRow, error) {
	var row SchemaStateRow
	err := s.c.db.QueryRowContext(ctx, `SELECT "key", "value", "updated_at" FROM "_schema_state" WHERE "key" = ? LIMIT 1`, key).
		Scan(&row.Key, &row.Value, &row.UpdatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s SchemaStateStore) Upsert(ctx context.Context, key, value, updatedAt string) error {
	return s.c.exec(ctx, `INSERT INTO "_schema_state" ("key", "value", "updated_at") VALUES (?, ?, ?)
                 ON CONFLICT("key") DO UPDATE
                 SET "value" = excluded."value", "updated_at" = excluded."updated_at"`, key, value, updatedAt)
}
