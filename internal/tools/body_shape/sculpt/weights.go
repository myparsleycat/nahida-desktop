package sculpt

import "math"

// BrushMode decides whether a brush adds to or removes from the selection mask.
type BrushMode int

const (
	BrushPaint BrushMode = iota
	BrushErase
)

// DefaultNormalThreshold is the minimum dot product between a vertex normal and
// the hit normal for a front-facing brush to reach the vertex.
const DefaultNormalThreshold = 0.15

// BrushFalloff is the smoothstep falloff of a surface brush. Hardness above 1
// sharpens the core and below 1 softens it; 0 is treated as 1.
func BrushFalloff(distance, radius, hardness float64) float64 {
	if radius <= 0 || distance >= radius {
		return 0
	}
	if distance <= 0 {
		return 1
	}

	normalized := Clamp01(distance / radius)
	smooth := 1 - smoothstep01(normalized)
	if hardness == 0 || hardness == 1 {
		return smooth
	}
	return math.Pow(smooth, max(0.01, hardness))
}

func PaintVertex(current, brushWeight, strength float64) float64 {
	return Clamp01(current + brushWeight*strength)
}

func EraseVertex(current, brushWeight, strength float64) float64 {
	return Clamp01(current - brushWeight*strength)
}

func (mode BrushMode) apply(current, brushWeight, strength float64) float64 {
	if mode == BrushErase {
		return EraseVertex(current, brushWeight, strength)
	}
	return PaintVertex(current, brushWeight, strength)
}

// merge combines a weight with its mirror or weld twin: erasing keeps the lower one.
func (mode BrushMode) merge(a, b float32) float32 {
	if mode == BrushErase {
		return min(a, b)
	}
	return max(a, b)
}

// facesAway reports whether a vertex normal points away from the hit normal.
// A vertex without a normal is never filtered, as a NaN comparison was not.
func facesAway(normals []float32, vertex int, hitNormal Vec3, threshold float64) bool {
	if vertex*3+2 >= len(normals) {
		return false
	}
	return vertexOffset(normals, vertex, Vec3{}).dot(hitNormal) < threshold
}

// BrushStrokeOptions describes a straight-line distance brush.
type BrushStrokeOptions struct {
	Positions []float32
	Weights   []float32
	HitPoint  Vec3
	Radius    float64
	Strength  float64
	// Hardness 0 means 1.
	Hardness float64
	Mode     BrushMode
	// Normals and HitNormal together skip vertices that face away from the hit.
	Normals   []float32
	HitNormal *Vec3
	// NormalThreshold nil means DefaultNormalThreshold.
	NormalThreshold *float64
}

// ApplyBrushStroke paints vertices within Radius of HitPoint, regardless of
// surface connectivity, and returns the vertices whose weight changed.
func ApplyBrushStroke(options BrushStrokeOptions) []uint32 {
	vertexCount := min(len(options.Weights), len(options.Positions)/3)
	threshold := DefaultNormalThreshold
	if options.NormalThreshold != nil {
		threshold = *options.NormalThreshold
	}
	filter := options.Normals != nil && options.HitNormal != nil
	r2 := options.Radius * options.Radius

	var changed []uint32
	for i := range vertexCount {
		offset := vertexOffset(options.Positions, i, options.HitPoint)
		dist2 := offset.dot(offset)
		if dist2 > r2 {
			continue
		}

		if filter && facesAway(options.Normals, i, *options.HitNormal, threshold) {
			continue
		}

		falloff := BrushFalloff(math.Sqrt(dist2), options.Radius, options.Hardness)
		if falloff <= 0 {
			continue
		}
		previous := float64(options.Weights[i])
		if next := options.Mode.apply(previous, falloff, options.Strength); next != previous {
			options.Weights[i] = float32(next)
			changed = append(changed, uint32(i))
		}
	}
	return changed
}

// MirrorWeightsAcrossX copies source weights to their mirror across x = 0. With
// a mirror map at least as long as the mesh it is a lookup; otherwise it searches
// for the nearest vertex within maxDistance of the mirrored position.
func MirrorWeightsAcrossX(
	positions, weights []float32,
	sources []uint32,
	maxDistance float64,
	mode BrushMode,
	mirrorMap []int32,
) {
	vertexCount := min(len(weights), len(positions)/3)
	if mirrorMap != nil && len(mirrorMap) >= vertexCount {
		for _, source := range sources {
			if int(source) >= vertexCount {
				continue
			}
			mirror := mirrorMap[source]
			if mirror < 0 || int(mirror) >= vertexCount {
				continue
			}
			weights[mirror] = mode.merge(weights[mirror], weights[source])
		}
		return
	}

	maxDistance2 := maxDistance * maxDistance
	for _, source := range sources {
		if int(source) >= vertexCount {
			continue
		}
		target := vertexOffset(positions, int(source), Vec3{})
		target[0] = -target[0]
		best := -1
		bestDist2 := maxDistance2
		for i := range vertexCount {
			if i == int(source) {
				continue
			}
			offset := vertexOffset(positions, i, target)
			if dist2 := offset.dot(offset); dist2 < bestDist2 {
				bestDist2 = dist2
				best = i
			}
		}
		if best >= 0 {
			weights[best] = mode.merge(weights[best], weights[source])
		}
	}
}

// WeightToRGB maps a weight to the heatmap ramp black, blue, yellow, red, in [0, 1].
func WeightToRGB(weight float64) Vec3 {
	w := Clamp01(weight)
	if w <= 0 {
		return Vec3{0.12, 0.12, 0.14}
	}
	if w < 0.5 {
		t := w / 0.5
		return Vec3{0.1 * t, 0.25 + 0.35*t, 0.85 - 0.2*t}
	}
	if w < 0.85 {
		t := (w - 0.5) / 0.35
		return Vec3{0.1 + 0.85*t, 0.6 + 0.3*t, 0.65 * (1 - t)}
	}
	t := (w - 0.85) / 0.15
	return Vec3{0.95, 0.9 * (1 - t), 0.05 * (1 - t)}
}

// WriteWeightColors writes packed RGB heatmap colors for each weight.
func WriteWeightColors(weights, colors []float32) {
	count := min(len(weights), len(colors)/3)
	for i := range count {
		rgb := WeightToRGB(float64(weights[i]))
		colors[i*3] = float32(rgb[0])
		colors[i*3+1] = float32(rgb[1])
		colors[i*3+2] = float32(rgb[2])
	}
}
