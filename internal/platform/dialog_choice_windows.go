//go:build windows

package platform

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	taskDialogOverwriteButton = 100
	taskDialogRenameButton    = 101
	taskDialogCancelButton    = 102

	taskDialogAllowCancellation        = 0x0008
	taskDialogPositionRelativeToWindow = 0x1000
	taskDialogSizeToContent            = 0x01000000
)

type taskDialogButton struct {
	id   int32
	text *uint16
}

type taskDialogConfig struct {
	size                 uint32
	parent               uintptr
	instance             uintptr
	flags                uint32
	commonButtons        uint32
	windowTitle          *uint16
	mainIcon             uintptr
	mainInstruction      *uint16
	content              *uint16
	buttonCount          uint32
	buttons              *taskDialogButton
	defaultButton        int32
	radioButtonCount     uint32
	radioButtons         uintptr
	defaultRadioButton   int32
	verificationText     *uint16
	expandedInformation  *uint16
	expandedControlText  *uint16
	collapsedControlText *uint16
	footerIcon           uintptr
	footer               *uint16
	callback             uintptr
	callbackData         uintptr
	width                uint32
}

var taskDialogIndirect = windows.NewLazySystemDLL("comctl32.dll").NewProc("TaskDialogIndirect")

func defaultDirectoryConflict(opts DirectoryConflictOptions) (DirectoryConflictChoice, error) {
	win, err := requireMainWindow()
	if err != nil {
		return "", err
	}
	name := opts.Name
	if name == "" {
		name = "Download"
	}
	title, err := windows.UTF16PtrFromString("폴더가 이미 존재합니다")
	if err != nil {
		return "", err
	}
	instruction, err := windows.UTF16PtrFromString(fmt.Sprintf("%q 폴더가 이미 존재합니다.", name))
	if err != nil {
		return "", err
	}
	content, err := windows.UTF16PtrFromString("기존 폴더에 파일을 덮어쓰시겠습니까?")
	if err != nil {
		return "", err
	}
	overwrite, err := windows.UTF16PtrFromString("덮어쓰기")
	if err != nil {
		return "", err
	}
	rename, err := windows.UTF16PtrFromString("새 이름으로 다운로드")
	if err != nil {
		return "", err
	}
	cancel, err := windows.UTF16PtrFromString("취소")
	if err != nil {
		return "", err
	}
	buttons := [...]taskDialogButton{
		{id: taskDialogOverwriteButton, text: overwrite},
		{id: taskDialogRenameButton, text: rename},
		{id: taskDialogCancelButton, text: cancel},
	}
	parent := uintptr(0)
	if native := win.NativeWindow(); native != nil {
		parent = uintptr(native)
	}
	config := taskDialogConfig{
		size:            uint32(unsafe.Sizeof(taskDialogConfig{})),
		parent:          parent,
		flags:           taskDialogAllowCancellation | taskDialogPositionRelativeToWindow | taskDialogSizeToContent,
		windowTitle:     title,
		mainInstruction: instruction,
		content:         content,
		buttonCount:     uint32(len(buttons)),
		buttons:         &buttons[0],
		defaultButton:   taskDialogRenameButton,
	}
	var selected int32
	result, _, callErr := taskDialogIndirect.Call(
		uintptr(unsafe.Pointer(&config)),
		uintptr(unsafe.Pointer(&selected)),
		0,
		0,
	)
	runtime.KeepAlive(buttons)
	runtime.KeepAlive(config)
	if result != 0 {
		return "", fmt.Errorf("show directory conflict dialog: HRESULT 0x%08X: %w", uint32(result), callErr)
	}
	switch selected {
	case taskDialogOverwriteButton:
		return DirectoryConflictOverwrite, nil
	case taskDialogRenameButton:
		return DirectoryConflictRename, nil
	case taskDialogCancelButton, 2:
		return DirectoryConflictCancel, nil
	default:
		return "", fmt.Errorf("directory conflict dialog returned button %d", selected)
	}
}
