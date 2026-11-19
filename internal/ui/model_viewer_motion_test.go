package ui

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"slices"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestMotionBounds(t *testing.T) {
	// Node 0 moves its box 10 along x; the last box holds the vertices weighted to no node and stays.
	boxes := []viewerMotionBox{
		{minimum: vec3{1, 0, 0}, maximum: vec3{2, 1, 1}, ok: true},
		{},
		{minimum: vec3{-1, -1, -1}, maximum: vec3{0, 0, 0}, ok: true},
	}
	bones := [][12]float32{{1, 0, 0, 10, 0, 1, 0, 0, 0, 0, 1, 0}, viewerMotionIdentity}

	minimum, maximum, ok := motionBounds(boxes, bones)
	if !ok || minimum != (vec3{-1, -1, -1}) || maximum != (vec3{12, 1, 1}) {
		t.Fatalf("bounds %v..%v (ok %v), want [-1 -1 -1]..[12 1 1]", minimum, maximum, ok)
	}
}

func TestAnchorNode(t *testing.T) {
	// neck -> head -> {jaw, eye}; the jaw carries the most weight of any one node.
	parents := []int32{-1, 0, 1, 1}

	if got := anchorNode([]float64{5, 30, 40, 25}, parents); got != 1 {
		t.Fatalf("face anchors on node %d, want the head (1)", got)
	}
	if got := anchorNode([]float64{0, 0, 25, 0}, parents); got != 2 {
		t.Fatalf("jaw-only mesh anchors on node %d, want the jaw (2)", got)
	}
	if got := anchorNode(make([]float64, 4), parents); got != -1 {
		t.Fatalf("unweighted mesh anchors on node %d, want -1", got)
	}
}

func TestPackMotionBlend(t *testing.T) {
	// Groups 0 and 1 map to nodes 7 and 300; group 2 has no node and group 3 is past the palette.
	palette := []int32{7, 300, -1}
	blend := []uint16{0, 1, 2, 3, 40000, 20000, 5535, 0}

	got := packMotionBlend(blend, palette)
	want := []uint32{
		7 | 300<<16,
		viewerMotionNoNode | viewerMotionNoNode<<16,
		40000 | 20000<<16,
		5535,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("packed %#x, want %#x", got, want)
	}
}

type fakeMotionLoader struct {
	motion *modelviewer.CharacterMotion
}

func (f fakeMotionLoader) LoadCharacterMotion(
	context.Context, string, string, modelviewer.CharacterFileProgress,
) (*modelviewer.CharacterMotion, error) {
	return f.motion, nil
}

func testMVG3SingleVertex(p [3]float32) []byte {
	var buf bytes.Buffer
	buf.WriteString("MVG3")
	for _, count := range [7]uint32{3, 3, 0, 0, 0, 0, 3} {
		_ = binary.Write(&buf, binary.LittleEndian, count)
	}
	_ = binary.Write(&buf, binary.LittleEndian, p)
	_ = binary.Write(&buf, binary.LittleEndian, [3]float32{0, 0, 1})
	_ = binary.Write(&buf, binary.LittleEndian, [3]uint32{0, 0, 0})
	return buf.Bytes()
}

func testRawBlend(groups, weights [4]uint16) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, groups)
	_ = binary.Write(&buf, binary.LittleEndian, weights)
	return buf.Bytes()
}

func TestLoadViewerMotionUsesSnapshot(t *testing.T) {
	motion := &modelviewer.CharacterMotion{
		FPS: 60,
		Clips: []modelviewer.CharacterMotionClip{
			{Name: "idle", Group: "idle", Frames: 2, Loop: true},
		},
	}
	sources := []viewerMotionSource{
		{
			info:      modelviewer.ModelViewerMeshTransport{ID: "body", BlendURL: "blend://0", BlendPalette: "body"},
			positions: []float32{1, 2, 3},
		},
		{
			info: modelviewer.ModelViewerMeshTransport{
				ID:     "prop",
				Bounds: &modelviewer.ModelViewerBounds{Min: vec3{-1, -2, -3}, Max: vec3{4, 5, 6}},
			},
		},
		{
			info: modelviewer.ModelViewerMeshTransport{
				ID: "hair", GeometryURL: "geom://2", BlendURL: "blend://2", BlendPalette: "hair",
			},
			averaged: true,
		},
	}
	blendBytes := testRawBlend([4]uint16{0, 0, 0, 0}, [4]uint16{65535, 0, 0, 0})
	geomBytes := testMVG3SingleVertex([3]float32{0, 1, 0})
	read := func(_ context.Context, url string) ([]byte, error) {
		switch url {
		case "blend://0", "blend://2":
			return blendBytes, nil
		case "geom://2":
			return geomBytes, nil
		default:
			t.Fatalf("unexpected read of %s", url)
			return nil, nil
		}
	}

	m, err := loadViewerMotion(t.Context(), fakeMotionLoader{motion: motion}, read, sources, "GIMI", "amber", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.scene != nil {
		t.Error("loadViewerMotion attached a scene before UI-thread install")
	}
	if len(m.meshes) != 2 || len(m.clips) != 1 || !slices.Equal(m.clipBoxesValid, []bool{true}) {
		t.Fatalf("loaded meshes=%d clips=%d valid=%v; want 2 meshes, 1 clip, [true]",
			len(m.meshes), len(m.clips), m.clipBoxesValid)
	}
	if !m.restBox.ok || m.restBox.minimum != (vec3{-1, -2, -3}) || m.restBox.maximum != (vec3{4, 5, 6}) {
		t.Errorf("restBox = %+v, want [-1 -2 -3]..[4 5 6]", m.restBox)
	}

	liveMeshes := make([]*viewerMesh, len(sources))
	for i, src := range sources {
		liveMeshes[i] = &viewerMesh{info: src.info}
	}
	if changed, ok := changedMotionSources(liveMeshes, sources); !ok || len(changed) != 0 {
		t.Errorf("changedMotionSources() = %v, %v for matching meshes, want [], true", changed, ok)
	}
	liveMeshes[1].info.BlendPalette = "updated"
	if changed, ok := changedMotionSources(liveMeshes, sources); !ok || !slices.Equal(changed, []int{1}) {
		t.Errorf("changedMotionSources() = %v, %v after a palette change, want [1], true", changed, ok)
	}
	if _, ok := changedMotionSources(liveMeshes[:1], sources); ok {
		t.Error("changedMotionSources() reported ok for a scene with another number of meshes")
	}
}

func TestAddMotionBoxes(t *testing.T) {
	boxes := make([]viewerMotionBox, 3)
	totals := make([]float64, 2)
	palette := []int32{0, -1}
	positions := []float32{
		1, 2, 3,
		10, 10, 10,
		float32(math.NaN()), 0, 0,
		0, float32(math.Inf(1)), 0,
	}
	blend := []uint16{
		0, 1, 0, 0, 40000, 25535, 0, 0,
		1, 0, 0, 0, 65535, 0, 0, 0,
		0, 0, 0, 0, 65535, 0, 0, 0,
		1, 0, 0, 0, 65535, 0, 0, 0,
	}

	addMotionBoxes(boxes, totals, positions, blend, palette)
	if !boxes[0].ok || boxes[0].minimum != (vec3{1, 2, 3}) || boxes[0].maximum != (vec3{1, 2, 3}) {
		t.Errorf("mapped box = %+v, want [1 2 3]..[1 2 3]", boxes[0])
	}
	if !boxes[2].ok || boxes[2].minimum != (vec3{10, 10, 10}) || boxes[2].maximum != (vec3{10, 10, 10}) {
		t.Errorf("unmapped box = %+v, want [10 10 10]..[10 10 10] without mixed or non-finite vertices", boxes[2])
	}
	if totals[0] != 40000 {
		t.Errorf("totals[0] = %v, want 40000", totals[0])
	}
}
