//go:build windows

package xxmi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEFMIParentExeFindsNewestGryphlinkGamesExe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	launcher := filepath.Join(root, "GRYPHLINK")
	gameExe := filepath.Join(launcher, "games", "EndField Game", "Endfield.exe")
	steamExe := filepath.Join(root, "steamapps", "common", "Endfield", "Endfield.exe")
	for _, file := range []string{
		filepath.Join(launcher, "Launcher.exe"),
		filepath.Join(launcher, "1.6.0", "Games.exe"),
		filepath.Join(launcher, "1.10.0", "Games.exe"),
		filepath.Join(launcher, "Cache", "Games.exe"),
		gameExe,
		steamExe,
	} {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	newest := filepath.Join(launcher, "1.10.0", "Games.exe")
	elsewhere := filepath.Join(root, "Other", "Endfield.exe")

	for name, tc := range map[string]struct {
		folder, gameExe, launch, start string
		want, err                      string
	}{
		"detected from game folder": {gameExe: gameExe, want: newest},
		"configured launcher":       {folder: launcher, gameExe: elsewhere, want: newest},
		"configured version folder": {folder: filepath.Join(launcher, "1.6.0"), gameExe: elsewhere, want: newest},
		"configured without launcher": {
			folder: filepath.Join(root, "Other"), gameExe: gameExe, err: errGameLauncherNotFound,
		},
		"not detected":       {gameExe: elsewhere, err: errGameLauncherNotFound},
		"steam release":      {gameExe: steamExe},
		"shell start":        {gameExe: gameExe, start: "Shell"},
		"started by a store": {gameExe: gameExe, launch: "Epic"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, err := DefaultImporterConfig("EFMI", root)
			if err != nil {
				t.Fatal(err)
			}
			cfg.EFMI.LauncherFolder = tc.folder
			if tc.launch != "" {
				cfg.GameLaunch = tc.launch
			}
			if tc.start != "" {
				cfg.ProcessStartMethod = tc.start
			}
			x := New()
			x.gryphlinkFolders = func() []string { return nil }

			got, err := x.efmiParentExe("EFMI", cfg, tc.gameExe)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("parent = %q, err = %v; want %s", got, err, tc.err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("parent = %q, err = %v; want %q", got, err, tc.want)
			}
		})
	}
}
