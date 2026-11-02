package modelviewer

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const mergedDeltaTestShader = `
struct VertexAttributes {
    float3 position;
    float3 normal;
    float4 tangent;
};
RWStructuredBuffer<VertexAttributes> rw_buffer : register(u5);
StructuredBuffer<float3> merged_shapekey_pos_deltas : register(t51);
StructuredBuffer<int> merged_shapekey_indices : register(t52);
StructuredBuffer<uint> vertex_freq_indices : register(t53);
void main() {
    uint num_slots = 2;
    float anim_weight_slot0 = IniParams[100 + freq_idx_slot0].x;
    output.position += total_diff_position;
}
`

const mergedDeltaTestDrawShader = `
#define SIZE IniParams[87].xy
#define OFFSET IniParams[87].zw
Texture2D<float4> tex : register(t100);
`

const mergedDeltaTestINI = `
[Constants]
global persist $Freq_Group1 = 0.5
global persist $auto_play_enabled_group1 = 1
global persist $speed_percent_group1 = 100
global persist $speed_percent_min_group1 = 10
global persist $speed_percent_max_group1 = 1000
global persist $frameEnd_group1 = 100
global $shapekey_frame_group1 = 0
global $step_accum_group1 = 0
global $norm_frame_group1 = 0
global $base_step_group1 = 0
global $Freq_a = 0
global $Freq_b = 0
global $help
global $ui_active
global $mouse_clicked = 0
global $is_dragging = 0
global $drag_x = 0
global $pressed = 0
global persist $rel_x = 0.1
global $param = 0
global $min_rel = 0.1
global $range = 0.4

[Present]
if ($auto_play_enabled_group1 == 1)
    $norm_frame_group1 = $shapekey_frame_group1 / $frameEnd_group1
    if ($norm_frame_group1 < 0.5)
        $base_step_group1 = 2
    else
    if (($norm_frame_group1 >= 0.5 && $norm_frame_group1 < 1.0))
        $base_step_group1 = 10
    else
        $base_step_group1 = 1
    endif
    endif
    if ($step_accum_group1 > 0)
        $step_accum_group1 = $step_accum_group1 - 1
    endif
    if ($step_accum_group1 > 0)
        $step_accum_group1 = $step_accum_group1 - 1
    endif
    $Freq_Group1 = $shapekey_frame_group1 / $frameEnd_group1
endif
$Freq_a = $Freq_Group1 * 2 - 0
$Freq_b = $Freq_Group1 * 2 - 1
run = CustomShaderAnim
if $help == 1 && $ui_active == 1
    if $mouse_clicked && $is_dragging == 0
        if cursor_x > 0.1 && cursor_x < 0.15 && cursor_y > 0.1 && cursor_y < 0.15
            $pressed = 1
        endif
    else
        if $is_dragging == 0
            if $pressed == 1
                if cursor_x > 0.1 && cursor_x < 0.15 && cursor_y > 0.1 && cursor_y < 0.15
                    run = CommandListTogglePlay
                endif
                $pressed = 0
            endif
        endif
    endif
    if $mouse_clicked
        if $is_dragging == 0
            if cursor_x > $rel_x + 0.1 && cursor_x < $rel_x + 0.15 && cursor_y > 0.3 && cursor_y < 0.35
                $is_dragging = 2
                $drag_x = cursor_x - $rel_x
            else if cursor_x > 0 && cursor_x < 1 && cursor_y > 0 && cursor_y < 1
                $is_dragging = 1
            endif
        endif
    else
        $is_dragging = 0
    endif
    $param = ($rel_x - $min_rel) / $range
    if $is_dragging == 2
        $rel_x = cursor_x - $drag_x
        if $rel_x < $min_rel
            $rel_x = $min_rel
        endif
        if $rel_x > $min_rel + $range
            $rel_x = $min_rel + $range
        endif
        $param = ($rel_x - $min_rel) / $range
        if ($auto_play_enabled_group1 == 1)
            $speed_percent_group1 = $speed_percent_min_group1 + $param * ($speed_percent_max_group1 - $speed_percent_min_group1)
        else
            $Freq_Group1 = $param
        endif
    endif
    ps-t100 = ResourcePanel
    x87 = 1
    y87 = 1
    z87 = 0
    w87 = 0
    run = CustomShaderDraw
    ps-t100 = ResourceButton
    x87 = 0.05
    y87 = 0.05
    z87 = 0.1
    w87 = 0.1
    run = CustomShaderDraw
    ps-t100 = ResourceHandle
    x87 = 0.05
    y87 = 0.05
    z87 = $rel_x + 0.1
    w87 = 0.3
    run = CustomShaderDraw
endif

[CustomShaderAnim]
x100 = $Freq_a
x101 = $Freq_b
cs-t51 = copy ResourceDeltas
cs-t52 = copy ResourceMap
cs-t53 = copy ResourceFreqs
cs = anim.hlsl
cs-u5 = copy ResourcePosition_0
ResourcePosition = ref cs-u5
Dispatch = 1, 1, 1

[CustomShaderDraw]
vs = draw.hlsl
ps = draw.hlsl
Draw = 4,0

[CommandListTogglePlay]
$auto_play_enabled_group1 = 1 - $auto_play_enabled_group1

[KeyPress]
condition = $help == 1 && $ui_active == 1
key = VK_LBUTTON
type = hold
$mouse_clicked = 1

[ResourcePosition]
type = Buffer
stride = 40
filename = pos.buf

[ResourcePosition_0]
type = Buffer
stride = 40
filename = pos.buf

[ResourceDeltas]
type = Buffer
stride = 12
filename = deltas.buf

[ResourceMap]
type = Buffer
stride = 4
filename = map.buf

[ResourceFreqs]
type = Buffer
stride = 4
filename = freqs.buf

[ResourcePanel]
filename = panel.png

[ResourceButton]
filename = button.png

[ResourceHandle]
filename = handle.png
`

func writeMergedDeltaTestMod(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	words := func(values ...uint32) []byte {
		var output []byte
		for _, value := range values {
			output = binary.LittleEndian.AppendUint32(output, value)
		}
		return output
	}
	floats := func(values ...float32) []byte {
		var output []byte
		for _, value := range values {
			output = binary.LittleEndian.AppendUint32(output, math.Float32bits(value))
		}
		return output
	}
	unused := uint32(modelViewerMergedDeltaUnusedSlot)
	noDelta := ^uint32(0)
	for name, content := range map[string][]byte{
		"pos.buf":    make([]byte, 120),
		"deltas.buf": floats(1, 2, 3, 4, 5, 6),
		// Vertex 0 reads key 1 from slot 1; vertex 2 reads key 0 from slot 0. An unused
		// intensity and a missing delta both leave their slots out.
		"map.buf":    words(noDelta, 0, 0, noDelta, 1, noDelta),
		"freqs.buf":  words(unused, 1, 0, 1, 0, unused),
		"anim.hlsl":  []byte(mergedDeltaTestShader),
		"draw.hlsl":  []byte(mergedDeltaTestDrawShader),
		"panel.png":  []byte("x"),
		"button.png": []byte("x"),
		"handle.png": []byte("x"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func detectMergedDeltaTestMod(
	t *testing.T,
	dir string,
) (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip, modelViewerGeometryScan, map[string]modelViewerVariableName) {
	t.Helper()
	return detectMergedDeltaTestINI(t, dir, mergedDeltaTestINI, modelViewerDirectMesh{
		id: "mesh", positionFile: "pos.buf", geometry: &modelViewerGeometry{VertexCount: 3},
	})
}

func detectMergedDeltaTestINI(
	t *testing.T,
	dir, ini string,
	meshes ...modelViewerDirectMesh,
) (*ModelViewerComputeDeformerTransport, []modelViewerPreparedAnimationClip, modelViewerGeometryScan, map[string]modelViewerVariableName) {
	t.Helper()
	sections, names := scopeModelViewerSections(parseModINI(ini), 0, "", "")
	resources := resolveModelViewerEffectiveResourcesAt(dir, dir, sections, collectModelViewerResources(sections))
	deformer, clips := detectModelViewerComputeAnimation(dir, dir, "", sections, resources, meshes, names)
	if deformer == nil || deformer.Kind != modelViewerMergedDeltaKind || len(clips) != 1 {
		t.Fatalf("deformer = %+v, clips = %d", deformer, len(clips))
	}
	return deformer, clips, modelViewerGeometryScan{sections: sections, resources: resources, iniDir: dir}, names
}

func TestDetectModelViewerMergedDeltaAnimationReadsPlaybackLists(t *testing.T) {
	deformer, clips, _, _ := detectMergedDeltaTestMod(t, writeMergedDeltaTestMod(t))

	merged := deformer.MergedDeltas
	if len(merged.Groups) != 1 || deformer.VertexCount != 3 {
		t.Fatalf("merged = %+v, vertices = %d", merged, deformer.VertexCount)
	}
	group := merged.Groups[0]
	if !slices.Equal(group.Steps, []ModelViewerComputeShapeStep{{Until: 0.5, Step: 2}, {Until: 1, Step: 10}}) ||
		group.StepLimit != 2 || group.FrameEnd != 100 {
		t.Fatalf("group = %+v", group)
	}
	want := []ModelViewerComputeDeltaKey{{Group: 0, Scale: 2, Offset: 0}, {Group: 0, Scale: 2, Offset: 1}}
	if !slices.Equal(merged.Keys, want) {
		t.Fatalf("keys = %+v", merged.Keys)
	}
	// Up and back down: 50/2 + 50/2 (the limit caps the 10 step) ticks each way.
	if frames := len(clips[0].Frames); frames != int(math.Ceil(100.0*2/2/60*60))+1 {
		t.Fatalf("clip frames = %d", frames)
	}
}

func TestBuildModelViewerMergedDeltasGroupsRecordsByKey(t *testing.T) {
	deformer, _, _, _ := detectMergedDeltaTestMod(t, writeMergedDeltaTestMod(t))

	// Vertex 1 is rendered by no mesh and vertex 2 moves to compact index 1.
	raw, err := deformer.MergedDeltas.build(context.Background(), func(vertex uint32) (uint32, bool) {
		return []uint32{0, 0, 1}[vertex], vertex != 1
	})
	if err != nil {
		t.Fatal(err)
	}
	word := func(index int) uint32 { return binary.LittleEndian.Uint32(raw[index*4:]) }
	float := func(index int) float32 { return math.Float32frombits(word(index)) }
	if len(raw) != 2*8+2*modelViewerMergedDeltaRecordBytes {
		t.Fatalf("bytes = %d", len(raw))
	}
	// Key 0 (x100) has the record of vertex 2, key 1 (x101) that of vertex 0.
	if word(0) != 0 || word(1) != 1 || word(2) != 1 || word(3) != 1 {
		t.Fatalf("table = %d %d %d %d", word(0), word(1), word(2), word(3))
	}
	if word(4) != 1 || float(5) != 4 || word(8) != 0 || float(9) != 1 {
		t.Fatalf("records = %v %v %v %v", word(4), float(5), word(8), float(9))
	}
}

func TestDetectModelViewerMergedDeltaAnimationBindsParamsPerDispatch(t *testing.T) {
	dir := writeMergedDeltaTestMod(t)
	if err := os.WriteFile(filepath.Join(dir, "second.buf"), make([]byte, 120), 0o600); err != nil {
		t.Fatal(err)
	}
	ini := strings.NewReplacer(
		"Dispatch = 1, 1, 1\n", `Dispatch = 1, 1, 1
x100 = $Freq_b
x101 = $Freq_a
cs-u5 = copy ResourceSecond_0
ResourceSecond = ref cs-u5
Dispatch = 1, 1, 1
`,
		"[ResourceDeltas]\n", `[ResourceSecond_0]
type = Buffer
stride = 40
filename = second.buf

[ResourceDeltas]
`,
	).Replace(mergedDeltaTestINI)
	geometry := &modelViewerGeometry{VertexCount: 3}
	deformer, _, _, _ := detectMergedDeltaTestINI(
		t, dir, ini,
		modelViewerDirectMesh{id: "first", positionFile: "pos.buf", geometry: geometry},
		modelViewerDirectMesh{id: "second", positionFile: "second.buf", geometry: geometry},
	)

	raw, err := deformer.MergedDeltas.build(context.Background(), func(vertex uint32) (uint32, bool) {
		return vertex, true
	})
	if err != nil {
		t.Fatal(err)
	}
	word := func(index int) uint32 { return binary.LittleEndian.Uint32(raw[index*4:]) }
	vertices := func(key int) []uint32 {
		var output []uint32
		first, count := int(word(key*2)), int(word(key*2+1))
		for record := first; record < first+count; record++ {
			output = append(output, word(len(deformer.MergedDeltas.Keys)*2+record*modelViewerMergedDeltaRecordBytes/4))
		}
		slices.Sort(output)
		return output
	}
	// The first buffer reads intensity 0 as $Freq_a, the second as $Freq_b.
	if got := vertices(0); !slices.Equal(got, []uint32{1, 2, 3}) {
		t.Fatalf("key 0 vertices = %v", got)
	}
	if got := vertices(1); !slices.Equal(got, []uint32{0, 4, 5}) {
		t.Fatalf("key 1 vertices = %v", got)
	}
}

func TestModelViewerOverlayPanelIgnoresHitTestListThatRunsEveryFrame(t *testing.T) {
	dir := writeMergedDeltaTestMod(t)
	ini := strings.NewReplacer(
		"global $pressed = 0\n", "global $pressed = 0\nglobal $hit = 0\n",
		"run = CommandListTogglePlay\n", "$hit = 1\n",
		"    ps-t100 = ResourcePanel\n", "    run = CommandListTogglePlay\n    ps-t100 = ResourcePanel\n",
		"[CommandListTogglePlay]\n$auto_play_enabled_group1 = 1 - $auto_play_enabled_group1\n",
		"[CommandListTogglePlay]\nif $hit == 1\n$auto_play_enabled_group1 = 1 - $auto_play_enabled_group1\n"+
			"$hit = 0\nendif\n",
	).Replace(mergedDeltaTestINI)
	deformer, _, scan, names := detectMergedDeltaTestINI(t, dir, ini, modelViewerDirectMesh{
		id: "mesh", positionFile: "pos.buf", geometry: &modelViewerGeometry{VertexCount: 3},
	})

	menus := buildModelViewerOverlayPanels(
		context.Background(),
		dir,
		[]modelViewerGeometryScan{scan},
		[]ModelViewerComputeDeformerTransport{*deformer},
		names,
	)
	if len(menus) != 1 || len(menus[0].Pages) != 1 {
		t.Fatalf("menus = %+v", menus)
	}
	page := menus[0].Pages[0]
	if len(page.Buttons) != 1 || len(page.Sliders) != 1 {
		t.Fatalf("page = %+v", page)
	}
	button := page.Buttons[0]
	if button.X != 192 ||
		!slices.Equal(button.Transitions, []ModelViewerOverlayTransition{{From: 0, To: 1}, {From: 1, To: 0}}) {
		t.Fatalf("button = %+v", button)
	}
}

func TestModelViewerOverlayPanelFindsButtonsAndSliders(t *testing.T) {
	dir := writeMergedDeltaTestMod(t)
	deformer, _, scan, names := detectMergedDeltaTestMod(t, dir)

	menus := buildModelViewerOverlayPanels(
		context.Background(),
		dir,
		[]modelViewerGeometryScan{scan},
		[]ModelViewerComputeDeformerTransport{*deformer},
		names,
	)
	if len(menus) != 1 || len(menus[0].Pages) != 1 {
		t.Fatalf("menus = %+v", menus)
	}
	page := menus[0].Pages[0]
	if len(page.Quads) != 3 || len(page.Buttons) != 1 || len(page.Sliders) != 1 {
		t.Fatalf("page = %+v", page)
	}

	// The click only takes effect frames after the release, once the panel lets go.
	button := page.Buttons[0]
	if button.Variable != "auto_play_enabled_group1" || button.X != 192 || button.Width != 96 ||
		!slices.Equal(button.Transitions, []ModelViewerOverlayTransition{{From: 0, To: 1}, {From: 1, To: 0}}) {
		t.Fatalf("button = %+v", button)
	}

	slider := page.Sliders[0]
	if slider.Handle != 2 || len(slider.Parts) != 1 || slider.Parts[0].Quad != 2 ||
		slider.Parts[0].StartX != 384 || slider.Parts[0].EndX != 1152 {
		t.Fatalf("slider = %+v", slider)
	}
	want := []ModelViewerOverlaySliderMode{
		{
			WhenVariable: "auto_play_enabled_group1", WhenValue: 1,
			Variable: "speed_percent_group1", Label: "Speed Percent Group1", Min: 10, Max: 1000,
		},
		{
			WhenVariable: "auto_play_enabled_group1", WhenValue: 0,
			Variable: "Freq_Group1", Label: "Freq Group1", Min: 0, Max: 1,
		},
	}
	if !slices.Equal(slider.Modes, want) {
		t.Fatalf("modes = %+v", slider.Modes)
	}
}
