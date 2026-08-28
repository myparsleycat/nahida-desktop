package mod

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

type mergeINILineKind int

const (
	mergeINIBlank mergeINILineKind = iota
	mergeINIComment
	mergeINIHeader
	mergeINIKV
	mergeINIOther
)

type mergeINILine struct {
	kind   mergeINILineKind
	raw    string
	name   string
	key    string
	value  string
	indent string
}

type mergeINISection struct {
	header string
	name   string
	lines  []mergeINILine
}

type parsedMergeINI struct {
	preamble []mergeINILine
	sections []mergeINISection
}

var (
	mergeINIHeaderRE        = regexp.MustCompile(`^\[([^\]]+)\]\s*$`)
	mergedModHeaderRE       = regexp.MustCompile(`(?i)^\s*;\s*(?:merged mods?|合并mod)\s*:\s*(.+)$`)
	mergedINICommaSplitRE   = regexp.MustCompile(`(?i)^(.+?\.ini["']?)\s*,\s*(.*)$`)
	mergeNamespaceLineRE    = regexp.MustCompile(`(?im)^\s*namespace\s*=\s*([^;\r\n]+)`)
	masterSwapRefRE         = regexp.MustCompile(`(?i)\$\\[^\\\s]+\\Master\\swapvar\w*`)
	masterSwapIfRE          = regexp.MustCompile(`(?i)^if\s+\(?\s*\$\\[^\\\s]+\\master\\swapvar\w*(?:\s*==\s*\S+\s*\)?)?\s*(?:;.*)?$`)
	masterSwapElseIfRE      = regexp.MustCompile(`(?i)^(?:else\s+if|elif)\s+\(?\s*\$\\[^\\\s]+\\master\\swapvar\w*(?:\s*==\s*\S+\s*\)?)?\s*(?:;.*)?$`)
	masterSwapElseRE        = regexp.MustCompile(`(?i)^else\s*(?:;.*)?$`)
	compoundMasterSwapRE    = regexp.MustCompile(`(?i)^(?:if|else\s+if|elif)\s+\(?\s*\$\\[^\\\s]+\\master\\swapvar\w*.*(?:&&|\|\|)`)
	existingSwapSlotRE      = regexp.MustCompile(`(?i)\$\\[^\\\s]+\\Master\\swapvar\w*\s*==\s*(\d+)`)
	positionSectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)position$`),
		regexp.MustCompile(`(?i)markbonedatacb$`),
		regexp.MustCompile(`(?i)headblend$`),
		regexp.MustCompile(`(?i)hairblend$`),
		regexp.MustCompile(`(?i)textureoverride(?:_?)component0(?:_lod0)?$`),
		regexp.MustCompile(`(?i)textureoverride\w+ib$`),
	}
)

func parseMergeINI(text string) parsedMergeINI {
	parsed := parsedMergeINI{}
	current := -1
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := classifyMergeINILine(raw)
		if line.kind == mergeINIHeader {
			parsed.sections = append(parsed.sections, mergeINISection{
				header: line.raw, name: line.name,
			})
			current = len(parsed.sections) - 1
			continue
		}
		if current >= 0 {
			parsed.sections[current].lines = append(parsed.sections[current].lines, line)
			continue
		}
		parsed.preamble = append(parsed.preamble, line)
	}
	return parsed
}

func serializeMergeINI(parsed parsedMergeINI) string {
	lines := make([]string, 0, len(parsed.preamble)+len(parsed.sections)*8)
	for _, line := range parsed.preamble {
		lines = append(lines, line.raw)
	}
	for _, section := range parsed.sections {
		lines = append(lines, section.header)
		for _, line := range section.lines {
			lines = append(lines, line.raw)
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func classifyMergeINILine(raw string) mergeINILine {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return mergeINILine{kind: mergeINIBlank, raw: raw}
	}
	if strings.HasPrefix(strings.TrimLeft(raw, " \t"), ";") {
		return mergeINILine{kind: mergeINIComment, raw: raw}
	}
	if match := mergeINIHeaderRE.FindStringSubmatch(trimmed); match != nil {
		return mergeINILine{kind: mergeINIHeader, raw: raw, name: strings.TrimSpace(match[1])}
	}
	indent := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]
	if eq := strings.Index(raw, "="); eq >= 0 {
		return mergeINILine{
			kind:   mergeINIKV,
			raw:    raw,
			key:    strings.TrimSpace(raw[:eq]),
			value:  strings.TrimSpace(raw[eq+1:]),
			indent: indent,
		}
	}
	return mergeINILine{kind: mergeINIOther, raw: raw}
}

func mergeINISectionValues(section mergeINISection) map[string]string {
	values := map[string]string{}
	for _, line := range section.lines {
		if line.kind == mergeINIKV {
			values[strings.ToLower(line.key)] = line.value
		}
	}
	return values
}

func extractMergedModPaths(text string) []string {
	var rawMatches []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[;") {
			break
		}
		if match := mergedModHeaderRE.FindStringSubmatch(raw); len(match) == 2 {
			rawMatches = append(rawMatches, strings.TrimSpace(match[1]))
		}
	}
	if len(rawMatches) == 0 {
		return nil
	}
	var out []string
	for _, rawMatch := range rawMatches {
		if rawMatch == "" {
			continue
		}
		if strings.HasPrefix(rawMatch, "[") && strings.HasSuffix(rawMatch, "]") {
			var list []any
			if json.Unmarshal([]byte(rawMatch), &list) == nil {
				var items []string
				for _, item := range list {
					value, ok := item.(string)
					if !ok || strings.TrimSpace(value) == "" {
						continue
					}
					items = append(items, strings.TrimSpace(value))
				}
				out = append(out, items...)
				continue
			}
		}
		if strings.HasPrefix(rawMatch, `"`) && strings.HasSuffix(rawMatch, `"`) {
			var value string
			if json.Unmarshal([]byte(rawMatch), &value) == nil && strings.TrimSpace(value) != "" {
				out = append(out, strings.TrimSpace(value))
				continue
			}
		}
		if strings.Contains(rawMatch, ",") {
			parts := splitMergedModCommaList(rawMatch)
			if len(parts) > 1 {
				allINI := true
				for _, part := range parts {
					if !strings.HasSuffix(strings.ToLower(part), ".ini") {
						allINI = false
						break
					}
				}
				if allINI {
					out = append(out, parts...)
					continue
				}
			}
		}
		out = append(out, strings.Trim(strings.TrimSpace(rawMatch), `"'`))
	}
	return out
}

func splitMergedModCommaList(raw string) []string {
	var parts []string
	remaining := raw
	for {
		match := mergedINICommaSplitRE.FindStringSubmatch(remaining)
		if match == nil {
			if trimmed := strings.Trim(strings.TrimSpace(remaining), `"'`); trimmed != "" {
				parts = append(parts, trimmed)
			}
			return parts
		}
		parts = append(parts, strings.Trim(strings.TrimSpace(match[1]), `"'`))
		remaining = match[2]
	}
}

func extractMergeNamespace(text string) string {
	match := mergeNamespaceLineRE.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func extractPositionSectionHash(text string) string {
	parsed := parseMergeINI(text)
	for _, pattern := range positionSectionPatterns {
		for _, section := range parsed.sections {
			if !pattern.MatchString(section.name) {
				continue
			}
			if hash := strings.ToLower(strings.TrimSpace(mergeINISectionValues(section)["hash"])); hash != "" {
				return hash
			}
		}
	}
	return ""
}

func extractPositionHash(text string) string {
	if hash := extractPositionSectionHash(text); hash != "" {
		return hash
	}
	hashes := extractMergeHashes(text)
	if len(hashes) == 0 {
		return ""
	}
	return hashes[0]
}

func extractPositionSectionMatchLines(text string) []string {
	parsed := parseMergeINI(text)
	var section *mergeINISection
	for _, pattern := range positionSectionPatterns {
		for i := range parsed.sections {
			entry := &parsed.sections[i]
			if pattern.MatchString(entry.name) && mergeINISectionValues(*entry)["hash"] != "" {
				section = entry
				break
			}
		}
		if section != nil {
			break
		}
	}
	if section == nil {
		for i := range parsed.sections {
			if mergeINISectionValues(parsed.sections[i])["hash"] != "" {
				section = &parsed.sections[i]
				break
			}
		}
	}
	if section == nil {
		return nil
	}
	var lines []string
	for _, line := range section.lines {
		if line.kind != mergeINIKV {
			continue
		}
		key := strings.ToLower(line.key)
		if key == "hash" || key == "match_priority" || key == "allow_duplicate_hash" {
			continue
		}
		if !isOverrideMatchKey(line.key) {
			continue
		}
		lines = append(lines, line.key+" = "+line.value)
	}
	return lines
}

func hasMasterSwapRef(text string) bool {
	return masterSwapRefRE.MatchString(text)
}

func hasCompoundMasterSwap(text string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if compoundMasterSwapRE.MatchString(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func isFileDisabledIniName(fileName string) bool {
	lower := strings.ToLower(fileName)
	return strings.HasPrefix(lower, "disabled") && strings.HasSuffix(lower, ".ini")
}

func isBackupIniName(fileName string) bool {
	return strings.HasPrefix(strings.ToLower(fileName), "disabled_backup_")
}

func isHelperIniName(fileName string) bool {
	lower := strings.ToLower(fileName)
	return isSupportININame(lower) || strings.Contains(lower, "orfix")
}

func isMasterIniPath(iniPath string) bool {
	base := strings.ToLower(filepath.Base(iniPath))
	return strings.HasPrefix(base, "master") && strings.HasSuffix(base, ".ini")
}
