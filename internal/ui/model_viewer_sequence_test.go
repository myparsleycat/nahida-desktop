package ui

import (
	"math"
	"testing"
	"time"
)

func TestSequencerSecondsHoldWhilePaused(t *testing.T) {
	start := time.Unix(1000, 0)
	s := &sequencer{shots: []WallpaperShot{{Seconds: 2}, {Seconds: 1}}}
	s.enter(0, start, orbitCamera{}, orbitCamera{}, 0, 0)

	s.pause(start.Add(time.Second))
	if s.due(start.Add(time.Hour), 0, false) {
		t.Fatal("a paused shot ran out")
	}
	s.resume(start.Add(time.Minute))
	if s.due(start.Add(time.Minute+900*time.Millisecond), 0, false) {
		t.Fatal("the shot ended before its time, counting the pause")
	}
	if !s.due(start.Add(time.Minute+time.Second), 0, false) {
		t.Fatal("the shot outlasted its seconds after the pause")
	}
	if s.next() != 1 {
		t.Fatalf("next = %d", s.next())
	}
	s.enter(1, start, orbitCamera{}, orbitCamera{}, 0, 0)
	if s.next() != 0 {
		t.Fatalf("the last shot leads to %d, not back to the first", s.next())
	}
}

func TestSequencerLoopsCountFromShotStart(t *testing.T) {
	start := time.Unix(1000, 0)
	s := &sequencer{shots: []WallpaperShot{{Loops: 2, Seconds: 1}}}
	s.enter(0, start, orbitCamera{}, orbitCamera{}, 0, 5)

	if s.due(start.Add(time.Hour), 6, true) {
		t.Fatal("one loop ended a two-loop shot")
	}
	if !s.due(start, 7, true) {
		t.Fatal("two loops did not end the shot")
	}
	if !s.due(start.Add(time.Second), 0, false) {
		t.Fatal("a shot whose clip does not play ignored its seconds")
	}
}

func TestSequencerCameraTurnsTheShortWay(t *testing.T) {
	start := time.Unix(1000, 0)
	from := orbitCamera{distance: 2, yaw: math.Pi - 0.1, radius: 1}
	to := orbitCamera{distance: 4, yaw: -math.Pi + 0.1, radius: 1}
	s := &sequencer{shots: []WallpaperShot{{}}}
	s.enter(0, start, from, to, time.Second, 0)

	lens := from.lens()
	mid, ok := s.camera(start.Add(500*time.Millisecond), lens)
	if !ok {
		t.Fatal("the camera settled halfway through its transition")
	}
	if got := math.Remainder(mid.yaw-math.Pi, 2*math.Pi); math.Abs(got) > 1e-9 {
		t.Fatalf("halfway yaw = %v, want the back of the model at pi", mid.yaw)
	}
	if math.Abs(mid.distance-3) > 1e-9 {
		t.Fatalf("halfway distance = %v", mid.distance)
	}
	end, ok := s.camera(start.Add(2*time.Second), lens)
	if !ok || end.distance != to.distance || end.yaw != to.yaw {
		t.Fatalf("end camera = %+v, %v", end, ok)
	}
	if _, ok := s.camera(start.Add(3*time.Second), lens); ok {
		t.Fatal("a settled camera is still moved")
	}
}

func TestShotViewEqualSurvivesStorage(t *testing.T) {
	camera := orbitCamera{target: vec3{0, 1, 0}, distance: 3, yaw: 0.5, pitch: 0.2, radius: 1}
	shown := WallpaperShot{
		Camera: cameraSnapshot(camera.withLens(toonFieldOfView)),
		State:  map[string]any{"swap": 2},
		Motion: &wallpaperTrack{Clip: "4"},
		Loops:  2,
	}
	stored := WallpaperShot{
		Camera:  cameraSnapshot(camera),
		State:   map[string]any{"swap": float64(2)},
		Motion:  &wallpaperTrack{Clip: "4"},
		Seconds: 5,
	}
	if !shotViewEqual(stored, shown) {
		t.Fatal("a stored shot differs from its view after a JSON round trip and a change of lens")
	}

	for name, change := range map[string]func(*WallpaperShot){
		"camera": func(s *WallpaperShot) { s.Camera.Yaw += 0.01 },
		"toggle": func(s *WallpaperShot) { s.State = map[string]any{"swap": 3} },
		"clip":   func(s *WallpaperShot) { s.Motion = &wallpaperTrack{Clip: "5"} },
		"form":   func(s *WallpaperShot) { s.Form = "Burst" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := shown
			change(&changed)
			if shotViewEqual(stored, changed) {
				t.Fatalf("a %s change was not seen", name)
			}
		})
	}
}
