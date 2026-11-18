package modelviewer

import "math"

const (
	// motionMapCell is the grid cell, in mod units (meters), that reference vertices are bucketed by. A mod vertex
	// votes only through a reference vertex in its own or a neighbouring cell.
	motionMapCell = 0.015
	// motionMapGroups bounds the vertex groups a palette is mapped for.
	motionMapGroups = 4096
	// motionIdentityShare is the share of the votes that keeps a palette in a skin's joint order.
	motionIdentityShare = 0.5
)

// MapPalette returns the skeleton node each vertex group of a mod palette follows, or -1 for a group no vertex
// uses. positions holds three floats per vertex in mod space; blend holds per vertex four groups, then their
// four weights as UNORM16.
//
// Each mod vertex votes through the nearest reference vertex of the source model: every pair of a mod group and
// a reference bone gains the product of their weights. A group takes the bone with the most votes, or the joint
// nearest the centre of its vertices when no reference vertex lies close. A palette whose votes mostly agree with
// a skin's own joint order keeps that order, as GIMI mods share the game's palette.
func (m *CharacterMotion) MapPalette(positions []float32, blend []uint16) []int32 {
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
	if groups == 0 || nodes == 0 {
		return nil
	}

	grid := make(map[[3]int32][]int32, len(m.refs))
	for i, ref := range m.refs {
		key := motionCell(ref.position[0], ref.position[1], ref.position[2])
		grid[key] = append(grid[key], int32(i))
	}
	scores := make([]float64, groups*nodes)
	centres := make([][4]float64, groups)
	total := 0.0
	for v := range vertices {
		px, py, pz := positions[v*3], positions[v*3+1], positions[v*3+2]
		for k := range 4 {
			if group, weight := int(blend[v*8+k]), float64(blend[v*8+4+k])/65535; weight > 0 && group < groups {
				centres[group][0] += weight * float64(px)
				centres[group][1] += weight * float64(py)
				centres[group][2] += weight * float64(pz)
				centres[group][3] += weight
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
					scores[group*nodes+int(node)] += vote
					total += vote
				}
			}
		}
	}

	palette := make([]int32, groups)
	for group := range groups {
		palette[group] = -1
		if centres[group][3] == 0 {
			continue
		}
		best, bestScore := -1, 0.0
		for node := range nodes {
			if score := scores[group*nodes+node]; score > bestScore {
				best, bestScore = node, score
			}
		}
		if best < 0 {
			centre := centres[group]
			best = m.nearestJoint(centre[0]/centre[3], centre[1]/centre[3], centre[2]/centre[3])
		}
		palette[group] = int32(best)
	}
	if total == 0 {
		return palette
	}
	for _, joints := range m.skins {
		agreed := 0.0
		for group := range min(groups, len(joints)) {
			agreed += scores[group*nodes+joints[group]]
		}
		if agreed >= total*motionIdentityShare {
			for group := range min(groups, len(joints)) {
				if palette[group] >= 0 {
					palette[group] = int32(joints[group])
				}
			}
			break
		}
	}
	return palette
}

func motionCell(x, y, z float32) [3]int32 {
	return [3]int32{
		int32(math.Floor(float64(x) / motionMapCell)),
		int32(math.Floor(float64(y) / motionMapCell)),
		int32(math.Floor(float64(z) / motionMapCell)),
	}
}

func (m *CharacterMotion) nearestReference(grid map[[3]int32][]int32, x, y, z float32) int {
	cell := motionCell(x, y, z)
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
