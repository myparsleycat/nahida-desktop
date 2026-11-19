package modelviewer

import (
	"fmt"
	"math"
	"slices"
)

// Some clips move a prop metres away from its rest offset for their whole length and stretch the joints that join it
// to the body, as Burnice's Walk does with her guns and hoses. The thresholds below come from the hosted GIMI, SRMI,
// ZZMI and EFMI motions: in Burnice's Walk the guns stay 3.5 m from their rest offset and 2.7 m from the body, and
// the hoses keep a scale of 24 to 28. The weapons SRMI battle stances move travel under 2 m, the props that stay 3 m
// from their rest offset but lie 1.0 to 1.8 m from the body (Yanqing, Himeko) stay shown, and no other joint keeps a
// scale of 20 for a whole clip. The props found this way carry at most 20 joints, under a tenth of the skin joints,
// while Jing Yuan's Root_M_Spt_JNT, which his battle stance keeps 5 m from its rest offset, carries a fifth.
const (
	// motionStowDistance, in metres, is how far from its rest offset a joint's translation must stay on every frame.
	motionStowDistance = 3.0
	// motionStowScale is the scale a joint must keep on some axis on every frame.
	motionStowScale = 20.0
	// motionStowClearance, in metres, is how far a joint put away by its translation must also stay from every
	// joint left in place on every frame, so a prop the hand still holds is not put away.
	motionStowClearance = 2.0
	// motionStowShare is the largest share of the skin joints the subtree of a joint put away may hold.
	motionStowShare = 0.125
)

// StowedNodes reports, by node, which nodes clip puts out of sight, or returns nil when it puts none away. A clip
// puts away a skin joint it keeps far from its rest offset or stretched for its whole length, together with the
// nodes below it. Joints most clips move that way, and joints carrying more than motionStowShare of the skin joints,
// are never put away.
func (m *CharacterMotion) StowedNodes(clip int) []bool {
	if clip < 0 || clip >= len(m.Clips) {
		return nil
	}
	return m.Clips[clip].stowed
}

// markStowed records the nodes each clip puts out of sight (see StowedNodes).
func (m *CharacterMotion) markStowed() error {
	inSkin := make([]bool, len(m.nodes))
	joints := 0
	for _, skin := range m.skins {
		for _, node := range skin {
			if !inSkin[node] {
				inSkin[node] = true
				joints++
			}
		}
	}
	// carried counts the skin joints in each node's subtree.
	carried := make([]int, len(m.nodes))
	for node, ok := range inSkin {
		if !ok {
			continue
		}
		for ancestor := node; ancestor >= 0; ancestor = m.nodes[ancestor].parent {
			carried[ancestor]++
		}
	}

	eligible := func(node int) bool { return float64(carried[node]) <= motionStowShare*float64(joints) }
	pose := m.NewPose()
	put := make([][]bool, len(m.Clips))
	counts := make([]int, len(m.nodes))
	for c := range m.Clips {
		var err error
		if put[c], err = m.stowedJoints(pose, c, inSkin, eligible); err != nil {
			return fmt.Errorf("clip %s: %w", m.Clips[c].Name, err)
		}
		for node, ok := range put[c] {
			if ok {
				counts[node]++
			}
		}
	}

	for c, marked := range put {
		if marked == nil {
			continue
		}
		// Joints most clips move this way stay shown.
		stowed := m.belowAny(func(node int) bool { return marked[node] && counts[node]*2 <= len(m.Clips) })
		if slices.Contains(stowed, true) {
			m.Clips[c].stowed = stowed
		}
	}
	return nil
}

// stowedJoints returns, by node, the skin joints clip puts away, those eligible accepts, or nil for none. pose
// evaluates the clip's frames when a joint put away by its translation needs its clearance checked.
func (m *CharacterMotion) stowedJoints(
	pose *CharacterMotionPose,
	clip int,
	inSkin []bool,
	eligible func(node int) bool,
) ([]bool, error) {
	put := make([]bool, len(m.nodes))
	// translated marks the joints put away by their translation alone, which must also clear the body.
	translated := make([]bool, len(m.nodes))
	for _, channel := range m.Clips[clip].channels {
		node := channel.node
		if !inSkin[node] || !eligible(node) {
			continue
		}
		switch {
		case channel.path == motionScale && motionScaleStowed(channel.values):
			put[node], translated[node] = true, false
		case channel.path == motionTranslation && !put[node] && m.translationStowed(node, channel.values):
			put[node], translated[node] = true, true
		}
	}
	if !slices.Contains(put, true) {
		return nil, nil
	}
	if !slices.Contains(translated, true) {
		return put, nil
	}

	// The body is every skin joint outside the subtrees of the joints put away. A joint that comes too close to it
	// joins it with its subtree, and the joints still put away are checked again against the larger body.
	clearance := m.fromMetres(motionStowClearance)
	bones := make([][12]float32, len(m.nodes))
	for joined := true; joined && slices.Contains(translated, true); {
		joined = false
		inPut := m.belowAny(func(node int) bool { return put[node] })
		var body []int
		for node, ok := range inSkin {
			if ok && !inPut[node] {
				body = append(body, node)
			}
		}
		for frame := range m.Clips[clip].Frames {
			if err := pose.Evaluate(clip, frame, bones); err != nil {
				return nil, err
			}
			for node, ok := range translated {
				if ok && m.nearestBody(pose.world, node, body) < clearance {
					put[node], translated[node], joined = false, false, true
				}
			}
			if joined {
				break
			}
		}
	}
	if !slices.Contains(put, true) {
		return nil, nil
	}
	return put, nil
}

// translationStowed reports whether a translation channel of node keeps the node at least motionStowDistance from
// its rest offset on every frame, measured in mod space.
func (m *CharacterMotion) translationStowed(node int, values []float32) bool {
	toMod := m.toMod
	if parent := m.nodes[node].parent; parent >= 0 {
		toMod = toMod.mul(m.nodes[parent].world)
	}
	limit := m.fromMetres(motionStowDistance)
	rest := m.nodes[node].rest.t
	for i := 0; i+3 <= len(values); i += 3 {
		d := [3]float64{float64(values[i]) - rest[0], float64(values[i+1]) - rest[1], float64(values[i+2]) - rest[2]}
		var moved [3]float64
		for row := range 3 {
			moved[row] = toMod[row*4]*d[0] + toMod[row*4+1]*d[1] + toMod[row*4+2]*d[2]
		}
		if !(math.Sqrt(moved[0]*moved[0]+moved[1]*moved[1]+moved[2]*moved[2]) >= limit) {
			return false
		}
	}
	return len(values) >= 3
}

// motionScaleStowed reports whether a scale channel keeps some axis at motionStowScale or more on every frame.
func motionScaleStowed(values []float32) bool {
	for i := 0; i+3 <= len(values); i += 3 {
		if !(max(values[i], values[i+1], values[i+2]) >= motionStowScale) {
			return false
		}
	}
	return len(values) >= 3
}

// nearestBody returns the distance, in mod units, from node to the nearest node of body, by world matrices.
func (m *CharacterMotion) nearestBody(worlds []motionAffine, node int, body []int) float64 {
	p := m.toMod.apply([3]float64{worlds[node][3], worlds[node][7], worlds[node][11]})
	nearest := math.Inf(1)
	for _, other := range body {
		q := m.toMod.apply([3]float64{worlds[other][3], worlds[other][7], worlds[other][11]})
		nearest = min(nearest, math.Sqrt(squaredDistance(p, q)))
	}
	return nearest
}

// belowAny marks, by node, the nodes that match or have an ancestor that matches.
func (m *CharacterMotion) belowAny(match func(node int) bool) []bool {
	out := make([]bool, len(m.nodes))
	for node := range m.nodes {
		for ancestor := node; ancestor >= 0; ancestor = m.nodes[ancestor].parent {
			if match(ancestor) {
				out[node] = true
				break
			}
		}
	}
	return out
}
