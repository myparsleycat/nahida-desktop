package ui

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// computeGeometryKey names the geometry a compute deformer wrote.
const computeGeometryKey = "compute"

// viewerCompute plays the compute deformer of the clip being shown. Its kernel runs on a goroutine of its
// own, one frame at a time, and the newest frame asked for while it runs is the next one, as with the React
// viewer's compute worker. Everything but the kernel belongs to the UI thread.
type viewerCompute struct {
	scene    *viewerScene
	deformer modelviewer.ModelViewerComputeDeformerTransport
	playback *modelviewer.ComputePlayback
	cancel   context.CancelFunc
	meshes   []viewerComputeMesh
	// kernel is set once its sources are read.
	kernel *modelviewer.ComputeDeformer
	busy   bool
	failed bool
	// wroteExtra marks tangents written over those the meshes had.
	wroteExtra bool
	// latest is the frame to compute next, and last the one asked for most recently.
	latest, last *viewerComputeRequest
}

type viewerComputeRequest struct {
	clip  *modelviewer.ModelViewerAnimationClip
	frame int
	// duration is the length of the clip in seconds at the rate it plays at.
	duration float64
}

// viewerComputeMesh is a mesh the deformer writes, with what it looked like before.
type viewerComputeMesh struct {
	index       int
	vertexCount int
	// sources maps the vertices of the mesh to those of the deformer; nil when they are the same.
	sources         []uint32
	sourcesURL      string
	extra           []byte
	hasTangents     bool
	tangentUsable   bool
	averagedOutline bool
}

type viewerComputeResult struct {
	current []float32
	// extra is nil for a frame that leaves the tangents as they are.
	extra         []byte
	tangentUsable bool
}

// syncCompute starts the deformer of the clip being played and stops the one of the clip before it.
func (v *ModelViewer) syncCompute() {
	id := ""
	if clip := v.animationClip(); clip != nil {
		id = clip.DeformerID
	}
	if c := v.compute; c != nil {
		if c.scene == v.scene && c.deformer.ID == id {
			return
		}
		v.stopCompute(true)
		v.present()
	}
	scene := v.scene
	if scene == nil || id == "" {
		return
	}
	at := slices.IndexFunc(
		scene.transport.ComputeDeformers,
		func(d modelviewer.ModelViewerComputeDeformerTransport) bool { return d.ID == id },
	)
	if at < 0 {
		return
	}

	deformer := scene.transport.ComputeDeformers[at]
	ctx, cancel := context.WithCancel(v.ctx)
	c := &viewerCompute{
		scene: scene, deformer: deformer, playback: modelviewer.NewComputePlayback(deformer), cancel: cancel,
	}
	for index, mesh := range scene.meshes {
		if !slices.Contains(deformer.MeshIDs, mesh.info.ID) {
			continue
		}
		url := deformer.MeshSourceIndices[mesh.info.ID]
		if url == "" {
			url = mesh.info.SourceIndicesURL
		}
		c.meshes = append(c.meshes, viewerComputeMesh{
			index: index, vertexCount: mesh.vertexCount, sourcesURL: url,
			extra: mesh.extra, hasTangents: mesh.hasTangents, tangentUsable: mesh.tangentUsable,
			averagedOutline: mesh.averagedOutline,
		})
	}
	v.compute = c
	if len(c.meshes) == 0 {
		v.failCompute(c, fmt.Errorf("compute deformer %s did not match any rendered mesh", deformer.ID), nil)
		return
	}
	c.playback.Observe(v.resolved)

	meshes := c.meshes
	go func() {
		kernel, err := modelviewer.NewComputeDeformer(ctx, deformer, v.read)
		sources := make([][]uint32, len(meshes))
		for i, mesh := range meshes {
			if err != nil {
				break
			}
			sources[i], err = readComputeSourceIndices(ctx, v.read, scene.meshes[mesh.index].info.ID, mesh, kernel)
		}
		fairy.Go(func() {
			if v.compute != c || ctx.Err() != nil {
				return
			}
			if err != nil {
				v.failCompute(c, err, nil)
				return
			}
			for i := range c.meshes {
				c.meshes[i].sources = sources[i]
			}
			c.kernel = kernel
			v.dispatchCompute()
		})
	}()
}

// readComputeSourceIndices reads the deformer vertex of each vertex of mesh, or nil when they are the same.
func readComputeSourceIndices(
	ctx context.Context,
	read viewerReader,
	id string,
	mesh viewerComputeMesh,
	kernel *modelviewer.ComputeDeformer,
) ([]uint32, error) {
	if mesh.sourcesURL == "" {
		if mesh.vertexCount > kernel.VertexCount() {
			return nil, fmt.Errorf("compute mesh %s has %d vertices for a deformer of %d",
				id, mesh.vertexCount, kernel.VertexCount())
		}
		return nil, nil
	}
	data, err := read(ctx, mesh.sourcesURL)
	if err != nil {
		return nil, fmt.Errorf("compute mesh %s source indices: %w", id, err)
	}
	if len(data) != mesh.vertexCount*4 {
		return nil, fmt.Errorf("compute mesh %s source index count is invalid", id)
	}
	sources := make([]uint32, mesh.vertexCount)
	for i := range sources {
		sources[i] = binary.LittleEndian.Uint32(data[i*4:])
		if int64(sources[i]) >= int64(kernel.VertexCount()) {
			return nil, fmt.Errorf("compute mesh %s source index %d is outside the vertex buffer", id, sources[i])
		}
	}
	return sources, nil
}

// requestCompute asks for frame index of clip, when the running deformer is the clip's.
func (v *ModelViewer) requestCompute(clip *modelviewer.ModelViewerAnimationClip, index int) {
	c := v.compute
	if c == nil || c.failed || clip.DeformerID != c.deformer.ID {
		return
	}
	request := &viewerComputeRequest{
		clip: clip, frame: index, duration: float64(len(clip.Frames)) / v.animationFPS(clip),
	}
	c.latest, c.last = request, request
	v.dispatchCompute()
}

// observeCompute hands the toggle state to the deformer; one that follows it computes its frame again, so a
// paused clip still shows the change.
func (v *ModelViewer) observeCompute() {
	c := v.compute
	if c == nil || c.failed {
		return
	}
	c.playback.Observe(v.resolved)
	if c.playback.FollowsState() && c.last != nil {
		c.latest = c.last
		v.dispatchCompute()
	}
}

// dispatchCompute computes the newest frame asked for, unless the kernel is still reading its sources or
// running an earlier frame.
func (v *ModelViewer) dispatchCompute() {
	c := v.compute
	if c == nil || c.failed || c.kernel == nil || c.busy || c.latest == nil {
		return
	}
	request := c.latest
	c.latest = nil
	if request.frame < 0 || request.frame >= len(request.clip.Frames) {
		return
	}
	frame := request.clip.Frames[request.frame]
	phase, weights := c.playback.Advance(request.clip.ID, frame.Time, request.duration, v.resolved)
	c.busy = true
	kernel, meshes := c.kernel, c.meshes
	go func() {
		results, err := computeViewerFrame(kernel, meshes, frame.Index, phase, weights)
		fairy.Go(func() {
			if v.compute != c {
				return
			}
			c.busy = false
			if err != nil {
				v.failCompute(c, err, request)
				return
			}
			for i, result := range results {
				mesh := c.scene.meshes[meshes[i].index]
				mesh.current, mesh.geometryKey = result.current, computeGeometryKey
				mesh.computed, mesh.dirty = true, true
				mesh.tangentUsable = result.tangentUsable
				if result.extra != nil {
					mesh.extra, mesh.extraDirty, c.wroteExtra = result.extra, true, true
				}
			}
			v.revision++
			v.dispatchCompute()
		})
	}()
}

// computeViewerFrame runs the kernel and lays its frame out for each mesh. The slices it returns are new, as
// the draw and a GPU upload may still read the ones before them.
func computeViewerFrame(
	kernel *modelviewer.ComputeDeformer,
	meshes []viewerComputeMesh,
	poseFrame int,
	phase float64,
	weights []float64,
) ([]viewerComputeResult, error) {
	frame, err := kernel.Frame(poseFrame, phase, weights)
	if err != nil {
		return nil, err
	}
	results := make([]viewerComputeResult, len(meshes))
	for i, mesh := range meshes {
		source := func(vertex int) int {
			if mesh.sources != nil {
				return int(mesh.sources[vertex])
			}
			return vertex
		}
		current := make([]float32, mesh.vertexCount*6)
		for vertex := range mesh.vertexCount {
			from := source(vertex) * 3
			copy(current[vertex*6:vertex*6+3], frame.Positions[from:from+3])
			copy(current[vertex*6+3:vertex*6+6], frame.Normals[from:from+3])
		}
		result := viewerComputeResult{current: current, tangentUsable: mesh.tangentUsable}
		switch {
		case frame.Tangents != nil:
			// GIMI-style buffers keep the outline's direction in TANGENT, which then moves with the frame.
			outline := mesh.hasTangents && !mesh.tangentUsable && !mesh.averagedOutline
			extra := slices.Clone(mesh.extra)
			for vertex := range mesh.vertexCount {
				at := extra[vertex*viewerVertexExtraStride:]
				from := source(vertex) * 4
				for axis, value := range frame.Tangents[from : from+4] {
					binary.LittleEndian.PutUint32(at[axis*4:], math.Float32bits(value))
				}
				if outline {
					copy(at[16:28], at[:12])
				}
			}
			result.extra = extra
			result.tangentUsable = usableTangentFrame(frame.Tangents, frame.Normals, mesh.sources)
		case !kernel.KeepsTangents():
			// The mesh's tangents no longer fit the frame, so the shader derives a frame where it may.
			result.tangentUsable = false
		}
		results[i] = result
	}
	return results, nil
}

// stopCompute ends the running deformer. restore gives its meshes back the geometry their toggle state
// selects, once the scene is presented again.
func (v *ModelViewer) stopCompute(restore bool) {
	c := v.compute
	if c == nil {
		return
	}
	v.compute = nil
	c.cancel()
	if restore {
		c.restore()
		v.revision++
	}
}

func (c *viewerCompute) restore() {
	for _, baseline := range c.meshes {
		mesh := c.scene.meshes[baseline.index]
		if !mesh.computed {
			continue
		}
		mesh.computed, mesh.geometryKey = false, ""
		mesh.tangentUsable = baseline.tangentUsable
		if c.wroteExtra {
			mesh.extra, mesh.extraDirty = baseline.extra, true
		}
	}
}

// failCompute reports a deformer that cannot run, shows its meshes as they were and stops playback. request
// is the frame that failed, or nil for a failure before any.
func (v *ModelViewer) failCompute(c *viewerCompute, err error, request *viewerComputeRequest) {
	if c.failed {
		return
	}
	c.failed, c.latest = true, nil
	c.cancel()
	c.restore()
	v.revision++
	v.present()
	v.setAnimationPlaying(false)

	fields := map[string]any{"modPath": v.path, "deformerId": c.deformer.ID, "kind": string(c.deformer.Kind)}
	if request != nil {
		fields["clipId"] = request.clip.ID
		fields["frame"] = request.clip.Frames[request.frame].Index
	}
	err = infra.ReportError(v.log, err, "ModelViewer.compute", infra.Diagnostic{
		Operation: "compute-deformer", Fields: fields,
	})
	v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.load_error"), err.Error())
}
