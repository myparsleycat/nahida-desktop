package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ModTogglePresetsStore struct{ c *Client }

const modTogglePresetColumns = `"id", "game", "mod_key", "mod_name", "name", "source", "inis", "created_at", "updated_at"`

func (s ModTogglePresetsStore) list(ctx context.Context, query string, args ...any) ([]ModTogglePresetRow, error) {
	rows, err := s.c.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ModTogglePresetRow
	for rows.Next() {
		var row ModTogglePresetRow
		if err := rows.Scan(
			&row.ID, &row.Game, &row.ModKey, &row.ModName, &row.Name, &row.Source, &row.INIs,
			&row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s ModTogglePresetsStore) ListByMod(ctx context.Context, game, modKey string) ([]ModTogglePresetRow, error) {
	return s.list(ctx, `SELECT `+modTogglePresetColumns+`
FROM "mod_toggle_presets" WHERE "game" = ? AND "mod_key" = ? ORDER BY "name"`, game, modKey)
}

func (s ModTogglePresetsStore) ListAll(ctx context.Context) ([]ModTogglePresetRow, error) {
	return s.list(ctx, `SELECT `+modTogglePresetColumns+`
FROM "mod_toggle_presets" ORDER BY "game", "mod_key", "name"`)
}

// Remaps are persisted before copying so failed imports reuse their IDs after a restart.
func (s ModTogglePresetsStore) ResolveImportIDs(
	ctx context.Context,
	game, modKey string,
	sourceIDs []string,
) (map[string]string, error) {
	resolved := make(map[string]string, len(sourceIDs))
	if len(sourceIDs) == 0 {
		return resolved, nil
	}
	const prefix = "toggle-preset-import-id:"
	requested := make(map[string]bool, len(sourceIDs))
	keys := make(map[string]string, len(sourceIDs))
	var unique []string
	for _, id := range sourceIDs {
		if requested[id] {
			continue
		}
		encoded, err := json.Marshal([3]string{game, modKey, id})
		if err != nil {
			return nil, err
		}
		keys[id] = prefix + base64.RawURLEncoding.EncodeToString(encoded)
		requested[id] = true
		unique = append(unique, id)
	}

	err := s.c.withImmediate(ctx, func(tx queryExec) error {
		mappings := make(map[string]string)
		reserved := make(map[string]bool)
		reservedByOther := make(map[string]bool)
		rows, err := tx.QueryContext(ctx, `SELECT "key", "value" FROM "app_state" WHERE "key" LIKE ?`, prefix+"%")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var key, target string
			if err := rows.Scan(&key, &target); err != nil {
				return err
			}
			mappings[key] = target
			reserved[target] = true
			encoded, decodeErr := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, prefix))
			var owner []string
			if decodeErr != nil || json.Unmarshal(encoded, &owner) != nil || len(owner) != 3 ||
				owner[0] != game || owner[1] != modKey {
				reservedByOther[target] = true
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}

		assigned := make(map[string]bool, len(unique))
		available := func(id string, allowOwned bool) (bool, error) {
			// Exported destination IDs remain usable by their owner before rows are inserted.
			if assigned[id] || reservedByOther[id] || (!allowOwned && reserved[id]) {
				return false, nil
			}
			var occupied bool
			err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM "mod_toggle_presets"
WHERE "id" = ? AND (? OR "game" <> ? OR "mod_key" <> ?))`,
				id, !allowOwned, game, modKey).Scan(&occupied)
			return !occupied, err
		}
		for _, id := range unique {
			target, mapped := mappings[keys[id]]
			ok := false
			var err error
			if mapped && (target == id || !requested[target]) {
				ok, err = available(target, true)
				if err != nil {
					return err
				}
			}
			if !mapped {
				target = id
				ok, err = available(target, true)
				if err != nil {
					return err
				}
			}
			for !ok {
				target = uuid.NewString()
				if requested[target] {
					continue
				}
				ok, err = available(target, false)
				if err != nil {
					return err
				}
			}
			if target != id || mapped {
				if _, err := tx.ExecContext(ctx, `INSERT INTO "app_state" ("key", "value", "updated_at")
VALUES (?, ?, ?) ON CONFLICT("key") DO UPDATE
SET "value" = excluded."value", "updated_at" = excluded."updated_at"`,
					keys[id], target, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
					return err
				}
			}
			assigned[target] = true
			resolved[id] = target
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

// ReplaceMod makes rows the presets of one mod in one immediate transaction.
func (s ModTogglePresetsStore) ReplaceMod(ctx context.Context, game, modKey string, rows []ModTogglePresetRow) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "mod_toggle_presets" WHERE "game" = ? AND "mod_key" = ?`,
			game, modKey); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "mod_toggle_presets" (`+modTogglePresetColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				row.ID, game, modKey, row.ModName, row.Name, row.Source, row.INIs, row.CreatedAt,
				row.UpdatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s ModTogglePresetsStore) DeleteByModIDs(ctx context.Context, game, modKey string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, len(ids)+2)
	args[0], args[1] = game, modKey
	for i, id := range ids {
		args[i+2] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	return s.c.exec(ctx, `DELETE FROM "mod_toggle_presets"
WHERE "game" = ? AND "mod_key" = ? AND "id" IN (`+placeholders+`)`, args...)
}
