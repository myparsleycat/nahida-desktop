//go:build windows

package platform

import (
	"unsafe"
)

var procGlobalMemoryStatusEx = modKernel32.NewProc("GlobalMemoryStatusEx")

// memoryStatusEx is MEMORYSTATUSEX.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

// PhysicalMemory returns the size of the installed physical memory in bytes, or 0 when it cannot be read.
func PhysicalMemory() uint64 {
	status := memoryStatusEx{}
	status.length = uint32(unsafe.Sizeof(status))
	if ok, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status))); ok == 0 {
		return 0
	}
	return status.totalPhys
}
