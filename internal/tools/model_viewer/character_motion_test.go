package modelviewer

import (
	"context"
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

	if err := pruneCharacterCache(dir, keep); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]bool{
		indexed: true, otherCase: true, nested: true, outside: true, stale: false, partial: false,
	} {
		if _, err := os.Stat(file); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", file, err == nil, want)
		}
	}
	if err := pruneCharacterCache(filepath.Join(dir, "missing"), nil); err != nil {
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

func TestCharacterMotionPose_EvaluateBetween(t *testing.T) {
	// The second key is a quarter turn about Z stored with a negated sign, the same rotation on the far hemisphere.
	half := math.Sqrt(0.5)
	m := &CharacterMotion{
		nodes: []motionNode{{parent: -1, rest: motionTRS{r: [4]float64{0, 0, 0, 1}, s: [3]float64{1, 1, 1}}}},
		Clips: []CharacterMotionClip{{
			Name: "turn", Frames: 2,
			channels: []motionChannel{
				{node: 0, path: motionRotation, values: []float32{0, 0, 0, 1, 0, 0, float32(-half), float32(-half)}},
				{node: 0, path: motionTranslation, values: []float32{0, 0, 0, 1, 2, 3}},
			},
		}},
	}
	m.nodes[0].world = motionIdentity()
	if err := m.bindSkeleton(motionIdentity()); err != nil {
		t.Fatal(err)
	}
	pose := m.NewPose()
	evaluate := func(frame, next int, at float64) [12]float32 {
		t.Helper()
		out := make([][12]float32, 1)
		if err := pose.EvaluateBetween(0, frame, next, at, out); err != nil {
			t.Fatal(err)
		}
		return out[0]
	}
	near := func(got, want [12]float32) bool {
		for i := range got {
			if math.Abs(float64(got[i]-want[i])) > 1e-5 {
				return false
			}
		}
		return true
	}

	for _, frame := range []int{0, 1} {
		out := make([][12]float32, 1)
		if err := pose.Evaluate(0, frame, out); err != nil {
			t.Fatal(err)
		}
		if got := evaluate(0, 1, float64(frame)); !near(got, out[0]) {
			t.Errorf("EvaluateBetween at %d = %v, want Evaluate's %v", frame, got, out[0])
		}
	}
	c := float32(half)
	want := [12]float32{c, -c, 0, 0.5, c, c, 0, 1, 0, 0, 1, 1.5}
	if got := evaluate(0, 1, 0.5); !near(got, want) {
		t.Errorf("halfway = %v, want an eighth turn about Z %v", got, want)
	}
}

func TestCharacterMotionPose_EvaluateFrom(t *testing.T) {
	m := mirroredTestMotion(t)
	m.Clips = append(m.Clips, CharacterMotionClip{Name: "rest", Frames: 1})
	pose := m.NewPose()
	if pose.Hold() {
		t.Fatal("Hold before any evaluation = true")
	}
	evaluate := func(hold float64, clip int) [][12]float32 {
		t.Helper()
		out := make([][12]float32, m.NodeCount())
		if err := pose.EvaluateFrom(hold, clip, 0, 0, 0, out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	near := func(got, want [][12]float32) bool {
		for i := range got {
			for k := range got[i] {
				if math.Abs(float64(got[i][k]-want[i][k])) > 1e-6 {
					return false
				}
			}
		}
		return true
	}

	reach := evaluate(0, 0)
	if !pose.Hold() {
		t.Fatal("Hold after an evaluation = false")
	}
	if got := evaluate(1, 1); !near(got, reach) {
		t.Errorf("fully held = %v, want the held pose %v", got, reach)
	}
	rest := evaluate(0, 1)
	if got := rest[1][3]; got != 0 {
		t.Errorf("unheld rest moves the left joint by %v, want 0", got)
	}

	// A change of clip mid-fade holds the blended pose shown, not the clip it was fading from.
	halfway := evaluate(0.5, 1)
	if got := halfway[1][3]; math.Abs(float64(got)+0.05) > 1e-6 {
		t.Errorf("halfway moves the left joint by %v, want -0.05", got)
	}
	pose.Hold()
	if got := evaluate(1, 0); !near(got, halfway) {
		t.Errorf("held mid-fade = %v, want the blend shown %v", got, halfway)
	}
}

// A rotation key with no length keeps the rest rotation, at the start of a blend as in a whole frame.
func TestCharacterMotionPose_EvaluateBetweenZeroKey(t *testing.T) {
	half := math.Sqrt(0.5)
	rest := motionTRS{r: [4]float64{0, 0, half, half}, s: [3]float64{1, 1, 1}}
	m := &CharacterMotion{
		nodes: []motionNode{{parent: -1, rest: rest}},
		Clips: []CharacterMotionClip{{
			Name: "turn", Frames: 2,
			channels: []motionChannel{{node: 0, path: motionRotation, values: []float32{0, 0, 0, 0, 0, 0, 0, 1}}},
		}},
	}
	m.nodes[0].world = rest.affine()
	if err := m.bindSkeleton(motionIdentity()); err != nil {
		t.Fatal(err)
	}
	pose := m.NewPose()
	whole, blended := make([][12]float32, 1), make([][12]float32, 1)
	if err := pose.Evaluate(0, 0, whole); err != nil {
		t.Fatal(err)
	}
	if err := pose.EvaluateBetween(0, 0, 1, 0, blended); err != nil {
		t.Fatal(err)
	}
	for i := range whole[0] {
		if math.Abs(float64(whole[0][i]-blended[0][i])) > 1e-6 {
			t.Fatalf("EvaluateBetween at 0 = %v, want Evaluate's %v", blended[0], whole[0])
		}
	}
}

func TestCharacterMotion_MapPalettes(t *testing.T) {
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
			var palette MotionPalette
			for group, x := range tt.xs {
				addTestVertices(
					&palette,
					fullTestWeight(uint16(group)),
					[3]float32{x, -0.005, 0},
					[3]float32{x, 0.005, 0},
				)
			}
			got, err := m.MapPalettes(t.Context(), []MotionPalette{palette})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got[0], tt.want) {
				t.Errorf("MapPalettes() = %v, want %v", got[0], tt.want)
			}
		})
	}
}

// A palette weighted mostly to a joint that two skins hold at the same index keeps the order of the skin its other
// groups agree with, as a hair palette whose head group alone would also agree with the cloak skin.
func TestCharacterMotion_MapPalettesSharedJointOrder(t *testing.T) {
	m := mirroredTestMotion(t)
	m.skins = [][]int{{1, 0}, {1, 2}}

	var palette MotionPalette
	for _, y := range []float32{-0.01, -0.006, -0.002, 0.002, 0.006, 0.01} {
		addTestVertices(&palette, fullTestWeight(0), [3]float32{-0.3, y, 0})
	}
	addTestVertices(&palette, fullTestWeight(1), [3]float32{0.3, -0.005, 0}, [3]float32{0.3, 0.005, 0})
	got, err := m.MapPalettes(t.Context(), []MotionPalette{palette})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int32{1, 2}; !slices.Equal(got[0], want) {
		t.Errorf("MapPalettes() = %v, want %v", got[0], want)
	}
}

// A palette whose dominant group agrees with a skin's joint order does not keep that order when its other groups lie
// far from their joints in it, as hair bangs did with a cloak skin that also starts with the head.
func TestCharacterMotion_MapPalettesFarJointOrder(t *testing.T) {
	m := mirroredTestMotion(t)

	var palette MotionPalette
	for _, y := range []float32{-0.01, -0.006, -0.002, 0.002, 0.006, 0.01} {
		addTestVertices(&palette, fullTestWeight(0), [3]float32{-0.3, y, 0})
	}
	addTestVertices(&palette, fullTestWeight(1), [3]float32{-0.3, -0.004, 0}, [3]float32{-0.3, 0.004, 0})
	got, err := m.MapPalettes(t.Context(), []MotionPalette{palette})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int32{1, 1}; !slices.Equal(got[0], want) {
		t.Errorf("MapPalettes() = %v, want %v", got[0], want)
	}
}

// A palette lying mostly on one skin follows that skin's bones where another skin's mesh lies closer, as a dress's
// back bow does under long hair.
func TestCharacterMotion_MapPalettesOwnSkin(t *testing.T) {
	m := mirroredTestMotion(t)
	m.skins = [][]int{{1}, {2}}
	for i := range m.refs {
		if m.refs[i].nodes[0] == 2 {
			m.refs[i].skin = 1
		}
	}
	overlap := [3]float32{-0.3, 0.05, 0}
	m.refs = append(m.refs,
		motionReference{position: overlap, skin: 1, nodes: [4]int32{2, -1, -1, -1}, weights: [4]float32{1}},
		motionReference{
			position: [3]float32{overlap[0], overlap[1], 0.004},
			nodes:    [4]int32{1, -1, -1, -1},
			weights:  [4]float32{1},
		},
	)

	var palette MotionPalette
	addTestVertices(&palette, fullTestWeight(0), overlap)
	for _, y := range []float32{-0.01, -0.006, -0.002, 0.002, 0.006, 0.01} {
		addTestVertices(&palette, fullTestWeight(1), [3]float32{-0.3, y, 0})
	}
	got, err := m.MapPalettes(t.Context(), []MotionPalette{palette})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int32{1, 1}; !slices.Equal(got[0], want) {
		t.Errorf("MapPalettes() = %v, want %v", got[0], want)
	}
}

func TestCharacterMotion_MapPalettesSeam(t *testing.T) {
	left := [][3]float32{{-0.3, -0.005, 0}, {-0.3, 0.005, 0}}
	right := [][3]float32{{0.3, -0.005, 0}, {0.3, 0.005, 0}}
	half := func(a, b uint16) [8]uint16 { return [8]uint16{a, b, 0, 0, 32768, 32767} }
	tests := []struct {
		name     string
		palettes func() []MotionPalette
		want     [][]int32
	}{
		{
			name: "seam vertices link",
			palettes: func() []MotionPalette {
				var a, b MotionPalette
				addTestVertices(&a, fullTestWeight(0), left...)
				addTestVertices(&a, fullTestWeight(0), seamTestPoints(0, 3)...)
				addTestVertices(&b, fullTestWeight(3), seamTestPoints(0, 3)...)
				return []MotionPalette{a, b}
			},
			want: [][]int32{{1}, {-1, -1, -1, 1}},
		},
		{
			name: "one touching vertex does not link",
			palettes: func() []MotionPalette {
				var a, b MotionPalette
				addTestVertices(&a, fullTestWeight(0), left...)
				addTestVertices(&a, fullTestWeight(0), seamTestPoints(0, 1)...)
				addTestVertices(&b, fullTestWeight(3), seamTestPoints(0, 1)...)
				return []MotionPalette{a, b}
			},
			want: [][]int32{{1}, {-1, -1, -1, 2}},
		},
		{
			name: "ambiguous weights do not link",
			palettes: func() []MotionPalette {
				// Both groups of a follow the left joint, so any link would move a group of b off the right joint.
				var a, b MotionPalette
				addTestVertices(&a, fullTestWeight(0), left[0])
				addTestVertices(&a, fullTestWeight(1), left...)
				addTestVertices(&a, half(0, 1), seamTestPoints(0, 3)...)
				addTestVertices(&b, half(3, 4), seamTestPoints(0, 3)...)
				return []MotionPalette{a, b}
			},
			want: [][]int32{{1, 1}, {-1, -1, -1, 2, 2}},
		},
		{
			name: "one vertex matching two weights links",
			palettes: func() []MotionPalette {
				var a, b MotionPalette
				addTestVertices(&a, fullTestWeight(0), left...)
				addTestVertices(&a, fullTestWeight(1), right...)
				addTestVertices(&a, [8]uint16{0, 1, 0, 0, 45875, 19660}, seamTestPoints(0, 1)...)
				addTestVertices(&b, [8]uint16{3, 4, 0, 0, 45875, 19660}, seamTestPoints(0, 1)...)
				return []MotionPalette{a, b}
			},
			want: [][]int32{{1, 2}, {-1, -1, -1, 1, 2}},
		},
		{
			name: "near duplicates at one point do not link",
			palettes: func() []MotionPalette {
				point := [3]float32{0.05005, 0.00005, 0.00005}
				near := [3]float32{0.05005, 0.00005, 0.000051}
				var a, b MotionPalette
				addTestVertices(&a, fullTestWeight(0), left...)
				addTestVertices(&a, fullTestWeight(0), point, near)
				addTestVertices(&b, fullTestWeight(3), point, near)
				return []MotionPalette{a, b}
			},
			want: [][]int32{{1}, {-1, -1, -1, 2}},
		},
		{
			name: "a negligible extra weight still links",
			palettes: func() []MotionPalette {
				var a, b MotionPalette
				addTestVertices(&a, fullTestWeight(0), left...)
				addTestVertices(&a, fullTestWeight(0), seamTestPoints(0, 3)...)
				addTestVertices(&b, [8]uint16{3, 7, 0, 0, 65534, 1}, seamTestPoints(0, 3)...)
				return []MotionPalette{a, b}
			},
			want: [][]int32{{1}, {-1, -1, -1, 1, -1, -1, -1, 2}},
		},
		{
			name: "a chain through other palettes keeps one palette's groups apart",
			palettes: func() []MotionPalette {
				var a, b, c MotionPalette
				addTestVertices(&a, fullTestWeight(0), left...)
				addTestVertices(&a, fullTestWeight(0), seamTestPoints(0, 3)...)
				addTestVertices(&a, fullTestWeight(1), right...)
				addTestVertices(&a, fullTestWeight(1), seamTestPoints(0.1, 3)...)
				addTestVertices(&b, fullTestWeight(0), seamTestPoints(0, 3)...)
				addTestVertices(&b, fullTestWeight(0), seamTestPoints(0.2, 3)...)
				addTestVertices(&c, fullTestWeight(0), seamTestPoints(0.2, 3)...)
				addTestVertices(&c, fullTestWeight(0), seamTestPoints(0.1, 3)...)
				return []MotionPalette{a, b, c}
			},
			want: [][]int32{{1, 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mirroredTestMotion(t).MapPalettes(t.Context(), tt.palettes())
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range tt.want {
				if !slices.Equal(got[i], want) {
					t.Errorf("palette %d = %v, want %v", i, got[i], want)
				}
			}
		})
	}
}

// addTestVertices appends points with one set of bone weights to a palette.
func addTestVertices(palette *MotionPalette, blend [8]uint16, points ...[3]float32) {
	for _, point := range points {
		palette.Positions = append(palette.Positions, point[:]...)
		palette.Blend = append(palette.Blend, blend[:]...)
	}
}

func fullTestWeight(group uint16) [8]uint16 {
	return [8]uint16{group, 0, 0, 0, 65535}
}

// seamTestPoints returns n points 1 mm apart from (0.05, y, 0): no reference vertex of mirroredTestMotion lies near
// them, and its right joint is the nearest.
func seamTestPoints(y float32, n int) [][3]float32 {
	points := make([][3]float32, n)
	for i := range points {
		points[i] = [3]float32{0.05, y, float32(i) * 0.001}
	}
	return points
}

var viewerMotionTestIdentity = [12]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}

// A WWMI draw binds its blend buffer as vb4, eight groups then eight weights per row, of which the four strongest
// are kept. Without a vb4 binding it is the BlendBuffer beside the PositionBuffer.
func TestResolveModelViewerBlendSourceWWMI(t *testing.T) {
	dir := t.TempDir()
	blendRow := []byte{5, 9, 1, 2, 3, 0, 0, 0, 10, 200, 30, 15, 0, 0, 0, 0}
	for name, data := range map[string][]byte{
		"Position.buf": make([]byte, 12),
		"TexCoord.buf": make([]byte, 16),
		"Blend.buf":    blendRow,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	resources := []modelViewerResource{
		{Name: "ResourcePositionBuffer", Filename: "Position.buf", Stride: 12},
		{Name: "ResourceTexcoordBuffer", Filename: "TexCoord.buf", Stride: 16},
		{Name: "ResourceBlendBuffer", Filename: "Blend.buf", Stride: 16},
	}
	resourceMap := map[string]modelViewerResource{}
	for _, resource := range resources {
		resourceMap[modelViewerNormalizeKey(resource.Name)] = resource
	}
	cache := newModelViewerBufferCache(context.Background())

	source := resolveModelViewerBlendSource(dir, nil, resources[0], 0, true, resourceMap, resources, cache)
	if source == nil || filepath.Base(source.path) != "Blend.buf" {
		t.Fatalf("blend source = %+v, want Blend.buf", source)
	}
	shapeKeyed := modelViewerResource{Name: "ResourceShapeKeyedPosition", Filename: "Position.buf", Stride: 12}
	bound := resolveModelViewerBlendSource(dir, []string{"BlendBufferOverride", "ResourceBlendBuffer"}, shapeKeyed,
		0, true, resourceMap, resources, cache)
	if bound == nil || filepath.Base(bound.path) != "Blend.buf" {
		t.Fatalf("vb4 blend source = %+v, want Blend.buf", bound)
	}
	blend := readModelViewerMeshBlend(cache, source, nil, 1)
	sum := 10.0 + 200 + 30 + 15
	want := []uint16{
		9, 1, 2, 5,
		uint16(math.Round(200 / sum * 65535)), uint16(math.Round(30 / sum * 65535)),
		uint16(math.Round(15 / sum * 65535)), uint16(math.Round(10 / sum * 65535)),
	}
	if !slices.Equal(blend, want) {
		t.Fatalf("blend = %v, want %v", blend, want)
	}
}
