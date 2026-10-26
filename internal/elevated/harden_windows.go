//go:build windows

package elevated

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	processImageLoadPolicy = 10

	imageLoadNoRemoteImages            = 1 << 0
	imageLoadNoLowMandatoryLabelImages = 1 << 1
	imageLoadPreferSystem32Images      = 1 << 2
)

var (
	errServerHardening = errors.New("elevated helper could not restrict DLL loading")

	procSetProcessMitigationPolicy = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetProcessMitigationPolicy")
)

// HardenHelperProcess keeps DLLs out of the elevated helper unless they come from System32 or are loaded by
// absolute path. The helper runs from a folder its unelevated user can write to, and Windows otherwise
// searches the executable's folder first, so it has to run before anything loads a library by name.
func HardenHelperProcess() error {
	if err := windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32); err != nil {
		return fmt.Errorf("%w: set default DLL directories: %w", errServerHardening, err)
	}
	policy := uint32(imageLoadNoRemoteImages | imageLoadNoLowMandatoryLabelImages | imageLoadPreferSystem32Images)
	result, _, callErr := procSetProcessMitigationPolicy.Call(
		processImageLoadPolicy,
		uintptr(unsafe.Pointer(&policy)),
		unsafe.Sizeof(policy),
	)
	if result == 0 {
		return fmt.Errorf("%w: set image load policy: %w", errServerHardening, callErr)
	}
	return nil
}
