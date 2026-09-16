package modelviewer

import (
	"regexp"
	"strings"
)

func extractModelViewerDirectStateRules(sections []modINISection, variables map[string]any) []ModelViewerStateRule {
	variables = modelViewerDirectConditionVariables(sections, variables)
	assignmentRE := regexp.MustCompile(`^\$([\w.]+)\s*=\s*(-?\d+(?:\.\d+)?)\s*$`)
	var rules []ModelViewerStateRule
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Present") {
			continue
		}
		var stack []modelViewerBranchFrame
		for _, raw := range section.Lines {
			line := strings.TrimSpace(raw)
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				expression := strings.TrimSpace(line[3:])
				stack = append(
					stack,
					modelViewerBranchFrame{
						active:  []modelViewerConditionClause{{Expression: expression, Expected: true}},
						inverse: []modelViewerConditionClause{{Expression: expression, Expected: false}},
					},
				)
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
				stack = append(
					stack,
					modelViewerBranchFrame{
						active: append(
							append([]modelViewerConditionClause(nil), previous.inverse...),
							modelViewerConditionClause{Expression: expression, Expected: true},
						),
						inverse: append(
							append([]modelViewerConditionClause(nil), previous.inverse...),
							modelViewerConditionClause{Expression: expression, Expected: false},
						),
					},
				)
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
			match := assignmentRE.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			var clauses []modelViewerConditionClause
			for _, frame := range stack {
				clauses = append(clauses, frame.active...)
			}
			conditions := modelViewerConditionsToDNF(clauses, variables)
			if len(conditions) == 0 {
				continue
			}
			rules = append(
				rules,
				ModelViewerStateRule{Var: modelViewerNormalizeKey(match[1]), Value: match[2], Conditions: conditions},
			)
		}
	}
	return rules
}
