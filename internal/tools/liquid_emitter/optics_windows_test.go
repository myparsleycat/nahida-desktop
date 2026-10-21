//go:build windows

package liquidemitter

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

const compositeWidth, compositeHeight = 32, 16

// compositeScene is a small view of a floor with a stream, a puddle, a film
// and a drop, in the inputs the composite reads.
type compositeScene struct {
	camera                                   testCamera
	sceneDepth, fluidDepth, thickness, depth []float32
	lighting, color                          []float32
	// material is the scattering and roughness the composite reads per pixel,
	// weighted as color is.
	material []float32
	// ambient is FluidAmbient.hlsl's light from above and from around.
	ambient []float32
}

func newCompositeScene() compositeScene {
	camera := newTestCamera(vec3{0, .8, .9}, vec3{0, .3, 0}, math.Pi/3, 2, .1, 100)
	pixels := compositeWidth * compositeHeight
	s := compositeScene{
		camera:     camera,
		sceneDepth: make([]float32, pixels),
		fluidDepth: make([]float32, pixels),
		thickness:  make([]float32, pixels),
		depth:      make([]float32, pixels),
		lighting:   make([]float32, 4*pixels),
		color:      make([]float32, 4*pixels),
		material:   make([]float32, 2*pixels),
		ambient:    []float32{.6, .6, .65, 1, .4, .45, .5, 1},
	}
	tints := [][3]float64{{0.9, 0.75, 0.03}, {0.3, 0.6, 0.9}, {1, 1, 1}, {0.8, 0.2, 0.2}}
	for y := range compositeHeight {
		for x := range compositeWidth {
			index := y*compositeWidth + x
			u, v := (float64(x)+.5)/compositeWidth, (float64(y)+.5)/compositeHeight
			ray := camera.ray(u, v)
			s.fluidDepth[index] = 1e30
			s.sceneDepth[index] = 1
			copy(s.lighting[4*index:], []float32{
				float32(.2 + .6*u), float32(.3 + .5*v), float32(.5 + .3*math.Sin(9*u)), 1,
			})
			if x == 5 && y == 3 {
				s.lighting[4*index] = 6
			}
			var floor vec3
			onFloor := ray[1] < 0
			if onFloor {
				floor = camera.eye.add(ray.scale(-camera.eye[1] / ray[1]))
				s.sceneDepth[index] = float32(camera.depth(camera.view(floor)[2]))
			}

			liquid, distance, chord := -1, 0.0, 0.0
			// A vertical stream of 4 cm radius through the origin, 30 to 70 cm up.
			o := camera.eye
			a := ray[0]*ray[0] + ray[2]*ray[2]
			b := o[0]*ray[0] + o[2]*ray[2]
			c := o[0]*o[0] + o[2]*o[2] - .04*.04
			if disc := b*b - a*c; disc > 0 {
				entry, exit := (-b-math.Sqrt(disc))/a, (-b+math.Sqrt(disc))/a
				if height := o[1] + ray[1]*entry; height > .3 && height < .7 {
					liquid, distance, chord = 0, camera.view(o.add(ray.scale(entry)))[2], exit-entry
				}
			}
			tag := uint32(0)
			if liquid < 0 && onFloor {
				near := func(center vec3, radius float64) bool {
					d := floor.sub(center)
					return d.dot(d) < radius*radius
				}
				switch {
				case near(vec3{.25, 0, .15}, .2):
					liquid, chord, tag = 1, .0012, 1|8
				case near(vec3{-.25, 0, .1}, .15):
					liquid, chord, tag = 2, .0008, 2|32
				case near(vec3{-.05, 0, .35}, .04):
					liquid, chord, tag = 3, .003, 3|16
				}
				distance = camera.view(floor)[2] - .0005
			}
			if liquid < 0 {
				continue
			}
			absorptionDepth := []float64{.015, .02, .03, .01}[liquid]
			weight := chord + 1e-5
			s.fluidDepth[index] = tagDepth(distance, tag)
			s.thickness[index] = float32(chord)
			for channel, tint := range tints[liquid] {
				s.color[4*index+channel] = float32(-math.Log(math.Max(tint, .02)) / absorptionDepth * weight)
			}
			s.color[4*index+3] = float32(weight)
			s.depth[index] = float32(absorptionDepth * 1000 * weight)
		}
	}
	return s
}

// renderComposite runs the composite of the bundle at file over the scene and
// returns every pixel's output, -1 where the composite drew nothing.
func renderComposite(t *testing.T, bundle *shaderBundle, file string, scene compositeScene) []float32 {
	t.Helper()
	d := newWARPDevice(t)
	vs := d.shader("vs", bundle.compile(file, "vs"))
	ps := d.shader("ps", bundle.compile(file, "ps"))
	iniParams := d.iniParams(map[int][4]float32{
		237: {1, 1, 0, 0},
		251: {.9, .75, .03, .015},
		252: {.85, .15, 1.6, .35},
		253: {.02, 0, 4, 1.0 / 60},
	})
	psViews := bindings{srv: map[int]unsafe.Pointer{
		100: d.floatTexture(compositeWidth, compositeHeight, scene.fluidDepth).srv,
		101: d.floatTexture(compositeWidth, compositeHeight, scene.thickness).srv,
		102: d.colorTexture(compositeWidth, compositeHeight, scene.color).srv,
		103: d.floatTexture(compositeWidth, compositeHeight, scene.depth).srv,
		104: d.colorTexture(compositeWidth, compositeHeight, scene.lighting).srv,
		105: d.structured(scene.ambient, 16).srv,
		106: d.floatTexture(compositeWidth, compositeHeight, scene.sceneDepth).srv,
		107: d.texture2D(compositeWidth, compositeHeight, formatR32G32Float, 8,
			float32Bytes(scene.material), false).srv,
		113: d.structured(scene.camera.rows(), 16).srv,
		120: iniParams,
	}}
	target := d.texture2D(compositeWidth, compositeHeight, formatR32G32B32A32Float, 16, nil, true)
	d.clearTarget(target, [4]float32{-1, -1, -1, -1})
	d.draw(vs, ps, bindings{}, psViews, []gpuTexture{target}, 3)
	return bytesFloat32(d.readTexture(target, 16))
}

// With no scattering or roughness, the composite must keep the optics it had
// before either existed: testdata holds its output from that version.
func TestCompositeKeepsLegacyOptics(t *testing.T) {
	bundle := newShaderBundle(t, games[0].dir)
	scene := newCompositeScene()
	got := map[string][]float32{
		"plain":   renderComposite(t, bundle, filepath.Join(bundle.root, "fluid", "FluidComposite.hlsl"), scene),
		"sampled": renderComposite(t, bundle, filepath.Join(bundle.root, "fluid", "FluidCompositeSampled.hlsl"), scene),
	}
	golden := filepath.Join("testdata", "composite_legacy.json")
	if os.Getenv("LIQUID_UPDATE_GOLDEN") == "1" {
		data, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var want map[string][]float32
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	for variant, pixels := range want {
		drawn := 0
		for index, value := range pixels {
			if value != -1 {
				drawn++
			}
			if diff := math.Abs(float64(got[variant][index] - value)); diff > 2e-4+1e-3*math.Abs(float64(value)) {
				t.Fatalf(
					"%s pixel %d channel %d = %v, legacy %v",
					variant,
					index/4,
					index%4,
					got[variant][index],
					value,
				)
			}
		}
		if drawn < 30*4 {
			t.Fatalf("%s composite drew only %d channels", variant, drawn)
		}
	}
}

// withMaterial sets every liquid pixel's scattering and roughness.
func (s compositeScene) withMaterial(scattering, roughness float32) compositeScene {
	s.material = make([]float32, len(s.material))
	for index := range s.thickness {
		weight := s.color[4*index+3]
		s.material[2*index], s.material[2*index+1] = scattering*weight, roughness*weight
	}
	return s
}

// Scattering gives back light from around the liquid and roughness spreads
// its highlights; in the dark neither lights it up, and neither changes how
// much of the pixel it covers.
func TestCompositeMaterialIsLitNotEmissive(t *testing.T) {
	bundle := newShaderBundle(t, games[0].dir)
	file := filepath.Join(bundle.root, "fluid", "FluidComposite.hlsl")
	dark := newCompositeScene()
	dark.lighting = make([]float32, len(dark.lighting))
	dark.ambient = make([]float32, len(dark.ambient))
	clear := renderComposite(t, bundle, file, dark)
	cloudy := renderComposite(t, bundle, file, dark.withMaterial(1, 1))
	drawn := 0
	for pixel := range len(cloudy) / 4 {
		if cloudy[4*pixel+3] == -1 {
			continue
		}
		drawn++
		for channel := range 3 {
			if v := cloudy[4*pixel+channel]; v > 1e-6 {
				t.Fatalf("pixel %d channel %d glows at %v in the dark", pixel, channel, v)
			}
		}
		if diff := math.Abs(float64(cloudy[4*pixel+3] - clear[4*pixel+3])); diff > 1e-6 {
			t.Fatalf("pixel %d coverage %v, %v without the material", pixel, cloudy[4*pixel+3], clear[4*pixel+3])
		}
	}
	if drawn < 30 {
		t.Fatalf("only %d pixels drawn", drawn)
	}
}

// The composite reads the material per unit of the weight it was summed
// under, as it reads the color: twice the liquid of the same kind looks the
// same, and the material does change it.
func TestCompositeMaterialFollowsWeight(t *testing.T) {
	bundle := newShaderBundle(t, games[0].dir)
	file := filepath.Join(bundle.root, "fluid", "FluidComposite.hlsl")
	scene := newCompositeScene()
	once := renderComposite(t, bundle, file, scene.withMaterial(.85, .35))
	doubled := scene.withMaterial(.85, .35)
	for _, values := range [][]float32{doubled.color, doubled.depth, doubled.material} {
		for index := range values {
			values[index] *= 2
		}
	}
	twice := renderComposite(t, bundle, file, doubled)
	plain := renderComposite(t, bundle, file, scene)
	changed := false
	for index, value := range once {
		if diff := math.Abs(float64(twice[index] - value)); diff > 1e-4+1e-3*math.Abs(float64(value)) {
			t.Fatalf("pixel %d channel %d = %v at twice the weight, %v at once", index/4, index%4, twice[index], value)
		}
		changed = changed || math.Abs(float64(plain[index]-value)) > 1e-3
	}
	if !changed {
		t.Fatal("scattering and roughness changed nothing")
	}
}
