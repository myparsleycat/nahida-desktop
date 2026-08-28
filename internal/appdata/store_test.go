package appdata

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesRootAndResolvesLayout(t *testing.T) {
	home := t.TempDir()
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(home, RootDirName)
	if store.Root() != wantRoot {
		t.Fatalf("Root = %q, want %q", store.Root(), wantRoot)
	}
	if info, err := os.Stat(wantRoot); err != nil || !info.IsDir() {
		t.Fatalf("root missing: %v", err)
	}

	for _, relative := range []string{DatabaseFile, LogsDir, ToolsDir, ModBisectDir, NTEModsDir} {
		got, err := store.Resolve(relative)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", relative, err)
		}
		if want := filepath.Join(wantRoot, relative); got != want {
			t.Fatalf("Resolve(%q) = %q, want %q", relative, got, want)
		}
	}
}

func TestStoreReadWriteAndEnsureDir(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir, err := store.EnsureDir(filepath.Join(ToolsDir, "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("directory missing: %v", err)
	}
	relative := filepath.Join("state", "nested", "value.txt")
	if err := store.WriteFile(relative, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadFile(relative)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "value" {
		t.Fatalf("ReadFile = %q", got)
	}
}

func TestStoreRejectsPathsOutsideRoot(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		"",
		".",
		"nested" + string(os.PathSeparator) + "..",
		"..",
		filepath.Join("..", "outside"),
		filepath.Join(store.Root(), "absolute"),
		`C:\outside`,
	}
	for _, relative := range invalid {
		if _, err := store.Resolve(relative); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Resolve(%q) error = %v, want ErrInvalidPath", relative, err)
		}
	}
}

func TestStorePreservesReadErrorCause(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadFile("missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadFile error = %v, want os.ErrNotExist", err)
	}
}
