package modelviewer

import (
	"path/filepath"
	"regexp"
	"slices"
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
	// own counts the leading sections that come from the unit's members; the
	// rest are imported from other namespaces.
	own   int
	names map[string]modelViewerVariableName
}

var (
	modelViewerShaderStageKeys = map[string]bool{
		"vs": true,
		"hs": true,
		"ds": true,
		"gs": true,
		"ps": true,
		"cs": true,
	}
	modelViewerForeignVarRE     = regexp.MustCompile(`\$\\([\w.\\-]+)\\(\w+)`)
	modelViewerForeignSectionRE = regexp.MustCompile(`(?i)\b(CommandList|CustomShader|Resource)\\([\w.\\-]+)\\(\w+)`)
	// The leading group keeps a variable such as $ResourceCount and the name part
	// of a qualified reference from reading as a section reference.
	modelViewerPlainSectionRE = regexp.MustCompile(`(?i)(^|[^\w$\\])(CommandList|CustomShader|Resource)(\w+)`)
	modelViewerNonWordRE      = regexp.MustCompile(`\W+`)
	modelViewerShaderSlotRE   = regexp.MustCompile(`^[vhdgpc]s-(?:t|u|cb)\d+$`)
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
// variables under scopeIndex, the unit's index in units. Variable references
// into the namespace of another unit resolve to the variables that unit scopes,
// and the sections it references there are imported.
func scopeModelViewerUnit(
	units []modelViewerINIUnit,
	scopeIndex int,
	parsed []modelViewerParsedINI,
	iniPaths []string,
	scopes []modelViewerINIScope,
	unitByNamespace map[string]int,
	folder string,
) modelViewerScopedUnit {
	unit := units[scopeIndex]
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
	own := len(merged)
	merged, origins = importModelViewerForeignSections(
		merged,
		origins,
		units,
		scopeIndex,
		parsed,
		iniPaths,
		unitByNamespace,
		primaryDir,
	)

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
	return modelViewerScopedUnit{sections: sections, origins: origins, own: own, names: names}
}

type modelViewerForeignSection struct {
	member, index int
}

// importModelViewerForeignSections appends the command lists, custom shaders,
// and resources that sections reference in the namespace of another unit, as
// a draw run from a command list of a separate draw INI or a vertex buffer a
// shared shape key INI fills. A resource brings the sections of other units
// that assign it. Imported sections are renamed into
// the unit with their own references qualified by their namespace, so they
// keep reaching that namespace's sections and variables.
func importModelViewerForeignSections(
	merged []modINISection,
	origins []int,
	units []modelViewerINIUnit,
	current int,
	parsed []modelViewerParsedINI,
	iniPaths []string,
	unitByNamespace map[string]int,
	primaryDir string,
) ([]modINISection, []int) {
	declared := make([]map[string]modelViewerForeignSection, len(units))
	declaredIn := func(unitIndex int) map[string]modelViewerForeignSection {
		if declared[unitIndex] == nil {
			declared[unitIndex] = make(map[string]modelViewerForeignSection)
			for _, member := range units[unitIndex].members {
				for index, section := range parsed[member].Sections {
					key := modelViewerForeignSectionKey(section.Header, section.Name)
					if _, exists := declared[unitIndex][key]; !exists && modelViewerImportableHeader(section.Header) {
						declared[unitIndex][key] = modelViewerForeignSection{member: member, index: index}
					}
				}
			}
		}
		return declared[unitIndex]
	}

	// importAt appends a section of another unit once and returns its name in
	// this unit. A Present or TextureOverride that assigns an imported resource
	// comes in as a command list of only its resource and slot assignments, so
	// it neither draws nor runs anything here.
	imported := make(map[modelViewerForeignSection]string)
	importAt := func(unitIndex int, source modelViewerForeignSection) string {
		if localName, exists := imported[source]; exists {
			return localName
		}
		ini := parsed[source.member]
		section := ini.Sections[source.index]
		header, name := section.Header, section.Name
		bindingsOnly := !modelViewerImportableHeader(header)
		if bindingsOnly {
			header = "CommandList"
			name = section.Header + section.Name + "_" + modelViewerString(source.member) + "_" +
				modelViewerString(source.index)
		}
		localName := modelViewerForeignSectionPrefix(unitIndex, units[unitIndex].namespace) + name
		imported[source] = localName

		selfRE := modelViewerNamespaceSelfRE(ini.Namespace)
		memberDir := filepath.Dir(iniPaths[source.member])
		qualifier := `\` + ini.Namespace + `\`
		own := declaredIn(unitIndex)
		lines := make([]string, 0, len(section.Lines))
		for _, line := range section.Lines {
			if bindingsOnly && !modelViewerBindingLine(line) {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(strings.SplitN(line, "=", 2)[0]), "filename") {
				if selfRE != nil {
					line = selfRE.ReplaceAllString(line, "${1}")
				}
				line = modelViewerVariableUseRE.ReplaceAllString(
					line,
					`$$`+strings.ReplaceAll(qualifier, `$`, `$$`)+`${1}`,
				)

				// A name the namespace does not declare falls back to the global
				// namespace, which is this unit only when it has no namespace.
				line = modelViewerPlainSectionRE.ReplaceAllStringFunc(line, func(token string) string {
					match := modelViewerPlainSectionRE.FindStringSubmatch(token)
					if _, exists := own[modelViewerForeignSectionKey(match[2], match[3])]; !exists &&
						units[current].namespace == "" {
						return token
					}
					return match[1] + match[2] + qualifier + match[3]
				})
			}
			if memberDir != primaryDir && strings.EqualFold(section.Header, "CustomShader") {
				line = rebaseModelViewerShaderLine(line, memberDir, primaryDir)
			}
			lines = append(lines, line)
		}
		merged = append(merged, modINISection{Header: header, Name: localName, Lines: lines})
		origins = append(origins, source.member)
		return localName
	}
	importSection := func(unitIndex int, header, name string) (string, bool) {
		source, exists := declaredIn(unitIndex)[modelViewerForeignSectionKey(header, name)]
		if !exists {
			return "", false
		}
		if localName, exists := imported[source]; exists {
			return localName, true
		}
		localName := importAt(unitIndex, source)
		if !strings.EqualFold(header, "Resource") {
			return localName, true
		}

		// Any namespace can assign the resource by its qualified name; its own
		// namespace can also use the plain name.
		for otherIndex, other := range units {
			if otherIndex == current {
				continue
			}
			for _, member := range other.members {
				for index, section := range parsed[member].Sections {
					if !strings.EqualFold(section.Header, "Resource") && modelViewerSectionAssignsResource(
						section,
						otherIndex == unitIndex,
						units[unitIndex].namespace,
						name,
					) {
						importAt(otherIndex, modelViewerForeignSection{member: member, index: index})
					}
				}
			}
		}
		return localName, true
	}

	// importSection appends to merged, and the loop must also reach the appended
	// sections, so the length is read on every iteration.
	for sectionIndex := 0; sectionIndex < len(merged); sectionIndex++ { //nolint:intrange // merged grows in the loop
		source := merged[sectionIndex].Lines
		if !modelViewerForeignSectionRE.MatchString(strings.Join(source, "\n")) {
			continue
		}
		lines := make([]string, len(source))
		for lineIndex, line := range source {
			lines[lineIndex] = modelViewerForeignSectionRE.ReplaceAllStringFunc(line, func(token string) string {
				match := modelViewerForeignSectionRE.FindStringSubmatch(token)
				target, ok := unitByNamespace[strings.ToLower(match[2])]
				if !ok {
					return token
				}
				if target == current {
					return match[1] + match[3]
				}
				localName, ok := importSection(target, match[1], match[3])
				if !ok {
					return token
				}
				return match[1] + localName
			})
		}
		setModelViewerSectionLines(&merged[sectionIndex], lines)
	}
	return merged, origins
}

func modelViewerImportableHeader(header string) bool {
	return strings.EqualFold(header, "CommandList") || strings.EqualFold(header, "CustomShader") ||
		strings.EqualFold(header, "Resource")
}

func modelViewerForeignSectionKey(header, name string) string {
	return strings.ToLower(header) + "\x00" + strings.ToLower(name)
}

// modelViewerForeignSectionPrefix names an imported section after its unit,
// which sections of the importing unit cannot spell. The unit index keeps
// namespaces apart that differ only in non-word characters, such as A.B and A_B.
func modelViewerForeignSectionPrefix(unitIndex int, namespace string) string {
	return "__" + modelViewerString(unitIndex) + "_" + modelViewerNonWordRE.ReplaceAllString(namespace, "_") + "__"
}

// modelViewerSectionAssignsResource reports whether section assigns resource
// name of namespace; owner allows the plain name of a section in namespace.
func modelViewerSectionAssignsResource(section modINISection, owner bool, namespace, name string) bool {
	for _, line := range section.Lines {
		left, _, ok := strings.Cut(strings.TrimSpace(strings.SplitN(line, ";", 2)[0]), "=")
		if !ok {
			continue
		}
		target := modelViewerAssignmentTarget(left)
		if owner && strings.EqualFold(target, "Resource"+name) ||
			namespace != "" && strings.EqualFold(target, `Resource\`+namespace+`\`+name) {
			return true
		}
	}
	return false
}

// modelViewerBindingLine keeps the branches and the resource and shader slot
// assignments of a section imported for the resources it assigns.
func modelViewerBindingLine(line string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(strings.SplitN(line, ";", 2)[0]))
	if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "elif ") ||
		strings.HasPrefix(trimmed, "else if ") || trimmed == "else" || trimmed == "endif" {
		return true
	}
	left, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return false
	}
	target := modelViewerAssignmentTarget(left)
	return strings.HasPrefix(target, "resource") || modelViewerShaderSlotRE.MatchString(target)
}

// attributeModelViewerDrawSources points the draw source of each mesh at the
// member declaring the TextureOverride it draws from, which is the INI a tool
// hooking that draw edits. The INIs of imported sections follow the members,
// since the draw can sit in a command list of another namespace.
func attributeModelViewerDrawSources(meshes []modelViewerDirectMesh, unit modelViewerScopedUnit, iniPaths []string) {
	declaring := make(map[string]string)
	var members []string
	for index, section := range unit.sections {
		if index < unit.own && strings.EqualFold(section.Header, "TextureOverride") {
			declaring[modelViewerNormalizeKey(section.Name)] = iniPaths[unit.origins[index]]
		}
		if member := iniPaths[unit.origins[index]]; !slices.Contains(members, member) {
			members = append(members, member)
		}
	}
	for index := range meshes {
		source := meshes[index].drawSource
		if source == nil {
			continue
		}
		source.UnitINIPaths = members
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
