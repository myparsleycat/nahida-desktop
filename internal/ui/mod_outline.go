package ui

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/tools"
	efmioutline "nahida.live/desktop/internal/tools/efmi_outline"
)

// outlineDraft is the EFMI outline fix of one mod: the inspected INI files and the parts the user checks.
// The selection belongs to the inspection it was made from, so a new inspection starts from the files again.
type outlineDraft struct {
	modPath, modName string
	loading          bool
	err              error
	inspection       *tools.EFMIOutlineInspection
	selection        map[string][]string
	busy             bool
	adopt            string
	manualOpen       map[string]bool
}

func (s *Shell) isEfmiGame() bool {
	game := s.gameConfig()
	return game != nil && strings.EqualFold(deref(game.Importer), "EFMI")
}

func (s *Shell) openOutline(path, name string) {
	d := &outlineDraft{modPath: path, modName: name, manualOpen: map[string]bool{}}
	s.mods.outline = d
	s.inspectOutline(d)
}

func (s *Shell) inspectOutline(d *outlineDraft) {
	d.loading = true
	s.background(func(ctx context.Context) {
		inspection, err := s.deps.Tools.EFMIOutlineInspect(ctx, d.modPath)
		fairy.Go(func() {
			d.loading = false
			if err != nil {
				s.modError(err, "efmi-outline-inspect", map[string]any{"path": d.modPath})
				d.err = err
				return
			}
			d.setInspection(inspection)
		})
	})
}

func (d *outlineDraft) setInspection(inspection tools.EFMIOutlineInspection) {
	d.inspection, d.err = &inspection, nil
	d.selection = map[string][]string{}
	for _, file := range inspection.Files {
		d.selection[file.Path] = appliedParts(file)
	}
}

func appliedParts(file efmioutline.OutlineFile) []string {
	ids := []string{}
	for _, part := range file.Parts {
		if part.Applied {
			ids = append(ids, part.ID)
		}
	}
	return ids
}

func outlineEditable(file efmioutline.OutlineFile) bool {
	return file.State == efmioutline.StateOriginal || file.State == efmioutline.StateApplied
}

func sameIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for _, id := range left {
		if !slices.Contains(right, id) {
			return false
		}
	}
	return true
}

func (d *outlineDraft) dirty() bool {
	if d.inspection == nil {
		return false
	}
	for _, file := range d.inspection.Files {
		if outlineEditable(file) && !sameIDs(d.selection[file.Path], appliedParts(file)) {
			return true
		}
	}
	return false
}

// runOutline applies, reverts or adopts and shows the inspection the action returns. A failed action inspects
// the mod again, since the files may have changed.
func (s *Shell) runOutline(d *outlineDraft, operation, success string,
	action func(context.Context) (tools.EFMIOutlineInspection, error),
) {
	if d.busy || d.loading {
		return
	}
	d.busy = true
	s.background(func(ctx context.Context) {
		inspection, err := action(ctx)
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				s.modError(err, operation, map[string]any{"path": d.modPath})
				s.toasts.Show(kit.ToastError, outlineErrorMessage(err), "")
				s.inspectOutline(d)
				return
			}
			d.setInspection(inspection)
			s.toasts.Show(kit.ToastSuccess, T(success), "")
		})
	})
}

func (s *Shell) applyOutline(d *outlineDraft) {
	input := tools.EFMIOutlineApplyInput{ModPath: d.modPath}
	for _, file := range d.inspection.Files {
		if outlineEditable(file) {
			input.Files = append(input.Files, efmioutline.OutlineSelection{
				Path: file.Path, PartIDs: slices.Clone(d.selection[file.Path]),
			})
		}
	}
	s.runOutline(d, "efmi-outline-apply", "page.tools.efmi_outline.applied",
		func(ctx context.Context) (tools.EFMIOutlineInspection, error) {
			return s.deps.Tools.EFMIOutlineApply(ctx, input)
		})
}

var outlineErrorCode = regexp.MustCompile(`EFMI_OUTLINE_([A-Z_]+):([^\n]*)`)

func outlineErrorMessage(err error) string {
	message := err.Error()
	match := outlineErrorCode.FindStringSubmatch(message)
	if match == nil {
		return message
	}
	file := strings.TrimSpace(match[2])
	const prefix = "page.tools.efmi_outline.error."
	switch match[1] {
	case "NOT_EFMI":
		return T(prefix + "not_efmi")
	case "MODIFIED":
		return T(prefix+"modified", "file", file)
	case "STALE":
		return T(prefix + "stale")
	case "UNSUPPORTED":
		return T(prefix+"unsupported", "file", file)
	}
	return message
}

func (s *Shell) outlineDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.outline
	if d == nil {
		return
	}
	t := kit.T()
	const prefix = "page.tools.efmi_outline."
	body := fairy.Col(
		fairy.Text(d.modName).FontSize(14).Color(t.MutedForeground).Ellipsis(),
	).Gap(16)
	switch {
	case d.loading && d.inspection == nil:
		body.Child(fairy.Box(kit.Spinner(20)).Center())
	case d.err != nil:
		body.Child(xxmiAlert(lucide.TriangleAlert, true, "", outlineErrorMessage(d.err)))
	case d.inspection != nil && len(d.inspection.Files) == 0:
		body.Child(fairy.Text(T(prefix + "empty")).FontSize(14))
	}
	if d.inspection != nil {
		files := fairy.Col().Gap(16)
		for _, file := range d.inspection.Files {
			files.Child(s.outlineFileCard(d, file))
		}
		body.Child(fairy.Col(files).MaxH(480).ScrollY())
	}

	close := func() { m.outline = nil }
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + "title"),
		Description: T(prefix + "description"),
		Body:        body,
		Width:       672,
		Actions: []*fairy.El{
			kit.Button(T("g.close"), close, kit.Outline),
			kit.Button(T("g.apply"), func() { s.applyOutline(d) }, kit.Loading(d.busy),
				kit.Disabled(d.loading || !d.dirty())),
		},
		OnClose: close,
		Key:     "mod.efmi-outline",
	})

	if d.adopt != "" {
		file := d.adopt
		dismiss := func() { d.adopt = "" }
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(prefix + "modified.confirm_title"),
			Alert:       true,
			Description: T(prefix + "modified.confirm_description"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline),
				kit.Button(T(prefix+"modified.adopt"), func() {
					d.adopt = ""
					s.runOutline(d, "efmi-outline-adopt", prefix+"adopted",
						func(ctx context.Context) (tools.EFMIOutlineInspection, error) {
							return s.deps.Tools.EFMIOutlineAdopt(ctx, d.modPath, file)
						})
				}, kit.Destructive),
			},
			OnClose: dismiss,
			Key:     "mod.efmi-outline-adopt",
		})
	}
}

func (s *Shell) outlineFileCard(d *outlineDraft, file efmioutline.OutlineFile) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.efmi_outline."
	editable := outlineEditable(file)
	var variant kit.Option
	switch file.State {
	case efmioutline.StateApplied:
		variant = kit.Default
	case efmioutline.StateModified, efmioutline.StateUnsupported:
		variant = kit.Destructive
	default:
		variant = kit.Secondary
	}
	head := fairy.Row(
		fairy.Text(file.Path).FontSize(14).Medium().Ellipsis().Grow(1).MinW(0),
		kit.Badge(T(prefix+"state."+string(file.State)), variant),
	).Gap(8).Align(fairy.Center)
	if file.HasBackup && editable {
		path := file.Path
		head.Child(kit.Button(T(prefix+"revert"), func() {
			s.runOutline(d, "efmi-outline-revert", prefix+"reverted",
				func(ctx context.Context) (tools.EFMIOutlineInspection, error) {
					return s.deps.Tools.EFMIOutlineRevert(ctx, d.modPath, path)
				})
		}, kit.Outline, kit.Small, kit.Disabled(d.busy || d.loading)))
	}
	card := fairy.Col(head).Gap(4)

	if file.State == efmioutline.StateModified {
		path := file.Path
		card.Child(xxmiAlert(lucide.TriangleAlert, true, T(prefix+"modified.title"), T(prefix+"modified.description"),
			kit.Button(T(prefix+"modified.adopt"), func() { d.adopt = path }, kit.Outline, kit.Small,
				kit.Disabled(d.busy || d.loading))))
	}

	for _, part := range file.Parts {
		id, path := part.ID, file.Path
		checked := slices.Contains(d.selection[path], id)
		lines := make([]string, len(part.Lines))
		for i, line := range part.Lines {
			lines[i] = itoa(line)
		}
		text := fairy.Col(
			fairy.Text(part.Label).FontSize(14).Medium().Ellipsis(),
			fairy.Text(strings.Join(part.Sections, ", ")+" · "+T(prefix+"lines", "lines", strings.Join(lines, ", "))).
				FontSize(12).Color(t.MutedForeground).Ellipsis(),
		).Gap(2).Grow(1).MinW(0)
		if part.Locked {
			text.Child(fairy.Text(T(prefix + "locked")).FontSize(12).Color(t.MutedForeground))
		}
		toggle := func(on bool) {
			ids := slices.DeleteFunc(slices.Clone(d.selection[path]), func(v string) bool { return v == id })
			if on {
				ids = append(ids, id)
			}
			d.selection[path] = ids
		}
		card.Child(fairy.Row(
			kit.Checkbox(checked, toggle, kit.Disabled(d.busy || !editable || part.Locked)).Label(part.Label),
			text,
		).Gap(8).Align(fairy.Center).PadXY(12, 8).Radius(6).HoverFill(hoverFill(t)).Animate(motion.Fast))
	}

	if n := len(file.Manual); n > 0 {
		path := file.Path
		open := d.manualOpen[path]
		card.Child(fairy.Row(
			fairy.Icon(lucide.ChevronRight).Size(12, 12).Rotate(pickAngle(open)).ScaleMotion(motion.Fast),
			fairy.Text(T(prefix+"manual.title", "count", n)),
		).Gap(6).Align(fairy.Center).PadXY(12, 4).FontSize(14).Color(t.MutedForeground).Cursor(fairy.CursorPointer).
			Role(fairy.RoleButton).Expanded(open).OnClick(func() { d.manualOpen[path] = !open }))
		if open {
			items := fairy.Col().Gap(4).PadXY(12, 0).FontSize(12)
			for _, item := range file.Manual {
				items.Child(fairy.RichText(
					fairy.Span{Text: fmt.Sprintf("[%s] %d: %s", item.Section, item.Line, item.Text), Font: monoFont},
					fairy.Span{Text: " · " + T(prefix+"manual."+item.Reason), Color: t.MutedForeground},
				).Wrap())
			}
			card.Child(items)
		}
	}
	return card
}
