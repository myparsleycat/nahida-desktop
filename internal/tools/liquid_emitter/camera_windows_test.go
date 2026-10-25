//go:build windows

package liquidemitter

import (
	"io/fs"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"unsafe"
)

// BuildCamera.hlsl derives what an adapter's camera rows leave out. Star
// Rail's constants hold no reversed-Z projection, and the inverse view next to
// them is the previous frame's, which only differs while the camera turns.
// Genshin's hold all four matrices, but the copy a shader binds fills only the
// rows it reads, so the first G-buffer draw of a frame may hold nothing but
// the view-projection. Either mistake only misplaces or hides the liquid in
// game.
func TestBuildCameraDerivesMissingMatrices(t *testing.T) {
	type mat4 [4][4]float64
	mul := func(a, b mat4) mat4 {
		var m mat4
		for r := range 4 {
			for c := range 4 {
				for k := range 4 {
					m[r][c] += a[r][k] * b[k][c]
				}
			}
		}
		return m
	}
	// A Unity camera yawed off the axes, whose view matrix mirrors Z.
	camera := func(yaw float64, eye [3]float64) (view, inverseView mat4) {
		sin, cos := math.Sin(yaw), math.Cos(yaw)
		basis := [3][3]float64{{cos, 0, -sin}, {0, 1, 0}, {sin, 0, cos}}
		for r := range 3 {
			for c := range 3 {
				inverseView[r][c] = basis[r][c] * [3]float64{1, 1, -1}[c]
				view[c][r] = inverseView[r][c]
			}
			inverseView[r][3] = eye[r]
		}
		for r := range 3 {
			for k := range 3 {
				view[r][3] -= view[r][k] * eye[k]
			}
		}
		view[3][3], inverseView[3][3] = 1, 1
		return view, inverseView
	}

	// A turning camera with the jittered, flipped, reversed-Z projection it
	// draws with; each case stores the matrices its game's constants hold.
	view, inverseView := camera(0.6, [3]float64{-86.2, -13.9, -43.6})
	_, previousInverseView := camera(0.5, [3]float64{-86, -13.9, -43.5})
	projection := mat4{{1.358, 0, 0.00028, 0}, {0, -2.4142, 0.00045, 0}, {0, 0, 3.3379e-5, 0.1}, {0, 0, -1, 0}}
	cases := []struct {
		game   string
		size   int
		stored map[int]mat4
	}{
		{"srmi", 41, map[int]mat4{23: view, 27: previousInverseView, 31: mul(projection, view)}},
		{"gimi", 35, map[int]mat4{17: mul(projection, view)}},
	}
	for _, tc := range cases {
		t.Run(tc.game, func(t *testing.T) {
			source := make([]float32, tc.size*4)
			for row, m := range tc.stored {
				for c := range 4 {
					for r := range 4 {
						source[(row+c)*4+r] = float32(m[r][c])
					}
				}
			}

			// The rows the adapter names, from the Probe.ini it installs.
			files, err := runtimeFiles(tc.game)
			if err != nil {
				t.Fatal(err)
			}
			probe, err := fs.ReadFile(files, probeFile)
			if err != nil {
				t.Fatal(err)
			}
			rows := map[int][4]float32{}
			for _, match := range regexp.MustCompile(`(?m)^\s*([xyzw])(25[45]) = (-?\d+)`).FindAllSubmatch(probe, -1) {
				register, _ := strconv.Atoi(string(match[2]))
				value, _ := strconv.Atoi(string(match[3]))
				row := rows[register]
				row[map[byte]int{'x': 0, 'y': 1, 'z': 2, 'w': 3}[match[1][0]]] = float32(value)
				rows[register] = row
			}

			bundle := newShaderBundle(t, tc.game)
			code := bundle.compile(filepath.Join(bundle.root, "camera", "BuildCamera.hlsl"), "cs")
			d := newWARPDevice(t)
			built := d.structured(make([]float32, 24*4), 16)
			d.dispatch(d.shader("cs", code), bindings{
				srv: map[int]unsafe.Pointer{120: d.iniParams(rows)},
				uav: map[int]unsafe.Pointer{0: built.uav},
				cb:  map[int]unsafe.Pointer{13: d.constants(source)},
			}, [3]int{1, 1, 1})

			got := d.readFloats(built)
			load := func(first int) mat4 {
				var m mat4
				for c := range 4 {
					for r := range 4 {
						m[r][c] = float64(got[(first+c)*4+r])
					}
				}
				return m
			}
			identity := mat4{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}
			for _, check := range []struct {
				name      string
				got, want mat4
			}{
				{"view", load(0), view},
				{"inverse view", load(4), inverseView},
				{"projection", load(8), projection},
				{"projection * inverse projection", mul(load(8), load(12)), identity},
			} {
				for r := range 4 {
					for c := range 4 {
						value := check.got[r][c]
						if math.IsNaN(value) || math.IsInf(value, 0) ||
							math.Abs(value-check.want[r][c]) > 1e-4*math.Max(1, math.Abs(check.want[r][c])) {
							t.Errorf("%s[%d][%d] = %g, want %g (rows %v)", check.name, r, c,
								check.got[r][c], check.want[r][c], rows)
						}
					}
				}
			}
		})
	}
}
