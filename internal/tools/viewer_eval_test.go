package tools

import "testing"

func TestViewerEvalLookupIsCaseInsensitive(t *testing.T) {
	value, ok := viewerEvalLookup(map[string]any{"Outfit": "1"}, "outfit")
	if !ok || modelViewerString(value) != "1" {
		t.Fatalf("lookup = %v %v", value, ok)
	}
}

func TestViewerEvalDNFSatisfied(t *testing.T) {
	dnf := ModelViewerDNF{{{Var: "outfit", Value: "1"}, {Var: "hat", Value: "0", Negate: true}}}
	if !viewerEvalDNFSatisfied(dnf, map[string]any{"outfit": "1", "hat": "1"}) {
		t.Fatal("expected matching state to satisfy DNF")
	}
	if viewerEvalDNFSatisfied(dnf, map[string]any{"outfit": "1", "hat": "0"}) {
		t.Fatal("negated hat still matched")
	}
	if !viewerEvalDNFSatisfied(nil, map[string]any{"outfit": "0"}) {
		t.Fatal("nil DNF should be unconstrained")
	}
	if viewerEvalDNFSatisfied(ModelViewerDNF{}, map[string]any{"outfit": "1"}) {
		t.Fatal("empty DNF should be false")
	}
}

func TestEvaluateViewerTransportResolvesLaterTextureAndHidesUnsatisfiedMesh(t *testing.T) {
	texA, texB := "a", "b"
	payload := ModelViewerTransport{
		DefaultState: map[string]any{"outfit": "0", "hat": "0"},
		Meshes: []ModelViewerMeshTransport{
			{
				ID:     "body",
				TexKey: &texA,
				TextureVariants: []ModelViewerTextureVariant{
					{Conditions: ModelViewerDNF{{{Var: "outfit", Value: "1"}}}, TexKey: texB},
				},
			},
			{
				ID:         "hat",
				Conditions: ModelViewerDNF{{{Var: "hat", Value: "1"}}},
			},
		},
	}
	hidden := evaluateViewerTransport(payload, nil)
	if !hidden.Meshes[0].Visible || hidden.Meshes[0].TexKey != texA || hidden.Meshes[1].Visible {
		t.Fatalf("default eval = %#v", hidden.Meshes)
	}
	shown := evaluateViewerTransport(payload, map[string]any{"outfit": "1", "hat": "1"})
	if shown.Meshes[0].TexKey != texB || !shown.Meshes[1].Visible {
		t.Fatalf("selected eval = %#v", shown.Meshes)
	}
}

func TestApplyViewerVariableSelectionAppliesGuardedEffects(t *testing.T) {
	variable := ModelViewerVariable{
		ID: "slot",
		Effects: []ModelViewerMenuEffect{
			{Var: "outfit", Value: "1", When: &ModelViewerMenuGuard{Var: "slot", Op: "==", Value: "2"}},
		},
	}
	next := applyViewerVariableSelection(map[string]any{"slot": "0", "outfit": "0"}, variable, "2")
	if modelViewerString(next["slot"]) != "2" || modelViewerString(next["outfit"]) != "1" {
		t.Fatalf("next = %#v", next)
	}
}

func computeViewerIneffectiveValues(payload ModelViewerTransport, state map[string]any) map[string]map[string][]viewerBlockingVar {
	resolved := viewerEvalApplyStateRules(mergeViewerEvalState(payload.DefaultState, state), payload.StateRules)
	baseline := evaluateViewerTransport(payload, resolved)
	result := make(map[string]map[string][]viewerBlockingVar)
	for _, variable := range payload.Variables {
		if variable.ControlType == "slider" || len(variable.Values) == 0 {
			continue
		}
		current, _ := viewerEvalLookup(resolved, variable.ID)
		if current == nil {
			current = variable.DefaultValue
		}
		dead := make(map[string][]viewerBlockingVar)
		for _, entry := range variable.Values {
			if modelViewerString(entry.Value) == modelViewerString(current) {
				continue
			}
			nextState := applyViewerVariableSelection(resolved, variable, entry.Value)
			nextEval := evaluateViewerTransport(payload, nextState)
			if !viewerEvalStatesDiffer(baseline, nextEval) {
				dead[modelViewerString(entry.Value)] = buildViewerBlockingVars(payload, variable.ID, resolved)
			}
		}
		if len(dead) > 0 {
			result[variable.ID] = dead
		}
	}
	return result
}
