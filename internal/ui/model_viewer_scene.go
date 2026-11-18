package ui

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerScene is a loaded mod: its transport, the meshes on the CPU and the GPU resources made from them.
// It belongs to the UI thread once installed; the loader builds it on its own goroutine before that.
type viewerScene struct {
	transport modelviewer.ModelViewerTransport
	meshes    []*viewerMesh
	// genshinFace is set when a GIMI mod draws a face, of its own or from the original model. Most replace only the
	// body, hair and dress, and the game draws its face.
	genshinFace bool
	// original is the character's original model shown with the mod, or nil. originalStale reports that a mesh
	// gained or lost all of its triangles since it was matched.
	original      *viewerOriginal
	originalStale bool
	// faceTexture is the key of the face diffuse the mod replaces by hash in the state shown, or "". The original
	// model's face draws with it in place of its own.
	faceTexture string
	// world turns the mod's space into the viewer's Y-up space.
	world mat4
	// minimum and maximum bound every mesh in viewer space.
	minimum, maximum vec3
	gpu              *viewerGPUSet
	// variantUse lists the loaded position variants by mesh and variant index, least recently drawn first,
	// and variantBytes is their size.
	variantUse   [][2]int
	variantBytes int
	// wanted holds the mesh states apply was last asked to show. They differ from the meshes' own while a
	// position variant they select, or a mesh they show, is still loading.
	wanted []modelviewer.ViewerMeshState
	// waiting is set while wanted is not shown yet.
	waiting bool
	// meshUse lists the loaded meshes, least recently drawn first, and meshBytes is their GPU size.
	meshUse   []int
	meshBytes int
	// meshBudget and variantBudget replace viewerMeshCacheBytes and viewerVariantCacheBytes when set.
	meshBudget, variantBudget int
	// schedule lists what each frame of the clip being played draws, and frame is the frame shown. With a schedule
	// the caches drop what the clip draws last, instead of what it drew longest ago.
	schedule *viewerSchedule
	frame    int
	// demand marks what the viewer still wants loaded.
	demand *viewerDemand
	// evictions counts the meshes and variants dropped from the caches. resident is a schedule found fully loaded
	// when evictions was residentAt.
	evictions  uint64
	resident   *viewerSchedule
	residentAt uint64
}

// viewerMeshCacheBytes bounds the GPU size of the meshes a scene keeps loaded beyond the ones it shows. A mod
// that animates by switching between many meshes, one per frame, only ever draws a few of them.
const viewerMeshCacheBytes = 256 << 20

func (s *viewerScene) meshCacheBytes() int {
	if s.meshBudget > 0 {
		return s.meshBudget
	}
	return viewerMeshCacheBytes
}

func (s *viewerScene) variantCacheBytes() int {
	if s.variantBudget > 0 {
		return s.variantBudget
	}
	return viewerVariantCacheBytes
}

// dropOrder orders the droppable items of a cache, given in least recently drawn order, by which to drop first:
// with a schedule the one the clip draws last, the least recently drawn of those drawn equally late first.
func dropOrder[T any](s *viewerScene, refs []T, item func(T) viewerScheduleItem) []T {
	if s.schedule == nil {
		return refs
	}
	distances := make(map[viewerScheduleItem]int, len(refs))
	for _, ref := range refs {
		distances[item(ref)] = s.schedule.distance(item(ref), s.frame)
	}
	ordered := slices.Clone(refs)
	slices.SortStableFunc(ordered, func(a, b T) int { return cmp.Compare(distances[item(b)], distances[item(a)]) })
	return ordered
}

// keepsMesh reports a mesh the cache never drops: one shown, about to show, or read by a deformer.
func (s *viewerScene) keepsMesh(ref int) bool {
	mesh := s.meshes[ref]
	return mesh.pinned || mesh.state.Visible || (ref < len(s.wanted) && s.wanted[ref].Visible)
}

// viewerMissing is what apply waits for: position variants by mesh index, and meshes to load.
type viewerMissing struct {
	variants map[int]int
	meshes   []int
}

func (m viewerMissing) empty() bool { return len(m.variants) == 0 && len(m.meshes) == 0 }

// gpuBytes is the size of the mesh's vertex and index buffers, counting the buffer of the next frame's geometry
// for a mesh whose geometry changes.
func (m *viewerMesh) gpuBytes() int {
	vertexBytes := 6*4 + 4*4 + viewerVertexExtraStride
	if !m.static() {
		vertexBytes += 6 * 4
	}
	return m.vertexCount*vertexBytes + m.info.IndexCount*4
}

// trackLoaded starts the cache with the meshes loaded so far.
func (s *viewerScene) trackLoaded() {
	s.meshUse, s.meshBytes = nil, 0
	for i, mesh := range s.meshes {
		if !mesh.unloaded {
			s.meshUse = append(s.meshUse, i)
			s.meshBytes += mesh.gpuBytes()
		}
	}
}

// storeMesh counts a newly loaded mesh and unloads others in dropOrder while the meshes the scene may drop are over
// the cache size; the ones it keeps do not count, and the new one stays. It returns the meshes it unloaded, whose
// GPU buffers the caller releases.
func (s *viewerScene) storeMesh(i int) []int {
	s.meshUse = append(s.meshUse, i)
	s.meshBytes += s.meshes[i].gpuBytes()
	held := 0
	var droppable []int
	for _, ref := range s.meshUse {
		switch {
		case s.keepsMesh(ref):
			held += s.meshes[ref].gpuBytes()
		case ref != i:
			droppable = append(droppable, ref)
		}
	}
	if s.meshBytes-held <= s.meshCacheBytes() {
		return nil
	}
	var unloaded []int
	for _, ref := range dropOrder(s, droppable, func(ref int) viewerScheduleItem {
		return viewerScheduleItem{mesh: ref, variant: -1}
	}) {
		if s.meshBytes-held <= s.meshCacheBytes() {
			break
		}
		mesh := s.meshes[ref]
		s.meshUse = slices.DeleteFunc(s.meshUse, func(other int) bool { return other == ref })
		s.meshBytes -= mesh.gpuBytes()
		mesh.unload()
		s.evictions++
		unloaded = append(unloaded, ref)
	}
	return unloaded
}

// forgetMesh unloads a mesh that has no GPU buffers, so a state that shows it loads it again.
func (s *viewerScene) forgetMesh(i int) {
	if at := slices.Index(s.meshUse, i); at >= 0 {
		s.meshUse = slices.Delete(s.meshUse, at, at+1)
		s.meshBytes -= s.meshes[i].gpuBytes()
	}
	s.meshes[i].unload()
	s.evictions++
}

// touchMesh marks a loaded mesh as the most recently drawn.
func (s *viewerScene) touchMesh(i int) {
	if at := slices.Index(s.meshUse, i); at >= 0 && at != len(s.meshUse)-1 {
		s.meshUse = append(slices.Delete(s.meshUse, at, at+1), i)
	}
}

// waitsForMesh reports a mesh the wanted states show that is not loaded yet.
func (s *viewerScene) waitsForMesh(i int) bool {
	mesh := s.meshes[i]
	return i < len(s.wanted) && s.wanted[i].Visible && mesh.unloaded && !mesh.failed
}

// viewerVariantCacheBytes bounds the position variants a scene keeps loaded when the installed memory is unknown.
// An animation may step through hundreds of them.
const viewerVariantCacheBytes = 256 << 20

func (g *viewerPositionGeometry) bytes() int { return len(g.current)*4 + len(g.extra) }

// storeVariant keeps a loaded position variant and drops others in dropOrder while over the cache size.
func (s *viewerScene) storeVariant(i, index int, variant *viewerPositionGeometry) {
	mesh := s.meshes[i]
	if previous := mesh.variants[index]; previous != nil {
		s.variantBytes -= previous.bytes()
	} else {
		s.variantUse = append(s.variantUse, [2]int{i, index})
	}
	mesh.variants[index] = variant
	s.variantBytes += variant.bytes()
	if s.variantBytes <= s.variantCacheBytes() {
		return
	}

	// A variant the scene waits to show stays, as does the new one.
	var droppable [][2]int
	for _, ref := range s.variantUse {
		if !s.waitsFor(ref[0], ref[1]) && ref != [2]int{i, index} {
			droppable = append(droppable, ref)
		}
	}
	for _, ref := range dropOrder(s, droppable, func(ref [2]int) viewerScheduleItem {
		return viewerScheduleItem{mesh: ref[0], variant: ref[1]}
	}) {
		if s.variantBytes <= s.variantCacheBytes() {
			break
		}
		s.variantUse = slices.DeleteFunc(s.variantUse, func(other [2]int) bool { return other == ref })
		s.variantBytes -= s.meshes[ref[0]].variants[ref[1]].bytes()
		delete(s.meshes[ref[0]].variants, ref[1])
		s.evictions++
	}
}

func variantGeometryKey(index int) string { return "variant:" + strconv.Itoa(index) }

// touchVariant marks a loaded position variant as the most recently drawn.
func (s *viewerScene) touchVariant(i, index int) {
	ref := [2]int{i, index}
	if at := slices.Index(s.variantUse, ref); at >= 0 && at != len(s.variantUse)-1 {
		s.variantUse = append(slices.Delete(s.variantUse, at, at+1), ref)
	}
}

type viewerMesh struct {
	info        modelviewer.ModelViewerMeshTransport
	vertexCount int
	// positions and normals are the base geometry, three floats per vertex.
	positions []float32
	normals   []float32
	// uvs holds four floats per vertex: the first UV set, then the second, or the first again without one.
	uvs     []float32
	indices []uint32
	// extra holds what the toon pass reads per vertex (see viewerVertexExtra): baseExtra, or the extra of the
	// position variant shown.
	extra     []byte
	baseExtra []byte
	// tangentUsable reports vertex tangents that span a frame with the normals. GIMI-style buffers store the
	// outline's smoothed normal in TANGENT instead.
	tangentUsable bool
	hasTangents   bool
	hasColors     bool
	hasUV1        bool
	// averagedOutline keeps the outline hull on seam-averaged normals even where TANGENT holds an outline
	// direction, as the material profile's hull does.
	averagedOutline bool
	// faceForward is the mean of the base normals, which points out of a face mesh in the mod's space.
	faceForward vec3
	// toonFace, toonHair and toonEye mark the parts the toon shades as a face, hair or eyes.
	toonFace, toonHair, toonEye bool
	// toonDress marks a Genshin Dress part, which takes the character's dress material.
	toonDress bool
	// efmiHead marks an EFMI mesh in the head zone, which a flat face style lights as a face, and efmiFace the
	// head meshes taken for the face.
	efmiHead, efmiFace bool
	// normalMapUnused marks a normal map slot that holds other shading data, such as a ZZMI face's SDF.
	normalMapUnused bool
	// overlayShadow marks one of EFMI's overlay shadows, which the game multiplies over the face through a mask
	// and stencil that mods do not carry; it draws only while game textures are highlighted.
	overlayShadow bool
	shapes        []viewerShapeTarget
	// variants holds the position variants loaded so far by index.
	variants map[int]*viewerPositionGeometry
	// unreadable holds the position variants that could not be read. A state that selects one keeps the geometry
	// shown before it.
	unreadable map[int]bool

	state modelviewer.ViewerMeshState
	// geometryKey names the geometry current holds: a position variant or a set of shape weights.
	geometryKey string
	// current interleaves the position and normal of each vertex as drawn. It is replaced, never changed in
	// place, so a GPU upload running on another goroutine can keep reading an earlier slice.
	current []float32
	// computed marks current as a frame of a compute deformer, which the toggle state leaves as it is.
	computed bool
	// dirty marks current as newer than the GPU's copy.
	dirty bool
	// next is the geometry of the animation's next frame, laid out as current, which the draw moves towards by
	// the share of the frame played; nil without one. nextKey names it as geometryKey names current, and nextDirty
	// marks it as newer than the GPU's copy. Like current, it is replaced, never changed in place.
	next      []float32
	nextKey   string
	nextDirty bool
	// swapped asks the draw to swap the GPU's copies of current and next, after the two traded places.
	swapped bool
	// extraDirty marks extra as newer than the GPU's copy; the body shape editor and compute deformers change
	// extra, by replacing it.
	extraDirty bool
	// overlay replaces the material colour, for the body shape editor; nil for mod meshes.
	overlay *viewerOverlay

	// unloaded marks a mesh whose geometry is not read yet, or was dropped from the cache; it has no GPU buffers
	// and draws nothing. failed marks one whose geometry could not be read.
	unloaded bool
	failed   bool
	// pinned keeps a compute deformer's mesh loaded, since the deformer reads its geometry.
	pinned bool
	// cpuDropped marks a mesh that keeps only its GPU buffers: its geometry never changes, so nothing reads the
	// arrays it was uploaded from.
	cpuDropped bool
}

// newViewerStub is a mesh known from its transport, before its geometry is read.
func newViewerStub(info modelviewer.ModelViewerMeshTransport, averagedOutline bool) *viewerMesh {
	return &viewerMesh{
		info: info, vertexCount: info.VertexCount, variants: map[int]*viewerPositionGeometry{},
		averagedOutline: averagedOutline, unloaded: true,
	}
}

// fill takes the geometry decoded from the mesh's buffers, starting from the base geometry the GPU buffers are
// made of. The state, marks and position variants of the mesh stay.
func (m *viewerMesh) fill(decoded *viewerMesh) {
	m.vertexCount = decoded.vertexCount
	m.positions, m.normals, m.uvs, m.indices, m.extra = decoded.positions, decoded.normals, decoded.uvs,
		decoded.indices, decoded.extra
	m.baseExtra = decoded.extra
	m.tangentUsable, m.hasTangents, m.hasColors, m.hasUV1 = decoded.tangentUsable, decoded.hasTangents,
		decoded.hasColors, decoded.hasUV1
	m.faceForward, m.shapes = decoded.faceForward, decoded.shapes
	m.current, m.geometryKey = decoded.current, decoded.geometryKey
	m.computed, m.dirty, m.extraDirty, m.swapped = false, false, false, false
	m.clearNext()
	m.unloaded, m.cpuDropped = false, false
}

// unload drops the mesh's geometry. Its GPU buffers are the caller's to release.
func (m *viewerMesh) unload() {
	m.positions, m.normals, m.uvs, m.indices, m.extra, m.shapes, m.current = nil, nil, nil, nil, nil, nil, nil
	m.baseExtra = nil
	m.geometryKey, m.computed, m.dirty, m.extraDirty, m.swapped = "", false, false, false, false
	m.clearNext()
	m.unloaded, m.cpuDropped = true, false
}

// clearNext drops the next frame's geometry. The GPU's copy of current stays where swapped says it is.
func (m *viewerMesh) clearNext() {
	m.next, m.nextKey, m.nextDirty = nil, "", false
}

// promoteNext makes the next frame's geometry current, and current the next, without building or uploading
// either again.
func (m *viewerMesh) promoteNext() {
	m.current, m.next = m.next, m.current
	m.geometryKey, m.nextKey = m.nextKey, m.geometryKey
	m.dirty, m.nextDirty = m.nextDirty, m.dirty
	m.swapped = !m.swapped
}

// static reports a mesh whose drawn geometry never changes: no shape targets, position variants or deformer.
func (m *viewerMesh) static() bool {
	return len(m.info.ShapeTargets) == 0 && len(m.info.PositionVariants) == 0 && !m.pinned
}

// dropCPU frees the arrays of a static mesh once its GPU buffers are made.
func (m *viewerMesh) dropCPU() {
	if m.unloaded || !m.static() {
		return
	}
	m.positions, m.normals, m.uvs, m.indices, m.extra, m.baseExtra, m.current = nil, nil, nil, nil, nil, nil, nil
	m.cpuDropped = true
}

// viewerOverlay is the body shape editor's look of a mesh: a linear tint, times the vertex colours stored
// sRGB-encoded in extra's COLOR when colors is set.
type viewerOverlay struct {
	tint   [3]float32
	colors bool
}

type viewerShapeTarget struct {
	positions    []float32
	lowPositions []float32
	mode         string
}

// viewerPositionGeometry is a position variant as drawn: its current and extra replace the mesh's while it shows.
type viewerPositionGeometry struct {
	current []float32
	extra   []byte
}

// viewerVariantBase is what a position variant takes from its mesh to make its extra data: the mesh's own extra,
// whose colours and, without tangents of the variant's own, tangents it keeps, and how the outline extrudes.
type viewerVariantBase struct {
	vertexCount    int
	extra          []byte
	tangentOutline bool
}

func (m *viewerMesh) variantBase() viewerVariantBase {
	return viewerVariantBase{vertexCount: m.vertexCount, extra: m.baseExtra, tangentOutline: m.tangentOutline()}
}

// drawn reports whether the mesh draws, given whether game textures are highlighted.
func (m *viewerMesh) drawn(highlight bool) bool {
	return m.state.Visible && (!m.overlayShadow || highlight)
}

// decodeViewerMesh reads the MVG3 mesh buffer the backend writes (see modelViewerMeshBytes).
func decodeViewerMesh(data []byte, averagedOutline bool) (*viewerMesh, error) {
	if len(data) < 32 || string(data[:4]) != "MVG3" {
		return nil, errors.New("invalid model viewer mesh header")
	}
	var counts [7]int
	for i := range counts {
		counts[i] = int(binary.LittleEndian.Uint32(data[4+i*4:]))
	}
	positions, normals, tangents, uvs, uvs1, colors, indices := counts[0], counts[1], counts[2], counts[3],
		counts[4], counts[5], counts[6]
	vertices := positions / 3
	switch {
	case positions%3 != 0,
		normals != 0 && normals != positions,
		tangents != 0 && tangents != vertices*4,
		uvs != 0 && uvs != vertices*2,
		uvs1 != 0 && uvs1 != vertices*2,
		colors != 0 && colors != vertices*4,
		len(data) != 32+colors+(positions+normals+tangents+uvs+uvs1+indices)*4:
		return nil, errors.New("invalid model viewer mesh layout")
	}
	offset := 32
	floats := func(count int) []float32 {
		values := float32s(data[offset : offset+count*4])
		offset += count * 4
		return values
	}
	mesh := &viewerMesh{
		vertexCount: vertices, variants: map[int]*viewerPositionGeometry{}, averagedOutline: averagedOutline,
	}
	mesh.positions = floats(positions)
	mesh.normals = floats(normals)
	tangentValues := floats(tangents)
	uv0, uv1 := floats(uvs), floats(uvs1)
	mesh.hasUV1 = len(uv1) > 0
	if !mesh.hasUV1 {
		uv1 = uv0
	}
	mesh.uvs = make([]float32, vertices*4)
	for v := range vertices {
		if len(uv0) > 0 {
			copy(mesh.uvs[v*4:v*4+2], uv0[v*2:v*2+2])
		}
		if len(uv1) > 0 {
			copy(mesh.uvs[v*4+2:v*4+4], uv1[v*2:v*2+2])
		}
	}
	colorValues := data[offset : offset+colors]
	offset += colors
	mesh.indices = uint32s(data[offset : offset+indices*4])
	for _, index := range mesh.indices {
		if int(index) >= vertices {
			return nil, fmt.Errorf("mesh index %d is out of range for %d vertices", index, vertices)
		}
	}
	if len(mesh.normals) == 0 {
		mesh.normals = vertexNormals(mesh.positions, mesh.indices)
	}
	mesh.hasTangents, mesh.hasColors = len(tangentValues) > 0, len(colorValues) > 0
	mesh.tangentUsable = usableTangentFrame(tangentValues, mesh.normals, nil)
	mesh.faceForward = meanNormal(mesh.normals)
	outline := viewerOutline(mesh.tangentOutline(), mesh.positions, mesh.normals, tangentValues)
	mesh.extra = viewerVertexExtras(tangentValues, outline, colorValues, vertices)
	// Start from the base geometry, so a mesh hidden at load still has GPU buffers to show later.
	mesh.current, mesh.geometryKey = interleaveViewerGeometry(mesh.positions, mesh.normals), baseGeometryKey
	return mesh, nil
}

// tangentOutline reports a hull that extrudes along the outline direction GIMI-style buffers keep in TANGENT.
func (m *viewerMesh) tangentOutline() bool {
	return m.hasTangents && !m.tangentUsable && !m.averagedOutline
}

// viewerOutline is the direction each vertex's outline hull extrudes along: the TANGENT direction of a tangent
// outline, or the normals averaged across seams.
func viewerOutline(tangentOutline bool, positions, normals, tangents []float32) []float32 {
	if !tangentOutline {
		return averagedNormals(positions, normals)
	}
	vertices := len(positions) / 3
	outline := make([]float32, vertices*3)
	for v := range vertices {
		copy(outline[v*3:v*3+3], tangents[v*4:v*4+3])
	}
	return outline
}

// viewerVertexExtraStride is the size of a vertex's extra data: its tangent (four floats), the direction its
// outline extrudes along (three floats) and its toon vertex colour (four bytes, read as UNORM).
const viewerVertexExtraStride = 32

func viewerVertexExtras(tangents, outline []float32, colors []byte, vertices int) []byte {
	extra := make([]byte, vertices*viewerVertexExtraStride)
	for v := range vertices {
		at := extra[v*viewerVertexExtraStride:]
		if len(tangents) > 0 {
			for i, value := range tangents[v*4 : v*4+4] {
				binary.LittleEndian.PutUint32(at[i*4:], math.Float32bits(value))
			}
		}
		for i, value := range outline[v*3 : v*3+3] {
			binary.LittleEndian.PutUint32(at[16+i*4:], math.Float32bits(value))
		}
		if len(colors) > 0 {
			copy(at[28:32], colors[v*4:v*4+4])
		}
	}
	return extra
}

// usableTangentFrame samples up to 2000 tangents and accepts them when at most a quarter run along their
// normals, as the React viewer's hasUsableTangentFrame does. sources, when set, maps the vertices of a mesh
// to those of the streams.
func usableTangentFrame(tangents, normals []float32, sources []uint32) bool {
	count := len(tangents) / 4
	if sources != nil {
		count = len(sources)
	}
	if count == 0 {
		return false
	}
	step := max(1, count/2000)
	sampled, alongNormal := 0, 0
	for vertex := 0; vertex < count; vertex += step {
		v := vertex
		if sources != nil {
			v = int(sources[vertex])
		}
		t := vec3{float64(tangents[v*4]), float64(tangents[v*4+1]), float64(tangents[v*4+2])}
		lengthSquared := t.dot(t)
		if math.IsNaN(lengthSquared) || math.IsInf(lengthSquared, 0) || lengthSquared == 0 {
			continue
		}
		sampled++
		if v*3+2 < len(normals) {
			n := vec3{float64(normals[v*3]), float64(normals[v*3+1]), float64(normals[v*3+2])}
			if math.Abs(n.dot(t)) > 0.5 {
				alongNormal++
			}
		}
	}
	return alongNormal*4 < sampled
}

func meanNormal(normals []float32) vec3 {
	var sum vec3
	for i := 0; i+2 < len(normals); i += 3 {
		sum = sum.add(vec3{float64(normals[i]), float64(normals[i+1]), float64(normals[i+2])})
	}
	if sum.dot(sum) == 0 {
		return vec3{0, 0, 1}
	}
	return sum.normalize()
}

// averagedNormals averages the normals of vertices at the same position. Vertices split along UV seams carry
// different normals, which would tear an outline hull open along every seam.
func averagedNormals(positions, normals []float32) []float32 {
	type key [3]int64
	at := func(v int) key {
		var k key
		for i := range 3 {
			k[i] = int64(math.Round(float64(positions[v*3+i]) * 1e5))
		}
		return k
	}
	vertices := len(positions) / 3
	// An open-addressed table: a Go map hashing the three-word key costs most of a mesh's decode.
	size := 1
	for size < vertices*2 {
		size <<= 1
	}
	slotKeys := make([]key, size)
	slotGroups := make([]int32, size)
	for i := range slotGroups {
		slotGroups[i] = -1
	}
	groups := make([]int32, vertices)
	var sums []vec3
	for v := range vertices {
		k := at(v)
		hash := uint64(k[0])*0x9e3779b97f4a7c15 ^ uint64(k[1])*0xc2b2ae3d27d4eb4f ^ uint64(k[2])*0x165667b19e3779f9
		slot := int((hash ^ hash>>29) & uint64(size-1))
		for slotGroups[slot] >= 0 && slotKeys[slot] != k {
			slot = (slot + 1) & (size - 1)
		}
		group := slotGroups[slot]
		if group < 0 {
			group = int32(len(sums))
			slotKeys[slot], slotGroups[slot] = k, group
			sums = append(sums, vec3{})
		}
		groups[v] = group
		sums[group] = sums[group].add(vec3{float64(normals[v*3]), float64(normals[v*3+1]), float64(normals[v*3+2])})
	}
	for i := range sums {
		sums[i] = sums[i].normalize()
	}
	averaged := make([]float32, len(positions))
	for v, group := range groups {
		n := sums[group]
		averaged[v*3], averaged[v*3+1], averaged[v*3+2] = float32(n[0]), float32(n[1]), float32(n[2])
	}
	return averaged
}

// baseGeometryKey names the base positions with no shape target applied.
const baseGeometryKey = "shape"

// float32s copies little-endian float32 values out of data. Windows runs only on little-endian CPUs, so the
// bytes copy as they are.
func float32s(data []byte) []float32 {
	values := make([]float32, len(data)/4)
	if len(values) > 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&values[0])), len(values)*4), data)
	}
	return values
}

// uint32s copies little-endian uint32 values out of data, as float32s does.
func uint32s(data []byte) []uint32 {
	values := make([]uint32, len(data)/4)
	if len(values) > 0 {
		copy(unsafe.Slice((*byte)(unsafe.Pointer(&values[0])), len(values)*4), data)
	}
	return values
}

// decodeViewerPosition reads a position variant: ten float64 bounds, then positions and normals, and four-float
// tangents when the variant carries them. Its extra data starts from the base's, with the variant's tangents and
// the outline direction of its own shape.
func decodeViewerPosition(data []byte, base viewerVariantBase) (*viewerPositionGeometry, error) {
	vertices := base.vertexCount
	if len(data) != 80+vertices*24 && len(data) != 80+vertices*40 {
		return nil, errors.New("invalid model viewer position variant length")
	}
	if len(base.extra) != vertices*viewerVertexExtraStride {
		return nil, errors.New("model viewer position variant has no base geometry")
	}
	positions := float32s(data[80 : 80+vertices*12])
	normals := float32s(data[80+vertices*12 : 80+vertices*24])
	var tangents []float32
	if len(data) > 80+vertices*24 {
		tangents = float32s(data[80+vertices*24:])
	}

	extra := slices.Clone(base.extra)
	if tangents != nil {
		for v := range vertices {
			copy(extra[v*viewerVertexExtraStride:], unsafe.Slice((*byte)(unsafe.Pointer(&tangents[v*4])), 16))
		}
	}
	// A tangent outline without tangents of the variant's own keeps the base's direction.
	if !base.tangentOutline || tangents != nil {
		outline := viewerOutline(base.tangentOutline, positions, normals, tangents)
		for v := range vertices {
			copy(extra[v*viewerVertexExtraStride+16:], unsafe.Slice((*byte)(unsafe.Pointer(&outline[v*3])), 12))
		}
	}
	return &viewerPositionGeometry{current: interleaveViewerGeometry(positions, normals), extra: extra}, nil
}

// vertexNormals averages the face normals around each vertex, weighted by face area.
func vertexNormals(positions []float32, indices []uint32) []float32 {
	normals := make([]float32, len(positions))
	at := func(i uint32) vec3 {
		return vec3{float64(positions[i*3]), float64(positions[i*3+1]), float64(positions[i*3+2])}
	}
	for face := 0; face+2 < len(indices); face += 3 {
		a, b, c := indices[face], indices[face+1], indices[face+2]
		n := at(b).sub(at(a)).cross(at(c).sub(at(a)))
		for _, v := range [3]uint32{a, b, c} {
			normals[v*3] += float32(n[0])
			normals[v*3+1] += float32(n[1])
			normals[v*3+2] += float32(n[2])
		}
	}
	for i := 0; i+2 < len(normals); i += 3 {
		n := vec3{float64(normals[i]), float64(normals[i+1]), float64(normals[i+2])}.normalize()
		normals[i], normals[i+1], normals[i+2] = float32(n[0]), float32(n[1]), float32(n[2])
	}
	return normals
}

func interleaveViewerGeometry(positions, normals []float32) []float32 {
	current := make([]float32, len(positions)*2)
	for v := 0; v*3+2 < len(positions); v++ {
		copy(current[v*6:v*6+3], positions[v*3:v*3+3])
		if v*3+2 < len(normals) {
			copy(current[v*6+3:v*6+6], normals[v*3:v*3+3])
		}
	}
	return current
}

// setGeometry rebuilds current for the mesh's state, when the state changed what it shows. A position variant
// that is not loaded leaves it as it is.
func (m *viewerMesh) setGeometry() {
	if m.computed || m.unloaded || m.cpuDropped {
		return
	}
	state := m.state
	if state.PositionVariant < 0 && len(m.info.PositionVariants) > 0 && m.baseExtra != nil &&
		unsafe.SliceData(m.extra) != unsafe.SliceData(m.baseExtra) {
		m.extra, m.extraDirty = m.baseExtra, true
	}
	key := m.geometryKeyFor(state)
	if key == m.geometryKey && m.current != nil {
		return
	}
	switch {
	case m.next != nil && key == m.nextKey:
		m.promoteNext()
	default:
		current := m.geometryFor(state)
		if current == nil {
			return
		}
		m.current, m.geometryKey, m.dirty = current, key, true
	}
	// A variant dropped from the cache since it became the next frame's geometry keeps the extra shown.
	if variant := m.variants[state.PositionVariant]; state.PositionVariant >= 0 && variant != nil {
		m.extra, m.extraDirty = variant.extra, true
	}
}

// setNext builds the geometry the mesh shows in the animation's next frame, under state, to move towards. The mesh
// has none when it is hidden in either frame, keeps its geometry, or would show a position variant not loaded yet.
// A compute deformer sets the next frame of the meshes it writes itself.
func (m *viewerMesh) setNext(state *modelviewer.ViewerMeshState) {
	if m.computed {
		return
	}
	if state == nil || !state.Visible || !m.state.Visible || m.unloaded || m.cpuDropped {
		m.clearNext()
		return
	}
	key := m.geometryKeyFor(*state)
	if key == m.geometryKey {
		m.clearNext()
		return
	}
	if m.next != nil && key == m.nextKey {
		return
	}
	next := m.geometryFor(*state)
	if next == nil || len(next) != len(m.current) {
		m.clearNext()
		return
	}
	m.next, m.nextKey, m.nextDirty = next, key, true
}

// geometryKeyFor names the geometry state shows: its position variant, or the base under its shape weights.
func (m *viewerMesh) geometryKeyFor(state modelviewer.ViewerMeshState) string {
	if state.PositionVariant >= 0 {
		return variantGeometryKey(state.PositionVariant)
	}
	var key strings.Builder
	key.WriteString(baseGeometryKey)
	for i, weight := range state.ShapeWeights {
		if i < len(m.shapes) && weight != 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0) {
			key.WriteString(":" + strconv.Itoa(i) + "=" + strconv.FormatFloat(weight, 'g', -1, 64))
		}
	}
	return key.String()
}

// geometryFor builds the geometry state shows, laid out as current, or returns nil while its position variant is
// not loaded.
func (m *viewerMesh) geometryFor(state modelviewer.ViewerMeshState) []float32 {
	if state.PositionVariant >= 0 {
		if variant := m.variants[state.PositionVariant]; variant != nil {
			return variant.current
		}
		return nil
	}
	positions, deformed := m.deform(state.ShapeWeights)
	normals := m.normals
	if deformed {
		normals = vertexNormals(positions, m.indices)
	}
	return interleaveViewerGeometry(positions, normals)
}

// deform applies the shape targets at weights to the base positions, as the React viewer's
// applyShapeTargets does.
func (m *viewerMesh) deform(weights []float64) ([]float32, bool) {
	base := m.positions
	midpoints := 0
	for i, target := range m.shapes {
		if target.mode == "midpoint_pair" && i < len(weights) &&
			len(target.endpoint(weights[i], len(base))) == len(base) {
			midpoints++
		}
	}
	divisor := float64(max(midpoints, 1))
	var positions []float32
	for i, target := range m.shapes {
		if i >= len(weights) || math.IsNaN(weights[i]) || math.IsInf(weights[i], 0) {
			continue
		}
		weight := weights[i]
		endpoint, factor, scale := target.positions, weight, 1.0
		if target.mode == "midpoint_pair" {
			endpoint = target.endpoint(weight, len(base))
			factor, scale = weight*4-2, divisor
			if weight <= 0.5 {
				factor = 2 - weight*4
			}
		}
		if factor == 0 || len(endpoint) != len(base) {
			continue
		}
		if positions == nil {
			positions = slices.Clone(base)
		}
		for j := range positions {
			positions[j] += float32(float64(endpoint[j]-base[j]) * factor / scale)
		}
	}
	if positions == nil {
		return base, false
	}
	return positions, true
}

func (t viewerShapeTarget) endpoint(weight float64, length int) []float32 {
	if weight <= 0.5 && t.lowPositions != nil {
		return t.lowPositions
	}
	if len(t.positions) != length {
		return nil
	}
	return t.positions
}

// viewerWorld is the React viewer's initial rotation: models authored Z-up are stood up, then the whole
// model turns by the transport's default yaw.
func viewerWorld(transport modelviewer.ModelViewerTransport) (mat4, vec3, vec3) {
	minimum, maximum, ok := viewerBounds(transport.Meshes, identity4())
	world := identity4()
	if ok {
		size := maximum.sub(minimum)
		if size[2] > size[1]*1.5 && size[2] > size[0]*1.15 {
			world = rotationX(-math.Pi / 2)
		}
	}
	if transport.DefaultYaw != 0 {
		world = rotationY(transport.DefaultYaw * math.Pi / 180).mul(world)
	}
	minimum, maximum, ok = viewerBounds(transport.Meshes, world)
	if !ok {
		return world, vec3{-1, -1, -1}, vec3{1, 1, 1}
	}
	return world, minimum, maximum
}

func viewerBounds(meshes []modelviewer.ModelViewerMeshTransport, world mat4) (vec3, vec3, bool) {
	minimum := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	maximum := vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	found := false
	for _, mesh := range meshes {
		if mesh.Bounds == nil {
			continue
		}
		extendBounds(&minimum, &maximum, mesh.Bounds.Min, mesh.Bounds.Max, world)
		found = true
	}
	return minimum, maximum, found
}

// extendBounds grows minimum and maximum over the box from lo to hi as world places it.
func extendBounds(minimum, maximum *vec3, lo, hi vec3, world mat4) {
	for corner := range 8 {
		p := lo
		for axis := range 3 {
			if corner&(1<<axis) != 0 {
				p[axis] = hi[axis]
			}
		}
		p = world.point(p)
		for axis := range 3 {
			minimum[axis] = min(minimum[axis], p[axis])
			maximum[axis] = max(maximum[axis], p[axis])
		}
	}
}

// waitsFor reports a position variant the wanted states select that its mesh has not built its geometry from.
func (s *viewerScene) waitsFor(i, index int) bool {
	if index < 0 || i >= len(s.wanted) {
		return false
	}
	state, mesh := s.wanted[i], s.meshes[i]
	return state.Visible && state.PositionVariant == index && !mesh.computed &&
		mesh.geometryKey != variantGeometryKey(index)
}

// apply shows states and rebuilds the geometry that changed. The meshes change together, as the React viewer
// commits a frame: while a mesh the states show or a position variant they select is still to be loaded, the
// scene keeps what it shows and apply returns what it waits for.
func (s *viewerScene) apply(states []modelviewer.ViewerMeshState) viewerMissing {
	s.wanted = states
	missing := viewerMissing{variants: map[int]int{}}
	for i, mesh := range s.meshes {
		if i >= len(states) {
			break
		}
		if s.waitsForMesh(i) {
			missing.meshes = append(missing.meshes, i)
		}
		index := states[i].PositionVariant
		if s.waitsFor(i, index) && mesh.variants[index] == nil && !mesh.unreadable[index] {
			missing.variants[i] = index
		}
	}
	s.waiting = !missing.empty()
	if s.waiting {
		return missing
	}

	for i, mesh := range s.meshes {
		if i >= len(states) {
			break
		}
		mesh.state = states[i]
		if !mesh.state.Visible {
			continue
		}
		mesh.setGeometry()
		s.touchMesh(i)
		if mesh.state.PositionVariant >= 0 {
			s.touchVariant(i, mesh.state.PositionVariant)
		}
	}
	return missing
}

// applyNext sets the geometry each mesh shows in the animation's next frame, under states. While the scene waits
// to show the frame before, no mesh has one.
func (s *viewerScene) applyNext(states []modelviewer.ViewerMeshState) {
	for i, mesh := range s.meshes {
		if s.waiting || i >= len(states) {
			mesh.setNext(nil)
			continue
		}
		mesh.setNext(&states[i])
	}
}

// readViewerVariant reads position variant index of the mesh info describes over base. It takes copies of the
// mesh's fields, since the UI thread fills the mesh while variants read.
func readViewerVariant(
	ctx context.Context,
	read func(context.Context, string) ([]byte, error),
	info modelviewer.ModelViewerMeshTransport,
	base viewerVariantBase,
	index int,
) (*viewerPositionGeometry, error) {
	if index < 0 || index >= len(info.PositionVariants) {
		return nil, fmt.Errorf("mesh %s has no position variant %d", info.ID, index)
	}
	data, err := read(ctx, info.PositionVariants[index].GeometryURL)
	if err != nil {
		return nil, err
	}
	return decodeViewerPosition(data, base)
}
