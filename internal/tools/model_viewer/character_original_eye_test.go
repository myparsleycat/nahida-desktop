package modelviewer

import (
	"image"
	"image/color"
	"slices"
	"testing"
)

// A translucent eye piece moves to its own part with the vertices, weights and shape key offsets of its triangles,
// so a blink still closes it.
func TestSplitEyeLayersKeepsVertexDataAligned(t *testing.T) {
	part := CharacterOriginalPart{
		Role:      OriginalRoleEye,
		Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 5, 5, 5, 6, 5, 5, 5, 6, 5},
		UVs:       []float32{0, 0, 0.1, 0, 0, 0.1, 0.5, 0.5, 0.6, 0.5, 0.5, 0.6},
		Nodes:     slices.Repeat([]int32{0, -1, -1, -1}, 6),
		Weights:   slices.Repeat([]float32{1, 0, 0, 0}, 6),
		Indices:   []uint32{0, 1, 2, 3, 4, 5},
		Targets: []CharacterOriginalTarget{{
			Name:      "Eye_Close",
			Positions: []float32{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, -3, 0, 0, -4, 0, 0, -5},
		}},
		eyeColumns: readZenlessEyeColumns([]float32{
			1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1,
			20.0 / 65535, 0, 0, 1, 20.0 / 65535, 0, 0, 1, 20.0 / 65535, 0, 0, 1,
		}),
	}
	lut := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range lut.Pix {
		lut.Pix[i] = 255
	}
	lut.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 153})

	parts := splitEyeLayers(part, lut)
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want the opaque iris and one layer", len(parts))
	}
	opaque, layer := parts[0], parts[1]
	if opaque.Layer != nil || !slices.Equal(opaque.Positions, part.Positions[:9]) {
		t.Errorf("opaque part: layer %+v positions %v", opaque.Layer, opaque.Positions)
	}
	if layer.Layer == nil || layer.Layer.Opacity != 0.6 || layer.Layer.Tint != [3]float32{} {
		t.Fatalf("layer = %+v, want black at 0.6", layer.Layer)
	}
	if !slices.Equal(layer.Positions, part.Positions[9:]) || !slices.Equal(layer.UVs, part.UVs[6:]) ||
		!slices.Equal(layer.Indices, []uint32{0, 1, 2}) || len(layer.Weights) != 12 || len(layer.Nodes) != 12 {
		t.Errorf("layer geometry: positions %v uvs %v indices %v", layer.Positions, layer.UVs, layer.Indices)
	}
	if got := layer.Targets[0].Positions; !slices.Equal(got, []float32{0, 0, -3, 0, 0, -4, 0, 0, -5}) {
		t.Errorf("layer shape key offsets = %v", got)
	}
}
