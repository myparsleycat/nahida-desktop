package modelviewer

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const mergedSkinTestShader = `
StructuredBuffer<ZZVertex40> src_rows : register(t0);
RWStructuredBuffer<float> dst_rows : register(u0);
void main() {
    uint dest_start = (uint)param.y;
    zz_write_row(dest_start + index, sp, sn, st, tw, stride_f);
}
`

const mergedSkinTestINI = `
[TextureOverrideVB]
hash = aaaaaaaa
vb0 = ResourceBodyPosition
run = CustomShaderSkin

[TextureOverrideIB]
hash = c2b4ce3a
handling = skip
ib = ResourceBodyIB
vb0 = ResourceBodyPosition
vb1 = ResourceRedirectTexcoord
drawindexed = 3, 0, 2

[CustomShaderSkin]
cs = skin.hlsl
x1 = 3
y1 = 2
cs-t0 = ref ResourceBodyPosition
cs-u0 = ref ResourceMerged
Dispatch = 1, 1, 1

[ResourceMerged]
type = Buffer
stride = 40

[ResourceBodyPosition]
type = Buffer
stride = 40
filename = pos.buf

[ResourceRedirectTexcoord]
type = Buffer
stride = 8
filename = tc.buf

[ResourceBodyIB]
type = Buffer
format = DXGI_FORMAT_R32_UINT
filename = body.ib
`

func TestBuildModelViewerDirectMeshesPlacesMergedSkinDrawOnCarrierRows(t *testing.T) {
	dir := t.TempDir()
	floats := func(values ...float32) []byte {
		var output []byte
		for _, value := range values {
			output = binary.LittleEndian.AppendUint32(output, math.Float32bits(value))
		}
		return output
	}
	positions := make([]byte, 0, 120)
	for vertex := range 3 {
		positions = append(positions, floats(float32(vertex+1), 0, 0, 0, 1, 0, 1, 0, 0, 1)...)
	}
	// Two rows lead the texcoords, for the merged rows before this carrier.
	texcoords := floats(0.9, 0.9, 0.9, 0.9, 0.1, 0, 0.2, 0, 0.3, 0)
	ini := filepath.Join(dir, "mod.ini")
	for path, content := range map[string][]byte{
		ini:                             []byte(mergedSkinTestINI),
		filepath.Join(dir, "skin.hlsl"): []byte(mergedSkinTestShader),
		filepath.Join(dir, "pos.buf"):   positions,
		filepath.Join(dir, "tc.buf"):    texcoords,
		filepath.Join(dir, "body.ib"): binary.LittleEndian.AppendUint32(
			binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 0), 1), 2,
		),
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	meshes, _, _, _, err := buildModelViewerDirectMeshes(ini, "", parseModINI(mergedSkinTestINI))
	if err != nil {
		t.Fatal(err)
	}
	if len(meshes) != 1 {
		t.Fatalf("meshes = %+v", meshes)
	}
	geometry := meshes[0].geometry
	if !slices.Equal(geometry.Position[:9], []float32{1, 0, 0, 2, 0, 0, 3, 0, 0}) {
		t.Fatalf("positions = %v", geometry.Position)
	}
	if !slices.Equal(geometry.Texcoord0, []float32{0.1, 1, 0.2, 1, 0.3, 1}) {
		t.Fatalf("texcoords = %v", geometry.Texcoord0)
	}
	vertices, err := meshes[0].drawSource.TriangleVertices(0)
	if err != nil || vertices != [3]uint32{0, 1, 2} || meshes[0].drawSource.BaseVertex != 2 {
		t.Fatalf("vertices = %v, base = %d, err = %v", vertices, meshes[0].drawSource.BaseVertex, err)
	}
}
