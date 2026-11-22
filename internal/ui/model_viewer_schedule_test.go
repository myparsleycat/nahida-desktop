package ui

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestViewerScheduleUsesGeometryFromItsSnapshot(t *testing.T) {
	t.Parallel()
	scene := &viewerScene{transport: modelviewer.ModelViewerTransport{
		Meshes: []modelviewer.ModelViewerMeshTransport{{
			Conditions: modelviewer.ModelViewerDNF{{}},
			PositionVariants: []modelviewer.ModelViewerPositionVariant{{
				GeometryURL: "variant", Conditions: modelviewer.ModelViewerDNF{{}},
			}},
		}},
	}}
	input := scene.snapshotMeshTransport()
	scene.transport.Meshes[0].PositionVariants = nil

	frames := []modelviewer.ModelViewerAnimationFrame{{}, {}}
	oldSchedule := buildViewerSchedule(t.Context(), input, nil, "", frames, false)
	newSchedule := buildViewerSchedule(t.Context(), scene.snapshotMeshTransport(), nil, "", frames, false)
	variant := viewerScheduleItem{mesh: 0, variant: 0}
	if !slices.Contains(oldSchedule.frames[0], variant) || slices.Contains(newSchedule.frames[0], variant) {
		t.Fatalf("old frame %v, new frame %v; only the old frame should request the variant",
			oldSchedule.frames[0], newSchedule.frames[0])
	}
}

// A looping clip with more frames than the cache holds evicts, least recently drawn first, exactly the frame it
// plays next. With the clip's schedule the cache drops the mesh drawn last instead.
func TestSceneDropsMeshDrawnLastWithSchedule(t *testing.T) {
	t.Parallel()
	const meshes = 6
	vertices := 1000
	scene := &viewerScene{schedule: &viewerSchedule{uses: map[viewerScheduleItem][]int{}, loop: true}}
	for i := range meshes {
		scene.meshes = append(
			scene.meshes,
			newViewerStub(modelviewer.ModelViewerMeshTransport{VertexCount: vertices}, false),
		)
		item := viewerScheduleItem{mesh: i, variant: -1}
		scene.schedule.frames = append(scene.schedule.frames, []viewerScheduleItem{item})
		scene.schedule.uses[item] = []int{i}
	}
	// Three droppable meshes fit, a fourth does not.
	scene.meshBudget = 3*scene.meshes[0].gpuBytes() + 1
	loaded := &viewerMesh{vertexCount: vertices, current: []float32{0}, geometryKey: baseGeometryKey}
	for _, i := range []int{0, 1, 3, 2} {
		scene.meshes[i].fill(loaded)
	}
	scene.trackLoaded()
	scene.frame = 2
	states := make([]modelviewer.ViewerMeshState, meshes)
	for i := range states {
		states[i] = modelviewer.ViewerMeshState{Visible: i == 2, PositionVariant: -1}
	}
	scene.apply(states)

	// From frame 2, mesh 3 is drawn next, mesh 0 in four frames and mesh 1 in five.
	scene.meshes[4].fill(loaded)
	if unloaded := scene.storeMesh(4); !slices.Equal(unloaded, []int{1}) {
		t.Fatalf("unloaded %v, want mesh 1", unloaded)
	}

	scene.schedule = nil
	scene.meshes[5].fill(loaded)
	if unloaded := scene.storeMesh(5); !slices.Equal(unloaded, []int{0}) {
		t.Fatalf("without a schedule unloaded %v, want the least recently drawn mesh 0", unloaded)
	}
}

// A load queued for a frame playback skipped past reads as unwanted, while one wanted before and after stays wanted.
func TestViewerDemandDropsItemsLeftBehind(t *testing.T) {
	t.Parallel()
	mesh := newViewerStub(modelviewer.ModelViewerMeshTransport{
		PositionVariants: make([]modelviewer.ModelViewerPositionVariant, 2),
	}, false)
	demand := newViewerDemand([]*viewerMesh{mesh, mesh})
	a, b, c := viewerScheduleItem{0, -1}, viewerScheduleItem{0, 1}, viewerScheduleItem{1, -1}
	demand.set([]viewerScheduleItem{a, b})
	demand.set([]viewerScheduleItem{b, c})
	if demand.wanted(a) || !demand.wanted(b) || !demand.wanted(c) {
		t.Fatalf("wanted a=%v b=%v c=%v", demand.wanted(a), demand.wanted(b), demand.wanted(c))
	}
}

// A folded frame carries its own tangents, which replace the base's in extra along with the outline direction a
// tangent outline takes from them; the base's vertex colours stay.
func TestDecodeViewerPositionTakesFrameTangents(t *testing.T) {
	t.Parallel()
	floats := func(values ...float32) []byte {
		data := make([]byte, len(values)*4)
		for i, value := range values {
			binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(value))
		}
		return data
	}
	baseExtra := viewerVertexExtras([]float32{1, 0, 0, 1}, []float32{0, 1, 0}, []byte{9, 8, 7, 6}, 1)
	header := make([]byte, 80)
	withTangents := slices.Concat(header, floats(1, 2, 3), floats(0, 0, 1), floats(0, 0.6, 0.8, -1))

	variant, err := decodeViewerPosition(withTangents, viewerVariantBase{
		vertexCount: 1, extra: baseExtra, tangentOutline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(variant.current, []float32{1, 2, 3, 0, 0, 1}) {
		t.Fatalf("current = %v", variant.current)
	}
	want := viewerVertexExtras([]float32{0, 0.6, 0.8, -1}, []float32{0, 0.6, 0.8}, []byte{9, 8, 7, 6}, 1)
	if !slices.Equal(variant.extra, want) {
		t.Fatalf("extra = %v, want %v", variant.extra, want)
	}

	plain := slices.Concat(header, floats(1, 2, 3), floats(0, 0, 1))
	variant, err = decodeViewerPosition(plain, viewerVariantBase{vertexCount: 1, extra: baseExtra})
	if err != nil {
		t.Fatal(err)
	}
	want = viewerVertexExtras([]float32{1, 0, 0, 1}, []float32{0, 0, 1}, []byte{9, 8, 7, 6}, 1)
	if !slices.Equal(variant.extra, want) {
		t.Fatalf("extra without frame tangents = %v, want %v", variant.extra, want)
	}
}
