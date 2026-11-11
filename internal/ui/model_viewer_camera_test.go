package ui

import (
	"math"
	"testing"
	"time"

	"github.com/myparsleycat/fairy"
)

const cameraTestStep = 1.0 / 60

func TestModelViewerDragRespondsImmediately(t *testing.T) {
	t.Parallel()
	for button, name := range []string{"orbit", "pan"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := orbitCamera{distance: 4, yaw: 0.3, pitch: 0.2, radius: 1}
			v := ModelViewer{camera: start, height: 400}
			v.drag(fairy.DragEvent{Phase: fairy.DragStart, Button: button, DX: 40, DY: 20})
			if button == 0 {
				if v.camera.yaw == start.yaw || v.camera.pitch == start.pitch {
					t.Fatal("drag did not immediately rotate the camera")
				}
				if v.motion.yaw == 0 || v.motion.pitch == 0 {
					t.Fatal("drag discarded the remaining rotation")
				}
			} else {
				if v.camera.target == start.target {
					t.Fatal("drag did not immediately pan the camera")
				}
				if v.motion.shift == (vec3{}) {
					t.Fatal("drag discarded the remaining pan")
				}
			}
		})
	}
}

// settle steps a motion until it reports nothing left, and fails when it never does: a
// motion that does not end keeps the viewer redrawing.
func settle(t *testing.T, motion *cameraMotion, camera *orbitCamera) {
	t.Helper()
	for range 2000 {
		if !motion.step(camera, cameraTestStep) {
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
			more := motion.step(&camera, cameraTestStep)
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

func TestModelViewerDragPreservesMovement(t *testing.T) {
	t.Parallel()
	for button, name := range []string{"orbit", "pan"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := orbitCamera{distance: 4, yaw: 0.3, pitch: 0.2, radius: 1}
			v := ModelViewer{camera: start, height: 400}
			now := time.Unix(1, 0)
			v.dragAt(fairy.DragEvent{Phase: fairy.DragStart, Button: button, DX: 20, DY: 10}, now)
			now = now.Add(50 * time.Millisecond)
			v.dragAt(fairy.DragEvent{Phase: fairy.DragMove, Button: button, DX: 70, DY: 30}, now)
			now = now.Add(50 * time.Millisecond)
			v.dragAt(fairy.DragEvent{Phase: fairy.DragMove, Button: button, DX: 50, DY: 20}, now)
			before, remaining := v.camera, v.motion
			v.dragAt(fairy.DragEvent{Phase: fairy.DragEnd, Button: button, DX: 50, DY: 20}, now)
			if v.camera != before || v.motion != remaining {
				t.Fatal("release without movement changed the camera or its remaining motion")
			}
			for range 300 {
				now = now.Add(time.Second / 60)
				v.advanceCamera(now)
			}
			if v.motion.running || v.motion.pending() {
				t.Fatal("settled camera still requests frames")
			}

			want := start
			if button == 0 {
				want.yaw -= 2 * math.Pi * 50 / 400
				want.pitch += 2 * math.Pi * 20 / 400
			} else {
				want.pan(50, 20, 400)
			}
			if math.Abs(v.camera.yaw-want.yaw) > 1e-4 || math.Abs(v.camera.pitch-want.pitch) > 1e-4 ||
				v.camera.target.sub(want.target).length() > 1e-4 {
				t.Fatalf("settled camera = %+v, want %+v", v.camera, want)
			}
		})
	}
}

func TestModelViewerDragCancellationKeepsZoom(t *testing.T) {
	t.Parallel()
	for button, name := range []string{"orbit", "pan"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := orbitCamera{distance: 4, yaw: 0.3, pitch: 0.2, radius: 1}
			v := ModelViewer{camera: start, home: start, height: 400}
			now := time.Unix(1, 0)
			v.motion.zoom(v.camera, 2)
			v.runCamera(now)
			v.dragAt(fairy.DragEvent{Phase: fairy.DragStart, Button: button, DX: 40, DY: 20}, now)
			if v.camera.distance != start.distance {
				t.Fatal("drag advanced zoom without elapsed time")
			}
			now = now.Add(50 * time.Millisecond)
			v.dragAt(fairy.DragEvent{Phase: fairy.DragEnd, Button: button, Canceled: true}, now)
			if v.camera.target != start.target || v.camera.yaw != start.yaw || v.camera.pitch != start.pitch {
				t.Fatalf("canceled drag did not restore the camera: %+v", v.camera)
			}
			if v.motion.yaw != 0 || v.motion.pitch != 0 || v.motion.shift != (vec3{}) {
				t.Fatal("canceled drag retained movement")
			}
			distance := v.camera.distance
			v.advanceCamera(now.Add(50 * time.Millisecond))
			if v.camera.distance <= distance || v.camera.target != start.target || v.camera.yaw != start.yaw ||
				v.camera.pitch != start.pitch {
				t.Fatalf("zoom did not continue independently after cancellation: %+v", v.camera)
			}

			v.resetView()
			v.advanceCamera(now.Add(time.Second))
			if v.camera != start || v.motion != (cameraMotion{}) {
				t.Fatal("reset camera resumed old motion")
			}
		})
	}
}

func TestModelViewerCameraFrameRates(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"orbit", "pan", "zoom", "axis"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			var reference orbitCamera
			for _, fps := range []int{60, 120, 144} {
				v := ModelViewer{camera: orbitCamera{distance: 4, yaw: 0.3, pitch: 0.2, radius: 1}, height: 400}
				start := time.Unix(1, 0)
				event := 0
				for frame := 0; frame <= fps/4; frame++ {
					now := start.Add(time.Duration(frame) * time.Second / time.Duration(fps))
					for event < 3 && !start.Add(time.Duration(event)*100*time.Millisecond).After(now) {
						at := start.Add(time.Duration(event) * 100 * time.Millisecond)
						v.advanceCamera(at)
						switch kind {
						case "orbit", "pan":
							button := 0
							if kind == "pan" {
								button = 1
							}
							phase := fairy.DragMove
							if event == 0 {
								phase = fairy.DragStart
							}
							v.dragAt(
								fairy.DragEvent{Phase: phase, Button: button, DX: float32(event+1) * 20, DY: 10},
								at,
							)
						case "zoom":
							v.motion.zoom(v.camera, 1)
							v.runCamera(at)
						case "axis":
							v.motion.turn = &[2]float64{float64(event+1) * 0.4, 0.4}
							v.runCamera(at)
						}
						event++
					}
					v.advanceCamera(now)
				}
				if fps == 60 {
					reference = v.camera
					continue
				}
				if math.Abs(v.camera.yaw-reference.yaw) > 1e-8 || math.Abs(v.camera.pitch-reference.pitch) > 1e-8 ||
					math.Abs(
						v.camera.distance-reference.distance,
					) > 1e-8 || v.camera.target.sub(reference.target).length() > 1e-8 {
					t.Fatalf("%d Hz camera = %+v, 60 Hz camera = %+v", fps, v.camera, reference)
				}
			}
		})
	}
}

func TestModelViewerCameraResumeAndStop(t *testing.T) {
	t.Parallel()
	start := orbitCamera{distance: 4, radius: 1}
	v := ModelViewer{camera: start}
	now := time.Unix(1, 0)
	v.motion.zoom(v.camera, 2)
	v.runCamera(now)
	if v.camera != start || v.revision == 0 {
		t.Fatal("zoom must wake the surface without immediately moving the camera")
	}
	v.runCamera(now.Add(10 * time.Millisecond))
	if v.motion.last != now {
		t.Fatal("starting active motion reset its elapsed time")
	}

	now = now.Add(time.Second)
	v.advanceCamera(now)
	decay := 1 - zoomDamping
	want := start.distance + (v.motion.distance-start.distance)*(1-decay*decay)
	if math.Abs(v.camera.distance-want) > 1e-10 {
		t.Fatalf("resumed distance = %v, want %v", v.camera.distance, want)
	}
	resumed := v.camera
	v.advanceCamera(now)
	if v.camera != resumed {
		t.Fatal("same timestamp advanced motion twice")
	}
	v.advanceCamera(now.Add(100 * time.Millisecond))
	want += (v.motion.distance - want) * (1 - math.Pow(1-zoomDamping, 6))
	if math.Abs(v.camera.distance-want) > 1e-10 {
		t.Fatalf("normal frame after resume = %v, want %v", v.camera.distance, want)
	}

	v.motion.stop()
	before := v.camera
	v.advanceCamera(now.Add(time.Second))
	if v.camera != before || v.motion != (cameraMotion{}) {
		t.Fatal("stopped motion resumed")
	}
	v.motion.zoom(v.camera, 1)
	v.runCamera(now)
	v.closed = true
	v.advanceCamera(now.Add(time.Second))
	if v.camera != before || v.motion != (cameraMotion{}) {
		t.Fatal("closed viewer advanced or retained motion")
	}
}
