package modelviewer

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
)

const (
	// A drag draws the next frame from positions the previous frame computed.
	modelViewerOverlayPanelSettleFrames = 3
	modelViewerOverlayPanelDragLow      = -1.0
	modelViewerOverlayPanelDragHigh     = 2.0
)

// ModelViewerOverlaySliderPart is a quad that moves with a slider, as it stands at
// both ends of the track.
type ModelViewerOverlaySliderPart struct {
	Quad       int     `json:"quad"`
	StartX     float64 `json:"startX"`
	StartWidth float64 `json:"startWidth"`
	EndX       float64 `json:"endX"`
	EndWidth   float64 `json:"endWidth"`
}

// ModelViewerOverlaySliderMode is what a slider sets while WhenVariable holds WhenValue.
type ModelViewerOverlaySliderMode struct {
	WhenVariable string  `json:"whenVariable"`
	WhenValue    float64 `json:"whenValue"`
	Variable     string  `json:"variable"`
	Label        string  `json:"label"`
	Min          float64 `json:"min"`
	Max          float64 `json:"max"`
}

type ModelViewerOverlaySlider struct {
	Handle int                            `json:"handle"`
	Parts  []ModelViewerOverlaySliderPart `json:"parts"`
	Modes  []ModelViewerOverlaySliderMode `json:"modes"`
}

// modelViewerOverlayPanel runs a menu that [Present] draws and operates itself,
// reading the cursor each frame while a held key marks the mouse button as down.
type modelViewerOverlayPanel struct {
	program  *modelViewerOverlayProgram
	pressKey string
	gates    map[string]any
	groups   []ModelViewerComputeShapeGroup
	names    map[string]modelViewerVariableName
}

// buildModelViewerOverlayPanels reproduces those menus by playing the frames of a
// press or a drag on each element. Only controls of the given shape groups are kept,
// since nothing else in the viewer reacts to what such a menu sets.
func buildModelViewerOverlayPanels(
	ctx context.Context,
	folder string,
	scans []modelViewerGeometryScan,
	deformers []ModelViewerComputeDeformerTransport,
	names map[string]modelViewerVariableName,
) []ModelViewerOverlayMenu {
	var groups []ModelViewerComputeShapeGroup
	for _, deformer := range deformers {
		if deformer.MergedDeltas != nil {
			groups = append(groups, deformer.MergedDeltas.Groups...)
		}
	}
	if len(groups) == 0 {
		return nil
	}

	var menus []ModelViewerOverlayMenu
	for _, scan := range scans {
		panel := newModelViewerOverlayPanel(folder, scan)
		if panel == nil {
			continue
		}
		panel.groups, panel.names = groups, names
		if menu := panel.build(ctx); menu != nil {
			menus = append(menus, *menu)
		}
	}
	return menus
}

func newModelViewerOverlayPanel(folder string, scan modelViewerGeometryScan) *modelViewerOverlayPanel {
	program := newModelViewerOverlayProgramBase(folder, scan)
	var pressSection []string
	var presentLines [][]string
	for index := range scan.sections {
		section := &scan.sections[index]
		lines := cleanModelViewerOverlayLines(section.Lines)
		switch {
		case strings.EqualFold(section.Header, "Present"):
			presentLines = append(presentLines, lines)
		case strings.EqualFold(section.Header, "CommandList"), strings.EqualFold(section.Header, "CustomShader"):
			program.lists[modelViewerNormalizeKey(section.Header+section.Name)] = lines
		case strings.HasPrefix(modelViewerNormalizeKey(section.Header), "key") && pressSection == nil &&
			modelViewerOverlayIsPressKey(lines):
			pressSection = lines
		}
	}
	if pressSection == nil {
		return nil
	}

	// The rest of [Present] runs the mod itself and would spend the step budget.
	for _, lines := range presentLines {
		for index := 0; index < len(lines); index++ {
			if !strings.HasPrefix(strings.ToLower(lines[index]), "if ") {
				continue
			}
			branches, end, ok := splitModelViewerDirectConditional(lines, index)
			if !ok {
				return nil
			}
			if slices.ContainsFunc(branches, func(branch modelViewerDirectConditionalBranch) bool {
				return program.linesDrawQuad(branch.lines)
			}) {
				program.presents = append(program.presents, lines[index:end+1])
			}
			index = end
		}
	}
	if len(program.presents) == 0 {
		return nil
	}
	program.loadTextures(folder, scan)

	gates := make(map[string]any)
	condition := sectionValue(pressSection, "condition")
	for _, match := range modelViewerOverlayPanelGateRE.FindAllStringSubmatch(condition, -1) {
		if value, err := strconv.ParseFloat(match[2], 64); err == nil {
			gates[modelViewerNormalizeKey(match[1])] = value
		}
	}
	var pressKey string
	for _, line := range pressSection {
		if match := modelViewerOverlayAssignRE.FindStringSubmatch(
			line,
		); match != nil &&
			strings.HasPrefix(match[1], "$") {
			pressKey = modelViewerNormalizeKey(match[1])
			break
		}
	}
	if pressKey == "" {
		return nil
	}
	return &modelViewerOverlayPanel{program: program, pressKey: pressKey, gates: gates}
}

func modelViewerOverlayIsPressKey(lines []string) bool {
	if !strings.EqualFold(sectionValue(lines, "type"), "hold") || sectionValue(lines, "run") != "" {
		return false
	}
	return slices.ContainsFunc(lines, func(line string) bool {
		key, value, ok := strings.Cut(line, "=")
		return ok && strings.EqualFold(strings.TrimSpace(key), "key") &&
			strings.Contains(strings.ToUpper(value), "VK_LBUTTON")
	})
}

func (p *modelViewerOverlayProgram) linesDrawQuad(lines []string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "run") {
			return false
		}
		target := modelViewerNormalizeKey(value)
		return strings.HasPrefix(target, "customshader") && sectionValue(p.lists[target], "ps") != ""
	})
}

// frame plays one game frame from the given state, which it leaves untouched.
func (p *modelViewerOverlayPanel) frame(state map[string]any, x, y float64, pressed bool) *modelViewerOverlayRun {
	run := p.program.newRun(state, x, y)
	run.opened = true
	run.state[p.pressKey] = float64(0)
	if pressed {
		run.state[p.pressKey] = float64(1)
	}
	for _, lines := range p.program.presents {
		run.exec(lines, 0, false)
	}
	return run
}

func (p *modelViewerOverlayPanel) settle(state map[string]any, x, y float64, pressed bool) *modelViewerOverlayRun {
	run := p.frame(state, x, y, pressed)
	for range modelViewerOverlayPanelSettleFrames - 1 {
		run = p.frame(run.state, x, y, pressed)
	}
	return run
}

func (p *modelViewerOverlayPanel) drag(state map[string]any, x, y float64) (low, high *modelViewerOverlayRun) {
	pressed := p.frame(state, x, y, true)
	low = p.settle(pressed.state, modelViewerOverlayPanelDragLow, y, true)
	high = p.settle(low.state, modelViewerOverlayPanelDragHigh, y, true)
	return low, high
}

// press plays a click, whose effect comes only frames after the release: the
// click also grabs the panel, and the release must let go of that first.
func (p *modelViewerOverlayPanel) press(state map[string]any, x, y float64) *modelViewerOverlayRun {
	run := p.frame(state, x, y, true)
	for range modelViewerOverlayPanelSettleFrames {
		run = p.frame(run.state, x, y, false)
	}
	return run
}

func (p *modelViewerOverlayPanel) public(key string) (id, label, form string) {
	if name, ok := p.names[key]; ok {
		return name.PublicID, humanizeModelViewerLabel(name.Label), name.Form
	}
	return key, humanizeModelViewerLabel(key), ""
}

func (p *modelViewerOverlayPanel) build(ctx context.Context) *ModelViewerOverlayMenu {
	initial := maps.Clone(p.program.initial)
	maps.Copy(initial, p.gates)
	idle := p.settle(initial, -1, -1, false)
	if p.program.unsupported || len(idle.quads) < 2 {
		return nil
	}

	backdrop, order := modelViewerOverlaySmallestFirst(idle.quads)
	var buttons []ModelViewerOverlayButton
	var sliders []ModelViewerOverlaySlider
	claimed := make(map[string]bool)
	form := ""
	for _, quadIndex := range order {
		if ctx.Err() != nil || p.program.unsupported {
			return nil
		}
		if quadIndex == backdrop {
			continue
		}
		quad := idle.quads[quadIndex]
		x := (quad.x + quad.width/2) / modelViewerOverlayWidth
		y := (quad.y + quad.height/2) / modelViewerOverlayHeight

		if button, key := p.buildButton(idle.state, quad, x, y); button != nil && !claimed[key] {
			claimed[key] = true
			buttons = append(buttons, *button)
			_, _, form = p.public(key)
			continue
		}
		if slider, key := p.buildSlider(idle, quadIndex, backdrop, x, y); slider != nil && !claimed[key] {
			claimed[key] = true
			sliders = append(sliders, *slider)
		}
	}
	if p.program.unsupported || ctx.Err() != nil {
		return nil
	}

	quads := make([]ModelViewerOverlayQuad, 0, len(idle.quads))
	for _, quad := range idle.quads {
		quads = append(quads, ModelViewerOverlayQuad{
			Path: quad.path, X: quad.x, Y: quad.y, Width: quad.width, Height: quad.height, Opacity: quad.opacity,
			FlipX: quad.flipX, FlipY: quad.flipY,
			Flipbook: idle.flipbook(quad),
		})
	}
	return assembleModelViewerOverlayMenu([]*modelViewerOverlayPageBuild{{
		page:  ModelViewerOverlayPage{Quads: quads, Buttons: buttons, Sliders: sliders},
		drawn: idle.quads,
	}}, form, nil)
}

func (p *modelViewerOverlayPanel) buildButton(
	state map[string]any,
	quad modelViewerOverlayTracedQuad,
	x, y float64,
) (*ModelViewerOverlayButton, string) {
	pressed := p.press(state, x, y)
	for _, group := range p.groups {
		key := group.autoPlayKey
		if modelViewerOverlaySameValue(state[key], pressed.state[key]) {
			continue
		}
		transitions := p.pressTransitions(state, x, y, key)
		if len(transitions) == 0 {
			continue
		}
		id, label, _ := p.public(key)
		return &ModelViewerOverlayButton{
			X: quad.x, Y: quad.y, Width: quad.width, Height: quad.height,
			Variable: id, Label: label,
			Transitions: transitions,
		}, key
	}
	return nil, ""
}

func (p *modelViewerOverlayPanel) pressTransitions(
	state map[string]any,
	x, y float64,
	key string,
) []ModelViewerOverlayTransition {
	var transitions []ModelViewerOverlayTransition
	for _, from := range []float64{0, 1} {
		start := maps.Clone(state)
		start[key] = from
		run := p.press(start, x, y)
		if to, err := modelViewerNumber(run.state[key]); err == nil && to != from {
			transitions = append(transitions, ModelViewerOverlayTransition{From: from, To: to})
		}
	}
	return transitions
}

// buildSlider tells a handle from the rest by dragging it: only a handle keeps its
// width, moves along the track, and leaves the panel itself where it is.
func (p *modelViewerOverlayPanel) buildSlider(
	idle *modelViewerOverlayRun,
	handle, backdrop int,
	x, y float64,
) (*ModelViewerOverlaySlider, string) {
	low, high := p.drag(idle.state, x, y)
	if len(low.quads) != len(idle.quads) || len(high.quads) != len(idle.quads) {
		return nil, ""
	}
	if low.quads[backdrop].x != high.quads[backdrop].x || low.quads[handle].x == high.quads[handle].x ||
		low.quads[handle].width != high.quads[handle].width {
		return nil, ""
	}
	groupIndex := slices.IndexFunc(p.groups, func(group ModelViewerComputeShapeGroup) bool {
		return !modelViewerOverlaySameValue(low.state[group.speedKey], high.state[group.speedKey]) ||
			!modelViewerOverlaySameValue(low.state[group.weightKey], high.state[group.weightKey])
	})
	if groupIndex < 0 {
		return nil, ""
	}
	group := p.groups[groupIndex]

	slider := &ModelViewerOverlaySlider{Handle: handle}
	for index := range low.quads {
		start, end := low.quads[index], high.quads[index]
		if start.x != end.x || start.width != end.width {
			slider.Parts = append(slider.Parts, ModelViewerOverlaySliderPart{
				Quad: index, StartX: start.x, StartWidth: start.width, EndX: end.x, EndWidth: end.width,
			})
		}
	}
	whenVariable, _, _ := p.public(group.autoPlayKey)
	for _, mode := range []struct {
		when float64
		key  string
	}{{1, group.speedKey}, {0, group.weightKey}} {
		state := maps.Clone(idle.state)
		state[group.autoPlayKey] = mode.when
		start, end := p.drag(state, x, y)
		minimum, minErr := modelViewerNumber(start.state[mode.key])
		maximum, maxErr := modelViewerNumber(end.state[mode.key])
		if minErr != nil || maxErr != nil || minimum == maximum {
			continue
		}
		id, label, _ := p.public(mode.key)
		slider.Modes = append(slider.Modes, ModelViewerOverlaySliderMode{
			WhenVariable: whenVariable, WhenValue: mode.when,
			Variable: id, Label: label, Min: minimum, Max: maximum,
		})
	}
	if len(slider.Modes) == 0 {
		return nil, ""
	}
	return slider, group.speedKey
}
