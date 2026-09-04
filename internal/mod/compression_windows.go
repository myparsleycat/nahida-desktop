//go:build windows

package mod

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wofProviderFile                 = 2
	fileProviderCompressionXpress4K = 0
	fsctlDeleteExternalBacking      = 0x90314
	hresultCompressionNotBeneficial = 0x80070158
)

var (
	wofutilDLL                 = windows.NewLazySystemDLL("wofutil.dll")
	procWofSetFileDataLocation = wofutilDLL.NewProc("WofSetFileDataLocation")
	procWofIsExternalFile      = wofutilDLL.NewProc("WofIsExternalFile")
	kernel32CompressionDLL     = windows.NewLazySystemDLL("kernel32.dll")
	procGetDiskFreeSpaceW      = kernel32CompressionDLL.NewProc("GetDiskFreeSpaceW")
	wofSetFileDataLocationCall = nativeWofSetFileDataLocation
	deleteExternalBackingCall  = nativeDeleteExternalBacking
	wofStateCall               = nativeWofState
	volumeClusterSizeCall      = volumeClusterSize
)

type wofCompressionInfoV1 struct {
	Algorithm uint32
	Flags     uint32
}

var xpressSkippedExtensions = map[string]struct{}{
	".dl_": {}, ".gif": {}, ".jpg": {}, ".jpeg": {}, ".png": {}, ".wmf": {},
	".mkv": {}, ".mp4": {}, ".wmv": {}, ".avi": {}, ".bik": {}, ".bk2": {},
	".flv": {}, ".ogg": {}, ".mpg": {}, ".m2v": {}, ".m4v": {}, ".vob": {},
	".mp3": {}, ".aac": {}, ".wma": {}, ".flac": {}, ".zip": {}, ".xap": {},
	".rar": {}, ".7z": {}, ".cab": {}, ".lzx": {}, ".docx": {}, ".xlsx": {},
	".pptx": {}, ".vssx": {}, ".vstx": {}, ".onepkg": {}, ".tar": {}, ".gz": {},
	".dmg": {}, ".bz2": {}, ".tgz": {}, ".lz": {}, ".xz": {}, ".txz": {}, ".zst": {}, ".nzst": {},
}

func applyXpress4K(
	ctx context.Context,
	roots []string,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	files, err := xpressCompressionFiles(roots)
	if err != nil {
		return err
	}
	setCompressionTotals(files, setTotals)
	return runXpressWorkers(ctx, files, progress, onError, func(file compressionFile) error {
		handle, err := openXpressFile(file.path)
		if err != nil {
			return fmt.Errorf("open: %w", err)
		}
		defer func() { _ = windows.CloseHandle(handle) }()
		mark(file.path)
		return wofCompressHandle(handle)
	})
}

func restoreWOF(
	ctx context.Context,
	roots []string,
	setTotals func(int, int64),
	progress func(string, int64, bool),
	mark compressionMutationMarker,
	onError func(string, error),
) error {
	files, err := walkCompressionFiles(roots, func(string, fs.FileInfo) bool { return true })
	if err != nil {
		return err
	}
	setCompressionTotals(files, setTotals)
	return runXpressWorkers(ctx, files, progress, onError, func(file compressionFile) error {
		external, provider, _, err := wofStateCall(file.path)
		if err != nil {
			return fmt.Errorf("inspect WOF state: %w", err)
		}
		if !external || provider != wofProviderFile {
			return nil
		}
		handle, err := openXpressFile(file.path)
		if err != nil {
			return fmt.Errorf("open: %w", err)
		}
		defer func() { _ = windows.CloseHandle(handle) }()
		mark(file.path)
		if err := deleteExternalBackingCall(handle); err != nil {
			return fmt.Errorf("remove WOF backing: %w", err)
		}
		return nil
	})
}

func xpressCompressionFiles(roots []string) ([]compressionFile, error) {
	files, scanErr := walkCompressionFiles(roots, func(path string, _ fs.FileInfo) bool {
		_, skipped := xpressSkippedExtensions[strings.ToLower(filepath.Ext(path))]
		return !skipped
	})
	clusters := make(map[string]uint32)
	badVolumes := make(map[string]struct{})
	var errs []error
	if scanErr != nil {
		errs = append(errs, scanErr)
	}
	for _, file := range files {
		volume := strings.ToLower(filepath.VolumeName(file.path))
		if _, ok := clusters[volume]; ok {
			continue
		}
		if _, ok := badVolumes[volume]; ok {
			continue
		}
		cluster, err := volumeClusterSizeCall(file.path)
		if err != nil {
			badVolumes[volume] = struct{}{}
			errs = append(errs, fmt.Errorf("cluster size for volume %q: %w", filepath.VolumeName(file.path), err))
			continue
		}
		clusters[volume] = cluster
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	result := make([]compressionFile, 0, len(files))
	for _, file := range files {
		if file.size > int64(clusters[strings.ToLower(filepath.VolumeName(file.path))]) {
			result = append(result, file)
		}
	}
	return result, nil
}

func setCompressionTotals(files []compressionFile, setTotals func(int, int64)) {
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.size
	}
	setTotals(len(files), totalBytes)
}

func runXpressWorkers(
	ctx context.Context,
	files []compressionFile,
	progress func(string, int64, bool),
	onError func(string, error),
	process func(compressionFile) error,
) error {
	if len(files) == 0 {
		return ctx.Err()
	}
	jobs := make(chan compressionFile)
	var wg sync.WaitGroup
	for range min(xpressWorkerCount(runtime.GOMAXPROCS(0)), len(files)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if err := process(file); err != nil && onError != nil {
					onError(file.path, err)
				}
				progress(file.path, file.size, false)
			}
		}()
	}
	for _, file := range files {
		select {
		case jobs <- file:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return ctx.Err()
}

func xpressWorkerCount(gomaxprocs int) int {
	return max(1, gomaxprocs)
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
	handle, err := openXpressFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return wofCompressHandle(handle)
}

func wofCompressHandle(handle windows.Handle) error {
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
	handle, err := openXpressFile(path)
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

func openXpressFile(path string) (windows.Handle, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
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
