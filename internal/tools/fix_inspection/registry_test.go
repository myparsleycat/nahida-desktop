package fixinspection

import (
	"context"
	"testing"
)

func TestFixInspectorRegistry(t *testing.T) {
	registry := NewFixInspectorRegistry()
	ctx := context.Background()

	res, err := registry.Inspect(ctx, "some/path", "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsFix {
		t.Fatal("expected NeedsFix to be false for empty registry")
	}

	dummy := &dummyInspector{supported: "GIMI"}
	registry.Register(dummy)

	res, err = registry.Inspect(ctx, "some/path", "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsFix {
		t.Fatal("expected NeedsFix to be false for unsupported importer")
	}

	res, err = registry.Inspect(ctx, "some/path", "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsFix || res.ToolName != "Dummy Fixer" {
		t.Fatalf("unexpected result from dummy inspector: %+v", res)
	}
}

type dummyInspector struct {
	supported string
}

func (d *dummyInspector) CanInspect(importer string) bool {
	return importer == d.supported
}

func (d *dummyInspector) Inspect(_ context.Context, _ string) (*FixInspectionResult, error) {
	return &FixInspectionResult{
		NeedsFix:   true,
		Importer:   d.supported,
		ToolName:   "Dummy Fixer",
		Summary:    "Dummy fix required",
		ActionTool: "dummy",
	}, nil
}
