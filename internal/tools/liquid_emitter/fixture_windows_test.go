//go:build windows

package liquidemitter

import "math"

func (a vec3) add(b vec3) vec3 { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }

func (a vec3) normalized() vec3 {
	unit, _ := a.unit()
	return unit
}

// testCamera is a perspective camera in the layout of camera/Camera.hlsl,
// with depth from 0 at near to 1 at far.
type testCamera struct {
	eye, right, up, forward vec3
	sx, sy, a, b            float64
}

func newTestCamera(eye, target vec3, fovY, aspect, near, far float64) testCamera {
	forward := target.sub(eye).normalized()
	right := forward.cross(vec3{0, 1, 0}).normalized()
	up := right.cross(forward)
	sy := 1 / math.Tan(fovY/2)
	return testCamera{
		eye: eye, right: right, up: up, forward: forward,
		sx: sy / aspect, sy: sy, a: far / (far - near), b: -near * far / (far - near),
	}
}

// view returns p in view space: x right, y up, z forward.
func (c testCamera) view(p vec3) vec3 {
	d := p.sub(c.eye)
	return vec3{d.dot(c.right), d.dot(c.up), d.dot(c.forward)}
}

// depth is the depth buffer value of a point at view distance z.
func (c testCamera) depth(z float64) float64 { return c.a + c.b/z }

// ray is the world direction through screen uv.
func (c testCamera) ray(u, v float64) vec3 {
	x, y := (u*2-1)/c.sx, (1-v*2)/c.sy
	return c.right.scale(x).add(c.up.scale(y)).add(c.forward).normalized()
}

// rows is the camera buffer: view, inverse view, projection, inverse
// projection and view-projection, each as its four columns, then the identity
// from game to world space.
func (c testCamera) rows() []float32 {
	type column [4]float64
	view := [4]column{
		{c.right[0], c.up[0], c.forward[0], 0},
		{c.right[1], c.up[1], c.forward[1], 0},
		{c.right[2], c.up[2], c.forward[2], 0},
		{-c.right.dot(c.eye), -c.up.dot(c.eye), -c.forward.dot(c.eye), 1},
	}
	inverseView := [4]column{
		{c.right[0], c.right[1], c.right[2], 0},
		{c.up[0], c.up[1], c.up[2], 0},
		{c.forward[0], c.forward[1], c.forward[2], 0},
		{c.eye[0], c.eye[1], c.eye[2], 1},
	}
	projection := [4]column{{c.sx, 0, 0, 0}, {0, c.sy, 0, 0}, {0, 0, c.a, 1}, {0, 0, c.b, 0}}
	inverseProjection := [4]column{{1 / c.sx, 0, 0, 0}, {0, 1 / c.sy, 0, 0}, {0, 0, 0, 1 / c.b}, {0, 0, 1, -c.a / c.b}}
	var viewProjection [4]column
	for j := range 4 {
		for i := range 4 {
			for k := range 4 {
				viewProjection[j][i] += projection[k][i] * view[j][k]
			}
		}
	}
	identity := [4]column{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}
	var out []float32
	for _, matrix := range [][4]column{view, inverseView, projection, inverseProjection, viewProjection, identity} {
		for _, col := range matrix {
			for _, value := range col {
				out = append(out, float32(value))
			}
		}
	}
	return out
}

// tagDepth is the fluid depth FluidSurface writes: the view distance with
// its tag in the low mantissa bits (SSFRCommon.hlsl encodeFluidDepth).
func tagDepth(distance float64, tag uint32) float32 {
	return math.Float32frombits(math.Float32bits(float32(distance))&^63 | tag)
}
