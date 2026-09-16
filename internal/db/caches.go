package db

import (
	"context"
	"database/sql"
	"strings"

	"github.com/samber/lo"
)

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
	return s.c.exec(
		ctx,
		`INSERT OR IGNORE INTO "image_cache" ("hash", "image", "size") VALUES (?, ?, ?)`,
		row.Hash,
		row.Image,
		row.Size,
	)
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
	for _, chunk := range lo.Chunk(paths, modScanCacheQueryChunk) {
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
