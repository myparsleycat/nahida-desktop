package tools

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
)

type modINISection struct {
	Header string
	Name   string
	Lines  []string
	Values map[string]string
}

type modBufferResource struct {
	Name     string
	Filename string
	Stride   int
	Format   string
}

var (
	modINIHeaderRE   = regexp.MustCompile(`^\[([^\]]+)\]$`)
	modINIMergedRE   = regexp.MustCompile(`(?i)^\s*;\s*(?:merged mods?|合并mod)\s*:\s*(.+)$`)
	modINISectionRE  = regexp.MustCompile(`(?i)^(TextureOverride|ShaderOverride|Resource|Constants|Present|CommandList|CustomShader)(.*)$`)
	modLOResourceRE  = regexp.MustCompile(`(?i)(?:_LOD$|_VB\d+_LOD)`)
	positionCSNameRE = regexp.MustCompile(`(?i)position(?:\.\d+)?cs$`)
	componentVB0RE   = regexp.MustCompile(`(?i)component\d+_vb0$`)
	componentVB2RE   = regexp.MustCompile(`(?i)component\d+_vb2$`)
	resourceRefRE    = regexp.MustCompile(`(?i)^(?:ref\s+)?Resource(.+)$`)
	nonAlphaNumRE    = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

func loadModINIBundle(input string) (string, []modINISection, error) {
	iniPath, sections, _, err := loadModINIBundleWithSources(input)
	return iniPath, sections, err
}

func loadModINIBundleWithSources(input string) (string, []modINISection, []string, error) {
	iniPath, err := findPrimaryModINI(input)
	if err != nil {
		return "", nil, nil, err
	}
	text, err := os.ReadFile(iniPath)
	if err != nil {
		return "", nil, nil, err
	}
	sections := parseModINI(string(text))
	sourcePaths := []string{iniPath}
	base := filepath.Dir(iniPath)
	refs := extractMergedINIRefs(string(text))
	for _, ref := range refs {
		refPath, resolveErr := resolveMergedINIRef(base, ref)
		if resolveErr != nil || samePathFold(refPath, iniPath) {
			continue
		}
		refText, readErr := os.ReadFile(refPath)
		if readErr != nil {
			return "", nil, nil, readErr
		}
		sections = append(sections, parseModINI(string(refText))...)
		sourcePaths = append(sourcePaths, refPath)
	}
	return iniPath, sections, sourcePaths, nil
}

func findPrimaryModINI(input string) (string, error) {
	resolved, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.Mode().IsRegular() {
		return resolved, nil
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a file or directory: %s", resolved)
	}
	type candidate struct {
		path  string
		score int
	}
	var candidates []candidate
	err = filepath.WalkDir(resolved, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ini") || strings.HasPrefix(strings.ToLower(entry.Name()), "disabled") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		candidates = append(candidates, candidate{path: path, score: scoreModINI(path, string(raw))})
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", contractError(fmt.Sprintf("No .ini found in %s", input))
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].path < candidates[j].path
	})
	return candidates[0].path, nil
}

func scoreModINI(path, text string) int {
	base := strings.ToLower(filepath.Base(path))
	score := 0
	overrideCount, resourceCount := 0, 0
	if base == "merged.ini" {
		score += 120
	}
	if strings.HasPrefix(base, "master") && strings.HasSuffix(base, ".ini") {
		score += 140
	}
	if len(extractMergedINIRefs(text)) > 0 {
		score += 80
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "namespace") && strings.Contains(line, "="):
			score += 60
		case strings.HasPrefix(lower, "global persist $"):
			score += 15
		case lower == "type = cycle" || lower == "type=cycle":
			score += 10
		case strings.HasPrefix(lower, "[textureoverride"):
			overrideCount++
		case strings.HasPrefix(lower, "[resource"):
			resourceCount++
		}
	}
	score += min(overrideCount, 50) + min(resourceCount, 50)
	if strings.Contains(strings.ToLower(text), "[keyhelp]") {
		score -= 25
	}
	if strings.HasPrefix(base, "disabled") && len(extractMergedINIRefs(text)) == 0 {
		score -= 10
	}
	return score
}

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

func extractMergedINIRefs(text string) []string {
	var rawMatches []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[;") {
			break
		}
		if match := modINIMergedRE.FindStringSubmatch(raw); len(match) == 2 {
			rawMatches = append(rawMatches, strings.TrimSpace(match[1]))
		}
	}
	var out []string
	for _, raw := range rawMatches {
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			var list []string
			if json.Unmarshal([]byte(raw), &list) == nil {
				for _, entry := range list {
					if entry = strings.TrimSpace(entry); entry != "" {
						out = append(out, entry)
					}
				}
				continue
			}
		}
		if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
			var value string
			if json.Unmarshal([]byte(raw), &value) == nil && strings.TrimSpace(value) != "" {
				out = append(out, strings.TrimSpace(value))
				continue
			}
		}
		parts := strings.Split(raw, ",")
		if len(parts) > 1 {
			allINI := true
			for i := range parts {
				parts[i] = strings.Trim(strings.TrimSpace(parts[i]), `"'`)
				allINI = allINI && strings.EqualFold(filepath.Ext(parts[i]), ".ini")
			}
			if allINI {
				out = append(out, parts...)
				continue
			}
		}
		value := strings.Trim(strings.TrimSpace(raw), `"'`)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveMergedINIRef(baseDir, entry string) (string, error) {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	candidate := filepath.FromSlash(strings.TrimSpace(entry))
	var resolved string
	if filepath.IsAbs(candidate) {
		resolved, err = filepath.Abs(candidate)
	} else {
		resolved, err = filepath.Abs(filepath.Join(base, candidate))
	}
	if err != nil || !sameOrChildPath(base, resolved) || samePathFold(base, resolved) {
		return "", infra.WithCause(errors.New("merged INI path is outside mod root"), err)
	}
	// EvalSymlinks can fail on Windows temp junctions even for in-tree files.
	// Keep the logical path when evaluation fails; successful evaluation still
	// rejects symlink escapes like Electron loadIniBundle.
	if realBase, evalErr := filepath.EvalSymlinks(base); evalErr == nil {
		if realPath, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
			if !sameOrChildPath(realBase, realPath) || samePathFold(realBase, realPath) {
				return "", errors.New("merged INI path is outside mod root")
			}
			resolved = realPath
		}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", infra.WithCause(errors.New("merged INI is not a regular file"), err)
	}
	return filepath.Clean(resolved), nil
}

func collectModResources(sections []modINISection) []modBufferResource {
	resources := make([]modBufferResource, 0)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Resource") {
			continue
		}
		stride, _ := strconv.Atoi(sectionValue(section.Lines, "stride"))
		resources = append(resources, modBufferResource{
			Name: section.Name, Filename: sectionValue(section.Lines, "filename"),
			Stride: stride, Format: sectionValue(section.Lines, "format"),
		})
	}
	return resources
}

func collectPositionResources(resources []modBufferResource) []modBufferResource {
	var out []modBufferResource
	for _, resource := range resources {
		if resource.Filename == "" || resource.Stride == 0 || modLOResourceRE.MatchString(resource.Name) || positionCSNameRE.MatchString(resource.Name) {
			continue
		}
		if (strings.Contains(strings.ToLower(resource.Name), "position") || componentVB0RE.MatchString(resource.Name)) && resource.Stride >= 12 {
			out = append(out, resource)
		}
	}
	return out
}

func collectIndexResources(resources []modBufferResource) []modBufferResource {
	var out []modBufferResource
	for _, resource := range resources {
		lowerName, upperFormat := strings.ToLower(resource.Name), strings.ToUpper(resource.Format)
		if resource.Filename == "" || modLOResourceRE.MatchString(resource.Name) {
			continue
		}
		if strings.Contains(lowerName, "index") || ((strings.Contains(upperFormat, "R16_UINT") || strings.Contains(upperFormat, "R32_UINT")) && !containsAny(lowerName, "position", "blend", "vector", "texcoord", "color")) {
			out = append(out, resource)
		}
	}
	return out
}

func collectNamedResources(resources []modBufferResource, kind string, excludeLOD bool) []modBufferResource {
	var out []modBufferResource
	for _, resource := range resources {
		if resource.Filename == "" || excludeLOD && modLOResourceRE.MatchString(resource.Name) {
			continue
		}
		lower := strings.ToLower(resource.Name)
		if strings.Contains(lower, kind) || kind == "blend" && componentVB2RE.MatchString(resource.Name) {
			out = append(out, resource)
		}
	}
	return out
}

func matchIndexResources(positions, indices []modBufferResource, sections []modINISection) map[string][]modBufferResource {
	matches := make(map[string][]modBufferResource)
	byName := make(map[string]modBufferResource)
	for _, resource := range append(append([]modBufferResource(nil), positions...), indices...) {
		byName[strings.ToLower(resource.Name)] = resource
	}
	commandLists := make(map[string]modINISection)
	for _, section := range sections {
		if strings.EqualFold(section.Header, "CommandList") {
			commandLists[strings.ToLower("commandlist"+section.Name)] = section
		}
	}
	for _, section := range sections {
		lines := expandCommandListLines(section.Lines, commandLists, make(map[string]bool))
		position, positionOK := resourceForReference(sectionValue(lines, "vb0"), byName)
		index, indexOK := resourceForReference(sectionValue(lines, "ib"), byName)
		if positionOK && indexOK {
			addIndexMatch(matches, position, index)
		}
	}
	for _, index := range indices {
		bestScore := 0
		var best []modBufferResource
		for _, position := range positions {
			score := indexResourceMatchScore(position, index)
			if score > bestScore {
				bestScore, best = score, []modBufferResource{position}
			} else if score > 0 && score == bestScore {
				best = append(best, position)
			}
		}
		if len(best) == 1 {
			addIndexMatch(matches, best[0], index)
		}
	}
	if len(indices) == 1 {
		for _, position := range positions {
			key := strings.ToLower(position.Name)
			if _, ok := matches[key]; !ok {
				addIndexMatch(matches, position, indices[0])
			}
		}
	}
	for key, matched := range matches {
		sort.SliceStable(matched, func(i, j int) bool { return resourceIndex(indices, matched[i]) < resourceIndex(indices, matched[j]) })
		matches[key] = matched
	}
	return matches
}

func expandCommandListLines(lines []string, lists map[string]modINISection, stack map[string]bool) []string {
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		index := strings.Index(trimmed, "=")
		if index < 0 || !strings.EqualFold(strings.TrimSpace(trimmed[:index]), "run") {
			out = append(out, line)
			continue
		}
		ref := strings.Fields(strings.TrimSpace(strings.SplitN(trimmed[index+1:], ";", 2)[0]))
		if len(ref) == 0 || !strings.HasPrefix(strings.ToLower(ref[0]), "commandlist") {
			out = append(out, line)
			continue
		}
		key := strings.ToLower(ref[0])
		section, ok := lists[key]
		if !ok || stack[key] {
			out = append(out, line)
			continue
		}
		nextStack := make(map[string]bool, len(stack)+1)
		for item := range stack {
			nextStack[item] = true
		}
		nextStack[key] = true
		out = append(out, expandCommandListLines(section.Lines, lists, nextStack)...)
	}
	return out
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

func resourceForReference(value string, resources map[string]modBufferResource) (modBufferResource, bool) {
	match := resourceRefRE.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return modBufferResource{}, false
	}
	resource, ok := resources[strings.ToLower(match[1])]
	return resource, ok
}

func addIndexMatch(matches map[string][]modBufferResource, position, index modBufferResource) {
	key := strings.ToLower(position.Name)
	for _, existing := range matches[key] {
		if strings.EqualFold(existing.Name, index.Name) {
			return
		}
	}
	matches[key] = append(matches[key], index)
}

func indexResourceMatchScore(position, index modBufferResource) int {
	positionBase, positionVariant := logicalResourceName(position.Name, "position")
	indexBase, indexVariant := logicalResourceName(index.Name, "index")
	if positionBase == "" || indexBase == "" || positionVariant != "" && indexVariant != "" && positionVariant != indexVariant || !strings.HasPrefix(indexBase, positionBase) {
		return 0
	}
	variantScore := 0
	if positionVariant == indexVariant {
		variantScore = 100
	}
	exactScore := 1_000
	if indexBase == positionBase {
		exactScore = 10_000
	}
	return exactScore + len(positionBase) + variantScore
}

func logicalResourceName(name, kind string) (string, string) {
	withoutCS := regexp.MustCompile(`(?i)cs$`).ReplaceAllString(name, "")
	pattern := `(?i)^(.*)(?:position(?:buffer)?|_vb0)(?:[._-](.+))?$`
	if kind == "index" {
		pattern = `(?i)^(.*)(?:index(?:buffer)?|_ib|ib)(?:[._-](.+))?$`
	}
	match := regexp.MustCompile(pattern).FindStringSubmatch(withoutCS)
	base, variant := withoutCS, ""
	if len(match) >= 2 {
		base = match[1]
	}
	if len(match) == 3 {
		variant = strings.ToLower(nonAlphaNumRE.ReplaceAllString(match[2], ""))
	}
	if kind == "index" {
		base = regexp.MustCompile(`(?i)^_?lod\d+(?:[._-]?)`).ReplaceAllString(base, "")
	}
	return strings.ToLower(nonAlphaNumRE.ReplaceAllString(base, "")), variant
}

func matchCompanionResource(position modBufferResource, candidates []modBufferResource) (modBufferResource, bool) {
	if len(candidates) == 0 {
		return modBufferResource{}, false
	}
	key := companionResourceKey(position.Name)
	for _, candidate := range candidates {
		if companionResourceKey(candidate.Name) == key {
			return candidate, true
		}
	}
	group := resourceGroupKey(position)
	if group != "" {
		for _, candidate := range candidates {
			if resourceGroupKey(candidate) == group {
				return candidate, true
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return modBufferResource{}, false
}

func companionResourceKey(name string) string {
	key := regexp.MustCompile(`(?i)_VB\d+(?:_LOD)?$`).ReplaceAllString(name, "")
	key = regexp.MustCompile(`(?i)_IB(?:_LOD)?$`).ReplaceAllString(key, "")
	key = regexp.MustCompile(`(?i)(Position|Vector|Index|Blend|TexCoord|Color)Buffer`).ReplaceAllString(key, "")
	key = regexp.MustCompile(`(?i)(Position|Vector|Index|Blend|Texcoord)`).ReplaceAllString(key, "")
	if !regexp.MustCompile(`(?i)^_?Component\d+$`).MatchString(key) {
		key = regexp.MustCompile(`(?i)[_-]Component\d+$`).ReplaceAllString(key, "")
	}
	return strings.ToLower(regexp.MustCompile(`[_-]+`).ReplaceAllString(key, ""))
}

func resourceGroupKey(resource modBufferResource) string {
	if resource.Filename != "" {
		stem := strings.TrimSuffix(filepath.Base(filepath.FromSlash(resource.Filename)), filepath.Ext(resource.Filename))
		if key := companionResourceKey(stem); key != "" {
			return key
		}
	}
	return companionResourceKey(resource.Name)
}

func readIndexBuffer(path, format string) ([]uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	use16 := strings.Contains(strings.ToUpper(format), "R16") || len(data)%4 != 0 && len(data)%2 == 0
	if use16 {
		out := make([]uint32, len(data)/2)
		for i := range out {
			out[i] = uint32(binary.LittleEndian.Uint16(data[i*2:]))
		}
		return out, nil
	}
	out := make([]uint32, len(data)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(data[i*4:])
	}
	return out, nil
}

func resourceIndex(resources []modBufferResource, target modBufferResource) int {
	for index, resource := range resources {
		if strings.EqualFold(resource.Name, target.Name) {
			return index
		}
	}
	return len(resources)
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func samePathFold(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}
