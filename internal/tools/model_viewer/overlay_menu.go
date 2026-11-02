package modelviewer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Decoders for backdrops whose visible area is measured.
	_ "image/png"
	"io"
	"maps"
	"math"
	"net/http"
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
	FlipX    bool                          `json:"flipX,omitempty"`
	FlipY    bool                          `json:"flipY,omitempty"`
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

// ModelViewerOverlayButton changes a model variable, moves the menu to another
// page, or both.
type ModelViewerOverlayButton struct {
	X           float64                        `json:"x"`
	Y           float64                        `json:"y"`
	Width       float64                        `json:"width"`
	Height      float64                        `json:"height"`
	Variable    string                         `json:"variable,omitempty"`
	Transitions []ModelViewerOverlayTransition `json:"transitions,omitempty"`
	Page        *int                           `json:"page,omitempty"`
	Hover       []ModelViewerOverlayHover      `json:"hover,omitempty"`
	// Label names a variable the regular controls do not list.
	Label string `json:"label,omitempty"`
}

// ModelViewerOverlayPage is what a menu shows in one state of its own variables.
type ModelViewerOverlayPage struct {
	Quads   []ModelViewerOverlayQuad   `json:"quads"`
	Buttons []ModelViewerOverlayButton `json:"buttons"`
	Sliders []ModelViewerOverlaySlider `json:"sliders,omitempty"`
}

// ModelViewerOverlayMenu is a menu a mod draws over the game and operates with
// the mouse, in pixels at the reference resolution with the corner shared by its
// pages at the origin. The first page is the one the mod opens on.
type ModelViewerOverlayMenu struct {
	// X and Y are where the mod draws that corner in the reference resolution.
	X      float64                  `json:"x"`
	Y      float64                  `json:"y"`
	Width  float64                  `json:"width"`
	Height float64                  `json:"height"`
	Form   string                   `json:"form,omitempty"`
	Pages  []ModelViewerOverlayPage `json:"pages"`
	// stateVariables are the listed variables that only page this menu.
	stateVariables []string
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
	maxModelViewerOverlayPages       = 16

	modelViewerOverlaySampleRate    = 120
	modelViewerOverlaySampleSeconds = 10

	modelViewerOverlayAlphaSampleStep = 4
	// Out of 65535; soft shadows fade out below this.
	modelViewerOverlayVisibleAlpha = 2048

	modelViewerOverlayDDSType = "image/vnd.ms-dds"
)

var (
	modelViewerOverlayAssignRE = regexp.MustCompile(
		`(?i)^(?:persist\s+global\s+|(?:(?:local|global)\s+)?(?:persist\s+)?)(\$[\w.]+|[xyzw]\d*)\s*=\s*(.+)$`,
	)
	modelViewerOverlayDeclareRE = regexp.MustCompile(
		`(?i)^(?:persist\s+global\s+|(?:(?:global|local)\s+)?(?:persist\s+)?)(\$[\w.]+)(?:\s*=\s*(.+))?$`,
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
	modelViewerOverlayCornerRE  = regexp.MustCompile(
		`\.pos\.xy\s*=\s*float2\(\s*\w+\[(\d+)\]\.x\s*,\s*\w+\[(\d+)\]\.y\s*\)`,
	)
	modelViewerOverlayNameRE      = regexp.MustCompile(`\$?[A-Za-z_\\][\w.\\]*`)
	modelViewerOverlayPanelGateRE = regexp.MustCompile(`\$([\w.]+)\s*==\s*(-?\d+(?:\.\d+)?)`)
)

type modelViewerOverlayShader struct {
	sizeRegister, shiftRegister, frameRegister string
	// lowRegister and highRegister hold the opposite corners of a quad in clip space,
	// for a vertex shader that places it without a size and an offset.
	lowRegister, highRegister string
	textureRegister           string
	opacity                   float64
	columns, rows             int
}

type modelViewerOverlayProgram struct {
	folder, shaderDir string
	lists             map[string][]string
	presents          [][]string
	clickLines        []string
	targets           map[string]bool
	textures          map[string]string
	missingTextures   map[string]bool
	// renamedImages are texture files whose content is an image despite the extension.
	renamedImages map[string]bool
	shaders       map[string]*modelViewerOverlayShader
	initial       map[string]any
	// menuKeys are the variables found to select a page.
	menuKeys    map[string]bool
	unsupported bool
	steps       int
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
	// drawing is the custom shader running, whose Draw lines each add a quad.
	drawing string
}

type modelViewerOverlayTracedQuad struct {
	path                string
	x, y, width, height float64
	opacity             float64
	flipX, flipY        bool
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
	menuOnly map[string]bool,
) []ModelViewerOverlayMenu {
	var menus []ModelViewerOverlayMenu
	for _, scan := range scans {
		program := newModelViewerOverlayProgram(folder, scan)
		if program == nil {
			continue
		}
		// A menu may draw from variables of the INI it controls, whose names are scoped apart.
		for _, other := range scans {
			maps.Copy(program.initial, modelViewerOverlayInitialState(other.sections))
		}
		if menu := program.build(ctx, variables, names, menuOnly); menu != nil {
			menus = append(menus, *menu)
		}
	}
	return menus
}

func newModelViewerOverlayProgramBase(folder string, scan modelViewerGeometryScan) *modelViewerOverlayProgram {
	return &modelViewerOverlayProgram{
		folder:          folder,
		shaderDir:       scan.iniDir,
		lists:           make(map[string][]string),
		targets:         make(map[string]bool),
		textures:        make(map[string]string),
		shaders:         make(map[string]*modelViewerOverlayShader),
		missingTextures: make(map[string]bool),
		renamedImages:   make(map[string]bool),
		initial:         modelViewerOverlayInitialState(scan.sections),
		menuKeys:        make(map[string]bool),
	}
}

func (p *modelViewerOverlayProgram) loadTextures(folder string, scan modelViewerGeometryScan) {
	for _, resource := range scan.resources {
		if resource.Filename == "" {
			continue
		}
		path := filepath.Join(folder, filepath.FromSlash(slashPath(resource.Filename)))
		name := modelViewerNormalizeKey(modelViewerTrimResourcePrefix(resource.Name))
		if regularFile(path) {
			p.textures[name] = path
			if !modelViewerOverlayKnownImageExtension(path) && modelViewerOverlayImageType(path) != "" {
				p.renamedImages[path] = true
			}
		} else {
			p.missingTextures[name] = true
		}
	}
}

func newModelViewerOverlayProgram(folder string, scan modelViewerGeometryScan) *modelViewerOverlayProgram {
	program := newModelViewerOverlayProgramBase(folder, scan)
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
	program.addPresentTargets(sectionValue(clickKey.Lines, "condition"))
	if len(program.targets) == 0 {
		return nil
	}
	program.loadTextures(folder, scan)
	return program
}

// addPresentTargets marks what [Present] runs under a condition testing everything
// the click key tests, which draws the menu whether or not a list assigns those
// variables. Keys alone may open such a menu.
func (p *modelViewerOverlayProgram) addPresentTargets(clickCondition string) {
	wanted := modelViewerVariableRE.FindAllStringSubmatch(clickCondition, -1)
	if len(wanted) == 0 {
		return
	}
	var visit func(lines []string)
	visit = func(lines []string) {
		for index := 0; index < len(lines); index++ {
			if !strings.HasPrefix(strings.ToLower(lines[index]), "if ") {
				continue
			}
			branches, end, ok := splitModelViewerDirectConditional(lines, index)
			if !ok {
				return
			}
			for _, branch := range branches {
				visit(branch.lines)
				if branch.expression == nil {
					continue
				}
				tested := modelViewerVariableRE.FindAllStringSubmatch(*branch.expression, -1)
				if slices.ContainsFunc(wanted, func(variable []string) bool {
					return !slices.ContainsFunc(tested, func(candidate []string) bool {
						return modelViewerNormalizeKey(candidate[1]) == modelViewerNormalizeKey(variable[1])
					})
				}) {
					continue
				}
				for _, line := range branch.lines {
					key, value, isAssign := strings.Cut(line, "=")
					if isAssign && strings.EqualFold(strings.TrimSpace(key), "run") {
						p.targets[modelViewerNormalizeKey(value)] = true
					}
				}
			}
			index = end
		}
	}
	for _, lines := range p.presents {
		visit(lines)
	}
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

type modelViewerOverlayVariable struct {
	variable ModelViewerVariable
	internal string
}

// modelViewerOverlayExplorer walks the pages of one menu, starting from the one
// the mod opens on.
type modelViewerOverlayExplorer struct {
	ctx     context.Context
	program *modelViewerOverlayProgram
	tracked map[string]modelViewerOverlayVariable
	// candidates maps listed variables that may turn out to page the menu to their IDs.
	candidates map[string]string
	// states holds what differs from [Constants] on each page found so far.
	states []map[string]any
	form   string
	// sliderKeys are the listed sliders a page may draw.
	sliderKeys []string
}

// modelViewerOverlayPageBuild holds a page in the window coordinates the mod drew it in.
type modelViewerOverlayPageBuild struct {
	page  ModelViewerOverlayPage
	drawn []modelViewerOverlayTracedQuad
	// state is what the page was drawn from, including the positions of its sliders.
	state map[string]any
}

func (p *modelViewerOverlayProgram) build(
	ctx context.Context,
	variables []ModelViewerVariable,
	names map[string]modelViewerVariableName,
	menuOnly map[string]bool,
) *ModelViewerOverlayMenu {
	initial := p.trace(-1, -1, nil)
	if p.unsupported || len(initial.quads) < 2 {
		return nil
	}

	publicToInternal := make(map[string]string, len(names))
	for internal, name := range names {
		publicToInternal[name.PublicID] = internal
	}
	explorer := &modelViewerOverlayExplorer{
		ctx:        ctx,
		program:    p,
		tracked:    make(map[string]modelViewerOverlayVariable, len(variables)),
		candidates: make(map[string]string),
		states:     []map[string]any{{}},
	}
	for _, variable := range variables {
		internal, ok := publicToInternal[variable.ID]
		if !ok {
			internal = modelViewerNormalizeKey(variable.ID)
		}
		explorer.tracked[internal] = modelViewerOverlayVariable{variable: variable, internal: internal}
	}
	explorer.findPageCandidates(initial, menuOnly)
	explorer.findSliderKeys()

	var pages []*modelViewerOverlayPageBuild
	for index := 0; index < len(explorer.states); index++ {
		knownKeys := len(p.menuKeys)
		page := explorer.buildPage(explorer.states[index])
		if ctx.Err() != nil {
			return nil
		}
		// A later page that cannot be reproduced costs only that page.
		if p.unsupported {
			if index == 0 {
				return nil
			}
			p.unsupported = false
			page = nil
		}
		pages = append(pages, page)

		// A page condition just stopped opening the menu by force; see chooseBranch.
		// Each restart needs a new key, and the step budget still bounds them.
		if len(p.menuKeys) != knownKeys && explorer.drawsDifferently(pages) {
			pages = nil
			explorer.states = explorer.states[:1]
			index = -1
		}
	}

	var stateVariables []string
	for key, id := range explorer.candidates {
		if p.menuKeys[key] {
			stateVariables = append(stateVariables, id)
		}
	}
	return assembleModelViewerOverlayMenu(pages, explorer.form, stateVariables)
}

// drawsDifferently reports whether a page built so far would now come out otherwise.
// A state the budget no longer reaches keeps the page already built.
func (e *modelViewerOverlayExplorer) drawsDifferently(pages []*modelViewerOverlayPageBuild) bool {
	for _, page := range pages {
		if page == nil {
			continue
		}
		run := e.program.tryTrace(page.state)
		if run != nil && !modelViewerOverlaySameQuads(run.quads, page.drawn, false) {
			return true
		}
	}
	return false
}

// findPageCandidates marks the listed variables that move the menu's elements and
// that nothing in the model reads, as a page number does when the click list steps
// it through a slot the variable scan recognizes. clickTarget decides which are one.
func (e *modelViewerOverlayExplorer) findPageCandidates(initial *modelViewerOverlayRun, menuOnly map[string]bool) {
	for key, entry := range e.tracked {
		if !menuOnly[modelViewerNormalizeKey(entry.variable.ID)] {
			continue
		}
		domain := modelViewerOverlayDomain(entry.variable)
		if len(domain) > maxModelViewerOverlayStateValues {
			continue
		}
		for _, value := range domain {
			run := e.program.tryTrace(map[string]any{key: value})
			if run != nil && !modelViewerOverlaySameQuads(run.quads, initial.quads, true) {
				e.candidates[key] = entry.variable.ID
				break
			}
		}
	}
}

func (e *modelViewerOverlayExplorer) buildPage(state map[string]any) *modelViewerOverlayPageBuild {
	p := e.program
	drawn, sliders := e.pageSliders(state)
	idle := p.trace(-1, -1, drawn)
	if p.unsupported || len(idle.quads) == 0 {
		p.unsupported = true
		return nil
	}
	backdrop, order := modelViewerOverlaySmallestFirst(idle.quads)

	var buttons []ModelViewerOverlayButton
	var buttonKeys []string
	var buttonVariables []modelViewerOverlayVariable
	for _, quadIndex := range order {
		if e.ctx.Err() != nil || p.unsupported {
			return nil
		}
		if quadIndex == backdrop {
			continue
		}
		quad := idle.quads[quadIndex]
		hovered := p.trace(
			(quad.x+quad.width/2)/modelViewerOverlayWidth,
			(quad.y+quad.height/2)/modelViewerOverlayHeight,
			drawn,
		)
		clicked := hovered.click(hovered.state)
		target := e.clickTarget(state, drawn, idle, hovered, clicked)

		// Prefer the first changed toggle over unchanged initializers. Keep a
		// clamped toggle as a fallback when the default is already at its bound.
		var hit *modelViewerOverlayVariable
		var transitions []ModelViewerOverlayTransition
		for _, key := range clicked.assigned {
			if entry, ok := e.tracked[key]; ok && !p.menuKeys[key] {
				candidate := hovered.transitions(entry.internal, entry.variable)
				if len(candidate) == 0 {
					continue
				}
				changed := modelViewerString(clicked.state[key]) != modelViewerString(hovered.state[key])
				if hit == nil || changed {
					hit = &entry
					transitions = candidate
				}
				if changed {
					break
				}
			}
		}
		if hit == nil && target < 0 {
			continue
		}

		button := ModelViewerOverlayButton{
			X: quad.x, Y: quad.y, Width: quad.width, Height: quad.height,
			Transitions: transitions,
			Hover:       modelViewerOverlayHoverChanges(idle.quads, hovered.quads),
		}
		if hit != nil {
			button.Variable = hit.variable.ID
		}
		if target >= 0 {
			button.Page = &target
		}
		key := fmt.Sprintf("%s:%v:%d", button.Variable, transitions, target)
		existing := -1
		for index := range buttons {
			// The two arrows of a two-page menu lead to one page from different places.
			if buttonKeys[index] == key && (target < 0 || modelViewerOverlayRectsOverlap(buttons[index], button)) {
				existing = index
				break
			}
		}
		if existing >= 0 {
			// A slot is usually an icon inside a frame; the frame takes the clicks.
			kept := &buttons[existing]
			if quad.width*quad.height <= kept.Width*kept.Height*4 {
				kept.X, kept.Y, kept.Width, kept.Height = quad.x, quad.y, quad.width, quad.height
			}
			continue
		}
		buttons = append(buttons, button)
		buttonKeys = append(buttonKeys, key)
		if hit != nil {
			buttonVariables = append(buttonVariables, *hit)
			if e.form == "" {
				e.form = hit.variable.Form
			}
		}
	}

	quads := make([]ModelViewerOverlayQuad, 0, len(idle.quads))
	for _, quad := range idle.quads {
		quads = append(quads, ModelViewerOverlayQuad{
			Path: quad.path, X: quad.x, Y: quad.y, Width: quad.width, Height: quad.height, Opacity: quad.opacity,
			FlipX: quad.flipX, FlipY: quad.flipY,
			Flipbook: idle.flipbook(quad),
		})
	}

	layers := make(map[int][]ModelViewerOverlayQuad)
	for _, entry := range buttonVariables {
		domain := modelViewerOverlayDomain(entry.variable)
		if len(domain) > maxModelViewerOverlayStateValues {
			continue
		}
		for _, value := range domain {
			if e.ctx.Err() != nil {
				return nil
			}
			current, err := modelViewerNumber(idle.state[entry.internal])
			if err == nil && current == value {
				continue
			}
			overrides := maps.Clone(drawn)
			overrides[entry.internal] = value
			changed := p.trace(-1, -1, overrides)
			if !modelViewerOverlaySameQuads(changed.quads, idle.quads, true) {
				addModelViewerOverlayLayers(quads, layers, idle, changed, entry.variable.ID, value)
				continue
			}
			for index, quad := range changed.quads {
				if quad.path != idle.quads[index].path || quad.opacity != idle.quads[index].opacity {
					quads[index].States = append(quads[index].States, ModelViewerOverlayQuadState{
						Variable: entry.variable.ID, Value: value, Path: quad.path, Opacity: quad.opacity,
					})
				}
			}
		}
	}
	quads = insertModelViewerOverlayLayers(quads, layers, buttons, sliders)
	return &modelViewerOverlayPageBuild{
		page:  ModelViewerOverlayPage{Quads: quads, Buttons: buttons, Sliders: sliders},
		drawn: idle.quads,
		state: drawn,
	}
}

// findSliderKeys keeps the listed sliders that something other than the click list
// reads, sparing the rest the traces that measure a slider on each page.
func (e *modelViewerOverlayExplorer) findSliderKeys() {
	read := make(map[string]bool)
	for _, lines := range e.program.lists {
		if slices.Equal(lines, e.program.clickLines) {
			continue
		}
		for _, line := range lines {
			for _, match := range modelViewerVariableRE.FindAllStringSubmatch(line, -1) {
				read[modelViewerNormalizeKey(match[1])] = true
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(e.tracked)) {
		slider := e.tracked[key].variable.Slider
		if read[key] && slider != nil && slider.Max > slider.Min {
			e.sliderKeys = append(e.sliderKeys, key)
		}
	}
}

// pageSliders finds the listed sliders whose value moves quads of this page, and
// returns the state to draw the page from. Each slider is measured at inner values
// and held at its middle there: a fill has no size at the end of its range, and a
// quad without a size is not drawn at all.
func (e *modelViewerOverlayExplorer) pageSliders(state map[string]any) (map[string]any, []ModelViewerOverlaySlider) {
	valueAt := func(slider *ModelViewerSlider, position float64) float64 {
		return slider.Min + (slider.Max-slider.Min)*position
	}
	if len(e.sliderKeys) == 0 {
		return state, nil
	}
	all := maps.Clone(state)
	for _, key := range e.sliderKeys {
		all[key] = valueAt(e.tracked[key].variable.Slider, 0.5)
	}

	drawn := maps.Clone(state)
	var sliders []ModelViewerOverlaySlider
	measured := -1
	for _, key := range e.sliderKeys {
		entry := e.tracked[key]
		trace := func(position float64) *modelViewerOverlayRun {
			probe := maps.Clone(all)
			probe[key] = valueAt(entry.variable.Slider, position)
			return e.program.tryTrace(probe)
		}
		low, high := trace(0.25), trace(0.75)
		if low == nil || high == nil || len(low.quads) != len(high.quads) {
			continue
		}
		slider := ModelViewerOverlaySlider{Handle: -1, Modes: []ModelViewerOverlaySliderMode{{
			Variable: entry.variable.ID, Label: entry.variable.Label,
			Min: entry.variable.Slider.Min, Max: entry.variable.Slider.Max,
		}}}
		for index := range low.quads {
			start, end := low.quads[index], high.quads[index]
			if start.x == end.x && start.width == end.width {
				continue
			}
			if start.width == end.width {
				slider.Handle = index
			}
			slider.Parts = append(slider.Parts, ModelViewerOverlaySliderPart{
				Quad:       index,
				StartX:     start.x - (end.x-start.x)/2,
				StartWidth: start.width - (end.width-start.width)/2,
				EndX:       end.x + (end.x-start.x)/2,
				EndWidth:   end.width + (end.width-start.width)/2,
			})
		}
		if slider.Handle >= 0 {
			drawn[key] = all[key]
			sliders = append(sliders, slider)
			measured = len(low.quads)
		}
	}
	if len(sliders) == 0 {
		return state, nil
	}
	// The parts index the quads drawn with every slider in the middle. A slider left
	// at its own value may hide a sizeless fill and shift the quads after it.
	if run := e.program.tryTrace(drawn); run == nil || len(run.quads) != measured {
		return all, sliders
	}
	return drawn, sliders
}

// addModelViewerOverlayLayers records a value that draws a different set of quads:
// the ones it no longer draws turn invisible for it, and the ones only it draws
// become hidden layers shown for it. layers holds them by the quad they go below.
func addModelViewerOverlayLayers(
	quads []ModelViewerOverlayQuad,
	layers map[int][]ModelViewerOverlayQuad,
	idle, changed *modelViewerOverlayRun,
	variable string,
	value float64,
) {
	matches := modelViewerOverlayAlignQuads(idle.quads, changed.quads)
	matchedChanged := make(map[int]bool, len(matches))
	matchedIdle := make(map[int]int, len(matches))
	for _, match := range matches {
		matchedIdle[match[0]] = match[1]
		matchedChanged[match[1]] = true
	}
	for index, quad := range idle.quads {
		if _, kept := matchedIdle[index]; !kept {
			quads[index].States = append(quads[index].States, ModelViewerOverlayQuadState{
				Variable: variable, Value: value, Path: quad.path,
			})
		}
	}

	next := 0
	for index, quad := range changed.quads {
		if matchedChanged[index] {
			continue
		}
		for next < len(matches) && matches[next][1] < index {
			next++
		}
		below := len(idle.quads)
		if next < len(matches) {
			below = matches[next][0]
		}
		state := ModelViewerOverlayQuadState{Variable: variable, Value: value, Path: quad.path, Opacity: quad.opacity}
		existing := slices.IndexFunc(layers[below], func(layer ModelViewerOverlayQuad) bool {
			return layer.X == quad.x && layer.Y == quad.y && layer.Width == quad.width && layer.Height == quad.height &&
				layer.Path == quad.path && layer.States[0].Variable == variable
		})
		if existing >= 0 {
			layers[below][existing].States = append(layers[below][existing].States, state)
			continue
		}
		layers[below] = append(layers[below], ModelViewerOverlayQuad{
			Path: quad.path, X: quad.x, Y: quad.y, Width: quad.width, Height: quad.height,
			FlipX: quad.flipX, FlipY: quad.flipY,
			States: []ModelViewerOverlayQuadState{state},
		})
	}
}

// modelViewerOverlayAlignQuads returns the longest common subsequence of two draw
// lists as index pairs in draw order.
func modelViewerOverlayAlignQuads(a, b []modelViewerOverlayTracedQuad) [][2]int {
	same := func(left, right modelViewerOverlayTracedQuad) bool {
		return left.path == right.path && left.x == right.x && left.y == right.y &&
			left.width == right.width && left.height == right.height
	}
	lengths := make([][]int, len(a)+1)
	for index := range lengths {
		lengths[index] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if same(a[i], b[j]) {
				lengths[i][j] = lengths[i+1][j+1] + 1
			} else {
				lengths[i][j] = max(lengths[i+1][j], lengths[i][j+1])
			}
		}
	}
	var matches [][2]int
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case same(a[i], b[j]):
			matches = append(matches, [2]int{i, j})
			i++
			j++
		case lengths[i+1][j] >= lengths[i][j+1]:
			i++
		default:
			j++
		}
	}
	return matches
}

func insertModelViewerOverlayLayers(
	quads []ModelViewerOverlayQuad,
	layers map[int][]ModelViewerOverlayQuad,
	buttons []ModelViewerOverlayButton,
	sliders []ModelViewerOverlaySlider,
) []ModelViewerOverlayQuad {
	if len(layers) == 0 {
		return quads
	}
	moved := make([]int, len(quads))
	merged := make([]ModelViewerOverlayQuad, 0, len(quads)+len(layers))
	for index, quad := range quads {
		merged = append(merged, layers[index]...)
		moved[index] = len(merged)
		merged = append(merged, quad)
	}
	merged = append(merged, layers[len(quads)]...)
	for buttonIndex := range buttons {
		for hoverIndex := range buttons[buttonIndex].Hover {
			hover := &buttons[buttonIndex].Hover[hoverIndex]
			hover.Quad = moved[hover.Quad]
		}
	}
	for sliderIndex := range sliders {
		slider := &sliders[sliderIndex]
		slider.Handle = moved[slider.Handle]
		for partIndex := range slider.Parts {
			slider.Parts[partIndex].Quad = moved[slider.Parts[partIndex].Quad]
		}
	}
	return merged
}

func modelViewerOverlayRectsOverlap(a, b ModelViewerOverlayButton) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

// clickTarget returns the index of the page a click leads to, or -1, registering
// the page when it is new.
//
// A menu variable keeps what the click gave it and changes what is drawn. Its value
// comes from nothing but itself and constants, which sets a page number apart from
// a click marker copied from the hovered slot: that redraws the menu too, but it
// describes the click instead of a place in the menu.
func (e *modelViewerOverlayExplorer) clickTarget(
	state, drawn map[string]any,
	idle, hovered, clicked *modelViewerOverlayRun,
) int {
	p := e.program
	next := maps.Clone(state)
	changed := false
	visited := make(map[string]bool, len(clicked.assigned))
	for _, key := range clicked.assigned {
		if visited[key] {
			continue
		}
		visited[key] = true
		_, isListed := e.tracked[key]
		_, isCandidate := e.candidates[key]
		if isListed && !isCandidate || !modelViewerOverlaySelfContained(clicked.sources[key], key) {
			continue
		}
		value, err := modelViewerNumber(clicked.state[key])
		if err != nil || modelViewerOverlaySameValue(clicked.state[key], hovered.state[key]) {
			continue
		}
		probe := maps.Clone(drawn)
		probe[key] = value
		run := p.tryTrace(probe)
		if run == nil || !modelViewerOverlaySameValue(run.state[key], value) ||
			modelViewerOverlaySameQuads(run.quads, idle.quads, false) {
			continue
		}
		p.menuKeys[key] = true
		changed = true
		if initial, declared := p.initial[key]; modelViewerOverlaySameValue(initial, value) ||
			!declared && value == 0 {
			delete(next, key)
		} else {
			next[key] = value
		}
	}
	if !changed {
		return -1
	}

	index := slices.IndexFunc(e.states, func(known map[string]any) bool {
		return maps.EqualFunc(known, next, modelViewerOverlaySameValue)
	})
	if index < 0 {
		if len(e.states) >= maxModelViewerOverlayPages {
			return -1
		}
		index = len(e.states)
		e.states = append(e.states, next)
	}
	return index
}

func modelViewerOverlaySameValue(a, b any) bool {
	return modelViewerString(a) == modelViewerString(b)
}

func modelViewerOverlaySelfContained(source, key string) bool {
	if source == "" {
		return false
	}
	for _, name := range modelViewerOverlayNameRE.FindAllString(source, -1) {
		if modelViewerNormalizeKey(name) != key {
			return false
		}
	}
	return true
}

func modelViewerOverlaySameQuads(a, b []modelViewerOverlayTracedQuad, layoutOnly bool) bool {
	return slices.EqualFunc(a, b, func(left, right modelViewerOverlayTracedQuad) bool {
		return left.x == right.x && left.y == right.y && left.width == right.width && left.height == right.height &&
			(layoutOnly || left.path == right.path && left.opacity == right.opacity)
	})
}

// assembleModelViewerOverlayMenu moves the pages to their shared corner and closes
// the gaps of dropped ones. Without a toggle on any page the menu operates nothing
// in the viewer, and the regular controls stay on their own.
func assembleModelViewerOverlayMenu(
	pages []*modelViewerOverlayPageBuild,
	form string,
	stateVariables []string,
) *ModelViewerOverlayMenu {
	originX, originY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	shiftX := func(value float64) float64 { return roundModelViewerOverlay(value - originX) }
	kept := make([]int, len(pages))
	count := 0
	hasToggle := false
	for index, page := range pages {
		kept[index] = -1
		if page == nil {
			continue
		}
		kept[index] = count
		count++
		for _, quad := range page.page.Quads {
			left, top, right, bottom := modelViewerOverlayVisibleBounds(quad)
			originX, originY = min(originX, left), min(originY, top)
			maxX, maxY = max(maxX, right), max(maxY, bottom)
		}
		hasToggle = hasToggle || slices.ContainsFunc(page.page.Buttons, func(button ModelViewerOverlayButton) bool {
			return button.Variable != ""
		}) || len(page.page.Sliders) > 0
	}
	if !hasToggle {
		return nil
	}

	menu := &ModelViewerOverlayMenu{
		X:      roundModelViewerOverlay(originX),
		Y:      roundModelViewerOverlay(originY),
		Width:  roundModelViewerOverlay(maxX - originX),
		Height: roundModelViewerOverlay(maxY - originY),
		Form:   form,
		Pages:  make([]ModelViewerOverlayPage, 0, count),
	}
	if count > 1 {
		menu.stateVariables = stateVariables
	}
	for _, build := range pages {
		if build == nil {
			continue
		}
		page := build.page
		for index := range page.Quads {
			quad := &page.Quads[index]
			quad.X, quad.Y = roundModelViewerOverlay(quad.X-originX), roundModelViewerOverlay(quad.Y-originY)
			quad.Width, quad.Height = roundModelViewerOverlay(quad.Width), roundModelViewerOverlay(quad.Height)
		}
		buttons := make([]ModelViewerOverlayButton, 0, len(page.Buttons))
		for _, button := range page.Buttons {
			if button.Page != nil {
				target := kept[*button.Page]
				button.Page = &target
				if target < 0 {
					button.Page = nil
					if button.Variable == "" {
						continue
					}
				}
			}
			button.X, button.Y = roundModelViewerOverlay(button.X-originX), roundModelViewerOverlay(button.Y-originY)
			button.Width, button.Height = roundModelViewerOverlay(button.Width), roundModelViewerOverlay(button.Height)
			buttons = append(buttons, button)
		}
		page.Buttons = buttons
		for _, slider := range page.Sliders {
			for index := range slider.Parts {
				part := &slider.Parts[index]
				part.StartX, part.EndX = shiftX(part.StartX), shiftX(part.EndX)
				part.StartWidth = roundModelViewerOverlay(part.StartWidth)
				part.EndWidth = roundModelViewerOverlay(part.EndWidth)
			}
		}
		menu.Pages = append(menu.Pages, page)
	}
	return menu
}

// modelViewerOverlayVisibleBounds is the area a quad takes up in the menu. A
// backdrop covering the window is mostly empty, with the menu painted on one part.
func modelViewerOverlayVisibleBounds(quad ModelViewerOverlayQuad) (left, top, right, bottom float64) {
	left, top, right, bottom = quad.X, quad.Y, quad.X+quad.Width, quad.Y+quad.Height
	if quad.Width < modelViewerOverlayWidth || quad.Height < modelViewerOverlayHeight {
		return left, top, right, bottom
	}
	file, err := os.Open(quad.Path)
	if err != nil {
		return left, top, right, bottom
	}
	defer func() { _ = file.Close() }()
	decoded, _, err := image.Decode(file)
	if err != nil {
		return left, top, right, bottom
	}

	bounds := decoded.Bounds()
	minX, minY, maxX, maxY := bounds.Max.X, bounds.Max.Y, bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y += modelViewerOverlayAlphaSampleStep {
		for x := bounds.Min.X; x < bounds.Max.X; x += modelViewerOverlayAlphaSampleStep {
			if _, _, _, alpha := decoded.At(x, y).RGBA(); alpha > modelViewerOverlayVisibleAlpha {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(
					maxX,
					x+modelViewerOverlayAlphaSampleStep,
				), max(
					maxY,
					y+modelViewerOverlayAlphaSampleStep,
				)
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return left, top, right, bottom
	}
	scaleX, scaleY := quad.Width/float64(bounds.Dx()), quad.Height/float64(bounds.Dy())
	return quad.X + float64(minX-bounds.Min.X)*scaleX, quad.Y + float64(minY-bounds.Min.Y)*scaleY,
		quad.X + float64(min(maxX, bounds.Max.X)-bounds.Min.X)*scaleX,
		quad.Y + float64(min(maxY, bounds.Max.Y)-bounds.Min.Y)*scaleY
}

// modelViewerOverlayMenuOnlyVariables returns the listed variables nothing in the
// model reads, so that a menu may claim them as its own state.
func modelViewerOverlayMenuOnlyVariables(transport *ModelViewerTransport) map[string]bool {
	used := modelViewerDirectGatingVariables(transport.Meshes, nil)
	mark := func(id string) { used[modelViewerNormalizeKey(id)] = true }
	for _, mesh := range transport.Meshes {
		for _, target := range mesh.ShapeTargets {
			mark(target.Var)
			if target.Wave != nil {
				mark(target.Wave.SpeedVar)
				for _, group := range target.Wave.When {
					for _, clause := range group {
						mark(clause.Var)
					}
				}
			}
		}
	}
	for _, rule := range transport.StateRules {
		mark(rule.Var)
		for _, group := range rule.Conditions {
			for _, clause := range group {
				mark(clause.Var)
			}
		}
	}
	for _, clip := range transport.Animations {
		for _, id := range clip.VariableIDs {
			mark(id)
		}
	}
	for _, variable := range transport.Variables {
		for _, effect := range variable.Effects {
			mark(effect.Var)
			if effect.When != nil {
				mark(effect.When.Var)
			}
			for _, guard := range effect.Also {
				mark(guard.Var)
			}
		}
	}

	menuOnly := make(map[string]bool)
	for _, variable := range transport.Variables {
		id := modelViewerNormalizeKey(variable.ID)
		if !used[id] && len(variable.Effects) == 0 {
			menuOnly[id] = true
		}
	}
	return menuOnly
}

// withoutModelViewerOverlayStateVariables removes what the served menus keep as
// their own state from the regular controls.
func withoutModelViewerOverlayStateVariables(
	variables []ModelViewerVariable,
	menus []ModelViewerOverlayMenu,
) []ModelViewerVariable {
	return slices.DeleteFunc(variables, func(variable ModelViewerVariable) bool {
		return slices.ContainsFunc(menus, func(menu ModelViewerOverlayMenu) bool {
			return slices.Contains(menu.stateVariables, variable.ID)
		})
	})
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
		isDDS := strings.EqualFold(filepath.Ext(path), ".dds")
		if !isDDS && modelViewerOverlayKnownImageExtension(path) {
			return path, nil
		}
		if url, ok := served[path]; ok {
			return url, nil
		}
		// The renderer tells images apart by extension, which a renamed one does not have.
		if imageType := modelViewerOverlayImageType(path); !isDDS && imageType != modelViewerOverlayDDSType {
			url, err := t.protocol.StoreMemoryLoaderWithContentType(
				sessionID,
				"overlay-tex:"+strconv.Itoa(len(served)),
				imageType,
				func(context.Context) ([]byte, error) { return os.ReadFile(path) },
			)
			if err != nil {
				return "", err
			}
			served[path] = url
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
		for pageIndex := range menu.Pages {
			page := &menu.Pages[pageIndex]
			for quadIndex := range page.Quads {
				quad := &page.Quads[quadIndex]
				rewrite(&quad.Path)
				for stateIndex := range quad.States {
					rewrite(&quad.States[stateIndex].Path)
				}
			}
			for buttonIndex := range page.Buttons {
				for hoverIndex := range page.Buttons[buttonIndex].Hover {
					rewrite(&page.Buttons[buttonIndex].Hover[hoverIndex].Path)
				}
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

func modelViewerOverlayKnownImageExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".dds":
		return true
	}
	return false
}

// modelViewerOverlayImageType returns the content type of a file holding an image
// the renderer can show, or nothing. Mods also ship textures under other extensions.
func modelViewerOverlayImageType(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, 512)
	count, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return ""
	}
	if bytes.HasPrefix(header[:count], []byte("DDS ")) {
		return modelViewerOverlayDDSType
	}
	switch contentType := http.DetectContentType(header[:count]); contentType {
	case "image/png", "image/jpeg", "image/webp":
		return contentType
	}
	return ""
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

// tryTrace draws a state that may not be reproducible, without giving up on the
// page it is explored from. It returns nil for such a state.
func (p *modelViewerOverlayProgram) tryTrace(overrides map[string]any) *modelViewerOverlayRun {
	before := p.unsupported
	run := p.trace(-1, -1, overrides)
	failed := p.unsupported
	p.unsupported = before
	if failed {
		return nil
	}
	return run
}

func (p *modelViewerOverlayProgram) trace(
	cursorX, cursorY float64,
	overrides map[string]any,
) *modelViewerOverlayRun {
	run := p.newRun(p.initial, cursorX, cursorY)
	maps.Copy(run.state, overrides)
	for _, lines := range p.presents {
		run.exec(lines, 0, true)
	}
	return run
}

// newRun starts a run from a copy of the given state, with the window and the cursor set.
func (p *modelViewerOverlayProgram) newRun(state map[string]any, cursorX, cursorY float64) *modelViewerOverlayRun {
	run := &modelViewerOverlayRun{
		program: p,
		state:   cloneModelViewerState(state),
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
			isShader := strings.HasPrefix(target, "customshader")
			body, ok := r.program.lists[target]
			if ok {
				outer := r.drawing
				if isShader {
					r.drawing = target
				}
				r.exec(body, depth+1, false)
				r.drawing = outer
			}
			if isShader && !r.program.listDraws(target, make(map[string]bool)) {
				r.recordQuad(target)
			}
			continue
		}
		if r.drawing != "" && modelViewerOverlayIsDrawLine(line) {
			r.recordQuad(r.drawing)
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

func modelViewerOverlayIsDrawLine(line string) bool {
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	fields := strings.Fields(key)
	return len(fields) > 0 && strings.EqualFold(fields[len(fields)-1], "draw") &&
		(len(fields) == 1 || len(fields) == 2 && strings.EqualFold(fields[0], "pre"))
}

// listDraws reports whether a list holds a Draw line, also through the command
// lists it runs, whether or not its conditions let the line execute.
func (p *modelViewerOverlayProgram) listDraws(list string, visiting map[string]bool) bool {
	if visiting[list] {
		return false
	}
	visiting[list] = true
	return slices.ContainsFunc(p.lists[list], func(line string) bool {
		if modelViewerOverlayIsDrawLine(line) {
			return true
		}
		key, value, ok := strings.Cut(line, "=")
		target := modelViewerNormalizeKey(value)
		return ok && strings.EqualFold(strings.TrimSpace(key), "run") &&
			strings.HasPrefix(target, "commandlist") && p.listDraws(target, visiting)
	})
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
// inside, in the click list, or on a condition made only of page variables, a failed
// condition such as a hit test is real.
func (r *modelViewerOverlayRun) chooseBranch(
	branches []modelViewerDirectConditionalBranch,
) *modelViewerDirectConditionalBranch {
	var taken *modelViewerDirectConditionalBranch
	for index := range branches {
		if branches[index].expression == nil || evaluateModelViewerCondition(*branches[index].expression, r.state) {
			taken = &branches[index]
			break
		}
	}
	if r.opened || taken != nil && r.program.linesReachMenu(taken.lines) {
		return taken
	}
	for index := range branches {
		if branches[index].expression != nil && r.program.selectsPage(*branches[index].expression) {
			return taken
		}
	}
	// An else on a closed gate, such as the one registering a mod not yet enabled,
	// is taken as readily as a true condition and must not keep the menu shut.
	for index := range branches {
		if r.program.linesReachMenu(branches[index].lines) {
			return &branches[index]
		}
	}
	return taken
}

func (p *modelViewerOverlayProgram) selectsPage(expression string) bool {
	names := modelViewerOverlayNameRE.FindAllString(expression, -1)
	return len(names) > 0 && !slices.ContainsFunc(names, func(name string) bool {
		return !p.menuKeys[modelViewerNormalizeKey(name)]
	})
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
	if !modelViewerOverlayKnownImageExtension(path) && !r.program.renamedImages[path] {
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
	if shader.lowRegister != "" {
		left, bottom := number("x"+shader.lowRegister), number("y"+shader.lowRegister)
		right, top := number("x"+shader.highRegister), number("y"+shader.highRegister)
		quad.x = (left + 1) / 2 * modelViewerOverlayWidth
		quad.y = (1 - top) / 2 * modelViewerOverlayHeight
		quad.width = (right - left) / 2 * modelViewerOverlayWidth
		quad.height = (top - bottom) / 2 * modelViewerOverlayHeight
	}
	if quad.width == 0 || quad.height == 0 {
		return
	}
	// A negative size draws away from the corner with the texture mirrored.
	if quad.width < 0 {
		quad.x, quad.width, quad.flipX = quad.x+quad.width, -quad.width, true
	}
	if quad.height < 0 {
		quad.y, quad.height, quad.flipY = quad.y+quad.height, -quad.height, true
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
	text := p.shaderSource(list, "ps")
	if text == "" {
		return nil
	}
	texture := modelViewerOverlayTextureRE.FindStringSubmatch(text)
	if texture == nil {
		return nil
	}
	shader := &modelViewerOverlayShader{textureRegister: texture[1], opacity: 1}
	size := modelViewerOverlaySizeRE.FindStringSubmatch(text)
	shift := modelViewerOverlayShiftRE.FindStringSubmatch(text)
	if size != nil && shift != nil {
		shader.sizeRegister, shader.shiftRegister = size[1], shift[1]
	} else {
		shader.lowRegister, shader.highRegister = modelViewerOverlayCornerRegisters(p.shaderSource(list, "vs"))
		if shader.lowRegister == "" {
			return nil
		}
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

func (p *modelViewerOverlayProgram) shaderSource(list, stage string) string {
	relative := stripModelViewerQuotes(sectionValue(p.lists[list], stage))
	if relative == "" {
		return ""
	}
	path, err := resolveModelViewerResourcePath(p.folder, p.shaderDir, relative)
	if err != nil {
		return ""
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(source)
}

// modelViewerOverlayCornerRegisters reads a triangle strip the vertex shader builds
// from two IniParams registers, with the first vertex at the low corner.
func modelViewerOverlayCornerRegisters(source string) (low, high string) {
	vertices := modelViewerOverlayCornerRE.FindAllStringSubmatch(source, -1)
	if len(vertices) != 4 {
		return "", ""
	}
	low, high = vertices[0][1], vertices[3][1]
	expected := [4][2]string{{low, low}, {low, high}, {high, low}, {high, high}}
	for index, vertex := range vertices {
		if vertex[1] != expected[index][0] || vertex[2] != expected[index][1] {
			return "", ""
		}
	}
	return low, high
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
