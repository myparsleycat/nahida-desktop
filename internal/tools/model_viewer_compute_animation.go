package tools

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	modelViewerGIMIShapePoseKind     = "gimi_shape_pose_v1"
	maxModelViewerComputeShaderBytes = 1 << 20
)

var (
	modelViewerComputeBindingRE  = regexp.MustCompile(`(?i)^(?:post\s+)?(cs-[tu]\d+|cs|x8[89]|dispatch|resource[\w.]+)\s*=\s*(.+)$`)
	modelViewerPhaseExpressionRE = regexp.MustCompile(`(?i)^\$([\w.]+)(?:\s*([+-])\s*([\d.]+))?$`)
	modelViewerStateBranchRE     = regexp.MustCompile(`(?i)^(?:if|elif|else\s+if)\s+\$([\w.]+)\s*==\s*(-?\d+(?:\.\d+)?)\s*$`)
	modelViewerShaderCommentRE   = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\r\n]*`)
	modelViewerShapeWeightRE     = regexp.MustCompile(`([+-]?\d+(?:\.\d+)?)\*\(sin\(freq\*([+-]?\d+(?:\.\d+)?)\)\+([+-]?\d+(?:\.\d+)?)\)`)
)

type modelViewerComputePass struct {
	x88, x89           string
	t50, t51, t52      string
	shader, outputName string
}

func detectModelViewerComputeAnimation(root, shaderBaseDir, scopeID string, sections []modINISection, effectiveResources []modelViewerResource, meshes []modelViewerDirectMesh, names map[string]modelViewerVariableName) (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip) {
	rawResources := modelViewerResourceMap(collectModelViewerResources(sections))
	effective := modelViewerResourceMap(effectiveResources)
	defaults := collectModelViewerDefaultVariables(sections)
	reachable := collectModelViewerReachableComputeSections(sections)
	shapeOnly := func() (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip) {
		return detectModelViewerShapeOnlyAnimation(root, shaderBaseDir, scopeID, sections, reachable, effective, defaults, meshes)
	}
	posePass, poseSection, ok := detectModelViewerKnownBonePass(root, shaderBaseDir, sections, reachable)
	if !ok {
		return shapeOnly()
	}
	base, baseOK := effective[modelViewerNormalizeKey(posePass.t50)]
	blend, blendOK := rawResources[modelViewerNormalizeKey(posePass.t51)]
	pose, poseOK := rawResources[modelViewerNormalizeKey(posePass.t52)]
	outputRaw, outputRawOK := rawResources[modelViewerNormalizeKey(posePass.outputName)]
	outputEffective, outputEffectiveOK := effective[modelViewerNormalizeKey(posePass.outputName)]
	boneCountValue, boneCountOK := resolveModelViewerNumericToken(posePass.x89, defaults)
	if !boneCountOK || math.IsNaN(boneCountValue) || math.IsInf(boneCountValue, 0) || boneCountValue != math.Trunc(boneCountValue) || boneCountValue <= 0 || boneCountValue > float64(math.MaxInt/56) {
		return shapeOnly()
	}
	boneCount := int(boneCountValue)
	if !baseOK || !blendOK || !poseOK || !outputRawOK || !outputEffectiveOK || outputRaw.Filename != "" || !samePathFold(outputEffective.Filename, base.Filename) || base.Stride != 40 || blend.Stride != 32 || pose.Stride != 56 {
		return shapeOnly()
	}
	baseSource, baseOK := modelViewerComputeSource(root, base)
	blendSource, blendOK := modelViewerComputeSource(root, blend)
	poseSource, poseOK := modelViewerComputeSource(root, pose)
	if !baseOK || !blendOK || !poseOK || baseSource.ByteLength%40 != 0 {
		return shapeOnly()
	}
	vertexCount := int(baseSource.ByteLength / 40)
	if vertexCount == 0 || blendSource.ByteLength != int64(vertexCount*32) || poseSource.ByteLength%int64(boneCount*56) != 0 {
		return shapeOnly()
	}
	frameCount := int(poseSource.ByteLength / int64(boneCount*56))
	meshIDs := modelViewerComputeMeshIDs(meshes, base.Filename)
	if len(meshIDs) == 0 || frameCount < 2 {
		return shapeOnly()
	}
	deformerID := modelViewerComputeDeformerID(scopeID, firstModelViewerString(posePass.outputName, base.Name))
	deformer := &ModelViewerComputeDeformerTransport{
		Kind:        modelViewerGIMIShapePoseKind,
		ID:          deformerID,
		MeshIDs:     meshIDs,
		VertexCount: vertexCount,
		Base:        baseSource,
		Pose: &ModelViewerComputePoseSource{
			Blend: blendSource, Frames: poseSource, BoneCount: boneCount, FrameCount: frameCount,
		},
	}
	deformer.ShapePasses = detectModelViewerKnownShapePasses(root, shaderBaseDir, sections, reachable, effective, defaults, base, vertexCount)
	clips := detectModelViewerGIMIShapePoseClips(sections, poseSection, posePass.x88, defaults, names, deformerID, frameCount)
	if len(clips) == 0 {
		clips = []modelViewerPreparedAnimationClip{buildModelViewerComputeFallbackClip(deformerID, "Pose Animation", frameCount, 30)}
	}
	return deformer, clips
}

func detectModelViewerShapeOnlyAnimation(root, shaderBaseDir, scopeID string, sections []modINISection, reachable map[string]bool, resources map[string]modelViewerResource, defaults map[string]any, meshes []modelViewerDirectMesh) (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip) {
	base, source, vertexCount, ok := findModelViewerKnownShapeBase(root, shaderBaseDir, sections, reachable, resources)
	if !ok {
		return nil, nil
	}
	meshIDs := modelViewerComputeMeshIDs(meshes, base.Filename)
	if len(meshIDs) == 0 {
		return nil, nil
	}
	deformerID := modelViewerComputeDeformerID(scopeID, base.Name)
	passes := detectModelViewerKnownShapePasses(root, shaderBaseDir, sections, reachable, resources, defaults, base, vertexCount)
	if len(passes) == 0 {
		return nil, nil
	}
	deformer := &ModelViewerComputeDeformerTransport{Kind: modelViewerGIMIShapePoseKind, ID: deformerID, MeshIDs: meshIDs, VertexCount: vertexCount, Base: source, ShapePasses: passes}
	duration := 1.0
	if passes[0].WrapAt > 0 && passes[0].PhaseRate > 0 {
		duration = passes[0].WrapAt / passes[0].PhaseRate
	} else if passes[0].AngularScale > 0 && passes[0].PhaseRate > 0 {
		duration = 2 * math.Pi / (passes[0].AngularScale * passes[0].PhaseRate)
	}
	frameCount := min(max(int(math.Ceil(duration*30))+1, 2), maxModelViewerAnimationFrames)
	return deformer, []modelViewerPreparedAnimationClip{buildModelViewerComputeFallbackClip(deformerID, "Shape Animation", frameCount, 30)}
}

func findModelViewerKnownShapeBase(root, shaderBaseDir string, sections []modINISection, reachable map[string]bool, resources map[string]modelViewerResource) (modelViewerResource, ModelViewerComputeBinarySource, int, bool) {
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") || !reachable[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		for _, pass := range collectModelViewerComputePasses(section) {
			shader, shaderOK := readModelViewerComputeShader(root, shaderBaseDir, pass.shader)
			if _, _, _, known := knownModelViewerShapeShaderParameters(shader); !shaderOK || !known {
				continue
			}
			base, exists := resources[modelViewerNormalizeKey(pass.t50)]
			source, sourceOK := modelViewerComputeSource(root, base)
			if exists && sourceOK && source.Stride == 40 && source.ByteLength%40 == 0 {
				return base, source, int(source.ByteLength / 40), true
			}
		}
	}
	return modelViewerResource{}, ModelViewerComputeBinarySource{}, 0, false
}

func buildModelViewerComputeFallbackClip(deformerID, label string, frameCount int, fps float64) modelViewerPreparedAnimationClip {
	clip := modelViewerPreparedAnimationClip{ID: deformerID + ":default", Label: label, DeformerID: deformerID, FPS: fps, FrameStart: 0, FrameEnd: frameCount - 1, Loop: true}
	for frame := range frameCount {
		clip.Frames = append(clip.Frames, modelViewerPreparedAnimationFrame{Index: frame, Time: float64(frame) / fps, Values: map[string]any{}})
	}
	return clip
}

func modelViewerComputeDeformerID(scopeID, resourceName string) string {
	resourceKey := modelViewerNormalizeKey(resourceName)
	if resourceKey == "" {
		resourceKey = "position"
	}
	if scopeKey := modelViewerNormalizeKey(scopeID); scopeKey != "" {
		return "gimi-compute:" + scopeKey + ":" + resourceKey
	}
	return "gimi-compute:" + resourceKey
}

func modelViewerResourceMap(resources []modelViewerResource) map[string]modelViewerResource {
	output := make(map[string]modelViewerResource, len(resources))
	for _, resource := range resources {
		output[modelViewerNormalizeKey(resource.Name)] = resource
	}
	return output
}

func modelViewerComputeSource(folder string, resource modelViewerResource) (ModelViewerComputeBinarySource, bool) {
	if resource.Filename == "" || resource.Stride <= 0 {
		return ModelViewerComputeBinarySource{}, false
	}
	path, err := resolveModelViewerResourcePath(folder, folder, resource.Filename)
	if err != nil || !modelViewerPathWithin(folder, path) {
		return ModelViewerComputeBinarySource{}, false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size()%int64(resource.Stride) != 0 {
		return ModelViewerComputeBinarySource{}, false
	}
	return ModelViewerComputeBinarySource{ByteLength: info.Size(), Stride: resource.Stride, sourcePath: path}, true
}

func modelViewerComputeMeshIDs(meshes []modelViewerDirectMesh, filename string) []string {
	var output []string
	for _, mesh := range meshes {
		if mesh.geometry != nil && (samePathFold(mesh.positionFile, filename) || strings.EqualFold(filepath.Base(mesh.positionFile), filepath.Base(filename))) {
			output = append(output, mesh.id)
		}
	}
	sort.Strings(output)
	return output
}

func detectModelViewerKnownBonePass(root, shaderBaseDir string, sections []modINISection, reachable map[string]bool) (modelViewerComputePass, modINISection, bool) {
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") || !reachable[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		for _, pass := range collectModelViewerComputePasses(section) {
			if pass.t50 == "" || pass.t51 == "" || pass.t52 == "" || pass.x88 == "" || pass.x89 == "" || pass.outputName == "" {
				continue
			}
			shader, ok := readModelViewerComputeShader(root, shaderBaseDir, pass.shader)
			if ok && isKnownModelViewerGIMIShapePoseBoneShader(shader) {
				return pass, section, true
			}
		}
	}
	return modelViewerComputePass{}, modINISection{}, false
}

func detectModelViewerKnownShapePasses(root, shaderBaseDir string, sections []modINISection, reachable map[string]bool, resources map[string]modelViewerResource, defaults map[string]any, base modelViewerResource, vertexCount int) []ModelViewerComputeShapePass {
	var output []ModelViewerComputeShapePass
	phaseVariable := ""
	hasLinkedOutput := false
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") || !reachable[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		for _, pass := range collectModelViewerComputePasses(section) {
			if pass.t50 == "" || pass.t51 == "" || pass.x88 == "" || pass.t52 != "" {
				continue
			}
			shader, shaderOK := readModelViewerComputeShader(root, shaderBaseDir, pass.shader)
			angularScale, amplitude, bias, known := knownModelViewerShapeShaderParameters(shader)
			if !shaderOK || !known {
				continue
			}
			passBase, baseOK := resources[modelViewerNormalizeKey(pass.t50)]
			target, targetOK := resources[modelViewerNormalizeKey(pass.t51)]
			variable, offset, expressionOK := parseModelViewerPhaseExpression(pass.x88)
			if !baseOK || !targetOK || !expressionOK || passBase.Stride != base.Stride || !samePathFold(passBase.Filename, base.Filename) {
				continue
			}
			source, sourceOK := modelViewerComputeSource(root, target)
			if !sourceOK || source.Stride != base.Stride || source.ByteLength != int64(vertexCount*base.Stride) {
				continue
			}
			if phaseVariable == "" {
				phaseVariable = variable
			} else if phaseVariable != variable {
				return nil
			}
			output = append(output, ModelViewerComputeShapePass{Target: source, PhaseOffset: offset, AngularScale: angularScale, Amplitude: amplitude, Bias: bias})
			if outputResource, exists := resources[modelViewerNormalizeKey(pass.outputName)]; exists && outputResource.Filename != "" && samePathFold(outputResource.Filename, base.Filename) {
				hasLinkedOutput = true
			}
		}
	}
	if len(output) == 0 || !hasLinkedOutput {
		return nil
	}
	rate, rateOK := findModelViewerAccumulatorRate(sections, phaseVariable, defaults)
	if !rateOK {
		return nil
	}
	wrap := findModelViewerAccumulatorWrap(sections, phaseVariable, defaults)
	for index := range output {
		output[index].PhaseRate = rate
		output[index].WrapAt = wrap
	}
	return output
}

func collectModelViewerReachableComputeSections(sections []modINISection) map[string]bool {
	lookup := make(map[string]modINISection)
	for _, section := range sections {
		if strings.EqualFold(section.Header, "CommandList") || strings.EqualFold(section.Header, "CustomShader") {
			lookup[modelViewerNormalizeKey(section.Header+section.Name)] = section
		}
	}
	queue := make([]modINISection, 0)
	for _, section := range sections {
		if strings.EqualFold(section.Header, "Present") || strings.EqualFold(section.Header, "Constants") {
			queue = append(queue, section)
		}
	}
	reachable := make(map[string]bool)
	expansions := 0
	for len(queue) > 0 && expansions < maxModelViewerDirectRunExpansions {
		section := queue[0]
		queue = queue[1:]
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			left, right, ok := strings.Cut(line, "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(left), "post ")), "run") {
				continue
			}
			key := modelViewerNormalizeKey(strings.TrimSpace(right))
			nested, exists := lookup[key]
			if !exists || reachable[key] {
				continue
			}
			reachable[key] = true
			queue = append(queue, nested)
			expansions++
		}
	}
	return reachable
}

func collectModelViewerComputePasses(section modINISection) []modelViewerComputePass {
	state := modelViewerComputePass{}
	uav := make(map[string]string)
	var output []modelViewerComputePass
	for _, raw := range section.Lines {
		line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
		match := modelViewerComputeBindingRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		key, value := strings.ToLower(match[1]), strings.TrimSpace(match[2])
		switch key {
		case "x88":
			state.x88 = value
		case "x89":
			state.x89 = value
		case "cs-t50":
			state.t50 = modelViewerResourceToken(value)
		case "cs-t51":
			state.t51 = modelViewerResourceToken(value)
		case "cs-t52":
			state.t52 = modelViewerResourceToken(value)
		case "cs":
			state.shader = stripModelViewerQuotes(value)
		case "dispatch":
			output = append(output, state)
		default:
			if modelViewerUAVSlotRE.MatchString(key) {
				if strings.EqualFold(value, "null") {
					delete(uav, key)
				} else {
					uav[key] = modelViewerResourceToken(value)
				}
				continue
			}
			if target := modelViewerResourceToken(key); target != "" {
				if slot := modelViewerUAVReference(value); slot != "" && uav[slot] != "" {
					state.outputName = target
				}
			}
		}
	}
	return output
}

func readModelViewerComputeShader(root, baseDir, relative string) (string, bool) {
	path, err := resolveModelViewerResourcePath(root, baseDir, relative)
	if err != nil || !modelViewerPathWithin(root, path) {
		return "", false
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxModelViewerComputeShaderBytes+1))
	if err != nil || len(raw) > maxModelViewerComputeShaderBytes {
		return "", false
	}
	return string(raw), true
}

func compactModelViewerShader(shader string) string {
	shader = modelViewerShaderCommentRE.ReplaceAllString(shader, "")
	return strings.Map(func(char rune) rune {
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' {
			return -1
		}
		return char
	}, strings.ToLower(shader))
}

func isKnownModelViewerGIMIShapePoseBoneShader(shader string) bool {
	compact := compactModelViewerShader(shader)
	required := []string{
		"structvertexattributes{float3position;float3normal;float4tangent;}",
		"structposeattributes{float3s;float3t;float4qr;float4qd;}",
		"structuredbuffer<vertexattributes>", "register(t50)",
		"structuredbuffer<blendattributes>", "register(t51)",
		"structuredbuffer<poseattributes>", "register(t52)",
		"frame*vg_count", "(frame+1)*vg_count", "p0_prev.s*weights.x", "p0_prev.t*weights.x",
		"pos.xyz=pos.xyz*scale+bias", "sign(dot(p0_prev.qr,", "p0_prev.qd*weights.x",
		"qr/=qr_len", "qd/=qr_len", "-qdw*qx+qdx*qw-qdy*qz+qdz*qy",
		"m00*pos.x+m01*pos.y+m02*pos.z", "m00*normal.x+m01*normal.y+m02*normal.z",
		".position=float3(pos_result.x,pos_result.y,pos_result.z)",
		".normal=normalize(float3(normal_result.x,normal_result.y,normal_result.z))",
	}
	for _, signature := range required {
		if !strings.Contains(compact, signature) {
			return false
		}
	}
	return strings.Contains(compact, "int4indicies") || strings.Contains(compact, "int4indices")
}

func knownModelViewerShapeShaderParameters(shader string) (float64, float64, float64, bool) {
	compact := compactModelViewerShader(shader)
	required := []string{
		"structvertexattributes{float3position;float3normal;float4tangent;}",
		"structuredbuffer<vertexattributes>", "register(t50)", "register(t51)",
		"shapekey[i].position-base[i].position", "shapekey[i].normal-base[i].normal",
		"shapekey[i].tangent-base[i].tangent", ".position+=diff.position*",
		".normal+=diff.normal*", ".tangent+=diff.tangent*",
	}
	for _, signature := range required {
		if !strings.Contains(compact, signature) {
			return 0, 0, 0, false
		}
	}
	match := modelViewerShapeWeightRE.FindStringSubmatch(compact)
	if match == nil {
		return 0, 0, 0, false
	}
	amplitude, amplitudeErr := strconv.ParseFloat(match[1], 64)
	scale, scaleErr := strconv.ParseFloat(match[2], 64)
	innerBias, biasErr := strconv.ParseFloat(match[3], 64)
	if amplitudeErr != nil || scaleErr != nil || biasErr != nil || amplitude == 0 || scale <= 0 {
		return 0, 0, 0, false
	}
	return scale, amplitude, amplitude * innerBias, true
}

func parseModelViewerPhaseExpression(expression string) (string, float64, bool) {
	match := modelViewerPhaseExpressionRE.FindStringSubmatch(strings.TrimSpace(expression))
	if match == nil {
		return "", 0, false
	}
	offset := 0.0
	if match[3] != "" {
		parsed, err := strconv.ParseFloat(match[3], 64)
		if err != nil {
			return "", 0, false
		}
		if match[2] == "-" {
			parsed = -parsed
		}
		offset = parsed
	}
	return modelViewerNormalizeKey(match[1]), offset, true
}

func findModelViewerAccumulatorRate(sections []modINISection, variable string, defaults map[string]any) (float64, bool) {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?i)^\$%s\s*=\s*\$%s\s*\+\s*(\$?[\w.-]+)\s*\*\s*\$[\w.]*dt[\w.]*\s*$`, regexp.QuoteMeta(variable), regexp.QuoteMeta(variable)))
	for _, section := range sections {
		for _, raw := range section.Lines {
			if match := pattern.FindStringSubmatch(strings.TrimSpace(raw)); match != nil {
				return resolveModelViewerNumericToken(match[1], defaults)
			}
		}
	}
	return 0, false
}

func findModelViewerAccumulatorWrap(sections []modINISection, variable string, defaults map[string]any) float64 {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?i)^if\s+\$%s\s*>\s*(\$?[\w.-]+)\s*$`, regexp.QuoteMeta(variable)))
	for _, section := range sections {
		for _, raw := range section.Lines {
			if match := pattern.FindStringSubmatch(strings.TrimSpace(raw)); match != nil {
				value, _ := resolveModelViewerNumericToken(match[1], defaults)
				return value
			}
		}
	}
	return 0
}

func detectModelViewerGIMIShapePoseClips(sections []modINISection, poseSection modINISection, frameExpression string, defaults map[string]any, names map[string]modelViewerVariableName, deformerID string, frameCount int) []modelViewerPreparedAnimationClip {
	frameVariable, _, ok := parseModelViewerPhaseExpression(frameExpression)
	if !ok {
		return nil
	}
	fps, ok := findModelViewerAccumulatorRate([]modINISection{poseSection}, frameVariable, defaults)
	if !ok || fps <= 0 {
		return nil
	}
	endVariable := ""
	startVariable := ""
	endPattern := regexp.MustCompile(fmt.Sprintf(`(?i)^if\s+\$%s\s*>\s*\$([\w.]+)\s*$`, regexp.QuoteMeta(frameVariable)))
	resetPattern := regexp.MustCompile(fmt.Sprintf(`(?i)^\$%s\s*=\s*\$([\w.]+)\s*$`, regexp.QuoteMeta(frameVariable)))
	for _, raw := range poseSection.Lines {
		line := strings.TrimSpace(raw)
		if match := endPattern.FindStringSubmatch(line); match != nil {
			endVariable = modelViewerNormalizeKey(match[1])
		}
		if match := resetPattern.FindStringSubmatch(line); match != nil {
			candidate := modelViewerNormalizeKey(match[1])
			if candidate != frameVariable {
				startVariable = candidate
			}
		}
	}
	if startVariable == "" || endVariable == "" {
		return nil
	}
	ranges := collectModelViewerStateRanges(sections, startVariable, endVariable)
	var clips []modelViewerPreparedAnimationClip
	for _, value := range sortedModelViewerStateRangeKeys(ranges) {
		rangeValue := ranges[value]
		if rangeValue.start < 0 || rangeValue.end < rangeValue.start || rangeValue.end >= frameCount || rangeValue.end-rangeValue.start+1 > maxModelViewerAnimationFrames {
			continue
		}
		stateVariable := rangeValue.variable
		labelName := stateVariable
		if name, exists := names[modelViewerNormalizeKey(stateVariable)]; exists {
			labelName = name.Label
		}
		clip := modelViewerPreparedAnimationClip{
			ID: deformerID + ":" + value, Label: humanizeModelViewerLabel(labelName) + " " + value, DeformerID: deformerID,
			VariableIDs: []string{stateVariable}, FPS: fps, FrameStart: rangeValue.start, FrameEnd: rangeValue.end, Loop: true,
		}
		stateValue, _ := strconv.ParseFloat(value, 64)
		for frame := rangeValue.start; frame <= rangeValue.end; frame++ {
			clip.Frames = append(clip.Frames, modelViewerPreparedAnimationFrame{Index: frame, Time: float64(frame-rangeValue.start) / fps, Values: map[string]any{stateVariable: stateValue}})
		}
		clips = append(clips, clip)
	}
	return clips
}

type modelViewerStateRange struct {
	variable   string
	start, end int
	hasStart   bool
	hasEnd     bool
}

func collectModelViewerStateRanges(sections []modINISection, startVariable, endVariable string) map[string]modelViewerStateRange {
	output := make(map[string]modelViewerStateRange)
	assignmentRE := regexp.MustCompile(`(?i)^\$([\w.]+)\s*=\s*(-?\d+(?:\.\d+)?)\s*$`)
	type branch struct{ parentVariable, parentValue, variable, value string }
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Present") {
			continue
		}
		var stack []branch
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				parentVariable, parentValue := "", ""
				if len(stack) > 0 {
					parentVariable, parentValue = stack[len(stack)-1].variable, stack[len(stack)-1].value
				}
				variable, value := parentVariable, parentValue
				if match := modelViewerStateBranchRE.FindStringSubmatch(line); match != nil {
					variable, value = modelViewerNormalizeKey(match[1]), match[2]
				}
				stack = append(stack, branch{parentVariable: parentVariable, parentValue: parentValue, variable: variable, value: value})
				continue
			case strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if "):
				if len(stack) == 0 {
					continue
				}
				current := &stack[len(stack)-1]
				current.variable, current.value = current.parentVariable, current.parentValue
				if match := modelViewerStateBranchRE.FindStringSubmatch(line); match != nil {
					current.variable, current.value = modelViewerNormalizeKey(match[1]), match[2]
				}
				continue
			case lower == "endif":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}
			if len(stack) == 0 || stack[len(stack)-1].variable == "" {
				continue
			}
			match := assignmentRE.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			assigned := modelViewerNormalizeKey(match[1])
			number, err := strconv.ParseFloat(match[2], 64)
			if err != nil || math.Trunc(number) != number {
				continue
			}
			current := output[stack[len(stack)-1].value]
			current.variable = stack[len(stack)-1].variable
			switch assigned {
			case startVariable:
				current.start, current.hasStart = int(number), true
			case endVariable:
				current.end, current.hasEnd = int(number), true
			default:
				continue
			}
			output[stack[len(stack)-1].value] = current
		}
	}
	for key, value := range output {
		if !value.hasStart || !value.hasEnd {
			delete(output, key)
		}
	}
	return output
}

func sortedModelViewerStateRangeKeys(ranges map[string]modelViewerStateRange) []string {
	keys := make([]string, 0, len(ranges))
	for key := range ranges {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, leftErr := strconv.ParseFloat(keys[i], 64)
		right, rightErr := strconv.ParseFloat(keys[j], 64)
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return keys[i] < keys[j]
	})
	return keys
}
