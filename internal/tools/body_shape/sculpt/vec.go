// Package sculpt holds the body shape editor's mesh model: loading the selected
// meshes into one surface, painting the selection mask, and deforming it.
//
// Positions, normals and weights are stored as float32 and computed in float64,
// so results match the JavaScript implementation this package replaces.
package sculpt

import "math"

// Vec3 is an xyz vector in mesh space.
type Vec3 [3]float64

const epsilon = 1e-8

func (v Vec3) dot(other Vec3) float64 {
	return v[0]*other[0] + v[1]*other[1] + v[2]*other[2]
}

func (v Vec3) reject(axis Vec3) Vec3 {
	along := v.dot(axis)
	return Vec3{v[0] - axis[0]*along, v[1] - axis[1]*along, v[2] - axis[2]*along}
}

func (v Vec3) normalize() (Vec3, bool) {
	length := hypot3(v[0], v[1], v[2])
	if math.IsNaN(length) || math.IsInf(length, 0) || length <= epsilon {
		return Vec3{}, false
	}
	return Vec3{v[0] / length, v[1] / length, v[2] / length}, true
}

func vertexOffset(values []float32, vertex int, origin Vec3) Vec3 {
	o := vertex * 3
	return Vec3{
		float64(values[o]) - origin[0],
		float64(values[o+1]) - origin[1],
		float64(values[o+2]) - origin[2],
	}
}

// hypot3 follows Math.hypot: an infinite component wins over NaN.
func hypot3(x, y, z float64) float64 {
	if math.IsInf(x, 0) || math.IsInf(y, 0) || math.IsInf(z, 0) {
		return math.Inf(1)
	}
	return math.Sqrt(x*x + y*y + z*z)
}

// jsRound follows Math.round, which rounds halves toward positive infinity.
func jsRound(value float64) float64 {
	floor := math.Floor(value)
	if value-floor >= 0.5 {
		return floor + 1
	}
	return floor
}

// Clamp01 clamps a value to [0, 1]; NaN passes through.
func Clamp01(value float64) float64 {
	if value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1
	}
	return value
}

func smoothstep01(t float64) float64 {
	return t * t * (3 - 2*t)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func addFloat32(values []float32, index int, delta float64) {
	values[index] = float32(float64(values[index]) + delta)
}
