package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

const (
	bisectDisabledSuffix = "mod-bisect-disabled"
	bisectStateEvent     = "tools:bisectState"
	bisectInconclusive   = "Bisect inconclusive"
	bisectAllExcluded    = "All enabled INIs were excluded"
	bisectExcludeEmpty   = "Exclude path is empty."
	bisectExcludeOutside = "Exclude path must be inside the selected importer."
	bisectExcludeRoot    = "Exclude path cannot be the importer root."
	bisectExcludeMissing = "Exclude path does not exist."
)

type BisectStatus string

const (
	BisectIdle      BisectStatus = "idle"
	BisectScanning  BisectStatus = "scanning"
	BisectRound     BisectStatus = "round"
	BisectDone      BisectStatus = "done"
	BisectReverting BisectStatus = "reverting"
	BisectCancelled BisectStatus = "cancelled"
)

type BisectSnapshot struct {
	Status         BisectStatus `json:"status"`
	Game           string       `json:"game"`
	ModRootPath    *string      `json:"modRootPath"`
	Round          int          `json:"round"`
	BatchSize      int          `json:"batchSize"`
	Candidates     []string     `json:"candidates"`
	CurrentBatch   []string     `json:"currentBatch"`
	ExcludePaths   []string     `json:"excludePaths"`
	UndoStackDepth int          `json:"undoStackDepth"`
	FinalBadPath   *string      `json:"finalBadPath"`
	Error          *string      `json:"error"`
}

type bisectSession struct {
	game         string
	modRootPath  string
	candidates   []string
	round        int
	batchSize    int
	currentBatch []string
	excludePaths []string
	undoStack    []bisectUndo
	finalBadPath *string
	phase        BisectStatus
}

type bisectUndo struct {
	disabled         []string
	candidatesBefore []string
	remainingAfter   []string
	round            int
	batchSize        int
}

func (t *Tools) BisectGetState() *BisectSnapshot {
	if t == nil {
		return nil
	}
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	if t.bisect == nil {
		return nil
	}
	status := BisectRound
	if t.bisect.finalBadPath != nil {
		status = BisectDone
	} else if t.bisect.phase == BisectScanning {
		status = BisectScanning
	}
	snapshot := snapshotFor(t.bisect, nil, status)
	return &snapshot
}

func (t *Tools) BisectValidateExcludePath(ctx context.Context, game, inputPath string) (string, error) {
	config, err := t.requireBisectGame(ctx, game)
	if err != nil {
		return "", err
	}
	return resolveBisectExclude(config.ModFolderPath, inputPath)
}

func (t *Tools) BisectStart(ctx context.Context, game string, excludePaths []string) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	if t.bisect != nil && t.bisect.finalBadPath == nil {
		return BisectSnapshot{}, errors.New("A bisect session is already running. Cancel it first.") //nolint:staticcheck // Electron contract text.
	}
	if t.bisect != nil {
		completed := t.bisect
		t.broadcastBisect(snapshotFor(completed, nil, BisectCancelled))
		if err := t.cancelBisectLocked(); err != nil {
			return BisectSnapshot{}, err
		}
		t.broadcastBisect(snapshotFor(completed, nil, BisectReverting))
		t.broadcastBisect(emptyBisectSnapshot(BisectIdle))
	}

	config, err := t.requireBisectGame(ctx, game)
	if err != nil {
		return BisectSnapshot{}, err
	}
	excludeRelatives := make([]string, 0, len(excludePaths))
	seen := make(map[string]struct{}, len(excludePaths))
	for _, inputPath := range excludePaths {
		relative, err := resolveBisectExclude(config.ModFolderPath, inputPath)
		if err != nil {
			return BisectSnapshot{}, err
		}
		key := strings.ToLower(relative)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			excludeRelatives = append(excludeRelatives, relative)
		}
	}
	excludeAbs := make([]string, len(excludeRelatives))
	for i, relative := range excludeRelatives {
		excludeAbs[i] = filepath.Join(config.ModFolderPath, filepath.FromSlash(relative))
	}

	session := &bisectSession{
		game: game, modRootPath: config.ModFolderPath, candidates: []string{},
		currentBatch: []string{}, excludePaths: excludeRelatives, undoStack: []bisectUndo{}, phase: BisectScanning,
	}
	t.bisect = session
	scanning := snapshotFor(session, nil, BisectScanning)
	t.broadcastBisect(scanning)

	paths, err := scanEnabledINIs(config.ModFolderPath)
	if err != nil {
		t.bisect = nil
		return BisectSnapshot{}, err
	}
	candidates := make([]string, 0, len(paths))
	for _, iniPath := range paths {
		if !anyPathContains(excludeAbs, iniPath) {
			candidates = append(candidates, iniPath)
		}
	}
	if len(candidates) == 0 {
		t.bisect = nil
		var message *string
		if len(paths) > 0 {
			message = stringPointer(bisectAllExcluded)
		}
		done := scanning
		done.Status = BisectDone
		done.Error = message
		t.broadcastBisect(done)
		return done, nil
	}

	if t.settings != nil {
		preserve, settingErr := t.settings.GetBisectPreserveD3dx(ctx)
		if settingErr != nil {
			t.bisect = nil
			return BisectSnapshot{}, settingErr
		}
		if preserve {
			if err := t.startD3dxGuardLocked(ctx, config); err != nil {
				t.logError(err, "ModBisect:d3dxGuard")
			}
		}
	}

	batchSize := chooseBisectBatchSize(len(candidates))
	batch := slices.Clone(candidates[:batchSize])
	if err := disableINIs(batch); err != nil {
		rollbackErr := enableINIs(batch)
		guardErr := t.stopD3dxGuardLocked(game)
		t.bisect = nil
		return BisectSnapshot{}, infra.WithCause(err, infra.AnnotateError(errors.Join(rollbackErr, guardErr), infra.Diagnostic{Stage: "rollback", Fields: map[string]any{"game": game}}))
	}
	session.candidates = candidates
	session.round = 1
	session.batchSize = batchSize
	session.currentBatch = batch
	session.phase = BisectRound
	snapshot := snapshotFor(session, nil, BisectRound)
	t.broadcastBisect(snapshot)
	return snapshot, nil
}

func (t *Tools) BisectRespond(ctx context.Context, fixed bool) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	session, err := t.requireBisectSessionLocked()
	if err != nil {
		return BisectSnapshot{}, err
	}
	if len(session.currentBatch) == 0 {
		return BisectSnapshot{}, errors.New("No active batch to respond to.") //nolint:staticcheck // Electron contract text.
	}

	currentBatch := slices.Clone(session.currentBatch)
	candidatesBefore := slices.Clone(session.candidates)
	remaining := currentBatch
	if !fixed {
		batchSet := lowerPathSet(currentBatch)
		remaining = make([]string, 0, len(candidatesBefore)-len(currentBatch))
		for _, candidate := range candidatesBefore {
			if _, inBatch := batchSet[strings.ToLower(candidate)]; !inBatch {
				remaining = append(remaining, candidate)
			}
		}
	}
	undo := bisectUndo{
		disabled: currentBatch, candidatesBefore: candidatesBefore, remainingAfter: slices.Clone(remaining),
		round: session.round, batchSize: session.batchSize,
	}

	if len(remaining) == 1 {
		culprit := remaining[0]
		if !fixed {
			if err := disableINIs([]string{culprit}); err != nil {
				return BisectSnapshot{}, err
			}
		}
		session.candidates = remaining
		session.undoStack = append(session.undoStack, undo)
		if fixed {
			session.currentBatch = []string{}
		} else {
			session.currentBatch = []string{culprit}
		}
		session.finalBadPath = stringPointer(culprit)
		snapshot := snapshotFor(session, nil, BisectDone)
		t.broadcastBisect(snapshot)
		return snapshot, nil
	}

	if len(remaining) == 0 {
		toRestore := disabledBisectSet(session)
		if err := enableINIs(toRestore); err != nil {
			return BisectSnapshot{}, err
		}
		if err := t.stopD3dxGuardLocked(session.game); err != nil {
			t.logError(err, "ModBisect:d3dxFinalRestore")
		}
		t.bisect = nil
		snapshot := snapshotFor(session, stringPointer(bisectInconclusive), BisectDone)
		t.broadcastBisect(snapshot)
		return snapshot, nil
	}

	nextBatchSize := chooseBisectBatchSize(len(remaining))
	nextBatch := slices.Clone(remaining[:nextBatchSize])
	if err := switchBisectBatch(currentBatch, nextBatch); err != nil {
		return BisectSnapshot{}, err
	}
	session.undoStack = append(session.undoStack, undo)
	session.candidates = remaining
	session.currentBatch = nextBatch
	session.batchSize = nextBatchSize
	session.round++
	snapshot := snapshotFor(session, nil, BisectRound)
	t.broadcastBisect(snapshot)
	return snapshot, nil
}

func (t *Tools) BisectUndoLastRound(_ context.Context) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	session, err := t.requireBisectSessionLocked()
	if err != nil {
		return BisectSnapshot{}, err
	}
	if len(session.undoStack) == 0 {
		return BisectSnapshot{}, errors.New("Nothing to undo.") //nolint:staticcheck // Electron contract text.
	}
	last := session.undoStack[len(session.undoStack)-1]
	if err := switchBisectBatch(session.currentBatch, last.disabled); err != nil {
		return BisectSnapshot{}, err
	}
	session.undoStack = session.undoStack[:len(session.undoStack)-1]
	session.candidates = last.candidatesBefore
	session.currentBatch = last.disabled
	session.batchSize = last.batchSize
	session.round = last.round
	session.finalBadPath = nil
	snapshot := snapshotFor(session, nil, BisectRound)
	t.broadcastBisect(snapshot)
	return snapshot, nil
}

func (t *Tools) BisectFinalize(ctx context.Context, keepDisabled []string) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	session, err := t.requireBisectSessionLocked()
	if err != nil {
		return BisectSnapshot{}, err
	}
	disabled := disabledBisectSet(session)
	keepSet := lowerPathSet(keepDisabled)
	var restore, keep []string
	for _, iniPath := range disabled {
		if _, ok := keepSet[strings.ToLower(iniPath)]; ok {
			keep = append(keep, iniPath)
		} else {
			restore = append(restore, iniPath)
		}
	}
	if err := enableINIs(restore); err != nil {
		return BisectSnapshot{}, err
	}
	style := "space"
	if len(keep) > 0 && t.settings != nil {
		style, err = t.settings.GetDisabledPrefixStyle(ctx)
		if err != nil {
			return BisectSnapshot{}, err
		}
	}
	for _, iniPath := range keep {
		if err := keepBisectINIDisabled(iniPath, style); err != nil {
			return BisectSnapshot{}, err
		}
	}
	if err := t.stopD3dxGuardLocked(session.game); err != nil {
		t.logError(err, "ModBisect:d3dxFinalRestore")
	}
	t.bisect = nil
	final := snapshotFor(session, nil, BisectIdle)
	reverting := final
	reverting.Status = BisectReverting
	t.broadcastBisect(reverting)
	t.broadcastBisect(final)
	return final, nil
}

func (t *Tools) BisectCancel(ctx context.Context) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	if t.bisect == nil {
		idle := emptyBisectSnapshot(BisectIdle)
		t.broadcastBisect(idle)
		return idle, nil
	}
	session := t.bisect
	t.broadcastBisect(snapshotFor(session, nil, BisectCancelled))
	if err := t.cancelBisectLocked(); err != nil {
		return BisectSnapshot{}, err
	}
	reverting := snapshotFor(session, nil, BisectReverting)
	t.broadcastBisect(reverting)
	idle := emptyBisectSnapshot(BisectIdle)
	t.broadcastBisect(idle)
	return idle, nil
}

func (t *Tools) BisectRecover(ctx context.Context, game string) (int, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	if t.bisect != nil {
		return 0, errors.New("Cannot recover while a bisect session is active.") //nolint:staticcheck // Electron contract text.
	}
	config, err := t.requireBisectGame(ctx, game)
	if err != nil {
		return 0, err
	}
	paths, err := listBisectOrphans(config.ModFolderPath)
	if err != nil {
		return 0, err
	}
	return t.recoverINIs(paths), nil
}

// RecoverBisects restores interrupted sessions for every configured non-NTE game.
//
//wails:ignore
func (t *Tools) RecoverBisects(ctx context.Context) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	games, err := client.GamePaths.List(ctx)
	if err != nil {
		return err
	}
	for _, game := range games {
		if game.ModFolderPath == "" || game.Importer != nil && *game.Importer == "NTE" {
			continue
		}
		if err := t.recoverD3dxBackupLocked(ctx, game); err != nil {
			t.logError(err, "ModBisect:d3dxRecover")
		}
		orphans, err := listBisectOrphans(game.ModFolderPath)
		if err != nil {
			return err
		}
		t.recoverINIs(orphans)
	}
	return nil
}

func (t *Tools) requireBisectGame(ctx context.Context, game string) (db.GamePathRow, error) {
	client, err := t.requireClient()
	if err != nil {
		return db.GamePathRow{}, err
	}
	row, err := client.GamePaths.GetByGame(ctx, strings.TrimSpace(game))
	if err != nil {
		return db.GamePathRow{}, err
	}
	if row == nil {
		return db.GamePathRow{}, fmt.Errorf("Game not found: %s", game) //nolint:staticcheck // Electron contract text.
	}
	if row.Importer != nil && *row.Importer == "NTE" {
		return db.GamePathRow{}, errors.New("NTE modders are not supported yet.") //nolint:staticcheck // Electron contract text.
	}
	if strings.TrimSpace(row.ModFolderPath) == "" {
		return db.GamePathRow{}, fmt.Errorf("Mod folder path is not configured for %s.", game) //nolint:staticcheck // Electron contract text.
	}
	return *row, nil
}

func (t *Tools) requireBisectSessionLocked() (*bisectSession, error) {
	if t.bisect == nil {
		return nil, errors.New("No active bisect session.") //nolint:staticcheck // Electron contract text.
	}
	return t.bisect, nil
}

func (t *Tools) cancelBisectLocked() error {
	session := t.bisect
	if session == nil {
		return nil
	}
	if err := enableINIs(disabledBisectSet(session)); err != nil {
		return err
	}
	if err := t.stopD3dxGuardLocked(session.game); err != nil {
		t.logError(err, "ModBisect:d3dxFinalRestore")
	}
	t.bisect = nil
	return nil
}

func (t *Tools) shutdownBisect() error {
	if t == nil {
		return nil
	}
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	return t.cancelBisectLocked()
}

func (t *Tools) recoverINIs(paths []string) int {
	restored := 0
	for _, original := range paths {
		disabled := bisectDisabledPath(original)
		if _, err := os.Stat(original); err == nil {
			if err := os.Remove(disabled); err == nil || errors.Is(err, os.ErrNotExist) {
				restored++
			}
			continue
		}
		if err := os.Rename(disabled, original); err != nil {
			t.logError(err, "ModBisect:recover")
		} else {
			restored++
		}
	}
	return restored
}

func (t *Tools) broadcastBisect(snapshot BisectSnapshot) { t.emitEvent(bisectStateEvent, snapshot) }

func snapshotFor(session *bisectSession, message *string, status BisectStatus) BisectSnapshot {
	root := session.modRootPath
	return BisectSnapshot{
		Status: status, Game: session.game, ModRootPath: &root, Round: session.round, BatchSize: session.batchSize,
		Candidates: slices.Clone(session.candidates), CurrentBatch: slices.Clone(session.currentBatch),
		ExcludePaths: slices.Clone(session.excludePaths), UndoStackDepth: len(session.undoStack),
		FinalBadPath: cloneStringPointer(session.finalBadPath), Error: cloneStringPointer(message),
	}
}

func emptyBisectSnapshot(status BisectStatus) BisectSnapshot {
	return BisectSnapshot{Status: status, Candidates: []string{}, CurrentBatch: []string{}, ExcludePaths: []string{}}
}

func scanEnabledINIs(root string) ([]string, error) {
	matches, err := doublestar.FilepathGlob(
		filepath.Join(root, "**", "*.ini"),
		doublestar.WithCaseInsensitive(),
		doublestar.WithFilesOnly(),
	)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(matches))
	for _, path := range matches {
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil || bisectPathIsHidden(relative) || isDisabledBisectRelative(relative) {
			continue
		}
		paths = append(paths, filepath.Clean(path))
	}
	sort.Strings(paths)
	return paths, nil
}

func bisectPathIsHidden(relative string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(relative), func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func isDisabledBisectDir(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "disabled ") || strings.HasPrefix(lower, "disabled_")
}

func isDisabledBisectRelative(relative string) bool {
	parts := strings.FieldsFunc(filepath.ToSlash(relative), func(r rune) bool { return r == '/' || r == '\\' })
	for _, part := range parts[:max(0, len(parts)-1)] {
		if isDisabledBisectDir(part) {
			return true
		}
	}
	return len(parts) > 0 && strings.HasPrefix(strings.ToLower(parts[len(parts)-1]), "disabled")
}

func resolveBisectExclude(root, input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", contractError(bisectExcludeEmpty)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	inputAbs := trimmed
	if !filepath.IsAbs(inputAbs) {
		inputAbs = filepath.Join(rootAbs, inputAbs)
	}
	inputAbs, err = filepath.Abs(inputAbs)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, inputAbs)
	if err != nil || pathEscapes(relative) {
		return "", infra.WithCause(contractError(bisectExcludeOutside), err)
	}
	if relative == "." {
		return "", contractError(bisectExcludeRoot)
	}
	if _, err := os.Stat(inputAbs); errors.Is(err, os.ErrNotExist) {
		return "", infra.WithCause(contractError(bisectExcludeMissing), err)
	} else if err != nil {
		return "", err
	}
	canonicalRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	canonicalInput, err := filepath.EvalSymlinks(inputAbs)
	if err != nil {
		return "", err
	}
	canonicalRel, err := filepath.Rel(canonicalRoot, canonicalInput)
	if err != nil || pathEscapes(canonicalRel) {
		return "", infra.WithCause(contractError(bisectExcludeOutside), err)
	}
	return filepath.ToSlash(relative), nil
}

func pathEscapes(relative string) bool {
	return relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative)
}

func anyPathContains(roots []string, candidate string) bool {
	for _, root := range roots {
		relative, err := filepath.Rel(root, candidate)
		if err == nil && !pathEscapes(relative) {
			return true
		}
	}
	return false
}

func chooseBisectBatchSize(count int) int { return (count + 1) / 2 }

func disabledBisectSet(session *bisectSession) []string {
	seen := make(map[string]struct{})
	var result []string
	appendUnique := func(paths []string) {
		for _, path := range paths {
			key := strings.ToLower(path)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				result = append(result, path)
			}
		}
	}
	for _, undo := range session.undoStack {
		appendUnique(undo.disabled)
	}
	appendUnique(session.currentBatch)
	return result
}

func disableINIs(paths []string) error {
	for _, path := range paths {
		if err := renameINIDisable(path); err != nil {
			return err
		}
	}
	return nil
}

func enableINIs(paths []string) error {
	for _, path := range paths {
		if err := renameINIEnable(path); err != nil {
			return err
		}
	}
	return nil
}

func switchBisectBatch(oldBatch, newBatch []string) error {
	if err := enableINIs(oldBatch); err != nil {
		return err
	}
	disabled := make([]string, 0, len(newBatch))
	for _, path := range newBatch {
		if err := renameINIDisable(path); err != nil {
			rollbackErr := enableINIs(disabled)
			restoreErr := disableINIs(oldBatch)
			return infra.WithCause(err, infra.AnnotateError(errors.Join(rollbackErr, restoreErr), infra.Diagnostic{Stage: "rollback", Fields: map[string]any{"path": path}}))
		}
		disabled = append(disabled, path)
	}
	return nil
}

func bisectDisabledPath(iniPath string) string { return iniPath + "." + bisectDisabledSuffix }

func renameINIDisable(iniPath string) error {
	target := bisectDisabledPath(iniPath)
	if _, originalErr := os.Stat(iniPath); errors.Is(originalErr, os.ErrNotExist) {
		if _, targetErr := os.Stat(target); targetErr == nil {
			return nil
		}
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("cannot disable ini because target already exists: %s", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(iniPath, target); err != nil {
		return err
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("failed to disable ini %s: %w", iniPath, err)
	}
	return nil
}

func renameINIEnable(iniPath string) error {
	target := bisectDisabledPath(iniPath)
	if _, originalErr := os.Stat(iniPath); originalErr == nil {
		if _, targetErr := os.Stat(target); errors.Is(targetErr, os.ErrNotExist) {
			return nil
		}
	}
	if _, err := os.Stat(iniPath); err == nil {
		return fmt.Errorf("cannot restore ini because original already exists: %s", iniPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(target, iniPath); err != nil {
		return err
	}
	return nil
}

func keepBisectINIDisabled(originalPath, style string) error {
	source := bisectDisabledPath(originalPath)
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		source = originalPath
	} else if err != nil {
		return err
	}
	prefix := "DISABLED "
	if style == "underscore" {
		prefix = "DISABLED_"
	}
	target := filepath.Join(filepath.Dir(originalPath), prefix+filepath.Base(originalPath))
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrExist) || errors.Is(err, os.ErrPermission) {
			return nil
		}
		return err
	}
	return nil
}

func listBisectOrphans(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), "."+bisectDisabledSuffix) {
			paths = append(paths, path[:len(path)-len(bisectDisabledSuffix)-1])
		}
		return nil
	})
	return paths, err
}

func lowerPathSet(paths []string) map[string]struct{} {
	result := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		result[strings.ToLower(path)] = struct{}{}
	}
	return result
}

func stringPointer(value string) *string { return &value }

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
