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
// state changes cancel a pending progress update, while ordinary progress uses
// a leading-and-trailing throttle so the latest snapshot is never stranded.
func (t *Transfer) scheduleEmitLocked(immediate bool, now time.Time) bool {
	if t.emitStopped {
		return false
	}
	if immediate {
		t.cancelProgressEmitLocked()
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
		return true
	}
	if t.progressEmitTimer != nil {
		return false
	}

	t.progressEmitGen++
	generation := t.progressEmitGen
	t.progressEmitTimer = time.AfterFunc(interval-elapsed, func() {
		t.flushProgressEmit(generation)
	})
	return false
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
	app := t.app
	syncWindowProgress := t.syncWindowProgress
	items := t.snapshotEntriesLocked()
	t.mu.Unlock()
	t.dispatchSnapshots(app, syncWindowProgress, items)
}
