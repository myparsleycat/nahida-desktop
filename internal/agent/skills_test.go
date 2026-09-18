package agent

import (
	"strings"
	"testing"
)

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
	} {
		if !strings.Contains(content, instruction) {
			t.Fatalf("modding-basics skill is missing Blender MCP guidance %q", instruction)
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
