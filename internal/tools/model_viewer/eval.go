package modelviewer

import (
	"math"
	"strconv"
	"strings"
)

type viewerEvalMesh struct {
	ID                   string
	Visible              bool
	TexKey               string
	NormalMapKey         string
	LightMapKey          string
	MaterialMapKey       string
	CutoutMapKey         string
	PositionVariantIndex *int
}

type viewerEvalState struct {
	State  map[string]any
	Meshes []viewerEvalMesh
}

func viewerEvalLookup(state map[string]any, variable string) (any, bool) {
	if value, ok := state[variable]; ok {
		return value, true
	}
	lowered := strings.ToLower(variable)
	for key, value := range state {
		if strings.ToLower(key) == lowered {
			return value, true
		}
	}
	return nil, false
}

// viewerEvalDNFSatisfied treats a nil DNF like an empty one, as the DNF
// builders do: modelViewerDNFAnd returns nil when every alternative contradicts.
func viewerEvalDNFSatisfied(dnf ModelViewerDNF, state map[string]any) bool {
	if len(dnf) == 0 {
		return false
	}
	for _, group := range dnf {
		matched := true
		for _, clause := range group {
			current, ok := viewerEvalLookup(state, clause.Var)
			if !ok {
				continue
			}
			equal := modelViewerString(current) == clause.Value
			if equal == clause.Negate {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func viewerEvalApplyStateRules(state map[string]any, rules []ModelViewerStateRule) map[string]any {
	next := cloneViewerEvalState(state)
	for _, rule := range rules {
		if viewerEvalDNFSatisfied(rule.Conditions, next) {
			next[rule.Var] = rule.Value
		}
	}
	return next
}

func viewerEvalResolveTexture(variants []ModelViewerTextureVariant, fallback *string, state map[string]any) string {
	for index := len(variants) - 1; index >= 0; index-- {
		if viewerEvalDNFSatisfied(variants[index].Conditions, state) {
			return variants[index].TexKey
		}
	}
	if fallback == nil {
		return ""
	}
	return *fallback
}

func viewerEvalResolvePositionIndex(variants []ModelViewerPositionVariant, state map[string]any) *int {
	if len(variants) == 0 {
		return nil
	}
	for index := len(variants) - 1; index >= 0; index-- {
		if viewerEvalDNFSatisfied(variants[index].Conditions, state) {
			value := index
			return &value
		}
	}
	return nil
}

func viewerEvalMenuGuardHolds(when *ModelViewerMenuGuard, state map[string]any) bool {
	if when == nil {
		return true
	}
	current, ok := viewerEvalLookup(state, when.Var)
	if !ok {
		return false
	}
	if when.Op == "==" {
		return modelViewerString(current) == when.Value
	}
	if when.Op == "!=" {
		return modelViewerString(current) != when.Value
	}
	left, leftOK := anyToFloat(current)
	right, rightOK := parseViewerEvalNumber(when.Value)
	if !leftOK || !rightOK || math.IsNaN(left) || math.IsInf(left, 0) || math.IsNaN(right) || math.IsInf(right, 0) {
		return false
	}
	switch when.Op {
	case ">":
		return left > right
	case "<":
		return left < right
	case ">=":
		return left >= right
	case "<=":
		return left <= right
	}
	return false
}

func applyViewerVariableSelection(state map[string]any, variable ModelViewerVariable, value any) map[string]any {
	next := cloneViewerEvalState(state)
	next[variable.ID] = value
	for _, effect := range variable.Effects {
		holds := viewerEvalMenuGuardHolds(effect.When, next)
		for index := 0; holds && index < len(effect.Also); index++ {
			holds = viewerEvalMenuGuardHolds(&effect.Also[index], next)
		}
		if holds {
			next[effect.Var] = effect.Value
		}
	}
	return next
}

func evaluateViewerTransport(payload ModelViewerTransport, state map[string]any) viewerEvalState {
	return evaluateViewerTransportInForm(payload, state, "")
}

// evaluateViewerTransportInForm hides the meshes outside form the way the
// renderer's evaluateViewerState does; an empty form selects every mesh.
func evaluateViewerTransportInForm(payload ModelViewerTransport, state map[string]any, form string) viewerEvalState {
	resolved := viewerEvalApplyStateRules(mergeViewerEvalState(payload.DefaultState, state), payload.StateRules)
	evaluated := viewerEvalState{State: resolved, Meshes: make([]viewerEvalMesh, len(payload.Meshes))}
	for index, mesh := range payload.Meshes {
		positionIndex := viewerEvalResolvePositionIndex(mesh.PositionVariants, resolved)
		visible := viewerFormVisible(mesh.Form, form) && viewerEvalDNFSatisfied(mesh.Conditions, resolved) &&
			(len(mesh.PositionVariants) == 0 || positionIndex != nil)
		evaluated.Meshes[index] = viewerEvalMesh{
			ID:                   mesh.ID,
			Visible:              visible,
			TexKey:               viewerEvalResolveTexture(mesh.TextureVariants, mesh.TexKey, resolved),
			NormalMapKey:         viewerEvalResolveTexture(mesh.NormalMapVariants, mesh.NormalMapKey, resolved),
			LightMapKey:          viewerEvalResolveTexture(mesh.LightMapVariants, mesh.LightMapKey, resolved),
			MaterialMapKey:       viewerEvalResolveTexture(mesh.MaterialMapVariants, mesh.MaterialMapKey, resolved),
			CutoutMapKey:         viewerEvalResolveTexture(mesh.CutoutMapVariants, mesh.CutoutMapKey, resolved),
			PositionVariantIndex: positionIndex,
		}
	}
	return evaluated
}

// ViewerMeshState is how one mesh of a transport draws in a viewer state.
type ViewerMeshState struct {
	Visible        bool
	TexKey         string
	NormalMapKey   string
	LightMapKey    string
	MaterialMapKey string
	CutoutMapKey   string
	// PositionVariant indexes the mesh's PositionVariants, or is -1 for its base positions.
	PositionVariant int
	// ShapeWeights holds a weight per entry of the mesh's ShapeTargets.
	ShapeWeights []float64
}

// EvaluateViewerState resolves state over the transport's defaults and state rules, and returns the
// resolved state with the state of each mesh in Meshes order. An empty form selects every mesh.
func EvaluateViewerState(
	payload ModelViewerTransport,
	state map[string]any,
	form string,
) (map[string]any, []ViewerMeshState) {
	evaluated := evaluateViewerTransportInForm(payload, state, form)
	meshes := make([]ViewerMeshState, len(evaluated.Meshes))
	for index, mesh := range evaluated.Meshes {
		variant := -1
		if mesh.PositionVariantIndex != nil {
			variant = *mesh.PositionVariantIndex
		}
		meshes[index] = ViewerMeshState{
			Visible:         mesh.Visible,
			TexKey:          mesh.TexKey,
			NormalMapKey:    mesh.NormalMapKey,
			LightMapKey:     mesh.LightMapKey,
			MaterialMapKey:  mesh.MaterialMapKey,
			CutoutMapKey:    mesh.CutoutMapKey,
			PositionVariant: variant,
			ShapeWeights:    viewerShapeWeights(payload.Meshes[index].ShapeTargets, evaluated.State),
		}
	}
	return evaluated.State, meshes
}

func viewerShapeWeights(targets []ModelViewerShapeTarget, state map[string]any) []float64 {
	if len(targets) == 0 {
		return nil
	}
	weights := make([]float64, len(targets))
	for index, target := range targets {
		current, _ := viewerEvalLookup(state, target.Var)
		value, ok := anyToFloat(current)
		if !ok {
			continue
		}
		switch {
		case target.Wave != nil && viewerEvalDNFSatisfied(target.Wave.When, state):
			weights[index] = target.Wave.Amplitude*math.Sin(value*target.Wave.AngularScale) + target.Wave.Bias
		case target.Wave != nil:
		case target.Divisor != 0:
			weights[index] = value / target.Divisor
		default:
			weights[index] = value
		}
	}
	return weights
}

// ApplyViewerVariableSelection returns state with variable set to value and the menu effects that the
// selection triggers applied.
func ApplyViewerVariableSelection(state map[string]any, variable ModelViewerVariable, value any) map[string]any {
	return applyViewerVariableSelection(state, variable, value)
}

func viewerFormVisible(meshForm, selected string) bool {
	return selected == "" || meshForm == "" || meshForm == selected || strings.HasPrefix(selected, meshForm+"::")
}

type viewerBlockingVar struct {
	ID    string
	Label string
	Value any
}

func viewerEvalStatesDiffer(left, right viewerEvalState) bool {
	if len(left.Meshes) != len(right.Meshes) {
		return true
	}
	for index := range left.Meshes {
		if left.Meshes[index].Visible != right.Meshes[index].Visible ||
			left.Meshes[index].TexKey != right.Meshes[index].TexKey ||
			left.Meshes[index].NormalMapKey != right.Meshes[index].NormalMapKey ||
			left.Meshes[index].LightMapKey != right.Meshes[index].LightMapKey ||
			left.Meshes[index].MaterialMapKey != right.Meshes[index].MaterialMapKey ||
			left.Meshes[index].CutoutMapKey != right.Meshes[index].CutoutMapKey ||
			viewerEvalIndexValue(
				left.Meshes[index].PositionVariantIndex,
			) != viewerEvalIndexValue(
				right.Meshes[index].PositionVariantIndex,
			) {
			return true
		}
	}
	return false
}

func viewerEvalIndexValue(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func buildViewerBlockingVars(payload ModelViewerTransport, testedVar string, state map[string]any) []viewerBlockingVar {
	testedLower := strings.ToLower(testedVar)
	coOccurring := map[string]bool{}
	addDNF := func(dnf ModelViewerDNF) {
		for _, group := range dnf {
			hasTested := false
			for _, clause := range group {
				if strings.ToLower(clause.Var) == testedLower {
					hasTested = true
					break
				}
			}
			if !hasTested {
				continue
			}
			for _, clause := range group {
				if strings.ToLower(clause.Var) != testedLower {
					coOccurring[clause.Var] = true
				}
			}
		}
	}
	for _, mesh := range payload.Meshes {
		addDNF(mesh.Conditions)
		for _, variant := range mesh.PositionVariants {
			addDNF(variant.Conditions)
		}
		for _, variants := range [][]ModelViewerTextureVariant{
			mesh.TextureVariants,
			mesh.NormalMapVariants,
			mesh.LightMapVariants,
			mesh.MaterialMapVariants,
			mesh.CutoutMapVariants,
		} {
			for _, variant := range variants {
				addDNF(variant.Conditions)
			}
		}
	}
	coOccurringLower := map[string]bool{}
	for name := range coOccurring {
		coOccurringLower[strings.ToLower(name)] = true
	}
	var blocking []viewerBlockingVar
	for _, variable := range payload.Variables {
		if strings.ToLower(variable.ID) == testedLower {
			continue
		}
		directly := coOccurringLower[strings.ToLower(variable.ID)]
		effect := false
		for _, item := range variable.Effects {
			if coOccurringLower[strings.ToLower(item.Var)] {
				effect = true
				break
			}
		}
		if !directly && !effect {
			continue
		}
		current, ok := viewerEvalLookup(state, variable.ID)
		if !ok {
			continue
		}
		blocking = append(blocking, viewerBlockingVar{ID: variable.ID, Label: variable.Label, Value: current})
	}
	return blocking
}

func cloneViewerEvalState(state map[string]any) map[string]any {
	next := make(map[string]any, len(state))
	for key, value := range state {
		next[key] = value
	}
	return next
}

func mergeViewerEvalState(base, overlay map[string]any) map[string]any {
	next := cloneViewerEvalState(base)
	for key, value := range overlay {
		next[key] = value
	}
	return next
}

func anyToFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case string:
		return parseViewerEvalNumber(typed)
	default:
		return parseViewerEvalNumber(modelViewerString(value))
	}
}

func parseViewerEvalNumber(value string) (float64, bool) {
	// Menu guards use JavaScript Number() in the renderer, including whitespace
	// and unsigned radix prefixes; keep backend suggestions consistent with it.
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, true
	}
	if len(value) > 2 && value[0] == '0' {
		base := 0
		switch value[1] {
		case 'x', 'X':
			base = 16
		case 'b', 'B':
			base = 2
		case 'o', 'O':
			base = 8
		}
		if base != 0 {
			number, err := strconv.ParseUint(value[2:], base, 64)
			return float64(number), err == nil
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	return number, err == nil
}
