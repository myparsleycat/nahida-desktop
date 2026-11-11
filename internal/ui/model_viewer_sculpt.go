package ui

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"

	"nahida.live/desktop/internal/infra"
)

// sculptBaseTint is the body shape viewport's surface colour without weight colours, #9aa4b2 in linear light.
var sculptBaseTint = [3]float32{
	float32(srgbToLinear(0x9a / 255.0)), float32(srgbToLinear(0xa4 / 255.0)), float32(srgbToLinear(0xb2 / 255.0)),
}

// sculptViewport draws one editable mesh with the model viewer's renderer and orbit camera, for the body shape
// editor. The mesh keeps its own space; the user's turn of the model is the world matrix. Its state belongs to
// the UI thread, and its GPU copy is made in the surface's draw.
type sculptViewport struct {
	renderer viewerRenderer
	scene    *viewerScene
	// key names the shown mesh; a new key frames the camera again.
	key string
	// orientation is the user's turn of the model in degrees of roll, pitch and yaw.
	orientation vec3
	camera      orbitCamera
	home        orbitCamera
	revision    uint64
	ring        *viewerRing
	// width and height are the surface's logical size.
	width, height float32
	drawErr       string

	dragStart   orbitCamera
	dragPanning bool
}

// show shows positions as the mesh named key; the same key only moves the vertices. indices nil draws
// consecutive vertex triples.
func (v *sculptViewport) show(key string, positions []float32, indices []uint32) {
	if v.scene != nil && v.key == key {
		v.setPositions(positions)
		return
	}
	v.release()
	vertexCount := len(positions) / 3
	if indices == nil {
		indices = make([]uint32, vertexCount-vertexCount%3)
		for i := range indices {
			indices[i] = uint32(i)
		}
	}
	normals := vertexNormals(positions, indices)
	mesh := &viewerMesh{
		vertexCount: vertexCount,
		positions:   positions,
		normals:     normals,
		uvs:         make([]float32, vertexCount*4),
		indices:     indices,
		extra:       viewerVertexExtras(nil, normals, nil, vertexCount),
		current:     interleaveViewerGeometry(positions, normals),
		overlay:     &viewerOverlay{tint: sculptBaseTint},
	}
	mesh.state.Visible = true
	v.scene = &viewerScene{meshes: []*viewerMesh{mesh}, world: identity4()}
	v.key = key
	v.frame()
	v.revision++
}

// clear removes the mesh.
func (v *sculptViewport) clear() {
	v.release()
	v.key, v.ring = "", nil
	v.revision++
}

func (v *sculptViewport) release() {
	if v.scene != nil {
		v.scene.gpu.release()
		v.scene = nil
	}
}

// close frees the mesh and the renderer. Call it on the UI thread when the viewport is no longer shown.
func (v *sculptViewport) close() {
	v.clear()
	_ = fairy.UseDevice(func(fairy.GPU) error {
		v.renderer.release()
		return nil
	})
}

func (v *sculptViewport) mesh() *viewerMesh {
	if v.scene == nil {
		return nil
	}
	return v.scene.meshes[0]
}

// setPositions moves the vertices, with normals computed again as three.js computeVertexNormals does.
func (v *sculptViewport) setPositions(positions []float32) {
	mesh := v.mesh()
	if mesh == nil || len(positions) != mesh.vertexCount*3 {
		return
	}
	mesh.current, mesh.dirty = interleaveViewerGeometry(positions, vertexNormals(positions, mesh.indices)), true
	v.revision++
}

// setColors shows packed linear RGB colours per vertex, or the plain surface colour when colors is nil.
func (v *sculptViewport) setColors(colors []float32) {
	mesh := v.mesh()
	if mesh == nil {
		return
	}
	if colors == nil {
		mesh.overlay = &viewerOverlay{tint: sculptBaseTint}
		v.revision++
		return
	}
	// extra is replaced rather than changed, as the GPU copy is made from it in the draw.
	extra := append([]byte(nil), mesh.extra...)
	for vertex := range min(mesh.vertexCount, len(colors)/3) {
		at := extra[vertex*viewerVertexExtraStride+28:]
		for channel := range 3 {
			at[channel] = linearToSRGB8(colors[vertex*3+channel])
		}
		at[3] = 255
	}
	mesh.extra, mesh.extraDirty = extra, true
	mesh.overlay = &viewerOverlay{tint: [3]float32{1, 1, 1}, colors: true}
	v.revision++
}

func linearToSRGB8(value float32) byte {
	c := min(max(float64(value), 0), 1)
	if c <= 0.0031308 {
		c *= 12.92
	} else {
		c = 1.055*math.Pow(c, 1/2.4) - 0.055
	}
	return byte(math.Round(c * 255))
}

func (v *sculptViewport) world() mat4 { return orientationMatrix(v.orientation) }

// bounds returns the box around the mesh in world space.
func (v *sculptViewport) bounds() (minimum, maximum vec3, ok bool) {
	mesh := v.mesh()
	if mesh == nil {
		return vec3{}, vec3{}, false
	}
	world := v.world()
	for i := 0; i+2 < len(mesh.current); i += 6 {
		p := world.point(vec3{float64(mesh.current[i]), float64(mesh.current[i+1]), float64(mesh.current[i+2])})
		if !ok {
			minimum, maximum, ok = p, p, true
			continue
		}
		for axis := range 3 {
			minimum[axis], maximum[axis] = min(minimum[axis], p[axis]), max(maximum[axis], p[axis])
		}
	}
	return minimum, maximum, ok
}

// frame points the camera at the whole mesh.
func (v *sculptViewport) frame() {
	minimum, maximum, ok := v.bounds()
	if !ok {
		return
	}
	v.camera = frameOrbit(minimum, maximum)
	v.home = v.camera
}

// rotate turns the model and keeps the centre of its bounds in view.
func (v *sculptViewport) rotate(delta vec3) {
	before, beforeMax, ok := v.bounds()
	v.orientation = v.orientation.add(delta)
	after, afterMax, _ := v.bounds()
	if ok {
		v.camera.follow(after.add(afterMax).scale(0.5).sub(before.add(beforeMax).scale(0.5)))
	}
	v.revision++
}

// resetView clears the turn of the model and frames it again.
func (v *sculptViewport) resetView() {
	v.orientation = vec3{}
	v.frame()
	v.revision++
}

// drag orbits with the left button and pans with the others or with Shift, as the model viewer does.
func (v *sculptViewport) drag(d fairy.DragEvent) {
	if d.Phase == fairy.DragStart {
		v.dragStart = v.camera
		v.dragPanning = d.Button != 0 || d.Mods&keyboard.ModShift != 0
	}
	c := v.dragStart
	if v.dragPanning {
		c.pan(float64(d.DX), float64(d.DY), float64(v.height))
	} else {
		c.orbit(float64(d.DX), float64(d.DY))
	}
	if d.Canceled {
		c = v.dragStart
	}
	v.camera = c
}

// modelRay returns the ray under a point of the surface in the mesh's own space, with x and y in logical
// pixels from its top-left corner.
func (v *sculptViewport) modelRay(x, y float32) (origin, direction vec3) {
	origin, direction = v.camera.ray(float64(x), float64(y), float64(v.width), float64(v.height))
	toModel := v.world().inverseAffine()
	return toModel.point(origin), toModel.direction(direction).normalize()
}

// setRing shows the brush cursor, or hides it for nil.
func (v *sculptViewport) setRing(ring *viewerRing) {
	if ring == nil && v.ring == nil {
		return
	}
	v.ring = ring
	v.revision++
}

func (v *sculptViewport) drawOptions(background [4]float32) viewerDrawOptions {
	return viewerDrawOptions{
		camera:      v.camera,
		world:       v.world(),
		background:  background,
		doubleSided: true,
		toneMapping: viewerToneNeutral,
		environment: viewerEnvironmentStudio,
		exposure:    viewerDefaultExposure,
		metalness:   0.05,
		roughness:   0.55,
		ring:        v.ring,
	}
}

func (v *sculptViewport) version(opts viewerDrawOptions) uint64 {
	h := fnv.New64a()
	c := opts.camera
	values := []float64{
		c.target[0], c.target[1], c.target[2], c.distance, c.yaw, c.pitch, c.lens(), float64(v.revision),
		float64(opts.background[0]), float64(opts.background[1]), float64(opts.background[2]),
	}
	values = append(values, opts.world[:]...)
	_ = binary.Write(h, binary.LittleEndian, values)
	return h.Sum64()
}

// surface is the viewport's drawing element; the caller adds the pointer handlers. log reports draw failures
// with fields.
func (v *sculptViewport) surface(background [4]float32, log *infra.Log, fields map[string]any) *fairy.El {
	opts := v.drawOptions(background)
	scene := v.scene
	return fairy.Surface(func(f fairy.SurfaceFrame) bool {
		err := v.upload(f, scene)
		if err == nil {
			err = v.renderer.draw(f, scene, opts)
		}
		if message := fmt.Sprint(err); err != nil && message != v.drawErr {
			v.drawErr = message
			_ = infra.ReportError(log, err, "BodyShape.draw", infra.Diagnostic{Operation: "draw", Fields: fields})
		}
		return false
	}).Retain(v.version(opts)).OnLayout(func(w, h float32) { v.width, v.height = w, h })
}

// upload makes the mesh's GPU copy on the frame's device when it has none there yet.
func (v *sculptViewport) upload(f fairy.SurfaceFrame, scene *viewerScene) error {
	if scene == nil || (scene.gpu != nil && scene.gpu.generation == f.Generation) {
		return nil
	}
	mesh := scene.meshes[0]
	buffers, err := newViewerMeshGPU(f.GPU().Device, viewerMeshUpload{
		current: mesh.current, uvs: mesh.uvs, extra: mesh.extra, indices: mesh.indices,
	})
	if err != nil {
		return err
	}
	scene.gpu.release()
	scene.gpu = &viewerGPUSet{
		generation: f.Generation, meshes: []viewerMeshGPU{buffers}, textures: map[string]viewerTextureGPU{},
	}
	mesh.dirty, mesh.extraDirty = false, false
	return nil
}
