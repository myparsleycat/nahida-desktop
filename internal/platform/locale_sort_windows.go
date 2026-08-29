//go:build windows

package platform

import (
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type localeCollator struct {
	mu     sync.Mutex
	handle uintptr
}

var (
	icuDLL          = syscall.NewLazyDLL("icu.dll")
	ucolOpen        = icuDLL.NewProc("ucol_open")
	ucolStrcoll     = icuDLL.NewProc("ucol_strcoll")
	ucolClose       = icuDLL.NewProc("ucol_close")
	localeCollators sync.Map
)

// NewLocaleLess returns an Electron localeCompare-compatible comparator for
// the current Windows locale, falling back to byte ordering when ICU is not
// available.
func NewLocaleLess() func(string, string) bool {
	return NewLocaleLessFor(SystemLocale())
}

func NewLocaleLessFor(locale string) func(string, string) bool {
	c := loadLocaleCollator(locale)
	if c == nil {
		return func(a, b string) bool { return strings.Compare(a, b) < 0 }
	}
	return func(a, b string) bool {
		left, leftErr := syscall.UTF16FromString(a)
		right, rightErr := syscall.UTF16FromString(b)
		if leftErr != nil || rightErr != nil {
			return strings.Compare(a, b) < 0
		}
		c.mu.Lock()
		result, _, _ := ucolStrcoll.Call(
			c.handle,
			uintptr(unsafe.Pointer(&left[0])),
			uintptr(len(left)-1),
			uintptr(unsafe.Pointer(&right[0])),
			uintptr(len(right)-1),
		)
		c.mu.Unlock()
		return int32(result) < 0
	}
}

func loadLocaleCollator(locale string) *localeCollator {
	if cached, ok := localeCollators.Load(locale); ok {
		return cached.(*localeCollator)
	}
	name, err := syscall.BytePtrFromString(locale)
	if err != nil {
		return nil
	}
	var status int32
	handle, _, _ := ucolOpen.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&status)),
	)
	if handle == 0 || status > 0 {
		return nil
	}
	created := &localeCollator{handle: handle}
	actual, loaded := localeCollators.LoadOrStore(locale, created)
	if loaded {
		_, _, _ = ucolClose.Call(handle)
		return actual.(*localeCollator)
	}
	return created
}
