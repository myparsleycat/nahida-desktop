//go:build windows

package liquidemitter

import (
	"math"
	"path/filepath"
	"testing"
	"unsafe"
)

// Splash pool layout of ParticleConfig.hlsl and Contact.hlsl.
const (
	airborneCount, splashCount = 2048, 6144
	contactRows                = 3
	transferCounters           = 69
	trailIncoming              = 3
	fluidRadius                = .0028
)

// splashDrop is a splash particle in FluidParticle's layout.
type splashDrop struct {
	position, velocity vec3
	age                float64
	owner              int
	// normal and mode of a clinging drop (predictedLambda), none for 0.
	normal vec3
	mode   float64
	scale  float64
	marker float64
}

func (p splashDrop) floats() []float32 {
	return []float32{
		float32(p.position[0]), float32(p.position[1]), float32(p.position[2]), float32(p.age),
		float32(p.velocity[0]), float32(p.velocity[1]), float32(p.velocity[2]), float32(p.owner + 1),
		float32(p.normal[0]), float32(p.normal[1]), float32(p.normal[2]), float32(p.mode),
		0, 0, float32(p.scale), float32(p.marker),
	}
}

func readDrop(values []float32, index int) splashDrop {
	v := values[index*particleFloats:]
	return splashDrop{
		position: vec3{float64(v[0]), float64(v[1]), float64(v[2])}, age: float64(v[3]),
		velocity: vec3{float64(v[4]), float64(v[5]), float64(v[6])}, owner: int(math.Round(float64(v[7]))) - 1,
		normal: vec3{float64(v[8]), float64(v[9]), float64(v[10])}, mode: float64(v[11]),
		scale: float64(v[14]), marker: float64(v[15]),
	}
}

func (p splashDrop) alive() bool { return p.owner >= 0 }

func (p splashDrop) volume() float64 { return p.scale * p.scale * p.scale }

// splashPasses runs SplashUpdate, SplashMerge and SplashSpawn as Probe.ini
// binds them, keeping the state that lasts between frames.
type splashPasses struct {
	t                           *testing.T
	d                           *warpDevice
	update, merge, spawn        unsafe.Pointer
	camera, slots               unsafe.Pointer
	history, adhesion, diagnose gpuBuffer
	particles                   []float32
	time                        float64
	depth                       gpuTexture
	lastTrail                   []float32
	lastTrailCount              uint32
	depthWidth, depthHeight     int
	// surfaceMode is IniParams[241].x, 1 while drops may follow the body.
	surfaceMode float32
	// pulse is Pulse.hlsl's state, and emitting IniParams[237].x.
	pulse    []float32
	emitting float32
	view     testCamera
}

func newSplashPasses(t *testing.T, view testCamera) *splashPasses {
	t.Helper()
	bundle := newShaderBundle(t, games[0].dir)
	d := newWARPDevice(t)
	fluid := func(name string) unsafe.Pointer {
		return d.shader("cs", bundle.compile(filepath.Join(bundle.root, "fluid", name), "cs"))
	}
	return &splashPasses{
		t: t, d: d, view: view,
		update: fluid("SplashUpdate.hlsl"), merge: fluid("SplashMerge.hlsl"), spawn: fluid("SplashSpawn.hlsl"),
		camera:    d.structured(view.rows(), 16).srv,
		slots:     d.structured(liveSlots(0, 1), 16).srv,
		history:   d.structured(make([]float32, splashCount*contactRows*4), 16),
		adhesion:  d.structured(make([]float32, splashCount*16), 64),
		diagnose:  d.uints(make([]uint32, 8)),
		particles: make([]float32, splashCount*particleFloats),
		pulse:     make([]float32, 6*4),
		time:      1,
	}
}

func (s *splashPasses) put(index int, p splashDrop) {
	copy(s.particles[index*particleFloats:], p.floats())
}

func (s *splashPasses) drop(index int) splashDrop { return readDrop(s.particles, index) }

// showWall makes the collision depth a wall facing the camera at view
// distance, or nothing for 0.
func (s *splashPasses) showWall(distance float64) {
	const size = 64
	values := make([]float32, size*size)
	for index := range values {
		values[index] = 1
		if distance > 0 {
			values[index] = float32(s.view.depth(distance))
		}
	}
	s.depthWidth, s.depthHeight = size, size
	s.depth = s.d.floatTexture(size, size, values)
}

func (s *splashPasses) params(dt float64) unsafe.Pointer {
	return s.d.iniParams(map[int][4]float32{
		237: {s.emitting, 1, 0, 0},
		239: {float32(dt / 2), float32(s.time - dt/2), 0, 0},
		241: {s.surfaceMode, 0, 0, 0},
	})
}

// followBody gives slot 0 a body frame at origin, seen this frame, which
// drops in contact mode 2 ride along with.
func (s *splashPasses) followBody(origin vec3) {
	rows := liveSlots(0, 1)
	for row, value := range map[int][4]float32{
		0: {1, 0, 0, 0}, 1: {0, 1, 0, 0}, 2: {0, 0, 1, 0},
		3: {float32(origin[0]), float32(origin[1]), float32(origin[2]), 0},
		4: {0, 0, 0, 1},
	} {
		copy(rows[row*4:], value[:])
	}
	s.slots = s.d.structured(rows, 16).srv
	s.surfaceMode = 1
}

func (s *splashPasses) common(dt float64, source gpuBuffer) map[int]unsafe.Pointer {
	return map[int]unsafe.Pointer{
		100: source.srv, 103: s.slots,
		105: s.d.structured(make([]float32, 32*4), 16).srv,
		106: s.depth.srv,
		107: s.d.uints([]uint32{0}).srv,
		108: s.d.structured(make([]float32, 2048*16), 64).srv,
		109: s.d.structured(s.pulse, 16).srv,
		113: s.camera, 120: s.params(dt),
	}
}

// step runs SplashUpdate for one frame of dt, ending at the next time.
func (s *splashPasses) step(dt float64) {
	s.t.Helper()
	s.time += dt
	source := s.d.structured(s.particles, 64)
	target := s.d.structured(make([]float32, len(s.particles)), 64)
	counters := s.d.uints(make([]uint32, transferCounters))
	trails := s.d.structured(make([]float32, airborneCount*particleFloats), 64)
	s.d.dispatch(s.update, bindings{
		srv: s.common(dt, source),
		uav: map[int]unsafe.Pointer{
			0: target.uav, 1: s.diagnose.uav, 2: s.history.uav, 3: trails.uav,
			4: s.d.uints([]uint32{0}).uav, 5: s.adhesion.uav, 6: counters.uav,
			7: s.d.uints(make([]uint32, airborneCount)).uav,
		},
	}, [3]int{splashCount / 64, 1, 1})
	s.particles = s.d.readFloats(target)
	s.lastTrailCount = s.d.readUints(counters)[trailIncoming]
	s.lastTrail = s.d.readFloats(trails)
}

// mergeAndSpawn runs SplashMerge and SplashSpawn on the particles as they
// are, as after the frame's update.
func (s *splashPasses) mergeAndSpawn(dt float64) {
	s.t.Helper()
	source := s.d.structured(s.particles, 64)
	mergeInto := s.d.uints(make([]uint32, airborneCount))
	s.d.dispatch(s.merge, bindings{
		srv: map[int]unsafe.Pointer{100: source.srv, 102: s.history.srv, 103: s.slots, 120: s.params(dt)},
		uav: map[int]unsafe.Pointer{1: mergeInto.uav},
	}, [3]int{airborneCount / 64, 1, 1})
	views := s.common(dt, source)
	views[102] = mergeInto.srv
	views[104] = s.d.structured(make([]float32, 128*4), 16).srv
	target := s.d.structured(make([]float32, len(s.particles)), 64)
	s.d.dispatch(s.spawn, bindings{
		srv: views,
		uav: map[int]unsafe.Pointer{
			0: target.uav, 1: s.d.uints(make([]uint32, 16)).uav, 2: s.history.uav,
			3: s.d.structured(make([]float32, airborneCount*particleFloats), 64).uav,
			4: s.d.uints([]uint32{0}).uav, 5: s.adhesion.uav,
			6: s.d.uints(make([]uint32, transferCounters)).uav,
			7: s.d.uints(make([]uint32, airborneCount)).uav,
		},
	}, [3]int{splashCount / 64, 1, 1})
	s.particles = s.d.readFloats(target)
}

func (s *splashPasses) held(index int) bool {
	return s.d.readFloats(s.history)[(index*contactRows+2)*4+3] > .5
}

func clinging(position vec3, owner int, normal vec3, scale float64) splashDrop {
	return splashDrop{position: position, owner: owner, normal: normal, mode: 1, scale: scale, marker: 4, age: .5}
}

// Clinging drops merge only in pairs that picked each other, so a row of
// three merges two and leaves the third for a later frame; a merge keeps
// volume and momentum, up to the largest drop, and the rest stays with the
// other drop. Drops of another character, facing apart or held on a surface
// they cannot see do not merge.
func TestClingingDropsMergeInMutualPairs(t *testing.T) {
	s := newSplashPasses(t, newTestCamera(vec3{0, 1, .5}, vec3{0, 1, 0}, math.Pi/3, 1, .1, 100))
	s.showWall(0)
	front, back := vec3{0, 0, 1}, vec3{0, 0, -1}
	a := clinging(vec3{0, 1, 0}, 0, front, 1)
	a.velocity = vec3{0, -.1, 0}
	b := clinging(vec3{.006, 1, 0}, 0, front, 1)
	b.velocity = vec3{0, -.3, 0}
	s.put(0, a)
	s.put(1, b)
	s.put(2, clinging(vec3{.012, 1, 0}, 0, front, 1))
	s.put(10, clinging(vec3{0, 1.2, 0}, 0, front, 1.6))
	s.put(11, clinging(vec3{.004, 1.2, 0}, 0, front, 1.6))
	s.put(20, clinging(vec3{0, 1.4, 0}, 0, front, 1))
	s.put(21, clinging(vec3{.004, 1.4, 0}, 1, front, 1))
	s.put(30, clinging(vec3{0, 1.6, 0}, 0, front, 1))
	s.put(31, clinging(vec3{.004, 1.6, 0}, 0, back, 1))
	s.put(40, clinging(vec3{0, 1.8, 0}, 0, front, 1))
	s.put(41, clinging(vec3{.004, 1.8, 0}, 0, front, 1))
	history := make([]float32, splashCount*contactRows*4)
	history[(40*contactRows+2)*4+3] = 1
	s.history = s.d.structured(history, 16)
	s.mergeAndSpawn(1.0 / 60)

	near := func(value, want float64) bool { return math.Abs(value-want) < 1e-4*math.Max(1, want) }
	if got := s.drop(0); !near(got.volume(), 2) || !near(got.velocity[1], -.2) {
		t.Errorf("merged drop: volume %v velocity %v", got.volume(), got.velocity)
	}
	if s.drop(1).alive() {
		t.Error("the merged-away drop lives on")
	}
	if got := s.drop(2); !got.alive() || !near(got.volume(), 1) {
		t.Errorf("the third drop of the row: %+v", got)
	}
	largest := 1.8 * 1.8 * 1.8
	if got := s.drop(10); !near(got.volume(), largest) {
		t.Errorf("capped merge: volume %v, want %v", got.volume(), largest)
	}
	if got := s.drop(11); !got.alive() || !near(got.volume(), 2*1.6*1.6*1.6-largest) {
		t.Errorf("rest of a capped merge: %+v", got)
	}
	for _, pair := range [][2]int{{20, 21}, {30, 31}, {40, 41}} {
		for _, index := range pair {
			if got := s.drop(index); !got.alive() || !near(got.volume(), 1) {
				t.Errorf("drop %d merged: %+v", index, got)
			}
		}
	}
}

// A clinging drop whose surface the depth stops showing stays on its last
// plane for CONTACT_HOLD, sliding at most CONTACT_HOLD_SLIDE, and holds on
// again when the same surface shows; a different surface does not take it,
// and once the hold runs out it falls.
func TestClingingDropHoldsHiddenContact(t *testing.T) {
	const dt, wall = 1.0 / 60, .5
	view := newTestCamera(vec3{0, 1, wall}, vec3{0, 1, 0}, math.Pi/3, 1, .1, 100)
	start := clinging(vec3{0, 1, fluidRadius + .0001}, 0, vec3{0, 0, 1}, 1)

	for _, scenario := range []struct {
		name      string
		recovered float64
		holds     bool
	}{
		{"same surface", wall, true},
		// Within reach of a drop that never let go, but off the held plane.
		{"surface behind", wall + .008, false},
		{"no surface", 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s := newSplashPasses(t, view)
			s.put(0, start)
			s.showWall(wall)
			s.step(dt)
			if s.held(0) || s.drop(0).mode < .5 {
				t.Fatal("the drop let go of a visible surface")
			}
			seen := s.drop(0).position
			s.showWall(0)
			for range 3 {
				s.step(dt)
				got := s.drop(0)
				if !s.held(0) || got.mode < .5 {
					t.Fatal("the drop did not hold on to the hidden surface")
				}
				if slid := got.position.sub(
					seen,
				); math.Hypot(slid[0], slid[1]) > .005+1e-5 ||
					math.Abs(slid[2]) > 1e-4 {
					t.Fatalf("held drop moved %v", slid)
				}
			}
			s.showWall(scenario.recovered)
			for range 5 {
				s.step(dt)
			}
			got := s.drop(0)
			if scenario.holds {
				if s.held(0) || got.mode < .5 {
					t.Fatalf("the drop did not hold on again: mode %v held %v", got.mode, s.held(0))
				}
				return
			}
			if got.mode > .5 {
				t.Fatal("the drop clung on past the hold")
			}
		})
	}
}

// A running drop leaves a segment each time it has slid about its own width,
// paying for it with its own volume, and a drop that stands lays none.
func TestClingingDropLaysTrail(t *testing.T) {
	const dt, wall = 1.0 / 60, .5
	s := newSplashPasses(t, newTestCamera(vec3{0, 1, wall}, vec3{0, 1, 0}, math.Pi/3, 1, .1, 100))
	s.showWall(wall)
	running := clinging(vec3{0, 1, fluidRadius + .0001}, 0, vec3{0, 0, 1}, 1.2)
	standing := clinging(vec3{.1, 1, .5*fluidRadius + .0001}, 0, vec3{0, 0, 1}, .5)
	standing.age = 0
	s.put(0, running)
	s.put(1, standing)
	segments := 0
	for range 20 {
		before := s.drop(0)
		s.step(dt)
		after := s.drop(0)
		if s.lastTrailCount == 0 {
			continue
		}
		if s.lastTrailCount > 1 {
			t.Fatalf("%d segments in one frame", s.lastTrailCount)
		}
		segments++
		laid := readDrop(s.lastTrail, 0)
		length := math.Hypot(laid.velocity[0], laid.velocity[1])
		// Splash.hlsl TRAIL_MIN_SPACING, TRAIL_MAX_SPACING and TRAIL_MAX_LENGTH.
		spacing := math.Min(math.Max(2*fluidRadius*before.scale, .001), .006)
		if length < spacing-1e-5 || length > .03+1e-5 {
			t.Fatalf("segment %v long", length)
		}
		if paid := before.volume() - after.volume(); math.Abs(paid-12*length*before.scale) > 1e-4 {
			t.Fatalf("segment of %v took %v volume", length, paid)
		}
		if laid.owner != 0 || laid.marker != 6 {
			t.Fatalf("segment %+v", laid)
		}
	}
	if segments < 3 {
		t.Fatalf("a running drop laid %d segments", segments)
	}
	if got := s.drop(1); got.scale != .5 {
		t.Fatalf("the standing drop paid for a trail: %+v", got)
	}
}

// A drop that rides the body has not slid over it: carried along with the
// body it lays no trail and keeps its volume.
func TestBodyCarriedDropLaysNoTrail(t *testing.T) {
	const dt, wall = 1.0 / 60, .5
	s := newSplashPasses(t, newTestCamera(vec3{0, 1, wall}, vec3{0, 1, 0}, math.Pi/3, 1, .1, 100))
	s.showWall(wall)
	s.followBody(vec3{0, 1.2, 0})
	drop := clinging(vec3{.1, 1, .65*fluidRadius + .0001}, 0, vec3{0, 0, 1}, .65)
	drop.mode, drop.age = 2, 0
	s.put(0, drop)
	s.step(dt)
	before := s.drop(0)

	s.followBody(vec3{0, 1.22, 0})
	s.step(dt)
	after := s.drop(0)
	if moved := after.position[1] - before.position[1]; math.Abs(moved-.02) > .002 {
		t.Fatalf("the body moved the drop %v", moved)
	}
	if s.lastTrailCount != 0 || after.scale != before.scale {
		t.Fatalf(
			"the carried drop laid %d segments and went from size %v to %v",
			s.lastTrailCount,
			before.scale,
			after.scale,
		)
	}
}

// A spray's drops beside its stream follow the emitter's amount as the
// stream does, so the least amount sprays next to nothing.
func TestSprayFollowsAmount(t *testing.T) {
	const dt, frames = 1.0 / 60, 30
	sprayed := map[float64]int{}
	for _, amount := range []float64{.02, 1, 2} {
		s := newSplashPasses(t, newTestCamera(vec3{0, 1, .5}, vec3{0, 1, 0}, math.Pi/3, 1, .1, 100))
		s.showWall(0)
		rows := liveSlots(0)
		rows[(0*slotRows+4)*4+3] = 1
		rows[(0*slotRows+12)*4+3] = float32(amount)
		s.slots = s.d.structured(rows, 16).srv
		// Pulse.hlsl: pulse speed 1 with six pulse drops a frame, open and flowing.
		s.pulse[2*4], s.pulse[2*4+2], s.pulse[3*4+3], s.pulse[5*4] = 1, 6, 1, 1
		s.emitting = 1
		for range frames {
			s.time += dt
			s.particles = make([]float32, splashCount*particleFloats)
			s.mergeAndSpawn(dt)
			for index := range airborneCount {
				if p := s.drop(index); p.alive() && p.marker == 3 {
					sprayed[amount]++
				}
			}
		}
	}
	if sprayed[1] < 6*frames {
		t.Fatalf("amount 1 sprayed %d drops in %d frames", sprayed[1], frames)
	}
	if sprayed[.02]*10 > sprayed[1] {
		t.Errorf("amount .02 sprayed %d drops, amount 1 %d", sprayed[.02], sprayed[1])
	}
	if ratio := float64(sprayed[2]) / float64(sprayed[1]); ratio < 1.8 || ratio > 2.2 {
		t.Errorf("amount 2 sprayed %d drops, amount 1 %d", sprayed[2], sprayed[1])
	}
}
