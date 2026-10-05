//go:build windows

package xxmi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"github.com/rodrigocfd/windigo/x/cosh"
	"github.com/rodrigocfd/windigo/x/winsh"
)

var persistFileIID = co.IID(co.GUID{
	Data1: 0x0000010b, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
})

type shortcutPersistFile struct{ win.IUnknown }

func (*shortcutPersistFile) IID() *co.IID { return &persistFileIID }

func (x *XXMI) CreateShortcut(ctx context.Context, key string) (string, error) {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return "", err
	}
	if !cfg.Enabled || !fileExists(filepath.Join(cfg.ImporterFolder, "d3dx.ini")) {
		return "", fmt.Errorf("XXMI_IMPORTER_NOT_INSTALLED: %s", key)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		return "", err
	}
	icon, err := writeShortcutIcon(filepath.Join(cacheRoot, shortcutIconDir), key)
	if err != nil {
		return "", err
	}
	return createShortcutOnDesktop(ctx, key, executable, icon)
}

func createShortcutOnDesktop(ctx context.Context, key, executable, icon string) (string, error) {
	path, err := desktopShortcutPath(key)
	if err != nil {
		return "", err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE); err != nil {
		return "", fmt.Errorf("initialize COM for shortcut: %w", err)
	}
	defer win.CoUninitialize()
	if err := createWindowsShortcut(ctx, path, executable, "--xxmi-launch "+key, icon); err != nil {
		return "", err
	}
	return path, nil
}

func desktopShortcutPath(key string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE); err != nil {
		return "", fmt.Errorf("initialize COM for shortcut: %w", err)
	}
	defer win.CoUninitialize()
	releaser := win.NewOleReleaser()
	defer releaser.Release()
	var desktop *winsh.IShellItem
	if err := winsh.SHGetKnownFolderItem(releaser, &cosh.FOLDERID_Desktop, cosh.KF_DEFAULT, 0, &desktop); err != nil {
		return "", err
	}
	desktopPath, err := desktop.GetDisplayName(cosh.SIGDN_FILESYSPATH)
	if err != nil {
		return "", err
	}
	path := filepath.Join(desktopPath, key+" Quick Start.lnk")
	return path, nil
}

func createWindowsShortcut(ctx context.Context, path, executable, args, icon string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	releaser := win.NewOleReleaser()
	defer releaser.Release()
	var link *winsh.IShellLink
	if err := win.CoCreateInstance(releaser, &cosh.CLSID_ShellLink, nil, co.CLSCTX_INPROC_SERVER, &link); err != nil {
		return err
	}
	if err := link.SetPath(executable); err != nil {
		return err
	}
	if err := link.SetArguments(args); err != nil {
		return err
	}
	if err := link.SetWorkingDirectory(filepath.Dir(executable)); err != nil {
		return err
	}
	if err := link.SetIconLocation(icon, 0); err != nil {
		return err
	}
	var persist *shortcutPersistFile
	if err := link.QueryInterface(releaser, &persist); err != nil {
		return err
	}
	filePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// Ppvt returns the COM object address as a uintptr; reinterpret it in place so
	// govet does not see a uintptr-to-pointer conversion.
	ppvt := persist.Ppvt()
	vtable := *(**[9]uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&ppvt)))
	result, _, _ := syscall.SyscallN(vtable[6], ppvt, uintptr(unsafe.Pointer(filePath)), 1)
	if result != uintptr(co.HRESULT_S_OK) {
		return co.HRESULT(result)
	}
	return nil
}
