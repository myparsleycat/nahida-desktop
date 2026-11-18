package ui

import (
	"context"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// A rebuilt component carries an empty mesh for each of its objects without triangles, including ones the scene
// never had. Those must not open the scene again on every edit, while a mesh the scene has takes the empty one. A
// face mesh that loses its triangles no longer counts as a face, and the original model's match goes stale.
func TestApplyLiveMeshesTakesEmptyMeshesInPlace(t *testing.T) {
	shown := modelviewer.ModelViewerMeshTransport{ID: "blender:uid:1", GeometryURL: "old", IndexCount: 6}
	scene := &viewerScene{
		transport: modelviewer.ModelViewerTransport{
			MemorySessionID: "session", MaterialProfile: gimiProfile,
			Meshes: []modelviewer.ModelViewerMeshTransport{shown},
		},
		meshes:      []*viewerMesh{{info: shown, unloaded: true, toonFace: true}},
		genshinFace: true,
	}
	// v.tools is nil, so a reopen fails before reaching the backend; the cancelled context keeps that failure from
	// being reported through the nil v.log.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	building := true
	v := &ModelViewer{
		ctx: ctx, scene: scene, blender: &viewerBlenderSource{link: 1},
		originalCancel: func() { building = false }, originalLoading: viewerCharacterKey{scene: scene, character: "c"},
	}

	v.applyLiveMeshes(modelviewer.ModelViewerLiveUpdate{
		MemorySessionID: "session",
		Meshes: []modelviewer.ModelViewerLiveMesh{
			{ID: "blender:uid:1", GeometryURL: "empty"},
			{ID: "blender:uid:3", GeometryURL: "never shown"},
		},
	})

	if v.loading || v.scene != scene {
		t.Fatal("an empty mesh the scene does not have opened the scene again")
	}
	if got := scene.transport.Meshes[0]; got.GeometryURL != "empty" || got.IndexCount != 0 {
		t.Errorf("shown mesh = %+v, want the empty geometry", got)
	}
	if got := scene.meshes[0].info; got.GeometryURL != "empty" || got.IndexCount != 0 {
		t.Errorf("unloaded mesh info = %+v, want the empty geometry", got)
	}
	if building || !scene.originalStale {
		t.Errorf("original build running = %v, stale = %v; want the build cancelled and the match stale",
			building, scene.originalStale)
	}
	if scene.genshinFace {
		t.Error("the emptied face mesh still counts as a face")
	}
}
