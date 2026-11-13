//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"github.com/rodrigocfd/windigo/x/cosh"
	"github.com/rodrigocfd/windigo/x/winsh"

	"nahida.live/desktop/internal/platform"
)

var persistFileIID = co.IID(co.GUID{
	Data1: 0x0000010b, Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46},
})

type shortcutPersistFile struct{ win.IUnknown }

func (*shortcutPersistFile) IID() *co.IID { return &persistFileIID }

// IPersistFile vtable slots: IUnknown takes 0 to 2, GetClassID 3, and IsDirty 4.
const (
	persistFileLoad = 5
	persistFileSave = 6
)

// call invokes IPersistFile.Load or Save, whose second argument is the open mode or the remember flag.
func (p *shortcutPersistFile) call(method int, path string, argument uintptr) error {
	filePath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// Ppvt returns the COM object address as a uintptr; reinterpret it in place so
	// govet does not see a uintptr-to-pointer conversion.
	ppvt := p.Ppvt()
	vtable := *(**[9]uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&ppvt)))
	result, _, _ := syscall.SyscallN(vtable[method], ppvt, uintptr(unsafe.Pointer(filePath)), argument)
	if result != uintptr(co.HRESULT_S_OK) {
		return co.HRESULT(result)
	}
	return nil
}

func (x *XXMI) CreateShortcut(ctx context.Context, key string) (string, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
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
	dir, err := x.shortcutIconRoot()
	if err != nil {
		return "", err
	}

	x.shortcutMu.Lock()
	defer x.shortcutMu.Unlock()
	icon, err := x.resolveShortcutIcon(ctx, dir, key)
	if err != nil {
		return "", err
	}
	path, err := desktopShortcutPath(key)
	if err != nil {
		return "", err
	}
	if err := writeShortcut(ctx, path, key, executable, icon); err != nil {
		return "", err
	}
	return path, nil
}

// resolveShortcutIcon writes the icon file the importer's shortcut uses and returns its path. A custom icon whose
// converted image is missing or damaged falls back to the importer's own icon.
func (x *XXMI) resolveShortcutIcon(ctx context.Context, dir, key string) (string, error) {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return "", fmt.Errorf("unknown importer %q", key)
	}
	id, err := x.importerIconID(ctx, spec.key)
	if err != nil {
		return "", err
	}
	if id == "" {
		return writeShortcutIcon(dir, spec.key)
	}
	data, err := readCustomImporterIcon(dir, spec.key, id)
	if err != nil {
		x.log.Warn(map[string]any{
			"importer": spec.key, "icon_id": id, "error": err.Error(),
		}, "XXMI.resolveShortcutIcon: custom icon unavailable, using the importer icon")
		return writeShortcutIcon(dir, spec.key)
	}
	path := customShortcutIconFile(dir, spec.key, id)
	if err := writeIconFile(path, data); err != nil {
		return "", fmt.Errorf("write %s shortcut icon: %w", spec.key, err)
	}
	return path, nil
}

func (x *XXMI) shortcutIconRoot() (string, error) {
	cacheRoot, err := x.cacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheRoot, shortcutIconDir), nil
}

// updateExistingShortcut sets icon on the shortcut at path and reports whether there was one to update. The target,
// arguments, and working directory stay as they are.
func updateExistingShortcut(ctx context.Context, path, icon string) (bool, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}

	err := withShellCOM(func() error {
		releaser := win.NewOleReleaser()
		defer releaser.Release()
		var link *winsh.IShellLink
		if err := win.CoCreateInstance(
			releaser, &cosh.CLSID_ShellLink, nil, co.CLSCTX_INPROC_SERVER, &link,
		); err != nil {
			return err
		}
		var persist *shortcutPersistFile
		if err := link.QueryInterface(releaser, &persist); err != nil {
			return err
		}
		if err := persist.call(persistFileLoad, path, 0); err != nil {
			return err
		}
		if err := link.SetIconLocation(icon, 0); err != nil {
			return err
		}
		return saveShortcut(persist, path)
	})
	if err != nil {
		return false, err
	}
	platform.NotifyShellItemUpdated(path)
	return true, nil
}

// saveShortcut saves the shell link to a temporary file next to path and replaces path with it, so a failed save
// leaves the previous shortcut intact.
func saveShortcut(persist *shortcutPersistFile, path string) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".nhd-shortcut-*.lnk")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	err = temporary.Close()
	if err == nil {
		// A false remember flag saves a copy and leaves the link object bound to its loaded file.
		err = persist.call(persistFileSave, temporaryPath, 0)
	}
	if err == nil {
		err = platform.ReplaceAtomic(temporaryPath, path)
	}
	if err != nil {
		_ = os.Remove(temporaryPath)
	}
	return err
}

// restoreDefaultShortcutIcons puts the desktop shortcuts of the importers in keys back on the importer's own icon
// and removes their custom icon previews. The caller must hold shortcutMu.
func (x *XXMI) restoreDefaultShortcutIcons(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	dir, err := x.shortcutIconRoot()
	if err != nil {
		return err
	}
	var errs []error
	for _, key := range keys {
		icon, err := writeShortcutIcon(dir, key)
		if err == nil {
			var path string
			if path, err = desktopShortcutPath(key); err == nil {
				_, err = updateExistingShortcut(ctx, path, icon)
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restore %s shortcut icon: %w", key, err))
		}
		errs = append(errs, pruneShortcutIcons(dir, key, ""))
	}
	return errors.Join(errs...)
}

func writeShortcut(ctx context.Context, path, key, executable, icon string) error {
	return withShellCOM(func() error {
		if err := createWindowsShortcut(ctx, path, executable, "--xxmi-launch "+key, icon); err != nil {
			return err
		}

		// Explorer keeps showing the previous icon of a rewritten shortcut until it is told the file changed.
		platform.NotifyShellItemUpdated(path)
		return nil
	})
}

// withShellCOM runs fn on a thread locked into a single-threaded COM apartment, which shell links and known
// folders require. A nested call joins the apartment of the outer one.
func withShellCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE); err != nil {
		return fmt.Errorf("initialize COM for shortcut: %w", err)
	}
	defer win.CoUninitialize()
	return fn()
}

func desktopShortcutPath(key string) (path string, err error) {
	err = withShellCOM(func() error {
		releaser := win.NewOleReleaser()
		defer releaser.Release()
		var desktop *winsh.IShellItem
		if err := winsh.SHGetKnownFolderItem(
			releaser, &cosh.FOLDERID_Desktop, cosh.KF_DEFAULT, 0, &desktop,
		); err != nil {
			return err
		}
		desktopPath, err := desktop.GetDisplayName(cosh.SIGDN_FILESYSPATH)
		if err != nil {
			return err
		}
		path = filepath.Join(desktopPath, key+" Quick Start.lnk")
		return nil
	})
	return path, err
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
	return saveShortcut(persist, path)
}
