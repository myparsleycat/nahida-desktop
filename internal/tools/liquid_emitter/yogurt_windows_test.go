//go:build windows

package liquidemitter

import (
	"io/fs"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func yogurtTestParcels(depths ...float32) []float32 {
	parcels := make([]float32, 2048*28)
	for i, depth := range depths {
		copy(parcels[i*28:], []float32{0, 0, depth, .00325, 0, 0, 0, 1, 0, 1, 0, 0, 0, .5, 2, 0, 20})
		parcels[i*28+27] = 1
	}
	return parcels
}

func drawYogurtTestSurface(t *testing.T, bundle *shaderBundle, d *warpDevice,
	parcels []float32, transparency, depth float32,
) []gpuTexture {
	t.Helper()
	camera := d.structured(newTestCamera(vec3{0, 0, -1}, vec3{}, .1, 1, .01, 10).rows(), 16).srv
	slots := liveSlots(0)
	slots[16*4] = 1
	slots[16*4+2] = 1
	slots[17*4+2] = transparency
	params := d.iniParams(map[int][4]float32{239: {1.0 / 120, 1}, 251: {.8, .7, .6, 0}})
	file := filepath.Join(bundle.root, "fluid", "YogurtSurface.hlsl")
	targets := []gpuTexture{
		d.texture2D(1, 1, formatR32G32Float, 8, nil, true),
		d.texture2D(1, 1, formatR32G32B32A32Float, 16, nil, true),
		d.texture2D(1, 1, formatR32G32B32A32Float, 16, nil, true),
	}
	for _, target := range targets {
		d.clearTarget(target, [4]float32{})
	}
	d.draw(d.shader("vs", bundle.compile(file, "vs")), d.shader("ps", bundle.compile(file, "ps")),
		bindings{srv: map[int]unsafe.Pointer{
			100: d.structured(parcels, 112).srv, 103: d.structured(slots, 16).srv, 113: camera, 120: params,
		}},
		bindings{srv: map[int]unsafe.Pointer{
			104: d.floatTexture(1, 1, []float32{depth}).srv, 113: camera, 120: params,
		}}, targets, 24)
	return targets
}

func compositeYogurtTestSurface(t *testing.T, bundle *shaderBundle, d *warpDevice, fields []gpuTexture) float32 {
	t.Helper()
	file := filepath.Join(bundle.root, "fluid", "YogurtComposite.hlsl")
	target := d.texture2D(1, 1, formatR32G32B32A32Float, 16, nil, true)
	d.clearTarget(target, [4]float32{})
	d.draw(d.shader("vs", bundle.compile(file, "vs")), d.shader("ps", bundle.compile(file, "ps")), bindings{},
		bindings{srv: map[int]unsafe.Pointer{
			100: fields[0].srv, 101: fields[1].srv, 102: fields[2].srv,
			103: d.floatTexture(1, 1, []float32{tagDepth(1.2, 63)}).srv,
			105: d.structured([]float32{1, 1, 1, 0, 1, 1, 1, 0}, 16).srv,
		}}, []gpuTexture{target}, 3)
	return bytesFloat32(d.readTexture(target, 16))[3]
}

func TestYogurtSurfaceRejectsOccludedFragmentsBeforeAggregation(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	d := newWARPDevice(t)
	// Match the runtime's additive MRT blending so a hidden parcel cannot
	// change the visible parcel's field, thickness, normal, or color.
	var desc struct {
		alphaToCoverage, independent uint32
		targets                      [8][8]uint32
	}
	desc.targets[0] = [8]uint32{1, 2, 2, 1, 2, 2, 1, 15}
	var blend unsafe.Pointer
	hr, _, _ := syscall.SyscallN(comMethod(d.device, 20), uintptr(d.device),
		uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(&blend)))
	d.keep(blend, "create additive blend state", hr)
	setBlend := func(state unsafe.Pointer) {
		_, _, _ = syscall.SyscallN(comMethod(d.context, 35), uintptr(d.context), uintptr(state), 0, 0xffffffff)
	}
	setBlend(blend)
	for _, test := range []struct {
		name   string
		depths []float32
	}{
		{name: "front and hidden parcel", depths: []float32{0, .8}},
		{name: "hidden and front parcel", depths: []float32{.8, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reference := yogurtTestParcels(test.depths...)
			for i, depth := range test.depths {
				if depth > 0 {
					reference[i*28+3] = 0
				}
			}
			front := drawYogurtTestSurface(t, bundle, d, reference, 1, tagDepth(1.2, 63))
			mixed := drawYogurtTestSurface(t, bundle, d, yogurtTestParcels(test.depths...), 1, tagDepth(1.2, 63))
			for i, target := range mixed {
				texel := 16
				if i == 0 {
					texel = 8
				}
				got, want := bytesFloat32(d.readTexture(target, texel)), bytesFloat32(d.readTexture(front[i], texel))
				if !closeFloats(got, want) {
					t.Errorf("MRT %d contains hidden parcel: %v, want %v", i, got, want)
				}
			}
			setBlend(nil)
			if alpha := compositeYogurtTestSurface(t, bundle, d, mixed); alpha <= .5 {
				t.Errorf("visible front parcel disappeared: alpha=%g", alpha)
			}
			setBlend(blend)
		})
	}
	for _, depth := range []float32{1e30, tagDepth(1.8, 63)} {
		visible := drawYogurtTestSurface(t, bundle, d, yogurtTestParcels(0), 1, depth)
		if weight := bytesFloat32(d.readTexture(visible[0], 8))[1]; weight < .99 {
			t.Errorf("unoccluded parcel rejected at fluid depth %g: weight=%g", depth, weight)
		}
	}
	setBlend(nil)
}

func TestYogurtOpacityIsContinuousAndMonotone(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	d := newWARPDevice(t)
	for _, radius := range []float32{.0005, .002, .006} {
		parcels := yogurtTestParcels(0)
		parcels[3] = radius
		previous := float32(-1)
		for _, opacity := range []float32{0, .25, .5, .75, .99, .999, 1, 1.001, 1.01, 1.25, 1.5, 1.75, 2} {
			fields := drawYogurtTestSurface(t, bundle, d, parcels, 2-opacity, 1e30)
			alpha := compositeYogurtTestSurface(t, bundle, d, fields)
			if math.IsNaN(float64(alpha)) || alpha < 0 || alpha > 1 || alpha < previous-1e-5 {
				t.Errorf("radius %g opacity %g: alpha %g after %g", radius, opacity, alpha, previous)
			}
			if opacity >= .999 && opacity <= 1.01 && previous >= 0 && math.Abs(float64(alpha-previous)) > .02 {
				t.Errorf("radius %g opacity %g: discontinuous alpha %g -> %g", radius, opacity, previous, alpha)
			}
			if (opacity == 0 && alpha != 0) || (opacity == 2 && math.Abs(float64(alpha-1)) > 1e-5) {
				t.Errorf("radius %g opacity %g: endpoint alpha=%g", radius, opacity, alpha)
			}
			previous = alpha
		}
	}
}

func TestYogurtDetachPreservesWorldPositionAcrossRootMotion(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	d := newWARPDevice(t)
	parcels := yogurtTestParcels(.01)
	parcels[0], parcels[1], parcels[11] = .2, 1, 2
	slots := liveSlots(0)
	slots[slotRootRow*4] = .3
	slots[14*4] = 3
	frame := make([]float32, 8*4*4)
	frame[0], frame[3] = .1, 1
	target := d.structured(make([]float32, len(parcels)), 112)
	d.dispatch(d.shader("cs", bundle.compile(filepath.Join(bundle.root, "fluid", "YogurtUpdate.hlsl"), "cs")),
		bindings{
			srv: map[int]unsafe.Pointer{
				100: d.structured(parcels, 112).srv, 103: d.structured(slots, 16).srv,
				105: d.structured(frame, 16).srv, 120: d.iniParams(map[int][4]float32{239: {.01, 1}, 240: {0, 0, 1}}),
			},
			uav: map[int]unsafe.Pointer{0: target.uav, 1: d.structured(make([]float32, 2048*12), 16).uav},
		}, [3]int{1, 1, 1})
	got := d.readFloats(target)
	if got[11] != 0 || got[3] <= 0 || math.Abs(float64(got[0]+.3-(parcels[0]+.1))) > 1e-6 {
		t.Fatalf("detach changed world position during root motion: %v", got[:12])
	}
}

func TestYogurtScreenResourcesAndDepthBinding(t *testing.T) {
	for _, game := range games {
		t.Run(game.dir, func(t *testing.T) {
			files, err := runtimeFiles(game.dir)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := fs.ReadFile(files, "Probe.ini")
			if err != nil {
				t.Fatal(err)
			}
			sections := map[string][]string{}
			section := ""
			for line := range strings.SplitSeq(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "[") {
					section = strings.Trim(line, "[]")
				} else {
					sections[section] = append(sections[section], line)
				}
			}
			for _, resource := range []string{"ResourceYogurtField", "ResourceYogurtShape", "ResourceYogurtColor"} {
				if !slices.Contains(sections["CommandListReleaseScreen"], resource+" = null") {
					t.Errorf("screen release retains %s", resource)
				}
			}
			if !slices.Contains(sections["CustomShaderYogurtSurface"], "ps-t104 = ResourceFluidDepthA") ||
				!slices.Contains(sections["CustomShaderYogurtSurface"], "ps-t104 = null") {
				t.Error("Yogurt surface must bind and unbind ordinary fluid depth at ps-t104")
			}
			for _, command := range []string{"ResourceSavedPsT104 = ref ps-t104", "ps-t104 = ref ResourceSavedPsT104"} {
				if !slices.Contains(sections["CommandListInjectWorld"], command) {
					t.Errorf("injection does not preserve ordinary fluid depth slot: %s", command)
				}
			}
		})
	}
}

func TestYogurtExchangeConservesDenseParcelVolume(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	for _, owners := range []int{1, 4, 8} {
		t.Run(string(rune('0'+owners))+" owners", func(t *testing.T) {
			d := newWARPDevice(t)
			slots := liveSlots(0, 1, 2, 3, 4, 5, 6, 7)
			for s := range 8 {
				slots[(s*slotRows+14)*4] = 3
			}
			parcels := make([]float32, 2048*28)
			for owner := range owners {
				for i := range 16 {
					at := (owner*16 + i) * 28
					parcels[at+1] = 1
					parcels[at+3] = .002 + float32(i)*.00002
					parcels[at+7] = float32(owner + 1)
					parcels[at+9] = 1
					parcels[at+11] = 2
					parcels[at+13] = .5
					parcels[at+16] = 20
					parcels[at+27] = 1
				}
			}
			source := d.structured(parcels, 112)
			exchange := d.structured(make([]float32, 2048*4), 16)
			params := d.iniParams(map[int][4]float32{239: {1.0 / 120, 1, 0, 0}, 240: {0, 0, 1, 0}})
			state := d.structured(slots, 16).srv
			d.dispatch(
				d.shader("cs", bundle.compile(filepath.Join(bundle.root, "fluid", "YogurtExchange.hlsl"), "cs")),
				bindings{
					srv: map[int]unsafe.Pointer{100: source.srv, 103: state, 120: params},
					uav: map[int]unsafe.Pointer{0: exchange.uav},
				},
				[3]int{32, 1, 1},
			)
			transfers := d.readFloats(exchange)
			moved := false
			for i := range owners * 16 {
				receiver := int(transfers[i*4])
				if transfers[i*4+1] > 0 {
					moved = true
					if receiver < 0 || receiver/16 != i/16 {
						t.Fatal("liquid transferred to another owner")
					}
				}
			}
			if !moved {
				t.Fatal("fixture did not merge any liquid")
			}
			target := d.structured(make([]float32, 2048*28), 112)
			d.dispatch(
				d.shader("cs", bundle.compile(filepath.Join(bundle.root, "fluid", "YogurtUpdate.hlsl"), "cs")),
				bindings{
					srv: map[int]unsafe.Pointer{100: source.srv, 102: exchange.srv, 103: state, 120: params},
					uav: map[int]unsafe.Pointer{
						0: target.uav,
						1: d.structured(make([]float32, 2048*12), 16).uav,
						2: d.structured(make([]float32, 128*28), 112).uav,
						3: d.uints([]uint32{0, 0, 128}).uav,
					},
				},
				[3]int{32, 1, 1},
			)
			updated := d.readFloats(target)
			for owner := range owners {
				var before, after float64
				for i := range 16 {
					index := (owner*16 + i) * 28
					r := float64(parcels[index+3])
					before += r * r * r
					r = float64(updated[index+3])
					after += r * r * r
				}
				if math.Abs(before-after) > before*1e-5 {
					t.Fatalf("owner %d volume changed: %g -> %g", owner, before, after)
				}
			}
			slots[slotGenerationRow*4] = 2
			d.dispatch(
				d.shader("cs", bundle.compile(filepath.Join(bundle.root, "fluid", "YogurtUpdate.hlsl"), "cs")),
				bindings{
					srv: map[int]unsafe.Pointer{
						100: source.srv,
						102: exchange.srv,
						103: d.structured(slots, 16).srv,
						120: params,
					},
					uav: map[int]unsafe.Pointer{0: target.uav},
				},
				[3]int{32, 1, 1},
			)
			updated = d.readFloats(target)
			for i := range 16 {
				if updated[i*28+3] != 0 {
					t.Fatal("reused slot retained old yogurt")
				}
			}
		})
	}
}

func TestYogurtEmissionSkipsInactiveSlotsAndRespectsCapacity(t *testing.T) {
	bundle := newShaderBundle(t, "wwmi")
	d := newWARPDevice(t)
	code := d.shader("cs", bundle.compile(filepath.Join(bundle.root, "fluid", "YogurtEmit.hlsl"), "cs"))
	for _, active := range []bool{false, true} {
		slots := liveSlots(0)
		slots[3] = 1
		slots[4*4+3] = 1
		if active {
			slots[14*4] = 3
		}
		counters := d.uints([]uint32{0, 0, 0})
		args := d.uints(make([]uint32, 16))
		state := d.structured(make([]float32, 8*4), 16)
		stats := d.uints([]uint32{2048, 0, 0, 0})
		if !active {
			stats = d.uints(make([]uint32, 4))
		}
		d.dispatch(
			code,
			bindings{
				srv: map[int]unsafe.Pointer{
					103: d.structured(slots, 16).srv,
					120: d.iniParams(
						map[int][4]float32{
							237: {1, 0, 0, 0},
							239: {.03, 1, 0, 0},
							240: {0, 0, 1, 0},
							241: {1, 0, 0, 0},
						},
					),
				},
				uav: map[int]unsafe.Pointer{
					2: d.structured(make([]float32, 128*28), 112).uav,
					3: counters.uav,
					4: stats.uav,
					5: state.uav,
					6: args.uav,
				},
			},
			[3]int{1, 1, 1},
		)
		counts := d.readUints(counters)
		work := d.readUints(args)
		if active {
			if counts[2] != 0 {
				t.Fatal("full pool admitted requests")
			}
		} else if work[0] != 0 || work[4] != 0 || work[8] != 0 {
			t.Fatalf("inactive mode scheduled work: %v", work)
		}
	}
}
