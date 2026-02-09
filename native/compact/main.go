package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wofProviderFile            = 2
	algorithmXpress8k          = 1
	fsctlDeleteExternalBacking = 0x90314

	notifyFilter = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
		windows.FILE_NOTIFY_CHANGE_DIR_NAME |
		windows.FILE_NOTIFY_CHANGE_ATTRIBUTES |
		windows.FILE_NOTIFY_CHANGE_SIZE |
		windows.FILE_NOTIFY_CHANGE_LAST_WRITE |
		windows.FILE_NOTIFY_CHANGE_CREATION
)

var globalWofInfo = wofCompressionInfo{
	Algorithm: algorithmXpress8k,
	Flags:     0,
}

type Command struct {
	Type  string   `json:"type"`
	Paths []string `json:"paths"`
}

type ProgressPayload struct {
	Message        string `json:"message"`
	ProcessedFiles uint64 `json:"processedFiles"`
	SkippedFiles   uint64 `json:"skippedFiles"`
	ErrorFiles     uint64 `json:"errorFiles"`
	TotalFiles     uint64 `json:"totalFiles"`
}

type wofCompressionInfo struct {
	Algorithm uint32
	Flags     uint32
}

type FileTask struct {
	PathStr string
	PathPtr *uint16
}

type IPC struct {
	mu sync.Mutex
}

func NewIPC() *IPC {
	return &IPC{}
}

func (ipc *IPC) Emit(msgType string, payload interface{}) {
	type message struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}
	msg := message{Type: msgType, Payload: payload}
	bytes, err := json.Marshal(msg)
	if err == nil {
		ipc.mu.Lock()
		fmt.Println(string(bytes))
		ipc.mu.Unlock()
	}
}

func (ipc *IPC) Log(text string) {
	ipc.Emit("log", text)
}

func (ipc *IPC) Progress(msg string, stats *Stats, total uint64) {
	processed := atomic.LoadUint64(&stats.Processed)
	skipped := atomic.LoadUint64(&stats.Skipped)
	errors := atomic.LoadUint64(&stats.Errors)

	currentTotal := total
	if currentTotal < processed+skipped+errors {
		currentTotal = processed + skipped + errors
	}

	ipc.Emit("progress", ProgressPayload{
		Message:        msg,
		ProcessedFiles: processed,
		SkippedFiles:   skipped,
		ErrorFiles:     errors,
		TotalFiles:     currentTotal,
	})
}

type WofService struct {
	dll            *syscall.LazyDLL
	procSetLoc     *syscall.LazyProc
	procIsExternal *syscall.LazyProc
}

func NewWofService() *WofService {
	mod := syscall.NewLazyDLL("wofutil.dll")
	return &WofService{
		dll:            mod,
		procSetLoc:     mod.NewProc("WofSetFileDataLocation"),
		procIsExternal: mod.NewProc("WofIsExternalFile"),
	}
}

func (w *WofService) IsCompressedDirect(pathPtr *uint16) bool {
	var isExternal int32
	var provider uint32
	var info wofCompressionInfo
	var bufLen uint32 = uint32(unsafe.Sizeof(info))

	ret, _, _ := w.procIsExternal.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&isExternal)),
		uintptr(unsafe.Pointer(&provider)),
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Pointer(&bufLen)),
	)
	return ret == 0 && isExternal != 0
}

func (w *WofService) CompressHandle(handle windows.Handle) error {
	ret, _, _ := w.procSetLoc.Call(
		uintptr(handle),
		uintptr(wofProviderFile),
		uintptr(unsafe.Pointer(&globalWofInfo)),
		uintptr(unsafe.Sizeof(globalWofInfo)),
	)
	if ret != 0 {
		return fmt.Errorf("wof error code: %d", ret)
	}
	return nil
}

func (w *WofService) DecompressDirect(pathPtr *uint16) error {
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	var bytesReturned uint32
	err = windows.DeviceIoControl(
		handle,
		fsctlDeleteExternalBacking,
		nil, 0, nil, 0,
		&bytesReturned, nil,
	)
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) && (errno == 313 || errno == syscall.ERROR_HANDLE_EOF) {
			return nil
		}
		if strings.Contains(err.Error(), "externally backed") {
			return nil
		}
		return err
	}
	return nil
}

type Stats struct {
	Processed uint64
	Skipped   uint64
	Errors    uint64
}

type App struct {
	ipc         *IPC
	wof         *WofService
	stats       Stats
	allowedExts map[string]bool

	jobMu         sync.Mutex
	currentCancel context.CancelFunc
	stopChannels  []chan struct{}
	watcherQueue  chan string
}

func NewApp() *App {
	return &App{
		ipc: NewIPC(),
		wof: NewWofService(),
		allowedExts: map[string]bool{
			".ib": true, ".vb": true, ".buf": true, ".dds": true,
		},
	}
}

func (app *App) ResetStats() {
	atomic.StoreUint64(&app.stats.Processed, 0)
	atomic.StoreUint64(&app.stats.Skipped, 0)
	atomic.StoreUint64(&app.stats.Errors, 0)
}

func (app *App) IsAllowed(path string) bool {
	return app.allowedExts[strings.ToLower(filepath.Ext(path))]
}

func (app *App) Run() {
	scanner := bufio.NewScanner(os.Stdin)
	app.ipc.Log("Compact process ready (Native Optimized)")

	for scanner.Scan() {
		var cmd Command
		if err := json.Unmarshal([]byte(scanner.Text()), &cmd); err != nil {
			app.ipc.Log("Invalid JSON")
			continue
		}

		app.CancelCurrentJob()
		ctx, cancel := context.WithCancel(context.Background())
		app.jobMu.Lock()
		app.currentCancel = cancel
		app.jobMu.Unlock()

		switch cmd.Type {
		case "compress":
			app.StopWatchers()
			go app.runPipeline(ctx, cmd.Paths, "compress")
		case "decompress":
			app.StopWatchers()
			go app.runPipeline(ctx, cmd.Paths, "decompress")
		case "stop":
			app.StopWatchers()
			app.ipc.Log("Stopped")
		default:
			app.ipc.Log("Unknown command")
		}
	}
}

func (app *App) CancelCurrentJob() {
	app.jobMu.Lock()
	if app.currentCancel != nil {
		app.currentCancel()
	}
	app.jobMu.Unlock()
}

func (app *App) StopWatchers() {
	app.jobMu.Lock()
	defer app.jobMu.Unlock()
	for _, ch := range app.stopChannels {
		close(ch)
	}
	app.stopChannels = nil
	if app.watcherQueue != nil {
		close(app.watcherQueue)
		app.watcherQueue = nil
	}
}

func getWorkersCount() int {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	return workers
}

func (app *App) runPipeline(ctx context.Context, paths []string, mode string) {
	workers := getWorkersCount()
	app.ipc.Log(fmt.Sprintf("[Mode: %s] Pipeline Workers: %d (Native Ptr)", mode, workers))
	app.ResetStats()

	chanScan := make(chan FileTask, 1000)
	chanTask := make(chan FileTask, 1000)

	var wgFilter sync.WaitGroup
	var wgWorker sync.WaitGroup

	for i := 0; i < workers; i++ {
		wgWorker.Add(1)
		go func() {
			defer wgWorker.Done()
			for task := range chanTask {
				if ctx.Err() != nil {
					return
				}
				if mode == "compress" {
					app.compressFileNative(task)
				} else {
					app.decompressFileNative(task)
				}
			}
		}()
	}

	for i := 0; i < workers; i++ {
		wgFilter.Add(1)
		go func() {
			defer wgFilter.Done()
			for task := range chanScan {
				if ctx.Err() != nil {
					return
				}

				attrs, err := windows.GetFileAttributes(task.PathPtr)
				if err != nil {
					continue
				}

				isReparse := (attrs & windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0

				if mode == "compress" {
					if !isReparse {
						chanTask <- task
					} else {
						atomic.AddUint64(&app.stats.Skipped, 1)
					}
				} else {
					if !isReparse {
						atomic.AddUint64(&app.stats.Skipped, 1)
						continue
					}
					if app.wof.IsCompressedDirect(task.PathPtr) {
						chanTask <- task
					} else {
						atomic.AddUint64(&app.stats.Skipped, 1)
					}
				}
			}
		}()
	}

	doneStats := make(chan struct{})
	go func() {
		ticker := time.NewTicker(1000 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-doneStats:
				return
			case <-ticker.C:
				app.ipc.Progress(strings.Title(mode)+"ing...", &app.stats, 0)
			}
		}
	}()

	go func() {
		defer close(chanScan)
		for _, root := range paths {
			if ctx.Err() != nil {
				return
			}
			filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if ctx.Err() != nil {
					return filepath.SkipAll
				}
				if err != nil || d.IsDir() {
					return nil
				}

				if !app.IsAllowed(p) {
					return nil
				}
				info, err := d.Info()
				if err != nil || info.Size() < 4096 {
					return nil
				}

				ptr, err := windows.UTF16PtrFromString(p)
				if err == nil {
					chanScan <- FileTask{PathStr: p, PathPtr: ptr}
				}
				return nil
			})
		}
	}()

	wgFilter.Wait()
	close(chanTask)
	wgWorker.Wait()

	close(doneStats)
	app.ipc.Progress("Done", &app.stats, 0)

	if mode == "compress" && ctx.Err() == nil {
		app.startWatcher(ctx, paths, workers)
	}
}

func (app *App) compressFileNative(task FileTask) {
	handle, err := windows.CreateFile(
		task.PathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		atomic.AddUint64(&app.stats.Errors, 1)
		return
	}
	defer windows.CloseHandle(handle)

	if err := app.wof.CompressHandle(handle); err == nil {
		atomic.AddUint64(&app.stats.Processed, 1)
	} else {
		atomic.AddUint64(&app.stats.Errors, 1)
	}
}

func (app *App) decompressFileNative(task FileTask) {
	if err := app.wof.DecompressDirect(task.PathPtr); err != nil {
		atomic.AddUint64(&app.stats.Errors, 1)
	} else {
		atomic.AddUint64(&app.stats.Processed, 1)
	}
}

func (app *App) startWatcher(ctx context.Context, paths []string, workers int) {
	app.jobMu.Lock()
	app.watcherQueue = make(chan string, 1000)
	app.jobMu.Unlock()

	for i := 0; i < workers; i++ {
		go func() {
			for path := range app.watcherQueue {
				if ctx.Err() != nil {
					return
				}
				app.compressWithRetry(ctx, path)
			}
		}()
	}

	app.jobMu.Lock()
	for _, path := range paths {
		absPath, _ := filepath.Abs(path)
		stopCh := make(chan struct{})
		app.stopChannels = append(app.stopChannels, stopCh)

		go func(p string, sc chan struct{}) {
			app.ipc.Log("Watching: " + p)
			app.watchDir(p, sc)
		}(absPath, stopCh)
	}
	app.jobMu.Unlock()
}

func (app *App) watchDir(pathStr string, stopCh chan struct{}) {
	pathPtr, _ := windows.UTF16PtrFromString(pathStr)
	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		app.ipc.Log("Watch fail: " + pathStr)
		return
	}
	defer windows.CloseHandle(handle)

	overlapped := &windows.Overlapped{}
	hEvent, _ := windows.CreateEvent(nil, 0, 0, nil)
	defer windows.CloseHandle(hEvent)
	overlapped.HEvent = hEvent

	buf := make([]byte, 65536)

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		var n uint32
		err := windows.ReadDirectoryChanges(handle, &buf[0], uint32(len(buf)), true, notifyFilter, &n, overlapped, 0)
		if err != nil && err != syscall.ERROR_IO_PENDING {
			return
		}

		if w, _ := windows.WaitForMultipleObjects([]windows.Handle{hEvent}, false, 500); w == windows.WAIT_OBJECT_0 {
			windows.GetOverlappedResult(handle, overlapped, &n, false)
			app.processWatchBuffer(buf[:n], pathStr)
		}
	}
}

func (app *App) processWatchBuffer(buf []byte, root string) {
	if len(buf) == 0 {
		return
	}
	var offset uint32
	for {
		entry := (*windows.FileNotifyInformation)(unsafe.Pointer(&buf[offset]))
		name := windows.UTF16ToString((*[1 << 30]uint16)(unsafe.Pointer(&entry.FileName))[:entry.FileNameLength/2])
		fullPath := filepath.Join(root, name)

		isChange := entry.Action == windows.FILE_ACTION_ADDED ||
			entry.Action == windows.FILE_ACTION_MODIFIED ||
			entry.Action == windows.FILE_ACTION_RENAMED_NEW_NAME

		if isChange && app.IsAllowed(fullPath) {
			select {
			case app.watcherQueue <- fullPath:
			default:
			}
		}

		if entry.NextEntryOffset == 0 {
			break
		}
		offset += entry.NextEntryOffset
	}
}

func (app *App) compressWithRetry(ctx context.Context, path string) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}

	for i := 0; i < 5; i++ {
		if ctx.Err() != nil {
			return
		}

		attrs, err := windows.GetFileAttributes(pathPtr)
		if err == nil {
			if (attrs & windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
				return
			}

			if !app.wof.IsCompressedDirect(pathPtr) {
				handle, err := windows.CreateFile(
					pathPtr,
					windows.GENERIC_READ|windows.GENERIC_WRITE,
					windows.FILE_SHARE_READ,
					nil,
					windows.OPEN_EXISTING,
					0,
					0,
				)
				if err == nil {
					if app.wof.CompressHandle(handle) == nil {
						atomic.AddUint64(&app.stats.Processed, 1)
						app.ipc.Progress("Auto: "+filepath.Base(path), &app.stats, 0)
						windows.CloseHandle(handle)
						return
					}
					windows.CloseHandle(handle)
				}
			}
		}
		time.Sleep(time.Duration(1<<i) * 200 * time.Millisecond)
	}
}

func main() {
	app := NewApp()
	app.Run()
}
