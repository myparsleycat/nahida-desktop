//go:build windows

package optimizer

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
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

func TestOptimizeSkipsUnreadableINI(t *testing.T) {
	root := t.TempDir()
	libraries := filepath.Join(root, "Core", "ZZMI", "Libraries")
	mods := filepath.Join(root, "Mods")
	for _, folder := range []string{libraries, filepath.Join(mods, "Rogue"), filepath.Join(mods, "Copy")} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stale := filepath.Join(libraries, "SlotFix.ini")
	duplicate := filepath.Join(mods, "Copy", "SlotFix.ini")
	for path, content := range map[string]string{
		stale:     "namespace = SlotFix\n",
		duplicate: "namespace = SlotFix\n",
		filepath.Join(mods, "Rogue", "rogue.ini"): "[Loader]\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("icacls", stale, "/deny", "*"+current.Uid+":(RD)").CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot deny reading a file: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", stale, "/remove:d", "*"+current.Uid).Run() })
	if _, err := os.ReadFile(stale); err == nil {
		t.Skip("filesystem does not enforce the read denial")
	}

	var skipped []string
	options := Options{
		Importer: "ZZMI", ImporterFolder: root, CachePath: filepath.Join(root, "cache.json"), Prefix: "DISABLED_",
		Unreadable: func(path string, _ error) { skipped = append(skipped, path) },
	}
	report, err := Optimize(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(skipped, []string{stale}) {
		t.Fatalf("skipped = %v, want %v", skipped, []string{stale})
	}
	if report.DisabledFiles != 1 {
		t.Fatalf("report = %+v, want only the readable rogue file disabled", report)
	}

	// Once the library is readable, the mod checked without its namespace is checked again despite the cache.
	if output, err := exec.Command("icacls", stale, "/remove:d", "*"+current.Uid).CombinedOutput(); err != nil {
		t.Fatalf("restore read access: %v (%s)", err, output)
	}
	if _, err := Optimize(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mods, "Copy", "DISABLED_SlotFix.ini")); err != nil {
		t.Fatalf("duplicate library namespace was not disabled: %v", err)
	}
}

func createJunction(t *testing.T, target, link string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
}
