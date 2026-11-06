package platform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveNoReplaceKeepsExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := MoveNoReplace(source, destination)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("MoveNoReplace error = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(destination); string(got) != "kept" {
		t.Fatalf("destination = %q, want kept", got)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source should remain: %v", err)
	}

	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := MoveNoReplace(source, destination); err != nil {
		t.Fatalf("MoveNoReplace into free name: %v", err)
	}
	if got, _ := os.ReadFile(destination); string(got) != "new" {
		t.Fatalf("destination = %q, want new", got)
	}
}
