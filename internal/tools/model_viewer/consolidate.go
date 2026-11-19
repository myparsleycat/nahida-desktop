package modelviewer

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// modelViewerFrameGeometry is one frame of a mesh whose animation frames differ only in their positions.
type modelViewerFrameGeometry struct {
	Positions []float32
	Normals   []float32
	Tangents  []float32
}

// consolidateModelViewerMeshes removes meshes that repeat another one under a different TextureOverride hash, and
// folds the frames of a frame animation that differ only in their positions into one mesh with a position variant
// per frame. Mods that support several game versions repeat every draw once per index buffer hash, and frame
// animations bind one copy of the same index, texcoord and blend buffers per frame; drawn as separate meshes, both
// multiply the memory and load time of a clip.
//
// It returns the payloads aligned with transport.Meshes and, for every mesh it removed, the ID of the mesh that
// now draws it.
func consolidateModelViewerMeshes(
	ctx context.Context,
	transport *ModelViewerTransport,
	payloads []modelViewerMeshPayload,
	sources map[string]DrawSource,
) ([]modelViewerMeshPayload, map[string]string, error) {
	if len(payloads) != len(transport.Meshes) {
		return nil, nil, fmt.Errorf("model viewer payload mesh count mismatch")
	}
	deformed := make(map[string]bool)
	for _, deformer := range transport.ComputeDeformers {
		for _, id := range deformer.MeshIDs {
			deformed[id] = true
		}
	}
	eligible := func(index int) bool {
		mesh := transport.Meshes[index]
		_, sourced := sources[mesh.ID]
		return sourced && !deformed[mesh.ID] && len(mesh.ShapeTargets) == 0 && len(mesh.PositionVariants) == 0 &&
			len(payloads[index].PositionSources) == 0 && len(payloads[index].Frames) == 0
	}

	aliases := make(map[string]string)
	removed := make([]bool, len(transport.Meshes))
	dedupeModelViewerMeshes(transport, payloads, sources, eligible, removed, aliases)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	foldModelViewerFrameMeshes(transport, payloads, sources, eligible, removed, aliases)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(aliases) == 0 {
		return payloads, nil, nil
	}

	meshes := transport.Meshes[:0]
	kept := payloads[:0]
	for index, mesh := range transport.Meshes {
		if !removed[index] {
			meshes = append(meshes, mesh)
			kept = append(kept, payloads[index])
		}
	}
	clear(transport.Meshes[len(meshes):])
	clear(payloads[len(kept):])
	transport.Meshes = meshes
	// An alias of a removed mesh points at the mesh that finally draws it.
	for id, target := range aliases {
		for {
			next, exists := aliases[target]
			if !exists {
				break
			}
			target = next
		}
		aliases[id] = target
	}
	keptIndex := make(map[string]int, len(meshes))
	for index, mesh := range meshes {
		keptIndex[mesh.ID] = index
	}
	for id, target := range aliases {
		mesh := &transport.Meshes[keptIndex[target]]
		mesh.MergedIDs = append(mesh.MergedIDs, id)
	}
	for index := range transport.Meshes {
		slices.Sort(transport.Meshes[index].MergedIDs)
	}
	return kept, aliases, nil
}

// dedupeModelViewerMeshes removes each mesh that draws exactly what an earlier mesh draws, from another override
// section of the same draw. The component is left out of the key: it is named after the section, which a hash copy
// usually tells apart with a suffix such as Body1.
func dedupeModelViewerMeshes(
	transport *ModelViewerTransport,
	payloads []modelViewerMeshPayload,
	sources map[string]DrawSource,
	eligible func(int) bool,
	removed []bool,
	aliases map[string]string,
) {
	buckets := make(map[string][]int)
	for index := range transport.Meshes {
		if !eligible(index) {
			continue
		}
		mesh := transport.Meshes[index]
		source := sources[mesh.ID]
		key := strings.Join([]string{
			mesh.Form,
			fmt.Sprint(mesh.Conditions),
			modelViewerMeshTextureKey(mesh),
			strings.ToLower(source.INIPath),
			strings.ToLower(source.IBPath),
			strings.ToUpper(source.IBFormat),
			strings.ToLower(source.vertexBuffer),
			fmt.Sprint(source.StartIndex, source.IndexCount, source.BaseVertex, source.Auto, source.Reversed),
		}, "\x00")
		duplicate := false
		for _, earlier := range buckets[key] {
			if modelViewerPayloadsEqual(payloads[earlier], payloads[index]) &&
				slices.Equal(sources[transport.Meshes[earlier].ID].sourceFaces, source.sourceFaces) {
				removed[index] = true
				aliases[mesh.ID] = transport.Meshes[earlier].ID
				duplicate = true
				break
			}
		}
		if !duplicate {
			buckets[key] = append(buckets[key], index)
		}
	}
}

// foldModelViewerFrameMeshes folds the meshes of one draw that are frames of an animation clip and draw the same
// triangles with the same texture coordinates and colours into the first of them. The frames become its position
// variants, which carry their own positions, normals and tangents.
func foldModelViewerFrameMeshes(
	transport *ModelViewerTransport,
	payloads []modelViewerMeshPayload,
	sources map[string]DrawSource,
	eligible func(int) bool,
	removed []bool,
	aliases map[string]string,
) {
	animated := make(map[string]bool)
	for _, clip := range transport.Animations {
		if clip.DeformerID != "" {
			continue
		}
		for _, id := range clip.VariableIDs {
			animated[modelViewerNormalizeKey(id)] = true
		}
	}
	if len(animated) == 0 {
		return
	}

	groups := make(map[string][]int)
	var order []string
	for index := range transport.Meshes {
		if removed[index] || !eligible(index) {
			continue
		}
		mesh := transport.Meshes[index]
		source := sources[mesh.ID]
		if source.IBPath == "" {
			continue
		}
		key := strings.Join([]string{
			mesh.Form,
			strings.ToLower(mesh.Component),
			modelViewerOptionalKey(mesh.HighlightMapKey),
			modelViewerOptionalKey(mesh.RampMapKey),
			strings.ToLower(source.INIPath),
			strings.ToLower(source.Section),
			strings.ToLower(source.IBPath),
			strings.ToUpper(source.IBFormat),
			fmt.Sprint(source.StartIndex, source.IndexCount, source.BaseVertex, source.Auto, source.Reversed),
		}, "\x00")
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], index)
	}

	for _, key := range order {
		members := groups[key]
		if len(members) < 2 {
			continue
		}
		// Frames that dropped different invalid triangles draw different topology, so they fold apart.
		var partitions [][]int
		for _, index := range members {
			placed := false
			for p, partition := range partitions {
				first := partition[0]
				if modelViewerFrameTopologyEqual(payloads[first], payloads[index]) &&
					slices.Equal(
						sources[transport.Meshes[first].ID].sourceFaces,
						sources[transport.Meshes[index].ID].sourceFaces,
					) {
					partitions[p] = append(partition, index)
					placed = true
					break
				}
			}
			if !placed {
				partitions = append(partitions, []int{index})
			}
		}
		for _, partition := range partitions {
			if len(partition) > 1 && modelViewerFramesExclusive(transport.Meshes, partition, animated) {
				foldModelViewerFrames(transport, payloads, partition, removed, aliases)
			}
		}
	}
}

// modelViewerFramesExclusive reports whether the meshes are frames of an animation: no two of them can draw
// together, and their conditions differ only in animation variables, so an outfit toggle never folds.
func modelViewerFramesExclusive(meshes []ModelViewerMeshTransport, members []int, animated map[string]bool) bool {
	var residual string
	for i, index := range members {
		conditions := meshes[index].Conditions
		if len(conditions) == 0 || modelViewerDNFIsTrue(conditions) {
			return false
		}
		groups := make([]string, 0, len(conditions))
		for _, group := range conditions {
			var rest []string
			animatedClause := false
			for _, clause := range group {
				if animated[modelViewerNormalizeKey(clause.Var)] {
					animatedClause = true
					continue
				}
				rest = append(rest, fmt.Sprint(modelViewerNormalizeKey(clause.Var), clause.Value, clause.Negate))
			}
			if !animatedClause {
				return false
			}
			slices.Sort(rest)
			groups = append(groups, strings.Join(rest, "\x01"))
		}
		slices.Sort(groups)
		groups = slices.Compact(groups)
		joined := strings.Join(groups, "\x02")
		if i == 0 {
			residual = joined
		} else if joined != residual {
			return false
		}
		for _, earlier := range members[:i] {
			if modelViewerDNFIntersects(meshes[earlier].Conditions, conditions) {
				return false
			}
		}
	}
	return true
}

// foldModelViewerFrames makes the first member draw every member's frame through a position variant, with each
// frame's textures, and removes the others.
func foldModelViewerFrames(
	transport *ModelViewerTransport,
	payloads []modelViewerMeshPayload,
	members []int,
	removed []bool,
	aliases map[string]string,
) {
	target := &transport.Meshes[members[0]]
	sameTextures := true
	for _, index := range members[1:] {
		if modelViewerMeshTextureKey(transport.Meshes[index]) != modelViewerMeshTextureKey(*target) {
			sameTextures = false
			break
		}
	}

	conditions := modelViewerDNFFalse()
	variants := make([]ModelViewerPositionVariant, 0, len(members))
	frames := make([]modelViewerFrameGeometry, 0, len(members))
	var textures [5][]ModelViewerTextureVariant
	for _, index := range members {
		mesh := transport.Meshes[index]
		payload := payloads[index]
		conditions = modelViewerDNFOr(conditions, mesh.Conditions)
		variants = append(variants, ModelViewerPositionVariant{Conditions: cloneModelViewerDNF(mesh.Conditions)})
		frames = append(frames, modelViewerFrameGeometry{
			Positions: payload.Positions, Normals: payload.Normals, Tangents: payload.Tangents,
		})
		if sameTextures {
			continue
		}
		for role, slot := range modelViewerMeshTextureSlots(&mesh) {
			fallback := ""
			if *slot.key != nil {
				fallback = **slot.key
			}
			textures[role] = append(textures[role], ModelViewerTextureVariant{
				Conditions: cloneModelViewerDNF(mesh.Conditions), TexKey: fallback,
			})
			for _, variant := range *slot.variants {
				textures[role] = append(textures[role], ModelViewerTextureVariant{
					Conditions: modelViewerDNFAnd(mesh.Conditions, variant.Conditions), TexKey: variant.TexKey,
				})
			}
		}
		if index != members[0] {
			removed[index] = true
			aliases[mesh.ID] = target.ID
		}
	}
	if sameTextures {
		for _, index := range members[1:] {
			removed[index] = true
			aliases[transport.Meshes[index].ID] = target.ID
		}
	} else {
		for role, slot := range modelViewerMeshTextureSlots(target) {
			*slot.variants = textures[role]
		}
		// Choices are not folded with the frames' conditions, so frames whose textures differ offer none.
		target.TextureChoices = nil
	}
	// A variant must match for the mesh to draw, so the frames' own conditions still decide which one shows.
	target.Conditions = conditions
	target.PositionVariants = variants
	payloads[members[0]].Frames = frames
}

type modelViewerTextureSlot struct {
	key      **string
	variants *[]ModelViewerTextureVariant
}

func modelViewerMeshTextureSlots(mesh *ModelViewerMeshTransport) [5]modelViewerTextureSlot {
	return [5]modelViewerTextureSlot{
		{&mesh.TexKey, &mesh.TextureVariants},
		{&mesh.NormalMapKey, &mesh.NormalMapVariants},
		{&mesh.LightMapKey, &mesh.LightMapVariants},
		{&mesh.MaterialMapKey, &mesh.MaterialMapVariants},
		{&mesh.CutoutMapKey, &mesh.CutoutMapVariants},
	}
}

func modelViewerMeshTextureKey(mesh ModelViewerMeshTransport) string {
	var key strings.Builder
	for _, slot := range modelViewerMeshTextureSlots(&mesh) {
		key.WriteString(modelViewerOptionalKey(*slot.key))
		fmt.Fprint(&key, *slot.variants)
		key.WriteByte(0)
	}
	key.WriteString(modelViewerOptionalKey(mesh.HighlightMapKey))
	key.WriteString(modelViewerOptionalKey(mesh.RampMapKey))
	for _, choice := range mesh.TextureChoices {
		key.WriteString(choice.Source)
		for _, slot := range []*string{choice.TexKey, choice.NormalMapKey, choice.LightMapKey, choice.MaterialMapKey} {
			key.WriteString(modelViewerOptionalKey(slot))
		}
		fmt.Fprint(&key, choice.TextureVariants, choice.NormalMapVariants, choice.LightMapVariants,
			choice.MaterialMapVariants)
		key.WriteByte(0)
	}
	return key.String()
}

func modelViewerOptionalKey(value *string) string {
	if value == nil {
		return "\x00nil"
	}
	return *value
}

// modelViewerFrameTopologyEqual reports whether two frames draw the same triangles over the same texture
// coordinates and colours, so they can share every buffer but positions, normals and tangents.
func modelViewerFrameTopologyEqual(left, right modelViewerMeshPayload) bool {
	return len(left.Positions) == len(right.Positions) &&
		len(left.Normals) == len(right.Normals) &&
		len(left.Tangents) == len(right.Tangents) &&
		slices.Equal(left.Indices, right.Indices) &&
		slices.Equal(left.SourceIndices, right.SourceIndices) &&
		slices.Equal(left.UVs, right.UVs) &&
		slices.Equal(left.UVs1, right.UVs1) &&
		slices.Equal(left.Colors, right.Colors)
}

func modelViewerPayloadsEqual(left, right modelViewerMeshPayload) bool {
	return modelViewerFrameTopologyEqual(left, right) &&
		slices.Equal(left.Positions, right.Positions) &&
		slices.Equal(left.Normals, right.Normals) &&
		slices.Equal(left.Tangents, right.Tangents)
}
