package ui

import (
	"context"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

// pathPrompt is a download waiting for its location, from a pathSelector:modeSelect event.
type pathPrompt struct {
	id             string
	suggestedName  string
	suggestedNames []string
	targetName     string
	importerKey    string
	source         string
	// fileName is the name typed in the mod manager overlay.
	fileName string
	busy     bool
}

func (s *Shell) promptPath(payload any) {
	data, _ := payload.(map[string]any)
	id, _ := data["selectionId"].(string)
	if id == "" {
		return
	}
	p := pathPrompt{id: id, source: asString(data["downloadSource"])}
	p.suggestedName = asString(data["suggestedName"])
	p.suggestedNames, _ = data["suggestedNames"].([]string)
	p.targetName = asString(data["downloadTargetName"])
	p.importerKey = asString(data["downloadImporterKey"])
	p.fileName = p.suggestedName
	s.paths = append(s.paths, p)
}

// answerPath drops the prompt that is answered. cancel tells the backend that no location was chosen.
func (s *Shell) answerPath(id string, cancel bool) {
	for i, p := range s.paths {
		if p.id == id {
			s.paths = append(s.paths[:i:i], s.paths[i+1:]...)
			break
		}
	}
	if cancel {
		s.cancelPath(id)
	}
}

func (s *Shell) cancelPath(id string) {
	go func() {
		if err := s.deps.Mod.CancelPathSelection(id); err != nil {
			_ = infra.ReportError(s.deps.Log, err, "PathSelector", infra.Diagnostic{
				Operation: "cancel", Fields: map[string]any{"selectionId": id},
			})
		}
	}()
}

func (s *Shell) showPathPrompt(cx *fairy.Cx) {
	if len(s.paths) == 0 {
		return
	}
	t := kit.T()
	p := &s.paths[0]
	const prefix = "components.path-selector-dialog."
	description := T(prefix+"description", "name", p.suggestedName)
	if len(p.suggestedNames) > 1 {
		description = T(prefix+"multiple_description", "count", len(p.suggestedNames))
	}
	id := p.id
	choice := func(icon *svg.Vector, title, line1, line2 string, onClick func()) *fairy.El {
		fill, hover, border := t.Background, t.Muted, t.Border
		if t.Dark {
			fill, hover, border = opacity(t.Input, 0.3), opacity(t.Input, 0.5), t.Input
		}
		return fairy.Col(
			fairy.Icon(icon).Size(32, 32),
			fairy.Col(
				fairy.Text(title).FontSize(14).LineHeight(20).SemiBold().TextAlign(fairy.TextCenter),
				fairy.Col(
					fairy.Text(line1).FontSize(12).LineHeight(16).Color(t.MutedForeground).TextAlign(fairy.TextCenter),
					fairy.Text(line2).FontSize(12).LineHeight(16).Color(t.MutedForeground).TextAlign(fairy.TextCenter),
				).Align(fairy.Center),
			).Align(fairy.Center),
		).Grow(1).Basis(0).MinW(0).Pad(12).Gap(8).Align(fairy.Center).Radius(t.RadiusLg()).Border(1, border).
			Fill(fill).HoverFill(hover).HoverColor(t.Foreground).Medium().Cursor(fairy.CursorPointer).Disabled(p.busy).
			Role(fairy.RoleButton).Label(title).Focusable().FocusVisibleBorder(t.Ring).FocusRing(3, t.Ring.Alpha(0.5)).
			Animate(motion.Fast).PressOffset(0, 1).OnClick(onClick)
	}
	body := fairy.Row(
		choice(lucide.Grid3x3, T(prefix+"mod_manager.title"), T(prefix+"mod_manager.description.0"),
			T(prefix+"mod_manager.description.1"), func() { s.choosePathInModManager(id) }),
		choice(lucide.FolderOpen, T(prefix+"explorer.title"), T(prefix+"explorer.description.0"),
			T(prefix+"explorer.description.1"), func() { s.choosePathInExplorer(id) }),
	).Gap(12).PadXY(0, 16)
	kit.ShowDialog(cx, kit.DialogProps{
		Title:           T(prefix + "title"),
		Description:     description,
		Body:            body,
		Width:           384,
		Actions:         []*fairy.El{kit.Button(T("g.cancel"), func() { s.answerPath(id, true) }, kit.Ghost)},
		OnClose:         func() { s.answerPath(id, true) },
		HideCloseButton: true,
		Key:             "path-selector." + id,
	})
}

func (s *Shell) choosePathInExplorer(id string) {
	if len(s.paths) == 0 || s.paths[0].id != id || s.paths[0].busy {
		return
	}
	s.paths[0].busy = true
	s.background(func(ctx context.Context) {
		selected, err := s.deps.Mod.SelectFolderPath(ctx, id)
		fairy.Go(func() {
			if err != nil {
				s.toasts.Show(kit.ToastError, T("components.path-selector-dialog.select_path_failed"), "")
				s.answerPath(id, false)
				return
			}
			if selected {
				s.answerPath(id, false)
				return
			}
			for i := range s.paths {
				if s.paths[i].id == id {
					s.paths[i].busy = false
				}
			}
		})
	})
}

// choosePathInModManager hands the download to the mod screen, where the selected folder becomes its
// location.
func (s *Shell) choosePathInModManager(id string) {
	for _, p := range s.paths {
		if p.id == id {
			if s.download != nil {
				s.cancelPath(s.download.id)
			}
			download := p
			s.download = &download
			break
		}
	}
	s.answerPath(id, false)
	s.navigate(RouteMod)
}

func (s *Shell) downloadOverlay() *fairy.El {
	t := kit.T()
	d := s.download
	const prefix = "components.download-confirmation-overlay."
	returnKey := setting.KeyModReturnToGamebananaAfterDownload
	s.settingsLoaded(returnKey)

	group := s.mods.group
	location := T(prefix + "need_select_character_folder")
	if group != nil {
		location = group.Name
	}
	card := fairy.Col(
		fairy.Col(
			fairy.Text(T(prefix+"title")).FontSize(18).LineHeight(28).SemiBold(),
			fairy.Text(T(prefix+"description")).FontSize(14).LineHeight(20).Color(t.MutedForeground),
		).Gap(4),
	).W(448).Pad(16).Gap(16).Radius(t.RadiusLg()).Fill(t.Background.Alpha(0.75)).Ring(1, t.Foreground).
		Shadow(0, 10, 15, -3, shadowColor(t)).OnClick(func() {})
	frosted(card, 16)
	field := func(label string, control *fairy.El) *fairy.El {
		return fairy.Col(fairy.Text(label).FontSize(14).LineHeight(20).Medium(),
			control.Border(1, t.Foreground.Alpha(0.2))).Gap(4)
	}
	if d.suggestedName != "" {
		card.Child(field(T(prefix+"file_name"), kit.TextField(d.fileName, func(v string) { d.fileName = v },
			kit.Placeholder(T(prefix+"file_name_placeholder")), kit.Disabled(d.busy))))
	}
	card.Child(field(T(prefix+"download_location"), kit.TextField(location, nil).
		FocusVisibleBorder(t.Foreground.Alpha(0.2)).FocusRing(0, fairy.Transparent)))
	if d.source == "gamebanana" {
		card.Child(kit.Checkbox(s.boolSetting(returnKey), func(on bool) { s.setSetting(returnKey, on) },
			kit.Label(T(prefix+"return_to_gamebanana"))))
	}
	canSelect := group != nil && (d.suggestedName == "" || strings.TrimSpace(d.fileName) != "") && !d.busy
	card.Child(fairy.Row(
		kit.Button(T("g.cancel"), s.cancelDownload, kit.Outline, kit.Disabled(d.busy)).Grow(1),
		kit.Button(T("g.select"), s.confirmDownload, kit.Icon(lucide.Download), kit.Disabled(!canSelect),
			kit.Loading(d.busy)).Grow(1),
	).Gap(8).MarginEdges(8, 0, 0, 0))
	return fairy.Box(card).Key("mod.download-overlay").Absolute().Inset(0).Center().Pad(16).
		Fill(fairy.RGBA(0, 0, 0, 0.2)).OnClick(func() {})
}

func (s *Shell) cancelDownload() {
	d := s.download
	if d == nil {
		return
	}
	s.download = nil
	s.cancelPath(d.id)
}

func (s *Shell) confirmDownload() {
	d := s.download
	group := s.mods.group
	if d == nil || group == nil || d.busy {
		return
	}
	var fileName *string
	if d.suggestedName != "" {
		name := strings.TrimSpace(d.fileName)
		fileName = &name
	}
	d.busy = true
	id, path, gameBanana := d.id, group.Path, d.source == "gamebanana"
	returnToGameBanana := s.boolSetting(setting.KeyModReturnToGamebananaAfterDownload)
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.SelectModManagerPath(ctx, id, path, fileName)
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "PathSelector", infra.Diagnostic{
				Operation: "select-mod-manager-path", Fields: map[string]any{"selectionId": id, "path": path},
			})
		}
		fairy.Go(func() {
			if s.download == nil || s.download.id != id {
				return
			}
			if err != nil {
				s.download.busy = false
				s.toasts.Show(kit.ToastError, T("components.download-confirmation-overlay.select_path_failed"), "")
				return
			}
			s.download = nil
			if gameBanana && returnToGameBanana {
				s.navigate(RouteGameBanana)
			}
		})
	})
}
