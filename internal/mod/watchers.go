package mod

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/watcher"
)

const watcherSettleDelay = 800 * time.Millisecond

const (
	watcherReadyInterval = 200 * time.Millisecond
	watcherReadyAttempts = 10
)

type managedWatcher struct {
	watcher *watcher.Watcher

	mu        sync.Mutex
	timer     *time.Timer
	token     uint64
	closed    bool
	wg        sync.WaitGroup
	eventName string
	emit      func(string, ...any)
}

func (m *Mod) WatchGame(ctx context.Context, game string) error {
	path, err := m.GetGamePath(ctx, game)
	if err != nil {
		return err
	}
	if path == nil {
		return nil
	}
	watcher, err := newManagedWatcher(*path, 1, "mod:update-game", m.emitEvent)
	if err != nil {
		return err
	}
	return m.replaceWatcher(true, watcher)
}

func (m *Mod) WatchCharacter(ctx context.Context, characterPath string) error {
	if _, err := m.ownedPath(ctx, characterPath); err != nil {
		return err
	}
	watcher, err := newManagedWatcher(characterPath, 1, "mod:update-mods", m.emitEvent)
	if err != nil {
		return err
	}
	return m.replaceWatcher(false, watcher)
}

func (m *Mod) ServiceShutdown() error {
	if m == nil {
		return nil
	}
	m.watchMu.Lock()
	game, character := m.gameWatcher, m.characterWatcher
	m.gameWatcher, m.characterWatcher = nil, nil
	m.watchMu.Unlock()
	var compressionErr error
	if m.compression != nil {
		compressionErr = m.compression.stop()
	}
	return errors.Join(closeManagedWatcher(game), closeManagedWatcher(character), compressionErr)
}

func (m *Mod) replaceWatcher(game bool, next *managedWatcher) error {
	m.watchMu.Lock()
	var previous *managedWatcher
	if game {
		previous, m.gameWatcher = m.gameWatcher, next
	} else {
		previous, m.characterWatcher = m.characterWatcher, next
	}
	m.watchMu.Unlock()
	return closeManagedWatcher(previous)
}

func (m *Mod) emitEvent(name string, data ...any) {
	if m.emit != nil {
		m.emit(name, data...)
		return
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data...)
	}
}

func newManagedWatcher(
	root string,
	depth int,
	eventName string,
	emit func(string, ...any),
) (*managedWatcher, error) {
	root, err := validDirectory(root)
	if err != nil {
		return nil, err
	}
	managed := &managedWatcher{eventName: eventName, emit: emit}
	service, err := watcher.WatchTree([]string{root}, watcher.TreeConfig{
		Depth: depth,
		Ops:   watcher.All,
	}, func(event watcher.Event) {
		managed.schedule(event)
	})
	if err != nil {
		return nil, err
	}
	managed.watcher = service
	return managed, nil
}

func (m *managedWatcher) schedule(event watcher.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.token++
	token := m.token
	if m.timer != nil && m.timer.Stop() {
		m.wg.Done()
	}
	m.wg.Add(1)
	var timer *time.Timer
	timer = time.AfterFunc(watcherSettleDelay, func() {
		defer m.wg.Done()
		m.mu.Lock()
		if m.timer == timer {
			m.timer = nil
		}
		current := !m.closed && token == m.token
		m.mu.Unlock()
		if !current {
			return
		}

		waitForWatchedPath(
			event,
			func() bool { return m.isCurrent(token) },
			watcherReadyInterval,
			watcherReadyAttempts,
			getWatchedPathSnapshot,
			time.Sleep,
		)
		if m.isCurrent(token) {
			m.emit(m.eventName)
		}
	})
	m.timer = timer
}

func (m *managedWatcher) isCurrent(token uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && token == m.token
}

func (m *managedWatcher) close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.closed = true
	m.token++
	if m.timer != nil && m.timer.Stop() {
		m.wg.Done()
	}
	m.timer = nil
	m.mu.Unlock()

	err := m.watcher.Close()
	m.wg.Wait()
	return err
}

func closeManagedWatcher(managed *managedWatcher) error {
	return managed.close()
}

type watchedPathSnapshot struct {
	kind             fs.FileMode
	fileCount        int
	directoryCount   int
	totalSize        int64
	latestModifiedAt int64
}

func waitForWatchedPath(
	event watcher.Event,
	isCurrent func() bool,
	interval time.Duration,
	attempts int,
	snapshot func(string) (*watchedPathSnapshot, error),
	sleep func(time.Duration),
) {
	if event.Op == watcher.Remove {
		return
	}
	for range attempts {
		if !isCurrent() {
			return
		}
		current, err := snapshot(event.Path)
		if err != nil {
			if !isTransientWatcherError(err) {
				return
			}
			sleep(interval)
			continue
		}
		if current == nil {
			return
		}

		sleep(interval)
		if !isCurrent() {
			return
		}
		next, err := snapshot(event.Path)
		if err == nil && next != nil && *current == *next {
			return
		}
		if err != nil && !isTransientWatcherError(err) {
			return
		}
		sleep(interval)
	}
}

func getWatchedPathSnapshot(path string) (*watchedPathSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	snapshot := &watchedPathSnapshot{
		kind:             info.Mode().Type(),
		latestModifiedAt: info.ModTime().UnixNano(),
	}
	if info.Mode().IsRegular() {
		snapshot.fileCount = 1
		snapshot.totalSize = info.Size()
		return snapshot, nil
	}
	if !info.IsDir() {
		return nil, nil
	}

	snapshot.directoryCount = 1
	pending := []string{path}
	for len(pending) > 0 {
		index := len(pending) - 1
		current := pending[index]
		pending = pending[:index]
		entries, err := os.ReadDir(current)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			entryPath := filepath.Join(current, entry.Name())
			entryInfo, err := os.Stat(entryPath)
			if err != nil {
				return nil, err
			}
			if modifiedAt := entryInfo.ModTime().UnixNano(); modifiedAt > snapshot.latestModifiedAt {
				snapshot.latestModifiedAt = modifiedAt
			}
			if entryInfo.IsDir() {
				snapshot.directoryCount++
				pending = append(pending, entryPath)
				continue
			}
			if entryInfo.Mode().IsRegular() {
				snapshot.fileCount++
				snapshot.totalSize += entryInfo.Size()
			}
		}
	}
	return snapshot, nil
}

func isTransientWatcherError(err error) bool {
	return errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, fs.ErrPermission) ||
		errors.Is(err, syscall.EBUSY)
}
