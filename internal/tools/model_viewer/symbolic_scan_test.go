package modelviewer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestModelViewerSymbolicDerivedBodySelector(t *testing.T) {
	t.Parallel()

	var ini strings.Builder
	ini.WriteString(`[Constants]
global persist $top = 2
global persist $bottom = 1
global persist $sleeve = 1
global persist $stocking = 1
global $body_variant = 15
global $mod_enabled = 0
[KeyTop]
type = cycle
$top = 2, 1, 0
[KeyBottom]
type = cycle
$bottom = 1, 0
[KeySleeve]
type = cycle
$sleeve = 1, 0
[KeyStocking]
type = cycle
$stocking = 1, 0
[TextureOverrideBody]
ib = ResourceIB
vb0 = ResourcePos
if $mod_enabled
$body_variant = $bottom * 4 + $sleeve * 2 + $stocking
if $top == 2
$body_variant = $body_variant + 8
endif
`)
	for variant := range 16 {
		branch := "else if"
		if variant == 0 {
			branch = "if"
		}
		fmt.Fprintf(&ini, "%s $body_variant == %d\ndrawindexed = 3, %d, 0\n", branch, variant, variant*3)
	}
	ini.WriteString("endif\nif $sleeve == 1\ndrawindexed = 3, 48, 0\nendif\nendif\n")
	sections := parseModINI(ini.String())
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	tracked := map[string]bool{"top": true, "bottom": true, "sleeve": true, "stocking": true}
	for top := range 3 {
		for bottom := range 2 {
			for sleeve := range 2 {
				for stocking := range 2 {
					state := map[string]string{
						"top": fmt.Sprint(top), "bottom": fmt.Sprint(bottom),
						"sleeve": fmt.Sprint(sleeve), "stocking": fmt.Sprint(stocking),
					}
					variant := bottom*4 + sleeve*2 + stocking
					if top == 2 {
						variant += 8
					}
					want := []int{variant * 3}
					if sleeve == 1 {
						want = append(want, 48)
					}
					var got []int
					for _, record := range records {
						if modelViewerDNFSatisfied(
							normalizeModelViewerDNFWithTracked(record.conditions, tracked),
							state,
						) {
							got = append(got, record.draw.StartIndex)
						}
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("state %v: draws = %v, want %v", state, got, want)
					}
				}
			}
		}
	}
}

func TestModelViewerSymbolicElifPreservesParentAlternatives(t *testing.T) {
	t.Parallel()

	var alternatives []string
	for value := range 12 {
		alternatives = append(alternatives, fmt.Sprintf("$color == %d", value))
	}
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceIB
vb0 = ResourcePos
if ` + strings.Join(alternatives, " || ") + `
if $mode == 0
drawindexed = 3, 0, 0
elif $mode == 1
drawindexed = 3, 3, 0
endif
endif
`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	for color := range 13 {
		for mode := range 3 {
			state := map[string]string{"color": fmt.Sprint(color), "mode": fmt.Sprint(mode)}
			var got []int
			for _, record := range records {
				if modelViewerDNFSatisfied(record.conditions, state) {
					got = append(got, record.draw.StartIndex)
				}
			}
			var want []int
			if color < 12 && mode < 2 {
				want = []int{mode * 3}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("state %v: draws = %v, want %v", state, got, want)
			}
		}
	}
}

func TestModelViewerSymbolicNumericRuntimeGuardStaysUnknown(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		expression string
		condition  string
	}{
		{name: "guard alias", expression: "$mod_enabled", condition: "$copy"},
		{name: "guard arithmetic", expression: "$mod_enabled + 1", condition: "$copy == 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sections := parseModINI(`[Constants]
global $mod_enabled = 0
global persist $outfit = 1
[KeyOutfit]
type = cycle
$outfit = 0, 1
[TextureOverrideBody]
handling = skip
ib = ResourceIB
vb0 = ResourcePos
if $mod_enabled
$copy = ` + test.expression + `
if ` + test.condition + `
if $outfit == 1
drawindexed = 3, 0, 0
endif
endif
endif
`)
			records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != 1 {
				t.Fatalf("draw count = %d, want 1", len(records))
			}
			conditions := normalizeModelViewerDNFWithTracked(records[0].conditions, map[string]bool{"outfit": true})
			for outfit := range 2 {
				state := map[string]string{"outfit": fmt.Sprint(outfit)}
				if got := modelViewerDNFSatisfied(conditions, state); got != (outfit == 1) {
					t.Fatalf("outfit %d: visible = %t, want %t", outfit, got, outfit == 1)
				}
			}
		})
	}
}

func TestModelViewerSymbolicUnknownAssignmentKeepsOtherStates(t *testing.T) {
	t.Parallel()

	sections := parseModINI(`[Constants]
global $mod_enabled = 0
global persist $outfit = 0
[KeyOutfit]
type = cycle
$outfit = 0, 1
[TextureOverrideBody]
ib = ResourceIB
vb0 = ResourcePos
$selector = $outfit + 1
if $outfit == 1
$selector = $mod_enabled + 1
endif
$copy = $selector + 0
if $copy == 1
drawindexed = 3, 0, 0
elif $copy == 2
drawindexed = 3, 3, 0
endif
`)
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	for outfit := range 2 {
		state := map[string]string{"outfit": fmt.Sprint(outfit)}
		var got []int
		for _, record := range records {
			conditions := normalizeModelViewerDNFWithTracked(record.conditions, map[string]bool{"outfit": true})
			if modelViewerDNFSatisfied(conditions, state) {
				got = append(got, record.draw.StartIndex)
			}
		}
		want := []int{0}
		if outfit == 1 {
			want = append(want, 3)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("outfit %d: draws = %v, want %v", outfit, got, want)
		}
	}
}

func TestModelViewerSymbolicBufferWritesPreserveDrawSnapshots(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
vb0 = ResourcePos
ib = ResourceA
drawindexed = 3, 0, 0
if $swap == 1
ib = ResourceB
endif
drawindexed = 3, 3, 0
run = CommandListNested
drawindexed = 3, 9, 0
[CommandListNested]
if $detail == 1
vb0 = ResourceAlt
endif
drawindexed = 3, 6, 0
ib = ResourceC
[TextureOverrideOther]
vb0 = ResourceOtherPos
ib = ResourceOtherIB
drawindexed = 3, 0, 0`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, swap := range []string{"0", "1", "2"} {
		for _, detail := range []string{"0", "1"} {
			state := map[string]string{"swap": swap, "detail": detail}
			ib, position := "A", "Pos"
			if swap == "1" {
				ib = "B"
			}
			if detail == "1" {
				position = "Alt"
			}
			wanted := map[string]modelViewerDirectBufferState{
				"Body:0":  {ib: "A", vb0: "Pos"},
				"Body:3":  {ib: ib, vb0: "Pos"},
				"Body:6":  {ib: ib, vb0: position},
				"Body:9":  {ib: "C", vb0: position},
				"Other:0": {ib: "OtherIB", vb0: "OtherPos"},
			}
			got := make(map[string]modelViewerDirectBufferState)
			for _, record := range records {
				if !modelViewerDNFSatisfied(record.conditions, state) {
					continue
				}
				key := fmt.Sprintf("%s:%d", record.sectionName, record.draw.StartIndex)
				if _, duplicate := got[key]; duplicate {
					t.Fatalf("state %v has duplicate draw %s", state, key)
				}
				got[key] = record.state
			}
			if !reflect.DeepEqual(got, wanted) {
				t.Fatalf("state %v: got %v, want %v", state, got, wanted)
			}
		}
	}
}

// A material set inside an enclosing branch replaces every material set before
// it, however long the chain of per-value variants it follows.
func TestModelViewerSymbolicTextureReplacementSurvivesLongBranchChain(t *testing.T) {
	var chain strings.Builder
	for value := 1; value <= 8; value++ {
		fmt.Fprintf(&chain, "if $color == %d\nResource\\Lib\\Diffuse = ref ResourceOld%d\nendif\n", value, value)
	}
	sections := parseModINI(`[TextureOverrideBody]
vb0 = ResourcePos
ib = ResourceIB
if $enabled == 1
Resource\Lib\Diffuse = ref ResourceOld
` + chain.String() + `Resource\Lib\Diffuse = ref ResourceNew
drawindexed = 3, 0, 0
endif`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].textureHistory) != 1 ||
		!strings.HasSuffix(records[0].textureHistory[0].resource, "New") {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerSymbolicResourceScanPreservesHistories(t *testing.T) {
	sections := parseModINI(`[CommandListRoot]
ib = ResourceA
vb0 = ResourcePos
Resource\ZZMI\Diffuse = ref ResourceDiffuse
this = ResourceDiffuse
drawindexed = 3, 0, 0
run = CommandListNested
[CommandListNested]
if $swap == 1
ib = ResourceB
ps-t1 = ResourceNormalMap
this = ResourceOtherDiffuse
endif
drawindexed = 3, 3, 0`)
	lookup := make(map[string]modINISection)
	for _, section := range sections {
		lookup[modelViewerNormalizeKey(section.Header+section.Name)] = section
	}
	full := &modelViewerSymbolicSectionState{
		buffers:  make(map[string][]modelViewerSymbolicAssignment),
		textures: make(map[string][]modelViewerSymbolicAssignment),
	}
	ctx := &modelViewerSymbolicScanContext{
		lookup:    lookup,
		variables: modelViewerDirectConditionVariables(sections, nil),
	}
	if err := ctx.scan(sections[0].Lines, full, nil, map[string]bool{"commandlistroot": true}); err != nil {
		t.Fatal(err)
	}
	resources, _, err := scanModelViewerSymbolicRoot(sections, sections[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.draws) == 0 || len(resources.draws) != 0 || !resources.explicitDraw ||
		!reflect.DeepEqual(full.buffers, resources.buffers) ||
		!reflect.DeepEqual(full.textures, resources.textures) ||
		!reflect.DeepEqual(full.thisHistory, resources.thisHistory) ||
		!reflect.DeepEqual(full.nonDiffuse, resources.nonDiffuse) {
		t.Fatalf("resource scan changed histories: full=%+v resources=%+v", full, resources)
	}
}

func TestModelViewerHashImageSlotsRejectNestedDraws(t *testing.T) {
	for _, draw := range []string{"", "drawindexed = 3, 0, 0", "drawindexedinstanced = 3, 2, 0, 0, 0"} {
		t.Run(draw, func(t *testing.T) {
			sections := parseModINI(`[TextureOverrideImage]
run = CommandListOuter
[CommandListOuter]
run = CommandListInner
[CommandListInner]
this = ResourceDiffuse
if $swap == 1
` + draw + `
endif`)
			slots := collectHashImageSlots(sections, map[string]modelViewerResource{
				"diffuse": {Name: "Diffuse", Filename: "diffuse.png"},
			}, nil, t.TempDir())
			if draw != "" {
				if len(slots) != 0 {
					t.Fatalf("nested draw became a hash-image slot: %+v", slots)
				}
				return
			}
			if len(slots) != 1 || len(slots[0].files) != 1 || slots[0].files[0].file != "diffuse.png" {
				t.Fatalf("nested image assignment was lost: %+v", slots)
			}
		})
	}
}

func TestBindHashImageTexturesResolvesTiedSlotsStably(t *testing.T) {
	t.Parallel()

	resources := []modelViewerResource{
		{Name: "Body", Filename: "BodyDiffuse.dds"},
		{Name: "BodyB", Filename: "BodyDiffuse-B.dds"},
		{Name: "Ult", Filename: "UltDiffuse.dds"},
		{Name: "UltB", Filename: "UltDiffuse-B.dds"},
	}
	for _, test := range []struct {
		name string
		ini  string
		want string
	}{
		{
			name: "declaration order",
			ini: `[TextureOverrideBody]
if $t == 0
this = ResourceBody
else if $t == 1
this = ResourceBodyB
endif
[TextureOverrideUlt]
if $t == 0
this = ResourceUlt
else if $t == 1
this = ResourceUltB
endif`,
			want: "BodyDiffuse.dds",
		},
		{
			name: "default state",
			ini: `[TextureOverrideUlt]
if $t == 0
this = ResourceUlt
endif
[TextureOverrideBody]
if $t == 1
this = ResourceBodyB
endif`,
			want: "BodyDiffuse-B.dds",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sections := parseModINI(test.ini)
			for range 64 {
				meshes := []modelViewerDirectMesh{{
					component:  "Component3",
					ibName:     "IndexBuffer",
					conditions: ModelViewerDNF{{{Var: "t", Value: "1"}}},
				}}
				bindHashImageTextures(meshes, sections, resources, "", map[string]any{"t": 1.0})
				if meshes[0].textureDefaultFile != test.want {
					t.Fatalf("default file = %q, want %q", meshes[0].textureDefaultFile, test.want)
				}
			}
		})
	}
}

func modelViewerAnimatedDrawSections(frames, draws int) []modINISection {
	var ini strings.Builder
	ini.WriteString("[TextureOverrideBody]\nrun = CommandListFrames\n[CommandListFrames]\n")
	for frame := range frames {
		if frame != 0 {
			ini.WriteString("else ")
		}
		fmt.Fprintf(&ini, "if $frame == %d\nib = ResourceIB%d\nvb0 = ResourcePos%d\n", frame, frame, frame)
		for draw := range draws {
			fmt.Fprintf(&ini, "drawindexed = 3, %d, 0\n", draw*3)
		}
	}
	ini.WriteString("endif\n")
	return parseModINI(ini.String())
}

func TestModelViewerSymbolicAnimationFramesRemainExclusive(t *testing.T) {
	sections := modelViewerAnimatedDrawSections(40, 3)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 120 {
		t.Fatalf("records = %d, want 120", len(records))
	}
	for frame := -1; frame <= 40; frame++ {
		count := 0
		for _, record := range records {
			if len(record.conditions) != 1 || len(record.conditions[0]) != 1 {
				t.Fatalf("redundant frame conditions: %#v", record.conditions)
			}
			if !modelViewerDNFSatisfied(record.conditions, map[string]string{"frame": modelViewerString(frame)}) {
				continue
			}
			count++
			if record.state.ib != fmt.Sprintf("IB%d", frame) || record.state.vb0 != fmt.Sprintf("Pos%d", frame) {
				t.Fatalf("frame %d selects %+v", frame, record.state)
			}
		}
		wanted := 3
		if frame < 0 || frame >= 40 {
			wanted = 0
		}
		if count != wanted {
			t.Fatalf("frame %d selects %d draws, want %d", frame, count, wanted)
		}
	}
}

func BenchmarkModelViewerSymbolicAnimationScan(b *testing.B) {
	sections := modelViewerAnimatedDrawSections(40, 30)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		records, err := collectModelViewerDirectDrawRecords(sections, nil)
		if err != nil || len(records) != 1200 {
			b.Fatalf("records=%d err=%v", len(records), err)
		}
	}
}
