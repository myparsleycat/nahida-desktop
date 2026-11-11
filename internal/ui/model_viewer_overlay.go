package ui

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"slices"
	"unsafe"

	"github.com/myparsleycat/fairy/d3d"
)

// viewerOverlayVertex is one corner of a tool's marker, in model space, with a premultiplied display colour.
type viewerOverlayVertex struct {
	position [3]float32
	color    [4]float32
}

// viewerOverlayDraw is what a tool draws over the model without depth testing, as three.js materials with
// depthTest off draw: unlit triangles and lines.
type viewerOverlayDraw struct {
	triangles []viewerOverlayVertex
	// lines holds pairs of line ends.
	lines []viewerOverlayVertex
	// animated, when set, adds triangles that move with the time in seconds since the surface appeared; the
	// surface then draws every frame.
	animated func(seconds float64) []viewerOverlayVertex
}

// version identifies the still part of the draw for a surface's Retain.
func (d *viewerOverlayDraw) version() uint64 {
	if d == nil {
		return 0
	}
	h := fnv.New64a()
	_ = binary.Write(h, binary.LittleEndian, d.triangles)
	_ = binary.Write(h, binary.LittleEndian, d.lines)
	if d.animated != nil {
		_, _ = h.Write([]byte{1})
	}
	return h.Sum64()
}

// overlayColor is an opaque sRGB colour such as 0xffc240, as three.js shows a basic material.
func overlayColor(hex uint32) [4]float32 {
	return [4]float32{
		float32(hex>>16&0xff) / 255, float32(hex>>8&0xff) / 255, float32(hex&0xff) / 255, 1,
	}
}

func overlayVertex(p vec3, color [4]float32) viewerOverlayVertex {
	return viewerOverlayVertex{position: [3]float32{float32(p[0]), float32(p[1]), float32(p[2])}, color: color}
}

// overlayFrame maps a part's local axes, with Y along axis, to model space at origin.
type overlayFrame struct {
	origin, x, y, z vec3
}

// newOverlayFrame turns +Y onto axis, as three.js Quaternion.setFromUnitVectors does, with lengths scaled by
// scale.
func newOverlayFrame(origin, axis vec3, scale float64) overlayFrame {
	y := axis.normalize()
	if y.length() == 0 {
		y = vec3{0, 1, 0}
	}
	helper := vec3{1, 0, 0}
	if math.Abs(y[0]) > 0.9 {
		helper = vec3{0, 0, 1}
	}
	x := y.cross(helper).normalize()
	z := x.cross(y)
	return overlayFrame{origin: origin, x: x.scale(scale), y: y.scale(scale), z: z.scale(scale)}
}

func (f overlayFrame) point(x, y, z float64) vec3 {
	return f.origin.add(f.x.scale(x)).add(f.y.scale(y)).add(f.z.scale(z))
}

// overlaySphere appends a sphere of radius around center, cut into slices around and stacks from pole to pole.
func overlaySphere(
	out []viewerOverlayVertex,
	center vec3,
	radius float64,
	slices, stacks int,
	color [4]float32,
) []viewerOverlayVertex {
	at := func(i, j int) vec3 {
		theta := math.Pi * float64(j) / float64(stacks)
		phi := 2 * math.Pi * float64(i) / float64(slices)
		return center.add(vec3{
			math.Sin(theta) * math.Cos(phi), math.Cos(theta), math.Sin(theta) * math.Sin(phi),
		}.scale(radius))
	}
	for j := range stacks {
		for i := range slices {
			a, b, c, d := at(i, j), at(i+1, j), at(i+1, j+1), at(i, j+1)
			out = append(out, overlayVertex(a, color), overlayVertex(b, color), overlayVertex(c, color),
				overlayVertex(a, color), overlayVertex(c, color), overlayVertex(d, color))
		}
	}
	return out
}

// overlayCone appends a cone or, with equal radii, a cylinder along the frame's Y from y0 to y1, with caps.
func overlayCone(
	out []viewerOverlayVertex,
	frame overlayFrame,
	radius0, radius1, y0, y1 float64,
	segments int,
	color [4]float32,
) []viewerOverlayVertex {
	ring := func(i int, radius, y float64) vec3 {
		angle := 2 * math.Pi * float64(i) / float64(segments)
		return frame.point(math.Cos(angle)*radius, y, math.Sin(angle)*radius)
	}
	bottom, top := frame.point(0, y0, 0), frame.point(0, y1, 0)
	for i := range segments {
		a, b := ring(i, radius0, y0), ring(i+1, radius0, y0)
		c, d := ring(i+1, radius1, y1), ring(i, radius1, y1)
		out = append(out, overlayVertex(a, color), overlayVertex(b, color), overlayVertex(c, color),
			overlayVertex(a, color), overlayVertex(c, color), overlayVertex(d, color),
			overlayVertex(bottom, color), overlayVertex(b, color), overlayVertex(a, color))
		if radius1 > 0 {
			out = append(out, overlayVertex(top, color), overlayVertex(d, color), overlayVertex(c, color))
		}
	}
	return out
}

// overlayPolyline appends the segments joining points.
func overlayPolyline(out []viewerOverlayVertex, points []vec3, color [4]float32) []viewerOverlayVertex {
	for i := 1; i < len(points); i++ {
		out = append(out, overlayVertex(points[i-1], color), overlayVertex(points[i], color))
	}
	return out
}

// drawOverlay draws the overlay over everything drawn so far.
func (r *viewerRenderer) drawOverlay(
	ctx d3d.Context,
	dev d3d.Device,
	rtv d3d.RenderTargetView,
	overlay *viewerOverlayDraw,
	seconds float64,
) error {
	triangles := overlay.triangles
	if overlay.animated != nil {
		triangles = append(triangles[:len(triangles):len(triangles)], overlay.animated(seconds)...)
	}
	count := len(triangles) + len(overlay.lines)
	if count == 0 {
		return nil
	}
	stride := uint32(unsafe.Sizeof(viewerOverlayVertex{}))
	if size := uint32(count) * stride; r.overlayVB.P == nil || r.overlayVB.Desc().ByteWidth < size {
		r.overlayVB.Release()
		var err error
		if r.overlayVB, err = dev.CreateBuffer(&d3d.BufferDesc{
			ByteWidth: max(size, 64*1024), Usage: d3d.USAGE_DYNAMIC,
			BindFlags: d3d.BIND_VERTEX_BUFFER, CPUAccessFlags: d3d.CPU_ACCESS_WRITE,
		}, nil); err != nil {
			r.overlayVB = d3d.Buffer{}
			return err
		}
	}
	vertices := slices.Concat(triangles, overlay.lines)
	if err := d3d.WriteData(ctx, r.overlayVB, vertices); err != nil {
		return err
	}

	ctx.OMSetRenderTargets([]d3d.RenderTargetView{rtv}, d3d.DepthStencilView{})
	ctx.OMSetBlendState(r.blend, [4]float32{}, 0xffffffff)
	ctx.RSSetState(r.raster)
	ctx.IASetInputLayout(r.overlayLayout)
	ctx.IASetVertexBuffers(0, []d3d.Buffer{r.overlayVB}, []uint32{stride}, []uint32{0})
	ctx.VSSetShader(r.overlayVS)
	ctx.PSSetShader(r.overlayPS)
	if len(triangles) > 0 {
		ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_TRIANGLELIST)
		ctx.Draw(uint32(len(triangles)), 0)
	}
	if len(overlay.lines) > 0 {
		ctx.IASetPrimitiveTopology(d3d.PRIMITIVE_TOPOLOGY_LINELIST)
		ctx.Draw(uint32(len(overlay.lines)), uint32(len(triangles)))
	}
	return nil
}
