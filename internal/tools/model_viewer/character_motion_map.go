package modelviewer

import (
	"cmp"
	"context"
	"math"
	"slices"
)

const (
	// motionMapCell is the grid cell, in metres, that reference vertices are bucketed by. A mod vertex
	// votes only through a reference vertex in its own or a neighbouring cell.
	motionMapCell = 0.015
	// motionMapNeighbours is how many of the nearest reference vertices a mod vertex blends the bone weights of.
	motionMapNeighbours = 4
	// motionMapSoftening, in metres, keeps the inverse squared distance blend finite on a reference vertex.
	motionMapSoftening = 0.001
	// motionMapFalloff is the distance, in metres, from the nearest reference vertex at which a mod vertex counts
	// half.
	motionMapFalloff = 0.01
	// motionCoverageExponent is β in a group's similarity to a node: their overlap divided by the square root of the
	// group's squared weight norm and by the node's squared reference weight norm to the power β. Within a group, 0
	// ranks nodes by plain overlap, which favours a large neighbouring bone, and 0.5 by cosine similarity, which
	// favours a small bone that covers only part of a group wrapped around the body. Below 0.5 a similarity grows with
	// the weighted vertex count, so where groups of a palette contend for a node, the one with more evidence is served
	// first.
	motionCoverageExponent = 0.375
	// motionAlternateShare is the share of a group's best similarity that another node must reach to take the group
	// when a group of the same palette holds the best node.
	motionAlternateShare = 0.9
	// motionSkinCoverage is the share of a palette's vertices that must lie near a reference vertex for the palette
	// to be matched to a skin.
	motionSkinCoverage = 0.25
	// motionSkinShare is the share of a palette's nearest reference vertices that one skin must hold for the palette
	// to be matched to it.
	motionSkinShare = 0.75
	// motionMapGroups bounds the vertex groups a palette is mapped for.
	motionMapGroups = 4096
	// motionIdentityShare is the share of the votes that keeps a palette in a skin's joint order.
	motionIdentityShare = 0.5
	// motionReachLimit is how far, in metres, a group's strongly weighted vertices may lie from a joint's bone for the
	// group to follow that joint in a skin's joint order.
	motionReachLimit = 0.25
	// motionIdentityFarShare is the share of a palette's groups within a skin's joint count that may lie beyond
	// motionReachLimit of their joint for the palette to keep that skin's joint order. Hair longer than the source
	// model's still roots at its bones, but its strongly weighted vertices can hang far below the bone chain.
	motionIdentityFarShare = 0.25
	// motionSeamDistance is how close, in metres, vertices of two palettes lie to be one seam vertex.
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

// motionVotes is what the vertices of one palette tell about its groups, each vertex counting by how close it lies
// to the source model: the skin the palette indexes, or -1 when it is not known; per group and skeleton node the
// inner product of the group's weights and the node's reference weights over the vertices, per group the squared
// norm of its weights, per node the squared norm of its reference weights, the weighted sum of each group's vertex
// positions with the weight total last, and the node each group keeps from the skin's joint order, or -1. Votes
// gathered through every skin also hold, per skin, the closeness of the vertices whose nearest reference vertex is
// that skin's, and how many vertices lie near any reference vertex.
type motionVotes struct {
	skin     int
	scores   []float64
	norms    []float64
	coverage []float64
	centres  [][4]float64
	kept     []int32
	skinMass []float64
	near     int
}

// motionReferenceWeight is a skeleton node and its share of the bone weights blended at a point.
type motionReferenceWeight struct {
	node   int32
	weight float64
}

// MapPalettes returns, for each mod palette, the skeleton node each of its vertex groups follows, or -1 for a group
// no vertex uses.
//
// Each mod vertex takes the bone weights of the reference vertices at its position, blended from the nearest of
// them, and counts less the farther it lies from them. A palette whose votes mostly agree with a skin's own joint
// order, and whose groups lie on that skin's bones (see identitySkin), keeps that order, as GIMI mods share the game's
// palette. A mod palette indexes the bones of one game draw, so any other palette is matched to one skin of the
// source model (see paletteSkin) and votes again through only that skin's reference vertices, however close another
// skin's mesh lies, as long hair rests over a dress's back bow.
//
// A group takes the node most similar to it over the mod vertices (see motionCoverageExponent), so a large
// neighbouring bone that also covers the group's vertices does not outweigh the group's own bone. Within a palette
// each node goes to one group, the most similar pairs first. A group whose best node another group holds takes its
// next free node only when that node reaches motionAlternateShare of its best similarity, and otherwise shares the
// best node, as for a bone the source skeleton lacks. A group no reference vertex lies near takes the joint nearest
// the centre of its vertices, of its palette's skin when one is known.
//
// Groups of different palettes linked by seam vertices (see motionSeamLinks), as the upper and lower body of a ZZMI
// mod are at the waist, pool their votes and take one node, preferring the nodes that the skins of all of them hold.
// A pool holds at most one group of each palette.
func (m *CharacterMotion) MapPalettes(ctx context.Context, palettes []MotionPalette) ([][]int32, error) {
	out := make([][]int32, len(palettes))
	nodes := len(m.nodes)
	if nodes == 0 {
		return out, nil
	}

	grid := make(map[[3]int32][]int32, len(m.refs))
	for i, ref := range m.refs {
		key := motionCell(ref.position[0], ref.position[1], ref.position[2], m.fromMetres(motionMapCell))
		grid[key] = append(grid[key], int32(i))
	}
	bones := m.restBones()
	votes := make([]motionVotes, len(palettes))
	offsets := make([]int, len(palettes)+1)
	for i, palette := range palettes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		votes[i] = m.votePalette(grid, bones, palette)
		offsets[i+1] = offsets[i] + len(votes[i].centres)
	}
	links, err := motionSeamLinks(ctx, palettes, offsets, m.fromMetres(motionSeamDistance))
	if err != nil {
		return nil, err
	}
	// allowed marks the nodes each palette's skin holds, or is nil for a palette of no known skin.
	allowed := make([][]bool, len(palettes))
	for i, v := range votes {
		if v.skin < 0 {
			continue
		}
		allowed[i] = make([]bool, nodes)
		for _, node := range m.skins[v.skin] {
			allowed[i][node] = true
		}
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

	type candidate struct {
		root, node int
		similarity float64
	}
	var candidates []candidate
	chosen := make([]int32, all)
	bestNode := make([]int32, all)
	bestSimilarity := make([]float64, all)
	used := make([][]bool, len(palettes))
	for i := range used {
		used[i] = make([]bool, nodes)
	}
	claim := func(root int, node int32) {
		chosen[root] = node
		for _, group := range members[root] {
			used[paletteOf[group]][node] = true
		}
	}
	summed := make([]float64, nodes)
	coverage := make([]float64, nodes)
	for root, groups := range members {
		chosen[root], bestNode[root] = -1, -1
		if len(groups) == 0 {
			continue
		}
		clear(summed)
		clear(coverage)
		var centre [4]float64
		norm := 0.0
		kept, keptWeight := int32(-1), 0.0
		for _, group := range groups {
			v, local := &votes[paletteOf[group]], group-offsets[paletteOf[group]]
			for node, score := range v.scores[local*nodes : (local+1)*nodes] {
				summed[node] += score
			}
			// A pool holds one group of each palette, so each palette's coverage is added once.
			for node, value := range v.coverage {
				coverage[node] += value
			}
			norm += v.norms[local]
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
			claim(root, kept)
			continue
		}

		holds := func(node int) bool {
			return !slices.ContainsFunc(groups, func(group int) bool {
				skin := allowed[paletteOf[group]]
				return skin != nil && !skin[node]
			})
		}
		// A pool whose skins share none of its voted nodes keeps its votes rather than falling back to a joint.
		for _, held := range []bool{true, false} {
			for node, score := range summed {
				if score <= 0 || coverage[node] <= 0 || norm <= 0 || held && !holds(node) {
					continue
				}
				similarity := score / (math.Sqrt(norm) * math.Pow(coverage[node], motionCoverageExponent))
				candidates = append(candidates, candidate{root: root, node: node, similarity: similarity})
				if similarity > bestSimilarity[root] {
					bestNode[root], bestSimilarity[root] = int32(node), similarity
				}
			}
			if bestNode[root] >= 0 {
				break
			}
		}
		if bestNode[root] < 0 {
			x, y, z := centre[0]/centre[3], centre[1]/centre[3], centre[2]/centre[3]
			joint := m.nearestJoint(x, y, z, holds)
			if joint < 0 {
				joint = m.nearestJoint(x, y, z, nil)
			}
			chosen[root] = int32(joint)
		}
	}

	slices.SortFunc(candidates, func(a, b candidate) int {
		if c := cmp.Compare(b.similarity, a.similarity); c != 0 {
			return c
		}
		if c := cmp.Compare(a.root, b.root); c != 0 {
			return c
		}
		return cmp.Compare(a.node, b.node)
	})
	for _, c := range candidates {
		if chosen[c.root] >= 0 || c.similarity < motionAlternateShare*bestSimilarity[c.root] {
			continue
		}
		taken := slices.ContainsFunc(members[c.root], func(group int) bool { return used[paletteOf[group]][c.node] })
		if !taken {
			claim(c.root, int32(c.node))
		}
	}
	for root, node := range bestNode {
		if node >= 0 && chosen[root] < 0 {
			chosen[root] = node
		}
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

// votePalette gathers the votes of one palette's vertices through the reference vertices bucketed in grid. Votes
// through every skin's vertices that mostly agree with the joint order of a skin whose bones the groups lie on keep
// that order (see identitySkin); otherwise the palette votes again through only the vertices of its skin (see
// paletteSkin), when one is known.
func (m *CharacterMotion) votePalette(
	grid map[[3]int32][]int32,
	bones []motionBone,
	palette MotionPalette,
) motionVotes {
	vertices := min(len(palette.Positions)/3, len(palette.Blend)/8)
	groups := 0
	for v := range vertices {
		for k := range 4 {
			if palette.Blend[v*8+4+k] != 0 {
				groups = max(groups, int(palette.Blend[v*8+k])+1)
			}
		}
	}
	groups = min(groups, motionMapGroups)

	votes := m.gatherVotes(grid, palette, vertices, groups, -1)
	nodes := len(m.nodes)
	total := 0.0
	for _, score := range votes.scores {
		total += score
	}
	var agreeing []int
	for s, joints := range m.skins {
		if total == 0 {
			break
		}
		agreed := 0.0
		for group := range min(groups, len(joints)) {
			agreed += votes.scores[group*nodes+joints[group]]
		}
		if agreed >= total*motionIdentityShare {
			agreeing = append(agreeing, s)
		}
	}
	if s := m.identitySkin(&votes, bones, palette, vertices, agreeing); s >= 0 {
		joints := m.skins[s]
		votes.skin = s
		for group := range min(groups, len(joints)) {
			if votes.centres[group][3] > 0 {
				votes.kept[group] = int32(joints[group])
			}
		}
		return votes
	}

	if skin := votes.paletteSkin(vertices); skin >= 0 {
		return m.gatherVotes(grid, palette, vertices, groups, skin)
	}
	return votes
}

// identitySkin returns, of the skins whose joint order a palette's votes agree with, the one whose joints its groups
// lie on, or -1 when none fits. Votes alone do not show it: skins of one character often share joints at the same
// index, such as the head at index 0 of both the hair and the cloak skin, and a palette weighted mostly to that joint
// agrees with all of them, however far its other groups lie from those skins' other joints.
//
// A group lies on a joint when its strongly weighted vertices come within motionReachLimit of the joint's bone. A
// skin fits unless more than motionIdentityFarShare of the groups it indexes lie off their joints. Of the fitting
// skins, those leaving the fewest groups unexplained, off their joints or beyond the joint count, compete on the
// votes for their own joint over the groups where they hold different joints, and on a tie the one whose joint count
// is nearest the palette's group count wins.
func (m *CharacterMotion) identitySkin(
	votes *motionVotes,
	bones []motionBone,
	palette MotionPalette,
	vertices int,
	skins []int,
) int {
	if len(skins) == 0 {
		return -1
	}
	groups, nodes := len(votes.centres), len(m.nodes)
	strong := strongVertices(palette, vertices, groups)
	limit := m.fromMetres(motionReachLimit)
	reaches := map[int]float64{}
	off := func(group, node int) bool {
		key := group*nodes + node
		reach, ok := reaches[key]
		if !ok {
			reach = boneReach(&bones[node], palette.Positions, strong[group])
			reaches[key] = reach
		}
		return reach > limit
	}
	var fitting []int
	fewest := math.MaxInt
	for _, s := range skins {
		joints := m.skins[s]
		far, inside, outside := 0, 0, 0
		for group, centre := range votes.centres {
			switch {
			case centre[3] == 0:
			case group >= len(joints):
				outside++
			default:
				inside++
				if off(group, joints[group]) {
					far++
				}
			}
		}
		if inside == 0 || float64(far) > motionIdentityFarShare*float64(inside) {
			continue
		}
		switch unexplained := far + outside; {
		case unexplained < fewest:
			fitting, fewest = []int{s}, unexplained
		case unexplained == fewest:
			fitting = append(fitting, s)
		}
	}
	if len(fitting) == 0 {
		return -1
	}

	jointAt := func(skin, group int) int {
		if group < len(m.skins[skin]) {
			return m.skins[skin][group]
		}
		return -1
	}
	best, bestAgreed := fitting[0], -1.0
	for _, s := range fitting {
		agreed := 0.0
		for group := range min(groups, len(m.skins[s])) {
			joint := jointAt(s, group)
			if slices.ContainsFunc(fitting, func(other int) bool { return jointAt(other, group) != joint }) {
				agreed += votes.scores[group*nodes+joint]
			}
		}
		closer := absInt(len(m.skins[s])-groups) < absInt(len(m.skins[best])-groups)
		if agreed > bestAgreed || agreed == bestAgreed && closer {
			best, bestAgreed = s, agreed
		}
	}
	return best
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// gatherVotes adds up the votes of a palette's first vertices for its first groups through the reference vertices
// of skin, or of every skin when it is negative, each vertex weighted by its closeness to the nearest of them.
// Through every skin it also records, per skin, the closeness of the vertices whose nearest reference vertex is that
// skin's.
func (m *CharacterMotion) gatherVotes(
	grid map[[3]int32][]int32,
	palette MotionPalette,
	vertices, groups, skin int,
) motionVotes {
	positions, blend := palette.Positions, palette.Blend
	nodes := len(m.nodes)
	votes := motionVotes{
		skin:     skin,
		scores:   make([]float64, groups*nodes),
		norms:    make([]float64, groups),
		coverage: make([]float64, nodes),
		centres:  make([][4]float64, groups),
		kept:     make([]int32, groups),
	}
	if skin < 0 {
		votes.skinMass = make([]float64, len(m.skins))
	}
	for group := range groups {
		votes.kept[group] = -1
	}

	references := make([]motionReferenceWeight, 0, motionMapNeighbours*4)
	for v := range vertices {
		px, py, pz := positions[v*3], positions[v*3+1], positions[v*3+2]
		// A group repeated in a vertex's slots is one entry of the group's weight vector.
		var vertexGroups [4]int
		var vertexWeights [4]float64
		count := 0
		for k := range 4 {
			group, weight := int(blend[v*8+k]), float64(blend[v*8+4+k])/65535
			if weight == 0 || group >= groups {
				continue
			}
			if i := slices.Index(vertexGroups[:count], group); i >= 0 {
				vertexWeights[i] += weight
				continue
			}
			vertexGroups[count], vertexWeights[count] = group, weight
			count++
		}
		for i, group := range vertexGroups[:count] {
			weight := vertexWeights[i]
			votes.centres[group][0] += weight * float64(px)
			votes.centres[group][1] += weight * float64(py)
			votes.centres[group][2] += weight * float64(pz)
			votes.centres[group][3] += weight
		}

		var nearest float64
		var nearestSkin int
		references, nearest, nearestSkin = m.referenceWeights(grid, px, py, pz, votes.skin, references)
		if votes.skinMass != nil && nearestSkin >= 0 {
			votes.near++
			if nearestSkin < len(votes.skinMass) {
				votes.skinMass[nearestSkin] += m.closeness(nearest)
			}
		}
		if len(references) == 0 || count == 0 {
			continue
		}
		confidence := m.closeness(nearest)
		for _, ref := range references {
			votes.coverage[ref.node] += confidence * ref.weight * ref.weight
		}
		for i, group := range vertexGroups[:count] {
			weight := vertexWeights[i]
			votes.norms[group] += confidence * weight * weight
			for _, ref := range references {
				votes.scores[group*nodes+int(ref.node)] += confidence * weight * ref.weight
			}
		}
	}

	return votes
}

// closeness is how much a mod vertex counts at a distance, in mod units, from its nearest reference vertex.
func (m *CharacterMotion) closeness(distance float64) float64 {
	falloff := m.fromMetres(motionMapFalloff)
	return 1 / (1 + (distance/falloff)*(distance/falloff))
}

// paletteSkin returns, from votes gathered through every skin over vertices, the skin whose reference vertices lie
// nearest most of them, each counting by its closeness, or -1 when fewer than motionSkinCoverage of the vertices lie
// near any reference vertex or no skin holds motionSkinShare of the closeness.
func (v *motionVotes) paletteSkin(vertices int) int {
	if v.skinMass == nil || float64(v.near) < motionSkinCoverage*float64(vertices) {
		return -1
	}

	skin, total := 0, 0.0
	mass := v.skinMass
	for s, value := range mass {
		total += value
		if value > mass[skin] {
			skin = s
		}
	}
	if total == 0 || mass[skin] < motionSkinShare*total {
		return -1
	}
	return skin
}

// motionSeamLinks returns the pairs of groups, numbered across palettes by offsets, that skin a seam, strongest
// first: vertices of two palettes within seamDistance, in mod units, of one position whose weights pair one to one,
// at motionSeamMinPositions distinct positions or more, or at one vertex whose two or more weights all pair. A group links to the group of another palette it shares the most seam weight with, when that
// group links back.
func motionSeamLinks(
	ctx context.Context,
	palettes []MotionPalette,
	offsets []int,
	seamDistance float64,
) ([][2]int, error) {
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
			cell := motionCell(x, y, z, seamDistance)
			for dx := int32(-1); dx <= 1; dx++ {
				for dy := int32(-1); dy <= 1; dy++ {
					for dz := int32(-1); dz <= 1; dz++ {
						for _, other := range cells[[3]int32{cell[0] + dx, cell[1] + dy, cell[2] + dz}] {
							o := palettes[other.palette]
							ex := o.Positions[other.vertex*3] - x
							ey := o.Positions[other.vertex*3+1] - y
							ez := o.Positions[other.vertex*3+2] - z
							if float64(ex*ex+ey*ey+ez*ez) > seamDistance*seamDistance {
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
				seamDistance)
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

// eachReference calls yield with every reference vertex bucketed in grid in a point's cell or a neighbouring one,
// and its squared distance to the point.
func (m *CharacterMotion) eachReference(
	grid map[[3]int32][]int32,
	x, y, z float32,
	yield func(index int32, distance float32),
) {
	cell := motionCell(x, y, z, m.fromMetres(motionMapCell))
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				for _, index := range grid[[3]int32{cell[0] + dx, cell[1] + dy, cell[2] + dz}] {
					p := m.refs[index].position
					ex, ey, ez := p[0]-x, p[1]-y, p[2]-z
					yield(index, ex*ex+ey*ey+ez*ez)
				}
			}
		}
	}
}

// referenceWeights blends, into out, the bone weights of up to motionMapNeighbours reference vertices of skin, or of
// any skin when it is negative, nearest a point in its grid neighbourhood, by inverse squared distance. It returns
// the blended node weights, which sum to one, or none when no weighted one lies in the neighbourhood, and the
// distance to the nearest of those vertices and its skin, or -1 for the skin when none lies there.
func (m *CharacterMotion) referenceWeights(
	grid map[[3]int32][]int32,
	x, y, z float32,
	skin int,
	out []motionReferenceWeight,
) ([]motionReferenceWeight, float64, int) {
	type near struct {
		index    int32
		distance float32
	}
	var nearest [motionMapNeighbours]near
	count := 0
	m.eachReference(grid, x, y, z, func(index int32, distance float32) {
		if skin >= 0 && int(m.refs[index].skin) != skin ||
			count == len(nearest) && distance >= nearest[count-1].distance {
			return
		}
		i := min(count, len(nearest)-1)
		for ; i > 0 && nearest[i-1].distance > distance; i-- {
			nearest[i] = nearest[i-1]
		}
		nearest[i] = near{index: index, distance: distance}
		count = min(count+1, len(nearest))
	})

	out = out[:0]
	if count == 0 {
		return out, 0, -1
	}
	total := 0.0
	softening := m.fromMetres(motionMapSoftening)
	for _, n := range nearest[:count] {
		share := 1 / (float64(n.distance) + softening*softening)
		ref := m.refs[n.index]
		for k, node := range ref.nodes {
			if node < 0 || ref.weights[k] <= 0 {
				continue
			}
			weight := share * float64(ref.weights[k])
			total += weight
			if i := slices.IndexFunc(out, func(e motionReferenceWeight) bool { return e.node == node }); i >= 0 {
				out[i].weight += weight
				continue
			}
			out = append(out, motionReferenceWeight{node: node, weight: weight})
		}
	}
	for i := range out {
		out[i].weight /= total
	}
	return out, math.Sqrt(float64(nearest[0].distance)), int(m.refs[nearest[0].index].skin)
}

// nearestJoint returns the skin joint, of those holds accepts or of all when it is nil, whose rest position in mod
// space is nearest to a point, or -1 when holds accepts none.
func (m *CharacterMotion) nearestJoint(x, y, z float64, holds func(node int) bool) int {
	best, bestDistance := -1, math.Inf(1)
	for _, joints := range m.skins {
		for _, node := range joints {
			if holds != nil && !holds(node) {
				continue
			}
			p := m.restPosition(node)
			if distance := (p[0]-x)*(p[0]-x) + (p[1]-y)*(p[1]-y) + (p[2]-z)*(p[2]-z); distance < bestDistance {
				best, bestDistance = node, distance
			}
		}
	}
	return best
}

// restPosition returns a node's rest position in mod space.
func (m *CharacterMotion) restPosition(node int) [3]float64 {
	w := m.nodes[node].world
	return m.toMod.apply([3]float64{w[3], w[7], w[11]})
}

// motionBone is a node's rest position in mod space and the rest positions of its children, where its bone ends.
type motionBone struct {
	joint [3]float64
	ends  [][3]float64
}

// restBones returns the bone of every node.
func (m *CharacterMotion) restBones() []motionBone {
	bones := make([]motionBone, len(m.nodes))
	for i := range m.nodes {
		bones[i].joint = m.restPosition(i)
	}
	for i, node := range m.nodes {
		if node.parent >= 0 {
			bones[node.parent].ends = append(bones[node.parent].ends, bones[i].joint)
		}
	}
	return bones
}

// distance returns how far a point lies from the bone: from the segments between the joint and each of its children,
// or from the joint of a node without children.
func (b *motionBone) distance(p [3]float64) float64 {
	best := math.Sqrt(squaredDistance(p, b.joint))
	for _, end := range b.ends {
		var d, ap [3]float64
		length := 0.0
		for k := range 3 {
			d[k], ap[k] = end[k]-b.joint[k], p[k]-b.joint[k]
			length += d[k] * d[k]
		}
		if length == 0 {
			continue
		}
		t := min(max((ap[0]*d[0]+ap[1]*d[1]+ap[2]*d[2])/length, 0), 1)
		closest := [3]float64{b.joint[0] + t*d[0], b.joint[1] + t*d[1], b.joint[2] + t*d[2]}
		best = min(best, math.Sqrt(squaredDistance(p, closest)))
	}
	return best
}

func squaredDistance(a, b [3]float64) float64 {
	return (a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2])
}

// strongVertices returns, per group of a palette's first groups, the vertices that weight it at least half as much
// as its heaviest vertex does.
func strongVertices(palette MotionPalette, vertices, groups int) [][]int32 {
	heaviest := make([]uint16, groups)
	for v := range vertices {
		for k := range 4 {
			if group := int(palette.Blend[v*8+k]); group < groups {
				heaviest[group] = max(heaviest[group], palette.Blend[v*8+4+k])
			}
		}
	}
	strong := make([][]int32, groups)
	for v := range vertices {
		for k := range 4 {
			group, weight := int(palette.Blend[v*8+k]), palette.Blend[v*8+4+k]
			if group >= groups || weight == 0 || 2*int(weight) < int(heaviest[group]) {
				continue
			}
			if n := len(strong[group]); n == 0 || strong[group][n-1] != int32(v) {
				strong[group] = append(strong[group], int32(v))
			}
		}
	}
	return strong
}

// boneReach returns how far, in mod units, the nearest of a group's strong vertices lies from a node's bone, or
// +Inf for a group without vertices.
func boneReach(bone *motionBone, positions []float32, strong []int32) float64 {
	best := math.Inf(1)
	for _, v := range strong {
		p := [3]float64{float64(positions[v*3]), float64(positions[v*3+1]), float64(positions[v*3+2])}
		best = min(best, bone.distance(p))
	}
	return best
}
