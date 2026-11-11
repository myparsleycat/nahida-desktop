package ui

import (
	"math"
	"time"
)

type vec3 [3]float64

func (a vec3) add(b vec3) vec3      { return vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
func (a vec3) sub(b vec3) vec3      { return vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func (a vec3) scale(k float64) vec3 { return vec3{a[0] * k, a[1] * k, a[2] * k} }
func (a vec3) dot(b vec3) float64   { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func (a vec3) length() float64      { return math.Sqrt(a.dot(a)) }
func (a vec3) cross(b vec3) vec3 {
	return vec3{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func (a vec3) normalize() vec3 {
	if l := a.length(); l > 0 {
		return a.scale(1 / l)
	}
	return a
}

// mat4 is a 4x4 matrix in row-major order that maps column vectors: p' = m * p.
type mat4 [16]float64

func identity4() mat4 { return mat4{0: 1, 5: 1, 10: 1, 15: 1} }

func (m mat4) mul(n mat4) mat4 {
	var r mat4
	for row := range 4 {
		for col := range 4 {
			var sum float64
			for k := range 4 {
				sum += m[row*4+k] * n[k*4+col]
			}
			r[row*4+col] = sum
		}
	}
	return r
}

func (m mat4) point(p vec3) vec3 {
	return vec3{
		m[0]*p[0] + m[1]*p[1] + m[2]*p[2] + m[3],
		m[4]*p[0] + m[5]*p[1] + m[6]*p[2] + m[7],
		m[8]*p[0] + m[9]*p[1] + m[10]*p[2] + m[11],
	}
}

func (m mat4) float32s() [16]float32 {
	var r [16]float32
	for i, v := range m {
		r[i] = float32(v)
	}
	return r
}

func rotationX(angle float64) mat4 {
	c, s := math.Cos(angle), math.Sin(angle)
	return mat4{1, 0, 0, 0, 0, c, -s, 0, 0, s, c, 0, 0, 0, 0, 1}
}

func rotationY(angle float64) mat4 {
	c, s := math.Cos(angle), math.Sin(angle)
	return mat4{c, 0, s, 0, 0, 1, 0, 0, -s, 0, c, 0, 0, 0, 0, 1}
}

// orthographic maps a right-handed view box of half-size r to Direct3D clip space, with depth from 0 at near
// to 1 at far.
func orthographic(r, near, far float64) mat4 {
	return mat4{
		1 / r, 0, 0, 0,
		0, 1 / r, 0, 0,
		0, 0, 1 / (near - far), near / (near - far),
		0, 0, 0, 1,
	}
}

func rotationZ(angle float64) mat4 {
	c, s := math.Cos(angle), math.Sin(angle)
	return mat4{c, -s, 0, 0, s, c, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

// lookAt is a right-handed view matrix: the camera looks down its -Z axis.
func lookAt(eye, target, up vec3) mat4 {
	f := target.sub(eye).normalize()
	r := f.cross(up).normalize()
	u := r.cross(f)
	return mat4{
		r[0], r[1], r[2], -r.dot(eye),
		u[0], u[1], u[2], -u.dot(eye),
		-f[0], -f[1], -f[2], f.dot(eye),
		0, 0, 0, 1,
	}
}

// perspective maps right-handed view space to Direct3D clip space with depth from 0 at near to 1 at far.
func perspective(fovY, aspect, near, far float64) mat4 {
	f := 1 / math.Tan(fovY/2)
	return mat4{
		f / aspect, 0, 0, 0,
		0, f, 0, 0,
		0, 0, far / (near - far), near * far / (near - far),
		0, 0, -1, 0,
	}
}

const modelViewerFieldOfView = 45 * math.Pi / 180

// orbitCamera circles a target point. Yaw turns about world Y and pitch tilts toward the poles; both are
// radians.
type orbitCamera struct {
	target   vec3
	distance float64
	yaw      float64
	pitch    float64
	// radius is the size of the framed model, which scales panning, zooming and the clip planes.
	radius float64
	// center is the middle of the framed model. The clip planes stay around its depth, so the model is not cut
	// when the target is panned away from it.
	center vec3
	// fov is the vertical field of view in radians; zero is modelViewerFieldOfView.
	fov float64
}

func (c orbitCamera) lens() float64 {
	if c.fov == 0 {
		return modelViewerFieldOfView
	}
	return c.fov
}

// forward is the direction the camera looks in.
func (c orbitCamera) forward() vec3 { return c.target.sub(c.eye()).normalize() }

// frameOrbit is the default three-quarter view of a model with the given bounds, matching the React
// viewer's frameCameraOnObject.
func frameOrbit(minimum, maximum vec3) orbitCamera {
	center := minimum.add(maximum).scale(0.5)
	size := maximum.sub(minimum)
	radius := max(size[0], size[1], size[2]) * 0.5
	if radius <= 0 || math.IsNaN(radius) || math.IsInf(radius, 0) {
		radius = 1
	}
	distance := max(radius/math.Sin(modelViewerFieldOfView/2), radius*1.8)
	offset := vec3{0.45, 0.15, 1}
	return orbitCamera{
		target:   center,
		distance: distance * offset.length(),
		yaw:      math.Atan2(offset[0], offset[2]),
		pitch:    math.Asin(offset[1] / offset.length()),
		radius:   radius,
		center:   center,
	}
}

func (c orbitCamera) eye() vec3 {
	cp := math.Cos(c.pitch)
	return c.target.add(vec3{math.Sin(c.yaw) * cp, math.Sin(c.pitch), math.Cos(c.yaw) * cp}.scale(c.distance))
}

// basis returns the camera's right, up and backward axes in world space.
func (c orbitCamera) basis() (right, up, back vec3) {
	back = c.eye().sub(c.target).normalize()
	right = vec3{0, 1, 0}.cross(back).normalize()
	return right, back.cross(right), back
}

func (c orbitCamera) view() mat4 { return lookAt(c.eye(), c.target, vec3{0, 1, 0}) }

func (c orbitCamera) projection(aspect float64) mat4 {
	depth := c.center.sub(c.eye()).dot(c.forward())
	near := max(depth-c.radius*4, c.radius*0.01, 1e-4)
	far := max(depth+c.radius*4, near+c.radius)
	return perspective(c.lens(), aspect, near, far)
}

// follow moves the camera with a model whose centre moved by delta.
func (c *orbitCamera) follow(delta vec3) {
	c.target, c.center = c.target.add(delta), c.center.add(delta)
}

// orbit turns the camera by a pointer movement in logical pixels.
func (c *orbitCamera) orbit(dx, dy float64) {
	c.yaw -= dx * 0.008
	c.pitch = min(max(c.pitch+dy*0.008, -1.55), 1.55)
}

// pan moves the target in the view plane so the model follows the pointer in a view height pixels tall.
func (c *orbitCamera) pan(dx, dy, height float64) {
	right, up, _ := c.basis()
	perPixel := 2 * c.distance * math.Tan(c.lens()/2) / max(height, 1)
	c.target = c.target.add(right.scale(-dx * perPixel)).add(up.scale(dy * perPixel))
}

// zoom scales the distance by notches of the wheel; positive notches move away.
func (c *orbitCamera) zoom(notches float64) {
	c.distance = min(max(c.distance*math.Pow(1.15, notches), c.radius*0.05), c.radius*50)
}

// The React viewer's camera motion: the damping of its OrbitControls, its wheel zoom and the turn of its axis
// gizmo. The damping factors apply per frame at 60 frames a second.
const (
	orbitDamping    = 0.08
	zoomDamping     = 0.16
	axisTurnDamping = 0.2
	axisTurnEpsilon = 0.001
	// zoomPerNotch is the exponent a wheel notch scales the distance by: 100 pixels at 0.0015 a pixel, times
	// the zoom speed of 1.5.
	zoomPerNotch = 100 * 0.0015 * 1.5
	// orbitPolePitch keeps the camera just short of the poles, where its up direction is undefined.
	orbitPolePitch = math.Pi/2 - 0.001
	// cameraTick is the interval the motion advances at.
	cameraTick = 16 * time.Millisecond
)

// cameraMotion is the movement an orbit camera still has to make: a damped drag, a wheel zoom or a turn to an
// axis view. Stepping it at cameraTick moves the camera as the React viewer's controls did.
type cameraMotion struct {
	// yaw, pitch and shift are the rotation and the move of the target a drag asked for that the damping has
	// not applied yet.
	yaw, pitch float64
	shift      vec3
	// distance is the distance the wheel zooms to, or zero.
	distance float64
	// turn is the yaw and pitch an axis view turns the camera to, or nil.
	turn *[2]float64
	// last is when the motion last advanced, while running.
	last    time.Time
	running bool
}

// rotate asks for a turn by a pointer movement in logical pixels over a view height pixels tall, at the
// speed of OrbitControls: the view's height turns the camera once around.
func (m *cameraMotion) rotate(dx, dy, height float64) {
	k := 2 * math.Pi / max(height, 1)
	m.yaw -= dx * k
	m.pitch += dy * k
}

// pan asks for a move of the target of c in the view plane, so the model follows the pointer.
func (m *cameraMotion) pan(c orbitCamera, dx, dy, height float64) {
	moved := c
	moved.pan(dx, dy, height)
	m.shift = m.shift.add(moved.target.sub(c.target))
}

// stop drops the motion that is left.
func (m *cameraMotion) stop() {
	m.yaw, m.pitch, m.shift, m.distance, m.turn = 0, 0, vec3{}, 0, nil
}

// zoom moves the zoom target of c by notches of the wheel; positive notches move away.
func (m *cameraMotion) zoom(c orbitCamera, notches float64) {
	distance := m.distance
	if distance == 0 {
		distance = c.distance
	}
	m.distance = min(max(distance*math.Exp(notches*zoomPerNotch), max(c.radius*0.01, 0.01)), c.radius*50)
}

// step advances c by dt seconds of the motion and reports whether any is left.
func (m *cameraMotion) step(c *orbitCamera, dt float64) bool {
	frames := dt * 60
	damped := func(factor float64) float64 { return 1 - math.Pow(1-factor, frames) }

	if m.yaw != 0 || m.pitch != 0 {
		k := damped(orbitDamping)
		c.yaw += m.yaw * k
		m.yaw -= m.yaw * k
		pitch := min(max(c.pitch+m.pitch*k, -orbitPolePitch), orbitPolePitch)
		// A drag past a pole has nowhere to go.
		if pitch != c.pitch+m.pitch*k {
			m.pitch = 0
		} else {
			m.pitch -= m.pitch * k
		}
		c.pitch = pitch
		if math.Abs(m.yaw) < 1e-5 && math.Abs(m.pitch) < 1e-5 {
			m.yaw, m.pitch = 0, 0
		}
	}
	if m.shift != (vec3{}) {
		k := damped(orbitDamping)
		c.target = c.target.add(m.shift.scale(k))
		m.shift = m.shift.scale(1 - k)
		if m.shift.length() < 1e-5*max(c.radius, 1e-3) {
			m.shift = vec3{}
		}
	}
	if m.turn != nil {
		yawError := math.Mod(m.turn[0]-c.yaw+math.Pi, 2*math.Pi)
		if yawError < 0 {
			yawError += 2 * math.Pi
		}
		yawError -= math.Pi
		pitchError := m.turn[1] - c.pitch
		k := damped(axisTurnDamping)
		if math.Abs(yawError) < axisTurnEpsilon && math.Abs(pitchError) < axisTurnEpsilon {
			k, m.turn = 1, nil
		}
		c.yaw += yawError * k
		c.pitch += pitchError * k
	}
	if m.distance != 0 {
		next := c.distance + (m.distance-c.distance)*damped(zoomDamping)
		if math.Abs(m.distance-next) < 1e-4*max(c.radius, 1e-3) {
			next, m.distance = m.distance, 0
		}
		c.distance = next
	}
	return m.yaw != 0 || m.pitch != 0 || m.shift != (vec3{}) || m.turn != nil || m.distance != 0
}

// viewerAxis is one axis of the orientation gizmo with the views along it, as the yaw and pitch of the camera.
type viewerAxis struct {
	label              string
	color              uint32
	direction          vec3
	positive, negative [2]float64
}

var viewerAxes = []viewerAxis{
	{"X", 0xff3352, vec3{1, 0, 0}, [2]float64{math.Pi / 2, 0}, [2]float64{-math.Pi / 2, 0}},
	{"Y", 0x8bdc00, vec3{0, 1, 0}, [2]float64{0, orbitPolePitch}, [2]float64{0, -orbitPolePitch}},
	{"Z", 0x2890ff, vec3{0, 0, 1}, [2]float64{0, 0}, [2]float64{math.Pi, 0}},
}

// ray returns the world-space ray through a point of a view width x height logical pixels, measured from its
// top-left corner. The direction is a unit vector.
func (c orbitCamera) ray(x, y, width, height float64) (origin, direction vec3) {
	width, height = max(width, 1), max(height, 1)
	tanHalf := math.Tan(c.lens() / 2)
	right, up, back := c.basis()
	ndcX, ndcY := 2*x/width-1, 1-2*y/height
	direction = right.scale(ndcX * tanHalf * width / height).add(up.scale(ndcY * tanHalf)).sub(back)
	return c.eye(), direction.normalize()
}

// inverseAffine inverts a matrix whose last row is 0 0 0 1. A singular matrix yields the identity.
func (m mat4) inverseAffine() mat4 {
	a, b, c := m[0], m[1], m[2]
	d, e, f := m[4], m[5], m[6]
	g, h, k := m[8], m[9], m[10]
	det := a*(e*k-f*h) - b*(d*k-f*g) + c*(d*h-e*g)
	if det == 0 || math.IsNaN(det) {
		return identity4()
	}
	inv := 1 / det
	r := mat4{
		(e*k - f*h) * inv, (c*h - b*k) * inv, (b*f - c*e) * inv, 0,
		(f*g - d*k) * inv, (a*k - c*g) * inv, (c*d - a*f) * inv, 0,
		(d*h - e*g) * inv, (b*g - a*h) * inv, (a*e - b*d) * inv, 0,
		0, 0, 0, 1,
	}
	t := r.point(vec3{m[3], m[7], m[11]})
	r[3], r[7], r[11] = -t[0], -t[1], -t[2]
	return r
}

// direction transforms a direction, leaving out the translation.
func (m mat4) direction(v vec3) vec3 {
	return vec3{
		m[0]*v[0] + m[1]*v[1] + m[2]*v[2],
		m[4]*v[0] + m[5]*v[1] + m[6]*v[2],
		m[8]*v[0] + m[9]*v[1] + m[10]*v[2],
	}
}
