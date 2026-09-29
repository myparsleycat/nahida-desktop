package db

import (
	"context"
	"database/sql"
	"time"
)

type XXMIImporterRow struct {
	Key       string
	Config    string
	UpdatedAt string
}

type XXMIImportersStore struct{ c *Client }

func (s XXMIImportersStore) Get(ctx context.Context, key string) (*XXMIImporterRow, error) {
	var row XXMIImporterRow
	err := s.c.db.QueryRowContext(ctx, `SELECT "key", "config", "updated_at" FROM "xxmi_importers" WHERE "key" = ?`, key).
		Scan(&row.Key, &row.Config, &row.UpdatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	return &row, err
}

func (s XXMIImportersStore) List(ctx context.Context) ([]XXMIImporterRow, error) {
	rows, err := s.c.query(ctx, `SELECT "key", "config", "updated_at" FROM "xxmi_importers" ORDER BY "key"`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []XXMIImporterRow
	for rows.Next() {
		var row XXMIImporterRow
		if err := rows.Scan(&row.Key, &row.Config, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s XXMIImportersStore) Upsert(ctx context.Context, key, config string) error {
	return s.c.exec(ctx, `INSERT INTO "xxmi_importers" ("key", "config", "updated_at") VALUES (?, ?, ?)
ON CONFLICT("key") DO UPDATE SET "config" = excluded."config", "updated_at" = excluded."updated_at"`,
		key, config, time.Now().UTC().Format(time.RFC3339Nano))
}

func (s XXMIImportersStore) ApplyImport(
	ctx context.Context,
	importers []XXMIImporterRow,
	packages []XXMIPackageRow,
	settings map[string]*string,
) error {
	return s.c.withImmediate(ctx, func(q queryExec) error {
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		for _, row := range importers {
			if _, err := q.ExecContext(
				ctx,
				`INSERT INTO "xxmi_importers" ("key", "config", "updated_at") VALUES (?, ?, ?)
ON CONFLICT("key") DO UPDATE SET "config" = excluded."config", "updated_at" = excluded."updated_at"`,
				row.Key,
				row.Config,
				updatedAt,
			); err != nil {
				return err
			}
		}
		for _, row := range packages {
			if _, err := q.ExecContext(
				ctx,
				`INSERT INTO "xxmi_packages" ("package", "latest_version", "latest_release_notes", "update_check_time", "skipped_version", "updated_at")
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT("package") DO UPDATE SET "latest_version" = excluded."latest_version", "latest_release_notes" = excluded."latest_release_notes",
"update_check_time" = excluded."update_check_time", "skipped_version" = excluded."skipped_version", "updated_at" = excluded."updated_at"`,
				row.Package,
				argString(row.LatestVersion),
				argString(row.LatestReleaseNotes),
				row.UpdateCheckTime,
				argString(row.SkippedVersion),
				updatedAt,
			); err != nil {
				return err
			}
		}
		for key, value := range settings {
			if _, err := q.ExecContext(ctx, `INSERT INTO "setting" ("key", "value") VALUES (?, ?)
ON CONFLICT("key") DO UPDATE SET "value" = excluded."value"`, key, argString(value)); err != nil {
				return err
			}
		}
		return nil
	})
}

// IncrementLaunchCount records one launch. A missing or negative count means "not counted yet", so the
// first recorded launch stores 1.
func (s XXMIImportersStore) IncrementLaunchCount(ctx context.Context, key string) error {
	return s.c.exec(ctx, `UPDATE "xxmi_importers"
SET "config" = json_set("config", '$.launchCount', MAX(COALESCE(json_extract("config", '$.launchCount'), 0), 0) + 1),
    "updated_at" = ?
WHERE "key" = ?`, time.Now().UTC().Format(time.RFC3339Nano), key)
}

func (s XXMIImportersStore) Delete(ctx context.Context, key string) error {
	return s.c.exec(ctx, `DELETE FROM "xxmi_importers" WHERE "key" = ?`, key)
}

// Reset deletes every importer config and package state together with settingKeys in one transaction.
func (s XXMIImportersStore) Reset(ctx context.Context, settingKeys []string) error {
	return s.c.withImmediate(ctx, func(q queryExec) error {
		if _, err := q.ExecContext(ctx, `DELETE FROM "xxmi_importers"`); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM "xxmi_packages"`); err != nil {
			return err
		}
		for _, key := range settingKeys {
			if _, err := q.ExecContext(ctx, `DELETE FROM "setting" WHERE "key" = ?`, key); err != nil {
				return err
			}
		}
		return nil
	})
}

type XXMIPackageRow struct {
	Package            string
	LatestVersion      *string
	LatestReleaseNotes *string
	UpdateCheckTime    int64
	SkippedVersion     *string
	UpdatedAt          string
}

type XXMIPackagesStore struct{ c *Client }

func (s XXMIPackagesStore) Get(ctx context.Context, pkg string) (*XXMIPackageRow, error) {
	var row XXMIPackageRow
	var latest, notes, skipped sql.NullString
	err := s.c.db.QueryRowContext(ctx, `SELECT "package", "latest_version", "latest_release_notes", "update_check_time", "skipped_version", "updated_at" FROM "xxmi_packages" WHERE "package" = ?`, pkg).
		Scan(&row.Package, &latest, &notes, &row.UpdateCheckTime, &skipped, &row.UpdatedAt)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.LatestVersion = ptrString(latest)
	row.LatestReleaseNotes = ptrString(notes)
	row.SkippedVersion = ptrString(skipped)
	return &row, nil
}

func (s XXMIPackagesStore) Upsert(ctx context.Context, row XXMIPackageRow) error {
	return s.c.exec(
		ctx,
		`INSERT INTO "xxmi_packages" ("package", "latest_version", "latest_release_notes", "update_check_time", "skipped_version", "updated_at")
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT("package") DO UPDATE SET "latest_version" = excluded."latest_version", "latest_release_notes" = excluded."latest_release_notes",
"update_check_time" = excluded."update_check_time", "skipped_version" = excluded."skipped_version", "updated_at" = excluded."updated_at"`,
		row.Package,
		argString(row.LatestVersion),
		argString(row.LatestReleaseNotes),
		row.UpdateCheckTime,
		argString(row.SkippedVersion),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
}
