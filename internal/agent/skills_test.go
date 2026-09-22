package agent

import (
	"strings"
	"testing"
)

func TestTextureRenderSkillAndReferencesAreEmbedded(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	views := catalog.Reload()
	found := map[string]bool{"texture-render-diagnosis": false, "gimi-texfx-transparency": false}
	for _, view := range views {
		if _, ok := found[view.Name]; ok {
			found[view.Name] = view.Error == "" && view.Source == "built-in" && view.Description != ""
		}
	}
	for name, available := range found {
		if !available {
			t.Fatalf("skill %s is not discoverable", name)
		}
	}
	for _, reference := range []struct{ skill, path string }{
		{"texture-render-diagnosis", ""},
		{"texture-render-diagnosis", "references/uv_selection.py"},
		{"gimi-texfx-transparency", ""},
		{"gimi-texfx-transparency", "references/texfx-transparency.md"},
	} {
		content, err := catalog.Load(reference.skill, reference.path)
		if err != nil || len(content) == 0 {
			t.Fatalf("load %s %q: %v", reference.skill, reference.path, err)
		}
	}
	if _, err := catalog.Load("texture-render-diagnosis", "references/texfx-transparency.md"); err == nil {
		t.Fatal("TexFx-specific reference is still exposed as a common texture reference")
	}
}

func TestBuiltInModDiagnosisSkill(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()

	content, err := catalog.Load("mod-diagnosis", "")
	if err != nil {
		t.Fatal(err)
	}
	steps := []string{
		"Direct-fix fast path",
		"Establish the symptom and scope",
		"Check compatible fixers when the symptom suggests an update",
		"Inspect INIs and resource integrity",
		"Escalate from static files to runtime evidence",
		"Isolate conflicts when the scope permits",
		"one or two ancestor directory levels",
		"Report or patch",
	}
	previous := -1
	for _, step := range steps {
		index := strings.Index(content, step)
		if index < 0 {
			t.Fatalf("mod-diagnosis skill is missing %q", step)
		}
		if index <= previous {
			t.Fatalf("mod-diagnosis skill step %q is out of order", step)
		}
		previous = index
	}
}

func TestBuiltInModDiagnosisSkillUsesRuntimeEvidenceAfterFailedTrial(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()

	content, err := catalog.Load("mod-diagnosis", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{
		"A syntactically correct patch is not proof that the edited path executed",
		"do not stack more speculative edits on top of it",
		"record the unchanged result",
		"capture the target game window before and after a trial",
		"Key delivery proves only that the input was sent",
		"Use `F10` first for ordinary INI/resource changes",
		"Clean up only artifacts that the current diagnostic session recorded as its own",
	} {
		if !strings.Contains(content, instruction) {
			t.Fatalf("mod-diagnosis skill is missing runtime-evidence guidance %q", instruction)
		}
	}
}

func TestBuiltInModDiagnosisSkillDistinguishesGIMIOutlineFixSymptoms(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()

	content, err := catalog.Load("mod-diagnosis", "")
	if err != nil {
		t.Fatal(err)
	}
	requiredInstructions := []string{
		"Wrong reflection or outline coloring",
		"Green skin, a multicolored face",
		`run = CommandList\global\ORFix\ORFix`,
		`run = CommandList\global\ORFix\NNFix`,
		"Do not substitute it automatically on faces",
		"after its `ps-t*` assignments and before `drawindexed`",
		"Never add both calls to one render path",
		"the presence of `ps-t2`",
		"commenting it out for a reversible test",
	}
	for _, instruction := range requiredInstructions {
		if !strings.Contains(content, instruction) {
			t.Fatalf("mod-diagnosis skill is missing GIMI outline-fix guidance %q", instruction)
		}
	}
}

func TestBuiltInModDiagnosisSkillUsesGIMIGreenFastPath(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()

	content, err := catalog.Load("mod-diagnosis", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{
		"An explicit request to fix, modify, update, or repair a mod authorizes focused file edits",
		"GIMI green texture caused by a missing no-normal fix",
		"the affected active non-face `TextureOverride` binds `ps-t0` to its Diffuse resource and `ps-t1` to its LightMap resource",
		"the path has no active normal-map binding",
		"normally immediately after `ps-t1`",
		"`Head` and `Body` sections are eligible",
		"do not automatically patch a `Face` section",
		"Do not require separate inspection of the GIMI library",
		"Do not run `tools.inspect_fixes`, enumerate all desktop actions",
		"The global command does not need to be defined inside the selected mod folder",
		"`TextureOverrideChongyunHead` and `TextureOverrideChongyunBody`",
		"Re-read both sections and stop",
	} {
		if !strings.Contains(content, instruction) {
			t.Fatalf("mod-diagnosis skill is missing GIMI green fast-path guidance %q", instruction)
		}
	}
}

func TestBuiltInSkillsStopAfterSufficientLocalEvidence(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()

	tests := []struct {
		skill string
		want  []string
	}{
		{
			skill: "mod-diagnosis",
			want: []string{
				"The exact rules in this entrypoint take precedence",
				"re-read the changed section",
				"then stop",
			},
		},
		{
			skill: "ini-editing",
			want: []string{
				"known, deterministic local pattern",
				"re-read the changed region, and stop",
				"never replace the whole INI with `write`",
				"A specialized skill's exact rule takes precedence",
			},
		},
		{
			skill: "modding-basics",
			want: []string{
				"an explicit request to fix or modify authorizes focused edits",
				"verify the changed region, and stop",
				"Those specialized skills take precedence over imported examples",
			},
		},
	}
	for _, test := range tests {
		content, err := catalog.Load(test.skill, "")
		if err != nil {
			t.Fatalf("load %s: %v", test.skill, err)
		}
		for _, instruction := range test.want {
			if !strings.Contains(content, instruction) {
				t.Errorf("%s is missing evidence-stop guidance %q", test.skill, instruction)
			}
		}
	}
}

func TestBuiltInModdingBasicsAllowsConnectedBlenderMCP(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()

	content, err := catalog.Load("modding-basics", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, instruction := range []string{
		"Use Blender directly when an enabled Blender MCP exposes the required operation",
		"If no capable Blender MCP is enabled, give optional manual instructions instead",
		"not permission to install Blender or an add-on",
		"also load `blender-xxmi-workflows`",
		"takes precedence over the generic and imported Blender examples here",
	} {
		if !strings.Contains(content, instruction) {
			t.Fatalf("modding-basics skill is missing Blender MCP guidance %q", instruction)
		}
	}
}

func TestBlenderXXMIWorkflowSkillAndReferencesAreEmbedded(t *testing.T) {
	t.Parallel()

	catalog := newSkillCatalog(t.TempDir())
	views := catalog.Reload()
	found := false
	for _, view := range views {
		if view.Name == "blender-xxmi-workflows" {
			found = view.Source == "built-in" && view.Error == "" && view.Description != ""
			break
		}
	}
	if !found {
		t.Fatal("blender-xxmi-workflows is not a discoverable built-in skill")
	}

	references := []struct {
		path string
		want string
	}{
		{path: "", want: "run_xxmi_audit"},
		{path: "references/scene-contract.md", want: "Build the target profile"},
		{path: "references/body-garment.md", want: "Classify before deforming"},
		{path: "references/pmx-mmd-conversion.md", want: "Keep two independent contracts"},
		{path: "references/mesh-transfer-export.md", want: "Define transfer direction"},
		{path: "references/xxmi_audits.py", want: "def preservation_signature("},
	}
	for _, reference := range references {
		content, err := catalog.Load("blender-xxmi-workflows", reference.path)
		if err != nil {
			t.Fatalf("load %q: %v", reference.path, err)
		}
		if !strings.Contains(content, reference.want) {
			t.Errorf("%q is missing %q", reference.path, reference.want)
		}
	}
}

func TestBlenderXXMIWorkflowDefersToExistingSpecialists(t *testing.T) {
	t.Parallel()

	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()
	content, err := catalog.Load("blender-xxmi-workflows", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{
		"`ini-editing` owns INI and toggle text",
		"`mod-diagnosis` owns broken or conflicting mods and runtime evidence",
		"`texture-render-diagnosis` owns DDS channels",
		"`modding-basics` still owns general XXMI concepts",
		"Those specialized skills take precedence",
	} {
		if !strings.Contains(content, boundary) {
			t.Errorf("workflow skill is missing specialist boundary %q", boundary)
		}
	}
}

func TestBuiltInModdingReferencesLoad(t *testing.T) {
	t.Parallel()
	catalog := newSkillCatalog(t.TempDir())
	catalog.Reload()

	references := []struct {
		skill     string
		reference string
		want      string
	}{
		{skill: "modding-basics", reference: "references/fundamentals.md", want: "Hashes and compatibility"},
		{
			skill:     "modding-basics",
			reference: "references/hunting-and-dumping.md",
			want:      "Choose the smallest useful capture",
		},
		{
			skill:     "modding-basics",
			reference: "references/source-user-and-creator-guides.md",
			want:      "Imported user and creator guides",
		},
		{
			skill:     "modding-basics",
			reference: "references/source-blender-and-export.md",
			want:      "Imported Blender and exporter guidance",
		},
		{
			skill:     "modding-basics",
			reference: "references/source-modeling-walkthroughs.md",
			want:      "Imported modeling walkthroughs",
		},
		{
			skill:     "modding-basics",
			reference: "references/source-hunting-guide.md",
			want:      "Imported hunting and dumping guide",
		},
		{
			skill:     "modding-basics",
			reference: "references/source-shader-effects.md",
			want:      "Imported shader and effect guide",
		},
		{
			skill:     "modding-basics",
			reference: "references/source-texture-modding.md",
			want:      "Imported texture-modding guide",
		},
		{
			skill:     "modding-basics",
			reference: "references/source-zzz-materials.md",
			want:      "Imported Zenless Zone Zero material guide",
		},
		{skill: "ini-editing", reference: "references/3dmigoto-syntax.md", want: "Validation checklist"},
		{
			skill:     "ini-editing",
			reference: "references/source-ini-language.md",
			want:      "Imported 3DMigoto INI language reference",
		},
		{
			skill:     "ini-editing",
			reference: "references/source-overrides-and-resources.md",
			want:      "Imported override and resource reference",
		},
		{
			skill:     "ini-editing",
			reference: "references/source-keys-and-custom-shaders.md",
			want:      "Imported key and custom-shader reference",
		},
		{
			skill:     "ini-editing",
			reference: "references/source-shader-overrides.md",
			want:      "Imported shader-override reference",
		},
		{skill: "mod-diagnosis", reference: "references/troubleshooting.md", want: "Symptom-driven troubleshooting"},
		{
			skill:     "mod-diagnosis",
			reference: "references/runtime-render-diagnosis.md",
			want:      "Keep a hypothesis ledger",
		},
		{
			skill:     "mod-diagnosis",
			reference: "references/source-troubleshooting-guide.md",
			want:      "Imported troubleshooting guide",
		},
	}
	for _, reference := range references {
		content, err := catalog.Load(reference.skill, reference.reference)
		if err != nil {
			t.Fatalf("load %s %s: %v", reference.skill, reference.reference, err)
		}
		if !strings.Contains(content, reference.want) {
			t.Fatalf("%s is missing %q", reference.reference, reference.want)
		}
	}
}
