package sculpt

import (
	"cmp"
	"math"
	"slices"
)

// DefaultDisplacementEpsilon is the smallest displacement counted as moved.
const DefaultDisplacementEpsilon = 1e-6

// BodyRegionID names a spatial region preset.
type BodyRegionID string

const (
	RegionShoulders BodyRegionID = "shoulders"
	RegionChest     BodyRegionID = "chest"
	RegionWaist     BodyRegionID = "waist"
	RegionHips      BodyRegionID = "hips"
	RegionButtocks  BodyRegionID = "buttocks"
	RegionThighs    BodyRegionID = "thighs"
	RegionCalves    BodyRegionID = "calves"
	RegionArms      BodyRegionID = "arms"
	RegionHead      BodyRegionID = "head"
)

// BodyRegionIDs lists the region presets in display order.
var BodyRegionIDs = []BodyRegionID{
	RegionShoulders, RegionChest, RegionWaist, RegionHips, RegionButtocks,
	RegionThighs, RegionCalves, RegionArms, RegionHead,
}

// MeshBounds is an axis-aligned bounding box with the body axes guessed from
// its extents: the longest is height and the wider of the others is lateral.
type MeshBounds struct {
	Min, Max, Center, Size             Vec3
	HeightAxis, LateralAxis, DepthAxis int
}

// DepthBand prefers the front (Bias 1) or back (Bias -1) of the depth axis.
type DepthBand struct {
	Bias, Inner, Outer float64
}

// RegionPreset is a region in normalized body space, relative to the mesh
// bounds, so it works across mods without vertex-index maps.
type RegionPreset struct {
	ID BodyRegionID
	// HeightStart and HeightEnd bound the band along the height axis, 0 at the
	// bottom and 1 at the top; HeightFade is the soft edge outside the band.
	HeightStart, HeightEnd, HeightFade float64
	// LateralInner and LateralOuter fade the weight from the center line out.
	LateralInner, LateralOuter float64
	Depth                      *DepthBand
	// DefaultAxisScale is the scale applied when the region is first selected.
	DefaultAxisScale Vec3
}

// RegionPresets holds the built-in region presets.
var RegionPresets = map[BodyRegionID]RegionPreset{
	RegionHead: {
		ID: RegionHead, HeightStart: 0.86, HeightEnd: 1.0, HeightFade: 0.04,
		// An outer edge above 1 keeps vertices on the bounding box edge.
		LateralInner: 0.0, LateralOuter: 1.15,
		DefaultAxisScale: Vec3{1, 0.2, 1},
	},
	RegionShoulders: {
		ID: RegionShoulders, HeightStart: 0.72, HeightEnd: 0.88, HeightFade: 0.05,
		LateralInner: 0.15, LateralOuter: 1.15,
		DefaultAxisScale: Vec3{1, 0.15, 0.35},
	},
	RegionChest: {
		ID: RegionChest, HeightStart: 0.55, HeightEnd: 0.76, HeightFade: 0.05,
		LateralInner: 0.0, LateralOuter: 1.05,
		Depth:            &DepthBand{Bias: 1, Inner: 0.0, Outer: 1.15},
		DefaultAxisScale: Vec3{0.7, 0.15, 1},
	},
	RegionWaist: {
		ID: RegionWaist, HeightStart: 0.42, HeightEnd: 0.58, HeightFade: 0.06,
		LateralInner: 0.0, LateralOuter: 1.05,
		DefaultAxisScale: Vec3{1, 0.1, 0.65},
	},
	RegionHips: {
		ID: RegionHips, HeightStart: 0.3, HeightEnd: 0.48, HeightFade: 0.05,
		LateralInner: 0.0, LateralOuter: 1.15,
		DefaultAxisScale: Vec3{1, 0.1, 0.35},
	},
	RegionButtocks: {
		ID: RegionButtocks, HeightStart: 0.28, HeightEnd: 0.48, HeightFade: 0.05,
		LateralInner: 0.05, LateralOuter: 1.1,
		Depth:            &DepthBand{Bias: -1, Inner: 0.1, Outer: 1.15},
		DefaultAxisScale: Vec3{0.4, 0.1, 1},
	},
	RegionThighs: {
		ID: RegionThighs, HeightStart: 0.12, HeightEnd: 0.36, HeightFade: 0.05,
		LateralInner: 0.08, LateralOuter: 1.15,
		DefaultAxisScale: Vec3{1, 0.1, 0.85},
	},
	RegionCalves: {
		ID: RegionCalves, HeightStart: 0.0, HeightEnd: 0.18, HeightFade: 0.04,
		LateralInner: 0.05, LateralOuter: 1.15,
		DefaultAxisScale: Vec3{1, 0.1, 0.85},
	},
	RegionArms: {
		ID: RegionArms, HeightStart: 0.48, HeightEnd: 0.82, HeightFade: 0.05,
		LateralInner: 0.55, LateralOuter: 1.2,
		DefaultAxisScale: Vec3{1, 0.15, 0.85},
	},
}

func ComputeMeshBounds(positions []float32) MeshBounds {
	low, high, ok := positionBounds(positions)
	if !ok {
		return MeshBounds{Size: Vec3{1, 1, 1}, HeightAxis: 1, LateralAxis: 0, DepthAxis: 2}
	}

	size := Vec3{max(high[0]-low[0], 1e-6), max(high[1]-low[1], 1e-6), max(high[2]-low[2], 1e-6)}
	order := []int{0, 1, 2}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(size[b], size[a]) })
	return MeshBounds{
		Min:         low,
		Max:         high,
		Center:      Vec3{(low[0] + high[0]) / 2, (low[1] + high[1]) / 2, (low[2] + high[2]) / 2},
		Size:        size,
		HeightAxis:  order[0],
		LateralAxis: order[1],
		DepthAxis:   order[2],
	}
}

// smoothBand keeps full strength inside [start, end] and fades only outside it.
func smoothBand(value, start, end, fade float64) float64 {
	if end <= start {
		return 0
	}
	rise := smoothstep(start-fade, start, value)
	fall := 1 - smoothstep(end, end+fade, value)
	return Clamp01(rise * fall)
}

func smoothstep(edge0, edge1, value float64) float64 {
	if edge1 <= edge0 {
		if value < edge0 {
			return 0
		}
		return 1
	}
	return smoothstep01(Clamp01((value - edge0) / (edge1 - edge0)))
}

// GenerateRegionWeights returns 0–1 weights for a region preset from the
// positions relative to the bounds. Pass nil bounds to compute them.
func GenerateRegionWeights(positions []float32, region BodyRegionID, bounds *MeshBounds) []float32 {
	if bounds == nil {
		computed := ComputeMeshBounds(positions)
		bounds = &computed
	}
	preset := RegionPresets[region]
	count := len(positions) / 3
	weights := make([]float32, count)
	h, l, d := bounds.HeightAxis, bounds.LateralAxis, bounds.DepthAxis
	lateralHalf := bounds.Size[l] * 0.5
	depthHalf := bounds.Size[d] * 0.5

	for i := range count {
		o := i * 3
		height := (float64(positions[o+h]) - bounds.Min[h]) / bounds.Size[h]
		lateral := math.Abs(float64(positions[o+l])-bounds.Center[l]) / lateralHalf
		depth := (float64(positions[o+d]) - bounds.Center[d]) / depthHalf

		w := smoothBand(height, preset.HeightStart, preset.HeightEnd, preset.HeightFade)
		if w <= 0 {
			continue
		}
		w *= Clamp01(1 - smoothstep(preset.LateralInner, preset.LateralOuter, lateral))
		if preset.Depth != nil {
			preferred := depth * preset.Depth.Bias
			w *= Clamp01(smoothstep(preset.Depth.Inner, preset.Depth.Outer, (preferred+1)*0.5))
		}
		weights[i] = float32(Clamp01(w))
	}
	return weights
}

// RegionOperation is how a region moves its weighted vertices.
type RegionOperation int

const (
	// OpScale scales around the pivot by 1 + Amount * AxisScale * weight.
	OpScale RegionOperation = iota
	// OpInflate moves along Normals by Amount * weight.
	OpInflate
	// OpTranslate moves by Translation * Amount * weight.
	OpTranslate
	// OpTaper scales x and z by Amount * TaperFactor * weight * height above the pivot.
	OpTaper
	// OpDisplace adds Displacement * Amount; the field is already shaped, so
	// Weights only drives the heatmap.
	OpDisplace
)

// ActiveRegionDeform is one deformation applied to the original positions.
type ActiveRegionDeform struct {
	// ID is a region preset, a bone key or any stable control key.
	ID        string
	Weights   []float32
	Amount    float64
	AxisScale Vec3
	Pivot     Vec3
	// VertexPivots holds a packed xyz pivot per vertex when the mask has
	// separate parts; it overrides Pivot.
	VertexPivots []float32
	Operation    RegionOperation
	Normals      []float32
	Translation  Vec3
	TaperFactor  float64
	Displacement []float32
}

func (r *ActiveRegionDeform) weightAt(vertex int) float64 {
	if vertex < len(r.Weights) {
		return float64(r.Weights[vertex])
	}
	return 0
}

func (r *ActiveRegionDeform) pivotAt(o int) Vec3 {
	if r.VertexPivots != nil && o+2 < len(r.VertexPivots) {
		return vertexOffset(r.VertexPivots, o/3, Vec3{})
	}
	return r.Pivot
}

// offset returns the region's displacement of one original vertex.
func (r *ActiveRegionDeform) offset(vertex int, position Vec3) Vec3 {
	o := vertex * 3
	if r.Operation == OpDisplace {
		if o+2 >= len(r.Displacement) {
			return Vec3{}
		}
		return vertexOffset(r.Displacement, vertex, Vec3{}).scaled(r.Amount)
	}
	w := r.weightAt(vertex)
	if w <= 0 {
		return Vec3{}
	}

	switch r.Operation {
	case OpInflate:
		if o+2 >= len(r.Normals) {
			return Vec3{}
		}
		normal := vertexOffset(r.Normals, vertex, Vec3{})
		return Vec3{normal[0] * r.Amount * w, normal[1] * r.Amount * w, normal[2] * r.Amount * w}
	case OpTranslate:
		return Vec3{
			r.Translation[0] * r.Amount * w,
			r.Translation[1] * r.Amount * w,
			r.Translation[2] * r.Amount * w,
		}
	case OpTaper:
		pivot := r.pivotAt(o)
		scale := r.Amount * r.TaperFactor * w * (position[1] - pivot[1])
		return Vec3{(position[0] - pivot[0]) * scale, 0, (position[2] - pivot[2]) * scale}
	default:
		pivot := r.pivotAt(o)
		factors := EffectiveScaleFactors(w, r.Amount, r.AxisScale)
		var delta Vec3
		for axis := range 3 {
			delta[axis] = pivot[axis] + (position[axis]-pivot[axis])*factors[axis] - position[axis]
		}
		return delta
	}
}

func (v Vec3) scaled(factor float64) Vec3 {
	return Vec3{v[0] * factor, v[1] * factor, v[2] * factor}
}

// ApplyMultiRegionDeform writes p' = p + Σ offsets of every region from the
// original positions into preview, never stacking on an earlier preview.
func ApplyMultiRegionDeform(original, preview []float32, regions []ActiveRegionDeform, epsilon float64) DeformMetrics {
	vertexCount := min(len(original)/3, len(preview)/3)
	accumulator := metricsAccumulator{metrics: DeformMetrics{VertexCount: vertexCount}}
	for i := range vertexCount {
		position := vertexOffset(original, i, Vec3{})
		var delta Vec3
		for index := range regions {
			region := &regions[index]
			if region.Amount == 0 {
				continue
			}
			offset := region.offset(i, position)
			delta = Vec3{delta[0] + offset[0], delta[1] + offset[1], delta[2] + offset[2]}
		}

		o := i * 3
		for axis := range 3 {
			preview[o+axis] = float32(position[axis] + delta[axis])
		}
		accumulator.add(hypot3(delta[0], delta[1], delta[2]), epsilon)
	}
	return accumulator.result()
}

// ComposeDisplayWeights returns the strongest influence per vertex across the
// regions for the heatmap. With ignoreAmount the raw masks show even at amount
// 0, as when previewing a bone.
func ComposeDisplayWeights(vertexCount int, regions []ActiveRegionDeform, ignoreAmount bool) []float32 {
	out := make([]float32, max(0, vertexCount))
	ComposeDisplayWeightsInto(out, vertexCount, regions, ignoreAmount)
	return out
}

func ComposeDisplayWeightsInto(out []float32, vertexCount int, regions []ActiveRegionDeform, ignoreAmount bool) {
	count := max(0, min(len(out), vertexCount))
	clear(out[:count])
	for index := range regions {
		region := &regions[index]
		if !ignoreAmount && region.Amount == 0 {
			continue
		}
		strength := 1.0
		if !ignoreAmount {
			strength = min(1, math.Abs(region.Amount)*2)
		}
		for i := range count {
			if w := min(1, max(0, region.weightAt(i)*strength)); w > float64(out[i]) {
				out[i] = float32(w)
			}
		}
	}
}

// WriteRegionHeatmap fills packed RGB colors with the heatmap of the regions'
// raw masks, using display as per-vertex scratch. Pass no regions to show the
// unselected color everywhere.
func WriteRegionHeatmap(colors, display []float32, regions []ActiveRegionDeform) {
	vertexCount := min(len(display), len(colors)/3)
	ComposeDisplayWeightsInto(display, vertexCount, regions, true)
	WriteWeightColors(display[:vertexCount], colors)
}

// ComputeRegionPivot returns the weighted centroid of a mask, or fallback when
// the mask is empty.
func ComputeRegionPivot(positions, weights []float32, fallback Vec3) Vec3 {
	if pivot, ok := weightedCentroid(positions, weights, epsilon); ok {
		return pivot
	}
	return fallback
}

type maskPart struct {
	root      int
	sum       Vec3
	total     float64
	low, high Vec3
}

// ComputeMaskPivots gives each separate part of a mask its own pivot, so
// scaling both thighs thickens each instead of pushing them apart. Parts whose
// bounds overlap keep one pivot: a pocket or belt loop on a torso must move with
// the torso, not grow in place. Parts never touch through a nonzero weight, so
// their different pivots cannot open a gap.
//
// It returns nil when one pivot applies to the whole mask.
func ComputeMaskPivots(positions, weights []float32, adjacency *VertexAdjacency) []float32 {
	vertexCount := min(len(weights), len(positions)/3)
	masked := func(vertex int) bool {
		o := vertex * 3
		return weights[vertex] > 0 && isFinite(float64(positions[o])) && isFinite(float64(positions[o+1])) &&
			isFinite(float64(positions[o+2]))
	}

	partOf := make([]int32, vertexCount)
	for i := range partOf {
		partOf[i] = -1
	}
	var parts []*maskPart
	var stack []int
	for seed := range vertexCount {
		if partOf[seed] >= 0 || !masked(seed) {
			continue
		}
		part := &maskPart{
			root: len(parts),
			low:  Vec3{math.Inf(1), math.Inf(1), math.Inf(1)},
			high: Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)},
		}
		partOf[seed] = int32(len(parts))
		stack = append(stack[:0], seed)
		for len(stack) > 0 {
			vertex := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			w := float64(weights[vertex])
			for axis := range 3 {
				value := float64(positions[vertex*3+axis])
				part.sum[axis] += value * w
				part.low[axis] = min(part.low[axis], value)
				part.high[axis] = max(part.high[axis], value)
			}
			part.total += w
			for _, neighbor := range adjacency.NeighborsOf(vertex) {
				if int(neighbor) >= vertexCount || partOf[neighbor] >= 0 || !masked(int(neighbor)) {
					continue
				}
				partOf[neighbor] = int32(len(parts))
				stack = append(stack, int(neighbor))
			}
		}
		parts = append(parts, part)
	}

	findRoot := func(index int) int {
		for parts[index].root != index {
			parts[index].root = parts[parts[index].root].root
			index = parts[index].root
		}
		return index
	}
	byLowX := make([]int, len(parts))
	for i := range byLowX {
		byLowX[i] = i
	}
	slices.SortStableFunc(byLowX, func(a, b int) int { return cmp.Compare(parts[a].low[0], parts[b].low[0]) })
	for order, index := range byLowX {
		part := parts[index]
		for _, otherIndex := range byLowX[order+1:] {
			other := parts[otherIndex]
			if other.low[0] > part.high[0] {
				break
			}
			if other.low[1] <= part.high[1] && part.low[1] <= other.high[1] &&
				other.low[2] <= part.high[2] && part.low[2] <= other.high[2] {
				parts[findRoot(index)].root = findRoot(otherIndex)
			}
		}
	}

	type group struct {
		sum   Vec3
		total float64
	}
	groups := make(map[int]*group)
	for index, part := range parts {
		root := findRoot(index)
		g := groups[root]
		if g == nil {
			g = &group{}
			groups[root] = g
		}
		for axis := range 3 {
			g.sum[axis] += part.sum[axis]
		}
		g.total += part.total
	}
	if len(groups) <= 1 {
		return nil
	}

	pivots := make([]float32, vertexCount*3)
	for vertex := range vertexCount {
		if partOf[vertex] < 0 {
			continue
		}
		g := groups[findRoot(int(partOf[vertex]))]
		for axis := range 3 {
			pivots[vertex*3+axis] = float32(g.sum[axis] / g.total)
		}
	}
	return pivots
}

// ComposeEffectiveScales multiplies the per-vertex scale factors of every
// scale-like region into packed xyz scales for vector correction.
func ComposeEffectiveScales(vertexCount int, regions []ActiveRegionDeform) []float32 {
	scales := make([]float32, max(0, vertexCount)*3)
	for i := range scales {
		scales[i] = 1
	}
	for index := range regions {
		region := &regions[index]
		if region.Amount == 0 || region.Operation == OpInflate || region.Operation == OpDisplace {
			continue
		}
		for i := range max(0, vertexCount) {
			w := region.weightAt(i)
			if w <= 0 {
				continue
			}
			factors := EffectiveScaleFactors(w, region.Amount, region.AxisScale)
			for axis := range 3 {
				scales[i*3+axis] = float32(float64(scales[i*3+axis]) * factors[axis])
			}
		}
	}
	return scales
}

// SelectionMask is a named mask that shape layers refer to.
type SelectionMask struct {
	ID      string
	Name    string
	Weights []float32
	Visible bool
	Locked  bool
}

// ShapeLayer applies one operation to a mask.
type ShapeLayer struct {
	ID          string
	Name        string
	MaskID      string
	Operation   RegionOperation
	Amount      float64
	AxisScale   Vec3
	Translation Vec3
	TaperFactor float64
	Enabled     bool
}

// EvaluateShapeLayers deforms preview from the original positions with every
// enabled layer whose mask is visible, each pivoting on its mask's centroid.
func EvaluateShapeLayers(original, preview []float32, masks []SelectionMask, layers []ShapeLayer, normals []float32) {
	byID := make(map[string]*SelectionMask, len(masks))
	for index := range masks {
		byID[masks[index].ID] = &masks[index]
	}

	var active []ActiveRegionDeform
	for _, layer := range layers {
		if !layer.Enabled || layer.Amount == 0 {
			continue
		}
		mask := byID[layer.MaskID]
		if mask == nil || !mask.Visible {
			continue
		}
		active = append(active, ActiveRegionDeform{
			ID:          layer.ID,
			Weights:     mask.Weights,
			Amount:      layer.Amount,
			AxisScale:   layer.AxisScale,
			Pivot:       ComputeRegionPivot(original, mask.Weights, ComputeBoundingCenter(original)),
			Operation:   layer.Operation,
			Normals:     normals,
			Translation: layer.Translation,
			TaperFactor: layer.TaperFactor,
		})
	}
	ApplyMultiRegionDeform(original, preview, active, DefaultDisplacementEpsilon)
}
