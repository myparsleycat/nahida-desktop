package menumaker

import (
	"regexp"
	"strconv"
	"strings"
)

const visibilityMarker = " ; nahida-menu-maker-visibility"

var (
	visibilityGlobalRe   = regexp.MustCompile(`(?i)^global\s+(\$[a-z0-9_]+)(?:\s*=|\s*$)`)
	visibilityHashRe     = regexp.MustCompile(`(?i)^hash\s*=\s*[a-f0-9]{8}$`)
	visibilityVariableRe = regexp.MustCompile(`(?i)\$[a-z0-9_]+`)
)

// visibilityScan is what one pass over the sections tells us about how the menu
// is rendered: which flags exist, which resets restore them, and which
// command lists a render override can reach.
type visibilityScan struct {
	declared         map[string]bool
	reset            map[string][]string
	used             map[string]bool
	commands         map[string][]string
	positionSections []int
	inputs           []string
	visited          map[string]bool
}

// prepareVisibility reuses a frame-scoped render flag, or instruments existing
// Position overrides without introducing competing hash registrations.
func prepareVisibility(sections []MenuMakerSection) ([]MenuMakerSection, []string, string) {
	scan := scanVisibilitySections(sections)
	if len(scan.inputs) > 0 {
		return sections, visibilityInputConditions(sections, scan.inputs), ""
	}
	// Position binding may be delegated to an animation command list, or an
	// existing active assignment may be missing its per-frame reset.
	hooks := collectVisibilityHooks(sections, scan)
	if len(scan.positionSections) == 0 && len(hooks) == 0 {
		return sections, nil, ""
	}

	variable := "$nhd_menu_active"
	for suffix := 2; scan.used[variable]; suffix++ {
		variable = "$nhd_menu_active_" + strconv.Itoa(suffix)
	}
	insertVisibilityHooks(sections, scan.positionSections, hooks, variable)
	return sections, []string{variable + " == 1"}, variable
}

// scanVisibilitySections strips stale markers and collects the render-flag
// state of every section.
func scanVisibilitySections(sections []MenuMakerSection) visibilityScan {
	scan := visibilityScan{
		declared: map[string]bool{},
		reset:    map[string][]string{},
		used:     map[string]bool{},
		commands: map[string][]string{},
	}
	renderLines := []string{}
	for index := range sections {
		section := &sections[index]
		kept := []string{}
		for _, line := range section.Lines {
			if !strings.HasSuffix(line, visibilityMarker) {
				kept = append(kept, line)
			}
		}
		section.Lines = kept
		if section.Name == nil {
			continue
		}
		name := strings.ToLower(*section.Name)
		if name == "present" {
			recordVisibilityResets(section, scan.reset)
		}
		hasHash := false
		position := strings.Contains(name, "position")
		for _, line := range section.Lines {
			line = stripComment(line)
			for _, variable := range visibilityVariableRe.FindAllString(line, -1) {
				scan.used[strings.ToLower(variable)] = true
			}
			if name == "constants" {
				if match := visibilityGlobalRe.FindStringSubmatch(line); match != nil {
					scan.declared[strings.ToLower(match[1])] = true
				}
			}
			hasHash = hasHash || visibilityHashRe.MatchString(line)
			key, value, _ := strings.Cut(line, "=")
			if strings.EqualFold(strings.TrimSpace(key), "vb0") &&
				strings.Contains(strings.ToLower(value), "position") {
				position = true
			}
		}
		if strings.HasPrefix(name, "commandlist") {
			scan.commands[name] = append(scan.commands[name], section.Lines...)
		}
		if strings.HasPrefix(name, "textureoverride") && hasHash {
			renderLines = append(renderLines, section.Lines...)
			if position {
				scan.positionSections = append(scan.positionSections, index)
			}
		}
	}
	scan.inputs, scan.visited = visibilityInputs(renderLines, scan)
	return scan
}

// recordVisibilityResets keeps the branch predicates around frame resets,
// including manager slot gates.
func recordVisibilityResets(section *MenuMakerSection, reset map[string][]string) {
	section.Lines = stripGeneratedPresent(section.Lines)
	filtered := []string{}
	for _, line := range section.Lines {
		if !strings.EqualFold(stripComment(line), "post run = CommandListGuiActivateReset") {
			filtered = append(filtered, line)
		}
	}
	section.Lines = filtered
	for _, scoped := range visibilityScopedLines(filtered) {
		if !strings.HasPrefix(strings.ToLower(scoped.line), "post ") {
			continue
		}
		match := assignRe.FindStringSubmatch(strings.TrimSpace(scoped.line[5:]))
		if match == nil || match[2] != "0" {
			continue
		}
		variable := strings.ToLower(match[1])
		reset[variable] = append(reset[variable], scoped.condition)
	}
}

// visibilityInputs walks the render lines, following `run = CommandList…` hops,
// and returns the visibility inputs plus the command lists it reached. Lines
// pulled in through a hop are scanned as well, which is why both results come
// out of one traversal.
func visibilityInputs(renderLines []string, scan visibilityScan) ([]string, map[string]bool) {
	inputs := []string{}
	visited := map[string]bool{}
	for index := 0; index < len(renderLines); index++ {
		line := stripComment(renderLines[index])
		if match := assignRe.FindStringSubmatch(line); match != nil && match[2] == "1" {
			variable := strings.ToLower(match[1])
			if scan.declared[variable] && len(scan.reset[variable]) > 0 {
				for _, condition := range scan.reset[variable] {
					input := variable + " == 1"
					if condition != "" {
						input += " && (" + condition + ")"
					}
					inputs = append(inputs, input)
				}
			}
		}
		if runAssignRe.MatchString(line) {
			command := strings.ToLower(strings.TrimSpace(afterEquals(line)))
			if !visited[command] {
				visited[command] = true
				renderLines = append(renderLines, scan.commands[command]...)
			}
		}
	}
	return inputs, visited
}

// visibilityInputConditions expands each input with the condition of every
// key-prefixed section that reads the same variable.
func visibilityInputConditions(sections []MenuMakerSection, inputs []string) []string {
	conditions := []string{}
	for _, input := range uniqueStrings(inputs) {
		variable := strings.Fields(input)[0]
		found := false
		for _, section := range sections {
			if section.Name == nil || !keyPrefixRe.MatchString(*section.Name) {
				continue
			}
			for _, line := range section.Lines {
				if !conditionAssignRe.MatchString(stripComment(line)) {
					continue
				}
				condition := stripComment(afterEquals(line))
				for _, token := range visibilityVariableRe.FindAllString(condition, -1) {
					if strings.EqualFold(token, variable) {
						conditions = append(conditions, "("+input+") && ("+condition+")")
						found = true
						break
					}
				}
			}
		}
		if !found {
			conditions = append(conditions, input)
		}
	}
	return uniqueStrings(conditions)
}

// collectVisibilityHooks finds the lines that need a per-frame visibility flag:
// Position bindings and active-state assignments in eligible sections.
func collectVisibilityHooks(sections []MenuMakerSection, scan visibilityScan) map[int][]int {
	hooks := map[int][]int{}
	for index, section := range sections {
		if section.Name == nil {
			continue
		}
		name := strings.ToLower(*section.Name)
		eligible := scan.visited[name]
		for _, positionSection := range scan.positionSections {
			eligible = eligible || positionSection == index
		}
		if strings.HasPrefix(name, "textureoverride") {
			for _, line := range section.Lines {
				eligible = eligible || visibilityHashRe.MatchString(stripComment(line))
			}
		}
		if !eligible {
			continue
		}
		for lineIndex, line := range section.Lines {
			line = stripComment(line)
			key, value, _ := strings.Cut(line, "=")
			if strings.EqualFold(strings.TrimSpace(key), "vb0") &&
				strings.Contains(strings.ToLower(value), "position") {
				hooks[index] = append(hooks[index], lineIndex)
			} else if match := assignRe.FindStringSubmatch(line); match != nil && match[2] == "1" &&
				scan.declared[strings.ToLower(match[1])] &&
				strings.Contains(strings.ToLower(match[1]), "active") {
				hooks[index] = append(hooks[index], lineIndex)
			}
		}
	}
	return hooks
}

// insertVisibilityHooks anchors the visibility flag on sections that bind a
// position but carry no assignment to hook onto.
func insertVisibilityHooks(sections []MenuMakerSection, positionSections []int, hooks map[int][]int, variable string) {
	for _, index := range positionSections {
		if len(hooks[index]) > 0 {
			continue
		}
		section := &sections[index]
		anchor := -1
		for lineIndex, line := range section.Lines {
			key, value, _ := strings.Cut(stripComment(line), "=")
			if strings.EqualFold(strings.TrimSpace(key), "vb0") &&
				strings.Contains(strings.ToLower(value), "position") {
				anchor = lineIndex
				break
			}
			if visibilityHashRe.MatchString(stripComment(line)) {
				anchor = lineIndex + 1
			}
		}
		if anchor >= 0 {
			hooks[index] = []int{anchor}
		}
	}
	for index, anchors := range hooks {
		for i := len(anchors) - 1; i >= 0; i-- {
			sections[index].Lines = appendedAt(
				sections[index].Lines,
				anchors[i],
				[]string{variable + " = 1" + visibilityMarker},
			)
		}
	}
}

type visibilityScopedLine struct {
	line      string
	condition string
}

// Preserve branch predicates around frame resets, including manager slot gates.
func visibilityScopedLines(lines []string) []visibilityScopedLine {
	type branch struct{ previous, current string }
	stack := []branch{}
	out := []visibilityScopedLine{}
	for _, raw := range lines {
		line := stripComment(raw)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "if "):
			condition := strings.TrimSpace(line[3:])
			stack = append(stack, branch{previous: "(" + condition + ")", current: "(" + condition + ")"})
		case (strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if ")) && len(stack) > 0:
			start := 5
			if strings.HasPrefix(lower, "else if ") {
				start = 8
			}
			condition := "(" + strings.TrimSpace(line[start:]) + ")"
			top := &stack[len(stack)-1]
			top.current = "!(" + top.previous + ") && " + condition
			top.previous += " || " + condition
		case lower == "else" && len(stack) > 0:
			top := &stack[len(stack)-1]
			top.current = "!(" + top.previous + ")"
		case lower == "endif" && len(stack) > 0:
			stack = stack[:len(stack)-1]
		default:
			conditions := []string{}
			for _, item := range stack {
				conditions = append(conditions, "("+item.current+")")
			}
			out = append(out, visibilityScopedLine{line: line, condition: strings.Join(conditions, " && ")})
		}
	}
	return out
}
