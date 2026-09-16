package db

import (
	"context"
	"database/sql"
)

type SettingsStore struct{ c *Client }

func (s SettingsStore) Get(ctx context.Context, key string) (*SettingRow, error) {
	var row SettingRow
	var value sql.NullString
	err := s.c.db.QueryRowContext(ctx, `SELECT "key", "value" FROM "setting" WHERE "key" = ? LIMIT 1`, key).
		Scan(&row.Key, &value)
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
	return s.list(
		ctx,
		`SELECT "key", "value", "updated_at" FROM "app_state" WHERE "key" LIKE ? ORDER BY "key"`,
		prefix+"%",
	)
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

// ApplyBatch atomically applies app-state upserts followed by deletes.
func (s AppStateStore) ApplyBatch(ctx context.Context, upserts []AppStateRow, deleteKeys []string) error {
	if len(upserts) == 0 && len(deleteKeys) == 0 {
		return nil
	}
	return s.c.withImmediate(ctx, func(q queryExec) error {
		for _, row := range upserts {
			if _, err := q.ExecContext(ctx, `INSERT INTO "app_state" ("key", "value", "updated_at") VALUES (?, ?, ?)
                 ON CONFLICT("key") DO UPDATE
                 SET "value" = excluded."value", "updated_at" = excluded."updated_at"`, row.Key, row.Value, row.UpdatedAt); err != nil {
				return err
			}
		}
		for _, key := range deleteKeys {
			if _, err := q.ExecContext(ctx, `DELETE FROM "app_state" WHERE "key" = ?`, key); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s AppStateStore) Delete(ctx context.Context, key string) error {
	return s.c.exec(ctx, `DELETE FROM "app_state" WHERE "key" = ?`, key)
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
