//go:build native_kernels && amd64

package nativecompute

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
)

func TestBundledLibraryRepairsCache(t *testing.T) {
	t.Parallel()
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("desktop-kernels-%x.dll", sha256.Sum256(bundledImage))
	relative := filepath.Join(appdata.NativeKernelsDir, name)
	if err := store.WriteFile(relative, []byte("stale image"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(appdata.NativeKernelsDir, "desktop-kernels-00.dll")
	unrelated := filepath.Join(appdata.NativeKernelsDir, "notes.txt")
	for _, file := range []string{previous, unrelated} {
		if err := store.WriteFile(file, []byte("other"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	library := New(store, nil)
	address, err := library.Lookup("desktop_kernels_abi_version")
	if library.handle != 0 {
		t.Cleanup(func() {
			if err := windows.FreeLibrary(library.handle); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil || address == 0 {
		t.Fatalf("look up native compute ABI: address = %x, error = %v", address, err)
	}
	if got, err := store.ReadFile(relative); err != nil || !bytes.Equal(got, bundledImage) {
		t.Fatalf("DLL cache was not restored to the bundled image: %v", err)
	}
	if _, err := store.ReadFile(previous); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("DLL of an earlier bundle was kept: %v", err)
	}
	if _, err := store.ReadFile(unrelated); err != nil {
		t.Fatalf("unrelated file in the DLL cache was removed: %v", err)
	}
	if _, err := library.Lookup("missing_kernel"); err == nil {
		t.Fatal("missing kernel was resolved")
	}
	if got, err := library.Lookup("desktop_kernels_abi_version"); err != nil || got != address {
		t.Fatalf("another lookup after a missing kernel: address = %x, error = %v", got, err)
	}
}
