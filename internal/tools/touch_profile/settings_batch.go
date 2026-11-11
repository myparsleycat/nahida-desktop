package touchprofile

import (
	"math"
	"sync"
	"time"
)

type SettingRange struct {
	Min, Max, Step float64
}

var (
	MaskStrengthRange    = SettingRange{Min: 0, Max: 2, Step: .05}
	MaskCurveRange       = SettingRange{Min: 0, Max: 2, Step: .05}
	MaskRadiusScaleRange = SettingRange{Min: .1, Max: 2, Step: .1}
)

func (r SettingRange) Clamp(value float64) float64 {
	return math.Max(r.Min, math.Min(r.Max, value))
}

// AdvancedSetting is one physics value of a zone. Key is the snake_case name the UI labels it by.
type AdvancedSetting struct {
	Key   string
	Range SettingRange
	Field func(*TouchAdvancedSettings) *float64
}

var AdvancedSettings = []AdvancedSetting{
	{"radius", SettingRange{.02, 1, .01}, func(a *TouchAdvancedSettings) *float64 { return &a.Radius }},
	{"strength", SettingRange{.1, 3, .05}, func(a *TouchAdvancedSettings) *float64 { return &a.Strength }},
	{"damping", SettingRange{.01, .99, .01}, func(a *TouchAdvancedSettings) *float64 { return &a.Damping }},
	{"spring", SettingRange{.01, 1, .01}, func(a *TouchAdvancedSettings) *float64 { return &a.Spring }},
	{"max_offset", SettingRange{.005, .3, .005}, func(a *TouchAdvancedSettings) *float64 { return &a.MaxOffset }},
	{"falloff", SettingRange{.1, 4, .1}, func(a *TouchAdvancedSettings) *float64 { return &a.Falloff }},
}

var physicsPresets = map[string]TouchAdvancedSettings{
	"soft":   {Radius: .24, Strength: .9, Damping: .92, Spring: .12, MaxOffset: .045, Falloff: 1.35},
	"normal": defaultTouchZoneSettings().Advanced,
	"firm":   {Radius: .16, Strength: 1.35, Damping: .8, Spring: .26, MaxOffset: .09, Falloff: 2.2},
}

// WithPhysicsPreset returns settings switched to a named physics preset, keeping the mask values and strength
// preset and turning the core attenuation off. It reports false for an unknown preset.
func WithPhysicsPreset(settings TouchZoneSettings, preset string) (TouchZoneSettings, bool) {
	advanced, ok := physicsPresets[preset]
	if !ok {
		return settings, false
	}
	settings.MaskCoreAttenuation = "off"
	settings.PhysicsPreset = preset
	settings.Advanced = advanced
	return settings, true
}

// StepMaskRadiusScale moves the radius scale by delta, rounded to hundredths and clamped, and reports whether
// it changed.
func StepMaskRadiusScale(current, delta float64) (float64, bool) {
	next := MaskRadiusScaleRange.Clamp(math.Round((current+delta)*100) / 100)
	return next, next != current
}

// SettingsBatchOptions configures a SettingsBatch. The callbacks run on the goroutine that enqueued, cleared or
// saved, never under the batch's lock.
type SettingsBatchOptions struct {
	Debounce          time.Duration
	Save              func([]TouchProfileZoneSettingsChange) (TouchProfileUpdateResult, error)
	OnPendingChange   func(pending bool)
	OnPreviewRequired func()
	OnError           func(error)
	// AfterFunc schedules the debounced save; nil uses time.AfterFunc.
	AfterFunc func(time.Duration, func()) (stop func() bool)
}

// SettingsBatch saves zone setting edits after a quiet period, keeping only the latest settings of each zone and
// at most one save in flight. Edits that arrive during a save are saved right after it.
type SettingsBatch struct {
	opts SettingsBatchOptions

	mu    sync.Mutex
	order []string
	dirty map[string]TouchProfileZoneSettingsChange
	// timer is the sequence of the scheduled flush, so a timer that fired after being replaced does nothing.
	timer               uint64
	stopTimer           func() bool
	inFlight            bool
	previewNeeded       bool
	savedPreviewChanged bool
	disposed            bool
}

func NewSettingsBatch(opts SettingsBatchOptions) *SettingsBatch {
	if opts.AfterFunc == nil {
		opts.AfterFunc = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	}
	return &SettingsBatch{opts: opts, dirty: map[string]TouchProfileZoneSettingsChange{}}
}

// Enqueue records changes; refreshPreview asks for OnPreviewRequired once a save reports a changed preview.
func (b *SettingsBatch) Enqueue(changes []TouchProfileZoneSettingsChange, refreshPreview bool) {
	b.mu.Lock()
	if b.disposed {
		b.mu.Unlock()
		return
	}
	for _, change := range changes {
		key := change.ComponentID + ":" + change.ZoneID
		if _, ok := b.dirty[key]; !ok {
			b.order = append(b.order, key)
		}
		b.dirty[key] = change
	}
	b.previewNeeded = b.previewNeeded || refreshPreview
	b.cancelTimer()
	b.timer++
	sequence := b.timer
	b.stopTimer = b.opts.AfterFunc(b.opts.Debounce, func() { b.flushTimer(sequence) })
	b.mu.Unlock()
	b.opts.OnPendingChange(true)
}

func (b *SettingsBatch) flushTimer(sequence uint64) {
	b.mu.Lock()
	current := b.timer == sequence && b.stopTimer != nil
	b.mu.Unlock()
	if current {
		b.Flush()
	}
}

// Flush saves the recorded changes now unless a save is in flight, which saves them when it ends.
func (b *SettingsBatch) Flush() {
	b.mu.Lock()
	changes, ok := b.take()
	b.mu.Unlock()
	for ok {
		result, err := b.opts.Save(changes)

		b.mu.Lock()
		if err != nil {
			b.previewNeeded, b.savedPreviewChanged = false, false
		} else {
			b.savedPreviewChanged = b.savedPreviewChanged || result.PreviewChanged
		}
		b.inFlight = false
		if b.disposed {
			b.mu.Unlock()
			if err != nil {
				b.opts.OnError(err)
			}
			return
		}
		changes, ok = b.take()
		notifyPreview := false
		if !ok {
			notifyPreview = b.savedPreviewChanged && b.previewNeeded
			b.previewNeeded, b.savedPreviewChanged = false, false
		}
		b.mu.Unlock()

		if err != nil {
			b.opts.OnError(err)
		}
		if !ok {
			if notifyPreview {
				b.opts.OnPreviewRequired()
			}
			b.opts.OnPendingChange(false)
		}
	}
}

// take removes the recorded changes for a save and marks it in flight. Call it with b.mu held.
func (b *SettingsBatch) take() ([]TouchProfileZoneSettingsChange, bool) {
	if b.disposed || b.inFlight || len(b.order) == 0 {
		return nil, false
	}
	b.cancelTimer()
	changes := make([]TouchProfileZoneSettingsChange, 0, len(b.order))
	for _, key := range b.order {
		changes = append(changes, b.dirty[key])
	}
	b.order, b.dirty = nil, map[string]TouchProfileZoneSettingsChange{}
	b.inFlight = true
	return changes, true
}

func (b *SettingsBatch) cancelTimer() {
	if b.stopTimer != nil {
		b.stopTimer()
		b.stopTimer = nil
	}
}

// Clear drops the recorded changes; a save in flight still finishes.
func (b *SettingsBatch) Clear() {
	b.mu.Lock()
	b.cancelTimer()
	b.order, b.dirty = nil, map[string]TouchProfileZoneSettingsChange{}
	b.previewNeeded, b.savedPreviewChanged = false, false
	idle := !b.inFlight
	b.mu.Unlock()
	if idle {
		b.opts.OnPendingChange(false)
	}
}

// Dispose drops the recorded changes and stops all further saves and callbacks.
func (b *SettingsBatch) Dispose() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.disposed = true
	b.cancelTimer()
	b.order, b.dirty = nil, map[string]TouchProfileZoneSettingsChange{}
}
