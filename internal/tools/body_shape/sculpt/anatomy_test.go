package sculpt

import (
	"math"
	"slices"
	"testing"
)

func grid(columns, rows int, point func(column, row int) Vec3) ([]float32, []uint32) {
	positions := make([]float32, columns*rows*3)
	for row := range rows {
		for column := range columns {
			p := point(column, row)
			o := (row*columns + column) * 3
			positions[o], positions[o+1], positions[o+2] = float32(p[0]), float32(p[1]), float32(p[2])
		}
	}
	var indices []uint32
	for row := 0; row+1 < rows; row++ {
		for column := 0; column+1 < columns; column++ {
			a := uint32(row*columns + column)
			c := uint32(columns)
			indices = append(indices, a, a+1, a+c, a+1, a+c+1, a+c)
		}
	}
	return positions, indices
}

func TestAnatomyVolumeGrowsEachBreastFromItsApex(t *testing.T) {
	const columns, rows = 51, 31
	positions, indices := grid(columns, rows, func(column, row int) Vec3 {
		x := float64(column)*0.1 - 2.5
		y := float64(row)*0.1 - 1.5
		bump := func(cx float64) float64 { return 0.5 * math.Exp(-((x-cx)*(x-cx)+y*y)/0.18) }
		return Vec3{x, y, bump(-1) + bump(1)}
	})
	vertexAt := func(x, y float64) int {
		return int(jsRound((y+1.5)/0.1))*columns + int(jsRound((x+2.5)/0.1))
	}
	mask := make([]float32, columns*rows)
	for vertex := range mask {
		x, y := float64(positions[vertex*3]), float64(positions[vertex*3+1])
		distance := math.Hypot(math.Abs(x)-1, y)
		mask[vertex] = float32(min(1, max(0, (0.8-distance)/0.2)))
	}

	basis, ok := BuildAnatomyBasis(AnatomyOptions{
		Model:     AnatomyVolume,
		Positions: positions,
		Mask:      mask,
		Up:        Vec3{0, 1, 0},
		Adjacency: BuildVertexAdjacency(columns*rows, indices, nil),
		Normals:   ComputeVertexNormals(positions, indices, nil),
	})
	if !ok {
		t.Fatal("no basis")
	}
	size := basis.Fields[slices.Index(AnatomyControls[AnatomyVolume], "size")]
	rise := func(vertex int) float64 { return float64(size[vertex*3+2]) }

	apex := rise(vertexAt(1, 0))
	if apex <= rise(vertexAt(1, 0.5)) {
		t.Errorf("apex rise %v not above its flank %v", apex, rise(vertexAt(1, 0.5)))
	}
	if left := rise(vertexAt(-1, 0)); math.Abs(apex-left) >= 1e-4 {
		t.Errorf("right apex %v, left apex %v", apex, left)
	}
	// Centered on its own apex, a breast grows the same toward the sternum and the armpit.
	inner, outer := rise(vertexAt(0.5, 0)), rise(vertexAt(1.5, 0))
	if math.Abs(inner-outer) >= max(inner, outer)*0.05 {
		t.Errorf("inner %v vs outer %v", inner, outer)
	}
	if gap := rise(vertexAt(0, 0)); math.Abs(gap) >= apex*0.01 {
		t.Errorf("sternum rise %v", gap)
	}
	outside := vertexAt(0, 1.4)
	assertEqual(t, "outside", size[outside*3:outside*3+3], []float32{0, 0, 0})
}

func TestAnatomyLimbGirthStaysOffTheAxis(t *testing.T) {
	const segments, rings = 16, 21
	axis := Vec3{1 / math.Sqrt(5), 2 / math.Sqrt(5), 0}
	side := Vec3{-axis[1], axis[0], 0}
	positions, indices := grid(segments, rings, func(segment, ring int) Vec3 {
		angle := float64(segment) / segments * math.Pi * 2
		along := float64(ring) * 0.2
		var p Vec3
		for i := range 3 {
			p[i] = axis[i]*along + side[i]*math.Cos(angle)*0.3
		}
		p[2] += math.Sin(angle) * 0.3
		return p
	})
	mask := make([]float32, segments*rings)
	for i := range mask {
		mask[i] = 1
	}

	basis, ok := BuildAnatomyBasis(AnatomyOptions{
		Model:     AnatomyLimb,
		Positions: positions,
		Mask:      mask,
		Up:        Vec3{0, 1, 0},
		Adjacency: BuildVertexAdjacency(segments*rings, indices, nil),
	})
	if !ok {
		t.Fatal("no basis")
	}
	girth := basis.Fields[slices.Index(AnatomyControls[AnatomyLimb], "girth")]
	for vertex := range segments * rings {
		if axial := vertexOffset(girth, vertex, Vec3{}).dot(axis); math.Abs(axial) >= 1e-5 {
			t.Fatalf("vertex %d moved %v along the limb", vertex, axial)
		}
	}
	radial := func(ring int) float64 {
		g := vertexOffset(girth, ring*segments, Vec3{})
		return math.Sqrt(g.dot(g))
	}
	if radial(10) <= radial(2) {
		t.Fatalf("mid-limb girth %v not above near-end girth %v", radial(10), radial(2))
	}
}
