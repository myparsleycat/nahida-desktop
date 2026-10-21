//go:build windows

package liquidemitter

import (
	"math"
	"path/filepath"
	"testing"
	"unsafe"
)

// Pool layout of ParticleConfig.hlsl and Slots.hlsl.
const (
	slotCount, primaryPerSlot, primaryCount  = 8, 1024, 8192
	slotRows, slotRootRow, slotGenerationRow = 16, 9, 15
	particleFloats                           = 16
	linkRows, linkNone, linkBroken           = 2, 0xffffffff, 0xfffffffe
)

// liveSlots returns the slot rows with the given slots assigned at the
// origin in generation 1.
func liveSlots(live ...int) []float32 {
	rows := make([]float32, slotCount*slotRows*4)
	for _, s := range live {
		rows[(s*slotRows+slotRootRow)*4+3] = 1
		rows[(s*slotRows+slotGenerationRow)*4] = 1
	}
	return rows
}

// streamParticle is a stream particle, the record FluidSolver.hlsl keeps of
// its birth (streamMeta), and its guide's lane (JetGuide longLane.w) and
// mode (carrierAmount.w).
type streamParticle struct {
	index                      int
	position, velocity         vec3
	birth, age                 float64
	generation, flow, sequence uint32
	lane, mode                 float64
}

type streamLinks struct {
	t       *testing.T
	d       *warpDevice
	shader  unsafe.Pointer
	slots   gpuBuffer
	links   gpuBuffer
	params  unsafe.Pointer
	records []float32
}

func newStreamLinks(t *testing.T) *streamLinks {
	t.Helper()
	bundle := newShaderBundle(t, games[0].dir)
	d := newWARPDevice(t)
	return &streamLinks{
		t: t, d: d,
		shader: d.shader("cs", bundle.compile(filepath.Join(bundle.root, "fluid", "StreamLink.hlsl"), "cs")),
		slots:  d.structured(liveSlots(0), 16),
		links:  d.structured(make([]float32, primaryCount*linkRows*4), 16),
		params: d.iniParams(nil),
	}
}

// run joins the particles as StreamLink does once a frame, keeping the links
// of the frames before.
func (s *streamLinks) run(particles []streamParticle) {
	s.t.Helper()
	pool := make([]float32, primaryCount*particleFloats)
	meta := make([]float32, primaryCount*4)
	guides := make([]float32, primaryCount*16)
	for _, p := range particles {
		guides[p.index*16+11], guides[p.index*16+15] = float32(p.lane), float32(p.mode)
		v := pool[p.index*particleFloats:]
		copy(v, []float32{
			float32(p.position[0]), float32(p.position[1]), float32(p.position[2]), float32(p.age),
			float32(p.velocity[0]), float32(p.velocity[1]), float32(p.velocity[2]), 1,
			0, 0, 0, 0,
			0, float32(p.birth), 0, 3,
		})
		copy(meta[p.index*4:], []float32{
			math.Float32frombits(p.generation), math.Float32frombits(p.flow),
			math.Float32frombits(p.sequence), 1,
		})
	}
	s.d.dispatch(s.shader, bindings{
		srv: map[int]unsafe.Pointer{
			100: s.d.structured(pool, 64).srv, 101: s.d.structured(meta, 16).srv,
			103: s.slots.srv, 112: s.d.structured(guides, 64).srv, 120: s.params,
		},
		uav: map[int]unsafe.Pointer{0: s.links.uav},
	}, [3]int{slotCount, 1, 1})
	s.records = s.d.readFloats(s.links)
}

func (s *streamLinks) ahead(index int) uint32 {
	return math.Float32bits(s.records[(index*linkRows+1)*4+1])
}

func (s *streamLinks) behind(index int) uint32 {
	return math.Float32bits(s.records[(index*linkRows+1)*4])
}

// A falling stream of 40 particles, born PRIMARY_LIFETIME/BASE_PRIMARY_COUNT
// apart and scattered over both banks of slot 0.
func fallingStream() []streamParticle {
	const count, gap, speed = 40, .48 / 512, 2.0
	now := count * gap
	particles := make([]streamParticle, count)
	for k := range particles {
		index := (91 * k) % 512
		if k%2 == 1 {
			index = 512 + (37*k)%512
		}
		birth := float64(k) * gap
		// Two particles born at once are ordered by their emission sequence.
		if k == 21 {
			birth = 20 * gap
		}
		particles[k] = streamParticle{
			index: index, position: vec3{0, 1 - (now-birth)*speed, 0}, velocity: vec3{0, -speed, 0},
			birth: birth, age: now - birth, generation: 1, sequence: uint32(1000 + k),
		}
	}
	return particles
}

// Particles join in the order they left the nozzle, whichever bank and slot
// they sit in, and only within the slot's current character and flow. A
// join that broke stays broken, until the particle is born again.
func TestStreamLinksFollowBirthOrder(t *testing.T) {
	s := newStreamLinks(t)
	stream := fallingStream()
	// A particle of the slot's former character, born among them, and one of
	// a later flow.
	stale := streamParticle{index: 700, position: vec3{0, .97, 0}, velocity: vec3{0, -2, 0},
		birth: stream[30].birth + 1e-5, generation: 0, sequence: 5}
	other := stream[10]
	other.index, other.flow, other.sequence, other.birth = 300, 1, 4000, stream[10].birth+1e-5
	s.run(append(append([]streamParticle{}, stream...), stale, other))

	for k := 0; k+1 < len(stream); k++ {
		want := uint32(stream[k+1].index)
		if k == 10 {
			want = linkNone
		}
		if got := s.ahead(stream[k].index); got != want {
			t.Errorf("particle %d links ahead to %#x, want %#x", k, got, want)
		}
	}
	if got := s.behind(stream[11].index); got != linkNone {
		t.Errorf("the particle after another flow joined %#x", got)
	}
	if got := s.ahead(stale.index); got != linkNone {
		t.Errorf("a particle of the former character joined %#x", got)
	}
	if radius := s.records[stream[5].index*linkRows*4+3]; radius <= 0 {
		t.Errorf("linked particle radius %v", radius)
	}

	// Particle 25 drifts away, then back.
	moved := append([]streamParticle{}, stream...)
	moved[25].position[0] += .1
	s.run(moved)
	if s.ahead(stream[24].index) != linkNone || s.ahead(stream[25].index) != linkNone {
		t.Fatal("particles a gap apart stayed joined")
	}
	s.run(stream)
	if s.ahead(stream[24].index) != linkNone || s.ahead(stream[25].index) != linkNone {
		t.Fatal("a broken join came back")
	}
	if s.ahead(stream[23].index) != uint32(stream[24].index) {
		t.Fatal("an unbroken join was lost")
	}

	// Born again into its slot, a particle joins the flow anew.
	reborn := append([]streamParticle{}, stream...)
	reborn[25].sequence = 9000
	s.run(reborn)
	if s.ahead(stream[24].index) != uint32(stream[25].index) {
		t.Fatal("a particle born again did not join")
	}
}

// A narrow nozzle's jet is released ballistic at birth (FluidSolver.hlsl
// frozenMode -1, lane BRANCH_JET) and flows as one tube, while a low-pressure
// flow scattering apart stays drops.
func TestStreamLinksJoinJetFlow(t *testing.T) {
	for _, flow := range []struct {
		name       string
		lane, mode float64
		joins      bool
	}{
		{"jet", -4, -1, true},
		{"scattering", 0, -.5, false},
	} {
		t.Run(flow.name, func(t *testing.T) {
			s := newStreamLinks(t)
			stream := fallingStream()
			for k := range stream {
				stream[k].lane, stream[k].mode = flow.lane, flow.mode
			}
			s.run(stream)
			for k := 0; k+1 < len(stream); k++ {
				if joined := s.ahead(stream[k].index) == uint32(stream[k+1].index); joined != flow.joins {
					t.Fatalf("particle %d joined %v", k, joined)
				}
			}
		})
	}
}
