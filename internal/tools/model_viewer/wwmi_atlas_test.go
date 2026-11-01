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
		rival   = "Textures/Components-0-1-3 t=66666666.dds"
		variant = "Textures/Components-0-1-2 t=22222222 B.dds"

		currentVariant = "Textures/Components-0-1 t=11111111 B.dds"
		renamedVariant = "Textures/Red Components-0-1-2 t=22222222.dds"
	)
	unrelatedVariants := []string{
		"Textures/Components-0-1 t=11111111 V1.dds",
		"Textures/Components-0-1 t=11111111 V2.dds",
		"Textures/Components-0-1 t=11111111 V3.dds",
	}
	for _, tt := range []struct {
		name           string
		binding        string
		inconsistent   bool
		blankCandidate bool
		blankSeams     bool
		noSeams        bool
		helperRole     string
		wrongSize      int
		correctSize    int
		prefix         string
		partialMatch   bool
		partialRows    int
		budgetPressure bool
		largeCurrent   bool
		rival          bool
		variant        bool
		currentVariant bool
		renamedVariant bool
		variantBudget  bool
		want           string
	}{
		{name: "inferred atlas", inconsistent: true, want: correct},
		{name: "smaller atlas", inconsistent: true, wrongSize: 2048, correctSize: 1024, want: correct},
		{name: "large current atlas leaves budget for rivals", inconsistent: true, largeCurrent: true, want: correct},
		{name: "current variants leave budget for rivals", inconsistent: true, variantBudget: true, want: correct},
		{
			name: "renamed variant keeps the declared default", inconsistent: true,
			partialMatch: true, partialRows: 10, renamedVariant: true, want: correct,
		},
		{name: "renamed atlas", inconsistent: true, prefix: "角色界面", want: correct},
		{name: "renamed atlas before redundant dumps", inconsistent: true, prefix: "角色界面", budgetPressure: true, want: correct},
		{
			name: "renamed atlas replaces small shared effect", inconsistent: true,
			wrongSize: 512, correctSize: 1024, prefix: "角色界面", partialMatch: true, want: correct,
		},
		{
			name: "different atlas sizes need stronger evidence", inconsistent: true,
			wrongSize: 1024, correctSize: 2048, prefix: "角色界面", partialMatch: true, want: wrong,
		},
		{
			name: "unrelated rival corroborates resized atlas", inconsistent: true,
			wrongSize: 2048, correctSize: 1024, prefix: "角色界面", partialMatch: true, partialRows: 10, rival: true,
			want: correct,
		},
		{
			name: "resized atlas without a rival stays", inconsistent: true,
			wrongSize: 2048, correctSize: 1024, prefix: "角色界面", partialMatch: true, partialRows: 10, want: wrong,
		},
		{
			name: "recolored variant keeps the first declared atlas", inconsistent: true,
			partialMatch: true, partialRows: 10, variant: true, want: correct,
		},
		{
			name: "current atlas keeps its default over its variant", inconsistent: true,
			partialMatch: true, partialRows: 10, currentVariant: true, want: wrong,
		},
		{name: "small helper map", inconsistent: true, wrongSize: 1024, correctSize: 512, want: wrong},
		{name: "ambiguous atlases", want: wrong},
		{name: "blank candidate", inconsistent: true, blankCandidate: true, want: wrong},
		{name: "blank seams with non-seam color variation", inconsistent: true, blankSeams: true, want: wrong},
		{name: "no UV seams", inconsistent: true, noSeams: true, want: wrong},
		{name: "renamed atlas without UV seams", inconsistent: true, noSeams: true, prefix: "角色界面", want: wrong},
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

			correctFile := strings.Replace(correct, "/Components-", "/"+tt.prefix+"Components-", 1)
			want := tt.want
			if want == correct {
				want = correctFile
			}
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
			decoyResources := ""
			if tt.budgetPressure {
				decoyResources = `
[ResourceDecoyA]
filename = Textures/Components-0-1-2-3 t=33333333.dds
[ResourceDecoyB]
filename = Textures/Components-0-1-2-3-4 t=44444444.dds
[ResourceDecoyC]
filename = Textures/Components-0-1-2-3-4-5 t=55555555.dds
`
			}
			files := []string{wrong, correctFile}
			if tt.rival {
				decoyResources += `
[ResourceRival]
filename = ` + rival + `
`
				files = append(files, rival)
			}
			variantResources := ""
			if tt.variant {
				variantResources = `
[ResourceVariant]
filename = ` + variant + `
`
				files = append(files, variant)
			}
			if tt.currentVariant {
				variantResources = `
[ResourceCurrentVariant]
filename = ` + currentVariant + `
`
				files = append(files, currentVariant)
			}
			if tt.renamedVariant {
				variantResources = `
[ResourceRenamedVariant]
filename = ` + renamedVariant + `
`
				files = append(files, renamedVariant)
			}
			if tt.variantBudget {
				for index, file := range unrelatedVariants {
					decoyResources += "\n[ResourceUnrelatedVariant" + modelViewerString(
						index,
					) + "]\nfilename = " + file + "\n"
				}
				files = append(files, unrelatedVariants...)
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
` + decoyResources + `
[ResourceCorrect]
filename = ` + correctFile + `
` + variantResources + `
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
			for _, file := range files {
				size := 64
				if file != correctFile && tt.wrongSize != 0 {
					size = tt.wrongSize
				}
				if file == correctFile && tt.correctSize != 0 {
					size = tt.correctSize
				}
				pixels := make([]color.NRGBA, size*size)
				for row := range size {
					for column := range size {
						x, y := column*64/size, row*64/size
						pixel := color.NRGBA{R: uint8(40 + y*3), G: uint8(30 + y*2), B: 70, A: 255}
						if file == correctFile && (tt.blankCandidate || tt.blankSeams && (x < 16 || x >= 48)) {
							pixel = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
						}
						lowerScore := file == variant || file == renamedVariant
						matching := file == correctFile || lowerScore
						if !matching && file != currentVariant && tt.inconsistent && x >= 32 {
							pixel.R = 255 - pixel.R
							pixel.G = 255 - pixel.G
						}

						// Contrast away from the seams lowers the variant's score.
						if lowerScore {
							pixel.B = uint8(70 + y)
						}
						partialRows := 16
						if tt.partialRows != 0 {
							partialRows = tt.partialRows
						}
						if matching && tt.partialMatch && x >= 32 && y < partialRows {
							pixel.R = 255 - pixel.R
							pixel.G = 255 - pixel.G
						}
						pixels[row*size+column] = pixel
					}
				}
				writeTextureFile(t, dir, file, encodeWwmiBC1MipDDS(t, size, size, pixels))
			}
			if tt.largeCurrent {
				if err := os.Truncate(filepath.Join(dir, filepath.FromSlash(wrong)), maxHintDecodeBytes); err != nil {
					t.Fatal(err)
				}
			}
			if tt.variantBudget {
				for _, file := range append([]string{correctFile}, unrelatedVariants...) {
					if err := os.Truncate(
						filepath.Join(dir, filepath.FromSlash(file)),
						maxHintDecodeBytes,
					); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tt.budgetPressure {
				data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(wrong)))
				if err != nil {
					t.Fatal(err)
				}

				// Padding charges the input byte budget without allocating large decoded atlases.
				for _, file := range []string{
					"Textures/Components-0-1-2-3 t=33333333.dds",
					"Textures/Components-0-1-2-3-4 t=44444444.dds",
					"Textures/Components-0-1-2-3-4-5 t=55555555.dds",
				} {
					writeTextureFile(t, dir, file, data)
					if err := os.Truncate(
						filepath.Join(dir, filepath.FromSlash(file)),
						maxHintDecodeBytes,
					); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Truncate(
					filepath.Join(dir, filepath.FromSlash(correctFile)),
					maxHintDecodeBytes,
				); err != nil {
					t.Fatal(err)
				}
			}
			fixture := loadViewerDir(t, dir)
			if len(fixture.result.Meshes) != 1 {
				t.Fatalf("meshes = %d, want 1", len(fixture.result.Meshes))
			}
			if got := texKey(fixture.result.Meshes[0]); !strings.Contains(got, want) {
				t.Fatalf("texture = %q, want %q", got, want)
			}
			if tt.helperRole != "" {
				mesh := fixture.result.Meshes[0]
				key := map[string]*string{
					"light_map":    mesh.LightMapKey,
					"normal_map":   mesh.NormalMapKey,
					"material_map": mesh.MaterialMapKey,
				}[tt.helperRole]
				if key == nil || !strings.Contains(*key, correctFile) {
					t.Fatalf("%s texture = %v, want %q", tt.helperRole, key, correctFile)
				}
			}
		})
	}
}
