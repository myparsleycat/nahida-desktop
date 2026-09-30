//go:build windows

package optimizer

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestWalkINIFollowsLinkedFolders(t *testing.T) {
	root := t.TempDir()
	realMods := filepath.Join(root, "real-mods")
	shared := filepath.Join(root, "shared-mod")
	for _, directory := range []string{filepath.Join(realMods, "Plain"), shared} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(realMods, "Plain", "plain.ini"), filepath.Join(shared, "shared.ini")} {
		if err := os.WriteFile(path, []byte("[Constants]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mods := filepath.Join(root, "importer", "Mods")
	if err := os.MkdirAll(filepath.Dir(mods), 0o700); err != nil {
		t.Fatal(err)
	}
	createJunction(t, realMods, mods)
	createJunction(t, shared, filepath.Join(realMods, "Shared"))
	// A link back to an ancestor must not loop forever.
	createJunction(t, realMods, filepath.Join(realMods, "Plain", "Loop"))

	var visited []string
	err := walkINI(context.Background(), mods, []string{"DISABLED*"}, func(path string, _ fs.FileInfo) error {
		relative, err := filepath.Rel(mods, path)
		visited = append(visited, relative)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	slices.Sort(visited)
	want := []string{filepath.Join("Plain", "plain.ini"), filepath.Join("Shared", "shared.ini")}
	if !slices.Equal(visited, want) {
		t.Fatalf("visited = %v, want %v", visited, want)
	}
}

func createJunction(t *testing.T, target, link string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
}
