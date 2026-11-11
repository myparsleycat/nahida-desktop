package sculpt

import "math"

// weldMinFacing separates duplicates facing further apart than this: they are
// the two sides of a double-sided shell.
const weldMinFacing = -0.5

// DeformMetrics summarizes how far a deformation moved the vertices.
type DeformMetrics struct {
	VertexCount      int
	MovedVertices    int
	MaxDisplacement  float64
	MeanDisplacement float64
}

type metricsAccumulator struct {
	metrics DeformMetrics
	sum     float64
}

func (a *metricsAccumulator) add(displacement, epsilon float64) {
	if displacement > epsilon {
		a.metrics.MovedVertices++
		a.sum += displacement
		if displacement > a.metrics.MaxDisplacement {
			a.metrics.MaxDisplacement = displacement
		}
	}
}

func (a *metricsAccumulator) result() DeformMetrics {
	if a.metrics.MovedVertices > 0 {
		a.metrics.MeanDisplacement = a.sum / float64(a.metrics.MovedVertices)
	}
	return a.metrics
}

// AnisotropicScaleFromOriginal writes preview = pivot + (original - pivot) *
// (1 + amount * axisScale * weight). It always starts from the original
// positions, so repeated calls never accumulate.
func AnisotropicScaleFromOriginal(original, preview, weights []float32, pivot Vec3, amount float64, axisScale Vec3) {
	vertexCount := min(len(weights), len(original)/3, len(preview)/3)
	for i := range vertexCount {
		o := i * 3
		w := float64(weights[i])
		if w <= 0 || amount == 0 {
			copy(preview[o:o+3], original[o:o+3])
			continue
		}
		factors := EffectiveScaleFactors(w, amount, axisScale)
		for axis := range 3 {
			preview[o+axis] = float32(pivot[axis] + (float64(original[o+axis])-pivot[axis])*factors[axis])
		}
	}
}

// ComputeVertexNormals returns area-weighted vertex normals for an indexed
// triangle mesh. Each weld group shares the summed normal of the duplicates that
// face the same way, so seams shade smoothly and double-sided shells keep both
// sides.
func ComputeVertexNormals(positions []float32, indices []uint32, weldGroups [][]uint32) []float32 {
	vertexCount := len(positions) / 3
	normals := make([]float32, vertexCount*3)

	for i := 0; i+2 < len(indices); i += 3 {
		ai, bi, ci := int(indices[i]), int(indices[i+1]), int(indices[i+2])
		if ai >= vertexCount || bi >= vertexCount || ci >= vertexCount {
			continue
		}
		a := vertexOffset(positions, ai, Vec3{})
		ab := vertexOffset(positions, bi, a)
		ac := vertexOffset(positions, ci, a)
		normal := Vec3{
			ab[1]*ac[2] - ab[2]*ac[1],
			ab[2]*ac[0] - ab[0]*ac[2],
			ab[0]*ac[1] - ab[1]*ac[0],
		}
		if !isFinite(normal[0]) || !isFinite(normal[1]) || !isFinite(normal[2]) {
			continue
		}
		if normal == (Vec3{}) {
			continue
		}
		for _, vertex := range [3]int{ai, bi, ci} {
			for axis := range 3 {
				addFloat32(normals, vertex*3+axis, normal[axis])
			}
		}
	}

	for _, group := range weldGroups {
		mergeNormalsByFacing(normals, group)
	}

	for i := range vertexCount {
		o := i * 3
		length := hypot3(float64(normals[o]), float64(normals[o+1]), float64(normals[o+2]))
		if length == 0 {
			continue
		}
		for axis := range 3 {
			normals[o+axis] = float32(float64(normals[o+axis]) / length)
		}
	}
	return normals
}

func mergeNormalsByFacing(normals []float32, group []uint32) {
	type cluster struct {
		sum      Vec3
		vertices []uint32
	}

	var clusters []*cluster
	for _, vertex := range group {
		normal := vertexOffset(normals, int(vertex), Vec3{})
		length := hypot3(normal[0], normal[1], normal[2])
		var match *cluster
		for _, candidate := range clusters {
			sumLength := hypot3(candidate.sum[0], candidate.sum[1], candidate.sum[2])
			if length == 0 || sumLength == 0 || normal.dot(candidate.sum)/(length*sumLength) > weldMinFacing {
				match = candidate
				break
			}
		}
		if match == nil {
			clusters = append(clusters, &cluster{sum: normal, vertices: []uint32{vertex}})
			continue
		}
		match.vertices = append(match.vertices, vertex)
		for axis := range 3 {
			match.sum[axis] += normal[axis]
		}
	}

	for _, c := range clusters {
		for _, vertex := range c.vertices {
			for axis := range 3 {
				normals[int(vertex)*3+axis] = float32(c.sum[axis])
			}
		}
	}
}

// EffectiveScaleFactors is the per-vertex scale I + weight * (A - I) used for
// normal and tangent correction.
func EffectiveScaleFactors(weight, amount float64, axisScale Vec3) Vec3 {
	return Vec3{
		1 + amount*axisScale[0]*weight,
		1 + amount*axisScale[1]*weight,
		1 + amount*axisScale[2]*weight,
	}
}

// CorrectSnorm8TangentNormal corrects one vertex of an SNORM8 tangent and normal
// buffer ([tx ty tz tw nx ny nz nw]) for a diagonal scale: the tangent follows
// the scale, the normal its inverse transpose, and the tangent is then
// re-orthogonalized. Unknown layouts must not call this.
func CorrectSnorm8TangentNormal(vectors []int8, vertex int, scales Vec3) {
	base := vertex * snorm8Stride
	if base < 0 || base+7 >= len(vectors) {
		return
	}
	if math.Abs(scales[0]-1) < epsilon && math.Abs(scales[1]-1) < epsilon && math.Abs(scales[2]-1) < epsilon {
		return
	}

	var tangent, normal Vec3
	for axis := range 3 {
		tangent[axis] = float64(vectors[base+axis]) / 127 * scales[axis]
		divisor := scales[axis]
		if divisor == 0 || math.IsNaN(divisor) {
			divisor = 1
		}
		normal[axis] = float64(vectors[base+4+axis]) / 127 / divisor
	}
	tangent = normalizeIfLong(tangent)
	normal = normalizeIfLong(normal)

	along := tangent.dot(normal)
	for axis := range 3 {
		tangent[axis] -= normal[axis] * along
	}
	tangent = normalizeIfLong(tangent)

	for axis := range 3 {
		vectors[base+axis] = snorm8(tangent[axis])
		vectors[base+4+axis] = snorm8(normal[axis])
	}
}

func normalizeIfLong(v Vec3) Vec3 {
	length := hypot3(v[0], v[1], v[2])
	if length > epsilon {
		return Vec3{v[0] / length, v[1] / length, v[2] / length}
	}
	return v
}

func snorm8(value float64) int8 {
	rounded := jsRound(value * 127)
	if rounded < -127 {
		return -127
	}
	if rounded > 127 {
		return 127
	}
	return int8(rounded)
}

// RecalculateNormalsAndTangents rewrites the normals of an SNORM8 tangent and
// normal buffer from the triangle normals of the positions. Without indices it
// returns the buffer unchanged.
func RecalculateNormalsAndTangents(positions []float32, indices []uint32, vectors []int8) []int8 {
	if len(indices) == 0 {
		return vectors
	}

	computed := ComputeVertexNormals(positions, indices, nil)
	vertexCount := min(len(positions)/3, len(vectors)/snorm8Stride)
	result := append([]int8(nil), vectors...)
	for i := range vertexCount {
		normal := vertexOffset(computed, i, Vec3{})
		length := hypot3(normal[0], normal[1], normal[2])
		if length <= epsilon {
			continue
		}
		for axis := range 3 {
			result[i*snorm8Stride+4+axis] = snorm8(normal[axis] / length)
		}
	}
	return result
}

// ComputeWeightedPivot returns the weight-averaged position, or the bounding
// box center when no weight is positive.
func ComputeWeightedPivot(positions, weights []float32) Vec3 {
	if pivot, ok := weightedCentroid(positions, weights, 0); ok {
		return pivot
	}
	return ComputeBoundingCenter(positions)
}

func weightedCentroid(positions, weights []float32, minTotal float64) (Vec3, bool) {
	var sum Vec3
	total := 0.0
	count := min(len(weights), len(positions)/3)
	for i := range count {
		w := float64(weights[i])
		if w <= 0 {
			continue
		}
		for axis := range 3 {
			sum[axis] += float64(positions[i*3+axis]) * w
		}
		total += w
	}
	if total <= minTotal {
		return Vec3{}, false
	}
	return Vec3{sum[0] / total, sum[1] / total, sum[2] / total}, true
}

// ComputeBoundingCenter returns the center of the axis-aligned bounding box.
func ComputeBoundingCenter(positions []float32) Vec3 {
	low, high, ok := positionBounds(positions)
	if !ok {
		return Vec3{}
	}
	return Vec3{(low[0] + high[0]) / 2, (low[1] + high[1]) / 2, (low[2] + high[2]) / 2}
}

// positionBounds skips NaN coordinates like the comparisons it replaces.
func positionBounds(positions []float32) (low, high Vec3, ok bool) {
	count := len(positions) / 3
	if count == 0 {
		return Vec3{}, Vec3{}, false
	}

	low = Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	high = Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := range count {
		for axis := range 3 {
			value := float64(positions[i*3+axis])
			if value < low[axis] {
				low[axis] = value
			}
			if value > high[axis] {
				high[axis] = value
			}
		}
	}
	return low, high, true
}

// DisplacementMetrics compares preview positions with the originals; epsilon
// is the smallest displacement counted as moved (the default is 1e-6).
func DisplacementMetrics(original, preview []float32, epsilon float64) DeformMetrics {
	vertexCount := min(len(original)/3, len(preview)/3)
	accumulator := metricsAccumulator{metrics: DeformMetrics{VertexCount: vertexCount}}
	for i := range vertexCount {
		o := i * 3
		accumulator.add(hypot3(
			float64(preview[o])-float64(original[o]),
			float64(preview[o+1])-float64(original[o+1]),
			float64(preview[o+2])-float64(original[o+2]),
		), epsilon)
	}
	return accumulator.result()
}
