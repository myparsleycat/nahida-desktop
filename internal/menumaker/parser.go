/**
 * Strict key mapping behavior is derived from XXMI-Menu-Maker.
 * Copyright (c) 2026 星念. MIT licensed; see internal/menumaker/NOTICE.md.
 */
package menumaker

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	originalLinesPrefix    = "; nahida-menu-maker-original="
	generatedReverseMarker = "; nahida-menu-maker-generated-reverse"
)

var (
	sectionRe       = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	assignRe        = regexp.MustCompile(`(?i)^\s*(\$[A-Za-z0-9_]+|[xyzw](?:\d+)?)\s*=\s*([^;]+?)\s*$`)
	generatedKeyRe  = regexp.MustCompile(`(?i)^KeyGui(?:Menu|Hold|Click|RightClick)$`)
	keyPrefixRe     = regexp.MustCompile(`(?i)^Key`)
	keyLineRe       = regexp.MustCompile(`(?i)^\s*key\s*=`)
	conditionLineRe = regexp.MustCompile(`(?i)^\s*condition\s*=`)
	commentTailRe   = regexp.MustCompile(`\s+;.*`)
	activeNameRe    = regexp.MustCompile(`(?i)\$[A-Za-z0-9_]*active[A-Za-z0-9_]*`)
	activeCompareRe = regexp.MustCompile(
		`(?i)(\$[A-Za-z0-9_]*active[A-Za-z0-9_]*)\s*(==|=|!=|>=|<=|>|<)\s*(-?\d+(?:\.\d+)?)|(-?\d+(?:\.\d+)?)\s*(==|=|!=|>=|<=|>|<)\s*(\$[A-Za-z0-9_]*active[A-Za-z0-9_]*)`,
	)
	variablePrefixRe  = regexp.MustCompile(`^\$[A-Za-z0-9_]+`)
	keyAssignRe       = regexp.MustCompile(`(?i)^key\s*=`)
	conditionAssignRe = regexp.MustCompile(`(?i)^condition\s*=`)
	typeAssignRe      = regexp.MustCompile(`(?i)^type\s*=`)
	backAssignRe      = regexp.MustCompile(`(?i)^back\s*=`)
	wrapAssignRe      = regexp.MustCompile(`(?i)^wrap\s*=`)
	runAssignRe       = regexp.MustCompile(`(?i)^run\s*=`)
	skipMetaAssignRe  = regexp.MustCompile(`(?i)^(smart|transition|transition_type|delay|release_delay)\s*=`)
	wrapFalseRe       = regexp.MustCompile(`(?i)^(false|0|no|off)$`)
	strictZeroRe      = regexp.MustCompile(`(?i)\s==\s0$`)
	spaceRe           = regexp.MustCompile(`\s+`)
	noModifiersRe     = regexp.MustCompile(`(?i)\bno_modifiers\b`)
	identifierRe      = regexp.MustCompile(`[^A-Za-z0-9_]`)
)

func parseDocument(text string) MenuMakerDocument {
	sections := parseSections(text)
	handlers := parseHandlers(sections)
	return MenuMakerDocument{
		Text:     text,
		Sections: sections,
		Handlers: handlers,
		Slots:    groupHandlers(handlers),
	}
}

func parseSections(text string) []MenuMakerSection {
	sections := []MenuMakerSection{}
	var name *string
	lines := []string{}
	index := 0
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if match := sectionRe.FindStringSubmatch(line); match != nil {
			sections = append(sections, MenuMakerSection{Name: name, Lines: lines, Index: index})
			index++
			trimmed := strings.TrimSpace(match[1])
			name = &trimmed
			lines = []string{line}
			continue
		}
		lines = append(lines, line)
	}
	sections = append(sections, MenuMakerSection{Name: name, Lines: lines, Index: index})
	return sections
}

func parseHandlers(sections []MenuMakerSection) []MenuMakerHandler {
	handlers := make([]MenuMakerHandler, 0)
	used := map[string]bool{}
	for _, section := range sections {
		if section.Name == nil || !keyPrefixRe.MatchString(*section.Name) || generatedKeyRe.MatchString(*section.Name) {
			continue
		}
		skipReverse := false
		for _, line := range section.Lines {
			if strings.EqualFold(strings.TrimSpace(line), generatedReverseMarker) {
				skipReverse = true
				break
			}
		}
		if skipReverse {
			continue
		}
		originalLines, ok := readOriginalLines(section.Lines)
		var semanticLines []string
		if ok {
			semanticLines = append(semanticLines, filterLines(section.Lines, keyLineRe)...)
			semanticLines = append(semanticLines, filterLines(section.Lines, conditionLineRe)...)
			for _, line := range originalLines {
				if keyLineRe.MatchString(line) || conditionLineRe.MatchString(line) {
					continue
				}
				semanticLines = append(semanticLines, line)
			}
		} else if len(section.Lines) > 0 {
			semanticLines = section.Lines[1:]
		}
		keys := []string{}
		condition := ""
		typeName := ""
		back := ""
		wrap := true
		entries := []MenuMakerEntry{}
		for _, line := range semanticLines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, ";") {
				continue
			}
			switch {
			case keyAssignRe.MatchString(trimmed):
				keys = append(keys, parseValue(trimmed))
			case conditionAssignRe.MatchString(trimmed):
				condition = strings.TrimSpace(afterEquals(trimmed))
			case typeAssignRe.MatchString(trimmed):
				typeName = strings.ToLower(strings.TrimSpace(afterEquals(stripComment(trimmed))))
			case backAssignRe.MatchString(trimmed):
				back = parseValue(trimmed)
			case wrapAssignRe.MatchString(trimmed):
				wrap = !wrapFalseRe.MatchString(parseValue(trimmed))
			case skipMetaAssignRe.MatchString(trimmed):
				continue
			case runAssignRe.MatchString(trimmed):
				entries = append(entries, MenuMakerEntry{
					Kind:   "run",
					Target: strings.TrimSpace(afterEquals(stripComment(trimmed))),
					Raw:    trimmed,
				})
			default:
				if assignment := assignRe.FindStringSubmatch(stripComment(trimmed)); assignment != nil {
					values := []string{}
					for _, value := range strings.Split(assignment[2], ",") {
						if trimmedValue := strings.TrimSpace(value); trimmedValue != "" {
							values = append(values, trimmedValue)
						}
					}
					entries = append(entries, MenuMakerEntry{
						Kind:     "assign",
						Variable: assignment[1],
						Values:   values,
						Raw:      trimmed,
					})
					continue
				}
				entries = append(entries, MenuMakerEntry{Kind: "raw", Line: trimmed})
			}
		}
		if len(entries) == 0 {
			continue
		}
		base := safeIdentifier(*section.Name)
		id := base
		suffix := 2
		for used[strings.ToLower(id)] {
			id = fmt.Sprintf("%s_%d", base, suffix)
			suffix++
		}
		used[strings.ToLower(id)] = true
		assignments := make([]MenuMakerEntry, 0)
		commandLists := make([]string, 0)
		rawEntries := make([]string, 0)
		steps := 0
		for _, entry := range entries {
			switch entry.Kind {
			case "assign":
				assignments = append(assignments, entry)
				if len(entry.Values) > steps {
					steps = len(entry.Values)
				}
			case "run":
				commandLists = append(commandLists, entry.Target)
			case "raw":
				rawEntries = append(rawEntries, entry.Line)
			}
		}
		if len(keys) == 0 {
			keys = []string{""}
		}
		handlers = append(handlers, MenuMakerHandler{
			ID:                  id,
			Section:             *section.Name,
			SourceIndex:         section.Index,
			Keys:                keys,
			Key:                 keys[0],
			Condition:           condition,
			Type:                typeName,
			Back:                back,
			Wrap:                wrap,
			Entries:             entries,
			Assignments:         assignments,
			CommandLists:        commandLists,
			RawEntries:          rawEntries,
			Steps:               steps,
			CommandName:         "CommandListCycle" + id,
			BackCommandName:     "CommandListCycle" + id + "Back",
			ActivateCommandName: "CommandListActivate" + id,
			StepVar:             "$ks_step_" + id,
			ActivatePulseVar:    "$gui_activate_pulse_" + id,
		})
	}
	return handlers
}

func groupHandlers(handlers []MenuMakerHandler) []MenuMakerSlot {
	type group struct {
		key      string
		handlers []MenuMakerHandler
	}
	order := []string{}
	groups := map[string]*group{}
	for _, handler := range handlers {
		for _, key := range handler.Keys {
			normalized := normalizeMenuMakerKey(key)
			groupKey := "section\x00" + strconv.Itoa(handler.SourceIndex)
			if normalized != "" {
				groupKey = "key\x00" + normalized
			}
			existing, ok := groups[groupKey]
			if !ok {
				existing = &group{key: key}
				groups[groupKey] = existing
				order = append(order, groupKey)
			}
			existing.handlers = append(existing.handlers, handler)
		}
	}
	slots := make([]MenuMakerSlot, 0, len(order))
	for index, groupKey := range order {
		group := groups[groupKey]
		variable := ""
		for _, handler := range group.handlers {
			if len(handler.Assignments) > 0 {
				variable = strings.TrimPrefix(handler.Assignments[0].Variable, "$")
				break
			}
		}
		name := variable
		if name == "" {
			names := make([]string, 0, len(group.handlers))
			for _, handler := range group.handlers {
				names = append(names, handler.Section)
			}
			name = strings.Join(names, " / ")
		}
		originalKeys := []string{}
		for _, handler := range group.handlers {
			originalKeys = append(originalKeys, handler.Keys...)
		}
		slots = append(slots, MenuMakerSlot{
			ID:           fmt.Sprintf("slot-%d-%s", index, safeIdentifier(groupKey)),
			Key:          group.key,
			OriginalKeys: uniqueCaseInsensitive(originalKeys),
			Handlers:     group.handlers,
			Name:         name,
			Skip:         false,
			MergeMode:    "strict",
		})
	}
	return slots
}

func normalizeMenuMakerKey(key string) string {
	normalized := strings.TrimSpace(key)
	normalized = spaceRe.ReplaceAllString(normalized, " ")
	normalized = noModifiersRe.ReplaceAllString(normalized, "")
	normalized = spaceRe.ReplaceAllString(normalized, " ")
	return strings.ToLower(strings.TrimSpace(normalized))
}

func extractActiveInputs(condition string) []string {
	inputs := []string{}
	covered := [][2]int{}
	for _, match := range activeCompareRe.FindAllStringSubmatchIndex(condition, -1) {
		groups := make([]string, 7)
		for i := range 6 {
			start, end := match[(i+1)*2], match[(i+1)*2+1]
			if start >= 0 && end >= 0 {
				groups[i+1] = condition[start:end]
			}
		}
		if groups[1] != "" {
			inputs = append(inputs, groups[1]+" "+normalizeOperator(groups[2])+" "+groups[3])
		} else {
			inputs = append(inputs, groups[6]+" "+reverseOperator(groups[5])+" "+groups[4])
		}
		covered = append(covered, [2]int{match[0], match[1]})
	}
	for _, match := range activeNameRe.FindAllStringIndex(condition, -1) {
		inside := false
		for _, span := range covered {
			if match[0] >= span[0] && match[0] < span[1] {
				inside = true
				break
			}
		}
		if !inside {
			inputs = append(inputs, condition[match[0]:match[1]])
		}
	}
	return uniqueCaseInsensitive(inputs)
}

func collectActiveInputs(sections []MenuMakerSection) []string {
	inputs := []string{}
	for _, section := range sections {
		if section.Name == nil || !strings.HasPrefix(strings.ToLower(*section.Name), "key") {
			continue
		}
		for _, line := range section.Lines {
			trimmed := strings.TrimSpace(line)
			if !conditionAssignRe.MatchString(trimmed) {
				continue
			}
			inputs = append(inputs, extractActiveInputs(strings.TrimSpace(afterEquals(trimmed)))...)
		}
	}
	strictZero := map[string]bool{}
	for _, input := range inputs {
		if strictZeroRe.MatchString(input) {
			strictZero[strings.ToLower(strings.Fields(input)[0])] = true
		}
	}
	filtered := []string{}
	for _, input := range uniqueCaseInsensitive(inputs) {
		if strings.Contains(input, " ") || !strictZero[strings.ToLower(input)] {
			filtered = append(filtered, input)
		}
	}
	return filtered
}

func filterLines(lines []string, pattern *regexp.Regexp) []string {
	out := []string{}
	for _, line := range lines {
		if pattern.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}

func parseValue(line string) string {
	value := strings.TrimSpace(afterEquals(line))
	if value == ";" {
		return value
	}
	return strings.TrimSpace(commentTailRe.ReplaceAllString(value, ""))
}

func afterEquals(line string) string {
	_, rest, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return rest
}

func stripComment(line string) string {
	before, _, _ := strings.Cut(line, ";")
	return strings.TrimSpace(before)
}

func safeIdentifier(value string) string {
	value = strings.TrimPrefix(value, "$")
	value = identifierRe.ReplaceAllString(value, "_")
	if value == "" {
		return "active"
	}
	return value
}

func normalizeOperator(operator string) string {
	if operator == "=" {
		return "=="
	}
	return operator
}

func reverseOperator(operator string) string {
	operator = normalizeOperator(operator)
	switch operator {
	case ">":
		return "<"
	case "<":
		return ">"
	case ">=":
		return "<="
	case "<=":
		return ">="
	default:
		return operator
	}
}

func uniqueCaseInsensitive(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func readOriginalLines(lines []string) ([]string, bool) {
	var encoded string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), originalLinesPrefix) {
			encoded = trimmed
			break
		}
	}
	if encoded == "" {
		return nil, false
	}
	decoded, err := decodeURIComponent(encoded[len(originalLinesPrefix):])
	if err != nil {
		return nil, false
	}
	var parsed any
	if err := json.Unmarshal([]byte(decoded), &parsed); err != nil {
		return nil, false
	}
	items, ok := parsed.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, isString := item.(string)
		if !isString {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

func encodeURIComponent(value string) string {
	var b strings.Builder
	for i := range len(value) {
		c := value[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '!' || c == '~' || c == '*' || c == '\'' || c == '(' || c == ')' {
			b.WriteByte(c)
			continue
		}
		_, _ = fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func decodeURIComponent(value string) (string, error) {
	return url.PathUnescape(strings.ReplaceAll(value, "+", "%2B"))
}
