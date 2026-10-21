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

// Star Rail's camera constants hold no reversed-Z projection, so SRMI's rows
// leave it out and BuildCamera.hlsl derives it from the view-projection. The
// inverse view next to them is the previous frame's, which only differs while
// the camera turns. Either mistake only misplaces the liquid in game.
func TestBuildCameraDerivesSRMIProjection(t *testing.T) {
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

	// The 656-byte per-camera constants of a turning camera, each matrix stored
	// as its four columns, with the jittered, flipped, reversed-Z projection it
	// draws with.
	view, _ := camera(0.6, [3]float64{-86.2, -13.9, -43.6})
	_, previousInverseView := camera(0.5, [3]float64{-86, -13.9, -43.5})
	projection := mat4{{1.358, 0, 0.00028, 0}, {0, -2.4142, 0.00045, 0}, {0, 0, 3.3379e-5, 0.1}, {0, 0, -1, 0}}
	source := make([]float32, 41*4)
	store := func(row int, m mat4) {
		for c := range 4 {
			for r := range 4 {
				source[(row+c)*4+r] = float32(m[r][c])
			}
		}
	}
	store(23, view)
	store(27, previousInverseView)
	store(31, mul(projection, view))

	// The rows the adapter names, from the Probe.ini it installs.
	files, err := runtimeFiles("srmi")
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

	bundle := newShaderBundle(t, "srmi")
	code := bundle.compile(filepath.Join(bundle.root, "camera", "BuildCamera.hlsl"), "cs")
	d := newWARPDevice(t)
	built := d.structured(make([]float32, 24*4), 16)
	d.dispatch(d.shader("cs", code), bindings{
		srv: map[int]unsafe.Pointer{120: d.iniParams(rows)},
		uav: map[int]unsafe.Pointer{0: built.uav},
		cb:  map[int]unsafe.Pointer{13: d.constants(source)},
	}, [3]int{1, 1, 1})

	got := d.readFloats(built)
	for c := range 4 {
		for r := range 4 {
			if value := float64(got[(8+c)*4+r]); math.Abs(value-projection[r][c]) > 1e-4 {
				t.Errorf("projection[%d][%d] = %g, want %g (rows %v)", r, c, value, projection[r][c], rows)
			}
		}
	}
}
