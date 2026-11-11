package ui

import (
	"math"
	"testing"
)

// settle steps a motion at the camera tick until it reports nothing left, and fails when it never does: a
// motion that does not end keeps the viewer redrawing.
func settle(t *testing.T, motion *cameraMotion, camera *orbitCamera) {
	t.Helper()
	for range 2000 {
		if !motion.step(camera, cameraTick.Seconds()) {
			return
		}
	}
	t.Fatalf("motion did not settle: %+v", *motion)
}

// The clip planes belong to the model: a target panned away from it and then orbited must not cut it.
func TestProjectionKeepsModelBetweenClipPlanes(t *testing.T) {
	t.Parallel()
	camera := frameOrbit(vec3{-1, -1, -1}, vec3{1, 1, 1})
	right, _, _ := camera.basis()
	camera.target = camera.target.add(right.scale(camera.radius * 10))
	camera.yaw += math.Pi / 2

	viewProj := camera.projection(1).mul(camera.view())
	// The model's centre is the origin, so its clip position is the matrix's last column.
	z, w := viewProj[11], viewProj[15]
	if w <= 0 || z < 0 || z > w {
		t.Errorf("model centre at clip depth %v of %v, want it between the clip planes", z, w)
	}
}

func TestCameraMotionSettlesOnItsTarget(t *testing.T) {
	t.Parallel()
	start := orbitCamera{distance: 4, yaw: 0.3, pitch: 0.2, radius: 1}

	t.Run("a drag turns the camera by the view's share of a full turn", func(t *testing.T) {
		t.Parallel()
		camera, motion := start, cameraMotion{}
		motion.rotate(100, -50, 400)
		settle(t, &motion, &camera)

		if want := start.yaw - 2*math.Pi*100/400; math.Abs(camera.yaw-want) > 1e-3 {
			t.Errorf("yaw = %v, want %v", camera.yaw, want)
		}
		if want := start.pitch - 2*math.Pi*50/400; math.Abs(camera.pitch-want) > 1e-3 {
			t.Errorf("pitch = %v, want %v", camera.pitch, want)
		}
	})

	t.Run("a drag past a pole stops short of it", func(t *testing.T) {
		t.Parallel()
		camera, motion := start, cameraMotion{}
		motion.rotate(0, 4000, 400)
		settle(t, &motion, &camera)

		if camera.pitch != orbitPolePitch {
			t.Errorf("pitch = %v, want %v", camera.pitch, orbitPolePitch)
		}
	})

	t.Run("the wheel zooms to the scaled distance", func(t *testing.T) {
		t.Parallel()
		camera, motion := start, cameraMotion{}
		motion.zoom(camera, 1)
		motion.zoom(camera, 1)
		settle(t, &motion, &camera)

		if want := start.distance * math.Exp(2*zoomPerNotch); math.Abs(camera.distance-want) > 1e-3 {
			t.Errorf("distance = %v, want %v", camera.distance, want)
		}
	})

	t.Run("an axis view is reached the short way around", func(t *testing.T) {
		t.Parallel()
		camera, motion := start, cameraMotion{}
		camera.yaw = 3 * math.Pi / 4
		motion.turn = &[2]float64{-3 * math.Pi / 4, 0}
		travelled := 0.0
		for range 2000 {
			before := camera.yaw
			more := motion.step(&camera, cameraTick.Seconds())
			travelled += math.Abs(camera.yaw - before)
			if !more {
				break
			}
		}

		if motion.turn != nil {
			t.Fatal("turn did not finish")
		}
		if math.Abs(travelled-math.Pi/2) > 1e-2 {
			t.Errorf("yaw travelled %v, want %v", travelled, math.Pi/2)
		}
		if math.Abs(camera.pitch) > 1e-3 {
			t.Errorf("pitch = %v, want 0", camera.pitch)
		}
	})
}
