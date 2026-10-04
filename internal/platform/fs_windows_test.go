//go:build windows

package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestHideFileAddsHiddenAttributeWithoutDiscardingExistingAttributes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nhd.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetFileAttributes(name)
	if err != nil {
		t.Fatal(err)
	}

	if err := HideFile(path); err != nil {
		t.Fatal(err)
	}
	after, err := windows.GetFileAttributes(name)
	if err != nil {
		t.Fatal(err)
	}
	if after&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
		t.Fatalf("attributes = %#x, hidden bit is not set", after)
	}
	if after&^windows.FILE_ATTRIBUTE_HIDDEN != before&^windows.FILE_ATTRIBUTE_HIDDEN {
		t.Fatalf("attributes changed beyond hidden bit: before=%#x after=%#x", before, after)
	}

	// A second call must be idempotent.
	if err := HideFile(path); err != nil {
		t.Fatal(err)
	}
}

// A junction is invisible to filepath.EvalSymlinks; FinalPath has to name the folder it points at.
func TestFinalPathFollowsJunction(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "Target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "Link")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	want, err := FinalPath(target)
	if err != nil {
		t.Fatal(err)
	}
	got, err := FinalPath(link)
	if err != nil || !SamePathFold(got, want) {
		t.Fatalf("FinalPath(link) = %q, %v; want %q", got, err, want)
	}
	if _, err := FinalPath(filepath.Join(link, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("FinalPath(missing) err = %v", err)
	}
}
