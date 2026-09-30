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

func TestValidateWWMIGameFolderNormalizesChildAndAncestor(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "Wuthering Waves Game")
	for _, folder := range []string{"Client", "Engine", filepath.Join("Client", "Binaries", "Win64"), "Saved", filepath.Join("Saved", "Nested")} {
		if err := os.MkdirAll(filepath.Join(game, folder), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(game, "Wuthering Waves.exe")
	for _, path := range []string{executable, filepath.Join(game, "Client", "Binaries", "Win64", "Client-Win64-Shipping.exe")} {
		if err := os.WriteFile(path, []byte("game"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, selected := range []string{root, filepath.Join(game, "Saved", "Nested")} {
		candidate, err := New().ValidateGameFolder(context.Background(), "WWMI", selected)
		if err != nil || candidate.Path != game || candidate.ExePath != executable {
			t.Fatalf("selected %s: candidate = %+v, err = %v", selected, candidate, err)
		}
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
