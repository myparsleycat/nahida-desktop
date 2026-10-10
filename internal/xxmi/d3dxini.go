package xxmi

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
)

var iniOptionPattern = regexp.MustCompile(`^(\s*)([^\s=;#]+)(\s*=\s*)(.*)$`)

type iniDocument struct {
	bom      []byte
	lines    []string
	newline  string
	terminal bool
	changed  bool
}

func parseINI(data []byte) *iniDocument {
	doc := &iniDocument{newline: "\n"}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		doc.bom = []byte{0xef, 0xbb, 0xbf}
		data = data[3:]
	}
	if bytes.Contains(data, []byte("\r\n")) {
		doc.newline = "\r\n"
	}
	text := string(data)
	doc.terminal = strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\r")
	if text != "" {
		doc.lines = strings.Split(text, doc.newline)
	}
	return doc
}

func (d *iniDocument) Bytes() []byte {
	text := strings.Join(d.lines, d.newline)
	if d.terminal && len(d.lines) > 0 {
		text += d.newline
	}
	return append(append([]byte(nil), d.bom...), text...)
}

func (d *iniDocument) Changed() bool { return d.changed }

func (d *iniDocument) sectionBounds(section string) (int, int) {
	start := -1
	for index, line := range d.lines {
		name, ok := iniSectionName(line)
		if !ok {
			continue
		}
		if start >= 0 {
			return start, index
		}
		if strings.EqualFold(name, section) {
			start = index
		}
	}
	return start, len(d.lines)
}

func (d *iniDocument) SetOption(section, key, value string, spaced bool) {
	indexes := d.optionIndexes(section, key)
	if len(indexes) > 0 {
		first := indexes[0]
		match := iniOptionPattern.FindStringSubmatch(d.lines[first])
		replacement := match[1] + match[2] + match[3] + value + iniCommentSuffix(match[4])
		if d.lines[first] != replacement {
			d.lines[first] = replacement
			d.changed = true
		}
		for index := len(indexes) - 1; index > 0; index-- {
			duplicate := indexes[index]
			d.lines = append(d.lines[:duplicate], d.lines[duplicate+1:]...)
			d.changed = true
		}
		return
	}

	separator := "="
	if spaced {
		separator = " = "
	}
	insert := d.sectionEnd(section)
	d.lines = slices.Insert(d.lines, insert, key+separator+value)
	d.changed = true
}

// sectionEnd returns the line index that appends to the section, ahead of the blank lines that close it. A
// missing section is created at the end of the document.
func (d *iniDocument) sectionEnd(section string) int {
	start, end := d.sectionBounds(section)
	if start < 0 {
		if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines, "["+section+"]")
		start, end = len(d.lines)-1, len(d.lines)
		d.changed = true
	}
	for end > start+1 && strings.TrimSpace(d.lines[end-1]) == "" {
		end--
	}
	return end
}

// AddMissingOptions copies the options of template that d has neither set nor commented out to the end of
// their sections, each with the comment lines directly above it. A commented-out option was turned off on
// purpose and stays that way. Sections that hold commands rather than settings are left alone.
func (d *iniDocument) AddMissingOptions(template *iniDocument) {
	section, skipped := "", true
	for index, line := range template.lines {
		if name, ok := iniSectionName(line); ok {
			section, skipped = name, !iniSettingsSection(name)
			continue
		}
		if skipped {
			continue
		}
		match := iniOptionPattern.FindStringSubmatch(line)
		if len(match) == 0 || !iniSettingKeyPattern.MatchString(match[2]) {
			continue
		}
		if d.mentionsOption(section, match[2]) {
			continue
		}

		// A commented-out option among those lines would read as one the user turned off on the next merge.
		first := index
		for first > 0 && strings.HasPrefix(strings.TrimSpace(template.lines[first-1]), ";") &&
			iniMentionedKey(template.lines[first-1]) == "" {
			first--
		}
		insert := d.sectionEnd(section)
		d.lines = slices.Insert(d.lines, insert, template.lines[first:index+1]...)
		d.changed = true
	}
}

var iniSettingKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// iniSettingsSection reports whether a d3dx.ini section holds independent options. The others are command
// lists, overrides, and sections the launch writes itself, where a line means something only in its place.
func iniSettingsSection(name string) bool {
	name = strings.ToLower(name)
	if slices.Contains([]string{
		"loader", "include", "constants", "present", "profile", "clearrendertargetview", "cleardepthstencilview",
		"clearunorderedaccessviewuint", "clearunorderedaccessviewfloat",
	}, name) {
		return false
	}
	return !slices.ContainsFunc([]string{
		"key", "commandlist", "builtincommandlist", "shaderoverride", "shaderregex", "textureoverride",
		"resource", "customshader", "builtincustomshader", "preset",
	}, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}

func iniSectionName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return "", false
	}
	return strings.TrimSpace(trimmed[1 : len(trimmed)-1]), true
}

// mentionsOption reports whether the section has the option, set or commented out.
func (d *iniDocument) mentionsOption(section, key string) bool {
	inSection := false
	for _, line := range d.lines {
		if name, ok := iniSectionName(line); ok {
			inSection = strings.EqualFold(name, section)
			continue
		}
		if !inSection {
			continue
		}
		if strings.EqualFold(iniMentionedKey(line), key) {
			return true
		}
	}
	return false
}

// iniMentionedKey returns the key of an option line, set or commented out, or "" for any other line.
func iniMentionedKey(line string) string {
	match := iniOptionPattern.FindStringSubmatch(strings.TrimLeft(line, " \t;#"))
	if len(match) == 0 {
		return ""
	}
	return match[2]
}

func iniCommentSuffix(value string) string {
	quote := byte(0)
	for index := range len(value) {
		char := value[index]
		if quote != 0 {
			if char == quote && (index == 0 || value[index-1] != '\\') {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if (char != ';' && char != '#') || index == 0 || value[index-1] != ' ' && value[index-1] != '\t' {
			continue
		}
		start := index - 1
		for start > 0 && (value[start-1] == ' ' || value[start-1] == '\t') {
			start--
		}
		return value[start:]
	}
	return ""
}

func (d *iniDocument) SetOptionUnique(section, key, value string, spaced bool) {
	d.SetOption(section, key, value, spaced)
}

func (d *iniDocument) RemoveOption(section, key string) {
	indexes := d.optionIndexes(section, key)
	for index := len(indexes) - 1; index >= 0; index-- {
		match := indexes[index]
		d.lines = append(d.lines[:match], d.lines[match+1:]...)
		d.changed = true
	}
}

// Option returns the value the section sets for key, without the comment that may follow it.
func (d *iniDocument) Option(section, key string) (string, bool) {
	indexes := d.optionIndexes(section, key)
	if len(indexes) == 0 {
		return "", false
	}
	value := iniOptionPattern.FindStringSubmatch(d.lines[indexes[0]])[4]
	return strings.TrimSpace(strings.TrimSuffix(value, iniCommentSuffix(value))), true
}

func (d *iniDocument) optionIndexes(section, key string) []int {
	indexes := []int{}
	inSection := false
	for index, line := range d.lines {
		if name, ok := iniSectionName(line); ok {
			inSection = strings.EqualFold(name, section)
			continue
		}
		if !inSection {
			continue
		}
		match := iniOptionPattern.FindStringSubmatch(line)
		if len(match) != 0 && strings.EqualFold(match[2], key) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}
