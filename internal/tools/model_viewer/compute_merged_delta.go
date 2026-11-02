package modelviewer

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	modelViewerMergedDeltaKind = "gimi_merged_delta_shape_v1"
	// Records of the served delta buffer: the vertex, then the position delta.
	modelViewerMergedDeltaRecordBytes = 16
	modelViewerMergedDeltaUnusedSlot  = 255
	modelViewerMergedDeltaFirstParam  = 100
	modelViewerMergedDeltaClipFPS     = 60
	// The playback lists step once per game frame.
	modelViewerMergedDeltaTicksPerSecond = 60
)

var (
	modelViewerMergedDeltaSlotsRE = regexp.MustCompile(`uintnum_slots=(\d+);`)
	// The variable group stays empty when the register takes any other value.
	modelViewerMergedDeltaBindRE   = regexp.MustCompile(`(?i)^x(\d+)\s*=\s*(?:\$([\w.]+)|.*)$`)
	modelViewerMergedDeltaRatioRE  = regexp.MustCompile(`^\$([\w.]+)\s*=\s*\$([\w.]+)\s*/\s*\$([\w.]+)$`)
	modelViewerMergedDeltaWeightRE = regexp.MustCompile(
		`^\$([\w.]+)\s*=\s*\$([\w.]+)\s*\*\s*(\d+(?:\.\d+)?)\s*-\s*(\d+(?:\.\d+)?)$`,
	)
	modelViewerMergedDeltaConstantRE = regexp.MustCompile(`^\$([\w.]+)\s*=\s*(\d+(?:\.\d+)?)$`)
	modelViewerMergedDeltaStepRE     = regexp.MustCompile(`(?i)^if\s*\(?\s*\$([\w.]+)\s*>\s*0\s*\)?$`)
	modelViewerMergedDeltaBelowRE    = regexp.MustCompile(`(?i)^if\s*\(.*\$[\w.]+\s*<\s*(\d+(?:\.\d+)?)\s*\)+$`)
)

// ModelViewerComputeShapeStep is how many frames a group steps per game frame at
// 100% speed while its weight is below Until.
type ModelViewerComputeShapeStep struct {
	Until float64 `json:"until"`
	Step  float64 `json:"step"`
}

// ModelViewerComputeShapeGroup is a set of shape keys one slider or play button
// drives. While it plays, its frame runs back and forth between 0 and FrameEnd,
// stepping at most StepLimit frames per game frame.
type ModelViewerComputeShapeGroup struct {
	AutoPlayVar string                        `json:"autoPlayVar"`
	SpeedVar    string                        `json:"speedVar"`
	WeightVar   string                        `json:"weightVar"`
	Steps       []ModelViewerComputeShapeStep `json:"steps"`
	StepLimit   float64                       `json:"stepLimit"`
	FrameEnd    float64                       `json:"frameEnd"`
	// The keys of the variables in the scanned sections, which the public IDs replace.
	autoPlayKey, speedKey, weightKey string
}

// ModelViewerComputeDeltaKey is one shape key. Its weight is the group weight times
// Scale minus Offset, clamped to 0..1; with a negative Group it stays at Weight.
type ModelViewerComputeDeltaKey struct {
	Group  int     `json:"group"`
	Scale  float64 `json:"scale"`
	Offset float64 `json:"offset"`
	Weight float64 `json:"weight,omitempty"`
}

type ModelViewerComputeMergedDeltas struct {
	URL    string                         `json:"url"`
	Groups []ModelViewerComputeShapeGroup `json:"groups"`
	Keys   []ModelViewerComputeDeltaKey   `json:"keys"`
	build  func(context.Context, func(uint32) (uint32, bool)) ([]byte, error)
}

type modelViewerMergedDeltaSegment struct {
	base, deltas, indices, freqs ModelViewerComputeBinarySource
	vertexCount, slots           int
	offset                       uint32
	// keys maps the shader's intensity index to the deformer's key.
	keys map[uint32]uint32
}

// detectModelViewerMergedDeltaAnimation recognizes shape keys baked into one
// merged position-delta buffer per mesh, with a lookup of (vertex, slot) to delta
// and to intensity parameter. Every merged buffer of the INI joins one deformer,
// since the same playback groups drive them all.
func detectModelViewerMergedDeltaAnimation(
	root, shaderBaseDir, scopeID string,
	sections []modINISection,
	reachable map[string]bool,
	resources map[string]modelViewerResource,
	defaults map[string]any,
	meshes []modelViewerDirectMesh,
) (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip) {
	groups, weights := collectModelViewerMergedDeltaGroups(sections, defaults)
	if len(groups) == 0 {
		return nil, nil
	}

	var segments []modelViewerMergedDeltaSegment
	var meshIDs []string
	offsets := make(map[string]uint32)
	keyIndex := make(map[string]uint32)
	var keys []ModelViewerComputeDeltaKey
	total := 0
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") ||
			!reachable[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		for _, pass := range collectModelViewerComputePasses(section) {
			shader, shaderOK := readModelViewerComputeShader(root, shaderBaseDir, pass.shader)
			slots, known := modelViewerMergedDeltaShaderSlots(shader)
			if !shaderOK || !known {
				continue
			}
			segment, ids, ok := resolveModelViewerMergedDeltaSegment(root, pass, slots, resources, meshes)
			if !ok || slices.ContainsFunc(segments, func(other modelViewerMergedDeltaSegment) bool {
				return other.base.sourcePath == segment.base.sourcePath
			}) {
				continue
			}

			segment.offset = uint32(total)
			segment.keys = make(map[uint32]uint32)
			for _, register := range slices.Sorted(maps.Keys(pass.deltaParams)) {
				variable := modelViewerNormalizeKey(pass.deltaParams[register])
				key, exists := keyIndex[variable]
				if !exists {
					key = uint32(len(keys))
					keyIndex[variable] = key
					keys = append(keys, modelViewerMergedDeltaKeyFor(variable, weights, defaults))
				}
				segment.keys[uint32(register-modelViewerMergedDeltaFirstParam)] = key
			}
			for _, id := range ids {
				offsets[id] = segment.offset
			}
			meshIDs = append(meshIDs, ids...)
			segments = append(segments, segment)
			total += segment.vertexCount
		}
	}
	if len(segments) == 0 {
		return nil, nil
	}
	slices.Sort(meshIDs)

	deformerID := modelViewerComputeDeformerID(scopeID, "merged-delta")
	keyCount := len(keys)
	deformer := &ModelViewerComputeDeformerTransport{
		Kind:              modelViewerMergedDeltaKind,
		ID:                deformerID,
		MeshIDs:           meshIDs,
		VertexCount:       total,
		meshSourceOffsets: offsets,
		Base: ModelViewerComputeBinarySource{
			ByteLength: int64(total) * 40,
			Stride:     40,
			sourcePath: deformerID + ":base",
			load: func(ctx context.Context) ([]byte, error) {
				output := make([]byte, 0, total*40)
				for _, segment := range segments {
					raw, err := readModelViewerEFMIFile(ctx, segment.base)
					if err != nil {
						return nil, err
					}
					output = append(output, raw...)
				}
				return output, nil
			},
		},
		MergedDeltas: &ModelViewerComputeMergedDeltas{
			Groups: groups,
			Keys:   keys,
			build: func(ctx context.Context, remap func(uint32) (uint32, bool)) ([]byte, error) {
				return buildModelViewerMergedDeltas(ctx, segments, keyCount, remap)
			},
		},
	}
	frameCount := min(
		max(int(math.Ceil(modelViewerMergedDeltaPeriod(groups, defaults)*modelViewerMergedDeltaClipFPS))+1, 2),
		maxModelViewerAnimationFrames,
	)
	return deformer, []modelViewerPreparedAnimationClip{
		buildModelViewerComputeFallbackClip(deformerID, "Shape Animation", frameCount, modelViewerMergedDeltaClipFPS),
	}
}

func modelViewerMergedDeltaShaderSlots(shader string) (int, bool) {
	compact := compactModelViewerShader(shader)
	for _, signature := range []string{
		"structvertexattributes{float3position;float3normal;float4tangent;}",
		"rwstructuredbuffer<vertexattributes>",
		"structuredbuffer<float3>merged_shapekey_pos_deltas:register(t51)",
		"structuredbuffer<int>merged_shapekey_indices:register(t52)",
		"structuredbuffer<uint>vertex_freq_indices:register(t53)",
		"iniparams[100+freq_idx_slot0].x",
		".position+=total_diff_position",
	} {
		if !strings.Contains(compact, signature) {
			return 0, false
		}
	}
	match := modelViewerMergedDeltaSlotsRE.FindStringSubmatch(compact)
	if match == nil {
		return 0, false
	}
	slots, err := strconv.Atoi(match[1])
	return slots, err == nil && slots > 0
}

func resolveModelViewerMergedDeltaSegment(
	root string,
	pass modelViewerComputePass,
	slots int,
	resources map[string]modelViewerResource,
	meshes []modelViewerDirectMesh,
) (modelViewerMergedDeltaSegment, []string, bool) {
	baseResource := resources[modelViewerNormalizeKey(pass.outputSource)]
	base, baseOK := modelViewerComputeSource(root, baseResource)
	deltas, deltasOK := modelViewerComputeSource(root, resources[modelViewerNormalizeKey(pass.t51)])
	indices, indicesOK := modelViewerComputeSource(root, resources[modelViewerNormalizeKey(pass.t52)])
	freqs, freqsOK := modelViewerComputeSource(root, resources[modelViewerNormalizeKey(pass.t53)])
	if !baseOK || !deltasOK || !indicesOK || !freqsOK ||
		base.Stride != 40 || deltas.Stride != 12 || indices.Stride != 4 || freqs.Stride != 4 {
		return modelViewerMergedDeltaSegment{}, nil, false
	}
	vertexCount := int(base.ByteLength / 40)
	slotBytes := int64(vertexCount) * int64(slots) * 4
	if vertexCount == 0 || indices.ByteLength != slotBytes || freqs.ByteLength != slotBytes {
		return modelViewerMergedDeltaSegment{}, nil, false
	}
	ids := modelViewerComputeMeshIDs(meshes, baseResource.Filename)
	if len(ids) == 0 {
		return modelViewerMergedDeltaSegment{}, nil, false
	}
	return modelViewerMergedDeltaSegment{
		base: base, deltas: deltas, indices: indices, freqs: freqs, vertexCount: vertexCount, slots: slots,
	}, ids, true
}

type modelViewerMergedDeltaWeight struct {
	group         int
	scale, offset float64
}

// collectModelViewerMergedDeltaGroups reads the playback lists the exporter writes
// to [Present]: weight = frame / frameEnd for each group, and each shape key mapped
// from its group's weight. The frame steps a limited number of times per game frame.
func collectModelViewerMergedDeltaGroups(
	sections []modINISection,
	defaults map[string]any,
) ([]ModelViewerComputeShapeGroup, map[string]modelViewerMergedDeltaWeight) {
	type ratio struct{ weight, frame, end string }
	var ratios []ratio
	type formula struct {
		key, source   string
		scale, offset float64
	}
	var formulas []formula
	steps := make(map[string][]ModelViewerComputeShapeStep)
	stepLimits := make(map[string]int)
	pendingUntil := -1.0
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Present") {
			continue
		}
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			until := pendingUntil
			pendingUntil = -1
			if match := modelViewerMergedDeltaRatioRE.FindStringSubmatch(line); match != nil {
				ratios = append(ratios, ratio{
					weight: modelViewerNormalizeKey(match[1]),
					frame:  modelViewerNormalizeKey(match[2]),
					end:    modelViewerNormalizeKey(match[3]),
				})
			} else if match := modelViewerMergedDeltaWeightRE.FindStringSubmatch(line); match != nil {
				scale, scaleErr := strconv.ParseFloat(match[3], 64)
				offset, offsetErr := strconv.ParseFloat(match[4], 64)
				if scaleErr == nil && offsetErr == nil {
					formulas = append(formulas, formula{
						key:    modelViewerNormalizeKey(match[1]),
						source: modelViewerNormalizeKey(match[2]),
						scale:  scale,
						offset: offset,
					})
				}
			} else if match := modelViewerMergedDeltaConstantRE.FindStringSubmatch(line); match != nil {
				if value, err := strconv.ParseFloat(match[2], 64); err == nil && until >= 0 {
					key := modelViewerNormalizeKey(match[1])
					steps[key] = append(steps[key], ModelViewerComputeShapeStep{Until: until, Step: value})
				}
			} else if match := modelViewerMergedDeltaStepRE.FindStringSubmatch(line); match != nil {
				stepLimits[modelViewerNormalizeKey(match[1])]++
			} else if match := modelViewerMergedDeltaBelowRE.FindStringSubmatch(line); match != nil {
				if value, err := strconv.ParseFloat(match[1], 64); err == nil {
					pendingUntil = value
				}
			}
		}
	}

	// The playback lists also compute a normalized frame nothing but themselves read.
	drivesKey := func(weight string) bool {
		return slices.ContainsFunc(formulas, func(item formula) bool { return item.source == weight })
	}
	var groups []ModelViewerComputeShapeGroup
	groupIndex := make(map[string]int)
	for _, item := range ratios {
		prefix, suffix, ok := strings.Cut(item.frame, "shapekeyframegroup")
		if _, seen := groupIndex[item.weight]; !ok || seen || !drivesKey(item.weight) {
			continue
		}
		name := func(stem string) string { return prefix + stem + suffix }
		frameEnd, endOK := resolveModelViewerNumericToken(item.end, defaults)
		_, autoPlayOK := resolveModelViewerNumericToken(name("autoplayenabledgroup"), defaults)
		_, speedOK := resolveModelViewerNumericToken(name("speedpercentgroup"), defaults)
		groupSteps := steps[name("basestepgroup")]
		stepLimit := stepLimits[name("stepaccumgroup")]
		if !endOK || !autoPlayOK || !speedOK || frameEnd <= 0 || len(groupSteps) == 0 || stepLimit == 0 {
			continue
		}
		groupIndex[item.weight] = len(groups)
		groups = append(groups, ModelViewerComputeShapeGroup{
			AutoPlayVar: name("autoplayenabledgroup"),
			SpeedVar:    name("speedpercentgroup"),
			WeightVar:   item.weight,
			Steps:       groupSteps,
			StepLimit:   float64(stepLimit),
			FrameEnd:    frameEnd,
			autoPlayKey: name("autoplayenabledgroup"),
			speedKey:    name("speedpercentgroup"),
			weightKey:   item.weight,
		})
	}

	weights := make(map[string]modelViewerMergedDeltaWeight)
	for _, item := range formulas {
		if group, ok := groupIndex[item.source]; ok {
			weights[item.key] = modelViewerMergedDeltaWeight{group: group, scale: item.scale, offset: item.offset}
		}
	}
	return groups, weights
}

func modelViewerMergedDeltaKeyFor(
	variable string,
	weights map[string]modelViewerMergedDeltaWeight,
	defaults map[string]any,
) ModelViewerComputeDeltaKey {
	if weight, ok := weights[variable]; ok {
		return ModelViewerComputeDeltaKey{Group: weight.group, Scale: weight.scale, Offset: weight.offset}
	}
	value, _ := resolveModelViewerNumericToken(variable, defaults)
	return ModelViewerComputeDeltaKey{Group: -1, Weight: value}
}

// modelViewerMergedDeltaPeriod returns the longest back-and-forth of the groups at
// their default speeds, which sets the length of the seek bar.
func modelViewerMergedDeltaPeriod(groups []ModelViewerComputeShapeGroup, defaults map[string]any) float64 {
	period := 1.0
	for _, group := range groups {
		speed, _ := resolveModelViewerNumericToken(group.speedKey, defaults)
		if speed <= 0 {
			continue
		}
		ticks, from := 0.0, 0.0
		for _, step := range group.Steps {
			if step.Step > 0 && step.Until > from {
				ticks += (step.Until - from) * group.FrameEnd / min(step.Step*speed/100, group.StepLimit)
				from = step.Until
			}
		}
		period = max(period, 2*ticks/modelViewerMergedDeltaTicksPerSecond)
	}
	return period
}

// buildModelViewerMergedDeltas lays the deltas out per key: a table of (first
// record, record count) for each key, then the records. remap drops the vertices
// no rendered mesh uses and gives the others their compact index.
func buildModelViewerMergedDeltas(
	ctx context.Context,
	segments []modelViewerMergedDeltaSegment,
	keyCount int,
	remap func(uint32) (uint32, bool),
) ([]byte, error) {
	records := make([][]byte, keyCount)
	for _, segment := range segments {
		freqs, err := readModelViewerEFMIFile(ctx, segment.freqs)
		if err != nil {
			return nil, err
		}
		indices, err := readModelViewerEFMIFile(ctx, segment.indices)
		if err != nil {
			return nil, err
		}
		deltas, err := readModelViewerEFMIFile(ctx, segment.deltas)
		if err != nil {
			return nil, err
		}

		for vertex := range segment.vertexCount {
			if vertex%65536 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			target, used := remap(segment.offset + uint32(vertex))
			if !used {
				continue
			}
			for slot := range segment.slots {
				position := (vertex*segment.slots + slot) * 4
				freq := binary.LittleEndian.Uint32(freqs[position:])
				index := int32(binary.LittleEndian.Uint32(indices[position:]))
				key, bound := segment.keys[freq]
				if freq == modelViewerMergedDeltaUnusedSlot || index == -1 || !bound {
					continue
				}
				start := int64(index) * 12
				if index < 0 || start+12 > int64(len(deltas)) {
					return nil, fmt.Errorf(
						"merged shape delta %d exceeds %d bytes: %s",
						index,
						len(deltas),
						segment.deltas.sourcePath,
					)
				}
				records[key] = binary.LittleEndian.AppendUint32(records[key], target)
				records[key] = append(records[key], deltas[start:start+12]...)
			}
		}
	}

	size := keyCount * 8
	for _, entries := range records {
		size += len(entries)
	}
	output := make([]byte, 0, size)
	first := 0
	for _, entries := range records {
		count := len(entries) / modelViewerMergedDeltaRecordBytes
		output = binary.LittleEndian.AppendUint32(output, uint32(first))
		output = binary.LittleEndian.AppendUint32(output, uint32(count))
		first += count
	}
	for _, entries := range records {
		output = append(output, entries...)
	}
	return output, nil
}
