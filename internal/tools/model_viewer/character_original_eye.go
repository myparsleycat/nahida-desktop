package modelviewer

import (
	"context"
	"image"
	"math"

	"nahida.live/desktop/internal/infra"
)

// Zenless eye materials draw the iris, the eyelid shadows over it and its reflections in one primitive. HoyoToon's
// Zenless shader multiplies each piece by a texel of _EyeColorMap that its vertex colour selects, doubled, and the
// texel's alpha sets how opaque the piece is. The hosted models store that selection in
// the red channel of COLOR_1 as zenlessEyeColumnStep times a column of the map's top row; this encoding was read
// from the stock eyes, where the two eyelid shadow pieces pick the dark texels and the reflection the teal one.
const (
	zenlessEyeColorSlot  = "_EyeColorMap"
	zenlessEyeColumnStep = 20
	zenlessEyeColumns    = 16
	noEyeColumn          = math.MaxUint8
)

// OriginalLayer is how a translucent piece is blended: its colour map times Tint (linear), at Opacity.
type OriginalLayer struct {
	Tint    [3]float32
	Opacity float32
}

// readZenlessEyeColumns reads the eye colour map column of each vertex from four COLOR_1 values per vertex.
func readZenlessEyeColumns(colors []float32) []uint8 {
	columns := make([]uint8, len(colors)/4)
	for v := range columns {
		raw := int(math.Round(float64(colors[v*4]) * math.MaxUint16))
		columns[v] = noEyeColumn
		if raw%zenlessEyeColumnStep == 0 && raw/zenlessEyeColumnStep < zenlessEyeColumns {
			columns[v] = uint8(raw / zenlessEyeColumnStep)
		}
	}
	return columns
}

// layerOriginalEyes splits the translucent pieces off the parts that carry a Zenless eye colour map. A map that
// cannot be loaded leaves its parts whole; only a cancellation fails.
func (t *Service) layerOriginalEyes(
	ctx context.Context,
	original *CharacterOriginal,
	progress CharacterFileProgress,
) error {
	maps := map[string]*image.NRGBA{}
	parts := make([]CharacterOriginalPart, 0, len(original.Parts))
	for index, part := range original.Parts {
		texture, ok := part.Textures[zenlessEyeColorSlot]
		if !ok || part.eyeColumns == nil {
			parts = append(parts, part)
			continue
		}
		key := texture.Color.File
		lut, ok := maps[key]
		if !ok {
			var err error
			lut, err = t.loadEyeColorMap(ctx, texture, progress)
			if err != nil {
				if infra.IsCancellationError(err) || ctx.Err() != nil {
					return err
				}
				_ = infra.ReportError(t.log, err, "ModelViewer.characterOriginal", infra.Diagnostic{
					Operation: "load-eye-color-map",
					Fields: map[string]any{
						"game": original.Game, "character": original.Character, "material": part.Material,
						"texture": texture.Name,
					},
				})
			}
			maps[key] = lut
		}
		if lut == nil {
			parts = append(parts, part)
			continue
		}
		split := splitEyeLayers(part, lut)
		if len(split) > 1 {
			for i := range split {
				split[i].splitFrom = index + 1
			}
		}
		parts = append(parts, split...)
	}
	original.Parts = parts
	return nil
}

// loadEyeColorMap reads an eye colour map with its rows top-down, as the image stores them.
func (t *Service) loadEyeColorMap(
	ctx context.Context,
	texture CharacterOriginalTexture,
	progress CharacterFileProgress,
) (*image.NRGBA, error) {
	colorData, err := t.characterOriginalTextureFile(ctx, texture.Color, progress)
	if err != nil {
		return nil, err
	}
	var alphaData []byte
	if texture.Alpha != nil {
		if alphaData, err = t.characterOriginalTextureFile(ctx, *texture.Alpha, progress); err != nil {
			return nil, err
		}
	}
	return mergeOriginalTexture(colorData, alphaData, texture.AlphaChannel)
}

// splitEyeLayers returns part without the triangles whose eye colour texel is translucent, followed by one part per
// such texel holding its triangles. A triangle takes the column of its first vertex.
func splitEyeLayers(part CharacterOriginalPart, lut *image.NRGBA) []CharacterOriginalPart {
	layers := map[uint8]*OriginalLayer{}
	layerOf := func(column uint8) *OriginalLayer {
		if column == noEyeColumn || int(column) >= lut.Rect.Dx() {
			return nil
		}
		if layer, ok := layers[column]; ok {
			return layer
		}
		texel := lut.NRGBAAt(lut.Rect.Min.X+int(column), lut.Rect.Min.Y)
		var layer *OriginalLayer
		if texel.A < math.MaxUint8 {
			layer = &OriginalLayer{Opacity: float32(texel.A) / math.MaxUint8}
			for i, channel := range []uint8{texel.R, texel.G, texel.B} {
				layer.Tint[i] = float32(2 * srgbChannelToLinear(float64(channel)/math.MaxUint8))
			}
		}
		layers[column] = layer
		return layer
	}

	var opaque []int
	var columns []uint8
	byColumn := map[uint8][]int{}
	for t := range len(part.Indices) / 3 {
		column := part.eyeColumns[part.Indices[t*3]]
		if layerOf(column) == nil {
			opaque = append(opaque, t)
			continue
		}
		if _, ok := byColumn[column]; !ok {
			columns = append(columns, column)
		}
		byColumn[column] = append(byColumn[column], t)
	}
	if len(columns) == 0 {
		return []CharacterOriginalPart{part}
	}

	var out []CharacterOriginalPart
	if len(opaque) > 0 {
		out = append(out, subsetOriginalPart(part, opaque))
	}
	for _, column := range columns {
		piece := subsetOriginalPart(part, byColumn[column])
		piece.Layer = layers[column]
		out = append(out, piece)
	}
	return out
}

// subsetOriginalPart is part with only the given triangles and the vertices they use.
func subsetOriginalPart(part CharacterOriginalPart, triangles []int) CharacterOriginalPart {
	indices := make([]float32, 0, len(triangles)*3)
	for _, t := range triangles {
		for k := range 3 {
			indices = append(indices, float32(part.Indices[t*3+k]))
		}
	}
	// The indices come from part, so none is past its vertices.
	used, rebased, _ := compactOriginalIndices(indices, len(part.Positions)/3)

	sub := part
	sub.Indices = rebased
	sub.Positions = compactOriginalVertices(part.Positions, 3, used)
	sub.Normals = compactOriginalVertices(part.Normals, 3, used)
	sub.Tangents = compactOriginalVertices(part.Tangents, 4, used)
	sub.UVs = compactOriginalVertices(part.UVs, 2, used)
	sub.UVs1 = compactOriginalVertices(part.UVs1, 2, used)
	sub.Colors = compactOriginalVertices(part.Colors, 4, used)
	sub.Nodes = compactOriginalVertices(part.Nodes, 4, used)
	sub.Weights = compactOriginalVertices(part.Weights, 4, used)
	sub.eyeColumns = compactOriginalVertices(part.eyeColumns, 1, used)
	if part.Targets != nil {
		sub.Targets = make([]CharacterOriginalTarget, len(part.Targets))
		for k, target := range part.Targets {
			target.Positions = compactOriginalVertices(target.Positions, 3, used)
			sub.Targets[k] = target
		}
	}
	return sub
}

func srgbChannelToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}
