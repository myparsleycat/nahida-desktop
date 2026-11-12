package ui

import (
	"context"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

// advancedSection edits raw stored setting rows.
type advancedSection struct {
	rows    []setting.AdvancedRow
	loading bool
	failed  bool
	search  string
	// editing is the key of the row being edited and draft its text.
	editing string
	draft   string
	// shown is the key of the row whose full value is open.
	shown string
}

func (s *Shell) loadAdvanced() {
	adv := &s.settings.advanced
	if adv.loading {
		return
	}
	adv.loading, adv.failed = true, false
	go func() {
		rows, err := s.deps.Settings.AdvancedGetAll(context.Background())
		if err != nil {
			_ = infra.ReportError(s.deps.Log, err, "Setting.AdvancedGetAll", infra.Diagnostic{
				Operation: "settings.advanced", Stage: "load",
			})
		}
		fairy.Go(func() {
			adv.loading = false
			if err != nil {
				adv.failed = true
				return
			}
			if rows == nil {
				rows = []setting.AdvancedRow{}
			}
			adv.rows = rows
		})
	}()
}

func (s *Shell) advancedSettings(viewHeight float32) *fairy.El {
	const prefix = "page.setting.adv."
	t := kit.T()
	adv := &s.settings.advanced
	switch {
	case adv.failed:
		return fairy.Col(
			fairy.Text(T("shell.setting_load_failed")).Color(t.Destructive),
			kit.Button(T("page.setting.acc.retry"), s.loadAdvanced, kit.Outline, kit.Small),
		).Gap(10).Pad(16).Align(fairy.Start)
	case adv.rows == nil:
		s.loadAdvanced()
		return fairy.Text(T("page.setting.network.loading")).Color(t.MutedForeground).Pad(16)
	}

	warning := fairy.Row(
		fairy.Icon(lucide.Info).Size(16, 16).Color(t.Destructive).Shrink(0),
		fairy.Col(
			fairy.Text(T(prefix+"warning_title")).FontSize(14).LineHeight(20).Medium().Color(t.Destructive),
			fairy.Text(T(prefix+"warning_description")).FontSize(14).LineHeight(20).Color(t.Destructive.Alpha(0.9)),
		).Gap(2).Grow(1).MinW(0),
	)
	warning.Gap(8).PadXY(10, 8).Radius(t.Radius).Border(1, t.Border).Fill(t.Card).FontSize(14)
	search := kit.TextField(adv.search, func(value string) { adv.search = value }, kit.Icon(lucide.Search),
		kit.Placeholder(T(prefix+"search_placeholder")), kit.Label(T(prefix+"search_placeholder")))

	query := strings.ToLower(strings.TrimSpace(adv.search))
	header := fairy.Row(
		advancedCell(fairy.Text("KEY").LineHeight(19.5), 1, 150),
		advancedCell(fairy.Text("VALUE").LineHeight(19.5), 3, 200),
		fairy.Box(fairy.Text("ACTION").LineHeight(19.5)).W(80).Pad(12).Center(),
	).Radius(6).Fill(t.Muted.Alpha(0.5)).Medium().LetterSpacing(0.65).Color(t.MutedForeground)
	table := fairy.Col(header).FontSize(13)
	matches := 0
	for _, row := range adv.rows {
		if !strings.Contains(strings.ToLower(row.Key), query) {
			continue
		}
		matches++
		table.Child(s.advancedRow(row, viewHeight))
	}

	body := fairy.Col(warning, search, table).Gap(16).Pad(12)
	if query != "" && matches == 0 {
		body.Child(
			fairy.Text(T(prefix+"no_results")).FontSize(14).LineHeight(20).Color(t.MutedForeground).Center().Pad(24).
				Radius(6).Border(1, t.Border).BorderDashed(),
		)
	}
	return body
}

func advancedCell(content *fairy.El, grow, minWidth float32) *fairy.El {
	return fairy.Box(content).Grow(grow).Basis(0).MinW(minWidth).Pad(12).Justify(fairy.JustifyCenter)
}

func (s *Shell) advancedRow(row setting.AdvancedRow, viewHeight float32) *fairy.El {
	t := kit.T()
	adv := &s.settings.advanced
	key := row.Key
	value := deref(row.Value)

	var valueCell, action *fairy.El
	if adv.editing == key {
		valueCell = kit.TextField(adv.draft, func(text string) { adv.draft = text }, kit.Label(key)).
			FontSize(13).LineHeight(19.5).Key("advanced.edit." + key).AutoFocus()
		save := func() {
			s.saveAdvanced(key, adv.draft)
			adv.editing = ""
		}
		action = fairy.Row(
			kit.IconButton(lucide.Save, save, kit.Ghost, kit.Small, kit.Label(T("g.save"))).Size(32, 32),
			kit.IconButton(lucide.X, func() { adv.editing = "" }, kit.Ghost, kit.Small, kit.Label(T("g.cancel"))).
				Size(32, 32),
		).Gap(4)
	} else {
		text := fairy.Text(value).FontSize(13).LineHeight(19.5).Ellipsis().WFull()
		if row.Value != nil {
			text.Cursor(fairy.CursorPointer).HoverColor(t.Primary).Focusable().Role(fairy.RoleButton).
				OnClick(func() { adv.shown = key })
		}
		if adv.shown == key {
			text.Popover(t.Surface(fairy.Col(fairy.Text(value).FontSize(14).LineHeight(20)), t.Popover).
				W(320).MaxH(viewHeight*0.6).ScrollY().Pad(10).Radius(t.RadiusLg()).
				Ring(1, t.Foreground.Alpha(0.1)).Color(t.PopoverForeground),
				fairy.PopoverOptions{Align: fairy.AlignStart, OnDismiss: func() { adv.shown = "" }})
		}
		valueCell = text
		action = kit.IconButton(lucide.Pencil, func() { adv.editing, adv.draft = key, value }, kit.Ghost, kit.Small,
			kit.Label(T("shell.edit"))).Size(32, 32)
	}
	return fairy.Row(
		advancedCell(fairy.Text(key).LineHeight(19.5).Medium(), 1, 150),
		advancedCell(valueCell, 3, 200),
		fairy.Box(action).W(80).Pad(12).Center(),
	).Key("advanced."+key).BorderEdges(0, 0, 1, 0, t.Border)
}

func (s *Shell) saveAdvanced(key, value string) {
	go func() {
		err := s.deps.Settings.AdvancedSet(context.Background(), key, value)
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Setting.AdvancedSet", infra.Diagnostic{
				Operation: "settings.advanced", Stage: "save", Fields: map[string]any{"key": key},
			})
		}
		fairy.Go(func() {
			if err != nil {
				s.toasts.Show(kit.ToastError, T("shell.setting_save_failed"), err.Error())
			}
			s.settings.advanced.rows = nil
		})
	}()
}
