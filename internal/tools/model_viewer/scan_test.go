package modelviewer

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestLoadModViewerBindsComponentCommandBuffersByFrame(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ini := `[Constants]
global $frame = 0
global $fps = 60
[Present]
$frame = (time * $fps) // 1

[TextureOverrideTorsoPartBlend]
run = CommandListTorsoPosition
[TextureOverrideTorsoPartTexcoord]
run = CommandListTorsoTexcoord
[TextureOverrideTorsoPartB]
run = CommandListTorsoIndex
drawindexed = 3, 0, 0
[TextureOverrideLimbPartBlend]
run = CommandListLimbPosition
[TextureOverrideLimbPartTexcoord]
vb1 = ResourceLimbPartTexcoord
[TextureOverrideLimbPartC]
ib = ResourceLimbPartCIB.0
drawindexed = 3, 0, 0
[TextureOverrideOtherBlend]
vb0 = ResourceOtherPosition.0
vb1 = ResourceOtherTexcoord

[CommandListTorsoPosition]
if $frame == 0
vb0 = ResourceTorsoPartPosition.0
else
vb0 = ResourceTorsoPartPosition.1
endif
[CommandListTorsoTexcoord]
if $frame == 0
vb1 = ResourceTorsoPartTexcoord.0
else
vb1 = ResourceTorsoPartTexcoord.1
endif
[CommandListTorsoIndex]
if $frame == 0
ib = ResourceTorsoPartBIB.0
else if $frame == 1
ib = ResourceTorsoPartBIB.1
endif
[CommandListLimbPosition]
if $frame == 0
vb0 = ResourceLimbPartPosition.8
vb2 = ResourceWeightsA
else
vb0 = ResourceLimbPartPosition.0
vb2 = ResourceWeightsB
endif
`

	for name, x := range map[string]float32{
		"TorsoPartPosition.0": 10, "TorsoPartPosition.1": 20,
		"LimbPartPosition.8": 30, "LimbPartPosition.0": 40, "OtherPosition.0": 50,
	} {
		data := make([]byte, 3*40)
		for vertex := range 3 {
			binary.LittleEndian.PutUint32(data[vertex*40:], math.Float32bits(x+float32(vertex)))
			binary.LittleEndian.PutUint32(data[vertex*40+4:], math.Float32bits(float32(vertex/2)))
		}
		writeTextureFile(t, dir, name+".buf", data)
		ini += fmt.Sprintf("\n[Resource%s]\nfilename = %s.buf\nstride = 40\n", name, name)
	}
	for name, u := range map[string]float32{
		"TorsoPartTexcoord.0": 0.125, "TorsoPartTexcoord.1": 0.25,
		"LimbPartTexcoord": 0.5, "OtherTexcoord": 0.75,
	} {
		data := make([]byte, 3*20)
		for vertex := range 3 {
			uv := u + float32(vertex)/16
			binary.LittleEndian.PutUint32(data[vertex*20+4:], math.Float32bits(uv))
			binary.LittleEndian.PutUint32(data[vertex*20+8:], math.Float32bits(uv))
		}
		writeTextureFile(t, dir, name+".buf", data)
		ini += fmt.Sprintf("\n[Resource%s]\nfilename = %s.buf\nstride = 20\n", name, name)
	}
	for name, indices := range map[string][]uint32{
		"TorsoPartBIB.0": {0, 1, 2}, "TorsoPartBIB.1": {1, 2, 0}, "LimbPartCIB.0": {0, 1, 2},
	} {
		writeTextureFile(t, dir, name+".ib", modelViewerUint32Bytes(indices))
		ini += fmt.Sprintf("\n[Resource%s]\nfilename = %s.ib\nformat = DXGI_FORMAT_R32_UINT\n", name, name)
	}
	for name, group := range map[string]uint32{"WeightsA": 7, "WeightsB": 9} {
		data := make([]byte, 3*32)
		for vertex := range 3 {
			binary.LittleEndian.PutUint32(data[vertex*32:], math.Float32bits(1))
			binary.LittleEndian.PutUint32(data[vertex*32+16:], group)
		}
		writeTextureFile(t, dir, name+".buf", data)
		ini += fmt.Sprintf("\n[Resource%s]\nfilename = %s.buf\nstride = 32\n", name, name)
	}

	writeTextureFile(t, dir, "mod.ini", []byte(ini))
	fixture := loadViewerDir(t, dir)
	type selectedMesh struct {
		x, u       float32
		firstIndex uint32
		boneGroup  uint16
	}
	if len(fixture.result.Animations) != 1 || len(fixture.result.Animations[0].Frames) != 2 {
		t.Fatalf("animations = %+v, want a two-frame clip", fixture.result.Animations)
	}
	for frame, wanted := range []map[string]selectedMesh{
		{"TorsoPartB": {10, 0.125, 0, 0}, "LimbPartC": {30, 0.5, 0, 7}},
		{"TorsoPartB": {20, 0.25, 1, 0}, "LimbPartC": {40, 0.5, 0, 9}},
	} {
		evaluated := evaluateViewerTransport(fixture.result, fixture.result.Animations[0].Frames[frame].Values)
		seen := make(map[string]selectedMesh)
		for index, state := range evaluated.Meshes {
			if !state.Visible {
				continue
			}
			mesh := fixture.result.Meshes[index]
			if _, duplicate := seen[mesh.Component]; duplicate {
				t.Fatalf("frame %d draws a second %s mesh", frame, mesh.Component)
			}
			geometry := readViewerMesh(t, fixture.protocol, mesh.GeometryURL)
			var boneGroup uint16
			if mesh.BlendURL != "" {
				data := readModelViewerProtocolBytes(t, fixture.protocol, mesh.BlendURL)
				boneGroup = binary.LittleEndian.Uint16(data)
			}
			seen[mesh.Component] = selectedMesh{geometry.Positions[0], geometry.UVs[0], geometry.Indices[0], boneGroup}
		}
		if !maps.Equal(seen, wanted) {
			t.Fatalf("frame %d draws %+v, want %+v", frame, seen, wanted)
		}
	}
}

func TestLoadModViewerKeepsDrawWithoutConditionalBlend(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	writeTextureFile(t, dir, "weights.buf", viewerTestBlendWeights(9))
	writeTextureFile(t, dir, "mod.ini", []byte(`[Constants]
global $extra = 0
[KeyExtra]
type = cycle
$extra = 0, 1
[TextureOverrideBodyBlend]
run = CommandListBlend
[CommandListBlend]
if $extra == 1
vb2 = ResourceWeights
endif
[TextureOverrideBodyA]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceWeights]
filename = weights.buf
stride = 32
`+viewerBodyResources))
	fixture := loadViewerDir(t, dir)
	for extra, wantGroup := range []uint16{0, 9} {
		states := evaluateViewerTransport(fixture.result, map[string]any{"extra": float64(extra)})
		visible := 0
		var group uint16
		for index, state := range states.Meshes {
			if !state.Visible {
				continue
			}
			visible++
			if url := fixture.result.Meshes[index].BlendURL; url != "" {
				data := readModelViewerProtocolBytes(t, fixture.protocol, url)
				group = binary.LittleEndian.Uint16(data)
			}
		}
		if visible != 1 || group != wantGroup {
			t.Errorf(
				"extra=%d: %d visible meshes, bone group %d; want 1 mesh, group %d",
				extra,
				visible,
				group,
				wantGroup,
			)
		}
	}
}

func TestLoadModViewerPrefersExactComponentTexcoord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	for name, u := range map[string]float32{"arm-tc.buf": 0.125, "robot-tc.buf": 0.625} {
		data := make([]byte, 8*20)
		for row := range 8 {
			uv := u + float32(row)/32
			binary.LittleEndian.PutUint32(data[row*20+4:], math.Float32bits(uv))
			binary.LittleEndian.PutUint32(data[row*20+8:], math.Float32bits(uv))
		}
		writeTextureFile(t, dir, name, data)
	}
	writeTextureFile(t, dir, "mod.ini", []byte(`[TextureOverrideRobotArmPosition]
vb0 = ResourcePos
[TextureOverrideRobotArmTexcoord]
vb1 = ResourceArmTex
[TextureOverrideRobotArmA]
ib = ResourceRobotArmAIB
drawindexed = 3, 0, 0
[TextureOverrideRobotTexcoord]
run = CommandListRobotTex
[CommandListRobotTex]
vb1 = ResourceRobotTex
[ResourceArmTex]
filename = arm-tc.buf
stride = 20
[ResourceRobotTex]
filename = robot-tc.buf
stride = 20
[ResourceRobotArmAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`+viewerBodyResources))
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 1 {
		t.Fatalf("got %d meshes, want one arm mesh", len(fixture.result.Meshes))
	}
	geometry := readViewerMesh(t, fixture.protocol, fixture.result.Meshes[0].GeometryURL)
	if got := geometry.UVs[0]; got != 0.125 {
		t.Fatalf("arm U = %v, want 0.125 from its own override", got)
	}
}

func viewerTestBlendWeights(group uint32) []byte {
	data := make([]byte, 8*32)
	for row := range 8 {
		binary.LittleEndian.PutUint32(data[row*32:], math.Float32bits(1))
		binary.LittleEndian.PutUint32(data[row*32+16:], group)
	}
	return data
}

func TestLoadModViewerKeepsComplexBufferConditionsExclusive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	writeTextureFile(t, dir, "weights.buf", viewerTestBlendWeights(9))
	var ini strings.Builder
	ini.WriteString("[Constants]\n")
	for index := range 16 {
		fmt.Fprintf(&ini, "global $switch%d = 0\n", index)
	}
	for index := range 16 {
		fmt.Fprintf(&ini, "[KeySwitch%d]\ntype = cycle\n$switch%d = 0, 1\n", index, index)
	}
	ini.WriteString(`
[TextureOverrideBodyBlend]
run = CommandListWeights
[CommandListWeights]
if `)
	var conditions []string
	for index := range 8 {
		conditions = append(conditions, fmt.Sprintf("($switch%d == 1 && $switch%d == 1)", 2*index, 2*index+1))
	}
	ini.WriteString(strings.Join(conditions, " || "))
	ini.WriteString(`
vb2 = ResourceWeights
endif
[TextureOverrideBodyA]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceWeights]
filename = weights.buf
stride = 32
` + viewerBodyResources)
	writeTextureFile(t, dir, "mod.ini", []byte(ini.String()))
	fixture := loadViewerDir(t, dir)
	state := map[string]any{}
	for _, on := range []bool{false, true, false} {
		value := float64(0)
		if on {
			value = 1
		}
		state["switch0"], state["switch1"] = value, value
		states := evaluateViewerTransport(fixture.result, state)
		state = states.State
		visible := 0
		var palette string
		for i, mesh := range states.Meshes {
			if mesh.Visible {
				visible++
				palette = fixture.result.Meshes[i].BlendPalette
			}
		}
		wantPalette := ""
		if on {
			wantPalette = "weights.buf#0"
		}
		if visible != 1 || palette != wantPalette {
			t.Errorf(
				"first pair enabled=%t: %d visible meshes, palette %q, want 1 mesh with palette %q",
				on,
				visible,
				palette,
				wantPalette,
			)
		}
	}
}

func TestLoadModViewerKeepsFrameBlendPalettes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	writeTextureFile(t, dir, "weights-a.buf", viewerTestBlendWeights(9))
	writeTextureFile(t, dir, "weights-b.buf", viewerTestBlendWeights(9))
	writeTextureFile(t, dir, "mod.ini", []byte(`[Constants]
global $frame = 0
global $fps = 60
[Present]
$frame = (time * $fps) // 1
[TextureOverrideBodyBlend]
run = CommandListWeights
[CommandListWeights]
if $frame == 0
vb2 = ResourceWeightsA
else if $frame == 1
vb2 = ResourceWeightsB
endif
[TextureOverrideBodyA]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceWeightsA]
filename = weights-a.buf
stride = 32
[ResourceWeightsB]
filename = weights-b.buf
stride = 32
`+viewerBodyResources))
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Animations) != 1 || len(fixture.result.Animations[0].Frames) != 2 {
		t.Fatalf("unexpected animations %v", fixture.result.Animations)
	}
	for index, want := range []string{"weights-a.buf#0", "weights-b.buf#0"} {
		states := evaluateViewerTransport(fixture.result, fixture.result.Animations[0].Frames[index].Values)
		var palettes []string
		for k, mesh := range states.Meshes {
			if mesh.Visible {
				palettes = append(palettes, fixture.result.Meshes[k].BlendPalette)
			}
		}
		if len(palettes) != 1 || palettes[0] != want {
			t.Errorf("frame %d: palettes %q, want %s", index, palettes, want)
		}
	}
}

func TestLoadModViewerKeepsDrawTexcoordWithCommandPosition(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	otherPos := make([]byte, 8*40)
	for row := range 8 {
		binary.LittleEndian.PutUint32(otherPos[row*40:], math.Float32bits(100+float32(row)))
	}
	writeTextureFile(t, dir, "other-pos.buf", otherPos)
	for name, u := range map[string]float32{"arm-tc.buf": 0.125, "other-tc.buf": 0.625} {
		data := make([]byte, 8*20)
		for row := range 8 {
			uv := u + float32(row)/32
			binary.LittleEndian.PutUint32(data[row*20+4:], math.Float32bits(uv))
			binary.LittleEndian.PutUint32(data[row*20+8:], math.Float32bits(uv))
		}
		writeTextureFile(t, dir, name, data)
	}
	writeTextureFile(t, dir, "mod.ini", []byte(`[Constants]
global $style = 0
[KeyStyle]
type = cycle
$style = 0, 1
[TextureOverrideArmBlend]
run = CommandListArmPosition
[CommandListArmPosition]
if $style == 1
vb0 = ResourcePos
endif
[TextureOverrideArmA]
ib = ResourceArmAIB
vb1 = ResourceArmTex
drawindexed = 3, 0, 0
[CommandListOtherBuffers]
vb0 = ResourceOtherPos
vb1 = ResourceOtherTex
[ResourceOtherPos]
filename = other-pos.buf
stride = 40
[ResourceArmTex]
filename = arm-tc.buf
stride = 20
[ResourceOtherTex]
filename = other-tc.buf
stride = 20
[ResourceArmAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`+viewerBodyResources))
	fixture := loadViewerDir(t, dir)
	states := evaluateViewerTransport(fixture.result, map[string]any{"style": float64(1)})
	var values [][2]float32
	for i, mesh := range states.Meshes {
		if mesh.Visible {
			geometry := readViewerMesh(t, fixture.protocol, fixture.result.Meshes[i].GeometryURL)
			values = append(values, [2]float32{geometry.Positions[0], geometry.UVs[0]})
		}
	}
	if len(values) != 1 || values[0] != [2]float32{0, 0.125} {
		t.Fatalf("(position x, UV u) = %v, want (0, 0.125) from the arm buffers", values)
	}
}

func TestLoadModViewerKeepsDrawConditionWhenJoiningBufferConditions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	writeTextureFile(t, dir, "weights.buf", viewerTestBlendWeights(9))
	var ini strings.Builder
	ini.WriteString("[Constants]\nglobal $kind = 0\n")
	for index := range 14 {
		fmt.Fprintf(&ini, "global $switch%d = 0\n", index)
	}
	for index := range 14 {
		fmt.Fprintf(&ini, "[KeySwitch%d]\ntype = cycle\n$switch%d = 0, 1\n", index, index)
	}
	ini.WriteString(`
[KeyKind]
type = cycle
$kind = 0, 1, 2
[TextureOverrideBodyBlend]
run = CommandListWeights
[CommandListWeights]
if `)
	var conditions []string
	for index := range 7 {
		conditions = append(conditions, fmt.Sprintf("($switch%d == 1 && $switch%d == 1)", 2*index, 2*index+1))
	}
	ini.WriteString(strings.Join(conditions, " || "))
	ini.WriteString(`
vb2 = ResourceWeights
endif
[TextureOverrideBodyA]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $kind == 0 || $kind == 1
drawindexed = 3, 0, 0
endif
[ResourceWeights]
filename = weights.buf
stride = 32
` + viewerBodyResources)
	writeTextureFile(t, dir, "mod.ini", []byte(ini.String()))
	fixture := loadViewerDir(t, dir)
	for _, scenario := range []struct{ kind, on, want int }{{0, 0, 1}, {0, 1, 1}, {2, 0, 0}, {2, 1, 0}} {
		state := map[string]any{
			"kind":    float64(scenario.kind),
			"switch0": float64(scenario.on),
			"switch1": float64(scenario.on),
		}
		states := evaluateViewerTransport(fixture.result, state)
		visible := 0
		for _, mesh := range states.Meshes {
			if mesh.Visible {
				visible++
			}
		}
		if visible != scenario.want {
			t.Errorf("kind=%d switch pair=%d: visible %d, want %d", scenario.kind, scenario.on, visible, scenario.want)
		}
	}
}

func TestLoadModViewerMatchesCommandBuffersThroughCanonicalIBName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"ArmorAIB", "armoraib"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeViewerGeometry(t, dir)
			writeTextureFile(t, dir, "weights.buf", viewerTestBlendWeights(9))
			ini := fmt.Sprintf(`[Constants]
global $option = 0
[KeyOption]
type = cycle
$option = 0, 1
[TextureOverrideArmorBlend]
run = CommandListWeights
[CommandListWeights]
if $option == 1
vb2 = ResourceWeights
endif
[TextureOverrideArmorA]
ib = Resource%s
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceArmorAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceWeights]
filename = weights.buf
stride = 32
`, name)
			writeTextureFile(t, dir, "mod.ini", []byte(ini+viewerBodyResources))
			fixture := loadViewerDir(t, dir)
			states := evaluateViewerTransport(fixture.result, map[string]any{"option": float64(1)})
			var palettes []string
			for index, mesh := range states.Meshes {
				if mesh.Visible {
					palettes = append(palettes, fixture.result.Meshes[index].BlendPalette)
				}
			}
			if len(palettes) != 1 || palettes[0] != "weights.buf#0" {
				t.Fatalf("visible palettes %q, want [weights.buf#0]", palettes)
			}
		})
	}
}

func TestLoadModViewerKeepsGlobalBlendForSharedPosition(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	writeTextureFile(t, dir, "weights.buf", viewerTestBlendWeights(9))
	writeTextureFile(t, dir, "mod.ini", []byte(`[TextureOverrideSharedBuffers]
run = CommandListSharedBuffers
[CommandListSharedBuffers]
vb0 = ResourcePos
vb1 = ResourceTc
vb2 = ResourceWeights
[TextureOverrideBodyA]
ib = ResourceBodyIB
vb0 = ResourcePos
drawindexed = 3, 0, 0
[ResourceWeights]
filename = weights.buf
stride = 32
`+viewerBodyResources))
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 1 {
		t.Fatalf("mesh count %d, want 1", len(fixture.result.Meshes))
	}
	mesh := fixture.result.Meshes[0]
	if mesh.BlendPalette != "weights.buf#0" || mesh.BlendURL == "" {
		t.Fatalf("blend palette=%q, URL=%q, want the shared weights", mesh.BlendPalette, mesh.BlendURL)
	}
}

func TestModelViewerRabbitFXCutoutFollowsDraws(t *testing.T) {
	sections := parseModINI(`[TextureOverrideParts]
ib = ResourceIB
Resource\RabbitFX\Diffuse = ref ResourceAtlasA
run = CommandList\RabbitFX\SetTextures
drawindexed = 3, 0, 0
Resource\RabbitFX\FXMap = ref resourceCutoutMaskA
run = CommandList\RabbitFX\Run
drawindexed = 3, 3, 0
Resource\RabbitFX\Diffuse = ref ResourceAtlasB
run = CommandList\RabbitFX\SetTextures
drawindexed = 3, 6, 0
Resource\RabbitFX\FXMap = ref ResourceCutoutMaskB
Resource\RabbitFX\Diffuse = ref ResourceAtlasC
run = CommandList\RabbitFX\SetTextures
drawindexed = 3, 9, 0
Resource\RabbitFX\FXMap = ref ResourceSparkle
run = CommandList\RabbitFX\Run
drawindexed = 3, 12, 0`)
	records, _, err := collectModelViewerSymbolicDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{0: "", 3: "CutoutMaskA", 6: "", 9: "CutoutMaskB", 12: ""}
	if len(records) != len(want) {
		t.Fatalf("records = %d", len(records))
	}
	for _, record := range records {
		got := ""
		for _, assignment := range record.textureHistory {
			if assignment.role == modelViewerCutoutMapRole {
				got = assignment.resource
			}
		}
		if got != want[record.draw.StartIndex] {
			t.Errorf("draw at %d cutout = %q, want %q", record.draw.StartIndex, got, want[record.draw.StartIndex])
		}
	}
}

func TestModelViewerSharedGeometryPreservesMeshState(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	sections := parseModINI(`[TextureOverrideBody]
if $swap == 0
ps-t0 = ResourceRedDiffuse
run = CommandListDraw
endif
[TextureOverrideAlternate]
if $swap == 1
ps-t0 = ResourceBlueDiffuse
run = CommandListDraw
endif
[CommandListDraw]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceRedDiffuse]
filename = red.png
[ResourceBlueDiffuse]
filename = blue.png
` + viewerBodyResources)
	cache := newModelViewerBufferCache(context.Background())
	defer cache.releaseAll()
	timing := &modelViewerMeshBuildTiming{}
	meshes, err := buildModelViewerDirectScannedMeshesAt(
		filepath.Join(dir, "mod.ini"),
		dir,
		sections,
		nil,
		cache,
		timing,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 || timing.Geometries != 1 || meshes[0].geometry != meshes[1].geometry {
		t.Fatalf("meshes=%d geometry extractions=%d", len(meshes), timing.Geometries)
	}
	if meshes[0].id == meshes[1].id || meshes[0].component != "Body" || meshes[1].component != "Alternate" {
		t.Fatal("sharing geometry merged mesh identity")
	}
	available := map[string]modelViewerTexturePayload{
		"reddiffuse": {Key: "reddiffuse"}, "bluediffuse": {Key: "bluediffuse"},
	}
	for index, mesh := range meshes {
		for _, frame := range []string{"0", "1", "2"} {
			if modelViewerDNFSatisfied(
				mesh.conditions,
				map[string]string{"swap": frame},
			) != (frame == modelViewerString(index)) {
				t.Fatalf("mesh %d changed visibility at frame %s", index, frame)
			}
		}
		item, payload := buildModelViewerDirectMeshPayload(mesh, nil, available, nil, cache)
		wantTexture := []string{"reddiffuse", "bluediffuse"}[index]
		if item.TexKey == nil || *item.TexKey != wantTexture {
			t.Fatalf("mesh %d texture = %v", index, item.TexKey)
		}
		if !slices.Equal(payload.UVs, []float32{0, 1, 0, 1, 0, 1}) ||
			!slices.Equal(payload.Indices, []uint32{0, 1, 2}) {
			t.Fatalf("shared geometry was transformed again: UVs=%v indices=%v", payload.UVs, payload.Indices)
		}
	}
}

func TestModelViewerSharedGeometryDistinguishesBaseVertex(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideOther]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 3
` + viewerBodyResources)
	cache := newModelViewerBufferCache(context.Background())
	defer cache.releaseAll()
	timing := &modelViewerMeshBuildTiming{}
	meshes, err := buildModelViewerDirectScannedMeshesAt(
		filepath.Join(dir, "mod.ini"),
		dir,
		sections,
		nil,
		cache,
		timing,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 || timing.Geometries != 2 {
		t.Fatalf("meshes=%d geometry extractions=%d", len(meshes), timing.Geometries)
	}
	for index, mesh := range meshes {
		start := uint32(index * 3)
		if mesh.geometry.Position[0] != float32(start) ||
			!slices.Equal(mesh.geometry.SourceIndices, []uint32{start, start + 1, start + 2}) {
			t.Fatalf(
				"base vertex %d selected positions=%v sources=%v",
				start,
				mesh.geometry.Position,
				mesh.geometry.SourceIndices,
			)
		}
		// Tools that hook the draw read the picked face back from the index buffer.
		vertices, err := mesh.drawSource.TriangleVertices(0)
		if err != nil || mesh.drawSource.Section != []string{"Body", "Other"}[index] ||
			vertices != [3]uint32{start, start + 1, start + 2} {
			t.Fatalf("draw source %+v triangle=%v err=%v", mesh.drawSource, vertices, err)
		}
	}
}

// The liquid emitter hooks an implicit draw at its ib = line, since its auto
// draw line belongs to the IB override that every part of the hash runs.
func TestModelViewerMarksImplicitAutoDraws(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	sections := parseModINI(`[TextureOverrideBodyPosition]
hash = 11111111
vb0 = ResourcePos
[TextureOverrideBodyTexcoord]
hash = 22222222
vb1 = ResourceTc
[TextureOverrideBodyIB]
hash = 33333333
handling = skip
drawindexed = auto
[TextureOverrideBodyHead]
hash = 33333333
match_first_index = 0
ib = ResourceBodyIB
[TextureOverrideOther]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = auto
` + viewerBodyResources)
	cache := newModelViewerBufferCache(context.Background())
	defer cache.releaseAll()
	meshes, err := buildModelViewerDirectScannedMeshesAt(filepath.Join(dir, "mod.ini"), dir, sections, nil, cache, nil)
	if err != nil {
		t.Fatal(err)
	}
	implicit := map[string]bool{}
	for _, mesh := range meshes {
		if mesh.drawSource == nil || !mesh.drawSource.Auto {
			t.Fatalf("draw source of %s = %+v", mesh.component, mesh.drawSource)
		}
		implicit[mesh.drawSource.Section] = mesh.drawSource.Implicit
	}
	if len(implicit) != 2 || !implicit["BodyHead"] || implicit["Other"] {
		t.Fatalf("implicit by section = %v", implicit)
	}
}

// Frame-swap animation mods bind vb0 only from another hash's command list,
// so the scan finds no mesh and the legacy build must still name each frame's draw.
func TestModelViewerLegacyMeshKeepsDrawSource(t *testing.T) {
	dir := t.TempDir()
	const vertexCount = 6
	position := make([]byte, vertexCount*40)
	for vertex := range vertexCount {
		for axis := range 3 {
			binary.LittleEndian.PutUint32(position[vertex*40+axis*4:], math.Float32bits(float32(vertex)))
		}
	}
	for name, data := range map[string][]byte{
		"BodyPosition.buf": position,
		"BodyBlend.buf":    make([]byte, vertexCount*32),
		"BodyTexcoord.buf": make([]byte, vertexCount*24),
		"BodyA.ib":         modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	iniPath := filepath.Join(dir, "Animation.ini")
	sections := parseModINI(`[Constants]
global $swapvar = 0
[TextureOverrideBodyBlend]
hash = 11111111
run = CommandListBodyBlend
[TextureOverrideBodyTexcoord]
hash = 33333333
vb1 = ResourceBodyTexcoord.0
[TextureOverrideBodyA]
hash = 22222222
match_first_index = 0
handling = skip
if $swapvar == 0
ib = ResourceBodyAIB.0
drawindexed = 3, 3, 0
else
ib = ResourceBodyAIB.1
drawindexed = 3, 3, 0
endif
[CommandListBodyBlend]
if $swapvar == 0
handling = skip
if DRAW_TYPE == 2 || DRAW_TYPE == 4
vb1 = ResourceBodyTexcoord.0
vb2 = ResourceBodyBlend.0
checktextureoverride = ib
elif DRAW_TYPE == 1
vb0 = ResourceBodyPosition.0
vb2 = ResourceBodyBlend.0
Draw = 6, 0
endif
endif
[ResourceBodyPosition.0]
type = Buffer
stride = 40
filename = BodyPosition.buf
[ResourceBodyBlend.0]
type = Buffer
stride = 32
filename = BodyBlend.buf
[ResourceBodyTexcoord.0]
type = Buffer
stride = 24
filename = BodyTexcoord.buf
[ResourceBodyAIB.0]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = BodyA.ib
[ResourceBodyAIB.1]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = BodyA.ib
`)
	resources := resolveModelViewerEffectiveResourcesAt(dir, dir, sections, collectModelViewerResources(sections), nil)
	cache := newModelViewerBufferCache(context.Background())
	defer cache.releaseAll()

	scanned, _, err := buildModelViewerDirectScannedMeshesPrepared(
		iniPath, dir, sections, collectModelViewerDefaultVariables(sections), resources, cache, nil,
	)
	if err != nil || len(scanned) != 0 {
		t.Fatalf("fixture must take the legacy build: scanned=%d err=%v", len(scanned), err)
	}
	meshes, _, _, _, _, err := buildModelViewerDirectMeshesPrepared(
		iniPath,
		dir,
		"",
		sections,
		resources,
		nil,
		cache,
		nil,
	)
	if err != nil || len(meshes) != 2 {
		t.Fatalf("meshes=%d err=%v", len(meshes), err)
	}

	// Each frame's draw is numbered within its own index buffer, so the IDs
	// must still differ or one frame's draw would replace the other's.
	if meshes[0].id == meshes[1].id {
		t.Fatalf("both frames have mesh ID %q", meshes[0].id)
	}
	mesh := meshes[0]
	source := mesh.drawSource
	if source == nil || source.INIPath != iniPath || source.Section != "BodyA" ||
		!source.MatchesIB("ResourceBodyAIB.0") || source.StartIndex != 3 || source.IndexCount != 3 {
		t.Fatalf("draw source = %+v", source)
	}
	// The viewer picks faces as source vertices; the hook reads them back.
	picked := [3]uint32{}
	for corner := range picked {
		picked[corner] = mesh.geometry.SourceIndices[mesh.geometry.Indices[corner]]
	}
	vertices, err := source.TriangleVertices(0)
	if err != nil || vertices != picked || picked != [3]uint32{3, 4, 5} {
		t.Fatalf("triangle=%v picked=%v err=%v", vertices, picked, err)
	}
}

func TestModelViewerSharedGeometryDistinguishesResourceFormats(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideOther]
ib = ResourceShortIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceShortIB]
filename = body.ib
format = DXGI_FORMAT_R16_UINT
` + viewerBodyResources)
	cache := newModelViewerBufferCache(context.Background())
	defer cache.releaseAll()
	timing := &modelViewerMeshBuildTiming{}
	meshes, err := buildModelViewerDirectScannedMeshesAt(
		filepath.Join(dir, "mod.ini"),
		dir,
		sections,
		nil,
		cache,
		timing,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 || timing.Geometries != 2 {
		t.Fatalf("meshes=%d geometry extractions=%d", len(meshes), timing.Geometries)
	}
	if !slices.Equal(meshes[0].geometry.SourceIndices, []uint32{0, 1, 2}) ||
		!slices.Equal(meshes[1].geometry.SourceIndices, []uint32{0, 1}) {
		t.Fatalf(
			"resource formats shared decoded data: %v / %v",
			meshes[0].geometry.SourceIndices,
			meshes[1].geometry.SourceIndices,
		)
	}
}

func TestModelViewerSameSectionGeometryPreservesConditionalDraws(t *testing.T) {
	for _, tc := range []struct {
		name    string
		draws   string
		sources [][]uint32
	}{
		{
			name: "base vertices",
			draws: `ib = ResourceBodyIB
if $swap == 0
drawindexed = 3, 0, 0
else
drawindexed = 3, 0, 3
endif`,
			sources: [][]uint32{{0, 1, 2}, {3, 4, 5}},
		},
		{
			name: "index formats",
			draws: `if $swap == 0
ib = ResourceBodyIB
else
ib = ResourceShortIB
endif
drawindexed = 3, 0, 0`,
			sources: [][]uint32{{0, 1, 2}, {0, 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometry(t, dir)
			sections := parseModINI(`[TextureOverrideBody]
vb0 = ResourcePos
vb1 = ResourceTc
` + tc.draws + `
[ResourceShortIB]
filename = body.ib
format = DXGI_FORMAT_R16_UINT
` + viewerBodyResources)
			cache := newModelViewerBufferCache(context.Background())
			defer cache.releaseAll()
			timing := &modelViewerMeshBuildTiming{}
			meshes, err := buildModelViewerDirectScannedMeshesAt(
				filepath.Join(dir, "mod.ini"), dir, sections, nil, cache, timing,
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(meshes) != 2 || timing.Geometries != 2 {
				t.Fatalf("meshes=%d geometry extractions=%d", len(meshes), timing.Geometries)
			}
			if meshes[0].id == meshes[1].id || meshes[0].geometry == meshes[1].geometry {
				t.Fatal("different conditional draws share mesh identity or geometry")
			}
			for index, mesh := range meshes {
				if mesh.component != "Body" || !slices.Equal(mesh.geometry.SourceIndices, tc.sources[index]) {
					t.Fatalf("mesh %d component=%s sources=%v", index, mesh.component, mesh.geometry.SourceIndices)
				}
				for _, swap := range []string{"0", "1"} {
					if modelViewerDNFSatisfied(mesh.conditions, map[string]string{"swap": swap}) !=
						(swap == modelViewerString(index)) {
						t.Fatalf("mesh %d changed visibility at swap %s", index, swap)
					}
				}
			}
		})
	}
}

func TestModelViewerScannerForksConditionalBufferState(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $swap = 0
[KeySwap]
type = cycle
$swap = 0,1
[TextureOverrideBody]
vb1 = ResourceTc
if $swap == 0
ib = ResourceBodyIB
vb0 = ResourcePos
else
ib = ResourceAltIB
vb0 = ResourceAltPos
endif
drawindexed = 3, 0, 0`)
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v", records)
	}
	states := map[string]bool{}
	for _, record := range records {
		states[record.state.ib+":"+record.state.vb0] = true
		if len(record.conditions) != 1 || len(record.conditions[0]) != 1 || record.conditions[0][0].Var != "swap" {
			t.Fatalf("conditions = %#v", record.conditions)
		}
	}
	if !states["BodyIB:Pos"] || !states["AltIB:AltPos"] ||
		records[0].conditions[0][0].Negate == records[1].conditions[0][0].Negate {
		t.Fatalf("states=%v records=%#v", states, records)
	}
}

func TestModelViewerWWMIDirectGeometryUsesVectorAndVB2Texcoord(t *testing.T) {
	dir := t.TempDir()
	iniText := `[Constants]
global $required_wwmi_version = 0.70
global persist $color = 1
[KeyColor]
type = cycle
$color = 1,2,3
[TextureOverrideNormalComponent3]
run = CommandListOverrideSharedResources
drawindexed = 3, 0, 0
[CommandListOverrideSharedResources]
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceVectorBuffer
vb2 = ResourceTexCoordBuffer
ps-t0 = ResourceC1
ps-t1 = ResourceB1
ps-t2 = ResourceD1
ps-t3 = ResourceA1
if $color == 2
ps-t3 = ResourceA2
elif $color == 3
ps-t3 = ResourceA3
endif
[ResourceC1]
filename = Textures/C1.dds
[ResourceB1]
filename = Textures/B1.dds
[ResourceA1]
filename = Textures/A1.dds
[ResourceA2]
filename = Textures/A2.dds
[ResourceA3]
filename = Textures/A3.dds
[ResourceD1]
filename = Textures/D1.dds
[ResourceIndexBuffer]
filename = index.buf
format = DXGI_FORMAT_R32_UINT
[ResourcePositionBuffer]
filename = position.buf
format = DXGI_FORMAT_R32G32B32_FLOAT
stride = 12
[ResourceVectorBuffer]
filename = vector.buf
format = DXGI_FORMAT_R8G8B8A8_SNORM
stride = 8
[ResourceTexCoordBuffer]
filename = texcoord.buf
format = DXGI_FORMAT_R16G16_FLOAT
stride = 16`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	positions := make([]byte, 3*12)
	for vertex, position := range [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}} {
		for component, value := range position {
			binary.LittleEndian.PutUint32(positions[vertex*12+component*4:], math.Float32bits(value))
		}
	}
	vectors := make([]byte, 3*8)
	for vertex := range 3 {
		copy(vectors[vertex*8:], []byte{127, 0, 0, 127, 0, 0, 127, 129})
	}
	texcoords := make([]byte, 3*16)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(texcoords[vertex*16+4:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(texcoords[vertex*16+8:], math.Float32bits(uv[1]))
	}
	for name, data := range map[string][]byte{
		"index.buf":    modelViewerUint32Bytes([]uint32{0, 1, 2}),
		"position.buf": positions,
		"vector.buf":   vectors,
		"texcoord.buf": texcoords,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	available := make(map[string]modelViewerTexturePayload)
	for _, name := range []string{"C1", "B1", "A1", "A2", "A3", "D1"} {
		format := uint32(99)
		if name == "C1" || name == "B1" {
			format = 98
		}
		writeTextureFile(t, dir, "Textures/"+name+".dds", encodeDX10HeaderDDS(8192, 8192, format))
		available[modelViewerNormalizeKey(name)] = modelViewerTexturePayload{Key: name}
	}
	sections := parseModINI(iniText)
	cache := newModelViewerBufferCache(context.Background())
	defer cache.releaseAll()
	meshes, err := buildModelViewerDirectScannedMeshesAt(
		iniPath,
		dir,
		sections,
		collectModelViewerDefaultVariables(sections),
		cache,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 {
		t.Fatalf("meshes = %#v", meshes)
	}
	geometry := meshes[0].geometry
	if !slices.Equal(geometry.Indices, []uint32{0, 2, 1}) {
		t.Fatalf("indices = %v", geometry.Indices)
	}
	wantUVs := []float32{0, .75, 1, .25, .5, .5}
	if !slices.Equal(geometry.Texcoord0, wantUVs) {
		t.Fatalf("uvs = %v, want %v", geometry.Texcoord0, wantUVs)
	}
	if !slices.Equal(geometry.Normal, []float32{0, 0, 1, 0, 0, 1, 0, 0, 1}) {
		t.Fatalf("normals = %v", geometry.Normal)
	}
	if len(geometry.Tangent) != 0 {
		t.Fatalf("WWMI tangent should use Three.js derivative frame: %v", geometry.Tangent)
	}
	item, _ := buildModelViewerDirectMeshPayload(meshes[0], nil, available, nil, cache)
	for _, value := range []string{"1", "2", "3"} {
		if got := viewerEvalResolveTexture(
			item.TextureVariants,
			item.TexKey,
			map[string]any{"color": value},
		); got != "A"+value {
			t.Fatalf("color=%s texture = %q, want A%s", value, got, value)
		}
	}

	t.Run("scoped suffixed buffers", func(t *testing.T) {
		scoped, _ := scopeModelViewerSections(parseModINI(strings.ReplaceAll(iniText, "Buffer", "Buffer.0")), 0, "", "")
		meshes, err := buildModelViewerDirectScannedMeshesAt(
			iniPath,
			dir,
			scoped,
			collectModelViewerDefaultVariables(scoped),
			cache,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(meshes) != 1 {
			t.Fatalf("meshes = %#v", meshes)
		}
		if got := meshes[0].geometry.Texcoord0; !slices.Equal(got, wantUVs) {
			t.Fatalf("scoped WWMI uvs = %v, want %v", got, wantUVs)
		}
	})
}

func TestModelViewerScannedMeshesSkipInterleaveValidationErrors(t *testing.T) {
	dir := t.TempDir()
	iniText := `[TextureOverrideBody]
ib = ResourceIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceIB]
filename = index.buf
format = DXGI_FORMAT_R32_UINT
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"index.buf": modelViewerUint32Bytes([]uint32{0, 1, 2}),
		"pos.buf":   make([]byte, 41),
		"tc.buf":    make([]byte, 20),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshesAt(
		iniPath,
		dir,
		sections,
		collectModelViewerDefaultVariables(sections),
		newModelViewerBufferCache(context.Background()),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 0 {
		t.Fatalf("meshes = %#v", meshes)
	}
}

func TestModelViewerScannedMeshesRejectIndexBufferOutsideModFolder(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	name := filepath.Base(dir) + "-outside.ib"
	outside := filepath.Join(filepath.Dir(dir), name)
	if err := os.WriteFile(outside, modelViewerUint32Bytes([]uint32{0, 1, 2}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	iniText := `[TextureOverrideBody]
ib = ResourceEscapingIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceEscapingIB]
filename = ..\` + name + `
format = DXGI_FORMAT_R32_UINT
` + viewerBodyResources
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshesAt(
		iniPath,
		dir,
		sections,
		collectModelViewerDefaultVariables(sections),
		newModelViewerBufferCache(context.Background()),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 0 {
		t.Fatalf("meshes = %#v", meshes)
	}
}

func TestModelViewerScannedMeshesSkipUndecodableIndexBuffer(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "broken.ib"), make([]byte, 5), 0o600); err != nil {
		t.Fatal(err)
	}
	iniText := `[TextureOverrideBroken]
ib = ResourceBrokenIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceBrokenIB]
filename = broken.ib
format = DXGI_FORMAT_R32_UINT
` + viewerBodyResources
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	cache := newModelViewerBufferCache(context.Background())
	defer cache.releaseAll()

	meshes, err := buildModelViewerDirectScannedMeshesAt(
		iniPath,
		dir,
		sections,
		collectModelViewerDefaultVariables(sections),
		cache,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 || meshes[0].drawSource == nil || meshes[0].drawSource.Section != "Body" {
		t.Fatalf("meshes = %#v", meshes)
	}

	var output strings.Builder
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	cache.diagnostics.Report(log, "StaticGlb.loadForViewer", "model-viewer-buffers")
	for _, want := range []string{"BrokenIB", "broken.ib", "DXGI_FORMAT_R32_UINT"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("report lacks %q: %s", want, output.String())
		}
	}
}

func TestModelViewerScannedMeshesPropagatePairedReadErrors(t *testing.T) {
	dir := t.TempDir()
	iniText := `[TextureOverrideBody]
ib = ResourceIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourceIB]
filename = index.buf
format = DXGI_FORMAT_R32_UINT
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "index.buf"),
		modelViewerUint32Bytes([]uint32{0, 1, 2}),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tc.buf"), make([]byte, 20), 0o600); err != nil {
		t.Fatal(err)
	}
	sections := parseModINI(iniText)
	if _, err := buildModelViewerDirectScannedMeshesAt(
		iniPath,
		dir,
		sections,
		collectModelViewerDefaultVariables(sections),
		newModelViewerBufferCache(context.Background()),
		nil,
	); err == nil {
		t.Fatal("missing position buffer succeeded")
	}
}

func TestModelViewerScannerKeepsReachableElseBuffersAfterIf0(t *testing.T) {
	sections := parseModINI(`[Constants]
global persist $swap = 0
[KeySwap]
type = cycle
$swap = 0,1
[TextureOverrideBody]
if 0
ib = ResourceDeadIB
else
if $swap == 0
ib = ResourceBodyIB
else
ib = ResourceAltIB
endif
endif
vb0 = ResourcePos
vb1 = ResourceTc
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceDeadIB]
filename = dead.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceAltIB]
filename = alt.ib
format = DXGI_FORMAT_R32_UINT`)
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %#v", records)
	}
	ibs := map[string]bool{}
	for _, record := range records {
		ibs[record.state.ib] = true
		if record.state.ib == "DeadIB" || modelViewerDNFIsTrue(record.conditions) {
			t.Fatalf("dead or unconstrained record = %#v", record)
		}
	}
	if !ibs["BodyIB"] || !ibs["AltIB"] {
		t.Fatalf("ibs = %v records=%#v", ibs, records)
	}
}

func TestModelViewerScannerPrunesConstantFalseAssignments(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if 0
ib = ResourceDeadIB
vb0 = ResourceDeadPos
vb1 = ResourceDeadTc
endif
drawindexed = 3, 0, 0`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].state.ib != "BodyIB" || records[0].state.vb0 != "Pos" ||
		records[0].state.vb1 != "Tc" ||
		len(records[0].draw.Conditions) != 0 {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerScannerDoesNotForkIndependentDrawGuards(t *testing.T) {
	var ini strings.Builder
	ini.WriteString(`[TextureOverrideBody]
ib = ResourceIB
vb0 = ResourcePos
vb1 = ResourceTc
`)
	for index := range 13 {
		fmt.Fprintf(&ini, "if $choice%d == 1\ndrawindexed = 3, 0, 0\nendif\n", index)
	}
	ini.WriteString("drawindexed = 6, 0, 0\n")
	records, err := collectModelViewerDirectDrawRecords(parseModINI(ini.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 14 {
		t.Fatalf("records = %d", len(records))
	}
	unconditional := 0
	for _, record := range records {
		if record.state.ib != "IB" {
			t.Fatalf("record = %#v", record)
		}
		if modelViewerDNFIsTrue(record.conditions) {
			unconditional++
			if record.draw.IndexCount != 6 {
				t.Fatalf("unconditional record = %#v", record)
			}
		}
	}
	if unconditional != 1 {
		t.Fatalf("unconditional = %d records=%#v", unconditional, records)
	}
}

func TestModelViewerScannerIgnoresCommandListVariableGuards(t *testing.T) {
	var ini strings.Builder
	ini.WriteString(`[Constants]
global persist $hair = 0
global persist $bodytoggle = 0
global persist $pregnant = 0
[KeyHair]
type = cycle
$hair = 0,1
[KeyBody]
type = cycle
$bodytoggle = 0,1,2
[KeyPregnant]
type = cycle
$pregnant = 0,1
[CommandListTedom]
if $bodytoggle == 0
	$nudebody = 1
elif $bodytoggle == 1
	$nudebody = 0
elif $bodytoggle == 2
	$nudebody = 1
endif
if $pregnant == 1
	$nudebody = 0
endif
[TextureOverrideBody]
ib = ResourceIB
vb0 = ResourcePos
vb1 = ResourceTc
if $hair == 0
ps-t1 = ResourceDiffuseA
else
ps-t1 = ResourceDiffuseB
endif
run = CommandListTedom
`)
	for index := range 13 {
		fmt.Fprintf(&ini, "if $choice%d == 1\ndrawindexed = 3, 0, 0\nendif\n", index)
	}
	ini.WriteString("[ResourceDiffuseA]\n[ResourceDiffuseB]\n")
	records, err := collectModelViewerDirectDrawRecords(
		parseModINI(ini.String()),
		collectModelViewerDefaultVariables(parseModINI(ini.String())),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 13 {
		t.Fatalf("records = %d", len(records))
	}
}

func TestModelViewerScannerDoesNotExpandLuciaTextureConditionsIntoDraws(t *testing.T) {
	var ini strings.Builder
	ini.WriteString(`[Constants]
global persist $swapvarColor = 0
[KeyColor]
type = cycle
$swapvarColor = 0,1,2,3
[ResourceBase]
[ResourceRed]
[ResourceBlack]
[ResourceGold]
[CommandListRunSlotFix]
`)
	for _, slot := range []string{"ps-t9", "ps-t3"} {
		for color, resource := range []string{"Base", "Red", "Black", "Gold"} {
			fmt.Fprintf(&ini, "if $swapvarColor == %d\n%s = Resource%s\nendif\n", color, slot, resource)
		}
	}
	for _, group := range []struct {
		name  string
		draws int
	}{{"HairA", 4}, {"HairB", 3}, {"BodyA", 40}, {"BodyB", 14}} {
		fmt.Fprintf(
			&ini,
			"[TextureOverride%s]\nib = Resource%sIB\nrun = CommandListRunSlotFix\nrun = CommandList%s\n",
			group.name,
			group.name,
			group.name,
		)
		fmt.Fprintf(&ini, "[CommandList%s]\n", group.name)
		for index := range group.draws {
			fmt.Fprintf(&ini, "drawindexed = 3, %d, 0\n", index*3)
		}
	}
	records, err := collectModelViewerDirectDrawRecords(parseModINI(ini.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 61 {
		t.Fatalf("records = %d, want 61", len(records))
	}
	for _, record := range records {
		if len(record.nonDiffuse) != 4 {
			t.Fatalf("non-diffuse resources = %#v", record.nonDiffuse)
		}
	}
}

func TestModelViewerScannerRejectsNegativeDrawsAndAcceptsAuto(t *testing.T) {
	sections := parseModINI(`[Constants]
global $n = -5
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = -3, 0, 0
drawindexed = 3, -1, 0
drawindexed = 3, 0, -2
drawindexed = $n, 0, 0
drawindexed = 3, $n, 0
drawindexed = 3, 0, $n
drawindexed = 3, 0, 0
drawindexed = auto`)
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].auto || !records[1].auto || records[0].draw.IndexCount != 3 {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerScannerRejectsArithmeticDrawArguments(t *testing.T) {
	sections := parseModINI(`[Constants]
global $n = 3
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = $n * 2, 0, 0
drawindexed = 3 + 3, 0, 0
drawindexed = $n, 0, 0`)
	records, err := collectModelViewerDirectDrawRecords(sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].draw.IndexCount != 3 {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerScannerTreatsNonVariableExpressionAtomAsTrue(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if 1 == 0
drawindexed = 3, 0, 0
endif`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !modelViewerDNFIsTrue(modelViewerConditionsToDNF(records[0].draw.Conditions, nil)) {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerScannerBoundsAndDeduplicatesRunExpansion(t *testing.T) {
	const depth = 12
	var ini strings.Builder
	for index := range depth {
		next := fmt.Sprintf("CommandListExp%d", index+1)
		fmt.Fprintf(&ini, "[CommandListExp%d]\nrun = %s\nrun = %s\n", index, next, next)
	}
	fmt.Fprintf(&ini, "[CommandListExp%d]\ndrawindexed = auto\n", depth)
	ini.WriteString(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
run = CommandListExp0`)
	records, err := collectModelViewerDirectDrawRecords(parseModINI(ini.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !records[0].auto {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerScannerBuildsEachConditionalGeometry(t *testing.T) {
	dir := t.TempDir()
	iniText := `[Constants]
global persist $swap = 0
[KeySwap]
type = cycle
$swap = 0,1
[TextureOverrideBody]
vb1 = ResourceTc
if $swap == 0
ib = ResourceBodyIB
vb0 = ResourcePos
else
ib = ResourceAltIB
vb0 = ResourceAltPos
endif
drawindexed = auto
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceAltPos]
filename = alt-pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceAltIB]
filename = alt.ib
format = DXGI_FORMAT_R32_UINT`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	writePosition := func(name string, firstX float32) {
		data := make([]byte, 3*40)
		for vertex := range 3 {
			binary.LittleEndian.PutUint32(data[vertex*40:], math.Float32bits(firstX+float32(vertex)))
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writePosition("pos.buf", 1)
	writePosition("alt-pos.buf", 100)
	tc := make([]byte, 3*20)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(tc[vertex*20+4:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(tc[vertex*20+8:], math.Float32bits(uv[1]))
	}
	if err := os.WriteFile(filepath.Join(dir, "tc.buf"), tc, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"body.ib", "alt.ib"} {
		if err := os.WriteFile(filepath.Join(dir, name), modelViewerUint32Bytes([]uint32{0, 1, 2}), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sections := parseModINI(iniText)
	resources := resolveModelViewerEffectiveResourcesAt(dir, dir, sections, collectModelViewerResources(sections), nil)
	cache := newModelViewerBufferCache(context.Background())
	meshes, _, err := buildModelViewerDirectScannedMeshesPrepared(
		iniPath, dir, sections, collectModelViewerDefaultVariables(sections), resources, cache, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 {
		t.Fatalf("meshes = %#v", meshes)
	}
	// Frame-swap mods bind one buffer set per frame; holding every frame's source data until the load ends
	// doubled the load's peak memory.
	if len(cache.files) != 0 || len(cache.indices) != 0 || len(cache.pairs) != 0 {
		t.Fatalf("buffers outlived their draws: files=%d indices=%d pairs=%d",
			len(cache.files), len(cache.indices), len(cache.pairs))
	}
	xs := []float32{meshes[0].geometry.Position[0], meshes[1].geometry.Position[0]}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	if xs[0] != 1 || xs[1] != 100 || len(meshes[0].conditions) != 1 || len(meshes[1].conditions) != 1 {
		t.Fatalf("xs=%v conditions=%#v/%#v", xs, meshes[0].conditions, meshes[1].conditions)
	}
}

func TestModelViewerMergedCommandListsKeepVariantBuffersAndConditions(t *testing.T) {
	dir := t.TempDir()
	iniText := `[Constants]
global persist $swapvar = 0
[KeySwap]
type = cycle
$swapvar = 0,1
[TextureOverrideCharBPosition]
run = CommandListCharBPosition
[TextureOverrideCharBBlend]
run = CommandListCharBBlend
[TextureOverrideCharBTexcoord]
run = CommandListCharBTexcoord
[TextureOverrideCharBIB]
run = CommandListCharBIB
[CommandListCharBPosition]
if $swapvar == 0
vb0 = ResourceCharBPosition.0
else if $swapvar == 1
vb0 = ResourceCharBPosition.1
endif
[CommandListCharBBlend]
if $swapvar == 0
vb1 = ResourceCharBBlend.0
else if $swapvar == 1
vb1 = ResourceCharBBlend.1
endif
[CommandListCharBTexcoord]
if $swapvar == 0
vb1 = ResourceCharBTexcoord.0
else if $swapvar == 1
vb1 = ResourceCharBTexcoord.1
endif
[CommandListCharBIB]
if $swapvar == 0
ib = ResourceCharBHeadIB.0
drawindexed = auto
else if $swapvar == 1
ib = ResourceCharBHeadIB.1
drawindexed = auto
endif
[ResourceCharBPosition.0]
stride = 40
filename = .\1. charbmain\CharBPosition.buf
[ResourceCharBBlend.0]
stride = 32
filename = .\1. charbmain\CharBBlend.buf
[ResourceCharBTexcoord.0]
stride = 20
filename = .\1. charbmain\CharBTexcoord.buf
[ResourceCharBHeadIB.0]
format = DXGI_FORMAT_R32_UINT
filename = .\1. charbmain\CharBHead.ib
[ResourceCharBPosition.1]
stride = 40
filename = .\2. charbbody\CharBPosition.buf
[ResourceCharBBlend.1]
stride = 32
filename = .\2. charbbody\CharBBlend.buf
[ResourceCharBTexcoord.1]
stride = 20
filename = .\2. charbbody\CharBTexcoord.buf
[ResourceCharBHeadIB.1]
format = DXGI_FORMAT_R32_UINT
filename = .\2. charbbody\CharBHead.ib`
	iniPath := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	writeVariant := func(folder string, axis float32) {
		variantDir := filepath.Join(dir, folder)
		if err := os.MkdirAll(variantDir, 0o755); err != nil {
			t.Fatal(err)
		}
		position := make([]byte, 3*40)
		texcoord := make([]byte, 3*20)
		for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
			binary.LittleEndian.PutUint32(position[vertex*40:], math.Float32bits(axis))
			binary.LittleEndian.PutUint32(position[vertex*40+4:], math.Float32bits(float32(vertex)))
			binary.LittleEndian.PutUint32(texcoord[vertex*20+4:], math.Float32bits(uv[0]))
			binary.LittleEndian.PutUint32(texcoord[vertex*20+8:], math.Float32bits(uv[1]))
		}
		files := map[string][]byte{
			"CharBPosition.buf": position,
			"CharBBlend.buf":    make([]byte, 3*32),
			"CharBTexcoord.buf": texcoord,
			"CharBHead.ib":      modelViewerUint32Bytes([]uint32{0, 1, 2}),
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(variantDir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeVariant("1. charbmain", 1)
	writeVariant("2. charbbody", 100)
	sections := parseModINI(iniText)
	meshes, _, _, _, err := buildModelViewerDirectMeshes(iniPath, "", sections)
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 {
		t.Fatalf("meshes = %#v", meshes)
	}
	xs := []float32{meshes[0].geometry.Position[0], meshes[1].geometry.Position[0]}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	if xs[0] != 1 || xs[1] != 100 || modelViewerDNFIsTrue(meshes[0].conditions) ||
		modelViewerDNFIsTrue(meshes[1].conditions) {
		t.Fatalf("xs=%v conditions=%#v/%#v", xs, meshes[0].conditions, meshes[1].conditions)
	}
}

func TestModelViewerTextureAssignmentsPreserveLaterWritesAndAuxVariants(t *testing.T) {
	dir := t.TempDir()
	iniText := `[Constants]
global persist $color = 0
global persist $detail = 0
global persist $metal = 0
[KeyColor]
type = cycle
$color = 0,1,2
[KeyDetail]
type = cycle
$detail = 0,1
[KeyMetal]
type = cycle
$metal = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\GIMI\Diffuse = ref ResourceDiffuseA
if $color == 1
Resource\GIMI\Diffuse = ref ResourceDiffuseB
endif
if $color == 2
Resource\GIMI\Diffuse = ref ResourceDiffuseC
endif
if $detail == 0
Resource\ZZMI\NormalMap = ref ResourceNormalA
else
Resource\ZZMI\NormalMap = ref ResourceNormalB
endif
Resource\ZZMI\LightMap = ref ResourceLight
if $metal == 1
Resource\ZZMI\MaterialMap = ref ResourceMaterial
endif
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceDiffuseA]
filename = diffuseA.png
[ResourceDiffuseB]
filename = diffuseB.png
[ResourceDiffuseC]
filename = diffuseC.png
[ResourceNormalA]
filename = normalA.png
[ResourceNormalB]
filename = normalB.png
[ResourceLight]
filename = light.png
[ResourceMaterial]
filename = material.png
[ResourceUnused]
filename = unused.png`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(iniText), 0o600); err != nil {
		t.Fatal(err)
	}
	position := make([]byte, 3*40)
	texcoord := make([]byte, 3*20)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(position[vertex*40:], math.Float32bits(float32(vertex)))
		binary.LittleEndian.PutUint32(texcoord[vertex*20+4:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(texcoord[vertex*20+8:], math.Float32bits(uv[1]))
	}
	for name, data := range map[string][]byte{"pos.buf": position, "tc.buf": texcoord, "body.ib": modelViewerUint32Bytes([]uint32{0, 1, 2})} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	preview := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	preview.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	for _, name := range []string{"diffuseA.png", "diffuseB.png", "diffuseC.png", "normalA.png", "normalB.png", "light.png", "material.png", "unused.png"} {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(file, preview); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	result, err := service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	mesh := result.Meshes[0]
	if result.MaterialProfile != "zzmi" || len(mesh.TextureVariants) != 3 || len(mesh.NormalMapVariants) != 2 ||
		mesh.LightMapKey == nil ||
		len(mesh.MaterialMapVariants) != 1 {
		t.Fatalf("profile=%q mesh=%#v", result.MaterialProfile, mesh)
	}
	if _, prepared := result.Textures[modelViewerNormalizeKey("ResourceUnused")]; prepared {
		t.Fatalf("unreferenced texture was prepared: %#v", result.Textures)
	}
	if removed, cleanupErr := service.CleanupModelViewer(
		context.Background(),
		result.MemorySessionID,
	); cleanupErr != nil ||
		!removed {
		t.Fatalf("cleanup=%v, %v", removed, cleanupErr)
	}
}
