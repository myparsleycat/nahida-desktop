package modbisect

import (
	"bytes"
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
	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
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
	bisectOutOfSync      = "Bisect files are out of sync. Cancel the session to restore them."
	bisectStateChanged   = "The bisect state changed. Review the current round and answer again."
	// bisectRecoverConflict prefixes the number of disabled INIs kept because a different file now holds the
	// original name.
	bisectRecoverConflict = "BISECT_RECOVER_CONFLICT"
)

var (
	// errBisectOutOfSync marks a rename failure whose rollback also failed, so the files no longer match the session.
	errBisectOutOfSync = errors.New("bisect files are out of sync")

	// ErrBisectOutOfSync refuses an answer while the files do not match the session.
	ErrBisectOutOfSync error = infra.ContractError(bisectOutOfSync)
	// ErrBisectStateChanged refuses an action made against an older version of the state.
	ErrBisectStateChanged error = infra.ContractError(bisectStateChanged)
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

// BisectSnapshot is the bisect state. Version increases with every broadcast, so a newer state is never replaced by an
// older one, and Respond, UndoLastRound and Finalize act only on the version the caller saw. Active is set while the
// backend holds a session, including a finished one that is not finalized yet.
type BisectSnapshot struct {
	Version uint64 `json:"version"`
	Active  bool   `json:"active"`
	// OutOfSync is set when renamed files no longer match the session; only cancel and finalize are accepted.
	OutOfSync      bool         `json:"outOfSync"`
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
	// outOfSync is set when a failed rename could not be rolled back; answers are refused until the session is
	// cancelled or finalized.
	outOfSync bool
}

type bisectUndo struct {
	disabled         []string
	candidatesBefore []string
	remainingAfter   []string
	round            int
	batchSize        int
}

func (t *Service) BisectGetState() *BisectSnapshot {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	snapshot := emptyBisectSnapshot(BisectIdle)
	if t.bisect != nil {
		snapshot = snapshotFor(t.bisect, nil, sessionStatus(t.bisect))
		snapshot.Active = true
	}
	snapshot.Version = t.bisectVersion
	return &snapshot
}

func sessionStatus(session *bisectSession) BisectStatus {
	switch {
	case session.finalBadPath != nil:
		return BisectDone
	case session.phase == BisectScanning:
		return BisectScanning
	default:
		return BisectRound
	}
}

func (t *Service) BisectValidateExcludePath(ctx context.Context, game, inputPath string) (string, error) {
	config, err := t.requireBisectGame(ctx, game)
	if err != nil {
		return "", err
	}
	return resolveBisectExclude(config.ModFolderPath, inputPath)
}

func (t *Service) BisectStart(ctx context.Context, game string, excludePaths []string) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	if t.bisectRecovering {
		//nolint:staticcheck // Electron contract text.
		return BisectSnapshot{}, errors.New(
			"Cannot start a bisect session while recovery is running.",
		)
	}
	if t.bisect != nil && t.bisect.finalBadPath == nil {
		//nolint:staticcheck // Electron contract text.
		return BisectSnapshot{}, errors.New(
			"A bisect session is already running. Cancel it first.",
		)
	}
	if t.bisect != nil {
		completed := t.bisect
		t.broadcastBisect(snapshotFor(completed, nil, BisectCancelled))
		if err := t.cancelBisectLocked(); err != nil {
			t.broadcastBisect(snapshotFor(completed, nil, sessionStatus(completed)))
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
		t.abortStartLocked()
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
			message = lo.ToPtr(bisectAllExcluded)
		}
		done := scanning
		done.Status = BisectDone
		done.Error = message
		return t.broadcastBisect(done), nil
	}

	if t.settings != nil {
		preserve, settingErr := t.settings.GetBisectPreserveD3dx(ctx)
		if settingErr != nil {
			t.abortStartLocked()
			return BisectSnapshot{}, settingErr
		}
		if preserve {
			if err := t.startD3dxGuardLocked(ctx, config); err != nil {
				t.logError(err, "ModBisect:d3dxGuard")
			}
		}
	}

	if err := t.writeSessionMarker(ctx, game, config.ModFolderPath, candidates); err != nil {
		guardErr := t.stopD3dxGuardLocked(game)
		t.abortStartLocked()
		return BisectSnapshot{}, errors.Join(err, guardErr)
	}

	batchSize := chooseBisectBatchSize(len(candidates))
	batch := slices.Clone(candidates[:batchSize])
	if err := disableINIs(batch); err != nil {
		rollbackErr := restoreINIs(batch)
		if rollbackErr == nil {
			t.clearSessionMarker(ctx, game)
		}
		guardErr := t.stopD3dxGuardLocked(game)
		t.abortStartLocked()
		return BisectSnapshot{}, infra.WithCause(
			err,
			infra.AnnotateError(
				errors.Join(rollbackErr, guardErr),
				infra.Diagnostic{Stage: "rollback", Fields: map[string]any{"game": game}},
			),
		)
	}
	session.candidates = candidates
	session.round = 1
	session.batchSize = batchSize
	session.currentBatch = batch
	session.phase = BisectRound
	return t.broadcastBisect(snapshotFor(session, nil, BisectRound)), nil
}

// abortStartLocked drops a session that failed to start and tells every screen, which saw it scanning.
func (t *Service) abortStartLocked() {
	t.bisect = nil
	t.broadcastBisect(emptyBisectSnapshot(BisectIdle))
}

func (t *Service) BisectRespond(ctx context.Context, version uint64, fixed bool) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	session, err := t.requireBisectVersionLocked(version)
	if err != nil {
		return BisectSnapshot{}, err
	}
	if session.outOfSync {
		return BisectSnapshot{}, ErrBisectOutOfSync
	}
	if len(session.currentBatch) == 0 {
		//nolint:staticcheck // Electron contract text.
		return BisectSnapshot{}, errors.New(
			"No active batch to respond to.",
		)
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
				return BisectSnapshot{}, t.failLocked(session, err)
			}
		}
		session.candidates = remaining
		session.undoStack = append(session.undoStack, undo)
		if fixed {
			session.currentBatch = []string{}
		} else {
			session.currentBatch = []string{culprit}
		}
		session.finalBadPath = lo.ToPtr(culprit)
		return t.broadcastBisect(snapshotFor(session, nil, BisectDone)), nil
	}

	if len(remaining) == 0 {
		toRestore := disabledBisectSet(session)
		if err := enableINIs(toRestore); err != nil {
			return BisectSnapshot{}, t.failLocked(session, err)
		}
		t.clearSessionMarker(ctx, session.game)
		if err := t.stopD3dxGuardLocked(session.game); err != nil {
			t.logError(err, "ModBisect:d3dxFinalRestore")
		}
		t.bisect = nil
		return t.broadcastBisect(snapshotFor(session, lo.ToPtr(bisectInconclusive), BisectDone)), nil
	}

	nextBatchSize := chooseBisectBatchSize(len(remaining))
	nextBatch := slices.Clone(remaining[:nextBatchSize])
	if err := switchBisectBatch(currentBatch, nextBatch); err != nil {
		return BisectSnapshot{}, t.failLocked(session, err)
	}
	session.undoStack = append(session.undoStack, undo)
	session.candidates = remaining
	session.currentBatch = nextBatch
	session.batchSize = nextBatchSize
	session.round++
	return t.broadcastBisect(snapshotFor(session, nil, BisectRound)), nil
}

func (t *Service) BisectUndoLastRound(_ context.Context, version uint64) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	session, err := t.requireBisectVersionLocked(version)
	if err != nil {
		return BisectSnapshot{}, err
	}
	if session.outOfSync {
		return BisectSnapshot{}, ErrBisectOutOfSync
	}
	if len(session.undoStack) == 0 {
		return BisectSnapshot{}, errors.New("Nothing to undo.") //nolint:staticcheck // Electron contract text.
	}
	last := session.undoStack[len(session.undoStack)-1]
	if err := switchBisectBatch(session.currentBatch, last.disabled); err != nil {
		return BisectSnapshot{}, t.failLocked(session, err)
	}
	session.undoStack = session.undoStack[:len(session.undoStack)-1]
	session.candidates = last.candidatesBefore
	session.currentBatch = last.disabled
	session.batchSize = last.batchSize
	session.round = last.round
	session.finalBadPath = nil
	return t.broadcastBisect(snapshotFor(session, nil, BisectRound)), nil
}

func (t *Service) BisectFinalize(ctx context.Context, version uint64, keepDisabled []string) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	session, err := t.requireBisectVersionLocked(version)
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
	style := "space"
	if len(keep) > 0 && t.settings != nil {
		style, err = t.settings.GetDisabledPrefixStyle(ctx)
		if err != nil {
			return BisectSnapshot{}, err
		}
	}
	if err := enableINIs(restore); err != nil {
		return BisectSnapshot{}, t.failLocked(session, err)
	}
	for _, iniPath := range keep {
		if err := keepBisectINIDisabled(iniPath, style); err != nil {
			// The restored files no longer match the session; a retry or a cancel finishes from here.
			t.setOutOfSyncLocked(session)
			return BisectSnapshot{}, err
		}
	}
	t.clearSessionMarker(ctx, session.game)
	if err := t.stopD3dxGuardLocked(session.game); err != nil {
		t.logError(err, "ModBisect:d3dxFinalRestore")
	}
	t.bisect = nil
	final := snapshotFor(session, nil, BisectIdle)
	reverting := final
	reverting.Status = BisectReverting
	t.broadcastBisect(reverting)
	return t.broadcastBisect(final), nil
}

func (t *Service) BisectCancel(ctx context.Context) (BisectSnapshot, error) {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	if t.bisect == nil {
		return t.broadcastBisect(emptyBisectSnapshot(BisectIdle)), nil
	}
	session := t.bisect
	t.broadcastBisect(snapshotFor(session, nil, BisectCancelled))
	if err := t.cancelBisectLocked(); err != nil {
		t.broadcastBisect(snapshotFor(session, nil, sessionStatus(session)))
		return BisectSnapshot{}, err
	}
	t.broadcastBisect(snapshotFor(session, nil, BisectReverting))
	return t.broadcastBisect(emptyBisectSnapshot(BisectIdle)), nil
}

// BisectRecoverConflictError reports the disabled INIs recovery kept because a different file now holds the
// original name. BisectRecover returns it together with the number it restored.
type BisectRecoverConflictError struct{ Conflicts int }

func (e *BisectRecoverConflictError) Error() string {
	return fmt.Sprintf("%s:%d", bisectRecoverConflict, e.Conflicts)
}

// BisectRecover restores the INIs an interrupted session left disabled. A disabled INI whose original name is now
// held by a different file is kept and counted in a *BisectRecoverConflictError.
func (t *Service) BisectRecover(ctx context.Context, game string) (int, error) {
	if err := t.beginBisectRecovery(); err != nil {
		//nolint:staticcheck // Electron contract text.
		return 0, errors.New(
			"Cannot recover while a bisect session is active.",
		)
	}
	defer t.endBisectRecovery()
	config, err := t.requireBisectGame(ctx, game)
	if err != nil {
		return 0, err
	}
	markers, err := t.sessionMarkers(ctx)
	if err != nil {
		return 0, err
	}
	var marked []string
	markerGame := ""
	for _, marker := range markers {
		if (marker.game == game || marker.game == config.Game) &&
			platform.SamePathFold(marker.ModRootPath, config.ModFolderPath) {
			marked, markerGame = marker.Paths, marker.game
		}
	}
	paths, err := bisectRecoveryTargets(ctx, config.ModFolderPath, marked)
	if err != nil {
		return 0, err
	}
	restored, conflicts := t.recoverINIs(ctx, paths)
	if err := ctx.Err(); err != nil {
		return restored, err
	}
	if markerGame != "" && restored+len(conflicts) == len(paths) {
		t.clearSessionMarker(ctx, markerGame)
	}
	if len(conflicts) > 0 {
		return restored, infra.ReportError(
			t.log,
			&BisectRecoverConflictError{Conflicts: len(conflicts)},
			"Tools",
			infra.Diagnostic{
				Operation: "ModBisect:recover", Stage: "conflict",
				Fields: map[string]any{"game": config.Game, "conflicts": firstPaths(conflicts)},
			},
		)
	}
	return restored, nil
}

// RecoverBisects restores interrupted sessions. It walks only the mod folders of sessions that left a marker,
// plus every configured non-NTE game once, for orphans left by builds that did not write markers.
func (t *Service) RecoverBisects(ctx context.Context) error {
	if err := t.beginBisectRecovery(); err != nil {
		return err
	}
	defer t.endBisectRecovery()

	client, err := t.requireClient()
	if err != nil {
		return err
	}
	rows, err := client.GamePaths.List(ctx)
	if err != nil {
		return err
	}
	games := make([]db.GamePathRow, 0, len(rows))
	for _, game := range rows {
		if game.ModFolderPath == "" || game.Importer != nil && *game.Importer == "NTE" {
			continue
		}
		games = append(games, game)
		if err := t.recoverD3dxBackupLocked(ctx, game); err != nil {
			t.logError(err, "ModBisect:d3dxRecover")
		}
	}

	legacyDone, err := client.AppState.GetValue(ctx, legacyScanDoneKey)
	if err != nil {
		return err
	}
	if legacyDone == nil {
		complete := true
		for _, game := range games {
			orphans, err := listBisectOrphans(ctx, game.ModFolderPath)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// One unreadable folder must not hold back the other games, the markers or the done key, or
			// every launch would repeat this walk.
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					t.logError(err, "ModBisect:recover")
				}
				continue
			}
			// A conflict is logged once and left for manual recovery; it does not hold back the done key.
			restored, conflicts := t.recoverINIs(ctx, orphans)
			t.logRecoverConflicts(game.Game, conflicts)
			complete = restored+len(conflicts) == len(orphans) && complete
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if complete {
			if err := client.AppState.Upsert(ctx, legacyScanDoneKey, "1", appStateTimestamp()); err != nil {
				return err
			}
		}
	}

	markers, err := t.sessionMarkers(ctx)
	if err != nil {
		return err
	}
	for _, marker := range markers {
		orphans, err := bisectRecoveryTargets(ctx, marker.ModRootPath, marker.Paths)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A missing root keeps its marker: it may be an unplugged drive that still holds renamed INIs.
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				t.logError(err, "ModBisect:recover")
			}
			continue
		}
		// A conflict is logged with its paths and does not keep the marker, or every launch would repeat the walk.
		restored, conflicts := t.recoverINIs(ctx, orphans)
		t.logRecoverConflicts(marker.game, conflicts)
		if restored+len(conflicts) == len(orphans) && ctx.Err() == nil {
			t.clearSessionMarker(ctx, marker.game)
		}
	}
	return ctx.Err()
}

func (t *Service) logRecoverConflicts(game string, conflicts []string) {
	if len(conflicts) == 0 {
		return
	}
	_ = infra.ReportError(t.log, errors.New("disabled INI kept because the original name is taken"), "Tools",
		infra.Diagnostic{
			Operation: "ModBisect:recover", Stage: "conflict",
			Fields: map[string]any{"game": game, "count": len(conflicts), "conflicts": firstPaths(conflicts)},
		})
}

func firstPaths(paths []string) []string {
	const limit = 10
	return paths[:min(len(paths), limit)]
}

// bisectRecoveryTargets lists the INIs to restore below root: the disabled files the walk finds, plus the
// session's recorded candidates that are still disabled. The walk does not enter junctions below root, but the
// session scan does, so only the record reaches INIs behind them.
func bisectRecoveryTargets(ctx context.Context, root string, marked []string) ([]string, error) {
	targets, err := listBisectOrphans(ctx, root)
	if err != nil {
		return nil, err
	}
	seen := lowerPathSet(targets)
	for _, relative := range marked {
		local := filepath.FromSlash(relative)
		if local == "" || filepath.IsAbs(local) || pathEscapes(filepath.Clean(local)) {
			continue
		}
		original := filepath.Join(root, local)
		if _, ok := seen[strings.ToLower(original)]; ok {
			continue
		}
		if _, err := os.Lstat(bisectDisabledPath(original)); err != nil {
			continue
		}
		seen[strings.ToLower(original)] = struct{}{}
		targets = append(targets, original)
	}
	return targets, nil
}

func (t *Service) beginBisectRecovery() error {
	t.bisectMu.Lock()
	defer t.bisectMu.Unlock()
	if t.bisect != nil || t.bisectRecovering {
		return errors.New("cannot recover while a bisect session is active")
	}
	t.bisectRecovering = true
	return nil
}

func (t *Service) endBisectRecovery() {
	t.bisectMu.Lock()
	t.bisectRecovering = false
	t.bisectMu.Unlock()
}

func (t *Service) requireBisectGame(ctx context.Context, game string) (db.GamePathRow, error) {
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
		//nolint:staticcheck // Electron contract text.
		return db.GamePathRow{}, errors.New(
			"NTE modders are not supported yet.",
		)
	}
	if strings.TrimSpace(row.ModFolderPath) == "" {
		//nolint:staticcheck // Electron contract text.
		return db.GamePathRow{}, fmt.Errorf(
			"Mod folder path is not configured for %s.",
			game,
		)
	}
	return *row, nil
}

func (t *Service) requireBisectSessionLocked() (*bisectSession, error) {
	if t.bisect == nil {
		return nil, errors.New("No active bisect session.") //nolint:staticcheck // Electron contract text.
	}
	return t.bisect, nil
}

func (t *Service) requireBisectVersionLocked(version uint64) (*bisectSession, error) {
	session, err := t.requireBisectSessionLocked()
	if err != nil {
		return nil, err
	}
	if version != t.bisectVersion {
		return nil, ErrBisectStateChanged
	}
	return session, nil
}

// failLocked marks the session out of sync when err says the files no longer match it, then returns err.
func (t *Service) failLocked(session *bisectSession, err error) error {
	if errors.Is(err, errBisectOutOfSync) {
		t.setOutOfSyncLocked(session)
	}
	return err
}

func (t *Service) setOutOfSyncLocked(session *bisectSession) {
	session.outOfSync = true
	t.broadcastBisect(snapshotFor(session, nil, sessionStatus(session)))
}

// cancelBisectLocked restores every INI the session disabled. It tries all of them; after a failure the session
// stays, out of sync, so a retry can finish.
func (t *Service) cancelBisectLocked() error {
	session := t.bisect
	if session == nil {
		return nil
	}
	if err := restoreINIs(disabledBisectSet(session)); err != nil {
		session.outOfSync = true
		return err
	}
	t.clearSessionMarker(context.Background(), session.game)
	if err := t.stopD3dxGuardLocked(session.game); err != nil {
		t.logError(err, "ModBisect:d3dxFinalRestore")
	}
	t.bisect = nil
	return nil
}

// recoverINIs renames disabled INIs back. When the original name is taken, a disabled copy with the same content is
// removed, and one with different content is kept and returned as a conflict.
func (t *Service) recoverINIs(ctx context.Context, paths []string) (restored int, conflicts []string) {
	for _, original := range paths {
		if ctx.Err() != nil {
			break
		}
		disabled := bisectDisabledPath(original)
		if _, err := os.Stat(original); err == nil {
			same, err := sameFileContent(disabled, original)
			switch {
			case errors.Is(err, os.ErrNotExist):
				restored++
			case err != nil:
				t.logError(err, "ModBisect:recover")
			case !same:
				conflicts = append(conflicts, original)
			default:
				if err := os.Remove(disabled); err == nil || errors.Is(err, os.ErrNotExist) {
					restored++
				} else {
					t.logError(err, "ModBisect:recover")
				}
			}
			continue
		}
		if err := os.Rename(disabled, original); err != nil {
			t.logError(err, "ModBisect:recover")
		} else {
			restored++
		}
	}
	return restored, conflicts
}

// sameFileContent compares two files byte for byte. A missing first file returns os.ErrNotExist.
func sameFileContent(first, second string) (bool, error) {
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false, err
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		return false, err
	}
	if firstInfo.Size() != secondInfo.Size() {
		return false, nil
	}
	firstData, err := os.ReadFile(first)
	if err != nil {
		return false, err
	}
	secondData, err := os.ReadFile(second)
	if err != nil {
		return false, err
	}
	return bytes.Equal(firstData, secondData), nil
}

// broadcastBisect stamps snapshot with the next version and whether a session exists, emits it and returns it.
func (t *Service) broadcastBisect(snapshot BisectSnapshot) BisectSnapshot {
	t.bisectVersion++
	snapshot.Version = t.bisectVersion
	snapshot.Active = t.bisect != nil
	t.emitEvent(bisectStateEvent, snapshot)
	return snapshot
}

func snapshotFor(session *bisectSession, message *string, status BisectStatus) BisectSnapshot {
	if message == nil && session.outOfSync {
		message = lo.ToPtr(bisectOutOfSync)
	}
	root := session.modRootPath
	return BisectSnapshot{
		OutOfSync: session.outOfSync,
		Status:    status, Game: session.game, ModRootPath: &root, Round: session.round, BatchSize: session.batchSize,
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
	paths := lo.FilterMap(matches, func(path string, _ int) (string, bool) {
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil || bisectPathIsHidden(relative) || isDisabledBisectRelative(relative) {
			return "", false
		}
		return filepath.Clean(path), true
	})
	sort.Strings(paths)
	return paths, nil
}

func bisectPathIsHidden(relative string) bool {
	isSeparator := func(r rune) bool { return r == '/' || r == '\\' }
	for _, part := range strings.FieldsFunc(filepath.ToSlash(relative), isSeparator) {
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
		return "", infra.ContractError(bisectExcludeEmpty)
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
		return "", infra.WithCause(infra.ContractError(bisectExcludeOutside), err)
	}
	if relative == "." {
		return "", infra.ContractError(bisectExcludeRoot)
	}
	if _, err := os.Stat(inputAbs); errors.Is(err, os.ErrNotExist) {
		return "", infra.WithCause(infra.ContractError(bisectExcludeMissing), err)
	} else if err != nil {
		return "", err
	}
	canonicalRoot, err := platform.FinalPath(rootAbs)
	if err != nil {
		return "", err
	}
	resolvedInput, err := platform.ResolveLinks(inputAbs)
	if err != nil {
		return "", err
	}
	canonicalInput, err := platform.FinalEntryPath(resolvedInput)
	if err != nil {
		return "", err
	}
	canonicalRel, err := filepath.Rel(canonicalRoot, canonicalInput)
	if err != nil || pathEscapes(canonicalRel) {
		return "", infra.WithCause(infra.ContractError(bisectExcludeOutside), err)
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

func disableINIs(paths []string) error { return renameINIs(paths, renameINIDisable, renameINIEnable) }

func enableINIs(paths []string) error { return renameINIs(paths, renameINIEnable, renameINIDisable) }

// renameINIs applies rename to each path in order. On a failure it reverts the paths it actually renamed, newest
// first, so the files are left as they were; if a revert also fails the error wraps errBisectOutOfSync.
func renameINIs(paths []string, rename, revert func(string) (bool, error)) error {
	var renamed []string
	for _, path := range paths {
		changed, err := rename(path)
		if err == nil {
			if changed {
				renamed = append(renamed, path)
			}
			continue
		}
		var revertErrs []error
		for undo := len(renamed) - 1; undo >= 0; undo-- {
			if _, revertErr := revert(renamed[undo]); revertErr != nil {
				revertErrs = append(revertErrs, revertErr)
			}
		}
		if len(revertErrs) == 0 {
			return err
		}
		return outOfSyncError(err, errors.Join(revertErrs...), path)
	}
	return nil
}

// restoreINIs enables every path, continuing past failures, for paths that must end up enabled either way.
func restoreINIs(paths []string) error {
	var errs []error
	for _, path := range paths {
		if _, err := renameINIEnable(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// switchBisectBatch enables oldBatch and disables newBatch. On a failure the files are put back as oldBatch had them.
func switchBisectBatch(oldBatch, newBatch []string) error {
	if err := enableINIs(oldBatch); err != nil {
		return err
	}
	err := disableINIs(newBatch)
	if err == nil || errors.Is(err, errBisectOutOfSync) {
		return err
	}
	if restoreErr := disableINIs(oldBatch); restoreErr != nil {
		return outOfSyncError(err, restoreErr, "")
	}
	return err
}

func outOfSyncError(err, rollbackErr error, path string) error {
	return infra.WithCause(
		fmt.Errorf("%w: %w", errBisectOutOfSync, err),
		infra.AnnotateError(rollbackErr, infra.Diagnostic{Stage: "rollback", Fields: map[string]any{"path": path}}),
	)
}

func bisectDisabledPath(iniPath string) string { return iniPath + "." + bisectDisabledSuffix }

// renameINIDisable gives the INI its bisect name and reports whether it renamed anything; an INI already under that
// name is left as it is.
func renameINIDisable(iniPath string) (bool, error) {
	target := bisectDisabledPath(iniPath)
	if _, originalErr := os.Stat(iniPath); errors.Is(originalErr, os.ErrNotExist) {
		if _, targetErr := os.Stat(target); targetErr == nil {
			return false, nil
		}
	}
	if _, err := os.Stat(target); err == nil {
		return false, fmt.Errorf("cannot disable ini because target already exists: %s", target)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.Rename(iniPath, target); err != nil {
		return false, err
	}
	if _, err := os.Stat(target); err != nil {
		return true, fmt.Errorf("failed to disable ini %s: %w", iniPath, err)
	}
	return true, nil
}

// renameINIEnable restores the INI's original name and reports whether it renamed anything. An INI already enabled,
// or gone under both names (deleted, or kept under its DISABLED name), is left as it is.
func renameINIEnable(iniPath string) (bool, error) {
	target := bisectDisabledPath(iniPath)
	_, originalErr := os.Stat(iniPath)
	_, targetErr := os.Stat(target)
	if errors.Is(targetErr, os.ErrNotExist) && (originalErr == nil || errors.Is(originalErr, os.ErrNotExist)) {
		return false, nil
	}
	if originalErr == nil {
		return false, fmt.Errorf("cannot restore ini because original already exists: %s", iniPath)
	} else if !errors.Is(originalErr, os.ErrNotExist) {
		return false, originalErr
	}
	if err := os.Rename(target, iniPath); err != nil {
		return false, err
	}
	return true, nil
}

// keepBisectINIDisabled renames the INI to its DISABLED name. When that name is taken, the INI gets a numbered
// name such as "DISABLED mod.ini (2)", as merged files do, so neither file is lost. An INI that is already gone was
// handled by an earlier attempt.
func keepBisectINIDisabled(originalPath, style string) error {
	source := bisectDisabledPath(originalPath)
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		source = originalPath
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	prefix := "DISABLED "
	if style == "underscore" {
		prefix = "DISABLED_"
	}
	target, err := freeSiblingPath(filepath.Dir(originalPath), prefix+filepath.Base(originalPath))
	if err != nil {
		return err
	}
	return os.Rename(source, target)
}

func freeSiblingPath(dir, name string) (string, error) {
	for number := 1; ; number++ {
		candidate := name
		if number > 1 {
			candidate = fmt.Sprintf("%s (%d)", name, number)
		}
		path := filepath.Join(dir, candidate)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		} else if err != nil {
			return "", err
		}
	}
}

func listBisectOrphans(ctx context.Context, root string) ([]string, error) {
	release, err := diskio.AcquireDir(ctx, root)
	if err != nil {
		return nil, err
	}
	defer release()

	var paths []string
	// The trailing separator makes the walk enter a Mods folder that is itself a junction.
	root = strings.TrimRight(root, `\/`) + string(filepath.Separator)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		// The game cannot load entries whose names end in a dot or space, and
		// reading such a folder fails, so bisect cannot have renamed files there.
		if path != root && platform.IsUnaddressableName(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
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

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
