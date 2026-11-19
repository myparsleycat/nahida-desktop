package modelviewer

import (
	"slices"
	"testing"
)

func TestCharacterMotion_StowedNodes(t *testing.T) {
	const (
		root = iota
		pelvis
		hand
		gun
		barrel
		hose
		sword
		tail
		figure
	)
	at := func(x, y, z float64) motionTRS {
		return motionTRS{t: [3]float64{x, y, z}, r: [4]float64{0, 0, 0, 1}, s: [3]float64{1, 1, 1}}
	}
	move := func(node int, x, y, z float32) motionChannel {
		return motionChannel{node: node, path: motionTranslation, values: []float32{x, y, z}}
	}
	// The tail leaves its rest offset in every clip, as a joint whose rest the game's poses never use.
	tailAway := move(tail, 0, 1, -3.5)
	m := &CharacterMotion{
		nodes: []motionNode{
			root:   {parent: -1, rest: at(0, 0, 0)},
			pelvis: {parent: root, rest: at(0, 1, 0)},
			hand:   {parent: pelvis, rest: at(0.5, 0, 0)},
			gun:    {parent: hand, rest: at(0.05, 0, 0)},
			barrel: {parent: gun, rest: at(0.1, 0, 0)},
			hose:   {parent: hand, rest: at(0.02, 0, 0)},
			sword:  {parent: root, rest: at(0.5, 1, 0)},
			tail:   {parent: root, rest: at(0, 1, -0.3)},
			figure: {parent: root, rest: at(0, 1, -1)},
		},
		Clips: []CharacterMotionClip{
			// A second figure with a skeleton of its own steps back with the gun.
			{Name: "put away", Frames: 1, channels: []motionChannel{
				move(gun, 3.5, 0, 0),
				{node: hose, path: motionScale, values: []float32{25, 1, 1}},
				move(figure, 0, 1, -6),
				tailAway,
			}},
			// The body travels 5 m and the sword, parented to the root, follows the hand.
			{Name: "travel", Frames: 1, channels: []motionChannel{
				move(pelvis, 0, 1, 5), move(sword, 0.5, 1, 5), tailAway,
			}},
			{Name: "carry", Frames: 1, channels: []motionChannel{move(gun, 2, 0, 0), tailAway}},
			{Name: "idle", Frames: 1, channels: []motionChannel{tailAway}},
		},
	}
	skin := []int{pelvis, hand, gun, barrel, hose, sword, tail, figure}
	grow := func(parent, count int) {
		for range count {
			skin = append(skin, len(m.nodes))
			m.nodes = append(m.nodes, motionNode{parent: parent, rest: at(0, 0.01, 0)})
		}
	}
	grow(figure, 5)
	grow(pelvis, 30)
	m.skins = [][]int{skin}
	locals := make([]motionTRS, len(m.nodes))
	for i := range m.nodes {
		locals[i] = m.nodes[i].rest
	}
	worlds, err := m.worlds(locals, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.nodes {
		m.nodes[i].world = worlds[i]
	}
	if err := m.bindSkeleton(motionIdentity()); err != nil {
		t.Fatal(err)
	}

	want := map[string][]int{"put away": {gun, barrel, hose}}
	clips := m.Clips
	for _, tt := range []struct {
		name  string
		clips []int
	}{
		{name: "all clips", clips: []int{0, 1, 2, 3}},
		// A joint half the clips put away is not one most clips move that way.
		{name: "half the clips", clips: []int{0, 3}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m.Clips = nil
			for _, c := range tt.clips {
				clip := clips[c]
				clip.stowed = nil
				m.Clips = append(m.Clips, clip)
			}
			if err := m.markStowed(); err != nil {
				t.Fatal(err)
			}
			for c, clip := range m.Clips {
				var got []int
				for node, hidden := range m.StowedNodes(c) {
					if hidden {
						got = append(got, node)
					}
				}
				if !slices.Equal(got, want[clip.Name]) {
					t.Errorf("clip %q stows %v, want %v", clip.Name, got, want[clip.Name])
				}
			}
		})
	}
}
