//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureZZMIGameCreatesAndPreservesSettings(t *testing.T) {
	game := t.TempDir()
	path := filepath.Join(game, "ZenlessZoneZero_Data", "Persistent", "LocalStorage", "GENERAL_DATA.bin")
	if err := configureZZMIGame(context.Background(), game, nil); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeSleepy(first, zzmiSleepyMagic)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := parseSleepyJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	system := settings.field("SystemSettingDataMap")
	for _, id := range []string{"3", "13162", "99"} {
		if system == nil || system.field(id) == nil {
			t.Fatalf("setting %s missing", id)
		}
	}
	if err := configureZZMIGame(context.Background(), game, nil); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("unchanged game settings were rewritten")
	}
}
