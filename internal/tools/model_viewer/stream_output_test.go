package modelviewer

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const viewerStreamReplayINI = `
[ResourceSO]
type = Buffer
stride = 40
[ResourceCurrent]
type = Buffer
stride = 40
[ResourcePrevious]
type = Buffer
stride = 40
[CommandListReplay]
if $ready == 0
so0 = ref ResourceSO
vb0 = ResourcePos
draw = 3, 0
vb0 = ResourceSecondPosition
draw = 3, 0
so0 = null
ResourceCurrent = copy ResourceSO
ResourcePrevious = copy ResourceCurrent
endif
[ResourceSecondPosition]
filename = second.buf
stride = 40
[ResourceSecondTexcoord]
filename = second-tc.buf
stride = 20
`

func TestLoadModViewerStreamOutputPreservesTexturesAndVariableMeshes(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	second := make([]byte, 3*40)
	for i := range 3 {
		binary.LittleEndian.PutUint32(second[i*40:], math.Float32bits(float32(10+i)))
	}
	writeTextureFile(t, dir, "second.buf", second)
	writeTextureFile(t, dir, "second-tc.buf", make([]byte, 6*20))
	writeTextureFile(t, dir, "diffuse.png", encodeColorPNG(2, 2))
	ini := `[Constants]
global $ready = 0
global persist $outfit = 0
[KeyOutfit]
type = cycle
$outfit = 0, 1, 2
[TextureOverrideFirst]
if $ready == 1
ib = ResourceBodyIB
vb0 = ResourceCurrent
vb1 = ResourceTc
Resource\ZZMI\Diffuse = ref ResourceDiffuse
if $outfit == 0
drawindexed = 3, 0, 0
endif
endif
[TextureOverrideSecond]
if $ready == 1
ib = ResourceBodyIB
vb0 = ResourcePrevious
vb1 = ResourceSecondTexcoord
Resource\ZZMI\Diffuse = ref ResourceDiffuse
if $outfit == 1 || $outfit == 2
drawindexed = 3, 0, 3
endif
endif
[ResourceDiffuse]
filename = diffuse.png
` + viewerBodyResources + viewerStreamReplayINI
	writeTextureFile(t, dir, "mod.ini", []byte(ini))
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 2 || len(fixture.result.Textures) != 1 {
		t.Fatalf("meshes=%d textures=%v", len(fixture.result.Meshes), fixture.result.Textures)
	}
	for i, mesh := range fixture.result.Meshes {
		geometry := readViewerMesh(t, fixture.protocol, mesh.GeometryURL)
		if len(geometry.Positions) != 9 || len(geometry.Indices) != 3 || geometry.Positions[0] != float32(i*10) {
			t.Fatalf("mesh %d: positions=%v indices=%v", i, geometry.Positions, geometry.Indices)
		}
	}
	for value := range 3 {
		state := evaluateViewerTransport(fixture.result, map[string]any{"outfit": value})
		for i, mesh := range state.Meshes {
			wantVisible := (i == 0) == (value == 0)
			if mesh.Visible != wantVisible || mesh.Visible && mesh.TexKey != "diffuse::diffuse.png" {
				t.Fatalf("outfit=%d mesh=%+v wantVisible=%v", value, mesh, wantVisible)
			}
		}
	}
}

func TestModelViewerStreamOutputFinalizesTrailingDraw(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	second := make([]byte, 3*40)
	for i := range 3 {
		binary.LittleEndian.PutUint32(second[i*40:], math.Float32bits(float32(10+i)))
	}
	writeTextureFile(t, dir, "second.buf", second)
	writeTextureFile(t, dir, "mod.ini", []byte(`[Constants]
global $ready = 0
[TextureOverrideBody]
if $ready == 0
ib = ResourceBodyIB
vb0 = ResourceCurrent
vb1 = ResourceTc
drawindexed = 3, 0, 0
endif
[ResourceCurrent]
stride = 40
[ResourceSO]
stride = 40
[ResourceSecondPosition]
filename = second.buf
stride = 40
[CommandListReplay]
if $ready == 0
so0 = ResourceSO
vb0 = ResourceSecondPosition
draw = 3, 0
ResourceCurrent = copy ResourceSO
endif
`+viewerBodyResources))
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 1 {
		t.Fatalf("meshes=%d", len(fixture.result.Meshes))
	}
	geometry := readViewerMesh(t, fixture.protocol, fixture.result.Meshes[0].GeometryURL)
	if len(geometry.Positions) != 9 || len(geometry.Indices) != 3 || geometry.Positions[0] != 10 {
		t.Fatalf("positions=%v indices=%v", geometry.Positions, geometry.Indices)
	}
}

func TestModelViewerStreamOutputRejectsTexcoordOutsideModFolder(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	outsideName := filepath.Base(dir) + "-stream-tc.buf"
	outside := filepath.Join(filepath.Dir(dir), outsideName)
	if err := os.WriteFile(outside, make([]byte, 3*20), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	writeTextureFile(t, dir, "second.buf", make([]byte, 3*40))
	writeTextureFile(t, dir, "mod.ini", []byte(`[Constants]
global $ready = 0
[TextureOverrideBody]
if $ready == 0
ib = ResourceBodyIB
vb0 = ResourceCurrent
vb1 = ResourceTc
drawindexed = 3, 0, 0
endif
[TextureOverrideEscaping]
if $ready == 0
ib = ResourceBodyIB
vb0 = ResourceCurrent
vb1 = ResourceEscapingTc
drawindexed = 3, 0, 0
endif
[ResourceCurrent]
stride = 40
[ResourceSO]
stride = 40
[ResourceSecondPosition]
filename = second.buf
stride = 40
[ResourceEscapingTc]
filename = ..\`+outsideName+`
stride = 20
[CommandListReplay]
if $ready == 0
so0 = ResourceSO
vb0 = ResourceSecondPosition
draw = 3, 0
ResourceCurrent = copy ResourceSO
endif
`+viewerBodyResources))
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 1 {
		t.Fatalf("meshes=%d", len(fixture.result.Meshes))
	}
}

func TestModelViewerStreamOutputRejectsUnsupportedReplay(t *testing.T) {
	for _, test := range []struct{ name, old, replacement string }{
		{"nested draw", "draw = 3, 0", "if $choice\ndraw = 3, 0\nendif"},
		{"invalid range", "draw = 3, 0", "draw = 4, 0"},
		{"negative start", "draw = 3, 0", "draw = 3, -1"},
		{"shader callback", "draw = 3, 0", "run = CustomShaderUnknown\ndraw = 3, 0"},
		{"stride mismatch", "[ResourceSO]\ntype = Buffer\nstride = 40", "[ResourceSO]\ntype = Buffer\nstride = 48"},
		{"escaping source", "filename = pos.buf", "filename = ../outside.buf"},
		{
			"conflicting replay", "draw = 3, 0",
			"draw = 3, 0\nso0 = ResourceSO\nvb0 = ResourceSecondPosition\ndraw = 3, 0",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometryN(t, dir, 3)
			writeTextureFile(t, dir, "second.buf", make([]byte, 3*40))
			ini := strings.Replace(viewerBodyResources+viewerStreamReplayINI, test.old, test.replacement, 1)
			sections := parseModelViewerINI(ini, "mod.ini").Sections
			resources := make(map[string]modelViewerResource)
			for _, resource := range collectModelViewerResources(sections) {
				resources[modelViewerNormalizeKey(resource.Name)] = resource
			}
			outputs := collectModelViewerStreamOutputs(dir, sections, resources, newModelViewerBufferCache())
			if len(outputs) != 0 {
				t.Fatalf("unsupported replay produced %d outputs", len(outputs))
			}
		})
	}
}

func TestModelViewerStreamCacheSeparatesINIs(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	data := make([]byte, 3*40)
	for i := range 3 {
		binary.LittleEndian.PutUint32(data[i*40:], math.Float32bits(float32(10+i)))
	}
	writeTextureFile(t, dir, "other.buf", data)
	ini := viewerBodyResources + `
[ResourceSO]
stride = 40
[ResourceCurrent]
stride = 40
[CommandListReplay]
so0 = ResourceSO
vb0 = ResourcePos
draw = 3, 0
so0 = null
ResourceCurrent = copy ResourceSO
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourceCurrent
vb1 = ResourceTc
drawindexed = 3, 0, 0
`
	writeTextureFile(t, dir, "a.ini", []byte(ini))
	writeTextureFile(t, dir, "b.ini", []byte(strings.ReplaceAll(ini, "filename = pos.buf", "filename = other.buf")))
	fixture := loadViewerDir(t, dir)
	if len(fixture.result.Meshes) != 2 {
		t.Fatalf("meshes=%d", len(fixture.result.Meshes))
	}
	for i, mesh := range fixture.result.Meshes {
		positions := readViewerMesh(t, fixture.protocol, mesh.GeometryURL).Positions
		if positions[0] != float32(i*10) {
			t.Fatalf("mesh %s: x=%v want=%d", mesh.ID, positions[0], i*10)
		}
	}
}

func TestModelViewerStreamPositionBranches(t *testing.T) {
	for _, test := range []struct {
		name, assignments string
		want              bool
	}{
		{"false", "if 0\nvb0 = ResourceOther\nendif", true},
		{"constant expression", "if 2 < 1\nvb0 = ResourceOther\nendif", true},
		{"inactive else", "if 1\nvb0 = ResourcePos\nelse\nvb0 = ResourceOther\nendif", true},
		{"else if", "if 0\nvb0 = ResourceOther\nelse if 1\nvb0 = ResourcePos\nelse\nvb0 = ResourceOther\nendif", true},
		{"elif", "if 0\nvb0 = ResourceOther\nelif 1\nvb0 = ResourcePos\nendif", true},
		{"nested inactive", "if 0\nif $choice\nvb0 = ResourceOther\nendif\nendif", true},
		{"unknown", "if $choice\nvb0 = ResourceOther\nendif", false},
		{"unknown else", "if $choice\nvb0 = ResourcePos\nelse\nvb0 = ResourceOther\nendif", false},
		{"overwritten unknown", "if $choice\nvb0 = ResourceOther\nendif\nvb0 = ResourcePos", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeViewerGeometryN(t, dir, 3)
			data := make([]byte, 3*40)
			binary.LittleEndian.PutUint32(data, math.Float32bits(10))
			writeTextureFile(t, dir, "other.buf", data)
			ini := viewerBodyResources + `
[ResourceOther]
filename = other.buf
stride = 40
[ResourceSO]
stride = 40
[CommandListReplay]
vb0 = ResourcePos
` + test.assignments + `
so0 = ResourceSO
draw = 3, 0
so0 = null
`
			sections := parseModelViewerINI(ini, "mod.ini").Sections
			resources := make(map[string]modelViewerResource)
			for _, resource := range collectModelViewerResources(sections) {
				resources[modelViewerNormalizeKey(resource.Name)] = resource
			}
			outputs := collectModelViewerStreamOutputs(dir, sections, resources, newModelViewerBufferCache())
			output, exists := outputs["so"]
			if exists != test.want {
				t.Fatalf("output exists=%v want=%v", exists, test.want)
			}
			if exists && math.Float32frombits(binary.LittleEndian.Uint32(output.data)) != 0 {
				t.Fatal("stream used a position from an inactive branch")
			}
		})
	}
}

func TestModelViewerStreamOutputRejectsLinkedSourceOutsideModFolder(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	outsideDir := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-linked")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outsideDir) })
	other := make([]byte, 3*40)
	binary.LittleEndian.PutUint32(other, math.Float32bits(10))
	if err := os.WriteFile(filepath.Join(outsideDir, "other.buf"), other, 0o600); err != nil {
		t.Fatal(err)
	}
	createViewerDirectoryLink(t, "junction", outsideDir, filepath.Join(dir, "linked"))
	ini := viewerBodyResources + `
[ResourceOther]
filename = linked\other.buf
stride = 40
[ResourceSO]
stride = 40
[CommandListReplay]
so0 = ResourceSO
vb0 = ResourceOther
draw = 3, 0
so0 = null
`
	sections := parseModelViewerINI(ini, "mod.ini").Sections
	resources := make(map[string]modelViewerResource)
	for _, resource := range collectModelViewerResources(sections) {
		resources[modelViewerNormalizeKey(resource.Name)] = resource
	}
	outputs := collectModelViewerStreamOutputs(dir, sections, resources, newModelViewerBufferCache())
	if len(outputs) != 0 {
		t.Fatalf("directory link outside the mod folder produced %d streams", len(outputs))
	}
}

func TestModelViewerStreamOutputKeepsStreamAcrossInactiveElse(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometryN(t, dir, 3)
	other := make([]byte, 3*40)
	binary.LittleEndian.PutUint32(other, math.Float32bits(10))
	writeTextureFile(t, dir, "other.buf", other)
	ini := viewerBodyResources + `
[ResourceOther]
filename = other.buf
stride = 40
[ResourceSO]
stride = 40
[CommandListReplay]
if $ready == 0
if 1
so0 = ResourceSO
vb0 = ResourcePos
draw = 3, 0
else
vb0 = ResourceOther
draw = 3, 0
endif
endif
`
	sections := parseModelViewerINI(ini, "mod.ini").Sections
	resources := make(map[string]modelViewerResource)
	for _, resource := range collectModelViewerResources(sections) {
		resources[modelViewerNormalizeKey(resource.Name)] = resource
	}
	outputs := collectModelViewerStreamOutputs(dir, sections, resources, newModelViewerBufferCache())
	output, exists := outputs["so"]
	if !exists {
		t.Fatal("the inactive else arm discarded the bound stream")
	}
	if output.stride != 40 || len(output.data) != 3*40 {
		t.Fatalf("stream = %d bytes stride %d", len(output.data), output.stride)
	}
	if first := math.Float32frombits(binary.LittleEndian.Uint32(output.data)); first != 0 {
		t.Fatalf("stream reused the position of the inactive else arm: %v", first)
	}
}

func TestModelViewerClaretLocal(t *testing.T) {
	dir := os.Getenv("MODEL_VIEWER_CLARET_PATH")
	if dir == "" {
		t.Skip("set MODEL_VIEWER_CLARET_PATH to verify the local mod")
	}
	fixture := loadViewerDir(t, filepath.Clean(dir))
	if len(fixture.result.Meshes) != 17 || len(fixture.result.Textures) != 4 {
		t.Fatalf("meshes=%d textures=%d", len(fixture.result.Meshes), len(fixture.result.Textures))
	}
	for _, mesh := range fixture.result.Meshes {
		geometry := readViewerMesh(t, fixture.protocol, mesh.GeometryURL)
		if len(geometry.Positions) == 0 || len(geometry.Indices) == 0 {
			t.Fatalf("empty geometry: %s", mesh.ID)
		}
	}
	wantVisible := []int{12, 12, 12, 11, 11, 11, 10, 10, 10, 9, 10, 9, 8, 10, 9, 8}
	for fg := -1; fg <= 14; fg++ {
		for rd := -1; rd <= 0; rd++ {
			state := evaluateViewerTransport(fixture.result, map[string]any{"swapvar_fg": fg, "swapvar_rd": rd})
			visible, textured := 0, 0
			for _, mesh := range state.Meshes {
				if mesh.Visible {
					visible++
					if mesh.TexKey != "" {
						textured++
					}
				}
			}
			want := wantVisible[fg+1] + rd*2
			// The xbsB override intentionally does not bind a replacement texture.
			if visible != want || textured != want-1 {
				t.Fatalf("fg=%d rd=%d visible=%d textured=%d want=%d", fg, rd, visible, textured, want)
			}
		}
	}
}
