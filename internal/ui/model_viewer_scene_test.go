package ui

import (
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// A frame changes every mesh at once: while a position variant it selects is still to be read, the scene keeps
// the frame before it. A variant that cannot be read holds nothing back.
func TestSceneApplyCommitsMeshesTogether(t *testing.T) {
	t.Parallel()
	mesh := func() *viewerMesh {
		return &viewerMesh{
			vertexCount: 1, positions: []float32{0, 0, 0}, normals: []float32{0, 0, 1},
			variants: map[int]*viewerPositionGeometry{},
		}
	}
	scene := &viewerScene{meshes: []*viewerMesh{mesh(), mesh()}}
	states := func(firstVisible bool, secondVariant int) []modelviewer.ViewerMeshState {
		return []modelviewer.ViewerMeshState{
			{Visible: firstVisible, PositionVariant: -1},
			{Visible: true, PositionVariant: secondVariant},
		}
	}
	if missing := scene.apply(states(true, -1)); len(missing) != 0 {
		t.Fatalf("base frame waits for %v", missing)
	}

	next := states(false, 0)
	if missing := scene.apply(next); len(missing) != 1 || missing[1] != 0 {
		t.Fatalf("missing = %v, want variant 0 of mesh 1", missing)
	}
	if !scene.meshes[0].state.Visible {
		t.Error("mesh 0 changed before the frame's position variant was read")
	}

	scene.storeVariant(1, 0, &viewerPositionGeometry{positions: []float32{1, 2, 3}, normals: []float32{0, 1, 0}})
	if missing := scene.apply(next); len(missing) != 0 {
		t.Fatalf("frame still waits for %v", missing)
	}
	if scene.meshes[0].state.Visible || scene.meshes[1].current[0] != 1 {
		t.Errorf("frame not shown: mesh 0 visible %v, mesh 1 at %v",
			scene.meshes[0].state.Visible, scene.meshes[1].current[:3])
	}

	scene.meshes[1].unreadable = map[int]bool{1: true}
	if missing := scene.apply(states(true, 1)); len(missing) != 0 || !scene.meshes[0].state.Visible {
		t.Errorf("an unreadable variant held the frame back: missing %v", missing)
	}
}
