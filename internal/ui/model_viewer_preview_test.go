package ui

import (
	"context"
	"maps"
	"slices"
	"testing"

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
