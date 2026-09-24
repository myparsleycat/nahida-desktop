package db

import (
	"context"
	"strings"

	"github.com/samber/lo"
)

// BackupCustomPathsStore keeps the folders the user added to the automatic
// backup beyond the mod folders of the registered games.
type BackupCustomPathsStore struct{ c *Client }

func (s BackupCustomPathsStore) List(ctx context.Context) ([]BackupCustomPathRow, error) {
	rows, err := s.c.query(ctx, `
SELECT "id", "path", "label", "order" FROM "backup_custom_path" ORDER BY "order", "label"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []BackupCustomPathRow{}
	for rows.Next() {
		var row BackupCustomPathRow
		if err := rows.Scan(&row.ID, &row.Path, &row.Label, &row.Order); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Insert adds a folder at the end of the list. A folder that is already listed
// is left as it is.
func (s BackupCustomPathsStore) Insert(ctx context.Context, row BackupCustomPathRow) error {
	return s.c.exec(ctx, `
INSERT INTO "backup_custom_path" ("id", "path", "label", "order")
VALUES (?, ?, ?, (SELECT COALESCE(MAX("order"), -1) + 1 FROM "backup_custom_path"))
ON CONFLICT("path") DO NOTHING`, row.ID, row.Path, row.Label)
}

func (s BackupCustomPathsStore) Delete(ctx context.Context, id string) error {
	return s.c.exec(ctx, `DELETE FROM "backup_custom_path" WHERE "id" = ?`, id)
}

// BackupFileCacheStore remembers the content hash of each backed-up file with
// the size and modification time it was read at, so an unchanged file is not
// hashed again.
type BackupFileCacheStore struct{ c *Client }

const backupFileCacheQueryChunk = 400

func (s BackupFileCacheStore) GetMany(ctx context.Context, paths []string) (map[string]BackupFileCacheRow, error) {
	out := make(map[string]BackupFileCacheRow, len(paths))
	for _, chunk := range lo.Chunk(paths, backupFileCacheQueryChunk) {
		if err := s.getChunk(ctx, chunk, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s BackupFileCacheStore) getChunk(ctx context.Context, paths []string, out map[string]BackupFileCacheRow) error {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(paths)), ",")
	rows, err := s.c.query(ctx, `
SELECT "path", "size", "mtime", "sha256" FROM "backup_file_cache" WHERE "path" IN (`+placeholders+`)`,
		lo.ToAnySlice(paths)...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var row BackupFileCacheRow
		if err := rows.Scan(&row.Path, &row.Size, &row.Mtime, &row.SHA256); err != nil {
			return err
		}
		out[row.Path] = row
	}
	return rows.Err()
}

func (s BackupFileCacheStore) UpsertMany(ctx context.Context, rows []BackupFileCacheRow) error {
	if len(rows) == 0 {
		return nil
	}
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		for _, row := range rows {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO "backup_file_cache" ("path", "size", "mtime", "sha256")
VALUES (?, ?, ?, ?)
ON CONFLICT("path") DO UPDATE
SET "size" = excluded."size", "mtime" = excluded."mtime", "sha256" = excluded."sha256"`,
				row.Path, row.Size, row.Mtime, row.SHA256); err != nil {
				return err
			}
		}
		return nil
	})
}

// BackupCommittedStore keeps the files of the snapshot this PC committed last,
// by target and path. A run backs up only what differs from it.
type BackupCommittedStore struct{ c *Client }

func (s BackupCommittedStore) All(ctx context.Context) ([]BackupCommittedRow, error) {
	rows, err := s.c.query(ctx, `SELECT "target_key", "rel_path", "sha256", "size", "mtime" FROM "backup_committed"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []BackupCommittedRow{}
	for rows.Next() {
		var row BackupCommittedRow
		if err := rows.Scan(&row.TargetKey, &row.RelPath, &row.SHA256, &row.Size, &row.Mtime); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Replace forgets every file and keeps rows instead.
func (s BackupCommittedStore) Replace(ctx context.Context, rows []BackupCommittedRow) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM "backup_committed"`); err != nil {
			return err
		}
		return upsertBackupCommitted(ctx, tx, rows)
	})
}

// Apply records what a committed snapshot changed: the files of the targets
// it no longer covers go, then the removed files, then the new contents.
func (s BackupCommittedStore) Apply(
	ctx context.Context,
	targets []string,
	upserts []BackupCommittedRow,
	deletes []BackupCommittedRow,
) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(targets)), ",")
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM "backup_committed" WHERE "target_key" NOT IN (`+placeholders+`)`,
			lo.ToAnySlice(targets)...); err != nil {
			return err
		}
		for _, row := range deletes {
			if _, err := tx.ExecContext(ctx, `
DELETE FROM "backup_committed" WHERE "target_key" = ? AND "rel_path" = ?`, row.TargetKey, row.RelPath); err != nil {
				return err
			}
		}
		return upsertBackupCommitted(ctx, tx, upserts)
	})
}

func upsertBackupCommitted(ctx context.Context, tx queryExec, rows []BackupCommittedRow) error {
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO "backup_committed" ("target_key", "rel_path", "sha256", "size", "mtime")
VALUES (?, ?, ?, ?, ?)
ON CONFLICT("target_key", "rel_path") DO UPDATE SET
"sha256" = excluded."sha256", "size" = excluded."size", "mtime" = excluded."mtime"`,
			row.TargetKey, row.RelPath, row.SHA256, row.Size, row.Mtime); err != nil {
			return err
		}
	}
	return nil
}
