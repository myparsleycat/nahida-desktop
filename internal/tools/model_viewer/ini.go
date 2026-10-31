package modelviewer

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nahida.live/desktop/internal/platform"
)

const (
	maxModelViewerINIFiles = 64
	maxModelViewerINIDepth = 3
)

type modelViewerINILine struct {
	Text    string
	INIPath string
	LineNo  int
	Section string
}

type modelViewerParsedINI struct {
	Sections  []modINISection
	Lines     map[string][]modelViewerINILine
	Namespace string
}

type modelViewerVariableName struct {
	PublicID string
	Label    string
	Form     string
}

var (
	modelViewerVariableUseRE  = regexp.MustCompile(`\$(\w+)`)
	modelViewerVariableDeclRE = regexp.MustCompile(`(?i)^global\s+(?:persist\s+)?\$(\w+)\b`)
	// Hash-override ZZMI exports bind no Resource\ZZMI slots; the ZZMI-only skin
	// command list is then the evidence that the maps use ZZZ channel packing.
	modelViewerZZMIMarkerRE = regexp.MustCompile(
		`(?i)(?:Resource\\ZZMI\\(?:Diffuse|NormalMap|LightMap|MaterialMap)|^run\s*=\s*CommandListSkinTexture$)`,
	)
	modelViewerWWMIMarkerRE = regexp.MustCompile(
		`(?i)(?:global\s+\$required_wwmi_version\b|(?:Resource|CommandList|\$)\\WWMIv1\\)`,
	)
	modelViewerRabbitFXRE = regexp.MustCompile(
		`(?i)(?:Resource\\RabbitFX\\(?:Diffuse|NormalMap|LightMap|MaterialMap)|run\s*=\s*CommandList\\RabbitFX\\SetTextures\b)`,
	)
	modelViewerScopeTokenRE = regexp.MustCompile(`^` + modelViewerScopeTokenPrefix + `\d+`)
)

const modelViewerScopeTokenPrefix = "mvscope"

// unscopedModelViewerVariable returns the INI name behind a normalized
// variable that scopeModelViewerSections renamed, for checks that look for a
// variable by the name the INI gives it.
func unscopedModelViewerVariable(name string) string {
	return modelViewerScopeTokenRE.ReplaceAllString(name, "")
}

func detectModelViewerMaterialProfile(sections []modINISection) string {
	wwmi, rabbitFX, zzmi := false, false, false
	for _, section := range sections {
		for _, line := range section.Lines {
			wwmi = wwmi || modelViewerWWMIMarkerRE.MatchString(line)
			rabbitFX = rabbitFX || modelViewerRabbitFXRE.MatchString(line)
			zzmi = zzmi || modelViewerZZMIMarkerRE.MatchString(line)
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
			if lhs == "namespace" {
				_, value, _ := strings.Cut(line, "=")
				parsed.Namespace = strings.TrimSpace(value)
			}
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
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ini") ||
			strings.HasPrefix(strings.ToUpper(entry.Name()), "DISABLED") {
			continue
		}
		paths = append(paths, filepath.Join(folder, entry.Name()))
	}
	sort.SliceStable(paths, func(i, j int) bool { return less(filepath.Base(paths[i]), filepath.Base(paths[j])) })
	return paths
}

// modelViewerINIScope names what one INI of a folder contributes: prefix
// scopes its variables' public IDs, meshKey starts its meshes' IDs and form is
// the folder holding it, relative to the mod folder.
type modelViewerINIScope struct {
	prefix, meshKey, form string
}

// modelViewerINIScopes keeps the names an INI's file name gives wherever that
// name is unique, and adds its folder where several share it, as a bundle of
// a character's forms does with one INI name in each form's folder.
func modelViewerINIScopes(iniPaths []string, folder string) []modelViewerINIScope {
	scopes := make([]modelViewerINIScope, len(iniPaths))
	stems := make(map[string]int, len(iniPaths))
	for _, iniPath := range iniPaths {
		stems[strings.ToLower(modelViewerINIStem(iniPath))]++
	}
	for index, iniPath := range iniPaths {
		scope := &scopes[index]
		scope.meshKey = modelViewerNormalizeKey(filepath.Base(iniPath))
		if len(iniPaths) == 1 {
			continue
		}
		scope.form = "."
		if relative, err := filepath.Rel(folder, filepath.Dir(iniPath)); err == nil {
			scope.form = filepath.ToSlash(relative)
		}
		stem := modelViewerINIStem(iniPath)
		if stems[strings.ToLower(stem)] == 1 {
			scope.prefix = stem + "::"
			continue
		}
		scope.prefix = path.Join(scope.form, stem) + "::"
		// Normalized keys drop every character outside a-z and 0-9, which may
		// be the whole folder name.
		sum := sha256.Sum256([]byte(strings.ToLower(scope.form)))
		scope.meshKey += hex.EncodeToString(sum[:4])
	}
	return scopes
}

func modelViewerINIStem(iniPath string) string {
	return strings.TrimSuffix(filepath.Base(iniPath), filepath.Ext(iniPath))
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
func scopeModelViewerSections(
	sections []modINISection,
	scopeIndex int,
	publicPrefix, form string,
) ([]modINISection, map[string]modelViewerVariableName) {
	canonical := canonicalModelViewerVariables(sections)
	internalByLower := make(map[string]string, len(canonical))
	publicByInternal := make(map[string]modelViewerVariableName, len(canonical))
	for lower, name := range canonical {
		internal := modelViewerScopeToken(scopeIndex, name)
		internalByLower[lower] = internal
		publicByInternal[modelViewerNormalizeKey(internal)] = modelViewerVariableName{
			PublicID: publicPrefix + name,
			Label:    name,
			Form:     form,
		}
	}
	output := make([]modINISection, len(sections))
	for index, section := range sections {
		output[index] = modINISection{Header: section.Header, Name: section.Name}
		lines := make([]string, 0, len(section.Lines))
		for _, line := range section.Lines {
			lines = append(lines, modelViewerVariableUseRE.ReplaceAllStringFunc(line, func(token string) string {
				name := strings.TrimPrefix(token, "$")
				if internal := internalByLower[strings.ToLower(name)]; internal != "" {
					return "$" + internal
				}
				return token
			}))
		}
		setModelViewerSectionLines(&output[index], lines)
	}
	return output, publicByInternal
}

func modelViewerScopeToken(scopeIndex int, name string) string {
	return modelViewerScopeTokenPrefix + modelViewerString(scopeIndex) + modelViewerNormalizeKey(name)
}

// setModelViewerSectionLines replaces the lines of a section and rebuilds its
// values from them, as parseModelViewerINI derives them.
func setModelViewerSectionLines(section *modINISection, lines []string) {
	section.Lines = lines
	section.Values = make(map[string]string, len(lines))
	for _, line := range lines {
		if eq := strings.Index(line, "="); eq >= 0 {
			key := strings.TrimSpace(line[:eq])
			section.Values[key] = stripModelViewerQuotes(strings.TrimSpace(line[eq+1:]))
		}
	}
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
		rebased := filepath.Clean(
			filepath.Join(relativeDir, filepath.FromSlash(strings.ReplaceAll(filename, `\`, "/"))),
		)
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
	for candidate := range section.Values {
		if strings.EqualFold(candidate, key) {
			section.Values[candidate] = value
			return
		}
	}
	section.Values[key] = value
}

func stripModelViewerQuotes(value string) string {
	if len(value) >= 2 &&
		(value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}
