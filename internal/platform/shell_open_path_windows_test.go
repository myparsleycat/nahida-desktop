package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestOpenShellPathPassesLongUnicodePathToWindowsShell(t *testing.T) {
	target := t.TempDir()
	for index := 0; len(windows.StringToUTF16(target))-1 <= 260; index++ {
		target = filepath.Join(target, fmt.Sprintf("segment-%02d-abcdefghij", index))
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target = filepath.Join(target, "한글 Mod (Body Shaped)")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}

	var gotHWND windows.Handle
	var gotVerb, gotFile string
	var gotArgs, gotCWD *uint16
	var gotShowCmd int32
	err := openShellPathWith(target, func(
		hwnd windows.Handle,
		verb *uint16,
		file *uint16,
		args *uint16,
		cwd *uint16,
		showCmd int32,
	) error {
		gotHWND = hwnd
		gotVerb = windows.UTF16PtrToString(verb)
		gotFile = windows.UTF16PtrToString(file)
		gotArgs = args
		gotCWD = cwd
		gotShowCmd = showCmd
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	absolute, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	if gotHWND != 0 || gotVerb != "open" || gotFile != absolute {
		t.Fatalf("ShellExecute(%d, %q, %q), want (0, open, %q)", gotHWND, gotVerb, gotFile, absolute)
	}
	if gotArgs != nil || gotCWD != nil || gotShowCmd != windows.SW_SHOWNORMAL {
		t.Fatalf("ShellExecute args = %p, cwd = %p, show = %d", gotArgs, gotCWD, gotShowCmd)
	}
}

func TestOpenShellPathRejectsMissingPathBeforeWindowsShell(t *testing.T) {
	called := false
	missing := filepath.Join(t.TempDir(), "missing")
	err := openShellPathWith(missing, func(
		windows.Handle,
		*uint16,
		*uint16,
		*uint16,
		*uint16,
		int32,
	) error {
		called = true
		return nil
	})
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path error = %v", err)
	}
	if called {
		t.Fatal("ShellExecute called for a missing path")
	}
}

func TestOpenShellPathPreservesWindowsShellError(t *testing.T) {
	sentinel := errors.New("shell execute failed")
	err := openShellPathWith(t.TempDir(), func(
		windows.Handle,
		*uint16,
		*uint16,
		*uint16,
		*uint16,
		int32,
	) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ShellExecute error = %v", err)
	}
}
