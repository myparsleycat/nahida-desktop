package db

import "context"

type ToggleViewerArtifactsStore struct{ c *Client }

func scanToggleViewerArtifact(scanner interface{ Scan(dest ...any) error }) (*ToggleViewerArtifactRow, error) {
	var row ToggleViewerArtifactRow
	if err := scanner.Scan(
		&row.ID,
		&row.TargetIniPath,
		&row.ToggleTxtPath,
		&row.ToggleIniPath,
		&row.ToggleTxtHash,
		&row.ToggleIniHash,
		&row.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &row, nil
}

func (s ToggleViewerArtifactsStore) List(ctx context.Context) ([]ToggleViewerArtifactRow, error) {
	rows, err := s.c.query(ctx, `
SELECT "id", "target_ini_path", "toggle_txt_path", "toggle_ini_path", "toggle_txt_hash", "toggle_ini_hash", "updated_at"
FROM "toggle_viewer_artifact" ORDER BY "target_ini_path"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ToggleViewerArtifactRow
	for rows.Next() {
		row, err := scanToggleViewerArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func (s ToggleViewerArtifactsStore) Upsert(ctx context.Context, row ToggleViewerArtifactRow) error {
	return s.c.exec(ctx, `
INSERT INTO "toggle_viewer_artifact"
("id", "target_ini_path", "toggle_txt_path", "toggle_ini_path", "toggle_txt_hash", "toggle_ini_hash", "updated_at")
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT("target_ini_path") DO UPDATE
SET "toggle_txt_path" = excluded."toggle_txt_path",
    "toggle_ini_path" = excluded."toggle_ini_path",
    "toggle_txt_hash" = excluded."toggle_txt_hash",
    "toggle_ini_hash" = excluded."toggle_ini_hash",
    "updated_at" = excluded."updated_at"`,
		row.ID, row.TargetIniPath, row.ToggleTxtPath, row.ToggleIniPath, row.ToggleTxtHash, row.ToggleIniHash,
		row.UpdatedAt)
}

func (s ToggleViewerArtifactsStore) UpdateIniHash(ctx context.Context, id, toggleIniHash, updatedAt string) error {
	return s.c.exec(ctx, `
UPDATE "toggle_viewer_artifact"
SET "toggle_ini_hash" = ?, "updated_at" = ?
WHERE "id" = ?`, toggleIniHash, updatedAt, id)
}

func (s ToggleViewerArtifactsStore) Delete(ctx context.Context, id, targetIniPath string) error {
	return s.c.exec(
		ctx,
		`DELETE FROM "toggle_viewer_artifact" WHERE "id" = ? AND "target_ini_path" = ?`,
		id,
		targetIniPath,
	)
}
