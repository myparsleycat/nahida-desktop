package ui

import (
	"context"
	"errors"
	"maps"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerPresetDraft is the dialog that saves the viewer's toggle state as a preset. choosing shows the store
// chooser first when no store is chosen yet.
type viewerPresetDraft struct {
	name      string
	busy      bool
	choosing  bool
	storePick string
}

func (v *ModelViewer) loadTogglePresets() {
	if v.shell == nil || v.shell.deps.Mod == nil {
		return
	}
	v.shell.background(func(ctx context.Context) {
		list, err := v.shell.deps.Mod.GetTogglePresets(ctx, v.path)
		fairy.Go(func() {
			if err == nil {
				v.togglePresets = list.Presets
			}
		})
	})
}

func (v *ModelViewer) togglePresetMenuItems() []kit.MenuItem {
	if v.shell == nil || v.shell.deps.Mod == nil {
		return nil
	}
	presets := []kit.MenuItem{{Label: T("page.tools.model_viewer.menu.no_presets"), Disabled: true}}
	if len(v.togglePresets) > 0 {
		presets = presets[:0]
		for _, preset := range v.togglePresets {
			presets = append(presets, kit.MenuItem{Label: preset.Name, OnClick: func() { v.applyTogglePreset(preset) }})
		}
	}
	supported, known := v.desktopChannel()
	var items []kit.MenuItem
	if known && !supported {
		items = append(items, desktopChannelNote())
	}
	return append(items, []kit.MenuItem{
		{
			Label: T("page.tools.model_viewer.menu.save_as_preset"), Icon: lucide.BookmarkPlus,
			Disabled: v.scene == nil || !supported,
			OnClick: func() {
				v.presetDraft = &viewerPresetDraft{storePick: mod.TogglePresetStoreMetadata}
			},
		},
		{
			Label: T("page.tools.model_viewer.menu.apply_preset"), Icon: lucide.Bookmark,
			Disabled: v.scene == nil || v.applyingPreset || !supported, Submenu: presets,
		},
	}...)
}

// desktopChannel reports whether the games of the viewer's importer serve the desktop channel. A game linked now
// serves it even when the next launch would deploy a DLL without it. known is false until that has been read.
func (v *ModelViewer) desktopChannel() (supported, known bool) {
	switch {
	case v.gameLinked:
		return true, true
	case v.shell == nil:
		return false, false
	}
	return v.shell.desktopChannel(v.importer)
}

func (v *ModelViewer) togglePresetDialogs(cx *fairy.Cx) {
	d := v.presetDraft
	if d == nil {
		return
	}
	close := func() {
		if !d.busy {
			v.presetDraft = nil
		}
	}
	if d.choosing {
		togglePresetStoreChooser(cx, "model-viewer.toggle-preset-store", &d.storePick, d.busy,
			v.chooseStoreAndSave, close)
		return
	}
	save := func(string) { v.saveTogglePreset() }
	name := strings.TrimSpace(d.name)
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T("page.tools.model_viewer.dialog.save_preset.title"),
		Body: kit.TextField(d.name, func(value string) { d.name = value },
			kit.Placeholder(T(togglePresetsPrefix+"name-placeholder")), kit.Label(T("g.name")),
			kit.Disabled(d.busy)).AutoFocus().OnSubmit(save),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T("g.save"), func() { save("") }, kit.Loading(d.busy), kit.Disabled(name == "")),
		},
		OnClose: close,
		Key:     "model-viewer.save-preset",
	})
}

// saveTogglePreset saves the persist variables of the current toggle state of each INI.
func (v *ModelViewer) saveTogglePreset() {
	d := v.presetDraft
	scene := v.scene
	if d == nil || d.busy || scene == nil {
		return
	}
	name := strings.TrimSpace(d.name)
	if name == "" {
		return
	}
	inis := map[string]map[string]any{}
	for _, ini := range scene.transport.INIs {
		inis[ini.Path] = iniToggleValues(maps.Clone(v.state), ini.Prefix)
	}
	d.busy = true
	toasts := v.toasts
	v.shell.background(func(ctx context.Context) {
		preset, err := v.shell.deps.Mod.CreateTogglePresetFromState(ctx, v.path, name, inis)
		fairy.Go(func() {
			d.busy = false
			if errors.Is(err, mod.ErrTogglePresetStoreUnset) {
				d.choosing = true
				return
			}
			if err != nil {
				showTogglePresetError(toasts, err)
				return
			}
			if v.presetDraft == d {
				v.presetDraft = nil
			}
			toasts.Show(kit.ToastSuccess, T(togglePresetsPrefix+"toast.saved"),
				T(togglePresetsPrefix+"toast.saved-from", "source", togglePresetSourceLabel(preset.Source)))
			v.loadTogglePresets()
		})
	})
}

func (v *ModelViewer) chooseStoreAndSave(store string) {
	d := v.presetDraft
	if d == nil || d.busy {
		return
	}
	d.busy = true
	toasts := v.toasts
	v.shell.background(func(ctx context.Context) {
		_, err := v.shell.deps.Mod.SetTogglePresetStore(ctx, store)
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				showTogglePresetError(toasts, err)
				return
			}
			d.choosing = false
			v.saveTogglePreset()
		})
	})
}

// applyTogglePreset writes the preset to the INIs and the running games, then shows its values in the viewer.
func (v *ModelViewer) applyTogglePreset(preset mod.TogglePreset) {
	if v.applyingPreset || v.shell == nil {
		return
	}
	v.applyingPreset = true
	toasts := v.toasts
	v.shell.background(func(ctx context.Context) {
		result, err := v.shell.deps.Mod.ApplyTogglePreset(ctx, v.path, preset.ID)
		fairy.Go(func() {
			v.applyingPreset = false
			if err != nil {
				showTogglePresetError(toasts, err)
				return
			}
			if v.scene != nil {
				v.setToggleState(viewerStateWithValues(v.state, v.scene.transport.INIs, result.Values))
			}
			showToggleApplyResult(toasts, result)
		})
	})
}

// viewerStateWithValues sets the variables of state that values, keyed by absolute INI path and declared name,
// holds a value for.
func viewerStateWithValues(
	state map[string]any,
	inis []modelviewer.ModelViewerINI,
	values map[string]map[string]string,
) map[string]any {
	next := maps.Clone(state)
	for _, ini := range inis {
		for path, vars := range values {
			if !platform.SamePathFold(path, ini.Path) {
				continue
			}
			for id, current := range next {
				name, ok := strings.CutPrefix(id, ini.Prefix)
				if !ok {
					continue
				}
				for varName, text := range vars {
					if strings.EqualFold(name, varName) {
						next[id] = viewerToggleValue(current, text)
					}
				}
			}
		}
	}
	return next
}
