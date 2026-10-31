package modelviewer

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestLoadModViewerIgnoresIncompleteTriangleIndices(t *testing.T) {
	for _, count := range []int{4, 5} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometry(t, dir)
			indices := []uint32{0, 1, 2, 3, 4}[:count]
			ini := "[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n" +
				"drawindexed = auto\n" + viewerBodyResources
			for name, data := range map[string][]byte{
				"mod.ini": []byte(ini), "body.ib": modelViewerUint32Bytes(indices),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			fixture := loadViewerDir(t, dir)
			if len(fixture.result.Meshes) != 1 {
				t.Fatalf("meshes = %v", fixture.result.Meshes)
			}
			mesh := fixture.result.Meshes[0]
			geometry := readViewerMesh(t, fixture.protocol, mesh.GeometryURL)
			if !slices.Equal(geometry.Indices, []uint32{0, 1, 2}) || len(geometry.Positions) != 9 {
				t.Fatalf("geometry = %+v", geometry)
			}
			source, ok := fixture.service.DrawSource(fixture.result.MemorySessionID, mesh.ID)
			vertices, err := source.TriangleVertices(0)
			if !ok || err != nil || vertices != [3]uint32{0, 1, 2} {
				t.Fatalf("source vertices=%v found=%t err=%v", vertices, ok, err)
			}
			if raw, err := os.ReadFile(
				filepath.Join(dir, "body.ib"),
			); err != nil ||
				!bytes.Equal(raw, modelViewerUint32Bytes(indices)) {
				t.Fatalf("source indices changed: %v", err)
			}
		})
	}
}

func TestLoadModViewerOmitsNonFiniteTriangles(t *testing.T) {
	for _, attribute := range []string{"position", "tangent", "normal"} {
		t.Run(attribute, func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometryN(t, dir, 32)
			position := make([]byte, 32*40)
			for vertex := range 32 {
				binary.LittleEndian.PutUint32(position[vertex*40+16:], math.Float32bits(1))
				binary.LittleEndian.PutUint32(position[vertex*40+24:], math.Float32bits(1))
				binary.LittleEndian.PutUint32(position[vertex*40+36:], math.Float32bits(1))
			}
			binary.LittleEndian.PutUint32(position, math.Float32bits(float32(math.NaN())))
			binary.LittleEndian.PutUint32(position[4*40:], math.Float32bits(1))
			binary.LittleEndian.PutUint32(position[5*40+4:], math.Float32bits(1))
			if attribute == "tangent" {
				binary.LittleEndian.PutUint32(position[3*40+24:], math.Float32bits(float32(math.NaN())))
			}
			if attribute == "normal" {
				binary.LittleEndian.PutUint32(position[3*40+16:], math.Float32bits(float32(math.Inf(1))))
			}
			ini := "[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n" +
				"drawindexed = 6, 0, 0\n" + viewerBodyResources
			for name, data := range map[string][]byte{
				"mod.ini": []byte(ini), "pos.buf": position,
				"body.ib": modelViewerUint32Bytes([]uint32{0, 1, 2, 3, 4, 5}),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var logs bytes.Buffer
			protocol := infra.NewProtocol()
			service := NewWithOptions(Options{
				Protocol: protocol, Log: infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true}),
			})
			for _, load := range []func(context.Context, string) (ModelViewerTransport, error){
				service.LoadModViewer, service.LoadModGridPreview,
			} {
				result, err := load(context.Background(), dir)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _, _ = service.CleanupModelViewer(context.Background(), result.MemorySessionID) })
				if len(result.Meshes) != 1 {
					t.Fatalf("meshes = %v", result.Meshes)
				}
				mesh := result.Meshes[0]
				geometry := readViewerMesh(t, protocol, mesh.GeometryURL)
				if !slices.Equal(geometry.Positions, []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}) ||
					!slices.Equal(geometry.Indices, []uint32{0, 1, 2}) {
					t.Fatalf("geometry = %+v", geometry)
				}
				if mesh.Bounds == nil || mesh.Bounds.Min != [3]float64{} || mesh.Bounds.Max != [3]float64{1, 1, 0} {
					t.Fatalf("bounds = %+v", mesh.Bounds)
				}
				if attribute == "position" && len(geometry.Tangents) != 12 ||
					attribute != "position" && len(geometry.Tangents) != 0 {
					t.Fatalf("tangents = %v", geometry.Tangents)
				}
				if attribute == "normal" && !slices.Equal(geometry.Normals, []float32{0, 0, 1, 0, 0, 1, 0, 0, 1}) {
					t.Fatalf("generated normals = %v", geometry.Normals)
				}
				if sources := readModelViewerProtocolBytes(
					t,
					protocol,
					mesh.SourceIndicesURL,
				); !bytes.Equal(
					sources,
					modelViewerUint32Bytes([]uint32{3, 4, 5}),
				) {
					t.Fatalf("source indices = %v", sources)
				}
				source, ok := service.DrawSource(result.MemorySessionID, mesh.ID)
				vertices, err := source.TriangleVertices(0)
				if !ok || err != nil || vertices != [3]uint32{3, 4, 5} {
					t.Fatalf("picked source vertices = %v, found = %t, err = %v", vertices, ok, err)
				}
			}
			if !strings.Contains(logs.String(), `"removedTriangles":1`) ||
				!strings.Contains(logs.String(), `"firstInvalidSourceVertex":0`) {
				t.Fatalf("recovery diagnostics = %s", logs.String())
			}
			if raw, err := os.ReadFile(filepath.Join(dir, "pos.buf")); err != nil || !bytes.Equal(raw, position) {
				t.Fatalf("source buffer changed: %v", err)
			}
		})
	}
}

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
	normals := readViewerMesh(t, fixture.protocol, mesh.GeometryURL).Normals
	tangents := readViewerMesh(t, fixture.protocol, mesh.GeometryURL).Tangents
	if len(normals) != 9 || normals[0] != 0 || normals[1] != 1 || normals[2] != 0 {
		t.Fatalf("normals = %v", normals)
	}
	if len(tangents) != 12 || tangents[0] != 1 || tangents[3] != -1 {
		t.Fatalf("tangents = %v", tangents)
	}
}

func TestLoadModViewerDoesNotTreatZeroPaddedFramesAsAuthoredTBN(t *testing.T) {
	dir := t.TempDir()
	fixture := loadViewerFixture(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
`+viewerBodyResources)
	mesh := readViewerMesh(t, fixture.protocol, fixture.result.Meshes[0].GeometryURL)
	if len(mesh.Normals) == 0 || len(mesh.Tangents) != 0 {
		t.Fatalf("expected generated normals without authored tangents: %#v", mesh)
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
