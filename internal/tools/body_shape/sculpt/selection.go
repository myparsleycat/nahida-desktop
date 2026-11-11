package sculpt

import (
	"math"
	"slices"
)

// weldCellRatio sizes the weld grid relative to the mesh extent. Seam duplicates
// can differ by float rounding; the cell is far below any modeled detail.
const weldCellRatio = 2e-5

// VertexAdjacency lists each vertex's neighbors in CSR form: the neighbors of v
// are Neighbors[Offsets[v]:Offsets[v+1]], sorted ascending.
type VertexAdjacency struct {
	Offsets   []uint32
	Neighbors []uint32
}

// VertexCount is the number of vertices the adjacency covers.
func (a *VertexAdjacency) VertexCount() int {
	return max(0, len(a.Offsets)-1)
}

// NeighborsOf returns the neighbors of a vertex, or nil when it is out of range.
func (a *VertexAdjacency) NeighborsOf(vertex int) []uint32 {
	if vertex < 0 || vertex+1 >= len(a.Offsets) {
		return nil
	}
	return a.Neighbors[a.Offsets[vertex]:a.Offsets[vertex+1]]
}

type cellKey struct {
	x, y, z float64
}

// BuildPositionWeldMap maps each vertex to the first vertex at the same
// position. Game meshes split vertices along UV and material seams, so index
// topology alone sees those seams as open boundaries.
func BuildPositionWeldMap(positions []float32) []uint32 {
	vertexCount := len(positions) / 3
	weldMap := make([]uint32, vertexCount)
	finiteAt := func(vertex int) bool {
		o := vertex * 3
		return isFinite(float64(positions[o])) && isFinite(float64(positions[o+1])) &&
			isFinite(float64(positions[o+2]))
	}

	low := Vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := Vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for vertex := range vertexCount {
		if !finiteAt(vertex) {
			continue
		}
		for axis := range 3 {
			low[axis] = min(low[axis], float64(positions[vertex*3+axis]))
			high[axis] = max(high[axis], float64(positions[vertex*3+axis]))
		}
	}

	extent := max(high[0]-low[0], high[1]-low[1], high[2]-low[2])
	cell := max(extent*weldCellRatio, math.SmallestNonzeroFloat64)
	firstsByCell := make(map[cellKey][]uint32)
	for vertex := range vertexCount {
		weldMap[vertex] = uint32(vertex)
		if !finiteAt(vertex) {
			continue
		}
		position := vertexOffset(positions, vertex, Vec3{})
		x := jsRound((position[0] - low[0]) / cell)
		y := jsRound((position[1] - low[1]) / cell)
		z := jsRound((position[2] - low[2]) / cell)

		// Twins differing by rounding can fall on either side of a cell boundary.
		nearest := -1
		nearestDistance := cell * cell
		for cx := max(x-1, 0); cx <= x+1; cx++ {
			for cy := max(y-1, 0); cy <= y+1; cy++ {
				for cz := max(z-1, 0); cz <= z+1; cz++ {
					for _, first := range firstsByCell[cellKey{cx, cy, cz}] {
						offset := vertexOffset(positions, int(first), position)
						if distance := offset.dot(offset); distance <= nearestDistance {
							nearest = int(first)
							nearestDistance = distance
						}
					}
				}
			}
		}
		if nearest >= 0 {
			weldMap[vertex] = uint32(nearest)
			continue
		}

		key := cellKey{x, y, z}
		firstsByCell[key] = append(firstsByCell[key], uint32(vertex))
	}
	return weldMap
}

// CollectWeldGroups lists the vertices sharing each weld representative,
// representative first, leaving out unwelded vertices.
func CollectWeldGroups(weldMap []uint32) [][]uint32 {
	var groups [][]uint32
	groupOf := make(map[uint32]int)
	for vertex, first := range weldMap {
		if int(first) == vertex {
			continue
		}
		index, ok := groupOf[first]
		if !ok {
			groupOf[first] = len(groups)
			groups = append(groups, []uint32{first, uint32(vertex)})
			continue
		}
		groups[index] = append(groups[index], uint32(vertex))
	}
	return groups
}

// SyncWeldedWeights gives every welded vertex one weight so a mask edit cannot
// pull a seam apart.
func SyncWeldedWeights(weights []float32, weldMap []uint32, mode BrushMode) {
	vertexCount := min(len(weights), len(weldMap))
	for vertex := range vertexCount {
		first := weldMap[vertex]
		if int(first) == vertex {
			continue
		}
		weights[first] = mode.merge(weights[first], weights[vertex])
	}
	for vertex := range vertexCount {
		weights[vertex] = weights[weldMap[vertex]]
	}
}

// BuildVertexAdjacency builds triangle-edge adjacency. Each weld group then
// shares the union of its members' neighbors, so seams connect.
func BuildVertexAdjacency(vertexCount int, indices []uint32, weldGroups [][]uint32) *VertexAdjacency {
	vertexCount = max(0, vertexCount)
	neighbors := make([][]uint32, vertexCount)
	link := func(a, b uint32) {
		if a != b {
			neighbors[a] = append(neighbors[a], b)
			neighbors[b] = append(neighbors[b], a)
		}
	}
	for i := 0; i+2 < len(indices); i += 3 {
		a, b, c := indices[i], indices[i+1], indices[i+2]
		if int(a) >= vertexCount || int(b) >= vertexCount || int(c) >= vertexCount {
			continue
		}
		link(a, b)
		link(a, c)
		link(b, c)
	}
	for vertex := range neighbors {
		neighbors[vertex] = sortedUnique(neighbors[vertex])
	}

	for _, group := range weldGroups {
		shared := slices.Clone(group)
		for _, vertex := range group {
			shared = append(shared, neighbors[vertex]...)
		}
		shared = sortedUnique(shared)
		for _, vertex := range group {
			neighbors[vertex] = slices.DeleteFunc(slices.Clone(shared), func(n uint32) bool { return n == vertex })
		}
	}

	offsets := make([]uint32, vertexCount+1)
	for vertex := range vertexCount {
		offsets[vertex+1] = offsets[vertex] + uint32(len(neighbors[vertex]))
	}
	flattened := make([]uint32, 0, offsets[vertexCount])
	for _, list := range neighbors {
		flattened = append(flattened, list...)
	}
	return &VertexAdjacency{Offsets: offsets, Neighbors: flattened}
}

func sortedUnique(values []uint32) []uint32 {
	slices.Sort(values)
	return slices.Compact(values)
}

// GeodesicBrushWorkspace holds scratch state reused across brush dabs so a
// stroke does not reallocate per-vertex distances.
type GeodesicBrushWorkspace struct {
	distances     []float64
	touched       []uint32
	heapVertices  []uint32
	heapDistances []float64
	changed       []uint32
}

func NewGeodesicBrushWorkspace(vertexCount int) *GeodesicBrushWorkspace {
	distances := make([]float64, max(0, vertexCount))
	for i := range distances {
		distances[i] = math.Inf(1)
	}
	return &GeodesicBrushWorkspace{distances: distances}
}

// GeodesicBrushOptions describes a brush that spreads along mesh edges.
type GeodesicBrushOptions struct {
	Positions []float32
	Adjacency *VertexAdjacency
	Weights   []float32
	Seeds     []uint32
	Radius    float64
	Strength  float64
	// Hardness 0 means 1.
	Hardness float64
	Mode     BrushMode
	// FrontFacingOnly with Normals and HitNormal leaves vertices facing away
	// from the hit unpainted; the brush still spreads through them.
	FrontFacingOnly bool
	Normals         []float32
	HitNormal       *Vec3
	// NormalThreshold nil means DefaultNormalThreshold.
	NormalThreshold *float64
	// Workspace is reused when it matches the vertex count.
	Workspace *GeodesicBrushWorkspace
}

// ApplyGeodesicBrush paints vertices whose accumulated edge distance from any
// seed is below the radius, so the brush cannot jump to a nearby surface that is
// not connected. It returns the vertices whose weight changed.
func ApplyGeodesicBrush(options GeodesicBrushOptions) []uint32 {
	if options.Adjacency == nil {
		return nil
	}
	vertexCount := min(len(options.Weights), options.Adjacency.VertexCount(), len(options.Positions)/3)
	if vertexCount == 0 || options.Radius <= 0 {
		return nil
	}

	workspace := options.Workspace
	if workspace == nil || len(workspace.distances) != vertexCount {
		workspace = NewGeodesicBrushWorkspace(vertexCount)
	}
	distances := workspace.distances
	for _, vertex := range workspace.touched {
		distances[vertex] = math.Inf(1)
	}
	workspace.touched = workspace.touched[:0]
	workspace.heapVertices = workspace.heapVertices[:0]
	workspace.heapDistances = workspace.heapDistances[:0]
	workspace.changed = workspace.changed[:0]

	for _, seed := range options.Seeds {
		if int(seed) >= vertexCount || distances[seed] == 0 {
			continue
		}
		distances[seed] = 0
		workspace.touched = append(workspace.touched, seed)
		workspace.push(seed, 0)
	}

	threshold := DefaultNormalThreshold
	if options.NormalThreshold != nil {
		threshold = *options.NormalThreshold
	}
	filter := options.FrontFacingOnly && options.Normals != nil && options.HitNormal != nil

	for len(workspace.heapVertices) > 0 {
		vertex, distance := workspace.pop()
		if distance != distances[vertex] || distance >= options.Radius {
			continue
		}

		if !filter || !facesAway(options.Normals, int(vertex), *options.HitNormal, threshold) {
			falloff := BrushFalloff(distance, options.Radius, options.Hardness)
			previous := float64(options.Weights[vertex])
			if next := options.Mode.apply(previous, falloff, options.Strength); next != previous {
				options.Weights[vertex] = float32(next)
				workspace.changed = append(workspace.changed, vertex)
			}
		}

		origin := vertexOffset(options.Positions, int(vertex), Vec3{})
		for _, neighbor := range options.Adjacency.NeighborsOf(int(vertex)) {
			if int(neighbor) >= vertexCount {
				continue
			}
			edge := vertexOffset(options.Positions, int(neighbor), origin)
			next := distance + math.Sqrt(edge.dot(edge))
			if next >= options.Radius || next >= distances[neighbor] {
				continue
			}
			if math.IsInf(distances[neighbor], 1) {
				workspace.touched = append(workspace.touched, neighbor)
			}
			distances[neighbor] = next
			workspace.push(neighbor, next)
		}
	}
	return slices.Clone(workspace.changed)
}

func (w *GeodesicBrushWorkspace) push(vertex uint32, distance float64) {
	index := len(w.heapVertices)
	w.heapVertices = append(w.heapVertices, vertex)
	w.heapDistances = append(w.heapDistances, distance)
	for index > 0 {
		parent := (index - 1) / 2
		if w.heapDistances[parent] <= distance {
			break
		}
		w.heapVertices[index] = w.heapVertices[parent]
		w.heapDistances[index] = w.heapDistances[parent]
		index = parent
	}
	w.heapVertices[index] = vertex
	w.heapDistances[index] = distance
}

func (w *GeodesicBrushWorkspace) pop() (uint32, float64) {
	vertex, distance := w.heapVertices[0], w.heapDistances[0]
	last := len(w.heapVertices) - 1
	lastVertex, lastDistance := w.heapVertices[last], w.heapDistances[last]
	w.heapVertices = w.heapVertices[:last]
	w.heapDistances = w.heapDistances[:last]
	if last == 0 {
		return vertex, distance
	}

	index := 0
	for {
		left := index*2 + 1
		if left >= last {
			break
		}
		child := left
		if right := left + 1; right < last && w.heapDistances[right] < w.heapDistances[left] {
			child = right
		}
		if w.heapDistances[child] >= lastDistance {
			break
		}
		w.heapVertices[index] = w.heapVertices[child]
		w.heapDistances[index] = w.heapDistances[child]
		index = child
	}
	w.heapVertices[index] = lastVertex
	w.heapDistances[index] = lastDistance
	return vertex, distance
}

// BuildSymmetryMap maps each vertex to the vertex nearest its mirror across
// the given axis (0, 1 or 2) within tolerance, or -1.
func BuildSymmetryMap(positions []float32, axis int, tolerance float64) []int32 {
	vertexCount := len(positions) / 3
	mirrorMap := make([]int32, vertexCount)
	for i := range mirrorMap {
		mirrorMap[i] = -1
	}
	if vertexCount == 0 {
		return mirrorMap
	}

	// Non-finite positions can never be within tolerance of anything, so they
	// stay out of the grid.
	cellSize := max(tolerance*2, 1e-4)
	cellOf := func(p Vec3) (cellKey, bool) {
		if !isFinite(p[0]) || !isFinite(p[1]) || !isFinite(p[2]) {
			return cellKey{}, false
		}
		return cellKey{math.Floor(p[0] / cellSize), math.Floor(p[1] / cellSize), math.Floor(p[2] / cellSize)}, true
	}
	grid := make(map[cellKey][]uint32)
	for i := range vertexCount {
		if key, ok := cellOf(vertexOffset(positions, i, Vec3{})); ok {
			grid[key] = append(grid[key], uint32(i))
		}
	}

	tolerance2 := tolerance * tolerance
	for i := range vertexCount {
		target := vertexOffset(positions, i, Vec3{})
		target[axis] = -target[axis]
		center, ok := cellOf(target)
		if !ok {
			continue
		}
		best := int32(-1)
		bestDist2 := tolerance2
		for dx := -1.0; dx <= 1; dx++ {
			for dy := -1.0; dy <= 1; dy++ {
				for dz := -1.0; dz <= 1; dz++ {
					for _, candidate := range grid[cellKey{center.x + dx, center.y + dy, center.z + dz}] {
						offset := vertexOffset(positions, int(candidate), target)
						if dist2 := offset.dot(offset); dist2 < bestDist2 {
							bestDist2 = dist2
							best = int32(candidate)
						}
					}
				}
			}
		}
		mirrorMap[i] = best
	}
	return mirrorMap
}

// MirrorWeightsWithCache copies source weights to their mirrors through a
// symmetry map and returns the mirrors that changed.
func MirrorWeightsWithCache(weights []float32, mirrorMap []int32, sources []uint32, mode BrushMode) []uint32 {
	vertexCount := min(len(weights), len(mirrorMap))
	var changed []uint32
	for _, source := range sources {
		if int(source) >= vertexCount {
			continue
		}
		mirror := mirrorMap[source]
		if mirror < 0 || int(mirror) >= vertexCount {
			continue
		}
		previous := weights[mirror]
		if next := mode.merge(previous, weights[source]); next != previous {
			weights[mirror] = next
			changed = append(changed, uint32(mirror))
		}
	}
	return changed
}

// SmoothSelectionWeights blends each weight toward its neighbors' average by
// strength, reading every iteration from the previous iteration's weights.
func SmoothSelectionWeights(weights []float32, adjacency *VertexAdjacency, strength float64, iterations int) {
	blend := Clamp01(strength)
	vertexCount := min(len(weights), adjacency.VertexCount())
	if blend == 0 || vertexCount == 0 {
		return
	}

	previous := make([]float32, len(weights))
	for range max(0, iterations) {
		copy(previous, weights)
		for vertex := range vertexCount {
			sum := 0.0
			count := 0
			for _, neighbor := range adjacency.NeighborsOf(vertex) {
				if int(neighbor) >= len(weights) {
					continue
				}
				sum += float64(previous[neighbor])
				count++
			}
			if count == 0 {
				continue
			}
			current := float64(previous[vertex])
			weights[vertex] = float32(current + (sum/float64(count)-current)*blend)
		}
	}
}

// GrowSelectionWeights raises each weight toward its strongest neighbor.
func GrowSelectionWeights(weights []float32, adjacency *VertexAdjacency, amount float64, iterations int) {
	strength := Clamp01(amount)
	vertexCount := min(len(weights), adjacency.VertexCount())
	if strength == 0 || vertexCount == 0 {
		return
	}

	previous := make([]float32, len(weights))
	for range max(0, iterations) {
		copy(previous, weights)
		for vertex := range vertexCount {
			highest := previous[vertex]
			for _, neighbor := range adjacency.NeighborsOf(vertex) {
				if int(neighbor) < len(weights) && previous[neighbor] > highest {
					highest = previous[neighbor]
				}
			}
			if current := float64(previous[vertex]); highest > previous[vertex] {
				weights[vertex] = float32(Clamp01(current + (float64(highest)-current)*strength))
			}
		}
	}
}

// ShrinkSelectionWeights lowers each selected weight toward its weakest
// neighbor; an isolated vertex or one next to an out-of-range neighbor shrinks
// toward zero.
func ShrinkSelectionWeights(weights []float32, adjacency *VertexAdjacency, amount float64, iterations int) {
	strength := Clamp01(amount)
	vertexCount := min(len(weights), adjacency.VertexCount())
	if strength == 0 || vertexCount == 0 {
		return
	}

	previous := make([]float32, len(weights))
	for range max(0, iterations) {
		copy(previous, weights)
		for vertex := range vertexCount {
			if previous[vertex] == 0 {
				continue
			}
			lowest := previous[vertex]
			neighbors := adjacency.NeighborsOf(vertex)
			if len(neighbors) == 0 {
				lowest = 0
			}
			for _, neighbor := range neighbors {
				if int(neighbor) >= len(weights) {
					lowest = 0
					break
				}
				if previous[neighbor] < lowest {
					lowest = previous[neighbor]
				}
			}
			if current := float64(previous[vertex]); lowest < previous[vertex] {
				weights[vertex] = float32(Clamp01(current - (current-float64(lowest))*strength))
			}
		}
	}
}

// BuildConnectedComponents labels each vertex with the index of its connected
// component, numbered in order of the lowest vertex.
func BuildConnectedComponents(vertexCount int, adjacency *VertexAdjacency) []uint32 {
	componentIDs := make([]uint32, max(0, vertexCount))
	count := min(vertexCount, adjacency.VertexCount())
	if count <= 0 {
		return componentIDs
	}

	visited := make([]bool, count)
	var queue []uint32
	next := uint32(0)
	for start := range count {
		if visited[start] {
			continue
		}
		visited[start] = true
		componentIDs[start] = next
		queue = append(queue[:0], uint32(start))
		for head := 0; head < len(queue); head++ {
			for _, neighbor := range adjacency.NeighborsOf(int(queue[head])) {
				if int(neighbor) >= count || visited[neighbor] {
					continue
				}
				visited[neighbor] = true
				componentIDs[neighbor] = next
				queue = append(queue, neighbor)
			}
		}
		next++
	}
	return componentIDs
}

// ComponentMode decides how a component pick changes the selection.
type ComponentMode int

const (
	ComponentAdd ComponentMode = iota
	ComponentSubtract
	ComponentReplace
)

// SelectConnectedComponent sets the weights of one component and returns the
// vertices that changed. Add raises the component to weightValue, subtract
// clears it, and replace selects only the component.
func SelectConnectedComponent(
	weights []float32,
	componentIDs []uint32,
	target uint32,
	mode ComponentMode,
	weightValue float64,
) []uint32 {
	value := Clamp01(weightValue)
	vertexCount := min(len(weights), len(componentIDs))

	var changed []uint32
	for i := range vertexCount {
		isTarget := componentIDs[i] == target
		current := float64(weights[i])
		next := current
		switch mode {
		case ComponentReplace:
			next = 0
			if isTarget {
				next = value
			}
		case ComponentSubtract:
			if isTarget {
				next = 0
			}
		default:
			if isTarget {
				next = max(current, value)
			}
		}
		if next != current {
			weights[i] = float32(next)
			changed = append(changed, uint32(i))
		}
	}
	return changed
}
