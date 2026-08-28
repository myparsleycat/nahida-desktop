package tools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type touchMeshBuffers struct {
	Positions   []float32
	Normals     []float32
	Indices     []uint32
	PositionRaw []byte
	BlendBytes  []byte
	BlendStride *int
	Bones       []BlendBoneInfo
}

type touchIndexInfo struct {
	Resource modBufferResource
	Offset   int
	Count    int
	Path     string
}

type touchConditionalLine struct {
	Line      string
	Condition *string
}

func analyzeTouchMod(modPath string, warn func(string)) (TouchModAnalysis, error) {
	resolved, err := filepath.Abs(modPath)
	if err != nil {
		return TouchModAnalysis{}, err
	}
	if _, err = os.Stat(resolved); err != nil {
		return TouchModAnalysis{}, contractError(fmt.Sprintf("Path does not exist: %s", resolved))
	}
	// Windows CI and some installations expose temporary or mod directories
	// through junctions. Keep the analysis root and the resource paths in the
	// same canonical namespace so relative fingerprints survive a folder rename.
	if real, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
		resolved = real
	}
	iniPath, sections, sourcePaths, err := loadModINIBundleWithSources(resolved)
	if err != nil {
		return TouchModAnalysis{}, err
	}
	modRoot := filepath.Dir(iniPath)
	if err := assertTouchProfileBundleAllowed(modRoot, sourcePaths); err != nil {
		return TouchModAnalysis{}, err
	}
	resources := collectModResources(sections)
	positions := collectPositionResources(resources)
	indices := collectIndexResources(resources)
	blends := collectNamedResources(resources, "blend", true)
	indexByPosition := matchIndexResources(positions, indices, sections)
	if len(positions) == 0 {
		return TouchModAnalysis{}, contractError("No position buffer resources found in mod.ini")
	}
	components := []TouchComponentAnalysis{}
	for _, position := range positions {
		if position.Filename == "" || position.Stride == 0 {
			continue
		}
		positionPath, resolveErr := resolveBodyShapeResource(modRoot, position.Filename)
		if resolveErr != nil {
			warn("Missing position buffer: " + filepath.Join(modRoot, filepath.FromSlash(position.Filename)))
			continue
		}
		positionBytes, readErr := os.ReadFile(positionPath)
		if readErr != nil {
			warn("Missing position buffer: " + positionPath)
			continue
		}
		vertexCount, validationErr := validatePositionBuffer(len(positionBytes), position.Stride, nil)
		if validationErr != nil {
			warn(fmt.Sprintf("Skipping position buffer %s: %s", positionPath, validationErr))
			continue
		}

		matches := indexByPosition[strings.ToLower(position.Name)]
		indexInfos := make([]touchIndexInfo, 0, len(matches))
		combined := []uint32{}
		indexPaths, indexRelativePaths := []string{}, []string{}
		indexFormats := []*string{}
		for _, index := range matches {
			indexPath, pathErr := resolveBodyShapeResource(modRoot, index.Filename)
			if pathErr != nil {
				warn("Missing index buffer: " + filepath.Join(modRoot, filepath.FromSlash(index.Filename)))
				continue
			}
			values, valueErr := readIndexBuffer(indexPath, index.Format)
			if valueErr != nil {
				warn("Missing index buffer: " + indexPath)
				continue
			}
			valid := true
			for _, value := range values {
				if value >= uint32(vertexCount) {
					valid = false
					break
				}
			}
			if !valid {
				warn(fmt.Sprintf("Skipping index buffer %s: index exceeds %d", indexPath, vertexCount-1))
				continue
			}
			indexInfos = append(indexInfos, touchIndexInfo{Resource: index, Offset: len(combined), Count: len(values), Path: indexPath})
			combined = append(combined, values...)
			indexPaths, indexRelativePaths = append(indexPaths, indexPath), append(indexRelativePaths, index.Filename)
			format := index.Format
			if format == "" {
				indexFormats = append(indexFormats, nil)
			} else {
				indexFormats = append(indexFormats, &format)
			}
		}
		if len(indexInfos) == 0 {
			warn("No usable index buffer for " + position.Name)
			continue
		}

		drawRanges, blendSection, ibSection, ibHash, variantCondition := findTouchDrawContext(sections, position, indexInfos, len(combined))
		drawRanges = uniqueTouchDrawRanges(drawRanges)
		kind := classifyTouchComponent(position.Name, indexInfos[0].Resource.Name, derefString(blendSection), derefString(ibSection))
		grade, reasons := gradeTouchComponent(position.Stride, positionBytes, vertexCount, combined, drawRanges, kind)
		meshPositions, _ := extractBodyPositions(positionBytes, position.Stride)
		blendRelative, blendPath, blendStride := (*string)(nil), (*string)(nil), (*int)(nil)
		bones := []BlendBoneInfo{}
		if blend, ok := matchCompanionResource(position, blends); ok {
			resolvedBlend, pathErr := resolveBodyShapeResource(modRoot, blend.Filename)
			if pathErr == nil {
				raw, readErr := os.ReadFile(resolvedBlend)
				stride := blend.Stride
				if stride == 0 {
					stride = defaultBlendStride
				}
				if readErr == nil {
					if validationErr := validateBlendBuffer(len(raw), vertexCount, stride); validationErr != nil {
						warn(fmt.Sprintf("Skipping blend buffer %s: %s", resolvedBlend, validationErr))
					} else {
						rel := blend.Filename
						blendRelative, blendPath, blendStride = &rel, &resolvedBlend, &stride
						bones = listBlendBones(raw, vertexCount, stride)
					}
				}
			}
		}
		primary := indexInfos[0]
		primaryName, primaryRel, primaryPath, primaryFormat := primary.Resource.Name, indexRelativePaths[0], primary.Path, primary.Resource.Format
		component := TouchComponentAnalysis{
			ID: sanitizeTouchID(position.Name), Name: position.Name, Kind: kind,
			InteractiveCandidate: touchInteractiveCandidate(kind, vertexCount, len(combined), position.Name),
			SupportGrade:         grade, SupportReasons: reasons, PositionResourceName: position.Name,
			PositionRelativePath: position.Filename, PositionPath: positionPath, PositionStride: position.Stride,
			VertexCount: vertexCount, IndexResourceName: &primaryName, IndexRelativePath: &primaryRel,
			IndexPath: &primaryPath, IndexRelativePaths: indexRelativePaths, IndexPaths: indexPaths,
			IndexFormats: indexFormats, IndexCount: len(combined), BlendSectionName: blendSection,
			IBSectionName: ibSection, IBHash: ibHash, VariantCondition: variantCondition,
			DrawRanges: drawRanges, ObjectMaps: buildTouchObjectMaps(drawRanges, kind, len(components)+1, meshPositions, combined),
			BlendRelativePath: blendRelative, BlendPath: blendPath, BlendStride: blendStride, Bones: bones,
		}
		if primaryFormat != "" {
			component.IndexFormat = &primaryFormat
		}
		if match := regexp.MustCompile(`\.(\d+)$`).FindStringSubmatch(position.Name); len(match) == 2 {
			component.VariantKey = &match[1]
		}
		components = append(components, component)
	}
	if len(components) == 0 {
		return TouchModAnalysis{}, contractError("No readable touch-compatible mesh components found")
	}
	grade := "A"
	reasonSet := map[string]bool{}
	reasons := []string{}
	meshPaths := []string{}
	for _, component := range components {
		if component.SupportGrade == "C" {
			grade = "C"
		} else if component.SupportGrade == "B" && grade == "A" {
			grade = "B"
		}
		for _, reason := range component.SupportReasons {
			if !reasonSet[reason] {
				reasonSet[reason] = true
				reasons = append(reasons, reason)
			}
		}
		meshPaths = append(meshPaths, component.PositionPath)
		meshPaths = append(meshPaths, component.IndexPaths...)
		if component.BlendPath != nil {
			meshPaths = append(meshPaths, *component.BlendPath)
		}
	}
	meshHash, err := hashTouchFiles(meshPaths, modRoot)
	if err != nil {
		return TouchModAnalysis{}, err
	}
	iniHash, err := hashTouchFiles(sourcePaths, modRoot)
	if err != nil {
		return TouchModAnalysis{}, err
	}
	modRel, _ := filepath.Rel(resolved, modRoot)
	if modRel == "" {
		modRel = "."
	}
	iniRel, _ := filepath.Rel(modRoot, iniPath)
	if iniRel == "" {
		iniRel = filepath.Base(iniPath)
	}
	sourceRel := make([]string, len(sourcePaths))
	for i, path := range sourcePaths {
		sourceRel[i], _ = filepath.Rel(modRoot, path)
	}
	return TouchModAnalysis{ModRoot: modRoot, SourceRoot: resolved, ModRootRelativeToSource: modRel, INIPath: iniPath, INIRelativePath: iniRel, SourceFilesRelativePaths: sourceRel, SupportGrade: grade, SupportReasons: reasons, Components: components, MeshHash: meshHash, INIHash: iniHash}, nil
}

func loadTouchMeshBuffers(component TouchComponentAnalysis) (touchMeshBuffers, error) {
	positionRaw, err := os.ReadFile(component.PositionPath)
	if err != nil {
		return touchMeshBuffers{}, err
	}
	positions, err := extractBodyPositions(positionRaw, component.PositionStride)
	if err != nil {
		return touchMeshBuffers{}, err
	}
	normals := make([]float32, component.VertexCount*3)
	if component.PositionStride >= 24 {
		for vertex := range component.VertexCount {
			base := vertex*component.PositionStride + 12
			for axis := range 3 {
				normals[vertex*3+axis] = math.Float32frombits(uint32(positionRaw[base+axis*4]) | uint32(positionRaw[base+axis*4+1])<<8 | uint32(positionRaw[base+axis*4+2])<<16 | uint32(positionRaw[base+axis*4+3])<<24)
			}
		}
	}
	indices := []uint32{}
	for index, path := range component.IndexPaths {
		format := ""
		if index < len(component.IndexFormats) && component.IndexFormats[index] != nil {
			format = *component.IndexFormats[index]
		} else if component.IndexFormat != nil {
			format = *component.IndexFormat
		}
		values, readErr := readIndexBuffer(path, format)
		if readErr != nil {
			return touchMeshBuffers{}, readErr
		}
		indices = append(indices, values...)
	}
	if len(component.IndexPaths) == 0 && component.IndexPath != nil {
		format := ""
		if component.IndexFormat != nil {
			format = *component.IndexFormat
		}
		indices, err = readIndexBuffer(*component.IndexPath, format)
		if err != nil {
			return touchMeshBuffers{}, err
		}
	}
	mesh := touchMeshBuffers{Positions: positions, Normals: normals, Indices: indices, PositionRaw: positionRaw, BlendStride: component.BlendStride, Bones: append([]BlendBoneInfo(nil), component.Bones...)}
	if component.BlendPath != nil && component.BlendStride != nil {
		mesh.BlendBytes, err = os.ReadFile(*component.BlendPath)
		if err != nil {
			return touchMeshBuffers{}, err
		}
		if len(mesh.Bones) == 0 {
			mesh.Bones = listBlendBones(mesh.BlendBytes, component.VertexCount, *component.BlendStride)
		}
	}
	return mesh, nil
}

func gradeTouchComponent(stride int, bytes []byte, vertexCount int, indices []uint32, ranges []TouchDrawRange, kind string) (string, []string) {
	if len(indices) == 0 || len(indices)%3 != 0 {
		return "C", []string{"Mesh is not a triangle list"}
	}
	for _, value := range indices {
		if value >= uint32(vertexCount) {
			return "C", []string{"Index buffer exceeds position vertex count"}
		}
	}
	if len(ranges) == 0 {
		return "C", []string{"No drawindexed ranges found in INI"}
	}
	if stride == touchPositionStride {
		if !hasTouchPNT(bytes, stride) {
			return "B", []string{"Stride 40 but normal/tangent layout looks incomplete"}
		}
		if kind == "unknown" {
			return "A", []string{"Component kind is ambiguous"}
		}
		return "A", []string{"Position stride 40 with PN-T layout"}
	}
	if stride >= 12 {
		return "B", []string{fmt.Sprintf("Position stride %d requires touch VB rebuild", stride)}
	}
	return "C", []string{fmt.Sprintf("Unsupported position stride %d", stride)}
}

func hasTouchPNT(data []byte, stride int) bool {
	if stride < 40 || len(data) < stride*3 {
		return false
	}
	limit := min(8, len(data)/stride)
	for i := range limit {
		base := i*stride + 12
		x := math.Float32frombits(binary.LittleEndian.Uint32(data[base:]))
		y := math.Float32frombits(binary.LittleEndian.Uint32(data[base+4:]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(data[base+8:]))
		n := math.Sqrt(float64(x*x + y*y + z*z))
		if math.IsNaN(n) || math.IsInf(n, 0) || n < .1 || n > 2.5 {
			return false
		}
	}
	return true
}

func classifyTouchComponent(names ...string) string {
	text := strings.ToLower(strings.Join(names, " "))
	for _, entry := range []struct{ kind, pattern string }{
		{"legs", `(leg|thigh|butt|hip|lower[_-]?body|xiaban|tuibu)`},
		{"hair", `(hair|tail|toufa)`},
		{"body", `(body|torso|chest|breast|upper[_-]?body|shangban|shenti)`},
		{"accessory", `(back|cloth|dress|coat|acc|weapon|face|head|zhuangshi|pifuzhuangshi)`},
	} {
		if regexp.MustCompile(entry.pattern).MatchString(text) {
			return entry.kind
		}
	}
	return "unknown"
}

func touchInteractiveCandidate(kind string, vertices, indices int, name string) bool {
	return (kind == "body" || kind == "legs") && vertices >= 1500 && indices >= 3000 && !regexp.MustCompile(`(?i)(body|leg)\d+`).MatchString(name)
}

func findTouchDrawContext(sections []modINISection, position modBufferResource, indices []touchIndexInfo, indexCount int) ([]TouchDrawRange, *string, *string, *string, *string) {
	resources := collectModResources(sections)
	byName := map[string]modBufferResource{}
	for _, resource := range resources {
		byName[strings.ToLower(resource.Name)] = resource
	}
	commandLists := map[string]modINISection{}
	for _, section := range sections {
		if strings.EqualFold(section.Header, "CommandList") {
			commandLists[strings.ToLower("commandlist"+section.Name)] = section
		}
	}
	indexMap := map[string]touchIndexInfo{}
	for _, info := range indices {
		indexMap[strings.ToLower(info.Resource.Name)] = info
	}
	var ranges []TouchDrawRange
	var blendSection, ibSection, ibHash, variant *string
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		lines := expandCommandListLines(section.Lines, commandLists, map[string]bool{})
		conditionLines := touchLinesWithConditions(lines)
		positionAssign := touchConditionalAssignments(conditionLines, "vb0", byName)
		for _, assignment := range positionAssign {
			if strings.EqualFold(assignment.resource.Name, position.Name) && blendSection == nil {
				value := section.Name
				blendSection = &value
			}
		}
		indexAssign := touchConditionalAssignments(conditionLines, "ib", byName)
		for _, assignment := range indexAssign {
			info, ok := indexMap[strings.ToLower(assignment.resource.Name)]
			if !ok {
				continue
			}
			if ibSection == nil {
				value := section.Name
				ibSection = &value
			}
			if ibHash == nil {
				if value := sectionValue(lines, "hash"); value != "" {
					ibHash = &value
				}
			}
			if variant == nil {
				variant = assignment.condition
			}
			for _, draw := range extractTouchDrawRanges(conditionLines) {
				if draw.ConditionText == nil || assignment.condition == nil || sameTouchCondition(draw.ConditionText, assignment.condition) {
					draw.FirstIndex += info.Offset
					ranges = append(ranges, draw)
				}
			}
			for _, entry := range conditionLines {
				if regexp.MustCompile(`(?i)^drawindexed\s*=\s*auto$`).MatchString(entry.Line) && sameTouchCondition(entry.Condition, assignment.condition) {
					ranges = append(ranges, TouchDrawRange{FirstIndex: info.Offset, IndexCount: info.Count, ConditionText: assignment.condition})
				}
			}
		}
	}
	if len(ranges) == 0 && ibHash != nil {
		ranges = append(ranges, TouchDrawRange{IndexCount: indexCount, ConditionText: variant})
	}
	return ranges, blendSection, ibSection, ibHash, variant
}

type touchAssignment struct {
	resource  modBufferResource
	condition *string
}

func touchConditionalAssignments(lines []touchConditionalLine, key string, resources map[string]modBufferResource) []touchAssignment {
	out := []touchAssignment{}
	refRE := regexp.MustCompile(`(?i)^(?:ref\s+)?Resource(.+)$`)
	for _, entry := range lines {
		separator := strings.Index(entry.Line, "=")
		if separator < 0 || !strings.EqualFold(strings.TrimSpace(entry.Line[:separator]), key) {
			continue
		}
		match := refRE.FindStringSubmatch(strings.TrimSpace(entry.Line[separator+1:]))
		if len(match) == 2 {
			if resource, ok := resources[strings.ToLower(match[1])]; ok {
				out = append(out, touchAssignment{resource, entry.Condition})
			}
		}
	}
	return out
}

func touchLinesWithConditions(lines []string) []touchConditionalLine {
	type frame struct {
		branches  []string
		condition string
	}
	stack := []frame{}
	out := []touchConditionalLine{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "if "):
			expression := strings.TrimSpace(line[3:])
			stack = append(stack, frame{[]string{expression}, expression})
			continue
		case strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if "):
			expression := strings.TrimSpace(line[5:])
			if strings.HasPrefix(lower, "else if ") {
				expression = strings.TrimSpace(line[8:])
			}
			if len(stack) == 0 {
				stack = append(stack, frame{[]string{expression}, expression})
			} else {
				top := &stack[len(stack)-1]
				branches := append(append([]string(nil), top.branches...), expression)
				top.condition = touchBranchCondition(branches, false)
				top.branches = branches
			}
			continue
		case lower == "else":
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				top.condition = touchBranchCondition(top.branches, true)
			}
			continue
		case lower == "endif":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		var condition *string
		if len(stack) > 0 {
			parts := make([]string, len(stack))
			for i := range stack {
				parts[i] = stack[i].condition
			}
			joined := strings.Join(parts, " && ")
			condition = &joined
		}
		out = append(out, touchConditionalLine{line, condition})
	}
	return out
}

func touchBranchCondition(branches []string, isElse bool) string {
	parts := []string{}
	limit := len(branches)
	if !isElse {
		limit--
	}
	for i := range limit {
		parts = append(parts, "!("+branches[i]+")")
	}
	if !isElse && len(branches) > 0 {
		parts = append(parts, "("+branches[len(branches)-1]+")")
	}
	return strings.Join(parts, " && ")
}
func sameTouchCondition(left, right *string) bool {
	normalize := func(value *string) string {
		if value == nil {
			return ""
		}
		return strings.Join(strings.Fields(*value), " ")
	}
	return normalize(left) == normalize(right)
}

func extractTouchDrawRanges(lines []touchConditionalLine) []TouchDrawRange {
	re := regexp.MustCompile(`(?i)^drawindexed\s*=\s*([^,]+)\s*,\s*([^,]+)\s*,\s*([^,]+)$`)
	out := []TouchDrawRange{}
	for _, entry := range lines {
		match := re.FindStringSubmatch(entry.Line)
		if len(match) != 4 {
			continue
		}
		count, e1 := strconv.Atoi(strings.TrimSpace(match[1]))
		first, e2 := strconv.Atoi(strings.TrimSpace(match[2]))
		base, e3 := strconv.Atoi(strings.TrimSpace(match[3]))
		if e1 == nil && e2 == nil && e3 == nil && count > 0 {
			out = append(out, TouchDrawRange{FirstIndex: first, IndexCount: count, BaseVertex: base, ConditionText: entry.Condition})
		}
	}
	return out
}

func uniqueTouchDrawRanges(input []TouchDrawRange) []TouchDrawRange {
	seen := map[string]bool{}
	out := []TouchDrawRange{}
	for _, item := range input {
		key := fmt.Sprintf("%d:%d:%d:%s", item.FirstIndex, item.IndexCount, item.BaseVertex, derefString(item.ConditionText))
		if !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	return out
}

func buildTouchObjectMaps(ranges []TouchDrawRange, kind string, objectID int, positions []float32, indices []uint32) []TouchObjectMapEntry {
	unique := map[string]TouchDrawRange{}
	for _, entry := range ranges {
		if entry.IndexCount >= 300 {
			key := fmt.Sprintf("%d:%d", entry.FirstIndex, entry.IndexCount)
			if _, ok := unique[key]; !ok {
				unique[key] = entry
			}
		}
	}
	meaningful := make([]TouchDrawRange, 0, len(unique))
	for _, entry := range unique {
		meaningful = append(meaningful, entry)
	}
	sort.Slice(meaningful, func(i, j int) bool {
		if meaningful[i].IndexCount != meaningful[j].IndexCount {
			return meaningful[i].IndexCount > meaningful[j].IndexCount
		}
		return meaningful[i].FirstIndex < meaningful[j].FirstIndex
	})
	if len(meaningful) == 0 && len(ranges) > 0 {
		meaningful = ranges[:1]
	}
	if len(meaningful) == 0 {
		return []TouchObjectMapEntry{}
	}
	if kind == "body" {
		if pair := pickTouchBodyPair(meaningful, positions, indices); len(pair) == 2 {
			return []TouchObjectMapEntry{{pair[0].FirstIndex, pair[0].IndexCount, touchObjectMode, objectID, "clothed"}, {pair[1].FirstIndex, pair[1].IndexCount, touchObjectMode, objectID, "nude"}}
		}
	}
	label := "main"
	if kind == "legs" {
		label = "skin"
	}
	return []TouchObjectMapEntry{{meaningful[0].FirstIndex, meaningful[0].IndexCount, touchObjectMode, objectID, label}}
}

func pickTouchBodyPair(ranges []TouchDrawRange, positions []float32, indices []uint32) []TouchDrawRange {
	bestScore := math.Inf(-1)
	var best []TouchDrawRange
	for i := range ranges {
		for j := i + 1; j < len(ranges); j++ {
			if ranges[i].IndexCount != ranges[j].IndexCount {
				continue
			}
			score := scoreTouchUpperBody(ranges[i], positions, indices) + scoreTouchUpperBody(ranges[j], positions, indices)
			total := score*10000 + float64(ranges[i].IndexCount)
			if total > bestScore {
				bestScore = total
				best = []TouchDrawRange{ranges[i], ranges[j]}
			}
		}
	}
	return best
}

func scoreTouchUpperBody(draw TouchDrawRange, positions []float32, indices []uint32) float64 {
	if len(positions) == 0 {
		return 0
	}
	minZ, maxZ := math.Inf(1), math.Inf(-1)
	for i := 2; i < len(positions); i += 3 {
		z := float64(positions[i])
		minZ = math.Min(minZ, z)
		maxZ = math.Max(maxZ, z)
	}
	span := math.Max(maxZ-minZ, 1e-6)
	low, high := minZ+span*.55, minZ+span*.85
	seen := map[uint32]bool{}
	upper, total := 0, 0
	sum := 0.0
	end := min(len(indices), draw.FirstIndex+draw.IndexCount)
	for i := max(0, draw.FirstIndex); i < end; i++ {
		v := indices[i]
		if int(v)*3+2 >= len(positions) || seen[v] {
			continue
		}
		seen[v] = true
		total++
		z := float64(positions[int(v)*3+2])
		sum += z
		if z >= low && z <= high {
			upper++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(upper)/float64(total)*2 + (sum/float64(total)-minZ)/span
}

func hashTouchFiles(paths []string, root string) (string, error) {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	hash := sha256.New()
	for _, path := range sorted {
		relative, _ := filepath.Rel(root, path)
		_, _ = hash.Write([]byte(filepath.ToSlash(relative)))
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		_, _ = hash.Write(raw)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func sanitizeTouchID(value string) string {
	value = regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "component"
	}
	return value
}
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
