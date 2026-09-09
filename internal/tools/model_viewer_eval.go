package tools

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

func viewerEvalDNFSatisfied(dnf ModelViewerDNF, state map[string]any) bool {
	if dnf == nil {
		return true
	}
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
		guard := effect.When
		if viewerEvalMenuGuardHolds(guard, next) {
			next[effect.Var] = effect.Value
		}
	}
	return next
}

func evaluateViewerTransport(payload ModelViewerTransport, state map[string]any) viewerEvalState {
	resolved := viewerEvalApplyStateRules(mergeViewerEvalState(payload.DefaultState, state), payload.StateRules)
	evaluated := viewerEvalState{State: resolved, Meshes: make([]viewerEvalMesh, len(payload.Meshes))}
	for index, mesh := range payload.Meshes {
		positionIndex := viewerEvalResolvePositionIndex(mesh.PositionVariants, resolved)
		visible := viewerEvalDNFSatisfied(mesh.Conditions, resolved) && (len(mesh.PositionVariants) == 0 || positionIndex != nil)
		evaluated.Meshes[index] = viewerEvalMesh{
			ID:                   mesh.ID,
			Visible:              visible,
			TexKey:               viewerEvalResolveTexture(mesh.TextureVariants, mesh.TexKey, resolved),
			NormalMapKey:         viewerEvalResolveTexture(mesh.NormalMapVariants, mesh.NormalMapKey, resolved),
			LightMapKey:          viewerEvalResolveTexture(mesh.LightMapVariants, mesh.LightMapKey, resolved),
			MaterialMapKey:       viewerEvalResolveTexture(mesh.MaterialMapVariants, mesh.MaterialMapKey, resolved),
			PositionVariantIndex: positionIndex,
		}
	}
	return evaluated
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
			viewerEvalIndexValue(left.Meshes[index].PositionVariantIndex) != viewerEvalIndexValue(right.Meshes[index].PositionVariantIndex) {
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
		for _, variants := range [][]ModelViewerTextureVariant{mesh.TextureVariants, mesh.NormalMapVariants, mesh.LightMapVariants, mesh.MaterialMapVariants} {
			for _, variant := range variants {
				hasTested := false
				for _, group := range variant.Conditions {
					for _, clause := range group {
						if strings.ToLower(clause.Var) == testedLower {
							hasTested = true
						}
					}
				}
				if !hasTested {
					continue
				}
				for _, group := range variant.Conditions {
					for _, clause := range group {
						if strings.ToLower(clause.Var) != testedLower {
							coOccurring[clause.Var] = true
						}
					}
				}
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
