//go:build windows

package elevated

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWriteHelperInstallsAndReusesBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("helper-binary")

	path, err := writeHelper(dir, data)
	if err != nil {
		t.Fatalf("writeHelper = %v", err)
	}
	if path != filepath.Join(dir, helperFileName(data)) {
		t.Fatalf("writeHelper path = %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed helper: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("installed helper = %q, want %q", got, data)
	}

	// A matching file must be reused rather than rewritten, so a live helper
	// that still holds the binary open is not disturbed on restart.
	if reused, err := writeHelper(dir, data); err != nil || reused != path {
		t.Fatalf("writeHelper reuse = %q, %v", reused, err)
	}

	// Changed contents install a new content-addressed file and drop the old
	// one, so the replacement never targets an image a helper still holds.
	updated := []byte("helper-binary-v2")
	updatedPath, err := writeHelper(dir, updated)
	if err != nil {
		t.Fatalf("writeHelper replace = %v", err)
	}
	if updatedPath == path {
		t.Fatal("writeHelper reused the path for different contents")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale helper was not removed: %v", err)
	}
	got, err = os.ReadFile(updatedPath)
	if err != nil {
		t.Fatalf("read replaced helper: %v", err)
	}
	if string(got) != string(updated) {
		t.Fatalf("replaced helper = %q, want %q", got, updated)
	}
}

func TestWriteHelperRejectsEmptyPayload(t *testing.T) {
	t.Parallel()

	if _, err := writeHelper(t.TempDir(), nil); !errors.Is(err, errHelperNotBundled) {
		t.Fatalf("writeHelper = %v, want %v", err, errHelperNotBundled)
	}
}

func TestBundledHelperMatchesBundleState(t *testing.T) {
	t.Parallel()

	// Development and test builds embed only the placeholder, while a packaged
	// build embeds the real helper. Accept either, but never install an empty
	// executable.
	data, err := bundledHelper()
	if errors.Is(err, errHelperNotBundled) {
		return
	}
	if err != nil {
		t.Fatalf("bundledHelper = %v", err)
	}
	path, err := writeHelper(t.TempDir(), data)
	if err != nil {
		t.Fatalf("writeHelper = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat extracted helper: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("extracted helper is empty")
	}
}

func TestVerifyHelperHandleDetectsTampering(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := []byte("helper-binary")
	path, err := writeHelper(dir, data)
	if err != nil {
		t.Fatalf("writeHelper = %v", err)
	}
	verify := func() error {
		file, openErr := os.Open(path)
		if openErr != nil {
			t.Fatalf("open helper: %v", openErr)
		}
		defer func() { _ = file.Close() }()
		return verifyHelperHandle(windows.Handle(file.Fd()), data)
	}
	if err := verify(); err != nil {
		t.Fatalf("verifyHelperHandle = %v", err)
	}

	if err := os.WriteFile(path, []byte("tampered"), 0o700); err != nil {
		t.Fatalf("tamper helper: %v", err)
	}
	if err := verify(); err == nil {
		t.Fatal("verifyHelperHandle accepted a tampered helper")
	}
}
