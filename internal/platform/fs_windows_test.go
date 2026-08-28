//go:build windows

package platform

import (
	"os"
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
