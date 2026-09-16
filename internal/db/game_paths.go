package db

import (
	"context"
	"database/sql"
)

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
	game, modFolderPath string,
) (*GamePathRow, error) {
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
			if _, err := tx.ExecContext(
				ctx,
				`UPDATE "game_paths" SET "order" = ? WHERE "game" = ?`,
				i+1,
				game,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s GamePathsStore) Delete(ctx context.Context, game string) error {
	return s.c.exec(ctx, `DELETE FROM "game_paths" WHERE "game" = ?`, game)
}
