//go:build windows

package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/db"
)

const (
	wofProviderFile                 = 2
	fileProviderCompressionXpress4K = 0
	fsctlDeleteExternalBacking      = 0x90314
	hresultCompressionNotBeneficial = 0x80070158
	moveFileReplaceExisting         = 0x1
	moveFileWriteThrough            = 0x8
	compressionLedgerPrefix         = "mod_compression:file:"
	defaultXpressWorkerCount        = 2
)

var (
	wofutilDLL                 = windows.NewLazySystemDLL("wofutil.dll")
	procWofSetFileDataLocation = wofutilDLL.NewProc("WofSetFileDataLocation")
	procWofIsExternalFile      = wofutilDLL.NewProc("WofIsExternalFile")
	kernel32CompressionDLL     = windows.NewLazySystemDLL("kernel32.dll")
	procGetDiskFreeSpaceW      = kernel32CompressionDLL.NewProc("GetDiskFreeSpaceW")
	procMoveFileExW            = kernel32CompressionDLL.NewProc("MoveFileExW")
	wofSetFileDataLocationCall = nativeWofSetFileDataLocation
	deleteExternalBackingCall  = nativeDeleteExternalBacking
	setCompressionCall         = nativeSetCompression
	wofStateCall               = nativeWofState
	fileAttributesCall         = nativeFileAttributes
	fileIdentityCall           = nativeFileIdentity
)

type wofCompressionInfoV1 struct {
	Algorithm uint32
	Flags     uint32
}

type wofLedgerEntry struct {
	FileID    string `json:"fileId"`
	Path      string `json:"path"`
	Provider  uint32 `json:"provider,omitempty"`
	Algorithm uint32 `json:"algorithm"`
	State     string `json:"state"`
}

type wofOwnership uint8

const (
	wofBackingNone wofOwnership = iota
	wofBackingOwned
	wofBackingForeign
)

var xpressSkippedExtensions = map[string]struct{}{
	".dl_": {}, ".gif": {}, ".jpg": {}, ".jpeg": {}, ".png": {}, ".wmf": {},
	".mkv": {}, ".mp4": {}, ".wmv": {}, ".avi": {}, ".bik": {}, ".bk2": {},
	".flv": {}, ".ogg": {}, ".mpg": {}, ".m2v": {}, ".m4v": {}, ".vob": {},
	".mp3": {}, ".aac": {}, ".wma": {}, ".flac": {}, ".zip": {}, ".xap": {},
	".rar": {}, ".7z": {}, ".cab": {}, ".lzx": {}, ".docx": {}, ".xlsx": {},
	".pptx": {}, ".vssx": {}, ".vstx": {}, ".onepkg": {}, ".tar": {}, ".gz": {},
	".dmg": {}, ".bz2": {}, ".tgz": {}, ".lz": {}, ".xz": {}, ".txz": {}, ".zst": {},
}

func applyXpress4K(
	ctx context.Context,
	roots []string,
	client *db.Client,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
) error {
	managed, managedErr := managedWofIDs(ctx, client)
	if managedErr != nil {
		return managedErr
	}
	files, scanErr := walkCompressionFiles(roots, func(path string, info fs.FileInfo) bool {
		if _, skipped := xpressSkippedExtensions[strings.ToLower(filepath.Ext(path))]; skipped {
			return false
		}
		cluster, err := volumeClusterSize(path)
		return err == nil && info.Size() > int64(cluster)
	})
	var errs []error
	if scanErr != nil {
		errs = append(errs, scanErr)
	}
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.size
	}
	setTotals(len(files), totalBytes)
	workers := defaultXpressWorkerCount
	if runtime.GOMAXPROCS(0) < workers {
		workers = 1
	}
	jobs := make(chan compressionFile)
	results := make(chan error, len(files))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				err := applyXpressFile(ctx, client, managed, file.path, mark)
				progress(file.path, file.size, err != nil)
				results <- err
			}
		}()
	}
	for _, file := range files {
		select {
		case jobs <- file:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			close(results)
			return errors.Join(append(errs, ctx.Err())...)
		}
	}
	close(jobs)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func applyXpressFile(ctx context.Context, client *db.Client, managed map[string]wofLedgerEntry, path string, mark compressionMutationMarker) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	external, provider, algorithm, err := wofStateCall(path)
	if err != nil {
		return err
	}
	id, err := fileIdentityCall(path)
	if err != nil {
		return err
	}
	if external {
		if entry, ok := managed[id]; ok && provider == entry.Provider && algorithm == entry.Algorithm {
			if !samePath(entry.Path, path) {
				entry.Path = path
				return saveWofLedger(ctx, client, entry)
			}
			return nil
		}
		return fmt.Errorf("unmanaged WOF backing detected: %s", path)
	}
	entry := wofLedgerEntry{
		FileID: id, Path: path, Provider: wofProviderFile,
		Algorithm: fileProviderCompressionXpress4K, State: "compressing",
	}
	if err := saveWofLedger(ctx, client, entry); err != nil {
		return err
	}
	mark(path)
	if err := wofCompress(path); err != nil {
		if errors.Is(err, errCompressionNotBeneficial) {
			return client.AppState.Delete(ctx, compressionLedgerPrefix+id)
		}
		return err
	}
	entry.State = "compressed"
	return saveWofLedger(ctx, client, entry)
}

func cleanupMissingWofLedgers(ctx context.Context, scopes []string, client *db.Client) error {
	managed, err := managedWofIDs(ctx, client)
	if err != nil {
		return err
	}
	var errs []error
	for id, entry := range managed {
		contained := false
		for _, scope := range scopes {
			if pathContains(scope, entry.Path) {
				contained = true
				break
			}
		}
		if !contained {
			continue
		}
		if _, err := os.Stat(entry.Path); errors.Is(err, os.ErrNotExist) {
			if deleteErr := client.AppState.Delete(ctx, compressionLedgerPrefix+id); deleteErr != nil {
				errs = append(errs, deleteErr)
			}
		} else if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func restoreManagedWOF(
	ctx context.Context,
	roots []string,
	client *db.Client,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
) error {
	managed, err := managedWofIDs(ctx, client)
	if err != nil {
		return err
	}
	if len(managed) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	files, scanErr := walkCompressionFiles(roots, func(path string, _ fs.FileInfo) bool { return true })
	var errs []error
	if scanErr != nil {
		errs = append(errs, scanErr)
	}
	var managedFiles int
	var totalBytes int64
	for _, file := range files {
		id, identityErr := fileIdentityCall(file.path)
		if identityErr == nil {
			if _, ok := managed[id]; ok {
				managedFiles++
				totalBytes += file.size
			}
		}
	}
	setTotals(managedFiles, totalBytes)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		id, err := fileIdentityCall(file.path)
		if err != nil {
			continue
		}
		entry, ok := managed[id]
		if !ok {
			continue
		}
		seen[id] = struct{}{}
		ownership, stateErr := inspectWofOwnership(file.path, entry)
		if stateErr == nil && ownership == wofBackingOwned {
			mark(file.path)
			stateErr = wofDecompress(file.path)
		}
		if stateErr == nil {
			stateErr = client.AppState.Delete(ctx, compressionLedgerPrefix+id)
		}
		progress(file.path, file.size, stateErr != nil)
		if stateErr != nil {
			errs = append(errs, fmt.Errorf("restore XPRESS4K %q: %w", file.path, stateErr))
		}
	}
	for id, entry := range managed {
		if _, ok := seen[id]; ok {
			continue
		}
		info, err := os.Stat(entry.Path)
		if errors.Is(err, os.ErrNotExist) {
			// The file may have been deleted while compressed. A missing file has no backing to restore.
			if deleteErr := client.AppState.Delete(ctx, compressionLedgerPrefix+id); deleteErr != nil {
				errs = append(errs, deleteErr)
			}
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("stat ledger path %q: %w", entry.Path, err))
			continue
		}
		currentID, err := fileIdentityCall(entry.Path)
		if err != nil || currentID != id {
			errs = append(errs, fmt.Errorf("WOF ledger identity changed for %q", entry.Path))
			continue
		}
		ownership, err := inspectWofOwnership(entry.Path, entry)
		if err == nil && ownership == wofBackingOwned {
			mark(entry.Path)
			err = wofDecompress(entry.Path)
		}
		if err == nil {
			err = client.AppState.Delete(ctx, compressionLedgerPrefix+id)
		}
		progress(entry.Path, info.Size(), err != nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("restore XPRESS4K ledger path %q: %w", entry.Path, err))
		}
	}
	return errors.Join(errs...)
}

func inspectWofOwnership(path string, entry wofLedgerEntry) (wofOwnership, error) {
	attributes, err := fileAttributesCall(path)
	if err != nil {
		return wofBackingNone, err
	}
	external, provider, algorithm, err := wofStateCall(path)
	if err != nil {
		return wofBackingNone, err
	}
	if attributes&windows.FILE_ATTRIBUTE_COMPRESSED != 0 {
		return wofBackingForeign, nil
	}
	if !external {
		return wofBackingNone, nil
	}
	if provider == entry.Provider && algorithm == entry.Algorithm {
		return wofBackingOwned, nil
	}
	return wofBackingForeign, nil
}

func unmanagedCompressionFiles(ctx context.Context, roots []string, client *db.Client) ([]string, error) {
	managed, err := managedWofIDs(ctx, client)
	if err != nil {
		return nil, err
	}
	var external []string
	files, scanErr := walkCompressionFiles(roots, func(path string, _ fs.FileInfo) bool { return true })
	var errs []error
	if scanErr != nil {
		errs = append(errs, scanErr)
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return external, errors.Join(append(errs, err)...)
		}
		attributes, err := fileAttributesCall(file.path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		wof, provider, algorithm, err := wofStateCall(file.path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !wof && attributes&windows.FILE_ATTRIBUTE_COMPRESSED == 0 {
			continue
		}
		id, err := fileIdentityCall(file.path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		entry, ok := managed[id]
		managedBacking := ok && wof && attributes&windows.FILE_ATTRIBUTE_COMPRESSED == 0 &&
			provider == entry.Provider && algorithm == entry.Algorithm
		if !managedBacking {
			external = append(external, file.path)
		}
	}
	return external, errors.Join(errs...)
}

func decompressExternalFiles(
	ctx context.Context,
	paths []string,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	files := make([]compressionFile, 0, len(paths))
	var errs []error
	var totalBytes int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			onError(path, err)
			errs = append(errs, fmt.Errorf("stat external compression %q: %w", path, err))
			continue
		}
		files = append(files, compressionFile{path: path, size: info.Size()})
		totalBytes += info.Size()
	}
	setTotals(len(files), totalBytes)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		err := decompressExternalFile(file.path, mark)
		progress(file.path, file.size, err != nil)
		if err != nil {
			onError(file.path, err)
			errs = append(errs, fmt.Errorf("decompress external compression %q: %w", file.path, err))
		}
	}
	return errors.Join(errs...)
}

func decompressExternalFile(path string, mark compressionMutationMarker) error {
	attributes, err := fileAttributesCall(path)
	if err != nil {
		return err
	}
	external, _, _, err := wofStateCall(path)
	if err != nil {
		return err
	}
	var errs []error
	if external {
		mark(path)
		if err := wofDecompress(path); err != nil {
			errs = append(errs, fmt.Errorf("remove WOF backing: %w", err))
		}
	}
	if attributes&windows.FILE_ATTRIBUTE_COMPRESSED != 0 {
		mark(path)
		if err := ntfsDecompress(path); err != nil {
			errs = append(errs, fmt.Errorf("remove NTFS compression: %w", err))
		}
	}
	return errors.Join(errs...)
}

func managedWofIDs(ctx context.Context, client *db.Client) (map[string]wofLedgerEntry, error) {
	rows, err := client.AppState.ListByPrefix(ctx, compressionLedgerPrefix)
	if err != nil {
		return nil, err
	}
	out := make(map[string]wofLedgerEntry, len(rows))
	for _, row := range rows {
		var entry wofLedgerEntry
		if err := json.Unmarshal([]byte(row.Value), &entry); err != nil {
			return nil, fmt.Errorf("decode WOF ledger %q: %w", row.Key, err)
		}
		if entry.Provider == 0 {
			entry.Provider = wofProviderFile
			if err := saveWofLedger(ctx, client, entry); err != nil {
				return nil, err
			}
		}
		out[entry.FileID] = entry
	}
	return out, nil
}

func saveWofLedger(ctx context.Context, client *db.Client, entry wofLedgerEntry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return client.AppState.Upsert(ctx, compressionLedgerPrefix+entry.FileID, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
}

func nativeWofState(path string) (bool, uint32, uint32, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, 0, 0, err
	}
	var external int32
	var provider uint32
	info := wofCompressionInfoV1{}
	infoSize := uint32(unsafe.Sizeof(info))
	hr, _, _ := procWofIsExternalFile.Call(
		uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&external)), uintptr(unsafe.Pointer(&provider)),
		uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&infoSize)),
	)
	if int32(hr) < 0 {
		return false, 0, 0, hresultError(hr)
	}
	return external != 0, provider, info.Algorithm, nil
}

var errCompressionNotBeneficial = errors.New("compression not beneficial")

func wofCompress(path string) error {
	handle, err := openCompressionFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	info := wofCompressionInfoV1{Algorithm: fileProviderCompressionXpress4K}
	hr := wofSetFileDataLocationCall(handle, wofProviderFile, &info)
	if uint32(hr) == hresultCompressionNotBeneficial {
		return errCompressionNotBeneficial
	}
	if int32(hr) < 0 {
		return hresultError(hr)
	}
	return nil
}

func wofDecompress(path string) error {
	handle, err := openDecompressionFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return deleteExternalBackingCall(handle)
}

func nativeWofSetFileDataLocation(handle windows.Handle, provider uint32, info *wofCompressionInfoV1) uintptr {
	hr, _, _ := procWofSetFileDataLocation.Call(
		uintptr(handle), uintptr(provider), uintptr(unsafe.Pointer(info)), unsafe.Sizeof(*info),
	)
	return hr
}

func nativeDeleteExternalBacking(handle windows.Handle) error {
	var returned uint32
	return windows.DeviceIoControl(handle, fsctlDeleteExternalBacking, nil, 0, nil, 0, &returned, nil)
}

func ntfsDecompress(path string) error {
	handle, err := openCompressionFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return setCompressionCall(handle, 0)
}

func nativeSetCompression(handle windows.Handle, format uint16) error {
	var returned uint32
	return windows.DeviceIoControl(
		handle,
		windows.FSCTL_SET_COMPRESSION,
		(*byte)(unsafe.Pointer(&format)),
		uint32(unsafe.Sizeof(format)),
		nil,
		0,
		&returned,
		nil,
	)
}

func nativeFileAttributes(path string) (uint32, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.GetFileAttributes(pathPtr)
}

func openCompressionFile(path string) (windows.Handle, error) {
	return openCompressionFileWithAccess(path, windows.GENERIC_READ|windows.GENERIC_WRITE)
}

func openDecompressionFile(path string) (windows.Handle, error) {
	return openCompressionFileWithAccess(path, windows.GENERIC_READ)
}

func openCompressionFileWithAccess(path string, access uint32) (windows.Handle, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(pathPtr, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_SEQUENTIAL_SCAN, 0)
}

func nativeFileIdentity(path string) (string, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(pathPtr, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08x-%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

func volumeClusterSize(path string) (uint32, error) {
	root := filepath.VolumeName(path) + `\`
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	var sectorsPerCluster, bytesPerSector uint32
	r1, _, callErr := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(rootPtr)), uintptr(unsafe.Pointer(&sectorsPerCluster)),
		uintptr(unsafe.Pointer(&bytesPerSector)), 0, 0,
	)
	if r1 == 0 {
		return 0, callErr
	}
	return sectorsPerCluster * bytesPerSector, nil
}

func hresultError(value uintptr) error {
	code := uint32(value)
	if code&0xffff0000 == 0x80070000 {
		return syscall.Errno(code & 0xffff)
	}
	return fmt.Errorf("HRESULT 0x%08x", code)
}

func isReparsePoint(info fs.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func replaceFile(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	r1, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(sourcePtr)), uintptr(unsafe.Pointer(destinationPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if r1 == 0 {
		return callErr
	}
	return nil
}

func hideFile(path string) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(pathPtr, attributes|windows.FILE_ATTRIBUTE_HIDDEN)
}
