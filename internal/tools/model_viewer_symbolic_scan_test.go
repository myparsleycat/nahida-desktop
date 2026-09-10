package tools

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

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
