//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGameTweakErrorsKeepImporterCode(t *testing.T) {
	zzmiFolder := t.TempDir()
	settingsFolder := filepath.Join(zzmiFolder, "ZenlessZoneZero_Data", "Persistent", "LocalStorage")
	if err := os.MkdirAll(settingsFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settingsFolder, "GENERAL_DATA.bin"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		key  string
		cfg  ImporterConfig
		code string
	}{
		{
			name: "HIMI invalid FPS", key: "HIMI",
			cfg:  ImporterConfig{HIMI: &HIMIOptions{UnlockFPS: true, UnlockFPSValue: 20}},
			code: "HIMI_FPS_UNLOCK_FAILED",
		},
		{
			name: "ZZMI missing settings", key: "ZZMI",
			cfg:  ImporterConfig{ConfigureGame: true, GameFolder: zzmiFolder},
			code: "ZZMI_GAME_CONFIG_FAILED",
		},
		{
			name: "WWMI missing game", key: "WWMI",
			cfg:  ImporterConfig{GameFolder: t.TempDir(), WWMI: &WWMIOptions{}},
			code: "WWMI_GAME_CONFIG_FAILED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := initializeGameLaunch(context.Background(), tc.key, tc.cfg, true, nil)
			if err == nil || !strings.Contains(err.Error(), tc.code) {
				t.Fatalf("game tweak error = %v, want %s", err, tc.code)
			}
		})
	}
}
