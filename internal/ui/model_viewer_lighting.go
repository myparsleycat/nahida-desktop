package ui

import (
	"math"
	"math/bits"
	"sync"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// The React viewer's rendering choices, as the model viewer settings store them.
const (
	viewerToneNeutral = "neutral"
	viewerToneACES    = "aces"
	viewerToneNone    = "none"

	viewerEnvironmentStudio = "studio"
	viewerEnvironmentSoft   = "soft"
	viewerEnvironmentNone   = "none"

	viewerDefaultExposure = 0.7
	viewerMinExposure     = 0
	viewerMaxExposure     = 4
)

// viewerLights are the intensities of the React viewer's lights for an environment (modelViewerLighting).
type viewerLights struct {
	ambient, key, fill, hemisphere float64
}

func viewerLighting(environment string) viewerLights {
	switch environment {
	case viewerEnvironmentNone:
		return viewerLights{ambient: 0.45, key: 1.35, fill: 0.45}
	case viewerEnvironmentSoft:
		return viewerLights{ambient: 0.5, key: 1.5, fill: 0.6, hemisphere: 0.55}
	default:
		return viewerLights{ambient: 0.6, key: 1.8, fill: 0.8, hemisphere: 0.9}
	}
}

// viewerBaseMaterial is the metalness and roughness of the MeshStandardMaterial the React viewer gives a
// profile's meshes.
func viewerBaseMaterial(profile modelviewer.ModelViewerMaterialProfile) (metalness, roughness float64) {
	switch profile {
	case "wwmi", "wuwa:rabbitfx":
		return 0, 1
	}
	return 0.05, 0.65
}

func clampViewerExposure(value float64) float64 {
	if math.IsNaN(value) {
		return viewerDefaultExposure
	}
	return min(max(value, viewerMinExposure), viewerMaxExposure)
}

func srgbToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// pmremLevels is the number of roughness levels three.js's PMREMGenerator.fromScene makes for its 256-pixel
// cube: level i holds roughness i/10.
const pmremLevels = 11

// viewerEnvironmentSH holds the order-2 spherical harmonics of the radiance three.js's RoomEnvironment shows a
// cube camera at its origin, for the studio room and for the soft one scaled to 0.85. The room is white and
// grey, so one channel describes it.
var viewerEnvironmentSH = sync.OnceValue(func() [2][9]float64 {
	return [2][9]float64{roomEnvironmentSH(1), roomEnvironmentSH(0.85)}
})

// viewerPMREMBands holds, for each PMREM roughness level, how much the level's filtering keeps of each SH band.
// PMREMGenerator blurs level i from level i-1 with a GGX lobe of the incremental roughness, so the factors
// multiply along the chain.
var viewerPMREMBands = sync.OnceValue(func() [pmremLevels][3]float64 {
	var levels [pmremLevels][3]float64
	levels[0] = [3]float64{1, 1, 1}
	last := float64(pmremLevels - 1)
	for i := 1; i < pmremLevels; i++ {
		target, source := float64(i)/last, float64(i-1)/last
		roughness := math.Sqrt(target*target-source*source) * target * 1.25
		step := ggxBands(roughness)
		for band := range 3 {
			levels[i][band] = levels[i-1][band] * step[band]
		}
	}
	return levels
})

// ggxBands is the Legendre projection of one PMREM GGX filter pass: the NdotL-weighted mean of P_l over the
// 256 Hammersley samples of its VNDF importance sampling with V = N.
func ggxBands(roughness float64) [3]float64 {
	const samples = 256
	if roughness < 0.001 {
		return [3]float64{1, 1, 1}
	}
	alpha := roughness * roughness
	var band1, band2, total float64
	for i := range samples {
		u := float64(i) / samples
		v := float64(bits.Reverse32(uint32(i))) * 2.3283064365386963e-10
		r, phi := math.Sqrt(u), 2*math.Pi*v
		t1, t2 := r*math.Cos(phi), r*math.Sin(phi)
		h := vec3{alpha * t1, alpha * t2, math.Sqrt(max(0, 1-t1*t1-t2*t2))}.normalize()
		z := 2*h[2]*h[2] - 1
		if z <= 0 {
			continue
		}
		band1 += z * z
		band2 += z * (3*z*z - 1) / 2
		total += z
	}
	if total == 0 {
		return [3]float64{1, 1, 1}
	}
	return [3]float64{1, band1 / total, band2 / total}
}

// shBasis evaluates the nine real order-2 spherical harmonics at a unit direction.
func shBasis(d vec3) [9]float64 {
	x, y, z := d[0], d[1], d[2]
	return [9]float64{
		0.282095,
		0.488603 * y, 0.488603 * z, 0.488603 * x,
		1.092548 * x * y, 1.092548 * y * z, 0.315392 * (3*z*z - 1), 1.092548 * x * z, 0.546274 * (x*x - y*y),
	}
}

type roomBox struct {
	center, size vec3
	turn         float64 // rotation about Y, radians
	emission     float64 // the radiance of a light panel; 0 for a lit surface
	inside       bool    // the room, seen from within
}

// roomEnvironmentBoxes is three.js r186's RoomEnvironment in its own coordinates.
var roomEnvironmentBoxes = []roomBox{
	{center: vec3{-0.757, 13.219, 0.717}, size: vec3{31.713, 28.305, 28.591}, inside: true},
	{center: vec3{-10.906, 2.009, 1.846}, size: vec3{2.328, 7.905, 4.651}, turn: -0.195},
	{center: vec3{-5.607, -0.754, -0.758}, size: vec3{1.970, 1.534, 3.955}, turn: 0.994},
	{center: vec3{6.167, 0.857, 7.803}, size: vec3{3.927, 6.285, 3.687}, turn: 0.561},
	{center: vec3{-2.017, 0.018, 6.124}, size: vec3{2.002, 4.566, 2.064}, turn: 0.333},
	{center: vec3{2.291, -0.756, -2.621}, size: vec3{1.546, 1.552, 1.496}, turn: -0.286},
	{center: vec3{-2.193, -0.369, -5.547}, size: vec3{3.875, 3.487, 2.986}, turn: 0.516},
	{center: vec3{-16.116, 14.37, 8.208}, size: vec3{0.1, 2.428, 2.739}, emission: 50},
	{center: vec3{-16.109, 18.021, -8.207}, size: vec3{0.1, 2.425, 2.751}, emission: 50},
	{center: vec3{14.904, 12.198, -1.832}, size: vec3{0.15, 4.265, 6.331}, emission: 17},
	{center: vec3{-0.462, 8.89, 14.520}, size: vec3{4.38, 5.441, 0.088}, emission: 43},
	{center: vec3{3.235, 11.486, -12.541}, size: vec3{2.5, 2.0, 0.1}, emission: 20},
	{center: vec3{0, 20, 0}, size: vec3{1, 0.1, 1}, emission: 100},
}

// roomEnvironmentSH renders RoomEnvironment as PMREMGenerator.fromScene sees it, without shadows or
// interreflection: the room sits 3.5 below the camera, scaled by scale, and its white MeshStandardMaterial
// surfaces (roughness 1) are lit only by the 900 cd point light with a cut-off distance of 28.
func roomEnvironmentSH(scale float64) [9]float64 {
	const faceSize = 64
	offset := vec3{0, -3.5, 0}
	light := offset.add(vec3{0.418, 16.199, 0.300}.scale(scale))
	var sh [9]float64
	for face := range 6 {
		for row := range faceSize {
			for col := range faceSize {
				u := (float64(col)+0.5)/faceSize*2 - 1
				v := (float64(row)+0.5)/faceSize*2 - 1
				var d vec3
				axis, sign := face/2, float64(1-2*(face%2))
				d[axis] = sign
				d[(axis+1)%3] = u
				d[(axis+2)%3] = v
				weight := 4 / (math.Pow(1+u*u+v*v, 1.5) * faceSize * faceSize)
				d = d.normalize()
				radiance := roomRadiance(d, scale, offset, light)
				for i, y := range shBasis(d) {
					sh[i] += radiance * y * weight
				}
			}
		}
	}
	return sh
}

func roomRadiance(d vec3, scale float64, offset, light vec3) float64 {
	nearest := math.Inf(1)
	var hit roomBox
	var normal vec3
	for _, box := range roomEnvironmentBoxes {
		center := offset.add(box.center.scale(scale))
		half := box.size.scale(scale / 2)
		c, s := math.Cos(box.turn), math.Sin(box.turn)
		// Into the box's frame: undo its turn about Y.
		toLocal := func(p vec3) vec3 { return vec3{c*p[0] - s*p[2], p[1], s*p[0] + c*p[2]} }
		origin, dir := toLocal(center.scale(-1)), toLocal(d)
		enter, exit := math.Inf(-1), math.Inf(1)
		enterAxis, exitAxis := 0, 0
		for axis := range 3 {
			if dir[axis] == 0 {
				if math.Abs(origin[axis]) > half[axis] {
					enter = math.Inf(1)
				}
				continue
			}
			t0 := (-half[axis] - origin[axis]) / dir[axis]
			t1 := (half[axis] - origin[axis]) / dir[axis]
			if t0 > t1 {
				t0, t1 = t1, t0
			}
			if t0 > enter {
				enter, enterAxis = t0, axis
			}
			if t1 < exit {
				exit, exitAxis = t1, axis
			}
		}
		if enter > exit {
			continue
		}
		// A box shows the face the ray enters; the room shows the far face, whose normal BackSide turns inward.
		// Either way the normal opposes the ray along the face's axis.
		t, axis := enter, enterAxis
		if box.inside {
			t, axis = exit, exitAxis
		}
		if t <= 0 || t >= nearest {
			continue
		}
		var local vec3
		local[axis] = -math.Copysign(1, dir[axis])
		nearest, hit = t, box
		normal = vec3{c*local[0] + s*local[2], local[1], -s*local[0] + c*local[2]}
	}
	if math.IsInf(nearest, 1) {
		return 0
	}
	if hit.emission > 0 {
		return hit.emission
	}

	p := d.scale(nearest)
	toLight := light.sub(p)
	distance := toLight.length()
	l := toLight.scale(1 / distance)
	dotNL := max(normal.dot(l), 0)
	if dotNL == 0 {
		return 0
	}
	falloff := 1 / max(distance*distance, 0.01)
	cutoff := min(max(1-math.Pow(distance/28, 4), 0), 1)
	irradiance := dotNL * 900 * falloff * cutoff * cutoff

	// MeshStandardMaterial with roughness 1 and F0 0.04, seen from the camera at the origin.
	view := d.scale(-1)
	h := l.add(view).normalize()
	dotNV, dotVH := max(normal.dot(view), 0), max(view.dot(h), 0)
	fresnel := math.Exp2((-5.55473*dotVH - 6.98316) * dotVH)
	f := 0.04*(1-fresnel) + fresnel
	specular := f * 0.5 / max(dotNL+dotNV, 1e-6) / math.Pi
	return irradiance * ((1-f)/math.Pi + specular)
}
