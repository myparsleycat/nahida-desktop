package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nahida.live/desktop/internal/platform"
)

const (
	maxModelViewerINIFiles = 10
	maxModelViewerINIDepth = 2
)

type modelViewerINILine struct {
	Text    string
	INIPath string
	LineNo  int
	Section string
}

type modelViewerParsedINI struct {
	Sections []modINISection
	Lines    map[string][]modelViewerINILine
}

type modelViewerVariableName struct {
	PublicID string
	Label    string
}

var (
	modelViewerVariableUseRE  = regexp.MustCompile(`\$(\w+)`)
	modelViewerVariableDeclRE = regexp.MustCompile(`(?i)^global\s+(?:persist\s+)?\$(\w+)\b`)
	modelViewerZZMIResourceRE = regexp.MustCompile(`(?i)Resource\\ZZMI\\(?:Diffuse|NormalMap|LightMap|MaterialMap)`)
	modelViewerWWMIMarkerRE   = regexp.MustCompile(`(?i)(?:global\s+\$required_wwmi_version\b|(?:Resource|CommandList|\$)\\WWMIv1\\)`)
	modelViewerRabbitFXRE     = regexp.MustCompile(`(?i)(?:Resource\\RabbitFX\\(?:Diffuse|NormalMap|LightMap|MaterialMap)|run\s*=\s*CommandList\\RabbitFX\\SetTextures\b)`)
)

func detectModelViewerMaterialProfile(sections []modINISection) string {
	wwmi, rabbitFX, zzmi := false, false, false
	for _, section := range sections {
		for _, line := range section.Lines {
			wwmi = wwmi || modelViewerWWMIMarkerRE.MatchString(line)
			rabbitFX = rabbitFX || modelViewerRabbitFXRE.MatchString(line)
			zzmi = zzmi || modelViewerZZMIResourceRE.MatchString(line)
		}
	}
	if wwmi && rabbitFX {
		return "wuwa:rabbitfx"
	}
	if zzmi {
		return "zzmi"
	}
	return ""
}

// parseModelViewerINI is the Go equivalent of Electron's parseIniText. It is
// intentionally separate from parseModINI because the latter is shared by
// unrelated tools and has different comment and duplicate-section semantics.
func parseModelViewerINI(text, iniPath string) modelViewerParsedINI {
	parsed := modelViewerParsedINI{Lines: make(map[string][]modelViewerINILine)}
	sectionIndexes := make(map[string]int)
	current := ""
	text = strings.TrimPrefix(text, "\ufeff")
	for index, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		lhs := line
		if eq := strings.Index(lhs, "="); eq >= 0 {
			lhs = lhs[:eq]
		}
		lhs = strings.ToLower(strings.TrimSpace(lhs))
		if lhs != "key" && lhs != "back" {
			if comment := strings.Index(line, ";"); comment >= 0 {
				line = strings.TrimSpace(line[:comment])
			}
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if _, exists := sectionIndexes[current]; !exists {
				header, name := current, current
				if match := modINISectionRE.FindStringSubmatch(current); len(match) == 3 {
					header, name = match[1], match[2]
				}
				sectionIndexes[current] = len(parsed.Sections)
				parsed.Sections = append(parsed.Sections, modINISection{
					Header: header,
					Name:   name,
					Values: make(map[string]string),
				})
			}
			continue
		}
		if current == "" {
			continue
		}
		sectionIndex := sectionIndexes[current]
		section := &parsed.Sections[sectionIndex]
		section.Lines = append(section.Lines, line)
		parsed.Lines[current] = append(parsed.Lines[current], modelViewerINILine{
			Text: line, INIPath: iniPath, LineNo: index + 1, Section: current,
		})
		if eq := strings.Index(line, "="); eq >= 0 {
			key := strings.TrimSpace(line[:eq])
			value := stripModelViewerQuotes(strings.TrimSpace(line[eq+1:]))
			section.Values[key] = value
		}
	}
	return parsed
}

func readModelViewerINI(iniPath string) (modelViewerParsedINI, error) {
	raw, err := os.ReadFile(iniPath)
	if err != nil {
		return modelViewerParsedINI{}, err
	}
	return parseModelViewerINI(string(raw), iniPath), nil
}

func discoverModelViewerActiveINIs(folder string, reports ...func(error)) ([]string, error) {
	less := platform.NewLocaleLess()
	paths := activeModelViewerINIs(folder, less, reports...)
	if len(paths) >= maxModelViewerINIFiles {
		return paths, nil
	}
	var walk func(string, int)
	walk = func(current string, depth int) {
		if len(paths) >= maxModelViewerINIFiles {
			return
		}
		entries, err := os.ReadDir(current)
		if err != nil {
			for _, report := range reports {
				report(err)
			}
			return
		}
		var dirs []os.DirEntry
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, entry)
			}
		}
		sort.SliceStable(dirs, func(i, j int) bool { return less(dirs[i].Name(), dirs[j].Name()) })
		if depth > 0 {
			for _, iniPath := range activeModelViewerINIs(current, less, reports...) {
				paths = append(paths, iniPath)
				if len(paths) >= maxModelViewerINIFiles {
					return
				}
			}
		}
		if depth >= maxModelViewerINIDepth {
			return
		}
		for _, dir := range dirs {
			walk(filepath.Join(current, dir.Name()), depth+1)
			if len(paths) >= maxModelViewerINIFiles {
				return
			}
		}
	}
	walk(folder, 0)
	return paths, nil
}

func activeModelViewerINIs(folder string, less func(string, string) bool, reports ...func(error)) []string {
	entries, err := os.ReadDir(folder)
	if err != nil {
		for _, report := range reports {
			report(err)
		}
		return nil
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ini") || strings.HasPrefix(strings.ToUpper(entry.Name()), "DISABLED") {
			continue
		}
		paths = append(paths, filepath.Join(folder, entry.Name()))
	}
	sort.SliceStable(paths, func(i, j int) bool { return less(filepath.Base(paths[i]), filepath.Base(paths[j])) })
	return paths
}

func modelViewerINIScope(iniPath, folder string, multi bool) (string, string) {
	if !multi {
		return "", ""
	}
	parent := filepath.Clean(filepath.Dir(iniPath))
	root := filepath.Clean(folder)
	source := strings.TrimSuffix(filepath.Base(iniPath), filepath.Ext(iniPath))
	if !strings.EqualFold(parent, root) {
		if relative, err := filepath.Rel(root, parent); err == nil {
			source = filepath.ToSlash(relative)
		}
	}
	identity := strings.TrimSuffix(filepath.Base(iniPath), filepath.Ext(iniPath))
	return identity + "::", source
}

func canonicalModelViewerVariables(sections []modINISection) map[string]string {
	declared := make(map[string]string)
	seen := make(map[string]string)
	for _, section := range sections {
		for _, line := range section.Lines {
			trimmed := strings.TrimSpace(line)
			if match := modelViewerVariableDeclRE.FindStringSubmatch(trimmed); len(match) == 2 {
				key := strings.ToLower(match[1])
				if _, exists := declared[key]; !exists {
					declared[key] = match[1]
				}
			}
			for _, match := range modelViewerVariableUseRE.FindAllStringSubmatch(trimmed, -1) {
				key := strings.ToLower(match[1])
				if _, exists := seen[key]; !exists {
					seen[key] = match[1]
				}
			}
		}
	}
	for key, name := range seen {
		if _, exists := declared[key]; !exists {
			declared[key] = name
		}
	}
	return declared
}

// scopeModelViewerSections assigns a collision-free internal token to each INI
// variable. Public IDs are restored only after the Electron-compatible payload
// has been assembled.
func scopeModelViewerSections(sections []modINISection, scopeIndex int, publicPrefix string) ([]modINISection, map[string]modelViewerVariableName) {
	canonical := canonicalModelViewerVariables(sections)
	internalByLower := make(map[string]string, len(canonical))
	publicByInternal := make(map[string]modelViewerVariableName, len(canonical))
	for lower, name := range canonical {
		internal := "mvscope" + modelViewerString(scopeIndex) + modelViewerNormalizeKey(name)
		internalByLower[lower] = internal
		publicByInternal[modelViewerNormalizeKey(internal)] = modelViewerVariableName{
			PublicID: publicPrefix + name,
			Label:    name,
		}
	}
	output := make([]modINISection, len(sections))
	for index, section := range sections {
		output[index] = modINISection{Header: section.Header, Name: section.Name, Values: make(map[string]string)}
		for _, line := range section.Lines {
			rewritten := modelViewerVariableUseRE.ReplaceAllStringFunc(line, func(token string) string {
				name := strings.TrimPrefix(token, "$")
				if internal := internalByLower[strings.ToLower(name)]; internal != "" {
					return "$" + internal
				}
				return token
			})
			output[index].Lines = append(output[index].Lines, rewritten)
			if eq := strings.Index(rewritten, "="); eq >= 0 {
				key := strings.TrimSpace(rewritten[:eq])
				value := stripModelViewerQuotes(strings.TrimSpace(rewritten[eq+1:]))
				output[index].Values[key] = value
			}
		}
	}
	return output, publicByInternal
}

func rebaseModelViewerResources(sections []modINISection, iniPath, folder string) {
	relativeDir, err := filepath.Rel(folder, filepath.Dir(iniPath))
	if err != nil || relativeDir == "." || relativeDir == "" {
		return
	}
	for index := range sections {
		section := &sections[index]
		if !strings.EqualFold(section.Header, "Resource") {
			continue
		}
		filename := modelViewerSectionValue(*section, "filename")
		if filename == "" {
			continue
		}
		rebased := filepath.Clean(filepath.Join(relativeDir, filepath.FromSlash(strings.ReplaceAll(filename, `\`, "/"))))
		setModelViewerSectionValue(section, "filename", rebased)
	}
}

func modelViewerSectionValue(section modINISection, key string) string {
	for candidate, value := range section.Values {
		if strings.EqualFold(candidate, key) {
			return value
		}
	}
	return ""
}

func setModelViewerSectionValue(section *modINISection, key, value string) {
	if section == nil {
		return
	}
	for candidate := range section.Values {
		if strings.EqualFold(candidate, key) {
			section.Values[candidate] = value
			return
		}
	}
	section.Values[key] = value
}

func stripModelViewerQuotes(value string) string {
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}
