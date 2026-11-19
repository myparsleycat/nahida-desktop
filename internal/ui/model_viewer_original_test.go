package ui

import (
	"image"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

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
