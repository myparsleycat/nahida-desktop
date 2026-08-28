package tools

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

const viewerBodyResources = `
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
`

func TestLoadModViewerLoadsINIBuffersAndTexturesWithoutAssetLayout(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "diffuse.png", encodeTinyPNG())
	fixture := loadViewerFixture(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\GIMI\Diffuse = ref ResourceDiffuse
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceDiffuse]
filename = diffuse.png
`)
	if len(fixture.result.Meshes) < 1 {
		t.Fatalf("meshes = %#v", fixture.result.Meshes)
	}
	mesh := fixture.result.Meshes[0]
	positions := readViewerFloat32s(t, fixture.protocol, mesh.PositionsURL)
	indices := readViewerUint32s(t, fixture.protocol, mesh.IndicesURL)
	if len(positions) != 9 || len(indices) != 3 || indices[0] != 0 || indices[1] != 1 || indices[2] != 2 {
		t.Fatalf("positions=%v indices=%v", positions, indices)
	}
	if texKey(mesh) == "" || fixture.result.Textures[texKey(mesh)].Role != "diffuse" {
		t.Fatalf("texKey=%q textures=%#v", texKey(mesh), fixture.result.Textures)
	}
}

func TestLoadModViewerInitializesEmptyTransportCollections(t *testing.T) {
	dir := t.TempDir()
	result := loadViewerMod(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
`+viewerBodyResources)

	if result.Meshes == nil || result.Textures == nil || result.Variables == nil || result.DefaultState == nil || result.StateRules == nil || result.Animations == nil {
		t.Fatalf("transport contains nil collections: %#v", result)
	}
}

func TestLoadModViewerIgnoresRuntimeGuardsButKeepsToggleConditions(t *testing.T) {
	dir := t.TempDir()
	result := loadViewerMod(t, dir, `[Constants]
global $mod_enabled = 0
global persist $outfit = 0
[KeyOutfit]
type = cycle
$outfit = 0, 1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $mod_enabled
if $outfit == 0
drawindexed = 3, 0, 0
endif
endif
`+viewerBodyResources)

	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	wanted := ModelViewerDNF{{{Var: "outfit", Value: "0"}}}
	if !reflect.DeepEqual(result.Meshes[0].Conditions, wanted) {
		t.Fatalf("conditions = %#v", result.Meshes[0].Conditions)
	}
	if evaluated := evaluateViewerTransport(result, nil); !evaluated.Meshes[0].Visible {
		t.Fatalf("default mesh is hidden: %#v", evaluated)
	}
	if evaluated := evaluateViewerTransport(result, map[string]any{"outfit": "1"}); evaluated.Meshes[0].Visible {
		t.Fatalf("toggle condition was discarded: %#v", evaluated)
	}
}

func TestLoadModViewerLoadsMeshINIsFromSubfolders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte("[Constants]\nglobal $swap = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "body"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "body", "body.ini"), []byte(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = ../pos.buf
stride = 40
[ResourceTc]
filename = ../tc.buf
stride = 20
[ResourceBodyIB]
filename = ../body.ib
format = DXGI_FORMAT_R32_UINT
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) < 1 {
		t.Fatalf("meshes = %#v", fixture.result.Meshes)
	}
	positions := readViewerFloat32s(t, fixture.protocol, fixture.result.Meshes[0].PositionsURL)
	indices := readViewerUint32s(t, fixture.protocol, fixture.result.Meshes[0].IndicesURL)
	if len(positions) != 9 || len(indices) != 3 {
		t.Fatalf("positions=%v indices=%v", positions, indices)
	}
}

func TestLoadModViewerKeepsMidSectionIBReassignmentAsTwoMeshes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[TextureOverrideBodyBlend]
ib = ResourceBodyHeadIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
ib = ResourceBodyDressIB
drawindexed = 3, 0, 0
[ResourceBodyHeadIB]
filename = head.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyDressIB]
filename = dress.ib
format = DXGI_FORMAT_R32_UINT
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometryN(t, dir, 32)
	if err := os.WriteFile(filepath.Join(dir, "head.ib"), modelViewerUint32Bytes([]uint32{10, 11, 12}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dress.ib"), modelViewerUint32Bytes([]uint32{20, 21, 22}), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 2 {
		t.Fatalf("meshes = %#v", fixture.result.Meshes)
	}
	sets := make([][]int, 2)
	for index, mesh := range fixture.result.Meshes {
		positions := readViewerFloat32s(t, fixture.protocol, mesh.PositionsURL)
		indices := readViewerUint32s(t, fixture.protocol, mesh.IndicesURL)
		values := make([]int, len(indices))
		for i, vertex := range indices {
			values[i] = int(math.Round(float64(positions[vertex*3])))
		}
		sortInts(values)
		sets[index] = values
	}
	if sets[0][0] > sets[1][0] {
		sets[0], sets[1] = sets[1], sets[0]
	}
	if len(sets[0]) != 3 || sets[0][0] != 10 || sets[0][1] != 11 || sets[0][2] != 12 ||
		len(sets[1]) != 3 || sets[1][0] != 20 || sets[1][1] != 21 || sets[1][2] != 22 {
		t.Fatalf("vertex sets = %#v", sets)
	}
}

func TestLoadModViewerHidesAndShowsCycleDNFDraws(t *testing.T) {
	dir := t.TempDir()
	fixture := loadViewerFixture(t, dir, `[Constants]
global $outfit = 0
[KeyOutfit]
type = cycle
$outfit = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $outfit == 0
drawindexed = 3, 0, 0
else
drawindexed = 3, 3, 0
endif
`+viewerBodyResources)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture = loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 2 {
		t.Fatalf("meshes = %#v", fixture.result.Meshes)
	}
	first := evaluateViewerTransport(fixture.result, map[string]any{"outfit": "0"})
	second := evaluateViewerTransport(fixture.result, map[string]any{"outfit": "1"})
	if !first.Meshes[0].Visible || first.Meshes[1].Visible || second.Meshes[0].Visible || !second.Meshes[1].Visible {
		t.Fatalf("first=%#v second=%#v", first.Meshes, second.Meshes)
	}
}

func TestLoadModViewerResolvesLaterWriteWinsDiffuseByState(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"diffuseA.png", "diffuseB.png", "diffuseC.png", "normalA.png", "normalB.png", "light.png", "material.png"} {
		writeTextureFile(t, dir, name, encodeTinyPNG())
	}
	result := loadViewerMod(t, dir, `[Constants]
global persist $color = 0
global $detail = 0
global $metal = 0
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
`+viewerBodyResources+`
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
`)
	if result.MaterialProfile != "zzmi" || len(result.Meshes) != 1 {
		t.Fatalf("result = %#v", result)
	}
	mesh := result.Meshes[0]
	if len(mesh.TextureVariants) < 3 || len(mesh.NormalMapVariants) < 2 || mesh.LightMapKey == nil || !strings.HasSuffix(*mesh.LightMapKey, "light.png") || len(mesh.MaterialMapVariants) < 1 || len(mesh.MaterialMapVariants[0].Conditions) == 0 {
		t.Fatalf("mesh = %#v", mesh)
	}
	color0 := evaluateViewerTransport(result, map[string]any{"color": "0", "detail": "0", "metal": "0"})
	color1 := evaluateViewerTransport(result, map[string]any{"color": "1", "detail": "1", "metal": "1"})
	color2 := evaluateViewerTransport(result, map[string]any{"color": "2", "detail": "0", "metal": "0"})
	if !strings.HasSuffix(color0.Meshes[0].TexKey, "diffuseA.png") || !strings.HasSuffix(color1.Meshes[0].TexKey, "diffuseB.png") || !strings.HasSuffix(color2.Meshes[0].TexKey, "diffuseC.png") {
		t.Fatalf("texKeys %q %q %q", color0.Meshes[0].TexKey, color1.Meshes[0].TexKey, color2.Meshes[0].TexKey)
	}
	if !strings.HasSuffix(color0.Meshes[0].NormalMapKey, "normalA.png") || !strings.HasSuffix(color1.Meshes[0].NormalMapKey, "normalB.png") {
		t.Fatalf("normals %q %q", color0.Meshes[0].NormalMapKey, color1.Meshes[0].NormalMapKey)
	}
	if color0.Meshes[0].MaterialMapKey != "" || !strings.HasSuffix(color1.Meshes[0].MaterialMapKey, "material.png") {
		t.Fatalf("materials %q %q", color0.Meshes[0].MaterialMapKey, color1.Meshes[0].MaterialMapKey)
	}
}

func TestLoadModViewerKeepsSingleRemainingConditionalDiffuse(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "diffuseB.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[Constants]
global persist $color = 0
[KeyColor]
type = cycle
$color = 0,1,2
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
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceDiffuseA]
filename = missingA.png
[ResourceDiffuseB]
filename = diffuseB.png
[ResourceDiffuseC]
filename = missingC.png
`)
	if len(result.Meshes) != 1 || len(result.Meshes[0].TextureVariants) != 1 || modelViewerDNFIsTrue(result.Meshes[0].TextureVariants[0].Conditions) {
		t.Fatalf("mesh = %#v", result.Meshes[0])
	}
	color0 := evaluateViewerTransport(result, map[string]any{"color": "0"})
	color1 := evaluateViewerTransport(result, map[string]any{"color": "1"})
	if !strings.HasSuffix(color1.Meshes[0].TexKey, "diffuseB.png") || color0.Meshes[0].TexKey == color1.Meshes[0].TexKey {
		t.Fatalf("color0=%q color1=%q", color0.Meshes[0].TexKey, color1.Meshes[0].TexKey)
	}
}

func TestLoadModViewerReplaysPresentLiteralAssignmentsBeforeVisibility(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $outfit = 0
global $piece = 0
[KeyOutfit]
type = cycle
$outfit = 0,1
[Present]
if $outfit == 0
    $piece = 0
elif $outfit == 1
    $piece = 1
endif
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $piece == 0
drawindexed = 3, 0, 0
endif
if $piece == 1
drawindexed = 3, 3, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := loadViewerDir(t, dir)
	foundPiece := false
	for _, rule := range fixture.result.StateRules {
		if rule.Var == "piece" {
			foundPiece = true
			break
		}
	}
	if !foundPiece {
		t.Fatalf("stateRules = %#v", fixture.result.StateRules)
	}
	first := evaluateViewerTransport(fixture.result, map[string]any{"outfit": "0"})
	second := evaluateViewerTransport(fixture.result, map[string]any{"outfit": "1"})
	if modelViewerString(first.State["piece"]) != "0" || modelViewerString(second.State["piece"]) != "1" {
		t.Fatalf("state first=%#v second=%#v", first.State, second.State)
	}
	if len(first.Meshes) != 2 || !first.Meshes[0].Visible || first.Meshes[1].Visible || second.Meshes[0].Visible || !second.Meshes[1].Visible {
		t.Fatalf("first=%#v second=%#v", first.Meshes, second.Meshes)
	}
}

func TestLoadModViewerFlipsUVV(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	tc := make([]byte, 8*20)
	for index, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(tc[index*20+4:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(tc[index*20+8:], math.Float32bits(uv[1]))
	}
	if err := os.WriteFile(filepath.Join(dir, "tc.buf"), tc, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := loadViewerDir(t, dir)
	if fixture.result.Meshes[0].UVsURL == "" {
		t.Fatal("missing UVs")
	}
	uvs := readViewerFloat32s(t, fixture.protocol, fixture.result.Meshes[0].UVsURL)
	if len(uvs) < 6 || uvs[0] != 0 || math.Abs(float64(uvs[1]-.75)) > 1e-6 || uvs[2] != 1 || math.Abs(float64(uvs[3]-.25)) > 1e-6 || math.Abs(float64(uvs[4]-.5)) > 1e-6 || math.Abs(float64(uvs[5]-.5)) > 1e-6 {
		t.Fatalf("uvs = %v", uvs)
	}
}

func TestLoadModViewerReplaysMenuSlotEffects(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $top = 1
global persist $pasties = 0
global persist $glasses = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
    $top = 1 - $top
    if $top == 0
        $pasties = 1
    endif
elif $clickedSlot == 2
    $glasses = $glasses + 1
    if $glasses > 2
        $glasses = 0
    endif
endif
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 1
drawindexed = 3, 0, 0
endif
if $pasties == 1
drawindexed = 3, 3, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	top := findViewerVariable(result, "top")
	if top == nil || !viewerVariableHasEffect(*top, "pasties") {
		t.Fatalf("variables = %#v", result.Variables)
	}
	withoutEffects := evaluateViewerTransport(result, map[string]any{"top": "0", "pasties": "0"})
	if withoutEffects.Meshes[0].Visible || withoutEffects.Meshes[1].Visible {
		t.Fatalf("withoutEffects = %#v", withoutEffects.Meshes)
	}
	selected := applyViewerVariableSelection(result.DefaultState, *top, "0")
	if modelViewerString(selected["top"]) != "0" || modelViewerString(selected["pasties"]) != "1" {
		t.Fatalf("selected = %#v", selected)
	}
	withEffects := evaluateViewerTransport(result, selected)
	if withEffects.Meshes[0].Visible || !withEffects.Meshes[1].Visible {
		t.Fatalf("withEffects = %#v", withEffects.Meshes)
	}
}

func TestLoadModViewerAppliesToggleSideEffectsAndHidesSingleValueVars(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $top = 1
global persist $onepiece = 1
[KeyTop]
condition = $active == 1
key = no_modifiers UP
type = cycle
$top = 0,1,2,3
$onepiece = 0
[KeyOnePiece]
condition = $active == 1
key = no_modifiers DOWN
type = cycle
$onepiece = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 1 && $onepiece == 0
drawindexed = 3, 0, 0
endif
if $onepiece == 1
drawindexed = 3, 3, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	top := findViewerVariable(result, "top")
	if top == nil || !viewerVariableHasEffect(*top, "onepiece") {
		t.Fatalf("variables = %#v", result.Variables)
	}
	for _, variable := range result.Variables {
		if variable.ID == "onepiece" && variable.Label == "Top" {
			t.Fatalf("onepiece should not reuse the Top label: %#v", variable)
		}
	}
	selected := applyViewerVariableSelection(result.DefaultState, *top, "2")
	if modelViewerString(selected["top"]) != "2" || modelViewerString(selected["onepiece"]) != "0" {
		t.Fatalf("selected = %#v", selected)
	}
	evaluated := evaluateViewerTransport(result, selected)
	if evaluated.Meshes[0].Visible || evaluated.Meshes[1].Visible {
		t.Fatalf("evaluated = %#v", evaluated.Meshes)
	}
}

func TestLoadModViewerTracksToggleEffectVariablesAsGatingVars(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $top = 0
global persist $resetvar = 0
[KeyTop]
condition = $active == 1
key = no_modifiers UP
type = cycle
$top = 0,1,2,3
$resetvar = 0
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 1
if $resetvar == 0
drawindexed = 3, 0, 0
endif
endif
if $resetvar == 1
drawindexed = 3, 3, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	baseline := evaluateViewerTransport(result, map[string]any{"top": "0", "resetvar": "0"})
	reset := evaluateViewerTransport(result, map[string]any{"top": "0", "resetvar": "1"})
	active := evaluateViewerTransport(result, map[string]any{"top": "1", "resetvar": "0"})
	if baseline.Meshes[0].Visible || baseline.Meshes[1].Visible || reset.Meshes[0].Visible || !reset.Meshes[1].Visible || !active.Meshes[0].Visible || active.Meshes[1].Visible {
		t.Fatalf("baseline=%#v reset=%#v active=%#v", baseline.Meshes, reset.Meshes, active.Meshes)
	}
}

func TestLoadModViewerDisablesIneffectiveToggleValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $skirt = 0
global persist $bottom = 0
global persist $futa = 0
[KeySkirt]
type = cycle
$skirt = 0,1
[KeyBottom]
type = cycle
$bottom = 0,1
[KeyFutanari]
type = cycle
$futa = 0,1,2,3,4
$bottom = 0
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $skirt == 1 && ($bottom != 0 || ($bottom == 0 && $futa != 4))
drawindexed = 3, 0, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	dead := computeViewerIneffectiveValues(result, map[string]any{"futa": "4", "bottom": "0", "skirt": "0"})
	skirtDead := dead["skirt"]
	if skirtDead == nil || skirtDead["1"] == nil {
		t.Fatalf("dead = %#v", dead)
	}
	foundFuta := false
	for _, blocking := range skirtDead["1"] {
		if strings.Contains(blocking.Label, "Futanari") {
			foundFuta = true
		}
	}
	if !foundFuta || skirtDead["0"] != nil {
		t.Fatalf("skirtDead = %#v", skirtDead)
	}
	live := computeViewerIneffectiveValues(result, map[string]any{"futa": "0", "bottom": "0", "skirt": "0"})
	if live["skirt"]["1"] != nil {
		t.Fatalf("live = %#v", live)
	}
}

func TestLoadModViewerResolvesHiddenSingleValueEffectTargetsAsBlockingVars(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $skirt = 0
global persist $outfit = 0
global persist $body = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
    $outfit = 1 - $outfit
    if $outfit == 1
        $body = 1
    endif
elif $clickedSlot == 2
    $skirt = 1 - $skirt
endif
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $skirt == 1 && $body != 0
drawindexed = 3, 0, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	outfit := findViewerVariable(result, "outfit")
	if outfit == nil || findViewerVariable(result, "body") != nil || !viewerVariableHasEffect(*outfit, "body") {
		t.Fatalf("variables = %#v", result.Variables)
	}
	dead := computeViewerIneffectiveValues(result, map[string]any{"outfit": "0", "skirt": "0", "body": "0"})
	entry := dead["skirt"]["1"]
	if entry == nil {
		t.Fatalf("dead = %#v", dead)
	}
	foundOutfit, foundBody := false, false
	for _, blocking := range entry {
		if blocking.ID == "outfit" {
			foundOutfit = true
		}
		if blocking.ID == "body" {
			foundBody = true
		}
	}
	if !foundOutfit || foundBody {
		t.Fatalf("blocking = %#v", entry)
	}
	live := computeViewerIneffectiveValues(result, map[string]any{"outfit": "1", "skirt": "0", "body": "1"})
	if live["skirt"]["1"] != nil {
		t.Fatalf("live = %#v", live)
	}
}

func TestLoadModViewerEvaluatesSecondToggleWithoutRebuildingGeometry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global $hat = 0
[KeyHat]
type = cycle
$hat = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $hat == 0
drawindexed = 3, 0, 0
else
drawindexed = 3, 3, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 2 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	positions := []string{result.Meshes[0].PositionsURL, result.Meshes[1].PositionsURL}
	indices := []string{result.Meshes[0].IndicesURL, result.Meshes[1].IndicesURL}
	first := evaluateViewerTransport(result, map[string]any{"hat": "0"})
	second := evaluateViewerTransport(result, map[string]any{"hat": "1"})
	if !first.Meshes[0].Visible || first.Meshes[1].Visible || second.Meshes[0].Visible || !second.Meshes[1].Visible {
		t.Fatalf("first=%#v second=%#v", first.Meshes, second.Meshes)
	}
	if result.Meshes[0].PositionsURL != positions[0] || result.Meshes[1].PositionsURL != positions[1] || result.Meshes[0].IndicesURL != indices[0] || result.Meshes[1].IndicesURL != indices[1] {
		t.Fatal("geometry URLs changed after evaluation")
	}
}

func TestLoadModViewerResolvesVarDrawindexedAndHidesUnusedAutoDraws(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global $n = 3
global $off = 0
global persist $top = 0
[KeySwap]
type = cycle
$top = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top == 0
drawindexed = $n, $off, 0
endif
if $top == 1
drawindexed = $n, 3, 0
endif
[TextureOverrideBodyIB]
hash = b03c7e30
handling = skip
drawindexed = auto
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 2 || findViewerVariable(result, "top") == nil {
		t.Fatalf("result = %#v", result)
	}
	first := evaluateViewerTransport(result, map[string]any{"top": "0"})
	second := evaluateViewerTransport(result, map[string]any{"top": "1"})
	if !first.Meshes[0].Visible || first.Meshes[1].Visible || second.Meshes[0].Visible || !second.Meshes[1].Visible {
		t.Fatalf("first=%#v second=%#v", first.Meshes, second.Meshes)
	}
}

func TestLoadModViewerExpandsTopLessThanTwoAgainstCycleValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global persist $top = 0
[KeySwap]
type = cycle
$top = 0,1,2
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $top < 2
drawindexed = 3, 0, 0
endif
if $top == 2
drawindexed = 3, 3, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 2 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	low := evaluateViewerTransport(result, map[string]any{"top": "0"})
	high := evaluateViewerTransport(result, map[string]any{"top": "2"})
	if !low.Meshes[0].Visible || low.Meshes[1].Visible || high.Meshes[0].Visible || !high.Meshes[1].Visible {
		t.Fatalf("low=%#v high=%#v", low.Meshes, high.Meshes)
	}
}

func TestLoadModViewerCollapsesPerFrameDrawindexedAnimation(t *testing.T) {
	dir := t.TempDir()
	result := loadViewerMod(t, dir, `[Constants]
global $fps = 15
global $frame = 0
[Present]
post $frame = time * $fps
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $frame == 0
drawindexed = 3, 0, 0
elif $frame == 1
drawindexed = 3, 0, 0
endif
`+viewerBodyResources)
	if len(result.Animations) != 1 || len(result.Meshes) != 1 || len(result.Meshes[0].PositionVariants) != 0 {
		t.Fatalf("result = %#v", result)
	}
	first := evaluateViewerTransport(result, map[string]any{"frame": 0})
	second := evaluateViewerTransport(result, map[string]any{"frame": 1})
	missing := evaluateViewerTransport(result, map[string]any{"frame": 2})
	if !first.Meshes[0].Visible || !second.Meshes[0].Visible || !missing.Meshes[0].Visible {
		t.Fatalf("first=%#v second=%#v missing=%#v", first.Meshes, second.Meshes, missing.Meshes)
	}
}

func TestLoadModViewerDeduplicatesDrawsThatDifferOnlyByBaseVertex(t *testing.T) {
	dir := t.TempDir()
	result := loadViewerMod(t, dir, `[Constants]
global $mode = 0
[KeyMode]
type = cycle
$mode = 0, 1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $mode == 0
drawindexed = 3, 0, 0
elif $mode == 1
drawindexed = 3, 0, 1
endif
`+viewerBodyResources)
	if len(result.Meshes) != 1 || len(result.Meshes[0].PositionVariants) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestLoadModViewerDoesNotTreatExcludedElseBranchAsVisible(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if 1
    drawindexed = 3, 0, 0
else
    drawindexed = 3, 3, 0
endif
`+viewerBodyResources), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "body.ib"), modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 2 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	var included, excluded *ModelViewerMeshTransport
	for index := range result.Meshes {
		mesh := &result.Meshes[index]
		if modelViewerDNFIsTrue(mesh.Conditions) {
			included = mesh
		}
		if len(mesh.Conditions) == 0 {
			excluded = mesh
		}
	}
	if included == nil || excluded == nil {
		t.Fatalf("included=%v excluded=%v meshes=%#v", included, excluded, result.Meshes)
	}
	evaluated := evaluateViewerTransport(result, map[string]any{})
	var includedVisible, excludedVisible bool
	for _, mesh := range evaluated.Meshes {
		if mesh.ID == included.ID {
			includedVisible = mesh.Visible
		}
		if mesh.ID == excluded.ID {
			excludedVisible = mesh.Visible
		}
	}
	if !includedVisible || excludedVisible {
		t.Fatalf("evaluated = %#v", evaluated.Meshes)
	}
}

func readViewerFloat32s(t *testing.T, protocol *infra.Protocol, url string) []float32 {
	t.Helper()
	raw := readModelViewerProtocolBytes(t, protocol, url)
	output := make([]float32, len(raw)/4)
	for index := range output {
		output[index] = math.Float32frombits(binary.LittleEndian.Uint32(raw[index*4:]))
	}
	return output
}

func readViewerUint32s(t *testing.T, protocol *infra.Protocol, url string) []uint32 {
	t.Helper()
	raw := readModelViewerProtocolBytes(t, protocol, url)
	output := make([]uint32, len(raw)/4)
	for index := range output {
		output[index] = binary.LittleEndian.Uint32(raw[index*4:])
	}
	return output
}

func findViewerVariable(payload ModelViewerTransport, id string) *ModelViewerVariable {
	for index := range payload.Variables {
		if payload.Variables[index].ID == id {
			return &payload.Variables[index]
		}
	}
	return nil
}

func viewerVariableHasEffect(variable ModelViewerVariable, name string) bool {
	for _, effect := range variable.Effects {
		if effect.Var == name {
			return true
		}
	}
	return false
}

func sortInts(values []int) {
	for i := range values {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
