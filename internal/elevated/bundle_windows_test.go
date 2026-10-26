//go:build windows

package elevated

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/platform"
)

func TestWriteHelperInstallsAndReusesBinary(t *testing.T) {
	t.Parallel()

	dir, err := platform.FinalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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

// The launch is checked against the image path Windows reports, so a helper below an app data root moved
// behind a junction must be installed at the junction destination. Restoring app data permissions needs
// the helper while the root refuses the account, directly or behind a junction, so extraction then moves
// to the fallback directory instead of failing.
func TestPrepareHelperPicksRunnableDirectory(t *testing.T) {
	t.Parallel()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	deny := func(t *testing.T, path, rights string) {
		t.Helper()
		if output, err := exec.Command(
			"icacls", path, "/deny", "*"+user.User.Sid.String()+":"+rights,
		).CombinedOutput(); err != nil {
			t.Skipf("filesystem cannot deny %s: %v (%s)", rights, err, output)
		}
		t.Cleanup(func() { _ = exec.Command("icacls", path, "/reset").Run() })
	}

	for _, test := range []struct {
		name     string
		junction bool
		rights   string
		fallback bool
	}{
		{name: "denied folder", rights: "(WD,AD)", fallback: true},
		{name: "junction", junction: true},
		{name: "junction to denied folder", junction: true, rights: "(F)", fallback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			home, err := platform.FinalPath(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(home, ".nahida-desktop")
			folder := root
			if test.junction {
				folder = filepath.Join(home, "moved")
			}
			if err := os.Mkdir(folder, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.junction {
				if output, err := exec.Command("cmd", "/c", "mklink", "/J", root, folder).CombinedOutput(); err != nil {
					t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
				}
			}
			if test.rights != "" {
				deny(t, folder, test.rights)
			}

			fallback := filepath.Join(home, "fallback")
			want := filepath.Join(folder, appdata.ElevatedDir)
			if test.fallback {
				want = fallback
			}
			data := []byte("helper-binary")
			path, err := prepareHelper(filepath.Join(root, appdata.ElevatedDir), fallback, data)
			if err != nil {
				t.Fatalf("prepareHelper = %v", err)
			}
			if path != filepath.Join(want, helperFileName(data)) {
				t.Fatalf("prepareHelper path = %q, want below %q", path, want)
			}
		})
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
