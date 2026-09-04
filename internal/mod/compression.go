package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nahida.live/desktop/internal/watcher"
)

const (
	compressionTransitionKey = "mod_compression:transition"
	compressionRootsKey      = "mod_compression:roots"
	compressionEvent         = "mod:compressionProgress"
	compressionManifestName  = ".nahida-compression.json"
	compressionTempMarker    = ".nahida-compression-tmp"
	compressionWatchDebounce = 900 * time.Millisecond
	compressionSelfChangeTTL = 2 * time.Second
)

type CompressionConfig struct {
	Method       string `json:"method"`
	ThresholdMiB int    `json:"thresholdMiB"`
}

type CompressionState struct {
	Enabled         bool   `json:"enabled"`
	Method          string `json:"method"`
	ThresholdMiB    int    `json:"thresholdMiB"`
	Status          string `json:"status"`
	TargetEnabled   *bool  `json:"targetEnabled,omitempty"`
	ProcessedFiles  int    `json:"processedFiles"`
	TotalFiles      int    `json:"totalFiles"`
	ProcessedBytes  int64  `json:"processedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	CurrentFileName string `json:"currentFileName,omitempty"`
	FailedFiles     int    `json:"failedFiles"`
	ExternalFiles   int    `json:"externalFiles"`
	Error           string `json:"error,omitempty"`
	CanToggle       bool   `json:"canToggle"`
	CanConfigure    bool   `json:"canConfigure"`
}

type compressionTransition struct {
	TargetEnabled bool   `json:"targetEnabled"`
	Method        string `json:"method"`
	ThresholdMiB  int    `json:"thresholdMiB"`
	Stage         string `json:"stage"`
	StartedAt     string `json:"startedAt"`
}

type compressionCoordinator struct {
	owner *Mod

	requestMu            sync.Mutex
	mu                   sync.Mutex
	state                CompressionState
	loaded               bool
	running              bool
	configuring          bool
	pendingFull          bool
	pendingScopes        map[string]struct{}
	debounceTimer        *time.Timer
	cancel               context.CancelFunc
	done                 chan struct{}
	watcher              *watcher.Watcher
	configWatcher        *watcher.Watcher
	opMu                 sync.Mutex
	watchMu              sync.Mutex
	selfChangeMu         sync.Mutex
	selfChanges          map[string]time.Time
	selfChangeTimer      *time.Timer
	selfChangeGeneration uint64
	stopped              atomic.Bool
}

func newCompressionCoordinator(owner *Mod) *compressionCoordinator {
	return &compressionCoordinator{
		owner: owner, pendingScopes: map[string]struct{}{}, selfChanges: map[string]time.Time{},
		state: CompressionState{Method: "xpress4k", ThresholdMiB: 1, Status: "checking"},
	}
}

// StartCompression recovers a persisted transition and begins continuous reconciliation.
//
//wails:ignore
func (m *Mod) StartCompression(ctx context.Context) error {
	if m == nil || m.compression == nil {
		return nil
	}
	if err := m.compression.loadState(ctx); err != nil {
		return err
	}
	m.compression.stopped.Store(false)
	if err := m.compression.replaceWatcher(ctx); err != nil {
		m.compression.logError(err, "watch", "", "")
	}
	m.compression.schedule()
	return nil
}

func (m *Mod) GetCompressionState(ctx context.Context) (CompressionState, error) {
	if m == nil || m.compression == nil {
		return CompressionState{}, errors.New("mod compression is not configured")
	}
	if err := m.compression.loadStateIfNeeded(ctx); err != nil {
		return CompressionState{}, err
	}
	return m.compression.snapshot(), nil
}

func (m *Mod) SetCompressionConfig(ctx context.Context, config CompressionConfig) (CompressionState, error) {
	if m == nil || m.compression == nil || m.settings == nil {
		return CompressionState{}, errors.New("mod compression is not configured")
	}
	if config.Method != "zstd" && config.Method != "xpress4k" {
		return m.compression.snapshot(), errors.New("INVALID_COMPRESSION_METHOD")
	}
	if config.ThresholdMiB < 1 || config.ThresholdMiB > 64 {
		return m.compression.snapshot(), errors.New("INVALID_COMPRESSION_THRESHOLD")
	}
	if !m.compression.requestMu.TryLock() {
		return m.compression.snapshot(), errors.New("COMPRESSION_CONFIG_LOCKED")
	}
	defer m.compression.requestMu.Unlock()
	state := m.compression.snapshot()
	if !state.CanConfigure {
		return state, errors.New("COMPRESSION_CONFIG_LOCKED")
	}
	settings, err := m.compression.settings()
	if err != nil {
		return state, err
	}
	m.compression.mu.Lock()
	m.compression.configuring = true
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()
	m.compression.publish()
	if err := settings.SetCompressionConfig(ctx, config.Method, config.ThresholdMiB); err != nil {
		m.compression.reloadConfigState(ctx, settings)
		m.compression.logError(err, "save-config", config.Method, "")
		return m.compression.snapshot(), errors.New("MOD_COMPRESSION_CONFIG_FAILED")
	}
	m.compression.mu.Lock()
	m.compression.state.Method = config.Method
	m.compression.state.ThresholdMiB = config.ThresholdMiB
	m.compression.configuring = false
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()
	m.compression.publish()
	return m.compression.snapshot(), nil
}

func (m *Mod) SetCompressionEnabled(ctx context.Context, enabled bool) (CompressionState, error) {
	if m == nil || m.compression == nil || m.client == nil {
		return CompressionState{}, errors.New("mod compression is not configured")
	}
	if !m.compression.requestMu.TryLock() {
		return m.compression.snapshot(), errors.New("COMPRESSION_TOGGLE_LOCKED")
	}
	defer m.compression.requestMu.Unlock()
	state := m.compression.snapshot()
	if state.Status == "blocked" || (!state.CanToggle && state.Status != "error") {
		return state, errors.New("COMPRESSION_TOGGLE_LOCKED")
	}
	transition := compressionTransition{
		TargetEnabled: enabled, Method: state.Method, ThresholdMiB: state.ThresholdMiB,
		Stage: "queued", StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := m.compression.saveTransition(ctx, transition); err != nil {
		return state, err
	}
	m.compression.mu.Lock()
	m.compression.state.TargetEnabled = boolPointer(enabled)
	m.compression.state.Status = "checking"
	m.compression.state.Error = ""
	m.compression.state.FailedFiles = 0
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()
	m.compression.publish()
	m.compression.enqueueFullLocked()
	return m.compression.snapshot(), nil
}

func (c *compressionCoordinator) loadStateIfNeeded(ctx context.Context) error {
	c.mu.Lock()
	loaded := c.loaded
	c.mu.Unlock()
	if loaded {
		return nil
	}
	return c.loadState(ctx)
}

func (c *compressionCoordinator) loadState(ctx context.Context) error {
	settings, err := c.settings()
	if err != nil {
		return err
	}
	method, err := settings.GetCompressionMethod(ctx)
	if err != nil {
		return err
	}
	threshold, err := settings.GetCompressionThresholdMib(ctx)
	if err != nil {
		return err
	}
	enabled, err := settings.GetCompressionEnabled(ctx)
	if err != nil {
		return err
	}
	transition, err := c.loadTransition(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.state = CompressionState{Enabled: enabled, Method: method, ThresholdMiB: threshold, Status: "checking"}
	c.loaded = true
	if transition != nil {
		c.state.Method = transition.Method
		c.state.ThresholdMiB = transition.ThresholdMiB
		c.state.TargetEnabled = boolPointer(transition.TargetEnabled)
	}
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
	return nil
}

func (c *compressionCoordinator) schedule() {
	c.requestMu.Lock()
	c.enqueueFullLocked()
	c.requestMu.Unlock()
}

type compressionWork struct {
	full   bool
	scopes []string
}

type compressionMutationMarker func(...string)

func ignoreCompressionMutations(...string) {}

func (c *compressionCoordinator) enqueueFullLocked() {
	c.mu.Lock()
	c.pendingFull = true
	clear(c.pendingScopes)
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
		c.debounceTimer = nil
	}
	c.mu.Unlock()
	c.startPendingLocked()
}

func (c *compressionCoordinator) scheduleScope(path string) {
	if strings.TrimSpace(path) == "" || c.stopped.Load() {
		return
	}
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	c.mu.Lock()
	if c.state.Status == "blocked" {
		c.pendingFull = true
		clear(c.pendingScopes)
	} else if !c.pendingFull {
		addCompressionScope(c.pendingScopes, path)
	}
	if c.running {
		c.mu.Unlock()
		return
	}
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(compressionWatchDebounce, func() {
		c.requestMu.Lock()
		c.mu.Lock()
		if c.debounceTimer != timer {
			c.mu.Unlock()
			c.requestMu.Unlock()
			return
		}
		c.debounceTimer = nil
		c.mu.Unlock()
		c.startPendingLocked()
		c.requestMu.Unlock()
	})
	c.debounceTimer = timer
	c.mu.Unlock()
}

func (c *compressionCoordinator) startPendingLocked() {
	if c.stopped.Load() {
		return
	}
	c.mu.Lock()
	if c.running || !c.pendingFull && len(c.pendingScopes) == 0 {
		c.mu.Unlock()
		return
	}
	work := c.takePendingLocked()
	ctx, cancel := context.WithCancel(context.Background())
	c.running, c.cancel, c.done = true, cancel, make(chan struct{})
	c.state.Status = "checking"
	c.deriveCapabilitiesLocked()
	done := c.done
	c.mu.Unlock()
	c.publish()
	go c.run(ctx, done, work)
}

func (c *compressionCoordinator) run(ctx context.Context, done chan struct{}, work compressionWork) {
	defer close(done)
	for {
		c.reconcile(ctx, work)
		c.requestMu.Lock()
		c.mu.Lock()
		if ctx.Err() != nil || !c.pendingFull && len(c.pendingScopes) == 0 {
			c.running, c.cancel = false, nil
			c.deriveCapabilitiesLocked()
			c.mu.Unlock()
			c.requestMu.Unlock()
			c.publish()
			return
		}
		work = c.takePendingLocked()
		c.state.Status = "checking"
		c.deriveCapabilitiesLocked()
		c.mu.Unlock()
		c.requestMu.Unlock()
		c.publish()
	}
}

func (c *compressionCoordinator) takePendingLocked() compressionWork {
	work := compressionWork{full: c.pendingFull, scopes: make([]string, 0, len(c.pendingScopes))}
	for scope := range c.pendingScopes {
		work.scopes = append(work.scopes, scope)
	}
	slices.Sort(work.scopes)
	c.pendingFull = false
	clear(c.pendingScopes)
	return work
}

func addCompressionScope(scopes map[string]struct{}, path string) {
	clean := filepath.Clean(path)
	key := strings.ToLower(clean)
	for existing := range scopes {
		if pathContains(existing, key) {
			return
		}
		if pathContains(key, existing) {
			delete(scopes, existing)
		}
	}
	scopes[key] = struct{}{}
}

func pathContains(parent, child string) bool {
	parent = strings.TrimSuffix(strings.ToLower(filepath.Clean(parent)), string(filepath.Separator))
	child = strings.ToLower(filepath.Clean(child))
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}

func (c *compressionCoordinator) reconcile(ctx context.Context, work compressionWork) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	transition, err := c.loadTransition(ctx)
	if err != nil {
		c.fail(err, "load-journal", "", "")
		return
	}
	state := c.snapshot()
	fullWork := work.full
	target := state.Enabled
	method, threshold := state.Method, state.ThresholdMiB
	if transition != nil {
		fullWork = true
		target, method, threshold = transition.TargetEnabled, transition.Method, transition.ThresholdMiB
		transition.Stage = "checking"
		if err := c.saveTransition(ctx, *transition); err != nil {
			c.fail(err, "update-journal", method, "")
			return
		}
	}
	currentRoots, err := c.roots(ctx)
	if err != nil {
		c.fail(err, "resolve-importers", "", "")
		return
	}
	managedRoots, err := c.loadManagedRoots(ctx)
	if err != nil {
		c.fail(err, "load-managed-roots", method, "")
		return
	}
	c.resetProgress("checking", target, method, threshold)
	workRoots := currentRoots
	if !fullWork {
		workRoots = existingCompressionScopes(work.scopes, currentRoots)
	}
	if !target {
		if fullWork {
			workRoots = unionPaths(currentRoots, managedRoots)
		}
	} else if fullWork && len(subtractPaths(managedRoots, currentRoots)) > 0 {
		removedRoots := subtractPaths(managedRoots, currentRoots)
		if method == "zstd" {
			err = restoreAllZstd(ctx, removedRoots, c.setTotals, c.progress, c.markSelfChanges)
		} else {
			err = restoreManagedWOF(ctx, removedRoots, c.owner.client, c.setTotals, c.progress, c.markSelfChanges)
		}
		if err != nil {
			c.fail(err, "restore-removed-importers", method, "")
			return
		}
	}
	external, err := unmanagedCompressionFiles(ctx, workRoots, c.owner.client)
	if err != nil {
		c.fail(err, "check-external", "", "")
		return
	}
	if len(external) > 0 {
		if !fullWork {
			external, err = unmanagedCompressionFiles(ctx, currentRoots, c.owner.client)
			if err != nil {
				c.fail(err, "check-external-full", "", "")
				return
			}
		}
		// A transition may already have compressed files before the unmanaged
		// backing appeared, so always roll back this application's ownership.
		if method == "zstd" {
			err = restoreAllZstd(ctx, unionPaths(workRoots, managedRoots), c.setTotals, c.progress, c.markSelfChanges)
		} else {
			err = restoreManagedWOF(ctx, unionPaths(workRoots, managedRoots), c.owner.client, c.setTotals, c.progress, c.markSelfChanges)
		}
		if err != nil {
			c.mu.Lock()
			c.state.ExternalFiles = len(external)
			c.mu.Unlock()
			c.fail(err, "rollback-external-conflict", method, "")
			return
		}
		settings, settingsErr := c.settings()
		if settingsErr != nil {
			c.fail(settingsErr, "disable-external-conflict", method, "")
			return
		}
		if err := settings.SetCompressionEnabled(ctx, false); err != nil {
			c.fail(err, "disable-external-conflict", method, "")
			return
		}
		if transition != nil {
			if err := c.owner.client.AppState.Delete(ctx, compressionTransitionKey); err != nil {
				c.fail(err, "clear-external-transition", method, "")
				return
			}
		}
		c.mu.Lock()
		c.state.Enabled = false
		c.state.Status = "blocked"
		c.state.TargetEnabled = nil
		c.state.ExternalFiles = len(external)
		c.state.Error = "EXTERNAL_COMPRESSION_DETECTED"
		c.deriveCapabilitiesLocked()
		c.mu.Unlock()
		c.publish()
		return
	}

	if target {
		c.setStatus("compressing")
		if transition != nil {
			transition.Stage = "compressing"
			if err := c.saveTransition(ctx, *transition); err != nil {
				c.fail(err, "update-journal", method, "")
				return
			}
		}
		if method == "zstd" {
			err = errors.Join(
				restoreEnabledZstd(ctx, workRoots, c.setTotals, c.progress, c.markSelfChanges),
				compressDisabledZstd(ctx, workRoots, int64(threshold)*1024*1024, c.setTotals, c.progress, c.markSelfChanges),
			)
		} else {
			ledgerScopes := workRoots
			if !fullWork {
				ledgerScopes = work.scopes
			}
			err = errors.Join(
				applyXpress4K(ctx, workRoots, c.owner.client, c.setTotals, c.progress, c.markSelfChanges),
				cleanupMissingWofLedgers(ctx, ledgerScopes, c.owner.client),
			)
		}
	} else if fullWork {
		c.setStatus("decompressing")
		if transition != nil {
			transition.Stage = "decompressing"
			if err := c.saveTransition(ctx, *transition); err != nil {
				c.fail(err, "update-journal", method, "")
				return
			}
		}
		if method == "zstd" {
			err = restoreAllZstd(ctx, workRoots, c.setTotals, c.progress, c.markSelfChanges)
		} else {
			err = restoreManagedWOF(ctx, workRoots, c.owner.client, c.setTotals, c.progress, c.markSelfChanges)
		}
	}
	if err != nil {
		c.fail(err, "reconcile", method, "")
		return
	}
	if target && fullWork {
		if err := c.saveManagedRoots(ctx, currentRoots); err != nil {
			c.fail(err, "save-managed-roots", method, "")
			return
		}
	} else if !target && fullWork {
		if err := c.owner.client.AppState.Delete(ctx, compressionRootsKey); err != nil {
			c.fail(err, "clear-managed-roots", method, "")
			return
		}
	}
	if transition != nil {
		settings, settingsErr := c.settings()
		if settingsErr != nil {
			c.fail(settingsErr, "commit-setting", method, "")
			return
		}
		if err := settings.SetCompressionEnabled(ctx, target); err != nil {
			c.fail(err, "commit-setting", method, "")
			return
		}
		if err := c.owner.client.AppState.Delete(ctx, compressionTransitionKey); err != nil {
			c.fail(err, "delete-journal", method, "")
			return
		}
	}
	c.mu.Lock()
	c.state.Enabled = target
	c.state.Status = "idle"
	c.state.TargetEnabled = nil
	c.state.CurrentFileName = ""
	c.state.Error = ""
	c.state.ExternalFiles = 0
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

func existingCompressionScopes(scopes, roots []string) []string {
	var result []string
	for _, scope := range scopes {
		contained := false
		for _, root := range roots {
			if pathContains(root, scope) {
				contained = true
				break
			}
		}
		if !contained {
			continue
		}
		if _, err := os.Lstat(scope); err == nil {
			result = append(result, scope)
		}
	}
	return unionPaths(result)
}

func (c *compressionCoordinator) roots(ctx context.Context) ([]string, error) {
	if c.owner.xxmi == nil {
		return nil, nil
	}
	importers, err := c.owner.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(importers))
	for _, importer := range importers {
		root, err := resolveCompressionRoot(importer.ImporterFolder)
		if err != nil {
			continue
		}
		key := strings.ToLower(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, root)
	}
	slices.Sort(roots)
	return roots, nil
}

func resolveCompressionRoot(importerFolder string) (string, error) {
	root, err := filepath.Abs(filepath.Join(importerFolder, "Mods"))
	if err != nil {
		return "", err
	}
	// XXMI commonly makes ImporterFolder\Mods a link to the user's configured
	// mod library. Resolve that trusted boundary once; recursive walkers still
	// reject reparse points found below the resulting root.
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("compression root is not a directory: %q", root)
	}
	return root, nil
}

func (c *compressionCoordinator) replaceWatcher(ctx context.Context) error {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()
	if c.stopped.Load() {
		return nil
	}
	roots, err := c.roots(ctx)
	if err != nil {
		return err
	}
	if c.watcher != nil {
		_ = c.watcher.Close()
		c.watcher = nil
	}
	if c.configWatcher != nil {
		_ = c.configWatcher.Close()
		c.configWatcher = nil
	}
	if len(roots) > 0 {
		c.watcher, err = watcher.WatchTree(roots, watcher.TreeConfig{
			Depth: -1, Ops: watcher.All, Debounce: 0,
			Filter: func(event watcher.Event) bool {
				name := filepath.Base(event.Path)
				return name != compressionManifestName && !strings.Contains(name, compressionTempMarker) &&
					!c.isSelfChange(event.Path) && !isCompressionDirectoryWrite(event)
			},
			OnError: func(err error) { c.logError(err, "watch", "", "") },
		}, func(event watcher.Event) {
			if scope := compressionScopeForPath(event.Path, roots); scope != "" {
				c.scheduleScope(scope)
			}
		})
		if err != nil {
			return err
		}
	}
	pathSource, ok := c.owner.xxmi.(interface {
		GetXXMIPath(context.Context) (*string, error)
	})
	if !ok {
		return nil
	}
	xxmiPath, err := pathSource.GetXXMIPath(ctx)
	if err != nil || xxmiPath == nil {
		return err
	}
	configPath := filepath.Join(*xxmiPath, "XXMI Launcher Config.json")
	c.configWatcher, err = watcher.WatchFile(configPath, watcher.FileConfig{
		Ops: watcher.All, SettleDelay: 900 * time.Millisecond, DistinctContent: true,
		OnError: func(err error) { c.logError(err, "watch-importers", "", configPath) },
	}, func(watcher.Event) {
		go func() {
			if err := c.replaceWatcher(context.Background()); err != nil {
				c.logError(err, "replace-importer-watch", "", configPath)
			}
			c.schedule()
		}()
	})
	return err
}

func (c *compressionCoordinator) stop() error {
	c.watchMu.Lock()
	c.stopped.Store(true)
	rootWatcher, configWatcher := c.watcher, c.configWatcher
	c.watcher, c.configWatcher = nil, nil
	c.watchMu.Unlock()
	watchErr := errors.Join(closeCompressionWatcher(rootWatcher), closeCompressionWatcher(configWatcher))
	c.requestMu.Lock()
	c.mu.Lock()
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
		c.debounceTimer = nil
	}
	c.pendingFull = false
	clear(c.pendingScopes)
	cancel, done := c.cancel, c.done
	c.cancel = nil
	c.mu.Unlock()
	c.requestMu.Unlock()
	c.selfChangeMu.Lock()
	if c.selfChangeTimer != nil {
		c.selfChangeTimer.Stop()
		c.selfChangeTimer = nil
	}
	c.selfChangeGeneration++
	clear(c.selfChanges)
	c.selfChangeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return watchErr
}

func closeCompressionWatcher(value *watcher.Watcher) error {
	if value == nil {
		return nil
	}
	return value.Close()
}

func (c *compressionCoordinator) restoreBeforeEnable(ctx context.Context, folder string) error {
	state := c.snapshot()
	compressionApplies := state.Method == "zstd" && (state.Enabled || state.TargetEnabled != nil && *state.TargetEnabled)
	if !compressionApplies {
		transition, err := c.loadTransition(ctx)
		if err != nil {
			return err
		}
		compressionApplies = transition != nil && transition.Method == "zstd" && transition.TargetEnabled
	}
	if !compressionApplies {
		return nil
	}
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	c.opMu.Lock()
	err := restoreZstdFolder(ctx, folder, c.markSelfChanges)
	c.opMu.Unlock()
	if err != nil {
		c.fail(err, "restore-before-enable", "zstd", folder)
		return err
	}
	c.scheduleScope(folder)
	return nil
}

func (c *compressionCoordinator) saveTransition(ctx context.Context, transition compressionTransition) error {
	raw, err := json.Marshal(transition)
	if err != nil {
		return err
	}
	return c.owner.client.AppState.Upsert(ctx, compressionTransitionKey, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
}

func (c *compressionCoordinator) settings() (compressionSettings, error) {
	settings, ok := c.owner.settings.(compressionSettings)
	if !ok || settings == nil {
		return nil, errors.New("mod compression settings are unavailable")
	}
	return settings, nil
}

func (c *compressionCoordinator) loadTransition(ctx context.Context) (*compressionTransition, error) {
	if c.owner.client == nil {
		return nil, errors.New("mod compression database is unavailable")
	}
	raw, err := c.owner.client.AppState.GetValue(ctx, compressionTransitionKey)
	if err != nil || raw == nil {
		return nil, err
	}
	var transition compressionTransition
	if err := json.Unmarshal([]byte(*raw), &transition); err != nil {
		return nil, fmt.Errorf("decode compression transition: %w", err)
	}
	return &transition, nil
}

func (c *compressionCoordinator) loadManagedRoots(ctx context.Context) ([]string, error) {
	raw, err := c.owner.client.AppState.GetValue(ctx, compressionRootsKey)
	if err != nil || raw == nil {
		return nil, err
	}
	var roots []string
	if err := json.Unmarshal([]byte(*raw), &roots); err != nil {
		return nil, fmt.Errorf("decode compression roots: %w", err)
	}
	return roots, nil
}

func (c *compressionCoordinator) saveManagedRoots(ctx context.Context, roots []string) error {
	raw, err := json.Marshal(roots)
	if err != nil {
		return err
	}
	return c.owner.client.AppState.Upsert(ctx, compressionRootsKey, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
}

func unionPaths(groups ...[]string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, paths := range groups {
		for _, path := range paths {
			key := strings.ToLower(filepath.Clean(path))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, filepath.Clean(path))
		}
	}
	slices.Sort(result)
	return result
}

func subtractPaths(paths, remove []string) []string {
	removed := map[string]struct{}{}
	for _, path := range remove {
		removed[strings.ToLower(filepath.Clean(path))] = struct{}{}
	}
	var result []string
	for _, path := range paths {
		if _, ok := removed[strings.ToLower(filepath.Clean(path))]; !ok {
			result = append(result, path)
		}
	}
	return result
}

func (c *compressionCoordinator) progress(path string, size int64, failed bool) {
	c.mu.Lock()
	c.state.ProcessedFiles++
	c.state.ProcessedBytes += size
	c.state.CurrentFileName = filepath.Base(path)
	if failed {
		c.state.FailedFiles++
	}
	c.mu.Unlock()
	c.publish()
}

func (c *compressionCoordinator) markSelfChanges(paths ...string) {
	expires := time.Now().Add(compressionSelfChangeTTL)
	c.selfChangeMu.Lock()
	for _, path := range paths {
		c.selfChanges[strings.ToLower(filepath.Clean(path))] = expires
	}
	if c.selfChangeTimer != nil {
		c.selfChangeTimer.Stop()
	}
	c.selfChangeGeneration++
	generation := c.selfChangeGeneration
	c.selfChangeTimer = time.AfterFunc(compressionSelfChangeTTL, func() {
		c.clearExpiredSelfChanges(generation)
	})
	c.selfChangeMu.Unlock()
}

func (c *compressionCoordinator) clearExpiredSelfChanges(generation uint64) {
	now := time.Now()
	c.selfChangeMu.Lock()
	if generation != c.selfChangeGeneration {
		c.selfChangeMu.Unlock()
		return
	}
	for candidate, expires := range c.selfChanges {
		if !expires.After(now) {
			delete(c.selfChanges, candidate)
		}
	}
	c.selfChangeTimer = nil
	if len(c.selfChanges) > 0 {
		var next time.Time
		for _, expires := range c.selfChanges {
			if next.IsZero() || expires.Before(next) {
				next = expires
			}
		}
		delay := time.Until(next)
		if delay < 0 {
			delay = 0
		}
		c.selfChangeTimer = time.AfterFunc(delay, func() {
			c.clearExpiredSelfChanges(generation)
		})
	}
	c.selfChangeMu.Unlock()
}

func (c *compressionCoordinator) isSelfChange(path string) bool {
	now := time.Now()
	key := strings.ToLower(filepath.Clean(path))
	c.selfChangeMu.Lock()
	defer c.selfChangeMu.Unlock()
	for candidate, expires := range c.selfChanges {
		if !expires.After(now) {
			delete(c.selfChanges, candidate)
		}
	}
	expires, ok := c.selfChanges[key]
	return ok && expires.After(now)
}

func (c *compressionCoordinator) setTotals(files int, bytes int64) {
	c.mu.Lock()
	c.state.TotalFiles += files
	c.state.TotalBytes += bytes
	c.mu.Unlock()
	c.publish()
}

func (c *compressionCoordinator) resetProgress(status string, target bool, method string, threshold int) {
	c.mu.Lock()
	c.state.Method, c.state.ThresholdMiB = method, threshold
	c.state.Status, c.state.TargetEnabled = status, boolPointer(target)
	c.state.ProcessedFiles, c.state.TotalFiles = 0, 0
	c.state.ProcessedBytes, c.state.TotalBytes = 0, 0
	c.state.FailedFiles, c.state.ExternalFiles = 0, 0
	c.state.CurrentFileName, c.state.Error = "", ""
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

func (c *compressionCoordinator) setStatus(status string) {
	c.mu.Lock()
	c.state.Status = status
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

func (c *compressionCoordinator) fail(err error, stage, method, path string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	c.logError(err, stage, method, path)
	c.mu.Lock()
	c.state.Status = "error"
	c.state.Error = "MOD_COMPRESSION_FAILED"
	c.state.TargetEnabled = nil
	if c.state.FailedFiles == 0 {
		c.state.FailedFiles = 1
	}
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

func (c *compressionCoordinator) logError(err error, stage, method, path string) {
	if err == nil || c.owner.log == nil {
		return
	}
	c.owner.log.Error(fmt.Sprintf("method=%s stage=%s path=%q cleanup=pending error=%v", method, stage, path, err), "Mod:compression")
}

func (c *compressionCoordinator) snapshot() CompressionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *compressionCoordinator) publish() {
	c.owner.emitEvent(compressionEvent, c.snapshot())
}

func (c *compressionCoordinator) deriveCapabilitiesLocked() {
	busy := c.state.Status == "checking" || c.state.Status == "compressing" || c.state.Status == "decompressing"
	c.state.CanToggle = !c.configuring && !busy && c.state.Status != "blocked"
	c.state.CanConfigure = !c.configuring && !c.state.Enabled && !busy && c.state.Status != "error"
}

func (c *compressionCoordinator) reloadConfigState(ctx context.Context, settings compressionSettings) {
	method, methodErr := settings.GetCompressionMethod(ctx)
	threshold, thresholdErr := settings.GetCompressionThresholdMib(ctx)
	c.mu.Lock()
	if methodErr == nil {
		c.state.Method = method
	}
	if thresholdErr == nil {
		c.state.ThresholdMiB = threshold
	}
	c.configuring = false
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

func compressionScopeForPath(path string, roots []string) string {
	clean := filepath.Clean(path)
	for _, root := range roots {
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if rel == "." {
			return root
		}
		first, _, _ := strings.Cut(rel, string(filepath.Separator))
		return filepath.Join(root, first)
	}
	return ""
}

func isCompressionDirectoryWrite(event watcher.Event) bool {
	if event.Op != watcher.Write {
		return false
	}
	info, err := os.Lstat(event.Path)
	return err == nil && info.IsDir()
}

func boolPointer(value bool) *bool { return &value }

type compressionFile struct {
	path string
	size int64
}

func walkCompressionFiles(roots []string, accept func(string, fs.FileInfo) bool) ([]compressionFile, error) {
	var files []compressionFile
	var errs []error
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				errs = append(errs, fmt.Errorf("access %q: %w", path, walkErr))
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				errs = append(errs, fmt.Errorf("stat %q: %w", path, err))
				return nil
			}
			if entry.IsDir() {
				if info.Mode()&os.ModeSymlink != 0 || info.Sys() != nil && isReparsePoint(info) {
					return filepath.SkipDir
				}
				return nil
			}
			if info.Mode().IsRegular() && accept(path, info) {
				files = append(files, compressionFile{path: path, size: info.Size()})
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return files, errors.Join(errs...)
}
