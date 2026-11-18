package modelviewer

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mirroredTestMotion is a root with a left joint at mesh +X and a right joint at mesh -X, for a game whose mods
// mirror X; the clip moves the left joint 0.1 further along mesh +X.
func mirroredTestMotion(t *testing.T) *CharacterMotion {
	t.Helper()
	rest := func(x float64) motionTRS {
		return motionTRS{t: [3]float64{x, 0, 0}, r: [4]float64{0, 0, 0, 1}, s: [3]float64{1, 1, 1}}
	}
	m := &CharacterMotion{
		flipX: true,
		nodes: []motionNode{{parent: -1, rest: rest(0)}, {parent: 0, rest: rest(0.3)}, {parent: 0, rest: rest(-0.3)}},
		skins: [][]int{{1, 2}},
		Clips: []CharacterMotionClip{{
			Name: "reach", Frames: 1,
			channels: []motionChannel{{node: 1, path: motionTranslation, values: []float32{0.4, 0, 0}}},
		}},
	}
	locals := []motionTRS{m.nodes[0].rest, m.nodes[1].rest, m.nodes[2].rest}
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
	// The left joint's vertices sit at mod -X, the right joint's at mod +X.
	for _, x := range []float32{-0.3, 0.3} {
		node := int32(1)
		if x > 0 {
			node = 2
		}
		for _, y := range []float32{-0.01, 0, 0.01} {
			m.refs = append(m.refs, motionReference{
				position: [3]float32{x, y, 0}, nodes: [4]int32{node, -1, -1, -1}, weights: [4]float32{1},
			})
		}
	}
	return m
}

func TestPruneCharacterMotionCache(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) string {
		t.Helper()
		file := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return file
	}
	indexed := write("gimi/amber.new.glb.gz")
	otherCase := write("srmi/Kafka.New.glb.gz")
	stale := write("gimi/amber.old.glb.gz")
	partial := write("srmi/kafka.new.glb.gz.ntmp")
	nested := write("gimi/folder/file.bin")
	outside := write("file.txt")
	keep := []string{indexed, filepath.Join(dir, "srmi", "kafka.new.glb.gz")}

	if err := pruneCharacterMotionCache(dir, keep); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]bool{
		indexed: true, otherCase: true, nested: true, outside: true, stale: false, partial: false,
	} {
		if _, err := os.Stat(file); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", file, err == nil, want)
		}
	}
	if err := pruneCharacterMotionCache(filepath.Join(dir, "missing"), nil); err != nil {
		t.Errorf("missing cache: %v", err)
	}
}

func TestCharacterMotionPose_Evaluate(t *testing.T) {
	m := mirroredTestMotion(t)
	out := make([][12]float32, m.NodeCount())
	if err := m.NewPose().Evaluate(0, 0, out); err != nil {
		t.Fatal(err)
	}

	// Moving along mesh +X moves a mirrored mod vertex along mod -X.
	if got := out[1][3]; math.Abs(float64(got)+0.1) > 1e-6 {
		t.Errorf("left joint moves mod X by %v, want -0.1", got)
	}
	if got := out[2]; got != viewerMotionTestIdentity {
		t.Errorf("right joint matrix = %v, want identity", got)
	}
}

func TestCharacterMotion_MapPalette(t *testing.T) {
	tests := []struct {
		name string
		// xs is the mod X of each vertex group's vertices.
		xs   []float32
		want []int32
	}{
		{name: "skin order", xs: []float32{-0.3, 0.3}, want: []int32{1, 2}},
		{name: "swapped palette", xs: []float32{0.3, -0.3}, want: []int32{2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mirroredTestMotion(t)
			var positions []float32
			var blend []uint16
			for group, x := range tt.xs {
				for _, y := range []float32{-0.005, 0.005} {
					positions = append(positions, x, y, 0)
					blend = append(blend, uint16(group), 0, 0, 0, 65535, 0, 0, 0)
				}
			}
			if got := m.MapPalette(positions, blend); !slices.Equal(got, tt.want) {
				t.Errorf("MapPalette() = %v, want %v", got, tt.want)
			}
		})
	}
}

var viewerMotionTestIdentity = [12]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}
