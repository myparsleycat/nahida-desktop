package transfer

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"
)

type speedSample struct {
	at   time.Time
	size int64
}

type orderedSnapshot struct {
	snapshot Snapshot
	order    uint64
}

// List returns newest-first renderer-safe snapshots.
func (t *Transfer) List() []Snapshot {
	t.mu.RLock()
	items := t.snapshotEntriesLocked()
	t.mu.RUnlock()
	return orderSnapshots(items)
}

func (t *Transfer) snapshotEntriesLocked() []orderedSnapshot {
	items := make([]orderedSnapshot, 0, len(t.entries))
	for _, item := range t.entries {
		items = append(items, orderedSnapshot{snapshot: cloneSnapshot(item.record.Snapshot), order: item.createdOrder})
	}
	return items
}

func orderSnapshots(items []orderedSnapshot) []Snapshot {
	slices.SortFunc(items, func(a, b orderedSnapshot) int {
		switch {
		case a.order > b.order:
			return -1
		case a.order < b.order:
			return 1
		default:
			return 0
		}
	})
	out := make([]Snapshot, len(items))
	for i := range items {
		out[i] = items[i].snapshot
	}
	return out
}

//wails:ignore
func (t *Transfer) Update(pid string, updates Updates) error {
	now := t.now()
	reservationChange := updates.Status != nil || updates.DestinationPaths != nil || updates.DestinationTargets != nil
	if reservationChange {
		t.destinationMu.Lock()
	}
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		if reservationChange {
			t.destinationMu.Unlock()
		}
		return fmt.Errorf("transfer %q not found", pid)
	}
	applyUpdates(&item.record.Snapshot, updates)
	if updates.TransferredSize != nil && item.record.Status == StatusProgress {
		lastIsDuplicate := len(item.samples) > 0 && item.samples[len(item.samples)-1].size == item.record.TransferredSize
		if !lastIsDuplicate {
			item.samples = append(item.samples, speedSample{at: now, size: item.record.TransferredSize})
		}
		cutoff := now.Add(-speedWindow)
		first := 0
		for first < len(item.samples) && item.samples[first].at.Before(cutoff) {
			first++
		}
		item.samples = slices.Clone(item.samples[first:])
		if len(item.samples) > 1 {
			oldest := item.samples[0]
			newest := item.samples[len(item.samples)-1]
			elapsed := newest.at.Sub(oldest.at).Seconds()
			if elapsed > 0 {
				item.record.Speed = float64(newest.size-oldest.size) / elapsed
			}
		} else {
			item.record.Speed = 0
			item.record.ETA = 0
		}
		if item.record.Speed > 0 && item.record.TotalSize > item.record.TransferredSize {
			item.record.ETA = math.Ceil(float64(item.record.TotalSize-item.record.TransferredSize) / item.record.Speed)
		}
		if item.record.TotalSize > 0 {
			item.record.Progress = clamp(float64(item.record.TransferredSize)/float64(item.record.TotalSize)*100, 0, 100)
		} else if item.record.TransferredSize > 0 {
			item.record.Progress = 100
		}
	}
	shouldEmit := t.scheduleEmitLocked(updates.Status != nil, now)
	t.mu.Unlock()
	if reservationChange {
		t.destinationMu.Unlock()
	}
	if shouldEmit {
		t.emit()
	}
	if updates.Status != nil {
		return t.RefreshPowerSaveBlock(context.Background())
	}
	return nil
}

//wails:ignore
func (t *Transfer) Get(pid string) (Record, bool) {
	t.mu.RLock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.RUnlock()
		return Record{}, false
	}
	record := cloneRecord(item.record)
	t.mu.RUnlock()
	return record, true
}

//wails:ignore
func (t *Transfer) SetData(pid string, data Data, totalSize int64, name string, destinationTargets []DestinationTarget) error {
	t.destinationMu.Lock()
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		t.destinationMu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.Data = cloneData(data)
	item.record.TotalSize = max(0, totalSize)
	item.record.TotalFiles = len(data.Files)
	item.record.DestinationPaths = destinationTargetPaths(destinationTargets)
	item.record.DestinationTargets = slices.Clone(destinationTargets)
	if name != "" {
		item.record.Name = name
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	t.destinationMu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return nil
}

//wails:ignore
func (t *Transfer) All() []Record {
	t.mu.RLock()
	out := make([]Record, 0, len(t.entries))
	for _, item := range t.entries {
		out = append(out, cloneRecord(item.record))
	}
	t.mu.RUnlock()
	return out
}

//wails:ignore
func (t *Transfer) MarkFileCompleted(pid, fileID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	item, ok := t.entries[pid]
	if !ok {
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.completedIDs[fileID] = struct{}{}
	return nil
}

//wails:ignore
func (t *Transfer) CompletedFilesCount(pid string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	item, ok := t.entries[pid]
	if !ok {
		return 0
	}
	return len(item.completedIDs)
}

//wails:ignore
func (t *Transfer) MarkFileFailed(pid, message string) error {
	t.mu.Lock()
	item, ok := t.entries[pid]
	if !ok {
		t.mu.Unlock()
		return fmt.Errorf("transfer %q not found", pid)
	}
	item.record.FailedFiles++
	if message != "" && item.record.Error == "" {
		item.record.Error = message
	}
	shouldEmit := t.scheduleEmitLocked(true, t.now())
	t.mu.Unlock()
	if shouldEmit {
		t.emit()
	}
	return nil
}

//wails:ignore
func (t *Transfer) IsFileCompleted(pid, fileID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	item, ok := t.entries[pid]
	if !ok {
		return false
	}
	_, ok = item.completedIDs[fileID]
	return ok
}

func applyUpdates(record *Snapshot, updates Updates) {
	if updates.Status != nil {
		record.Status = *updates.Status
	}
	if updates.CurrentID != nil {
		record.CurrentID = *updates.CurrentID
	}
	if updates.TotalSize != nil {
		record.TotalSize = max(0, *updates.TotalSize)
		if record.TotalSize > 0 {
			record.TransferredSize = min(record.TransferredSize, record.TotalSize)
		}
	}
	if updates.TotalFiles != nil {
		record.TotalFiles = max(0, *updates.TotalFiles)
	}
	if updates.TransferredSize != nil {
		record.TransferredSize = max(0, *updates.TransferredSize)
		if record.TotalSize > 0 {
			record.TransferredSize = min(record.TransferredSize, record.TotalSize)
		}
	}
	if updates.Progress != nil {
		record.Progress = clamp(*updates.Progress, 0, 100)
	}
	if updates.TransferredFiles != nil {
		record.TransferredFiles = max(0, *updates.TransferredFiles)
	}
	if updates.FailedFiles != nil {
		record.FailedFiles = max(0, *updates.FailedFiles)
	}
	if updates.Path != nil {
		record.Path = *updates.Path
	}
	if updates.DestinationPaths != nil {
		record.DestinationPaths = slices.Clone(updates.DestinationPaths)
		record.DestinationTargets = nil
	}
	if updates.DestinationTargets != nil {
		record.DestinationTargets = slices.Clone(updates.DestinationTargets)
		record.DestinationPaths = destinationTargetPaths(updates.DestinationTargets)
	}
	if updates.Error != nil {
		record.Error = *updates.Error
	}
	if updates.ErrorCode != nil {
		record.ErrorCode = *updates.ErrorCode
	}
	if updates.PlanPhase != nil {
		phase := *updates.PlanPhase
		record.PlanPhase = &phase
	}
	if updates.PlanProgress != nil {
		progress := clamp(*updates.PlanProgress, 0, 100)
		record.PlanProgress = &progress
	}
	if updates.ClearCurrentID {
		record.CurrentID = ""
	}
	if updates.ClearError {
		record.Error = ""
	}
	if updates.ClearErrorCode {
		record.ErrorCode = ""
	}
	if updates.ClearPlanPhase {
		record.PlanPhase = nil
	}
	if updates.ClearPlanProgress {
		record.PlanProgress = nil
	}
}

func AggregateProgress(transfers []Snapshot) *float64 {
	openGroups := make(map[uint64]struct{})
	for _, item := range transfers {
		if isOpen(item.Status) && item.QueueGroupID != nil {
			openGroups[*item.QueueGroupID] = struct{}{}
		}
	}
	scoped := make([]Snapshot, 0, len(transfers))
	for _, item := range transfers {
		if !isAggregate(item.Status) {
			continue
		}
		if len(openGroups) == 0 {
			if isOpen(item.Status) {
				scoped = append(scoped, item)
			}
			continue
		}
		if item.QueueGroupID != nil {
			if _, ok := openGroups[*item.QueueGroupID]; ok {
				scoped = append(scoped, item)
			}
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	totalSize := int64(0)
	for _, item := range scoped {
		totalSize += max(0, item.TotalSize)
	}
	var value float64
	if totalSize > 0 {
		transferred := int64(0)
		for _, item := range scoped {
			if item.Status == StatusCompleted {
				transferred += max(0, item.TotalSize)
			} else {
				transferred += max(0, min(item.TransferredSize, item.TotalSize))
			}
		}
		value = float64(transferred) / float64(totalSize) * 100
	} else {
		for _, item := range scoped {
			if item.Status == StatusCompleted {
				value += 100
			} else {
				value += clamp(item.Progress, 0, 100)
			}
		}
		value /= float64(len(scoped))
	}
	value = clamp(value, 0, 100)
	return &value
}

func CalculateWindowProgress(transfers []Snapshot) *WindowProgress {
	remaining := make([]Snapshot, 0, len(transfers))
	for _, item := range transfers {
		if item.Status != StatusCompleted && item.Status != StatusCanceled {
			remaining = append(remaining, item)
		}
	}
	if len(remaining) == 0 {
		return nil
	}

	mode := WindowProgressNormal
	switch {
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusProgress
	}):
		mode = WindowProgressNormal
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusPreparing || item.Status == StatusPending
	}):
		mode = WindowProgressIndeterminate
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusPaused
	}):
		mode = WindowProgressPaused
	case slices.ContainsFunc(remaining, func(item Snapshot) bool {
		return item.Status == StatusError
	}):
		mode = WindowProgressError
	}

	progress := AggregateProgress(transfers)
	if progress == nil {
		progress = fallbackWindowProgress(remaining)
	}
	if progress == nil {
		return nil
	}
	return &WindowProgress{Value: *progress, Mode: mode}
}

func fallbackWindowProgress(transfers []Snapshot) *float64 {
	if len(transfers) == 0 {
		return nil
	}
	totalSize := int64(0)
	for _, item := range transfers {
		totalSize += max(0, item.TotalSize)
	}
	var value float64
	if totalSize > 0 {
		transferred := int64(0)
		for _, item := range transfers {
			transferred += max(0, min(item.TransferredSize, item.TotalSize))
		}
		value = float64(transferred) / float64(totalSize) * 100
	} else {
		for _, item := range transfers {
			value += clamp(item.Progress, 0, 100)
		}
		value /= float64(len(transfers))
	}
	value = clamp(value, 0, 100)
	return &value
}

func isOpen(status Status) bool {
	return status == StatusPreparing || status == StatusPending || status == StatusProgress
}

func isAggregate(status Status) bool {
	return isOpen(status) || status == StatusCompleted
}

func isTerminal(status Status) bool {
	return status == StatusCompleted || status == StatusCanceled || status == StatusError
}

func isValidStatus(status Status) bool {
	return isOpen(status) || status == StatusCompleted || status == StatusPaused || status == StatusCanceled || status == StatusError
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(value, high))
}

func cloneData(data Data) Data {
	data.Files = slices.Clone(data.Files)
	data.Dirs = slices.Clone(data.Dirs)
	if data.Root != nil {
		root := *data.Root
		data.Root = &root
	}
	return data
}

func cloneRecord(record Record) Record {
	record.Data = cloneData(record.Data)
	record.Snapshot = cloneSnapshot(record.Snapshot)
	return record
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.DestinationPaths = slices.Clone(snapshot.DestinationPaths)
	snapshot.DestinationTargets = slices.Clone(snapshot.DestinationTargets)
	if snapshot.QueueGroupID != nil {
		id := *snapshot.QueueGroupID
		snapshot.QueueGroupID = &id
	}
	if snapshot.PlanPhase != nil {
		phase := *snapshot.PlanPhase
		snapshot.PlanPhase = &phase
	}
	if snapshot.PlanProgress != nil {
		progress := *snapshot.PlanProgress
		snapshot.PlanProgress = &progress
	}
	return snapshot
}
