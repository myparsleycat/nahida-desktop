package modelviewer

import (
	"context"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
)

type ModelViewerOverlayFlipbook struct {
	Columns    int     `json:"columns"`
	Rows       int     `json:"rows"`
	FrameStart int     `json:"frameStart"`
	FrameEnd   int     `json:"frameEnd"`
	FPS        float64 `json:"fps"`
}

// ModelViewerOverlayQuadState is how a quad looks while a variable holds a value
// other than its default.
type ModelViewerOverlayQuadState struct {
	Variable string  `json:"variable"`
	Value    float64 `json:"value"`
	Path     string  `json:"path"`
	Opacity  float64 `json:"opacity"`
}

type ModelViewerOverlayQuad struct {
	Path     string                        `json:"path"`
	X        float64                       `json:"x"`
	Y        float64                       `json:"y"`
	Width    float64                       `json:"width"`
	Height   float64                       `json:"height"`
	Opacity  float64                       `json:"opacity"`
	Flipbook *ModelViewerOverlayFlipbook   `json:"flipbook,omitempty"`
	States   []ModelViewerOverlayQuadState `json:"states,omitempty"`
}

type ModelViewerOverlayTransition struct {
	From float64 `json:"from"`
	To   float64 `json:"to"`
}

// ModelViewerOverlayHover replaces the texture of a quad while its button is hovered.
type ModelViewerOverlayHover struct {
	Quad int    `json:"quad"`
	Path string `json:"path"`
}

type ModelViewerOverlayButton struct {
	X           float64                        `json:"x"`
	Y           float64                        `json:"y"`
	Width       float64                        `json:"width"`
	Height      float64                        `json:"height"`
	Variable    string                         `json:"variable"`
	Transitions []ModelViewerOverlayTransition `json:"transitions"`
	Hover       []ModelViewerOverlayHover      `json:"hover,omitempty"`
}

// ModelViewerOverlayMenu is a menu a mod draws over the game and operates with
// the mouse, in pixels at the reference resolution with its corner at the origin.
type ModelViewerOverlayMenu struct {
	Width   float64                    `json:"width"`
	Height  float64                    `json:"height"`
	Form    string                     `json:"form,omitempty"`
	Quads   []ModelViewerOverlayQuad   `json:"quads"`
	Buttons []ModelViewerOverlayButton `json:"buttons"`
}

const (
	// Menus position elements with fractions of the window size, some of them
	// mixing width and height, so a 16:9 window reproduces the authored layout.
	modelViewerOverlayWidth  = 1920.0
	modelViewerOverlayHeight = 1080.0

	maxModelViewerOverlayDepth = 32
	// Spent across all runs of a menu, which also bounds how long a menu can stall a load.
	maxModelViewerOverlaySteps = 1000000
	maxModelViewerOverlayQuads = 256
	maxModelViewerOverlayRange = 256
	// A menu tells a toggle's values apart; a long slider would only spend the step budget.
	maxModelViewerOverlayStateValues = 16

	modelViewerOverlaySampleRate    = 120
	modelViewerOverlaySampleSeconds = 10
)

var (
	modelViewerOverlayAssignRE = regexp.MustCompile(
		`(?i)^(?:(?:local|global)\s+)?(?:persist\s+)?(\$[\w.]+|[xyzw]\d*)\s*=\s*(.+)$`,
	)
	modelViewerOverlayDeclareRE = regexp.MustCompile(
		`(?i)^(?:(?:global|local)\s+)?(?:persist\s+)?(\$[\w.]+)(?:\s*=\s*(.+))?$`,
	)
	modelViewerOverlayLocalRE   = regexp.MustCompile(`(?i)^local\s+(\$[\w.]+)$`)
	modelViewerOverlayBindRE    = regexp.MustCompile(`(?i)^ps-t(\d+)\s*=\s*(.+)$`)
	modelViewerOverlayConfigRE  = regexp.MustCompile(`^\$([\w.]+)$`)
	modelViewerOverlaySizeRE    = regexp.MustCompile(`(?m)^\s*#define\s+(?:DIMENSIONS|SIZE)\s+\w+\[(\d+)\]\.xy\b`)
	modelViewerOverlayShiftRE   = regexp.MustCompile(`(?m)^\s*#define\s+(?:SHIFT|OFFSET)\s+\w+\[(\d+)\]\.zw\b`)
	modelViewerOverlayAlphaRE   = regexp.MustCompile(`(?m)^\s*\w+\.a\s*=\s*\w+\.a\s*\*\s*([0-9.]+)\s*;`)
	modelViewerOverlayFrameRE   = regexp.MustCompile(`(?m)^\s*#define\s+FRAME_INDEX\s+\w+\[(\d+)\]\.x\b`)
	modelViewerOverlayColumnsRE = regexp.MustCompile(`(?m)^\s*#define\s+FLIPBOOK_COLS\s+(\d+)\b`)
	modelViewerOverlayRowsRE    = regexp.MustCompile(`(?m)^\s*#define\s+FLIPBOOK_ROWS\s+(\d+)\b`)
	modelViewerOverlayTextureRE = regexp.MustCompile(`Texture2D\s*(?:<[^>]*>)?\s+\w+\s*:\s*register\(\s*t(\d+)\s*\)`)
)

type modelViewerOverlayShader struct {
	sizeRegister, shiftRegister, frameRegister string
	textureRegister                            string
	opacity                                    float64
	columns, rows                              int
}

type modelViewerOverlayProgram struct {
	folder, shaderDir string
	lists             map[string][]string
	presents          [][]string
	clickLines        []string
	targets           map[string]bool
	textures          map[string]string
	missingTextures   map[string]bool
	shaders           map[string]*modelViewerOverlayShader
	initial           map[string]any
	unsupported       bool
	steps             int
}

type modelViewerOverlayRun struct {
	program *modelViewerOverlayProgram
	state   map[string]any
	bound   map[string]string
	sources map[string]string
	quads   []modelViewerOverlayTracedQuad
	// assigned lists the variables in the order the lines set them.
	assigned []string
	// opened is set once the menu lists are running; see chooseBranch.
	opened bool
}

type modelViewerOverlayTracedQuad struct {
	path                string
	x, y, width, height float64
	opacity             float64
	shader              *modelViewerOverlayShader
	frameSource         string
	frame               float64
}

// buildModelViewerOverlayMenus reproduces the mouse menus of the scanned INIs by
// running their command lists as the game would, recording what they draw and
// what a click on each element changes. Mods with no such menu, or one this does
// not understand, get none and keep the regular toggle controls.
func buildModelViewerOverlayMenus(
	ctx context.Context,
	folder string,
	scans []modelViewerGeometryScan,
	variables []ModelViewerVariable,
	names map[string]modelViewerVariableName,
) []ModelViewerOverlayMenu {
	var menus []ModelViewerOverlayMenu
	for _, scan := range scans {
		program := newModelViewerOverlayProgram(folder, scan)
		if program == nil {
			continue
		}
		if menu := program.build(ctx, variables, names); menu != nil {
			menus = append(menus, *menu)
		}
	}
	return menus
}

func newModelViewerOverlayProgram(folder string, scan modelViewerGeometryScan) *modelViewerOverlayProgram {
	program := &modelViewerOverlayProgram{
		folder:          folder,
		shaderDir:       scan.iniDir,
		lists:           make(map[string][]string),
		targets:         make(map[string]bool),
		textures:        make(map[string]string),
		shaders:         make(map[string]*modelViewerOverlayShader),
		missingTextures: make(map[string]bool),
		initial:         modelViewerOverlayInitialState(scan.sections),
	}
	var clickKey *modINISection
	for index := range scan.sections {
		section := &scan.sections[index]
		lines := cleanModelViewerOverlayLines(section.Lines)
		switch {
		case strings.EqualFold(section.Header, "Present"):
			program.presents = append(program.presents, lines)
		case strings.EqualFold(section.Header, "CommandList"), strings.EqualFold(section.Header, "CustomShader"):
			program.lists[modelViewerNormalizeKey(section.Header+section.Name)] = lines
		case strings.HasPrefix(modelViewerNormalizeKey(section.Header), "key") && clickKey == nil &&
			modelViewerOverlayIsClickKey(lines):
			clickKey = section
		}
	}
	if clickKey == nil {
		return nil
	}
	program.clickLines = program.lists[modelViewerNormalizeKey(sectionValue(clickKey.Lines, "run"))]
	if len(program.clickLines) == 0 {
		return nil
	}

	// The lists assigning what the click key tests are the ones drawing the menu.
	watched := make(map[string]bool)
	for _, match := range modelViewerVariableRE.FindAllStringSubmatch(sectionValue(clickKey.Lines, "condition"), -1) {
		watched[modelViewerNormalizeKey(match[1])] = true
	}
	for _, branch := range splitModelViewerMenuSlotBranches(program.clickLines) {
		watched[branch.slotVariable] = true
	}
	for name, lines := range program.lists {
		if slices.Equal(lines, program.clickLines) {
			continue
		}
		for _, line := range lines {
			match := modelViewerOverlayAssignRE.FindStringSubmatch(line)
			if match != nil && watched[modelViewerNormalizeKey(match[1])] {
				program.targets[name] = true
				break
			}
		}
	}
	if len(program.targets) == 0 {
		return nil
	}

	for _, resource := range scan.resources {
		if resource.Filename == "" {
			continue
		}
		path := filepath.Join(folder, filepath.FromSlash(slashPath(resource.Filename)))
		name := modelViewerNormalizeKey(modelViewerTrimResourcePrefix(resource.Name))
		if regularFile(path) {
			program.textures[name] = path
		} else {
			program.missingTextures[name] = true
		}
	}
	return program
}

// modelViewerOverlayInitialState holds only what [Constants] declares. Values
// assigned at run time, such as a drag flag a held key sets, are not there yet
// when the game draws the menu for the first time.
func modelViewerOverlayInitialState(sections []modINISection) map[string]any {
	state := make(map[string]any)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Constants") {
			continue
		}
		for _, line := range cleanModelViewerOverlayLines(section.Lines) {
			match := modelViewerOverlayDeclareRE.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimSpace(match[2]), 64)
			if err != nil {
				value = 0
			}
			state[modelViewerNormalizeKey(match[1])] = value
		}
	}
	return state
}

func cleanModelViewerOverlayLines(lines []string) []string {
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(strings.SplitN(line, ";", 2)[0]); line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return cleaned
}

func modelViewerOverlayIsClickKey(lines []string) bool {
	if sectionValue(lines, "run") == "" || strings.EqualFold(sectionValue(lines, "type"), "hold") {
		return false
	}
	return slices.ContainsFunc(lines, func(line string) bool {
		key, value, ok := strings.Cut(line, "=")
		return ok && strings.EqualFold(strings.TrimSpace(key), "key") &&
			strings.Contains(strings.ToUpper(value), "VK_LBUTTON")
	})
}

func (p *modelViewerOverlayProgram) build(
	ctx context.Context,
	variables []ModelViewerVariable,
	names map[string]modelViewerVariableName,
) *ModelViewerOverlayMenu {
	idle := p.trace(-1, -1, nil)
	if p.unsupported || len(idle.quads) < 2 {
		return nil
	}
	backdrop, order := modelViewerOverlaySmallestFirst(idle.quads)

	publicToInternal := make(map[string]string, len(names))
	for internal, name := range names {
		publicToInternal[name.PublicID] = internal
	}
	type located struct {
		variable ModelViewerVariable
		internal string
	}
	tracked := make(map[string]located, len(variables))
	for _, variable := range variables {
		internal, ok := publicToInternal[variable.ID]
		if !ok {
			internal = modelViewerNormalizeKey(variable.ID)
		}
		tracked[internal] = located{variable: variable, internal: internal}
	}

	originX, originY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, quad := range idle.quads {
		originX, originY = min(originX, quad.x), min(originY, quad.y)
		maxX, maxY = max(maxX, quad.x+quad.width), max(maxY, quad.y+quad.height)
	}
	menu := &ModelViewerOverlayMenu{
		Width:  roundModelViewerOverlay(maxX - originX),
		Height: roundModelViewerOverlay(maxY - originY),
	}
	place := func(quad modelViewerOverlayTracedQuad) (float64, float64, float64, float64) {
		return roundModelViewerOverlay(quad.x - originX), roundModelViewerOverlay(quad.y - originY),
			roundModelViewerOverlay(quad.width), roundModelViewerOverlay(quad.height)
	}

	buttonIndex := make(map[string]int)
	var buttonVariables []located
	for _, quadIndex := range order {
		if ctx.Err() != nil || p.unsupported {
			return nil
		}
		if quadIndex == backdrop {
			continue
		}
		quad := idle.quads[quadIndex]
		hovered := p.trace(
			(quad.x+quad.width/2)/modelViewerOverlayWidth,
			(quad.y+quad.height/2)/modelViewerOverlayHeight,
			nil,
		)
		clicked := hovered.click(hovered.state)

		// A click can also adjust related toggles. The one it sets first is the
		// button's own; the variable's effects carry out the others.
		var hit *located
		for _, key := range clicked.assigned {
			if entry, ok := tracked[key]; ok &&
				modelViewerString(clicked.state[key]) != modelViewerString(hovered.state[key]) {
				hit = &entry
				break
			}
		}
		if hit == nil {
			continue
		}
		transitions := hovered.transitions(hit.internal, hit.variable)
		if len(transitions) == 0 {
			continue
		}
		key := fmt.Sprintf("%s:%v", hit.variable.ID, transitions)
		x, y, width, height := place(quad)
		if existing, ok := buttonIndex[key]; ok {
			// A slot is usually an icon inside a frame; the frame takes the clicks.
			button := &menu.Buttons[existing]
			if width*height <= button.Width*button.Height*4 {
				button.X, button.Y, button.Width, button.Height = x, y, width, height
			}
			continue
		}
		buttonIndex[key] = len(menu.Buttons)
		buttonVariables = append(buttonVariables, *hit)
		menu.Buttons = append(menu.Buttons, ModelViewerOverlayButton{
			X: x, Y: y, Width: width, Height: height,
			Variable:    hit.variable.ID,
			Transitions: transitions,
			Hover:       modelViewerOverlayHoverChanges(idle.quads, hovered.quads),
		})
		if menu.Form == "" {
			menu.Form = hit.variable.Form
		}
	}
	if len(menu.Buttons) == 0 {
		return nil
	}

	for _, quad := range idle.quads {
		x, y, width, height := place(quad)
		menu.Quads = append(menu.Quads, ModelViewerOverlayQuad{
			Path: quad.path, X: x, Y: y, Width: width, Height: height, Opacity: quad.opacity,
			Flipbook: idle.flipbook(quad),
		})
	}

	for _, entry := range buttonVariables {
		domain := modelViewerOverlayDomain(entry.variable)
		if len(domain) > maxModelViewerOverlayStateValues {
			continue
		}
		for _, value := range domain {
			if ctx.Err() != nil {
				return nil
			}
			current, err := modelViewerNumber(idle.state[entry.internal])
			if err == nil && current == value {
				continue
			}
			changed := p.trace(-1, -1, map[string]any{entry.internal: value})
			if len(changed.quads) != len(idle.quads) {
				continue
			}
			for index, quad := range changed.quads {
				if quad.path != idle.quads[index].path || quad.opacity != idle.quads[index].opacity {
					menu.Quads[index].States = append(menu.Quads[index].States, ModelViewerOverlayQuadState{
						Variable: entry.variable.ID, Value: value, Path: quad.path, Opacity: quad.opacity,
					})
				}
			}
		}
	}
	if p.unsupported {
		return nil
	}
	return menu
}

func modelViewerOverlaySmallestFirst(quads []modelViewerOverlayTracedQuad) (int, []int) {
	order := make([]int, len(quads))
	for index := range order {
		order[index] = index
	}
	area := func(index int) float64 { return quads[index].width * quads[index].height }
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case area(a) < area(b):
			return -1
		case area(a) > area(b):
			return 1
		}
		return 0
	})
	return order[len(order)-1], order
}

func modelViewerOverlayHoverChanges(idle, hovered []modelViewerOverlayTracedQuad) []ModelViewerOverlayHover {
	if len(idle) != len(hovered) {
		return nil
	}
	var changes []ModelViewerOverlayHover
	for index := range idle {
		if idle[index].path != hovered[index].path {
			changes = append(changes, ModelViewerOverlayHover{Quad: index, Path: hovered[index].path})
		}
	}
	return changes
}

// serveModelViewerOverlayTextures gives the menus' DDS textures a URL serving them
// as PNG. A menu with a texture that cannot be read is dropped for the regular controls.
func (t *Service) serveModelViewerOverlayTextures(
	ctx context.Context,
	sessionID string,
	menus []ModelViewerOverlayMenu,
) []ModelViewerOverlayMenu {
	served := make(map[string]string)
	serve := func(path string) (string, error) {
		if !strings.EqualFold(filepath.Ext(path), ".dds") {
			return path, nil
		}
		if url, ok := served[path]; ok {
			return url, nil
		}
		if _, err := inspectModelViewerDDS(ctx, path); err != nil {
			return "", err
		}
		url, err := t.protocol.StoreMemoryLoaderWithContentType(
			sessionID,
			"overlay-tex:"+strconv.Itoa(len(served)),
			"image/png",
			func(loadCtx context.Context) ([]byte, error) {
				return prepareModelViewerDDSFallback(loadCtx, path, 0)
			},
		)
		if err != nil {
			return "", err
		}
		served[path] = url
		return url, nil
	}

	kept := make([]ModelViewerOverlayMenu, 0, len(menus))
	for _, menu := range menus {
		var failure error
		rewrite := func(path *string) {
			if failure == nil {
				*path, failure = serve(*path)
			}
		}
		for quadIndex := range menu.Quads {
			quad := &menu.Quads[quadIndex]
			rewrite(&quad.Path)
			for stateIndex := range quad.States {
				rewrite(&quad.States[stateIndex].Path)
			}
		}
		for buttonIndex := range menu.Buttons {
			for hoverIndex := range menu.Buttons[buttonIndex].Hover {
				rewrite(&menu.Buttons[buttonIndex].Hover[hoverIndex].Path)
			}
		}
		if failure != nil {
			if !infra.IsCancellationError(failure) {
				t.log.Warn(fmt.Sprintf("Overlay menu texture is unusable: %v", failure), "StaticGlb.loadForViewer")
			}
			continue
		}
		kept = append(kept, menu)
	}
	return kept
}

// flipbook samples the frame expression over time, since menus compute their
// frame from the game clock in many ways.
func (r *modelViewerOverlayRun) flipbook(quad modelViewerOverlayTracedQuad) *ModelViewerOverlayFlipbook {
	if quad.shader.columns <= 0 || quad.shader.rows <= 0 {
		return nil
	}
	flipbook := &ModelViewerOverlayFlipbook{
		Columns:    quad.shader.columns,
		Rows:       quad.shader.rows,
		FrameStart: int(math.Round(quad.frame)),
		FrameEnd:   int(math.Round(quad.frame)),
	}
	expression := quad.frameSource
	if source := modelViewerOverlayConfigRE.FindStringSubmatch(expression); source != nil {
		expression = r.sources[modelViewerNormalizeKey(source[1])]
	}
	if expression == "" {
		return flipbook
	}

	state := maps.Clone(r.state)
	changes, previous := 0, math.NaN()
	for step := range modelViewerOverlaySampleRate * modelViewerOverlaySampleSeconds {
		if r.program.spendStep() {
			return flipbook
		}
		state["time"] = float64(step) / modelViewerOverlaySampleRate
		value, err := evaluateModelViewerExpression(expression, state)
		if err != nil {
			return flipbook
		}
		number, err := modelViewerNumber(value)
		if err != nil {
			return flipbook
		}
		frame := int(math.Round(number))
		if step == 0 {
			flipbook.FrameStart, flipbook.FrameEnd = frame, frame
		}
		flipbook.FrameStart, flipbook.FrameEnd = min(flipbook.FrameStart, frame), max(flipbook.FrameEnd, frame)
		if step > 0 && number != previous {
			changes++
		}
		previous = number
	}
	flipbook.FPS = float64(changes) / modelViewerOverlaySampleSeconds
	return flipbook
}

func roundModelViewerOverlay(value float64) float64 {
	return math.Round(value*100) / 100
}

func (p *modelViewerOverlayProgram) trace(
	cursorX, cursorY float64,
	overrides map[string]any,
) *modelViewerOverlayRun {
	run := &modelViewerOverlayRun{
		program: p,
		state:   cloneModelViewerState(p.initial),
		bound:   make(map[string]string),
		sources: make(map[string]string),
	}
	for key, value := range map[string]float64{
		"windowwidth": modelViewerOverlayWidth, "windowheight": modelViewerOverlayHeight,
		"reswidth": modelViewerOverlayWidth, "resheight": modelViewerOverlayHeight,
		"rtwidth": modelViewerOverlayWidth, "rtheight": modelViewerOverlayHeight,
		"cursorx": cursorX, "cursory": cursorY,
		"cursorwindowx": cursorX * modelViewerOverlayWidth, "cursorwindowy": cursorY * modelViewerOverlayHeight,
		"cursorscreenx": cursorX * modelViewerOverlayWidth, "cursorscreeny": cursorY * modelViewerOverlayHeight,
		"time": 0,
	} {
		run.state[key] = value
	}
	maps.Copy(run.state, overrides)
	for _, lines := range p.presents {
		run.exec(lines, 0, true)
	}
	return run
}

// exec runs command list lines. Gates that keep the menu closed, such as its
// visibility variable, fail with no branch taken; the branch leading to the menu
// is then entered anyway. Present sections also run unrelated lists, which
// followMenuOnly skips.
func (r *modelViewerOverlayRun) exec(lines []string, depth int, followMenuOnly bool) {
	if depth > maxModelViewerOverlayDepth {
		r.program.unsupported = true
		return
	}
	for index := 0; index < len(lines); index++ {
		if r.program.spendStep() {
			return
		}
		line := lines[index]
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "post ") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "pre "), "PRE "))
		if strings.HasPrefix(lower, "if ") {
			branches, end, ok := splitModelViewerDirectConditional(lines, index)
			if !ok {
				r.program.unsupported = true
				return
			}
			if branch := r.chooseBranch(branches); branch != nil {
				r.exec(branch.lines, depth, followMenuOnly)
			}
			index = end
			continue
		}
		if match := modelViewerOverlayLocalRE.FindStringSubmatch(line); match != nil {
			r.state[modelViewerNormalizeKey(match[1])] = float64(0)
			continue
		}
		if match := modelViewerOverlayBindRE.FindStringSubmatch(line); match != nil {
			fields := strings.Fields(match[2])
			r.bound[match[1]] = modelViewerNormalizeKey(modelViewerTrimResourcePrefix(fields[len(fields)-1]))
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(key), "run") {
			target := modelViewerNormalizeKey(value)
			if followMenuOnly && !r.program.reaches(target, make(map[string]bool)) {
				continue
			}
			if r.program.targets[target] {
				r.opened = true
			}
			if body, ok := r.program.lists[target]; ok {
				r.exec(body, depth+1, false)
			}
			if strings.HasPrefix(target, "customshader") {
				r.recordQuad(target)
			}
			continue
		}
		if match := modelViewerOverlayAssignRE.FindStringSubmatch(line); match != nil {
			if value, err := evaluateModelViewerExpression(match[2], r.state); err == nil {
				if number, err := modelViewerNumber(value); err == nil {
					key := modelViewerNormalizeKey(match[1])
					r.state[key] = number
					r.sources[key] = strings.TrimSpace(match[2])
					if alias := modelViewerOverlayRegisterAlias(match[1]); alias != "" {
						r.state[alias] = number
						r.sources[alias] = r.sources[key]
					}
					r.assigned = append(r.assigned, key)
				}
			}
		}
	}
}

// modelViewerOverlayRegisterAlias returns the other spelling of IniParams register
// 0, where x and x0 name the same value.
func modelViewerOverlayRegisterAlias(name string) string {
	key := strings.ToLower(name)
	switch {
	case strings.HasPrefix(key, "$"):
		return ""
	case len(key) == 1:
		return key + "0"
	case key[1:] == "0":
		return key[:1]
	}
	return ""
}

// spendStep reports whether the program must stop: its budget ran out, or an
// earlier failure already gave up on the menu.
func (p *modelViewerOverlayProgram) spendStep() bool {
	if p.steps++; p.steps > maxModelViewerOverlaySteps {
		p.unsupported = true
	}
	return p.unsupported
}

// chooseBranch forces a way into the menu only on the path leading to it. Once
// inside, and in the click list, a failed condition such as a hit test is real.
func (r *modelViewerOverlayRun) chooseBranch(
	branches []modelViewerDirectConditionalBranch,
) *modelViewerDirectConditionalBranch {
	for index := range branches {
		if branches[index].expression == nil || evaluateModelViewerCondition(*branches[index].expression, r.state) {
			return &branches[index]
		}
	}
	if r.opened {
		return nil
	}
	for index := range branches {
		if r.program.linesReachMenu(branches[index].lines) {
			return &branches[index]
		}
	}
	return nil
}

func (p *modelViewerOverlayProgram) linesReachMenu(lines []string) bool {
	return slices.ContainsFunc(lines, func(line string) bool {
		key, value, ok := strings.Cut(line, "=")
		return ok && strings.EqualFold(strings.TrimSpace(key), "run") &&
			p.reaches(modelViewerNormalizeKey(value), make(map[string]bool))
	})
}

func (p *modelViewerOverlayProgram) reaches(list string, visiting map[string]bool) bool {
	if p.targets[list] {
		return true
	}
	if visiting[list] {
		return false
	}
	visiting[list] = true
	body, ok := p.lists[list]
	return ok && slices.ContainsFunc(body, func(line string) bool {
		key, value, isAssign := strings.Cut(line, "=")
		return isAssign && strings.EqualFold(strings.TrimSpace(key), "run") &&
			p.reaches(modelViewerNormalizeKey(value), visiting)
	})
}

func (r *modelViewerOverlayRun) recordQuad(list string) {
	shader := r.program.shader(list)
	if shader == nil || len(r.quads) >= maxModelViewerOverlayQuads {
		return
	}
	resource := r.bound[shader.textureRegister]
	path, ok := r.program.textures[resource]
	if !ok {
		// The game shows a resource filled at run time, but draws nothing for an empty
		// slot or a file the mod does not ship.
		if resource != "" && resource != "null" && !r.program.missingTextures[resource] {
			r.program.unsupported = true
		}
		return
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".dds":
	default:
		// The renderer cannot show the rest, and a menu missing parts misleads.
		r.program.unsupported = true
		return
	}
	number := func(register string) float64 {
		value, _ := modelViewerNumber(r.state[register])
		return value
	}
	quad := modelViewerOverlayTracedQuad{
		path:    path,
		x:       number("z"+shader.shiftRegister) * modelViewerOverlayWidth,
		y:       number("w"+shader.shiftRegister) * modelViewerOverlayHeight,
		width:   number("x"+shader.sizeRegister) * modelViewerOverlayWidth,
		height:  number("y"+shader.sizeRegister) * modelViewerOverlayHeight,
		opacity: shader.opacity,
		shader:  shader,
	}
	if quad.width <= 0 || quad.height <= 0 {
		return
	}
	if shader.frameRegister != "" {
		quad.frame = number("x" + shader.frameRegister)
		quad.frameSource = r.sources["x"+shader.frameRegister]
	}
	r.quads = append(r.quads, quad)
}

// shader reads the conventions the common menu shaders declare for placing a
// textured quad from IniParams; a shader without them draws nothing here.
func (p *modelViewerOverlayProgram) shader(list string) *modelViewerOverlayShader {
	if shader, ok := p.shaders[list]; ok {
		return shader
	}
	p.shaders[list] = nil
	relative := stripModelViewerQuotes(sectionValue(p.lists[list], "ps"))
	if relative == "" {
		return nil
	}
	path, err := resolveModelViewerResourcePath(p.folder, p.shaderDir, relative)
	if err != nil {
		return nil
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(source)
	size := modelViewerOverlaySizeRE.FindStringSubmatch(text)
	shift := modelViewerOverlayShiftRE.FindStringSubmatch(text)
	texture := modelViewerOverlayTextureRE.FindStringSubmatch(text)
	if size == nil || shift == nil || texture == nil {
		return nil
	}
	shader := &modelViewerOverlayShader{
		sizeRegister:    size[1],
		shiftRegister:   shift[1],
		textureRegister: texture[1],
		opacity:         1,
	}
	if alpha := modelViewerOverlayAlphaRE.FindStringSubmatch(text); alpha != nil {
		if value, err := strconv.ParseFloat(alpha[1], 64); err == nil {
			shader.opacity = value
		}
	}
	frame := modelViewerOverlayFrameRE.FindStringSubmatch(text)
	columns := modelViewerOverlayColumnsRE.FindStringSubmatch(text)
	rows := modelViewerOverlayRowsRE.FindStringSubmatch(text)
	if frame != nil && columns != nil && rows != nil {
		shader.frameRegister = frame[1]
		shader.columns, _ = strconv.Atoi(columns[1])
		shader.rows, _ = strconv.Atoi(rows[1])
	}
	p.shaders[list] = shader
	return shader
}

// click runs the click list from the given state, with the cursor where this run
// left it.
func (r *modelViewerOverlayRun) click(state map[string]any) *modelViewerOverlayRun {
	after := &modelViewerOverlayRun{
		program: r.program,
		opened:  true,
		state:   maps.Clone(state),
		bound:   make(map[string]string),
		sources: make(map[string]string),
	}
	after.exec(r.program.clickLines, 0, false)
	return after
}

func (r *modelViewerOverlayRun) transitions(
	internal string,
	variable ModelViewerVariable,
) []ModelViewerOverlayTransition {
	var transitions []ModelViewerOverlayTransition
	for _, from := range modelViewerOverlayDomain(variable) {
		state := maps.Clone(r.state)
		state[internal] = from
		to, err := modelViewerNumber(r.click(state).state[internal])
		to = roundModelViewerOverlayValue(to)
		if err == nil && to != from {
			transitions = append(transitions, ModelViewerOverlayTransition{From: from, To: to})
		}
	}
	return transitions
}

func modelViewerOverlayDomain(variable ModelViewerVariable) []float64 {
	var domain []float64
	for _, entry := range variable.Values {
		if number, err := modelViewerNumber(entry.Value); err == nil {
			domain = append(domain, number)
		}
	}
	if slider := variable.Slider; slider != nil && slider.Step > 0 {
		steps := int(math.Round((slider.Max - slider.Min) / slider.Step))
		for index := 0; index <= steps && steps <= maxModelViewerOverlayRange; index++ {
			domain = append(domain, roundModelViewerOverlayValue(slider.Min+float64(index)*slider.Step))
		}
	}
	return domain
}

// roundModelViewerOverlayValue removes the floating point error of stepping a
// fractional slider, so a value matches the one the renderer holds.
func roundModelViewerOverlayValue(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}
