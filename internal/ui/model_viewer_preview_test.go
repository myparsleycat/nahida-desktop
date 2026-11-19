package ui

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/setting"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func previewMod(signature string, toggles ...[3]string) mod.ModInfo {
	info := mod.ModInfo{Path: `D:\Mods\Character`, Signature: signature, Mtime: 1}
	for _, toggle := range toggles {
		current := toggle[2]
		info.Inis = append(info.Inis, mod.IniResult{ToggleKeys: []mod.ToggleKey{
			{IniFileName: toggle[0], Variable: toggle[1], CurrentValue: &current},
		}})
	}
	return info
}

// The card shows the mod as its INIs pose it, so a toggle must reach the variable of its own INI even when
// another INI has one of the same name.
func TestModelPreviewStateScopesVariablesToTheirINI(t *testing.T) {
	transport := modelviewer.ModelViewerTransport{
		Variables: []modelviewer.ModelViewerVariable{
			{ID: "First::Swap"}, {ID: "Second::Swap"}, {ID: "Outfit"},
		},
	}
	info := previewMod("a",
		[3]string{`sub\First.ini`, "$swap", "1"},
		[3]string{"Second.ini", "$swap", "2"},
		[3]string{"Character.ini", "$OUTFIT", "3"},
		[3]string{"Character.ini", "$missing", "9"},
	)

	state := modelPreviewState(transport, info)
	want := map[string]any{"First::Swap": "1", "Second::Swap": "2", "Outfit": "3"}
	if len(state) != len(want) {
		t.Fatalf("state = %v, want %v", state, want)
	}
	for key, value := range want {
		if state[key] != value {
			t.Errorf("state[%q] = %v, want %v", key, state[key], value)
		}
	}
}

// A preset names each INI by its slash-separated path in the mod. Its preview must pose the variables of that INI
// only, keep same-named variables of other INIs, and leave variables the preset does not hold as they are.
func TestTogglePresetValuesPoseTheirINI(t *testing.T) {
	modPath := filepath.Join(t.TempDir(), "Character")
	inis := []modelviewer.ModelViewerINI{
		{Path: filepath.Join(modPath, "Sub", "First.ini"), Prefix: "First::"},
		{Path: filepath.Join(modPath, "Second.ini"), Prefix: "Second::"},
	}
	state := map[string]any{"First::swap": 0, "Second::swap": 0, "First::outfit": 2}
	preset := map[string]map[string]string{"sub/first.ini": {"Swap": "1"}}

	got := viewerStateWithValues(state, inis, presetINIValues(modPath, preset))
	want := map[string]any{"First::swap": 1, "Second::swap": 0, "First::outfit": 2}
	if !maps.Equal(got, want) {
		t.Errorf("state = %v, want %v", got, want)
	}
}

// A failure keeps the card from asking again for a minute and, after the retry, for good, so a render that
// was stopped because its folder was left must not count as one.
func TestModelPreviewCancelIsNotAFailure(t *testing.T) {
	s := &Shell{}
	p := &s.mods.modelPreviews
	job := &modelPreviewJob{key: "key", info: previewMod("a")}
	p.current, p.cancel = job, func() {}
	p.cancelRunning()

	s.finishModelPreview(job, nil, 0, context.Canceled)

	if p.current != nil {
		t.Error("the preview queue is still marked running")
	}
	if failure := p.failure(job.key, job.info.Path); failure != nil {
		t.Errorf("the canceled render was recorded as a failure: %+v", *failure)
	}
}

// A card built while its own preview renders must not queue it again, or the mod is rendered twice in a row.
func TestModelPreviewDoesNotQueueTheRunningKey(t *testing.T) {
	s := &Shell{}
	s.settings.values = map[string]any{
		setting.KeyModelViewerToneMapping: "",
		setting.KeyModelViewerEnvironment: "",
		setting.KeyModelViewerExposure:    1.0,
		setting.KeyModelViewerToonShadows: true,
		setting.KeyModelViewerToonStyles:  "",
	}
	info := previewMod("a", [3]string{"Character.ini", "$outfit", "0"})
	p := &s.mods.modelPreviews
	p.current = &modelPreviewJob{key: modelPreviewKey(info, s.modelPreviewSettings())}

	if img, loading := s.modelPreview(info); img != nil || !loading {
		t.Errorf("modelPreview = %v, %v; want a placeholder while it renders", img, loading)
	}
	if len(p.queue) != 0 {
		t.Errorf("the running preview was queued again: %v", slices.Collect(maps.Keys(p.queue)))
	}
}

// Previews share one render slot, so moving the pointer to another preset must drop the presets it left, running or
// queued, without counting the stopped render as a failure.
func TestTogglePresetPreviewDropsTheLeftPresets(t *testing.T) {
	s := &Shell{}
	s.settings.values = map[string]any{
		setting.KeyModelViewerToneMapping: "",
		setting.KeyModelViewerEnvironment: "",
		setting.KeyModelViewerExposure:    1.0,
		setting.KeyModelViewerToonShadows: true,
		setting.KeyModelViewerToonStyles:  "",
	}
	info := previewMod("a", [3]string{"Character.ini", "$outfit", "0"})
	p := &s.mods.modelPreviews
	canceled := false
	running := &modelPreviewJob{key: "preset a", preset: &mod.TogglePreset{ID: "a"}}
	p.current, p.cancel = running, func() { canceled = true }
	p.queue = map[string]*modelPreviewJob{
		"preset b": {key: "preset b", preset: &mod.TogglePreset{ID: "b"}, wanted: time.Now()},
		"card":     {key: "card", info: info, wanted: time.Now()},
	}

	preset := mod.TogglePreset{ID: "c", INIs: map[string]map[string]string{"Character.ini": {"outfit": "1"}}}
	if img, loading := s.requestModelPreview(info, &preset); img != nil || !loading {
		t.Errorf("requestModelPreview = %v, %v; want a placeholder", img, loading)
	}

	if !running.stopped || !canceled {
		t.Errorf("the render of the left preset kept running: stopped=%v canceled=%v", running.stopped, canceled)
	}
	var presets []string
	for _, job := range p.queue {
		if job.preset != nil {
			presets = append(presets, job.preset.ID)
		}
	}
	if !slices.Equal(presets, []string{"c"}) || p.queue["card"] == nil {
		t.Errorf("queue = %v, want the card and preset c", slices.Collect(maps.Keys(p.queue)))
	}
}

// The view builds the cards before the preset under the pointer, so a free render slot must go to the preset only
// after the whole build has asked, not to the first card that asked.
func TestModelPreviewStartsThePresetBeforeCardsOfTheSameBuild(t *testing.T) {
	s := &Shell{}
	s.settings.values = map[string]any{
		setting.KeyModelViewerToneMapping: "",
		setting.KeyModelViewerEnvironment: "",
		setting.KeyModelViewerExposure:    1.0,
		setting.KeyModelViewerToonShadows: true,
		setting.KeyModelViewerToonStyles:  "",
	}
	// The render goroutine stops before rendering, so the test sees only which job took the slot.
	s.deps.Ready = func(context.Context) error { return context.Canceled }
	card := previewMod("card", [3]string{"Card.ini", "$outfit", "0"})
	card.Path = `D:\Mods\Card`
	info := previewMod("a", [3]string{"Character.ini", "$outfit", "0"})
	preset := mod.TogglePreset{ID: "c", INIs: map[string]map[string]string{"Character.ini": {"outfit": "1"}}}
	p := &s.mods.modelPreviews

	s.modelPreview(card)
	s.requestModelPreview(info, &preset)
	if p.current != nil {
		t.Fatalf("a render started while the build was still asking: %s", p.current.key)
	}

	s.pumpModelPreviews()
	if p.current == nil || p.current.preset == nil || p.current.preset.ID != "c" {
		t.Errorf("the free slot went to %+v, want preset c", p.current)
	}
}

// A cached image must not outlive a change to what it shows.
func TestModelPreviewKeyFollowsWhatTheImageShows(t *testing.T) {
	settings := modelPreviewSettings{ToneMapping: "neutral", Environment: "studio", Exposure: 0.7}
	base := modelPreviewKey(previewMod("a", [3]string{"Character.ini", "$outfit", "0"}), settings)

	changed := map[string]string{
		"toggle value": modelPreviewKey(previewMod("a", [3]string{"Character.ini", "$outfit", "1"}), settings),
		"signature":    modelPreviewKey(previewMod("b", [3]string{"Character.ini", "$outfit", "0"}), settings),
		"settings": modelPreviewKey(previewMod("a", [3]string{"Character.ini", "$outfit", "0"}),
			modelPreviewSettings{ToneMapping: "neutral", Environment: "studio", Exposure: 1}),
	}
	for name, key := range changed {
		if key == base {
			t.Errorf("a change of %s keeps the key %s", name, key)
		}
	}

	spelled := previewMod("a", [3]string{"CHARACTER.INI", "$Outfit", "0"})
	spelled.Path = `d:\mods\character`
	if key := modelPreviewKey(spelled, settings); key != base {
		t.Errorf("another spelling of the same mod gives %s, want %s", key, base)
	}

	toggled := previewMod("a", [3]string{"Character.ini", "$outfit", "0"})
	toggled.Path = `D:\Mods\DISABLED Character`
	if key := modelPreviewKey(toggled, settings); key != base {
		t.Errorf("the disabled mod gives %s, want %s", key, base)
	}
}
