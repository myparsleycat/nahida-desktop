//go:build windows

package xxmi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	nvAppBackendRelativePath = `NVIDIA Corporation\NVIDIA App\NvBackend\NvBackendAPI64.dll`
	nvAppStorageRelativePath = `NVIDIA Corporation\NVIDIA App\NvBackend\ApplicationStorage.json`
)

type nvAppApplicationStorage struct {
	Applications []nvAppStoredApplication `json:"Applications"`
}

type nvAppStoredApplication struct {
	LocalID     uint32               `json:"LocalId"`
	Application nvAppApplicationInfo `json:"Application"`
}

type nvAppApplicationInfo struct {
	DriverProfile string   `json:"DriverProfile"`
	ImageFiles    []string `json:"ImageFiles"`
	DetectedFiles []string `json:"DetectedFiles"`
}

type nvAppFGXState struct {
	enabled   byte
	useGlobal byte
}

// nvAppFGXProcs is the small native surface used by NVIDIA App's own
// NvCplPlugin for its per-program Smooth Motion (FGX) setting.
type nvAppFGXProcs struct {
	initialize   uintptr
	deinitialize uintptr
	getState     uintptr
	setState     uintptr
}

var nvAppFGXMu sync.Mutex

var errNVAppApplicationAmbiguous = errors.New("multiple NVIDIA App applications match")

func readNVAppSmoothMotion(exe string) (enabled, handled bool, err error) {
	handled, err = withNVAppFGX(exe, func(app nvAppStoredApplication, procs nvAppFGXProcs) error {
		state, err := getNVAppFGXState(procs, app)
		if err != nil {
			return err
		}
		enabled = state.enabled != 0
		return nil
	})
	return enabled, handled, err
}

func writeNVAppSmoothMotionOff(exe string) (bool, error) {
	return withNVAppFGX(exe, func(app nvAppStoredApplication, procs nvAppFGXProcs) error {
		path, err := windows.UTF16PtrFromString(app.Application.DriverProfile)
		if err != nil {
			return fmt.Errorf("encode NVIDIA App game path: %w", err)
		}
		state := nvAppFGXState{}
		status := nvBackendCall(
			procs.setState,
			0,
			uintptr(app.LocalID),
			uintptr(unsafe.Pointer(path)),
			uintptr(unsafe.Pointer(&state)),
		)
		runtime.KeepAlive(path)
		runtime.KeepAlive(&state)
		if status != 0 {
			return fmt.Errorf("NVIDIA App SetFGXState for local application %d: status %d", app.LocalID, status)
		}

		stored, err := getNVAppFGXState(procs, app)
		if err != nil {
			return fmt.Errorf("verify NVIDIA App Smooth Motion override: %w", err)
		}
		if stored.enabled != 0 || stored.useGlobal != 0 {
			return fmt.Errorf(
				"%w through NVIDIA App (enabled=%d, useGlobal=%d)",
				errSmoothMotionWriteNotApplied,
				stored.enabled,
				stored.useGlobal,
			)
		}
		return nil
	})
}

func getNVAppFGXState(procs nvAppFGXProcs, app nvAppStoredApplication) (nvAppFGXState, error) {
	path, err := windows.UTF16PtrFromString(app.Application.DriverProfile)
	if err != nil {
		return nvAppFGXState{}, fmt.Errorf("encode NVIDIA App game path: %w", err)
	}
	var state nvAppFGXState
	status := nvBackendCall(
		procs.getState,
		uintptr(app.LocalID),
		uintptr(unsafe.Pointer(path)),
		uintptr(unsafe.Pointer(&state)),
	)
	runtime.KeepAlive(path)
	runtime.KeepAlive(&state)
	if status != 0 {
		return nvAppFGXState{}, fmt.Errorf(
			"NVIDIA App GetFGXState for local application %d: status %d",
			app.LocalID,
			status,
		)
	}
	return state, nil
}

func withNVAppFGX(
	exe string,
	fn func(nvAppStoredApplication, nvAppFGXProcs) error,
) (handled bool, err error) {
	app, found, err := resolveNVAppApplication(exe)
	if errors.Is(err, errNVAppApplicationAmbiguous) {
		// NVIDIA App may own this executable, but selecting either matching application
		// could read or update an unrelated game's setting. Keep the operation handled so
		// callers do not fall through to the driver database as though NVIDIA App were absent.
		return true, err
	}
	if err != nil {
		return true, fmt.Errorf("resolve NVIDIA App application: %w", err)
	}
	if !found {
		return false, nil
	}

	dllPath, found := nvAppBackendPath()
	if !found {
		return false, nil
	}

	nvAppFGXMu.Lock()
	defer nvAppFGXMu.Unlock()

	dll, procs, err := loadNVAppFGX(dllPath)
	if err != nil {
		return true, err
	}
	defer func() {
		if freeErr := windows.FreeLibrary(dll); freeErr != nil {
			err = errors.Join(err, fmt.Errorf("unload NVIDIA App backend: %w", freeErr))
		}
	}()

	if status := nvBackendCall(procs.initialize); status != 0 {
		return true, fmt.Errorf("initialize NVIDIA App backend: status %d", status)
	}
	defer func() {
		if status := nvBackendCall(procs.deinitialize); status != 0 {
			err = errors.Join(err, fmt.Errorf("deinitialize NVIDIA App backend: status %d", status))
		}
	}()

	return true, fn(app, procs)
}

func loadNVAppFGX(path string) (windows.Handle, nvAppFGXProcs, error) {
	dll, err := windows.LoadLibraryEx(
		path,
		0,
		windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32,
	)
	if err != nil {
		return 0, nvAppFGXProcs{}, fmt.Errorf("load NVIDIA App backend: %w", err)
	}
	procs := nvAppFGXProcs{}
	for name, destination := range map[string]*uintptr{
		"EasyAPIInit":   &procs.initialize,
		"EasyAPIDeinit": &procs.deinitialize,
		"GetFGXState":   &procs.getState,
		"SetFGXState":   &procs.setState,
	} {
		*destination, err = windows.GetProcAddress(dll, name)
		if err != nil {
			_ = windows.FreeLibrary(dll)
			return 0, nvAppFGXProcs{}, fmt.Errorf("load NVIDIA App backend procedure %s: %w", name, err)
		}
	}
	return dll, procs, nil
}

func nvBackendCall(proc uintptr, args ...uintptr) uint32 {
	result, _, _ := syscall.SyscallN(proc, args...)
	return uint32(result)
}

func nvAppBackendPath() (string, bool) {
	programFiles := os.Getenv("ProgramW6432")
	if programFiles == "" {
		programFiles = os.Getenv("ProgramFiles")
	}
	if programFiles == "" {
		return "", false
	}
	path := filepath.Join(programFiles, nvAppBackendRelativePath)
	info, err := os.Stat(path)
	return path, err == nil && info.Mode().IsRegular()
}

func resolveNVAppApplication(exe string) (nvAppStoredApplication, bool, error) {
	localAppData, err := os.UserCacheDir()
	if err != nil {
		return nvAppStoredApplication{}, false, fmt.Errorf("resolve LocalAppData for NVIDIA App: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(localAppData, nvAppStorageRelativePath))
	if errors.Is(err, os.ErrNotExist) {
		return nvAppStoredApplication{}, false, nil
	}
	if err != nil {
		return nvAppStoredApplication{}, false, fmt.Errorf("read NVIDIA App application storage: %w", err)
	}
	return findNVAppApplication(data, exe)
}

func findNVAppApplication(data []byte, exe string) (nvAppStoredApplication, bool, error) {
	var storage nvAppApplicationStorage
	if err := json.Unmarshal(data, &storage); err != nil {
		return nvAppStoredApplication{}, false, fmt.Errorf("decode NVIDIA App application storage: %w", err)
	}

	wanted := filepath.Clean(exe)
	if filepath.IsAbs(wanted) {
		if exact, found := findNVAppApplicationByPath(storage.Applications, wanted); found {
			return exact, true, nil
		}
	}

	// Only an absolute path is precise enough to pick between applications. A basename is
	// shared by many executables, and a driver profile may even name a supporting process,
	// so any basename match that spans more than one local application stays unresolved
	// instead of preferring one field over another.
	wantedBase := filepath.Base(wanted)
	var best nvAppStoredApplication
	found := false
	ambiguous := false
	for _, app := range storage.Applications {
		if app.LocalID == 0 || app.Application.DriverProfile == "" {
			continue
		}
		if !nvAppApplicationHasBasename(app.Application, wantedBase) {
			continue
		}
		if found && app.LocalID != best.LocalID {
			ambiguous = true
			continue
		}
		best, found = app, true
	}
	switch {
	case ambiguous:
		return nvAppStoredApplication{}, false, fmt.Errorf("%w %s", errNVAppApplicationAmbiguous, exe)
	case !found:
		return nvAppStoredApplication{}, false, nil
	default:
		return best, true, nil
	}
}

func findNVAppApplicationByPath(apps []nvAppStoredApplication, wanted string) (nvAppStoredApplication, bool) {
	for _, app := range apps {
		if app.LocalID == 0 || app.Application.DriverProfile == "" {
			continue
		}
		for _, path := range nvAppApplicationPaths(app.Application) {
			if path != "" && strings.EqualFold(filepath.Clean(path), wanted) {
				return app, true
			}
		}
	}
	return nvAppStoredApplication{}, false
}

func nvAppApplicationHasBasename(app nvAppApplicationInfo, name string) bool {
	for _, path := range nvAppApplicationPaths(app) {
		if path != "" && strings.EqualFold(filepath.Base(path), name) {
			return true
		}
	}
	return false
}

func nvAppApplicationPaths(app nvAppApplicationInfo) []string {
	paths := make([]string, 0, 1+len(app.ImageFiles)+len(app.DetectedFiles))
	paths = append(paths, app.DriverProfile)
	paths = append(paths, app.ImageFiles...)
	paths = append(paths, app.DetectedFiles...)
	return paths
}
