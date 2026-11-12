package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/modruntime"
	liquidemitter "nahida.live/desktop/internal/tools/liquid_emitter"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// liquidEmitterDialog shows the Liquid Emitter the mod screen opened, over the main window.
func (s *Shell) liquidEmitterDialog(cx *fairy.Cx) {
	lt := s.tools.liquidDialog
	if lt == nil {
		return
	}
	t := kit.T()
	lt.sync()
	title := lt.modName
	if title == "" {
		title = toolName(toolInfo{nameKey: liquidPrefix + "title", beta: true})
	}
	header := fairy.Row(
		fairy.Text(title).FontSize(16).LineHeight(24).SemiBold().Ellipsis().Grow(1).MinW(0),
		kit.IconButton(lucide.X, lt.requestClose, kit.Ghost, kit.Small).Label(T("g.close")),
	).Gap(8).Align(fairy.Center).Shrink(0)
	// A model that failed to load leaves nothing to edit, so the tool shows only the error, as React does.
	var body *fairy.El
	if err := lt.viewer.loadErr; err != "" {
		body = fairy.Col(xxmiAlert(lucide.TriangleAlert, true, T(liquidPrefix+"load_error"), err))
	} else {
		body = fairy.Box(lt.viewer.content(cx, *t, nil)).Radius(t.RadiusMd()).Border(1, t.Border).Clip()
	}
	panel := fairy.Col(header, body.Grow(1).MinH(0)).Gap(12).Pad(12).
		Fill(t.Background).Color(t.Foreground).Radius(t.RadiusXl()).Border(1, t.Border).Clip().
		Shadow(0, 20, 50, 0, shadowColor(t)).Role(fairy.RoleDialog).Label(title)
	showWindowDialog(cx, "mod.liquid-emitter", panel, lt.requestClose)
	lt.viewer.dialogs(cx)
	lt.question(cx)
}

func (lt *liquidTool) question(cx *fairy.Cx) {
	dismiss := func() { lt.ask = "" }
	props := kit.DialogProps{OnClose: dismiss, Width: 480, Key: "liquid-emitter." + lt.ask}
	cancel := kit.Button(T("g.cancel"), dismiss, kit.Outline)
	switch lt.ask {
	case "":
		return
	case "slots":
		next := lt.slotsNext
		props.Title = T(liquidPrefix + "slots.title")
		props.Description = T(liquidPrefix+"slots.description", "count", len(lt.emitters())+1, "current", lt.slotsNow,
			"next", next)
		props.Actions = []*fairy.El{
			cancel,
			kit.Button(T(liquidPrefix+"slots.keep"), func() {
				lt.ask = ""
				lt.applyAnchor()
			}, kit.Secondary),
			kit.Button(T(liquidPrefix+"slots.confirm", "next", next), func() {
				lt.ask = ""
				lt.raiseSlots(next)
			}),
		}
	case "remove-all":
		props.Title = T(liquidPrefix + "remove_all.title")
		props.Description = T(liquidPrefix + "remove_all.description")
		props.Actions = []*fairy.El{cancel, kit.Button(T(liquidPrefix+"remove_all.confirm"), func() {
			lt.ask = ""
			lt.remove(nil)
		})}
	case "restore":
		props.Title = T(liquidPrefix + "generated.restore_title")
		props.Description = T(liquidPrefix + "generated.restore_description")
		props.Actions = []*fairy.El{cancel, kit.Button(T(liquidPrefix+"generated.restore_confirm"), func() {
			lt.ask = ""
			lt.restore()
		}, kit.Destructive)}
	case "close":
		props.Title = T(liquidPrefix + "unsaved.title")
		props.Description = T(liquidPrefix + "unsaved.description")
		props.Actions = []*fairy.El{cancel, kit.Button(T(liquidPrefix+"unsaved.confirm"), func() {
			lt.ask = ""
			lt.shell.closeLiquidEmitterDialog()
		}, kit.Destructive)}
	}
	kit.ShowDialog(cx, props)
}

func (lt *liquidTool) mesh(id string) *modelviewer.ModelViewerMeshTransport {
	if lt.viewer.scene == nil {
		return nil
	}
	meshes := lt.viewer.scene.transport.Meshes
	for i := range meshes {
		if meshes[i].ID == id {
			return &meshes[i]
		}
	}
	return nil
}

// partLabel names a mesh by its override section; the draw number that ends the mesh ID tells apart the parts
// one section draws.
func (lt *liquidTool) partLabel(mesh *modelviewer.ModelViewerMeshTransport) string {
	for _, other := range lt.viewer.scene.transport.Meshes {
		if other.ID != mesh.ID && other.Component == mesh.Component && other.Form == mesh.Form {
			return mesh.Component + " #" + mesh.ID[strings.LastIndex(mesh.ID, ":")+1:]
		}
	}
	return mesh.Component
}

// drawn reports whether the game draws the mesh's part in the view: a frame animation shows one mesh per frame,
// all of them one draw, so a part counts as drawn while any mesh of its draw is visible.
func (lt *liquidTool) drawn(id string, visible map[string]bool) bool {
	if visible[id] {
		return true
	}
	mesh := lt.mesh(id)
	if mesh == nil || mesh.DrawKey == "" || lt.viewer.scene == nil {
		return false
	}
	for _, other := range lt.viewer.scene.transport.Meshes {
		if other.DrawKey == mesh.DrawKey && visible[other.ID] {
			return true
		}
	}
	return false
}

// anchorView is where the edited anchor stands in the view: inForm while the selected form has its part, and
// hidden when a toggle swapped that part for another.
func (lt *liquidTool) anchorView(visible map[string]bool) (inForm, hidden bool) {
	anchor := lt.anchor()
	if anchor == nil || lt.viewer.scene == nil {
		return false, false
	}
	form := ""
	if mesh := lt.mesh(anchor.meshID); mesh != nil {
		form = mesh.Form
	}
	inForm = formVisible(form, lt.viewer.form)
	return inForm, inForm && !lt.drawn(anchor.meshID, visible)
}

func (lt *liquidTool) inspector() *fairy.El {
	t := kit.T()
	picks := lt.picking && lt.editable()
	pickVariant := kit.Outline
	if picks {
		pickVariant = kit.Default
	}
	header := fairy.Row(
		fairy.Text(T(liquidPrefix+"title")).FontSize(14).LineHeight(20).Medium().Grow(1).MinW(0).Ellipsis(),
		kit.Button(T(liquidPrefix+"pick.toggle"), func() { lt.picking = !lt.picking }, pickVariant, kit.Small,
			kit.Icon(lucide.Crosshair), kit.IconInline, kit.Disabled(!lt.editable())),
	).Gap(8).Align(fairy.Center).PadXY(16, 12)

	sections := []*fairy.El{header}
	if runtime := lt.runtimeSection(); runtime != nil {
		sections = append(sections, runtime)
	}
	sections = append(sections, lt.emitterSection(), lt.offsetSection(), lt.directionSection(),
		lt.positionSection())
	if look := lt.lookSection(); look != nil {
		sections = append(sections, look)
	}
	if mode := lt.modeSection(); mode != nil {
		sections = append(sections, mode)
	}
	card := fairy.Col().Radius(6).Fill(t.Muted.Alpha(0.3))
	for i, section := range sections {
		if i > 0 {
			section.BorderEdges(1, 0, 0, 0, t.Border)
		}
		card.Child(section)
	}
	return card
}

// section is a titled block of the inspector that folds away, as the React InspectorSection.
func (lt *liquidTool) section(id, title, summary string, actions *fairy.El, body func() *fairy.El) *fairy.El {
	t := kit.T()
	open := lt.sections[id]
	chevron := lucide.ChevronRight
	if open {
		chevron = lucide.ChevronDown
	}
	trigger := fairy.Row(
		fairy.Icon(chevron).Size(16, 16).Color(t.MutedForeground).Shrink(0),
		fairy.Text(title).FontSize(14).LineHeight(20).Medium().Grow(1).MinW(0).Ellipsis().TooltipWhenTruncated(),
	).Gap(6).Align(fairy.Center).Grow(1).MinW(0).Role(fairy.RoleButton).Label(title).Expanded(open).Focusable().
		Cursor(fairy.CursorPointer).OnClick(func() { lt.sections[id] = !open })
	if !open && summary != "" {
		trigger.Child(fairy.Text(summary).FontSize(12).LineHeight(16).Color(t.MutedForeground).
			MinW(0).Ellipsis().PadEdges(0, 0, 0, 8))
	}
	row := fairy.Row(trigger).Gap(8).Align(fairy.Center)
	if actions != nil {
		row.Child(actions.Shrink(0))
	}
	col := fairy.Col(row).Key("liquid-emitter."+id).PadXY(16, 12)
	if open {
		col.Child(body().MarginEdges(12, 0, 0, 0))
	}
	return col
}

func liquidHint(text string) *fairy.El {
	return fairy.Text(text).FontSize(12).LineHeight(16).Color(kit.T().MutedForeground).Wrap()
}

// liquidRange is a labelled slider with its value shown on the right, as the React RangeField.
func liquidRange(label, display string, value, minimum, maximum, step float64, disabled bool,
	set func(float64),
) *fairy.El {
	t := kit.T()
	return fairy.Col(
		fairy.Row(
			fairy.Text(label).FontSize(14).LineHeight(19.25).Medium().Grow(1).MinW(0),
			fairy.Text(display).FontSize(11).LineHeight(16.5).Color(t.MutedForeground),
		).Gap(8).Align(fairy.Center),
		kit.Slider(value, minimum, maximum, step, set, kit.Disabled(disabled), kit.Label(label)),
	).Gap(6)
}

// liquidChoice is a row of buttons of which the selected one is filled.
func liquidChoice(values []string, selected string, label func(string) string, disabled bool,
	set func(string),
) *fairy.El {
	row := fairy.Row().Gap(4)
	for _, value := range values {
		variant := kit.Outline
		if value == selected {
			variant = kit.Default
		}
		row.Child(kit.Button(label(value), func() { set(value) }, variant, kit.Small, kit.Disabled(disabled)).
			Grow(1).Basis(0).MinW(0))
	}
	return row
}

func liquidCheck(checked, disabled bool, label, hint string, set func(bool)) *fairy.El {
	t := kit.T()
	text := fairy.Col(fairy.Text(label).FontSize(14).LineHeight(20)).Gap(2).Grow(1).MinW(0)
	if hint != "" {
		text.Child(fairy.Text(hint).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	row := fairy.Row(kit.Checkbox(checked, set, kit.Disabled(disabled)).Shrink(0), text).Gap(8).Align(fairy.Start)
	if disabled {
		return row.Opacity(0.6)
	}
	return row.Cursor(fairy.CursorPointer).OnClick(func() { set(!checked) })
}

func (lt *liquidTool) runtimeSection() *fairy.El {
	t := kit.T()
	problem := lt.inspectErr != nil
	state := ""
	if lt.inspection != nil {
		state = lt.inspection.Runtime.State
	}
	// An installed runtime needs nothing from the user.
	if !problem && (state == "" || state == modruntime.StateCurrent) {
		return nil
	}
	required := state == modruntime.StateMissing || state == modruntime.StateDisabled
	actionable := !problem && (required || state == modruntime.StateOutdated || state == modruntime.StateModified)

	row := fairy.Row(fairy.Text(T(liquidPrefix + "runtime.title")).FontSize(13).Medium().Grow(1).MinW(0).Ellipsis()).
		Gap(8).Align(fairy.Center)
	if !problem {
		row.Child(kit.Badge(T(liquidPrefix+"runtime.state."+state), kit.WithTone(kit.ToneDestructive)))
	}
	if actionable {
		variant := kit.Outline
		if required {
			variant = kit.Default
		}
		row.Child(kit.Button(T(liquidPrefix+"runtime.action."+state), lt.install, variant, kit.XS,
			kit.Disabled(lt.busy != ""), kit.Loading(lt.busy == "install-runtime")))
	}
	col := fairy.Col(row).Gap(8).PadXY(16, 12)

	description, color := "", t.MutedForeground
	switch {
	case problem:
		description, color = liquidErrorMessage(lt.inspectErr), t.Destructive
	case state == modruntime.StateForeign:
		description, color = T(liquidPrefix+"runtime.foreign_description", "path", lt.inspection.Runtime.Path),
			t.Destructive
	case required:
		description = T(liquidPrefix + "runtime.required." + state + ".description")
	}
	if description != "" {
		col.Child(fairy.Text(description).FontSize(12).Color(color).Wrap())
	}
	return col
}

func (lt *liquidTool) emitterSection() *fairy.El {
	t := kit.T()
	emitters := lt.emitters()
	current := lt.current()
	adding := current == nil
	anchor := lt.anchor()
	busy := lt.busy != ""
	editable := lt.editable()
	maxEmitters := "-"
	if lt.inspection != nil {
		maxEmitters = strconv.Itoa(lt.inspection.MaxEmitters)
	}
	summary := T(liquidPrefix + "selection.adding")
	if !adding {
		summary = "#" + strconv.Itoa(current.PID)
	}

	icon := func(vector *svg.Vector, key string, disabled bool, onClick func()) *fairy.El {
		label := T(liquidPrefix + key)
		return kit.IconButton(vector, onClick, kit.Ghost, kit.XS, kit.Tooltip(label), kit.Disabled(disabled)).
			Label(label)
	}
	actions := fairy.Row(
		icon(lucide.Plus, "selection.new", !editable || busy || lt.atLimit() || (adding && anchor == nil),
			func() { lt.selectEmitter(nil, nil) }),
		icon(lucide.Undo2, "history.undo", !editable || lt.history.index == 0, func() { lt.step(-1) }),
		icon(lucide.Redo2, "history.redo", !editable || lt.history.index >= len(lt.history.entries)-1,
			func() { lt.step(1) }),
	).Gap(2).Align(fairy.Center)
	if len(emitters) > 0 {
		actions.Child(kit.Button(T(liquidPrefix+"remove_all.button"), func() { lt.ask = "remove-all" }, kit.Ghost,
			kit.XS, kit.Disabled(busy)))
	}

	title := T(liquidPrefix+"emitters.title", "count", len(emitters), "max", maxEmitters)
	return lt.section("emitters", title, summary, actions, func() *fairy.El {
		list := fairy.Col().Gap(4)
		editingLabel, editingForm := "", ""
		if anchor != nil {
			editingLabel = anchor.meshID
			if mesh := lt.mesh(anchor.meshID); mesh != nil {
				editingLabel, editingForm = lt.partLabel(mesh), mesh.Form
			}
		}
		unsaved := lt.unsaved()
		for _, emitter := range emitters {
			list.Child(lt.emitterRow(emitter, current, editingLabel, editingForm, anchor != nil, unsaved))
		}
		if adding {
			detail := T(liquidPrefix + "selection.empty")
			if anchor != nil {
				detail = editingLabel + " · " + T(liquidPrefix+"selection.triangle", "index", anchor.faceIndex)
			}
			name := fairy.Row(fairy.Text(T(liquidPrefix + "selection.adding")).FontSize(13).Ellipsis().MinW(0)).
				Gap(8).Align(fairy.Center)
			if badge := lt.formBadge(editingForm); badge != nil {
				name.Child(badge)
			}
			list.Child(fairy.Col(name, fairy.Text(detail).FontSize(12).Color(t.MutedForeground).Ellipsis()).Gap(2).
				PadXY(8, 6).Radius(6).Fill(t.Muted))
		}

		body := fairy.Col(list).Gap(8)
		if _, hidden := lt.anchorView(lt.viewer.visibleMeshes()); hidden {
			body.Child(fairy.Row(
				fairy.Icon(lucide.TriangleAlert).Size(14, 14).Color(t.Warning).Shrink(0),
				fairy.Text(T(liquidPrefix+"selection.hidden")).FontSize(12).Color(t.Warning).Wrap().Grow(1).MinW(0),
			).Gap(6).Align(fairy.Start))
		}
		if adding && lt.atLimit() {
			body.Child(liquidHint(T(liquidPrefix+"selection.limit", "max", lt.inspection.MaxEmitters)))
		}
		if lt.notice != "" {
			body.Child(fairy.Text(lt.notice).FontSize(12).Color(t.Destructive).Wrap())
		}
		return body
	})
}

func (lt *liquidTool) emitterRow(
	emitter liquidemitter.Emitter,
	current *liquidemitter.Emitter,
	editingLabel, editingForm string,
	editing, unsaved bool,
) *fairy.El {
	t := kit.T()
	busy := lt.busy != ""
	selected := current != nil && current.PID == emitter.PID
	mesh := lt.mesh(emitter.Anchor.MeshID)
	label, form := emitter.Section, liquidINIFolder(emitter.INIPath)
	if mesh != nil {
		label, form = lt.partLabel(mesh), mesh.Form
	}
	if selected && editing {
		label, form = editingLabel, editingForm
	}
	lookKey := "emitters.look_custom"
	if liquidFollowsRuntime(emitter.Look) {
		lookKey = "emitters.look_runtime"
	}

	name := fairy.Row(fairy.Text(label).FontSize(13).Ellipsis().MinW(0)).Gap(8).Align(fairy.Center)
	if badge := lt.formBadge(form); badge != nil {
		name.Child(badge)
	}
	detail := fmt.Sprintf("%s · #%d · %s", emitter.INIPath, emitter.PID, T(liquidPrefix+lookKey))
	text := fairy.Col(name, fairy.Text(detail).FontSize(12).Color(t.MutedForeground).Ellipsis()).Gap(2).Grow(1).MinW(0)
	if mesh == nil || busy {
		text.Opacity(0.6)
		if mesh == nil {
			text.Tooltip(T(liquidPrefix + "emitters.missing_mesh"))
		}
	} else {
		text.Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Label(label).OnClick(func() {
			saved := liquidAnchorFromEmitter(emitter)
			if saved == nil {
				return
			}
			pid := emitter.PID
			lt.selectEmitter(&pid, saved)
			lt.showForm(mesh.Form)
		})
	}

	row := fairy.Row(text).Key(fmt.Sprintf("liquid-emitter.emitter.%d", emitter.PID)).Gap(8).Align(fairy.Center).
		PadXY(8, 6).Radius(6).HoverFill(hoverFill(t))
	if selected {
		row.Fill(t.Muted)
		if unsaved {
			row.Child(kit.Badge(T(liquidPrefix+"emitters.unsaved"), kit.Outline))
		}
	}
	if !emitter.Hooked {
		row.Child(kit.Badge(T(liquidPrefix+"emitters.unhooked"), kit.WithTone(kit.ToneDestructive)))
	}
	remove := T(liquidPrefix + "remove")
	return row.Child(kit.IconButton(lucide.Trash, func() { lt.remove([]int{emitter.PID}) }, kit.Ghost, kit.XS,
		kit.Tooltip(remove), kit.Disabled(busy)).Label(remove))
}

// formBadge names the form of a bundled mod an emitter belongs to; nothing for a single mod.
func (lt *liquidTool) formBadge(form string) *fairy.El {
	if lt.viewer.scene == nil || form == "" {
		return nil
	}
	forms := lt.viewer.scene.transport.Forms
	if len(forms) < 2 {
		return nil
	}
	for _, entry := range forms {
		if entry.ID == form {
			return kit.Badge(entry.Label, kit.Secondary).MaxW(128).Shrink(0)
		}
	}
	return nil
}

// liquidINIFolder is the folder of a mod-relative INI path, as the viewer names forms.
func liquidINIFolder(iniPath string) string {
	slash := strings.LastIndex(iniPath, "/")
	if slash < 0 {
		return "."
	}
	return iniPath[:slash]
}

func (lt *liquidTool) offsetSection() *fairy.El {
	anchor := lt.anchor()
	locked := lt.locked()
	unit := lt.meshUnit()
	summary := ""
	if anchor != nil {
		parts := make([]string, 3)
		for axis, value := range anchor.offset {
			parts[axis] = strconv.FormatFloat(value*unit*1000, 'f', 1, 64)
		}
		summary = strings.Join(parts, " · ")
	}
	return lt.section("offset", T(liquidPrefix+"offset.title"), summary, nil, func() *fairy.El {
		body := fairy.Col().Gap(12)
		for axis, label := range liquidAxes {
			millimeters := 0.0
			if anchor != nil {
				millimeters = anchor.offset[axis] * unit * 1000
			}
			body.Child(liquidRange(label, strconv.FormatFloat(millimeters, 'f', 1, 64)+" mm", millimeters,
				-liquidMaxOffsetMeters*1000, liquidMaxOffsetMeters*1000, 0.5, locked, func(value float64) {
					lt.setOffsetAxis(axis, value/1000, fmt.Sprintf("offset-%d", axis))
					delete(lt.positionDrafts, axis)
				}))
		}
		return body.Child(kit.Button(T(liquidPrefix+"offset.reset"), func() {
			clear(lt.positionDrafts)
			lt.update("offset-reset", func(a *liquidAnchor) { a.offset = vec3{} })
		}, kit.Outline, kit.Small, kit.Disabled(locked)).WFull())
	})
}

func (lt *liquidTool) directionSection() *fairy.El {
	angles, _, ok := lt.angles()
	locked := lt.locked()
	summary := ""
	if ok {
		summary = T(liquidPrefix + "direction.summary_down")
		if angles.tilt >= liquidPoleDegrees {
			summary = fmt.Sprintf("%.0f° · %.0f°", angles.tilt, angles.azimuth)
		}
	}
	return lt.section("direction", T(liquidPrefix+"direction.title"), summary, nil, func() *fairy.El {
		return fairy.Col(
			liquidRange(T(liquidPrefix+"direction.tilt"), fmt.Sprintf("%.0f°", angles.tilt), angles.tilt, 0, 180, 1,
				locked, lt.setTilt),
			liquidRange(T(liquidPrefix+"direction.azimuth"), fmt.Sprintf("%.0f°", angles.azimuth), angles.azimuth, 0,
				359, 1, locked, lt.setAzimuth),
			liquidHint(T(liquidPrefix+"direction.hint")),
			kit.Button(T(liquidPrefix+"direction.down"), func() {
				down := lt.down()
				lt.update("direction-down", func(a *liquidAnchor) { a.direction = down })
			}, kit.Outline, kit.Small, kit.Disabled(locked)).WFull(),
			liquidCheck(lt.showTrajectory, false, T(liquidPrefix+"trajectory.label"),
				T(liquidPrefix+"trajectory.hint"), func(on bool) { lt.showTrajectory = on }),
		).Gap(12)
	})
}

func (lt *liquidTool) positionSection() *fairy.El {
	anchor := lt.anchor()
	locked := lt.locked()
	down := lt.down()
	upAxis := ""
	for axis, value := range down {
		if value != 0 {
			upAxis = liquidAxes[axis]
			break
		}
	}
	return lt.section("position", T(liquidPrefix+"position.title"), "", nil, func() *fairy.El {
		t := kit.T()
		fields := fairy.Row().Gap(8)
		for axis, label := range liquidAxes {
			text, drafting := lt.positionDrafts[axis]
			if !drafting && anchor != nil {
				text = strconv.FormatFloat(math.Round((anchor.point[axis]+anchor.offset[axis])*1e4)/1e4, 'f', -1, 64)
			}
			field := kit.TextField(text, func(next string) {
				lt.positionDrafts[axis] = next
				value, err := strconv.ParseFloat(strings.TrimSpace(next), 64)
				if current := lt.anchor(); current != nil && err == nil && !math.IsInf(value, 0) {
					lt.setOffsetAxis(axis, (value-current.point[axis])*lt.meshUnit(), fmt.Sprintf("position-%d", axis))
				}
			}, kit.Disabled(locked), kit.Label(label)).Key(fmt.Sprintf("liquid-emitter.position.%d", axis)).
				OnBlur(func() { delete(lt.positionDrafts, axis) })
			fields.Child(fairy.Col(fairy.Text(label).FontSize(12).Color(t.MutedForeground), field).Gap(4).Grow(1).
				Basis(0).MinW(0))
		}
		return fairy.Col(fields, liquidHint(T(liquidPrefix+"position.hint", "axis", upAxis))).Gap(8)
	})
}

func (lt *liquidTool) modeSection() *fairy.El {
	inspection := lt.inspection
	if inspection == nil {
		return nil
	}
	busy := lt.busy != ""
	t := kit.T()

	// A generated mod has no mode to choose but must keep its way back visible.
	if inspection.SourcePath != "" {
		return fairy.Box(fairy.Col(
			fairy.Text(T(liquidPrefix+"generated.title")).FontSize(13).Medium(),
			fairy.Text(T(liquidPrefix+"generated.description", "path", inspection.SourcePath)).FontSize(12).
				Color(t.MutedForeground).Wrap(),
			kit.Button(T(liquidPrefix+"generated.restore"), func() { lt.ask = "restore" }, kit.Outline, kit.Small,
				kit.Disabled(busy)).WFull(),
		).Gap(8).PadXY(12, 8).Radius(6).Border(1, t.Border).Fill(t.Background.Alpha(0.4))).Pad(16)
	}

	return lt.section("mode", T(liquidPrefix+"mode.title"), T(liquidPrefix+"mode."+lt.mode), nil, func() *fairy.El {
		return fairy.Col(
			liquidChoice([]string{liquidemitter.ApplyInPlace, liquidemitter.ApplyNewMod}, lt.mode,
				func(mode string) string { return T(liquidPrefix + "mode." + mode) }, busy || !lt.editable(),
				func(mode string) { lt.mode = mode }),
			liquidHint(T(liquidPrefix+"mode."+lt.mode+"_hint")),
		).Gap(8)
	})
}

func (lt *liquidTool) footer() *fairy.El {
	current := lt.current()
	busy := lt.busy != ""
	mode := lt.applyMode()
	// A new mod and an emitter whose hook went missing are worth applying unchanged.
	applicable := lt.unsaved() || mode == liquidemitter.ApplyNewMod || current != nil && !current.Hooked
	row := fairy.Row().Gap(8)
	if current != nil {
		pid := current.PID
		row.Child(kit.Button(T(liquidPrefix+"remove"), func() { lt.remove([]int{pid}) }, kit.Outline,
			kit.Disabled(busy)).Grow(1).Basis(0))
	}
	label := "apply"
	switch {
	case mode == liquidemitter.ApplyNewMod:
		label = "mode.new_mod"
	case current == nil:
		label = "apply_new"
	}
	pending := lt.busy == "apply" || lt.busy == "raise-slots" || lt.busy == "set-look"
	return row.Child(kit.Button(T(liquidPrefix+label), lt.handleApply,
		kit.Disabled(lt.locked() || busy || !applicable || current == nil && lt.atLimit()), kit.Loading(pending)).
		Grow(1).Basis(0))
}
