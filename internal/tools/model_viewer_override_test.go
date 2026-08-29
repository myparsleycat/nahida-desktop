package tools

import "testing"

func TestModelViewerDrawBindingsSelectActiveBranch(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $toggle = 1

[TextureOverrideBody]
if $toggle == 0
ib = ResourceBodyIB
drawindexed = 3, 0, 0
else
ib = ResourceBodyIB
drawindexed = 3, 3, 2
endif`)
	variables := collectModelViewerDefaultVariables(sections)
	bindings := collectModelViewerDrawBindings(sections, variables)
	indices, err := buildModelViewerIndicesForState(bindings, "BodyIB", []uint32{0, 1, 2, 0, 1, 2}, variables, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(indices) != 3 || indices[0] != 2 || indices[2] != 4 {
		t.Fatalf("indices = %v", indices)
	}
}

func TestModelViewerDrawBindingsParseInstancedDraws(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
drawindexedinstanced = 3, INSTANCE_COUNT, 3, 2, FIRST_INSTANCE`)
	variables := collectModelViewerDefaultVariables(sections)
	bindings := collectModelViewerDrawBindings(sections, variables)
	indices, err := buildModelViewerIndicesForState(bindings, "BodyIB", []uint32{0, 1, 2, 0, 1, 2}, variables, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(indices) != 3 || indices[0] != 2 || indices[2] != 4 {
		t.Fatalf("indices = %v", indices)
	}
}

func TestCollectModelViewerDefaultVariablesMatchesElectronDeclarations(t *testing.T) {
	sections := parseModINI(`[Constants]
$top = 1
persist $mode = 2
global persist $variant = 3
local $ignored = 4
$cycle = 0, 1

[Present]
global $other = enabled
$top = 9`)

	variables := collectModelViewerDefaultVariables(sections)
	if variables["top"] != float64(1) || variables["mode"] != float64(2) || variables["variant"] != float64(3) || variables["other"] != "enabled" {
		t.Fatalf("defaults = %#v", variables)
	}
	if _, ok := variables["ignored"]; ok {
		t.Fatalf("local declaration was collected: %#v", variables)
	}
	if _, ok := variables["cycle"]; ok {
		t.Fatalf("cycle assignment was collected: %#v", variables)
	}
}
