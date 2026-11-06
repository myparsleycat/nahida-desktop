//go:build windows

package mod

import (
	"os"
	"path/filepath"
	"testing"
)

func runNteScriptForTest(t *testing.T, script string) {
	t.Helper()
	if code, err := executeNtePowerShell(script, false); err != nil {
		t.Fatalf("script exit %d: %v", code, err)
	}
}

func TestNteLinkAndUnlinkKeepModContent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Library")
	link := filepath.Join(root, "Game", "Mods")
	writeFile(t, filepath.Join(link, "Char", "mod.ini"), "game")

	runNteScriptForTest(t, nteLinkScript(target, link))
	if got, err := os.ReadFile(filepath.Join(target, "Char", "mod.ini")); err != nil || string(got) != "game" {
		t.Fatalf("linked content = %q, %v", got, err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeIrregular == 0 && info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link is not a junction: %v, %v", info, err)
	}

	runNteScriptForTest(t, nteUnlinkScript(link))
	if got, err := os.ReadFile(filepath.Join(link, "Char", "mod.ini")); err != nil || string(got) != "game" {
		t.Fatalf("unlinked content = %q, %v", got, err)
	}
	if info, err := os.Lstat(link); err != nil || !info.IsDir() {
		t.Fatalf("unlinked folder = %v, %v", info, err)
	}
	if _, err := os.Stat(link + ".nte-unlink"); !os.IsNotExist(err) {
		t.Fatalf("staging folder remains: %v", err)
	}
}

func TestNteUnlinkFinishesInterruptedRun(t *testing.T) {
	link := filepath.Join(t.TempDir(), "Mods")
	writeFile(t, filepath.Join(link+".nte-unlink", "Char", "mod.ini"), "staged")

	runNteScriptForTest(t, nteUnlinkScript(link))
	if got, err := os.ReadFile(filepath.Join(link, "Char", "mod.ini")); err != nil || string(got) != "staged" {
		t.Fatalf("resumed content = %q, %v", got, err)
	}
}

func TestNteLinkFinishesInterruptedRun(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Library")
	link := filepath.Join(root, "Game", "Mods")
	writeFile(t, filepath.Join(target+".nte-link", "Moved", "mod.ini"), "moved")
	writeFile(t, filepath.Join(link, "Left", "mod.ini"), "left")

	runNteScriptForTest(t, nteLinkScript(target, link))
	for name, want := range map[string]string{"Moved": "moved", "Left": "left"} {
		if got, err := os.ReadFile(filepath.Join(target, name, "mod.ini")); err != nil || string(got) != want {
			t.Fatalf("%s content = %q, %v", name, got, err)
		}
	}
}

func TestNteLinkResumeLeavesGameModsWhenLibraryIsNotEmpty(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Library")
	link := filepath.Join(root, "Game", "Mods")
	writeFile(t, filepath.Join(target+".nte-link", "Moved", "mod.ini"), "moved")
	writeFile(t, filepath.Join(link, "Left", "mod.ini"), "left")
	writeFile(t, filepath.Join(target, "Other", "mod.ini"), "other")

	if code, _ := executeNtePowerShell(nteLinkScript(target, link), false); code != 32 {
		t.Fatalf("exit = %d, want 32", code)
	}
	if got, err := os.ReadFile(filepath.Join(link, "Left", "mod.ini")); err != nil || string(got) != "left" {
		t.Fatalf("game mods after a refused resume = %q, %v", got, err)
	}
}

func TestNteLinkConflictAfterMoveReturnsGameMods(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Library")
	link := filepath.Join(root, "Game", "Mods")
	writeFile(t, filepath.Join(target+".nte-link", "Moved", "mod.ini"), "moved")
	writeFile(t, filepath.Join(target, "Other", "mod.ini"), "other")

	if code, _ := executeNtePowerShell(nteLinkScript(target, link), false); code != 32 {
		t.Fatalf("exit = %d, want 32", code)
	}
	if got, err := os.ReadFile(filepath.Join(link, "Moved", "mod.ini")); err != nil || string(got) != "moved" {
		t.Fatalf("game mods after the conflict = %q, %v", got, err)
	}
	if _, err := os.Stat(target + ".nte-link"); !os.IsNotExist(err) {
		t.Fatalf("staging folder remains: %v", err)
	}
}

func TestNteLinkKeepsWorkingJunctionWithLeftoverStaging(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Library")
	link := filepath.Join(root, "Game", "Mods")
	writeFile(t, filepath.Join(target, "Kept", "mod.ini"), "kept")
	runNteScriptForTest(t, nteLinkScript(target, link))
	writeFile(t, filepath.Join(target+".nte-link", "Stale", "mod.ini"), "stale")

	if code, _ := executeNtePowerShell(nteLinkScript(target, link), false); code != 32 {
		t.Fatalf("exit = %d, want 32", code)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeIrregular == 0 && info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link is no longer a junction: %v, %v", info, err)
	}
	if got, err := os.ReadFile(
		filepath.Join(target+".nte-link", "Stale", "mod.ini"),
	); err != nil ||
		string(got) != "stale" {
		t.Fatalf("staged content = %q, %v", got, err)
	}
}
