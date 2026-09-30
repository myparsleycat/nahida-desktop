//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
)

func TestCreateWindowsShortcut(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("original path", func(t *testing.T) {
		testCreateWindowsShortcut(t, executable)
	})
	t.Run("uppercase path", func(t *testing.T) {
		testCreateWindowsShortcut(t, strings.ToUpper(executable))
	})
	t.Run("short path", func(t *testing.T) {
		longPath, err := windows.UTF16PtrFromString(executable)
		if err != nil {
			t.Fatal(err)
		}
		buffer := make([]uint16, len(executable)+1)
		length, err := windows.GetShortPathName(longPath, &buffer[0], uint32(len(buffer)))
		if err != nil || length >= uint32(len(buffer)) {
			t.Fatalf("short executable path length = %d, error = %v", length, err)
		}
		shortPath := windows.UTF16ToString(buffer)
		if strings.EqualFold(shortPath, executable) {
			t.Skip("executable has no distinct 8.3 path on this filesystem")
		}
		testCreateWindowsShortcut(t, shortPath)
	})
}

func testCreateWindowsShortcut(t *testing.T, executable string) {
	t.Helper()
	executableInfo, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE); err != nil {
		t.Fatal(err)
	}
	defer win.CoUninitialize()
	path := filepath.Join(t.TempDir(), "GIMI Quick Start.lnk")
	if err := createWindowsShortcut(context.Background(), path, executable, "--xxmi-launch GIMI"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("shortcut = %v, %v", info, err)
	}
	releaser := win.NewOleReleaser()
	defer releaser.Release()
	var link *win.IShellLink
	if err := win.CoCreateInstance(releaser, &co.CLSID_ShellLink, nil, co.CLSCTX_INPROC_SERVER, &link); err != nil {
		t.Fatal(err)
	}
	var persist *shortcutPersistFile
	if err := link.QueryInterface(releaser, &persist); err != nil {
		t.Fatal(err)
	}
	filePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	vtable := *(**[9]uintptr)(unsafe.Pointer(persist.Ppvt()))
	result, _, _ := syscall.SyscallN(vtable[5], uintptr(unsafe.Pointer(persist.Ppvt())),
		uintptr(unsafe.Pointer(filePath)), 0)
	if result != uintptr(co.HRESULT_S_OK) {
		t.Fatal(co.HRESULT(result))
	}
	target, err := link.GetPath(nil, co.SLGP_RAWPATH)
	if err != nil || !filepath.IsAbs(target) {
		t.Fatalf("shortcut target = %q, want executable = %q, error = %v", target, executable, err)
	}

	// Shell links can expand 8.3 names and normalize casing; compare file identity.
	targetInfo, err := os.Stat(target)
	if err != nil || !os.SameFile(targetInfo, executableInfo) {
		t.Fatalf("shortcut target = %q, want executable = %q, error = %v", target, executable, err)
	}
	args, err := link.GetArguments()
	if err != nil || args != "--xxmi-launch GIMI" {
		t.Fatalf("shortcut arguments = %q, error = %v", args, err)
	}
	icon, index, err := link.GetIconLocation()
	if err != nil || index != 0 {
		t.Fatalf("shortcut icon = %q, want executable = %q, index = %d, error = %v", icon, executable, index, err)
	}
	iconInfo, err := os.Stat(icon)
	if err != nil || !os.SameFile(iconInfo, executableInfo) {
		t.Fatalf("shortcut icon = %q, want executable = %q, index = %d, error = %v", icon, executable, index, err)
	}
}

func TestRemoveSavedShortcutOnlyDeletesExpectedPath(t *testing.T) {
	root := t.TempDir()
	expected := filepath.Join(root, "GIMI Quick Start.lnk")
	other := filepath.Join(root, "other.lnk")
	if err := os.WriteFile(expected, []byte("shortcut"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeSavedShortcut(other, expected); err == nil {
		t.Fatal("unrelated shortcut was accepted")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("unrelated shortcut was removed:", err)
	}
	if err := removeSavedShortcut(expected, expected); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expected); !os.IsNotExist(err) {
		t.Fatalf("expected shortcut remains: %v", err)
	}
	if err := removeSavedShortcut(expected, expected); err != nil {
		t.Fatal("repeated deletion should succeed:", err)
	}
}
