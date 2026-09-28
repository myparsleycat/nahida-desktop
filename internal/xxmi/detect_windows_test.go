//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateGameFolder(t *testing.T) {
	game := t.TempDir()
	executable := filepath.Join(game, "GenshinImpact.exe")
	if err := os.WriteFile(executable, []byte("game"), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := New().ValidateGameFolder(context.Background(), "GIMI", game)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Path != game || candidate.ExePath != executable {
		t.Fatalf("candidate = %+v", candidate)
	}
	if _, err := New().ValidateGameFolder(context.Background(), "SRMI", game); err == nil {
		t.Fatal("accepted Genshin folder for SRMI")
	}
}

func TestReadGamePathHints(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "Genshin Impact game")
	logPath := filepath.Join(root, "output_log.txt")
	data := `TelemetryInterface path:` + filepath.Join(game, "GenshinImpact_Data", "SDKCaches")
	if err := os.WriteFile(logPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var candidates []string
	readGamePathHints(logPath, "GIMI", func(path string) { candidates = append(candidates, path) })
	if len(candidates) != 1 || !strings.EqualFold(strings.TrimRight(candidates[0], `\/`), game) {
		t.Fatalf("detected candidates = %q, want %q", candidates, game)
	}
}

func TestReadHoYoPlayPathHint(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gamedata.dat")
	game := filepath.Join(root, "ZenlessZoneZero Game")
	data := `{"installPath":"` + strings.ReplaceAll(game, `\`, `\\`) + `"}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var candidates []string
	readGamePathHints(path, "ZZMI", func(path string) { candidates = append(candidates, path) })
	if len(candidates) == 0 || !strings.EqualFold(candidates[0], game) {
		t.Fatalf("detected candidates = %q, want %q", candidates, game)
	}
}
