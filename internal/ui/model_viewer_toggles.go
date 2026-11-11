package ui

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const (
	viewerSideWidth    = 360
	viewerSideMinWidth = 280
	// viewerSideReserved is the viewer's minimum width with the splitter.
	viewerSideReserved = 332
	viewerSideKeyStep  = 16
	// viewerAllFormsOption stands for every form in the form list, where "" means no highlighted row.
	viewerAllFormsOption = "*"
	// viewerIneffectiveDelay lets slider ticks settle before the backend re-evaluates every variable.
	viewerIneffectiveDelay = 150 * time.Millisecond
	// viewerGameTextureHighlight tints the meshes the mod binds no diffuse texture to.
	viewerGameTextureHighlight = 0xf59e0b
)

// viewerIneffective holds the toggle values that change nothing in the current state, by variable and value.
type viewerIneffective struct {
	seq    uint64
	cancel context.CancelFunc
	// form is the form the values were computed for; they show only while it is selected.
	form   string
	values map[string]map[string]modelviewer.ModelViewerIneffectiveValue
}

// refreshIneffective asks the backend which toggle values have no effect in the current state. The previous
// answer stays visible until the next arrives, so an open list does not flash.
func (v *ModelViewer) refreshIneffective() {
	scene := v.scene
	if v.ineffective.cancel != nil {
		v.ineffective.cancel()
		v.ineffective.cancel = nil
	}
	v.ineffective.seq++
	if scene == nil || v.tools == nil || len(scene.transport.Variables) == 0 {
		return
	}
	seq, form, state := v.ineffective.seq, v.form, maps.Clone(v.state)
	time.AfterFunc(viewerIneffectiveDelay, func() {
		fairy.Go(func() {
			if v.closed || v.scene != scene || seq != v.ineffective.seq {
				return
			}
			ctx, cancel := context.WithCancel(v.ctx)
			v.ineffective.cancel = cancel
			go func() {
				entries, err := v.tools.GetModelViewerIneffectiveValues(
					ctx, scene.transport.MemorySessionID, form, state,
				)
				if err != nil && ctx.Err() == nil {
					_ = infra.ReportError(v.log, err, "ModelViewer.ineffectiveValues", infra.Diagnostic{
						Operation: "ineffective-values", Fields: map[string]any{"modPath": v.path, "form": form},
					})
				}
				fairy.Go(func() {
					defer cancel()
					if v.scene != scene || seq != v.ineffective.seq {
						return
					}
					v.ineffective.cancel = nil
					v.ineffective.form, v.ineffective.values = form, nil
					if err != nil {
						return
					}
					values := map[string]map[string]modelviewer.ModelViewerIneffectiveValue{}
					for _, entry := range entries {
						if values[entry.VariableID] == nil {
							values[entry.VariableID] = map[string]modelviewer.ModelViewerIneffectiveValue{}
						}
						values[entry.VariableID][entry.Value] = entry
					}
					v.ineffective.values = values
				})
			}()
		})
	})
}

// ineffectiveValue returns why value of variable changes nothing in the current state, if it does not.
func (v *ModelViewer) ineffectiveValue(variableID string, value any) (modelviewer.ModelViewerIneffectiveValue, bool) {
	if v.ineffective.form != v.form {
		return modelviewer.ModelViewerIneffectiveValue{}, false
	}
	entry, ok := v.ineffective.values[variableID][fmt.Sprint(value)]
	return entry, ok
}

// setToggleState replaces the toggle state and shows it.
func (v *ModelViewer) setToggleState(state map[string]any) {
	v.state, v.preview = state, nil
	v.evaluate()
	v.refreshIneffective()
}

func (v *ModelViewer) setForm(form string) {
	v.form, v.previewForm = form, nil
	v.evaluate()
	v.refreshIneffective()
}

// previewValue shows value of variable without selecting it, while its row is highlighted in the open list.
func (v *ModelViewer) previewValue(variable modelviewer.ModelViewerVariable, value any) {
	v.preview = maps.Clone(v.state)
	v.preview[variable.ID] = value
	v.evaluate()
}

func (v *ModelViewer) clearPreview() {
	if v.preview != nil {
		v.preview = nil
		v.evaluate()
	}
}

// applyResolution selects the values a suggestion names, which make an ineffective value usable.
func (v *ModelViewer) applyResolution(suggestion modelviewer.ModelViewerResolutionSuggestion) {
	v.setToggleState(v.resolutionState(suggestion))
}

// resolutionState is the toggle state with the values a suggestion names selected.
func (v *ModelViewer) resolutionState(suggestion modelviewer.ModelViewerResolutionSuggestion) map[string]any {
	state := v.state
	for _, change := range suggestion.Changes {
		variable := modelviewer.ModelViewerVariable{ID: change.VarID}
		for _, candidate := range v.scene.transport.Variables {
			if candidate.ID == change.VarID {
				variable = candidate
				break
			}
		}
		state = modelviewer.ApplyViewerVariableSelection(state, variable, change.ToValue)
	}
	return state
}

// showsVariable reports whether the toggles list variable in the selected form. The variables a clip animates
// follow its frames instead.
func (v *ModelViewer) showsVariable(variable modelviewer.ModelViewerVariable) bool {
	if len(variable.Values) == 0 && variable.ControlType != "slider" {
		return false
	}
	if clip := v.animationClip(); clip != nil && slices.Contains(clip.VariableIDs, variable.ID) {
		return false
	}
	return formVisible(variable.Form, v.form)
}

// shownVariables returns the variables the toggles list, in their declared order.
func (v *ModelViewer) shownVariables() []modelviewer.ModelViewerVariable {
	if v.scene == nil {
		return nil
	}
	var shown []modelviewer.ModelViewerVariable
	for _, variable := range v.scene.transport.Variables {
		if v.showsVariable(variable) {
			shown = append(shown, variable)
		}
	}
	return shown
}

// viewerVariableGroup is the toggles one form declares.
type viewerVariableGroup struct {
	form, label string
	variables   []modelviewer.ModelViewerVariable
}

// groupVariablesByForm splits the toggles of a bundled mod by the form declaring them, keeping those of no
// form first and unlabeled, so equally named toggles of two forms stay apart.
func groupVariablesByForm(
	variables []modelviewer.ModelViewerVariable,
	forms []modelviewer.ModelViewerForm,
) []viewerVariableGroup {
	groups := []viewerVariableGroup{{}}
	for _, form := range forms {
		groups = append(groups, viewerVariableGroup{form: form.ID, label: form.Label})
	}
	for _, variable := range variables {
		at := 0
		if variable.Form != "" {
			at = max(slices.IndexFunc(groups, func(g viewerVariableGroup) bool { return g.form == variable.Form }), 0)
		}
		groups[at].variables = append(groups[at].variables, variable)
	}
	used := slices.DeleteFunc(groups, func(g viewerVariableGroup) bool { return len(g.variables) == 0 })
	if len(used) == 1 {
		used[0].label = ""
	}
	return used
}

// showsToggleControls reports whether the side column has anything to show: toggles, or the overlay menus
// while they do not float over the viewer.
func (v *ModelViewer) showsToggleControls(variables []modelviewer.ModelViewerVariable) bool {
	return len(variables) > 0 || (!v.overlayInViewer() && len(v.shownOverlayMenus()) > 0)
}

// toggleControls fills a column width logical pixels wide with the overlay menus that do not float over the
// viewer, the tiles of a mod with menu art, and the toggles of the selected form: lists in two columns under
// their form, then sliders.
func (v *ModelViewer) toggleControls(
	t kit.Theme,
	variables []modelviewer.ModelViewerVariable,
	width float32,
) *fairy.El {
	var lists, sliders []modelviewer.ModelViewerVariable
	for _, variable := range variables {
		if variable.ControlType == "slider" {
			sliders = append(sliders, variable)
		} else {
			lists = append(lists, variable)
		}
	}
	const pad = 16
	inner := width - 2*pad
	panel := fairy.Col().Gap(12).Pad(pad)
	if v.gameLinked {
		panel.Child(fairy.Row(
			kit.Button(T("page.tools.model_viewer.menu.apply_to_game"), v.applyTogglesToGame, kit.Outline, kit.Small,
				kit.Icon(lucide.Gamepad2), kit.Disabled(v.applyingToGame), kit.Loading(v.applyingToGame)),
		).Justify(fairy.JustifyEnd))
	}
	if !v.overlayInViewer() {
		for _, index := range v.shownOverlayMenus() {
			menuWidth := min(inner, float32(v.scene.transport.OverlayMenus[index].Width))
			panel.Child(
				fairy.Row(v.overlayMenuPanel(index, menuWidth)).Justify(fairy.JustifyCenter).PadEdges(0, 0, 4, 0),
			)
		}
	}
	if v.scene.transport.UIAssets.BackgroundPath != "" && len(lists) > 0 {
		panel.Child(fairy.Box(v.variantTiles(t, lists, inner)).PadEdges(0, 0, 4, 0))
	}
	for _, group := range groupVariablesByForm(lists, v.scene.transport.Forms) {
		section := fairy.Col().Gap(8)
		if group.label != "" {
			section.Child(fairy.Text(group.label).FontSize(12).Medium().Color(t.MutedForeground).Ellipsis())
		}
		grid := fairy.Col().Gap(12)
		for at := 0; at < len(group.variables); at += 2 {
			row := fairy.Row(v.listControl(t, group.variables[at])).Gap(12)
			if at+1 < len(group.variables) {
				row.Child(v.listControl(t, group.variables[at+1]))
			} else {
				row.Child(fairy.Box().Grow(1).Basis(0))
			}
			grid.Child(row)
		}
		panel.Child(section.Child(grid))
	}
	for _, variable := range sliders {
		panel.Child(v.sliderControl(t, variable))
	}
	return panel
}

func (v *ModelViewer) listControl(t kit.Theme, variable modelviewer.ModelViewerVariable) *fairy.El {
	current, set := v.state[variable.ID]
	if !set {
		current = variable.DefaultValue
	}
	items := make([]kit.Item, 0, len(variable.Values))
	selected := ""
	for i, value := range variable.Values {
		item := kit.Item{Value: strconv.Itoa(i), Label: value.Label, OnHover: func(in bool) {
			if in {
				v.previewValue(variable, value.Value)
			} else {
				v.clearPreview()
			}
		}}
		if item.Label == "" {
			item.Label = fmt.Sprint(value.Value)
		}
		if fmt.Sprint(value.Value) == fmt.Sprint(current) {
			selected = item.Value
		}
		entry, ineffective := v.ineffectiveValue(variable.ID, value.Value)
		item.Disabled = ineffective
		if !ineffective {
			items = append(items, item)
			continue
		}
		blocking := make([]string, len(entry.BlockingVars))
		for j, variable := range entry.BlockingVars {
			blocking[j] = variable.Label + "=" + variable.Value
		}
		item.Tooltip = T("page.tools.model_viewer.ineffective_blocked_by", "variables", strings.Join(blocking, ", "))
		if len(entry.Suggestions) > 0 {
			first := entry.Suggestions[0]
			item.Tooltip += "\n" + T("page.tools.model_viewer.ineffective_suggestion")
			for _, suggestion := range entry.Suggestions {
				item.Tooltip += "\n• " + suggestion.Display
			}
			actionHint := T("page.tools.model_viewer.ineffective_apply")
			for _, change := range first.Changes {
				actionHint += "\n• " + change.VarLabel + ": " + fmt.Sprint(
					change.FromValue,
				) + " → " + fmt.Sprint(
					change.ToValue,
				)
			}
			item.Trailing = kit.IconButton(lucide.Check, func() { v.applyResolution(first) }, kit.Ghost, kit.Small,
				kit.Tooltip(actionHint)).Label(T("page.tools.model_viewer.ineffective_apply")).
				OnHover(func(in bool) {
					if in {
						v.preview = v.resolutionState(first)
						v.evaluate()
					} else {
						v.clearPreview()
					}
				})
		}
		items = append(items, item)
	}

	valueAt := func(index string) (any, bool) {
		i, err := strconv.Atoi(index)
		if err != nil || i < 0 || i >= len(variable.Values) {
			return nil, false
		}
		return variable.Values[i].Value, true
	}
	list := kit.Select("model-viewer.variable."+variable.ID, selected, items, func(index string) {
		if value, ok := valueAt(index); ok {
			v.selectValue(variable, value)
		}
	}, kit.Label(variable.Label), kit.OnHighlight(func(index string) {
		if value, ok := valueAt(index); ok {
			v.previewValue(variable, value)
			return
		}
		v.clearPreview()
	}), kit.OnOpenChange(func(open bool) {
		if !open {
			v.clearPreview()
		}
	})).WFull()
	return fairy.Col(
		fairy.Text(variable.Label).FontSize(14).LineHeight(20).Medium().Ellipsis(),
		list,
	).Gap(8).Grow(1).Basis(0).MinW(0).PadEdges(0, 0, 0, 12).BorderEdges(0, 0, 0, 4, t.Primary.Alpha(0.4))
}

func (v *ModelViewer) sliderControl(t kit.Theme, variable modelviewer.ModelViewerVariable) *fairy.El {
	slider := modelviewer.ModelViewerSlider{Max: 1}
	if variable.Slider != nil {
		slider = *variable.Slider
	}
	value, ok := viewerNumber(v.state[variable.ID])
	if !ok {
		if value, ok = viewerNumber(variable.DefaultValue); !ok && len(variable.Values) > 0 {
			value, _ = viewerNumber(variable.Values[0].Value)
		}
	}
	return fairy.Col(
		fairy.Row(
			fairy.Text(variable.Label).
				FontSize(14).
				LineHeight(20).
				Medium().
				Ellipsis().
				Grow(1).
				MinW(0),
			fairy.Text(formatViewerNumber(value)).FontSize(12).LineHeight(16).Color(t.MutedForeground).Shrink(0),
		).Gap(12).Align(fairy.Center).MarginEdges(0, 0, 4, 0),
		kit.Slider(value, slider.Min, slider.Max, slider.Step, func(next float64) {
			v.selectValue(variable, next)
		}, kit.Label(variable.Label)).Key("model-viewer.variable."+variable.ID).WFull(),
		fairy.Row(
			fairy.Text(formatViewerNumber(slider.Min)).LineHeight(16.5),
			fairy.Text(formatViewerNumber(slider.Max)).LineHeight(16.5),
		).Justify(fairy.JustifyBetween).FontSize(11).Color(t.MutedForeground),
	).Gap(8).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
}

func viewerNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed)
	case int:
		return float64(typed), true
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	}
	return 0, false
}

// formSwitcher is the form list over the viewer's top-left corner. The form highlighted in the open list
// shows without being selected.
func (v *ModelViewer) formSwitcher() *fairy.El {
	forms := v.scene.transport.Forms
	items := []kit.Item{{Value: viewerAllFormsOption, Label: T("page.tools.model_viewer.forms.all")}}
	var groups []string
	for _, form := range forms {
		if !slices.Contains(groups, form.Group) {
			groups = append(groups, form.Group)
		}
	}
	for _, group := range groups {
		if group != "" {
			items = append(items, kit.Item{Heading: true, Label: group})
		}
		for _, form := range forms {
			if form.Group == group {
				items = append(items, kit.Item{Value: form.ID, Label: form.Label})
			}
		}
	}
	selected := viewerAllFormsOption
	if slices.ContainsFunc(forms, func(form modelviewer.ModelViewerForm) bool { return form.ID == v.form }) {
		selected = v.form
	}
	formOf := func(option string) string {
		if option == viewerAllFormsOption {
			return ""
		}
		return option
	}
	list := kit.Select("model-viewer.form", selected, items, func(option string) { v.setForm(formOf(option)) },
		kit.Small, kit.Label(T("page.tools.model_viewer.forms.label")), kit.OnHighlight(func(option string) {
			var preview *string
			if option != "" {
				form := formOf(option)
				preview = &form
			}
			if (preview == nil) != (v.previewForm == nil) || (preview != nil && *preview != *v.previewForm) {
				v.previewForm = preview
				v.evaluate()
			}
		}))
	return fairy.Box(list).Absolute().Top(12).Left(12).MaxW(280)
}

// gameTextureCount counts the drawn meshes the mod binds no diffuse texture to. The game draws them with its
// own textures, which the viewer cannot read.
func (v *ModelViewer) gameTextureCount() int {
	count := 0
	for _, state := range v.toggleStates {
		if state.Visible && state.TexKey == "" {
			count++
		}
	}
	return count
}

// gameTextureNotice counts the parts drawn without a texture and switches their highlight.
func (v *ModelViewer) gameTextureNotice(t kit.Theme, count int) *fairy.El {
	label := T("page.tools.model_viewer.game_textures", "count", count)
	button := kit.Button(label, func() {
		v.highlightGameTextures = !v.highlightGameTextures
		v.revision++
	}, kit.Outline, kit.Small, kit.Icon(lucide.Info), kit.Tooltip(T("page.tools.model_viewer.game_textures_hint")))
	if v.highlightGameTextures {
		amber := fairy.Hex(viewerGameTextureHighlight)
		button.Border(1, amber).Color(amber)
	}
	return fairy.Box(button).Absolute().Bottom(12).Left(12)
}

// sideSplit lays the viewer beside the side column. Without a tool the column is the toggles alone, which a
// splitter resizes, and hides on a click.
func (v *ModelViewer) sideSplit(t kit.Theme, viewport *fairy.El, windowWidth float32) *fairy.El {
	variables := v.shownVariables()
	stacked := windowWidth < toolsWideViewport
	measure := func(w, _ float32) {
		if v.splitWidth != w {
			v.splitWidth = w
			fairy.Go(func() {})
		}
	}
	if stacked {
		width := v.splitWidth
		if width <= 0 {
			width = windowWidth
		}
		var side *fairy.El
		if v.tool != nil && v.tool.inspector != nil {
			side = v.toolPanel(t, variables, width)
		} else if v.showsToggleControls(variables) {
			side = v.togglePanel(t, variables, width)
		}
		stack := fairy.Col(viewport.Basis(0).Grow(1)).Gap(12).Grow(1).MinH(0).OnLayout(measure)
		if side != nil {
			stack.Child(side.Basis(0).Grow(1).MinH(0))
		}
		return stack
	}
	row := fairy.Row(viewport).Grow(1).MinH(0).OnLayout(measure)
	if v.tool != nil && v.tool.inspector != nil {
		return row.Gap(12).Child(v.toolPanel(t, variables, viewerSideWidth).W(viewerSideWidth).Shrink(0))
	}
	if !v.showsToggleControls(variables) {
		return row
	}

	maxWidth := max(viewerSideMinWidth, v.splitWidth-viewerSideReserved)
	if v.splitWidth == 0 {
		// Nothing is laid out yet to limit the column by.
		maxWidth = max(v.sideWidth, viewerSideMinWidth)
	}
	width := min(max(v.sideWidth, viewerSideMinWidth), maxWidth)
	resize := func(next float32) {
		v.sideWidth, v.sideClosed = min(max(next, viewerSideMinWidth), maxWidth), false
	}
	shown := width
	if v.sideClosed {
		shown = 0
	}
	title := T("page.tools.model_viewer.toggle_viewer_splitter")
	var grip *fairy.El
	if v.sideClosed {
		grip = fairy.Icon(lucide.ChevronLeft).Size(12, 12).Color(t.MutedForeground)
	} else {
		grip = fairy.Box().Size(4, 32).Radius(2).Fill(t.Border)
	}
	splitter := fairy.Box(grip).Key("model-viewer.splitter").W(12).HFull().Shrink(0).Center().
		Cursor(fairy.CursorResizeEW).Role(fairy.RoleSeparator).Label(title).Focusable().
		OnClick(func() { v.sideClosed = !v.sideClosed }).
		OnDrag(func(d fairy.DragEvent) {
			if d.Phase == fairy.DragStart {
				v.sideDragStart = shown
			}
			start := v.sideDragStart
			if start == 0 {
				// Dragging a closed panel away from the viewer has nothing to open.
				if d.DX >= 0 {
					return
				}
				start = viewerSideMinWidth
			}
			resize(start - d.DX)
			if d.Canceled {
				resize(max(v.sideDragStart, viewerSideMinWidth))
				v.sideClosed = v.sideDragStart == 0
			}
		}).
		OnKey(func(k keyboard.KeyEvent) bool {
			switch {
			case k.Key == keyboard.KeyEnter || k.Key == keyboard.KeySpace:
				v.sideClosed = !v.sideClosed
			case !v.sideClosed && k.Key == keyboard.KeyLeft:
				resize(width + viewerSideKeyStep)
			case !v.sideClosed && k.Key == keyboard.KeyRight:
				resize(width - viewerSideKeyStep)
			default:
				return false
			}
			return true
		})
	row.Child(splitter)
	if !v.sideClosed {
		row.Child(v.togglePanel(t, variables, width).W(width).Shrink(0))
	}
	return row
}

func (v *ModelViewer) togglePanel(t kit.Theme, variables []modelviewer.ModelViewerVariable, width float32) *fairy.El {
	return fairy.Col(v.toggleControls(t, variables, width)).Key("model-viewer.toggles").
		MinW(0).ScrollY().Radius(6).Clip().Fill(t.Muted.Alpha(0.3))
}

// toolPanel is the side column of a tool's viewer: the tool's cards in a 360-pixel column set 12 pixels apart
// from the viewer, the toggles folded under them, and the tool's footer pinned below.
func (v *ModelViewer) toolPanel(t kit.Theme, variables []modelviewer.ModelViewerVariable, width float32) *fairy.El {
	column := fairy.Col(v.tool.inspector()).Gap(12)
	if v.showsToggleControls(variables) {
		column.Child(v.togglesSection(t, variables, width))
	}
	side := fairy.Col(fairy.Col(column).Grow(1).MinH(0).ScrollY()).Gap(12)
	if v.tool.footer != nil {
		side.Child(fairy.Box(v.tool.footer()).Pad(12).Shrink(0).Radius(6).Fill(t.Muted.Alpha(0.3)))
	}
	return side.MinW(0).MinH(0)
}

// togglesSection folds the toggles under a tool's panel into a card that shows how many there are while closed.
func (v *ModelViewer) togglesSection(
	t kit.Theme,
	variables []modelviewer.ModelViewerVariable,
	width float32,
) *fairy.El {
	open := v.togglesOpen
	title := T("page.tools.model_viewer.toggle_viewer")
	chevron := lucide.ChevronRight
	if open {
		chevron = lucide.ChevronDown
	}
	trigger := fairy.Row(
		fairy.Icon(chevron).Size(16, 16).Color(t.MutedForeground).Shrink(0),
		fairy.Text(title).FontSize(14).LineHeight(20).Medium().Grow(1).MinW(0).Ellipsis(),
	).Gap(6).Align(fairy.Center).PadXY(16, 12).Role(fairy.RoleButton).Label(title).Expanded(open).Focusable().
		Cursor(fairy.CursorPointer).OnClick(func() { v.togglesOpen = !open })
	if !open {
		trigger.Child(fairy.Text(itoa(len(variables))).FontSize(12).LineHeight(16).Color(t.MutedForeground).Shrink(0))
	}
	card := fairy.Col(trigger).Key("model-viewer.toggles").Shrink(0).Radius(6).Fill(t.Muted.Alpha(0.3))
	if open {
		card.Child(v.toggleControls(t, variables, width).BorderEdges(1, 0, 0, 0, t.Border))
	}
	return card
}
