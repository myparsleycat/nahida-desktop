//go:build windows

package xxmi

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"github.com/rodrigocfd/windigo/x/cosh"
	"github.com/rodrigocfd/windigo/x/winsh"
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
	iconPath, err := writeShortcutIcon(t.TempDir(), "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	wantIconInfo, err := os.Stat(iconPath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "GIMI Quick Start.lnk")
	err = createWindowsShortcut(context.Background(), path, executable, "--xxmi-launch GIMI", iconPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("shortcut = %v, %v", info, err)
	}
	releaser := win.NewOleReleaser()
	defer releaser.Release()
	var link *winsh.IShellLink
	if err := win.CoCreateInstance(releaser, &cosh.CLSID_ShellLink, nil, co.CLSCTX_INPROC_SERVER, &link); err != nil {
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
	ppvt := persist.Ppvt()
	vtable := *(**[9]uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&ppvt)))
	result, _, _ := syscall.SyscallN(vtable[5], ppvt, uintptr(unsafe.Pointer(filePath)), 0)
	if result != uintptr(co.HRESULT_S_OK) {
		t.Fatal(co.HRESULT(result))
	}
	target, err := link.GetPath(nil, cosh.SLGP_RAWPATH)
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
		t.Fatalf("shortcut icon = %q, want = %q, index = %d, error = %v", icon, iconPath, index, err)
	}
	iconInfo, err := os.Stat(icon)
	if err != nil || !os.SameFile(iconInfo, wantIconInfo) {
		t.Fatalf("shortcut icon = %q, want = %q, index = %d, error = %v", icon, iconPath, index, err)
	}
}

func TestWriteShortcutIconForEveryImporter(t *testing.T) {
	const (
		imageIcon      = 1
		loadFromFile   = 0x10
		largeIconPixel = 256
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	loadImage, destroyIcon := user32.NewProc("LoadImageW"), user32.NewProc("DestroyIcon")

	dir := t.TempDir()
	for key := range importerPackages {
		image, err := shortcutIcons.Open("shortcut_icons/" + key + ".png")
		if err != nil {
			t.Fatalf("%s has no shortcut icon: %v", key, err)
		}
		config, err := png.DecodeConfig(image)
		_ = image.Close()
		if err != nil || config.Width != largeIconPixel || config.Height != largeIconPixel {
			t.Fatalf("%s shortcut icon = %dx%d, want 256x256, error = %v", key, config.Width, config.Height, err)
		}

		path, err := writeShortcutIcon(dir, strings.ToLower(key))
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, key+".ico"); path != want {
			t.Fatalf("%s shortcut icon path = %q, want %q", key, path, want)
		}
		pathPtr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		handle, _, callErr := loadImage.Call(
			0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, largeIconPixel, largeIconPixel, loadFromFile,
		)
		if handle == 0 {
			t.Fatalf("Windows cannot load %s shortcut icon %q: %v", key, path, callErr)
		}
		_, _, _ = destroyIcon.Call(handle)

		// Creating the same shortcut again must leave the stored icon in place.
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writeShortcutIcon(dir, key); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("%s shortcut icon was rewritten: %v", key, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(importerPackages) {
		t.Fatalf(
			"shortcut icon directory holds %d entries, want %d, error = %v",
			len(entries),
			len(importerPackages),
			err,
		)
	}
	if _, err := writeShortcutIcon(dir, "unknown"); err == nil {
		t.Fatal("unknown importer produced a shortcut icon")
	}
}
