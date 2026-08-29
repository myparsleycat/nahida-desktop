package watcher

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	filewatcher "github.com/larsartmann/go-filewatcher/v2"
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

// Filter decides whether an event should be delivered. Directory creation is
// processed for recursive registration before this filter runs.
type Filter func(Event) bool

// ErrorHandler receives runtime watcher and recursive-registration errors.
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
	backend  *filewatcher.Watcher
	cancel   context.CancelFunc
	done     chan struct{}
	dispatch *dispatcher
	roots    []root
	depth    int
	ops      Op
	filter   Filter
	onError  ErrorHandler

	once     sync.Once
	closeErr error
}

type root struct {
	path string
}

// WatchTree watches all roots with a shared invalidation debounce. Each call
// creates a separate watcher, so unrelated features cannot cancel one
// another's pending callback.
func WatchTree(roots []string, conf TreeConfig, handler func(Event)) (*Watcher, error) {
	if conf.Depth < -1 {
		return nil, fmt.Errorf("watcher depth must be -1 or greater: %d", conf.Depth)
	}

	return start(roots, conf.Depth, defaultOps(conf.Ops), conf.Debounce, conf.Filter, conf.OnError, handler)
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

	roots := make([]root, 0, len(paths))
	backendPaths := make([]string, 0, len(paths))
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
		roots = append(roots, root{path: abs})
		backendPaths = append(backendPaths, abs)
	}

	backend, err := filewatcher.New(
		backendPaths,
		filewatcher.WithRecursive(false),
		filewatcher.WithGitignore(false),
		filewatcher.WithSkipDotDirs(false),
		filewatcher.WithBuffer(256),
		filewatcher.WithErrorHandler(func(_ filewatcher.ErrorContext, err error) {
			if onError != nil {
				onError(err)
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	for _, item := range roots {
		if err := addTree(backend, item.path, depth, false); err != nil {
			_ = backend.Close()
			return nil, err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	events, err := backend.Watch(ctx)
	if err != nil {
		cancel()
		_ = backend.Close()
		return nil, fmt.Errorf("start watcher: %w", err)
	}

	w := &Watcher{
		backend:  backend,
		cancel:   cancel,
		done:     make(chan struct{}),
		dispatch: newDispatcher(debounce, handler),
		roots:    roots,
		depth:    depth,
		ops:      ops,
		filter:   filter,
		onError:  onError,
	}
	go w.run(events)

	return w, nil
}

func (w *Watcher) run(events <-chan filewatcher.Event) {
	defer close(w.done)
	for event := range events {
		ev, ok := convertEvent(event)
		if !ok {
			continue
		}
		if ev.Op == Create {
			w.addCreatedDirectory(ev.Path)
		}
		if w.ops&ev.Op == 0 || w.filter != nil && !w.filter(ev) {
			continue
		}
		w.dispatch.Dispatch(ev)
	}
}

func (w *Watcher) addCreatedDirectory(path string) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}

	remaining, ok := w.remainingDepth(path)
	if !ok {
		return
	}
	if err := addTree(w.backend, path, remaining, true); err != nil && w.onError != nil {
		w.onError(err)
	}
}

func (w *Watcher) remainingDepth(path string) (int, bool) {
	best := -2
	for _, item := range w.roots {
		level, ok := childDepth(item.path, path)
		if !ok || level == 0 {
			continue
		}
		if w.depth == -1 {
			return -1, true
		}
		if level <= w.depth {
			remaining := w.depth - level
			if remaining > best {
				best = remaining
			}
		}
	}
	return best, best >= 0
}

// Close stops the backend, discards pending debounce callbacks, and waits for
// any handler already in progress. It is safe to call more than once.
func (w *Watcher) Close() error {
	if w == nil {
		return nil
	}
	w.once.Do(func() {
		w.cancel()
		w.closeErr = w.backend.Close()
		<-w.done
		w.dispatch.Close()
	})
	return w.closeErr
}

func addTree(backend *filewatcher.Watcher, rootPath string, depth int, includeRoot bool) error {
	return filepath.WalkDir(rootPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk watcher directory %q: %w", path, walkErr)
		}
		if !entry.IsDir() {
			return nil
		}

		level, ok := childDepth(rootPath, path)
		if !ok {
			return nil
		}
		if depth >= 0 && level > depth {
			return filepath.SkipDir
		}
		if level == 0 && !includeRoot {
			return nil
		}
		if err := backend.Add(path); err != nil {
			return fmt.Errorf("add watcher directory %q: %w", path, err)
		}
		return nil
	})
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

func convertEvent(event filewatcher.Event) (Event, bool) {
	var op Op
	switch event.Op {
	case filewatcher.Create:
		op = Create
	case filewatcher.Write:
		op = Write
	case filewatcher.Remove:
		op = Remove
	case filewatcher.Rename:
		op = Rename
	default:
		return Event{}, false
	}
	return Event{Path: filepath.Clean(event.Path), Op: op, Hash: event.Hash}, true
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
