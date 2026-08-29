package platform

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	foDelete          = 0x0003
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
)

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	procSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

func openWithHandler(target string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func trashItem(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	from, err := syscall.UTF16FromString(abs)
	if err != nil {
		return err
	}
	from = append(from, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	ret, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if ret != 0 {
		return fmt.Errorf("trash %q: code %d", abs, ret)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("trash %q: aborted", abs)
	}
	return nil
}

func openCmd(path string) error {
	cmd := exec.Command("cmd.exe", "/c", "start", "cmd.exe")
	cmd.Dir = path
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008} // DETACHED_PROCESS
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procGetClipboardData         = user32.NewProc("GetClipboardData")
	procRegisterClipboardFormatW = user32.NewProc("RegisterClipboardFormatW")
	procGlobalAlloc              = kernel32.NewProc("GlobalAlloc")
	procGlobalLock               = kernel32.NewProc("GlobalLock")
	procGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	procGlobalSize               = kernel32.NewProc("GlobalSize")
	fileNameWFormat              uint32
	uriListFormat                uint32
)

func writeClipboardText(text string) error {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := len(utf16) * 2
	mem, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if mem == 0 {
		return fmt.Errorf("clipboard alloc: %w", err)
	}
	ptr, _, err := procGlobalLock.Call(mem)
	if ptr == 0 {
		return fmt.Errorf("clipboard lock: %w", err)
	}
	copy(unsafe.Slice((*byte)(ptrToUnsafe(ptr)), size), unsafe.Slice((*byte)(unsafe.Pointer(&utf16[0])), size))
	_, _, _ = procGlobalUnlock.Call(mem)
	if r, _, err := procOpenClipboard.Call(0); r == 0 {
		return fmt.Errorf("open clipboard: %w", err)
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("empty clipboard: %w", err)
	}
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, mem); r == 0 {
		return fmt.Errorf("set clipboard: %w", err)
	}
	return nil
}

func clipboardFiles() []string {
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return []string{}
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	if path := readClipboardFileNameW(); path != "" {
		return []string{path}
	}
	return readClipboardURIList()
}

func readClipboardFileNameW() string {
	if fileNameWFormat == 0 {
		name, _ := syscall.UTF16PtrFromString("FileNameW")
		r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
		fileNameWFormat = uint32(r)
	}
	if fileNameWFormat == 0 {
		return ""
	}
	handle, _, _ := procGetClipboardData.Call(uintptr(fileNameWFormat))
	if handle == 0 {
		return ""
	}
	return trimTrailingNUL(utf16FromGlobal(handle))
}

func readClipboardURIList() []string {
	if uriListFormat == 0 {
		name, _ := syscall.UTF16PtrFromString("text/uri-list")
		r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
		uriListFormat = uint32(r)
	}
	if uriListFormat == 0 {
		return nil
	}
	handle, _, _ := procGetClipboardData.Call(uintptr(uriListFormat))
	if handle == 0 {
		return nil
	}
	return parseClipboardURIList(trimTrailingNUL(string(bytesFromGlobal(handle))))
}

func parseClipboardURIList(text string) []string {
	var files []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "file://") {
			continue
		}
		parsed, err := url.Parse(line)
		if err != nil {
			continue
		}
		path := parsed.Path
		if strings.HasPrefix(path, "/") && len(path) >= 3 && path[2] == ':' {
			path = path[1:]
		}
		if decoded, err := url.PathUnescape(path); err == nil {
			path = decoded
		}
		if path != "" {
			files = append(files, filepath.FromSlash(path))
		}
	}
	return files
}

func bytesFromGlobal(handle uintptr) []byte {
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil
	}
	defer func() { _, _, _ = procGlobalUnlock.Call(handle) }()
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return nil
	}
	return append([]byte(nil), unsafe.Slice((*byte)(ptrToUnsafe(ptr)), int(size))...)
}

func utf16FromGlobal(handle uintptr) string {
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return ""
	}
	defer func() { _, _, _ = procGlobalUnlock.Call(handle) }()
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return ""
	}
	words := unsafe.Slice((*uint16)(ptrToUnsafe(ptr)), int(size)/2)
	return syscall.UTF16ToString(words)
}

func ptrToUnsafe(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func trimTrailingNUL(value string) string {
	return strings.TrimRight(value, "\x00")
}
