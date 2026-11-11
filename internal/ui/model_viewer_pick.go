package ui

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/infra"
)

// viewerTool is a tool's use of the model viewer: it picks the surface, draws markers over the model and adds a
// panel beside it. Every hook may be nil.
type viewerTool struct {
	// pick receives a click on the model that did not drag; ok is false when the click missed every drawn mesh.
	pick func(pick viewerSurfacePick, ok bool)
	// overlay returns the markers to draw over the model, built with the view.
	overlay func() *viewerOverlayDraw
	// inspector is the tool's panel above the toggles, and footer the row under it.
	inspector func() *fairy.El
	footer    func() *fairy.El
}

// viewerSurfacePick is a point on a drawn mesh in the mesh's own space, as the React viewer's
// ModelViewerSurfacePick.
type viewerSurfacePick struct {
	meshID    string
	faceIndex int
	// vertices are the source vertex buffer indices of the face's corners, in the viewer's corner order.
	vertices    [3]uint32
	barycentric vec3
	triangle    [3]vec3
	point       vec3
	normal      vec3
}

// viewerHit is the nearest drawn triangle under a ray.
type viewerHit struct {
	mesh, face  int
	distance    float64
	barycentric vec3
}

func (m *viewerMesh) vertexPosition(vertex uint32) vec3 {
	at := int(vertex) * 6
	if at+2 >= len(m.current) {
		return vec3{}
	}
	return vec3{float64(m.current[at]), float64(m.current[at+1]), float64(m.current[at+2])}
}

// corners returns a face's index buffer entries and their current positions.
func (m *viewerMesh) corners(face int) ([3]uint32, [3]vec3, bool) {
	var indices [3]uint32
	var points [3]vec3
	if face < 0 || face*3+2 >= len(m.indices) {
		return indices, points, false
	}
	for corner := range 3 {
		indices[corner] = m.indices[face*3+corner]
		if int(indices[corner]) >= m.vertexCount {
			return indices, points, false
		}
		points[corner] = m.vertexPosition(indices[corner])
	}
	return indices, points, true
}

// rayTriangle is the two-sided Möller-Trumbore test. It returns the distance along direction and the weights
// of b and c.
func rayTriangle(origin, direction, a, b, c vec3) (distance, u, w float64, ok bool) {
	ab, ac := b.sub(a), c.sub(a)
	p := direction.cross(ac)
	det := ab.dot(p)
	if math.Abs(det) < 1e-14 {
		return 0, 0, 0, false
	}
	inverse := 1 / det
	s := origin.sub(a)
	if u = s.dot(p) * inverse; u < 0 || u > 1 {
		return 0, 0, 0, false
	}
	q := s.cross(ab)
	if w = direction.dot(q) * inverse; w < 0 || u+w > 1 {
		return 0, 0, 0, false
	}
	if distance = ac.dot(q) * inverse; distance <= 0 {
		return 0, 0, 0, false
	}
	return distance, u, w, true
}

// hitScene finds the nearest triangle of a drawn mesh under a ray in model space. EFMI's overlay shadows are no
// surface to pick, even while game textures are highlighted.
func hitScene(scene *viewerScene, origin, direction vec3) (viewerHit, bool) {
	var best viewerHit
	found := false
	for i, mesh := range scene.meshes {
		if !mesh.drawn(false) {
			continue
		}
		for face := 0; face*3+2 < len(mesh.indices); face++ {
			_, points, ok := mesh.corners(face)
			if !ok {
				continue
			}
			distance, u, w, ok := rayTriangle(origin, direction, points[0], points[1], points[2])
			if !ok || (found && distance >= best.distance) {
				continue
			}
			best = viewerHit{mesh: i, face: face, distance: distance, barycentric: vec3{1 - u - w, u, w}}
			found = true
		}
	}
	return best, found
}

// hitAt casts the ray under a point of the viewport, in logical pixels from its top-left corner.
func (v *ModelViewer) hitAt(x, y float32) (viewerHit, bool) {
	if v.scene == nil {
		return viewerHit{}, false
	}
	origin, direction := v.camera.ray(float64(x), float64(y), float64(v.width), float64(v.height))
	toModel := v.world().inverseAffine()
	return hitScene(v.scene, toModel.point(origin), toModel.direction(direction))
}

// pickAt reports the surface under a click to the tool. A mesh compacted from a larger vertex buffer maps its
// corners back to the source buffer, which may have to be read first; only the latest click reports.
func (v *ModelViewer) pickAt(x, y float32) {
	tool := v.tool
	if tool == nil || tool.pick == nil || v.scene == nil {
		return
	}
	v.pickSeq++
	hit, ok := v.hitAt(x, y)
	if !ok {
		tool.pick(viewerSurfacePick{}, false)
		return
	}
	mesh := v.scene.meshes[hit.mesh]
	indices, points, _ := mesh.corners(hit.face)
	pick := viewerSurfacePick{
		meshID: mesh.info.ID, faceIndex: hit.face, vertices: indices, barycentric: hit.barycentric,
		triangle: points, normal: points[2].sub(points[1]).cross(points[0].sub(points[1])).normalize(),
	}
	for corner := range 3 {
		pick.point = pick.point.add(points[corner].scale(hit.barycentric[corner]))
	}

	url := mesh.info.SourceIndicesURL
	if url == "" {
		tool.pick(pick, true)
		return
	}
	resolve := func(sources []uint32) {
		for corner, index := range indices {
			if int(index) < len(sources) {
				pick.vertices[corner] = sources[index]
			}
		}
		tool.pick(pick, true)
	}
	if sources, cached := v.sourceIndices[url]; cached {
		resolve(sources)
		return
	}
	seq, ctx := v.pickSeq, v.ctx
	go func() {
		data, err := v.read(ctx, url)
		if err == nil && len(data)%4 != 0 {
			err = fmt.Errorf("source index buffer has %d bytes", len(data))
		}
		fairy.Go(func() {
			if v.closed || seq != v.pickSeq || v.tool != tool {
				return
			}
			if err != nil {
				_ = infra.ReportError(v.log, err, "ModelViewer.pick", infra.Diagnostic{
					Operation: "read-source-indices", Fields: map[string]any{"modPath": v.path, "meshId": pick.meshID},
				})
				tool.pick(viewerSurfacePick{}, false)
				return
			}
			sources := make([]uint32, len(data)/4)
			for i := range sources {
				sources[i] = binary.LittleEndian.Uint32(data[i*4:])
			}
			if v.sourceIndices == nil {
				v.sourceIndices = map[string][]uint32{}
			}
			v.sourceIndices[url] = sources
			resolve(sources)
		})
	}()
}

// surfacePoint places a face's barycentric point on the first drawn mesh of meshIDs, where its geometry is
// now; it reports false while none of them is drawn.
func (v *ModelViewer) surfacePoint(meshIDs []string, face int, barycentric vec3) (vec3, bool) {
	if v.scene == nil {
		return vec3{}, false
	}
	for _, id := range meshIDs {
		for _, mesh := range v.scene.meshes {
			if mesh.info.ID != id || !mesh.state.Visible {
				continue
			}
			_, points, ok := mesh.corners(face)
			if !ok {
				return vec3{}, false
			}
			var point vec3
			for corner := range 3 {
				point = point.add(points[corner].scale(barycentric[corner]))
			}
			return point, true
		}
	}
	return vec3{}, false
}

// visibleMeshes returns the IDs of the meshes drawn after toggles and form.
func (v *ModelViewer) visibleMeshes() map[string]bool {
	visible := map[string]bool{}
	if v.scene != nil {
		for _, mesh := range v.scene.meshes {
			if mesh.state.Visible {
				visible[mesh.info.ID] = true
			}
		}
	}
	return visible
}
