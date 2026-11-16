package ui

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
)

// modKeyEdit is a key or back binding of a mod's toggle section, or an input key of an NTE mod, being recorded.
type modKeyEdit struct {
	modPath, iniPath, section string
	variable                  string // "key" or "back"
	nte                       *mod.NteKey
	// nteUsed holds the other keys of the NTE key's package, and problem the locale key of why the recording
	// problemFor cannot be saved.
	nteUsed    []string
	problem    string
	problemFor string
	recording  keyRecording
}

func (s *Shell) openModKeyEdit(info mod.ModInfo, iniPath, section, variable, value string) {
	var others []string
	for _, ini := range info.Inis {
		for _, toggle := range ini.ToggleKeys {
			if toggle.SectionName == section && platform.SamePathFold(ini.Path, iniPath) {
				continue
			}
			for _, binding := range []*string{toggle.Key, toggle.Back} {
				if binding != nil && *binding != "" {
					others = append(others, *binding)
				}
			}
		}
	}
	s.mods.keyEdit = &modKeyEdit{
		modPath: info.Path, iniPath: iniPath, section: section, variable: variable,
		recording: keyRecording{value: value, otherKeys: others},
	}
}

func (s *Shell) openNteKeyEdit(info mod.ModInfo, key mod.NteKey) {
	var used []string
	for _, other := range info.NteKeys {
		if other.Container == key.Container && other.Chunk == key.Chunk && other.Key != key.Key {
			used = append(used, other.Key)
		}
	}
	s.mods.keyEdit = &modKeyEdit{
		modPath: info.Path, variable: "key", nte: &key, nteUsed: used,
		recording: keyRecording{value: mod.NteKeyBinding(key.Key)},
	}
}

func (s *Shell) modKeyDialog(cx *fairy.Cx) {
	d := s.mods.keyEdit
	if d == nil {
		return
	}
	title := strings.ToUpper(d.variable[:1]) + d.variable[1:]
	description := T("page.mod.ini.key_dialog_description")
	if d.nte != nil {
		description = T("page.mod.ini.nte_key_dialog_description")
	}
	dismiss := func() { s.mods.keyEdit = nil }
	save := func() {
		if d.nte != nil {
			key, ok := mod.NteKeyFromBinding(d.recording.value)
			switch {
			case !ok:
				d.problem, d.problemFor = "page.mod.ini.nte_key_unsupported", d.recording.value
				return
			case slices.Contains(d.nteUsed, key):
				d.problem, d.problemFor = "page.mod.ini.nte_key_in_use", d.recording.value
				return
			}
		}
		s.mods.keyEdit = nil
		s.saveModKey(*d)
	}
	body := keyRecorder(&d.recording, title, "mod.ini-key-manual", save)
	if d.problem != "" && d.problemFor == d.recording.value {
		body = fairy.Col(body, fairy.Text(T(d.problem)).FontSize(13).
			Color(kit.T().Destructive)).Gap(8)
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       title,
		Description: description,
		Body:        body,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), dismiss, kit.Outline),
			kit.Button(T("g.save"), save),
		},
		OnClose: dismiss,
		Key:     "mod.ini-key",
	})
}

func (s *Shell) saveModKey(d modKeyEdit) {
	if d.nte != nil {
		s.saveNteKey(d)
		return
	}
	value := d.recording.value
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.UpdateToggleKey(ctx, d.modPath, d.iniPath, d.section, d.variable, value)
		if err != nil {
			s.modError(err, "update-toggle-key", map[string]any{
				"modPath": d.modPath, "iniPath": d.iniPath, "section": d.section, "variable": d.variable,
				"value": value,
			})
		}
		fairy.Go(func() {
			if err != nil {
				s.toasts.Show(kit.ToastError, err.Error(), "")
				return
			}
			s.refreshListing()
		})
	})
}

func (s *Shell) saveNteKey(d modKeyEdit) {
	key := *d.nte
	newKey, _ := mod.NteKeyFromBinding(d.recording.value)
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.UpdateNteKey(ctx, d.modPath, key.Container, key.Chunk, key.Key, newKey)
		locked := errors.Is(err, mod.ErrModFolderLocked)
		if err != nil && !locked {
			s.modError(err, "update-nte-key", map[string]any{
				"modPath": d.modPath, "container": key.Container, "package": key.Package, "oldKey": key.Key,
				"newKey": newKey,
			})
		}
		fairy.Go(func() {
			switch {
			case locked:
				s.showFolderError(err, "page.mod.hooks.use-mod-mutations.toggle-mutation.")
			case err != nil:
				s.toasts.Show(kit.ToastError, T("page.mod.ini.nte_key_save_failed"), "")
			default:
				s.refreshListing()
			}
		})
	})
}
