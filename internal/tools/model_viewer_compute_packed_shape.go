package tools

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

const modelViewerPackedShapeKind = "gimi_cyclic_packed_shape_v1"

type modelViewerPackedShapeCandidate struct {
	section                  modINISection
	pass                     modelViewerComputePass
	equalities               []modelViewerStateEquality
	base                     modelViewerResource
	baseSource, targetSource ModelViewerComputeBinarySource
	phaseOffset              float64
	angularScale             float64
	amplitude                float64
	bias                     float64
	vertexCount              int
}

type modelViewerStateEquality struct {
	variable, value string
}

type modelViewerComputeBranchFrame struct {
	variable, value, parentVariable, parentValue string
}

func detectModelViewerPackedShapeAnimation(
	root, shaderBaseDir, scopeID string,
	sections []modINISection,
	reachable map[string]bool,
	resources map[string]modelViewerResource,
	defaults map[string]any,
	meshes []modelViewerDirectMesh,
) (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip) {
	candidates := collectModelViewerPackedShapeCandidates(root, shaderBaseDir, sections, reachable, resources)
	if len(candidates) == 0 {
		return nil, nil
	}
	selected, stateVariable, ok := selectModelViewerPackedShapeStages(candidates)
	if !ok {
		return nil, nil
	}
	meshIDs := modelViewerComputeMeshIDs(meshes, selected[0].base.Filename)
	if len(meshIDs) == 0 {
		return nil, nil
	}
	incomingStarts := collectModelViewerPackedShapeIncomingStarts(selected, stateVariable, defaults)
	stages := make([]ModelViewerComputeShapeStage, 0, len(selected))
	duration := 0.0
	for _, item := range selected {
		if item.vertexCount != selected[0].vertexCount {
			return nil, nil
		}
		phaseVariable, _, _ := parseModelViewerPhaseExpression(item.pass.x88)
		branchLines := item.section.Lines
		stateValue := ""
		if stateVariable != "" {
			value, hasValue := modelViewerComputeEqualityValue(item.equalities, stateVariable)
			if !hasValue {
				return nil, nil
			}
			stateValue = value
			branchLines = modelViewerComputeBranchLines(item.section, stateVariable, value)
		}
		rate, rateOK := findModelViewerAccumulatorRateInLines(branchLines, phaseVariable, defaults)
		if !rateOK || rate <= 0 {
			return nil, nil
		}
		stage := ModelViewerComputeShapeStage{
			Base:      item.baseSource,
			Target:    item.targetSource,
			PhaseRate: rate,
			WrapAt:    findModelViewerAccumulatorWrapInLines(branchLines, phaseVariable, defaults),
			PhaseStart: modelViewerPackedShapePhaseStart(
				incomingStarts,
				stateVariable,
				stateValue,
				phaseVariable,
				branchLines,
				defaults,
			),
			PhaseOffset:  item.phaseOffset,
			AngularScale: item.angularScale,
			Amplitude:    item.amplitude,
			Bias:         item.bias,
		}
		stage.Duration = modelViewerPackedShapeStageDuration(stage)
		stages = append(stages, stage)
		duration += stage.Duration
	}
	deformerID := modelViewerComputeDeformerID(scopeID, selected[0].base.Name)
	frameCount := min(max(int(math.Ceil(duration*30))+1, 2), maxModelViewerAnimationFrames)
	return &ModelViewerComputeDeformerTransport{
		Kind: modelViewerPackedShapeKind, ID: deformerID, MeshIDs: meshIDs, VertexCount: selected[0].vertexCount,
		Base: selected[0].baseSource, ShapeStages: stages,
	}, []modelViewerPreparedAnimationClip{buildModelViewerComputeFallbackClip(deformerID, "Shape Animation", frameCount, 30)}
}

func collectModelViewerPackedShapeCandidates(
	root, shaderBaseDir string,
	sections []modINISection,
	reachable map[string]bool,
	resources map[string]modelViewerResource,
) []modelViewerPackedShapeCandidate {
	var candidates []modelViewerPackedShapeCandidate
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") ||
			!reachable[modelViewerNormalizeKey(section.Header+section.Name)] {
			continue
		}
		passes := collectModelViewerComputePasses(section)
		equalitySets := collectModelViewerComputeDispatchEqualities(section)
		if len(passes) != len(equalitySets) {
			continue
		}
		for index, pass := range passes {
			if pass.t50 == "" || pass.t51 == "" || pass.t52 != "" || pass.x88 == "" || pass.outputName == "" ||
				pass.shader == "" {
				continue
			}
			shader, shaderOK := readModelViewerComputeShader(root, shaderBaseDir, pass.shader)
			angularScale, amplitude, bias, known := packedShapeShaderParameters(shader)
			_, phaseOffset, expressionOK := parseModelViewerPhaseExpression(pass.x88)
			base, baseOK := resources[modelViewerNormalizeKey(pass.t50)]
			target, targetOK := resources[modelViewerNormalizeKey(pass.t51)]
			baseSource, baseSourceOK := modelViewerComputeSource(root, base)
			targetSource, targetSourceOK := modelViewerComputeSource(root, target)
			if !shaderOK || !known || !expressionOK || !baseOK || !targetOK || !baseSourceOK || !targetSourceOK {
				continue
			}
			if baseSource.Stride != modelViewerPackedObjectStride ||
				targetSource.Stride != modelViewerPackedObjectStride ||
				baseSource.ByteLength != targetSource.ByteLength ||
				baseSource.ByteLength%int64(modelViewerPackedObjectStride) != 0 {
				continue
			}
			vertexCount := int(baseSource.ByteLength / int64(modelViewerPackedObjectStride))
			if vertexCount == 0 {
				continue
			}
			candidates = append(candidates, modelViewerPackedShapeCandidate{
				section: section, pass: pass, equalities: equalitySets[index], base: base,
				baseSource: baseSource, targetSource: targetSource, phaseOffset: phaseOffset,
				angularScale: angularScale, amplitude: amplitude, bias: bias, vertexCount: vertexCount,
			})
		}
	}
	return candidates
}

func selectModelViewerPackedShapeStages(
	candidates []modelViewerPackedShapeCandidate,
) ([]modelViewerPackedShapeCandidate, string, bool) {
	if len(candidates) == 1 {
		return candidates, "", true
	}
	sets := make([][]modelViewerStateEquality, len(candidates))
	for index, item := range candidates {
		sets[index] = item.equalities
	}
	stateVariable, sequenced := selectModelViewerComputeStateVariable(sets)
	if !sequenced {
		return nil, "", false
	}
	grouped := make(map[string]modelViewerPackedShapeCandidate, len(candidates))
	for _, item := range candidates {
		value, hasValue := modelViewerComputeEqualityValue(item.equalities, stateVariable)
		if !hasValue {
			return nil, "", false
		}
		if _, exists := grouped[value]; exists {
			return nil, "", false
		}
		grouped[value] = item
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sortModelViewerNumericStrings(keys)
	selected := make([]modelViewerPackedShapeCandidate, 0, len(keys))
	for _, key := range keys {
		selected = append(selected, grouped[key])
	}
	return selected, stateVariable, true
}

func collectModelViewerComputeDispatchEqualities(section modINISection) [][]modelViewerStateEquality {
	var stack []modelViewerComputeBranchFrame
	var output [][]modelViewerStateEquality
	for _, raw := range section.Lines {
		line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
		var handled bool
		stack, handled = applyModelViewerComputeBranch(stack, line)
		if handled {
			continue
		}
		match := modelViewerComputeBindingRE.FindStringSubmatch(line)
		if match == nil || !strings.EqualFold(match[1], "dispatch") {
			continue
		}
		output = append(output, modelViewerComputeEqualities(stack))
	}
	return output
}

func applyModelViewerComputeBranch(
	stack []modelViewerComputeBranchFrame,
	line string,
) ([]modelViewerComputeBranchFrame, bool) {
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
		return append(stack, modelViewerComputeBranchFrame{
			variable: variable, value: value, parentVariable: parentVariable, parentValue: parentValue,
		}), true
	case strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if "):
		variable, value := "", ""
		if match := modelViewerStateBranchRE.FindStringSubmatch(line); match != nil {
			variable, value = modelViewerNormalizeKey(match[1]), match[2]
		}
		if len(stack) == 0 {
			return append(stack, modelViewerComputeBranchFrame{variable: variable, value: value}), true
		}
		current := stack[len(stack)-1]
		current.variable, current.value = current.parentVariable, current.parentValue
		if variable != "" {
			current.variable, current.value = variable, value
		}
		stack[len(stack)-1] = current
		return stack, true
	case lower == "else":
		if len(stack) > 0 {
			current := stack[len(stack)-1]
			current.variable, current.value = current.parentVariable, current.parentValue
			stack[len(stack)-1] = current
		}
		return stack, true
	case lower == "endif":
		if len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
		return stack, true
	}
	return stack, false
}

func modelViewerComputeEqualities(stack []modelViewerComputeBranchFrame) []modelViewerStateEquality {
	var output []modelViewerStateEquality
	seen := map[string]bool{}
	for _, frame := range stack {
		if frame.variable == "" || seen[frame.variable] {
			continue
		}
		seen[frame.variable] = true
		output = append(output, modelViewerStateEquality{variable: frame.variable, value: frame.value})
	}
	return output
}

func modelViewerComputeEqualityValue(equalities []modelViewerStateEquality, variable string) (string, bool) {
	for _, equality := range equalities {
		if equality.variable == variable {
			return equality.value, true
		}
	}
	return "", false
}

func selectModelViewerComputeStateVariable(sets [][]modelViewerStateEquality) (string, bool) {
	values := map[string]map[string]bool{}
	for _, equalities := range sets {
		seen := map[string]bool{}
		for _, equality := range equalities {
			if seen[equality.variable] {
				continue
			}
			seen[equality.variable] = true
			if values[equality.variable] == nil {
				values[equality.variable] = map[string]bool{}
			}
			values[equality.variable][equality.value] = true
		}
	}
	bestVariable, bestCount, tied := "", 0, false
	for variable, set := range values {
		if len(set) < 2 {
			continue
		}
		switch {
		case len(set) > bestCount:
			bestVariable, bestCount, tied = variable, len(set), false
		case len(set) == bestCount:
			tied = true
		}
	}
	if bestCount < 2 || tied {
		return "", false
	}
	return bestVariable, true
}

func modelViewerComputeStackHas(stack []modelViewerComputeBranchFrame, variable, value string) bool {
	for _, frame := range stack {
		if frame.variable == variable && frame.value == value {
			return true
		}
	}
	return false
}

func modelViewerComputeBranchLines(section modINISection, variable, value string) []string {
	var stack []modelViewerComputeBranchFrame
	var lines []string
	for _, raw := range section.Lines {
		line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
		lower := strings.ToLower(line)
		isOpen := strings.HasPrefix(lower, "if ") || strings.HasPrefix(lower, "elif ") ||
			strings.HasPrefix(lower, "else if ")
		if isOpen && modelViewerComputeStackHas(stack, variable, value) {
			lines = append(lines, line)
		}
		var handled bool
		stack, handled = applyModelViewerComputeBranch(stack, line)
		if handled || isOpen {
			continue
		}
		if modelViewerComputeStackHas(stack, variable, value) {
			lines = append(lines, line)
		}
	}
	return lines
}

func modelViewerPackedShapeStageDuration(stage ModelViewerComputeShapeStage) float64 {
	if stage.WrapAt > stage.PhaseStart && stage.PhaseRate > 0 {
		return (stage.WrapAt - stage.PhaseStart) / stage.PhaseRate
	}
	if stage.AngularScale > 0 && stage.PhaseRate > 0 {
		return 2 * math.Pi / (stage.AngularScale * stage.PhaseRate)
	}
	return 1
}

func collectModelViewerPackedShapeIncomingStarts(
	selected []modelViewerPackedShapeCandidate,
	stateVariable string,
	defaults map[string]any,
) map[string]float64 {
	incoming := map[string]float64{}
	if stateVariable == "" {
		return incoming
	}
	for _, item := range selected {
		value, hasValue := modelViewerComputeEqualityValue(item.equalities, stateVariable)
		phaseVariable, _, expressionOK := parseModelViewerPhaseExpression(item.pass.x88)
		if !hasValue || !expressionOK {
			continue
		}
		lines := modelViewerComputeBranchLines(item.section, stateVariable, value)
		reset, hasReset := findModelViewerAccumulatorResetInLines(lines, phaseVariable, defaults)
		next, hasNext := findModelViewerNumericAssignmentInLines(lines, stateVariable)
		if !hasReset || !hasNext {
			continue
		}
		incoming[next] = reset
	}
	return incoming
}

func modelViewerPackedShapePhaseStart(
	incoming map[string]float64,
	stateVariable, stateValue, phaseVariable string,
	branchLines []string,
	defaults map[string]any,
) float64 {
	if start, ok := incoming[stateValue]; ok {
		return start
	}
	if stateVariable == "" {
		if reset, ok := findModelViewerAccumulatorResetInLines(branchLines, phaseVariable, defaults); ok {
			return reset
		}
	}
	if defaultStart, ok := resolveModelViewerNumericToken(phaseVariable, defaults); ok {
		return defaultStart
	}
	return 0
}

func findModelViewerNumericAssignmentInLines(lines []string, variable string) (string, bool) {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?i)^\$%s\s*=\s*(-?\d+(?:\.\d+)?)\s*$`, regexp.QuoteMeta(variable)))
	for _, raw := range lines {
		if match := pattern.FindStringSubmatch(strings.TrimSpace(raw)); match != nil {
			return match[1], true
		}
	}
	return "", false
}

func findModelViewerAccumulatorResetInLines(
	lines []string,
	variable string,
	defaults map[string]any,
) (float64, bool) {
	pattern := regexp.MustCompile(fmt.Sprintf(`(?i)^\$%s\s*=\s*(\$?[\w.-]+)\s*$`, regexp.QuoteMeta(variable)))
	for _, raw := range lines {
		if match := pattern.FindStringSubmatch(strings.TrimSpace(raw)); match != nil {
			return resolveModelViewerNumericToken(match[1], defaults)
		}
	}
	return 0, false
}

func isKnownModelViewerPackedShapeShader(shader string) bool {
	compact := compactModelViewerShader(shader)
	if strings.Contains(compact, "register(t52)") || strings.Contains(compact, "poseattributes") {
		return false
	}
	required := []string{
		"structvertexattributes{uint2position;uintnormal;uinttexcoord;uinttangent;}",
		"structuredbuffer<vertexattributes>", "register(t50)", "register(t51)",
		"f16tof32", "f32tof16",
	}
	for _, signature := range required {
		if !strings.Contains(compact, signature) {
			return false
		}
	}
	_, _, _, ok := parseModelViewerShapeWeight(compact)
	return ok
}

func packedShapeShaderParameters(shader string) (float64, float64, float64, bool) {
	if !isKnownModelViewerPackedShapeShader(shader) {
		return 0, 0, 0, false
	}
	return parseModelViewerShapeWeight(compactModelViewerShader(shader))
}
