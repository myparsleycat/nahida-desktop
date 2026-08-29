package tools

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func writeEFMIComponentBuffers(t *testing.T, dir, component string) {
	t.Helper()
	meshes := filepath.Join(dir, "Meshes")
	if err := os.MkdirAll(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	pos := make([]byte, 3*16)
	for vertex := range 3 {
		binary.LittleEndian.PutUint32(pos[vertex*16:], math.Float32bits(float32(vertex)))
	}
	tc := make([]byte, 3*12)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(tc[vertex*12:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(tc[vertex*12+4:], math.Float32bits(uv[1]))
	}
	files := map[string][]byte{
		component + "_VB0.buf": pos,
		component + "_VB1.buf": tc,
		component + "_VB2.buf": make([]byte, 3*12),
		component + "_IB.buf":  {0, 0, 1, 0, 2, 0},
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(meshes, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeEFMIDumpBuffers(t *testing.T, dir string) {
	t.Helper()
	bufferDir := filepath.Join(dir, "Buffer")
	if err := os.MkdirAll(bufferDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pos := make([]byte, 3*16)
	for vertex := range 3 {
		binary.LittleEndian.PutUint32(pos[vertex*16:], math.Float32bits(float32(vertex)))
	}
	tc := make([]byte, 3*12)
	for vertex, uv := range [][2]float32{{0, .25}, {1, .75}, {.5, .5}} {
		binary.LittleEndian.PutUint32(tc[vertex*12:], math.Float32bits(uv[0]))
		binary.LittleEndian.PutUint32(tc[vertex*12+4:], math.Float32bits(uv[1]))
	}
	files := map[string][]byte{
		"809f7872-114840-0-Position.buf": pos,
		"809f7872-114840-0-Texcoord.buf": tc,
		"809f7872-114840-0-Blend.buf":    make([]byte, 3*12),
		"809f7872-114840-0-Index.buf":    modelViewerUint32Bytes([]uint32{0, 1, 2}),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(bufferDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestModelViewerScannerParsesDrawIndexedInstanced(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
vb2 = ResourceBlend
handling = skip
drawindexedinstanced = 3, 0, 0
drawindexedinstanced = 6, INSTANCE_COUNT, 3, 2, FIRST_INSTANCE`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].auto || records[0].draw.IndexCount != 6 || records[0].draw.StartIndex != 3 || records[0].draw.BaseVertex != 2 {
		t.Fatalf("records = %#v", records)
	}
}

func TestModelViewerScannerSkipsLODOverrides(t *testing.T) {
	sections := parseModINI(`[TextureOverride_Component0]
ib = Resource_Component0_IB
vb0 = Resource_Component0_VB0
vb1 = Resource_Component0_VB1
run = CommandList_Draw_Component0
[TextureOverride_Component0_LOD0]
ib = Resource_Component0_IB
vb0 = Resource_Component0_VB0
vb1 = Resource_Component0_VB1
run = CommandList_Draw_Component0
[CommandList_Draw_Component0]
drawindexedinstanced = 3, INSTANCE_COUNT, 0, 0, FIRST_INSTANCE`)
	records, err := collectModelViewerDirectDrawRecords(sections, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].sectionName != "_Component0" || records[0].draw.IndexCount != 3 {
		t.Fatalf("records = %#v", records)
	}
}

func TestLoadModViewerLoadsEFMIDumpStyleInstancedDraw(t *testing.T) {
	dir := t.TempDir()
	writeEFMIDumpBuffers(t, dir)
	writeTextureFile(t, dir, "Texture/809f7872-114840-0-DiffuseMap.png", encodeTinyPNG())
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[TextureOverride_809f7872_114840_0]
hash = 809f7872
handling = skip
ps-t16 = Resource-809f7872-114840-0-DiffuseMap
vb2 = Resource_809f7872_114840_0_Blend
vb0 = Resource_809f7872_114840_0_Position
vb1 = Resource_809f7872_114840_0_Texcoord
ib = Resource_809f7872_114840_0_Index
drawindexedinstanced = 3, INSTANCE_COUNT, 0, 0, FIRST_INSTANCE
[Resource_809f7872_114840_0_Position]
type = Buffer
stride = 16
filename = Buffer/809f7872-114840-0-Position.buf
[Resource_809f7872_114840_0_Texcoord]
type = Buffer
stride = 12
filename = Buffer/809f7872-114840-0-Texcoord.buf
[Resource_809f7872_114840_0_Blend]
type = Buffer
stride = 12
filename = Buffer/809f7872-114840-0-Blend.buf
[Resource_809f7872_114840_0_Index]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = Buffer/809f7872-114840-0-Index.buf
[Resource-809f7872-114840-0-DiffuseMap]
filename = Texture/809f7872-114840-0-DiffuseMap.png
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", fixture.result.Meshes)
	}
	mesh := fixture.result.Meshes[0]
	if texKey(mesh) == "" || fixture.result.Textures[texKey(mesh)].Role != "diffuse" {
		t.Fatalf("texKey=%q textures=%#v", texKey(mesh), fixture.result.Textures)
	}
	uvs := readViewerFloat32s(t, fixture.protocol, mesh.UVsURL)
	if len(uvs) < 2 || math.Abs(float64(uvs[1]-.75)) > 1e-5 {
		t.Fatalf("uvs = %v", uvs)
	}
}

func TestLoadModViewerLoadsEFMIComponentCommandList(t *testing.T) {
	dir := t.TempDir()
	writeEFMIComponentBuffers(t, dir, "Component0")
	writeTextureFile(t, dir, "Textures/body.png", encodeTinyPNG())
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(`[Constants]
global $required_efmi_version = 1.10
global $mod_enabled = 0
global $object_detected = 0
global persist $swapvar_toggle_0 = -1
global $draw_component_0_body = 1
[KeySwap]
type = cycle
$swapvar_toggle_0 = -1, 0
[CommandList_Draw_Component0]
ib = ref Resource_Component0_IB
vb0 = ref Resource_Component0_VB0
vb1 = ref Resource_Component0_VB1
vb2 = ref Resource_Component0_VB2
Resource\RabbitFX\Diffuse = ref Resource_Texture0
if $draw_component_0_body
    drawindexedinstanced = 3, INSTANCE_COUNT, 0, 0, FIRST_INSTANCE
endif
[TextureOverride_Component0]
hash = 737f4c35
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component0
endif
[TextureOverride_Component0_LOD0]
hash = 86f40ec1
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component0
endif
[Resource_Component0_IB]
type = Buffer
format = DXGI_FORMAT_R16_UINT
filename = Meshes/Component0_IB.buf
[Resource_Component0_VB0]
type = Buffer
stride = 16
filename = Meshes/Component0_VB0.buf
[Resource_Component0_VB1]
type = Buffer
stride = 12
filename = Meshes/Component0_VB1.buf
[Resource_Component0_VB2]
type = Buffer
stride = 12
filename = Meshes/Component0_VB2.buf
[Resource_Texture0]
filename = Textures/body.png
`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := loadViewerDir(t, dir).result
	if len(result.Meshes) != 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	if evaluated := evaluateViewerTransport(result, nil); !evaluated.Meshes[0].Visible {
		t.Fatalf("default mesh is hidden: %#v", evaluated)
	}
	if texKey(result.Meshes[0]) == "" {
		t.Fatalf("missing diffuse: %#v", result.Meshes[0])
	}
}
