package tools

import (
	"context"
	"os"
	"slices"
	"testing"
)

// Opt-in integration test; local mod assets are never copied into the repository.
func TestCyclicPackedLocalMod(t *testing.T) {
	dir := os.Getenv("MODEL_VIEWER_CYCLIC_MOD")
	if dir == "" {
		t.Skip("set MODEL_VIEWER_CYCLIC_MOD to a cyclic packed mod folder")
	}
	service := New()
	payload, err := service.LoadModViewer(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := service.CleanupModelViewer(context.Background(), payload.MemorySessionID); err != nil {
			t.Error(err)
		}
	})
	if len(payload.ComputeDeformers) != 1 || len(payload.Animations) != 1 {
		t.Fatalf("deformers=%d animations=%d", len(payload.ComputeDeformers), len(payload.Animations))
	}
	d, clip := payload.ComputeDeformers[0], payload.Animations[0]
	if d.Kind != modelViewerPackedObjectKind || d.Pose == nil || clip.DeformerID != d.ID || len(clip.Frames) < 2 {
		t.Fatalf("invalid cyclic animation: deformer=%+v clip=%s", d, clip.ID)
	}
	for _, source := range []ModelViewerComputeBinarySource{d.Base, d.Pose.Blend, d.Pose.Frames} {
		data := readModelViewerProtocolBytes(t, service.protocol, source.URL)
		if int64(len(data)) != source.ByteLength {
			t.Fatalf("source=%s bytes=%d want=%d", source.URL, len(data), source.ByteLength)
		}
	}
	if d.Base.Encoding != modelViewerPackedFloatEncoding || d.Base.Stride != 28 {
		t.Fatal("packed compute source was not prepared")
	}
	for _, frameIndex := range []int{0, len(clip.Frames) / 2, len(clip.Frames) - 1} {
		visible := 0
		for _, mesh := range evaluateViewerTransport(payload, clip.Frames[frameIndex].Values).Meshes {
			if mesh.Visible && slices.Contains(d.MeshIDs, mesh.ID) {
				visible++
			}
		}
		if visible == 0 {
			t.Fatalf("frame %d hides every animated mesh", clip.Frames[frameIndex].Index)
		}
	}
	t.Logf(
		"vertices=%d bones=%d poseFrames=%d clip=%d..%d fps=%g",
		d.VertexCount,
		d.Pose.BoneCount,
		d.Pose.FrameCount,
		clip.FrameStart,
		clip.FrameEnd,
		clip.FPS,
	)
}
