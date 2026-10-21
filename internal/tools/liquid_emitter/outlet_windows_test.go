//go:build windows

package liquidemitter

import (
	"math"
	"testing"
	"unsafe"
)

// outletHarness runs EmitterState.hlsl's advanceEmitter and commitEmitter for
// one slot over a timeline of frames in a single dispatch, the way
// EmitterAdvance and OozeCommit do once a frame each.
const outletHarness = `
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "EmitterState.hlsl"
// Per frame: dt, flowing, nozzle kind, slot generation; amount, force,
// viscosity, pulse; root shift and which requests get a particle (bit 0, 1).
StructuredBuffer<float4> frames:register(t0);
// Per frame: the volume at the outlet after the commit, the volume the
// committed requests took, the volume turned away, drops committed; phase,
// requests waiting, the volume they hold, the flow generation.
RWStructuredBuffer<float4> results:register(u0);
[numthreads(1,1,1)]
void main(){
 uint count,stride;frames.GetDimensions(count,stride);
 EmitterRows st=(EmitterRows)0;
 st.r[EMITTER_OOZE].w=1;
 float now=0;
 [loop]for(uint f=0;f<count/3;f++){
  float4 a=frames[f*3],b=frames[f*3+1],c=frames[f*3+2];
  OutletInput o;
  o.dt=a.x;now+=a.x;o.now=now;o.live=true;o.flowing=a.y>.5;o.kind=(uint)a.z;o.generation=(uint)a.w;
  o.shift=c.xyz;o.amount=b.x;o.force=b.y;o.viscosity=b.z;o.pulse=b.w;
  o.outlet=float3(0,1,0);o.up=float3(0,0,1);
  o.surface=false;o.surfacePoint=0;o.surfaceNormal=float3(0,1,0);o.contactMode=0;
  float saturated;bool expired;
  advanceEmitter(st,o,saturated,expired);
  float r0=st.r[EMITTER_REQUEST].w,r1=st.r[EMITTER_REQUEST+REQUEST_ROWS].w;
  uint mask=(uint)c.w;
  bool d0=(mask&1)!=0&&r0>0,d1=(mask&2)!=0&&r1>0;
  uint delivered;bool waiting;
  commitEmitter(st,d0,d1,delivered,waiting);
  float left=st.r[EMITTER_REQUEST].w+st.r[EMITTER_REQUEST+REQUEST_ROWS].w;
  results[f*2]=float4(st.r[EMITTER_OOZE].x,(d0?r0:0)+(d1?r1:0),saturated,delivered);
  results[f*2+1]=float4(st.r[EMITTER_OOZE].y,(oozeRequested(st,0)?1:0)+(oozeRequested(st,1)?1:0),left,st.r[EMITTER_FLOW].y);
 }
}
`

// outletFrame is one frame of an outlet's timeline.
type outletFrame struct {
	dt                        float64
	flowing                   bool
	kind, generation          int
	amount, force, viscosity  float64
	pulse                     float64
	shift                     vec3
	deliverFirst, deliverNext bool
}

type outletResult struct {
	volume, taken, saturated float64
	drops                    int
	phase, waiting           int
	held                     float64
	flow                     int
}

func runOutlet(t *testing.T, frames []outletFrame) []outletResult {
	t.Helper()
	bundle := newShaderBundle(t, games[0].dir)
	code := bundle.compile(bundle.harness("OutletHarness.hlsl", outletHarness), "cs")
	d := newWARPDevice(t)
	input := make([]float32, 0, 12*len(frames))
	for _, f := range frames {
		mask := 0
		if f.deliverFirst {
			mask |= 1
		}
		if f.deliverNext {
			mask |= 2
		}
		input = append(input,
			float32(f.dt), boolFloat(f.flowing), float32(f.kind), float32(f.generation),
			float32(f.amount), float32(f.force), float32(f.viscosity), float32(f.pulse),
			float32(f.shift[0]), float32(f.shift[1]), float32(f.shift[2]), float32(mask))
	}
	results := d.structured(make([]float32, 8*len(frames)), 16)
	d.dispatch(d.shader("cs", code), bindings{
		srv: map[int]unsafe.Pointer{0: d.structured(input, 16).srv, 120: d.iniParams(nil)},
		uav: map[int]unsafe.Pointer{0: results.uav},
	}, [3]int{1, 1, 1})
	values := d.readFloats(results)
	out := make([]outletResult, len(frames))
	for index := range out {
		v := values[8*index:]
		out[index] = outletResult{
			volume: float64(v[0]), taken: float64(v[1]), saturated: float64(v[2]), drops: int(v[3]),
			phase: int(v[4]), waiting: int(v[5]), held: float64(v[6]), flow: int(v[7]),
		}
	}
	return out
}

func boolFloat(value bool) float32 {
	if value {
		return 1
	}
	return 0
}

// Slots.hlsl NOZZLE_OOZE, EmitterState.hlsl OOZE_INFLOW and OOZE_CAPACITY.
const nozzleOoze, oozeInflow, oozeCapacity = 2, 4.0, 4.0

func oozeTimeline(rate float64, seconds float64, change func(frame *outletFrame, time float64)) []outletFrame {
	count := int(math.Round(seconds * rate))
	frames := make([]outletFrame, count)
	for index := range frames {
		frames[index] = outletFrame{
			dt: 1 / rate, flowing: true, kind: nozzleOoze, generation: 1,
			amount: 1, force: 1, viscosity: 1, pulse: 1, deliverFirst: true, deliverNext: true,
		}
		if change != nil {
			change(&frames[index], float64(index)/rate)
		}
	}
	return frames
}

// The liquid an oozing outlet gathers leaves it only as drops that got a
// particle, so what flowed in is what left plus what still hangs there, at
// any frame rate; and the frame rate changes neither by more than a drop.
func TestOozeOutletKeepsVolumeAtAnyFrameRate(t *testing.T) {
	const seconds = 3.0
	inflow := oozeInflow * seconds
	var totals []float64
	var drops []int
	for _, rate := range []float64{30, 60, 120} {
		results := runOutlet(t, oozeTimeline(rate, seconds, nil))
		taken, count := 0.0, 0
		for _, r := range results {
			taken += r.taken
			count += r.drops
			if r.saturated != 0 {
				t.Fatalf("%v Hz: the outlet turned %v away below its capacity", rate, r.saturated)
			}
		}
		total := taken + results[len(results)-1].volume
		if math.Abs(total-inflow) > .01*inflow {
			t.Errorf(
				"%v Hz: %v left as drops and %v hangs, of %v that flowed in",
				rate,
				taken,
				results[len(results)-1].volume,
				inflow,
			)
		}
		totals = append(totals, total)
		drops = append(drops, count)
	}
	for index := 1; index < len(totals); index++ {
		if math.Abs(totals[index]-totals[0]) > .01*totals[0] {
			t.Errorf("volume differs between frame rates: %v", totals)
		}
		if diff := drops[index] - drops[0]; diff < -2 || diff > 2 {
			t.Errorf("drop counts differ between frame rates: %v", drops)
		}
	}
	if drops[0] < 4 {
		t.Fatalf("only %d drops in %v s", drops[0], seconds)
	}
}

// A drop no particle was free for stays requested and keeps its volume on
// the outlet; the outlet fills up to its capacity meanwhile and turns the
// rest away, and once particles are free it does not make up for the drops
// it could not release.
func TestOozeOutletWaitsForParticles(t *testing.T) {
	const rate = 60.0
	results := runOutlet(t, oozeTimeline(rate, 4, func(frame *outletFrame, time float64) {
		frame.amount = 2
		frame.deliverFirst = time < .5 || time >= 2.5
		frame.deliverNext = frame.deliverFirst
	}))
	waited, turnedAway := false, 0.0
	for index, r := range results {
		time := float64(index) / rate
		if r.volume > oozeCapacity+1e-4 {
			t.Fatalf("t=%.3f: %v gathered past the capacity", time, r.volume)
		}
		if r.waiting > 2 {
			t.Fatalf("t=%.3f: %d requests at once", time, r.waiting)
		}
		if time >= .5 && time < 2.5 {
			if r.taken != 0 {
				t.Fatalf("t=%.3f: a drop left without a particle", time)
			}
			waited = waited || r.waiting > 0
			turnedAway += r.saturated
		}
	}
	if !waited {
		t.Fatal("no request waited while no particle was free")
	}
	if turnedAway <= 0 {
		t.Fatal("the full outlet took everything that flowed")
	}
	// Once particles are free again, at most two requests leave a frame.
	for index := int(2.5 * rate); index < len(results); index++ {
		if results[index].drops > 2 {
			t.Fatalf("t=%.3f: %d drops in one frame", float64(index)/rate, results[index].drops)
		}
	}
	resume := int(2.5 * rate)
	released := 0.0
	for _, r := range results[resume:] {
		released += r.taken
	}
	held, inflow := results[resume-1].volume, oozeInflow*2*float64(len(results)-resume)/rate
	if released+results[len(results)-1].volume > held+inflow+1e-3 {
		t.Fatalf("released %v after the wait from %v held and %v new flow", released, held, inflow)
	}
}

// A slot handed to another character and a root that jumped start the
// outlet empty, with a new flow; another nozzle lets the liquid left at the
// outlet fade over MODE_FADE without releasing it.
func TestOozeOutletResets(t *testing.T) {
	const rate = 60.0
	results := runOutlet(t, oozeTimeline(rate, 1.5, func(frame *outletFrame, time float64) {
		frame.deliverFirst, frame.deliverNext = false, false
		switch {
		case time >= .3 && time < .6:
			frame.generation = 2
		case time >= .6 && math.Abs(time-.8) < .5/rate:
			frame.generation = 2
			frame.shift = vec3{2, 0, 0}
		case time >= .6 && time < 1.2:
			frame.generation = 2
		case time >= 1.2:
			frame.generation, frame.kind = 2, 0
		}
	}))
	at := func(time float64) outletResult { return results[int(math.Round(time*rate))] }
	if before, after := at(.3-1/rate), at(.3); after.volume >= before.volume || after.flow == before.flow {
		t.Errorf("a new owner kept the outlet: %+v then %+v", before, after)
	}
	if before, after := at(.8-1/rate), at(.8); after.volume >= before.volume || after.flow == before.flow {
		t.Errorf("a teleport kept the outlet: %+v then %+v", before, after)
	}
	if after := at(1.2); after.waiting != 0 {
		t.Errorf("another nozzle kept %d requests", after.waiting)
	}
	if faded := at(1.2 + .15 + 2/rate); faded.volume != 0 {
		t.Errorf("liquid of the former nozzle still at the outlet after the fade: %v", faded.volume)
	}
	for _, r := range results[int(1.2*rate):] {
		if r.taken != 0 {
			t.Fatal("liquid of the former nozzle left as a drop")
		}
	}
}
