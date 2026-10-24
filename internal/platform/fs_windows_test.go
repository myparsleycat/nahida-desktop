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

// Only a permission denial is reported: a restore cannot fix a read-only attribute or a missing file,
// and startup must not ask for administrator approval for them.
func TestAccessDeniedReportsOnlyPermissionDenial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(name string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	plain := write("plain.db")
	readOnly := write("read-only.db")
	if err := os.Chmod(readOnly, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o600) })
	denied := write("denied.db")
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(
		"icacls", denied, "/deny", "*"+user.User.Sid.String()+":(W)",
	).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot deny writing a file: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", denied, "/reset").Run() })

	// A junction is judged by the folder it points at, where writes through it land.
	deniedFolder := filepath.Join(root, "denied-folder")
	if err := os.Mkdir(deniedFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(
		"icacls", deniedFolder, "/deny", "*"+user.User.Sid.String()+":(WD,AD)",
	).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot deny creating entries: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", deniedFolder, "/reset").Run() })
	junction := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, deniedFolder).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	// A folder that refuses only changes to its own attributes still lets the app create entries in it.
	attributesLocked := filepath.Join(root, "attributes-locked")
	if err := os.Mkdir(attributesLocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(
		"icacls", attributesLocked, "/deny", "*"+user.User.Sid.String()+":(WA)",
	).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot deny writing attributes: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", attributesLocked, "/reset").Run() })

	// Refusing traversal stops nothing for a token that bypasses the traverse check, which every account
	// has by default, so the folder is denied only when creating a file in it really fails.
	traverseLocked := filepath.Join(root, "traverse-locked")
	if err := os.Mkdir(traverseLocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(
		"icacls", traverseLocked, "/deny", "*"+user.User.Sid.String()+":(X)",
	).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot deny traversal: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", traverseLocked, "/reset").Run() })
	probeErr := os.WriteFile(filepath.Join(traverseLocked, "probe.db"), []byte("data"), 0o600)
	if probeErr != nil && !errors.Is(probeErr, os.ErrPermission) {
		t.Fatal(probeErr)
	}

	for path, want := range map[string]bool{
		plain:                             false,
		readOnly:                          false,
		filepath.Join(root, "missing.db"): false,
		root:                              false,
		attributesLocked:                  false,
		traverseLocked:                    probeErr != nil,
		denied:                            true,
		junction:                          true,
	} {
		if got := AccessDenied(path); got != want {
			t.Errorf("AccessDenied(%q) = %v, want %v", path, got, want)
		}
	}
}

// GetFinalPathNameByHandle answers with the \\?\ extended-length prefix; FinalPath has to hand back
// an ordinary path whose volume is the drive letter.
func TestFinalPathStripsExtendedLengthPrefix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if volume := filepath.VolumeName(root); len(volume) != 2 || volume[1] != ':' {
		t.Skipf("temp dir %q is not on a drive-letter volume", root)
	}

	got, err := FinalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if volume := filepath.VolumeName(got); len(volume) != 2 || volume[1] != ':' {
		t.Fatalf("FinalPath(%q) = %q, volume %q; want a plain drive letter", root, got, volume)
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
