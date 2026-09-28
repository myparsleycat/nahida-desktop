//go:build windows

package xxmi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	_ "modernc.org/sqlite"
)

func configureWWMILocalStorage(ctx context.Context, gameFolder string, cfg ImporterConfig) error {
	folder := filepath.Join(gameFolder, "Client", "Saved", "LocalStorage")
	root, err := ensureInstallRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	directory, err := root.root.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		return err
	}
	type candidate struct {
		name string
		time int64
	}
	var databases []candidate
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "LocalStorage") || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := root.childInfo(entry.Name())
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("invalid WWMI LocalStorage file %s: %w", entry.Name(), err)
		}
		databases = append(databases, candidate{name: entry.Name(), time: info.ModTime().UnixNano()})
	}
	slices.SortFunc(databases, func(a, b candidate) int {
		if a.time > b.time {
			return -1
		}
		if a.time < b.time {
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	if len(databases) > 0 && databases[0].name != "LocalStorage.db" {
		for _, name := range []string{"LocalStorage.db-journal", "LocalStorage.db"} {
			if err := removeWWMIFile(root, name); err != nil {
				return err
			}
		}
		if _, err := root.childInfo(databases[0].name + "-journal"); err == nil {
			if err := root.root.Rename(databases[0].name+"-journal", "LocalStorage.db-journal"); err != nil {
				return err
			}
		}
		if err := root.root.Rename(databases[0].name, "LocalStorage.db"); err != nil {
			return err
		}
	}
	for _, candidate := range databases[min(1, len(databases)):] {
		if candidate.name == "LocalStorage.db" && databases[0].name != "LocalStorage.db" {
			continue
		}
		for _, name := range []string{candidate.name + "-journal", candidate.name} {
			if err := removeWWMIFile(root, name); err != nil {
				return err
			}
		}
	}
	database, err := sql.Open("sqlite", filepath.Join(folder, "LocalStorage.db"))
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	database.SetMaxOpenConns(1)
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	var tableCount int
	if err := transaction.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'LocalStorage'",
	).Scan(&tableCount); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(
		ctx,
		"CREATE TABLE IF NOT EXISTS LocalStorage(key text primary key not null, value text not null)",
	); err != nil {
		return err
	}
	if tableCount == 0 {
		for _, key := range []string{"NotFirstTimeOpenPush", "HasLocalGameSettings", "IsCustomImageQuality"} {
			if err := setWWMIValue(ctx, transaction, key, `"___1B___"`); err != nil {
				return err
			}
		}
	}
	if err := updateWWMIFPS(ctx, transaction, cfg.WWMI.UnlockFPS); err != nil {
		return err
	}
	if cfg.ConfigureGame {
		if cfg.WWMI.ForceMaxLODBias {
			if err := setWWMIValue(ctx, transaction, "ImageDetail", "3"); err != nil {
				return err
			}
		}
		for _, key := range []string{"RayTracing", "RayTracedReflection", "RayTracedGI"} {
			if err := setWWMIValue(ctx, transaction, key, "0"); err != nil {
				return err
			}
		}
		var wounded string
		err := transaction.QueryRowContext(ctx, "SELECT value FROM LocalStorage WHERE key = ?", "SkinDamageMode").
			Scan(&wounded)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if wounded == "1" && !cfg.WWMI.DisableWoundedFX && !cfg.WoundedFXDecided {
			return errors.New("WWMI_WOUNDED_FX_DECISION_REQUIRED")
		}
		value := "1"
		if cfg.WWMI.DisableWoundedFX {
			value = "0"
		}
		if err := setWWMIValue(ctx, transaction, "SkinDamageMode", value); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func removeWWMIFile(root *installRoot, name string) error {
	if _, err := root.childInfo(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return root.root.Remove(name)
}

func setWWMIValue(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(
		ctx,
		"INSERT INTO LocalStorage(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value WHERE value<>excluded.value",
		key,
		value,
	)
	return err
}

func updateWWMIFPS(ctx context.Context, tx *sql.Tx, enabled bool) error {
	rows, err := tx.QueryContext(ctx, "SELECT name, sql FROM sqlite_master WHERE type = 'trigger'")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var remove []string
	for rows.Next() {
		var name, body string
		if err := rows.Scan(&name, &body); err != nil {
			return err
		}
		if strings.Contains(body, "CustomFrameRate") {
			remove = append(remove, name)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, name := range remove {
		quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+quoted); err != nil {
			return err
		}
	}
	if !enabled {
		return nil
	}
	for _, entry := range []struct{ key, value string }{{"MenuData", wwmiMenuData}, {"PlayMenuInfo", wwmiPlayMenuInfo}, {"CustomFrameRate", "120"}} {
		if err := setWWMIValue(ctx, tx, entry.key, entry.value); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE TRIGGER CustomFrameRateLock AFTER UPDATE OF value ON LocalStorage
		WHEN NEW.key = 'CustomFrameRate' BEGIN UPDATE LocalStorage SET value = '120' WHERE key = 'CustomFrameRate'; END`)
	return err
}
