package tools

import "testing"

func TestModelViewerExpressionEvaluation(t *testing.T) {
	variables := map[string]any{"toggle": float64(2), "name": "body"}
	for expression, expected := range map[string]bool{
		"$toggle == 2":               true,
		"$toggle > 1 && $toggle < 3": true,
		"!($toggle == 1)":            true,
		"$name == 'body'":            true,
		"($toggle + 2) // 2 == 2":    true,
		"$missing":                   false,
	} {
		if got := evaluateModelViewerCondition(expression, variables); got != expected {
			t.Fatalf("%s = %v, want %v", expression, got, expected)
		}
	}
	if value, ok := evaluateModelViewerNumeric("3 * (2 + 1)", variables); !ok || value != 9 {
		t.Fatalf("numeric = %d, %v", value, ok)
	}
}
