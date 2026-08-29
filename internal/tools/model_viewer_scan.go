package tools

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxModelViewerDirectRunExpansions = 4096

var (
	sectionHashUnderscoreRE = regexp.MustCompile(`(?i)_([0-9a-f]{8})_`)
	sectionHashBareRE       = regexp.MustCompile(`(?i)[0-9a-f]{8}`)
)

type modelViewerDirectBufferState struct{ ib, vb0, vb1, vb2 string }

type modelViewerDirectDrawRecord struct {
	sectionName     string
	state           modelViewerDirectBufferState
	textureHistory  []modelViewerDirectTextureAssignment
	authoredDiffuse bool
	nonDiffuse      []string
	thisFiles       []string
	conditions      ModelViewerDNF
	draw            modelViewerDrawInstruction
	auto            bool
	copies          int
}

type modelViewerDirectConditionalBranch struct {
	expression *string
	lines      []string
}

func collectModelViewerDirectDrawRecords(sections []modINISection, variables map[string]any) ([]modelViewerDirectDrawRecord, error) {
	return collectModelViewerSymbolicDrawRecords(sections, variables)
}

func splitModelViewerDirectConditional(lines []string, start int) ([]modelViewerDirectConditionalBranch, int, bool) {
	first := strings.TrimSpace(lines[start])
	expression := strings.TrimSpace(first[3:])
	branches := []modelViewerDirectConditionalBranch{{expression: &expression}}
	branchStart, depth := start+1, 0
	for index := start + 1; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "if "):
			depth++
		case lower == "endif":
			if depth > 0 {
				depth--
				continue
			}
			branches[len(branches)-1].lines = lines[branchStart:index]
			return branches, index, true
		case depth == 0 && (strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if ")):
			branches[len(branches)-1].lines = lines[branchStart:index]
			nextExpression := strings.TrimSpace(line[5:])
			if strings.HasPrefix(lower, "else if ") {
				nextExpression = strings.TrimSpace(line[8:])
			}
			branches = append(branches, modelViewerDirectConditionalBranch{expression: &nextExpression})
			branchStart = index + 1
		case depth == 0 && lower == "else":
			branches[len(branches)-1].lines = lines[branchStart:index]
			branches = append(branches, modelViewerDirectConditionalBranch{})
			branchStart = index + 1
		}
	}
	return nil, start, false
}

func modelViewerTrimTextureValue(value string) string {
	resource := strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(resource), "ref ") {
		resource = strings.TrimSpace(resource[4:])
	}
	return modelViewerTrimResourcePrefix(resource)
}

func isNonDiffusePsSlot(key, resource string) bool {
	normalizedKey := modelViewerNormalizeKey(key)
	if !strings.HasPrefix(normalizedKey, "pst") || resource == "" {
		return false
	}
	slot := strings.TrimPrefix(normalizedKey, "pst")
	return slot != "0" && !strings.Contains(strings.ToLower(resource), "diffuse")
}

func modelViewerTextureAssignment(key, value, sectionName string) (string, string, bool, bool) {
	normalizedKey := modelViewerNormalizeKey(key)
	isTextureTarget := normalizedKey == "this" || strings.HasPrefix(normalizedKey, "pst") || strings.Contains(normalizedKey, "diffuse") || strings.Contains(normalizedKey, "normalmap") || strings.Contains(normalizedKey, "lightmap") || strings.Contains(normalizedKey, "materialmap")
	if !isTextureTarget {
		return "", "", false, false
	}
	resource := modelViewerTrimTextureValue(value)
	if resource == "" {
		return "", "", false, false
	}
	lowerResource := strings.ToLower(resource)
	sectionRole := hashTextureSuffixRE.FindString(sectionName)
	channel := ""
	authored := false
	switch {
	case strings.Contains(normalizedKey, "normalmap"):
		channel = "normal_map"
	case strings.Contains(normalizedKey, "lightmap"):
		channel = "light_map"
	case strings.Contains(normalizedKey, "materialmap"):
		channel = "material_map"
	case strings.Contains(normalizedKey, "diffuse"):
		channel = "diffuse"
		authored = true
	case normalizedKey == "this":
		switch {
		case strings.Contains(lowerResource, "normalmap"):
			return "normal_map", resource, false, true
		case strings.Contains(lowerResource, "lightmap"):
			return "light_map", resource, false, true
		case strings.Contains(lowerResource, "materialmap"):
			return "material_map", resource, false, true
		case strings.Contains(lowerResource, "diffuse") || strings.EqualFold(sectionRole, "Diffuse"):
			return "diffuse", resource, true, true
		default:
			return "", "", false, false
		}
	case normalizedKey == "pst0":
		channel = "diffuse"
		authored = strings.Contains(lowerResource, "diffuse")
	case strings.HasPrefix(normalizedKey, "pst"):
		if strings.Contains(lowerResource, "normalmap") || strings.Contains(lowerResource, "lightmap") || strings.Contains(lowerResource, "materialmap") {
			channel = classifyModelViewerTextureRole(resource)
			return channel, resource, false, true
		}
		if strings.Contains(lowerResource, "diffuse") {
			channel = "diffuse"
			authored = true
			break
		}
		return "", "", false, false
	default:
		channel = classifyModelViewerTextureRole(resource)
		if channel == "diffuse" {
			return "", "", false, false
		}
	}
	if channel == "" {
		return "", "", false, false
	}
	return channel, resource, authored, true
}

func dedupeModelViewerDirectDrawRecords(records []modelViewerDirectDrawRecord) []modelViewerDirectDrawRecord {
	seen := make(map[string]int)
	output := make([]modelViewerDirectDrawRecord, 0, len(records))
	for _, record := range records {
		key := fmt.Sprintf("%s|%#v|%#v|%#v|%#v|%t", record.sectionName, record.state, record.draw, record.conditions, record.textureHistory, record.auto)
		if index, exists := seen[key]; exists {
			output[index].copies++
			continue
		}
		record.copies = 1
		seen[key] = len(output)
		output = append(output, record)
	}
	return output
}

func collectModelViewerDirectResourceConditions(sections []modINISection, variables map[string]any) map[string]ModelViewerDNF {
	output := make(map[string]ModelViewerDNF)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CommandList") {
			continue
		}
		state, _, err := scanModelViewerSymbolicRoot(sections, section, variables)
		if err != nil {
			continue
		}
		for _, slot := range []string{"ib", "vb0", "vb1", "vb2"} {
			for _, assignment := range effectiveModelViewerSymbolicAssignments(state.buffers[slot]) {
				resource := assignment.resource
				key := modelViewerNormalizeKey(resource)
				if key == "" {
					continue
				}
				if existing, exists := output[key]; exists {
					output[key] = modelViewerDNFOr(existing, assignment.conditions)
				} else {
					output[key] = cloneModelViewerDNF(assignment.conditions)
				}
			}
		}
	}
	return output
}

func buildModelViewerDirectScannedMeshes(iniPath string, sections []modINISection, variables map[string]any) ([]modelViewerDirectMesh, error) {
	return buildModelViewerDirectScannedMeshesAt(iniPath, filepath.Dir(iniPath), sections, variables, newModelViewerBufferCache(), nil)
}

// modelViewerMeshBuildTiming accumulates optional per-stage timings across all
// ini files of one viewer load. Nil timings skip accumulation.
type modelViewerMeshBuildTiming struct {
	ScanMs          int64
	Records         int
	SetupMs         int64
	GeometryMs      int64
	Geometries      int
	OverridesMs     int64
	AttachMs        int64
	NormalizeMs     int64
	LegacyMs        int64
	GroupsMs        int64
	LegacyScanMs    int64
	LegacyPrepareMs int64
	LegacyExtractMs int64
}

func modelViewerHasGeometryGroup(sections []modINISection, resources []modelViewerResource) bool {
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil || len(records) == 0 {
		return false
	}
	resourceMap := make(map[string]modelViewerResource, len(resources))
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	for _, record := range records {
		ib := resourceMap[modelViewerNormalizeKey(record.state.ib)]
		if ib.Filename == "" {
			continue
		}
		if position := resourceMap[modelViewerNormalizeKey(record.state.vb0)]; position.Filename != "" {
			return true
		}
		for _, resource := range resources {
			if resource.Filename != "" && resource.Stride >= 12 {
				return true
			}
		}
	}
	return false
}

func buildModelViewerDirectScannedMeshesAt(iniPath, modDir string, sections []modINISection, variables map[string]any, cache *modelViewerBufferCache, timing *modelViewerMeshBuildTiming) ([]modelViewerDirectMesh, error) {
	stageStartedAt := time.Now()
	records, err := collectModelViewerDirectDrawRecords(sections, variables)
	if err != nil {
		return nil, err
	}
	if timing != nil {
		timing.ScanMs += time.Since(stageStartedAt).Milliseconds()
		timing.Records += len(records)
	}
	if len(records) == 0 {
		return nil, nil
	}
	if len(records) > maxModelViewerDraws {
		return nil, contractError(fmt.Sprintf("Mod has too many draws (%d; limit %d).", len(records), maxModelViewerDraws))
	}
	stageStartedAt = time.Now()
	resources := collectModelViewerResources(sections)
	resourceMap := make(map[string]modelViewerResource)
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	globalBuffers := collectModelViewerGlobalBuffers(sections, variables, resources, resourceMap)
	conditionVariables := modelViewerDirectConditionVariables(sections, variables)
	hashPositions, hashTexcoords := collectHashVertexBuffers(sections)
	componentPositions, componentTexcoords := collectModelViewerComponentBuffers(sections, resourceMap)
	if timing != nil {
		timing.SetupMs += time.Since(stageStartedAt).Milliseconds()
	}
	var output []modelViewerDirectMesh
	geometryIndexes := make(map[string]int)
	for _, record := range records {
		conditions := cloneModelViewerDNF(record.conditions)
		if conditions == nil {
			conditions = modelViewerConditionsToDNF(record.draw.Conditions, conditionVariables)
		}
		state := record.state
		if state.ib == "" {
			state.ib = globalBuffers.ib
		}
		if state.vb0 == "" || state.vb1 == "" {
			hash := extractSectionHash(record.sectionName)
			if hash == "" {
				hash = extractSectionHash(state.ib)
			}
			if state.vb0 == "" {
				state.vb0 = hashPositions[hash]
			}
			if state.vb1 == "" {
				state.vb1 = hashTexcoords[hash]
			}
			// Electron resolveComponentBuffers parity: mods generated by XXMI bind
			// vertex buffers in separate Blend/Texcoord/Position TextureOverrides
			// whose link is the component family, not a hash embedded in the name.
			if state.vb0 == "" || state.vb1 == "" {
				component := modelViewerComponentFromIB(state.ib)
				if state.vb0 == "" {
					state.vb0 = lookupModelViewerComponentValue(componentPositions, component)
				}
				if state.vb1 == "" {
					state.vb1 = lookupModelViewerComponentValue(componentTexcoords, component)
				}
			}
			if (state.vb0 == "" || state.vb1 == "") && globalBuffers.vb0 != "" && globalBuffers.vb1 != "" {
				state.vb0 = globalBuffers.vb0
				state.vb1 = globalBuffers.vb1
				state.vb2 = ""
			}
		}
		ib, ibOK := resourceMap[modelViewerNormalizeKey(state.ib)]
		position, posOK := resourceMap[modelViewerNormalizeKey(state.vb0)]
		if !ibOK || !posOK || ib.Filename == "" || position.Filename == "" {
			continue
		}
		texcoordName := state.vb1
		texcoord, tcOK := resourceMap[modelViewerNormalizeKey(texcoordName)]
		if !tcOK || texcoord.Filename == "" {
			continue
		}
		geometryKey := fmt.Sprintf("%s|%s|%s|%s|%d|%d|%t", record.sectionName, ib.Filename, position.Filename, texcoord.Filename, record.draw.IndexCount, record.draw.StartIndex, record.auto)
		if existingIndex, exists := geometryIndexes[geometryKey]; exists {
			mesh := &output[existingIndex]
			mesh.conditions = modelViewerDNFOr(mesh.conditions, conditions)
			mesh.textureAuthored = mesh.textureAuthored || record.authoredDiffuse
			mesh.nonDiffuseTextureFiles = unionSlashPaths(mesh.nonDiffuseTextureFiles, resourceFilenames(resourceMap, record.nonDiffuse))
			appendModelViewerDirectTextureHistory(mesh, record.textureHistory, resourceMap)
			continue
		}
		ibPath := filepath.Join(modDir, filepath.FromSlash(ib.Filename))
		ibRaw, err := cache.read(ibPath)
		if err != nil {
			continue
		}
		indices, err := cache.decodeIndices(ibPath, ib.Format, ibRaw)
		if err != nil {
			return nil, err
		}
		end := len(indices)
		if !record.auto {
			end = record.draw.StartIndex + record.draw.IndexCount
		}
		if record.draw.StartIndex < 0 || end < record.draw.StartIndex || end > len(indices) {
			continue
		}
		active := make([]uint32, 0, end-record.draw.StartIndex)
		for _, index := range indices[record.draw.StartIndex:end] {
			active = append(active, index+uint32(record.draw.BaseVertex))
		}
		posStride := position.Stride
		if posStride <= 0 {
			posStride = 40
		}
		tcStride := texcoord.Stride
		if tcStride <= 0 {
			tcStride = 20
		}
		buffers, buffersErr := cache.paired(filepath.Join(modDir, filepath.FromSlash(position.Filename)), posStride, filepath.Join(modDir, filepath.FromSlash(texcoord.Filename)), tcStride)
		if buffersErr != nil {
			continue
		}
		combined, stride, uvOffset, uvFormat := buffers.combined, buffers.stride, buffers.uvOffset, buffers.uvFormat
		layout := modelViewerFmtLayout{Stride: stride, Topology: "trianglelist", IndexFormat: ib.Format, Elements: []modelViewerFmtElement{{SemanticName: "POSITION", Format: "DXGI_FORMAT_R32G32B32_FLOAT", AlignedByteOffset: 0, InputSlotClass: "per-vertex"}, {SemanticName: "TEXCOORD", Format: uvFormat, AlignedByteOffset: uvOffset, InputSlotClass: "per-vertex"}}}
		if posStride >= 40 && buffers.hasFrame {
			layout.Elements = append(layout.Elements, modelViewerFmtElement{SemanticName: "NORMAL", Format: "DXGI_FORMAT_R32G32B32_FLOAT", AlignedByteOffset: 12, InputSlotClass: "per-vertex"}, modelViewerFmtElement{SemanticName: "TANGENT", Format: "DXGI_FORMAT_R32G32B32A32_FLOAT", AlignedByteOffset: 24, InputSlotClass: "per-vertex"})
		}
		stageStartedAt = time.Now()
		geometry, geometryErr := extractModelViewerGeometry(combined, stride, layout, active, true, false, true, nil)
		if timing != nil {
			timing.GeometryMs += time.Since(stageStartedAt).Milliseconds()
		}
		if geometryErr != nil {
			return nil, geometryErr
		}
		if geometry == nil {
			continue
		}
		if timing != nil {
			timing.Geometries++
		}
		for offset := 1; offset < len(geometry.Texcoord0); offset += 2 {
			geometry.Texcoord0[offset] = 1 - geometry.Texcoord0[offset]
		}
		component := record.sectionName
		if component == "" {
			component = record.state.ib
		}
		id := modelViewerNormalizeKey(filepath.Base(iniPath)) + ":" + modelViewerNormalizeKey(component) + ":" + fmt.Sprint(len(output))
		indexCount := record.draw.IndexCount * recordDrawCopies(record)
		if record.auto {
			indexCount = len(active) * recordDrawCopies(record)
		}
		mesh := modelViewerDirectMesh{id: id, component: component, sectionName: record.sectionName, ibName: state.ib, positionFile: position.Filename, geometry: geometry, conditions: conditions, positionAssignments: []modelViewerDirectPositionAssignment{{conditions: cloneModelViewerDNF(conditions), file: position.Filename, positions: geometry.Position}}, textureAuthored: record.authoredDiffuse, nonDiffuseTextureFiles: resourceFilenames(resourceMap, record.nonDiffuse), indexCount: indexCount}
		appendModelViewerDirectTextureHistory(&mesh, record.textureHistory, resourceMap)
		geometryIndexes[geometryKey] = len(output)
		output = append(output, mesh)
	}
	stageStartedAt = time.Now()
	if err := attachModelViewerDirectPositionOverrides(output, sections, resources, modDir, conditionVariables, cache); err != nil {
		return nil, err
	}
	if timing != nil {
		timing.OverridesMs += time.Since(stageStartedAt).Milliseconds()
	}
	stageStartedAt = time.Now()
	attachFamilyAndStemTextures(output, sections, resources, conditionVariables)
	attachIbComponentDumpTextures(output, resources)
	bindHashImageTextures(output, sections, resources, modDir, conditionVariables)
	attachWwmiDumpTextures(output, resources, modDir)
	if timing != nil {
		timing.AttachMs += time.Since(stageStartedAt).Milliseconds()
	}
	stageStartedAt = time.Now()
	for meshIndex := range output {
		output[meshIndex].conditions = normalizeModelViewerDNFWithDomains(output[meshIndex].conditions, conditionVariables)
		for assignmentIndex := range output[meshIndex].textureAssignments {
			output[meshIndex].textureAssignments[assignmentIndex].conditions = normalizeModelViewerDNFWithDomains(output[meshIndex].textureAssignments[assignmentIndex].conditions, conditionVariables)
		}
		for assignmentIndex := range output[meshIndex].positionAssignments {
			output[meshIndex].positionAssignments[assignmentIndex].conditions = normalizeModelViewerDNFWithDomains(output[meshIndex].positionAssignments[assignmentIndex].conditions, conditionVariables)
		}
	}
	if timing != nil {
		timing.NormalizeMs += time.Since(stageStartedAt).Milliseconds()
	}
	return output, nil
}

func collectModelViewerGlobalBuffers(sections []modINISection, variables map[string]any, resources []modelViewerResource, resourceMap map[string]modelViewerResource) modelViewerDirectBufferState {
	var global modelViewerDirectBufferState
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CommandList") {
			continue
		}
		state, _, err := scanModelViewerSymbolicRoot(sections, section, variables)
		if err != nil {
			continue
		}
		unconditional := func(slot string) string {
			for _, assignment := range effectiveModelViewerSymbolicAssignments(state.buffers[slot]) {
				if modelViewerDNFIsTrue(assignment.conditions) {
					return assignment.resource
				}
			}
			return ""
		}
		if global.ib == "" {
			global.ib = unconditional("ib")
		}
		if global.vb0 == "" {
			global.vb0 = unconditional("vb0")
		}
		if global.vb1 == "" {
			global.vb1 = unconditional("vb1")
		}
	}

	if global.vb0 != "" && resourceMap[modelViewerNormalizeKey(global.vb0)].Filename == "" {
		for _, resource := range resources {
			if resource.Filename != "" && strings.Contains(strings.ToUpper(resource.Format), "R32G32B32") {
				global.vb0 = resource.Name
				break
			}
		}
	}
	return global
}

type modelViewerDirectPositionResourceAssignment struct {
	target, resource string
	conditions       ModelViewerDNF
}

func attachModelViewerDirectPositionOverrides(meshes []modelViewerDirectMesh, sections []modINISection, resources []modelViewerResource, modDir string, variables map[string]any, cache *modelViewerBufferCache) error {
	var assignments []modelViewerDirectPositionResourceAssignment
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") || !strings.HasSuffix(strings.ToLower(section.Name), "position") {
			continue
		}
		target := section.Name[:len(section.Name)-len("position")]
		state, _, err := scanModelViewerSymbolicRoot(sections, section, variables)
		if err != nil {
			return err
		}
		for _, assignment := range effectiveModelViewerSymbolicAssignments(state.buffers["vb0"]) {
			if len(assignment.conditions) > 0 {
				assignments = append(assignments, modelViewerDirectPositionResourceAssignment{target: target, resource: assignment.resource, conditions: cloneModelViewerDNF(assignment.conditions)})
			}
		}
	}
	if len(assignments) == 0 {
		return nil
	}
	resourceMap := make(map[string]modelViewerResource)
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	for meshIndex := range meshes {
		mesh := &meshes[meshIndex]
		var variants []modelViewerDirectPositionAssignment
		files := make(map[string]bool)
		for _, assignment := range assignments {
			if !modelViewerKeyMatches(assignment.target, mesh.component, false) {
				continue
			}
			resource, ok := resourceMap[modelViewerNormalizeKey(assignment.resource)]
			if !ok || resource.Filename == "" {
				continue
			}
			positions, err := readModelViewerShapePositions(cache, filepath.Join(modDir, filepath.FromSlash(resource.Filename)), resource.Stride, mesh.geometry.SourceIndices, mesh.geometry.VertexCount)
			if err != nil || len(positions) != len(mesh.geometry.Position) {
				continue
			}
			variants = append(variants, modelViewerDirectPositionAssignment{conditions: assignment.conditions, file: resource.Filename, positions: positions})
			files[modelViewerNormalizeKey(filepath.ToSlash(resource.Filename))] = true
		}
		if len(files) > 1 {
			mesh.positionAssignments = variants
		}
	}
	return nil
}

func appendModelViewerDirectTextureHistory(mesh *modelViewerDirectMesh, assignments []modelViewerDirectTextureAssignment, resourceMap map[string]modelViewerResource) {
	if mesh == nil {
		return
	}
	for _, assignment := range assignments {
		if resource, ok := resourceMap[modelViewerNormalizeKey(assignment.resource)]; ok {
			assignment.file = resource.Filename
		}
		if assignment.role == "diffuse" {
			mesh.textureAuthored = mesh.textureAuthored || assignment.authored
			if mesh.textureDefaultFile == "" && assignment.file != "" {
				mesh.textureDefaultFile = assignment.file
			}
		}
		merged := false
		for index := range mesh.textureAssignments {
			existing := &mesh.textureAssignments[index]
			if existing.role != assignment.role || modelViewerNormalizeKey(existing.resource) != modelViewerNormalizeKey(assignment.resource) {
				continue
			}
			existing.conditions = modelViewerDNFOr(existing.conditions, assignment.conditions)
			existing.authored = existing.authored || assignment.authored
			if existing.file == "" {
				existing.file = assignment.file
			}
			merged = true
			break
		}
		if !merged {
			assignment.conditions = cloneModelViewerDNF(assignment.conditions)
			mesh.textureAssignments = append(mesh.textureAssignments, assignment)
		}
	}
}

func resourceFilenames(resourceMap map[string]modelViewerResource, names []string) []string {
	var files []string
	for _, name := range names {
		if resource, ok := resourceMap[modelViewerNormalizeKey(name)]; ok && resource.Filename != "" {
			files = append(files, resource.Filename)
		}
	}
	return files
}

func collectHashVertexBuffers(sections []modINISection) (map[string]string, map[string]string) {
	positions := map[string]string{}
	texcoords := map[string]string{}
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		hash := extractSectionHash(section.Name)
		if hash == "" {
			continue
		}
		vb0 := modelViewerTrimTextureValue(modelViewerSectionValue(section, "vb0"))
		vb1 := modelViewerTrimTextureValue(modelViewerSectionValue(section, "vb1"))
		if vb0 != "" {
			if _, exists := positions[hash]; !exists {
				positions[hash] = vb0
			}
		}
		if vb1 != "" {
			if _, exists := texcoords[hash]; !exists {
				texcoords[hash] = vb1
			}
		}
	}
	return positions, texcoords
}

func recordDrawCopies(record modelViewerDirectDrawRecord) int {
	if record.copies < 1 {
		return 1
	}
	return record.copies
}

func extractSectionHash(name string) string {
	if match := sectionHashUnderscoreRE.FindStringSubmatch(name); len(match) > 1 {
		return strings.ToLower(match[1])
	}
	if match := sectionHashBareRE.FindString(name); match != "" {
		return strings.ToLower(match)
	}
	return ""
}

func sectionHandlingSkip(section modINISection) bool {
	return strings.EqualFold(strings.TrimSpace(modelViewerSectionValue(section, "handling")), "skip")
}

var modelViewerLODOverrideRE = regexp.MustCompile(`(?i)_LOD\d*$`)

func isModelViewerPreviewSkippedOverride(name string) bool {
	return modelViewerLODOverrideRE.MatchString(name)
}

var (
	modelViewerComponentTrailingLettersRE = regexp.MustCompile(`[A-Za-z]+$`)
	modelViewerComponentTrailingWordRE    = regexp.MustCompile(`[A-Z][a-z]+$`)
)

// collectModelViewerComponentBuffers mirrors Electron's resolveComponentBuffers:
// TextureOverrides whose name ends in Blend/Texcoord/Position bind vertex
// buffers per component family ("RobinSummerettoHeadBlend" -> component
// "RobinSummerettoHead"), which is the only linkage XXMI-generated INIs provide
// between buffer overrides and the draw sections that reference the IBs.
func collectModelViewerComponentBuffers(sections []modINISection, resourceMap map[string]modelViewerResource) (map[string]string, map[string]string) {
	componentPositions := make(map[string]string)
	componentTexcoords := make(map[string]string)
	assign := func(mapping map[string]string, component, value string) {
		key := modelViewerNormalizeKey(component)
		value = modelViewerTrimResourcePrefix(value)
		if key == "" || value == "" {
			return
		}
		if _, exists := mapping[key]; !exists {
			mapping[key] = value
		}
	}
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		if component, ok := trimModelViewerOverrideSuffix(section.Name, "Texcoord"); ok {
			if value := modelViewerSectionValue(section, "vb1"); value != "" {
				assign(componentTexcoords, component, value)
			}
		}
	}
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		if component, ok := trimModelViewerOverrideSuffix(section.Name, "Blend"); ok {
			if value := modelViewerSectionValue(section, "vb0"); value != "" {
				assign(componentPositions, component, value)
			}
			key := modelViewerNormalizeKey(component)
			if value := modelViewerTrimResourcePrefix(modelViewerSectionValue(section, "vb1")); value != "" {
				if _, exists := componentTexcoords[key]; !exists {
					if resource, ok := resourceMap[modelViewerNormalizeKey(value)]; !ok || resource.Stride != 32 {
						componentTexcoords[key] = value
					}
				}
			}
		} else if component, ok := trimModelViewerOverrideSuffix(section.Name, "Position"); ok {
			if value := modelViewerSectionValue(section, "vb0"); value != "" {
				assign(componentPositions, component, value)
			}
		}
	}
	return componentPositions, componentTexcoords
}

func trimModelViewerOverrideSuffix(name, suffix string) (string, bool) {
	if len(name) < len(suffix) || !strings.EqualFold(name[len(name)-len(suffix):], suffix) {
		return "", false
	}
	return name[:len(name)-len(suffix)], true
}

// modelViewerComponentFromIB mirrors Electron's ibResToComponent for
// scanner-trimmed IB resource names ("RobinSummerettoHeadAIB" ->
// "RobinSummerettoHead").
func modelViewerComponentFromIB(ibResource string) string {
	value := strings.TrimSpace(ibResource)
	value = modelViewerNumericSuffixRE.ReplaceAllString(value, "")
	value = strings.TrimSuffix(value, "IB")
	if len(value) > 0 && value[len(value)-1] >= 'A' && value[len(value)-1] <= 'Z' {
		value = value[:len(value)-1]
	}
	return value
}

// lookupModelViewerComponentValue mirrors Electron's lookupCompValue: exact
// component first, then the component with trailing letters stripped, then the
// component with a trailing CamelCase word stripped.
func lookupModelViewerComponentValue(mapping map[string]string, component string) string {
	if value, ok := mapping[modelViewerNormalizeKey(component)]; ok {
		return value
	}
	strippedLetters := modelViewerComponentTrailingLettersRE.ReplaceAllString(component, "")
	if strippedLetters != "" && strippedLetters != component {
		if value, ok := mapping[modelViewerNormalizeKey(strippedLetters)]; ok {
			return value
		}
	}
	strippedWord := modelViewerComponentTrailingWordRE.ReplaceAllString(component, "")
	if strippedWord != "" && strippedWord != component {
		if value, ok := mapping[modelViewerNormalizeKey(strippedWord)]; ok {
			return value
		}
	}
	return ""
}
