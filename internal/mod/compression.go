package mod

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/watcher"
)

const (
	legacyCompressionTransitionKey = "mod_compression:transition"
	legacyCompressionRootsKey      = "mod_compression:roots"
	legacyCompressionLedgerPrefix  = "mod_compression:file:"
	compressionEvent               = "mod:compressionProgress"
	legacyCompressionManifestName  = ".nahida-compression.json"
	compressionTempMarker          = ".nahida-compression-tmp"
	compressionProgressEvery       = 100 * time.Millisecond
	compressionWatchDebounce       = 900 * time.Millisecond
	compressionSelfChangeTTL       = 2 * time.Second
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
	Error           string `json:"error,omitempty"`
	CanToggle       bool   `json:"canToggle"`
	CanConfigure    bool   `json:"canConfigure"`
}

type compressionCoordinator struct {
	owner      *Mod
	diagnostic infra.DiagnosticThrottle

	requestMu            sync.Mutex
	mu                   sync.Mutex
	state                CompressionState
	loaded               bool
	running              bool
	configuring          bool
	pendingFull          bool
	pendingScopes        map[string]struct{}
	debounceTimer        *time.Timer
	cancel               context.CancelCauseFunc
	done                 chan struct{}
	watcher              *watcher.Watcher
	configWatcher        *watcher.Watcher
	opMu                 sync.Mutex
	watchMu              sync.Mutex
	selfChangeMu         sync.Mutex
	selfChanges          map[string]time.Time
	selfChangeTimer      *time.Timer
	selfChangeDeadline   time.Time
	selfChangeGeneration uint64
	wofOwnership         compressionFileOwnership
	xpressDeclined       compressionDeclinedFiles
	zstdDeclined         compressionDeclinedFiles
	lastProgressEmit     time.Time
	stopped              atomic.Bool
}

func newCompressionCoordinator(owner *Mod) *compressionCoordinator {
	return &compressionCoordinator{
		owner: owner, pendingScopes: map[string]struct{}{}, selfChanges: map[string]time.Time{},
		state: CompressionState{Method: "xpress4k", ThresholdMiB: 1, Status: "checking"},
	}
}

// StartCompression loads the desired setting and begins continuous reconciliation.
func (m *Mod) StartCompression(ctx context.Context) error {
	m.watchMu.Lock()
	defer m.watchMu.Unlock()

	if err := m.compression.loadState(ctx); err != nil {
		return m.compression.fail(err, "load-state", "", "")
	}
	if err := m.compression.cleanupLegacyState(ctx); err != nil {
		return m.compression.fail(err, "cleanup-legacy-state", "", "")
	}
	m.compression.stopped.Store(false)
	m.RefreshCompressionImporters(ctx)
	return nil
}

// RefreshCompressionImporters updates watched mod folders and queues a full pass for the current selection.
func (m *Mod) RefreshCompressionImporters(ctx context.Context) {
	if m.compression.stopped.Load() {
		return
	}
	if err := m.compression.replaceWatcher(ctx); err != nil {
		m.compression.logError(err, "refresh-importers", "", "")
	}
	m.compression.schedule()
}

// WaitCompressionPass waits for the reconciliation run started by
// StartCompression to stop before returning. Startup uses it to keep
// file-mutating calls gated while mod files are being rewritten.
func (m *Mod) WaitCompressionPass(ctx context.Context) error {
	return m.compression.waitForRun(ctx)
}

func (m *Mod) GetCompressionState(ctx context.Context) (CompressionState, error) {
	if err := m.compression.loadStateIfNeeded(ctx); err != nil {
		return CompressionState{}, err
	}
	return m.compression.snapshot(), nil
}

func (m *Mod) SetCompressionConfig(ctx context.Context, config CompressionConfig) (CompressionState, error) {
	if m.settings == nil {
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
		_ = m.compression.fail(err, "save-config", config.Method, "")
		return m.compression.snapshot(), errors.New("MOD_COMPRESSION_CONFIG_FAILED")
	}
	m.compression.mu.Lock()
	m.compression.state.Method = config.Method
	m.compression.state.ThresholdMiB = config.ThresholdMiB
	m.compression.state.Status = "idle"
	m.compression.state.Error = ""
	m.compression.configuring = false
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()
	m.compression.publish()
	return m.compression.snapshot(), nil
}

func (m *Mod) SetCompressionEnabled(ctx context.Context, enabled bool) (CompressionState, error) {
	if m.client == nil {
		return CompressionState{}, errors.New("mod compression is not configured")
	}
	if !m.compression.requestMu.TryLock() {
		return m.compression.snapshot(), errors.New("COMPRESSION_TOGGLE_LOCKED")
	}
	defer m.compression.requestMu.Unlock()
	state := m.compression.snapshot()
	interruptingEnable := !enabled && compressionEnableInProgress(state)
	if (!state.CanToggle && state.Status != "error") || (enabled && compressionEnableInProgress(state)) {
		return state, errors.New("COMPRESSION_TOGGLE_LOCKED")
	}
	if interruptingEnable {
		m.compression.cancelCurrentWorkLocked()
	}
	settings, err := m.compression.settings()
	if err != nil {
		reported := m.compression.fail(err, "resolve-settings", state.Method, "")
		if interruptingEnable {
			m.compression.enqueueFullLocked()
		}
		return m.compression.snapshot(), reported
	}
	if err := settings.SetCompressionEnabled(ctx, enabled); err != nil {
		reported := m.compression.fail(err, "save-enabled", state.Method, "")
		if interruptingEnable {
			m.compression.enqueueFullLocked()
		}
		return m.compression.snapshot(), reported
	}
	m.compression.mu.Lock()
	m.compression.state.Enabled = enabled
	m.compression.state.TargetEnabled = lo.ToPtr(enabled)
	m.compression.state.Status = "checking"
	m.compression.state.Error = ""
	m.compression.deriveCapabilitiesLocked()
	m.compression.mu.Unlock()
	m.compression.publish()
	m.compression.enqueueFullLocked()
	return m.compression.snapshot(), nil
}

func compressionEnableInProgress(state CompressionState) bool {
	return (state.Status == "checking" || state.Status == "compressing") &&
		state.TargetEnabled != nil && *state.TargetEnabled
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
	c.mu.Lock()
	c.state = CompressionState{Enabled: enabled, Method: method, ThresholdMiB: threshold, Status: "checking"}
	c.loaded = true
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

// errCompressionPreempted cancels a running pass that a mod action needs to get out of the way. The pass
// queues its work again instead of dropping it.
var errCompressionPreempted = errors.New("compression pass preempted")

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
	if !c.pendingFull {
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
	ctx, cancel := context.WithCancelCause(context.Background())
	c.running, c.cancel, c.done = true, cancel, make(chan struct{})
	c.resetCheckingLocked()
	c.deriveCapabilitiesLocked()
	done := c.done
	c.mu.Unlock()
	c.publish()
	go c.run(ctx, done, work)
}

// cancelCurrentWorkLocked supersedes all work for the current transition.
// The caller must hold requestMu so no new requests or watcher work can be
// queued while the worker reaches its cancellation boundary.
func (c *compressionCoordinator) cancelCurrentWorkLocked() {
	c.mu.Lock()
	c.pendingFull = false
	clear(c.pendingScopes)
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
		c.debounceTimer = nil
	}
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel(context.Canceled)
	}
	if done != nil {
		<-done
	}
}

func (c *compressionCoordinator) run(ctx context.Context, done chan struct{}, work compressionWork) {
	defer close(done)
	for {
		c.reconcile(ctx, work)
		c.mu.Lock()
		if errors.Is(context.Cause(ctx), errCompressionPreempted) {
			c.requeueLocked(work)
		}
		if ctx.Err() != nil || !c.pendingFull && len(c.pendingScopes) == 0 {
			c.running, c.cancel = false, nil
			c.deriveCapabilitiesLocked()
			c.mu.Unlock()
			c.publish()
			return
		}
		work = c.takePendingLocked()
		c.resetCheckingLocked()
		c.deriveCapabilitiesLocked()
		c.mu.Unlock()
		c.publish()
	}
}

// waitForRun blocks until the reconciliation run that is active when it is
// called stops. Runs started later by watcher events are not awaited.
func (c *compressionCoordinator) waitForRun(ctx context.Context) error {
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// requeueLocked returns the work of a preempted run to the queue so the next run finishes it.
func (c *compressionCoordinator) requeueLocked(work compressionWork) {
	if work.full {
		c.pendingFull = true
		clear(c.pendingScopes)
		return
	}
	if c.pendingFull {
		return
	}
	for _, scope := range work.scopes {
		addCompressionScope(c.pendingScopes, scope)
	}
}

func (c *compressionCoordinator) takePendingLocked() compressionWork {
	work := compressionWork{
		full:   c.pendingFull,
		scopes: make([]string, 0, len(c.pendingScopes)),
	}
	for scope := range c.pendingScopes {
		work.scopes = append(work.scopes, scope)
	}
	slices.Sort(work.scopes)
	c.pendingFull = false
	clear(c.pendingScopes)
	return work
}

func (c *compressionCoordinator) resetCheckingLocked() {
	c.state.Status = "checking"
	if c.state.TargetEnabled == nil {
		c.state.TargetEnabled = lo.ToPtr(c.state.Enabled)
	}
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
	return platform.SameOrChildPath(parent, child)
}

func (c *compressionCoordinator) reconcile(ctx context.Context, work compressionWork) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	state := c.snapshot()
	target := state.Enabled
	method, threshold := state.Method, state.ThresholdMiB
	currentRoots, err := c.roots(ctx)
	if err != nil {
		_ = c.fail(err, "resolve-importers", "", "")
		return
	}
	c.resetProgress("checking", target, method, threshold)
	workRoots := currentRoots
	if !work.full {
		workRoots = existingCompressionScopes(work.scopes, currentRoots)
	}
	logFileError := func(stage string) func(string, error) {
		return func(path string, err error) { c.logError(err, stage, method, path) }
	}

	if target {
		if method == "zstd" {
			err = errors.Join(
				restoreEnabledZstd(
					ctx,
					workRoots,
					c.addRestoreTotals,
					c.progress,
					c.markSelfChanges,
					logFileError("restore-file"),
				),
				compressDisabledZstd(
					ctx,
					workRoots,
					int64(threshold)*1024*1024,
					c.addCompressTotals,
					c.progress,
					c.markSelfChanges,
					&c.zstdDeclined,
					logFileError("compress-file"),
				),
			)
		} else {
			err = applyXpress4K(
				ctx,
				workRoots,
				c.addCompressTotals,
				c.progress,
				&c.wofOwnership,
				&c.xpressDeclined,
				c.markSelfChanges,
				logFileError("compress-file"),
			)
		}
	} else if work.full {
		if method == "zstd" {
			err = restoreAllZstd(
				ctx,
				workRoots,
				c.addRestoreTotals,
				c.progress,
				c.markSelfChanges,
				logFileError("restore-file"),
			)
		} else {
			err = restoreWOF(
				ctx,
				workRoots,
				c.addRestoreTotals,
				c.progress,
				&c.wofOwnership,
				c.markSelfChanges,
				logFileError("decompress-file"),
			)
		}
	}
	if infra.IsCancellationError(err) {
		return
	}
	if err != nil {
		_ = c.fail(err, "reconcile", method, "")
		return
	}
	c.mu.Lock()
	c.state.Status = "idle"
	c.state.TargetEnabled = nil
	c.state.CurrentFileName = ""
	c.state.Error = ""
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

func existingCompressionScopes(scopes, roots []string) []string {
	// Roots are physical while mod actions report paths through the link, so the
	// same folder can arrive under two spellings and only collapses once resolved.
	// A scope keeps its last name, so the walkers still reject a reparse point there
	// instead of leaving the root.
	resolved := map[string]struct{}{}
	for _, scope := range scopes {
		physical, err := platform.FinalEntryPath(scope)
		if err != nil {
			continue
		}
		contained := false
		for _, root := range roots {
			if pathContains(root, physical) {
				contained = true
				break
			}
		}
		if !contained {
			continue
		}
		if _, err := os.Lstat(physical); err == nil {
			addCompressionScope(resolved, physical)
		}
	}
	return unionPaths(slices.Collect(maps.Keys(resolved)))
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
	// reject reparse points found below the resulting root, including a junction
	// passed to them as the root itself.
	root, err = platform.FinalPath(root)
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
		c.logError(c.watcher.Close(), "close-watcher", "", "")
		c.watcher = nil
	}
	if c.configWatcher != nil {
		c.logError(c.configWatcher.Close(), "close-config-watcher", "", "")
		c.configWatcher = nil
	}
	if len(roots) > 0 {
		c.watcher, err = watcher.WatchTree(roots, watcher.TreeConfig{
			Depth: -1, Ops: watcher.All, Debounce: 0,
			Filter: func(event watcher.Event) bool {
				name := filepath.Base(event.Path)
				return name != legacyCompressionManifestName && !strings.Contains(name, compressionTempMarker) &&
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
	// Only an external launcher keeps this file; the built-in runtime has nothing to watch here.
	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
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
	c.selfChangeDeadline = time.Time{}
	clear(c.selfChanges)
	c.selfChangeMu.Unlock()
	if cancel != nil {
		cancel(context.Canceled)
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

// restoreBeforeEnable restores the zstd archives of a disabled folder before the caller renames it.
// It runs whatever the compression settings are, because archives left by an interrupted or switched
// method would otherwise stay in the enabled folder. A running pass is preempted so it cannot collect
// paths under the old name, and its work is queued again.
func (c *compressionCoordinator) restoreBeforeEnable(ctx context.Context, folder string) error {
	state := c.snapshot()
	zstdActive := state.Method == "zstd" && (state.Enabled || state.TargetEnabled != nil && *state.TargetEnabled)
	// Without zstd no pass writes archives, so a folder holding none needs neither a restore nor a preempted pass.
	if !zstdActive {
		found, err := hasZstdArchives(ctx, folder)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.mu.Unlock()
	if cancel != nil {
		cancel(errCompressionPreempted)
	}
	if done != nil {
		<-done
	}
	// The watcher reports physical paths. A restore marked under the link would look
	// like an outside change and could queue a pass that compresses the folder again
	// before the caller renames it.
	if physical, err := platform.FinalEntryPath(folder); err == nil {
		folder = physical
	}
	defer c.scheduleScope(folder)
	c.opMu.Lock()
	err := restoreZstdFolder(ctx, folder, c.markSelfChanges, func(path string, err error) {
		c.logError(err, "restore-file-before-enable", "zstd", path)
	})
	c.opMu.Unlock()
	if err == nil {
		return nil
	}
	// Leftover archives under another method are not a failure of the compression feature the user sees.
	if !zstdActive {
		if infra.IsCancellationError(err) {
			return err
		}
		return c.diagnostic.ReportError(
			c.owner.log, err, "Mod:compression", compressionDiagnostic("restore-before-enable", "zstd", folder),
		)
	}
	return c.fail(err, "restore-before-enable", "zstd", folder)
}

func (c *compressionCoordinator) settings() (compressionSettings, error) {
	settings, ok := c.owner.settings.(compressionSettings)
	if !ok {
		return nil, errors.New("mod compression settings are unavailable")
	}
	return settings, nil
}

func (c *compressionCoordinator) cleanupLegacyState(ctx context.Context) error {
	if c.owner.client == nil {
		return errors.New("mod compression database is unavailable")
	}
	rows, err := c.owner.client.AppState.ListByPrefix(ctx, legacyCompressionLedgerPrefix)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(rows)+2)
	keys = append(keys, legacyCompressionTransitionKey, legacyCompressionRootsKey)
	for _, row := range rows {
		if strings.HasPrefix(row.Key, legacyCompressionLedgerPrefix) {
			keys = append(keys, row.Key)
		}
	}
	return c.owner.client.AppState.ApplyBatch(ctx, nil, keys)
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

func (c *compressionCoordinator) progress(path string, size int64, _ bool) {
	c.mu.Lock()
	c.state.ProcessedFiles++
	c.state.ProcessedBytes += size
	c.state.CurrentFileName = filepath.Base(path)
	now := time.Now()
	publish := now.Sub(c.lastProgressEmit) >= compressionProgressEvery
	if publish {
		c.lastProgressEmit = now
	}
	c.mu.Unlock()
	if publish {
		c.publish()
	}
}

func (c *compressionCoordinator) markSelfChanges(paths ...string) {
	expires := time.Now().Add(compressionSelfChangeTTL)
	c.selfChangeMu.Lock()
	for _, path := range paths {
		c.selfChanges[strings.ToLower(filepath.Clean(path))] = expires
	}
	if c.selfChangeTimer == nil {
		c.scheduleSelfChangeTimerLocked()
	} else if expires.Before(c.selfChangeDeadline) {
		c.selfChangeTimer.Stop()
		c.selfChangeTimer = nil
		c.scheduleSelfChangeTimerLocked()
	}
	c.selfChangeMu.Unlock()
}

func (c *compressionCoordinator) scheduleSelfChangeTimerLocked() {
	var next time.Time
	for _, expires := range c.selfChanges {
		if next.IsZero() || expires.Before(next) {
			next = expires
		}
	}
	if next.IsZero() {
		c.selfChangeTimer = nil
		c.selfChangeDeadline = time.Time{}
		return
	}
	delay := time.Until(next)
	if delay < 0 {
		delay = 0
	}
	c.selfChangeGeneration++
	generation := c.selfChangeGeneration
	c.selfChangeDeadline = next
	c.selfChangeTimer = time.AfterFunc(delay, func() {
		c.clearExpiredSelfChanges(generation)
	})
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
	c.selfChangeDeadline = time.Time{}
	c.scheduleSelfChangeTimerLocked()
	c.selfChangeMu.Unlock()
}

func (c *compressionCoordinator) isSelfChange(path string) bool {
	now := time.Now()
	key := strings.ToLower(filepath.Clean(path))
	// Expired entries are pruned by the self-change timer, so a watcher event only looks up its own path.
	c.selfChangeMu.Lock()
	defer c.selfChangeMu.Unlock()
	expires, ok := c.selfChanges[key]
	return ok && expires.After(now)
}

// addWorkTotals reports discovered work. Only calls carrying files or bytes
// may flip the busy status, so empty scans keep the checking state.
func (c *compressionCoordinator) addCompressTotals(files int, bytes int64) {
	c.addWorkTotals("compressing", files, bytes)
}

func (c *compressionCoordinator) addRestoreTotals(files int, bytes int64) {
	c.addWorkTotals("decompressing", files, bytes)
}

func (c *compressionCoordinator) addWorkTotals(status string, files int, bytes int64) {
	c.mu.Lock()
	if files > 0 || bytes > 0 {
		c.state.Status = status
	}
	c.state.TotalFiles += files
	c.state.TotalBytes += bytes
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

func (c *compressionCoordinator) resetProgress(status string, target bool, method string, threshold int) {
	c.mu.Lock()
	c.state.Method, c.state.ThresholdMiB = method, threshold
	c.state.Status, c.state.TargetEnabled = status, lo.ToPtr(target)
	c.state.ProcessedFiles, c.state.TotalFiles = 0, 0
	c.state.ProcessedBytes, c.state.TotalBytes = 0, 0
	c.state.CurrentFileName, c.state.Error = "", ""
	c.lastProgressEmit = time.Now()
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
}

// fail records a pass-level failure in the state shown to the renderer and returns the error marked as
// reported for callers that pass it on.
func (c *compressionCoordinator) fail(err error, stage, method, path string) error {
	if infra.IsCancellationError(err) {
		return err
	}
	reported := c.diagnostic.ReportError(
		c.owner.log,
		err,
		"Mod:compression",
		compressionDiagnostic(stage, method, path),
	)
	c.mu.Lock()
	c.state.Status = "error"
	c.state.Error = "MOD_COMPRESSION_FAILED"
	c.state.TargetEnabled = nil
	c.deriveCapabilitiesLocked()
	c.mu.Unlock()
	c.publish()
	return reported
}

func (c *compressionCoordinator) logError(err error, stage, method, path string) {
	if err == nil {
		return
	}
	c.diagnostic.Report(c.owner.log, err, "Mod:compression", compressionDiagnostic(stage, method, path))
}

func compressionDiagnostic(stage, method, path string) infra.Diagnostic {
	return infra.Diagnostic{
		Operation: "compression",
		Stage:     stage,
		Fields:    map[string]any{"method": method, "path": path, "cleanup": "pending"},
	}
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
	busy := c.running || c.state.Status == "checking" || c.state.Status == "compressing" ||
		c.state.Status == "decompressing"
	interruptibleEnable := compressionEnableInProgress(c.state)
	c.state.CanToggle = !c.configuring && (!busy || interruptibleEnable)
	c.state.CanConfigure = !c.configuring && !c.state.Enabled && !busy
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

type compressionFile struct {
	path     string
	size     int64
	modified time.Time
}

// Partial downloads and editor saves are still being written; compressing one
// competes with its writer, and the final rename raises a watcher event anyway.
var compressionInProgressExtensions = map[string]struct{}{
	".ntmp": {}, ".tmp": {}, ".part": {}, ".partial": {}, ".crdownload": {}, ".download": {},
}

func isCompressionInProgressPath(path string) bool {
	_, ok := compressionInProgressExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

// compressionDeclinedFiles remembers files WOF found not worth compressing.
// Every attempt raises a watcher event but keeps the size and write time, so a
// file is retried only after one of them changes.
type compressionDeclinedFiles struct {
	mu    sync.Mutex
	files map[string]compressionFile
}

func (d *compressionDeclinedFiles) add(file compressionFile) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.files == nil {
		d.files = map[string]compressionFile{}
	}
	d.files[compressionPathKey(file.path)] = file
}

func (d *compressionDeclinedFiles) contains(file compressionFile) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := compressionPathKey(file.path)
	declined, ok := d.files[key]
	if ok && (declined.size != file.size || !declined.modified.Equal(file.modified)) {
		delete(d.files, key)
		return false
	}
	return ok
}

// retain forgets files under roots that a complete scan of roots no longer
// found, so deleted or moved mods do not stay remembered for the session.
func (d *compressionDeclinedFiles) retain(roots []string, scanned []compressionFile) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.files) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(scanned))
	for _, file := range scanned {
		seen[compressionPathKey(file.path)] = struct{}{}
	}
	for key := range d.files {
		if _, ok := seen[key]; ok {
			continue
		}
		if slices.ContainsFunc(roots, func(root string) bool { return pathContains(root, key) }) {
			delete(d.files, key)
		}
	}
}

func compressionPathKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

type compressionFileOwnership struct {
	mu    sync.Mutex
	files map[string]struct{}
}

func (o *compressionFileOwnership) add(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.files == nil {
		o.files = map[string]struct{}{}
	}
	o.files[id] = struct{}{}
}

func (o *compressionFileOwnership) contains(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.files[id]
	return ok
}

func (o *compressionFileOwnership) empty() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.files) == 0
}

func (o *compressionFileOwnership) remove(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.files, id)
}

func setCompressionTotals(files []compressionFile, setTotals func(int, int64)) {
	var totalBytes int64
	for _, file := range files {
		totalBytes += file.size
	}
	setTotals(len(files), totalBytes)
}

func walkCompressionFiles(
	ctx context.Context,
	roots []string,
	accept func(string, fs.FileInfo) bool,
) ([]compressionFile, error) {
	var files []compressionFile
	var errs []error
	for _, root := range roots {
		release, err := diskio.AcquireDir(ctx, root)
		if err != nil {
			errs = append(errs, err)
			break
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				// A mod renamed or deleted while the pass walks it has nothing left to process.
				if !errors.Is(walkErr, fs.ErrNotExist) {
					errs = append(errs, fmt.Errorf("access %q: %w", path, walkErr))
				}
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if path != root && platform.IsUnaddressableName(entry.Name()) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			info, err := entry.Info()
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
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
				files = append(files, compressionFile{path: path, size: info.Size(), modified: info.ModTime()})
			}
			return nil
		})
		release()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return files, errors.Join(errs...)
}
