//go:build windows

package xxmi

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigureWWMILocalStorageFPSAndWoundedDecision(t *testing.T) {
	ctx := context.Background()
	game := t.TempDir()
	cfg := ImporterConfig{ConfigureGame: true, WWMI: &WWMIOptions{UnlockFPS: true}}
	if err := configureWWMILocalStorage(ctx, game, cfg, true, nil); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(game, "Client", "Saved", "LocalStorage", "LocalStorage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	var fps string
	if err := database.QueryRow("SELECT value FROM LocalStorage WHERE key = ?", "CustomFrameRate").
		Scan(&fps); err != nil ||
		fps != "120" {
		t.Fatalf("CustomFrameRate = %q, error = %v", fps, err)
	}
	if _, err := database.Exec("UPDATE LocalStorage SET value = ? WHERE key = ?", "60", "CustomFrameRate"); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT value FROM LocalStorage WHERE key = ?", "CustomFrameRate").
		Scan(&fps); err != nil ||
		fps != "120" {
		t.Fatalf("locked CustomFrameRate = %q, error = %v", fps, err)
	}
	if _, err := database.Exec("UPDATE LocalStorage SET value = ? WHERE key = ?", "1", "SkinDamageMode"); err != nil {
		t.Fatal(err)
	}
	if err := configureWWMILocalStorage(
		ctx,
		game,
		cfg,
		true,
		nil,
	); err == nil ||
		err.Error() != "WWMI_WOUNDED_FX_DECISION_REQUIRED" {
		t.Fatalf("wounded effect error = %v", err)
	}
	cfg.WoundedFXDecided = true
	cfg.WWMI.DisableWoundedFX = true
	cfg.WWMI.UnlockFPS = false
	if err := configureWWMILocalStorage(ctx, game, cfg, true, nil); err != nil {
		t.Fatal(err)
	}
	var trigger string
	err = database.QueryRow("SELECT name FROM sqlite_master WHERE type = 'trigger' AND name = ?", "CustomFrameRateLock").
		Scan(&trigger)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("FPS trigger after disabling = %q, %v", trigger, err)
	}
	var wounded string
	if err := database.QueryRow("SELECT value FROM LocalStorage WHERE key = ?", "SkinDamageMode").
		Scan(&wounded); err != nil ||
		wounded != "0" {
		t.Fatalf("SkinDamageMode = %q, error = %v", wounded, err)
	}
}

func TestConfigureWWMILocalStorageBypassKeepsFPSWithoutMigotoSettings(t *testing.T) {
	ctx := context.Background()
	game := t.TempDir()
	cfg := ImporterConfig{ConfigureGame: true, WWMI: &WWMIOptions{UnlockFPS: true, ForceMaxLODBias: true}}
	if err := configureWWMILocalStorage(ctx, game, cfg, false, nil); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(game, "Client", "Saved", "LocalStorage", "LocalStorage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	var fps string
	if err := database.QueryRow("SELECT value FROM LocalStorage WHERE key = ?", "CustomFrameRate").
		Scan(&fps); err != nil ||
		fps != "120" {
		t.Fatalf("CustomFrameRate = %q, error = %v", fps, err)
	}
	var count int
	if err := database.QueryRow("SELECT count(*) FROM LocalStorage WHERE key IN (?, ?)", "ImageDetail", "RayTracing").
		Scan(&count); err != nil || count != 0 {
		t.Fatalf("Migoto settings count = %d, error = %v", count, err)
	}
}

func TestConfigureWWMILocalStorageKeepsNewestDatabase(t *testing.T) {
	ctx := context.Background()
	game := t.TempDir()
	folder := filepath.Join(game, "Client", "Saved", "LocalStorage")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, marker := range map[string]string{"LocalStorage.db": "old", "LocalStorage_2026.db": "new"} {
		database, err := sql.Open("sqlite", filepath.Join(folder, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("CREATE TABLE LocalStorage(key text primary key, value text)"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("INSERT INTO LocalStorage(key, value) VALUES(?, ?)", "marker", marker); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(folder, "LocalStorage.db"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "LocalStorage.db-journal"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ImporterConfig{WWMI: &WWMIOptions{}}
	if err := configureWWMILocalStorage(ctx, game, cfg, true, nil); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(folder, "LocalStorage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	var marker string
	if err := database.QueryRow("SELECT value FROM LocalStorage WHERE key = ?", "marker").
		Scan(&marker); err != nil ||
		marker != "new" {
		t.Fatalf("active database marker = %q, err = %v", marker, err)
	}
	for _, name := range []string{"LocalStorage_2026.db", "LocalStorage.db-journal"} {
		if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("stale %s remains: %v", name, err)
		}
	}
}
