package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/watcher"
)

type ToggleViewerState struct {
	IsRunning bool    `json:"isRunning"`
	Mode      *string `json:"mode"`
}

type toggleViewerSettings interface {
	GetToggleViewerAutoGenerate(context.Context) (bool, error)
	SetToggleViewerAutoGenerate(context.Context, bool) error
	GetToggleViewerHotkey(context.Context) (string, error)
}

type toggleViewerTask struct {
	mode   string
	cancel context.CancelFunc
}

type toggleViewerWatcher struct {
	watcher *watcher.Watcher
	roots   []string
}

func (t *Tools) ToggleViewerGetLogs() []string {
	t.toggleMu.Lock()
	defer t.toggleMu.Unlock()
	return append([]string(nil), t.toggleLogs...)
}

func (t *Tools) ToggleViewerGetState() ToggleViewerState {
	t.toggleMu.Lock()
	defer t.toggleMu.Unlock()
	state := ToggleViewerState{IsRunning: t.toggleTask != nil}
	if t.toggleTask != nil {
		state.Mode = stringPointer(t.toggleTask.mode)
	}
	return state
}

func (t *Tools) ToggleViewerCancelCurrentWork() bool {
	t.toggleMu.Lock()
	task := t.toggleTask
	if task != nil {
		task.cancel()
	}
	t.toggleMu.Unlock()
	if task != nil {
		t.toggleLogInfo("Requested stop for current toggle viewer task")
		return true
	}
	return false
}

func (t *Tools) ToggleViewerRunBatchGenerate(ctx context.Context) error {
	task, taskCtx, err := t.beginToggleViewerTask(ctx, "generate")
	if err != nil {
		return err
	}
	defer t.finishToggleViewerTask(task)
	if err := t.scanAllToggleViewerArtifacts(taskCtx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	settings, ok := t.settings.(toggleViewerSettings)
	if ok {
		enabled, getErr := settings.GetToggleViewerAutoGenerate(ctx)
		if getErr == nil && enabled {
			return t.startToggleViewerWatcher(ctx, false)
		}
	}
	return nil
}

func (t *Tools) ToggleViewerRunBatchDelete(ctx context.Context) error {
	if t.ToggleViewerGetState().IsRunning {
		return contractError("Toggle viewer task is already running")
	}
	if settings, ok := t.settings.(toggleViewerSettings); ok {
		enabled, getErr := settings.GetToggleViewerAutoGenerate(ctx)
		if getErr != nil {
			return getErr
		}
		if enabled {
			if err := settings.SetToggleViewerAutoGenerate(ctx, false); err != nil {
				return err
			}
		}
	}
	task, taskCtx, err := t.beginToggleViewerTask(ctx, "delete")
	if err != nil {
		return err
	}
	defer t.finishToggleViewerTask(task)
	if err := t.stopToggleViewerWatcher(false); err != nil {
		return err
	}
	modsPaths, err := t.enabledToggleViewerModsPaths(taskCtx)
	if err != nil {
		return err
	}
	if len(modsPaths) == 0 {
		t.toggleLogInfo("Batch delete skipped: no enabled importer mods folder")
		return nil
	}
	roots := expandToggleRootAliases(modsPaths)
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	records, err := client.ToggleViewerArtifacts.List(taskCtx)
	if err != nil {
		return err
	}
	deletedFiles, deletedRecords, targetRecords := 0, 0, 0
	for _, record := range records {
		if !pathInAnyToggleRoot(record.TargetIniPath, roots) {
			continue
		}
		targetRecords++
		if err := taskCtx.Err(); err != nil {
			t.toggleLogInfo("Batch delete cancelled")
			return nil
		}
		hasError := false
		for _, artifactPath := range toggleManagedArtifactPaths(record.TargetIniPath) {
			removed, removeErr := removeToggleArtifactFile(artifactPath)
			if removeErr != nil {
				hasError = true
				t.toggleLogError(fmt.Sprintf("Failed to delete artifact file %s: %s", artifactPath, removeErr))
			} else if removed {
				deletedFiles++
			}
		}
		if hasError {
			t.toggleLogError("Keeping artifact record due to delete error: " + record.TargetIniPath)
			continue
		}
		if err := client.ToggleViewerArtifacts.DeleteByIDAndTargetIniPath(taskCtx, record.ID, record.TargetIniPath); err != nil {
			return err
		}
		deletedRecords++
	}
	t.toggleLogInfo(fmt.Sprintf("Batch delete completed. deletedFiles=%d, deletedRecords=%d, targetRecords=%d", deletedFiles, deletedRecords, targetRecords))
	return nil
}

func (t *Tools) ToggleViewerApplyHotkeyToArtifacts(ctx context.Context, hotkey string) error {
	hotkey = strings.TrimSpace(hotkey)
	if hotkey == "" {
		hotkey = defaultToggleViewerHotkey
	}
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil {
		return err
	}
	updated := 0
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		expectedINI := filepath.Join(filepath.Dir(record.TargetIniPath), "toggle-viewer.ini")
		if !samePathFold(expectedINI, record.ToggleIniPath) {
			t.toggleLogError("Skipped untrusted toggle viewer artifact path: " + record.ToggleIniPath)
			continue
		}
		current, readErr := os.ReadFile(expectedINI)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			t.toggleLogError(fmt.Sprintf("Failed to apply hotkey to %s: %s", expectedINI, readErr))
			continue
		}
		next := replaceToggleViewerHotkey(string(current), hotkey)
		if next == string(current) {
			continue
		}
		if writeErr := os.WriteFile(expectedINI, []byte(next), 0o600); writeErr != nil {
			t.toggleLogError(fmt.Sprintf("Failed to apply hotkey to %s: %s", expectedINI, writeErr))
			continue
		}
		if err := client.ToggleViewerArtifacts.UpdateHashes(ctx, record.ID, toggleContentSHA(next), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		updated++
	}
	t.toggleLogInfo(fmt.Sprintf("Applied toggle viewer hotkey to artifacts: updated=%d", updated))
	return nil
}

// StartToggleViewerWatcher starts recursive importer watchers and an initial scan.
//
//wails:ignore
func (t *Tools) StartToggleViewerWatcher(ctx context.Context) error {
	return t.startToggleViewerWatcher(ctx, true)
}

func (t *Tools) startToggleViewerWatcher(ctx context.Context, initialScan bool) error {
	settings, ok := t.settings.(toggleViewerSettings)
	if !ok || t.xxmi == nil {
		return nil
	}
	enabled, err := settings.GetToggleViewerAutoGenerate(ctx)
	if err != nil || !enabled {
		return err
	}
	modsPaths, err := t.enabledToggleViewerModsPaths(ctx)
	if err != nil {
		return err
	}
	if err := t.stopToggleViewerWatcher(false); err != nil {
		return err
	}
	if len(modsPaths) == 0 {
		t.toggleLogInfo("No mods folder found for enabled importers")
		return nil
	}
	watcher, err := newToggleViewerWatcher(modsPaths, t.requestToggleViewerScan)
	if err != nil {
		return err
	}
	t.toggleMu.Lock()
	if t.toggleClosing {
		t.toggleMu.Unlock()
		_ = watcher.Close()
		return errors.New("tools service is shutting down")
	}
	t.toggleWatcher = watcher
	t.toggleMu.Unlock()
	t.toggleLogInfo(fmt.Sprintf("Started toggle viewer watcher (%d)", len(modsPaths)))
	if initialScan {
		t.requestToggleViewerScan()
	}
	return nil
}

// StopToggleViewerWatcher stops recursive importer watchers.
//
//wails:ignore
func (t *Tools) StopToggleViewerWatcher() error { return t.stopToggleViewerWatcher(true) }

func (t *Tools) stopToggleViewerWatcher(cancelScan bool) error {
	t.toggleMu.Lock()
	watcher := t.toggleWatcher
	t.toggleWatcher = nil
	t.togglePending = false
	task := t.toggleTask
	if cancelScan && task != nil && task.mode == "scan" {
		task.cancel()
	}
	t.toggleMu.Unlock()
	if watcher == nil {
		return nil
	}
	err := watcher.Close()
	t.toggleLogInfo(fmt.Sprintf("Stopped toggle viewer watcher (%d)", len(watcher.roots)))
	return err
}

func (t *Tools) requestToggleViewerScan() {
	task, ctx, err := t.beginToggleViewerTask(context.Background(), "scan")
	if err != nil {
		t.toggleMu.Lock()
		if t.toggleWatcher != nil && !t.toggleClosing {
			t.togglePending = true
		}
		t.toggleMu.Unlock()
		return
	}
	go func() {
		defer t.finishToggleViewerTask(task)
		if err := t.scanAllToggleViewerArtifacts(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.toggleLogError("Initial toggle viewer scan failed: " + err.Error())
		}
	}()
}

func (t *Tools) scanAllToggleViewerArtifacts(ctx context.Context) error {
	modsPaths, err := t.enabledToggleViewerModsPaths(ctx)
	if err != nil {
		return err
	}
	if len(modsPaths) == 0 {
		t.toggleLogInfo("Toggle viewer scan skipped: no enabled importer mods folder")
		return nil
	}
	hotkey := defaultToggleViewerHotkey
	if settings, ok := t.settings.(toggleViewerSettings); ok {
		if configured, getErr := settings.GetToggleViewerHotkey(ctx); getErr == nil && strings.TrimSpace(configured) != "" {
			hotkey = configured
		}
	}
	var artifacts []toggleViewerArtifact
	seen := make(map[string]bool)
	for _, modsPath := range modsPaths {
		iniPaths, scanErr := scanToggleViewerINIs(ctx, modsPath)
		if scanErr != nil {
			return scanErr
		}
		for _, iniPath := range iniPaths {
			content, readErr := os.ReadFile(iniPath)
			if readErr != nil {
				t.toggleLogError(fmt.Sprintf("Failed to read ini %s: %s", iniPath, readErr))
				continue
			}
			if artifact := generateToggleViewerArtifact(iniPath, string(content), hotkey); artifact != nil {
				artifacts = append(artifacts, *artifact)
				seen[artifact.targetINIPath] = true
			}
		}
	}
	if err := t.persistToggleViewerArtifacts(ctx, artifacts); err != nil {
		return err
	}
	if err := t.deleteStaleToggleViewerRecords(ctx, seen); err != nil {
		return err
	}
	t.toggleLogInfo(fmt.Sprintf("Scan complete. matched=%d, importers=%d", len(seen), len(modsPaths)))
	return nil
}

func (t *Tools) persistToggleViewerArtifacts(ctx context.Context, artifacts []toggleViewerArtifact) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeToggleIfChanged(artifact.toggleTXTPath, artifact.txtContent); err != nil {
			return err
		}
		if err := writeToggleIfChanged(artifact.toggleINIPath, artifact.iniContent); err != nil {
			return err
		}
		id, err := newToolsID()
		if err != nil {
			return err
		}
		if err := client.ToggleViewerArtifacts.Upsert(ctx, db.ToggleViewerArtifactRow{
			ID: id, TargetIniPath: artifact.targetINIPath, ToggleTxtPath: artifact.toggleTXTPath,
			ToggleIniPath: artifact.toggleINIPath, ToggleTxtHash: artifact.toggleTXTHash,
			ToggleIniHash: artifact.toggleINIHash, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tools) deleteStaleToggleViewerRecords(ctx context.Context, seen map[string]bool) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if seen[record.TargetIniPath] {
			continue
		}
		for _, artifactPath := range toggleManagedArtifactPaths(record.TargetIniPath) {
			if _, err := removeToggleArtifactFile(artifactPath); err != nil {
				t.toggleLogError(fmt.Sprintf("Failed to delete managed artifact file %s: %s", artifactPath, err))
			}
		}
		if err := client.ToggleViewerArtifacts.DeleteByIDAndTargetIniPath(ctx, record.ID, record.TargetIniPath); err != nil {
			return err
		}
		t.toggleLogInfo("Removed stale toggle-viewer artifact record: " + record.TargetIniPath)
	}
	return nil
}

func (t *Tools) enabledToggleViewerModsPaths(ctx context.Context) ([]string, error) {
	if t.xxmi == nil {
		return nil, nil
	}
	importers, err := t.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, importer := range importers {
		modsPath := filepath.Join(importer.ImporterFolder, "mods")
		if info, statErr := os.Stat(modsPath); statErr == nil && info.IsDir() {
			paths = append(paths, modsPath)
		}
	}
	return paths, nil
}

func (t *Tools) beginToggleViewerTask(parent context.Context, mode string) (*toggleViewerTask, context.Context, error) {
	t.toggleMu.Lock()
	defer t.toggleMu.Unlock()
	if t.toggleClosing {
		return nil, nil, errors.New("tools service is shutting down")
	}
	if t.toggleTask != nil {
		return nil, nil, contractError("Toggle viewer task is already running")
	}
	ctx, cancel := context.WithCancel(parent)
	task := &toggleViewerTask{mode: mode, cancel: cancel}
	t.toggleTask = task
	t.toggleWG.Add(1)
	return task, ctx, nil
}

func (t *Tools) finishToggleViewerTask(task *toggleViewerTask) {
	if task == nil {
		return
	}
	task.cancel()
	t.toggleMu.Lock()
	if t.toggleTask == task {
		t.toggleTask = nil
	}
	pending := t.togglePending && t.toggleWatcher != nil && !t.toggleClosing
	t.togglePending = false
	t.toggleMu.Unlock()
	t.toggleWG.Done()
	if pending {
		t.requestToggleViewerScan()
	}
}

func (t *Tools) toggleLogInfo(message string)  { t.addToggleLog("INFO", message) }
func (t *Tools) toggleLogError(message string) { t.addToggleLog("ERROR", message) }

func (t *Tools) addToggleLog(level, message string) {
	entry := fmt.Sprintf("[%s] [%s] %s", time.Now().UTC().Format(time.RFC3339Nano), level, message)
	t.toggleMu.Lock()
	t.toggleLogs = append(t.toggleLogs, entry)
	if len(t.toggleLogs) > 30 {
		t.toggleLogs = append([]string(nil), t.toggleLogs[len(t.toggleLogs)-30:]...)
	}
	logs := append([]string(nil), t.toggleLogs...)
	t.toggleMu.Unlock()
	if t.log != nil {
		if level == "ERROR" {
			t.log.Error(message, "ToggleViewer")
		} else {
			t.log.Info(message, "ToggleViewer")
		}
	}
	if t.emit != nil {
		t.emit("setting:xxmi:toggleViewerLogs", logs)
	}
}

func (t *Tools) shutdownToggleViewer() error {
	if t == nil {
		return nil
	}
	t.toggleMu.Lock()
	t.toggleClosing = true
	task := t.toggleTask
	watcher := t.toggleWatcher
	t.toggleWatcher = nil
	if task != nil {
		task.cancel()
	}
	t.toggleMu.Unlock()
	var err error
	if watcher != nil {
		err = watcher.Close()
	}
	done := make(chan struct{})
	go func() {
		t.toggleWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return err
	case <-time.After(5 * time.Second):
		return errors.Join(err, errors.New("timed out waiting for toggle viewer task to stop"))
	}
}

func newToggleViewerWatcher(roots []string, onScan func()) (*toggleViewerWatcher, error) {
	service, err := watcher.WatchTree(roots, watcher.TreeConfig{
		Depth:    -1,
		Ops:      watcher.All,
		Debounce: 500 * time.Millisecond,
		Filter: func(event watcher.Event) bool {
			lower := strings.ToLower(filepath.Base(event.Path))
			return lower != "toggle-viewer.ini" && lower != "toggle-viewer.txt"
		},
	}, func(watcher.Event) {
		if onScan != nil {
			onScan()
		}
	})
	if err != nil {
		return nil, err
	}
	return &toggleViewerWatcher{watcher: service, roots: append([]string(nil), roots...)}, nil
}

func (w *toggleViewerWatcher) Close() error {
	if w == nil {
		return nil
	}
	return w.watcher.Close()
}

func scanToggleViewerINIs(ctx context.Context, root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		if strings.EqualFold(filepath.Ext(name), ".ini") && name != "toggle-viewer.ini" && !strings.HasPrefix(name, "disabled") {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

func writeToggleIfChanged(path, content string) error {
	if current, err := os.ReadFile(path); err == nil && string(current) == content {
		return nil
	}
	// Match the Electron implementation: write generated artifacts through the
	// existing file instead of replacing its directory entry. XXMI can keep INI
	// files open without delete sharing, which makes MoveFileEx fail even though
	// a normal write is permitted.
	return os.WriteFile(path, []byte(content), 0o600)
}

func toggleManagedArtifactPaths(targetINIPath string) []string {
	dir := filepath.Dir(targetINIPath)
	return []string{filepath.Join(dir, "toggle-viewer.txt"), filepath.Join(dir, "toggle-viewer.ini")}
}

func removeToggleArtifactFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("artifact path is not a regular file")
	}
	return true, os.Remove(path)
}

func expandToggleRootAliases(roots []string) []string {
	set := make(map[string]bool)
	for _, root := range roots {
		resolved, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		set[resolved] = true
		if real, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
			set[real] = true
		}
	}
	out := make([]string, 0, len(set))
	for root := range set {
		out = append(out, root)
	}
	return out
}

func pathInAnyToggleRoot(path string, roots []string) bool {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range roots {
		if sameOrChildPath(root, resolved) {
			return true
		}
	}
	return false
}
