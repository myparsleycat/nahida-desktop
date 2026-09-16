package modelviewer

import (
	"os"
	"regexp"
	"strings"
)

type modINISection struct {
	Header string
	Name   string
	Lines  []string
	Values map[string]string
}

var (
	modINIHeaderRE  = regexp.MustCompile(`^\[([^\]]+)\]$`)
	modINISectionRE = regexp.MustCompile(
		`(?i)^(TextureOverride|ShaderOverride|Resource|Constants|Present|CommandList|CustomShader)(.*)$`,
	)
)

func parseModINI(text string) []modINISection {
	sections := make([]modINISection, 0)
	current := -1
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if match := modINIHeaderRE.FindStringSubmatch(line); len(match) == 2 {
			full := strings.TrimSpace(match[1])
			header, name := full, full
			if kind := modINISectionRE.FindStringSubmatch(full); len(kind) == 3 {
				header, name = kind[1], kind[2]
			}
			sections = append(sections, modINISection{Header: header, Name: name, Values: make(map[string]string)})
			current = len(sections) - 1
			continue
		}
		if current < 0 {
			continue
		}
		sections[current].Lines = append(sections[current].Lines, strings.TrimSpace(line))
		if index := strings.Index(line, "="); index >= 0 {
			sections[current].Values[strings.TrimSpace(line[:index])] = strings.TrimSpace(line[index+1:])
		}
	}
	return sections
}

func sectionValue(lines []string, key string) string {
	for index := len(lines) - 1; index >= 0; index-- {
		separator := strings.Index(lines[index], "=")
		if separator >= 0 && strings.EqualFold(strings.TrimSpace(lines[index][:separator]), key) {
			return strings.TrimSpace(lines[index][separator+1:])
		}
	}
	return ""
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
