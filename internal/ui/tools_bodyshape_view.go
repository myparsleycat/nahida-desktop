package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"
	"github.com/samber/lo"

	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/tools/body_shape/sculpt"
)

const bodyShapePrefix = "page.tools.body_shape."

// page is the editor on the tools screen.
func (e *bodyShapeEditor) page(cx *fairy.Cx) *fairy.El {
	return e.content(cx)
}

// bodyShapeDialog shows the editor the mod screen opened, over the main window.
func (s *Shell) bodyShapeDialog(cx *fairy.Cx) {
	e := s.tools.bodyShapeDialog
	if e == nil {
		return
	}
	t := kit.T()
	title := e.modName
	if title == "" {
		title = toolName(toolInfo{nameKey: bodyShapePrefix + "title", beta: true})
	}
	header := fairy.Row(
		fairy.Text(title).SemiBold().FontSize(16).LineHeight(24).Grow(1).MinW(0).Ellipsis(),
		kit.IconButton(lucide.X, s.closeBodyShapeDialog, kit.Ghost, kit.Small).Label(T("g.close")),
	).Gap(8).Align(fairy.Center).Shrink(0)
	panel := fairy.Col(header, e.content(cx).Grow(1).MinH(0)).Gap(12).Pad(12).Radius(t.RadiusXl()).
		Border(1, t.Border).Fill(t.Background).Clip().Shadow(0, 20, 50, 0, shadowColor(t)).Role(fairy.RoleDialog).
		Label(title)
	showWindowDialog(cx, "mod.body-shape", panel, s.closeBodyShapeDialog)
}

func (e *bodyShapeEditor) key(name string) string { return "body-shape." + e.id + "." + name }

func (e *bodyShapeEditor) content(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	width, _ := cx.Size()
	page := fairy.Col().Grow(1).MinH(0).WFull().Gap(12)
	if e.mesh != nil {
		page.Child(fairy.Row(e.menuBar()).Shrink(0))
	}
	page.Child(toolsViewportSplit(e.viewport(t), e.sidePanel(t), width < toolsWideViewport))
	return page
}

func (e *bodyShapeEditor) menuBar() *fairy.El {
	menus := []kit.MenubarMenu{
		{Label: T("page.tools.model_viewer.menu.model"), Items: func() []kit.MenuItem {
			items := []kit.MenuItem{{Heading: true, Label: T("page.tools.model_viewer.menu.rotate")}}
			for _, rotation := range viewerRotations {
				items = append(items, kit.MenuItem{
					Label:   T("page.tools.model_viewer.rotate_actions." + rotation.label),
					OnClick: func() { e.rotate(rotation.delta) },
				})
			}
			return append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
				Label: T("page.tools.model_viewer.menu.reset"), Icon: lucide.RotateCcw, OnClick: e.resetView,
			})
		}},
		{Label: T(bodyShapePrefix + "brush_menu"), Icon: lucide.Brush, Items: e.brushMenu},
	}
	return kit.Menubar(e.key("menu"), menus)
}

func (e *bodyShapeEditor) brushMenu() []kit.MenuItem {
	mode := func(label string, value sculpt.BrushMode) kit.MenuItem {
		return kit.MenuItem{Label: T(bodyShapePrefix + label), Radio: true, Checked: e.brush.Mode == value,
			OnClick: func() { e.brush.Mode = value }}
	}
	return []kit.MenuItem{
		{Label: T(bodyShapePrefix + "brush_enable"), Checkbox: true, Checked: e.brushOn, OnClick: func() {
			e.brushOn = !e.brushOn
			if !e.brushOn {
				e.view.setRing(nil)
			}
		}},
		{Separator: true},
		{Heading: true, Label: T(bodyShapePrefix + "brush_mode")},
		mode("brush_mode_paint", sculpt.BrushPaint),
		mode("brush_mode_erase", sculpt.BrushErase),
		{Separator: true},
		{Label: T(bodyShapePrefix + "brush_mirror_x"), Checkbox: true, Checked: e.brush.MirrorX,
			OnClick: func() { e.brush.MirrorX = !e.brush.MirrorX }},
		{Separator: true},
		{Heading: true, Label: T(bodyShapePrefix+"brush_radius") + " (" + formatViewerNumber(e.brush.Radius) + ")"},
		{Content: e.numberField("radius", e.brush.Radius, e.setRadius)},
		{Heading: true, Label: T(bodyShapePrefix+"brush_strength") + " (" + formatViewerNumber(e.brush.Strength) + ")"},
		{Content: e.numberField("strength", e.brush.Strength, func(value float64) {
			e.brush.Strength = min(value, 1)
		})},
		{Separator: true},
		{Label: T(bodyShapePrefix + "brush_reset"), Icon: lucide.RotateCcw, Disabled: !e.hasSelection,
			OnClick: e.clearSelection},
	}
}

// numberField edits a positive brush number, applying each valid value as it is typed.
func (e *bodyShapeEditor) numberField(name string, value float64, set func(float64)) *fairy.El {
	text, ok := e.drafts[name]
	if !ok {
		text = formatViewerNumber(value)
	}
	return kit.TextField(text, func(next string) {
		e.drafts[name] = next
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(next), 64); err == nil && parsed > 0 {
			set(parsed)
		}
	}).Key(e.key("brush." + name)).OnBlur(func() { delete(e.drafts, name) })
}

func (e *bodyShapeEditor) viewport(t *kit.Theme) *fairy.El {
	box := fairy.Box().MinW(0).MinH(320).Radius(6).Clip().Fill(t.Muted.Alpha(0.3))
	if e.mesh == nil {
		message := T(bodyShapePrefix + "empty")
		if e.loading {
			message = T(bodyShapePrefix + "loading")
		}
		return box.Child(
			fairy.Text(message).FontSize(14).LineHeight(20).Color(t.MutedForeground).TextAlign(fairy.TextCenter).Wrap().
				Absolute().Inset(0).Center().Pad(24),
		)
	}

	background := t.Background.Mix(t.Muted, 0.5)
	surface := e.view.surface([4]float32{background.R, background.G, background.B, 1}, e.shell.deps.Log,
		map[string]any{"modPath": e.modPath()}).Key(e.key("surface")).WFull().HFull().
		DragButtons(fairy.MouseLeft | fairy.MouseRight | fairy.MouseMiddle).
		OnPointerDown(func(p fairy.PointerEvent) {
			e.endStroke()
			if e.brushOn && p.Button == 0 && p.Mods&keyboard.ModAlt == 0 {
				e.painting = true
				e.beginStroke()
				e.dab(p.LocalX, p.LocalY)
			}
		}).
		OnDrag(func(d fairy.DragEvent) {
			if e.painting {
				if d.Phase != fairy.DragStart {
					e.dab(d.LocalX, d.LocalY)
				}
				if d.Phase == fairy.DragEnd {
					e.endStroke()
				}
				return
			}
			e.view.setRing(nil)
			e.view.drag(d)
		}).
		OnClick(func() {
			if e.painting {
				e.endStroke()
			}
		}).
		OnPointerMove(func(p fairy.PointerEvent) bool {
			e.hover(p.LocalX, p.LocalY)
			return e.brushOn
		}).
		OnHover(func(in bool) {
			if !in {
				e.view.setRing(nil)
			}
		}).
		OnWheel(func(w fairy.WheelEvent) bool {
			if e.brushOn && w.Mods&keyboard.ModCtrl != 0 {
				step := 0.01
				if w.DY > 0 {
					step = -0.01
				}
				e.setRadius(e.brush.Radius + step)
				delete(e.drafts, "radius")
				return true
			}
			e.view.camera.zoom(float64(w.DY))
			return true
		})
	if e.brushOn {
		surface.Cursor(fairy.CursorCrosshair)
	} else {
		surface.Cursor(fairy.CursorMove)
	}
	box.Child(surface)
	if e.loading {
		box.Child(
			fairy.Row(kit.Spinner(16), fairy.Text(T(bodyShapePrefix+"loading")).FontSize(14).LineHeight(20)).Gap(8).
				Align(fairy.Center).PadXY(12, 8).Radius(6).Border(1, t.Border).Fill(t.Background.Alpha(0.9)).
				Absolute().Inset(0).Center().PassThrough(),
		)
	}
	return box
}

func (e *bodyShapeEditor) sidePanel(t *kit.Theme) *fairy.El {
	heading := fairy.Col(fairy.Text(toolName(toolInfo{nameKey: bodyShapePrefix + "title", beta: true})).
		FontSize(14).LineHeight(20).Medium()).
		Gap(2).PadXY(16, 12).Shrink(0).BorderEdges(0, 0, 1, 0, t.Border)
	if e.modName != "" {
		heading.Child(
			fairy.Text(e.modName).
				FontSize(12).
				LineHeight(16).
				Color(t.MutedForeground).
				Ellipsis().
				TooltipWhenTruncated(),
		)
	}
	body := fairy.Col().Gap(16).Pad(16)
	if e.fixedPath == "" {
		body.Child(e.pathControls())
	}
	if e.session != nil && e.mesh != nil {
		exportLabel := T(bodyShapePrefix + "export")
		if e.exporting {
			exportLabel = T(bodyShapePrefix + "exporting")
		}
		body.Child(e.meshPicker(), e.boneSection(t), e.toolControls(t), e.deformSection(t), e.selectionButtons(),
			e.metricsBox(t), kit.Button(exportLabel, e.export, kit.Icon(lucide.Save),
				kit.Loading(e.exporting), kit.Disabled(e.exporting || e.metrics.MovedVertices == 0)))
	}
	return fairy.Col(heading, fairy.Col(body).Grow(1).MinH(0).ScrollY()).MinH(0).Radius(6).
		Fill(t.Muted.Alpha(0.3)).Clip()
}

func (e *bodyShapeEditor) pathControls() *fairy.El {
	pick := func() {
		go func() {
			result, err := e.shell.deps.Dialog.SelectDirectory("body-shape-mod")
			fairy.Go(func() {
				if err != nil {
					e.report(err, "pick-folder", nil)
					return
				}
				if !result.Canceled && result.FilePath != "" {
					e.pathDraft = result.FilePath
				}
			})
		}()
	}
	field := fairy.Row(
		kit.TextField(e.pathDraft, func(value string) { e.pathDraft = value },
			kit.Placeholder(T(bodyShapePrefix+"mod_path_placeholder")), kit.OnSubmit(e.loadMod)).
			Key(e.key("path")).Grow(1).MinW(0),
		kit.IconButton(lucide.FolderOpen, pick, kit.Outline).Label(T("g.browse")),
	).Gap(8)
	label := T(bodyShapePrefix + "load")
	if e.loading {
		label = T(bodyShapePrefix + "loading")
	}
	return fairy.Col(
		kit.Field(T(bodyShapePrefix+"mod_path"), field),
		kit.Button(label, func() { e.loadMod(e.pathDraft) }, kit.Loading(e.loading),
			kit.Disabled(strings.TrimSpace(e.pathDraft) == "" || e.loading)),
	).Gap(12)
}

// checkRow is a check box row of the mesh and bone lists.
func checkRow(key, label string, checked, disabled bool, onChange func(bool)) *fairy.El {
	t := kit.T()
	row := fairy.Row(
		kit.Checkbox(checked, onChange, kit.Disabled(disabled)).Shrink(0),
		fairy.Text(label).FontSize(12).Grow(1).MinW(0).Ellipsis().TooltipWhenTruncated(),
	).Key(key).Gap(8).Align(fairy.Center).PadXY(8, 6).Radius(4)
	if disabled {
		return row.Opacity(0.5)
	}
	return row.HoverFill(hoverFill(t)).Cursor(fairy.CursorPointer).OnClick(func() { onChange(!checked) })
}

func (e *bodyShapeEditor) meshPicker() *fairy.El {
	selected := e.mesh.MeshIDs
	fileKeys := map[string]string{}
	for _, mesh := range e.session.Meshes {
		fileKeys[mesh.ID] = mesh.PositionFileKey
	}
	items := lo.Map(e.session.Meshes, func(mesh tools.BodyShapeMeshSummary, _ int) kit.Item {
		return kit.Item{
			Value: mesh.ID,
			Label: fmt.Sprintf("%s (%d)", mesh.Name, mesh.VertexCount),
			// Meshes reading one position file cannot be exported together.
			Disabled: slices.ContainsFunc(selected, func(id string) bool {
				return id != mesh.ID && fileKeys[id] == mesh.PositionFileKey
			}),
		}
	})
	picker := kit.MultiCombobox(e.key("meshes"), selected, items, e.selectMeshes,
		kit.Placeholder(T(bodyShapePrefix+"mesh_placeholder")), kit.EmptyText(T(bodyShapePrefix+"no_meshes")))
	return kit.Field(T(bodyShapePrefix+"mesh"), picker)
}

type bodyShapeBoneItem struct {
	key, label string
}

func (e *bodyShapeEditor) boneItems() []bodyShapeBoneItem {
	var items []bodyShapeBoneItem
	multiple := len(e.mesh.Segments) > 1
	for index, segment := range e.mesh.Segments {
		for _, bone := range segment.Bones {
			label := T(bodyShapePrefix+"bone_label", "id", bone.ID, "count", bone.VertexCount)
			if multiple {
				label = T(bodyShapePrefix+"segment_bone_label", "mesh", segment.Name, "bone", label)
			}
			items = append(items, bodyShapeBoneItem{
				key: sculpt.BoneSource{Segment: index, BoneID: bone.ID}.Key(), label: label,
			})
		}
	}
	return items
}

func (e *bodyShapeEditor) setHighlight(key string) {
	if e.highlight != key {
		e.highlight = key
		e.paintColors()
	}
}

func (e *bodyShapeEditor) boneSection(t *kit.Theme) *fairy.El {
	section := fairy.Col(
		fairy.Text(T(bodyShapePrefix+"selection_mask")).Medium(),
		fairy.Text(T(bodyShapePrefix+"selection_mask_hint")).FontSize(11).Color(t.MutedForeground).Wrap(),
	).Gap(8)
	if e.mesh.BoneCount() == 0 {
		return section
	}

	query := strings.ToLower(strings.TrimSpace(e.boneQuery))
	list := fairy.Col().Gap(2).Pad(4).MaxH(240).ScrollY().Role(fairy.RoleList)
	shown := 0
	for _, item := range e.boneItems() {
		if query != "" && !strings.Contains(strings.ToLower(item.label), query) {
			continue
		}
		shown++
		checked := slices.Contains(e.boneKeys, item.key)
		list.Child(checkRow(e.key("bone."+item.key), item.label, checked, false, func(on bool) {
			keys := slices.DeleteFunc(slices.Clone(e.boneKeys), func(key string) bool { return key == item.key })
			if on {
				keys = append(keys, item.key)
			}
			e.selectBones(keys)
		}).OnHover(func(in bool) {
			if in {
				e.setHighlight(item.key)
			} else if e.highlight == item.key {
				e.setHighlight("")
			}
		}))
	}
	if shown == 0 {
		list.Child(fairy.Text(T(bodyShapePrefix + "no_bones")).FontSize(12).Color(t.MutedForeground).Pad(8))
	}
	search := kit.TextField(e.boneQuery, func(value string) { e.boneQuery = value },
		kit.Placeholder(T(bodyShapePrefix+"import_bones_placeholder"))).Key(e.key("bones.search"))
	return section.Child(fairy.Col(search, list).Gap(4).Pad(4).Radius(6).Border(1, t.Border).
		OnHover(func(in bool) {
			if !in {
				e.setHighlight("")
			}
		}))
}

func (e *bodyShapeEditor) toolControls(t *kit.Theme) *fairy.El {
	tool := "brush"
	if e.brush.Tool == sculpt.ToolComponent {
		tool = "component"
	}
	controls := fairy.Col(kit.Field(T(bodyShapePrefix+"selection_tool"), kit.Select(e.key("tool"), tool, []kit.Item{
		{Value: "brush", Label: T(bodyShapePrefix + "tool_mode_brush")},
		{Value: "component", Label: T(bodyShapePrefix + "tool_mode_component"), Disabled: e.mesh.ComponentIDs == nil},
	}, func(value string) {
		e.brush.Tool = sculpt.ToolBrush
		if value == "component" {
			e.brush.Tool = sculpt.ToolComponent
		}
	}))).Gap(16)
	if e.brush.Tool == sculpt.ToolBrush {
		reach, hint := "surface", T(bodyShapePrefix+"brush_reach_surface_hint")
		if e.brush.Reach == sculpt.ReachThrough {
			reach, hint = "through", T(bodyShapePrefix+"brush_reach_through_hint")
		}
		controls.Child(kit.Field(T(bodyShapePrefix+"brush_reach"), kit.Select(e.key("reach"), reach, []kit.Item{
			{Value: "surface", Label: T(bodyShapePrefix + "brush_reach_surface")},
			{Value: "through", Label: T(bodyShapePrefix + "brush_reach_through")},
		}, func(value string) {
			e.brush.Reach = sculpt.ReachSurface
			if value == "through" {
				e.brush.Reach = sculpt.ReachThrough
			}
		}), kit.Description(hint)))
	}
	toggle := func(label string, on bool, set func(bool)) *fairy.El {
		return fairy.Row(
			fairy.Text(label).FontSize(12).Color(t.MutedForeground).Grow(1).MinW(0),
			kit.Switch(on, set).Label(label),
		).Gap(8).Align(fairy.Center)
	}
	return controls.Child(
		toggle(T(bodyShapePrefix+"show_weights"), e.showWeights, func(on bool) {
			e.showWeights = on
			e.paintColors()
		}),
		toggle(T(bodyShapePrefix+"show_original"), e.showOriginal, func(on bool) {
			e.showOriginal = on
			e.update(0)
		}),
	)
}

// bodyShapeRange is a labelled slider with its value shown on the right, as the React RangeField.
func bodyShapeRange(label, display string, value, minimum, maximum, step float64, set func(float64)) *fairy.El {
	t := kit.T()
	return fairy.Col(
		fairy.Row(
			fairy.Text(label).FontSize(14).LineHeight(19.25).Medium().Grow(1).MinW(0),
			fairy.Text(display).FontSize(11).LineHeight(16.5).Color(t.MutedForeground),
		).Gap(8).Align(fairy.Center),
		kit.Slider(value, minimum, maximum, step, set).Label(label),
	).Gap(6)
}

func (e *bodyShapeEditor) deformSection(t *kit.Theme) *fairy.El {
	if !e.hasSelection {
		return fairy.Text(T(bodyShapePrefix + "selection_empty_hint")).FontSize(12).Color(t.MutedForeground).Wrap()
	}
	operations := make([]kit.Item, 0, len(sculpt.DeformOperations))
	for _, op := range e.mesh.Operations() {
		operations = append(operations, kit.Item{Value: string(op), Label: T(bodyShapePrefix + "deform_" + string(op))})
	}
	box := fairy.Col(
		fairy.Text(T(bodyShapePrefix+"selection_controls")).Medium(),
		kit.Field(T(bodyShapePrefix+"deform_operation"), kit.Select(e.key("operation"), string(e.op), operations,
			func(value string) { e.setOperation(sculpt.DeformOperation(value)) })),
	).Gap(12).Pad(12).Radius(6).Fill(t.Background.Alpha(0.6))
	c := e.control

	if model, ok := e.op.Anatomy(); ok {
		for index, name := range sculpt.AnatomyControls[model] {
			value := 0.0
			if index < len(c.Anatomy) {
				value = c.Anatomy[index]
			}
			box.Child(bodyShapeRange(T(bodyShapePrefix+"anatomy."+string(model)+"."+name),
				fmt.Sprintf("%.0f%%", value*100), value, -1, 1, 0.01, func(next float64) { e.setAnatomy(index, next) }))
		}
		return box.Child(
			fairy.Text(T(bodyShapePrefix + "anatomy.up_hint")).FontSize(12).Color(t.MutedForeground).Wrap(),
		)
	}

	if e.op == sculpt.DeformTranslate {
		extent := e.meshExtent()
		for axis, name := range []string{"translate_x", "translate_y", "translate_z"} {
			box.Child(bodyShapeRange(T(bodyShapePrefix+name), fmt.Sprintf("%.3f", c.Translation[axis]),
				c.Translation[axis], -extent*0.5, extent*0.5, extent*0.005, func(next float64) {
					e.setControl(func(c *sculpt.DeformControl) { c.Translation[axis] = next })
				}))
		}
		return box
	}

	limit, display := 0.5, fmt.Sprintf("%.0f%%", c.Amount*100)
	switch e.op {
	case sculpt.DeformInflate:
		limit, display = 0.25, fmt.Sprintf("%.3f", c.Amount)
	case sculpt.DeformTaper:
		limit = 1
	default:
	}
	box.Child(bodyShapeRange(T(bodyShapePrefix+"deform_amount"), display, c.Amount, -limit, limit, 0.01,
		func(next float64) { e.setControl(func(c *sculpt.DeformControl) { c.Amount = next }) }))
	switch e.op {
	case sculpt.DeformScale:
		for axis, name := range []string{"axis_x", "axis_y", "axis_z"} {
			box.Child(bodyShapeRange(T(bodyShapePrefix+name), fmt.Sprintf("%.2f", c.AxisScale[axis]),
				c.AxisScale[axis], 0, 2, 0.05, func(next float64) {
					e.setControl(func(c *sculpt.DeformControl) { c.AxisScale[axis] = next })
				}))
		}
	case sculpt.DeformTaper:
		box.Child(bodyShapeRange(T(bodyShapePrefix+"taper_factor"), fmt.Sprintf("%.2f", c.TaperFactor),
			c.TaperFactor, 0, 2, 0.05, func(next float64) {
				e.setControl(func(c *sculpt.DeformControl) { c.TaperFactor = next })
			}))
	default:
	}
	return box
}

// meshExtent is the largest side of the original mesh's bounds, which scales the translation sliders.
func (e *bodyShapeEditor) meshExtent() float64 {
	size := sculpt.ComputeMeshBounds(e.mesh.OriginalPositions).Size
	return max(size[0], size[1], size[2], 1e-3)
}

func (e *bodyShapeEditor) selectionButtons() *fairy.El {
	m := e.mesh
	noAdjacency := !e.hasSelection || m.Adjacency == nil
	button := func(label string, icon *svg.Vector, onClick func(), disabled bool) *fairy.El {
		opts := []kit.Option{kit.Outline, kit.Disabled(disabled)}
		if icon != nil {
			opts = append(opts, kit.Icon(icon))
		}
		return kit.Button(T(bodyShapePrefix+label), onClick, opts...).Basis(0).Grow(1).MinW(0)
	}
	edit := func(edit func(m *sculpt.LoadedMesh) bool) func() {
		return func() { e.editSelection(func(m *sculpt.LoadedMesh) { edit(m) }) }
	}
	return fairy.Col(
		fairy.Row(
			button("undo_selection", lucide.Undo2, e.undo, !e.history.CanUndo()),
			button("redo_selection", lucideIcon("redo-2"), e.redo, !e.history.CanRedo()),
		).Gap(8),
		fairy.Row(
			button("grow_selection", lucide.Maximize2, edit((*sculpt.LoadedMesh).GrowSelection), noAdjacency),
			button("shrink_selection", lucide.Minimize2, edit((*sculpt.LoadedMesh).ShrinkSelection), noAdjacency),
		).Gap(8),
		fairy.Row(
			button("smooth_selection", nil, edit((*sculpt.LoadedMesh).SmoothSelection), noAdjacency),
			button("reset_selected", nil, func() {
				e.resetDeform()
				e.update(0)
			}, !e.hasSelection),
		).Gap(8),
	).Gap(8)
}

func (e *bodyShapeEditor) metricsBox(t *kit.Theme) *fairy.El {
	line := func(key, value string) *fairy.El {
		return fairy.Text(T(bodyShapePrefix+"metrics."+key) + ": " + value)
	}
	box := fairy.Col(
		line("vertices", strconv.Itoa(e.metrics.VertexCount)),
		line("moved", strconv.Itoa(e.metrics.MovedVertices)),
		line("max_disp", fmt.Sprintf("%.5f", e.metrics.MaxDisplacement)),
	).Gap(4).Pad(12).Radius(6).Fill(t.Background.Alpha(0.6)).FontSize(12).Color(t.MutedForeground)
	if count := e.mesh.BoneCount(); count > 0 {
		box.Child(line("bones", strconv.Itoa(count)))
	}
	return box
}
