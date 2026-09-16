package db

import (
	"context"
	"database/sql"

	"github.com/samber/lo"
)

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
	out := make([]ScriptBasicRow, 0)
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
	return s.c.exec(
		ctx,
		`
INSERT INTO "script"
("id", "name", "source", "is_src_zstd", "type", "size", "zstd_size", "sha256", "zstd_sha256")
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID,
		row.Name,
		row.Source,
		lo.Ternary(row.IsSrcZstd, 1, 0),
		string(row.Type),
		row.Size,
		argInt64(row.ZstdSize),
		row.SHA256,
		argString(row.ZstdSHA256),
	)
}

func (s ScriptsStore) UpdateCompressedSource(
	ctx context.Context,
	id string,
	source []byte,
	zstdSHA256 string,
	zstdSize int64,
) error {
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

	itemRows, err := s.c.query(
		ctx,
		`SELECT "preset_id", "script_id", "order" FROM "script_preset_item" ORDER BY "preset_id", "order"`,
	)
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

	grouped := lo.GroupBy(items, func(item ScriptPresetItemRow) string { return item.PresetID })
	out := make([]ScriptPresetWithScripts, 0, len(presets))
	for _, preset := range presets {
		scripts := grouped[preset.ID]
		if scripts == nil {
			scripts = make([]ScriptPresetItemRow, 0)
		}
		out = append(out, ScriptPresetWithScripts{ScriptPresetRow: preset, Scripts: scripts})
	}

	return out, nil
}

func (s ScriptPresetsStore) FindByID(ctx context.Context, id string) (*ScriptPresetRow, error) {
	var row ScriptPresetRow
	err := s.c.db.QueryRowContext(ctx, `SELECT "id", "name" FROM "script_preset" WHERE "id" = ? LIMIT 1`, id).
		Scan(&row.ID, &row.Name)
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
	err := s.c.db.QueryRowContext(ctx, `SELECT "id", "name" FROM "script_preset" WHERE "name" = ? LIMIT 1`, name).
		Scan(&row.ID, &row.Name)
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
func (s ScriptPresetsStore) InsertSnapshot(
	ctx context.Context,
	row ScriptPresetRow,
	items []ScriptPresetItemRow,
) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO "script_preset" ("id", "name") VALUES (?, ?)`,
			row.ID,
			row.Name,
		); err != nil {
			return err
		}
		for _, item := range items {
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO "script_preset_item" ("preset_id", "script_id", "order") VALUES (?, ?, ?)`,
				item.PresetID,
				item.ScriptID,
				item.Order,
			); err != nil {
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
	out := make([]ScriptPresetItemRow, 0)
	for rows.Next() {
		var row ScriptPresetItemRow
		if err := rows.Scan(&row.PresetID, &row.ScriptID, &row.Order); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s ScriptPresetItemsStore) FindUsageByScriptID(
	ctx context.Context,
	scriptID string,
) (*ScriptPresetItemUsage, error) {
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
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO "script_preset_item" ("preset_id", "script_id", "order") VALUES (?, ?, ?)`,
				row.PresetID,
				row.ScriptID,
				row.Order,
			); err != nil {
				return err
			}
		}
		return nil
	})
}
