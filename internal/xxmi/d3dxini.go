package xxmi

import (
	"bytes"
	"regexp"
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
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 3 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
			continue
		}
		if start >= 0 {
			return start, index
		}
		if strings.EqualFold(strings.TrimSpace(trimmed[1:len(trimmed)-1]), section) {
			start = index
		}
	}
	return start, len(d.lines)
}

func (d *iniDocument) SetOption(section, key, value string, spaced bool) {
	start, end := d.sectionBounds(section)
	if start < 0 {
		if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines, "["+section+"]")
		start, end = len(d.lines)-1, len(d.lines)
		d.changed = true
	}
	for index := start + 1; index < end; index++ {
		match := iniOptionPattern.FindStringSubmatch(d.lines[index])
		if len(match) == 0 || !strings.EqualFold(match[2], key) {
			continue
		}
		replacement := match[1] + match[2] + match[3] + value
		if d.lines[index] != replacement {
			d.lines[index] = replacement
			d.changed = true
		}
		return
	}
	separator := "="
	if spaced {
		separator = " = "
	}
	insert := end
	for insert > start+1 && strings.TrimSpace(d.lines[insert-1]) == "" {
		insert--
	}
	d.lines = append(d.lines[:insert], append([]string{key + separator + value}, d.lines[insert:]...)...)
	d.changed = true
}

func (d *iniDocument) SetOptionUnique(section, key, value string, spaced bool) {
	start, end := d.sectionBounds(section)
	if start >= 0 {
		found := false
		for index := end - 1; index > start; index-- {
			match := iniOptionPattern.FindStringSubmatch(d.lines[index])
			if len(match) == 0 || !strings.EqualFold(match[2], key) {
				continue
			}
			if !found {
				found = true
				continue
			}
			d.lines = append(d.lines[:index], d.lines[index+1:]...)
			d.changed = true
		}
	}
	d.SetOption(section, key, value, spaced)
}

func (d *iniDocument) RemoveOption(section, key string) {
	start, end := d.sectionBounds(section)
	if start < 0 {
		return
	}
	for index := end - 1; index > start; index-- {
		match := iniOptionPattern.FindStringSubmatch(d.lines[index])
		if len(match) == 0 || !strings.EqualFold(match[2], key) {
			continue
		}
		d.lines = append(d.lines[:index], d.lines[index+1:]...)
		d.changed = true
	}
}
