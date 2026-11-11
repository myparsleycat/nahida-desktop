package sculpt

import (
	"math"
	"slices"
)

// AnatomyModel is a deformation that follows body anatomy rather than the
// selection weights.
type AnatomyModel string

const (
	AnatomyVolume AnatomyModel = "volume"
	AnatomyLimb   AnatomyModel = "limb"
	AnatomyTorso  AnatomyModel = "torso"
)

// AnatomyControls lists each model's controls in field order. The names are
// also the keys of the export change summary and of the slider labels.
var AnatomyControls = map[AnatomyModel][]string{
	AnatomyVolume: {"size", "projection", "lift", "spacing"},
	AnatomyLimb:   {"girth", "bias"},
	AnatomyTorso:  {"girth", "width"},
}

// AnatomyBasis is the precomputed displacement of each control at value 1.
type AnatomyBasis struct {
	// Gate is the smoothed selection that bounds the fields.
	Gate []float32
	// Fields holds one packed xyz displacement field per control in
	// AnatomyControls[model] order.
	Fields [][]float32
}

const (
	// lobeSideShare is the share of the mask needed on each side before left
	// and right are shaped apart.
	lobeSideShare = 0.1
	// midlineBandRatio is the width, as a share of the mask's half width, over
	// which split lobes fade out at x = 0.
	midlineBandRatio      = 0.1
	gateSmoothIterations  = 3
	fieldSmoothIterations = 6
	// footprintRMSRatio scales the RMS footprint radius: a uniform disc has
	// R/√2, and the margin keeps the profile off the mask core.
	footprintRMSRatio = 1.6
	torsoBins         = 32
)

// AnatomyOptions are the inputs of BuildAnatomyBasis. Adjacency, Normals and
// WeldMap are optional, except that the volume model needs Normals.
type AnatomyOptions struct {
	Model     AnatomyModel
	Positions []float32
	Mask      []float32
	// Up is the mesh-space direction shown as up; see ModelSpaceUp.
	Up        Vec3
	Adjacency *VertexAdjacency
	Normals   []float32
	WeldMap   []uint32
}

// BuildAnatomyBasis builds displacement fields that follow body anatomy: the
// selection only decides which surface may move, and the model decides how it
// moves. Every field is linear in its control, so a slider change only
// recombines the fields. It returns false when there is nothing to shape.
func BuildAnatomyBasis(options AnatomyOptions) (*AnatomyBasis, bool) {
	positions, mask := options.Positions, options.Mask
	vertexCount := min(len(mask), len(positions)/3)
	up, ok := options.Up.normalize()
	if !ok || !slices.ContainsFunc(mask, func(w float32) bool { return w > 0 }) {
		return nil, false
	}
	if options.Model == AnatomyVolume && len(options.Normals) < vertexCount*3 {
		return nil, false
	}
	controls, ok := AnatomyControls[options.Model]
	if !ok {
		return nil, false
	}

	adjacency := options.Adjacency
	if adjacency != nil && adjacency.VertexCount() < vertexCount {
		adjacency = nil
	}
	gate := slices.Clone(mask)
	if adjacency != nil {
		SmoothSelectionWeights(gate, adjacency, 0.5, gateSmoothIterations)
	}
	fields := make([][]float32, len(controls))
	for i := range fields {
		fields[i] = make([]float32, vertexCount*3)
	}
	context := shapeContext{
		positions:   positions,
		mask:        mask,
		gate:        gate,
		up:          up,
		center:      ComputeBoundingCenter(positions),
		vertexCount: vertexCount,
		fields:      fields,
	}

	if options.Model == AnatomyTorso {
		context.shapeTorso()
	} else {
		for _, lobe := range splitLobes(positions, mask, gate, vertexCount) {
			if options.Model == AnatomyVolume {
				context.shapeVolume(lobe, options.Normals)
			} else {
				context.shapeLimb(lobe)
			}
		}
	}

	if adjacency != nil {
		var support []int
		for vertex := range vertexCount {
			if gate[vertex] > 0 {
				support = append(support, vertex)
			}
		}
		scratch := make([]float32, len(support)*3)
		for _, field := range fields {
			smoothField(field, support, scratch, adjacency, vertexCount)
		}
	}
	if options.WeldMap != nil {
		for _, field := range fields {
			for vertex := range min(vertexCount, len(options.WeldMap)) {
				source := int(options.WeldMap[vertex]) * 3
				if source+3 <= len(field) {
					copy(field[vertex*3:vertex*3+3], field[source:source+3])
				}
			}
		}
	}
	return &AnatomyBasis{Gate: gate, Fields: fields}, true
}

// CombineAnatomyBasis sums each field scaled by its control value into out,
// allocating it when nil, and returns it. Missing values count as 0.
func CombineAnatomyBasis(basis *AnatomyBasis, values []float64, out []float32) []float32 {
	if out == nil {
		size := 0
		if len(basis.Fields) > 0 {
			size = len(basis.Fields[0])
		}
		out = make([]float32, size)
	}
	clear(out)
	for index, field := range basis.Fields {
		if index >= len(values) || values[index] == 0 {
			continue
		}
		for i := range min(len(out), len(field)) {
			addFloat32(out, i, float64(field[i])*values[index])
		}
	}
	return out
}

type shapeContext struct {
	positions   []float32
	mask        []float32
	gate        []float32
	up          Vec3
	center      Vec3
	vertexCount int
	fields      [][]float32
}

type lobe struct {
	vertices []int
	// side is -1 or 1 for the side of the symmetry plane holding most of the lobe.
	side float64
	// midlineBand is the distance from x = 0 over which a split lobe fades in;
	// 0 when the mask is not split.
	midlineBand float64
}

// midlineFade brings split lobes to zero at x = 0, so the left and right fields
// meet without a jump.
func (l lobe) midlineFade(x float64) float64 {
	if l.midlineBand <= 0 {
		return 1
	}
	return smoothstep01(Clamp01(l.side * x / l.midlineBand))
}

// splitLobes splits a selection across the x = 0 symmetry plane, the same
// plane the brush mirror uses.
func splitLobes(positions, mask, gate []float32, vertexCount int) []lobe {
	negative, positive, halfWidth := 0.0, 0.0, 0.0
	for vertex := range vertexCount {
		x := float64(positions[vertex*3])
		if x < 0 {
			negative += float64(mask[vertex])
		} else {
			positive += float64(mask[vertex])
		}
		if mask[vertex] > 0 {
			halfWidth = max(halfWidth, math.Abs(x))
		}
	}

	total := negative + positive
	split := negative > total*lobeSideShare && positive > total*lobeSideShare
	var lobes []lobe
	if split {
		band := halfWidth * midlineBandRatio
		lobes = []lobe{{side: -1, midlineBand: band}, {side: 1, midlineBand: band}}
	} else {
		side := -1.0
		if positive >= negative {
			side = 1
		}
		lobes = []lobe{{side: side}}
	}
	for vertex := range vertexCount {
		if gate[vertex] <= 0 {
			continue
		}
		index := 0
		if split && positions[vertex*3] >= 0 {
			index = 1
		}
		lobes[index].vertices = append(lobes[index].vertices, vertex)
	}
	return slices.DeleteFunc(lobes, func(l lobe) bool { return len(l.vertices) == 0 })
}

func (c *shapeContext) position(vertex int, origin Vec3) Vec3 {
	return vertexOffset(c.positions, vertex, origin)
}

// shapeVolume models breasts and buttocks: tissue grows from a base on the body
// wall toward an apex.
func (c *shapeContext) shapeVolume(l lobe, normals []float32) {
	sizeField, projectionField, liftField, spacingField := c.fields[0], c.fields[1], c.fields[2], c.fields[3]

	var normalSum, centroid Vec3
	weightSum, maxWeight := 0.0, 0.0
	for _, vertex := range l.vertices {
		weight := float64(c.mask[vertex])
		if weight <= 0 {
			continue
		}
		for axis := range 3 {
			normalSum[axis] += float64(normals[vertex*3+axis]) * weight
			centroid[axis] += float64(c.positions[vertex*3+axis]) * weight
		}
		weightSum += weight
		maxWeight = max(maxWeight, weight)
	}
	direction, ok := normalSum.normalize()
	if !ok || weightSum <= epsilon {
		return
	}
	for axis := range 3 {
		centroid[axis] /= weightSum
	}

	heightOf := func(vertex int) float64 { return c.position(vertex, centroid).dot(direction) }
	core := maxWeight * 0.5
	coreMin, coreMax := math.Inf(1), math.Inf(-1)
	edgeSum, edgeCount := 0.0, 0
	maskMin := math.Inf(1)
	for _, vertex := range l.vertices {
		weight := float64(c.mask[vertex])
		if weight <= 0 {
			continue
		}
		height := heightOf(vertex)
		maskMin = min(maskMin, height)
		if weight >= core {
			coreMin = min(coreMin, height)
			coreMax = max(coreMax, height)
		} else {
			edgeSum += height
			edgeCount++
		}
	}
	if !isFinite(coreMax) {
		return
	}

	// Averaging the top of the core keeps the apex stable on flat or noisy tips.
	var apex Vec3
	apexCount := 0
	apexFloor := coreMax - (coreMax-coreMin)*0.1
	for _, vertex := range l.vertices {
		if float64(c.mask[vertex]) < core || heightOf(vertex) < apexFloor {
			continue
		}
		for axis := range 3 {
			apex[axis] += float64(c.positions[vertex*3+axis])
		}
		apexCount++
	}
	for axis := range 3 {
		apex[axis] /= float64(apexCount)
	}

	base := maskMin
	if edgeCount > 0 {
		base = min(edgeSum/float64(edgeCount), coreMin)
	}
	apexOffset := Vec3{apex[0] - centroid[0], apex[1] - centroid[1], apex[2] - centroid[2]}
	height := apexOffset.dot(direction) - base
	if height <= epsilon {
		return
	}

	planarSum := 0.0
	for _, vertex := range l.vertices {
		weight := float64(c.mask[vertex])
		if weight <= 0 {
			continue
		}
		planar := c.position(vertex, apex).reject(direction)
		planarSum += planar.dot(planar) * weight
	}
	radius := math.Sqrt(planarSum/weightSum) * footprintRMSRatio
	if radius <= epsilon {
		return
	}

	lift, hasLift := c.up.reject(direction).normalize()
	outward, hasOutward := Vec3{l.side, 0, 0}.reject(direction).normalize()
	for _, vertex := range l.vertices {
		planar := c.position(vertex, apex).reject(direction)
		r := math.Sqrt(planar.dot(planar)) / radius
		if r >= 1 {
			continue
		}
		falloff := 1 - r*r
		profile := falloff * falloff * float64(c.gate[vertex]) * l.midlineFade(float64(c.positions[vertex*3]))
		rise := max(0, heightOf(vertex)-base)
		t := min(1, rise/height)
		o := vertex * 3
		for axis := range 3 {
			projection := direction[axis] * rise * profile
			addFloat32(projectionField, o+axis, projection)
			addFloat32(sizeField, o+axis, projection+planar[axis]*0.5*t*profile)
			if hasLift {
				addFloat32(liftField, o+axis, lift[axis]*0.5*height*t*profile)
			}
			if hasOutward {
				addFloat32(spacingField, o+axis, outward[axis]*0.5*height*t*profile)
			}
		}
	}
}

func (c *shapeContext) weightedCentroid(vertices []int, weightSum float64) Vec3 {
	var centroid Vec3
	for _, vertex := range vertices {
		weight := float64(c.mask[vertex])
		if weight <= 0 {
			continue
		}
		for axis := range 3 {
			centroid[axis] += float64(c.positions[vertex*3+axis]) * weight
		}
	}
	for axis := range 3 {
		centroid[axis] /= weightSum
	}
	return centroid
}

// shapeLimb models thighs, arms and calves: girth changes around the limb axis,
// never along it.
func (c *shapeContext) shapeLimb(l lobe) {
	girthField, biasField := c.fields[0], c.fields[1]

	weightSum := 0.0
	for _, vertex := range l.vertices {
		if weight := float64(c.mask[vertex]); weight > 0 {
			weightSum += weight
		}
	}
	if weightSum <= epsilon {
		return
	}
	centroid := c.weightedCentroid(l.vertices, weightSum)

	var covariance [9]float64
	for _, vertex := range l.vertices {
		weight := float64(c.mask[vertex])
		if weight <= 0 {
			continue
		}
		offset := c.position(vertex, centroid)
		for row := range 3 {
			for column := range 3 {
				covariance[row*3+column] += offset[row] * offset[column] * weight
			}
		}
	}
	axis, ok := principalAxis(covariance, c.up)
	if !ok {
		return
	}
	// The proximal end faces the body center for legs and for arms in any pose,
	// unlike up, which is nearly perpendicular to T-pose arms.
	toCenter := Vec3{c.center[0] - centroid[0], c.center[1] - centroid[1], c.center[2] - centroid[2]}
	if axis.dot(toCenter) < 0 {
		axis = axis.scaled(-1)
	}

	low, high := maskedRange(l.vertices, c.mask, func(vertex int) float64 {
		return c.position(vertex, centroid).dot(axis)
	})
	if high-low <= epsilon {
		return
	}
	for _, vertex := range l.vertices {
		offset := c.position(vertex, centroid)
		radial := offset.reject(axis)
		u := min(1, max(0, (offset.dot(axis)-low)/(high-low)))
		centered := 2*u - 1
		belly := (1 - centered*centered) * float64(c.gate[vertex]) * l.midlineFade(float64(c.positions[vertex*3]))
		o := vertex * 3
		for i := range 3 {
			addFloat32(girthField, o+i, radial[i]*0.5*belly)
			addFloat32(biasField, o+i, radial[i]*0.5*centered*belly)
		}
	}
}

// shapeTorso models the waist and hips: circumference changes around a
// centerline that follows the torso.
func (c *shapeContext) shapeTorso() {
	girthField, widthField := c.fields[0], c.fields[1]

	var vertices []int
	for vertex := range c.vertexCount {
		if c.gate[vertex] > 0 {
			vertices = append(vertices, vertex)
		}
	}
	heightOf := func(vertex int) float64 { return c.position(vertex, Vec3{}).dot(c.up) }
	low, high := maskedRange(vertices, c.mask, heightOf)
	if high-low <= epsilon {
		return
	}

	// Slice centroids trace the torso centerline, which leans and curves in real models.
	sums := make([][4]float64, torsoBins)
	for _, vertex := range vertices {
		weight := float64(c.mask[vertex])
		if weight <= 0 {
			continue
		}
		scaled := math.Floor((heightOf(vertex) - low) / (high - low) * torsoBins)
		if math.IsNaN(scaled) {
			continue
		}
		bin := int(min(torsoBins-1, max(0, scaled)))
		planar := c.position(vertex, Vec3{}).reject(c.up)
		for axis := range 3 {
			sums[bin][axis] += planar[axis] * weight
		}
		sums[bin][3] += weight
	}
	centers, ok := fillBins(sums)
	if !ok {
		return
	}

	lateral, hasLateral := Vec3{1, 0, 0}.reject(c.up).normalize()
	for _, vertex := range vertices {
		position := (heightOf(vertex) - low) / (high - low)
		scaled := min(torsoBins-1, max(0, position*torsoBins-0.5))
		if math.IsNaN(scaled) {
			continue
		}
		lower := int(math.Floor(scaled))
		upper := min(torsoBins-1, lower+1)
		blend := scaled - float64(lower)
		planar := c.position(vertex, Vec3{}).reject(c.up)
		var radial Vec3
		for axis := range 3 {
			radial[axis] = planar[axis] - (centers[lower][axis]*(1-blend) + centers[upper][axis]*blend)
		}
		u := min(1, max(0, position))
		centered := 2*u - 1
		belly := (1 - centered*centered) * float64(c.gate[vertex])
		side := 0.0
		if hasLateral {
			side = radial.dot(lateral)
		}
		o := vertex * 3
		for axis := range 3 {
			addFloat32(girthField, o+axis, radial[axis]*0.5*belly)
			if hasLateral {
				addFloat32(widthField, o+axis, lateral[axis]*side*0.5*belly)
			}
		}
	}
}

// fillBins fills empty slices from their neighbors and smooths the centerline.
func fillBins(sums [][4]float64) ([]Vec3, bool) {
	var known []int
	for index, sum := range sums {
		if sum[3] > epsilon {
			known = append(known, index)
		}
	}
	if len(known) == 0 {
		return nil, false
	}

	mean := func(index, axis int) float64 { return sums[index][axis] / sums[index][3] }
	centers := make([]Vec3, len(sums))
	for index := range sums {
		below, above := known[0], known[len(known)-1]
		for _, candidate := range known {
			if candidate <= index {
				below = candidate
			}
		}
		for _, candidate := range slices.Backward(known) {
			if candidate >= index {
				above = candidate
			}
		}
		t := 0.0
		if above != below {
			t = float64(index-below) / float64(above-below)
		}
		for axis := range 3 {
			centers[index][axis] = mean(below, axis)*(1-t) + mean(above, axis)*t
		}
	}

	for range 2 {
		previous := slices.Clone(centers)
		for index := range centers {
			before := previous[max(0, index-1)]
			after := previous[min(len(previous)-1, index+1)]
			for axis := range 3 {
				centers[index][axis] = (before[axis] + previous[index][axis]*2 + after[axis]) / 4
			}
		}
	}
	return centers, true
}

// smoothField smooths a displacement field over the gated surface with the
// outside held at zero. Smoothing the displacement rather than the positions
// keeps surface detail while removing the steps selection edges would leave.
func smoothField(field []float32, support []int, next []float32, adjacency *VertexAdjacency, vertexCount int) {
	for range fieldSmoothIterations {
		for index, vertex := range support {
			neighbors := adjacency.NeighborsOf(vertex)
			o := vertex * 3
			if len(neighbors) == 0 {
				copy(next[index*3:index*3+3], field[o:o+3])
				continue
			}
			var sum Vec3
			for _, neighbor := range neighbors {
				if int(neighbor) >= vertexCount {
					continue
				}
				for axis := range 3 {
					sum[axis] += float64(field[int(neighbor)*3+axis])
				}
			}
			for axis := range 3 {
				next[index*3+axis] = float32((float64(field[o+axis]) + sum[axis]/float64(len(neighbors))) / 2)
			}
		}
		for index, vertex := range support {
			copy(field[vertex*3:vertex*3+3], next[index*3:index*3+3])
		}
	}
}

func maskedRange(vertices []int, mask []float32, value func(vertex int) float64) (low, high float64) {
	low, high = math.Inf(1), math.Inf(-1)
	for _, vertex := range vertices {
		if mask[vertex] <= 0 {
			continue
		}
		current := value(vertex)
		low = min(low, current)
		high = max(high, current)
	}
	return low, high
}

func principalAxis(covariance [9]float64, seed Vec3) (Vec3, bool) {
	axis := Vec3{seed[0] + 0.1, seed[1] + 0.2, seed[2] + 0.3}
	for range 32 {
		var next Vec3
		for row := range 3 {
			next[row] = covariance[row*3]*axis[0] + covariance[row*3+1]*axis[1] + covariance[row*3+2]*axis[2]
		}
		var ok bool
		if axis, ok = next.normalize(); !ok {
			return Vec3{}, false
		}
	}
	return axis, true
}
