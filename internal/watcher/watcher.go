//go:build windows

package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Op identifies a filesystem operation. Multiple operations may be combined
// when configuring a watcher; each delivered Event contains one operation.
type Op uint8

const (
	Create Op = 1 << iota
	Write
	Remove
	Rename

	All = Create | Write | Remove | Rename
)

// Event describes a filesystem change after path normalization and settling.
type Event struct {
	Path string
	Op   Op
	Hash string
}

// Filter decides whether an event should be delivered after depth and
// operation filtering.
type Filter func(Event) bool

// ErrorHandler receives runtime ReadDirectoryChangesW errors.
type ErrorHandler func(error)

// TreeConfig configures directory watching. Depth 0 watches only each root,
// positive values include that many directory levels, and -1 is unlimited.
type TreeConfig struct {
	Depth    int
	Ops      Op
	Debounce time.Duration
	Filter   Filter
	OnError  ErrorHandler
}

// FileConfig configures an exact-file watch. The parent directory is watched
// non-recursively so atomic replacement continues to be observed.
type FileConfig struct {
	Ops             Op
	SettleDelay     time.Duration
	DistinctContent bool
	OnError         ErrorHandler
}

// Watcher owns one independent backend watcher and its debounce state.
type Watcher struct {
	events   chan Event
	stop     chan struct{}
	done     chan struct{}
	ioDone   chan struct{}
	dispatch *dispatcher
	roots    []*root
	port     windows.Handle
	depth    int
	ops      Op
	filter   Filter
	onError  ErrorHandler
	overflow string

	once     sync.Once
	closeErr error
}

type root struct {
	path       string
	handle     windows.Handle
	recursive  bool
	buffer     []byte
	mu         sync.Mutex
	overlapped windows.Overlapped
	pending    bool
}

// WatchTree watches all roots with a shared invalidation debounce. Each call
// creates a separate watcher, so unrelated features cannot cancel one
// another's pending callback.
func WatchTree(roots []string, conf TreeConfig, handler func(Event)) (*Watcher, error) {
	if conf.Depth < -1 {
		return nil, fmt.Errorf("watcher depth must be -1 or greater: %d", conf.Depth)
	}

	return start(roots, conf.Depth, defaultOps(conf.Ops), conf.Debounce, conf.Filter, conf.OnError, "", handler)
}

// WatchFile watches one exact file by watching its parent directory. When
// DistinctContent is enabled, the initial SHA-256 is seeded and callbacks are
// only delivered after the settled content actually changes.
func WatchFile(path string, conf FileConfig, handler func(Event)) (*Watcher, error) {
	target, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve watched file %q: %w", path, err)
	}
	target = filepath.Clean(target)

	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("stat watched file %q: %w", target, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("watched path is not a regular file: %q", target)
	}

	var gate *contentGate
	if conf.DistinctContent {
		gate = newContentGate(target, conf.OnError)
	}

	filter := func(ev Event) bool { return SamePath(ev.Path, target) }
	deliver := func(ev Event) {
		if gate != nil && !gate.prepare(&ev) {
			return
		}
		handler(ev)
	}

	return start(
		[]string{filepath.Dir(target)},
		0,
		defaultFileOps(conf.Ops),
		conf.SettleDelay,
		filter,
		conf.OnError,
		target,
		deliver,
	)
}

func start(
	paths []string,
	depth int,
	ops Op,
	debounce time.Duration,
	filter Filter,
	onError ErrorHandler,
	overflow string,
	handler func(Event),
) (*Watcher, error) {
	if len(paths) == 0 {
		return nil, errors.New("watcher requires at least one root")
	}
	if handler == nil {
		return nil, errors.New("watcher handler is nil")
	}
	if debounce < 0 {
		return nil, fmt.Errorf("watcher debounce must not be negative: %s", debounce)
	}

	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve watcher root %q: %w", path, err)
		}
		abs = filepath.Clean(abs)
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("stat watcher root %q: %w", abs, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("watcher root is not a directory: %q", abs)
		}
		resolved = append(resolved, abs)
	}

	roots := make([]*root, 0, len(resolved))
	for _, path := range resolved {
		watched, err := openRoot(path, depth != 0)
		if err != nil {
			return nil, errors.Join(err, closeRoots(roots))
		}
		roots = append(roots, watched)
	}
	port, err := windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 0)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("create watcher completion port: %w", err), closeRoots(roots))
	}
	for index, item := range roots {
		if _, err := windows.CreateIoCompletionPort(item.handle, port, uintptr(index+1), 0); err != nil {
			return nil, errors.Join(
				fmt.Errorf("associate watcher root %q with completion port: %w", item.path, err),
				windows.CloseHandle(port),
				closeRoots(roots),
			)
		}
	}

	w := &Watcher{
		events:   make(chan Event, 256),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		ioDone:   make(chan struct{}),
		dispatch: newDispatcher(debounce, handler),
		roots:    roots,
		port:     port,
		depth:    depth,
		ops:      ops,
		filter:   filter,
		onError:  onError,
		overflow: overflow,
	}
	go w.run()
	go w.readCompletions()
	for _, item := range w.roots {
		if err := w.beginRead(item); err != nil {
			return nil, errors.Join(err, w.Close())
		}
	}

	return w, nil
}

func (w *Watcher) run() {
	defer close(w.done)
	for {
		select {
		case <-w.stop:
			return
		case ev := <-w.events:
			if !w.includes(ev.Path) || w.ops&ev.Op == 0 || w.filter != nil && !w.filter(ev) {
				continue
			}
			w.dispatch.Dispatch(ev)
		}
	}
}

func openRoot(path string, recursive bool) (*root, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("encode watcher root %q: %w", path, err)
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open watcher root %q: %w", path, err)
	}
	return &root{
		path: path, handle: handle, recursive: recursive, buffer: make([]byte, 64*1024),
	}, nil
}

func closeRoots(roots []*root) error {
	var result error
	for _, item := range roots {
		result = errors.Join(result, windows.CloseHandle(item.handle))
	}
	return result
}

func (w *Watcher) readCompletions() {
	defer close(w.ioDone)
	for {
		var (
			length     uint32
			key        uintptr
			overlapped *windows.Overlapped
		)
		err := windows.GetQueuedCompletionStatus(w.port, &length, &key, &overlapped, windows.INFINITE)
		if key == 0 && overlapped == nil {
			if w.stopping() && !w.hasPendingReads() {
				return
			}
			if err != nil {
				w.reportError(fmt.Errorf("wait for watcher completion: %w", err))
			}
			continue
		}
		if key == 0 || key > uintptr(len(w.roots)) {
			w.reportError(fmt.Errorf("watcher completion returned invalid root key: %d", key))
			continue
		}
		item := w.roots[key-1]
		item.finishRead(overlapped)
		if w.stopping() {
			if !w.hasPendingReads() {
				return
			}
			continue
		}
		if isOverflow(err) {
			w.emitOverflow(item.path)
		} else if err != nil {
			w.reportReadError(item.path, err)
			continue
		} else if length == 0 {
			w.emitOverflow(item.path)
		} else {
			w.decode(item.path, item.buffer[:length])
		}
		if err := w.beginRead(item); err != nil {
			w.reportError(err)
		}
	}
}

func (w *Watcher) beginRead(item *root) error {
	const changes = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
		windows.FILE_NOTIFY_CHANGE_DIR_NAME |
		windows.FILE_NOTIFY_CHANGE_ATTRIBUTES |
		windows.FILE_NOTIFY_CHANGE_SIZE |
		windows.FILE_NOTIFY_CHANGE_LAST_WRITE |
		windows.FILE_NOTIFY_CHANGE_CREATION
	item.mu.Lock()
	defer item.mu.Unlock()
	if w.stopping() {
		return nil
	}
	item.overlapped = windows.Overlapped{}
	item.pending = true
	err := windows.ReadDirectoryChanges(
		item.handle,
		unsafe.SliceData(item.buffer),
		uint32(len(item.buffer)),
		item.recursive,
		changes,
		nil,
		&item.overlapped,
		0,
	)
	if err != nil {
		item.pending = false
		return wrapReadError(item.path, err)
	}
	return nil
}

func (r *root) finishRead(overlapped *windows.Overlapped) {
	r.mu.Lock()
	if &r.overlapped == overlapped {
		r.pending = false
	}
	r.mu.Unlock()
}

func (w *Watcher) hasPendingReads() bool {
	for _, item := range w.roots {
		item.mu.Lock()
		pending := item.pending
		item.mu.Unlock()
		if pending {
			return true
		}
	}
	return false
}

func (w *Watcher) decode(rootPath string, buffer []byte) {
	const nameOffset = uint32(unsafe.Offsetof(windows.FileNotifyInformation{}.FileName))
	for offset := uint32(0); offset < uint32(len(buffer)); {
		if offset+nameOffset > uint32(len(buffer)) {
			return
		}
		info := (*windows.FileNotifyInformation)(unsafe.Pointer(&buffer[offset]))
		if info.FileNameLength%2 != 0 || offset+nameOffset+info.FileNameLength > uint32(len(buffer)) {
			return
		}
		nameLength := int(info.FileNameLength / 2)
		name := windows.UTF16ToString(unsafe.Slice(&info.FileName, nameLength))
		if op, ok := actionOp(info.Action); ok {
			w.emit(Event{Path: filepath.Join(rootPath, name), Op: op})
		}
		if info.NextEntryOffset == 0 {
			return
		}
		next := offset + info.NextEntryOffset
		if next <= offset || next >= uint32(len(buffer)) {
			return
		}
		offset = next
	}
}

func isOverflow(err error) bool {
	return errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) || errors.Is(err, windows.ERROR_MORE_DATA)
}

func (w *Watcher) emitOverflow(rootPath string) {
	if w.overflow != "" {
		w.emit(Event{Path: w.overflow, Op: Write})
		return
	}
	w.emit(Event{Path: rootPath, Op: Write})
}

func actionOp(action uint32) (Op, bool) {
	switch action {
	case windows.FILE_ACTION_ADDED:
		return Create, true
	case windows.FILE_ACTION_REMOVED:
		return Remove, true
	case windows.FILE_ACTION_MODIFIED:
		return Write, true
	case windows.FILE_ACTION_RENAMED_OLD_NAME, windows.FILE_ACTION_RENAMED_NEW_NAME:
		return Rename, true
	default:
		return 0, false
	}
}

func (w *Watcher) emit(event Event) {
	select {
	case <-w.stop:
	case w.events <- event:
	}
}

func (w *Watcher) stopping() bool {
	select {
	case <-w.stop:
		return true
	default:
		return false
	}
}

func (w *Watcher) reportReadError(path string, err error) {
	if w.stopping() && (errors.Is(err, windows.ERROR_OPERATION_ABORTED) || errors.Is(err, windows.ERROR_INVALID_HANDLE)) {
		return
	}
	w.reportError(wrapReadError(path, err))
}

func wrapReadError(path string, err error) error {
	return fmt.Errorf("watch directory tree %q: %w", path, err)
}

func (w *Watcher) reportError(err error) {
	if !w.stopping() && w.onError != nil {
		w.onError(err)
	}
}

func (w *Watcher) includes(path string) bool {
	if w.depth == -1 {
		return true
	}
	for _, item := range w.roots {
		level, ok := childDepth(item.path, path)
		if ok && level <= w.depth+1 {
			return true
		}
	}
	return false
}

// Close stops the backend, discards pending debounce callbacks, and waits for
// any handler already in progress. It is safe to call more than once.
func (w *Watcher) Close() error {
	if w == nil {
		return nil
	}
	w.once.Do(func() {
		close(w.stop)
		for _, item := range w.roots {
			item.mu.Lock()
			var overlapped *windows.Overlapped
			if item.pending {
				overlapped = &item.overlapped
			}
			if err := windows.CancelIoEx(item.handle, overlapped); err != nil {
				if errors.Is(err, windows.ERROR_NOT_FOUND) {
					item.pending = false
				} else {
					w.closeErr = errors.Join(w.closeErr, fmt.Errorf("cancel watcher root %q: %w", item.path, err))
				}
			}
			item.mu.Unlock()
		}
		if err := windows.PostQueuedCompletionStatus(w.port, 0, 0, nil); err != nil {
			w.closeErr = errors.Join(w.closeErr, fmt.Errorf("wake watcher completion port: %w", err))
		}
		<-w.ioDone
		w.closeErr = errors.Join(w.closeErr, closeRoots(w.roots), windows.CloseHandle(w.port))
		<-w.done
		w.dispatch.Close()
	})
	return w.closeErr
}

func childDepth(parent, child string) (int, bool) {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return 0, false
	}
	relative = filepath.Clean(relative)
	if relative == "." {
		return 0, true
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return 0, false
	}
	return len(strings.Split(relative, string(os.PathSeparator))), true
}

func defaultOps(ops Op) Op {
	if ops == 0 {
		return All
	}
	return ops
}

func defaultFileOps(ops Op) Op {
	if ops == 0 {
		return Create | Write
	}
	return ops
}

// SamePath compares cleaned absolute paths using Windows case semantics.
func SamePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil {
		left = leftAbs
	}
	if rightErr == nil {
		right = rightAbs
	}
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	return strings.EqualFold(left, right)
}

type contentGate struct {
	mu      sync.Mutex
	path    string
	last    string
	onError ErrorHandler
}

func newContentGate(path string, onError ErrorHandler) *contentGate {
	gate := &contentGate{path: path, onError: onError}
	if hash, err := hashFile(path); err == nil {
		gate.last = hash
	} else if onError != nil {
		onError(err)
	}
	return gate
}

func (g *contentGate) prepare(event *Event) bool {
	if event.Op != Create && event.Op != Write {
		return true
	}

	hash, err := hashFile(g.path)
	if err != nil {
		if g.onError != nil {
			g.onError(err)
		}
		return true
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == hash {
		return false
	}
	g.last = hash
	event.Hash = hash
	return true
}

func hashFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash watched file %q: %w", path, err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

type dispatcher struct {
	mu      sync.Mutex
	delay   time.Duration
	handler func(Event)
	timer   *time.Timer
	closed  bool
	wg      sync.WaitGroup
}

func newDispatcher(delay time.Duration, handler func(Event)) *dispatcher {
	return &dispatcher{delay: delay, handler: handler}
}

func (d *dispatcher) Dispatch(event Event) {
	if d.delay == 0 {
		d.mu.Lock()
		closed := d.closed
		d.mu.Unlock()
		if !closed {
			d.handler(event)
		}
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	if d.timer != nil && d.timer.Stop() {
		d.wg.Done()
	}
	d.wg.Add(1)
	var timer *time.Timer
	timer = time.AfterFunc(d.delay, func() {
		defer d.wg.Done()
		d.mu.Lock()
		if d.timer == timer {
			d.timer = nil
		}
		closed := d.closed
		d.mu.Unlock()
		if !closed {
			d.handler(event)
		}
	})
	d.timer = timer
}

func (d *dispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	if d.timer != nil && d.timer.Stop() {
		d.wg.Done()
	}
	d.timer = nil
	d.mu.Unlock()
	d.wg.Wait()
}
