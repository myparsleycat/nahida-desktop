package ui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/tools"
)

const fixToolRowStep float32 = 64

// fixToolManager keeps the scripts that fix tool presets run. picked is the preset being built, in run order;
// drag is the index of the row being dragged and dragFrom where it started.
type fixToolManager struct {
	tab     string
	scripts query[[]db.ScriptBasicRow]
	presets query[[]db.ScriptPresetWithScripts]
	search  string

	picked     []db.ScriptBasicRow
	presetName string
	saving     bool
	drag       int
	dragFrom   int
	dragging   bool
}

func (s *Shell) fixToolPage(cx *fairy.Cx) *fairy.El {
	f := &s.tools.fixTool
	if f.tab == "" {
		f.tab = "presets"
	}
	t := kit.T()
	segment := func(value, label string) *fairy.El {
		button := fairy.Row(fairy.Text(label).FontSize(14).LineHeight(20).Medium()).
			Key("tools.fix.tab."+value).Pad(8).Align(fairy.Center).Radius(8).Role(fairy.RoleButton).
			Selected(f.tab == value).Focusable().FocusRing(3, t.Ring.Alpha(0.5)).
			OnClick(func() { f.tab = value })
		if f.tab == value {
			button.Ring(1, t.Foreground)
		}
		return button
	}
	body := fairy.Col(
		fairy.Row(segment("presets", "Presets"), segment("builder", "Builder")).
			H(40).Pad(6).Gap(8).Radius(14).Fill(t.Card).AlignSelf(fairy.Start).Shrink(0).Role(fairy.RoleGroup),
	).Gap(8).Pad(16).WFull().Grow(1).MinH(0)
	if f.tab == "presets" {
		return body.Child(s.fixPresetList().Grow(1).MinH(0))
	}
	return body.Child(fairy.Row(
		s.fixScriptList(cx).Basis(0).Grow(1).MinW(0),
		fairy.Box(fairy.Icon(lucide.ArrowRight).Size(16, 16)).W(32).Center().Shrink(0),
		s.fixPresetBuilder().Basis(0).Grow(1).MinW(0),
	).Gap(12).Grow(1).MinH(0))
}

func sortedByName[T any](items []T, name func(T) string) []T {
	less := platform.NewLocaleLess()
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(a, b T) int {
		switch {
		case less(name(a), name(b)):
			return -1
		case less(name(b), name(a)):
			return 1
		}
		return 0
	})
	return out
}

const fixScriptsKey = "tools.fix.scripts"

func fixPaneHeader(title, description string, trailing ...*fairy.El) *fairy.El {
	t := kit.T()
	text := fairy.Col(fairy.Text(title).FontSize(16).SemiBold()).Gap(2).Grow(1).MinW(0)
	if description != "" {
		text.Child(fairy.Text(description).FontSize(14).Color(t.MutedForeground).Wrap())
	}
	return fairy.Row(append([]*fairy.El{text}, trailing...)...).Gap(8).Align(fairy.Center)
}

func fixScriptIcon(script db.ScriptBasicRow) *fairy.El {
	icon := lucide.SquareTerminal
	if script.Type == "python" {
		icon = lucide.FileCog
	}
	return fairy.Icon(icon).Size(24, 24).Color(kit.T().MutedForeground).Shrink(0).Tooltip(script.Type)
}

func (s *Shell) fixPresetList() *fairy.El {
	f := &s.tools.fixTool
	t := kit.T()
	presets := fetch(s, &f.presets, s.deps.Tools.GetPresets)
	list := fairy.Col().Gap(8).Pad(12)
	if len(presets.value) == 0 {
		list.Child(fairy.Text(T("page.tools.fix-tool-manager.presets.empty")).FontSize(14).Color(t.MutedForeground))
	}
	for _, preset := range sortedByName(presets.value, func(p db.ScriptPresetWithScripts) string { return p.Name }) {
		id, name := preset.ID, preset.Name
		list.Child(fairy.Row(
			fairy.Text(name).FontSize(14).Medium().Ellipsis().Grow(1).MinW(0).TooltipWhenTruncated(),
			kit.IconButton(lucide.Trash, func() {
				s.deleteFixItem("fix-tool.delete-preset", map[string]any{"preset": name}, &f.presets,
					func(ctx context.Context) error { return s.deps.Tools.DeletePreset(ctx, id) })
			}, kit.Destructive, kit.Label(T("g.delete"))),
		).Key("tools.fix.preset." + id).Gap(8).Align(fairy.Center).Pad(12).Radius(6).HoverFill(hoverFill(t)).
			Animate(motion.Fast))
	}
	return fairy.Col(
		fixPaneHeader(T("page.tools.fix-tool-manager.presets.title"), "").Pad(12).BorderEdges(0, 0, 1, 0, t.Border),
		fairy.Col(list).Grow(1).MinH(0).ScrollY(),
	)
}

// deleteFixItem runs a delete and reads the list it changed again.
func (s *Shell) deleteFixItem(
	operation string,
	fields map[string]any,
	list interface{ invalidate() },
	del func(context.Context) error,
) {
	s.background(func(ctx context.Context) {
		err := del(ctx)
		if err != nil {
			err = s.toolError(err, operation, fields)
		}
		fairy.Go(func() {
			list.invalidate()
			if err != nil {
				s.toasts.Show(kit.ToastError, err.Error(), "")
			}
		})
	})
}

func (s *Shell) fixScriptList(cx *fairy.Cx) *fairy.El {
	f := &s.tools.fixTool
	t := kit.T()
	const prefix = "page.tools.fix-tool-manager.builder.left."
	scripts := fetch(s, &f.scripts, s.deps.Tools.GetScripts)
	query := strings.ToLower(f.search)
	var visible []db.ScriptBasicRow
	for _, script := range sortedByName(scripts.value, func(r db.ScriptBasicRow) string { return r.Name }) {
		picked := slices.ContainsFunc(f.picked, func(p db.ScriptBasicRow) bool { return p.ID == script.ID })
		if !picked && strings.Contains(strings.ToLower(script.Name), query) {
			visible = append(visible, script)
		}
	}

	list := fairy.Col().Gap(2).Pad(12)
	if len(visible) == 0 {
		list.Child(fairy.Col(
			fairy.Text(T(prefix+"noScripts.title")).FontSize(14).Medium().Color(t.MutedForeground),
			fairy.Text(T(prefix+"noScripts.description")).FontSize(12).Color(t.MutedForeground).Wrap(),
		).Gap(4).Align(fairy.Center).PadXY(18, 50).Radius(8).Border(2, t.Border).BorderDashed())
	}
	for _, script := range visible {
		list.Child(fairy.Row(
			fixScriptIcon(script),
			fairy.Text(script.Name).FontSize(14).Medium().Ellipsis().Grow(1).MinW(0).TooltipWhenTruncated(),
			kit.IconButton(lucide.Plus, func() { f.picked = append(f.picked, script) }, kit.Outline,
				kit.Label(T(prefix+"add"))),
			kit.IconButton(lucide.Trash, func() {
				s.deleteFixItem("fix-tool.delete-script", map[string]any{"script": script.Name}, &f.scripts,
					func(ctx context.Context) error { return s.deps.Tools.DeleteScript(ctx, script.ID) })
			}, kit.Destructive, kit.Label(T("g.delete"))),
		).Key("tools.fix.script." + script.ID).Gap(8).Align(fairy.Center).Pad(12).Radius(6).HoverFill(hoverFill(t)).
			Animate(motion.Fast))
	}

	var dropBadge []*fairy.El
	if cx.FileDropTarget() == fixScriptsKey {
		dropBadge = append(dropBadge, kit.Badge(T(prefix+"dropToAdd"), kit.Icon(lucide.Upload)).Pulse(2, 0.5))
	}
	header := fairy.Col(
		fixPaneHeader(T(prefix+"title"), T(prefix+"description"), dropBadge...),
		kit.TextField(f.search, func(value string) { f.search = value }, kit.Placeholder(T(prefix+"search")),
			kit.Icon(lucide.Search), kit.Label(T(prefix+"search"))).Key("tools.fix.search"),
	).Gap(8).Pad(12).BorderEdges(0, 0, 1, 0, t.Border)
	return fairy.Col(header, fairy.Col(list).Grow(1).MinH(0).ScrollY()).Key(fixScriptsKey).Radius(8).
		OnFileDrop(func(drop fairy.FileDrop) { s.saveFixScripts(drop.Paths) }).
		DropFill(t.Accent.Alpha(0.05)).DropBorder(t.Accent.Alpha(0.2)).Animate(motion.Bezier(0.2, 0.4, 0, 0.2, 1))
}

// saveFixScripts adds the dropped .py and .exe files to the script library.
func (s *Shell) saveFixScripts(paths []string) {
	f := &s.tools.fixTool
	s.background(func(ctx context.Context) {
		for _, path := range paths {
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".py" && ext != ".exe" {
				continue
			}
			if err := s.deps.Tools.SaveScript(ctx, path); err != nil {
				_ = s.toolError(err, "fix-tool.save-script", map[string]any{"path": path})
			}
		}
		fairy.Go(f.scripts.invalidate)
	})
}

func (s *Shell) fixPresetBuilder() *fairy.El {
	f := &s.tools.fixTool
	t := kit.T()
	const prefix = "page.tools.fix-tool-manager.builder.right."

	list := fairy.Col().Gap(8).Pad(12)
	if len(f.picked) == 0 {
		list.Child(fairy.Col(
			fairy.Text(T(prefix+"noScripts.title")).FontSize(14).Color(t.MutedForeground),
			fairy.Text(T(prefix+"noScripts.description")).FontSize(12).Color(t.MutedForeground).Wrap(),
		).Gap(4).Align(fairy.Center).PadXY(18, 50).Radius(8).Border(2, t.Border).BorderDashed())
	}
	for i, script := range f.picked {
		row := fairy.Row(
			fairy.Row(
				fairy.Icon(lucide.GripVertical).Size(16, 16).Color(t.MutedForeground),
				fairy.Text(itoa(i+1)).FontSize(12).Medium().Color(t.MutedForeground).MinW(20),
			).Gap(8).Align(fairy.Center).Shrink(0),
			fixScriptIcon(script),
			fairy.Text(script.Name).FontSize(14).Medium().Ellipsis().Grow(1).MinW(0),
			kit.IconButton(lucide.X, func() {
				f.picked = slices.DeleteFunc(f.picked, func(p db.ScriptBasicRow) bool { return p.ID == script.ID })
			}, kit.Outline, kit.Label(T(prefix+"remove"))).Color(t.Destructive),
		).Key("tools.fix.picked."+script.ID).H(fixToolRowStep-8).PadXY(16, 0).Gap(12).Align(fairy.Center).
			Radius(6).Fill(t.Muted.Alpha(0.5)).HoverFill(t.Muted).Animate(motion.Fast).Cursor(fairy.CursorMove).
			OnDrag(func(e fairy.DragEvent) { s.dragFixScript(i, e) })
		if f.dragging && f.drag == i {
			row.Fill(t.Muted).Border(1, t.Accent)
		}
		list.Child(row)
	}

	save := kit.Button(T("g.save"), s.saveFixPreset, kit.Small, kit.Icon(lucide.Save), kit.Loading(f.saving),
		kit.Disabled(strings.TrimSpace(f.presetName) == "" || len(f.picked) == 0))
	header := fairy.Col(
		fixPaneHeader(T(prefix+"title"), T(prefix+"description"), save),
		fairy.Row(
			fairy.Text(T(prefix+"presetName")).FontSize(14).NoWrap(),
			kit.TextField(f.presetName, func(value string) { f.presetName = value },
				kit.Placeholder(T(prefix+"enterPresetName")), kit.Label(T(prefix+"presetName")),
				kit.Disabled(len(f.picked) == 0)).Key("tools.fix.preset-name").Grow(1).MinW(0),
		).Gap(16).Align(fairy.Center),
	).Gap(8).Pad(12).BorderEdges(0, 0, 1, 0, t.Border)
	return fairy.Col(header, fairy.Col(list).Grow(1).MinH(0).ScrollY())
}

// dragFixScript moves the dragged preset row to the slot under the pointer; a cancelled drag puts it back.
func (s *Shell) dragFixScript(i int, e fairy.DragEvent) {
	f := &s.tools.fixTool
	switch e.Phase {
	case fairy.DragStart:
		f.dragging, f.drag, f.dragFrom = true, i, i
	case fairy.DragMove:
		if !f.dragging || !f.dragValid() {
			f.dragging = false
			return
		}
		target := min(max(f.dragFrom+int(e.DY/fixToolRowStep+sign(e.DY)*0.5), 0), len(f.picked)-1)
		if target != f.drag {
			item := f.picked[f.drag]
			f.picked = slices.Insert(slices.Delete(f.picked, f.drag, f.drag+1), target, item)
			f.drag = target
		}
	case fairy.DragEnd:
		if f.dragging && f.dragValid() && e.Canceled && f.drag != f.dragFrom {
			item := f.picked[f.drag]
			f.picked = slices.Insert(slices.Delete(f.picked, f.drag, f.drag+1), f.dragFrom, item)
		}
		f.dragging = false
	}
}

// dragValid reports whether the dragged row and its start are still in the list, which a save can empty.
func (f *fixToolManager) dragValid() bool {
	return f.drag >= 0 && f.drag < len(f.picked) && f.dragFrom >= 0 && f.dragFrom < len(f.picked)
}

func sign(v float32) float32 {
	if v < 0 {
		return -1
	}
	return 1
}

func (s *Shell) saveFixPreset() {
	f := &s.tools.fixTool
	name := strings.TrimSpace(f.presetName)
	if f.saving || name == "" || len(f.picked) == 0 {
		return
	}
	ids := make([]string, 0, len(f.picked))
	for _, script := range f.picked {
		ids = append(ids, script.ID)
	}
	f.saving = true
	const prefix = "page.tools.fix-tool-manager.builder.right.#.handleSavePreset."
	s.background(func(ctx context.Context) {
		err := s.deps.Tools.CreatePreset(ctx, tools.CreateScriptPresetInput{Name: name, ScriptIDs: ids})
		exists := err != nil && strings.Contains(err.Error(), "already exists")
		if err != nil && !exists {
			err = s.toolError(err, "fix-tool.create-preset", map[string]any{"name": name, "scripts": ids})
		}
		fairy.Go(func() {
			f.saving = false
			switch {
			case exists:
				s.toasts.Show(kit.ToastWarning, T(prefix+"alreadyExists"), "")
			case err != nil:
				s.toasts.Show(kit.ToastError, T(prefix+"error"), err.Error())
			default:
				f.presetName, f.picked, f.dragging = "", nil, false
				f.presets.invalidate()
				s.toasts.Show(kit.ToastSuccess, T(prefix+"success"), "")
			}
		})
	})
}
