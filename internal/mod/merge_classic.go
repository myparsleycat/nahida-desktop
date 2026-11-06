package mod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nahida.live/desktop/internal/platform"
)

type classicEntry struct {
	control bool
	key     string
	value   string
	raw     string
}

type classicSection struct {
	header     string
	name       string
	entries    []classicEntry
	location   string
	groupIndex int
}

func (m *Mod) executeClassicMerge(
	ctx context.Context,
	node MergePlanNode,
	packs []MergePackClassification,
	request MergeModsRequest,
	created *[]mergeRollback,
) (string, error) {
	container, err := createUniqueMergeFolder(request.GroupPath, node.Name, created)
	if err != nil {
		return "", err
	}
	names, err := enabledMergeNames(packs, container)
	if err != nil {
		return "", err
	}
	mapped := make([]string, len(packs))
	for i, pack := range packs {
		destination := filepath.Join(container, names[i])
		if request.Placement == "new_folder" {
			if err := copyMergePack(ctx, pack.Path, destination); err != nil {
				return "", err
			}
			*created = append(*created, mergeRollback{kind: "remove", path: destination})
		} else {
			if err := os.Rename(pack.Path, destination); err != nil {
				return "", err
			}
			*created = append(*created, mergeRollback{kind: "move", from: destination, to: pack.Path})
		}
		mapped[i] = destination
	}
	sources := make([]classicSource, 0, len(packs))
	for i, pack := range packs {
		ini := ""
		if pack.PrimaryIniPath != nil {
			relative, relErr := filepath.Rel(pack.Path, *pack.PrimaryIniPath)
			if relErr == nil && fileExists(filepath.Join(mapped[i], relative)) {
				ini = filepath.Join(mapped[i], relative)
			}
		}
		if ini == "" {
			inis, collectErr := collectEnabledINIs(mapped[i])
			if collectErr != nil {
				return "", collectErr
			}
			if len(inis) > 0 {
				ini = inis[0]
			}
		}
		if ini != "" {
			sources = append(sources, classicSource{path: ini, index: i})
		}
	}
	if len(sources) < 2 {
		return "", errors.New("CLASSIC_MERGE_NEEDS_TWO_INIS")
	}
	if err := writeClassicMergedINI(container, sources, node.ForwardKey, node.BackKey, created); err != nil {
		return "", err
	}
	if request.Placement == "new_folder" {
		for _, pack := range packs {
			if err := disableOriginalForMerge(pack.Path, created); err != nil {
				return "", err
			}
		}
	}
	return container, nil
}

type classicSource struct {
	path  string
	index int
}

func writeClassicMergedINI(
	outputDir string,
	sources []classicSource,
	forwardKey, backKey string,
	created *[]mergeRollback,
) error {
	sections := []classicSection{}
	for _, source := range sources {
		raw, err := os.ReadFile(source.path)
		if err != nil {
			return err
		}
		sections = append(sections, parseClassicSections(string(raw), filepath.Dir(source.path), source.index)...)
	}
	type commandGroup struct {
		name     string
		sections []classicSection
	}
	// Section names must be unique in the merged file. The packs' own command lists keep their names,
	// because their overrides call them by name, so a generated list or override yields instead.
	used := map[string]bool{}
	for _, section := range sections {
		if section.header == "CommandList" && classicEntryValue(section, "hash") == "" {
			used[strings.ToLower(section.header+section.name)] = true
		}
	}
	uniqueName := func(header, name string) string {
		candidate := name
		for suffix := 2; used[strings.ToLower(header+candidate)]; suffix++ {
			candidate = fmt.Sprintf("%s.%d", name, suffix)
		}
		used[strings.ToLower(header+candidate)] = true
		return candidate
	}
	commandIndex := map[string]int{}
	commandGroups := []commandGroup{}
	overrides, resources := []string{}, []string{}
	for _, section := range sections {
		hash := classicEntryValue(section, "hash")
		if hash != "" {
			key := strings.ToLower(hash) + "::" + classicEntryValueDefault(section, "match_first_index", "-1")
			index, exists := commandIndex[key]
			if !exists {
				index = len(commandGroups)
				commandIndex[key] = index
				group := commandGroup{}
				if !isVertexLimitRaise(section.name) {
					group.name = uniqueName("CommandList", section.name)
				}
				commandGroups = append(commandGroups, group)
				overrides = append(overrides, buildClassicOverride(
					section, uniqueName(section.header, section.name), group.name, forwardKey != "",
				))
			}
			commandGroups[index].sections = append(commandGroups[index].sections, section)
			continue
		}
		if section.header == "CommandList" {
			key := "commandlist::" + strings.ToLower(section.name)
			index, exists := commandIndex[key]
			if !exists {
				index = len(commandGroups)
				commandIndex[key] = index
				commandGroups = append(commandGroups, commandGroup{name: section.name})
			}
			commandGroups[index].sections = append(commandGroups[index].sections, section)
			continue
		}
		if classicHasEntry(section, "filename") || classicHasEntry(section, "type") {
			resources = append(resources, buildClassicResource(section, outputDir))
		}
	}
	commands := []string{}
	for _, group := range commandGroups {
		if len(group.sections) > 0 && group.name != "" && !isVertexLimitRaise(group.sections[0].name) {
			commands = append(commands, buildClassicCommandList(group.name, group.sections))
		}
	}
	values := make([]string, len(sources))
	paths := make([]string, len(sources))
	for i, source := range sources {
		values[i] = fmt.Sprint(source.index)
		paths[i] = mergeRelativePath(outputDir, source.path)
	}
	backLine := ""
	if backKey != "" {
		backLine = "back = " + backKey + "\n"
	}
	present := "[Present]\npost $active = 0\n"
	for _, section := range sections {
		if strings.EqualFold(section.name, "creditinfo") {
			present += "run = CommandListCreditInfo\n"
			break
		}
	}
	content := fmt.Sprintf("; Merged Mod: %s\n\n; Constants ---------------------------\n\n"+
		"[Constants]\nglobal persist $swapvar = 0\nglobal $active\nglobal $creditinfo = 0\n\n"+
		"[KeySwap]\ncondition = $active == 1\nkey = %s\n%stype = cycle\n$swapvar = %s\n"+
		"$creditinfo = 0\n\n%s\n; Shader ------------------------------\n\n"+
		"; Overrides ---------------------------\n\n%s\n; CommandList -------------------------\n\n%s\n"+
		"; Resources ---------------------------\n\n%s\n\n; .ini generated by Nahida Desktop mod merger\n",
		strings.Join(paths, ", "), forwardKey, backLine, strings.Join(values, ","),
		present, strings.Join(overrides, "\n"), strings.Join(commands, "\n"), strings.Join(resources, "\n"))
	output := filepath.Join(outputDir, "merged.ini")
	if err := recordMergeWrite(output, created); err != nil {
		return err
	}
	if err := platform.WriteFileAtomic(output, []byte(content), 0o644); err != nil {
		return err
	}
	for _, source := range sources {
		if fileExists(source.path) {
			if err := disableINIForMerge(source.path, created); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseClassicSections(text, location string, groupIndex int) []classicSection {
	recognized := []string{
		"TextureOverride",
		"ShaderOverride",
		"Resource",
		"Constants",
		"Present",
		"CommandList",
		"CustomShader",
	}
	result := []classicSection{}
	current := -1
	text = strings.TrimPrefix(text, "\ufeff")
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			lower := strings.ToLower(name)
			current = -1
			for _, header := range recognized {
				// 3DMigoto matches section names without regard to case.
				if len(name) >= len(header) && strings.EqualFold(name[:len(header)], header) &&
					!strings.Contains(lower, "commandlistreflectiontexture") &&
					!strings.Contains(lower, "commandlistoutline") {
					result = append(result, classicSection{
						header: header, name: name[len(header):],
						location: location, groupIndex: groupIndex,
					})
					current = len(result) - 1
					break
				}
			}
			continue
		}
		if current < 0 {
			continue
		}
		if strings.HasPrefix(strings.TrimLeft(raw, " \t"), ";") {
			continue
		}
		if isControlFlowLine(trimmed) {
			result[current].entries = append(result[current].entries, classicEntry{control: true, raw: trimmed})
			continue
		}
		if at := strings.IndexByte(raw, '='); at >= 0 {
			key, value := strings.TrimSpace(raw[:at]), strings.TrimSpace(raw[at+1:])
			if !strings.Contains(key, "CharacterIB") && !strings.Contains(key, "ResourceRef") {
				result[current].entries = append(result[current].entries, classicEntry{key: key, value: value})
			}
		}
	}
	return result
}

func isVertexLimitRaise(name string) bool {
	return strings.Contains(strings.ToLower(name), "vertexlimitraise")
}

// buildClassicOverride writes the override of one hash. An empty commandName leaves out the run line.
func buildClassicOverride(section classicSection, name, commandName string, setActive bool) string {
	lines := []string{"[" + section.header + name + "]", "hash = " + classicEntryValue(section, "hash")}
	if value := classicEntryValue(section, "match_first_index"); value != "" {
		lines = append(lines, "match_first_index = "+value)
	}
	preserve := map[string]bool{"allow_duplicate_hash": true, "filter_index": true, "match_priority": true,
		"cull": true, "topology": true, "override_vertex_count": true, "override_byte_stride": true}
	for _, entry := range section.entries {
		if !entry.control && preserve[strings.ToLower(entry.key)] {
			lines = append(lines, entry.key+" = "+entry.value)
		}
	}
	if commandName != "" {
		lines = append(lines, "run = CommandList"+commandName)
	}
	if setActive && strings.Contains(strings.ToLower(section.name), "position") {
		lines = append(lines, "$active = 1")
	}
	return strings.Join(append(lines, ""), "\n")
}

func buildClassicResource(section classicSection, outputDir string) string {
	lines := []string{fmt.Sprintf("[%s%s.%d]", section.header, section.name, section.groupIndex)}
	for _, entry := range section.entries {
		if entry.control {
			continue
		}
		value := entry.value
		if strings.EqualFold(entry.key, "filename") {
			raw := strings.Trim(strings.TrimSpace(value), "\"'")
			if !filepath.IsAbs(raw) {
				raw = filepath.Join(section.location, raw)
			}
			value = mergeRelativePath(outputDir, raw)
		}
		lines = append(lines, entry.key+" = "+value)
	}
	return strings.Join(append(lines, ""), "\n")
}

func buildClassicCommandList(name string, group []classicSection) string {
	lines := []string{"[CommandList" + name + "]"}
	excluded := map[string]bool{"hash": true, "match_first_index": true, "allow_duplicate_hash": true,
		"override_vertex_count": true, "override_byte_stride": true}
	for index, section := range group {
		branch := "if"
		if index > 0 {
			branch = "else if"
		}
		lines = append(lines, fmt.Sprintf("%s $swapvar == %d", branch, section.groupIndex))
		indent := 1
		for _, entry := range section.entries {
			if entry.control {
				lower := strings.ToLower(entry.raw)
				if lower == "endif" {
					indent = max(1, indent-1)
				} else if strings.HasPrefix(lower, "else") || strings.HasPrefix(lower, "elif") {
					indent = max(1, indent-1)
					lines = append(lines, strings.Repeat("\t", indent)+entry.raw)
					indent++
					continue
				}
				lines = append(lines, strings.Repeat("\t", indent)+entry.raw)
				if strings.HasPrefix(lower, "if") {
					indent++
				}
				continue
			}
			if excluded[strings.ToLower(entry.key)] {
				continue
			}
			value := entry.value
			if isClassicResourceReference(entry.key, value) {
				value = appendClassicResourceSuffix(value, section.groupIndex)
			}
			lines = append(lines, strings.Repeat("\t", indent)+entry.key+" = "+value)
		}
	}
	return strings.Join(append(lines, "endif", ""), "\n")
}

func classicEntryValue(section classicSection, key string) string {
	for _, entry := range section.entries {
		if !entry.control && strings.EqualFold(entry.key, key) {
			return entry.value
		}
	}
	return ""
}

func classicEntryValueDefault(section classicSection, key, fallback string) string {
	if value := classicEntryValue(section, key); value != "" {
		return value
	}
	return fallback
}

func classicHasEntry(section classicSection, key string) bool {
	return classicEntryValue(section, key) != ""
}

func isControlFlowLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "if ") || strings.HasPrefix(lower, "if\t") ||
		strings.HasPrefix(lower, "else if") || strings.HasPrefix(lower, "elif") ||
		lower == "else" || lower == "endif"
}

var (
	classicNumberRE       = regexp.MustCompile(`^\d+(?:\.\d+)?$`)
	classicResourceWordRE = regexp.MustCompile(`(?i)\bResource\w*`)
	classicSlotKeyRE      = regexp.MustCompile(`(?i)^(?:vb|ib|ps-t|vs-t|ps-u|cs-u|cs-t)\d*`)
	classicKeywordRE      = regexp.MustCompile(`(?i)^[a-z_]+$`)
	classicResourceNameRE = regexp.MustCompile(`(?i)\bResource[^\s,;]+`)
)

func isClassicResourceReference(key, value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "null" || lower == "auto" || strings.HasPrefix(lower, "commandlist") ||
		classicNumberRE.MatchString(trimmed) {
		return false
	}
	if classicResourceWordRE.MatchString(trimmed) {
		return true
	}
	return classicSlotKeyRE.MatchString(key) && !classicKeywordRE.MatchString(trimmed)
}

func appendClassicResourceSuffix(value string, index int) string {
	location := classicResourceNameRE.FindStringIndex(value)
	if location == nil {
		return fmt.Sprintf("%s.%d", value, index)
	}
	target := value[location[0]:location[1]]
	if strings.HasSuffix(target, fmt.Sprintf(".%d", index)) {
		return value
	}
	return value[:location[0]] + fmt.Sprintf("%s.%d", target, index) + value[location[1]:]
}
