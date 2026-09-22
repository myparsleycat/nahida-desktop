package agent

import (
	"strings"
	"testing"
)

func TestRunXXMIAuditBuildsReadOnlyPrograms(t *testing.T) {
	tests := []struct {
		name       string
		arguments  map[string]any
		invocation string
	}{
		{
			name:       "scene preflight",
			arguments:  map[string]any{"audit": "scene_preflight"},
			invocation: "result = scene_preflight()",
		},
		{
			name: "named preservation signature",
			arguments: map[string]any{
				"audit":        "preservation_signature",
				"object_names": []string{"Body's Mesh"},
			},
			invocation: "result = preservation_signature('Body\\'s Mesh')",
		},
		{
			name:       "PMX source",
			arguments:  map[string]any{"audit": "pmx_source"},
			invocation: "result = pmx_source_audit()",
		},
		{
			name: "weight integrity with target profile limit",
			arguments: map[string]any{
				"audit":          "weight_integrity",
				"object_names":   []string{"Body", "Dress"},
				"max_influences": 8,
			},
			invocation: "result = weight_integrity_audit(['Body', 'Dress'], 8)",
		},
		{
			name: "material and UV",
			arguments: map[string]any{
				"audit":        "material_uv",
				"object_names": []string{"Body"},
			},
			invocation: "result = material_uv_audit(['Body'])",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addon := startFakeBlenderAddon(t)
			pointBlenderAt(t, addon)

			result := callBuiltInTool(t, "run_xxmi_audit", test.arguments)
			if result.IsError {
				t.Fatalf("unexpected tool error: %s", resultText(t, result))
			}
			requests := addon.recorded()
			if len(requests) != 1 {
				t.Fatalf("add-on saw %d requests, want 1", len(requests))
			}
			if !requests[0].StrictJSON {
				t.Error("XXMI audits must require strict JSON results")
			}
			if !strings.Contains(requests[0].Code, "def scene_preflight()") {
				t.Fatal("audit program omitted the embedded implementation")
			}
			if !strings.Contains(requests[0].Code, test.invocation) {
				t.Fatalf("audit program omitted %q", test.invocation)
			}
		})
	}
}

func TestRunXXMIAuditRejectsInvalidArgumentsBeforeBlender(t *testing.T) {
	tests := []struct {
		name      string
		arguments map[string]any
	}{
		{name: "unknown audit", arguments: map[string]any{"audit": "unknown"}},
		{
			name: "scene options",
			arguments: map[string]any{
				"audit":        "scene_preflight",
				"object_names": []string{"Body"},
			},
		},
		{
			name: "multiple signature objects",
			arguments: map[string]any{
				"audit":        "preservation_signature",
				"object_names": []string{"Body", "Dress"},
			},
		},
		{
			name: "invalid influence limit",
			arguments: map[string]any{
				"audit":          "weight_integrity",
				"max_influences": 0,
			},
		},
		{
			name: "empty object name",
			arguments: map[string]any{
				"audit":        "material_uv",
				"object_names": []string{""},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addon := startFakeBlenderAddon(t)
			pointBlenderAt(t, addon)

			result := callBuiltInTool(t, "run_xxmi_audit", test.arguments)
			if !result.IsError {
				t.Fatal("invalid audit arguments must return a tool error")
			}
			if len(addon.recorded()) != 0 {
				t.Fatal("invalid audit arguments must not reach Blender")
			}
		})
	}
}

func TestXXMIAuditSourceIsEmbeddedAndReadOnly(t *testing.T) {
	t.Parallel()

	source, err := builtInSkills.ReadFile(xxmiAuditSourcePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, function := range []string{
		"def scene_preflight()",
		"def preservation_signature(",
		"def pmx_source_audit()",
		"def weight_integrity_audit(",
		"def material_uv_audit(",
	} {
		if !strings.Contains(text, function) {
			t.Errorf("embedded audit source is missing %q", function)
		}
	}
	for _, mutation := range []string{"bpy.ops.", ".select_set(", "bpy.data.objects.remove("} {
		if strings.Contains(text, mutation) {
			t.Errorf("read-only audit source contains mutation primitive %q", mutation)
		}
	}
}

func TestXXMIAuditSourceUsesSemanticMMDAndImageClassification(t *testing.T) {
	t.Parallel()

	source, err := builtInSkills.ReadFile(xxmiAuditSourcePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, behavior := range []string{
		`marker_type == "ROOT"`,
		`marker_type in {"RIGID_BODY", "RIGID"}`,
		`marker_type == "JOINT"`,
		`"requires_external_file": requires_external_file`,
		`os.path.isfile(absolute)`,
		`image["requires_external_file"] and not image["exists_on_disk"]`,
	} {
		if !strings.Contains(text, behavior) {
			t.Errorf("embedded audit source is missing classification behavior %q", behavior)
		}
	}
	if strings.Contains(text, `if markers or "mmd" in lowered or "pmx" in lowered`) {
		t.Error("MMD root detection must not treat every object with registered MMD properties as a root")
	}
}
