package tools

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestLoadModViewerPreservesAuthoredNormalsAndTangents(t *testing.T) {
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
	pos := make([]byte, 8*40)
	for vertex := range 8 {
		binary.LittleEndian.PutUint32(pos[vertex*40:], math.Float32bits(float32(vertex)))
		binary.LittleEndian.PutUint32(pos[vertex*40+4:], math.Float32bits(float32(vertex)))
		binary.LittleEndian.PutUint32(pos[vertex*40+8:], math.Float32bits(float32(vertex)))
		binary.LittleEndian.PutUint32(pos[vertex*40+16:], math.Float32bits(1))
		binary.LittleEndian.PutUint32(pos[vertex*40+24:], math.Float32bits(1))
		binary.LittleEndian.PutUint32(pos[vertex*40+36:], math.Float32bits(-1))
	}
	if err := os.WriteFile(filepath.Join(dir, "pos.buf"), pos, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := loadViewerDir(t, dir)
	mesh := fixture.result.Meshes[0]
	if mesh.NormalsURL == "" || mesh.TangentsURL == "" {
		t.Fatalf("authored TBN missing: %#v", mesh)
	}
	normals := readViewerFloat32s(t, fixture.protocol, mesh.NormalsURL)
	tangents := readViewerFloat32s(t, fixture.protocol, mesh.TangentsURL)
	if len(normals) != 9 || normals[0] != 0 || normals[1] != 1 || normals[2] != 0 {
		t.Fatalf("normals = %v", normals)
	}
	if len(tangents) != 12 || tangents[0] != 1 || tangents[3] != -1 {
		t.Fatalf("tangents = %v", tangents)
	}
}

func TestLoadModViewerDoesNotTreatZeroPaddedFramesAsAuthoredTBN(t *testing.T) {
	dir := t.TempDir()
	result := loadViewerMod(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
`+viewerBodyResources)
	if result.Meshes[0].NormalsURL != "" || result.Meshes[0].TangentsURL != "" {
		t.Fatalf("zero padding was treated as authored TBN: %#v", result.Meshes[0])
	}
}

func TestLoadModViewerKeepsPNGAndJPEGMimeTypes(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "diffuse.png", encodeTinyPNG())
	var jpegBuf bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 78, G: 90, B: 12, A: 255})
	if err := jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	writeTextureFile(t, dir, "alt.jpg", jpegBuf.Bytes())
	fixture := loadViewerFixture(t, dir, `[Constants]
global persist $color = 0
[KeyColor]
type = cycle
$color = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\GIMI\Diffuse = ref ResourceDiffuse
if $color == 1
Resource\GIMI\Diffuse = ref ResourceAlt
endif
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceDiffuse]
filename = diffuse.png
[ResourceAlt]
filename = alt.jpg
`)
	var pngURL, jpegURL string
	for key, texture := range fixture.result.Textures {
		if strings.HasSuffix(key, "diffuse.png") {
			pngURL = texture.URL
		}
		if strings.HasSuffix(key, "alt.jpg") {
			jpegURL = texture.URL
		}
	}
	if pngURL == "" || jpegURL == "" {
		t.Fatalf("textures = %#v", fixture.result.Textures)
	}
	if got := viewerProtocolContentType(t, fixture.protocol, pngURL); got != "image/jpeg" {
		t.Fatalf("png mime = %q", got)
	}
	if got := viewerProtocolContentType(t, fixture.protocol, jpegURL); got != "image/jpeg" {
		t.Fatalf("jpeg mime = %q", got)
	}
}

func TestLoadModViewerOmitsUnreadableTextures(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "diffuse.png", []byte{0x89, 0x50, 0x4e, 0x47})
	writeTextureFile(t, dir, "alt.jpg", []byte{0xff, 0xd8, 0xff})
	result := loadViewerMod(t, dir, `[Constants]
global persist $color = 0
[KeyColor]
type = cycle
$color = 0,1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\GIMI\Diffuse = ref ResourceDiffuse
if $color == 1
Resource\GIMI\Diffuse = ref ResourceAlt
endif
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceDiffuse]
filename = diffuse.png
[ResourceAlt]
filename = alt.jpg
`)
	if texKey(result.Meshes[0]) != "" || len(result.Meshes[0].TextureVariants) != 0 {
		t.Fatalf("mesh = %#v textures=%#v", result.Meshes[0], result.Textures)
	}
}

func TestLoadModViewerOmitsDeclaredOversizedPNG(t *testing.T) {
	dir := t.TempDir()
	raw := make([]byte, 24)
	copy(raw, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	copy(raw[12:16], "IHDR")
	binary.BigEndian.PutUint32(raw[16:20], 20_000)
	binary.BigEndian.PutUint32(raw[20:24], 20_000)
	writeTextureFile(t, dir, "diffuse.png", raw)
	result := loadViewerMod(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\GIMI\Diffuse = ref ResourceDiffuse
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceDiffuse]
filename = diffuse.png
`)
	if texKey(result.Meshes[0]) != "" {
		t.Fatalf("oversized png was kept: %#v", result.Meshes[0])
	}
}

func TestLoadModViewerOmitsUnsupportedTextures(t *testing.T) {
	dir := t.TempDir()
	writeTextureFile(t, dir, "diffuse.txt", []byte("not an image"))
	result := loadViewerMod(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\GIMI\Diffuse = ref ResourceDiffuse
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceDiffuse]
filename = diffuse.txt
`)
	if texKey(result.Meshes[0]) != "" || len(result.Textures) != 0 {
		t.Fatalf("unsupported texture kept: mesh=%#v textures=%#v", result.Meshes[0], result.Textures)
	}
}

func viewerProtocolContentType(t *testing.T, protocol *infra.Protocol, path string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	protocol.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, %q", path, response.Code, response.Body.String())
	}
	return response.Header().Get("Content-Type")
}
