package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/watcher"
)

const (
	fixInspectionChangedEvent = "tools:fix-inspections"
	fixInspectionSettleDelay  = 800 * time.Millisecond
)

var fixInspectionDisabledPrefixRE = regexp.MustCompile(`(?i)^(?:disabled[\s_]*)+[\s_]+`)

// Fix inspection records are process-local and intentionally never persisted.
type FixInspectionRecord struct {
	ModPath     string              `json:"modPath"`
	DisplayName string              `json:"displayName"`
	Result      FixInspectionResult `json:"result"`
}

// Revision lets renderers reject an initial snapshot that loses a race with a watcher event.
type FixInspectionSnapshot struct {
	Revision    uint64                `json:"revision"`
	Inspections []FixInspectionRecord `json:"inspections"`
}

type trackedFixInspection struct {
	record         FixInspectionRecord
	identity       fs.FileInfo
	contentWatcher *watcher.Watcher
	parentWatcher  *watcher.Watcher
}

// Only actionable results are retained and watched for changes.
func (t *Tools) InspectModForFix(ctx context.Context, modPath, importer string) (*FixInspectionResult, error) {
	if t == nil || t.fixInspectors == nil {
		return &FixInspectionResult{Importer: strings.ToUpper(importer)}, nil
	}

	resolved, err := filepath.Abs(modPath)
	if err != nil {
		return nil, fmt.Errorf("resolve fix inspection target %q: %w", modPath, err)
	}
	resolved = filepath.Clean(resolved)

	t.fixInspectionRunMu.Lock()
	defer t.fixInspectionRunMu.Unlock()

	result, err := t.fixInspectors.Inspect(ctx, resolved, importer)
	if err != nil {
		return nil, err
	}
	if !result.NeedsFix {
		changed, stopped := t.removeFixInspection(resolved)
		t.logError(closeFixInspectionWatchers(stopped), "FixInspector.stopWatch")
		if changed {
			t.emitFixInspectionSnapshot()
		}
		return result, nil
	}

	record := FixInspectionRecord{
		ModPath:     resolved,
		DisplayName: filepath.Base(resolved),
		Result:      cloneFixInspectionResult(*result),
	}
	changed, watchErr := t.storeFixInspection(record)
	if watchErr != nil {
		t.logError(watchErr, "FixInspector.watch")
	}
	refreshed, stopped := t.refreshFixInspectionLocked(ctx, fixInspectionKey(resolved), result)
	changed = changed || refreshed
	t.logError(closeFixInspectionWatchers(stopped), "FixInspector.stopWatch")
	if changed {
		t.emitFixInspectionSnapshot()
	}
	return result, nil
}

// Refresh also catches ancestor moves that Windows cannot report through an open child handle.
func (t *Tools) RefreshFixInspections(ctx context.Context) FixInspectionSnapshot {
	if t == nil {
		return FixInspectionSnapshot{Inspections: []FixInspectionRecord{}}
	}
	t.fixInspectionRunMu.Lock()
	changed, stopped := t.refreshAllFixInspectionsLocked(ctx)
	t.fixInspectionRunMu.Unlock()
	t.logError(closeFixInspectionWatchers(stopped), "FixInspector.stopWatch")
	if changed {
		t.emitFixInspectionSnapshot()
	}
	return t.fixInspectionSnapshot()
}

//wails:ignore
func (t *Tools) QueueFixInspections(paths []string) {
	if t == nil {
		return
	}
	targets := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		key := fixInspectionKey(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, path)
	}
	if len(targets) == 0 {
		return
	}

	t.fixInspectionMu.Lock()
	if t.fixInspectionClosed {
		t.fixInspectionMu.Unlock()
		return
	}
	t.fixInspectionWG.Add(1)
	ctx := t.fixInspectionCtx
	t.fixInspectionMu.Unlock()

	go func() {
		defer t.fixInspectionWG.Done()
		for _, path := range targets {
			if ctx.Err() != nil {
				return
			}
			if err := t.inspectAddedModForFix(ctx, path); err != nil {
				if ctx.Err() != nil {
					return
				}
				t.logError(fmt.Errorf("inspect added mod %q: %w", path, err), "FixInspector.addedMod")
			}
		}
	}()
}

func (t *Tools) inspectAddedModForFix(ctx context.Context, modPath string) error {
	if settings, ok := t.settings.(FixInspectionSettings); ok {
		enabled, err := settings.GetAutoInspectFix(ctx)
		if err != nil {
			return fmt.Errorf("read automatic fix inspection setting: %w", err)
		}
		if !enabled {
			return nil
		}
	}

	importer, err := t.resolveFixInspectionImporter(ctx, modPath)
	if err != nil {
		return err
	}
	_, err = t.InspectModForFix(ctx, modPath, importer)
	return err
}

func (t *Tools) resolveFixInspectionImporter(ctx context.Context, modPath string) (string, error) {
	client, err := t.requireClient()
	if err != nil {
		return "", err
	}
	target, err := filepath.EvalSymlinks(modPath)
	if err != nil {
		return "", fmt.Errorf("resolve added mod path %q: %w", modPath, err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve absolute added mod path %q: %w", target, err)
	}

	games, err := client.GamePaths.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list game paths for fix inspection: %w", err)
	}
	var importer string
	bestRootLength := -1
	for _, game := range games {
		if game.Importer == nil || strings.TrimSpace(*game.Importer) == "" {
			continue
		}
		root, resolveErr := filepath.EvalSymlinks(game.ModFolderPath)
		if resolveErr != nil {
			continue
		}
		root, resolveErr = filepath.Abs(root)
		if resolveErr != nil || !sameOrChildPath(root, target) || len(root) <= bestRootLength {
			continue
		}
		importer = *game.Importer
		bestRootLength = len(root)
	}
	if importer == "" {
		return "", fmt.Errorf("added mod path is outside configured game folders: %s", target)
	}
	return importer, nil
}

func (t *Tools) storeFixInspection(record FixInspectionRecord) (bool, error) {
	key := fixInspectionKey(record.ModPath)
	t.fixInspectionMu.Lock()
	if t.fixInspectionClosed {
		t.fixInspectionMu.Unlock()
		return false, nil
	}
	if current := t.fixInspections[key]; current != nil {
		changed := !equalFixInspectionRecord(current.record, record)
		current.record = record
		if changed {
			t.fixInspectionRevision++
		}
		t.fixInspectionMu.Unlock()
		return changed, nil
	}
	t.fixInspectionMu.Unlock()

	identity, identityErr := fixInspectionIdentity(record.ModPath)
	tracked := &trackedFixInspection{record: record, identity: identity}
	contentWatcher, contentErr := watcher.WatchTree(
		[]string{record.ModPath},
		watcher.TreeConfig{
			Depth:    -1,
			Ops:      watcher.All,
			Debounce: fixInspectionSettleDelay,
			OnError: func(err error) {
				t.logError(fmt.Errorf("watch fix inspection contents %q: %w", record.ModPath, err), "FixInspector.watch")
			},
		},
		func(watcher.Event) { t.queueFixInspectionRefresh(key) },
	)
	tracked.contentWatcher = contentWatcher

	parentWatcher, parentErr := watcher.WatchTree(
		[]string{filepath.Dir(record.ModPath)},
		watcher.TreeConfig{
			Depth:    0,
			Ops:      watcher.Remove | watcher.Rename,
			Debounce: fixInspectionSettleDelay,
			Filter: func(event watcher.Event) bool {
				return watcher.SamePath(event.Path, record.ModPath)
			},
			OnError: func(err error) {
				t.logError(fmt.Errorf("watch fix inspection parent for %q: %w", record.ModPath, err), "FixInspector.watch")
			},
		},
		func(watcher.Event) { t.queueFixInspectionRefresh(key) },
	)
	tracked.parentWatcher = parentWatcher

	t.fixInspectionMu.Lock()
	if t.fixInspectionClosed {
		t.fixInspectionMu.Unlock()
		closeErr := closeFixInspectionWatchers([]*trackedFixInspection{tracked})
		return false, errors.Join(identityErr, contentErr, parentErr, closeErr)
	}
	if current := t.fixInspections[key]; current != nil {
		changed := !equalFixInspectionRecord(current.record, record)
		current.record = record
		if changed {
			t.fixInspectionRevision++
		}
		t.fixInspectionMu.Unlock()
		closeErr := closeFixInspectionWatchers([]*trackedFixInspection{tracked})
		return changed, errors.Join(identityErr, contentErr, parentErr, closeErr)
	}
	t.fixInspections[key] = tracked
	t.fixInspectionRevision++
	t.fixInspectionMu.Unlock()
	return true, errors.Join(identityErr, contentErr, parentErr)
}

func (t *Tools) queueFixInspectionRefresh(key string) {
	t.fixInspectionMu.Lock()
	if t.fixInspectionClosed || t.fixInspections[key] == nil {
		t.fixInspectionMu.Unlock()
		return
	}
	t.fixInspectionWG.Add(1)
	ctx := t.fixInspectionCtx
	t.fixInspectionMu.Unlock()

	go func() {
		defer t.fixInspectionWG.Done()
		t.fixInspectionRunMu.Lock()
		changed, stopped := t.refreshFixInspectionLocked(ctx, key, nil)
		t.fixInspectionRunMu.Unlock()
		t.logError(closeFixInspectionWatchers(stopped), "FixInspector.stopWatch")
		if changed {
			t.emitFixInspectionSnapshot()
		}
	}()
}

func (t *Tools) refreshAllFixInspectionsLocked(ctx context.Context) (bool, []*trackedFixInspection) {
	t.fixInspectionMu.Lock()
	keys := make([]string, 0, len(t.fixInspections))
	for key := range t.fixInspections {
		keys = append(keys, key)
	}
	t.fixInspectionMu.Unlock()

	var changed bool
	var stopped []*trackedFixInspection
	for _, key := range keys {
		itemChanged, itemStopped := t.refreshFixInspectionLocked(ctx, key, nil)
		changed = changed || itemChanged
		stopped = append(stopped, itemStopped...)
		if ctx.Err() != nil {
			break
		}
	}
	return changed, stopped
}

func (t *Tools) refreshFixInspectionLocked(ctx context.Context, key string, inspected *FixInspectionResult) (bool, []*trackedFixInspection) {
	t.fixInspectionMu.Lock()
	if t.fixInspectionClosed {
		t.fixInspectionMu.Unlock()
		return false, nil
	}
	tracked := t.fixInspections[key]
	if tracked == nil {
		t.fixInspectionMu.Unlock()
		return false, nil
	}
	record := cloneFixInspectionRecord(tracked.record)
	identity := tracked.identity
	t.fixInspectionMu.Unlock()

	var changed bool
	var stopped []*trackedFixInspection
	info, err := os.Stat(record.ModPath)
	if err != nil || !info.IsDir() {
		if err != nil && errors.Is(err, fs.ErrNotExist) {
			renamedPath := findDisabledFixInspectionRename(record.ModPath, identity)
			if renamedPath == "" {
				return t.removeFixInspection(record.ModPath)
			}
			_, stopped = t.removeFixInspection(record.ModPath)
			record.ModPath = renamedPath
			record.DisplayName = filepath.Base(renamedPath)
			_, watchErr := t.storeFixInspection(record)
			t.logError(watchErr, "FixInspector.watch")
			key = fixInspectionKey(renamedPath)
			changed = true
		} else {
			if err == nil {
				return t.removeFixInspection(record.ModPath)
			}
			t.logError(fmt.Errorf("stat fix inspection target %q: %w", record.ModPath, err), "FixInspector.refresh")
			return false, nil
		}
	}

	result := inspected
	if result == nil {
		result, err = t.fixInspectors.Inspect(ctx, record.ModPath, record.Result.Importer)
		if err != nil {
			if ctx.Err() != nil {
				return changed, stopped
			}
			if errors.Is(err, fs.ErrNotExist) {
				removed, removedWatchers := t.removeFixInspection(record.ModPath)
				return changed || removed, append(stopped, removedWatchers...)
			}
			t.logError(fmt.Errorf("refresh fix inspection target %q: %w", record.ModPath, err), "FixInspector.refresh")
			return changed, stopped
		}
	}
	if !result.NeedsFix {
		removed, removedWatchers := t.removeFixInspection(record.ModPath)
		return changed || removed, append(stopped, removedWatchers...)
	}

	record.Result = cloneFixInspectionResult(*result)
	t.fixInspectionMu.Lock()
	current := t.fixInspections[key]
	if t.fixInspectionClosed || current == nil {
		t.fixInspectionMu.Unlock()
		return changed, stopped
	}
	recordChanged := !equalFixInspectionRecord(current.record, record)
	if recordChanged {
		current.record = record
		t.fixInspectionRevision++
	}
	t.fixInspectionMu.Unlock()
	return changed || recordChanged, stopped
}

func findDisabledFixInspectionRename(previousPath string, identity fs.FileInfo) string {
	if identity == nil {
		return ""
	}
	entries, err := os.ReadDir(filepath.Dir(previousPath))
	if err != nil {
		return ""
	}
	previousName := filepath.Base(previousPath)
	for _, entry := range entries {
		if !entry.IsDir() || !isDisabledFixInspectionRename(previousName, entry.Name()) {
			continue
		}
		candidate := filepath.Join(filepath.Dir(previousPath), entry.Name())
		info, statErr := os.Stat(candidate)
		if statErr == nil && info.IsDir() && os.SameFile(identity, info) {
			return candidate
		}
	}
	return ""
}

func fixInspectionIdentity(path string) (fs.FileInfo, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, statErr := directory.Stat()
	return info, errors.Join(statErr, directory.Close())
}

func isDisabledFixInspectionRename(previousName, candidateName string) bool {
	previousName = strings.TrimSpace(previousName)
	candidateName = strings.TrimSpace(candidateName)
	previousDisabled := fixInspectionDisabledPrefixRE.MatchString(previousName)
	candidateDisabled := fixInspectionDisabledPrefixRE.MatchString(candidateName)
	if !previousDisabled && !candidateDisabled {
		return false
	}
	previousBase := strings.TrimSpace(fixInspectionDisabledPrefixRE.ReplaceAllString(previousName, ""))
	candidateBase := strings.TrimSpace(fixInspectionDisabledPrefixRE.ReplaceAllString(candidateName, ""))
	return previousBase != "" && strings.EqualFold(previousBase, candidateBase)
}

func (t *Tools) removeFixInspection(modPath string) (bool, []*trackedFixInspection) {
	key := fixInspectionKey(modPath)
	t.fixInspectionMu.Lock()
	tracked := t.fixInspections[key]
	if tracked != nil {
		delete(t.fixInspections, key)
		t.fixInspectionRevision++
	}
	t.fixInspectionMu.Unlock()
	if tracked == nil {
		return false, nil
	}
	return true, []*trackedFixInspection{tracked}
}

func (t *Tools) fixInspectionSnapshot() FixInspectionSnapshot {
	if t == nil {
		return FixInspectionSnapshot{Inspections: []FixInspectionRecord{}}
	}
	t.fixInspectionMu.Lock()
	defer t.fixInspectionMu.Unlock()
	items := make([]FixInspectionRecord, 0, len(t.fixInspections))
	for _, tracked := range t.fixInspections {
		items = append(items, cloneFixInspectionRecord(tracked.record))
	}
	slices.SortFunc(items, func(left, right FixInspectionRecord) int {
		return strings.Compare(strings.ToLower(left.ModPath), strings.ToLower(right.ModPath))
	})
	return FixInspectionSnapshot{Revision: t.fixInspectionRevision, Inspections: items}
}

func (t *Tools) emitFixInspectionSnapshot() {
	t.emitEvent(fixInspectionChangedEvent, t.fixInspectionSnapshot())
}

func (t *Tools) shutdownFixInspections() error {
	if t == nil {
		return nil
	}
	t.fixInspectionMu.Lock()
	t.fixInspectionClosed = true
	tracked := make([]*trackedFixInspection, 0, len(t.fixInspections))
	for _, item := range t.fixInspections {
		tracked = append(tracked, item)
	}
	t.fixInspections = make(map[string]*trackedFixInspection)
	cancel := t.fixInspectionCancel
	t.fixInspectionMu.Unlock()

	cancel()
	err := closeFixInspectionWatchers(tracked)
	t.fixInspectionWG.Wait()
	t.fixInspectionRunMu.Lock()
	defer t.fixInspectionRunMu.Unlock()
	return err
}

func closeFixInspectionWatchers(tracked []*trackedFixInspection) error {
	var result error
	for _, item := range tracked {
		if item == nil {
			continue
		}
		if item.contentWatcher != nil {
			result = errors.Join(result, item.contentWatcher.Close())
		}
		if item.parentWatcher != nil {
			result = errors.Join(result, item.parentWatcher.Close())
		}
	}
	return result
}

func fixInspectionKey(path string) string {
	resolved, err := filepath.Abs(path)
	if err == nil {
		path = resolved
	}
	return strings.ToLower(filepath.Clean(path))
}

func cloneFixInspectionRecord(record FixInspectionRecord) FixInspectionRecord {
	record.Result = cloneFixInspectionResult(record.Result)
	return record
}

func cloneFixInspectionResult(result FixInspectionResult) FixInspectionResult {
	result.Details = slices.Clone(result.Details)
	result.AffectedFiles = slices.Clone(result.AffectedFiles)
	return result
}

func equalFixInspectionRecord(left, right FixInspectionRecord) bool {
	return left.ModPath == right.ModPath &&
		left.DisplayName == right.DisplayName &&
		left.Result.NeedsFix == right.Result.NeedsFix &&
		left.Result.Importer == right.Result.Importer &&
		left.Result.ToolName == right.Result.ToolName &&
		left.Result.Summary == right.Result.Summary &&
		left.Result.ActionTool == right.Result.ActionTool &&
		slices.Equal(left.Result.Details, right.Result.Details) &&
		slices.Equal(left.Result.AffectedFiles, right.Result.AffectedFiles)
}
