//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
)

func TestCreateWindowsShortcut(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE); err != nil {
		t.Fatal(err)
	}
	defer win.CoUninitialize()
	path := filepath.Join(t.TempDir(), "GIMI Quick Start.lnk")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := createWindowsShortcut(context.Background(), path, executable, "--xxmi-launch GIMI"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("shortcut = %v, %v", info, err)
	}
}
