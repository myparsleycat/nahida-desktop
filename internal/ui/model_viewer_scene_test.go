package ui

import (
	"slices"
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
	if missing := scene.apply(states(true, -1)); !missing.empty() {
		t.Fatalf("base frame waits for %v", missing)
	}

	next := states(false, 0)
	if missing := scene.apply(next); len(missing.variants) != 1 || missing.variants[1] != 0 {
		t.Fatalf("missing = %v, want variant 0 of mesh 1", missing)
	}
	if !scene.meshes[0].state.Visible {
		t.Error("mesh 0 changed before the frame's position variant was read")
	}

	scene.storeVariant(1, 0, &viewerPositionGeometry{positions: []float32{1, 2, 3}, normals: []float32{0, 1, 0}})
	if missing := scene.apply(next); !missing.empty() {
		t.Fatalf("frame still waits for %v", missing)
	}
	if scene.meshes[0].state.Visible || scene.meshes[1].current[0] != 1 {
		t.Errorf("frame not shown: mesh 0 visible %v, mesh 1 at %v",
			scene.meshes[0].state.Visible, scene.meshes[1].current[:3])
	}

	scene.meshes[1].unreadable = map[int]bool{1: true}
	if missing := scene.apply(states(true, 1)); !missing.empty() || !scene.meshes[0].state.Visible {
		t.Errorf("an unreadable variant held the frame back: missing %v", missing)
	}
}

// A mod that animates by showing one mesh per frame loads each frame's mesh when it is first shown. The frame
// before stays until it is loaded, and the cache never drops a mesh the scene shows or is about to show.
func TestSceneWaitsForUnloadedMeshesAndKeepsShownOnes(t *testing.T) {
	t.Parallel()
	// Each mesh is over a third of the cache, so a third one loaded pushes the cache over its size.
	vertices := viewerMeshCacheBytes/(3*(6*4+4*4+viewerVertexExtraStride)) + 1
	scene := &viewerScene{}
	for range 4 {
		mesh := newViewerStub(modelviewer.ModelViewerMeshTransport{VertexCount: vertices}, false)
		scene.meshes = append(scene.meshes, mesh)
	}
	loaded := &viewerMesh{vertexCount: vertices, current: []float32{0}, geometryKey: baseGeometryKey}
	frame := func(shown int) []modelviewer.ViewerMeshState {
		states := make([]modelviewer.ViewerMeshState, len(scene.meshes))
		for i := range states {
			states[i] = modelviewer.ViewerMeshState{Visible: i == shown, PositionVariant: -1}
		}
		return states
	}
	scene.meshes[0].fill(loaded)
	scene.trackLoaded()
	if missing := scene.apply(frame(0)); !missing.empty() {
		t.Fatalf("loaded frame waits for %v", missing)
	}

	if missing := scene.apply(frame(1)); len(missing.meshes) != 1 || missing.meshes[0] != 1 {
		t.Fatalf("missing = %v, want mesh 1", missing)
	}
	if !scene.meshes[0].state.Visible || scene.meshes[1].state.Visible || !scene.waiting {
		t.Fatal("the frame changed, or stopped waiting, before its mesh was loaded")
	}
	scene.meshes[1].fill(loaded)
	if unloaded := scene.storeMesh(1); len(unloaded) != 0 {
		t.Fatalf("unloaded %v below the cache size", unloaded)
	}

	// Mesh 0 is still shown and mesh 1 is the frame waited for, so neither is dropped.
	scene.meshes[2].fill(loaded)
	if unloaded := scene.storeMesh(2); len(unloaded) != 0 {
		t.Fatalf("unloaded %v, a shown or wanted mesh", unloaded)
	}
	if missing := scene.apply(frame(2)); !missing.empty() {
		t.Fatalf("loaded frame waits for %v", missing)
	}

	// Once frame 2 shows, mesh 0 is the least recently drawn one nothing shows. The shown mesh 2 does not count
	// against the cache, so dropping mesh 0 is enough.
	scene.meshes[3].fill(loaded)
	unloaded := scene.storeMesh(3)
	if !slices.Equal(unloaded, []int{0}) || !scene.meshes[0].unloaded || scene.meshes[1].unloaded {
		t.Fatalf("unloaded %v, want mesh 0", unloaded)
	}
}
