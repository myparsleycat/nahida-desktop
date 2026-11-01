package modmesh

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func linkJunction(t *testing.T, link, target string) {
	t.Helper()
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
}

func TestResolveResourceBelowJunctionKeepsCallerRoot(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	stored := filepath.Join(base, "Stored", "Mod")
	if err := os.MkdirAll(stored, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stored, "Position.buf"), make([]byte, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "Outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "Escaped.buf"), make([]byte, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(base, "Mods")
	linkJunction(t, mods, filepath.Dir(stored))
	linkJunction(t, filepath.Join(stored, "Escape"), outside)

	root := filepath.Join(mods, "Mod")
	resolved, err := ResolveResource(root, "Position.buf")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "Position.buf"); resolved != want {
		t.Fatalf("resolved = %q, want %q", resolved, want)
	}
	if _, err := ResolveResource(root, "Escape/Escaped.buf"); err == nil {
		t.Fatal("resource behind a junction leaving the mod was accepted")
	}
}
