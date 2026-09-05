package mod

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	node MergePlanNode,
	packs []MergePackClassification,
	request MergeModsRequest,
	created *[]mergeRollback,
) (string, error) {
	container, err := createUniqueMergeFolder(request.GroupPath, mergeFolderName(node, request), created)
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
			if err := copyDirectory(pack.Path, destination); err != nil {
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
		key      string
		sections []classicSection
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
				commandGroups = append(commandGroups, commandGroup{key: key})
				overrides = append(overrides, buildClassicOverride(section, forwardKey != ""))
			}
			commandGroups[index].sections = append(commandGroups[index].sections, section)
			continue
		}
		if section.header == "CommandList" {
			key := section.name + "::0"
			index, exists := commandIndex[key]
			if !exists {
				index = len(commandGroups)
				commandIndex[key] = index
				commandGroups = append(commandGroups, commandGroup{key: key})
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
		if len(group.sections) > 0 && !strings.Contains(group.sections[0].name, "VertexLimitRaise") {
			commands = append(commands, buildClassicCommandList(group.sections))
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
	if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
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
	recognized := []string{"TextureOverride", "ShaderOverride", "Resource", "Constants", "Present", "CommandList", "CustomShader"}
	result := []classicSection{}
	current := -1
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			current = -1
			for _, header := range recognized {
				if strings.HasPrefix(name, header) && !strings.Contains(name, "CommandListReflectionTexture") &&
					!strings.Contains(name, "CommandListOutline") {
					result = append(result, classicSection{
						header: header, name: strings.TrimPrefix(name, header),
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

func buildClassicOverride(section classicSection, setActive bool) string {
	lines := []string{"[" + section.header + section.name + "]", "hash = " + classicEntryValue(section, "hash")}
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
	if !strings.Contains(section.name, "VertexLimitRaise") {
		lines = append(lines, "run = CommandList"+section.name)
	}
	if setActive && strings.Contains(section.name, "Position") {
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

func buildClassicCommandList(group []classicSection) string {
	lines := []string{"[CommandList" + group[0].name + "]"}
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

func isClassicResourceReference(key, value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "null" || lower == "auto" || strings.HasPrefix(lower, "commandlist") ||
		regexp.MustCompile(`^\d+(?:\.\d+)?$`).MatchString(trimmed) {
		return false
	}
	if regexp.MustCompile(`(?i)\bResource\w*`).MatchString(trimmed) {
		return true
	}
	return regexp.MustCompile(`(?i)^(?:vb|ib|ps-t|vs-t|ps-u|cs-u|cs-t)\d*`).MatchString(key) &&
		!regexp.MustCompile(`(?i)^[a-z_]+$`).MatchString(trimmed)
}

func appendClassicResourceSuffix(value string, index int) string {
	re := regexp.MustCompile(`(?i)\bResource[^\s,;]+`)
	location := re.FindStringIndex(value)
	if location == nil {
		return fmt.Sprintf("%s.%d", value, index)
	}
	target := value[location[0]:location[1]]
	if strings.HasSuffix(target, fmt.Sprintf(".%d", index)) {
		return value
	}
	return value[:location[0]] + fmt.Sprintf("%s.%d", target, index) + value[location[1]:]
}
