package modelviewer

import (
	"encoding/binary"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadModViewerMatchesSharedAtlasToUVSeams(t *testing.T) {
	t.Parallel()
	const (
		wrong   = "Textures/Components-0-1 t=11111111.dds"
		correct = "Textures/Components-0-1-2 t=22222222.dds"
	)
	for _, tt := range []struct {
		name           string
		binding        string
		inconsistent   bool
		blankCandidate bool
		blankSeams     bool
		noSeams        bool
		helperRole     string
		want           string
	}{
		{name: "inferred atlas", inconsistent: true, want: correct},
		{name: "ambiguous atlases", want: wrong},
		{name: "blank candidate", inconsistent: true, blankCandidate: true, want: wrong},
		{name: "blank seams with non-seam color variation", inconsistent: true, blankSeams: true, want: wrong},
		{name: "no UV seams", inconsistent: true, noSeams: true, want: wrong},
		{name: "authored atlas", inconsistent: true, binding: `Resource\WWMI\Diffuse = ref ResourceWrong`, want: wrong},
		{
			name: "authored light map", inconsistent: true,
			binding: `Resource\WWMI\LightMap = ref ResourceCorrect`, helperRole: "light_map", want: wrong,
		},
		{
			name: "authored normal map", inconsistent: true,
			binding: `Resource\WWMI\NormalMap = ref ResourceCorrect`, helperRole: "normal_map", want: wrong,
		},
		{
			name: "authored material map", inconsistent: true,
			binding: `Resource\WWMI\MaterialMap = ref ResourceCorrect`, helperRole: "material_map", want: wrong,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			vertexCount := 64
			if tt.blankSeams {
				vertexCount = 96
			}
			position := make([]byte, vertexCount*12)
			vector := make([]byte, vertexCount*8)
			texcoord := make([]byte, vertexCount*16)
			indices := make([]uint32, (vertexCount+2)/3*3)
			for vertex := range vertexCount {
				binary.LittleEndian.PutUint32(position[vertex*12:], math.Float32bits(float32(vertex/2)))
				if tt.noSeams || vertex >= 64 {
					binary.LittleEndian.PutUint32(position[vertex*12:], math.Float32bits(float32(vertex)))
				}
				binary.LittleEndian.PutUint32(position[vertex*12+4:], math.Float32bits(float32(vertex/2%2)))
				copy(vector[vertex*8:], []byte{127, 0, 0, 127, 0, 0, 127, 127})
				u := float32(0.125)
				if vertex%2 == 1 {
					u = 0.875
				}
				v := float32(vertex/2) / 32
				if vertex >= 64 {
					u, v = 0.5, float32(vertex-64)/32
				}
				binary.LittleEndian.PutUint16(texcoord[vertex*16:], modelViewerFloatToHalfBits(u))
				binary.LittleEndian.PutUint16(texcoord[vertex*16+2:], modelViewerFloatToHalfBits(v))
				indices[vertex] = uint32(vertex)
			}
			for index := vertexCount; index < len(indices); index++ {
				indices[index] = uint32(vertexCount - 1)
			}
			for name, data := range map[string][]byte{
				"pos.buf": position,
				"vec.buf": vector,
				"tc.buf":  texcoord,
				"body.ib": modelViewerUint32Bytes(indices),
				"mod.ini": []byte(`[Constants]
global $required_wwmi_version = 1
[TextureOverrideComponent0]
ib = ResourceIndexBuffer
vb0 = ResourcePositionBuffer
vb1 = ResourceVectorBuffer
vb2 = ResourceTexcoordBuffer
` + tt.binding + `
drawindexed = ` + modelViewerString(len(indices)) + `, 0, 0
[ResourceWrong]
filename = ` + wrong + `
[ResourceCorrect]
filename = ` + correct + `
[ResourceIndexBuffer]
filename = body.ib
format = DXGI_FORMAT_R32_UINT
[ResourcePositionBuffer]
filename = pos.buf
stride = 12
[ResourceVectorBuffer]
filename = vec.buf
stride = 8
[ResourceTexcoordBuffer]
filename = tc.buf
stride = 16
`),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range []string{wrong, correct} {
				pixels := make([]color.NRGBA, 64*64)
				for y := range 64 {
					for x := range 64 {
						pixel := color.NRGBA{R: uint8(40 + y*3), G: uint8(30 + y*2), B: 70, A: 255}
						if file == correct && (tt.blankCandidate || tt.blankSeams && (x < 16 || x >= 48)) {
							pixel = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
						}
						if file == wrong && tt.inconsistent && x >= 32 {
							pixel.R = 255 - pixel.R
							pixel.G = 255 - pixel.G
						}
						pixels[y*64+x] = pixel
					}
				}
				writeTextureFile(t, dir, file, encodeWwmiBC1MipDDS(t, 64, 64, pixels))
			}
			fixture := loadViewerDir(t, dir)
			if len(fixture.result.Meshes) != 1 {
				t.Fatalf("meshes = %d, want 1", len(fixture.result.Meshes))
			}
			if got := texKey(fixture.result.Meshes[0]); !strings.Contains(got, tt.want) {
				t.Fatalf("texture = %q, want %q", got, tt.want)
			}
			if tt.helperRole != "" {
				mesh := fixture.result.Meshes[0]
				key := map[string]*string{
					"light_map":    mesh.LightMapKey,
					"normal_map":   mesh.NormalMapKey,
					"material_map": mesh.MaterialMapKey,
				}[tt.helperRole]
				if key == nil || !strings.Contains(*key, correct) {
					t.Fatalf("%s texture = %v, want %q", tt.helperRole, key, correct)
				}
			}
		})
	}
}
