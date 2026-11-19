package modelviewer

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/lo"
)

func TestModelViewerMenuNumericGuardsMatchRendererCoercion(t *testing.T) {
	for _, test := range []struct {
		value, threshold string
		want             bool
	}{
		{" 1 ", "0", true}, {"", "0", true}, {"0x10", "15", true}, {"Infinity", "1", false}, {"not-a-number", "0", false},
	} {
		got := viewerEvalMenuGuardHolds(
			&ModelViewerMenuGuard{Var: "value", Op: ">=", Value: test.threshold},
			map[string]any{"value": test.value},
		)
		if got != test.want {
			t.Errorf("guard %q >= %q = %t", test.value, test.threshold, got)
		}
	}
}

func TestModelViewerIneffectiveServiceResolvesEffectsAndSuggestions(t *testing.T) {
	payload := ModelViewerTransport{
		DefaultState: map[string]any{"outfit": "0", "body": "0", "skirt": "0"},
		Variables: []ModelViewerVariable{
			{
				ID:           "outfit",
				Label:        "Outfit",
				DefaultValue: "0",
				Values:       []ModelViewerVariableValue{{Value: "0"}, {Value: "1"}},
				Effects: []ModelViewerMenuEffect{
					{When: &ModelViewerMenuGuard{Var: "outfit", Op: "==", Value: "1"}, Var: "body", Value: "1"},
				},
			},
			{
				ID:           "skirt",
				Label:        "Skirt",
				DefaultValue: "0",
				Values:       []ModelViewerVariableValue{{Value: "0"}, {Value: "1"}},
			},
		},
		Meshes: []ModelViewerMeshTransport{
			{ID: "skirt", Conditions: ModelViewerDNF{{{Var: "body", Value: "1"}, {Var: "skirt", Value: "1"}}}},
		},
	}
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.modelViewerSessions["test"] = &modelViewerSession{
		evaluator: newModelViewerEvaluator(ctx, payload),
		cancel:    cancel,
	}
	result, err := service.GetModelViewerIneffectiveValues(context.Background(), "test", "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var skirt *ModelViewerIneffectiveValue
	for i := range result {
		if result[i].VariableID == "skirt" {
			skirt = &result[i]
		}
	}
	if skirt == nil || len(skirt.BlockingVars) != 1 || skirt.BlockingVars[0].ID != "outfit" ||
		len(skirt.Suggestions) != 1 {
		t.Fatalf("skirt=%+v result=%+v", skirt, result)
	}
	suggestion := skirt.Suggestions[0]
	if suggestion.Display != "Outfit: 0 → 1" || len(suggestion.Changes) != 2 ||
		suggestion.Changes[0].VarID != "outfit" ||
		suggestion.Changes[1].VarID != "skirt" {
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
	_, err = service.GetModelViewerIneffectiveValues(cancelled, "test", "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled evaluation=%v", err)
	}
	if _, err := service.GetModelViewerIneffectiveValues(context.Background(), "test", "missing", nil); err == nil {
		t.Fatal("unknown form accepted")
	}
	if _, err := service.CleanupModelViewer(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetModelViewerIneffectiveValues(context.Background(), "test", "", nil); err == nil {
		t.Fatal("cleaned session accepted")
	}
}

func TestModelViewerIneffectiveValuesIgnoreMeshesOutsideForm(t *testing.T) {
	payload := ModelViewerTransport{
		DefaultState: map[string]any{"hat": "0"},
		Variables: []ModelViewerVariable{{
			ID:           "hat",
			Label:        "Hat",
			DefaultValue: "0",
			Values:       []ModelViewerVariableValue{{Value: "0"}, {Value: "1"}},
			Form:         "outfit",
		}, {
			ID:           "cape",
			Label:        "Cape",
			DefaultValue: "0",
			Values:       []ModelViewerVariableValue{{Value: "0"}, {Value: "1"}},
			Form:         "other",
		}},
		Meshes: []ModelViewerMeshTransport{
			{ID: "body-a", Form: "outfit::a.ini::A", Conditions: modelViewerDNFTrue()},
			{ID: "hat-a", Form: "outfit::a.ini::A", Conditions: ModelViewerDNF{{{Var: "hat", Value: "1"}}}},
			{ID: "body-b", Form: "outfit::a.ini::B", Conditions: modelViewerDNFTrue()},
		},
	}
	evaluator := newModelViewerEvaluator(context.Background(), payload)
	for _, test := range []struct {
		form        string
		ineffective bool
	}{{"", false}, {"outfit::a.ini::A", false}, {"outfit::a.ini::B", true}} {
		result, err := evaluator.evaluate(context.Background(), test.form, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		hat, cape := false, false
		for _, entry := range result {
			hat = hat || entry.VariableID == "hat" && entry.Value == "1"
			cape = cape || entry.VariableID == "cape"
		}
		if hat != test.ineffective {
			t.Errorf("form %q: hat=1 ineffective=%t, want %t (result=%+v)", test.form, hat, test.ineffective, result)
		}
		if cape != (test.form == "") {
			t.Errorf("form %q: other form's variable reported=%t (result=%+v)", test.form, cape, result)
		}
	}
}

func TestModelViewerIneffectiveValuesCountTextureChoices(t *testing.T) {
	// The UI applies a choice over the mesh's slots, so a variable that switches only a choice's texture still
	// changes what the viewer draws once that choice is picked.
	payload := ModelViewerTransport{
		DefaultState: map[string]any{"swap": "0"},
		Variables: []ModelViewerVariable{{
			ID:           "swap",
			Label:        "Swap",
			DefaultValue: "0",
			Values:       []ModelViewerVariableValue{{Value: "0"}, {Value: "1"}},
		}},
		Meshes: []ModelViewerMeshTransport{{
			ID:         "body-c",
			Conditions: modelViewerDNFTrue(),
			TextureChoices: []ModelViewerTextureChoice{{
				Source: "BodyA",
				TexKey: lo.ToPtr("a"),
				TextureVariants: []ModelViewerTextureVariant{
					{Conditions: ModelViewerDNF{{{Var: "swap", Value: "1"}}}, TexKey: "a-swapped"},
				},
			}, {Source: "BodyB", TexKey: lo.ToPtr("b")}},
		}},
	}
	result, err := newModelViewerEvaluator(context.Background(), payload).
		evaluate(context.Background(), "", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range result {
		if entry.VariableID == "swap" {
			t.Errorf("swap=%s reported ineffective though it switches a texture choice", entry.Value)
		}
	}
}
