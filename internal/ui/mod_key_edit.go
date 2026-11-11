package ui

import (
	"context"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
)

// modKeyEdit is a key or back binding of a mod's toggle section being recorded.
type modKeyEdit struct {
	modPath, iniPath, section string
	variable                  string // "key" or "back"
	recording                 keyRecording
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

func (s *Shell) modKeyDialog(cx *fairy.Cx) {
	d := s.mods.keyEdit
	if d == nil {
		return
	}
	title := strings.ToUpper(d.variable[:1]) + d.variable[1:]
	dismiss := func() { s.mods.keyEdit = nil }
	save := func() {
		s.mods.keyEdit = nil
		s.saveModKey(*d)
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       title,
		Description: T("page.mod.ini.key_dialog_description"),
		Body:        keyRecorder(&d.recording, title, "mod.ini-key-manual", save),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), dismiss, kit.Outline),
			kit.Button(T("g.save"), save),
		},
		OnClose: dismiss,
		Key:     "mod.ini-key",
	})
}

func (s *Shell) saveModKey(d modKeyEdit) {
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
