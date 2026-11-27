package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	ErrDuplicateGameName = errors.New("DUPLICATE_GAME_NAME")
	ErrGameNotFound      = errors.New("GAME_NOT_FOUND")
)

type GamePathsStore struct{ c *Client }

const gamePathSelectCols = `"id", "game", "legacy_name", "modFolderPath", "importer",
       "linkedModFolderPath", "gameInstallPath", "gameExecutablePath", "nteLauncherPath"`

func scanGamePath(scanner interface{ Scan(dest ...any) error }) (*GamePathRow, error) {
	var row GamePathRow
	var legacyName, importer, linked, install, exe, nte sql.NullString
	dest := []any{
		&row.ID, &row.Game, &legacyName, &row.ModFolderPath, &importer, &linked, &install, &exe, &nte, &row.Order,
	}
	if err := scanner.Scan(dest...); err != nil {
		return nil, err
	}
	row.LegacyName = ptrString(legacyName)
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

func (s GamePathsStore) GetByID(ctx context.Context, gameID string) (*GamePathRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT `+gamePathSelectCols+`,
       CASE WHEN "order" = 0 THEN rowid ELSE "order" END AS "order"
FROM "game_paths" WHERE "id" = ? LIMIT 1`, gameID)
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

func (s GamePathsStore) FindByGameOrModFolderPath(
	ctx context.Context,
	game, modFolderPath string,
) (*GamePathRow, error) {
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

func (s GamePathsStore) FindByModFolderPathOtherGame(
	ctx context.Context,
	gameID, modFolderPath string,
) (*GamePathRow, error) {
	row := s.c.db.QueryRowContext(ctx, `
SELECT `+gamePathSelectCols+`, "order"
FROM "game_paths"
WHERE ("modFolderPath" = ? OR "linkedModFolderPath" = ?) AND "id" <> ?
LIMIT 1`, modFolderPath, modFolderPath, gameID)
	out, err := scanGamePath(row)
	if isNoRows(err) {
		return nil, nil
	}
	return out, err
}

func (s GamePathsStore) Insert(ctx context.Context, row GamePathRow) error {
	if row.ID == "" {
		row.ID = uuid.NewString()
	}
	return gamePathWriteError(s.c.exec(ctx, `
INSERT INTO "game_paths" (
    "id", "game", "legacy_name", "modFolderPath", "importer", "linkedModFolderPath",
    "gameInstallPath", "gameExecutablePath", "nteLauncherPath", "order"
)
SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(MAX(CASE WHEN "order" = 0 THEN rowid ELSE "order" END), 0) + 1
FROM "game_paths"`,
		row.ID, row.Game, argString(row.LegacyName), row.ModFolderPath, argString(row.Importer),
		argString(row.LinkedModFolderPath), argString(row.GameInstallPath), argString(row.GameExecutablePath),
		argString(row.NteLauncherPath)))
}

func (s GamePathsStore) Upsert(ctx context.Context, row GamePathRow) error {
	if row.ID == "" {
		row.ID = uuid.NewString()
	}
	return s.c.exec(ctx, `
INSERT INTO "game_paths" (
    "id", "game", "legacy_name", "modFolderPath", "importer", "linkedModFolderPath",
    "gameInstallPath", "gameExecutablePath", "nteLauncherPath", "order"
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT("game") DO UPDATE
SET "modFolderPath" = excluded."modFolderPath",
    "importer" = excluded."importer",
    "linkedModFolderPath" = excluded."linkedModFolderPath",
    "gameInstallPath" = excluded."gameInstallPath",
    "gameExecutablePath" = excluded."gameExecutablePath",
    "nteLauncherPath" = excluded."nteLauncherPath",
    "order" = excluded."order"`,
		row.ID, row.Game, argString(row.LegacyName), row.ModFolderPath, argString(row.Importer),
		argString(row.LinkedModFolderPath), argString(row.GameInstallPath), argString(row.GameExecutablePath),
		argString(row.NteLauncherPath), row.Order)
}

type GamePathUpdates struct {
	ModFolderPath       string
	Importer            *string
	LinkedModFolderPath *string
	GameInstallPath     *string
	GameExecutablePath  *string
}

func (s GamePathsStore) Update(ctx context.Context, gameID string, updates GamePathUpdates) error {
	return s.c.exec(ctx, `
UPDATE "game_paths"
SET "modFolderPath" = ?, "importer" = ?, "linkedModFolderPath" = ?, "gameInstallPath" = ?, "gameExecutablePath" = ?
WHERE "id" = ?`,
		updates.ModFolderPath, argString(updates.Importer), argString(updates.LinkedModFolderPath),
		argString(updates.GameInstallPath), argString(updates.GameExecutablePath), gameID)
}

// MoveModFolderPath points the game at to only while its row still holds the importer and the from folder the caller
// read, and no other game uses to, so an edit saved meanwhile is never overwritten. It reports whether it moved.
func (s GamePathsStore) MoveModFolderPath(
	ctx context.Context,
	gameID string,
	importer *string,
	from, to string,
) (bool, error) {
	result, err := s.c.db.ExecContext(ctx, `
UPDATE "game_paths" SET "modFolderPath" = ?
WHERE "id" = ? AND "modFolderPath" = ? AND "importer" IS ?
  AND NOT EXISTS (
    SELECT 1 FROM "game_paths" AS "other"
    WHERE "other"."id" <> ? AND ("other"."modFolderPath" = ? OR "other"."linkedModFolderPath" = ?)
  )`,
		to, gameID, from, argString(importer), gameID, to, to)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// AdoptImporter assigns an importer only while the game has no importer and holds the folder the caller
// read. It reports whether it assigned.
func (s GamePathsStore) AdoptImporter(ctx context.Context, gameID, modFolderPath, importer string) (bool, error) {
	result, err := s.c.db.ExecContext(ctx, `
UPDATE "game_paths" SET "importer" = ?
WHERE "id" = ? AND "modFolderPath" = ? AND ("importer" IS NULL OR trim("importer") = '')`,
		importer, gameID, modFolderPath)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

func (s GamePathsStore) SetNteLauncherPath(ctx context.Context, gameID, nteLauncherPath string) error {
	return s.c.exec(ctx, `UPDATE "game_paths" SET "nteLauncherPath" = ? WHERE "id" = ?`, nteLauncherPath, gameID)
}

func (s GamePathsStore) Reorder(ctx context.Context, games []string) error {
	return s.c.withImmediate(ctx, func(tx queryExec) error {
		for i, gameID := range games {
			if _, err := tx.ExecContext(
				ctx,
				`UPDATE "game_paths" SET "order" = ? WHERE "id" = ?`,
				i+1,
				gameID,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s GamePathsStore) Delete(ctx context.Context, gameID string) error {
	return s.c.exec(ctx, `DELETE FROM "game_paths" WHERE "id" = ?`, gameID)
}

func (s GamePathsStore) Rename(ctx context.Context, gameID, name string) error {
	result, err := s.c.db.ExecContext(ctx, `UPDATE "game_paths" SET "game" = ? WHERE "id" = ?`, name, gameID)
	if err != nil {
		return gamePathWriteError(err)
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return ErrGameNotFound
	}
	return err
}

func gamePathWriteError(err error) error {
	if constraint, ok := errors.AsType[*sqlite.Error](
		err,
	); ok &&
		constraint.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return ErrDuplicateGameName
	}
	return err
}
