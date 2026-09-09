package tools

import (
	"context"
	"errors"
	"testing"
)

func TestModelViewerMenuNumericGuardsMatchRendererCoercion(t *testing.T) {
	for _, test := range []struct {
		value, threshold string
		want             bool
	}{
		{" 1 ", "0", true}, {"", "0", true}, {"0x10", "15", true}, {"Infinity", "1", false}, {"not-a-number", "0", false},
	} {
		got := viewerEvalMenuGuardHolds(&ModelViewerMenuGuard{Var: "value", Op: ">=", Value: test.threshold}, map[string]any{"value": test.value})
		if got != test.want {
			t.Errorf("guard %q >= %q = %t", test.value, test.threshold, got)
		}
	}
}

func TestModelViewerIneffectiveServiceResolvesEffectsAndSuggestions(t *testing.T) {
	payload := ModelViewerTransport{
		DefaultState: map[string]any{"outfit": "0", "body": "0", "skirt": "0"},
		Variables: []ModelViewerVariable{
			{ID: "outfit", Label: "Outfit", DefaultValue: "0", Values: []ModelViewerVariableValue{{Value: "0"}, {Value: "1"}}, Effects: []ModelViewerMenuEffect{{When: &ModelViewerMenuGuard{Var: "outfit", Op: "==", Value: "1"}, Var: "body", Value: "1"}}},
			{ID: "skirt", Label: "Skirt", DefaultValue: "0", Values: []ModelViewerVariableValue{{Value: "0"}, {Value: "1"}}},
		},
		Meshes: []ModelViewerMeshTransport{{ID: "skirt", Conditions: ModelViewerDNF{{{Var: "body", Value: "1"}, {Var: "skirt", Value: "1"}}}}},
	}
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.modelViewerSessions["test"] = &modelViewerSession{evaluator: newModelViewerEvaluator(ctx, payload), cancel: cancel}
	result, err := service.GetModelViewerIneffectiveValues(context.Background(), "test", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var skirt *ModelViewerIneffectiveValue
	for i := range result {
		if result[i].VariableID == "skirt" {
			skirt = &result[i]
		}
	}
	if skirt == nil || len(skirt.BlockingVars) != 1 || skirt.BlockingVars[0].ID != "outfit" || len(skirt.Suggestions) != 1 {
		t.Fatalf("skirt=%+v result=%+v", skirt, result)
	}
	suggestion := skirt.Suggestions[0]
	if suggestion.Display != "Outfit: 0 → 1" || len(suggestion.Changes) != 2 || suggestion.Changes[0].VarID != "outfit" || suggestion.Changes[1].VarID != "skirt" {
		t.Fatalf("suggestion=%+v", suggestion)
	}
	state := map[string]any{}
	for _, change := range suggestion.Changes {
		for _, variable := range payload.Variables {
			if change.VarID == variable.ID {
				state = applyViewerVariableSelection(state, variable, change.ToValue)
			}
		}
	}
	if !evaluateViewerTransport(payload, state).Meshes[0].Visible {
		t.Fatal("suggestion failed to show skirt")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := service.GetModelViewerIneffectiveValues(cancelled, "test", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled evaluation=%v", err)
	}
	if _, err := service.CleanupModelViewer(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetModelViewerIneffectiveValues(context.Background(), "test", nil); err == nil {
		t.Fatal("cleaned session accepted")
	}
}
