package tools

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

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
	if !states["BodyIB:Pos"] || !states["AltIB:AltPos"] || records[0].conditions[0][0].Negate == records[1].conditions[0][0].Negate {
		t.Fatalf("states=%v records=%#v", states, records)
	}
}

func TestModelViewerWWMIDirectGeometryUsesVectorAndVB2Texcoord(t *testing.T) {
	dir := t.TempDir()
	iniText := `[Constants]
global $required_wwmi_version = 0.70
[TextureOverrideComponent]
run = CommandListOverrideSharedResources
drawindexed = 3, 0, 0
[CommandListOverrideSharedResources]
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceVectorBuffer
vb2 = ResourceTexCoordBuffer
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
		vectors[vertex*8+2] = 127
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
	sections := parseModINI(iniText)
	meshes, err := buildModelViewerDirectScannedMeshesAt(
		iniPath,
		dir,
		sections,
		collectModelViewerDefaultVariables(sections),
		newModelViewerBufferCache(),
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
	if len(geometry.Normal) != 9 || geometry.Normal[2] != 1 || geometry.Normal[5] != 1 || geometry.Normal[8] != 1 {
		t.Fatalf("normals = %v", geometry.Normal)
	}
	if len(geometry.Tangent) != 0 {
		t.Fatalf("WWMI tangent should use Three.js derivative frame: %v", geometry.Tangent)
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
	if len(records) != 1 || records[0].state.ib != "BodyIB" || records[0].state.vb0 != "Pos" || records[0].state.vb1 != "Tc" || len(records[0].draw.Conditions) != 0 {
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
	records, err := collectModelViewerDirectDrawRecords(parseModINI(ini.String()), collectModelViewerDefaultVariables(parseModINI(ini.String())))
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
		fmt.Fprintf(&ini, "[TextureOverride%s]\nib = Resource%sIB\nrun = CommandListRunSlotFix\nrun = CommandList%s\n", group.name, group.name, group.name)
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
	meshes, err := buildModelViewerDirectScannedMeshes(iniPath, sections, collectModelViewerDefaultVariables(sections))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 2 {
		t.Fatalf("meshes = %#v", meshes)
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
	if xs[0] != 1 || xs[1] != 100 || modelViewerDNFIsTrue(meshes[0].conditions) || modelViewerDNFIsTrue(meshes[1].conditions) {
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
	service.UseClient(openToolsTestDB(t))
	result, err := service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	mesh := result.Meshes[0]
	if result.MaterialProfile != "zzmi" || len(mesh.TextureVariants) != 3 || len(mesh.NormalMapVariants) != 2 || mesh.LightMapKey == nil || len(mesh.MaterialMapVariants) != 1 {
		t.Fatalf("profile=%q mesh=%#v", result.MaterialProfile, mesh)
	}
	if _, prepared := result.Textures[modelViewerNormalizeKey("ResourceUnused")]; prepared {
		t.Fatalf("unreferenced texture was prepared: %#v", result.Textures)
	}
	if removed, cleanupErr := service.CleanupModelViewer(context.Background(), result.MemorySessionID); cleanupErr != nil || !removed {
		t.Fatalf("cleanup=%v, %v", removed, cleanupErr)
	}
}
