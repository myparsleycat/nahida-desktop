package modelviewer

import (
	"cmp"
	"context"
	"math"
	"slices"
)

const (
	// motionMapCell is the grid cell, in mod units (meters), that reference vertices are bucketed by. A mod vertex
	// votes only through a reference vertex in its own or a neighbouring cell.
	motionMapCell = 0.015
	// motionMapGroups bounds the vertex groups a palette is mapped for.
	motionMapGroups = 4096
	// motionIdentityShare is the share of the votes that keeps a palette in a skin's joint order.
	motionIdentityShare = 0.5
	// motionSeamDistance is how close, in mod units, vertices of two palettes lie to be one seam vertex.
	motionSeamDistance = 0.0001
	// motionSeamWeight is how far apart, as a fraction, two bone weights of a seam vertex may be to pair their groups.
	motionSeamWeight = 0.002
	// motionSeamMinPositions is at how many distinct positions two groups must share seam vertices to be linked,
	// unless one of those vertices matches two or more weights all at once, so an accessory weighted to one bone that
	// touches the body at a point keeps its own bone.
	motionSeamMinPositions = 3
)

// MotionPalette is the bone weights of the meshes that share one mod palette: three floats per vertex in mod
// space, and per vertex four groups, then their four weights as UNORM16.
type MotionPalette struct {
	Positions []float32
	Blend     []uint16
}

// motionVotes is what the vertices of one palette tell about its groups: per group and skeleton node the votes for
// that node, the weighted sum of each group's vertex positions with the weight total last, and the node each group
// keeps from a skin's joint order, or -1.
type motionVotes struct {
	scores  []float64
	centres [][4]float64
	kept    []int32
}

// MapPalettes returns, for each mod palette, the skeleton node each of its vertex groups follows, or -1 for a group
// no vertex uses.
//
// Each mod vertex votes through the nearest reference vertex of the source model: every pair of a mod group and
// a reference bone gains the product of their weights. A group takes the bone with the most votes, or the joint
// nearest the centre of its vertices when no reference vertex lies close. A palette whose votes mostly agree with
// a skin's own joint order keeps that order, as GIMI mods share the game's palette.
//
// Groups of different palettes linked by seam vertices (see motionSeamLinks), as the upper and lower body of a ZZMI
// mod are at the waist, pool their votes and take one node. A pool holds at most one group of each palette.
func (m *CharacterMotion) MapPalettes(ctx context.Context, palettes []MotionPalette) ([][]int32, error) {
	out := make([][]int32, len(palettes))
	nodes := len(m.nodes)
	if nodes == 0 {
		return out, nil
	}

	grid := make(map[[3]int32][]int32, len(m.refs))
	for i, ref := range m.refs {
		key := motionCell(ref.position[0], ref.position[1], ref.position[2], motionMapCell)
		grid[key] = append(grid[key], int32(i))
	}
	votes := make([]motionVotes, len(palettes))
	offsets := make([]int, len(palettes)+1)
	for i, palette := range palettes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		votes[i] = m.votePalette(grid, palette)
		offsets[i+1] = offsets[i] + len(votes[i].centres)
	}
	links, err := motionSeamLinks(ctx, palettes, offsets)
	if err != nil {
		return nil, err
	}

	all := offsets[len(palettes)]
	paletteOf := make([]int, all)
	for i := range palettes {
		for group := offsets[i]; group < offsets[i+1]; group++ {
			paletteOf[group] = i
		}
	}
	classOf := make([]int, all)
	members := make([][]int, all)
	for group := range all {
		classOf[group], members[group] = group, []int{group}
	}
	for _, link := range links {
		a, b := classOf[link[0]], classOf[link[1]]
		if a == b || slices.ContainsFunc(members[a], func(x int) bool {
			return slices.ContainsFunc(members[b], func(y int) bool { return paletteOf[x] == paletteOf[y] })
		}) {
			continue
		}
		root, child := min(a, b), max(a, b)
		for _, group := range members[child] {
			classOf[group] = root
		}
		members[root], members[child] = append(members[root], members[child]...), nil
	}

	chosen := make([]int32, all)
	summed := make([]float64, nodes)
	for root, groups := range members {
		chosen[root] = -1
		if len(groups) == 0 {
			continue
		}
		clear(summed)
		var centre [4]float64
		kept, keptWeight := int32(-1), 0.0
		for _, group := range groups {
			v, local := &votes[paletteOf[group]], group-offsets[paletteOf[group]]
			for node, score := range v.scores[local*nodes : (local+1)*nodes] {
				summed[node] += score
			}
			for k := range 4 {
				centre[k] += v.centres[local][k]
			}
			if v.kept[local] >= 0 && v.centres[local][3] > keptWeight {
				kept, keptWeight = v.kept[local], v.centres[local][3]
			}
		}
		if centre[3] == 0 {
			continue
		}
		if kept >= 0 {
			chosen[root] = kept
			continue
		}
		best, bestScore := -1, 0.0
		for node, score := range summed {
			if score > bestScore {
				best, bestScore = node, score
			}
		}
		if best < 0 {
			best = m.nearestJoint(centre[0]/centre[3], centre[1]/centre[3], centre[2]/centre[3])
		}
		chosen[root] = int32(best)
	}
	for i, v := range votes {
		out[i] = make([]int32, len(v.centres))
		for group, centre := range v.centres {
			out[i][group] = -1
			if centre[3] > 0 {
				out[i][group] = chosen[classOf[offsets[i]+group]]
			}
		}
	}
	return out, nil
}

// votePalette gathers the votes of one palette's vertices through the reference vertices bucketed in grid.
func (m *CharacterMotion) votePalette(grid map[[3]int32][]int32, palette MotionPalette) motionVotes {
	positions, blend := palette.Positions, palette.Blend
	vertices := min(len(positions)/3, len(blend)/8)
	groups := 0
	for v := range vertices {
		for k := range 4 {
			if blend[v*8+4+k] != 0 {
				groups = max(groups, int(blend[v*8+k])+1)
			}
		}
	}
	groups = min(groups, motionMapGroups)
	nodes := len(m.nodes)
	votes := motionVotes{
		scores: make([]float64, groups*nodes), centres: make([][4]float64, groups), kept: make([]int32, groups),
	}
	for group := range groups {
		votes.kept[group] = -1
	}

	total := 0.0
	for v := range vertices {
		px, py, pz := positions[v*3], positions[v*3+1], positions[v*3+2]
		for k := range 4 {
			if group, weight := int(blend[v*8+k]), float64(blend[v*8+4+k])/65535; weight > 0 && group < groups {
				votes.centres[group][0] += weight * float64(px)
				votes.centres[group][1] += weight * float64(py)
				votes.centres[group][2] += weight * float64(pz)
				votes.centres[group][3] += weight
			}
		}
		ref := m.nearestReference(grid, px, py, pz)
		if ref < 0 {
			continue
		}
		reference := m.refs[ref]
		for k := range 4 {
			group, weight := int(blend[v*8+k]), float64(blend[v*8+4+k])/65535
			if weight == 0 || group >= groups {
				continue
			}
			for r, node := range reference.nodes {
				if node >= 0 && reference.weights[r] > 0 {
					vote := weight * float64(reference.weights[r])
					votes.scores[group*nodes+int(node)] += vote
					total += vote
				}
			}
		}
	}

	if total == 0 {
		return votes
	}
	for _, joints := range m.skins {
		agreed := 0.0
		for group := range min(groups, len(joints)) {
			agreed += votes.scores[group*nodes+joints[group]]
		}
		if agreed >= total*motionIdentityShare {
			for group := range min(groups, len(joints)) {
				if votes.centres[group][3] > 0 {
					votes.kept[group] = int32(joints[group])
				}
			}
			break
		}
	}
	return votes
}

// motionSeamLinks returns the pairs of groups, numbered across palettes by offsets, that skin a seam, strongest
// first: vertices of two palettes at one position whose weights pair one to one, at motionSeamMinPositions distinct
// positions or more, or at one vertex whose two or more weights all pair. A group links to the group of another palette it shares the most seam weight with, when that
// group links back.
func motionSeamLinks(ctx context.Context, palettes []MotionPalette, offsets []int) ([][2]int, error) {
	if len(palettes) < 2 {
		return nil, nil
	}

	type seamVertex struct{ palette, vertex int }
	type seamPair struct{ a, b, paletteA, paletteB int }
	type seamSupport struct {
		weight    float64
		positions map[[3]int32]struct{}
		whole     bool
	}
	cells := map[[3]int32][]seamVertex{}
	shared := map[seamPair]seamSupport{}
	for p, palette := range palettes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unique := uniqueSeamVertices(palette)
		queries := unique
		if p == 0 {
			queries = nil
		}
		for _, v := range queries {
			x, y, z := palette.Positions[v*3], palette.Positions[v*3+1], palette.Positions[v*3+2]
			cell := motionCell(x, y, z, motionSeamDistance)
			for dx := int32(-1); dx <= 1; dx++ {
				for dy := int32(-1); dy <= 1; dy++ {
					for dz := int32(-1); dz <= 1; dz++ {
						for _, other := range cells[[3]int32{cell[0] + dx, cell[1] + dy, cell[2] + dz}] {
							o := palettes[other.palette]
							ex := o.Positions[other.vertex*3] - x
							ey := o.Positions[other.vertex*3+1] - y
							ez := o.Positions[other.vertex*3+2] - z
							if ex*ex+ey*ey+ez*ez > motionSeamDistance*motionSeamDistance {
								continue
							}
							pairSeamGroups(palette.Blend[v*8:v*8+8], o.Blend[other.vertex*8:other.vertex*8+8],
								func(a, b uint16, weight float64, whole bool) {
									if offsets[p]+int(a) >= offsets[p+1] ||
										offsets[other.palette]+int(b) >= offsets[other.palette+1] {
										return
									}
									pair := seamPair{
										offsets[p] + int(a),
										offsets[other.palette] + int(b),
										p,
										other.palette,
									}
									support := shared[pair]
									support.whole = support.whole || whole
									if _, ok := support.positions[cell]; !ok {
										if support.positions == nil {
											support.positions = map[[3]int32]struct{}{}
										}
										support.positions[cell] = struct{}{}
										support.weight += weight
									}
									shared[pair] = support
								})
						}
					}
				}
			}
		}
		if p == len(palettes)-1 {
			break
		}
		for _, v := range unique {
			cell := motionCell(palette.Positions[v*3], palette.Positions[v*3+1], palette.Positions[v*3+2],
				motionSeamDistance)
			cells[cell] = append(cells[cell], seamVertex{p, v})
		}
	}

	type best struct {
		group  int
		weight float64
	}
	bests := map[[2]int]best{}
	consider := func(group, otherPalette, other int, weight float64) {
		key := [2]int{group, otherPalette}
		if b, ok := bests[key]; !ok || weight > b.weight || weight == b.weight && other < b.group {
			bests[key] = best{other, weight}
		}
	}
	supported := func(support seamSupport) bool {
		return support.whole || len(support.positions) >= motionSeamMinPositions
	}
	for pair, support := range shared {
		if supported(support) {
			consider(pair.a, pair.paletteB, pair.b, support.weight)
			consider(pair.b, pair.paletteA, pair.a, support.weight)
		}
	}
	type link struct {
		groups [2]int
		weight float64
	}
	var links []link
	for pair, support := range shared {
		if supported(support) && bests[[2]int{pair.a, pair.paletteB}].group == pair.b &&
			bests[[2]int{pair.b, pair.paletteA}].group == pair.a {
			links = append(links, link{[2]int{pair.a, pair.b}, support.weight})
		}
	}
	slices.SortFunc(links, func(x, y link) int {
		if c := cmp.Compare(y.weight, x.weight); c != 0 {
			return c
		}
		if c := cmp.Compare(x.groups[0], y.groups[0]); c != 0 {
			return c
		}
		return cmp.Compare(x.groups[1], y.groups[1])
	})
	out := make([][2]int, len(links))
	for i, l := range links {
		out[i] = l.groups
	}
	return out, nil
}

// uniqueSeamVertices returns the vertices of a palette with a finite position, the first of each that repeat a
// position and its weights, as the meshes of a palette share buffer rows and split vertices at UV seams.
func uniqueSeamVertices(palette MotionPalette) []int {
	type key struct {
		position [3]float32
		blend    [8]uint16
	}
	seen := map[key]struct{}{}
	var unique []int
	for v := range min(len(palette.Positions)/3, len(palette.Blend)/8) {
		position := [3]float32(palette.Positions[v*3 : v*3+3])
		finite := true
		for _, c := range position {
			if math.IsNaN(float64(c)) || math.IsInf(float64(c), 0) {
				finite = false
			}
		}
		if !finite {
			continue
		}
		k := key{position, [8]uint16(palette.Blend[v*8 : v*8+8])}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		unique = append(unique, v)
	}
	return unique
}

// pairSeamGroups calls yield with each group of a and the group of b whose weight matches it, the smaller of the two
// weights, and whether a has two or more weights and every one of them paired. Weights within motionSeamWeight of zero count as absent. Both must have the same number of
// weights, and a weight that matches more than one weight on the other side pairs nothing.
func pairSeamGroups(a, b []uint16, yield func(groupA, groupB uint16, weight float64, whole bool)) {
	significant := func(weight uint16) bool { return float64(weight) > motionSeamWeight*65535 }
	count := func(blend []uint16) int {
		n := 0
		for k := range 4 {
			if significant(blend[4+k]) {
				n++
			}
		}
		return n
	}
	weights := count(a)
	if weights != count(b) {
		return
	}
	near := func(x, y uint16) bool { return math.Abs(float64(x)-float64(y)) <= motionSeamWeight*65535 }
	var pairs [4][2]int
	n := 0
	for i := range 4 {
		if !significant(a[4+i]) {
			continue
		}
		match := -1
		for j := range 4 {
			if !significant(b[4+j]) || !near(a[4+i], b[4+j]) {
				continue
			}
			if match >= 0 {
				match = -1
				break
			}
			match = j
		}
		if match < 0 {
			continue
		}
		unique := true
		for k := range 4 {
			if k != i && significant(a[4+k]) && near(a[4+k], b[4+match]) {
				unique = false
			}
		}
		if unique {
			pairs[n] = [2]int{i, match}
			n++
		}
	}

	whole := weights >= 2 && n == weights
	for _, pair := range pairs[:n] {
		i, j := pair[0], pair[1]
		yield(a[i], b[j], float64(min(a[4+i], b[4+j]))/65535, whole)
	}
}

func motionCell(x, y, z float32, size float64) [3]int32 {
	return [3]int32{
		int32(math.Floor(float64(x) / size)),
		int32(math.Floor(float64(y) / size)),
		int32(math.Floor(float64(z) / size)),
	}
}

func (m *CharacterMotion) nearestReference(grid map[[3]int32][]int32, x, y, z float32) int {
	cell := motionCell(x, y, z, motionMapCell)
	best, bestDistance := -1, float32(math.Inf(1))
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				for _, index := range grid[[3]int32{cell[0] + dx, cell[1] + dy, cell[2] + dz}] {
					p := m.refs[index].position
					ex, ey, ez := p[0]-x, p[1]-y, p[2]-z
					if distance := ex*ex + ey*ey + ez*ez; distance < bestDistance {
						best, bestDistance = int(index), distance
					}
				}
			}
		}
	}
	return best
}

// nearestJoint returns the skin joint whose rest position in mod space is nearest to a point.
func (m *CharacterMotion) nearestJoint(x, y, z float64) int {
	best, bestDistance := -1, math.Inf(1)
	for _, joints := range m.skins {
		for _, node := range joints {
			p := m.toMod.apply([3]float64{m.nodes[node].world[3], m.nodes[node].world[7], m.nodes[node].world[11]})
			if distance := (p[0]-x)*(p[0]-x) + (p[1]-y)*(p[1]-y) + (p[2]-z)*(p[2]-z); distance < bestDistance {
				best, bestDistance = node, distance
			}
		}
	}
	return best
}
