package modelviewer

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/samber/lo"
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
		if len(branches) == 0 ||
			len(branches) == 1 && modelViewerNormalizeKey(section.Name) != modelViewerNormalizeKey("ClickedSlot") {
			continue
		}
		for _, branch := range branches {
			variable, values, effects, ok := parseModelViewerMenuBranch(branch.lines)
			if ok {
				bindings = append(
					bindings,
					modelViewerSlotBinding{
						Slot:          branch.slot,
						Variable:      variable,
						Values:        values,
						Effects:       effects,
						AlwaysVisible: true,
					},
				)
			}
		}
	}
	bindings = append(bindings, collectModelViewerArrowBindings(sections)...)
	bindings = append(bindings, collectModelViewerKeyListBindings(sections, bindings)...)
	bindings = append(bindings, collectModelViewerWrapBindings(sections, defaults, bindings)...)
	// A key and a menu can share a slot; keep the menu's visibility and effects
	// without losing values that only the key binding can select.
	sort.SliceStable(bindings, func(i, j int) bool {
		return bindings[i].AlwaysVisible && !bindings[j].AlwaysVisible
	})
	merged := make([]modelViewerSlotBinding, 0, len(bindings))
	bySlot := make(map[string]int, len(bindings))
	for _, binding := range bindings {
		key := strconv.Itoa(binding.Slot) + ":" + binding.Variable
		if index, ok := bySlot[key]; ok {
			merged[index].Values = lo.UniqBy(append(merged[index].Values, binding.Values...), modelViewerString)
			continue
		}
		bySlot[key] = len(merged)
		merged = append(merged, binding)
	}
	return merged
}

// collectModelViewerKeyListBindings reads the variables a list run by a key steps
// with a modulo, as a click on a menu slot does. What the rest of the branch sets
// becomes the variable's effects:
//
//	if cursor_x > 0.1 && cursor_x < 0.2
//	    $x = ($x + 1) % 3
//	    run = CommandListApplyX
//	endif
func collectModelViewerKeyListBindings(
	sections []modINISection,
	existing []modelViewerSlotBinding,
) []modelViewerSlotBinding {
	flattener := &modelViewerEffectFlattener{
		lists:     make(map[string][]string),
		variables: make(map[string]any),
		visiting:  make(map[string]bool),
	}
	for _, section := range sections {
		if strings.EqualFold(section.Header, "CommandList") {
			lines := make([]string, 0, len(section.Lines))
			for _, line := range section.Lines {
				if line = strings.TrimSpace(strings.SplitN(line, ";", 2)[0]); line != "" {
					lines = append(lines, line)
				}
			}
			flattener.lists[modelViewerNormalizeKey(section.Header+section.Name)] = lines
		}
	}
	addModelViewerBindingDomains(flattener.variables, existing)

	type step struct {
		variable string
		count    int
		rest     []string
		clicked  bool
	}
	var steps []step
	var visit func(block []string)
	visit = func(block []string) {
		for index := 0; index < len(block); index++ {
			if strings.HasPrefix(strings.ToLower(block[index]), "if ") {
				branches, end, ok := splitModelViewerDirectConditional(block, index)
				if !ok {
					return
				}
				for _, branch := range branches {
					visit(branch.lines)
				}
				index = end
				continue
			}
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(block[index])
			if assignment == nil {
				continue
			}
			match := modelViewerMenuIncrModRE.FindStringSubmatch(strings.TrimSpace(assignment[2]))
			if match == nil || !strings.EqualFold(match[1], assignment[1]) {
				continue
			}
			count, err := strconv.Atoi(match[2])
			if err == nil && count > 1 && count <= maxModelViewerMenuValues {
				steps = append(steps, step{
					variable: modelViewerNormalizeKey(assignment[1]),
					count:    count,
					rest:     block[index+1:],
				})
			}
		}
	}
	// Mouse keys go first: the same variable may also step from a controller key.
	for _, clicked := range []bool{true, false} {
		for _, section := range sections {
			if strings.HasPrefix(modelViewerNormalizeKey(section.Header), "key") &&
				modelViewerOverlayIsClickKey(cleanModelViewerOverlayLines(section.Lines)) == clicked {
				start := len(steps)
				visit(flattener.lists[modelViewerNormalizeKey(sectionValue(section.Lines, "run"))])
				for index := start; index < len(steps); index++ {
					steps[index].clicked = clicked
				}
			}
		}
	}
	for _, found := range steps {
		values := modelViewerMenuCycleValues(0, found.count-1)
		key := "__domain:" + found.variable
		if _, known := flattener.variables[key]; !known {
			flattener.variables[key] = values
		}
	}

	seen := make(map[string]bool, len(existing))
	for _, binding := range existing {
		seen[binding.Variable] = true
	}
	var output []modelViewerSlotBinding
	for _, found := range steps {
		if seen[found.variable] {
			continue
		}
		seen[found.variable] = true
		effects, ok := flattener.flatten(found.rest, modelViewerDNFTrue())
		if !ok {
			effects = nil
		}
		// As with other menu slots, a click may act only through its effects.
		output = append(output, modelViewerSlotBinding{
			Variable:      found.variable,
			Values:        modelViewerMenuCycleValues(0, found.count-1),
			Effects:       effects,
			AlwaysVisible: found.clicked && len(effects) > 0,
		})
	}
	return output
}

// modelViewerEffectFlattener turns command list lines into effects, which apply in
// order and test their guards again each time. Lines it cannot express that way fail
// the whole list, since a partial list would leave the model in a state the mod
// never produces.
type modelViewerEffectFlattener struct {
	lists     map[string][]string
	variables map[string]any
	visiting  map[string]bool
}

func (f *modelViewerEffectFlattener) flatten(lines []string, guard ModelViewerDNF) ([]ModelViewerMenuEffect, bool) {
	var effects []ModelViewerMenuEffect
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if strings.HasPrefix(strings.ToLower(line), "if ") {
			branches, end, ok := splitModelViewerDirectConditional(lines, index)
			if !ok {
				return nil, false
			}
			nested, ok := f.flattenConditional(branches, guard)
			if !ok {
				return nil, false
			}
			effects = append(effects, nested...)
			index = end
		} else if key, value, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(key), "run") {
			target := modelViewerNormalizeKey(value)
			list, known := f.lists[target]
			if !known && strings.HasPrefix(target, "commandlist") {
				return nil, false
			}
			if known {
				if f.visiting[target] {
					return nil, false
				}
				f.visiting[target] = true
				nested, listOK := f.flatten(list, guard)
				delete(f.visiting, target)
				if !listOK {
					return nil, false
				}
				effects = append(effects, nested...)
			}
		} else if assignment := modelViewerMenuAssignRE.FindStringSubmatch(line); assignment != nil {
			value := strings.TrimSpace(assignment[2])
			if !modelViewerMenuLiteralRE.MatchString(value) {
				return nil, false
			}
			effects = append(
				effects,
				modelViewerGuardedEffects(guard, modelViewerNormalizeKey(assignment[1]), value)...)
		}
		if len(effects) > maxModelViewerMenuValues {
			return nil, false
		}
	}
	return effects, true
}

func (f *modelViewerEffectFlattener) flattenConditional(
	branches []modelViewerDirectConditionalBranch,
	guard ModelViewerDNF,
) ([]ModelViewerMenuEffect, bool) {
	conditions := make([]ModelViewerDNF, len(branches))
	for index, branch := range branches {
		conditions[index] = modelViewerDNFTrue()
		if branch.expression != nil {
			condition, ok := f.parseGuard(*branch.expression)
			if !ok {
				return nil, false
			}
			conditions[index] = condition
		}
	}

	var effects []ModelViewerMenuEffect
	prior := modelViewerDNFTrue()
	for index, branch := range branches {
		scope, ok := modelViewerExactDNFAnd(prior, conditions[index])
		if !ok {
			return nil, false
		}
		if scope, ok = modelViewerExactDNFAnd(guard, scope); !ok {
			return nil, false
		}
		nested, ok := f.flatten(branch.lines, scope)
		if !ok {
			return nil, false
		}

		// A later branch must see the value the mod tested, not what this one set,
		// and its guard holds the negation of this branch's condition.
		if index+1 < len(branches) {
			var reads []string
			for _, condition := range conditions[index:] {
				reads = append(reads, dnfVarNames(condition)...)
			}
			if slices.ContainsFunc(nested, func(effect ModelViewerMenuEffect) bool {
				return slices.Contains(reads, effect.Var)
			}) {
				return nil, false
			}
		}

		// Writing a variable the branch tests would fail the guards of the lines after
		// it, so that goes last, and only one such write can.
		reads := dnfVarNames(scope)
		written := func(effect ModelViewerMenuEffect) bool { return slices.Contains(reads, effect.Var) }
		var last *ModelViewerMenuEffect
		for _, effect := range nested {
			if !written(effect) {
				effects = append(effects, effect)
				continue
			}
			if last != nil && (last.Var != effect.Var || last.Value != effect.Value) {
				return nil, false
			}
			last = &effect
		}
		for _, effect := range nested {
			if written(effect) {
				effects = append(effects, effect)
			}
		}

		if prior, ok = modelViewerExactDNFAnd(prior, modelViewerDNFNot(conditions[index])); !ok {
			return nil, false
		}
	}
	return effects, true
}

// modelViewerExactDNFAnd reports false where modelViewerDNFAnd gives up on too many
// alternatives by returning true, which would leave effects without their guards.
func modelViewerExactDNFAnd(left, right ModelViewerDNF) (ModelViewerDNF, bool) {
	result := modelViewerDNFAnd(left, right)
	return result, !modelViewerDNFIsTrue(result) || modelViewerDNFIsTrue(left) && modelViewerDNFIsTrue(right)
}

// parseGuard rejects what the DNF parser would read as always true: tests of the
// cursor or other expressions, comparisons of a variable without known values, and
// expressions that could reach the parser's limit on alternatives, which it reports
// the same way. Each atom of n alternatives multiplies that bound by at most n+1.
func (f *modelViewerEffectFlattener) parseGuard(expression string) (ModelViewerDNF, bool) {
	bound := 1
	for _, token := range tokenizeModelViewerDNF(expression) {
		if token == "(" || token == ")" || token == "&&" || token == "||" || token == "!" {
			continue
		}
		alternatives := 1
		if !modelViewerDNFBareRE.MatchString(token) {
			match := modelViewerDNFClauseRE.FindStringSubmatch(token)
			if match == nil {
				return nil, false
			}
			if match[2] != "==" && match[2] != "!=" {
				variable := modelViewerNormalizeKey(match[1])
				if _, known := f.variables["__domain:"+variable]; !known {
					return nil, false
				}
				alternatives = len(expandModelViewerDNFComparison(variable, match[2], match[3], f.variables))
			}
		}
		if bound *= alternatives + 1; bound > maxModelViewerDNFGroups {
			return nil, false
		}
	}
	return parseModelViewerConditionDNF(expression, nil, f.variables), true
}

func modelViewerGuardedEffects(guard ModelViewerDNF, variable, value string) []ModelViewerMenuEffect {
	effects := make([]ModelViewerMenuEffect, 0, len(guard))
	for _, group := range guard {
		effect := ModelViewerMenuEffect{Var: variable, Value: value}
		for index, clause := range group {
			menuGuard := ModelViewerMenuGuard{Var: clause.Var, Op: "==", Value: clause.Value}
			if clause.Negate {
				menuGuard.Op = "!="
			}
			if index == 0 {
				effect.When = &menuGuard
			} else {
				effect.Also = append(effect.Also, menuGuard)
			}
		}
		effects = append(effects, effect)
	}
	return effects
}

// collectModelViewerWrapBindings reads the values of a variable a command list
// steps through and wraps back to zero, as an on-screen button does on click:
//
//	$x = $x + 1
//	if $x > 2
//	    $x = 0
//	endif
//
// The bound is a literal or a variable with an integer default. Variables that
// already have a binding keep it.
func collectModelViewerWrapBindings(
	sections []modINISection,
	defaults map[string]any,
	existing []modelViewerSlotBinding,
) []modelViewerSlotBinding {
	seen := make(map[string]bool, len(existing))
	for _, binding := range existing {
		seen[binding.Variable] = true
	}
	var output []modelViewerSlotBinding
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CommandList") {
			continue
		}
		lines := make([]string, len(section.Lines))
		for index, line := range section.Lines {
			lines[index] = strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
		}
		for index, line := range lines {
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(line)
			if assignment == nil {
				continue
			}
			step := modelViewerMenuIncrRE.FindStringSubmatch(strings.TrimSpace(assignment[2]))
			variable := modelViewerNormalizeKey(assignment[1])
			if step == nil || modelViewerNormalizeKey(step[1]) != variable || seen[variable] {
				continue
			}
			maximum, ok := parseModelViewerWrapBound(lines[index+1:], variable, defaults)
			if !ok || maximum < 1 || maximum >= maxModelViewerMenuValues {
				continue
			}
			values := make([]any, 0, maximum+1)
			for value := range maximum + 1 {
				values = append(values, float64(value))
			}
			seen[variable] = true
			output = append(output, modelViewerSlotBinding{Variable: variable, Values: values})
		}
	}
	return output
}

var modelViewerWrapGuardRE = regexp.MustCompile(`(?i)^if\s+\$(\w+)\s*(>=|>)\s*(\$?\w+)$`)

// parseModelViewerWrapBound returns the largest value variable keeps when the
// lines after its increment start with the guard that resets it to zero.
func parseModelViewerWrapBound(lines []string, variable string, defaults map[string]any) (int, bool) {
	if len(lines) == 0 {
		return 0, false
	}
	guard := modelViewerWrapGuardRE.FindStringSubmatch(lines[0])
	if guard == nil || modelViewerNormalizeKey(guard[1]) != variable {
		return 0, false
	}
	var bound any = guard[3]
	if strings.HasPrefix(guard[3], "$") {
		bound = defaults[modelViewerNormalizeKey(guard[3])]
	}
	number, err := modelViewerNumber(bound)
	if err != nil || number != float64(int(number)) {
		return 0, false
	}
	maximum := int(number)
	if guard[2] == ">=" {
		maximum--
	}

	depth := 0
	for _, line := range lines[1:] {
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "if "):
			depth++
		case lower == "endif":
			if depth == 0 {
				return 0, false
			}
			depth--
		case depth == 0:
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(line)
			if assignment != nil && modelViewerNormalizeKey(assignment[1]) == variable &&
				strings.TrimSpace(assignment[2]) == "0" {
				return maximum, true
			}
		}
	}
	return 0, false
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
					slots = append(
						slots,
						modelViewerMenuSlotBranch{
							slotVariable: modelViewerNormalizeKey(match[1]),
							slot:         slot,
							lines:        branch.lines,
						},
					)
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
		lhsRaw := assignment[1]
		lhs, rhs := modelViewerNormalizeKey(lhsRaw), strings.TrimSpace(assignment[2])
		var guard *modelViewerMenuGuard
		if len(stack) > 0 {
			guard = stack[len(stack)-1].guard
		}
		if match := modelViewerMenuFlipRE.FindStringSubmatch(rhs); match != nil && strings.EqualFold(match[1], lhsRaw) {
			variable, values = lhs, modelViewerMenuCycleValues(0, 1)
			continue
		}
		if match := modelViewerMenuIncrModRE.FindStringSubmatch(
			rhs,
		); match != nil &&
			strings.EqualFold(match[1], lhsRaw) {
			count, _ := strconv.Atoi(match[2])
			variable, values = lhs, modelViewerMenuCycleValues(0, count-1)
			continue
		}
		if match := modelViewerMenuIncrRE.FindStringSubmatch(rhs); match != nil && strings.EqualFold(match[1], lhsRaw) {
			variable, values = lhs, modelViewerMenuCycleValues(0, 1)
			if guard != nil && guard.variable == lhs && (guard.op == "<" || guard.op == "<=") {
				wrap = &struct {
					guard modelViewerMenuGuard
					depth int
				}{guard: *guard, depth: len(stack)}
			}
			continue
		}
		if match := modelViewerMenuModRE.FindStringSubmatch(
			rhs,
		); match != nil && strings.EqualFold(match[1], lhsRaw) &&
			variable == lhs {
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
		var also []ModelViewerMenuGuard
		for _, enclosing := range stack[:max(len(stack)-1, 0)] {
			if enclosing.guard != nil {
				also = append(also, ModelViewerMenuGuard{
					Var: enclosing.guard.variable, Op: enclosing.guard.op, Value: strconv.Itoa(enclosing.guard.value),
				})
			}
		}
		effects = append(effects, ModelViewerMenuEffect{When: when, Also: also, Var: lhs, Value: rhs})
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
		bySlot[slot] = append(
			bySlot[slot],
			modelViewerSlotBinding{Slot: slot, Variable: variable, Values: values, AlwaysVisible: true},
		)
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
		if guard == nil || guard.variable != variable || direction == "-" && guard.op != "<" && guard.op != "<=" ||
			direction == "+" && guard.op != ">" && guard.op != ">=" {
			continue
		}
		for _, later := range lines[index+1:] {
			if strings.EqualFold(strings.TrimSpace(later), "endif") {
				break
			}
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(later))
			if assignment == nil || modelViewerNormalizeKey(assignment[1]) != variable ||
				!modelViewerMenuLiteralRE.MatchString(strings.TrimSpace(assignment[2])) {
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
