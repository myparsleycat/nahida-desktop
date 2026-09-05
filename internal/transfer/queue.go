package transfer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

func (t *Transfer) Cancel(pid string) error {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if isTerminal(item.record.Status) && item.cancel == nil {
		delete(t.entries, pid)
		shouldEmit := t.scheduleEmitLocked(true, t.now())
		t.mu.Unlock()
		t.destinationMu.Unlock()
		if shouldEmit {
			t.emit()
		}
		return t.RefreshPowerSaveBlock(context.Background())
	}
	if item.cancel != nil {
		item.cancel()
	}
	item.record.Status = StatusCanceled
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	t.signalQueue()
	return t.RefreshPowerSaveBlock(context.Background())
}

func (t *Transfer) Pause(pid string) error {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if item.record.Status != StatusPending && item.record.Status != StatusPreparing && item.record.Status != StatusProgress {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return nil
	}
	item.record.Status = StatusPaused
	if item.cancel != nil {
		item.cancel()
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return t.RefreshPowerSaveBlock(context.Background())
}

func (t *Transfer) Resume(pid string) error {
	restartable, err := t.canRestart(pid)
	if err != nil || !restartable {
		return err
	}
	return t.ManualStart(pid)
}

func (t *Transfer) Retry(pid string) error {
	restartable, err := t.canRestart(pid)
	if err != nil || !restartable {
		return err
	}
	if err := t.ResetTransfer(pid); err != nil {
		return err
	}
	return t.ManualStart(pid)
}

func (t *Transfer) canRestart(pid string) (bool, error) {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return false, fmt.Errorf("transfer %q not found", pid)
	}
	restartable := item.restartData != nil
	t.mu.RUnlock()
	return restartable, nil
}

func (t *Transfer) PauseAll() error {
	for _, item := range t.List() {
		if item.Status == StatusProgress {
			if err := t.Pause(item.PID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Transfer) ResumeAll() error {
	for _, item := range t.List() {
		if item.Status == StatusPaused {
			if err := t.Resume(item.PID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Transfer) Clear() error {
	t.destinationMu.Lock()
	t.mu.Lock()
	changed := false
	for pid, item := range t.entries {
		if isTerminal(item.record.Status) && item.cancel == nil {
			delete(t.entries, pid)
			changed = true
		}
	}
	shouldEmit := changed && t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return t.RefreshPowerSaveBlock(context.Background())
}

//wails:ignore
func (t *Transfer) Create(params CreateParams) (Record, error) {
	if params.PID == "" {
		return Record{}, errors.New("transfer pid is required")
	}
	if params.Type != "upload" && params.Type != "download" {
		return Record{}, fmt.Errorf("unsupported transfer type %q", params.Type)
	}
	initialStatus := params.InitialStatus
	if initialStatus == "" {
		initialStatus = StatusPreparing
	}
	if !isValidStatus(initialStatus) {
		return Record{}, fmt.Errorf("unsupported transfer status %q", initialStatus)
	}
	totalSize := int64(0)
	for _, file := range params.Data.Files {
		if size := LogicalFileBytes(file); size > 0 {
			totalSize += size
		}
	}

	t.destinationMu.Lock()
	t.mu.Lock()
	if _, exists := t.entries[params.PID]; exists {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return Record{}, fmt.Errorf("transfer %q already exists", params.PID)
	}
	queueGroupID := params.QueueGroupID
	if queueGroupID == nil {
		hasOpen := false
		for _, item := range t.entries {
			if isOpen(item.record.Status) {
				hasOpen = true
				break
			}
		}
		if !hasOpen || t.queueGroupSequence == 0 {
			t.queueGroupSequence++
		}
		id := t.queueGroupSequence
		queueGroupID = &id
	}
	t.creationSequence++
	t.queueSequence++
	destinationPaths := slices.Clone(params.DestinationPaths)
	if params.DestinationTargets != nil {
		destinationPaths = destinationTargetPaths(params.DestinationTargets)
	}
	record := Record{
		Snapshot: Snapshot{
			PID:                params.PID,
			Type:               params.Type,
			QueueGroupID:       queueGroupID,
			CurrentID:          params.CurrentID,
			Status:             initialStatus,
			TotalSize:          totalSize,
			StartTime:          t.now().UnixMilli(),
			Name:               params.Name,
			TotalFiles:         len(params.Data.Files),
			Path:               params.Path,
			DestinationPaths:   destinationPaths,
			DestinationTargets: slices.Clone(params.DestinationTargets),
		},
		Data: cloneData(params.Data),
	}
	t.entries[params.PID] = &entry{
		record:       record,
		restartData:  params.RestartData,
		completedIDs: make(map[string]struct{}),
		createdOrder: t.creationSequence,
		queueOrder:   t.queueSequence,
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	if initialStatus == StatusPending && !params.ManualStart {
		t.signalQueue()
	}
	err := t.RefreshPowerSaveBlock(context.Background())
	t.notifyStarted(context.Background(), params.Name)
	return cloneRecord(record), err
}

// AttachCancel connects a transfer entry to work managed outside the queue.
// It is used by server-side Drive imports so Transfer.Cancel stops the active
// HTTP stream through the same cancellation path as other transfers.
//
//wails:ignore
func (t *Transfer) AttachCancel(pid string, cancel context.CancelFunc) error {
	if cancel == nil {
		return errors.New("transfer cancel function is required")
	}
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if ok {
		item.cancel = cancel
	}
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	return nil
}

// ClearCancel detaches externally managed work after it finishes.
//
//wails:ignore
func (t *Transfer) ClearCancel(pid string) {
	t.destinationMu.Lock()
	t.mu.Lock()
	if item, ok := t.entries[pid]; ok {
		item.cancel = nil
	}
	t.mu.Unlock()
	t.destinationMu.Unlock()
}

func (t *Transfer) notifyStarted(ctx context.Context, name string) {
	if t.eventEmit == nil {
		return
	}
	t.eventEmit("fn:toast", "전송이 시작되었습니다", map[string]any{"description": name})
	settings, ok := t.settings.(startNotificationSettings)
	if !ok {
		return
	}
	move, err := settings.GetMoveTransferPageWhenStartTransfer(ctx)
	if err != nil {
		t.logError(err, "transfer.startNotification")
		return
	}
	if move {
		t.eventEmit("fn:navi", "/transfer")
	}
}

//wails:ignore
func (t *Transfer) RegisterRunner(pid string, runner Runner) error {
	if runner == nil {
		return errors.New("transfer runner is required")
	}
	t.mu.Lock()
	item, ok := t.entries[pid]
	if ok {
		item.runner = runner
	}
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	t.signalQueue()
	return nil
}

//wails:ignore
func (t *Transfer) ProcessQueue(ctx context.Context) error {
	for {
		pid, runner, runCtx, ok := t.next(ctx)
		if !ok {
			return ctx.Err()
		}
		if runner == nil {
			return nil
		}
		err := runner(runCtx, t, pid)
		t.finishRun(pid, err)
	}
}

func (t *Transfer) next(parent context.Context) (string, Runner, context.Context, bool) {
	if err := parent.Err(); err != nil {
		return "", nil, nil, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var selected *entry
	for _, item := range t.entries {
		if item.record.Status != StatusPending {
			continue
		}
		if item.runner == nil {
			continue
		}
		if selected == nil || item.queueOrder < selected.queueOrder {
			selected = item
		}
	}
	if selected == nil {
		return "", nil, nil, true
	}
	runCtx, cancel := context.WithCancel(parent)
	selected.cancel = cancel
	return selected.record.PID, selected.runner, runCtx, true
}

func (t *Transfer) finishRun(pid string, runErr error) {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return
	}
	item.cancel = nil
	interrupted := item.record.Status == StatusPaused || item.record.Status == StatusCanceled
	if !interrupted && runErr != nil && !isTerminal(item.record.Status) {
		item.record.Status = StatusError
		item.record.Error = runErr.Error()
	}
	snapshot := item.record.Snapshot
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	if t.reportFailure != nil {
		_ = t.reportFailure(runErr, map[string]any{"operation": "run", "stage": "finish", "pid": pid, "name": snapshot.Name, "path": snapshot.Path, "type": snapshot.Type, "currentId": snapshot.CurrentID})
	} else if !interrupted && runErr != nil && !isNormalRunnerCancellation(runErr) && !isReportedRunnerError(runErr) {
		t.logRecord(map[string]any{
			"operation":        "run",
			"stage":            "finish",
			"type":             snapshot.Type,
			"pid":              snapshot.PID,
			"name":             snapshot.Name,
			"currentId":        snapshot.CurrentID,
			"path":             snapshot.Path,
			"totalBytes":       snapshot.TotalSize,
			"transferredBytes": snapshot.TransferredSize,
			"totalFiles":       snapshot.TotalFiles,
			"transferredFiles": snapshot.TransferredFiles,
			"error":            runErr.Error(),
		}, "Transfer")
	}
	if err := t.RefreshPowerSaveBlock(context.Background()); err != nil && !isReportedRunnerError(err) {
		t.logRecord(map[string]any{"operation": "finish", "stage": "power-save", "pid": pid, "error": err.Error()}, "Transfer")
	}
}

func isReportedRunnerError(err error) bool {
	var reported interface{ DiagnosticReported() bool }
	return errors.As(err, &reported) && reported.DiagnosticReported()
}

func isNormalRunnerCancellation(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	message := strings.ToUpper(strings.TrimSpace(err.Error()))
	return message == "DRIVE_COPY_CANCELED" || strings.HasPrefix(message, "DRIVE_COPY_CANCELED:") ||
		message == "CUSTOM_DOWNLOAD_ABORTED" || strings.HasPrefix(message, "CUSTOM_DOWNLOAD_ABORTED:") ||
		message == "CUSTOM_DOWNLOAD_CANCELED" || strings.HasPrefix(message, "CUSTOM_DOWNLOAD_CANCELED:")
}

//wails:ignore
func (t *Transfer) RestartData(pid string) (any, bool) {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return nil, false
	}
	data := item.restartData
	t.mu.RUnlock()
	return data, true
}

//wails:ignore
func (t *Transfer) ManualStart(pid string) error {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if item.record.Status == StatusProgress {
		t.mu.RUnlock()
		return nil
	}
	runningPID := ""
	for otherPID, other := range t.entries {
		if otherPID != pid && (other.record.Status == StatusProgress || other.record.Status == StatusPreparing) {
			runningPID = otherPID
			break
		}
	}
	t.mu.RUnlock()
	if runningPID != "" {
		if err := t.Pause(runningPID); err != nil {
			return err
		}
	}

	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok = t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	if item.runner == nil {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q has no registered runner", pid)
	}
	if !isOpen(item.record.Status) || item.record.QueueGroupID == nil {
		hasOpen := false
		for otherPID, other := range t.entries {
			if otherPID != pid && isOpen(other.record.Status) {
				hasOpen = true
				break
			}
		}
		if !hasOpen || t.queueGroupSequence == 0 {
			t.queueGroupSequence++
		}
		id := t.queueGroupSequence
		item.record.QueueGroupID = &id
	}
	if item.record.Status != StatusPreparing {
		item.record.Status = StatusPending
	}
	for _, other := range t.entries {
		if other != item {
			other.queueOrder++
		}
	}
	item.queueOrder = 0
	item.record.FailedFiles = 0
	item.record.Error = ""
	item.record.ErrorCode = ""
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	t.signalQueue()
	return nil
}

//wails:ignore
func (t *Transfer) ResetStartTime(pid string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	item, ok := t.entries[pid]
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.StartTime = t.now().UnixMilli()
	item.samples = nil
	return nil
}

//wails:ignore
func (t *Transfer) ResetTransfer(pid string) error {
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.TransferredSize = 0
	item.record.Progress = 0
	item.record.Speed = 0
	item.record.ETA = 0
	item.record.StartTime = t.now().UnixMilli()
	item.record.TransferredFiles = 0
	item.record.FailedFiles = 0
	item.record.Error = ""
	item.record.ErrorCode = ""
	item.samples = nil
	item.completedIDs = make(map[string]struct{})
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return nil
}

func (t *Transfer) signalQueue() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}
