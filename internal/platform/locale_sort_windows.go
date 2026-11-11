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
	icuDLL           = syscall.NewLazyDLL("icu.dll")
	ucolOpen         = icuDLL.NewProc("ucol_open")
	ucolStrcoll      = icuDLL.NewProc("ucol_strcoll")
	ucolClose        = icuDLL.NewProc("ucol_close")
	ucolSetAttr      = icuDLL.NewProc("ucol_setAttribute")
	localeCollators  sync.Map
	naturalCollators sync.Map
)

// ICU UColAttribute and UColAttributeValue constants.
const (
	ucolCaseFirst        = 2
	ucolCaseLevel        = 3
	ucolStrength         = 5
	ucolNumericCollation = 7
	ucolPrimary          = 0
	ucolOn               = 17
	ucolLowerFirst       = 24
)

// NewLocaleLess returns an Electron localeCompare-compatible comparator for
// the current Windows locale, falling back to byte ordering when ICU is not
// available.
func NewLocaleLess() func(string, string) bool {
	return NewLocaleLessFor(SystemLocale())
}

func NewLocaleLessFor(locale string) func(string, string) bool {
	c := loadLocaleCollator(locale, false)
	if c == nil {
		return func(a, b string) bool { return strings.Compare(a, b) < 0 }
	}
	return func(a, b string) bool { return c.compare(a, b) < 0 }
}

// NewNaturalCompare returns a three-way comparator for the current Windows locale that orders digit runs by
// their value and lower case first, ignoring accents, as Intl.Collator with numeric, sensitivity "case" and
// caseFirst "lower" does. It falls back to byte ordering when ICU is not available.
func NewNaturalCompare() func(string, string) int {
	return newNaturalCompareFor(SystemLocale())
}

func newNaturalCompareFor(locale string) func(string, string) int {
	c := loadLocaleCollator(locale, true)
	if c == nil {
		return strings.Compare
	}
	return c.compare
}

func (c *localeCollator) compare(a, b string) int {
	left, leftErr := syscall.UTF16FromString(a)
	right, rightErr := syscall.UTF16FromString(b)
	if leftErr != nil || rightErr != nil {
		return strings.Compare(a, b)
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
	return int(int32(result))
}

func loadLocaleCollator(locale string, natural bool) *localeCollator {
	cache := &localeCollators
	if natural {
		cache = &naturalCollators
	}
	if cached, ok := cache.Load(locale); ok {
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
	if natural {
		for _, attr := range [][2]uintptr{
			{ucolNumericCollation, ucolOn},
			{ucolStrength, ucolPrimary},
			{ucolCaseLevel, ucolOn},
			{ucolCaseFirst, ucolLowerFirst},
		} {
			_, _, _ = ucolSetAttr.Call(handle, attr[0], attr[1], uintptr(unsafe.Pointer(&status)))
		}
		if status > 0 {
			_, _, _ = ucolClose.Call(handle)
			return nil
		}
	}
	created := &localeCollator{handle: handle}
	actual, loaded := cache.LoadOrStore(locale, created)
	if loaded {
		_, _, _ = ucolClose.Call(handle)
		return actual.(*localeCollator)
	}
	return created
}
