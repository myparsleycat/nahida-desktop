//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const localeNameMax = 85

func systemLocaleFromOS() string {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	if name := localeNameFromUILanguage(kernel32); name != "" {
		return name
	}
	return localeNameFromUserDefault(kernel32)
}

func localeNameFromUILanguage(kernel32 *windows.LazyDLL) string {
	getUILanguage := kernel32.NewProc("GetUserDefaultUILanguage")
	lcidToLocaleName := kernel32.NewProc("LCIDToLocaleName")
	if err := getUILanguage.Find(); err != nil {
		return ""
	}
	if err := lcidToLocaleName.Find(); err != nil {
		return ""
	}
	langID, _, _ := getUILanguage.Call()
	if langID == 0 {
		return ""
	}
	buf := make([]uint16, localeNameMax)
	n, _, _ := lcidToLocaleName.Call(langID, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func localeNameFromUserDefault(kernel32 *windows.LazyDLL) string {
	getLocaleName := kernel32.NewProc("GetUserDefaultLocaleName")
	if err := getLocaleName.Find(); err != nil {
		return ""
	}
	buf := make([]uint16, localeNameMax)
	n, _, _ := getLocaleName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}
