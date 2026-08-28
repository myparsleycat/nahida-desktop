package tools

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ModelViewerSlider struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

type ModelViewerUIAssets struct {
	BackgroundPath string `json:"backgroundPath,omitempty"`
	SlotPath       string `json:"slotPath,omitempty"`
	SlotHoverPath  string `json:"slotHoverPath,omitempty"`
	SlotActivePath string `json:"slotActivePath,omitempty"`
}

type modelViewerSlotBinding struct {
	Slot          int
	Variable      string
	Values        []any
	Effects       []ModelViewerMenuEffect
	AlwaysVisible bool
}

const maxModelViewerMenuValues = 4096

type modelViewerMenuSlotBranch struct {
	slotVariable string
	slot         int
	lines        []string
}

var (
	modelViewerMenuSlotRE    = regexp.MustCompile(`(?i)^\$([\w.]+)\s*={2,3}\s*(\d+)$`)
	modelViewerMenuAssignRE  = regexp.MustCompile(`(?i)^\$([\w.]+)\s*=\s*(.+)$`)
	modelViewerMenuFlipRE    = regexp.MustCompile(`(?i)^1\s*-\s*\$([\w.]+)$`)
	modelViewerMenuIncrRE    = regexp.MustCompile(`(?i)^\$([\w.]+)\s*\+\s*1$`)
	modelViewerMenuIncrModRE = regexp.MustCompile(`(?i)^\(\s*\$([\w.]+)\s*\+\s*1\s*\)\s*%\s*(\d+)$`)
	modelViewerMenuModRE     = regexp.MustCompile(`(?i)^\$([\w.]+)\s*%\s*(\d+)$`)
	modelViewerMenuGuardRE   = regexp.MustCompile(`(?i)^\$([\w.]+)\s*(==|!=|>=|<=|>|<)\s*(-?\d+)$`)
	modelViewerMenuLiteralRE = regexp.MustCompile(`^-?\d+(?:\.\d+)?$`)
	modelViewerMenuButtonRE  = regexp.MustCompile(`(?i)^Button(\d+)(Left|Right)$`)
	modelViewerMenuStepRE    = regexp.MustCompile(`(?i)^\$([\w.]+)\s*([+-])\s*1$`)
)

func collectModelViewerSlotBindings(sections []modINISection, defaults map[string]any) []modelViewerSlotBinding {
	var bindings []modelViewerSlotBinding
	keyIndex := 0
	for _, section := range sections {
		if strings.HasPrefix(modelViewerNormalizeKey(section.Header), "key") {
			keyIndex++
			if !strings.EqualFold(modelViewerSectionValue(section, "type"), "cycle") {
				continue
			}
			for _, line := range section.Lines {
				assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(line))
				if assignment == nil {
					continue
				}
				key, raw := "$"+assignment[1], strings.TrimSpace(assignment[2])
				if !strings.HasPrefix(key, "$") || !strings.Contains(raw, ",") {
					continue
				}
				binding := modelViewerSlotBinding{Slot: keyIndex, Variable: modelViewerNormalizeKey(key)}
				for _, entry := range strings.Split(raw, ",") {
					entry = strings.TrimSpace(entry)
					if entry == "" {
						continue
					}
					if number, err := strconv.ParseFloat(entry, 64); err == nil {
						binding.Values = append(binding.Values, number)
					} else {
						binding.Values = append(binding.Values, entry)
					}
					if len(binding.Values) > maxModelViewerMenuValues {
						binding.Values = nil
						break
					}
				}
				if len(binding.Values) > 0 {
					bindings = append(bindings, binding)
				}
			}
		}
	}
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CommandList") {
			continue
		}
		branches := splitModelViewerMenuSlotBranches(section.Lines)
		if len(branches) == 0 || len(branches) == 1 && modelViewerNormalizeKey(section.Name) != modelViewerNormalizeKey("ClickedSlot") {
			continue
		}
		for _, branch := range branches {
			variable, values, effects, ok := parseModelViewerMenuBranch(branch.lines)
			if ok {
				bindings = append(bindings, modelViewerSlotBinding{Slot: branch.slot, Variable: variable, Values: values, Effects: effects, AlwaysVisible: true})
			}
		}
	}
	bindings = append(bindings, collectModelViewerArrowBindings(sections)...)
	return dedupeModelViewerSlotBindings(bindings)
}

func splitModelViewerMenuSlotBranches(lines []string) []modelViewerMenuSlotBranch {
	cleaned := make([]string, len(lines))
	for index, line := range lines {
		cleaned[index] = strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
	}
	var scan func([]string, int) []modelViewerMenuSlotBranch
	scan = func(block []string, depth int) []modelViewerMenuSlotBranch {
		var found []modelViewerMenuSlotBranch
		for index := 0; index < len(block); index++ {
			if !strings.HasPrefix(strings.ToLower(block[index]), "if ") {
				continue
			}
			branches, end, ok := splitModelViewerDirectConditional(block, index)
			if !ok {
				continue
			}
			var nested []modelViewerMenuSlotBranch
			for _, branch := range branches {
				nested = append(nested, scan(branch.lines, depth+1)...)
			}
			var slots []modelViewerMenuSlotBranch
			first := modelViewerMenuSlotRE.FindStringSubmatch(stringValue(branches[0].expression))
			if first != nil {
				for _, branch := range branches {
					match := modelViewerMenuSlotRE.FindStringSubmatch(stringValue(branch.expression))
					if match == nil || !strings.EqualFold(match[1], first[1]) {
						continue
					}
					slot, _ := strconv.Atoi(match[2])
					slots = append(slots, modelViewerMenuSlotBranch{slotVariable: modelViewerNormalizeKey(match[1]), slot: slot, lines: branch.lines})
				}
			}
			if (len(slots) >= 2 || len(slots) == 1 && depth == 0) && len(nested) == 0 {
				found = append(found, slots...)
			}
			found = append(found, nested...)
			index = end
		}
		return found
	}
	return scan(cleaned, 0)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type modelViewerMenuGuard struct {
	variable, op string
	value        int
}

func parseModelViewerMenuGuard(value string) *modelViewerMenuGuard {
	match := modelViewerMenuGuardRE.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return nil
	}
	number, _ := strconv.Atoi(match[3])
	return &modelViewerMenuGuard{variable: modelViewerNormalizeKey(match[1]), op: match[2], value: number}
}

func parseModelViewerMenuBranch(lines []string) (string, []any, []ModelViewerMenuEffect, bool) {
	type frame struct {
		guard    *modelViewerMenuGuard
		branches int
	}
	var variable string
	var values []any
	var effects []ModelViewerMenuEffect
	var stack []frame
	var wrap *struct {
		guard modelViewerMenuGuard
		depth int
	}
	inWrapElse := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "if ") {
			stack = append(stack, frame{guard: parseModelViewerMenuGuard(strings.TrimSpace(line[3:])), branches: 1})
			continue
		}
		if lower == "endif" {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if wrap != nil && len(stack) < wrap.depth {
				wrap, inWrapElse = nil, false
			}
			continue
		}
		if lower == "else" || strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if ") {
			inWrapElse = wrap != nil && len(stack) == wrap.depth && lower == "else"
			if len(stack) > 0 {
				current := &stack[len(stack)-1]
				if lower == "else" {
					current.guard = negateModelViewerMenuGuard(current.guard)
				} else {
					offset := 5
					if strings.HasPrefix(lower, "else if ") {
						offset = 8
					}
					current.guard = parseModelViewerMenuGuard(strings.TrimSpace(line[offset:]))
				}
				current.branches++
			}
			continue
		}
		assignment := modelViewerMenuAssignRE.FindStringSubmatch(line)
		if assignment == nil {
			continue
		}
		lhs, rhs := modelViewerNormalizeKey(assignment[1]), strings.TrimSpace(assignment[2])
		var guard *modelViewerMenuGuard
		if len(stack) > 0 {
			guard = stack[len(stack)-1].guard
		}
		if match := modelViewerMenuFlipRE.FindStringSubmatch(rhs); match != nil && modelViewerNormalizeKey(match[1]) == lhs {
			variable, values = lhs, modelViewerMenuCycleValues(0, 1)
			continue
		}
		if match := modelViewerMenuIncrModRE.FindStringSubmatch(rhs); match != nil && modelViewerNormalizeKey(match[1]) == lhs {
			count, _ := strconv.Atoi(match[2])
			variable, values = lhs, modelViewerMenuCycleValues(0, count-1)
			continue
		}
		if match := modelViewerMenuIncrRE.FindStringSubmatch(rhs); match != nil && modelViewerNormalizeKey(match[1]) == lhs {
			variable, values = lhs, modelViewerMenuCycleValues(0, 1)
			if guard != nil && guard.variable == lhs && (guard.op == "<" || guard.op == "<=") {
				wrap = &struct {
					guard modelViewerMenuGuard
					depth int
				}{guard: *guard, depth: len(stack)}
			}
			continue
		}
		if match := modelViewerMenuModRE.FindStringSubmatch(rhs); match != nil && modelViewerNormalizeKey(match[1]) == lhs && variable == lhs {
			count, _ := strconv.Atoi(match[2])
			values = modelViewerMenuCycleValues(0, count-1)
			continue
		}
		if !modelViewerMenuLiteralRE.MatchString(rhs) {
			continue
		}
		literal, err := strconv.ParseFloat(rhs, 64)
		if err != nil || literal != float64(int(literal)) {
			continue
		}
		if inWrapElse && variable == lhs && wrap != nil && wrap.guard.variable == lhs {
			hi := wrap.guard.value
			if wrap.guard.op == "<=" {
				hi++
			}
			values = modelViewerMenuCycleValues(int(literal), hi)
			continue
		}
		if guard != nil && variable == lhs && guard.variable == lhs && (guard.op == ">" || guard.op == ">=") {
			hi := guard.value
			if guard.op == ">=" {
				hi--
			}
			values = modelViewerMenuCycleValues(int(literal), hi)
			continue
		}
		var when *ModelViewerMenuGuard
		if guard != nil {
			when = &ModelViewerMenuGuard{Var: guard.variable, Op: guard.op, Value: strconv.Itoa(guard.value)}
		}
		effects = append(effects, ModelViewerMenuEffect{When: when, Var: lhs, Value: rhs})
	}
	return variable, values, effects, variable != "" && len(values) > 0
}

func negateModelViewerMenuGuard(guard *modelViewerMenuGuard) *modelViewerMenuGuard {
	if guard == nil {
		return nil
	}
	negated := *guard
	negated.op = map[string]string{"==": "!=", "!=": "==", "<": ">=", ">=": "<", ">": "<=", "<=": ">"}[guard.op]
	return &negated
}

func modelViewerMenuCycleValues(low, high int) []any {
	length := high - low + 1
	if length <= 0 || length > maxModelViewerMenuValues {
		return nil
	}
	values := make([]any, length)
	for index := range values {
		values[index] = float64(low + index)
	}
	return values
}

func collectModelViewerArrowBindings(sections []modINISection) []modelViewerSlotBinding {
	bySlot := make(map[int][]modelViewerSlotBinding)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CommandList") {
			continue
		}
		match := modelViewerMenuButtonRE.FindStringSubmatch(section.Name)
		if match == nil {
			continue
		}
		variable, values, ok := parseModelViewerArrowButton(section.Lines)
		if !ok {
			continue
		}
		slot, _ := strconv.Atoi(match[1])
		bySlot[slot] = append(bySlot[slot], modelViewerSlotBinding{Slot: slot, Variable: variable, Values: values, AlwaysVisible: true})
	}
	if len(bySlot) < 2 {
		return nil
	}
	slots := make([]int, 0, len(bySlot))
	for slot := range bySlot {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	output := make([]modelViewerSlotBinding, 0, len(slots))
	for _, slot := range slots {
		output = append(output, bySlot[slot][0])
	}
	return output
}

func parseModelViewerArrowButton(lines []string) (string, []any, bool) {
	variable, direction := "", ""
	for _, line := range lines {
		assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(line))
		if assignment == nil {
			continue
		}
		step := modelViewerMenuStepRE.FindStringSubmatch(strings.TrimSpace(assignment[2]))
		if step != nil && modelViewerNormalizeKey(step[1]) == modelViewerNormalizeKey(assignment[1]) {
			variable, direction = modelViewerNormalizeKey(assignment[1]), step[2]
			break
		}
	}
	if variable == "" {
		return "", nil, false
	}
	for index, line := range lines {
		lower := strings.ToLower(strings.TrimSpace(line))
		if !strings.HasPrefix(lower, "if ") {
			continue
		}
		guard := parseModelViewerMenuGuard(strings.TrimSpace(line)[3:])
		if guard == nil || guard.variable != variable || direction == "-" && guard.op != "<" && guard.op != "<=" || direction == "+" && guard.op != ">" && guard.op != ">=" {
			continue
		}
		for _, later := range lines[index+1:] {
			if strings.EqualFold(strings.TrimSpace(later), "endif") {
				break
			}
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(later))
			if assignment == nil || modelViewerNormalizeKey(assignment[1]) != variable || !modelViewerMenuLiteralRE.MatchString(strings.TrimSpace(assignment[2])) {
				continue
			}
			reset, err := strconv.Atoi(strings.TrimSpace(assignment[2]))
			if err != nil {
				continue
			}
			low, high := reset, guard.value
			if direction == "-" {
				low, high = guard.value, reset
				if guard.op == "<=" {
					low++
				}
			} else if guard.op == ">=" {
				high--
			}
			values := modelViewerMenuCycleValues(low, high)
			return variable, values, len(values) > 0
		}
	}
	return "", nil, false
}

func cloneModelViewerState(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func appendUniqueModelViewerValue(values []any, value any) []any {
	key := modelViewerString(value)
	for _, existing := range values {
		if modelViewerString(existing) == key {
			return values
		}
	}
	return append(values, value)
}

func dedupeModelViewerSlotBindings(input []modelViewerSlotBinding) []modelViewerSlotBinding {
	seen := make(map[string]bool)
	out := make([]modelViewerSlotBinding, 0, len(input))
	for _, binding := range input {
		key := strconv.Itoa(binding.Slot) + ":" + binding.Variable
		if !seen[key] {
			seen[key] = true
			out = append(out, binding)
		}
	}
	return out
}

func allModelViewerNumbers(values []any) bool {
	for _, value := range values {
		if _, ok := value.(float64); !ok {
			return false
		}
	}
	return true
}

func modelViewerAsFloat(value any) float64 { parsed, _ := modelViewerNumber(value); return parsed }

func humanizeModelViewerLabel(value string) string {
	value = strings.TrimLeft(value, "$")
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

func deriveModelViewerVariableToken(value string) string {
	value = strings.TrimLeft(value, "$")
	if strings.HasPrefix(strings.ToLower(value), "swapvar") {
		value = value[len("swapvar"):]
	}
	return value
}

func inferModelViewerSlider(variable string, values []any, force bool) *ModelViewerSlider {
	if !force && !strings.HasPrefix(strings.ToLower(deriveModelViewerVariableToken(variable)), "slider") {
		return nil
	}
	if len(values) < 3 || !allModelViewerNumbers(values) {
		return nil
	}
	numbers := make([]float64, len(values))
	for i, value := range values {
		numbers[i] = modelViewerAsFloat(value)
	}
	sort.Float64s(numbers)
	step := 0.0
	for i := 1; i < len(numbers); i++ {
		difference := numbers[i] - numbers[i-1]
		rounded, _ := strconv.ParseFloat(strconv.FormatFloat(difference, 'f', 6, 64), 64)
		if rounded > 0 && (step == 0 || rounded < step) {
			step = rounded
		}
	}
	if step == 0 {
		step = 1
	}
	return &ModelViewerSlider{Min: numbers[0], Max: numbers[len(numbers)-1], Step: step}
}
