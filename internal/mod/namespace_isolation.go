package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/watcher"
)

const namespaceIsolationEvent = "mod:namespace-isolation-state"

type NamespaceIsolationState struct {
	Revision  uint64                       `json:"revision"`
	Checking  bool                         `json:"checking"`
	Conflicts []NamespaceIsolationConflict `json:"conflicts"`
}

type NamespaceIsolationConflict struct {
	ID          string   `json:"id"`
	ImporterKey string   `json:"importerKey"`
	Namespace   string   `json:"namespace"`
	ModPaths    []string `json:"modPaths"`
	INIPaths    []string `json:"iniPaths"`
	Status      string   `json:"status"`
	Reason      string   `json:"reason"`
	Detail      string   `json:"detail"`
}

type NamespaceIsolationHooks struct {
	CheckStopped   func(context.Context, string) error
	WithMutation   func(context.Context, string, func() error) error
	SuspendPersist func(context.Context) (func() error, error)
}

type namespaceIsolationCoordinator struct {
	owner        *Mod
	lifecycleMu  sync.Mutex
	mu           sync.Mutex
	opMu         sync.Mutex
	reconcileMu  sync.Mutex
	watchEnabled bool
	paused       bool
	hooks        NamespaceIsolationHooks
	state        NamespaceIsolationState
	cancel       context.CancelFunc
	activeCancel context.CancelFunc
	done         chan struct{}
	notify       chan struct{}
	watcher      *watcher.Watcher
	watchRoots   []string
	self         map[string][]byte
	failureEpoch atomic.Uint64
	failures     map[string]namespaceIsolationFailure
	settle       time.Duration
	fallback     time.Duration
	poll         time.Duration
}

type namespaceIsolationFailure struct {
	hash     string
	epoch    uint64
	conflict NamespaceIsolationConflict
}

func newNamespaceIsolationCoordinator(m *Mod) *namespaceIsolationCoordinator {
	return &namespaceIsolationCoordinator{
		owner: m, state: NamespaceIsolationState{Conflicts: []NamespaceIsolationConflict{}},
		notify: make(chan struct{}, 1), self: map[string][]byte{}, failures: map[string]namespaceIsolationFailure{},
		settle: 800 * time.Millisecond, fallback: 60 * time.Second, poll: 2 * time.Second,
	}
}

//wails:ignore
func (m *Mod) UseNamespaceIsolationHooks(hooks NamespaceIsolationHooks) {
	c := m.namespaceIsolation
	c.mu.Lock()
	c.hooks = hooks
	c.mu.Unlock()
}

func (m *Mod) GetNamespaceIsolationState() NamespaceIsolationState {
	c := m.namespaceIsolation
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.state
	state.Conflicts = slices.Clone(state.Conflicts)
	for i := range state.Conflicts {
		state.Conflicts[i].ModPaths = slices.Clone(state.Conflicts[i].ModPaths)
		state.Conflicts[i].INIPaths = slices.Clone(state.Conflicts[i].INIPaths)
	}
	return state
}

func (m *Mod) RescanNamespaceIsolation(ctx context.Context, importerKey string) (NamespaceIsolationState, error) {
	m.namespaceIsolation.failureEpoch.Add(1)
	err := m.namespaceIsolation.reconcile(ctx, strings.TrimSpace(importerKey), false)
	if err != nil {
		m.namespaceIsolation.report(err, "rescan", importerKey)
	}
	return m.GetNamespaceIsolationState(), err
}

//wails:ignore
func (m *Mod) StartNamespaceIsolation(ctx context.Context) error {
	c := m.namespaceIsolation
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.cancel != nil {
		return nil
	}
	enabled, err := c.enabled(ctx)
	c.failureEpoch.Add(1)
	c.mu.Lock()
	c.paused = false
	c.watchEnabled = err == nil && enabled
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if !enabled {
		// Disabled isolation never walks the mod folders on its own: no startup
		// pass, watcher, or worker. Admission stays open for manual rescans.
		c.publish(false, []NamespaceIsolationConflict{})
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	// Startup completes recovery and reconciliation before persist watching starts.
	err = c.reconcile(runCtx, "", false)
	if err == nil {
		err = runCtx.Err()
	}
	if err != nil {
		cancel()
		// Admission stays open as before the first Start: launch preparation and
		// rescans must keep working while the background worker is down.
		c.mu.Lock()
		c.watchEnabled = false
		c.mu.Unlock()
		c.reconcileMu.Lock()
		if c.watcher != nil {
			err = errors.Join(err, c.watcher.Close())
			c.watcher, c.watchRoots = nil, nil
		}
		c.reconcileMu.Unlock()
		return err
	}
	c.cancel, c.done = cancel, make(chan struct{})
	go c.run(runCtx, c.done)
	return nil
}

//wails:ignore
func (m *Mod) StopNamespaceIsolation() error {
	if m == nil || m.namespaceIsolation == nil {
		return nil
	}
	c := m.namespaceIsolation
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.mu.Lock()
	c.watchEnabled = false
	c.paused = true
	if c.activeCancel != nil {
		c.activeCancel()
	}
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		<-c.done
		c.cancel, c.done = nil, nil
	}
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	if c.watcher != nil {
		err := c.watcher.Close()
		c.watcher, c.watchRoots = nil, nil
		return err
	}
	return nil
}

//wails:ignore
func (m *Mod) RefreshNamespaceIsolationImporters(ctx context.Context) {
	if ctx.Err() == nil {
		m.namespaceIsolation.enqueue()
	}
}

//wails:ignore
func (m *Mod) PrepareNamespaceIsolationLaunch(ctx context.Context, key string) error {
	// Disabled isolation must not read every INI of the importer on each launch.
	enabled, err := m.namespaceIsolation.enabled(ctx)
	if err != nil || !enabled {
		return err
	}
	m.namespaceIsolation.failureEpoch.Add(1)
	// The caller owns the importer gate; acquiring it here would deadlock.
	if err := m.namespaceIsolation.reconcile(ctx, key, true); err != nil {
		return err
	}
	for _, conflict := range m.GetNamespaceIsolationState().Conflicts {
		if strings.EqualFold(conflict.ImporterKey, key) && conflict.Reason == "unresolved_transaction" {
			return fmt.Errorf("NAMESPACE_ISOLATION_TRANSACTION_UNRESOLVED: %s", conflict.Detail)
		}
	}
	return nil
}

//wails:ignore
func (m *Mod) ReserveModFiles() func() { return m.beginModOperation() }

// beginModOperation is used only at outer filesystem boundaries. Helpers called
// inside these boundaries must not reacquire the RWMutex while a writer waits.
func (m *Mod) beginModOperation() func() {
	m.operationMu.RLock()
	return func() {
		m.operationMu.RUnlock()
		m.namespaceIsolation.enqueue()
	}
}

func (c *namespaceIsolationCoordinator) enqueue() {
	if c == nil {
		return
	}
	c.failureEpoch.Add(1)
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *namespaceIsolationCoordinator) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	full := time.NewTicker(c.fallback)
	defer full.Stop()
	waiting := time.NewTicker(c.poll)
	defer waiting.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.notify:
		case <-full.C:
		case <-waiting.C:
			if !c.waitingGameStopped(ctx) {
				continue
			}
		}
		// Stop cancels the active pass just before this loop's own context.
		err := c.reconcile(ctx, "", false)
		if err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			c.report(err, "reconcile", "")
		}
	}
}

// waitingGameStopped polls only the process guard. A reconcile reads every INI,
// so it runs once a waiting importer's game has exited rather than on each tick.
func (c *namespaceIsolationCoordinator) waitingGameStopped(ctx context.Context) bool {
	c.mu.Lock()
	checkStopped := c.hooks.CheckStopped
	keys := map[string]bool{}
	for _, conflict := range c.state.Conflicts {
		if conflict.Status == "waiting_for_game_exit" {
			keys[conflict.ImporterKey] = true
		}
	}
	c.mu.Unlock()

	if checkStopped == nil {
		return false
	}
	for key := range keys {
		if checkStopped(ctx, key) == nil {
			return true
		}
	}
	return false
}

func (c *namespaceIsolationCoordinator) publish(checking bool, conflicts []NamespaceIsolationConflict) {
	c.mu.Lock()
	old, _ := json.Marshal(c.state.Conflicts)
	next, _ := json.Marshal(conflicts)
	changed := string(old) != string(next)
	oldConflicts := map[string]string{}
	for _, conflict := range c.state.Conflicts {
		encoded, _ := json.Marshal(conflict)
		oldConflicts[conflict.ID] = string(encoded)
	}
	if !changed && c.state.Checking == checking {
		c.mu.Unlock()
		return
	}
	c.state.Checking, c.state.Conflicts = checking, conflicts
	c.state.Revision++
	c.mu.Unlock()
	c.owner.emitEvent(namespaceIsolationEvent, c.owner.GetNamespaceIsolationState())
	if changed {
		for _, conflict := range conflicts {
			encoded, _ := json.Marshal(conflict)
			if conflict.Detail != "" && oldConflicts[conflict.ID] != string(encoded) {
				c.report(errors.New(conflict.Detail), conflict.Reason, conflict.ImporterKey)
			}
		}
	}
}

func (c *namespaceIsolationCoordinator) report(err error, stage, key string) {
	if err != nil && c.owner.log != nil {
		_ = infra.ReportError(c.owner.log, err, "Mod:namespaceIsolation", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "namespace-isolation", Stage: stage,
			Fields: map[string]any{"importerKey": key, "state": c.owner.GetNamespaceIsolationState()},
		})
	}
}

func (c *namespaceIsolationCoordinator) logIsolationSuccess(conflict NamespaceIsolationConflict) {
	if c.owner.log == nil {
		return
	}
	mappings := map[string]any{}
	backups := map[string]string{}
	for _, path := range conflict.ModPaths {
		_, manifest, _, err := readNamespaceManifest(path)
		if err == nil && manifest != nil {
			mappings[path] = manifest.Mappings
		}
		backups[path] = filepath.Join(path, ".nhd-namespace", "transactions")
	}
	c.owner.log.Info(map[string]any{
		"operation":         "namespace-isolation",
		"stage":             "committed",
		"importerKey":       conflict.ImporterKey,
		"conflictID":        conflict.ID,
		"oldNamespace":      conflict.Namespace,
		"newMappings":       mappings,
		"modPaths":          conflict.ModPaths,
		"iniPaths":          conflict.INIPaths,
		"backupDirectories": backups,
		"rollback":          "not-required",
	}, "Mod:namespaceIsolation")
}

func (c *namespaceIsolationCoordinator) replaceWatcher(roots []string) error {
	slices.Sort(roots)
	if slices.Equal(roots, c.watchRoots) && c.watcher != nil {
		return nil
	}
	if c.watcher != nil {
		if err := c.watcher.Close(); err != nil {
			return err
		}
		c.watcher = nil
	}
	c.watchRoots = slices.Clone(roots)
	if len(roots) == 0 {
		return nil
	}
	w, err := watcher.WatchTree(roots, watcher.TreeConfig{
		Depth: -1, Ops: watcher.All, Debounce: c.settle,
		Filter: func(event watcher.Event) bool {
			if namespaceIgnoredPath(event.Path) {
				return false
			}
			// Logs, shader caches and mod assets change constantly and are never part
			// of the inventory. Removed paths cannot be classified and still pass,
			// because a deleted directory changes the mod boundaries.
			if !strings.EqualFold(filepath.Ext(event.Path), ".ini") &&
				!strings.EqualFold(filepath.Base(event.Path), "nhd.json") {
				if info, err := os.Lstat(event.Path); err == nil && info.Mode().IsRegular() {
					return false
				}
			}
			c.mu.Lock()
			expected, ok := c.self[strings.ToLower(event.Path)]
			c.mu.Unlock()
			if ok {
				content, err := os.ReadFile(event.Path)
				if err == nil && string(content) == string(expected) {
					return false
				}
			}
			return true
		},
		OnError: func(err error) { c.report(err, "watch", ""); c.enqueue() },
	}, func(watcher.Event) { c.enqueue() })
	c.watcher = w
	return err
}

func namespaceIgnoredPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(strings.ToLower(part), ".nhd") {
			return true
		}
	}
	return false
}
