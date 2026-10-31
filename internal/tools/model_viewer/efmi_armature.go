package modelviewer

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/platform"
)

// EFMI armature mods skin several components in draw-time compute passes that
// share one pose buffer: t110 holds float position plus a packed normal per
// vertex, t111 raw 16-bit weights and bone indices, t112 the pose frames, and u5
// the result the draw binds as vb0. All components must advance together, and a
// clip drives a single deformer, so their records merge into one vertex range.

const (
	modelViewerEFMIArmatureVariant  = "efmi_typed"
	modelViewerEFMIVertexStride     = 16
	modelViewerEFMIBlendStride      = 16
	modelViewerEFMIDecodedBlend     = 32
	modelViewerEFMIDecodedVertex    = 28
	modelViewerEFMIShapeConfigBytes = (2 + 33) * 16
	modelViewerEFMIShapeKeyLimit    = 127
)

var (
	modelViewerEFMIArmatureSignatures = []string{
		"structposeattributes{float3s;float3t;float4qr;float4qd;}",
		"rwbuffer<float4>rw_buffer:register(u5)",
		"buffer<float>base:register(t110)",
		"buffer<uint4>blend:register(t111)",
		"structuredbuffer<poseattributes>pose:register(t112)",
		"#definetimeiniparams[88].x", "#definevg_countiniparams[89].x",
		"pos=float4(in_position.x,in_position.z,-in_position.y,1.0f)",
		"norm=float3(norm.x,-norm.z,norm.y)",
		"idx_prev=frame*vg_count+indices", "idx_next=(frame+1)*vg_count+indices",
		"pos.xyz=pos.xyz*scale+bias", "sign(dot(p0_prev.qr,",
		"qr/=qr_len", "qd/=qr_len", "-qdw*qx+qdx*qw-qdy*qz+qdz*qy",
		"rw_buffer[i]=float4(pos_result.x,-pos_result.z,pos_result.y,",
		"norm2packed(float3(norm_result.x,norm_result.z,-norm_result.y)",
	}
	modelViewerPoolCopyRE = regexp.MustCompile(
		`(?i)^(pool\w+)\s*\[[^\]]*\]\s*=\s*(?:copy|ref)\s+(resource\w+)\s*$`,
	)
	modelViewerShapeValueRE = regexp.MustCompile(
		`(?i)^\(sin\(\$([\w.]+)(?:\+(\d+(?:\.\d+)?))?\)\+1\)/2$`,
	)
)

type modelViewerEFMIArmaturePass struct {
	shader, x88, x89       string
	base, blend, pose, out string
}

func collectModelViewerEFMIArmaturePass(section modINISection) (modelViewerEFMIArmaturePass, bool) {
	var pass modelViewerEFMIArmaturePass
	for _, raw := range section.Lines {
		left, right, ok := strings.Cut(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]), "=")
		if !ok {
			continue
		}
		right = strings.TrimSpace(right)
		if strings.EqualFold(right, "null") {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(left)) {
		case "cs":
			pass.shader = stripModelViewerQuotes(right)
		case "x88":
			pass.x88 = right
		case "x89":
			pass.x89 = right
		case "cs-t110":
			pass.base = right
		case "cs-t111":
			pass.blend = modelViewerResourceToken(right)
		case "cs-t112":
			pass.pose = modelViewerResourceToken(right)
		case "cs-u5":
			pass.out = modelViewerResourceToken(right)
		}
	}
	return pass, pass.base != "" && pass.blend != "" && pass.pose != "" && pass.out != ""
}

// modelViewerEFMIArmatureInput names the file-backed resource behind t110. A
// shape key pool holds one buffer per component, indexed at run time, so the
// output's own name selects the entry.
func modelViewerEFMIArmatureInput(value, output string, pools map[string][]string) string {
	if resource := modelViewerResourceToken(value); resource != "" {
		return resource
	}
	pool, _, indexed := strings.Cut(strings.TrimPrefix(strings.TrimSpace(value), "copy "), "[")
	if !indexed {
		return ""
	}
	outputKey := modelViewerNormalizeKey(output)
	selected := ""
	for _, candidate := range pools[modelViewerNormalizeKey(pool)] {
		if key := modelViewerNormalizeKey(candidate); strings.HasPrefix(outputKey, key) &&
			len(key) > len(modelViewerNormalizeKey(selected)) {
			selected = candidate
		}
	}
	return selected
}

func collectModelViewerPoolSources(sections []modINISection) map[string][]string {
	pools := make(map[string][]string)
	for _, section := range sections {
		for _, raw := range section.Lines {
			match := modelViewerPoolCopyRE.FindStringSubmatch(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]))
			if match == nil {
				continue
			}
			key := modelViewerNormalizeKey(match[1])
			pools[key] = appendUniqueModelViewer(pools[key], modelViewerTrimResourcePrefix(match[2]))
		}
	}
	return pools
}

func (a modelViewerResourceAliases) addEFMIArmatureInputs(sections []modINISection) {
	var pools map[string][]string
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") {
			continue
		}
		pass, ok := collectModelViewerEFMIArmaturePass(section)
		if !ok {
			continue
		}
		if pools == nil {
			pools = collectModelViewerPoolSources(sections)
		}
		a.add(a.computeInputs, pass.out, modelViewerEFMIArmatureInput(pass.base, pass.out, pools))
	}
}

func isKnownModelViewerEFMIArmatureShader(shader string) bool {
	return modelViewerShaderContainsAll(compactModelViewerShader(shader), modelViewerEFMIArmatureSignatures)
}

type modelViewerEFMISegment struct {
	base, blend ModelViewerComputeBinarySource
	vertexCount int
	offset      int
}

// modelViewerEFMISegmentMeshIDs prefers exact path matches because mesh matching
// falls back to the base name, which same-named buffers in other folders share.
func modelViewerEFMISegmentMeshIDs(
	meshes []modelViewerDirectMesh,
	filename string,
	claimed map[string]uint32,
) []string {
	exact := slices.DeleteFunc(slices.Clone(meshes), func(mesh modelViewerDirectMesh) bool {
		return !platform.SamePathFold(mesh.positionFile, filename)
	})
	if len(exact) > 0 {
		meshes = exact
	}
	return slices.DeleteFunc(modelViewerComputeMeshIDs(meshes, filename), func(id string) bool {
		_, owned := claimed[id]
		return owned
	})
}

func detectModelViewerEFMIArmature(
	root, shaderBaseDir, scopeID string,
	sections []modINISection,
	effective, raw map[string]modelViewerResource,
	defaults map[string]any,
	names map[string]modelViewerVariableName,
	meshes []modelViewerDirectMesh,
	warn func(string),
) (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip) {
	runTargets := collectModelViewerRunTargets(sections)
	var first modelViewerEFMIArmaturePass
	var poseSection modINISection
	var poseSource ModelViewerComputeBinarySource
	var segments []modelViewerEFMISegment
	var meshIDs []string
	offsets := make(map[string]uint32)
	total := 0
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") ||
			!runTargets[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		pass, ok := collectModelViewerEFMIArmaturePass(section)
		if !ok {
			continue
		}
		shader, shaderOK := readModelViewerComputeShader(root, shaderBaseDir, pass.shader)
		if !shaderOK || !isKnownModelViewerEFMIArmatureShader(shader) {
			continue
		}
		base := effective[modelViewerNormalizeKey(pass.out)]
		pose := raw[modelViewerNormalizeKey(pass.pose)]
		baseSource, baseOK := modelViewerComputeSource(root, base)
		blendSource, blendOK := modelViewerComputeSource(root, raw[modelViewerNormalizeKey(pass.blend)])
		currentPose, poseOK := modelViewerComputeSource(root, pose)
		if raw[modelViewerNormalizeKey(pass.out)].Filename != "" || !baseOK || !blendOK || !poseOK ||
			baseSource.Stride != modelViewerEFMIVertexStride || blendSource.Stride != modelViewerEFMIBlendStride ||
			blendSource.ByteLength != baseSource.ByteLength || currentPose.Stride != 56 {
			continue
		}
		if len(segments) == 0 {
			first, poseSection, poseSource = pass, section, currentPose
		} else if pass.x88 != first.x88 || pass.x89 != first.x89 || currentPose.sourcePath != poseSource.sourcePath {
			return nil, nil
		}
		ids := modelViewerEFMISegmentMeshIDs(meshes, base.Filename, offsets)
		if len(ids) == 0 || slices.ContainsFunc(segments, func(segment modelViewerEFMISegment) bool {
			return segment.base.sourcePath == baseSource.sourcePath
		}) {
			continue
		}
		vertexCount := int(baseSource.ByteLength / modelViewerEFMIVertexStride)
		for _, id := range ids {
			offsets[id] = uint32(total)
		}
		meshIDs = append(meshIDs, ids...)
		segments = append(segments, modelViewerEFMISegment{
			base: baseSource, blend: blendSource, vertexCount: vertexCount, offset: total,
		})
		total += vertexCount
	}
	if len(segments) == 0 {
		return nil, nil
	}
	boneCountValue, boneCountOK := resolveModelViewerNumericToken(first.x89, defaults)
	if !boneCountOK || boneCountValue != math.Trunc(boneCountValue) || boneCountValue <= 0 ||
		boneCountValue > float64(math.MaxInt/56) {
		return nil, nil
	}
	boneCount := int(boneCountValue)
	frameCount := int(poseSource.ByteLength / int64(boneCount*56))
	if frameCount < 2 || poseSource.ByteLength%int64(boneCount*56) != 0 {
		return nil, nil
	}
	slices.Sort(meshIDs)
	deformerID := modelViewerComputeDeformerID(scopeID, "efmi-armature")
	deformer := &ModelViewerComputeDeformerTransport{
		Kind:              modelViewerPackedDualQuaternionKind,
		ID:                deformerID,
		MeshIDs:           meshIDs,
		VertexCount:       total,
		meshSourceOffsets: offsets,
		Base: ModelViewerComputeBinarySource{
			ByteLength: int64(total) * modelViewerEFMIDecodedVertex,
			Stride:     modelViewerEFMIDecodedVertex,
			Encoding:   modelViewerPackedFloatEncoding,
			sourcePath: deformerID + ":base",
			load: func(ctx context.Context) ([]byte, error) {
				return readModelViewerEFMIBase(ctx, segments, total)
			},
		},
		Pose: &ModelViewerComputePoseSource{
			DualQuaternionVariant: modelViewerEFMIArmatureVariant,
			Blend: ModelViewerComputeBinarySource{
				ByteLength: int64(total) * modelViewerEFMIDecodedBlend,
				Stride:     modelViewerEFMIDecodedBlend,
				sourcePath: deformerID + ":blend",
				load: func(ctx context.Context) ([]byte, error) {
					return readModelViewerEFMIBlend(ctx, segments, total)
				},
			},
			Frames:     poseSource,
			BoneCount:  boneCount,
			FrameCount: frameCount,
		},
	}
	deformer.ShapePasses = detectModelViewerEFMIShapePasses(
		root, deformerID, sections, effective, raw, defaults, runTargets, segments, total,
	)
	clips, explicitRange := detectModelViewerGIMIShapePoseClips(
		sections, poseSection, first.x88, defaults, names, deformerID, frameCount,
		func(message string) {
			warn(fmt.Sprintf("shader=%q pose=%q %s",
				filepath.Join(shaderBaseDir, first.shader), poseSource.sourcePath, message))
		},
	)
	if len(clips) == 0 {
		if explicitRange || frameCount > maxModelViewerComputePoseFrames {
			warn(fmt.Sprintf("Skipped invalid compute pose range: shader=%q pose=%q frameCount=%d explicitRange=%t",
				filepath.Join(shaderBaseDir, first.shader), poseSource.sourcePath, frameCount, explicitRange))
			return nil, nil
		}
		fps, ok := modelViewerComputePoseFPS(sections, poseSection, first.x88, defaults)
		if !ok {
			fps = 30
		}
		clips = []modelViewerPreparedAnimationClip{
			buildModelViewerComputeFallbackClip(deformerID, "Pose Animation", frameCount, fps),
		}
	}
	return deformer, clips
}

func collectModelViewerRunTargets(sections []modINISection) map[string]bool {
	targets := make(map[string]bool)
	for _, section := range sections {
		for _, raw := range section.Lines {
			left, right, ok := strings.Cut(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]), "=")
			if ok && strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(left), "post ")), "run") {
				targets[modelViewerNormalizeKey(strings.TrimSpace(right))] = true
			}
		}
	}
	return targets
}

func readModelViewerEFMIFile(ctx context.Context, source ModelViewerComputeBinarySource) ([]byte, error) {
	release, err := diskio.Acquire(ctx, source.sourcePath)
	if err != nil {
		return nil, err
	}
	defer release()

	raw, err := os.ReadFile(source.sourcePath)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != source.ByteLength {
		return nil, fmt.Errorf("compute source size changed: expected %d, received %d", source.ByteLength, len(raw))
	}
	return raw, nil
}

func readModelViewerEFMIBase(ctx context.Context, segments []modelViewerEFMISegment, total int) ([]byte, error) {
	output := make([]byte, total*modelViewerEFMIDecodedVertex)
	for _, segment := range segments {
		raw, err := readModelViewerEFMIFile(ctx, segment.base)
		if err != nil {
			return nil, err
		}
		for vertex := range segment.vertexCount {
			record := raw[vertex*modelViewerEFMIVertexStride:]
			target := output[(segment.offset+vertex)*modelViewerEFMIDecodedVertex:]
			copy(target[:12], record[:12])
			binary.LittleEndian.PutUint32(target[12:], math.Float32bits(1))
			normal := decodeModelViewerEFMINormal(binary.LittleEndian.Uint32(record[12:]))
			for axis, value := range normal {
				binary.LittleEndian.PutUint32(target[16+axis*4:], math.Float32bits(value))
			}
		}
	}
	return output, ctx.Err()
}

// decodeModelViewerEFMINormal mirrors the shader's 10-bit octahedral decode.
func decodeModelViewerEFMINormal(packed uint32) [3]float32 {
	component := func(shift uint32) float64 {
		value := int(packed >> shift & 0x3ff)
		if value >= 512 {
			value -= 1024
		}
		return float64(value) / 511
	}
	sign := func(value float64) float64 {
		switch {
		case value > 0:
			return 1
		case value < 0:
			return -1
		}
		return 0
	}
	x, y := component(0), component(10)
	z := 1 - math.Abs(x) - math.Abs(y)
	if z < 0 {
		x, y = (1-math.Abs(y))*sign(x), (1-math.Abs(x))*sign(y)
	}
	length := math.Sqrt(x*x + y*y + z*z)
	if length == 0 {
		return [3]float32{}
	}
	return [3]float32{float32(x / length), float32(y / length), float32(z / length)}
}

func readModelViewerEFMIBlend(ctx context.Context, segments []modelViewerEFMISegment, total int) ([]byte, error) {
	output := make([]byte, total*modelViewerEFMIDecodedBlend)
	for _, segment := range segments {
		raw, err := readModelViewerEFMIFile(ctx, segment.blend)
		if err != nil {
			return nil, err
		}
		for vertex := range segment.vertexCount {
			record := raw[vertex*modelViewerEFMIBlendStride:]
			target := output[(segment.offset+vertex)*modelViewerEFMIDecodedBlend:]
			for influence := range 4 {
				weight := float32(binary.LittleEndian.Uint16(record[influence*2:])) / 65535
				binary.LittleEndian.PutUint32(target[influence*4:], math.Float32bits(weight))
				bone := binary.LittleEndian.Uint16(record[8+influence*2:])
				binary.LittleEndian.PutUint32(target[16+influence*4:], uint32(bone))
			}
		}
	}
	return output, ctx.Err()
}

type modelViewerEFMIShapeKey struct {
	segment         modelViewerEFMISegment
	ids, deltas     ModelViewerComputeBinarySource
	firstEntry, end int
}

type modelViewerEFMIShapeComponent struct {
	configs, ids, deltas, position string
}

// modelViewerLocalVariableKey names the assignment target of an INI line once
// the loader has scoped its variables; anything but a plain $variable is empty.
func modelViewerLocalVariableKey(target string) string {
	name, isVariable := strings.CutPrefix(target, "$")
	if !isVariable || strings.Contains(name, `\`) {
		return ""
	}
	return modelViewerNormalizeKey(unscopedModelViewerVariable(name))
}

func collectModelViewerEFMIShapeComponents(sections []modINISection) map[string]modelViewerEFMIShapeComponent {
	output := make(map[string]modelViewerEFMIShapeComponent)
	for _, section := range sections {
		component := ""
		for _, raw := range section.Lines {
			left, right, ok := strings.Cut(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]), "=")
			if !ok {
				continue
			}
			left, right = strings.TrimSpace(left), strings.TrimSpace(right)
			if modelViewerLocalVariableKey(left) == "componentid" {
				component = right
				continue
			}
			pool, _, indexed := strings.Cut(left, "[")
			resource := modelViewerResourceToken(right)
			if !indexed || component == "" || resource == "" {
				continue
			}
			current := output[component]
			switch modelViewerNormalizeKey(pool) {
			case "poolshapekeybatchconfigs":
				current.configs = resource
			case "poolshapekeyvertexids":
				current.ids = resource
			case "poolshapekeyvertexoffsets":
				current.deltas = resource
			case "poolvertexpositions":
				current.position = resource
			default:
				continue
			}
			output[component] = current
		}
	}
	return output
}

// detectModelViewerEFMIShapePasses turns the sine-driven shape keys of a run
// command list into shape passes over the merged records. A key that cannot be
// resolved leaves the pose animation intact without it.
func detectModelViewerEFMIShapePasses(
	root, deformerID string,
	sections []modINISection,
	effective, raw map[string]modelViewerResource,
	defaults map[string]any,
	runTargets map[string]bool,
	segments []modelViewerEFMISegment,
	total int,
) []ModelViewerComputeShapePass {
	components := collectModelViewerEFMIShapeComponents(sections)
	type passKey struct {
		variable string
		offset   float64
	}
	var order []passKey
	keys := make(map[passKey][]modelViewerEFMIShapeKey)
	for _, section := range sections {
		if !runTargets[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		component, shapeKey := "", -1
		for _, line := range section.Lines {
			left, right, ok := strings.Cut(strings.TrimSpace(strings.SplitN(line, ";", 2)[0]), "=")
			if !ok {
				continue
			}
			left, right = strings.TrimSpace(left), strings.TrimSpace(right)
			switch modelViewerLocalVariableKey(left) {
			case "componentid":
				component = right
			case "shapekeyid":
				if value, err := strconv.Atoi(right); err == nil {
					shapeKey = value
				}
			case "shapekeyvalue":
				match := modelViewerShapeValueRE.FindStringSubmatch(strings.ReplaceAll(right, " ", ""))
				if match == nil {
					continue
				}
				key, ok := resolveModelViewerEFMIShapeKey(
					root,
					components[component],
					shapeKey,
					effective,
					raw,
					segments,
				)
				if !ok {
					continue
				}
				offset := 0.0
				if match[2] != "" {
					offset, _ = strconv.ParseFloat(match[2], 64)
				}
				pass := passKey{variable: modelViewerNormalizeKey(match[1]), offset: offset}
				if len(keys[pass]) == 0 {
					order = append(order, pass)
				}
				keys[pass] = append(keys[pass], key)
			}
		}
	}
	output := make([]ModelViewerComputeShapePass, 0, len(order))
	for index, pass := range order {
		rate, ok := findModelViewerAccumulatorRate(sections, pass.variable, defaults)
		if !ok || rate <= 0 {
			continue
		}
		wrap, start := findModelViewerAccumulatorLoop(sections, pass.variable, defaults)
		passKeys := keys[pass]
		output = append(output, ModelViewerComputeShapePass{
			Target: ModelViewerComputeBinarySource{
				ByteLength: int64(total) * modelViewerEFMIDecodedVertex,
				Stride:     modelViewerEFMIDecodedVertex,
				Encoding:   modelViewerPackedFloatEncoding,
				sourcePath: fmt.Sprintf("%s:shape:%d", deformerID, index),
				load: func(ctx context.Context) ([]byte, error) {
					return readModelViewerEFMIShapeTarget(ctx, segments, passKeys, total)
				},
			},
			PhaseRate:    rate,
			WrapAt:       wrap,
			PhaseStart:   start,
			PhaseOffset:  pass.offset,
			AngularScale: 1,
			Amplitude:    0.5,
			Bias:         0.5,
		})
	}
	return output
}

func resolveModelViewerEFMIShapeKey(
	root string,
	component modelViewerEFMIShapeComponent,
	shapeKey int,
	effective, raw map[string]modelViewerResource,
	segments []modelViewerEFMISegment,
) (modelViewerEFMIShapeKey, bool) {
	if shapeKey < 0 || shapeKey >= modelViewerEFMIShapeKeyLimit {
		return modelViewerEFMIShapeKey{}, false
	}
	position, positionOK := modelViewerComputeSource(root, effective[modelViewerNormalizeKey(component.position)])
	configs, configsOK := modelViewerComputeSource(root, raw[modelViewerNormalizeKey(component.configs)])
	ids, idsOK := modelViewerComputeSource(root, raw[modelViewerNormalizeKey(component.ids)])
	deltas, deltasOK := modelViewerComputeSource(root, raw[modelViewerNormalizeKey(component.deltas)])
	if !positionOK || !configsOK || !idsOK || !deltasOK || ids.Stride != 4 || deltas.Stride != 2 ||
		configs.ByteLength != modelViewerEFMIShapeConfigBytes || deltas.ByteLength != ids.ByteLength/4*6 {
		return modelViewerEFMIShapeKey{}, false
	}
	index := slices.IndexFunc(segments, func(segment modelViewerEFMISegment) bool {
		return segment.base.sourcePath == position.sourcePath
	})
	config, err := os.ReadFile(configs.sourcePath)
	if index < 0 || err != nil || int64(len(config)) != configs.ByteLength {
		return modelViewerEFMIShapeKey{}, false
	}
	batchStart := int(binary.LittleEndian.Uint32(config[2*16:]))
	start := batchStart + int(binary.LittleEndian.Uint32(config[3*16+shapeKey*4:]))
	end := batchStart + int(binary.LittleEndian.Uint32(config[3*16+(shapeKey+1)*4:]))
	if start < 0 || end < start || int64(end) > ids.ByteLength/4 {
		return modelViewerEFMIShapeKey{}, false
	}
	return modelViewerEFMIShapeKey{
		segment: segments[index], ids: ids, deltas: deltas, firstEntry: start, end: end,
	}, true
}

func readModelViewerEFMIShapeTarget(
	ctx context.Context,
	segments []modelViewerEFMISegment,
	keys []modelViewerEFMIShapeKey,
	total int,
) ([]byte, error) {
	output, err := readModelViewerEFMIBase(ctx, segments, total)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		ids, err := readModelViewerEFMIFile(ctx, key.ids)
		if err != nil {
			return nil, err
		}
		deltas, err := readModelViewerEFMIFile(ctx, key.deltas)
		if err != nil {
			return nil, err
		}
		for entry := key.firstEntry; entry < key.end; entry++ {
			vertex := int(binary.LittleEndian.Uint32(ids[entry*4:]))
			if vertex >= key.segment.vertexCount {
				return nil, fmt.Errorf("shape key vertex %d exceeds %d vertices", vertex, key.segment.vertexCount)
			}
			target := output[(key.segment.offset+vertex)*modelViewerEFMIDecodedVertex:]
			for axis := range 3 {
				delta := modelViewerHalfToFloat(binary.LittleEndian.Uint16(deltas[(entry*3+axis)*2:]))
				value := math.Float32frombits(binary.LittleEndian.Uint32(target[axis*4:])) + delta
				binary.LittleEndian.PutUint32(target[axis*4:], math.Float32bits(value))
			}
		}
	}
	return output, ctx.Err()
}
