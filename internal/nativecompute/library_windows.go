package nativecompute

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const abiVersion = 1

var errImageMismatch = errors.New("native compute DLL differs from the bundled image")

type Library struct {
	store  *appdata.Store
	log    *infra.Log
	once   sync.Once
	handle windows.Handle
}

func New(store *appdata.Store, log *infra.Log) *Library {
	return &Library{store: store, log: log}
}

// Lookup returns zero when the bundle is unavailable. A loaded DLL stays mapped for the process lifetime.
func (l *Library) Lookup(symbol string) (uintptr, error) {
	if l == nil || l.store == nil || len(bundledImage) == 0 {
		return 0, nil
	}
	l.once.Do(func() {
		handle, err := loadBundle(l.store, bundledImage)
		if err != nil && l.log != nil {
			_ = infra.ReportError(l.log, err, "NativeCompute", infra.Diagnostic{
				Operation: "load-native-kernels", Severity: infra.DiagnosticWarn,
				Fields: map[string]any{"fallback": "Go kernels until restart"},
			})
		}
		l.handle = handle
	})
	if l.handle == 0 {
		return 0, nil
	}
	address, err := windows.GetProcAddress(l.handle, symbol)
	if err != nil {
		return 0, fmt.Errorf("resolve native compute symbol %q: %w", symbol, err)
	}
	return address, nil
}

func loadBundle(store *appdata.Store, image []byte) (_ windows.Handle, err error) {
	stage, cached := "resolve", ""
	defer func() {
		err = infra.AnnotateError(err, infra.Diagnostic{Stage: stage, Fields: map[string]any{"path": cached}})
	}()

	name := fmt.Sprintf("desktop-kernels-%x.dll", sha256.Sum256(image))
	relative := filepath.Join(appdata.NativeKernelsDir, name)
	cached, err = store.Resolve(relative)
	if err != nil {
		return 0, err
	}

	stage = "verify"
	file, err := openVerifiedImage(cached, image)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, errImageMismatch) {
		stage = "write"
		if writeErr := store.WriteFile(relative, image, 0o600); writeErr != nil {
			return 0, writeErr
		}
		stage = "verify"
		file, err = openVerifiedImage(cached, image)
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()
	removeStaleImages(cached)

	// Resolve the name from the verified handle, which denies writes and deletes until LoadLibraryEx returns.
	stage = "final-path"
	path, err := platform.FinalPathOfHandle(windows.Handle(file.Fd()))
	if err != nil {
		return 0, fmt.Errorf("resolve final path of native compute DLL %q: %w", cached, err)
	}
	stage = "load"
	handle, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return 0, fmt.Errorf("load native compute DLL %q: %w", path, err)
	}
	stage = "abi"
	if err := verifyABI(handle); err != nil {
		return 0, errors.Join(err, windows.FreeLibrary(handle))
	}
	return handle, nil
}

// removeStaleImages deletes the DLLs that earlier bundles cached next to current. A DLL another process still
// has loaded cannot be deleted and stays until a later start.
func removeStaleImages(current string) {
	dir := filepath.Dir(current)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if matched, _ := filepath.Match("desktop-kernels-*.dll", entry.Name()); matched &&
			!platform.SamePathFold(path, current) {
			_ = os.Remove(path)
		}
	}
}

func verifyABI(handle windows.Handle) error {
	getVersion, err := windows.GetProcAddress(handle, "desktop_kernels_abi_version")
	if err != nil {
		return fmt.Errorf("resolve native compute ABI version: %w", err)
	}
	version, _, _ := syscall.SyscallN(getVersion)
	if uint32(version) != abiVersion {
		return fmt.Errorf("unsupported native compute ABI: got %d, want %d", uint32(version), abiVersion)
	}
	return nil
}

func openVerifiedImage(path string, image []byte) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	var info windows.ByHandleFileInformation
	err = windows.GetFileInformationByHandle(handle, &info)
	if err == nil && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		err = errImageMismatch
	}
	if err == nil {
		var data []byte
		data, err = io.ReadAll(io.LimitReader(file, int64(len(image))+1))
		if err == nil && !bytes.Equal(data, image) {
			err = errImageMismatch
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("verify native compute DLL %q: %w", path, err)
	}
	return file, nil
}
