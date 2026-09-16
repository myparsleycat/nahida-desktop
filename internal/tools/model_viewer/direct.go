package modelviewer

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type modelViewerDirectMesh struct {
	id, component, sectionName, ibName string
	positionFile                       string
	geometry                           *modelViewerGeometry
	conditions                         ModelViewerDNF
	textureAssignments                 []modelViewerDirectTextureAssignment
	positionAssignments                []modelViewerDirectPositionAssignment
	textureAuthored                    bool
	textureDefaultFile                 string
	nonDiffuseTextureFiles             []string
	indexCount                         int
}

type modelViewerDirectTextureAssignment struct {
	role       string
	resource   string
	file       string
	authored   bool
	conditions ModelViewerDNF
}

type modelViewerDirectPositionAssignment struct {
	conditions  ModelViewerDNF
	sourcePath  string
	stride      int
	sourceBytes int64
}

func modelViewerDirectGatingVariables(meshes []ModelViewerMeshTransport, _ []ModelViewerStateRule) map[string]bool {
	output := make(map[string]bool)
	addDNF := func(dnf ModelViewerDNF) {
		for _, group := range dnf {
			for _, clause := range group {
				output[modelViewerNormalizeKey(clause.Var)] = true
			}
		}
	}
	for _, mesh := range meshes {
		addDNF(mesh.Conditions)
		for _, variants := range [][]ModelViewerTextureVariant{
			mesh.TextureVariants,
			mesh.NormalMapVariants,
			mesh.LightMapVariants,
			mesh.MaterialMapVariants,
		} {
			for _, variant := range variants {
				addDNF(variant.Conditions)
			}
		}
		for _, variant := range mesh.PositionVariants {
			addDNF(variant.Conditions)
		}
	}
	return output
}

func modelViewerVariableIsGating(variable ModelViewerVariable, gating map[string]bool) bool {
	return gating[modelViewerNormalizeKey(variable.ID)]
}

func buildModelViewerDirectMeshes(
	iniPath, assetPath string,
	sections []modINISection,
) ([]modelViewerDirectMesh, []modelViewerTextureBinding, []modelViewerResource, []modelViewerShapeKey, error) {
	return buildModelViewerDirectMeshesAt(
		iniPath,
		filepath.Dir(iniPath),
		assetPath,
		sections,
		newModelViewerBufferCache(),
		nil,
	)
}

func buildModelViewerDirectMeshesAt(
	iniPath, modDir, assetPath string,
	sections []modINISection,
	cache *modelViewerBufferCache,
	timing *modelViewerMeshBuildTiming,
) ([]modelViewerDirectMesh, []modelViewerTextureBinding, []modelViewerResource, []modelViewerShapeKey, error) {
	resources := resolveModelViewerEffectiveResourcesAt(modDir, modDir, sections, collectModelViewerResources(sections))
	return buildModelViewerDirectMeshesPrepared(iniPath, modDir, assetPath, sections, resources, cache, timing)
}

func buildModelViewerDirectMeshesPrepared(
	iniPath, modDir, assetPath string,
	sections []modINISection,
	resources []modelViewerResource,
	cache *modelViewerBufferCache,
	timing *modelViewerMeshBuildTiming,
) ([]modelViewerDirectMesh, []modelViewerTextureBinding, []modelViewerResource, []modelViewerShapeKey, error) {
	variables := collectModelViewerDefaultVariables(sections)
	textures := collectModelViewerTextureBindings(sections, variables)
	scanned, scanErr := buildModelViewerDirectScannedMeshesPrepared(
		iniPath,
		modDir,
		sections,
		variables,
		resources,
		cache,
		timing,
	)
	if scanErr != nil {
		return nil, nil, nil, nil, scanErr
	}
	if len(scanned) > 0 {
		return scanned, textures, resources, collectModelViewerShapeKeys(sections, resources, modDir), nil
	}
	output, legacyErr := buildModelViewerLegacyMeshes(modelViewerLegacyBuild{
		iniPath:   iniPath,
		modDir:    modDir,
		assetPath: assetPath,
		sections:  sections,
		resources: resources,
		variables: variables,
		textures:  textures,
		cache:     cache,
		timing:    timing,
	})
	if legacyErr != nil {
		return nil, nil, nil, nil, legacyErr
	}
	return output, textures, resources, collectModelViewerShapeKeys(sections, resources, modDir), nil
}

// modelViewerLegacyBuild carries the shared inputs of the legacy mesh build.
type modelViewerLegacyBuild struct {
	iniPath   string
	modDir    string
	assetPath string
	sections  []modINISection
	resources []modelViewerResource
	variables map[string]any
	textures  []modelViewerTextureBinding
	cache     *modelViewerBufferCache
	timing    *modelViewerMeshBuildTiming
}

// buildModelViewerLegacyMeshes handles the implicit-group fallback. Electron's
// component resolver can still construct a draw group when an override supplies
// only an IB and the vertex buffers are discoverable by resource family, so this
// path stays reachable even though the scanned path handles every explicit case.
func buildModelViewerLegacyMeshes(build modelViewerLegacyBuild) ([]modelViewerDirectMesh, error) {
	iniPath, modDir, assetPath := build.iniPath, build.modDir, build.assetPath
	sections, resources, variables := build.sections, build.resources, build.variables
	textures, cache, timing := build.textures, build.cache, build.timing
	legacyStartedAt := time.Now()
	layoutName := detectModelViewerLayout(sections, resources)
	groupsStartedAt := time.Now()
	groups, err := collectModelViewerBufferGroups(modDir, layoutName, resources, cache, nil)
	if timing != nil {
		timing.GroupsMs += time.Since(groupsStartedAt).Milliseconds()
	}
	if err != nil {
		return nil, err
	}
	conditionVariables := modelViewerDirectConditionVariables(sections, variables)
	legacyScanStartedAt := time.Now()
	resourceConditions := collectModelViewerDirectResourceConditions(sections, variables)
	draws := collectModelViewerDrawBindings(sections, variables)
	ibs := collectModelViewerIBResources(resources, groups, sections, variables, textures, draws, true)
	if timing != nil {
		timing.LegacyScanMs += time.Since(legacyScanStartedAt).Milliseconds()
	}
	var output []modelViewerDirectMesh
	type legacyPreparedDraw struct {
		section string
		draw    modelViewerDrawInstruction
	}
	type legacyPreparedIB struct {
		ib      modelViewerIbResource
		group   *modelViewerBufferGroup
		layout  modelViewerFmtLayout
		indices []uint32
		draws   []legacyPreparedDraw
	}
	var prepared []legacyPreparedIB
	drawTotal := 0
	legacyPrepareStartedAt := time.Now()
	for _, ib := range ibs {
		var group *modelViewerBufferGroup
		for index := range groups {
			if modelViewerKeyMatches(groups[index].Key, ib.Key, true) {
				group = &groups[index]
				break
			}
		}
		if group == nil {
			for index := range groups {
				if modelViewerKeyMatches(groups[index].Key, ib.Key, false) {
					group = &groups[index]
					break
				}
			}
		}
		if group == nil {
			continue
		}
		localFMT := filepath.Join(
			modDir,
			strings.TrimSuffix(filepath.Base(ib.Filename), filepath.Ext(ib.Filename))+".fmt",
		)
		fmtKey := fmt.Sprintf("%s|%s|%s|%d|%s|%s", modDir, assetPath, ib.Filename, group.Stride, layoutName, ib.Format)
		layout, loadErr := cache.fmtLayout(fmtKey, func() (modelViewerFmtLayout, error) {
			if assetPath != "" || regularFile(localFMT) {
				return loadModelViewerFmt(modDir, assetPath, ib, group.Stride, layoutName)
			}
			return modelViewerFmtLayout{}, fmt.Errorf("asset layout path is not configured")
		})
		if loadErr != nil {
			layout, loadErr = inferModelViewerFmtLayout(*group, resources, layoutName, ib.Format)
			if loadErr != nil {
				continue
			}
		}
		ibPath := filepath.Join(modDir, filepath.FromSlash(ib.Filename))
		raw, readErr := cache.read(ibPath)
		if readErr != nil {
			continue
		}
		indices, decodeErr := cache.decodeIndices(ibPath, lo.CoalesceOrEmpty(ib.Format, layout.IndexFormat), raw)
		if decodeErr != nil {
			continue
		}
		var ibDraws []legacyPreparedDraw
		for _, binding := range draws {
			if modelViewerNormalizeKey(binding.IBResourceName) == modelViewerNormalizeKey(ib.Name) {
				for _, draw := range binding.Draws {
					ibDraws = append(ibDraws, legacyPreparedDraw{section: binding.SectionName, draw: draw})
				}
			}
		}
		if len(ibDraws) == 0 {
			ibDraws = append(
				ibDraws,
				legacyPreparedDraw{draw: modelViewerDrawInstruction{IBResourceName: ib.Name, IndexCount: len(indices)}},
			)
		}
		drawTotal += len(ibDraws)
		if drawTotal > maxModelViewerDraws {
			return nil, infra.ContractError(
				fmt.Sprintf("Mod has too many draws (%d; limit %d).", drawTotal, maxModelViewerDraws),
			)
		}
		prepared = append(
			prepared,
			legacyPreparedIB{ib: ib, group: group, layout: layout, indices: indices, draws: ibDraws},
		)
	}
	if timing != nil {
		timing.LegacyPrepareMs += time.Since(legacyPrepareStartedAt).Milliseconds()
	}
	// Per-draw geometry extraction runs in parallel; results are reassembled in
	// the original ib+draw order so mesh output stays identical to the serial pass.
	type legacyTask struct {
		entry     legacyPreparedIB
		draw      legacyPreparedDraw
		drawIndex int
	}
	var tasks []legacyTask
	for _, entry := range prepared {
		for drawIndex, drawEntry := range entry.draws {
			tasks = append(tasks, legacyTask{entry: entry, draw: drawEntry, drawIndex: drawIndex})
		}
	}
	legacyExtractStartedAt := time.Now()
	taskMeshes := make([]modelViewerDirectMesh, len(tasks))
	taskValid := make([]bool, len(tasks))
	buildLegacyMesh := func(task legacyTask) (modelViewerDirectMesh, bool) {
		end := task.draw.draw.StartIndex + task.draw.draw.IndexCount
		if task.draw.draw.StartIndex < 0 || end > len(task.entry.indices) {
			return modelViewerDirectMesh{}, false
		}
		active := make([]uint32, 0, task.draw.draw.IndexCount)
		for _, index := range task.entry.indices[task.draw.draw.StartIndex:end] {
			value := int64(index) + int64(task.draw.draw.BaseVertex)
			if value >= 0 {
				active = append(active, uint32(value))
			}
		}
		if layoutName == "wwmi" {
			reverseModelViewerTriangleWinding(active)
		}
		geoKey := fmt.Sprintf(
			"%s|%d|%s|%s|%s|%d|%d|%d",
			strings.Join(task.entry.group.SourceFiles, "|"),
			task.entry.group.Stride,
			modelViewerLayoutKey(task.entry.layout),
			filepath.Join(modDir, filepath.FromSlash(task.entry.ib.Filename)),
			lo.CoalesceOrEmpty(task.entry.ib.Format, task.entry.layout.IndexFormat),
			task.draw.draw.StartIndex,
			task.draw.draw.IndexCount,
			task.draw.draw.BaseVertex,
		)
		geometry, geometryErr := cache.geometry(geoKey, func() (*modelViewerGeometry, error) {
			geometry, err := extractModelViewerGeometry(
				task.entry.group.VB,
				task.entry.group.Stride,
				task.entry.layout,
				active,
				modelViewerGeometryOptions{includeTangents: true, compact: true},
				nil,
			)
			if err != nil || geometry == nil {
				return geometry, err
			}
			for offset := 1; offset < len(geometry.Texcoord0); offset += 2 {
				geometry.Texcoord0[offset] = 1 - geometry.Texcoord0[offset]
			}
			return geometry, nil
		})
		if geometryErr != nil || geometry == nil {
			return modelViewerDirectMesh{}, false
		}
		conditions := modelViewerConditionsToDNF(task.draw.draw.Conditions, conditionVariables)
		if assigned, exists := resourceConditions[modelViewerNormalizeKey(task.entry.ib.Name)]; exists {
			conditions = modelViewerDNFAnd(conditions, assigned)
		}
		// Electron names meshes after the TextureOverride section (displayName),
		// not the IB resource key.
		component := task.draw.section
		if component == "" {
			component = task.entry.ib.Name
		}
		id := modelViewerNormalizeKey(
			filepath.Base(iniPath),
		) + ":" + modelViewerNormalizeKey(
			component,
		) + ":" + strconv.Itoa(
			task.drawIndex,
		)
		positionFile := task.entry.group.VBFilename
		if len(task.entry.group.SourceFiles) > 0 {
			positionFile = task.entry.group.SourceFiles[0]
		}
		return modelViewerDirectMesh{
			id:           id,
			component:    component,
			sectionName:  task.draw.section,
			ibName:       task.entry.ib.Name,
			positionFile: positionFile,
			geometry:     geometry,
			conditions:   conditions,
		}, true
	}
	if workers := min(len(tasks), runtime.GOMAXPROCS(0)); workers > 1 {
		work := make(chan int)
		var workerGroup sync.WaitGroup
		for range workers {
			workerGroup.Add(1)
			go func() {
				defer workerGroup.Done()
				for taskIndex := range work {
					taskMeshes[taskIndex], taskValid[taskIndex] = buildLegacyMesh(tasks[taskIndex])
				}
			}()
		}
		for taskIndex := range tasks {
			work <- taskIndex
		}
		close(work)
		workerGroup.Wait()
	} else {
		for taskIndex := range tasks {
			taskMeshes[taskIndex], taskValid[taskIndex] = buildLegacyMesh(tasks[taskIndex])
		}
	}
	if timing != nil {
		timing.LegacyExtractMs += time.Since(legacyExtractStartedAt).Milliseconds()
	}
	for taskIndex := range tasks {
		if taskValid[taskIndex] {
			output = append(output, taskMeshes[taskIndex])
		}
	}
	if err := attachModelViewerDirectPositionOverrides(
		output,
		sections,
		resources,
		modDir,
		conditionVariables,
		cache,
	); err != nil {
		return nil, err
	}
	attachFamilyAndStemTextures(output, sections, resources, conditionVariables)
	attachIbComponentDumpTextures(output, resources)
	bindHashImageTextures(output, sections, resources, modDir, conditionVariables)
	attachWwmiDumpTextures(output, resources, modDir)
	for meshIndex := range output {
		output[meshIndex].conditions = normalizeModelViewerDNFWithDomains(
			output[meshIndex].conditions,
			conditionVariables,
		)
		for assignmentIndex := range output[meshIndex].textureAssignments {
			output[meshIndex].textureAssignments[assignmentIndex].conditions = normalizeModelViewerDNFWithDomains(
				output[meshIndex].textureAssignments[assignmentIndex].conditions,
				conditionVariables,
			)
		}
		for assignmentIndex := range output[meshIndex].positionAssignments {
			output[meshIndex].positionAssignments[assignmentIndex].conditions = normalizeModelViewerDNFWithDomains(
				output[meshIndex].positionAssignments[assignmentIndex].conditions,
				conditionVariables,
			)
		}
	}
	if timing != nil {
		timing.LegacyMs += time.Since(legacyStartedAt).Milliseconds()
	}
	return output, nil
}

func collectModelViewerIBResources(
	resources []modelViewerResource,
	groups []modelViewerBufferGroup,
	_ []modINISection,
	_ map[string]any,
	textureBindings []modelViewerTextureBinding,
	drawBindings []modelViewerDrawBinding,
	_ bool,
) []modelViewerIbResource {
	keys := sortedModelViewerGroupKeys(groups)
	var output []modelViewerIbResource
	seen := make(map[string]bool)
	for _, resource := range resources {
		filename := strings.TrimSpace(resource.Filename)
		nameKey := strings.ToLower(resource.Name)
		extension := strings.ToLower(filepath.Ext(filename))
		isIndexBuffer := extension == ".ib" || strings.Contains(strings.ToUpper(resource.Format), "UINT") ||
			strings.HasSuffix(nameKey, "ib") ||
			strings.Contains(nameKey, "indexbuffer")
		if filename == "" || !isIndexBuffer {
			continue
		}
		identity := modelViewerNormalizeKey(resource.Name + ":" + filename)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		stem := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
		ib := modelViewerIbResource{
			Name:     resource.Name,
			Filename: filename,
			Format:   resource.Format,
			Key:      modelViewerBestKeyForIB(stem, resource.Name, keys),
		}
		for _, binding := range drawBindings {
			if modelViewerNormalizeKey(binding.IBResourceName) == modelViewerNormalizeKey(resource.Name) &&
				binding.OverrideHash != "" {
				ib.OverrideHashes = appendUniqueModelViewer(ib.OverrideHashes, binding.OverrideHash)
			}
		}
		for _, binding := range textureBindings {
			if modelViewerNormalizeKey(binding.IBResourceName) == modelViewerNormalizeKey(resource.Name) &&
				binding.OverrideHash != "" {
				ib.OverrideHashes = appendUniqueModelViewer(ib.OverrideHashes, binding.OverrideHash)
			}
		}
		if len(ib.OverrideHashes) > 0 {
			ib.OverrideHash = ib.OverrideHashes[0]
		}
		output = append(output, ib)
	}
	return output
}

func inferModelViewerFmtLayout(
	group modelViewerBufferGroup,
	resources []modelViewerResource,
	layoutName, indexFormat string,
) (modelViewerFmtLayout, error) {
	layout := modelViewerFmtLayout{Stride: group.Stride, Topology: "trianglelist", IndexFormat: indexFormat}
	if layout.IndexFormat == "" {
		layout.IndexFormat = "DXGI_FORMAT_R32_UINT"
	}
	if layoutName == "wwmi" {
		byKind := make(map[string]modelViewerResource)
		for _, resource := range resources {
			typed := parseModelViewerWwmiResourceName(resource.Name)
			if typed == nil || !modelViewerKeyMatches(typed.Key, group.Key, true) {
				continue
			}
			byKind[typed.Kind] = resource
		}
		offset, texcoordOffset := 0, -1
		for _, kind := range []string{"position", "vector", "blend", "color", "texcoord"} {
			resource, exists := byKind[kind]
			if !exists {
				continue
			}
			switch kind {
			case "position":
				layout.Elements = append(
					layout.Elements,
					modelViewerFmtElement{
						SemanticName:      "POSITION",
						Format:            "DXGI_FORMAT_R32G32B32_FLOAT",
						AlignedByteOffset: offset,
						InputSlotClass:    "per-vertex",
					},
				)
			case "vector":
				layout.Elements = append(
					layout.Elements,
					modelViewerFmtElement{
						SemanticName:      "NORMAL",
						Format:            lo.CoalesceOrEmpty(resource.Format, "DXGI_FORMAT_R8G8B8A8_SNORM"),
						AlignedByteOffset: offset,
						InputSlotClass:    "per-vertex",
					},
				)
			case "texcoord":
				texcoordOffset = offset
			}
			offset += resource.Stride
		}
		if texcoordOffset >= 0 {
			layout.Elements = append(
				layout.Elements,
				modelViewerFmtElement{
					SemanticName:      "TEXCOORD",
					Format:            "DXGI_FORMAT_R16G16_FLOAT",
					AlignedByteOffset: texcoordOffset,
					InputSlotClass:    "per-vertex",
				},
			)
		}
	} else {
		positionStride, blendStride, texcoordStride := 0, 0, 0
		for _, resource := range resources {
			typed := parseModelViewerMihoyoResourceName(resource.Name)
			if typed == nil || !modelViewerKeyMatches(typed.Key, group.Key, true) {
				continue
			}
			switch typed.Kind {
			case "position":
				positionStride = resource.Stride
			case "blend":
				blendStride = resource.Stride
			case "texcoord":
				texcoordStride = resource.Stride
			}
		}
		if positionStride == 0 {
			positionStride = group.Stride
		}
		if texcoordStride == 0 && isModelViewerPackedObjectStride(group.Stride) &&
			modelViewerPositionLooksPackedObject(group.VB, group.Stride) {
			return modelViewerPackedObjectLayout(indexFormat, group.Stride), nil
		}
		layout.Elements = append(
			layout.Elements,
			modelViewerFmtElement{
				SemanticName:      "POSITION",
				Format:            "DXGI_FORMAT_R32G32B32_FLOAT",
				AlignedByteOffset: 0,
				InputSlotClass:    "per-vertex",
			},
		)
		if positionStride >= 40 && detectModelViewerPositionFrame(group.VB, group.Stride) {
			layout.Elements = append(
				layout.Elements,
				modelViewerFmtElement{
					SemanticName:      "NORMAL",
					Format:            "DXGI_FORMAT_R32G32B32_FLOAT",
					AlignedByteOffset: 12,
					InputSlotClass:    "per-vertex",
				},
				modelViewerFmtElement{
					SemanticName:      "TANGENT",
					Format:            "DXGI_FORMAT_R32G32B32A32_FLOAT",
					AlignedByteOffset: 24,
					InputSlotClass:    "per-vertex",
				},
			)
		}
		if texcoordStride > 0 {
			baseOffset := positionStride + blendStride
			uvOffset, uvFormat := detectModelViewerUVBest(group.VB, group.Stride, baseOffset, texcoordStride)
			layout.Elements = append(
				layout.Elements,
				modelViewerFmtElement{
					SemanticName:      "TEXCOORD",
					Format:            uvFormat,
					AlignedByteOffset: uvOffset,
					InputSlotClass:    "per-vertex",
				},
			)
		}
	}
	if findModelViewerElement(layout, "POSITION", -1) == nil {
		return layout, fmt.Errorf("could not infer vertex layout for %s", group.Key)
	}
	return layout, nil
}

func buildModelViewerDirectMeshPayload(
	mesh modelViewerDirectMesh,
	textures []modelViewerTextureBinding,
	availableTextures map[string]modelViewerTexturePayload,
	shapeKeys []modelViewerShapeKey,
	cache *modelViewerBufferCache,
) (ModelViewerMeshTransport, modelViewerMeshPayload) {
	item := ModelViewerMeshTransport{
		ID:                  mesh.id,
		Component:           mesh.component,
		Conditions:          mesh.conditions,
		TextureVariants:     []ModelViewerTextureVariant{},
		NormalMapVariants:   []ModelViewerTextureVariant{},
		LightMapVariants:    []ModelViewerTextureVariant{},
		MaterialMapVariants: []ModelViewerTextureVariant{},
		ShapeTargets:        []ModelViewerShapeTarget{},
		PositionVariants:    []ModelViewerPositionVariant{},
	}
	payload := modelViewerMeshPayload{
		Positions:     mesh.geometry.Position,
		Normals:       mesh.geometry.Normal,
		Tangents:      mesh.geometry.Tangent,
		UVs:           mesh.geometry.Texcoord0,
		Indices:       mesh.geometry.Indices,
		SourceIndices: mesh.geometry.SourceIndices,
	}
	if len(mesh.positionAssignments) > 0 {
		for _, assignment := range mesh.positionAssignments {
			if assignment.sourcePath == "" || assignment.stride <= 0 || assignment.sourceBytes <= 0 ||
				len(assignment.conditions) == 0 {
				continue
			}
			item.PositionVariants = append(
				item.PositionVariants,
				ModelViewerPositionVariant{Conditions: assignment.conditions},
			)
			payload.PositionSources = append(payload.PositionSources, assignment)
		}
	}
	authoredRoles := make(map[string]bool)
	var firstDiffuseKey string
	skippedMissingDiffuse := false
	for _, assignment := range mesh.textureAssignments {
		key := modelViewerAssignmentTextureKey(assignment)
		if key == "" {
			continue
		}
		availableTexture, available := availableTextures[key]
		if !available {
			if fallback := modelViewerNormalizeKey(assignment.resource); fallback != "" {
				if availableTexture, available = availableTextures[fallback]; available {
					key = availableTexture.Key
				} else {
					if assignment.role == "diffuse" {
						skippedMissingDiffuse = true
					}
					continue
				}
			} else {
				if assignment.role == "diffuse" {
					skippedMissingDiffuse = true
				}
				continue
			}
		} else {
			key = availableTexture.Key
		}
		authoredRoles[assignment.role] = true
		if assignment.role == "diffuse" && firstDiffuseKey == "" {
			firstDiffuseKey = key
		}
		if modelViewerDNFIsTrue(assignment.conditions) {
			switch assignment.role {
			case "diffuse":
				item.TexKey = lo.ToPtr(key)
			case "normal_map":
				item.NormalMapKey = lo.ToPtr(key)
			case "light_map":
				item.LightMapKey = lo.ToPtr(key)
			case "material_map":
				item.MaterialMapKey = lo.ToPtr(key)
			}
			continue
		}
		variant := ModelViewerTextureVariant{Conditions: assignment.conditions, TexKey: key}
		switch assignment.role {
		case "diffuse":
			item.TextureVariants = append(item.TextureVariants, variant)
		case "normal_map":
			item.NormalMapVariants = append(item.NormalMapVariants, variant)
		case "light_map":
			item.LightMapVariants = append(item.LightMapVariants, variant)
		case "material_map":
			item.MaterialMapVariants = append(item.MaterialMapVariants, variant)
		}
	}
	if item.TexKey == nil && firstDiffuseKey != "" && !skippedMissingDiffuse {
		// Electron uses the mesh default file as texKey. If that file is missing,
		// remaining conditional variants stay variants only (load.test.ts).
		item.TexKey = lo.ToPtr(firstDiffuseKey)
	}
	for _, binding := range textures {
		if modelViewerNormalizeKey(binding.IBResourceName) != modelViewerNormalizeKey(mesh.ibName) {
			continue
		}
		if mesh.sectionName != "" && !strings.EqualFold(binding.SectionName, mesh.sectionName) {
			continue
		}
		candidates := append([]string(nil), binding.TextureResourceNames...)
		candidates = appendUniqueModelViewer(candidates, binding.DiffuseResourceName)
		sort.Strings(candidates)
		bestByRole := make(map[string]string)
		for _, name := range candidates {
			role := binding.TextureRoles[modelViewerNormalizeKey(name)]
			if role == "" {
				role = classifyModelViewerTextureRole(name)
			}
			existing := bestByRole[role]
			if existing == "" || modelViewerTextureNamePriority(name) > modelViewerTextureNamePriority(existing) {
				bestByRole[role] = name
			}
		}
		for role, name := range bestByRole {
			if authoredRoles[role] {
				continue
			}
			key := modelViewerNormalizeKey(name)
			availableTexture, available := availableTextures[key]
			if !available {
				continue
			}
			key = availableTexture.Key
			switch role {
			case "diffuse":
				item.TexKey = lo.ToPtr(key)
			case "normal_map":
				item.NormalMapKey = lo.ToPtr(key)
			case "light_map":
				item.LightMapKey = lo.ToPtr(key)
			case "material_map":
				item.MaterialMapKey = lo.ToPtr(key)
			}
		}
		break
	}
	for _, shapeKey := range shapeKeys {
		matched := false
		for _, prefix := range shapeKey.TargetMeshPrefixes {
			if modelViewerKeyMatches(prefix, mesh.component, false) {
				matched = true
				break
			}
		}
		if !matched && len(shapeKey.TargetMeshPrefixes) == 0 && shapeKey.BasePath != "" && mesh.positionFile != "" {
			positionPath := mesh.positionFile
			if !filepath.IsAbs(positionPath) {
				positionPath = filepath.Join(filepath.Dir(shapeKey.BasePath), filepath.FromSlash(positionPath))
			}
			matched = platform.SamePathFold(shapeKey.BasePath, positionPath)
		}
		if !matched {
			continue
		}
		for _, dimension := range shapeKey.Dimensions {
			var high []float32
			var highErr error
			if dimension.Sparse {
				high, highErr = readModelViewerSparseShapePositions(cache, dimension, mesh.geometry)
			} else {
				high, highErr = readModelViewerShapePositions(
					cache,
					dimension.BiggerPath,
					shapeKey.VertexStride,
					mesh.geometry.SourceIndices,
					mesh.geometry.VertexCount,
				)
			}
			low := append([]float32(nil), mesh.geometry.Position...)
			var lowErr error
			if dimension.SmallerPath != "" {
				low, lowErr = readModelViewerShapePositions(
					cache,
					dimension.SmallerPath,
					shapeKey.VertexStride,
					mesh.geometry.SourceIndices,
					mesh.geometry.VertexCount,
				)
			}
			if highErr != nil || lowErr != nil {
				continue
			}
			item.ShapeTargets = append(
				item.ShapeTargets,
				ModelViewerShapeTarget{Var: dimension.VariableID, Mode: dimension.Mode},
			)
			payload.ShapePositions = append(payload.ShapePositions, high)
			payload.ShapeLowPositions = append(payload.ShapeLowPositions, low)
		}
	}
	return item, payload
}

func modelViewerSourceIndicesAreIdentity(indices []uint32) bool {
	if len(indices) == 0 {
		return true
	}
	for index, source := range indices {
		if source != uint32(index) {
			return false
		}
	}
	return true
}

func readModelViewerShapePositions(
	cache *modelViewerBufferCache,
	path string,
	stride int,
	sources []uint32,
	vertexCount int,
) ([]float32, error) {
	raw, err := cache.read(path)
	if err != nil {
		return nil, err
	}
	if stride <= 0 {
		stride = 40
	}
	if sources == nil {
		sources = make([]uint32, vertexCount)
		for index := range sources {
			sources[index] = uint32(index)
		}
	}
	output := make([]float32, len(sources)*3)
	for index, source := range sources {
		offset := int(source) * stride
		if offset+12 > len(raw) {
			continue
		}
		output[index*3] = math.Float32frombits(binary.LittleEndian.Uint32(raw[offset:]))
		output[index*3+1] = math.Float32frombits(binary.LittleEndian.Uint32(raw[offset+4:]))
		output[index*3+2] = math.Float32frombits(binary.LittleEndian.Uint32(raw[offset+8:]))
	}
	return output, nil
}
