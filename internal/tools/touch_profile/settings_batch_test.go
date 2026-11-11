package touchprofile

import (
	"reflect"
	"sync"
	"testing"
	"time"
)

// fakeTimers runs a scheduled function only when the test fires it.
type fakeTimers struct {
	mu      sync.Mutex
	pending []func()
}

func (f *fakeTimers) after(_ time.Duration, run func()) func() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, run)
	return func() bool { return true }
}

func (f *fakeTimers) scheduled(t *testing.T, index int) func() {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if index >= len(f.pending) {
		t.Fatalf("timer %d was not scheduled", index)
	}
	return f.pending[index]
}

func settingsChange(zone string, maskStrength float64) TouchProfileZoneSettingsChange {
	settings := defaultTouchZoneSettings()
	settings.MaskStrength = maskStrength
	return TouchProfileZoneSettingsChange{ComponentID: "body", ZoneID: zone, Settings: settings}
}

type batchRecorder struct {
	mu       sync.Mutex
	saves    [][]TouchProfileZoneSettingsChange
	previews int
	idle     chan struct{}
}

func (r *batchRecorder) options(timers *fakeTimers, save func(int) TouchProfileUpdateResult) SettingsBatchOptions {
	return SettingsBatchOptions{
		Debounce: 120 * time.Millisecond,
		Save: func(changes []TouchProfileZoneSettingsChange) (TouchProfileUpdateResult, error) {
			r.mu.Lock()
			r.saves = append(r.saves, changes)
			call := len(r.saves)
			r.mu.Unlock()
			return save(call), nil
		},
		OnPendingChange: func(pending bool) {
			if !pending && r.idle != nil {
				r.idle <- struct{}{}
			}
		},
		OnPreviewRequired: func() {
			r.mu.Lock()
			r.previews++
			r.mu.Unlock()
		},
		OnError:   func(err error) { panic(err) },
		AfterFunc: timers.after,
	}
}

func (r *batchRecorder) snapshot() ([][]TouchProfileZoneSettingsChange, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]TouchProfileZoneSettingsChange(nil), r.saves...), r.previews
}

func TestSettingsBatchDebouncesAndKeepsLatestSettingsPerZone(t *testing.T) {
	timers := &fakeTimers{}
	recorder := &batchRecorder{}
	batch := NewSettingsBatch(recorder.options(timers, func(int) TouchProfileUpdateResult {
		return TouchProfileUpdateResult{PreviewChanged: true}
	}))

	batch.Enqueue([]TouchProfileZoneSettingsChange{settingsChange("left", .5)}, true)
	batch.Enqueue([]TouchProfileZoneSettingsChange{settingsChange("left", .8), settingsChange("right", .6)}, true)
	timers.scheduled(t, 0)()
	if saves, _ := recorder.snapshot(); len(saves) != 0 {
		t.Fatalf("replaced timer saved %d batches", len(saves))
	}

	timers.scheduled(t, 1)()
	saves, previews := recorder.snapshot()
	want := [][]TouchProfileZoneSettingsChange{{settingsChange("left", .8), settingsChange("right", .6)}}
	if !reflect.DeepEqual(saves, want) {
		t.Fatalf("saves = %+v, want %+v", saves, want)
	}
	if previews != 1 {
		t.Fatalf("preview requests = %d, want 1", previews)
	}
}

func TestSettingsBatchSavesEditsMadeDuringASaveRightAfterIt(t *testing.T) {
	timers := &fakeTimers{}
	started, release := make(chan struct{}), make(chan struct{})
	recorder := &batchRecorder{idle: make(chan struct{}, 1)}
	batch := NewSettingsBatch(recorder.options(timers, func(call int) TouchProfileUpdateResult {
		if call == 1 {
			close(started)
			<-release
		}
		return TouchProfileUpdateResult{PreviewChanged: true}
	}))

	batch.Enqueue([]TouchProfileZoneSettingsChange{settingsChange("left", .5)}, true)
	go timers.scheduled(t, 0)()
	<-started
	batch.Enqueue([]TouchProfileZoneSettingsChange{settingsChange("left", .7)}, true)
	batch.Enqueue([]TouchProfileZoneSettingsChange{settingsChange("left", .9)}, true)
	timers.scheduled(t, 2)()
	if saves, _ := recorder.snapshot(); len(saves) != 1 {
		t.Fatalf("saves while one is in flight = %d, want 1", len(saves))
	}

	close(release)
	<-recorder.idle
	saves, previews := recorder.snapshot()
	if len(saves) != 2 || !reflect.DeepEqual(saves[1], []TouchProfileZoneSettingsChange{settingsChange("left", .9)}) {
		t.Fatalf("saves = %+v, want the latest left edit second", saves)
	}
	if previews != 1 {
		t.Fatalf("preview requests = %d, want 1", previews)
	}
}
