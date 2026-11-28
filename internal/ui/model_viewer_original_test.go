package ui

import (
	"image"
	"testing"

	"github.com/myparsleycat/fairy/d3d"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestOriginalBangsFollowPresentedReplacement(t *testing.T) {
	part := &viewerOriginalPart{shown: true, bang: true, mesh: &viewerMesh{}, gpu: viewerMeshGPU{indexCount: 3}}
	replacement := &viewerMesh{}
	scene := &viewerScene{meshes: []*viewerMesh{replacement}}
	original := &viewerOriginal{
		scene: scene, data: &modelviewer.CharacterOriginal{Character: "odette"},
		parts: []*viewerOriginalPart{part}, bangs: []*viewerOriginalPart{part}, bangMeshes: []int{0}, bangBlocked: true,
	}
	scene.original = original
	viewer := &ModelViewer{scene: scene}
	for _, visible := range []bool{false, true, false} {
		viewer.toggleStates = []modelviewer.ViewerMeshState{{Visible: visible, PositionVariant: -1}}
		viewer.present()
		shown := len(original.draws(d3d.Device{}, nil, 0)) > 0
		if shown == visible {
			t.Errorf("replacement visible = %t, original drawn = %t", visible, shown)
		}
	}
	viewer.toggleOriginalPart(original, part)
	viewer.toggleStates[0].Visible = true
	viewer.present()
	viewer.toggleStates[0].Visible = false
	viewer.present()
	if len(original.draws(d3d.Device{}, nil, 0)) != 0 {
		t.Error("returning to the original Bang showed it after the user hid it")
	}
}

func TestGameTextureCountSkipsOverlayShadows(t *testing.T) {
	untextured := modelviewer.ViewerMeshState{Visible: true}
	textured := modelviewer.ViewerMeshState{Visible: true, TexKey: "body"}
	v := &ModelViewer{
		scene: &viewerScene{meshes: []*viewerMesh{{}, {overlayShadow: true}, {}}},
		toggleStates: []modelviewer.ViewerMeshState{
			untextured, untextured, textured,
		},
	}
	if got := v.gameTextureCount(); got != 1 {
		t.Errorf("gameTextureCount = %d, want 1 without the overlay shadow", got)
	}
}

func TestOriginalLUTWhite(t *testing.T) {
	// A 2³ LUT as LoadCharacterOriginalTexture returns it: rows flipped, so the file's top row (green at its
	// maximum) comes last. Each texel holds its own red, green and blue input in the low bits.
	lut := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			i := y*lut.Stride + x*4
			lut.Pix[i], lut.Pix[i+1], lut.Pix[i+2], lut.Pix[i+3] = uint8(x%2), uint8(y), uint8(x/2), 255
		}
	}
	white, err := originalLUTWhite(lut)
	if err != nil {
		t.Fatal(err)
	}
	if got := white.Pix; white.Rect.Dx() != 1 || white.Rect.Dy() != 1 || got[0] != 1 || got[1] != 1 || got[2] != 1 {
		t.Errorf("white = %v %v, want the texel of input (1, 1, 1)", white.Rect, got)
	}

	if _, err := originalLUTWhite(image.NewNRGBA(image.Rect(0, 0, 4, 4))); err == nil {
		t.Error("a 4x4 image passed as a LUT")
	}
}

func TestOriginalMotionNodeMapDuplicates(t *testing.T) {
	t.Run("different layout uses first duplicate node", func(t *testing.T) {
		motionNames := []string{"Root", "Head", "Neck", "Head"}
		originalNames := []string{"Root", "Head", "Other"}
		byName, sameLayout := originalMotionNodeMap(originalNames, len(motionNames), func(i int) string {
			return motionNames[i]
		})
		if sameLayout {
			t.Fatal("sameLayout = true for differing node lists")
		}
		target, ok := resolveOriginalMotionNode(1, originalNames, byName, sameLayout)
		if !ok || target != 1 {
			t.Errorf("resolved (%d, %v), want (1, true)", target, ok)
		}
	})

	t.Run("identical layout preserves exact node indices", func(t *testing.T) {
		names := []string{"Root", "End", "End"}
		byName, sameLayout := originalMotionNodeMap(names, len(names), func(i int) string {
			return names[i]
		})
		if !sameLayout {
			t.Fatal("sameLayout = false for identical node lists")
		}
		for _, want := range []int32{1, 2} {
			got, ok := resolveOriginalMotionNode(want, names, byName, sameLayout)
			if !ok || got != int(want) {
				t.Errorf("node %d resolved (%d, %v), want (%d, true)", want, got, ok, want)
			}
		}
	})
}
