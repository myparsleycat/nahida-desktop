package db

import (
	"context"
	"database/sql"
)

type ModPresetsStore struct{ c *Client }

func scanModPreset(scanner interface{ Scan(dest ...any) error }) (*ModPresetRow, error) {
	var row ModPresetRow
	var desc sql.NullString
	if err := scanner.Scan(
		&row.ID,
		&row.Game,
		&row.Name,
		&desc,
		&row.ItemCount,
		&row.CreatedAt,
		&row.UpdatedAt,
		&row.Version,
	); err != nil {
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
	return s.c.exec(
		ctx,
		`
INSERT INTO "mod_presets"
("id", "game", "name", "description", "item_count", "created_at", "updated_at", "version")
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID,
		row.Game,
		row.Name,
		argString(row.Description),
		row.ItemCount,
		row.CreatedAt,
		row.UpdatedAt,
		row.Version,
	)
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
		if err := rows.Scan(
			&row.PresetID,
			&row.ModKey,
			&row.RelativePath,
			&row.GroupRelativePath,
			&row.FolderName,
			&enabled,
			&row.ItemOrder,
		); err != nil {
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
			if _, err := tx.ExecContext(
				ctx,
				`
INSERT INTO "mod_preset_items"
("preset_id", "mod_key", "relative_path", "group_relative_path", "folder_name", "is_enabled", "item_order")
VALUES (?, ?, ?, ?, ?, ?, ?)`,
				row.PresetID,
				row.ModKey,
				row.RelativePath,
				row.GroupRelativePath,
				row.FolderName,
				boolToInt(row.IsEnabled),
				row.ItemOrder,
			); err != nil {
				return err
			}
		}
		return nil
	})
}
