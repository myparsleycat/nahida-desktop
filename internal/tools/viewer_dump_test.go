package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

type solidImage struct {
	w, h int
	c    color.NRGBA
}

func (s solidImage) ColorModel() color.Model { return color.NRGBAModel }
func (s solidImage) Bounds() image.Rectangle { return image.Rect(0, 0, s.w, s.h) }
func (s solidImage) At(int, int) color.Color { return s.c }

func writeViewerGeometry(t *testing.T, dir string) {
	t.Helper()
	writeViewerGeometryN(t, dir, 8)
}

func writeViewerGeometryN(t *testing.T, dir string, vertexCount int) {
	t.Helper()
	position := make([]byte, vertexCount*40)
	texcoord := make([]byte, vertexCount*20)
	for vertex := range vertexCount {
		binary.LittleEndian.PutUint32(position[vertex*40:], math.Float32bits(float32(vertex)))
		binary.LittleEndian.PutUint32(position[vertex*40+4:], math.Float32bits(float32(vertex)))
		binary.LittleEndian.PutUint32(position[vertex*40+8:], math.Float32bits(float32(vertex)))
	}
	for name, data := range map[string][]byte{
		"pos.buf":  position,
		"tc.buf":   texcoord,
		"body.ib":  modelViewerUint32Bytes([]uint32{0, 1, 2}),
		"bodyb.ib": modelViewerUint32Bytes([]uint32{0, 1, 2}),
		"bodyc.ib": modelViewerUint32Bytes([]uint32{0, 1, 2}),
		"fire.ib":  modelViewerUint32Bytes([]uint32{0, 1, 2}),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func encodeColorPNG(width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			r := uint8(0)
			g := uint8(0)
			if width > 1 {
				r = uint8(math.Round(float64(x) / float64(width-1) * 255))
			}
			if height > 1 {
				g = uint8(math.Round(float64(y) / float64(height-1) * 255))
			}
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeFlatPNG(width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: 240, G: 240, B: 240, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeOversizedPNG(width, height int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, solidImage{w: width, h: height, c: color.NRGBA{R: 32, G: 64, B: 96, A: 255}}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeTinyPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeTinyPNG2x2() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func makeDdsHeader(width, height int) []byte {
	header := make([]byte, 128)
	copy(header[:4], "DDS ")
	binary.LittleEndian.PutUint32(header[4:8], 124)
	binary.LittleEndian.PutUint32(header[12:16], uint32(height))
	binary.LittleEndian.PutUint32(header[16:20], uint32(width))
	return header
}

func loadViewerMod(t *testing.T, dir, ini string) ModelViewerTransport {
	t.Helper()
	return loadViewerFixture(t, dir, ini).result
}

type viewerLoadFixture struct {
	result   ModelViewerTransport
	protocol *infra.Protocol
	service  *Tools
}

func loadViewerFixture(t *testing.T, dir, ini string) viewerLoadFixture {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, dir)
	return loadViewerDir(t, dir)
}

func loadViewerDir(t *testing.T, dir string) viewerLoadFixture {
	t.Helper()
	protocol := infra.NewProtocol()
	service := NewWithOptions(Options{Protocol: protocol})
	service.UseClient(openToolsTestDB(t))
	result, err := service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = service.CleanupModelViewer(context.Background(), result.MemorySessionID)
	})
	return viewerLoadFixture{result: result, protocol: protocol, service: service}
}

func texKey(mesh ModelViewerMeshTransport) string {
	if mesh.TexKey == nil {
		return ""
	}
	return *mesh.TexKey
}

func lightMapKey(mesh ModelViewerMeshTransport) string {
	if mesh.LightMapKey == nil {
		return ""
	}
	return *mesh.LightMapKey
}

func meshesNamed(result ModelViewerTransport, name string) []ModelViewerMeshTransport {
	var output []ModelViewerMeshTransport
	for _, mesh := range result.Meshes {
		if mesh.Component == name {
			output = append(output, mesh)
		}
	}
	return output
}

func meshesContaining(result ModelViewerTransport, fragment string) []ModelViewerMeshTransport {
	var output []ModelViewerMeshTransport
	for _, mesh := range result.Meshes {
		if strings.Contains(mesh.Component, fragment) {
			output = append(output, mesh)
		}
	}
	return output
}

func writeTextureFile(t *testing.T, dir, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInspectWwmiTextureHintSkipsOversizedPNGUsingIHDRArea(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.png")
	header := make([]byte, 24)
	copy(header[:8], pngSignature)
	copy(header[12:16], "IHDR")
	binary.BigEndian.PutUint32(header[16:20], 8192)
	binary.BigEndian.PutUint32(header[20:24], 8192)
	if err := os.WriteFile(path, header, 0o600); err != nil {
		t.Fatal(err)
	}
	hint := inspectWwmiTextureHint(path)
	if hint == nil || hint.Area != 8192*8192 || hint.IsLikelyFlat || hint.ColorSpace != "srgb" {
		t.Fatalf("hint = %#v", hint)
	}
}

func TestInspectWwmiTextureHintSkipsOversizedDDSUsingHeaderArea(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.dds")
	if err := os.WriteFile(path, makeDdsHeader(8192, 8192), 0o600); err != nil {
		t.Fatal(err)
	}
	hint := inspectWwmiTextureHint(path)
	if hint == nil || hint.Area != 8192*8192 || hint.IsLikelyFlat {
		t.Fatalf("hint = %#v", hint)
	}
}

func TestLoadModViewerBindsWWMIComponentDumpTextures(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-2 t=beef001d.png", encodeColorPNG(16, 16))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef0010.jpg", encodeTinyPNG())
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001b.png", encodeColorPNG(32, 32))
	result := loadViewerMod(t, dir, `[TextureOverrideComponent2]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[TextureOverrideComponent3]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture0]
filename = Textures/Components-2 t=beef001d.png
[TextureOverrideTexture0]
hash = beef000f
this = ResourceTexture0
[ResourceTexture1]
filename = Textures/Components-3 t=beef0010.jpg
[TextureOverrideTexture1]
hash = beef0010
this = ResourceTexture1
[ResourceTexture4]
filename = Textures/Components-3 t=beef001b.png
[TextureOverrideTexture4]
hash = beef0011
this = ResourceTexture4
`)
	component2 := meshesNamed(result, "Component2")
	component3 := meshesNamed(result, "Component3")
	if len(component2) < 1 || len(component3) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range component2 {
		if !strings.Contains(texKey(mesh), "Components-2") {
			t.Fatalf("component2 texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range component3 {
		if !strings.Contains(texKey(mesh), "beef001b.png") || strings.Contains(texKey(mesh), "beef0010") {
			t.Fatalf("component3 texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerDoesNotReplaceRabbitFXDiffuseWithWWMIDump(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "keep.png", encodeTinyPNG())
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001b.png", encodeTinyPNG2x2())
	result := loadViewerMod(t, dir, `[TextureOverrideComponent3]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
Resource\RabbitFX\Diffuse = ref ResourceTextureKeep
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTextureKeep]
filename = keep.png
[ResourceTexture0]
filename = Textures/Components-3 t=beef001b.png
[TextureOverrideTexture0]
hash = beef0011
this = ResourceTexture0
`)
	if len(result.Meshes) < 1 {
		t.Fatal(result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if !strings.Contains(texKey(mesh), "keep.png") {
			t.Fatalf("texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerDoesNotBindWWMIDumpOntoNonComponentMesh(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001b.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
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
[ResourceTexture0]
filename = Textures/Components-3 t=beef001b.png
[TextureOverrideTexture0]
hash = beef0011
this = ResourceTexture0
`)
	if len(result.Meshes) != 1 || texKey(result.Meshes[0]) != "" {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
}

func TestLoadModViewerBindsWWMIPsT0ResourcesWithoutDiffuseName(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001e.png", encodeColorPNG(32, 32))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001c.png", encodeColorPNG(16, 16))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001cA.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[Constants]
global persist $socks = 0
[KeySocks]
type = cycle
$socks = 0, 1
[TextureOverrideComponent3]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
if $socks == 0
ps-t0 = ResourceTexture35
ps-t3 = ResourceTexture32
endif
if $socks == 1
ps-t0 = ResourceTexture35A
ps-t3 = ResourceTexture32
endif
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture32]
filename = Textures/Components-3 t=beef001e.png
[ResourceTexture35]
filename = Textures/Components-3 t=beef001c.png
[ResourceTexture35A]
filename = Textures/Components-3 t=beef001cA.png
`)
	if len(result.Meshes) < 1 {
		t.Fatal(result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if !strings.Contains(texKey(mesh), "beef001c") || strings.Contains(texKey(mesh), "beef001e") {
			t.Fatalf("texKey = %q variants=%#v", texKey(mesh), mesh.TextureVariants)
		}
	}
	foundA, foundB := false, false
	for _, variant := range result.Meshes[0].TextureVariants {
		if strings.Contains(variant.TexKey, "beef001cA.png") {
			foundA = true
		}
		if strings.Contains(variant.TexKey, "beef001c.png") {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Fatalf("variants = %#v", result.Meshes[0].TextureVariants)
	}
}

func TestLoadModViewerBindsSharedWWMIComponentDumps(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-0-1-2-3-4-5 t=beef0012.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[TextureOverrideComponent5]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture11]
filename = Textures/Components-0-1-2-3-4-5 t=beef0012.png
[TextureOverrideTexture11]
hash = beef0012
this = ResourceTexture11
`)
	if len(result.Meshes) < 1 {
		t.Fatal(result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if !strings.Contains(texKey(mesh), "beef0012.png") {
			t.Fatalf("texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerDoesNotPickWWMIDumpAssignedToNonDiffusePsSlot(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-2 t=beef001f.png", encodeColorPNG(32, 32))
	writeTextureFile(t, dir, "Textures/Components-2 t=beef0020.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[TextureOverrideComponent2]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t1 = ResourceTexture31
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture31]
filename = Textures/Components-2 t=beef001f.png
[ResourceTextureOther]
filename = Textures/Components-2 t=beef0020.png
`)
	if len(result.Meshes) < 1 {
		t.Fatal(result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if !strings.Contains(texKey(mesh), "beef0020.png") || strings.Contains(texKey(mesh), "beef001f") {
			t.Fatalf("texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerUnionsWWMINonDiffuseDumpExclusions(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001f.png", encodeColorPNG(32, 32))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef0020.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[TextureOverrideComponent3]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t1 = ResourceTexture31
drawindexed = 3, 0, 0
[TextureOverridecomponent3]
hash = beef000e
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture31]
filename = Textures/Components-3 t=beef001f.png
[ResourceTextureOther]
filename = Textures/Components-3 t=beef0020.png
`)
	if len(result.Meshes) < 2 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if !strings.Contains(texKey(mesh), "beef0020.png") || strings.Contains(texKey(mesh), "beef001f") {
			t.Fatalf("texKey = %q component=%q", texKey(mesh), mesh.Component)
		}
	}
}

func TestLoadModViewerSkipsDecodingOversizedPNGCandidates(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001c.png", encodeOversizedPNG(8192, 8192))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001b.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[TextureOverrideComponent3]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t0 = ResourceTexture35
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture35]
filename = Textures/Components-3 t=beef001c.png
[ResourceTexture4]
filename = Textures/Components-3 t=beef001b.png
`)
	if len(result.Meshes) < 1 {
		t.Fatal(result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if !strings.Contains(texKey(mesh), "beef001c.png") || strings.Contains(texKey(mesh), "beef001b") {
			t.Fatalf("texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerSkipsDecodingOversizedDDSCandidates(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001c.dds", makeDdsHeader(8192, 8192))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001b.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[TextureOverrideComponent3]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t0 = ResourceTexture35
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture35]
filename = Textures/Components-3 t=beef001c.dds
[ResourceTexture4]
filename = Textures/Components-3 t=beef001b.png
`)
	if len(result.Meshes) != 1 || texKey(result.Meshes[0]) != "" {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for key := range result.Textures {
		if strings.Contains(key, "beef001b") {
			t.Fatalf("textures = %#v", result.Textures)
		}
	}
}

func TestLoadModViewerReplacesUnnamedWWMIPsT0FlatColorMap(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001c.png", encodeFlatPNG(16, 16))
	writeTextureFile(t, dir, "Textures/Components-3 t=beef001b.png", encodeColorPNG(16, 16))
	result := loadViewerMod(t, dir, `[TextureOverrideComponent3]
hash = beef000d
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceTexCoordBuffer
ps-t0 = ResourceTexture35
drawindexed = 3, 0, 0
[ResourcePositionBuffer]
filename = pos.buf
stride = 40
[ResourceTexCoordBuffer]
filename = tc.buf
stride = 20
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceTexture35]
filename = Textures/Components-3 t=beef001c.png
[ResourceTexture4]
filename = Textures/Components-3 t=beef001b.png
`)
	if len(result.Meshes) < 1 {
		t.Fatal(result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if !strings.Contains(texKey(mesh), "beef001b.png") || strings.Contains(texKey(mesh), "beef001c") {
			t.Fatalf("texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerBindsSRMIIBComponentDumpAtlas(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Texture/beef0006_1_beef0007_Hash_DiffuseMap.png", encodeTinyPNG())
	writeTextureFile(t, dir, "Texture/beef0006_1_beef0008_Hash_LightMap.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[TextureOverride_VB_beef0006_Position]
hash = beef0006
vb0 = ResourcePos
[TextureOverride_VB_beef0006_Texcoord]
hash = beef0006
vb1 = ResourceTc
[TextureOverride_IB_beef0006_Component1]
hash = beef0006
match_first_index = 0
ib = Resource_beef0006_Component1
drawindexed = 3, 0, 0
[TextureOverride_IB_beef0006_Component2]
hash = beef0006
match_first_index = 14598
ib = Resource_beef0006_Component2
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[Resource_beef0006_Component1]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_beef0006_Component2]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_beef0007]
filename = Texture/beef0006_1_beef0007_Hash_DiffuseMap.png
[TextureOverride_beef0007]
hash = beef0007
this = Resource_Texture_beef0007
[Resource_Texture_beef0008]
filename = Texture/beef0006_1_beef0008_Hash_LightMap.png
[TextureOverride_beef0008]
hash = beef0008
this = Resource_Texture_beef0008
`)
	component1 := meshesNamed(result, "_IB_beef0006_Component1")
	component2 := meshesNamed(result, "_IB_beef0006_Component2")
	if len(component1) < 1 || len(component2) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range component1 {
		if !strings.Contains(texKey(mesh), "Hash_DiffuseMap") {
			t.Fatalf("component1 texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range component2 {
		if !strings.Contains(texKey(mesh), "Hash_DiffuseMap") || !strings.Contains(lightMapKey(mesh), "Hash_LightMap") {
			t.Fatalf("component2 texKey=%q light=%q", texKey(mesh), lightMapKey(mesh))
		}
	}
}

func TestLoadModViewerDoesNotShareDistinctIBComponentDumpAtlases(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Texture/beef0006_1_aaaaaaa1_Hash_DiffuseMap.png", encodeTinyPNG())
	writeTextureFile(t, dir, "Texture/beef0006_2_aaaaaaa2_Hash_DiffuseMap.png", encodeTinyPNG2x2())
	result := loadViewerMod(t, dir, `[TextureOverride_VB_beef0006_Position]
hash = beef0006
vb0 = ResourcePos
[TextureOverride_VB_beef0006_Texcoord]
hash = beef0006
vb1 = ResourceTc
[TextureOverride_IB_beef0006_Component1]
hash = beef0006
ib = Resource_beef0006_Component1
drawindexed = 3, 0, 0
[TextureOverride_IB_beef0006_Component2]
hash = beef0006
ib = Resource_beef0006_Component2
drawindexed = 3, 0, 0
[TextureOverride_IB_beef0006_Component3]
hash = beef0006
ib = Resource_beef0006_Component3
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[Resource_beef0006_Component1]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_beef0006_Component2]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[Resource_beef0006_Component3]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_aaaaaaa1]
filename = Texture/beef0006_1_aaaaaaa1_Hash_DiffuseMap.png
[Resource_Texture_aaaaaaa2]
filename = Texture/beef0006_2_aaaaaaa2_Hash_DiffuseMap.png
`)
	component1 := meshesNamed(result, "_IB_beef0006_Component1")
	component2 := meshesNamed(result, "_IB_beef0006_Component2")
	component3 := meshesNamed(result, "_IB_beef0006_Component3")
	if len(component1) < 1 || len(component2) < 1 || len(component3) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range component1 {
		if !strings.Contains(texKey(mesh), "aaaaaaa1") {
			t.Fatalf("component1 texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range component2 {
		if !strings.Contains(texKey(mesh), "aaaaaaa2") {
			t.Fatalf("component2 texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range component3 {
		if texKey(mesh) != "" {
			t.Fatalf("component3 texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerKeepsPerComponentDumpAtlasesWhenHashLeftoverExists(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Texture/beef0006_1_aaaaaaa1_Hash_DiffuseMap.png", encodeTinyPNG())
	writeTextureFile(t, dir, "Texture/beef0006_2_aaaaaaa2_Hash_DiffuseMap.png", encodeTinyPNG2x2())
	writeTextureFile(t, dir, "other.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[TextureOverride_VB_beef0006_Position]
hash = beef0006
vb0 = ResourcePos
[TextureOverride_VB_beef0006_Texcoord]
hash = beef0006
vb1 = ResourceTc
[TextureOverride_IB_beef0006_Component1]
hash = beef0006
ib = Resource_beef0006_Component1
drawindexed = 3, 0, 0
[TextureOverride_IB_beef0006_Component2]
hash = beef0006
ib = Resource_beef0006_Component2
drawindexed = 3, 0, 0
[TextureOverride_IB_beef0006_Component3]
hash = beef0006
ib = Resource_beef0006_Component3
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[Resource_beef0006_Component1]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_beef0006_Component2]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[Resource_beef0006_Component3]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_aaaaaaa1]
filename = Texture/beef0006_1_aaaaaaa1_Hash_DiffuseMap.png
[Resource_Texture_aaaaaaa2]
filename = Texture/beef0006_2_aaaaaaa2_Hash_DiffuseMap.png
[TextureOverride_deadbeef]
hash = deadbeef
this = ResourceOther
[ResourceOther]
filename = other.png
`)
	component1 := meshesNamed(result, "_IB_beef0006_Component1")
	component2 := meshesNamed(result, "_IB_beef0006_Component2")
	component3 := meshesNamed(result, "_IB_beef0006_Component3")
	if len(component1) < 1 || len(component2) < 1 || len(component3) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range component1 {
		if !strings.Contains(texKey(mesh), "aaaaaaa1") {
			t.Fatalf("component1 texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range component2 {
		if !strings.Contains(texKey(mesh), "aaaaaaa2") {
			t.Fatalf("component2 texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range component3 {
		if !strings.Contains(texKey(mesh), "other.png") {
			t.Fatalf("component3 texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerDoesNotBindIBComponentDumpOntoBodyA(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "Texture/beef0006_1_beef0007_Hash_DiffuseMap.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[TextureOverrideBodyA]
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[Resource_Texture_beef0007]
filename = Texture/beef0006_1_beef0007_Hash_DiffuseMap.png
`)
	if len(result.Meshes) != 1 || texKey(result.Meshes[0]) != "" {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
}

func TestLoadModViewerBindsHashOnlyThisImagesUsingMatchingToggleVars(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "face.png", encodeTinyPNG())
	writeTextureFile(t, dir, "bodytex.png", encodeTinyPNG2x2())
	result := loadViewerMod(t, dir, `[Constants]
global persist $eyes = 0
global persist $cloth = 0
global persist $clothcolor = 0
[KeyEyes]
type = cycle
$eyes = 0,1
[KeyCloth]
type = cycle
$cloth = 0,1
[KeyClothColor]
type = cycle
$clothcolor = 0,1
[TextureOverride_VB_aaaaaaa1_face_Position]
hash = aaaaaaa1
vb0 = ResourcePos
[TextureOverride_VB_aaaaaaa1_face_Texcoord]
hash = aaaaaaa1
vb1 = ResourceTc
[TextureOverride_VB_bbbbbbbb_body_Position]
hash = bbbbbbbb
vb0 = ResourcePos
[TextureOverride_VB_bbbbbbbb_body_Texcoord]
hash = bbbbbbbb
vb1 = ResourceTc
[TextureOverride_IB_aaaaaaa1_face_Component1]
hash = aaaaaaa1
ib = ResourceFaceIB
if $eyes == 0
drawindexed = 3, 0, 0
endif
[TextureOverride_IB_bbbbbbbb_body_Component2]
hash = bbbbbbbb
ib = ResourceBodyIB
if $cloth == 0
drawindexed = 3, 0, 0
endif
[TextureOverride_beef0009]
hash = beef0009
if $eyes == 0
this = ResourceFaceTex
endif
[TextureOverride_beef000a]
hash = beef000a
if $clothcolor == 0
this = ResourceBodyTex
endif
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceFaceIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyIB]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[ResourceFaceTex]
filename = face.png
[ResourceBodyTex]
filename = bodytex.png
`)
	face := meshesContaining(result, "face_Component1")
	body := meshesContaining(result, "body_Component2")
	if len(face) < 1 || len(body) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range face {
		if !strings.Contains(texKey(mesh), "face.png") {
			t.Fatalf("face texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range body {
		if !strings.Contains(texKey(mesh), "bodytex.png") {
			t.Fatalf("body texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerAssignsLeftoverHashOnlyImagesToHeavierIBPartsFirst(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "weapon.png", encodeTinyPNG2x2())
	writeTextureFile(t, dir, "fire.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[TextureOverride_VB_aaaaaaa1_weapon_Position]
hash = aaaaaaa1
vb0 = ResourcePos
[TextureOverride_VB_aaaaaaa1_weapon_Texcoord]
hash = aaaaaaa1
vb1 = ResourceTc
[TextureOverride_VB_bbbbbbbb_fire_Position]
hash = bbbbbbbb
vb0 = ResourcePos
[TextureOverride_VB_bbbbbbbb_fire_Texcoord]
hash = bbbbbbbb
vb1 = ResourceTc
[TextureOverride_IB_aaaaaaa1_weapon_Component1]
hash = aaaaaaa1
ib = ResourceWeaponIB
drawindexed = 3, 0, 0
drawindexed = 3, 0, 0
[TextureOverride_IB_bbbbbbbb_fire_Component1]
hash = bbbbbbbb
ib = ResourceFireIB
drawindexed = 3, 0, 0
[TextureOverride_beef000b]
hash = beef000b
this = ResourceWeaponTex
[TextureOverride_beef000c]
hash = beef000c
this = ResourceFireTex
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceWeaponIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceFireIB]
filename = fire.ib
format = DXGI_FORMAT_R32_UINT
[ResourceWeaponTex]
filename = weapon.png
[ResourceFireTex]
filename = fire.png
`)
	weapon := meshesContaining(result, "weapon_Component1")
	fire := meshesContaining(result, "fire_Component1")
	if len(weapon) < 1 || len(fire) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range weapon {
		if !strings.Contains(texKey(mesh), "weapon.png") {
			t.Fatalf("weapon texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range fire {
		if !strings.Contains(texKey(mesh), "fire.png") {
			t.Fatalf("fire texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerFallsBackToIBStemDiffuseResources(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "bodyb.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[TextureOverrideBodyB]
ib = ResourceBodyBIB.0
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyBIB.0]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyBDiffuse.0]
filename = bodyb.png
`)
	if len(result.Meshes) != 1 || !strings.Contains(texKey(result.Meshes[0]), "bodyb.png") {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	if _, ok := result.Textures[texKey(result.Meshes[0])]; !ok {
		t.Fatalf("textures = %#v", result.Textures)
	}
}

func TestLoadModViewerBindsSiblingBodyADiffuseThisOverride(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "body.png", encodeTinyPNG())
	writeTextureFile(t, dir, "womb.png", encodeTinyPNG())
	writeTextureFile(t, dir, "ult.png", encodeTinyPNG())
	writeTextureFile(t, dir, "light.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[Constants]
global persist $nine2 = 0
global persist $zero2 = 0
[KeyNine2]
type = cycle
$nine2 = 0,1
[KeyZero2]
type = cycle
$zero2 = 0,1
[TextureOverrideBodyPosition]
vb0 = ResourcePos
[TextureOverrideBodyTexcoord]
vb1 = ResourceTc
[TextureOverrideBodyA]
hash = beef0014
match_first_index = 0
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyB]
hash = beef0014
match_first_index = 62376
ib = ResourceBodyBIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyADiffuse]
hash = beef0015
if $nine2 == 0
    if $zero2 == 0
this = ResourceBodyADiffuse
    else
this = ResourceDiffWomb
    endif
else
this = ResourceBodyUltADiffuse
endif
[TextureOverrideBodyALightMap]
hash = beef0016
this = ResourceBodyALightMap
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyBIB]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyADiffuse]
filename = body.png
[ResourceDiffWomb]
filename = womb.png
[ResourceBodyUltADiffuse]
filename = ult.png
[ResourceBodyALightMap]
filename = light.png
`)
	bodyB := meshesNamed(result, "BodyB")
	if len(bodyB) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range bodyB {
		if lightMapKey(mesh) == "" {
			t.Fatalf("bodyB lightMap missing: %#v", mesh)
		}
		keys := []string{texKey(mesh)}
		for _, variant := range mesh.TextureVariants {
			keys = append(keys, variant.TexKey)
		}
		joined := strings.Join(keys, " ")
		if !strings.Contains(joined, "body.png") || !strings.Contains(joined, "womb.png") || !strings.Contains(joined, "ult.png") {
			t.Fatalf("bodyB textures = %q variants=%#v", joined, mesh.TextureVariants)
		}
	}
}

func TestLoadModViewerUsesLastSameHashDiffuseAndLightMap(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "bodya.png", encodeTinyPNG())
	writeTextureFile(t, dir, "bodyb.png", encodeTinyPNG())
	writeTextureFile(t, dir, "bodyc.png", encodeTinyPNG())
	writeTextureFile(t, dir, "bodya-light.png", encodeTinyPNG())
	writeTextureFile(t, dir, "bodyc-light.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[TextureOverrideBodyA]
hash = beef0006
match_first_index = 0
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyB]
hash = beef0006
match_first_index = 14598
ib = ResourceBodyBIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyC]
hash = beef0006
match_first_index = 32934
ib = ResourceBodyCIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyADiffuse]
hash = beef0007
this = ResourceBodyADiffuse
[TextureOverrideBodyBDiffuse]
hash = beef0007
this = ResourceBodyBDiffuse
[TextureOverrideBodyCDiffuse]
hash = beef0007
this = ResourceBodyCDiffuse
[TextureOverrideBodyALightMap]
hash = beef0008
this = ResourceBodyALightMap
[TextureOverrideBodyCLightMap]
hash = beef0008
this = ResourceBodyCLightMap
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyBIB]
filename = bodyb.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyCIB]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyADiffuse]
filename = bodya.png
[ResourceBodyBDiffuse]
filename = bodyb.png
[ResourceBodyCDiffuse]
filename = bodyc.png
[ResourceBodyALightMap]
filename = bodya-light.png
[ResourceBodyCLightMap]
filename = bodyc-light.png
`)
	bodyA := meshesNamed(result, "BodyA")
	bodyB := meshesNamed(result, "BodyB")
	if len(bodyA) < 1 || len(bodyB) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range bodyA {
		if !strings.Contains(texKey(mesh), "bodyc.png") || strings.Contains(texKey(mesh), "bodya.png") || !strings.Contains(lightMapKey(mesh), "bodyc-light.png") {
			t.Fatalf("bodyA texKey=%q light=%q", texKey(mesh), lightMapKey(mesh))
		}
	}
	for _, mesh := range bodyB {
		if !strings.Contains(texKey(mesh), "bodyc.png") {
			t.Fatalf("bodyB texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerKeepsNameMatchedDiffuseWhenHashesDiffer(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "bodya.png", encodeTinyPNG())
	writeTextureFile(t, dir, "bodyc.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[TextureOverrideBodyA]
ib = ResourceBodyAIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyC]
ib = ResourceBodyCIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
[TextureOverrideBodyADiffuse]
hash = beef0007
this = ResourceBodyADiffuse
[TextureOverrideBodyCDiffuse]
hash = beef0017
this = ResourceBodyCDiffuse
[ResourcePos]
filename = pos.buf
stride = 40
[ResourceTc]
filename = tc.buf
stride = 20
[ResourceBodyAIB]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyCIB]
filename = bodyc.ib
format = DXGI_FORMAT_R32_UINT
[ResourceBodyADiffuse]
filename = bodya.png
[ResourceBodyCDiffuse]
filename = bodyc.png
`)
	bodyA := meshesNamed(result, "BodyA")
	bodyC := meshesNamed(result, "BodyC")
	if len(bodyA) < 1 || len(bodyC) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range bodyA {
		if !strings.Contains(texKey(mesh), "bodya.png") {
			t.Fatalf("bodyA texKey = %q", texKey(mesh))
		}
	}
	for _, mesh := range bodyC {
		if !strings.Contains(texKey(mesh), "bodyc.png") {
			t.Fatalf("bodyC texKey = %q", texKey(mesh))
		}
	}
}

func TestLoadModViewerBindsSRMIThisHashTexturesFromSiblingDiffuseOverride(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "head.png", encodeTinyPNG())
	result := loadViewerMod(t, dir, `[Constants]
global persist $swapvar = 0
[KeySwap]
type = cycle
$swapvar = 0,1
[TextureOverrideHeadPosition]
vb0 = ResourceHeadPosition
[TextureOverrideHeadTexcoord]
vb1 = ResourceHeadTexcoord
[TextureOverrideHeadA]
hash = beef0018
vb0 = ResourceHeadPosition
vb1 = ResourceHeadTexcoord
run = CommandListHeadA
[TextureOverrideHeadADiffuse]
hash = beef0019
run = CommandListHeadADiffuse
[CommandListHeadA]
if $swapvar == 0
ib = ResourceHeadAIB.0
else if $swapvar == 1
ib = ResourceHeadAIB.0
endif
[CommandListHeadADiffuse]
if $swapvar == 0
this = ResourceHeadADiffuse.0
else if $swapvar == 1
this = ResourceHeadADiffuse.0
endif
[ResourceHeadPosition]
filename = pos.buf
stride = 40
[ResourceHeadTexcoord]
filename = tc.buf
stride = 20
[ResourceHeadAIB.0]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = body.ib
[ResourceHeadADiffuse.0]
filename = head.png
`)
	if len(result.Meshes) < 1 {
		t.Fatalf("meshes = %#v", result.Meshes)
	}
	for _, mesh := range result.Meshes {
		if texKey(mesh) == "" {
			t.Fatalf("mesh missing texKey: %#v", mesh)
		}
		if _, ok := result.Textures[texKey(mesh)]; !ok {
			t.Fatalf("textures = %#v key=%q", result.Textures, texKey(mesh))
		}
		if result.Textures[texKey(mesh)].Role != "diffuse" {
			t.Fatalf("role = %q", result.Textures[texKey(mesh)].Role)
		}
	}
}
