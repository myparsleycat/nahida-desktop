//go:build windows

package xxmi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
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
	path, err := createShortcutOnDesktop(ctx, key, executable)
	if err != nil {
		return "", err
	}
	cfg.ShortcutPath = path
	if err := x.SaveImporterConfig(ctx, key, cfg); err != nil {
		return "", err
	}
	return path, nil
}

func (x *XXMI) DeleteShortcut(ctx context.Context, key string) error {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return err
	}
	if cfg.ShortcutPath == "" {
		return nil
	}
	expected, err := desktopShortcutPath(key)
	if err != nil {
		return err
	}
	if err := removeSavedShortcut(cfg.ShortcutPath, expected); err != nil {
		return err
	}
	cfg.ShortcutPath = ""
	return x.SaveImporterConfig(ctx, key, cfg)
}

func createShortcutOnDesktop(ctx context.Context, key, executable string) (string, error) {
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
	if err := createWindowsShortcut(ctx, path, executable, "--xxmi-launch "+key); err != nil {
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
	var desktop *win.IShellItem
	if err := win.SHGetKnownFolderItem(releaser, &co.FOLDERID_Desktop, co.KF_DEFAULT, 0, &desktop); err != nil {
		return "", err
	}
	desktopPath, err := desktop.GetDisplayName(co.SIGDN_FILESYSPATH)
	if err != nil {
		return "", err
	}
	path := filepath.Join(desktopPath, key+" Quick Start.lnk")
	return path, nil
}

func removeSavedShortcut(saved, expected string) error {
	if !strings.EqualFold(filepath.Clean(saved), filepath.Clean(expected)) {
		return fmt.Errorf("quick start shortcut path is outside the current desktop: %s", saved)
	}
	if err := os.Remove(saved); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func createWindowsShortcut(ctx context.Context, path, executable, args string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	releaser := win.NewOleReleaser()
	defer releaser.Release()
	var link *win.IShellLink
	if err := win.CoCreateInstance(releaser, &co.CLSID_ShellLink, nil, co.CLSCTX_INPROC_SERVER, &link); err != nil {
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
	if err := link.SetIconLocation(executable, 0); err != nil {
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
	vtable := *(**[9]uintptr)(unsafe.Pointer(persist.Ppvt()))
	result, _, _ := syscall.SyscallN(vtable[6], uintptr(unsafe.Pointer(persist.Ppvt())),
		uintptr(unsafe.Pointer(filePath)), 1)
	if result != uintptr(co.HRESULT_S_OK) {
		return co.HRESULT(result)
	}
	return nil
}
