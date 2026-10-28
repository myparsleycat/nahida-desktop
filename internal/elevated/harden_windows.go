//go:build windows

package elevated

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

var errServerHardening = errors.New("elevated helper could not restrict DLL loading")

// HardenHelperProcess keeps DLLs out of the elevated helper unless they come from System32 or are loaded by
// absolute path. The helper runs from a folder its unelevated user can write to, and Windows otherwise
// searches the executable's folder first, so it has to run before anything loads a library by name.
//
// An image load mitigation policy is deliberately not set: child processes inherit it, so the games the helper
// starts would load System32 copies instead of the proxy DLLs mods such as OptiScaler place beside them.
func HardenHelperProcess() error {
	if err := windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32); err != nil {
		return fmt.Errorf("%w: set default DLL directories: %w", errServerHardening, err)
	}
	return nil
}
