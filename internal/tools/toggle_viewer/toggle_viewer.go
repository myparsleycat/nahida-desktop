package toggleviewer

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/watcher"
)

const (
	modeGenerate = "generate"
	modeDelete   = "delete"
	modeScan     = "scan"
	maxLogs      = 30

	// tempPrefix marks in-flight artifact replacements, which the watcher must not rescan for.
	tempPrefix        = ".toggle-viewer-"
	defaultRetryDelay = time.Minute
)

type treeWatcher struct {
	watcher *watcher.Watcher
	roots   []string
}

type sourceINI struct {
	path string
	info fs.FileInfo
}

// cachedSource is the parse result of a source INI, valid while its size and modification time are
// unchanged; source is nil when the INI does not qualify.
type cachedSource struct {
	size        int64
	modTime     time.Time
	source      *toggleViewerSource
	missingHash bool
}

func (s *Service) Logs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.logs...)
}

func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := State{IsRunning: s.task != nil}
	if s.task != nil {
		mode := s.task.mode
		state.Mode = &mode
	}
	return state
}

func (s *Service) CancelCurrentWork() bool {
	s.mu.Lock()
	current := s.task
	if current != nil {
		current.cancel()
	}
	s.mu.Unlock()
	if current == nil {
		return false
	}
	s.logInfo("Requested stop for current toggle viewer task")
	return true
}

func (s *Service) RunBatchGenerate(ctx context.Context) error {
	current, taskCtx, err := s.beginTask(ctx, modeGenerate)
	if err != nil {
		return err
	}
	// A batch rereads every INI, so an edit that kept the size and modification time is still picked up.
	s.sources = nil
	result, err := s.scanAll(taskCtx)
	cancelled := taskCtx.Err() != nil
	s.finishTask(current)
	if cancelled {
		s.logInfo("Batch generate cancelled")
		return nil
	}
	if err != nil {
		return s.reportError(err, "batch-generate")
	}
	if len(result.skipped) > 0 {
		s.logInfo(skippedSummary(result.skipped))
	}

	// The batch scan already covered every importer, so the watcher starts without another one. A
	// watcher already on the same folders is kept, because a restart drops the changes it has not
	// reported yet and cancels the rescan started for changes made during the batch.
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	watching, err := s.watcherCurrentLocked(ctx)
	if err != nil {
		return err
	}
	if !watching {
		if err := s.startWatcherLocked(ctx, false); err != nil {
			return err
		}
	}
	if result.retry {
		s.scheduleRetry()
	}
	return nil
}

// skippedSummary names a few skipped INIs by folder and file, because the UI log keeps few entries.
func skippedSummary(skipped []string) string {
	const shown = 5
	names := make([]string, 0, min(len(skipped), shown))
	for _, path := range skipped[:min(len(skipped), shown)] {
		names = append(names, filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path)))
	}
	summary := "Skipped INIs with toggle keys but no position hash: " + strings.Join(names, ", ")
	if len(skipped) > shown {
		summary += fmt.Sprintf(" and %d more", len(skipped)-shown)
	}
	return summary
}

func (s *Service) RunBatchDelete(ctx context.Context) error {
	if s.State().IsRunning {
		return infra.ContractError("Toggle viewer task is already running")
	}
	if s.settings != nil {
		enabled, err := s.settings.GetToggleViewerAutoGenerate(ctx)
		if err != nil {
			return err
		}
		if enabled {
			if err := s.settings.SetToggleViewerAutoGenerate(ctx, false); err != nil {
				return err
			}
		}
	}

	// Close the watcher before taking the task: importer maintenance holds watchMu while it
	// waits for tasks, so a task must never wait for watchMu.
	if err := s.StopWatcher(); err != nil {
		return s.reportError(err, "batch-delete")
	}
	current, taskCtx, err := s.beginTask(ctx, modeDelete)
	if err != nil {
		return err
	}
	defer s.finishTask(current)
	if err := s.deleteAll(taskCtx); err != nil {
		if taskCtx.Err() != nil {
			s.logInfo("Batch delete cancelled")
			return nil
		}
		return s.reportError(err, "batch-delete")
	}
	return nil
}

func (s *Service) deleteAll(ctx context.Context) error {
	modsPaths, err := s.enabledModsPaths(ctx)
	if err != nil {
		return err
	}
	if len(modsPaths) == 0 {
		s.logInfo("Batch delete skipped: no enabled importer mods folder")
		return nil
	}
	client, err := s.requireClient()
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil {
		return err
	}

	// Folders renamed or moved after generation, such as a mod disabled while auto generation was off,
	// keep records pointing at the old location, where the user may since have put files of their own.
	cleanup := s.newArtifactCleanup(records)
	if err := forEachMovedArtifact(ctx, modsPaths, recordedDirs(records), cleanup.remove); err != nil {
		return err
	}
	var targets []db.ToggleViewerArtifactRow
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !pathInAnyRoot(record.TargetIniPath, modsPaths) {
			continue
		}
		targets = append(targets, record)
		for _, artifactPath := range managedArtifactPaths(record.TargetIniPath) {
			cleanup.remove(artifactPath)
		}
	}

	deletedRecords := 0
	for _, record := range targets {
		if cleanup.keep[record.ID] {
			s.logInfo("Keeping artifact record due to delete error: " + record.TargetIniPath)
			continue
		}
		if err := client.ToggleViewerArtifacts.Delete(ctx, record.ID, record.TargetIniPath); err != nil {
			return err
		}
		deletedRecords++
	}
	s.logInfo(fmt.Sprintf(
		"Batch delete completed. deletedFiles=%d, deletedRecords=%d, targetRecords=%d",
		cleanup.removed, deletedRecords, len(targets),
	))
	return nil
}

// artifactCleanup removes only artifact files whose content one of its records holds, so a file the
// user created under an artifact name survives. It keeps the records a later cleanup needs to still
// recognise a copy: those matching a copy that could not be removed, or every record when a copy
// could not be read, because its owner is then unknown.
type artifactCleanup struct {
	service   *Service
	records   []db.ToggleViewerArtifactRow
	idsByHash map[string][]string
	keep      map[string]bool
	removed   int
}

func (s *Service) newArtifactCleanup(records []db.ToggleViewerArtifactRow) *artifactCleanup {
	idsByHash := map[string][]string{}
	for _, record := range records {
		idsByHash[record.ToggleTxtHash] = append(idsByHash[record.ToggleTxtHash], record.ID)
		idsByHash[record.ToggleIniHash] = append(idsByHash[record.ToggleIniHash], record.ID)
	}
	return &artifactCleanup{service: s, records: records, idsByHash: idsByHash, keep: map[string]bool{}}
}

func (c *artifactCleanup) remove(path string) {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		c.service.logError(fmt.Sprintf("Keeping artifact records: cannot read artifact %s: %s", path, err), err)
		for _, record := range c.records {
			c.keep[record.ID] = true
		}
		return
	}

	matched := c.idsByHash[toggleContentSHA(string(content))]
	if len(matched) == 0 {
		return
	}
	if err := os.Remove(path); err != nil {
		c.service.logError(fmt.Sprintf("Failed to delete artifact file %s: %s", path, err), err)
		for _, id := range matched {
			c.keep[id] = true
		}
		return
	}
	c.removed++
}

// forEachMovedArtifact visits the artifact files under roots outside skipDirs, holding the root's
// disk slot while visit runs.
func forEachMovedArtifact(
	ctx context.Context,
	roots []string,
	skipDirs []string,
	visit func(path string),
) error {
	for _, root := range roots {
		release, err := diskio.AcquireDir(ctx, root)
		if err != nil {
			return err
		}
		var paths []string
		err = walkRegularFiles(ctx, root, nil, func(path string, _ fs.DirEntry) {
			if isArtifactName(filepath.Base(path)) && !containsPathFold(skipDirs, filepath.Dir(path)) {
				paths = append(paths, path)
			}
		})
		if err != nil {
			release()
			return fmt.Errorf("scan toggle viewer artifacts %q: %w", root, err)
		}
		for _, path := range paths {
			visit(path)
		}
		release()
	}
	return nil
}

// ApplyHotkey rewrites the [Key] binding of every recorded artifact with the stored hotkey, falling
// back to hotkey only without settings, so concurrent setting changes converge on the latest one. It
// runs under watchMu so it waits for importer maintenance instead of writing into folders that are
// being moved. An artifact the game keeps open is retried later, with or without the watcher.
// Artifacts of folders renamed or moved after generation are found by their recorded INI hash, like
// in batch delete, and the hash is updated so a later batch delete still identifies them.
func (s *Service) ApplyHotkey(ctx context.Context, hotkey string) error {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	hotkey = s.resolveHotkey(ctx, hotkey)
	client, err := s.requireClient()
	if err != nil {
		return err
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil {
		return err
	}
	updated, deferred := 0, false
	defer func() {
		if deferred {
			s.scheduleHotkeyRetry(hotkey)
		}
	}()
	// rewrite returns the hash of the content left at path, which stays the previous content while the
	// game keeps the file open.
	rewrite := func(path string) (string, error) {
		current, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				s.logError(fmt.Sprintf("Deferred hotkey update of unreadable %s: %s", path, err), err)
				deferred = true
			}
			return "", err
		}
		next := replaceToggleViewerHotkey(string(current), hotkey)
		if next == string(current) {
			return toggleContentSHA(next), nil
		}
		if err := writeArtifact(path, next); err != nil {
			s.logError(fmt.Sprintf("Deferred hotkey update of %s: %s", path, err), err)
			deferred = true
			return toggleContentSHA(string(current)), nil
		}
		updated++
		return toggleContentSHA(next), nil
	}
	setHash := func(record db.ToggleViewerArtifactRow, hash string) error {
		if record.ToggleIniHash == hash {
			return nil
		}
		return client.ToggleViewerArtifacts.UpdateIniHash(
			ctx, record.ID, hash, time.Now().UTC().Format(time.RFC3339Nano),
		)
	}

	orphansByHash := map[string][]db.ToggleViewerArtifactRow{}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		expectedINI := filepath.Join(filepath.Dir(record.TargetIniPath), artifactININame)
		if !platform.SamePathFold(expectedINI, record.ToggleIniPath) {
			s.logInfo("Skipped untrusted toggle viewer artifact path: " + record.ToggleIniPath)
			continue
		}
		hash, err := rewrite(expectedINI)
		if errors.Is(err, os.ErrNotExist) {
			orphansByHash[record.ToggleIniHash] = append(orphansByHash[record.ToggleIniHash], record)
			continue
		}
		if err != nil {
			continue
		}
		if err := setHash(record, hash); err != nil {
			return err
		}
	}
	if len(orphansByHash) > 0 {
		unread, err := s.applyHotkeyToMovedArtifacts(ctx, records, orphansByHash, rewrite, setHash)
		if unread {
			deferred = true
		}
		if err != nil {
			return err
		}
	}
	s.logInfo(fmt.Sprintf("Applied toggle viewer hotkey to artifacts: updated=%d", updated))
	return nil
}

// applyHotkeyToMovedArtifacts rewrites the artifacts of folders moved away from their records, found
// by the INI hash of the orphaned records. Generated INIs do not name their folder, so several moved
// files can share a hash and the owner of each is unknown. The orphans of a hash are therefore spread
// over the hashes the files end up with, files still holding the old content first, so a retry and a
// batch delete keep finding every file the game kept open. Orphans left over usually share a folder
// with a found file and take its hash, but while a moved file could not be read they keep their old
// hash, because the unread file may be the one they own. It reports whether a moved file was unread.
func (s *Service) applyHotkeyToMovedArtifacts(
	ctx context.Context,
	records []db.ToggleViewerArtifactRow,
	orphansByHash map[string][]db.ToggleViewerArtifactRow,
	rewrite func(path string) (string, error),
	setHash func(record db.ToggleViewerArtifactRow, hash string) error,
) (bool, error) {
	modsPaths, err := s.enabledModsPaths(ctx)
	if err != nil {
		return false, err
	}
	unread := false
	movedByHash := map[string][]string{}
	err = forEachMovedArtifact(ctx, modsPaths, recordedDirs(records), func(path string) {
		if !strings.EqualFold(filepath.Base(path), artifactININame) {
			return
		}
		content, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				s.logError(fmt.Sprintf("Deferred hotkey update of unreadable moved artifact %s: %s", path, err), err)
				unread = true
			}
			return
		}
		if hash := toggleContentSHA(string(content)); len(orphansByHash[hash]) > 0 {
			movedByHash[hash] = append(movedByHash[hash], path)
		}
	})
	if err != nil {
		return unread, err
	}

	for oldHash, paths := range movedByHash {
		var kept, rewritten []string
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return unread, err
			}
			hash, err := rewrite(path)
			switch {
			case errors.Is(err, os.ErrNotExist):
			case err != nil || hash == oldHash:
				kept = append(kept, oldHash)
			default:
				rewritten = append(rewritten, hash)
			}
		}
		hashes := slices.Concat(kept, rewritten)
		if len(hashes) == 0 {
			continue
		}
		for index, record := range orphansByHash[oldHash] {
			if index >= len(hashes) && unread {
				break
			}
			if err := setHash(record, hashes[min(index, len(hashes)-1)]); err != nil {
				return unread, err
			}
		}
	}
	return unread, nil
}

// StartWatcher watches the mods folders of every enabled importer and rescans on change while
// auto generation is enabled. It reads the stored setting, so a stale caller cannot override a
// newer transition, and it stops the watcher when auto generation is off.
func (s *Service) StartWatcher(ctx context.Context) error {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	return s.startWatcherLocked(ctx, true)
}

func (s *Service) StopWatcher() error {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	_, err := s.stopWatcherLocked()
	return err
}

func (s *Service) startWatcherLocked(ctx context.Context, initialScan bool) error {
	rescan, err := s.stopWatcherLocked()
	if err != nil {
		return err
	}
	initialScan = initialScan || rescan
	if s.settings == nil || s.importers == nil {
		return nil
	}
	enabled, err := s.settings.GetToggleViewerAutoGenerate(ctx)
	if err != nil || !enabled {
		return err
	}
	modsPaths, err := s.enabledModsPaths(ctx)
	if err != nil {
		return err
	}
	if len(modsPaths) == 0 {
		s.logInfo("No mods folder found for enabled importers")
		return nil
	}

	service, err := watcher.WatchTree(modsPaths, watcher.TreeConfig{
		Depth:    -1,
		Ops:      watcher.All,
		Debounce: 500 * time.Millisecond,
		Filter: func(event watcher.Event) bool {
			return s.relevantEvent(modsPaths, event)
		},
	}, func(watcher.Event) {
		s.requestScan()
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		_ = service.Close()
		return errors.New("toggle viewer is shutting down")
	}
	s.watcher = &treeWatcher{watcher: service, roots: modsPaths}
	s.mu.Unlock()

	s.logInfo(fmt.Sprintf("Started toggle viewer watcher (%d)", len(modsPaths)))
	if initialScan {
		s.requestScan()
	}
	return nil
}

// relevantEvent reports whether a change under roots can alter what a scan reads. The watcher only
// delivers the last event of a debounce window, so irrelevant events must be dropped one by one here
// rather than in the scan callback. Mod assets, temporary files and the directory writes that
// accompany their creation never change a source; a path that is gone only matters when it held a
// folder with sources at the last scan, which is unknown before that scan and while a task runs.
func (s *Service) relevantEvent(roots []string, event watcher.Event) bool {
	name := filepath.Base(event.Path)
	if isArtifactName(name) || hasPrefixFold(name, tempPrefix) {
		return false
	}
	for _, root := range roots {
		if platform.SamePathFold(root, event.Path) {
			return true
		}
		if rel, err := filepath.Rel(root, event.Path); err == nil && platform.SameOrChildPath(root, event.Path) &&
			slices.ContainsFunc(strings.Split(rel, string(filepath.Separator)), isDisabledName) {
			return false
		}
	}
	if strings.EqualFold(filepath.Ext(name), ".ini") {
		return true
	}

	info, err := os.Lstat(event.Path)
	if err == nil {
		return info.IsDir() && event.Op != watcher.Write
	}
	if !errors.Is(err, os.ErrNotExist) {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sourceDirs == nil || s.task != nil {
		return true
	}
	return slices.ContainsFunc(s.sourceDirs, func(dir string) bool {
		return platform.SameOrChildPath(event.Path, dir)
	})
}

// watcherCurrentLocked reports whether the running watcher already covers what a restart would watch.
func (s *Service) watcherCurrentLocked(ctx context.Context) (bool, error) {
	s.mu.Lock()
	current := s.watcher
	s.mu.Unlock()
	if current == nil || s.settings == nil || s.importers == nil {
		return false, nil
	}
	enabled, err := s.settings.GetToggleViewerAutoGenerate(ctx)
	if err != nil || !enabled {
		return false, err
	}
	modsPaths, err := s.enabledModsPaths(ctx)
	if err != nil {
		return false, err
	}
	return slices.EqualFunc(current.roots, modsPaths, platform.SamePathFold), nil
}

// stopWatcherLocked reports whether the watcher left a rescan unfinished, either requested or
// cancelled while running, so a restart can run it instead of losing the change it was for.
func (s *Service) stopWatcherLocked() (bool, error) {
	s.mu.Lock()
	current := s.watcher
	s.watcher = nil
	scanning := s.task != nil && s.task.mode == modeScan
	rescan := current != nil && (s.pending || scanning)
	s.pending = false
	s.stopRetryLocked()
	if scanning {
		s.task.cancel()
	}
	s.mu.Unlock()
	if current == nil {
		return false, nil
	}
	err := current.watcher.Close()
	s.logInfo(fmt.Sprintf("Stopped toggle viewer watcher (%d)", len(current.roots)))
	return rescan, err
}

// requestScan starts a scan or, while a task runs, leaves a follow-up for finishTask. Every decision
// shares one critical section with the watcher check, so neither a task finishing in between nor a
// watcher stopped by a request already on its way can be missed.
func (s *Service) requestScan() {
	s.mu.Lock()
	if s.watcher == nil || s.closing || s.suspended {
		s.mu.Unlock()
		return
	}
	current, ctx, err := s.beginTaskLocked(context.Background(), modeScan)
	if err != nil {
		s.pending = true
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.emitState()
	go func() {
		defer s.finishTask(current)
		result, err := s.scanAll(ctx)
		if result.retry {
			s.scheduleRetry()
		}
		if ctx.Err() == nil {
			s.diagnostics.Report(s.log, err, "ToggleViewer", infra.Diagnostic{
				Operation: "toggle-viewer.scan", Stage: "background",
			})
		}
	}()
}

// scanResult reports a scan. retry is set when a write or a cleanup was deferred because an artifact
// could not be replaced or removed yet; skipped lists the INIs with toggle keys but no position hash.
type scanResult struct {
	retry   bool
	folders int
	skipped []string
}

// rootScan is what scanRoot found under one Mods folder; see scanRoot for unreadDirs.
type rootScan struct {
	artifacts  []toggleViewerArtifact
	unreadDirs []string
	skipped    []string
}

func (s *Service) scanAll(ctx context.Context) (scanResult, error) {
	modsPaths, err := s.enabledModsPaths(ctx)
	if err != nil {
		return scanResult{}, err
	}
	if len(modsPaths) == 0 {
		s.logInfo("Toggle viewer scan skipped: no enabled importer mods folder")
		return scanResult{}, nil
	}
	hotkey := s.resolveHotkey(ctx, "")

	var artifacts []toggleViewerArtifact
	var unreadDirs, skipped []string
	sources := map[string]cachedSource{}
	for _, modsPath := range modsPaths {
		scan, err := s.scanRoot(ctx, modsPath, hotkey, sources)
		if err != nil {
			return scanResult{}, err
		}
		artifacts = append(artifacts, scan.artifacts...)
		unreadDirs = append(unreadDirs, scan.unreadDirs...)
		skipped = append(skipped, scan.skipped...)
	}
	s.sources = sources
	sourceDirs := append(make([]string, 0, len(unreadDirs)+len(artifacts)), unreadDirs...)
	for _, artifact := range artifacts {
		sourceDirs = append(sourceDirs, filepath.Dir(artifact.toggleINIPath))
	}
	s.mu.Lock()
	s.sourceDirs = sourceDirs
	s.mu.Unlock()
	s.writeMu.Lock()
	deferredDirs, err := s.persistArtifacts(ctx, artifacts, hotkey)
	deferredDirs = append(deferredDirs, unreadDirs...)
	if err != nil {
		s.writeMu.Unlock()
		return scanResult{retry: len(deferredDirs) > 0}, err
	}
	pendingCleanups, err := s.deleteStaleRecords(ctx, modsPaths, artifacts, deferredDirs)
	s.writeMu.Unlock()
	result := scanResult{
		retry:   len(deferredDirs) > 0 || pendingCleanups > 0,
		folders: len(artifacts),
		skipped: skipped,
	}
	if err != nil {
		return result, err
	}

	matched := 0
	for _, artifact := range artifacts {
		matched += len(artifact.targetINIPaths)
	}
	s.logInfo(fmt.Sprintf(
		"Scan complete. matched=%d, folders=%d, noPositionHash=%d, deferred=%d, pendingCleanups=%d, importers=%d",
		matched, len(artifacts), len(skipped), len(deferredDirs), pendingCleanups, len(modsPaths),
	))
	return result, nil
}

// scanRoot also returns the folders with a source INI that could not be read. Their artifacts are
// neither rebuilt from the remaining sources nor treated as stale, because the unread source may
// still qualify; the caller defers them to a later scan. An INI whose size and modification time
// match the previous scan reuses its parsed source, and every INI read is recorded in sources.
func (s *Service) scanRoot(
	ctx context.Context,
	modsPath, hotkey string,
	sources map[string]cachedSource,
) (rootScan, error) {
	release, err := diskio.AcquireDir(ctx, modsPath)
	if err != nil {
		return rootScan{}, err
	}
	defer release()

	inis, err := scanINIs(ctx, modsPath)
	if err != nil {
		return rootScan{}, fmt.Errorf("scan toggle viewer mods %q: %w", modsPath, err)
	}
	var dirs, unreadDirs, skipped []string
	sourcesByDir := map[string][]toggleViewerSource{}
	for _, ini := range inis {
		iniPath := ini.path
		cached, ok := s.sources[iniPath]
		if !ok || ini.info == nil || cached.size != ini.info.Size() || !cached.modTime.Equal(ini.info.ModTime()) {
			content, err := os.ReadFile(iniPath)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				s.logError(fmt.Sprintf("Deferred toggle viewer scan of unreadable ini %s: %s", iniPath, err), err)
				if dir := filepath.Dir(iniPath); !containsPathFold(unreadDirs, dir) {
					unreadDirs = append(unreadDirs, dir)
				}
				continue
			}
			source, missingHash := parseToggleViewerSource(iniPath, string(content))
			cached = cachedSource{source: source, missingHash: missingHash}
			if ini.info != nil {
				cached.size, cached.modTime = ini.info.Size(), ini.info.ModTime()
			}
		}
		if ini.info != nil {
			sources[iniPath] = cached
		}
		if cached.missingHash {
			skipped = append(skipped, iniPath)
		}
		source := cached.source
		if source == nil {
			continue
		}
		dir := filepath.Dir(iniPath)
		if _, exists := sourcesByDir[dir]; !exists {
			dirs = append(dirs, dir)
		}
		sourcesByDir[dir] = append(sourcesByDir[dir], *source)
	}

	artifacts := make([]toggleViewerArtifact, 0, len(dirs))
	for _, dir := range dirs {
		if containsPathFold(unreadDirs, dir) {
			continue
		}
		artifacts = append(artifacts, buildToggleViewerArtifact(sourcesByDir[dir], hotkey))
	}
	return rootScan{artifacts: artifacts, unreadDirs: unreadDirs, skipped: skipped}, nil
}

// persistArtifacts returns the folders that kept some of their previous artifact files because they
// could not be replaced, so a later scan retries them. A folder where one file was still written is
// recorded anyway, because batch delete only finds artifacts through their records; the kept file is
// recorded with its own content's hash so a batch delete still identifies it after its folder moves.
// The artifacts were built with builtHotkey; a hotkey stored since then replaces it, because
// ApplyHotkey may already have run. The caller holds writeMu.
func (s *Service) persistArtifacts(
	ctx context.Context,
	artifacts []toggleViewerArtifact,
	builtHotkey string,
) ([]string, error) {
	client, err := s.requireClient()
	if err != nil {
		return nil, err
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil {
		return nil, err
	}
	previous := make(map[string]db.ToggleViewerArtifactRow, len(records))
	for _, record := range records {
		previous[record.TargetIniPath] = record
	}

	if hotkey := s.resolveHotkey(ctx, builtHotkey); hotkey != builtHotkey {
		for index := range artifacts {
			artifacts[index].iniContent = replaceToggleViewerHotkey(artifacts[index].iniContent, hotkey)
			artifacts[index].toggleINIHash = toggleContentSHA(artifacts[index].iniContent)
		}
	}
	// Once a folder's files are written its records must land too, or a cancellation in between
	// leaves files that batch delete cannot find; cancellation is honoured between folders instead.
	recordCtx := context.WithoutCancel(ctx)
	var deferred []string
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return deferred, err
		}
		txtErr := writeArtifactIfChanged(artifact.toggleTXTPath, artifact.txtContent)
		iniErr := writeArtifactIfChanged(artifact.toggleINIPath, artifact.iniContent)
		if err := errors.Join(txtErr, iniErr); err != nil {
			dir := filepath.Dir(artifact.toggleINIPath)
			deferred = append(deferred, dir)
			s.logError(fmt.Sprintf("Deferred toggle viewer update in %s: %s", dir, err), err)
			if txtErr != nil && iniErr != nil {
				continue
			}
		}
		txtHash := keptArtifactHash(txtErr, artifact.toggleTXTPath, artifact.toggleTXTHash)
		iniHash := keptArtifactHash(iniErr, artifact.toggleINIPath, artifact.toggleINIHash)
		updatedAt := time.Now().UTC().Format(time.RFC3339Nano)
		for _, target := range artifact.targetINIPaths {
			record, exists := previous[target]
			row := db.ToggleViewerArtifactRow{
				TargetIniPath: target,
				ToggleTxtPath: artifact.toggleTXTPath,
				ToggleIniPath: artifact.toggleINIPath,
				ToggleTxtHash: cmp.Or(txtHash, record.ToggleTxtHash, artifact.toggleTXTHash),
				ToggleIniHash: cmp.Or(iniHash, record.ToggleIniHash, artifact.toggleINIHash),
				UpdatedAt:     updatedAt,
			}
			if exists && record.ToggleTxtPath == row.ToggleTxtPath && record.ToggleIniPath == row.ToggleIniPath &&
				record.ToggleTxtHash == row.ToggleTxtHash && record.ToggleIniHash == row.ToggleIniHash {
				continue
			}
			id, err := newID()
			if err != nil {
				return deferred, err
			}
			row.ID = id
			if err := client.ToggleViewerArtifacts.Upsert(recordCtx, row); err != nil {
				return deferred, err
			}
		}
	}
	return deferred, nil
}

// deleteStaleRecords forgets sources that no longer qualify and returns how many cleanups are left
// for a later scan. A folder's artifact files are only removed when no current source still uses
// them, and only while they hold recorded content, like in batch delete. Copies left in a folder moved
// away from a stale record are removed before the record, because nothing identifies them once it is
// gone. A record is kept while its files could not be removed, or while its folder is deferred,
// because a source that could not be read or an artifact that could not be replaced leaves the
// folder's current state unknown. Records outside the scanned roots belong to importers hidden from the
// app, whose sources were not read, so they are left alone.
func (s *Service) deleteStaleRecords(
	ctx context.Context,
	scannedRoots []string,
	artifacts []toggleViewerArtifact,
	deferredDirs []string,
) (int, error) {
	client, err := s.requireClient()
	if err != nil {
		return 0, err
	}
	records, err := client.ToggleViewerArtifacts.List(ctx)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	var liveDirs []string
	for _, artifact := range artifacts {
		liveDirs = append(liveDirs, filepath.Dir(artifact.toggleINIPath))
		for _, target := range artifact.targetINIPaths {
			seen[target] = true
		}
	}

	var stale []db.ToggleViewerArtifactRow
	for _, record := range records {
		if seen[record.TargetIniPath] || containsPathFold(deferredDirs, filepath.Dir(record.TargetIniPath)) ||
			!pathInAnyRoot(record.TargetIniPath, scannedRoots) {
			continue
		}
		stale = append(stale, record)
	}
	if len(stale) == 0 {
		return 0, nil
	}

	// Every folder with a current source is recorded or deferred by now, so the walk only reaches
	// folders whose artifacts nothing uses.
	cleanup := s.newArtifactCleanup(stale)
	skipDirs := append(recordedDirs(records), deferredDirs...)
	if err := forEachMovedArtifact(ctx, scannedRoots, skipDirs, cleanup.remove); err != nil {
		return 0, err
	}
	for _, record := range stale {
		if !containsPathFold(liveDirs, filepath.Dir(record.TargetIniPath)) {
			for _, artifactPath := range managedArtifactPaths(record.TargetIniPath) {
				cleanup.remove(artifactPath)
			}
		}
	}

	pending := 0
	for _, record := range stale {
		if cleanup.keep[record.ID] {
			pending++
			continue
		}
		if err := client.ToggleViewerArtifacts.Delete(ctx, record.ID, record.TargetIniPath); err != nil {
			return pending, err
		}
		s.logInfo("Removed stale toggle-viewer artifact record: " + record.TargetIniPath)
	}
	return pending, nil
}

// enabledModsPaths resolves each importer's Mods folder to its physical location, because XXMI
// commonly links it to the user's mod library and a walk does not descend into a junction root.
func (s *Service) enabledModsPaths(ctx context.Context) ([]string, error) {
	if s.importers == nil {
		return nil, nil
	}
	importers, err := s.importers.GetEnabledImporters(ctx)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, importer := range importers {
		modsPath := filepath.Join(importer.ImporterFolder, "Mods")
		if resolved, err := platform.FinalPath(modsPath); err == nil {
			modsPath = resolved
		}
		info, err := os.Stat(modsPath)
		if err != nil || !info.IsDir() {
			continue
		}
		if !containsPathFold(paths, modsPath) {
			paths = append(paths, modsPath)
		}
	}
	return paths, nil
}

func (s *Service) beginTask(parent context.Context, mode string) (*task, context.Context, error) {
	s.mu.Lock()
	current, ctx, err := s.beginTaskLocked(parent, mode)
	s.mu.Unlock()
	if err == nil {
		s.emitState()
	}
	return current, ctx, err
}

func (s *Service) beginTaskLocked(parent context.Context, mode string) (*task, context.Context, error) {
	if s.closing {
		return nil, nil, errors.New("toggle viewer is shutting down")
	}
	if s.suspended {
		return nil, nil, infra.ContractError("Importer maintenance is in progress")
	}
	if s.task != nil {
		return nil, nil, infra.ContractError("Toggle viewer task is already running")
	}
	ctx, cancel := context.WithCancel(parent)
	current := &task{mode: mode, cancel: cancel}
	s.task = current
	s.tasks.Add(1)
	return current, ctx, nil
}

func (s *Service) finishTask(current *task) {
	current.cancel()
	s.mu.Lock()
	if s.task == current {
		s.task = nil
	}
	pending := s.pending && s.watcher != nil && !s.closing && !s.suspended
	s.pending = false
	s.mu.Unlock()
	s.emitState()
	s.tasks.Done()
	if pending {
		s.requestScan()
	}
}

// emitState reads the state under stateMu, so the last event delivered always carries the latest state
// even when tasks begin and finish concurrently.
func (s *Service) emitState() {
	if s.emit == nil {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.emit(stateEvent, s.State())
}

// scheduleRetry rescans once the delay passes while the watcher runs, because nothing else notices
// when the game releases an artifact it kept open.
func (s *Service) scheduleRetry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watcher == nil || s.closing || s.retry != nil {
		return
	}
	s.retry = time.AfterFunc(s.retryDelay, func() {
		s.mu.Lock()
		s.retry = nil
		s.mu.Unlock()
		s.requestScan()
	})
}

// scheduleHotkeyRetry applies the hotkey again once the delay passes. Unlike scan retries it does not
// depend on the watcher, because batch-generated artifacts are kept while auto generation is off.
func (s *Service) scheduleHotkeyRetry(hotkey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.hotkeyRetry != nil {
		return
	}
	s.hotkeyRetry = time.AfterFunc(s.retryDelay, func() {
		s.mu.Lock()
		s.hotkeyRetry = nil
		if s.closing {
			s.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.hotkeyCancel = cancel
		s.hotkeyRuns.Add(1)
		s.mu.Unlock()
		defer s.hotkeyRuns.Done()
		defer cancel()

		err := s.ApplyHotkey(ctx, hotkey)
		if !infra.IsCancellationError(err) {
			s.diagnostics.Report(s.log, err, "ToggleViewer", infra.Diagnostic{
				Operation: "toggle-viewer.hotkey", Stage: "retry",
			})
		}
	})
}

// resolveHotkey prefers the stored setting and falls back to fallback, then to the default hotkey.
func (s *Service) resolveHotkey(ctx context.Context, fallback string) string {
	if s.settings != nil {
		if stored, err := s.settings.GetToggleViewerHotkey(ctx); err == nil && strings.TrimSpace(stored) != "" {
			return strings.TrimSpace(stored)
		}
	}
	if fallback = strings.TrimSpace(fallback); fallback != "" {
		return fallback
	}
	return defaultHotkey
}

func (s *Service) stopRetryLocked() {
	if s.retry != nil {
		s.retry.Stop()
		s.retry = nil
	}
}

func (s *Service) reportError(err error, stage string) error {
	s.addLog("ERROR", err.Error())
	return infra.ReportError(s.log, err, "ToggleViewer", infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: "toggle-viewer", Stage: stage,
	})
}

func (s *Service) logInfo(message string) {
	if s.log != nil {
		s.log.Info(message, "ToggleViewer")
	}
	s.addLog("INFO", message)
}

func (s *Service) logError(message string, err error) {
	s.diagnostics.Report(s.log, err, "ToggleViewer", infra.Diagnostic{
		Operation: "toggle-viewer", Stage: "artifact", Fields: map[string]any{"context": message},
	})
	s.addLog("ERROR", message)
}

// addLog keeps the entry format of the toggle persistence log so the renderer parses both alike.
func (s *Service) addLog(level, message string) {
	entry := "[" + time.Now().Format("Jan 2, 2006, 3:04:05 PM") + "] [" + level + "] " + message
	s.mu.Lock()
	s.logs = append(s.logs, entry)
	if len(s.logs) > maxLogs {
		s.logs = append([]string{}, s.logs[len(s.logs)-maxLogs:]...)
	}
	logs := append([]string{}, s.logs...)
	s.mu.Unlock()
	if s.emit != nil {
		s.emit(logsEvent, logs)
	}
}

// scanINIs skips folders and files that 3DMigoto does not load, so disabled mods get no artifacts and
// the stale cleanup removes the ones they kept from before they were disabled.
func scanINIs(ctx context.Context, root string) ([]sourceINI, error) {
	var inis []sourceINI
	err := walkRegularFiles(ctx, root, isDisabledName, func(path string, entry fs.DirEntry) {
		if !isSourceININame(entry.Name()) {
			return
		}
		ini := sourceINI{path: path}
		if info, err := entry.Info(); err == nil {
			ini.info = info
		}
		inis = append(inis, ini)
	})
	return inis, err
}

// walkRegularFiles visits the regular files under root, skipping the folders below root whose name
// matches skipDir when it is set.
func walkRegularFiles(
	ctx context.Context,
	root string,
	skipDir func(name string) bool,
	visit func(path string, entry fs.DirEntry),
) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() && skipDir != nil && path != root && skipDir(entry.Name()) {
			return fs.SkipDir
		}
		if entry.Type().IsRegular() {
			visit(path, entry)
		}
		return nil
	})
}

func isSourceININame(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".ini") && !isArtifactName(name) && !isDisabledName(name)
}

func isDisabledName(name string) bool {
	return hasPrefixFold(name, "disabled")
}

// keptArtifactHash returns the hash to record for an artifact file: the written content's, or after a
// failed replacement the hash of the file left in place. It returns "" when that file cannot be read.
func keptArtifactHash(writeErr error, path, writtenHash string) string {
	if writeErr == nil {
		return writtenHash
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return toggleContentSHA(string(content))
}

func writeArtifactIfChanged(path, content string) error {
	if current, err := os.ReadFile(path); err == nil && string(current) == content {
		return nil
	}
	return writeArtifact(path, content)
}

// writeArtifact replaces the file through a sibling temporary file so a failed write never leaves a
// truncated INI behind. XXMI keeps loaded INI files open without delete sharing, which fails the
// replacement until the game releases them; the existing file is then kept as it was.
func writeArtifact(path, content string) (returnErr error) {
	temp, err := os.CreateTemp(filepath.Dir(path), tempPrefix+"*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, os.Remove(temp.Name()))
		}
	}()
	if _, err := temp.WriteString(content); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return platform.ReplaceAtomic(temp.Name(), path)
}

func managedArtifactPaths(targetINIPath string) []string {
	dir := filepath.Dir(targetINIPath)
	return []string{filepath.Join(dir, artifactTXTName), filepath.Join(dir, artifactININame)}
}

func isArtifactName(name string) bool {
	return strings.EqualFold(name, artifactININame) || strings.EqualFold(name, artifactTXTName)
}

func recordedDirs(records []db.ToggleViewerArtifactRow) []string {
	dirs := make([]string, 0, len(records))
	for _, record := range records {
		dirs = append(dirs, filepath.Dir(record.TargetIniPath))
	}
	return dirs
}

// pathInAnyRoot compares the physical location of path's folder, because roots are resolved
// through the Mods link while older records may hold the linked path.
func pathInAnyRoot(path string, roots []string) bool {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if dir, err := platform.FinalPath(filepath.Dir(resolved)); err == nil {
		resolved = filepath.Join(dir, filepath.Base(resolved))
	}
	for _, root := range roots {
		if platform.SameOrChildPath(root, resolved) {
			return true
		}
	}
	return false
}

func hasPrefixFold(name, prefix string) bool {
	return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
}

func containsPathFold(paths []string, path string) bool {
	for _, existing := range paths {
		if platform.SamePathFold(existing, path) {
			return true
		}
	}
	return false
}

func newID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
