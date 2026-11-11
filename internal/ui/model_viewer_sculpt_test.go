package ui

import (
	"math"
	"testing"
)

func TestViewerShadersCompile(t *testing.T) {
	if _, err := viewerShaderCode(); err != nil {
		t.Fatal(err)
	}
	if _, err := viewerBlitCode(); err != nil {
		t.Fatal(err)
	}
}

// The brush must hit the vertex drawn under the pointer: a model point projected with the draw's matrices
// lies on the pick ray of its pixel.
func TestSculptViewportRayHitsProjectedPoint(t *testing.T) {
	v := &sculptViewport{
		orientation: vec3{90, -30, 45},
		camera:      orbitCamera{target: vec3{0.2, 1, -0.3}, distance: 3, yaw: 0.7, pitch: 0.4, radius: 1},
		width:       640,
		height:      360,
	}
	viewProj := v.camera.projection(float64(v.width / v.height)).mul(v.camera.view())
	for _, point := range []vec3{{0, 0, 0}, {0.3, -0.2, 0.5}, {-0.4, 0.6, -0.1}} {
		world := v.world().point(point)
		var clip [4]float64
		for row := range 4 {
			clip[row] = viewProj[row*4]*world[0] + viewProj[row*4+1]*world[1] + viewProj[row*4+2]*world[2] +
				viewProj[row*4+3]
		}
		x := (clip[0]/clip[3] + 1) / 2 * float64(v.width)
		y := (1 - clip[1]/clip[3]) / 2 * float64(v.height)

		origin, direction := v.modelRay(float32(x), float32(y))
		toPoint := point.sub(origin)
		miss := toPoint.sub(direction.scale(toPoint.dot(direction))).length()
		if miss > 1e-4 || toPoint.dot(direction) <= 0 {
			t.Errorf("ray through %v misses it by %g (along %g)", point, miss, toPoint.dot(direction))
		}
	}
}

func TestInverseAffine(t *testing.T) {
	m := orientationMatrix(vec3{10, 20, 30})
	m[3], m[7], m[11] = 1, -2, 3
	p := vec3{0.5, -1, 2}
	if got := m.inverseAffine().point(m.point(p)); got.sub(p).length() > 1e-9 || math.IsNaN(got[0]) {
		t.Errorf("inverse(m)(m(p)) = %v, want %v", got, p)
	}
}

// A pick saves its face and weights into the mod, so it must take the nearest drawn face and weigh its corners
// in index order, skipping hidden meshes.
func TestHitSceneTakesNearestDrawnFace(t *testing.T) {
	triangle := func(z float32, visible bool) *viewerMesh {
		mesh := &viewerMesh{
			vertexCount: 3,
			indices:     []uint32{0, 1, 2},
			current:     []float32{0, 0, z, 0, 0, 1, 1, 0, z, 0, 0, 1, 0, 1, z, 0, 0, 1},
		}
		mesh.state.Visible = visible
		return mesh
	}
	scene := &viewerScene{meshes: []*viewerMesh{triangle(2, false), triangle(-1, true), triangle(1, true)}}

	hit, ok := hitScene(scene, vec3{0.2, 0.3, 5}, vec3{0, 0, -1})
	if !ok || hit.mesh != 2 || hit.face != 0 {
		t.Fatalf("hit = %+v, %v; want mesh 2 face 0", hit, ok)
	}
	if want := (vec3{0.5, 0.2, 0.3}); hit.barycentric.sub(want).length() > 1e-12 {
		t.Errorf("barycentric = %v, want %v", hit.barycentric, want)
	}
	if _, ok := hitScene(scene, vec3{0.8, 0.8, 5}, vec3{0, 0, -1}); ok {
		t.Error("a ray outside every face hit")
	}
}
