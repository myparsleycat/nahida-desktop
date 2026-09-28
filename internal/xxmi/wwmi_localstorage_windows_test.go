//go:build windows

package xxmi

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestConfigureWWMILocalStorageFPSAndWoundedDecision(t *testing.T) {
	ctx := context.Background()
	game := t.TempDir()
	cfg := ImporterConfig{ConfigureGame: true, WWMI: &WWMIOptions{UnlockFPS: true}}
	if err := configureWWMILocalStorage(ctx, game, cfg); err != nil {
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
	); err == nil ||
		err.Error() != "WWMI_WOUNDED_FX_DECISION_REQUIRED" {
		t.Fatalf("wounded effect error = %v", err)
	}
	cfg.WoundedFXDecided = true
	cfg.WWMI.DisableWoundedFX = true
	cfg.WWMI.UnlockFPS = false
	if err := configureWWMILocalStorage(ctx, game, cfg); err != nil {
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
