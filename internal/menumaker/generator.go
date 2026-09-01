/**
 * Strict command generation is derived from XXMI-Menu-Maker.
 * Copyright (c) 2026 星念. MIT licensed; see internal/menumaker/NOTICE.md.
 */
package menumaker

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	baseSlotSize   = 64
	generatedBegin = "; BEGIN NAHIDA XXMI MENU MAKER"
	generatedEnd   = "; END NAHIDA XXMI MENU MAKER"
)

var (
	generatedSectionRe = regexp.MustCompile(`(?i)^(?:KeyGui(?:Menu|Hold|Click|RightClick)|CommandListGui(?:Dims|Menu|Bg|Slots|Slot|Click|RightClick|ActivateReset)|CustomShaderGuiDraw|ResourceGui(?:Bg|Title|Slot(?:Hover)?(?:On)?\d+(?:S\d+)?))$`)
	preservedMetaRe    = regexp.MustCompile(`(?i)^(key|condition|type|run|back|wrap|smart|transition|transition_type|delay|release_delay)\s*=`)
	ifLineRe           = regexp.MustCompile(`(?i)^if\b`)
	endifLineRe        = regexp.MustCompile(`(?i)^endif\b`)
	guiMenuRe          = regexp.MustCompile(`(?i)\$gui_menu\b`)
	activeVarRe        = regexp.MustCompile(`(?i)\$[A-Za-z0-9_]*active[A-Za-z0-9_]*`)
	legacyVarRe        = func() []*regexp.Regexp {
		out := make([]*regexp.Regexp, 0, 5)
		for _, name := range []string{"$gui_menu", "$gui_hover", "$gui_slot", "$gui_mx", "$gui_my"} {
			out = append(out, regexp.MustCompile(`(?i)global(?:\s+persist)?\s+`+regexp.QuoteMeta(name)+`\b`))
		}
		return out
	}()
	globalPrefixRe = regexp.MustCompile(`(?i)^\s*global\s+(?:persist\s+)?`)
)

func generatePreview(sourceText string, slots []MenuMakerSlot, settings MenuMakerSettings) MenuMakerGenerateResult {
	document := parseDocument(sourceText)
	geometry := calculateGeometry(slots, settings)
	constants := parseInitialConstants(document.Sections)
	iniText := generateINI(document, slots, settings, geometry, constants)
	groups := make([]MenuMakerSlotStateGroup, 0, len(slots))
	for _, slot := range slots {
		groups = append(groups, MenuMakerSlotStateGroup{
			SlotID: slot.ID,
			States: slotValueStates(slot, settings, constants),
		})
	}
	return MenuMakerGenerateResult{
		INIText:    iniText,
		Geometry:   geometry,
		SlotStates: groups,
		AssetPaths: assetPaths(slots, settings, constants),
	}
}

func generateINI(
	document MenuMakerDocument,
	slots []MenuMakerSlot,
	settings MenuMakerSettings,
	geometry MenuMakerGeometry,
	constants map[string]string,
) string {
	handlers := uniqueHandlers(slots)
	activeInputs := collectActiveInputs(document.Sections)
	activeCondition := strings.Join(activeInputs, " || ")
	activeVariables := []string{}
	for _, input := range activeInputs {
		if match := variablePrefixRe.FindString(input); match != "" {
			activeVariables = append(activeVariables, match)
		}
	}
	activeVariables = uniqueStrings(activeVariables)
	sections := stripGeneratedSections(stripMarkedGeneratedBlock(document.Sections), document.Handlers)
	sections = rewriteKeySections(sections, slots, settings)
	injected := injectPresent(sections, activeCondition, activeVariables, settings.ResetActiveOnPresent, handlers, settings)
	originalParts := make([]string, 0, len(injected.sections))
	for _, section := range injected.sections {
		originalParts = append(originalParts, strings.Join(section.Lines, "\n"))
	}
	original := strings.TrimSpace(strings.Join(originalParts, "\n"))
	return original + "\n\n" + buildGeneratedBlock(slots, handlers, geometry, settings, activeCondition, constants, injected.hasPresent) + "\n"
}

func calculateGeometry(slots []MenuMakerSlot, settings MenuMakerSettings) MenuMakerGeometry {
	active := []MenuMakerSlot{}
	for _, slot := range slots {
		if !slot.Skip {
			active = append(active, slot)
		}
	}
	columns := orInt(settings.Columns, 3)
	if columns < 1 {
		columns = 1
	}
	scale := math.Max(0.5, math.Min(2, orFloat(settings.PanelScale, 1)))
	slotSize := jsRound(baseSlotSize * scale)
	padding := jsRound(16 * scale)
	titleHeight := 0
	if strings.TrimSpace(settings.Title) != "" {
		titleHeight = jsRound(32 * scale)
	}
	scaledGap := jsRound(math.Max(0, float64(settings.Gap)) * scale)
	rows := 0
	if columns > 0 {
		rows = int(math.Ceil(float64(len(active)) / float64(columns)))
	}
	panelWidth := padding*2 + columns*slotSize + maxInt(0, columns-1)*scaledGap
	panelHeight := padding*2 + titleHeight + rows*slotSize + maxInt(0, rows-1)*scaledGap
	positions := make([]MenuMakerSlotPosition, 0, len(active))
	for index, slot := range active {
		positions = append(positions, MenuMakerSlotPosition{
			SlotID:     slot.ID,
			AssetIndex: index + 1,
			X:          padding + (index%columns)*(slotSize+scaledGap),
			Y:          padding + titleHeight + (index/columns)*(slotSize+scaledGap),
			Size:       slotSize,
		})
	}
	return MenuMakerGeometry{
		PanelWidth:  panelWidth,
		PanelHeight: panelHeight,
		SlotSize:    slotSize,
		Padding:     padding,
		TitleHeight: titleHeight,
		ScaledGap:   scaledGap,
		Slots:       positions,
	}
}

func slotActiveCondition(slot MenuMakerSlot, settings MenuMakerSettings, constants map[string]string) string {
	parts := []string{}
	for _, handler := range slot.Handlers {
		if value := handlerActiveCondition(handler, settings, constants); value != "" {
			parts = append(parts, value)
		}
	}
	parts = uniqueStrings(parts)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	wrapped := make([]string, 0, len(parts))
	for _, part := range parts {
		wrapped = append(wrapped, "("+part+")")
	}
	return strings.Join(wrapped, " || ")
}

func slotValueStates(slot MenuMakerSlot, settings MenuMakerSettings, constants map[string]string) []MenuMakerSlotValueState {
	var handler *MenuMakerHandler
	for index := range slot.Handlers {
		candidate := &slot.Handlers[index]
		if handlerActiveCondition(*candidate, settings, constants) != "" {
			handler = candidate
			break
		}
	}
	if handler == nil || len(handler.Assignments) == 0 {
		return []MenuMakerSlotValueState{}
	}
	assignment := handler.Assignments[0]
	typeName := effectiveType(*handler, settings)
	inactive := constants[strings.ToLower(assignment.Variable)]
	if inactive == "" {
		if typeName == "toggle" || typeName == "hold" {
			inactive = "0"
		} else if len(assignment.Values) > 0 {
			inactive = assignment.Values[0]
		} else {
			inactive = "0"
		}
	}
	onValues := assignment.Values
	if typeName == "toggle" || typeName == "hold" {
		value := "1"
		if len(assignment.Values) > 0 {
			value = assignment.Values[0]
		}
		onValues = []string{value}
	}
	values := uniqueStrings(append([]string{inactive}, onValues...))
	states := make([]MenuMakerSlotValueState, 0, len(values))
	for index, value := range values {
		resourceSuffix := ""
		fileSuffix := ""
		if index > 0 {
			resourceSuffix = "S" + strconv.Itoa(index+1)
			fileSuffix = "_s" + strconv.Itoa(index+1)
		}
		states = append(states, MenuMakerSlotValueState{
			Variable:       assignment.Variable,
			Value:          value,
			Active:         value != inactive,
			ResourceSuffix: resourceSuffix,
			FileSuffix:     fileSuffix,
		})
	}
	return states
}

func assetPaths(slots []MenuMakerSlot, settings MenuMakerSettings, constants map[string]string) []string {
	paths := []string{"res_gui/draw_2d.hlsl", "res_gui/bg.png"}
	if strings.TrimSpace(settings.Title) != "" {
		paths = append(paths, "res_gui/title.png")
	}
	index := 0
	for _, slot := range slots {
		if slot.Skip {
			continue
		}
		states := slotValueStates(slot, settings, constants)
		if len(states) == 0 {
			states = []MenuMakerSlotValueState{{}}
		}
		suffix := pad2(index + 1)
		for _, state := range states {
			paths = append(paths,
				"res_gui/slot_"+suffix+state.FileSuffix+".png",
				"res_gui/slot_hover_"+suffix+state.FileSuffix+".png",
			)
		}
		index++
	}
	return paths
}

func buildGeneratedBlock(
	slots []MenuMakerSlot,
	handlers []MenuMakerHandler,
	geometry MenuMakerGeometry,
	settings MenuMakerSettings,
	activeCondition string,
	constants map[string]string,
	hasPresent bool,
) string {
	activeSlots := []MenuMakerSlot{}
	for _, slot := range slots {
		if !slot.Skip {
			activeSlots = append(activeSlots, slot)
		}
	}
	lines := []string{
		generatedBegin,
		"; Generated by Nahida Desktop XXMI Menu Maker",
		"; Source and third-party notices: internal/menumaker/NOTICE.md",
		"",
	}
	if !hasPresent {
		if activeCondition != "" {
			lines = append(lines, "[Present]", "if $gui_menu && ("+activeCondition+")", "  run = CommandListGuiMenu", "endif")
		} else {
			lines = append(lines, "[Present]", "if $gui_menu", "  run = CommandListGuiMenu", "endif")
		}
		if settings.ResetActiveOnPresent {
			for _, variable := range activeVariablesFrom(activeCondition) {
				lines = append(lines, "post "+variable+" = 0")
			}
		}
		needsActivate := false
		for _, handler := range handlers {
			if effectiveType(handler, settings) == "activate" && len(handler.Assignments) > 0 {
				needsActivate = true
				break
			}
		}
		if needsActivate {
			lines = append(lines, "post run = CommandListGuiActivateReset")
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		"[Constants]",
		"global $gui_menu = 0",
		"global $gui_hover = 0",
		"global $gui_hovered = 0",
		"global $gui_clicked = 0",
		"global $gui_right_clicked = 0",
		"global $gui_slot = 0",
		"global $gui_hold = 0",
		"global $gui_drag = 0",
		"global $gui_ww",
		"global $gui_wh",
		"global $gui_cpx",
		"global $gui_cpy",
		"global $gui_cox",
		"global $gui_coy",
		"global persist $gui_mx = 0.05",
		"global persist $gui_my = 0.20",
	)
	for _, handler := range handlers {
		typeName := effectiveType(handler, settings)
		if isCycleType(typeName) && len(handler.Assignments) > 0 {
			lines = append(lines, "global "+handler.StepVar+" = 0")
		}
		if typeName == "activate" && len(handler.Assignments) > 0 {
			lines = append(lines, "global "+handler.ActivatePulseVar+" = 0")
		}
	}
	menuKey := strings.TrimSpace(settings.MenuKey)
	if menuKey == "" {
		menuKey = "alt"
	}
	lines = append(lines, "", "[KeyGuiMenu]")
	if activeCondition != "" {
		lines = append(lines, "condition = "+activeCondition)
	}
	lines = append(lines,
		"key = "+menuKey,
		"type = hold",
		"$gui_menu = 1",
		"",
		"[KeyGuiHold]",
		"condition = $gui_menu == 1 && $gui_hover == 0",
		"key = "+clickKey(settings.ClickModifier, false),
		"type = hold",
		"$gui_hold = 1",
		"",
		"[KeyGuiClick]",
		"condition = $gui_menu == 1 && $gui_hover == 1",
		"key = "+clickKey(settings.ClickModifier, false),
		"run = CommandListGuiClick",
		"",
		"[KeyGuiRightClick]",
		"condition = $gui_menu == 1 && $gui_hover == 1",
		"key = "+clickKey(settings.ClickModifier, true),
		"run = CommandListGuiRightClick",
		"",
		"[CommandListGuiDims]",
		"if window_width != 0",
		"  $gui_ww = window_width",
		"  $gui_wh = window_height",
		"elif rt_width != 0",
		"  $gui_ww = rt_width",
		"  $gui_wh = rt_height",
		"else",
		"  $gui_ww = res_width",
		"  $gui_wh = res_height",
		"endif",
		"if cursor_x != 0 && cursor_y != 0",
		"  $gui_cpx = cursor_x",
		"  $gui_cpy = cursor_y",
		"else",
		"  $gui_cpx = cursor_screen_x / $gui_ww",
		"  $gui_cpy = cursor_screen_y / $gui_wh",
		"endif",
		"",
		"[CommandListGuiMenu]",
		"run = CommandListGuiDims",
		"run = CommandListGuiBg",
		"run = CommandListGuiSlots",
		"",
		"[CommandListGuiBg]",
		"x87 = "+strconv.Itoa(geometry.PanelWidth)+" / $gui_ww",
		"y87 = "+strconv.Itoa(geometry.PanelHeight)+" / $gui_wh",
		"z87 = $gui_mx",
		"w87 = $gui_my",
		"if $gui_drag == 0",
		"  if $gui_hold && $gui_cpx > $gui_mx && $gui_cpx < ($gui_mx + x87) && $gui_cpy > $gui_my && $gui_cpy < ($gui_my + y87)",
		"    $gui_cox = $gui_cpx - $gui_mx",
		"    $gui_coy = $gui_cpy - $gui_my",
		"    $gui_drag = 1",
		"  endif",
		"else",
		"  if $gui_hold",
		"    $gui_mx = $gui_cpx - $gui_cox",
		"    $gui_my = $gui_cpy - $gui_coy",
		"  else",
		"    $gui_drag = 0",
		"  endif",
		"  z87 = $gui_mx",
		"  w87 = $gui_my",
		"endif",
		"ps-t100 = ResourceGuiBg",
		"run = CustomShaderGuiDraw",
	)
	if geometry.TitleHeight > 0 {
		scale := orFloat(settings.PanelScale, 1)
		lines = append(lines,
			"x87 = "+strconv.Itoa(geometry.PanelWidth-jsRound(6*scale))+" / $gui_ww",
			"y87 = "+strconv.Itoa(geometry.TitleHeight)+" / $gui_wh",
			"z87 = $gui_mx + "+strconv.Itoa(jsRound(3*scale))+" / $gui_ww",
			"w87 = $gui_my + "+strconv.Itoa(jsRound(3*scale))+" / $gui_wh",
			"ps-t100 = ResourceGuiTitle",
			"run = CustomShaderGuiDraw",
		)
	}
	lines = append(lines, "", "[CommandListGuiSlots]", "$gui_hover = 0", "$gui_hovered = 0", "$gui_slot = 0")
	for index, position := range geometry.Slots {
		lines = append(lines,
			"; slot "+strconv.Itoa(index+1)+": "+escapeComment(activeSlots[index].Name),
			"x87 = "+strconv.Itoa(position.Size)+" / $gui_ww",
			"y87 = "+strconv.Itoa(position.Size)+" / $gui_wh",
			"z87 = $gui_mx + "+strconv.Itoa(position.X)+" / $gui_ww",
			"w87 = $gui_my + "+strconv.Itoa(position.Y)+" / $gui_wh",
			"$gui_slot = "+strconv.Itoa(index+1),
			"run = CommandListGuiSlot",
		)
	}
	lines = append(lines,
		"",
		"[CommandListGuiSlot]",
		"if $gui_cpx > z87 && $gui_cpx < (z87 + x87) && $gui_cpy > w87 && $gui_cpy < (w87 + y87)",
		"  $gui_hover = 1",
		"  $gui_hovered = $gui_slot",
		"endif",
	)
	for index, slot := range activeSlots {
		prefix := "if"
		if index > 0 {
			prefix = "elif"
		}
		lines = append(lines, prefix+" $gui_slot == "+strconv.Itoa(index+1))
		emitSlotTextureChoice(&lines, index+1, slotValueStates(slot, settings, constants))
	}
	if len(activeSlots) > 0 {
		lines = append(lines, "endif")
	}
	lines = append(lines, "run = CustomShaderGuiDraw", "", "[CommandListGuiClick]", "$gui_clicked = $gui_hovered")
	for index, slot := range activeSlots {
		prefix := "if"
		if index > 0 {
			prefix = "elif"
		}
		lines = append(lines, prefix+" $gui_clicked == "+strconv.Itoa(index+1), "  ; "+escapeComment(slot.Name))
		for _, handler := range slot.Handlers {
			emitGUIHandler(&lines, handler, settings, "  ", false)
		}
	}
	if len(activeSlots) > 0 {
		lines = append(lines, "endif")
	}
	lines = append(lines, "", "[CommandListGuiRightClick]", "$gui_right_clicked = $gui_hovered")
	for index, slot := range activeSlots {
		prefix := "if"
		if index > 0 {
			prefix = "elif"
		}
		lines = append(lines, prefix+" $gui_right_clicked == "+strconv.Itoa(index+1), "  ; "+escapeComment(slot.Name))
		for _, handler := range slot.Handlers {
			emitGUIHandler(&lines, handler, settings, "  ", true)
		}
	}
	if len(activeSlots) > 0 {
		lines = append(lines, "endif", "")
	} else {
		lines = append(lines, "")
	}
	for _, handler := range handlers {
		emitHandlerCommandLists(&lines, handler, settings, constants)
	}
	emitActivateReset(&lines, handlers, settings, constants)
	lines = append(lines,
		"[CustomShaderGuiDraw]",
		"vs = res_gui\\draw_2d.hlsl",
		"ps = res_gui\\draw_2d.hlsl",
		"blend = ADD SRC_ALPHA INV_SRC_ALPHA",
		"cull = none",
		"topology = triangle_strip",
		"run = BuiltInCommandListUnbindAllRenderTargets",
		"o0 = set_viewport bb",
		"draw = 4, 0",
		"",
		"[ResourceGuiBg]",
		"filename = res_gui\\bg.png",
		"",
	)
	if geometry.TitleHeight > 0 {
		lines = append(lines, "[ResourceGuiTitle]", "filename = res_gui\\title.png", "")
	}
	for index, slot := range activeSlots {
		assetIndex := pad2(index + 1)
		states := slotValueStates(slot, settings, constants)
		if len(states) == 0 {
			states = []MenuMakerSlotValueState{{}}
		}
		for _, state := range states {
			lines = append(lines,
				"["+slotResourceName(index+1, false, state.ResourceSuffix)+"]",
				"filename = "+slotAssetFilename(assetIndex, false, state.FileSuffix),
				"",
				"["+slotResourceName(index+1, true, state.ResourceSuffix)+"]",
				"filename = "+slotAssetFilename(assetIndex, true, state.FileSuffix),
				"",
			)
		}
	}
	lines = append(lines, generatedEnd)
	return strings.Join(lines, "\n")
}

func emitSlotTextureChoice(lines *[]string, slotNumber int, states []MenuMakerSlotValueState) {
	extras := []MenuMakerSlotValueState{}
	if len(states) > 1 {
		extras = states[1:]
	}
	emitBranch := func(hover bool, indent string) {
		if len(extras) == 0 {
			*lines = append(*lines, indent+"ps-t100 = "+slotResourceName(slotNumber, hover, ""))
			return
		}
		for index, state := range extras {
			prefix := "if"
			if index > 0 {
				prefix = "elif"
			}
			*lines = append(*lines,
				indent+prefix+" "+state.Variable+" == "+state.Value,
				indent+"  ps-t100 = "+slotResourceName(slotNumber, hover, state.ResourceSuffix),
			)
		}
		*lines = append(*lines,
			indent+"else",
			indent+"  ps-t100 = "+slotResourceName(slotNumber, hover, ""),
			indent+"endif",
		)
	}
	*lines = append(*lines, "  if $gui_hovered == "+strconv.Itoa(slotNumber))
	emitBranch(true, "    ")
	*lines = append(*lines, "  else")
	emitBranch(false, "    ")
	*lines = append(*lines, "  endif")
}

func slotResourceName(slotNumber int, hover bool, suffix string) string {
	name := "ResourceGuiSlot"
	if hover {
		name = "ResourceGuiSlotHover"
	}
	return name + strconv.Itoa(slotNumber) + suffix
}

func slotAssetFilename(assetIndex string, hover bool, fileSuffix string) string {
	if hover {
		return "res_gui\\slot_hover_" + assetIndex + fileSuffix + ".png"
	}
	return "res_gui\\slot_" + assetIndex + fileSuffix + ".png"
}

func handlerActiveCondition(handler MenuMakerHandler, settings MenuMakerSettings, constants map[string]string) string {
	typeName := effectiveType(handler, settings)
	if !isCycleType(typeName) || len(handler.Assignments) == 0 {
		return ""
	}
	assignment := handler.Assignments[0]
	if typeName == "toggle" || typeName == "hold" {
		value := "1"
		if len(assignment.Values) > 0 {
			value = assignment.Values[0]
		}
		return assignment.Variable + " == " + value
	}
	inactive := constants[strings.ToLower(assignment.Variable)]
	if inactive == "" {
		if len(assignment.Values) > 0 {
			inactive = assignment.Values[0]
		} else {
			inactive = "0"
		}
	}
	return assignment.Variable + " != " + inactive
}

func rewriteKeySections(sections []MenuMakerSection, slots []MenuMakerSlot, settings MenuMakerSettings) []MenuMakerSection {
	owned := map[int]struct {
		handler MenuMakerHandler
		slot    MenuMakerSlot
	}{}
	for _, slot := range slots {
		for _, handler := range slot.Handlers {
			owned[handler.SourceIndex] = struct {
				handler MenuMakerHandler
				slot    MenuMakerSlot
			}{handler: handler, slot: slot}
		}
	}
	out := make([]MenuMakerSection, 0, len(sections))
	for _, section := range sections {
		item, ok := owned[section.Index]
		if !ok {
			out = append(out, section)
			continue
		}
		typeName := effectiveType(item.handler, settings)
		if typeName == "activate" {
			if !settings.RemoveOriginalKeys {
				out = append(out, section)
			}
			continue
		}
		if settings.RemoveOriginalKeys {
			continue
		}
		if item.slot.MergeMode == "guiOnly" {
			out = append(out, section)
			continue
		}
		keys := item.handler.Keys
		if item.slot.MergeMode == "allKeys" {
			keys = item.slot.OriginalKeys
		}
		rewritten := rewriteKeySection(section, item.handler, keys, item.handler.CommandName, false)
		out = append(out, rewritten)
		if typeName == "cycle" && item.handler.Back != "" && len(item.handler.Assignments) > 0 {
			out = append(out, rewriteKeySection(section, item.handler, []string{item.handler.Back}, item.handler.BackCommandName, true))
		}
	}
	return out
}

func rewriteKeySection(section MenuMakerSection, handler MenuMakerHandler, keys []string, commandName string, back bool) MenuMakerSection {
	name := ""
	if section.Name != nil {
		name = *section.Name
	}
	if back {
		name += "Back"
	}
	header := "[" + name + "]"
	lines := []string{header}
	if back {
		lines = append(lines, generatedReverseMarker)
	} else {
		lines = append(lines, originalLinesPrefix+encodeURIComponent(marshalJSON(originalSemanticLines(section))))
	}
	for _, key := range keys {
		if key != "" {
			lines = append(lines, "key = "+key)
		}
	}
	if handler.Condition != "" {
		lines = append(lines, "condition = "+handler.Condition)
	}
	if !back {
		lines = append(lines, preservedLines(section)...)
	}
	lines = append(lines, "run = "+commandName)
	sectionName := name
	return MenuMakerSection{Name: &sectionName, Lines: lines, Index: section.Index}
}

func preservedLines(section MenuMakerSection) []string {
	if len(section.Lines) == 0 {
		return nil
	}
	out := []string{}
	for _, line := range section.Lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), originalLinesPrefix) {
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			out = append(out, line)
			continue
		}
		if preservedMetaRe.MatchString(trimmed) {
			continue
		}
		if assignRe.MatchString(stripComment(trimmed)) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func originalSemanticLines(section MenuMakerSection) []string {
	for _, line := range section.Lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), originalLinesPrefix) {
			continue
		}
		decoded, err := decodeURIComponent(trimmed[len(originalLinesPrefix):])
		if err != nil {
			break
		}
		var parsed any
		if json.Unmarshal([]byte(decoded), &parsed) != nil {
			break
		}
		items, ok := parsed.([]any)
		if !ok {
			break
		}
		out := make([]string, 0, len(items))
		valid := true
		for _, item := range items {
			text, isString := item.(string)
			if !isString {
				valid = false
				break
			}
			out = append(out, text)
		}
		if valid {
			return out
		}
		break
	}
	if len(section.Lines) == 0 {
		return []string{}
	}
	out := []string{}
	for _, line := range section.Lines[1:] {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), originalLinesPrefix) {
			continue
		}
		out = append(out, line)
	}
	return out
}

type presentInjection struct {
	sections   []MenuMakerSection
	hasPresent bool
}

func injectPresent(
	sections []MenuMakerSection,
	activeCondition string,
	activeVariables []string,
	resetActive bool,
	handlers []MenuMakerHandler,
	settings MenuMakerSettings,
) presentInjection {
	hasPresent := false
	needsActivateReset := false
	for _, handler := range handlers {
		if effectiveType(handler, settings) == "activate" && len(handler.Assignments) > 0 {
			needsActivateReset = true
			break
		}
	}
	output := make([]MenuMakerSection, 0, len(sections))
	for _, section := range sections {
		if section.Name == nil || strings.ToLower(*section.Name) != "present" {
			output = append(output, section)
			continue
		}
		hasPresent = true
		lines := stripGeneratedPresent(section.Lines)
		inserted := []string{"if $gui_menu", "  run = CommandListGuiMenu", "endif", ""}
		if activeCondition != "" {
			inserted[0] = "if $gui_menu && (" + activeCondition + ")"
		}
		if len(lines) == 0 {
			lines = appendedAt(lines, 0, inserted)
		} else {
			lines = appendedAt(lines, 1, inserted)
		}
		if resetActive {
			for _, variable := range activeVariables {
				reset := "post " + variable + " = 0"
				found := false
				for _, line := range lines {
					if strings.EqualFold(strings.TrimSpace(line), reset) {
						found = true
						break
					}
				}
				if !found {
					lines = append(lines, reset)
				}
			}
		}
		if needsActivateReset {
			found := false
			for _, line := range lines {
				if strings.EqualFold(strings.TrimSpace(line), "post run = commandlistguiactivatereset") {
					found = true
					break
				}
			}
			if !found {
				lines = append(lines, "post run = CommandListGuiActivateReset")
			}
		}
		section.Lines = lines
		output = append(output, section)
	}
	return presentInjection{sections: output, hasPresent: hasPresent}
}

func appendedAt(lines []string, index int, inserted []string) []string {
	if index > len(lines) {
		index = len(lines)
	}
	out := make([]string, 0, len(lines)+len(inserted))
	out = append(out, lines[:index]...)
	out = append(out, inserted...)
	out = append(out, lines[index:]...)
	return out
}

func stripGeneratedPresent(lines []string) []string {
	if len(lines) == 0 {
		return []string{}
	}
	output := []string{lines[0]}
	for index := 1; index < len(lines); {
		if !ifLineRe.MatchString(strings.TrimSpace(lines[index])) || !guiMenuRe.MatchString(lines[index]) {
			output = append(output, lines[index])
			index++
			continue
		}
		depth := 0
		end := -1
		for cursor := index; cursor < len(lines); cursor++ {
			if ifLineRe.MatchString(strings.TrimSpace(lines[cursor])) {
				depth++
			}
			if endifLineRe.MatchString(strings.TrimSpace(lines[cursor])) {
				depth--
				if depth == 0 {
					end = cursor
					break
				}
			}
		}
		inner := []string{}
		if end >= 0 {
			for _, line := range lines[index+1 : end] {
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					inner = append(inner, trimmed)
				}
			}
		}
		if end < 0 || len(inner) != 1 || strings.ToLower(inner[0]) != "run = commandlistguimenu" {
			output = append(output, lines[index])
			index++
			continue
		}
		index = end + 1
		if index < len(lines) && strings.TrimSpace(lines[index]) == "" {
			index++
		}
	}
	return output
}

func emitGUIHandler(lines *[]string, handler MenuMakerHandler, settings MenuMakerSettings, indent string, reverse bool) {
	typeName := effectiveType(handler, settings)
	if reverse && !hasReverseCommand(handler, settings) {
		return
	}
	command := handler.CommandName
	if reverse {
		command = handler.BackCommandName
	} else if typeName == "activate" {
		command = handler.ActivateCommandName
	}
	if handler.Condition != "" {
		*lines = append(*lines, indent+"if "+handler.Condition)
		*lines = append(*lines, indent+"  run = "+command)
		*lines = append(*lines, indent+"endif")
		return
	}
	*lines = append(*lines, indent+"run = "+command)
}

func emitHandlerCommandLists(lines *[]string, handler MenuMakerHandler, settings MenuMakerSettings, constants map[string]string) {
	typeName := effectiveType(handler, settings)
	if typeName == "activate" {
		*lines = append(*lines, "["+handler.ActivateCommandName+"]")
		emitActivateEntries(lines, "", handler.Entries)
		if len(handler.Assignments) > 0 {
			*lines = append(*lines, handler.ActivatePulseVar+" = 1")
		}
		*lines = append(*lines, "")
		return
	}
	*lines = append(*lines, "["+handler.CommandName+"]")
	if isCycleType(typeName) && len(handler.Assignments) > 0 {
		emitCycle(lines, handler, typeName, constants, 1)
	} else {
		emitRunEntries(lines, "", handler.Entries)
	}
	*lines = append(*lines, "")
	if hasReverseCommand(handler, settings) {
		*lines = append(*lines, "["+handler.BackCommandName+"]")
		emitCycle(lines, handler, typeName, constants, -1)
		*lines = append(*lines, "")
	}
}

func emitCycle(lines *[]string, handler MenuMakerHandler, typeName string, constants map[string]string, direction int) {
	steps := handler.Steps
	if typeName == "toggle" || typeName == "hold" {
		steps = 2
	}
	if steps < 1 {
		steps = 1
	}
	op := "+"
	if direction < 0 {
		op = "-"
	}
	*lines = append(*lines, handler.StepVar+" = "+handler.StepVar+" "+op+" 1")
	if direction < 0 {
		*lines = append(*lines, "if "+handler.StepVar+" < 0")
		wrapValue := 0
		if handler.Wrap {
			wrapValue = steps - 1
		}
		*lines = append(*lines, "  "+handler.StepVar+" = "+strconv.Itoa(wrapValue), "endif")
	} else {
		*lines = append(*lines, "if "+handler.StepVar+" >= "+strconv.Itoa(steps))
		wrapValue := steps - 1
		if handler.Wrap {
			wrapValue = 0
		}
		*lines = append(*lines, "  "+handler.StepVar+" = "+strconv.Itoa(wrapValue), "endif")
	}
	for step := range steps {
		prefix := "if"
		if step > 0 {
			prefix = "elif"
		}
		*lines = append(*lines, prefix+" "+handler.StepVar+" == "+strconv.Itoa(step))
		emitStepEntries(lines, "  ", handler.Entries, step, typeName, constants)
	}
	*lines = append(*lines, "endif")
}

func emitStepEntries(lines *[]string, indent string, entries []MenuMakerEntry, step int, typeName string, constants map[string]string) {
	for _, entry := range entries {
		switch entry.Kind {
		case "assign":
			*lines = append(*lines, indent+entry.Variable+" = "+assignStepValue(entry, step, typeName, constants))
		case "run":
			*lines = append(*lines, indent+"run = "+entry.Target)
		default:
			*lines = append(*lines, indent+entry.Line)
		}
	}
}

func assignStepValue(entry MenuMakerEntry, step int, typeName string, constants map[string]string) string {
	if (typeName == "toggle" || typeName == "hold") && step == 0 {
		if stored, ok := constants[strings.ToLower(entry.Variable)]; ok {
			return stored
		}
		return "0"
	}
	index := step
	if typeName == "toggle" || typeName == "hold" {
		index = 0
	}
	if index < len(entry.Values) {
		return entry.Values[index]
	}
	return lastOr(entry.Values, "0")
}

func emitRunEntries(lines *[]string, indent string, entries []MenuMakerEntry) {
	for _, entry := range entries {
		switch entry.Kind {
		case "assign":
			*lines = append(*lines, indent+entry.Variable+" = "+lastOr(entry.Values, "0"))
		case "run":
			*lines = append(*lines, indent+"run = "+entry.Target)
		default:
			*lines = append(*lines, indent+entry.Line)
		}
	}
}

func emitActivateEntries(lines *[]string, indent string, entries []MenuMakerEntry) {
	for _, entry := range entries {
		switch entry.Kind {
		case "assign":
			value := "0"
			if len(entry.Values) > 0 {
				value = entry.Values[0]
			}
			*lines = append(*lines, indent+entry.Variable+" = "+value)
		case "run":
			*lines = append(*lines, indent+"run = "+entry.Target)
		default:
			*lines = append(*lines, indent+entry.Line)
		}
	}
}

func emitActivateReset(lines *[]string, handlers []MenuMakerHandler, settings MenuMakerSettings, constants map[string]string) {
	active := []MenuMakerHandler{}
	for _, handler := range handlers {
		if effectiveType(handler, settings) == "activate" && len(handler.Assignments) > 0 {
			active = append(active, handler)
		}
	}
	if len(active) == 0 {
		return
	}
	*lines = append(*lines, "[CommandListGuiActivateReset]")
	for _, handler := range active {
		*lines = append(*lines, "if "+handler.ActivatePulseVar+" == 1")
		for _, assignment := range handler.Assignments {
			value := constants[strings.ToLower(assignment.Variable)]
			if value == "" {
				value = "0"
			}
			*lines = append(*lines, "  "+assignment.Variable+" = "+value)
		}
		*lines = append(*lines, "  "+handler.ActivatePulseVar+" = 0", "endif")
	}
	*lines = append(*lines, "")
}

func parseInitialConstants(sections []MenuMakerSection) map[string]string {
	values := map[string]string{}
	for _, section := range sections {
		if section.Name == nil || strings.ToLower(*section.Name) != "constants" {
			continue
		}
		if len(section.Lines) == 0 {
			continue
		}
		for _, line := range section.Lines[1:] {
			body := strings.TrimSpace(globalPrefixRe.ReplaceAllString(stripComment(line), ""))
			match := assignRe.FindStringSubmatch(body)
			if match == nil {
				continue
			}
			key := strings.ToLower(match[1])
			if _, exists := values[key]; !exists {
				values[key] = strings.TrimSpace(match[2])
			}
		}
	}
	return values
}

func stripMarkedGeneratedBlock(sections []MenuMakerSection) []MenuMakerSection {
	parts := make([]string, 0, len(sections))
	for _, section := range sections {
		parts = append(parts, strings.Join(section.Lines, "\n"))
	}
	text := strings.Join(parts, "\n")
	start := strings.Index(text, generatedBegin)
	end := -1
	if start >= 0 {
		end = strings.Index(text[start:], generatedEnd)
		if end >= 0 {
			end += start
		}
	}
	if start < 0 || end < 0 {
		return sections
	}
	left := strings.TrimRightFunc(text[:start], unicode.IsSpace)
	right := strings.TrimLeftFunc(text[end+len(generatedEnd):], unicode.IsSpace)
	return parseSections(left + "\n" + right)
}

func stripGeneratedSections(sections []MenuMakerSection, handlers []MenuMakerHandler) []MenuMakerSection {
	handlerSections := map[string]bool{}
	for _, handler := range handlers {
		for _, name := range []string{handler.CommandName, handler.BackCommandName, handler.ActivateCommandName} {
			handlerSections[strings.ToLower(name)] = true
		}
	}
	out := make([]MenuMakerSection, 0, len(sections))
	for _, section := range sections {
		if section.Name == nil {
			out = append(out, section)
			continue
		}
		if generatedSectionRe.MatchString(*section.Name) || handlerSections[strings.ToLower(*section.Name)] {
			continue
		}
		if strings.ToLower(*section.Name) == "constants" && isLegacyGeneratedConstants(section) {
			continue
		}
		out = append(out, section)
	}
	return out
}

func isLegacyGeneratedConstants(section MenuMakerSection) bool {
	body := strings.Join(section.Lines, "\n")
	for _, pattern := range legacyVarRe {
		if !pattern.MatchString(body) {
			return false
		}
	}
	return true
}

func effectiveType(handler MenuMakerHandler, settings MenuMakerSettings) string {
	typeName := strings.ToLower(handler.Type)
	switch typeName {
	case "cycle", "toggle", "hold", "activate":
		return typeName
	default:
		fallback := strings.ToLower(settings.FallbackType)
		if fallback == "" {
			return "cycle"
		}
		return fallback
	}
}

func isCycleType(typeName string) bool {
	return typeName == "cycle" || typeName == "toggle" || typeName == "hold"
}

func hasReverseCommand(handler MenuMakerHandler, settings MenuMakerSettings) bool {
	return isCycleType(effectiveType(handler, settings)) && len(handler.Assignments) > 0
}

func clickKey(modifier string, right bool) string {
	button := "VK_LBUTTON"
	if right {
		button = "VK_RBUTTON"
	}
	switch modifier {
	case "ctrl":
		return "no_alt no_shift ctrl " + button
	case "shift":
		return "no_alt shift no_ctrl " + button
	case "none":
		return "no_ctrl no_shift no_alt " + button
	default:
		return "no_ctrl no_shift alt " + button
	}
}

func uniqueHandlers(slots []MenuMakerSlot) []MenuMakerHandler {
	seen := map[int]bool{}
	out := []MenuMakerHandler{}
	for _, slot := range slots {
		for _, handler := range slot.Handlers {
			if seen[handler.SourceIndex] {
				continue
			}
			seen[handler.SourceIndex] = true
			out = append(out, handler)
		}
	}
	return out
}

func uniqueNormalizedKeys(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		normalized := normalizeMenuMakerKey(value)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, value)
	}
	return out
}

func activeVariablesFrom(condition string) []string {
	matches := activeVarRe.FindAllString(condition, -1)
	return uniqueStrings(matches)
}

func escapeComment(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.ReplaceAll(value, "\r", " ")
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func orInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func orFloat(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func jsRound(value float64) int {
	return int(math.Round(value))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func pad2(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func lastOr(values []string, fallback string) string {
	if len(values) == 0 {
		return fallback
	}
	return values[len(values)-1]
}

func marshalJSON(value any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "[]"
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
