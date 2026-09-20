//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"golang.org/x/sys/windows"
)

// focusWaitTimeout bounds how long SendKeys waits for the target window to
// reach the foreground before giving up without sending anything.
const (
	focusWaitTimeout  = time.Second
	focusPollInterval = 25 * time.Millisecond
)

var (
	procEnumWindows     = modUser32.NewProc("EnumWindows")
	enumWindowsProc     = syscall.NewCallback(collectEnumeratedWindow)
	enumeratedWindows   sync.Map // scan token -> *[]win.HWND
	enumeratedWindowSeq atomic.Uint64
)

// collectEnumeratedWindow appends to the collector named by lParam. The
// collector travels as a numeric token, so the callback performs no pointer
// arithmetic of its own.
func collectEnumeratedWindow(handle, lParam uintptr) uintptr {
	value, ok := enumeratedWindows.Load(uint64(lParam))
	if !ok {
		return 0
	}
	collector := value.(*[]win.HWND)
	*collector = append(*collector, win.HWND(handle))
	return 1
}

// enumerateTopLevel returns every top-level window in Z-order. It mirrors the
// token registry EnumWindows pattern in internal/xxmi, which stays valid under
// the race detector's pointer checks.
func enumerateTopLevel() []win.HWND {
	handles := make([]win.HWND, 0, 64)
	token := enumeratedWindowSeq.Add(1)
	enumeratedWindows.Store(token, &handles)
	defer enumeratedWindows.Delete(token)
	_, _, _ = procEnumWindows.Call(enumWindowsProc, uintptr(token))

	return handles
}

// windowScanner reads the top-level windows the platform services address.
// Input and Screen share it, so a target resolves the same way for keys and for
// captures.
type windowScanner struct {
	enumerate  func() []win.HWND
	isVisible  func(win.HWND) bool
	foreground func() win.HWND
	processOf  func(uint32) string
}

func defaultWindowScanner() windowScanner {
	return windowScanner{
		enumerate:  enumerateTopLevel,
		isVisible:  isWindowVisible,
		foreground: win.GetForegroundWindow,
		processOf:  processName,
	}
}

// records returns every visible, non-tool top-level window in Z-order.
func (s windowScanner) records() []windowRecord {
	foreground := s.foreground()
	names := make(map[uint32]string, 16)
	records := make([]windowRecord, 0, 64)
	for _, handle := range s.enumerate() {
		if !handle.IsWindow() || !s.isVisible(handle) {
			continue
		}
		if exStyle, err := handle.ExStyle(); err == nil && exStyle&co.WS_EX_TOOLWINDOW != 0 {
			continue
		}
		_, pid, err := handle.GetWindowThreadProcessId()
		if err != nil {
			continue
		}

		name, known := names[pid]
		if !known {
			name = s.processOf(pid)
			names[pid] = name
		}
		title, _ := handle.GetWindowText()
		className, _ := handle.GetClassName()
		record := windowRecord{
			handle: handle, title: title, className: className, pid: pid, processName: name,
			isForeground: handle == foreground, isMinimized: handle.IsIconic(),
		}
		if rect, err := handle.GetWindowRect(); err == nil {
			record.area = int64(rect.Right-rect.Left) * int64(rect.Bottom-rect.Top)
		}
		records = append(records, record)
	}
	return records
}

// windowRecord is one enumerated top-level window. selectWindow ranks records
// without touching the Win32 API, which keeps the choice testable.
type windowRecord struct {
	handle       win.HWND
	title        string
	className    string
	pid          uint32
	processName  string
	isForeground bool
	isMinimized  bool
	area         int64
}

func (r windowRecord) info() WindowInfo {
	return WindowInfo{
		Handle:       fmt.Sprintf("0x%X", uintptr(r.handle)),
		Title:        r.title,
		ClassName:    r.className,
		PID:          r.pid,
		ProcessName:  r.processName,
		IsForeground: r.isForeground,
		IsMinimized:  r.isMinimized,
	}
}

func (r windowRecord) describe() string {
	parts := []string{strconv.Quote(r.title)}
	if r.processName != "" {
		parts = append(parts, r.processName)
	}
	return strings.Join(append(parts, fmt.Sprintf("pid %d", r.pid), fmt.Sprintf("0x%X", uintptr(r.handle))), ", ")
}

// Input sends keyboard keys to another window. Window targets are resolved per
// request, so a caller may pass a window title, a process name, a PID, or any
// combination of them.
type Input struct {
	mu         sync.Mutex
	diagnostic func(error, string, map[string]any)

	enumerate     func() []win.HWND
	isVisible     func(win.HWND) bool
	foreground    func() win.HWND
	focus         func(win.HWND)
	processOf     func(uint32) string
	postMessage   func(win.HWND, co.WM, win.WPARAM, win.LPARAM) error
	sendInput     func([]win.INPUT) (int, error)
	mapVirtualKey func(co.VK) uint32

	focusTimeout time.Duration
	focusPoll    time.Duration
}

func NewInput() *Input {
	scanner := defaultWindowScanner()
	return &Input{
		enumerate:  scanner.enumerate,
		isVisible:  scanner.isVisible,
		foreground: scanner.foreground,
		focus:      func(handle win.HWND) { ForceForegroundWindow(uintptr(handle)) },
		processOf:  scanner.processOf,
		postMessage: func(handle win.HWND, message co.WM, wParam win.WPARAM, lParam win.LPARAM) error {
			return handle.PostMessage(message, wParam, lParam)
		},
		sendInput:     win.SendInput,
		mapVirtualKey: defaultMapVirtualKey,
		focusTimeout:  focusWaitTimeout,
		focusPoll:     focusPollInterval,
	}
}

//wails:ignore
func (i *Input) UseDiagnostic(report func(error, string, map[string]any)) {
	i.diagnostic = report
}

// ListWindows returns the visible top-level windows that match filter, in
// Z-order. Title and Process match case insensitive substrings here, because
// the caller is looking for a window rather than addressing an exact one.
func (i *Input) ListWindows(ctx context.Context, filter WindowFilter) ([]WindowInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	title := foldWindowText(filter.Title)
	process := foldWindowText(filter.Process)
	matched := make([]WindowInfo, 0, defaultWindowLimit)
	for _, record := range i.enumerateRecords() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if filter.PID != 0 && record.pid != filter.PID {
			continue
		}
		if !matchesProcessKey(foldWindowText(record.processName), process) {
			continue
		}
		if title != "" && !foldWindowText(record.title).contains(title) {
			continue
		}
		matched = append(matched, record.info())
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultWindowLimit
	}
	if limit > maxWindowLimit {
		limit = maxWindowLimit
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

// ResolveWindow returns the window that a request would address. It reports
// WINDOW_NOT_FOUND instead of guessing when nothing matches.
func (i *Input) ResolveWindow(ctx context.Context, target WindowTarget) (WindowInfo, error) {
	if err := ctx.Err(); err != nil {
		return WindowInfo{}, err
	}
	record, err := selectWindow(i.enumerateRecords(), target)
	if err != nil {
		return WindowInfo{}, err
	}
	return record.info(), nil
}

// SendKeys sends every key of the request to the resolved target window. With
// KeyDeliveryMessage the window stays in the background; with
// KeyDeliveryForeground it is focused first and the keys are injected with
// SendInput, which is what games and other raw-input targets need.
func (i *Input) SendKeys(ctx context.Context, request KeyRequest) (KeyResult, error) {
	keys, options, err := resolveKeyRequest(request)
	if err != nil {
		return KeyResult{}, err
	}
	chords := make([]keyChord, 0, len(keys))
	for _, key := range keys {
		chord, err := parseKeyChord(key)
		if err != nil {
			i.report(err, "parse", map[string]any{"target": request.Target.describe(), "keys": keys})
			return KeyResult{}, err
		}
		chords = append(chords, chord)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return KeyResult{}, err
	}
	record, err := selectWindow(i.enumerateRecords(), request.Target)
	if err != nil {
		fields := request.Target.resolveDiagnosticFields()
		fields["keys"] = keys
		i.report(err, "resolve", fields)
		return KeyResult{}, err
	}

	fields := map[string]any{
		"target":   request.Target.describe(),
		"delivery": string(options.delivery),
		"keys":     keys,
		"window":   record.describe(),
	}
	if options.delivery == KeyDeliveryMessage {
		err = i.sendMessageKeys(ctx, record, chords, options)
	} else {
		err = i.sendForegroundKeys(ctx, record, chords, options)
	}
	if err != nil {
		i.report(err, string(options.delivery), fields)
		return KeyResult{}, err
	}
	return KeyResult{Window: record.info(), Delivery: options.delivery, Keys: keys}, nil
}

func (i *Input) report(err error, stage string, fields map[string]any) {
	if err == nil || i.diagnostic == nil {
		return
	}
	i.diagnostic(err, stage, fields)
}

func (i *Input) enumerateRecords() []windowRecord {
	return windowScanner{
		enumerate:  i.enumerate,
		isVisible:  i.isVisible,
		foreground: i.foreground,
		processOf:  i.processOf,
	}.records()
}

// windowCandidate is one record with its folded title, so the exact, prefix,
// and substring narrowing passes compare keys instead of refolding the same
// title for every matcher.
type windowCandidate struct {
	record windowRecord
	title  windowKey
}

// selectWindow picks the single best record for target: every set field must
// match, titles are compared exact first and loosened only when nothing
// matched, and remaining ties prefer the foreground window, a titled window,
// and the largest window, in that order.
func selectWindow(records []windowRecord, target WindowTarget) (*windowRecord, error) {
	if target.empty() {
		return nil, ErrInputTargetRequired
	}

	process := foldWindowText(target.Process)
	candidates := make([]windowCandidate, 0, len(records))
	for _, record := range records {
		if target.PID != 0 && record.pid != target.PID {
			continue
		}
		if !matchesProcessKey(foldWindowText(record.processName), process) {
			continue
		}
		candidates = append(candidates, windowCandidate{record: record, title: foldWindowText(record.title)})
	}
	if len(candidates) == 0 {
		return nil, windowNotFound(target)
	}
	if title := foldWindowText(target.Title); title != "" {
		matched := false
		for _, match := range []func(windowKey, windowKey) bool{
			windowKey.equal, windowKey.hasPrefix, windowKey.contains,
		} {
			narrowed := make([]windowCandidate, 0, len(candidates))
			for _, candidate := range candidates {
				if match(candidate.title, title) {
					narrowed = append(narrowed, candidate)
				}
			}
			if len(narrowed) > 0 {
				candidates, matched = narrowed, true
				break
			}
		}
		if !matched {
			return nil, windowNotFound(target)
		}
	}

	slices.SortStableFunc(candidates, func(left, right windowCandidate) int {
		return compareWindowRecords(left.record, right.record)
	})
	return &candidates[0].record, nil
}

// matchesProcessKey compares an executable name. It matches exactly or as a
// substring so "StarRail.exe" and "starrail" both select the game window.
func matchesProcessKey(process, wanted windowKey) bool {
	if wanted == "" {
		return true
	}
	if process == "" {
		return false
	}
	return process.equal(wanted) || process.contains(wanted)
}

func compareWindowRecords(left, right windowRecord) int {
	if left.isForeground != right.isForeground {
		if left.isForeground {
			return -1
		}
		return 1
	}
	if titled, otherTitled := left.title != "", right.title != ""; titled != otherTitled {
		if titled {
			return -1
		}
		return 1
	}
	switch {
	case left.area > right.area:
		return -1
	case left.area < right.area:
		return 1
	default:
		return 0
	}
}

func windowNotFound(target WindowTarget) error {
	return fmt.Errorf("%w: no visible window matches %s", ErrWindowNotFound, target.describe())
}

// windowKey is window text folded for matching: every Unicode whitespace
// sequence collapses to one ASCII space and the result is lowercased, so a
// title that carries NBSP or another invisible space still matches a regular
// space typed by the caller. Each record and target is folded once, then the
// matchers compare keys, so no value is refolded per comparison.
type windowKey string

func foldWindowText(value string) windowKey {
	return windowKey(strings.ToLower(normalizeWindowText(value)))
}

func (k windowKey) equal(wanted windowKey) bool {
	return k == wanted
}

func (k windowKey) hasPrefix(wanted windowKey) bool {
	return strings.HasPrefix(string(k), string(wanted))
}

func (k windowKey) contains(wanted windowKey) bool {
	return strings.Contains(string(k), string(wanted))
}

func (i *Input) sendMessageKeys(
	ctx context.Context,
	record *windowRecord,
	chords []keyChord,
	options keyOptions,
) error {
	for index, chord := range chords {
		if index > 0 {
			if err := waitForInput(ctx, options.interval); err != nil {
				return err
			}
		}
		if err := i.sendMessageChord(ctx, record, chord, options.hold); err != nil {
			return err
		}
	}
	return nil
}

// sendMessageChord posts the chord and always releases what it pressed, so a
// modifier never stays logically down when a later step fails.
func (i *Input) sendMessageChord(
	ctx context.Context,
	record *windowRecord,
	chord keyChord,
	hold time.Duration,
) error {
	pressed := make([]co.VK, 0, len(chord.modifiers)+1)
	release := func(cause error) error {
		var releaseErr error
		for index := len(pressed) - 1; index >= 0; index-- {
			if err := i.postKey(record.handle, pressed[index], true); err != nil && releaseErr == nil {
				releaseErr = err
			}
		}
		return errors.Join(cause, releaseErr)
	}

	for _, modifier := range chord.modifiers {
		if err := i.postKey(record.handle, modifier, false); err != nil {
			return release(err)
		}
		pressed = append(pressed, modifier)
	}
	if err := i.postKey(record.handle, chord.key, false); err != nil {
		return release(err)
	}
	pressed = append(pressed, chord.key)

	if err := waitForInput(ctx, hold); err != nil {
		return release(err)
	}
	return release(nil)
}

func (i *Input) postKey(handle win.HWND, vk co.VK, keyUp bool) error {
	scan, extended := keyScan(vk, i.mapVirtualKey(vk))
	message := co.WM_KEYDOWN
	if keyUp {
		message = co.WM_KEYUP
	}
	if err := i.postMessage(handle, message, win.WPARAM(vk), win.LPARAM(keyLParam(scan, extended, keyUp))); err != nil {
		return fmt.Errorf(
			"%w: post WM 0x%04X to window 0x%X: %w",
			ErrSendInputBlocked,
			uint16(message),
			uintptr(handle),
			err,
		)
	}
	return nil
}

func (i *Input) sendForegroundKeys(
	ctx context.Context,
	record *windowRecord,
	chords []keyChord,
	options keyOptions,
) error {
	previous := i.foreground()
	refocus := !windowInProcess(record, previous)
	if refocus {
		i.focus(record.handle)
		if err := i.waitForForeground(ctx, record); err != nil {
			return err
		}
	}

	sendErr := i.sendInjectedKeys(ctx, record, chords, options)
	if refocus && options.restoreFocus && previous.IsWindow() {
		i.focus(previous)
	}
	return sendErr
}

func (i *Input) waitForForeground(ctx context.Context, record *windowRecord) error {
	deadline := time.Now().Add(i.focusTimeout)
	for {
		if windowInProcess(record, i.foreground()) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%w: %s did not reach the foreground", ErrWindowNotForeground, record.describe())
		}
		if err := waitForInput(ctx, i.focusPoll); err != nil {
			return err
		}
	}
}

// windowInProcess reports whether handle is the target window or another window
// of its process, which covers launchers that forward input to a child window.
func windowInProcess(record *windowRecord, handle win.HWND) bool {
	if handle == 0 {
		return false
	}
	if handle == record.handle {
		return true
	}
	_, pid, err := handle.GetWindowThreadProcessId()
	return err == nil && pid == record.pid
}

func (i *Input) sendInjectedKeys(
	ctx context.Context,
	record *windowRecord,
	chords []keyChord,
	options keyOptions,
) error {
	for index, chord := range chords {
		if err := ctx.Err(); err != nil {
			return err
		}
		if index > 0 {
			if err := waitForInput(ctx, options.interval); err != nil {
				return err
			}
		}
		if err := i.sendInjectedChord(ctx, chord, options.hold); err != nil {
			return err
		}
	}
	return nil
}

func (i *Input) sendInjectedChord(ctx context.Context, chord keyChord, hold time.Duration) error {
	downs := make([]win.INPUT, 0, len(chord.modifiers)+1)
	for _, modifier := range chord.modifiers {
		downs = append(downs, i.keyInput(modifier, false))
	}
	downs = append(downs, i.keyInput(chord.key, false))
	if err := i.injectBatch(downs); err != nil {
		return errors.Join(err, i.releaseChord(chord))
	}

	if err := waitForInput(ctx, hold); err != nil {
		return errors.Join(err, i.releaseChord(chord))
	}
	return i.releaseChord(chord)
}

func (i *Input) releaseChord(chord keyChord) error {
	ups := make([]win.INPUT, 0, len(chord.modifiers)+1)
	ups = append(ups, i.keyInput(chord.key, true))
	for index := len(chord.modifiers) - 1; index >= 0; index-- {
		ups = append(ups, i.keyInput(chord.modifiers[index], true))
	}
	return i.injectBatch(ups)
}

func (i *Input) keyInput(vk co.VK, keyUp bool) win.INPUT {
	scan, extended := keyScan(vk, i.mapVirtualKey(vk))
	flags := co.KEYEVENTF_SCANCODE
	if extended {
		flags |= co.KEYEVENTF_EXTENDEDKEY
	}
	if keyUp {
		flags |= co.KEYEVENTF_KEYUP
	}

	// The virtual key stays empty because a scan code is injected: games that
	// read raw input or DirectInput ignore virtual-key events.
	var input win.INPUT
	input.Type = co.INPUT_KEYBOARD
	input.SetKeybdInput(win.KEYBDINPUT{WScan: scan, DwFlags: flags})
	return input
}

func (i *Input) injectBatch(inputs []win.INPUT) error {
	if len(inputs) == 0 {
		return nil
	}
	sent, err := i.sendInput(inputs)
	if err != nil {
		return fmt.Errorf("%w: SendInput: %w", ErrSendInputBlocked, err)
	}
	if sent != len(inputs) {
		return fmt.Errorf("%w: SendInput injected %d of %d events", ErrSendInputBlocked, sent, len(inputs))
	}
	return nil
}

func waitForInput(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func defaultMapVirtualKey(vk co.VK) uint32 {
	return win.MapVirtualKey(vk, co.MAPVK_VK_TO_VSC)
}

func isWindowVisible(handle win.HWND) bool {
	return windows.IsWindowVisible(windows.HWND(handle))
}
