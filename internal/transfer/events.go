package transfer

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (t *Transfer) emit() {
	t.emitMu.Lock()
	defer t.emitMu.Unlock()

	t.mu.RLock()
	if t.emitStopped {
		t.mu.RUnlock()
		return
	}
	app := t.app
	syncWindowProgress := t.syncWindowProgress
	items := t.snapshotEntriesLocked()
	t.mu.RUnlock()
	t.dispatchSnapshots(app, syncWindowProgress, items)
}

func (t *Transfer) dispatchSnapshots(
	app *application.App,
	syncWindowProgress func(*WindowProgress),
	items []orderedSnapshot,
) {
	snapshots := orderSnapshots(items)
	if syncWindowProgress != nil {
		syncWindowProgress(CalculateWindowProgress(snapshots))
	}
	if app != nil {
		app.Event.Emit(updateEventName, snapshots)
	}
}

// scheduleEmitLocked plans a renderer update while t.mu is held. Immediate
// state changes are immediate; active transfers also refresh while reads or
// server responses are stalled, so speed and ETA do not remain stale.
func (t *Transfer) scheduleEmitLocked(immediate bool, now time.Time) bool {
	if t.emitStopped {
		return false
	}
	if immediate {
		t.cancelProgressEmitLocked()
		t.scheduleHeartbeatLocked()
		return true
	}

	interval := t.emitEvery
	if interval <= 0 {
		interval = emitInterval
	}
	elapsed := now.Sub(t.lastProgressEmit)
	if t.lastProgressEmit.IsZero() || elapsed < 0 || elapsed >= interval {
		t.cancelProgressEmitLocked()
		t.lastProgressEmit = now
		t.scheduleHeartbeatLocked()
		return true
	}
	if t.progressEmitTimer != nil {
		return false
	}

	t.startProgressTimerLocked(interval - elapsed)
	return false
}

func (t *Transfer) startProgressTimerLocked(delay time.Duration) {
	t.progressEmitGen++
	generation := t.progressEmitGen
	t.progressEmitTimer = time.AfterFunc(delay, func() {
		t.flushProgressEmit(generation)
	})
}

func (t *Transfer) scheduleHeartbeatLocked() {
	if t.emitStopped || t.progressEmitTimer != nil || (t.app == nil && t.syncWindowProgress == nil) {
		return
	}
	for _, item := range t.entries {
		if item.record.Status == StatusPreparing || item.record.Status == StatusProgress {
			interval := t.emitEvery
			if interval <= 0 {
				interval = emitInterval
			}
			t.startProgressTimerLocked(interval)
			return
		}
	}
}

func (t *Transfer) cancelProgressEmitLocked() {
	t.progressEmitGen++
	if t.progressEmitTimer != nil {
		t.progressEmitTimer.Stop()
		t.progressEmitTimer = nil
	}
}

func (t *Transfer) flushProgressEmit(generation uint64) {
	t.emitMu.Lock()
	defer t.emitMu.Unlock()

	t.mu.Lock()
	if t.emitStopped || generation != t.progressEmitGen || t.progressEmitTimer == nil {
		t.mu.Unlock()
		return
	}
	t.progressEmitTimer = nil
	t.lastProgressEmit = t.now()
	for _, item := range t.entries {
		if item.record.Status == StatusProgress {
			updateSpeed(item, t.lastProgressEmit, false)
		}
	}
	app := t.app
	syncWindowProgress := t.syncWindowProgress
	items := t.snapshotEntriesLocked()
	t.scheduleHeartbeatLocked()
	t.mu.Unlock()
	t.dispatchSnapshots(app, syncWindowProgress, items)
}
