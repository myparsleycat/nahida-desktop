//go:build windows

package liquidemitter

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"unsafe"
)

// tubeHarness traces rays through links of StreamTube.hlsl: per ray, the
// union length of each of up to two links.
const tubeHarness = `
#include "StreamTube.hlsl"
// Per link: start and radius, end and radius, tangents at both, caps (bit 0
// start, bit 1 end) in the start tangent's w.
StructuredBuffer<float4> tubeLinks:register(t0);
// Per ray: origin, direction.
StructuredBuffer<float4> rays:register(t1);
RWStructuredBuffer<float4> lengths:register(u0);
[numthreads(1,1,1)]
void main(uint3 id:SV_GroupID){
 float3 o=rays[id.x*2].xyz,d=normalize(rays[id.x*2+1].xyz);
 float4 total=0;
 [unroll]for(uint k=0;k<2;k++){
  float4 a=tubeLinks[k*4],b=tubeLinks[k*4+1],ta=tubeLinks[k*4+2],tb=tubeLinks[k*4+3];
  float2 spans[TUBE_SPANS];
  linkSpans(o-a.xyz,d,0,a.w,ta.xyz,b.xyz-a.xyz,b.w,tb.xyz,tubePieces(a.xyz,ta.xyz,b.xyz,tb.xyz),
   ((uint)ta.w&1)!=0,((uint)ta.w&2)!=0,spans);
  float2 first;
  total[k]=spanUnion(spans,0,first);
 }
 lengths[id.x]=total;
}
`

type tubeLink struct {
	start, end         vec3
	radius             float64
	startCap, endCap   bool
	startTangent, endT vec3
}

func traceTubes(t *testing.T, links [2]tubeLink, rays [][2]vec3) [][2]float64 {
	t.Helper()
	bundle := newShaderBundle(t, games[0].dir)
	code := bundle.compile(bundle.harness("TubeHarness.hlsl", tubeHarness), "cs")
	d := newWARPDevice(t)
	var linkRows, rayRows []float32
	for _, l := range links {
		caps := 0
		if l.startCap {
			caps |= 1
		}
		if l.endCap {
			caps |= 2
		}
		linkRows = append(linkRows,
			float32(l.start[0]), float32(l.start[1]), float32(l.start[2]), float32(l.radius),
			float32(l.end[0]), float32(l.end[1]), float32(l.end[2]), float32(l.radius),
			float32(l.startTangent[0]), float32(l.startTangent[1]), float32(l.startTangent[2]), float32(caps),
			float32(l.endT[0]), float32(l.endT[1]), float32(l.endT[2]), 0)
	}
	for _, r := range rays {
		rayRows = append(rayRows, float32(r[0][0]), float32(r[0][1]), float32(r[0][2]), 0,
			float32(r[1][0]), float32(r[1][1]), float32(r[1][2]), 0)
	}
	out := d.structured(make([]float32, 4*len(rays)), 16)
	d.dispatch(d.shader("cs", code), bindings{
		srv: map[int]unsafe.Pointer{0: d.structured(linkRows, 16).srv, 1: d.structured(rayRows, 16).srv},
		uav: map[int]unsafe.Pointer{0: out.uav},
	}, [3]int{len(rays), 1, 1})
	values := d.readFloats(out)
	lengths := make([][2]float64, len(rays))
	for index := range lengths {
		lengths[index] = [2]float64{float64(values[4*index]), float64(values[4*index+1])}
	}
	return lengths
}

// A chain of links is one tube: rays across it cross it once, a cap adds
// only where it sticks out of the chain's end, and where two links meet
// neither adds the other's share.
func TestStreamTubeCountsJointsOnce(t *testing.T) {
	const r, step = .002, .01
	along := vec3{step, 0, 0}
	links := [2]tubeLink{
		{start: vec3{0, 0, 0}, end: vec3{step, 0, 0}, radius: r, startCap: true, startTangent: along, endT: along},
		{start: vec3{step, 0, 0}, end: vec3{2 * step, 0, 0}, radius: r, endCap: true, startTangent: along, endT: along},
	}
	oblique := vec3{1, 0, -1}.normalized()
	rays := [][2]vec3{
		// Across the first link's middle.
		{vec3{step / 2, 0, 1}, vec3{0, 0, -1}},
		// Down the whole chain's axis.
		{vec3{-1, 0, 0}, vec3{1, 0, 0}},
		// Through the joint at 45 degrees.
		{vec3{step, 0, 0}.sub(oblique), oblique},
		// Across the joint itself.
		{vec3{step, 0, 1}, vec3{0, 0, -1}},
	}
	got := traceTubes(t, links, rays)
	near := func(value, want float64) bool { return math.Abs(value-want) < 2e-6+1e-3*want }
	if !near(got[0][0], 2*r) || got[0][1] != 0 {
		t.Errorf("across a link: %v, want %v", got[0], 2*r)
	}
	if sum := got[1][0] + got[1][1]; !near(sum, 2*step+2*r) {
		t.Errorf("along the chain: %v, want %v", sum, 2*step+2*r)
	}
	if sum := got[2][0] + got[2][1]; !near(sum, 2*r*math.Sqrt2) {
		t.Errorf("through the joint: %v, want %v", sum, 2*r*math.Sqrt2)
	}
	if sum := got[3][0] + got[3][1]; !near(sum, 2*r) {
		t.Errorf("across the joint: %v, want %v", sum, 2*r)
	}
}

// rectHarness runs SSFRCommon.hlsl screenRect per sphere.
const rectHarness = `
#include "SSFRCommon.hlsl"
StructuredBuffer<float4> spheres:register(t0);
RWStructuredBuffer<float4> rects:register(u0);
[numthreads(1,1,1)]
void main(uint3 id:SV_GroupID){
 float4 sphere=spheres[id.x],rect;
 bool visible=screenRect(sphere.xyz,sphere.w,rect);
 rects[id.x]=visible?rect:float4(9,9,9,9);
}
`

// A drop is drawn by the screen rectangle around it, however near: once it
// reaches the near plane the rectangle is the whole screen, and only a drop
// wholly behind it or off the screen is left out.
func TestScreenRectNearPlane(t *testing.T) {
	bundle := newShaderBundle(t, games[0].dir)
	code := bundle.compile(bundle.harness("RectHarness.hlsl", rectHarness), "cs")
	d := newWARPDevice(t)
	camera := newTestCamera(vec3{0, 1, 1}, vec3{0, 1, 0}, math.Pi/3, 1, .1, 100)
	spheres := []float32{
		0, 1, 0, .01, // ahead
		0, 1, .96, .02, // reaching the near plane (view distance .05)
		0, 1, 1, .2, // around the eye
		0, 1, 1.5, .1, // behind the eye
		5, 1, 0, .1, // off to the side
	}
	out := d.structured(make([]float32, len(spheres)), 16)
	d.dispatch(d.shader("cs", code), bindings{
		srv: map[int]unsafe.Pointer{
			0:   d.structured(spheres, 16).srv,
			113: d.structured(camera.rows(), 16).srv,
			120: d.iniParams(nil),
		},
		uav: map[int]unsafe.Pointer{0: out.uav},
	}, [3]int{len(spheres) / 4, 1, 1})
	rects := d.readFloats(out)
	rect := func(index int) [4]float32 { return [4]float32(rects[4*index : 4*index+4]) }
	if r := rect(0); !(r[0] < 0 && r[2] > 0 && r[1] < 0 && r[3] > 0 && r[2]-r[0] < .5) {
		t.Errorf("a drop ahead: %v", r)
	}
	full := [4]float32{-1, -1, 1, 1}
	if r := rect(1); r != full {
		t.Errorf("a drop at the near plane: %v", r)
	}
	if r := rect(2); r != full {
		t.Errorf("a drop around the eye: %v", r)
	}
	hidden := [4]float32{9, 9, 9, 9}
	if r := rect(3); r != hidden {
		t.Errorf("a drop behind the eye: %v", r)
	}
	if r := rect(4); r != hidden {
		t.Errorf("a drop off the screen: %v", r)
	}
}

// A drop the near plane cuts is drawn from there to where the ray leaves it,
// where it formerly vanished: its entry lay before the near plane, and its
// size on screen culled it.
func TestSurfaceDrawsDropCutByNearPlane(t *testing.T) {
	const width, height = 16, 16
	bundle := newShaderBundle(t, games[0].dir)
	file := filepath.Join(bundle.root, "fluid", "FluidSurface.hlsl")
	d := newWARPDevice(t)
	camera := newTestCamera(vec3{0, 1, 1}, vec3{0, 1, 0}, math.Pi/3, 1, .1, 100)
	const scale, distance = 2, .055
	radius := .0028 * 1.6 * scale
	splashes := make([]float32, 6144*particleFloats)
	copy(splashes, []float32{0, 1, float32(1 - distance), .1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, scale, 2})
	params := d.iniParams(nil)
	cameraRows := d.structured(camera.rows(), 16).srv
	vsViews := bindings{srv: map[int]unsafe.Pointer{
		100: d.structured(make([]float32, primaryCount*particleFloats), 64).srv,
		101: d.structured(splashes, 64).srv,
		102: d.structured(make([]float32, 6144*3*4), 16).srv,
		103: d.structured(liveSlots(0), 16).srv,
		104: d.structured(make([]float32, primaryCount*linkRows*4), 16).srv,
		112: d.structured(make([]float32, primaryCount*16), 64).srv,
		113: cameraRows, 120: params,
	}}
	psViews := bindings{srv: map[int]unsafe.Pointer{113: cameraRows, 120: params}}
	targets := []gpuTexture{
		d.texture2D(width, height, formatR32Float, 4, nil, true),
		d.texture2D(width, height, formatR32Float, 4, nil, true),
		d.texture2D(width, height, formatR32G32B32A32Float, 16, nil, true),
		d.texture2D(width, height, formatR32Float, 4, nil, true),
		d.texture2D(width, height, formatR32G32Float, 8, nil, true),
	}
	d.clearTarget(targets[0], [4]float32{1e30})
	d.draw(d.shader("vs", bundle.compile(file, "vs")), d.shader("ps", bundle.compile(file, "ps")),
		vsViews, psViews, targets, 86016)

	center := (height/2)*width + width/2
	depth := bytesFloat32(d.readTexture(targets[0], 4))[center]
	thickness := bytesFloat32(d.readTexture(targets[1], 4))[center]
	// The pixel's ray runs within a few degrees of the view axis.
	exit, near := distance+radius, .05
	if math.Abs(float64(thickness)-(exit-near)) > .05*(exit-near) {
		t.Errorf("thickness %v, want about %v", thickness, exit-near)
	}
	if math.Abs(float64(depth)-exit) > .03*exit {
		t.Errorf("depth %v, want about %v where the ray leaves the drop", depth, exit)
	}
}

// grownPuddleDraw is what drawGrownPuddle measured: the floor pixels well
// inside the puddle, those of them it covers, the pixels it covers on top of
// the block, and how far, in view distance, the depth it wrote at a covered
// floor pixel lies off the floor there at most.
type grownPuddleDraw struct {
	inside, covered, onBlock int
	offFloor                 float64
}

// drawGrownPuddle draws one grown puddle on a flat floor with its plane
// tilted toward the camera, and a block 3 cm tall standing in it at blockX
// from its center, as a shoe would. The scene depth has depthSize texels a
// side for the puddle's 128 pixels, as a game rendering below its output
// resolution has.
func drawGrownPuddle(t *testing.T, tilt float64, depthSize int, blockX float64) grownPuddleDraw {
	t.Helper()
	const width, height, blockTop = 128, 128, .03
	bundle := newShaderBundle(t, games[0].dir)
	file := filepath.Join(bundle.root, "fluid", "FluidSurface.hlsl")
	d := newWARPDevice(t)
	camera := newTestCamera(vec3{0, 1.6, 2}, vec3{0, 0, 0}, math.Pi/6, 1, .1, 100)
	// The scene at screen point u, v: its view distance, and whether it lies
	// on the block or on the floor well inside the puddle and clear of it.
	scene := func(u, v float64) (distance float64, onBlock, inner bool) {
		ray := camera.ray(u, v)
		if ray[1] >= 0 {
			return 0, false, false
		}
		point := camera.eye.add(ray.scale((blockTop - camera.eye[1]) / ray[1]))
		if math.Abs(point[0]-blockX) < .05 && math.Abs(point[2]) < .05 {
			return camera.view(point)[2], true, false
		}
		point = camera.eye.add(ray.scale(-camera.eye[1] / ray[1]))
		return camera.view(point)[2], false, point.dot(point) < .25*.25 && math.Abs(point[0]-blockX) > .09
	}
	depth := make([]float32, depthSize*depthSize)
	for y := range depthSize {
		for x := range depthSize {
			depth[y*depthSize+x] = 1
			if distance, _, _ := scene(
				(float64(x)+.5)/float64(depthSize),
				(float64(y)+.5)/float64(depthSize),
			); distance > 0 {
				depth[y*depthSize+x] = float32(camera.depth(distance))
			}
		}
	}

	// About 800 drops, which spreads it well past 25 cm.
	scale := math.Cbrt(800)
	normal := vec3{0, math.Cos(tilt), math.Sin(tilt)}
	puddle := splashDrop{
		position: normal.scale(fluidRadius*scale/9 + .0002), normal: normal, mode: 3, spread: 1, scale: scale,
		marker: 5,
	}
	splashes := make([]float32, splashCount*particleFloats)
	copy(splashes, puddle.floats())
	params := d.iniParams(map[int][4]float32{237: {0, 1, 0, 0}})
	cameraRows := d.structured(camera.rows(), 16).srv
	vsViews := bindings{srv: map[int]unsafe.Pointer{
		100: d.structured(make([]float32, primaryCount*particleFloats), 64).srv,
		101: d.structured(splashes, 64).srv,
		102: d.structured(make([]float32, splashCount*3*4), 16).srv,
		103: d.structured(liveSlots(0), 16).srv,
		104: d.structured(make([]float32, primaryCount*linkRows*4), 16).srv,
		112: d.structured(make([]float32, primaryCount*16), 64).srv,
		113: cameraRows, 120: params,
	}}
	psViews := bindings{srv: map[int]unsafe.Pointer{
		106: d.floatTexture(depthSize, depthSize, depth).srv, 113: cameraRows, 120: params,
	}}
	targets := []gpuTexture{
		d.texture2D(width, height, formatR32Float, 4, nil, true),
		d.texture2D(width, height, formatR32Float, 4, nil, true),
		d.texture2D(width, height, formatR32G32B32A32Float, 16, nil, true),
		d.texture2D(width, height, formatR32Float, 4, nil, true),
		d.texture2D(width, height, formatR32G32Float, 8, nil, true),
	}
	d.clearTarget(targets[0], [4]float32{1e30})
	d.draw(d.shader("vs", bundle.compile(file, "vs")), d.shader("ps", bundle.compile(file, "ps")),
		vsViews, psViews, targets, (primaryCount+splashCount)*6)

	var got grownPuddleDraw
	written := bytesFloat32(d.readTexture(targets[0], 4))
	thickness := bytesFloat32(d.readTexture(targets[1], 4))
	for y := range height {
		for x := range width {
			index := y*width + x
			distance, onBlock, inner := scene((float64(x)+.5)/width, (float64(y)+.5)/height)
			drawn := thickness[index] > 0
			if onBlock && drawn {
				got.onBlock++
			}
			if !inner {
				continue
			}
			got.inside++
			if drawn {
				got.covered++
				got.offFloor = math.Max(got.offFloor, math.Abs(float64(written[index])-distance))
			}
		}
	}
	return got
}

// A grown puddle is one wide disc on the plane its center sees, which depth
// noise tilts by a few degrees. It must still cover the floor under it rather
// than break into rows where its plane leaves the floor, lie on the floor
// rather than on the steps of a scene depth coarser than the screen, and
// still not climb onto what stands in it, near its center or far enough out
// that a block's top lies within the floor's allowed rise.
func TestSurfaceDrapesGrownPuddleOnFloor(t *testing.T) {
	for _, depthSize := range []int{128, 85} {
		for _, degrees := range []float64{0, 4, -4} {
			for _, blockX := range []float64{.12, .32} {
				got := drawGrownPuddle(t, degrees*math.Pi/180, depthSize, blockX)
				label := fmt.Sprintf("depth %d, tilt %v degrees, block at %v m", depthSize, degrees, blockX)
				if got.inside < 200 || got.covered != got.inside {
					t.Errorf("%s: puddle covers %d of %d floor pixels", label, got.covered, got.inside)
				}
				if got.offFloor > .002 {
					t.Errorf("%s: puddle depth lies up to %.4f m off the floor", label, got.offFloor)
				}
				// A depth coarser than the screen gives the block's silhouette texels
				// a slope allowance that keeps a pixel or two of any film on it
				// (clipOffScene); the drape must add none.
				allowed := 0
				if depthSize < 128 {
					allowed = 2
				}
				if got.onBlock > allowed {
					t.Errorf("%s: puddle covers %d pixels on top of the block", label, got.onBlock)
				}
			}
		}
	}
}

// A puddle seen from standing height recedes by more than the depth filter's
// range within a few rows. Smoothing across rows must leave such a plane where
// it lies: a filter that measures each sample against the center alone drops
// the nearer ones and clamps the farther ones, pushing the puddle behind the
// floor by an amount that changes from row to row, which the composite shows
// as bands.
func TestSmoothKeepsRecedingPuddlePlane(t *testing.T) {
	const size, near, recede = 64, 2.0, .01
	bundle := newShaderBundle(t, games[0].dir)
	file := filepath.Join(bundle.root, "fluid", "FluidSmoothV.hlsl")
	d := newWARPDevice(t)
	// A narrow view, so the filter spans a few pixels at 2 m.
	camera := newTestCamera(vec3{0, 1, 0}, vec3{0, 1, -1}, math.Pi/60, 1, .1, 100)
	depth := make([]float32, size*size)
	for y := range size {
		for x := range size {
			depth[y*size+x] = tagDepth(near+recede*float64(y), 8)
		}
	}
	params := d.iniParams(nil)
	targets := []gpuTexture{
		d.texture2D(size, size, formatR32Float, 4, nil, true),
		d.texture2D(size, size, formatR32Float, 4, nil, true),
	}
	d.draw(d.shader("vs", bundle.compile(file, "vs")), d.shader("ps", bundle.compile(file, "ps")), bindings{},
		bindings{srv: map[int]unsafe.Pointer{
			100: d.floatTexture(size, size, depth).srv,
			101: d.floatTexture(size, size, make([]float32, size*size)).srv,
			103: d.structured(liveSlots(0), 16).srv,
			113: d.structured(camera.rows(), 16).srv, 120: params,
		}}, targets, 3)

	smoothed := bytesFloat32(d.readTexture(targets[0], 4))
	worst := 0.0
	for y := 8; y < size-8; y++ {
		want := near + recede*float64(y)
		worst = math.Max(worst, math.Abs(float64(smoothed[y*size+size/2])-want))
	}
	if worst > .0005 {
		t.Errorf("smoothing moved the receding puddle by up to %.4f m", worst)
	}
}
