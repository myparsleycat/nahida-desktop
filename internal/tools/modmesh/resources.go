package modmesh

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func CollectResources(sections []Section) []BufferResource {
	resources := make([]BufferResource, 0)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Resource") {
			continue
		}
		stride, _ := strconv.Atoi(SectionValue(section.Lines, "stride"))
		resources = append(resources, BufferResource{
			Name: section.Name, Filename: SectionValue(section.Lines, "filename"),
			Stride: stride, Format: SectionValue(section.Lines, "format"),
		})
	}
	return resources
}

func CollectPositionResources(resources []BufferResource) []BufferResource {
	var out []BufferResource
	for _, resource := range resources {
		if resource.Filename == "" || resource.Stride == 0 || modLOResourceRE.MatchString(resource.Name) ||
			positionCSNameRE.MatchString(resource.Name) {
			continue
		}
		positionLike := strings.Contains(strings.ToLower(resource.Name), "position")
		if (positionLike || componentVB0RE.MatchString(resource.Name)) && resource.Stride >= 12 {
			out = append(out, resource)
		}
	}
	return out
}

func CollectIndexResources(resources []BufferResource) []BufferResource {
	var out []BufferResource
	for _, resource := range resources {
		lowerName, upperFormat := strings.ToLower(resource.Name), strings.ToUpper(resource.Format)
		if resource.Filename == "" || modLOResourceRE.MatchString(resource.Name) {
			continue
		}
		uintFormat := strings.Contains(upperFormat, "R16_UINT") || strings.Contains(upperFormat, "R32_UINT")
		named := containsAny(lowerName, "position", "blend", "vector", "texcoord", "color")
		if strings.Contains(lowerName, "index") || uintFormat && !named {
			out = append(out, resource)
		}
	}
	return out
}

func CollectNamedResources(resources []BufferResource, kind string, excludeLOD bool) []BufferResource {
	var out []BufferResource
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

func MatchIndexResources(
	positions, indices []BufferResource,
	sections []Section,
) map[string][]BufferResource {
	matches := make(map[string][]BufferResource)
	byName := make(map[string]BufferResource)
	for _, resource := range append(append([]BufferResource(nil), positions...), indices...) {
		byName[strings.ToLower(resource.Name)] = resource
	}
	commandLists := make(map[string]Section)
	for _, section := range sections {
		if strings.EqualFold(section.Header, "CommandList") {
			commandLists[strings.ToLower("commandlist"+section.Name)] = section
		}
	}
	for _, section := range sections {
		lines := ExpandCommandListLines(section.Lines, commandLists, make(map[string]bool))
		position, positionOK := resourceForReference(SectionValue(lines, "vb0"), byName)
		index, indexOK := resourceForReference(SectionValue(lines, "ib"), byName)
		if positionOK && indexOK {
			addIndexMatch(matches, position, index)
		}
	}
	for _, index := range indices {
		bestScore := 0
		var best []BufferResource
		for _, position := range positions {
			score := indexResourceMatchScore(position, index)
			if score > bestScore {
				bestScore, best = score, []BufferResource{position}
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
		sort.SliceStable(
			matched,
			func(i, j int) bool { return resourceIndex(indices, matched[i]) < resourceIndex(indices, matched[j]) },
		)
		matches[key] = matched
	}
	return matches
}

func ExpandCommandListLines(lines []string, lists map[string]Section, stack map[string]bool) []string {
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
		out = append(out, ExpandCommandListLines(section.Lines, lists, nextStack)...)
	}
	return out
}

func SectionValue(lines []string, key string) string {
	for index := len(lines) - 1; index >= 0; index-- {
		separator := strings.Index(lines[index], "=")
		if separator >= 0 && strings.EqualFold(strings.TrimSpace(lines[index][:separator]), key) {
			return strings.TrimSpace(lines[index][separator+1:])
		}
	}
	return ""
}

func resourceForReference(value string, resources map[string]BufferResource) (BufferResource, bool) {
	match := resourceRefRE.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return BufferResource{}, false
	}
	resource, ok := resources[strings.ToLower(match[1])]
	return resource, ok
}

func addIndexMatch(matches map[string][]BufferResource, position, index BufferResource) {
	key := strings.ToLower(position.Name)
	for _, existing := range matches[key] {
		if strings.EqualFold(existing.Name, index.Name) {
			return
		}
	}
	matches[key] = append(matches[key], index)
}

func indexResourceMatchScore(position, index BufferResource) int {
	positionBase, positionVariant := logicalResourceName(position.Name, "position")
	indexBase, indexVariant := logicalResourceName(index.Name, "index")
	if positionBase == "" || indexBase == "" ||
		positionVariant != "" && indexVariant != "" && positionVariant != indexVariant ||
		!strings.HasPrefix(indexBase, positionBase) {
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

func MatchCompanionResource(position BufferResource, candidates []BufferResource) (BufferResource, bool) {
	if len(candidates) == 0 {
		return BufferResource{}, false
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
	return BufferResource{}, false
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

func resourceGroupKey(resource BufferResource) string {
	if resource.Filename != "" {
		stem := strings.TrimSuffix(
			filepath.Base(filepath.FromSlash(resource.Filename)),
			filepath.Ext(resource.Filename),
		)
		if key := companionResourceKey(stem); key != "" {
			return key
		}
	}
	return companionResourceKey(resource.Name)
}

func ReadIndexBuffer(path, format string) ([]uint32, error) {
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

func resourceIndex(resources []BufferResource, target BufferResource) int {
	for index, resource := range resources {
		if strings.EqualFold(resource.Name, target.Name) {
			return index
		}
	}
	return len(resources)
}
