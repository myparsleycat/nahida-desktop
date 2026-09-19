//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
)

func TestSelectWindowRequiresTarget(t *testing.T) {
	t.Parallel()

	if _, err := selectWindow(nil, WindowTarget{}); !errors.Is(err, ErrInputTargetRequired) {
		t.Fatalf("selectWindow(no target) = %v, want %v", err, ErrInputTargetRequired)
	}
}

func TestSelectWindowPrefersForegroundThenLargest(t *testing.T) {
	t.Parallel()

	background := windowRecord{handle: 0x1, title: "Game", processName: "game.exe", pid: 10, area: 900}
	foreground := windowRecord{
		handle: 0x2, title: "Game", processName: "game.exe", pid: 10, area: 100, isForeground: true,
	}

	record, err := selectWindow([]windowRecord{background, foreground}, WindowTarget{Title: "Game"})
	if err != nil {
		t.Fatalf("selectWindow = %v", err)
	}
	if record.handle != foreground.handle {
		t.Fatalf("selectWindow picked 0x%X, want the foreground window 0x%X", record.handle, foreground.handle)
	}

	record, err = selectWindow([]windowRecord{background, foreground}, WindowTarget{Process: "game.exe", PID: 10})
	if err != nil {
		t.Fatalf("selectWindow by process and pid = %v", err)
	}
	if record.handle != foreground.handle {
		t.Fatalf("selectWindow picked 0x%X, want 0x%X", record.handle, foreground.handle)
	}
}

func TestSelectWindowNarrowsTitlesExactThenPrefix(t *testing.T) {
	t.Parallel()

	records := []windowRecord{
		{handle: 0x1, title: "Nahida", processName: "launcher.exe", pid: 10, area: 900, isForeground: true},
		{handle: 0x2, title: "Nahida Test", processName: "launcher.exe", pid: 10, area: 10},
		{handle: 0x3, title: "Nahida Test Overlay", processName: "launcher.exe", pid: 10, area: 500},
	}

	// An exact title wins over the foreground window and over a larger window.
	record, err := selectWindow(records, WindowTarget{Title: "nahida test"})
	if err != nil {
		t.Fatalf("selectWindow(exact title) = %v", err)
	}
	if record.handle != 0x2 {
		t.Fatalf("selectWindow(exact title) picked 0x%X, want 0x2", record.handle)
	}

	// A prefix narrows the candidates before the ranking decides.
	record, err = selectWindow(records, WindowTarget{Title: "Nahida Te"})
	if err != nil {
		t.Fatalf("selectWindow(prefix title) = %v", err)
	}
	if record.handle != 0x3 {
		t.Fatalf("selectWindow(prefix title) picked 0x%X, want the larger window 0x3", record.handle)
	}

	// A substring only matched by one window selects that window.
	record, err = selectWindow(records, WindowTarget{Title: "Overlay"})
	if err != nil {
		t.Fatalf("selectWindow(substring title) = %v", err)
	}
	if record.handle != 0x3 {
		t.Fatalf("selectWindow(substring title) picked 0x%X, want 0x3", record.handle)
	}
}

func TestSelectWindowReportsMissingWindow(t *testing.T) {
	t.Parallel()

	records := []windowRecord{{handle: 0x1, title: "Game", processName: "game.exe", pid: 10}}
	cases := []WindowTarget{
		{Process: "other.exe"},
		{PID: 11},
		{Title: "Missing"},
		{Process: "game.exe", PID: 11},
		{Title: "Game", Process: "other.exe"},
	}

	for _, target := range cases {
		if _, err := selectWindow(records, target); !errors.Is(err, ErrWindowNotFound) {
			t.Fatalf("selectWindow(%s) = %v, want %v", target.describe(), err, ErrWindowNotFound)
		}
	}
}

func TestMatchesProcessAcceptsExactAndSubstring(t *testing.T) {
	t.Parallel()

	cases := []struct {
		processName string
		wanted      string
		want        bool
	}{
		{processName: "StarRail.exe", wanted: "StarRail.exe", want: true},
		{processName: "StarRail.exe", wanted: "starrail", want: true},
		{processName: "StarRail.exe", wanted: "GenshinImpact.exe", want: false},
		{processName: "", wanted: "starrail", want: false},
		{processName: "StarRail.exe", wanted: "", want: true},
		{processName: "", wanted: "", want: true},
	}

	for _, testCase := range cases {
		if got := matchesProcess(testCase.processName, testCase.wanted); got != testCase.want {
			t.Fatalf("matchesProcess(%q, %q) = %t, want %t",
				testCase.processName, testCase.wanted, got, testCase.want)
		}
	}
}

func TestListWindowsAndResolveWindowSkipHiddenWindows(t *testing.T) {
	title := "Nahida Input Test " + t.Name()
	window := newTestWindow(t, title)
	input := NewInput()
	ctx := context.Background()

	// A hidden window is never a target: it could not be focused either.
	found, err := input.ListWindows(ctx, WindowFilter{Title: title})
	if err != nil {
		t.Fatalf("ListWindows = %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("ListWindows found %d hidden windows: %+v", len(found), found)
	}
	if _, err := input.ResolveWindow(ctx, WindowTarget{Title: title}); !errors.Is(err, ErrWindowNotFound) {
		t.Fatalf("ResolveWindow(hidden window) = %v, want %v", err, ErrWindowNotFound)
	}

	// With the visibility seam pointing at the test window, the reported
	// properties come from the real Win32 calls.
	window.restrictTo(input)
	info, err := input.ResolveWindow(ctx, WindowTarget{Title: title})
	if err != nil {
		t.Fatalf("ResolveWindow = %v", err)
	}
	if want := fmt.Sprintf("0x%X", uintptr(window.handle)); info.Handle != want {
		t.Fatalf("handle = %q, want %q", info.Handle, want)
	}
	if info.Title != title || info.PID != uint32(os.Getpid()) {
		t.Fatalf("window = %+v, want title %q and the current pid", info, title)
	}
	if !strings.HasPrefix(info.ClassName, testWindowClassPrefix) {
		t.Fatalf("className = %q, want the %s prefix", info.ClassName, testWindowClassPrefix)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(info.ProcessName, filepath.Base(executable)) {
		t.Fatalf("processName = %q, want %q", info.ProcessName, filepath.Base(executable))
	}
	if info.IsForeground || info.IsMinimized {
		t.Fatalf("window = %+v, want neither foreground nor minimized", info)
	}

	found, err = input.ListWindows(ctx, WindowFilter{Process: filepath.Base(executable)})
	if err != nil {
		t.Fatalf("ListWindows(process) = %v", err)
	}
	if len(found) != 1 || found[0].Handle != info.Handle {
		t.Fatalf("ListWindows(process) = %+v, want the test window only", found)
	}

	// The visibility filter drops the window even when it is enumerable.
	input.isVisible = func(win.HWND) bool { return false }
	found, err = input.ListWindows(ctx, WindowFilter{})
	if err != nil {
		t.Fatalf("ListWindows(invisible) = %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("ListWindows(invisible) = %+v, want nothing", found)
	}
}

func TestWindowInProcessMatchesHandleAndProcess(t *testing.T) {
	window := newTestWindow(t, "Nahida Input Test "+t.Name())
	record := &windowRecord{handle: window.handle, pid: uint32(os.Getpid())}

	if !windowInProcess(record, window.handle) {
		t.Fatal("windowInProcess(own handle) = false, want true")
	}
	if windowInProcess(record, 0) {
		t.Fatal("windowInProcess(0) = true, want false")
	}
	if windowInProcess(record, win.GetDesktopWindow()) {
		t.Fatal("windowInProcess(desktop window) = true, want false")
	}
}

func TestSendKeysPostsKeysToBackgroundWindow(t *testing.T) {
	title := "Nahida Input Test " + t.Name()
	window := newTestWindow(t, title)
	input := NewInput()
	window.restrictTo(input)

	result, err := input.SendKeys(context.Background(), KeyRequest{
		Target:   WindowTarget{Title: title},
		Keys:     []string{"vk_f10"},
		Delivery: KeyDeliveryMessage,
		HoldMs:   10,
	})
	if err != nil {
		t.Fatalf("SendKeys = %v", err)
	}
	if result.Window.PID != uint32(os.Getpid()) || result.Delivery != KeyDeliveryMessage {
		t.Fatalf("result = %+v, want the test window and message delivery", result)
	}
	if !slices.Equal(result.Keys, []string{"vk_f10"}) {
		t.Fatalf("result.Keys = %v, want [vk_f10]", result.Keys)
	}

	// MapVirtualKey reports 0x44 for F10 on every keyboard layout.
	const f10Scan = 0x44
	if scan := uint16(win.MapVirtualKey(co.VK_F10, co.MAPVK_VK_TO_VSC)); scan != f10Scan {
		t.Fatalf("MapVirtualKey(VK_F10) = 0x%X, want 0x%X", scan, f10Scan)
	}

	down := window.next(t)
	if down.msg != co.WM_KEYDOWN || co.VK(down.wParam) != co.VK_F10 {
		t.Fatalf("first message = 0x%X with wParam 0x%X, want WM_KEYDOWN with VK_F10", down.msg, down.wParam)
	}
	if want := keyLParam(f10Scan, false, false); uintptr(down.lParam) != want {
		t.Fatalf("key down lParam = 0x%X, want 0x%X", down.lParam, want)
	}

	up := window.next(t)
	if up.msg != co.WM_KEYUP || co.VK(up.wParam) != co.VK_F10 {
		t.Fatalf("second message = 0x%X with wParam 0x%X, want WM_KEYUP with VK_F10", up.msg, up.wParam)
	}
	if want := keyLParam(f10Scan, false, true); uintptr(up.lParam) != want {
		t.Fatalf("key up lParam = 0x%X, want 0x%X", up.lParam, want)
	}
}

func TestSendKeysDoesNotInjectWithoutForeground(t *testing.T) {
	title := "Nahida Input Test " + t.Name()
	window := newTestWindow(t, title)
	input := NewInput()
	window.restrictTo(input)

	focused := 0
	input.focus = func(win.HWND) { focused++ }
	input.focusTimeout = 50 * time.Millisecond
	input.focusPoll = 5 * time.Millisecond
	input.sendInput = func([]win.INPUT) (int, error) {
		t.Error("SendKeys injected input even though the window never reached the foreground")
		return 0, nil
	}

	_, err := input.SendKeys(context.Background(), KeyRequest{
		Target: WindowTarget{Title: title},
		Keys:   []string{"vk_f10"},
	})
	if !errors.Is(err, ErrWindowNotForeground) {
		t.Fatalf("SendKeys = %v, want %v", err, ErrWindowNotForeground)
	}
	if focused != 1 {
		t.Fatalf("focus attempts = %d, want 1", focused)
	}
	if messages := window.drain(); len(messages) != 0 {
		t.Fatalf("window received %+v, want nothing", messages)
	}
}

func TestSendKeysInjectsScanCodesAndRestoresFocus(t *testing.T) {
	title := "Nahida Input Test " + t.Name()
	window := newTestWindow(t, title)
	input := NewInput()
	window.restrictTo(input)

	previous := win.GetDesktopWindow()
	current := previous
	var focused []win.HWND
	var batches [][]win.INPUT
	input.foreground = func() win.HWND { return current }
	input.focus = func(handle win.HWND) {
		focused = append(focused, handle)
		current = handle
	}
	input.sendInput = func(inputs []win.INPUT) (int, error) {
		batches = append(batches, slices.Clone(inputs))
		return len(inputs), nil
	}

	result, err := input.SendKeys(context.Background(), KeyRequest{
		Target: WindowTarget{Title: title},
		Keys:   []string{"shift vk_f10"},
	})
	if err != nil {
		t.Fatalf("SendKeys = %v", err)
	}
	if result.Delivery != KeyDeliveryForeground {
		t.Fatalf("delivery = %q, want %q", result.Delivery, KeyDeliveryForeground)
	}
	if want := []win.HWND{window.handle, previous}; !slices.Equal(focused, want) {
		t.Fatalf("focus calls = %+v, want the target window then the previous one", focused)
	}

	// MapVirtualKey reports 0x44 for F10 on every keyboard layout; the shift
	// scan code is read back instead of hardcoding a layout dependent value.
	f10Scan := uint16(win.MapVirtualKey(co.VK_F10, co.MAPVK_VK_TO_VSC))
	shiftScan := uint16(win.MapVirtualKey(co.VK_SHIFT, co.MAPVK_VK_TO_VSC))
	type pressedKey struct {
		scan  uint16
		flags co.KEYEVENTF
	}
	want := []pressedKey{
		{scan: shiftScan, flags: co.KEYEVENTF_SCANCODE},
		{scan: f10Scan, flags: co.KEYEVENTF_SCANCODE},
		{scan: f10Scan, flags: co.KEYEVENTF_SCANCODE | co.KEYEVENTF_KEYUP},
		{scan: shiftScan, flags: co.KEYEVENTF_SCANCODE | co.KEYEVENTF_KEYUP},
	}

	got := make([]pressedKey, 0, len(want))
	for _, batch := range batches {
		for _, injected := range batch {
			if injected.Type != co.INPUT_KEYBOARD {
				t.Fatalf("injected type = %v, want a keyboard input", injected.Type)
			}
			keyboard := injected.KeybdInput()
			got = append(got, pressedKey{scan: keyboard.WScan, flags: keyboard.DwFlags})
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("injected keys = %+v, want %+v", got, want)
	}

	// RestoreFocus false leaves the target in the foreground.
	current = previous
	noRestore := false
	if _, err := input.SendKeys(context.Background(), KeyRequest{
		Target:       WindowTarget{Title: title},
		Keys:         []string{"vk_f11"},
		RestoreFocus: &noRestore,
	}); err != nil {
		t.Fatalf("SendKeys(no restore) = %v", err)
	}
	if want := []win.HWND{window.handle, previous, window.handle}; !slices.Equal(focused, want) {
		t.Fatalf("focus calls = %+v, want the target window only after the first request", focused)
	}
}

// testWindowClassPrefix names the window class of the test windows.
const testWindowClassPrefix = "NahidaInputTest"

var testWindowSeq atomic.Uint32

// testWindow is a hidden top-level window created and pumped on its own locked
// thread. It stands in for a game or launcher window: the real window APIs and
// message delivery are exercised without putting anything on screen.
type testWindow struct {
	handle   win.HWND
	messages chan windowMessage
	closed   chan struct{}
}

type windowMessage struct {
	msg    co.WM
	wParam win.WPARAM
	lParam win.LPARAM
}

func newTestWindow(t *testing.T, title string) *testWindow {
	t.Helper()

	window := &testWindow{
		messages: make(chan windowMessage, 32),
		closed:   make(chan struct{}),
	}
	created := make(chan error, 1)
	go func() {
		// The window belongs to this thread: create, pump, and destroy it here.
		runtime.LockOSThread()
		defer close(window.closed)
		window.pump(created, title)
	}()

	select {
	case err := <-created:
		if err != nil {
			// Window creation needs an interactive window station, which some
			// build agents do not provide.
			t.Skipf("test window unavailable: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("test window creation timed out")
	}
	t.Cleanup(func() { window.close(t) })

	return window
}

// restrictTo points the service at this window only, standing in for the
// visibility filter that a hidden window would never pass.
func (w *testWindow) restrictTo(input *Input) {
	input.enumerate = func() []win.HWND { return []win.HWND{w.handle} }
	input.isVisible = func(win.HWND) bool { return true }
}

func (w *testWindow) next(t *testing.T) windowMessage {
	t.Helper()

	select {
	case message := <-w.messages:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("no key message reached the test window")
		return windowMessage{}
	}
}

func (w *testWindow) drain() []windowMessage {
	var messages []windowMessage
	for {
		select {
		case message := <-w.messages:
			messages = append(messages, message)
		default:
			return messages
		}
	}
}

func (w *testWindow) close(t *testing.T) {
	t.Helper()

	select {
	case <-w.closed:
		return
	default:
	}
	if err := w.handle.PostMessage(co.WM_CLOSE, 0, 0); err != nil {
		t.Errorf("close test window: %v", err)
		return
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Error("test window did not shut down")
	}
}

// pump registers the window class, creates the hidden window, and runs its
// message loop until the window is closed. It reports the creation result
// through created and returns once the loop has ended.
func (w *testWindow) pump(created chan<- error, title string) {
	instance, err := win.GetModuleHandle("")
	if err != nil {
		created <- err
		return
	}
	class, err := windows.UTF16FromString(fmt.Sprintf("%s%d", testWindowClassPrefix, testWindowSeq.Add(1)))
	if err != nil {
		created <- err
		return
	}

	callback := syscall.NewCallback(
		func(handle win.HWND, msg co.WM, wParam win.WPARAM, lParam win.LPARAM) uintptr {
			switch msg {
			case co.WM_KEYDOWN, co.WM_KEYUP, co.WM_CHAR:
				select {
				case w.messages <- windowMessage{msg: msg, wParam: wParam, lParam: lParam}:
				default:
				}
				return 0
			case co.WM_CLOSE:
				win.PostQuitMessage(0)
				return 0
			default:
				return handle.DefWindowProc(msg, wParam, lParam)
			}
		},
	)

	wcx := win.WNDCLASSEX{
		Style:         co.CS_HREDRAW | co.CS_VREDRAW,
		LpfnWndProc:   callback,
		HInstance:     instance,
		LpszClassName: &class[0],
	}
	wcx.SetCbSize()

	atom, err := win.RegisterClassEx(&wcx)
	if err != nil {
		created <- err
		return
	}

	handle, err := win.CreateWindowEx(
		0,
		win.ClassNameAtom(atom),
		title,
		co.WS_OVERLAPPEDWINDOW, // no WS_VISIBLE: the window stays off screen
		win.POINT{},
		win.SIZE{Cx: 320, Cy: 200},
		0,
		0,
		instance,
		0,
	)
	if err != nil {
		created <- err
		return
	}
	w.handle = handle
	created <- nil

	var msg win.MSG
	for {
		count, err := win.GetMessage(&msg, 0, 0, 0)
		if err != nil || count == 0 {
			break
		}
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
	_ = handle.DestroyWindow()
	_ = win.UnregisterClass(win.ClassNameAtom(atom), instance)
}
