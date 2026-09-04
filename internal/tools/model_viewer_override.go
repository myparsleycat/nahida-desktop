package tools

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type modelViewerConditionClause struct {
	Expression string
	Expected   bool
}

type modelViewerDrawInstruction struct {
	IBResourceName string
	IndexCount     int
	StartIndex     int
	BaseVertex     int
	Auto           bool
	Conditions     []modelViewerConditionClause
}

type modelViewerDrawBinding struct {
	SectionName    string
	IBResourceName string
	OverrideHash   string
	Draws          []modelViewerDrawInstruction
}

type modelViewerBranchFrame struct {
	active  []modelViewerConditionClause
	inverse []modelViewerConditionClause
}

var modelViewerDefaultVariableRE = regexp.MustCompile(`(?i)^(?:global\s+)?(?:persist\s+)?(\$\w+)\s*=\s*([^,]+)$`)

func collectModelViewerDefaultVariables(sections []modINISection) map[string]any {
	variables := make(map[string]any)
	for _, section := range sections {
		for _, line := range section.Lines {
			match := modelViewerDefaultVariableRE.FindStringSubmatch(strings.TrimSpace(line))
			if match == nil {
				continue
			}
			key := modelViewerNormalizeKey(match[1])
			if _, exists := variables[key]; exists {
				continue
			}
			valueText := strings.TrimSpace(match[2])
			value := any(valueText)
			if parsed, err := strconv.ParseFloat(valueText, 64); err == nil {
				value = parsed
			}
			variables[key] = value
		}
	}
	return variables
}

func collectModelViewerDrawBindings(sections []modINISection, variables map[string]any) []modelViewerDrawBinding {
	sectionLookup := make(map[string]modINISection)
	for _, section := range sections {
		sectionLookup[modelViewerNormalizeKey(section.Header+section.Name)] = section
	}
	var bindings []modelViewerDrawBinding
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") || isModelViewerPreviewSkippedOverride(section.Name) {
			continue
		}
		draws, _ := collectModelViewerDrawContext(section, variables, sectionLookup, nil, "", make(map[string]bool))
		byIB := make(map[string][]modelViewerDrawInstruction)
		order := make([]string, 0)
		for _, draw := range draws {
			if draw.IBResourceName == "" {
				continue
			}
			key := modelViewerNormalizeKey(draw.IBResourceName)
			if _, exists := byIB[key]; !exists {
				order = append(order, key)
			}
			byIB[key] = append(byIB[key], draw)
		}
		for _, key := range order {
			group := byIB[key]
			bindings = append(bindings, modelViewerDrawBinding{SectionName: section.Name, IBResourceName: group[0].IBResourceName, OverrideHash: modelViewerSectionValue(section, "hash"), Draws: group})
		}
	}
	return bindings
}

func collectModelViewerDrawContext(section modINISection, variables map[string]any, sectionLookup map[string]modINISection, inherited []modelViewerConditionClause, inheritedIB string, visited map[string]bool) ([]modelViewerDrawInstruction, string) {
	name := modelViewerNormalizeKey(section.Header + section.Name)
	if visited[name] {
		return nil, inheritedIB
	}
	visited = cloneModelViewerVisited(visited)
	visited[name] = true
	var instructions []modelViewerDrawInstruction
	var stack []modelViewerBranchFrame
	currentIB := inheritedIB
	activeConditions := func() []modelViewerConditionClause {
		out := append([]modelViewerConditionClause(nil), inherited...)
		for _, frame := range stack {
			out = append(out, frame.active...)
		}
		return out
	}
	for _, raw := range section.Lines {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "if "):
			expression := strings.TrimSpace(line[3:])
			stack = append(stack, modelViewerBranchFrame{active: []modelViewerConditionClause{{Expression: expression, Expected: true}}, inverse: []modelViewerConditionClause{{Expression: expression, Expected: false}}})
			continue
		case strings.HasPrefix(lower, "elif "), strings.HasPrefix(lower, "else if "):
			expression := strings.TrimSpace(line[5:])
			if strings.HasPrefix(lower, "else if ") {
				expression = strings.TrimSpace(line[8:])
			}
			previous := modelViewerBranchFrame{}
			if len(stack) > 0 {
				previous = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			active := append(append([]modelViewerConditionClause(nil), previous.inverse...), modelViewerConditionClause{Expression: expression, Expected: true})
			inverse := append(append([]modelViewerConditionClause(nil), previous.inverse...), modelViewerConditionClause{Expression: expression, Expected: false})
			stack = append(stack, modelViewerBranchFrame{active: active, inverse: inverse})
			continue
		case lower == "else":
			if len(stack) > 0 {
				previous := stack[len(stack)-1]
				stack[len(stack)-1] = modelViewerBranchFrame{active: previous.inverse}
			}
			continue
		case lower == "endif":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		key, value, assignment := strings.Cut(line, "=")
		if assignment {
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if nested := modelViewerNestedSectionName(key, value); nested != "" {
				if nestedSection, ok := sectionLookup[modelViewerNormalizeKey(nested)]; ok {
					draws, nextIB := collectModelViewerDrawContext(nestedSection, variables, sectionLookup, activeConditions(), currentIB, visited)
					instructions = append(instructions, draws...)
					if nextIB != "" {
						currentIB = nextIB
					}
				}
				continue
			}
			if modelViewerNormalizeKey(key) == "ib" {
				currentIB = modelViewerTrimResourcePrefix(value)
				continue
			}
			if strings.EqualFold(key, "drawindexed") || strings.EqualFold(key, "drawindexedinstanced") {
				draw, ok := parseModelViewerDrawIndexed(key, value, variables)
				if !ok || draw.Auto {
					continue
				}
				draw.IBResourceName = currentIB
				draw.Conditions = activeConditions()
				instructions = append(instructions, draw)
			}
		}
	}
	return instructions, currentIB
}

func buildModelViewerIndicesForState(bindings []modelViewerDrawBinding, ibName string, indices []uint32, variables map[string]any, warn func(string)) ([]uint32, error) {
	var active []modelViewerDrawInstruction
	for _, binding := range bindings {
		if modelViewerNormalizeKey(binding.IBResourceName) != modelViewerNormalizeKey(ibName) {
			continue
		}
		for _, draw := range binding.Draws {
			enabled := true
			for _, clause := range draw.Conditions {
				if evaluateModelViewerCondition(clause.Expression, variables) != clause.Expected {
					enabled = false
					break
				}
			}
			if enabled {
				active = append(active, draw)
			}
		}
	}
	if len(active) == 0 {
		return indices, nil
	}
	merged := make([]uint32, 0)
	for _, draw := range active {
		end := draw.StartIndex + draw.IndexCount
		if draw.StartIndex < 0 || draw.IndexCount < 0 || end < 0 || end > len(indices) {
			if warn != nil {
				warn(fmt.Sprintf("Skipping invalid draw range start=%d count=%d", draw.StartIndex, draw.IndexCount))
			}
			continue
		}
		for _, index := range indices[draw.StartIndex:end] {
			value := int64(index) + int64(draw.BaseVertex)
			if value < 0 {
				return nil, fmt.Errorf("merged index became negative for draw start=%d count=%d baseVertex=%d", draw.StartIndex, draw.IndexCount, draw.BaseVertex)
			}
			merged = append(merged, uint32(value))
		}
	}
	return merged, nil
}

func cloneModelViewerVisited(input map[string]bool) map[string]bool {
	out := make(map[string]bool, len(input)+1)
	for key, value := range input {
		out[key] = value
	}
	return out
}
