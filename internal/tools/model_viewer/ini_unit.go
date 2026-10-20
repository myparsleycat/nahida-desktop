package modelviewer

import (
	"path/filepath"
	"regexp"
	"strings"
)

// modelViewerINIUnit is the INIs of one 3DMigoto namespace, which reach each
// other's sections and variables by plain name, as one INI. An INI without a
// namespace is a unit of its own.
type modelViewerINIUnit struct {
	namespace string
	// members index the discovered INIs in discovery order.
	members []int
	// primary is the member whose TextureOverrides draw the unit's meshes.
	primary int
}

// modelViewerScopedUnit holds the merged, scoped sections of a unit and the
// member each section came from.
type modelViewerScopedUnit struct {
	sections []modINISection
	origins  []int
	names    map[string]modelViewerVariableName
}

var (
	modelViewerShaderStageKeys = map[string]bool{"vs": true, "hs": true, "ds": true, "gs": true, "ps": true, "cs": true}
	modelViewerForeignVarRE    = regexp.MustCompile(`\$\\([\w.\\-]+)\\(\w+)`)
)

func groupModelViewerINIUnits(parsed []modelViewerParsedINI) []modelViewerINIUnit {
	var units []modelViewerINIUnit
	byNamespace := make(map[string]int)
	for index, ini := range parsed {
		namespace := strings.ToLower(ini.Namespace)
		if existing, ok := byNamespace[namespace]; ok && namespace != "" {
			units[existing].members = append(units[existing].members, index)
			continue
		}
		if namespace != "" {
			byNamespace[namespace] = len(units)
		}
		units = append(units, modelViewerINIUnit{namespace: ini.Namespace, members: []int{index}, primary: index})
	}

	for unitIndex := range units {
		unit := &units[unitIndex]
		for _, member := range unit.members {
			if modelViewerHasTextureOverride(parsed[member].Sections) {
				unit.primary = member
				break
			}
		}
	}
	return units
}

func modelViewerHasTextureOverride(sections []modINISection) bool {
	for _, section := range sections {
		if strings.EqualFold(section.Header, "TextureOverride") {
			return true
		}
	}
	return false
}

// scopeModelViewerUnit merges the members of a unit into one INI and scopes its
// variables under scopeIndex. Variable references into the namespace of another
// unit resolve to the variables that unit scopes.
func scopeModelViewerUnit(
	unit modelViewerINIUnit,
	scopeIndex int,
	parsed []modelViewerParsedINI,
	iniPaths []string,
	scopes []modelViewerINIScope,
	unitByNamespace map[string]int,
	folder string,
) modelViewerScopedUnit {
	primaryDir := filepath.Dir(iniPaths[unit.primary])
	var merged []modINISection
	var origins []int
	declaredBy := make(map[string]int)
	for _, member := range unit.members {
		selfRE := modelViewerNamespaceSelfRE(parsed[member].Namespace)
		memberDir := filepath.Dir(iniPaths[member])
		for _, section := range parsed[member].Sections {
			lines := make([]string, 0, len(section.Lines))
			for _, line := range section.Lines {
				if selfRE != nil && !strings.EqualFold(strings.TrimSpace(strings.SplitN(line, "=", 2)[0]), "filename") {
					line = selfRE.ReplaceAllString(line, "${1}")
				}
				if memberDir != primaryDir && strings.EqualFold(section.Header, "CustomShader") {
					line = rebaseModelViewerShaderLine(line, memberDir, primaryDir)
				}
				if match := modelViewerVariableDeclRE.FindStringSubmatch(strings.TrimSpace(line)); len(match) == 2 {
					if _, exists := declaredBy[strings.ToLower(match[1])]; !exists {
						declaredBy[strings.ToLower(match[1])] = member
					}
				}
				lines = append(lines, line)
			}
			merged = append(merged, modINISection{Header: section.Header, Name: section.Name, Lines: lines})
			origins = append(origins, member)
		}
	}

	primaryScope := scopes[unit.primary]
	sections, names := scopeModelViewerSections(merged, scopeIndex, primaryScope.prefix, primaryScope.form)
	for key, name := range names {
		if member, ok := declaredBy[strings.ToLower(name.Label)]; ok {
			name.PublicID = scopes[member].prefix + name.Label
			names[key] = name
		}
	}
	for index := range sections {
		section := &sections[index]
		if !modelViewerForeignVarRE.MatchString(strings.Join(section.Lines, "\n")) {
			continue
		}
		lines := make([]string, len(section.Lines))
		for lineIndex, line := range section.Lines {
			lines[lineIndex] = modelViewerForeignVarRE.ReplaceAllStringFunc(line, func(token string) string {
				match := modelViewerForeignVarRE.FindStringSubmatch(token)
				target, ok := unitByNamespace[strings.ToLower(match[1])]
				if !ok {
					return token
				}
				return "$" + modelViewerScopeToken(target, match[2])
			})
		}
		setModelViewerSectionLines(section, lines)
	}

	// Resource paths are relative to the INI that declares them, and scoping
	// rebuilt the values the rebase rewrites.
	for start := 0; start < len(sections); {
		end := start
		for end < len(sections) && origins[end] == origins[start] {
			end++
		}
		rebaseModelViewerResources(sections[start:end], iniPaths[origins[start]], folder)
		start = end
	}
	return modelViewerScopedUnit{sections: sections, origins: origins, names: names}
}

// attributeModelViewerDrawSources points the draw source of each mesh at the
// member declaring the TextureOverride it draws from, which is the INI a tool
// hooking that draw edits.
func attributeModelViewerDrawSources(meshes []modelViewerDirectMesh, unit modelViewerScopedUnit, iniPaths []string) {
	declaring := make(map[string]string)
	for index, section := range unit.sections {
		if strings.EqualFold(section.Header, "TextureOverride") {
			declaring[modelViewerNormalizeKey(section.Name)] = iniPaths[unit.origins[index]]
		}
	}
	for index := range meshes {
		source := meshes[index].drawSource
		if source == nil {
			continue
		}
		if iniPath, ok := declaring[modelViewerNormalizeKey(source.Section)]; ok {
			source.INIPath = iniPath
		}
	}
}

// modelViewerNamespaceSelfRE matches the namespace qualifier of a reference
// into the INI's own namespace, as in CommandList\NS\Name or $\NS\name, which
// names the same section or variable as the unqualified spelling.
func modelViewerNamespaceSelfRE(namespace string) *regexp.Regexp {
	if namespace == "" {
		return nil
	}
	return regexp.MustCompile(
		`(?i)(\$|\b(?:CommandList|Resource|CustomShader|ShaderOverride|TextureOverride|ShaderRegex|Preset|Key))\\` +
			regexp.QuoteMeta(namespace) + `\\`,
	)
}

// rebaseModelViewerShaderLine makes a shader file of a CustomShader declared in
// memberDir relative to primaryDir, where the unit resolves shader paths.
func rebaseModelViewerShaderLine(line, memberDir, primaryDir string) string {
	key, value, ok := strings.Cut(line, "=")
	if !ok || !modelViewerShaderStageKeys[strings.ToLower(strings.TrimSpace(key))] {
		return line
	}
	path := stripModelViewerQuotes(strings.TrimSpace(value))
	if path == "" || strings.EqualFold(path, "null") || filepath.IsAbs(path) {
		return line
	}
	relative, err := filepath.Rel(
		primaryDir,
		filepath.Join(memberDir, filepath.FromSlash(strings.ReplaceAll(path, `\`, "/"))),
	)
	if err != nil {
		return line
	}
	return strings.TrimSpace(key) + " = " + relative
}
