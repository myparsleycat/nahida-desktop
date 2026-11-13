//go:build windows

package liquidemitter

import (
	"math"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestBodyCollisionWorksWithoutSceneDepth(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	code := bundle.compile(bundle.harness("BodyCollisionFixture.hlsl", `
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "Collision.hlsl"
RWStructuredBuffer<float4> result:register(u0);
[numthreads(1,1,1)]
void main(){float3 at,n;bool hit=collideFluid(0,float3(.2,.2,.01),float3(.2,.2,-.01),float3(0,0,-1),.001,at,n);result[0]=float4(at,hit?1:0);}
`), "cs")
	for _, stamp := range []float32{1, .8} {
		d := newWARPDevice(t)
		atlas := make([]float32, 8*14*4)
		row := func(i int, v [4]float32) { copy(atlas[i*4:], v[:]) }
		row(0, [4]float32{0, 0, 0, stamp})
		row(1, [4]float32{1, 1, 9, 1})
		row(2, [4]float32{0, 0, 0, 1})
		row(3, [4]float32{1, 0, 0, 1})
		row(4, [4]float32{0, 1, 0, 1})
		row(8, [4]float32{0, 0, 0, 0})
		row(9, [4]float32{1, 1, 0, 0})
		row(10, [4]float32{0, 0, 0, 0})
		row(11, [4]float32{1, 1, 0, 0})
		row(12, [4]float32{1e30, 1e30, 1e30, 0})
		row(13, [4]float32{-1e30, -1e30, -1e30, 0})
		result := d.structured(make([]float32, 4), 16)
		d.dispatch(
			d.shader("cs", code),
			bindings{
				srv: map[int]unsafe.Pointer{
					103: d.structured(liveSlots(0), 16).srv,
					114: d.structured(atlas, 16).srv,
					115: d.uints([]uint32{1}).srv,
					120: d.iniParams(map[int][4]float32{239: {0, 1, 0, 0}}),
				},
				uav: map[int]unsafe.Pointer{0: result.uav},
			},
			[3]int{1, 1, 1},
		)
		values := d.readFloats(result)
		if (values[3] > .5) != (stamp == 1) {
			t.Fatalf("capture %g collision: %v", stamp, values)
		}
		if stamp == 1 && math.Abs(float64(values[2]-.0011)) > 1e-5 {
			t.Fatalf("wrong body collision: %v", values)
		}
	}
}

func TestBodyCaptureSharesDrawButKeepsDistinctModels(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	d := newWARPDevice(t)
	code := d.shader("cs", bundle.compile(filepath.Join(bundle.root, "bodymesh", "Prepare.hlsl"), "cs"))
	stats := make([]float32, 40)
	copy(stats[36:], []float32{12, 0, 1, 0})
	work := d.uints(make([]uint32, 16))
	registry := d.structured(make([]float32, 9*4), 16)
	dispatch := func(model uint32) {
		d.dispatch(
			code,
			bindings{
				srv: map[int]unsafe.Pointer{
					0:   d.structured(make([]float32, 8*14*4), 16).srv,
					1:   d.structured([]float32{1, 9, 0, 0, -1, -1, -1, 0}, 16).srv,
					2:   d.structured(stats, 16).srv,
					3:   d.structured(make([]float32, (1+16*9)*4), 16).srv,
					4:   d.uints([]uint32{model}).srv,
					120: d.iniParams(map[int][4]float32{225: {1, 1, 12, 0}, 243: {0, 0, -2, 0}}),
				},
				uav: map[int]unsafe.Pointer{0: work.uav, 1: registry.uav},
			},
			[3]int{1, 1, 1},
		)
	}
	dispatch(1)
	args := d.readUints(work)
	if args[0] != 3 || args[3] != math.MaxUint32-1 || args[4] != 12 || args[15] != 1 {
		t.Fatalf("incorrect indexed instance arguments: %v", args)
	}
	dispatch(1)
	if args = d.readUints(work); args[15] != 0 {
		t.Fatalf("duplicate draw captured twice: %v", args)
	}
	dispatch(2)
	if args = d.readUints(work); args[14] != 1 || args[15] != 1 {
		t.Fatalf("different model shared capture: %v", args)
	}
}

func TestBodyBindingFollowsDeformationAndRejectsOtherOwners(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	code := bundle.compile(bundle.harness("BodyBindingFixture.hlsl", `
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#define BODY_FACES 2
#define BODY_LEAVES 2
#define BODY_STRIDE 18
#include "../bodymesh/Attachment.hlsl"
RWStructuredBuffer<float4> result:register(u0);
[numthreads(1,1,1)]
void main(){
 float3 at=float3(.2,.2,.0011),normal=float3(0,0,1);uint mesh;
 bool held=bodyReuse(0,0,7,.001,at,normal,mesh);
 float3 to=0,n=0;bool boundary=false;
 bool moved=false;if(held)moved=bodyFollowDetailed(0,mesh,float3(.8,.8,.0011),.001,to,n,boundary);
 result[0]=float4(to,moved?1:0);result[1]=float4(normal,held?1:0);
}`), "cs")
	for _, test := range []struct {
		name              string
		root, stamp, seam float32
		generation        float32
		held, moved       bool
	}{
		{name: "adjacent faces", stamp: 1, generation: 1, held: true, moved: true},
		{name: "other actor", root: .2, stamp: 1, generation: 1},
		{name: "stale capture", stamp: .8, generation: 1},
		{name: "reused slot", stamp: 1, generation: 2},
		{name: "animated seam opens", stamp: 1, seam: .001, generation: 1, held: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newWARPDevice(t)
			atlas := make([]float32, 8*18*4)
			row := func(i int, v [4]float32) { copy(atlas[i*4:], v[:]) }
			row(0, [4]float32{test.root, 0, 0, test.stamp})
			row(1, [4]float32{1, 2, 9, 1})
			row(2, [4]float32{0, 0, 0, 1})
			row(3, [4]float32{1, 0, 0, 1})
			row(4, [4]float32{0, 1, 0, 1})
			row(5, [4]float32{1, -1, -1, 0})
			row(6, [4]float32{1, 0, test.seam, 1})
			row(7, [4]float32{1, 1, test.seam, 1})
			row(8, [4]float32{0, 1, test.seam, 1})
			row(9, [4]float32{-1, 0, -1, 0})
			slots := liveSlots(0)
			slots[slotGenerationRow*4] = test.generation
			binding := d.structured([]float32{1, 0, 7, 1, .6, .2, 1, 9, 1, 0, 0, 0}, 16)
			output := d.structured(make([]float32, 8), 16)
			d.dispatch(
				d.shader("cs", code),
				bindings{
					srv: map[int]unsafe.Pointer{
						103: d.structured(slots, 16).srv,
						110: d.structured(atlas, 16).srv,
						111: d.uints([]uint32{1}).srv,
						120: d.iniParams(map[int][4]float32{239: {0, 1, 0, 0}}),
					},
					uav: map[int]unsafe.Pointer{0: output.uav, 1: binding.uav},
				},
				[3]int{1, 1, 1},
			)
			values := d.readFloats(output)
			if (values[7] > .5) != test.held || (values[3] > .5) != test.moved {
				t.Fatalf("contact = %v", values)
			}
			if test.moved && (math.Abs(float64(values[0]-.8)) > 1e-5 || math.Abs(float64(values[1]-.8)) > 1e-5) {
				t.Fatalf("wrong followed point: %v", values)
			}
		})
	}
}

func TestBodyAcquisitionReusesBindingAcrossSlowFrames(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	consumer := bundle.compile(bundle.harness("AcquiredBindingFixture.hlsl", `
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
#include "../bodymesh/Attachment.hlsl"
RWStructuredBuffer<float4> result:register(u0);
[numthreads(1,1,1)]
void main(){
 float3 at=float3(.2,.2,IniParams[0].x+.0001),normal=float3(0,0,1);uint mesh;
 bool held=bodyReuse(0,0,.5,IniParams[0].x,at,normal,mesh);
 result[0]=float4(at,held?1:0);
}`), "cs")
	for _, shader := range []string{"BodyAcquire.hlsl", "YogurtAcquire.hlsl"} {
		t.Run(shader, func(t *testing.T) {
			code := bundle.compile(filepath.Join(bundle.root, "fluid", shader), "cs")
			for _, test := range []struct {
				name       string
				gap, stamp float32
				held       bool
			}{
				{name: "fast frame", gap: .02, stamp: 1, held: true},
				{name: "20 FPS", gap: .05, stamp: 1, held: true},
				{name: "8 FPS", gap: .125, stamp: 1, held: true},
				{name: "stale capture", gap: .125, stamp: .8},
			} {
				t.Run(test.name, func(t *testing.T) {
					d := newWARPDevice(t)
					radius := float32(.0028)
					parcels := make([]float32, 6144*16)
					copy(parcels, []float32{.2, .2, radius + .0001, 0, 0, 0, 0, 1, 0, 0, 1, 4, 0, .5, 1, 4})
					stride := 64
					if shader == "YogurtAcquire.hlsl" {
						radius = .0002
						parcels = make([]float32, 2048*28)
						copy(parcels, []float32{.2, .2, radius + .0001, .00325, 0, 0, 0, 1, 0, 0, 1, 2, 0, .5, 2, 0})
						parcels[27] = 1
						stride = 112
					}
					atlas := make([]float32, 8*14*4)
					copy(atlas, []float32{
						0, 0, 0, test.stamp, 1, 1, 9, 1,
						.1, 0, 0, 1, 1.1, 0, 0, 1, .1, 1, 0, 1, -1, -1, -1, 0,
					})
					for _, row := range []int{8, 10} {
						copy(atlas[row*4:], []float32{.1, 0, 0, 0, 1.1, 1, 0, 0})
					}
					state := liveSlots(0)
					state[14*4] = 3
					binding := d.structured([]float32{1, 0, .5, 1, .6, .2, 1 - test.gap, 9, 1, 0, 0, 0}, 16)
					params := d.iniParams(map[int][4]float32{0: {radius}, 239: {1.0 / 60, 1}})
					views := bindings{
						srv: map[int]unsafe.Pointer{
							100: d.structured(parcels, stride).srv, 103: d.structured(state, 16).srv,
							110: d.structured(atlas, 16).srv, 111: d.uints([]uint32{1}).srv, 120: params,
						},
						uav: map[int]unsafe.Pointer{1: binding.uav},
					}
					d.dispatch(d.shader("cs", code), views, [3]int{1, 1, 1})
					result := d.structured(make([]float32, 4), 16)
					views.uav[0] = result.uav
					d.dispatch(d.shader("cs", consumer), views, [3]int{1, 1, 1})
					got := d.readFloats(result)
					if (got[3] > .5) != test.held {
						t.Fatalf("binding reuse = %v, want held %v", got, test.held)
					}
					if test.held && math.Abs(float64(got[0]-.3)) > 1e-5 {
						t.Fatalf("lost material point on deformed body: %v, want x=.3", got)
					}
				})
			}
		})
	}
}
