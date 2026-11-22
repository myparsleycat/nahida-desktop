package ui

import (
	"context"
	"slices"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestGPURecoveryKeepsPendingTextureWithoutDevice(t *testing.T) {
	t.Parallel()
	previous := &viewerGPUSet{}
	scene := &viewerScene{gpu: previous}
	v := &ModelViewer{scene: scene, liveTextureWanted: map[string]string{"diffuse": "latest"}}
	if v.liveUploadReady(scene, previous, 0) {
		t.Error("the live upload accepted a GPU set without a device")
	}

	mixed := &viewerGPUSet{textures: map[string]viewerTextureGPU{"diffuse": {}}}
	textures := map[string]modelviewer.ModelViewerTextureTransport{"diffuse": {URL: "latest"}}
	if v.installRecoveredGPU(scene, mixed, 0, textures) || scene.gpu != previous {
		t.Error("recovery installed mixed-generation GPU resources without a device")
	}
	if got := v.liveTextureWanted["diffuse"]; got != "latest" {
		t.Errorf("pending texture after device loss = %q, want latest", got)
	}

	recovered := &viewerGPUSet{generation: 1, textures: map[string]viewerTextureGPU{"diffuse": {}}}
	if !v.installRecoveredGPU(scene, recovered, 1, textures) || scene.gpu != recovered {
		t.Fatal("recovery did not install the current GPU set")
	}
	if _, pending := v.liveTextureWanted["diffuse"]; pending {
		t.Error("the texture installed on the replacement device is still pending")
	}
}

func TestLiveUpdatesSurviveGPURecovery(t *testing.T) {
	const generation = 1
	oldGPU := &viewerGPUSet{generation: generation}
	info := modelviewer.ModelViewerMeshTransport{ID: "body", GeometryURL: "before-recovery", IndexCount: 3}
	scene := &viewerScene{
		transport: modelviewer.ModelViewerTransport{
			MemorySessionID: "session", Meshes: []modelviewer.ModelViewerMeshTransport{info},
		},
		gpu: oldGPU, meshes: []*viewerMesh{{info: info}},
	}
	v := &ModelViewer{scene: scene, uploading: true, liveWanted: map[int]string{0: info.GeometryURL}}
	update := func(geometry, texture string) {
		v.applyLiveMeshes(modelviewer.ModelViewerLiveUpdate{
			MemorySessionID: "session",
			Meshes:          []modelviewer.ModelViewerLiveMesh{{ID: "body", GeometryURL: geometry, IndexCount: 3}},
			Textures: []modelviewer.ModelViewerLiveTexture{{
				Key: "diffuse", Texture: modelviewer.ModelViewerTextureTransport{URL: texture},
			}},
		})
	}
	update("during-recovery", "first")
	v.uploading, v.uploadErr = false, "device unavailable"
	update("after-failure", "latest")
	if v.liveUploadReady(scene, oldGPU, generation) {
		t.Fatal("the failed GPU set can start or accept uploads")
	}
	if got := v.liveWanted[0]; got != "after-failure" || scene.transport.Meshes[0].GeometryURL != got {
		t.Fatalf("pending geometry %q and transport %+v do not name the latest edit", got, scene.transport.Meshes[0])
	}
	if got := v.liveTextureWanted["diffuse"]; got != "latest" || scene.transport.Textures["diffuse"].URL != got {
		t.Fatalf("pending texture %q and transport %+v do not name the latest edit", got, scene.transport.Textures)
	}
	if got := scene.meshes[0].info.GeometryURL; got != "before-recovery" {
		t.Fatalf("installed geometry changed before the upload completed: %q", got)
	}

	v.uploadErr = ""
	scene.gpu = &viewerGPUSet{generation: generation}
	if v.liveUploadReady(scene, oldGPU, generation) || !v.liveUploadReady(scene, scene.gpu, generation) {
		t.Fatal("only the recovered GPU set should accept the pending uploads")
	}

	// Recovery could not restore the previous geometry. Keep uploads paused while inspecting the replay inputs.
	scene.forgetMesh(0)
	v.uploading = true
	scene.gpu.textures = map[string]viewerTextureGPU{"diffuse": {}, "changed": {}}
	scene.transport.Textures["changed"] = modelviewer.ModelViewerTextureTransport{URL: "newer"}
	scene.transport.Textures["missing"] = modelviewer.ModelViewerTextureTransport{URL: "not-uploaded"}
	v.liveTextureWanted["changed"], v.liveTextureWanted["missing"] = "newer", "not-uploaded"
	v.resumeLiveUploads(scene, map[string]modelviewer.ModelViewerTextureTransport{
		"diffuse": {URL: "latest"},
		"changed": {URL: "older"},
		"missing": {URL: "not-uploaded"},
	})
	if got := scene.meshes[0].info.GeometryURL; got != "after-failure" || len(v.liveWanted) != 0 {
		t.Errorf("next regular load would read %q with live uploads pending: %v", got, v.liveWanted)
	}
	if _, pending := v.liveTextureWanted["diffuse"]; pending {
		t.Error("the recovered current texture would be uploaded again")
	}
	if v.liveTextureWanted["changed"] != "newer" || v.liveTextureWanted["missing"] != "not-uploaded" {
		t.Errorf("recovery dropped textures it did not upload at their latest revision: %v", v.liveTextureWanted)
	}

	scene.transport.MemorySessionID = ""
	v.releaseScene()
	if len(v.liveWanted) != 0 || len(v.liveTextureWanted) != 0 || v.liveUploadReady(scene, scene.gpu, generation) {
		t.Fatal("releasing the scene left live uploads pending")
	}
}

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

func TestReplaceBlendClearsAndUpdatesState(t *testing.T) {
	t.Run("updates palette, anchorNodes, boxes, and clears stale state", func(t *testing.T) {
		scene := &viewerScene{
			meshes: []*viewerMesh{
				{info: modelviewer.ModelViewerMeshTransport{
					ID:     "body",
					Bounds: &modelviewer.ModelViewerBounds{Min: vec3{-1, -1, -1}, Max: vec3{1, 1, 1}},
				}},
				{info: modelviewer.ModelViewerMeshTransport{
					ID:     "prop",
					Bounds: &modelviewer.ModelViewerBounds{Min: vec3{10, 10, 10}, Max: vec3{12, 12, 12}},
				}},
			},
		}
		m := &viewerMotion{
			scene:          scene,
			bones:          make([][12]float32, 2),
			parents:        []int32{-1, 0},
			boxes:          make([]viewerMotionBox, 3),
			clipBoxes:      make([]viewerMotionBox, 1),
			clipBoxesValid: []bool{true},
			anchorNodes:    []int32{-1, -1},
		}

		positions := []float32{1, 2, 3}
		blend := []uint16{0, 0, 0, 0, 65535, 0, 0, 0}
		m.palettes = map[string][]int32{"newPalette": {1}}
		m.replaceBlend(0, "newPalette", positions, blend)
		if len(m.meshes) != 1 || m.meshes[0].index != 0 {
			t.Fatalf("meshes = %+v, want mesh 0 posed", m.meshes)
		}
		if m.anchorNodes[0] != 1 {
			t.Errorf("anchorNodes[0] = %d, want 1", m.anchorNodes[0])
		}
		if !m.boxes[1].ok || m.boxes[1].minimum != (vec3{1, 2, 3}) || m.boxes[1].maximum != (vec3{1, 2, 3}) {
			t.Errorf("boxes[1] = %+v, want [1 2 3]..[1 2 3]", m.boxes[1])
		}
		if !m.restBox.ok || m.restBox.minimum != (vec3{10, 10, 10}) || m.restBox.maximum != (vec3{12, 12, 12}) {
			t.Errorf("restBox = %+v, want [10 10 10]..[12 12 12]", m.restBox)
		}
		if m.clipBoxesValid[0] {
			t.Error("clipBoxesValid[0] remained valid after replaceBlend")
		}

		m.replaceBlend(0, "", nil, nil)
		if len(m.meshes) != 0 {
			t.Errorf("meshes = %d after clearing blend, want 0", len(m.meshes))
		}
		if m.anchorNodes[0] != -1 {
			t.Errorf("anchorNodes[0] = %d after clearing blend, want -1", m.anchorNodes[0])
		}
		if m.boxes[1].ok {
			t.Errorf("boxes[1] = %+v after clearing blend, want empty", m.boxes[1])
		}
		if !m.restBox.ok || m.restBox.minimum != (vec3{-1, -1, -1}) || m.restBox.maximum != (vec3{12, 12, 12}) {
			t.Errorf("restBox = %+v after clearing blend, want [-1 -1 -1]..[12 12 12]", m.restBox)
		}
	})

	t.Run("recomputes active clipBox without disturbing m.pose", func(t *testing.T) {
		motion := &modelviewer.CharacterMotion{
			FPS:   60,
			Clips: []modelviewer.CharacterMotionClip{{Name: "idle", Frames: 2}},
		}
		scene := &viewerScene{
			meshes: []*viewerMesh{{info: modelviewer.ModelViewerMeshTransport{
				ID:     "body",
				Bounds: &modelviewer.ModelViewerBounds{Min: vec3{0, 0, 0}, Max: vec3{2, 2, 2}},
			}}},
		}
		m := &viewerMotion{
			scene:  scene,
			motion: motion,
			pose:   motion.NewPose(),
			clips:  []modelviewer.ModelViewerAnimationClip{{ID: "0", FPS: 60}},
			meshes: []viewerMotionMesh{
				{
					index: 0,
					boxes: []viewerMotionNodeBox{
						{node: -1, box: viewerMotionBox{minimum: vec3{5, 5, 5}, maximum: vec3{5, 5, 5}, ok: true}},
					},
				},
			},
			boxes:          []viewerMotionBox{{minimum: vec3{5, 5, 5}, maximum: vec3{5, 5, 5}, ok: true}},
			clipBoxes:      []viewerMotionBox{{minimum: vec3{5, 5, 5}, maximum: vec3{5, 5, 5}, ok: true}},
			clipBoxesValid: []bool{true},
			clipBox:        viewerMotionBox{minimum: vec3{5, 5, 5}, maximum: vec3{5, 5, 5}, ok: true},
			anchorNodes:    []int32{-1},
		}
		m.play.clipID = "0"

		m.replaceBlend(0, "", nil, nil)
		if m.clipBox.ok {
			t.Errorf("clipBox = %+v after clearing only posed mesh, want empty", m.clipBox)
		}
		if !m.restBox.ok || m.restBox.minimum != (vec3{0, 0, 0}) || m.restBox.maximum != (vec3{2, 2, 2}) {
			t.Errorf("restBox = %+v, want [0 0 0]..[2 2 2]", m.restBox)
		}
		if m.pose.Hold() {
			t.Error("clipBox recomputation mutated m.pose")
		}
	})
}

// A group MapPalettes could not place is not mapped again on every live update, only once more vertices are
// weighted to it than it has seen.
func TestPaletteNeedsMappingEvidence(t *testing.T) {
	m := &viewerMotion{palettes: map[string][]int32{"body": {0}}}
	needs := func(blend []uint16) bool {
		return paletteNeedsMapping(blend, m.palettes["body"], m.paletteEvidence["body"])
	}
	one := []uint16{0, 1, 0, 0, 32768, 32767, 0, 0}
	if !needs(one) {
		t.Fatal("paletteNeedsMapping() = false for a group past the palette, want true")
	}

	m.recordMappingEvidence("body", one)
	if needs(one) {
		t.Errorf(
			"paletteNeedsMapping() = true for the vertices MapPalettes saw, evidence %v",
			m.paletteEvidence["body"],
		)
	}
	two := append(slices.Clone(one), 1, 0, 0, 0, 65535, 0, 0, 0)
	if !needs(two) {
		t.Errorf(
			"paletteNeedsMapping() = false once group 1 weights more vertices, evidence %v",
			m.paletteEvidence["body"],
		)
	}
}
