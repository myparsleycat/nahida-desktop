package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"
	"github.com/samber/lo"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/tools/body_shape/sculpt"
)

// bodyShapeMaxCachedSelections is how many loaded mesh selections an editor keeps, as the React session did.
const bodyShapeMaxCachedSelections = 8

// Changes that bodyShapeEditor.update derives the preview from.
const (
	// bodyShapeMask: the selection weights changed.
	bodyShapeMask = 1 << iota
	// bodyShapeShape: the operation or the model's turn changed, so the pivots and anatomy fields are rebuilt.
	bodyShapeShape
)

// bodyShapeEditor is one body shape editor: the tools page's or the dialog the mod screen opens for one mod.
// It edits a selection mask over the chosen meshes and deforms the selection. Its state belongs to the UI
// thread.
type bodyShapeEditor struct {
	shell *Shell
	// id keeps the keys of the page's editor and the dialog's apart.
	id string
	// fixedPath is the mod a dialog editor is bound to; empty on the tools page.
	fixedPath, modName string
	onExported         func()

	pathDraft string
	loading   bool
	loadSeq   int
	meshSeq   int
	cancel    context.CancelFunc
	session   *tools.BodyShapeSessionDescriptor
	cache     map[string]*sculpt.LoadedMesh
	// cacheOrder lists the cached selection keys, oldest first.
	cacheOrder []string
	mesh       *sculpt.LoadedMesh

	boneKeys  []string
	boneQuery string
	// highlight is the bone whose weights the heatmap previews while the pointer is on its row.
	highlight string

	op           sculpt.DeformOperation
	control      sculpt.DeformControl
	showOriginal bool
	showWeights  bool

	brushOn  bool
	brush    sculpt.StrokeSettings
	drafts   map[string]string
	painting bool
	// strokeBefore is the selection when the brush stroke being painted began.
	strokeBefore []float32
	history      sculpt.SelectionHistory

	pivots       *sculpt.SelectionPivots
	basis        *sculpt.AnatomyBasis
	displacement []float32
	regions      []sculpt.ActiveRegionDeform
	metrics      sculpt.DeformMetrics
	hasSelection bool
	colors       []float32
	display      []float32

	exporting bool
	view      sculptViewport
}

func newBodyShapeEditor(s *Shell, id string) *bodyShapeEditor {
	e := &bodyShapeEditor{shell: s, id: id, showWeights: true, drafts: map[string]string{}}
	e.brush = sculpt.StrokeSettings{Radius: 0.15, Strength: 0.1, MirrorX: true}
	e.resetDeform()
	return e
}

// bodyShapePage returns the tools page's editor, made on first use.
func (s *Shell) bodyShapePage() *bodyShapeEditor {
	if s.tools.bodyShape == nil {
		s.tools.bodyShape = newBodyShapeEditor(s, "page")
	}
	return s.tools.bodyShape
}

// closeBodyShapePage frees the tools page's editor when another tool is chosen, as the React tool unmounted.
func (s *Shell) closeBodyShapePage() {
	if e := s.tools.bodyShape; e != nil {
		s.tools.bodyShape = nil
		e.close()
	}
}

// openBodyShapeDialog edits the mod at modPath in a dialog over the main window, as the mod screen's menu did.
func (s *Shell) openBodyShapeDialog(modPath, modName string) {
	if s.tools.bodyShapeDialog != nil || s.deps.Tools == nil {
		return
	}
	e := newBodyShapeEditor(s, "dialog")
	e.fixedPath, e.modName = modPath, modName
	e.onExported = func() {
		s.refreshListing()
		s.closeBodyShapeDialog()
	}
	s.tools.bodyShapeDialog = e
	e.loadMod(modPath)
}

func (s *Shell) closeBodyShapeDialog() {
	if e := s.tools.bodyShapeDialog; e != nil {
		s.tools.bodyShapeDialog = nil
		e.close()
	}
}

// close cancels loading, closes the backend session and frees the meshes.
func (e *bodyShapeEditor) close() {
	e.loadSeq++
	e.cancelMeshLoad()
	e.closeSession()
	e.view.close()
}

func (e *bodyShapeEditor) closeSession() {
	session := e.session
	e.session, e.mesh, e.cache, e.cacheOrder = nil, nil, nil, nil
	if session == nil {
		return
	}
	service := e.shell.deps.Tools
	go func() { _, _ = service.BodyShapeCloseSession(context.Background(), session.SessionID) }()
}

func (e *bodyShapeEditor) cancelMeshLoad() {
	e.meshSeq++
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

func (e *bodyShapeEditor) report(err error, operation string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["modPath"] = e.modPath()
	_ = infra.ReportError(e.shell.deps.Log, err, "BodyShape", infra.Diagnostic{Operation: operation, Fields: fields})
}

func (e *bodyShapeEditor) modPath() string {
	if e.session != nil {
		return e.session.ModRoot
	}
	if e.fixedPath != "" {
		return e.fixedPath
	}
	return e.pathDraft
}

// loadMod opens a backend session on the mod folder and loads its first mesh.
func (e *bodyShapeEditor) loadMod(path string) {
	path = strings.TrimSpace(path)
	service := e.shell.deps.Tools
	if path == "" || service == nil {
		return
	}
	e.loadSeq++
	seq := e.loadSeq
	e.cancelMeshLoad()
	e.closeSession()
	e.view.clear()
	e.loading = true
	go func() {
		descriptor, err := service.BodyShapeLoadMod(context.Background(), path)
		fairy.Go(func() {
			if seq != e.loadSeq {
				if err == nil {
					go func() { _, _ = service.BodyShapeCloseSession(context.Background(), descriptor.SessionID) }()
				}
				return
			}
			if err != nil {
				e.loading = false
				e.report(err, "load-mod", map[string]any{"path": path})
				e.shell.toasts.Show(kit.ToastError, T("page.tools.body_shape.toast.load_failed"), err.Error())
				return
			}
			e.session = &descriptor
			e.cache = map[string]*sculpt.LoadedMesh{}
			e.resetEditing()
			e.view.orientation = vec3{}
			e.showWeights = true
			if len(descriptor.Meshes) == 0 {
				e.loading = false
				return
			}
			e.loadMeshes([]string{descriptor.Meshes[0].ID})
		})
	}()
}

// resetEditing clears what belongs to one mesh selection, as the React tool did when the meshes changed.
func (e *bodyShapeEditor) resetEditing() {
	e.boneKeys, e.boneQuery, e.highlight = nil, "", ""
	e.history.Reset()
	e.strokeBefore, e.painting = nil, false
	e.op = sculpt.DeformScale
	e.resetDeform()
}

func (e *bodyShapeEditor) resetDeform() {
	e.control = sculpt.DefaultDeformControl()
}

// loadMeshes shows the meshes with these IDs merged into one surface, from the cache or loaded in the
// background.
func (e *bodyShapeEditor) loadMeshes(ids []string) {
	if e.session == nil {
		return
	}
	summaries := make([]tools.BodyShapeMeshSummary, 0, len(ids))
	for _, mesh := range e.session.Meshes {
		if slices.Contains(ids, mesh.ID) {
			summaries = append(summaries, mesh)
		}
	}
	if len(summaries) == 0 {
		return
	}
	ids = make([]string, len(summaries))
	for i, summary := range summaries {
		ids[i] = summary.ID
	}
	e.cancelMeshLoad()
	key := sculpt.MeshSelectionKey(ids)
	if cached := e.cache[key]; cached != nil {
		e.loading = false
		e.setMesh(cached)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.loading = true
	seq, sessionID, service := e.meshSeq, e.session.SessionID, e.shell.deps.Tools
	go func() {
		mesh, err := loadBodyShapeMesh(ctx, service, sessionID, ids)
		fairy.Go(func() {
			if seq != e.meshSeq {
				return
			}
			e.cancel = nil
			cancel()
			e.loading = false
			if err != nil {
				if !infra.IsCancellationError(err) {
					e.report(err, "load-mesh", map[string]any{"meshIds": ids})
					e.shell.toasts.Show(kit.ToastError, T("page.tools.body_shape.toast.load_failed"), err.Error())
				}
				return
			}
			if len(e.cacheOrder) >= bodyShapeMaxCachedSelections {
				delete(e.cache, e.cacheOrder[0])
				e.cacheOrder = e.cacheOrder[1:]
			}
			e.cache[key] = mesh
			e.cacheOrder = append(e.cacheOrder, key)
			e.setMesh(mesh)
		})
	}()
}

func loadBodyShapeMesh(
	ctx context.Context,
	service *tools.Tools,
	sessionID string,
	ids []string,
) (*sculpt.LoadedMesh, error) {
	inputs := make([]sculpt.MeshInput, 0, len(ids))
	for _, id := range ids {
		data, err := service.BodyShapeMeshData(ctx, tools.BodyShapeMeshInput{SessionID: sessionID, MeshID: id})
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, sculpt.MeshInput{
			ID:          data.ID,
			Name:        data.Name,
			VertexCount: data.VertexCount,
			Positions:   data.Positions,
			Indices:     data.Indices,
			BlendBytes:  data.BlendBytes,
			BlendStride: data.BlendStride,
			Bones:       data.Bones,
		})
	}
	return sculpt.LoadMesh(ctx, inputs)
}

func (e *bodyShapeEditor) setMesh(mesh *sculpt.LoadedMesh) {
	e.mesh = mesh
	e.colors = make([]float32, mesh.VertexCount*3)
	e.display = make([]float32, mesh.VertexCount)
	e.metrics = sculpt.DeformMetrics{}
	// A cached selection may still hold the deformation it was left with.
	copy(mesh.PreviewPositions, mesh.OriginalPositions)
	e.view.show(mesh.ID, mesh.PreviewPositions, mesh.Indices)
	e.update(bodyShapeMask | bodyShapeShape)
}

// selectMeshes changes the edited meshes, starting the selection and deformation over.
func (e *bodyShapeEditor) selectMeshes(ids []string) {
	if len(ids) == 0 {
		return
	}
	e.resetEditing()
	e.loadMeshes(ids)
}

// update derives the deformation, the preview positions, the metrics and the heatmap from the editor's state.
// change says what changed besides the sliders.
func (e *bodyShapeEditor) update(change int) {
	m := e.mesh
	if m == nil {
		return
	}
	_, anatomy := e.op.Anatomy()
	if change != 0 {
		e.hasSelection = m.HasSelection()
		e.pivots = nil
		if !anatomy {
			if pivots, ok := m.SelectionPivots(); ok {
				e.pivots = &pivots
			}
		}
		switch {
		case !anatomy:
			e.basis = nil
		// Rebuilding the anatomy fields per dab would stall painting; the stroke's end rebuilds them.
		case change&bodyShapeShape != 0 || e.strokeBefore == nil:
			e.basis, _ = m.AnatomyBasis(e.op, sculpt.Vec3(e.view.orientation))
		}
	}
	e.displacement = nil
	if e.basis != nil {
		e.displacement = sculpt.CombineAnatomyBasis(e.basis, e.control.Anatomy, nil)
	}
	e.regions = m.ActiveRegions(e.op, e.control, e.pivots, e.displacement)

	moved := e.metrics.MovedVertices
	e.metrics = m.UpdatePreview(e.regions, e.showOriginal)
	if moved > 0 || e.metrics.MovedVertices > 0 {
		e.view.setPositions(m.PreviewPositions)
	}
	e.paintColors()
}

// paintColors shows the heatmap of the deformed selection, or of the previewed bone.
func (e *bodyShapeEditor) paintColors() {
	m := e.mesh
	if m == nil {
		return
	}
	if !e.showWeights {
		e.view.setColors(nil)
		return
	}
	regions := e.regions
	if source, ok := sculpt.ParseBoneSource(e.highlight); ok && e.highlight != "" {
		regions = []sculpt.ActiveRegionDeform{m.BoneHighlight(source)}
	}
	if e.showOriginal {
		regions = nil
	}
	sculpt.WriteRegionHeatmap(e.colors, e.display, regions)
	e.view.setColors(e.colors)
}

// commitSelection records the change from before in the undo history and reports whether there was one.
func (e *bodyShapeEditor) commitSelection(before []float32) bool {
	return e.mesh != nil && e.history.Commit(before, e.mesh.SelectionWeights)
}

// editSelection runs an edit of the whole selection as one undo step.
func (e *bodyShapeEditor) editSelection(edit func(m *sculpt.LoadedMesh)) {
	m := e.mesh
	if m == nil {
		return
	}
	before := slices.Clone(m.SelectionWeights)
	edit(m)
	e.commitSelection(before)
	e.update(bodyShapeMask)
}

func (e *bodyShapeEditor) selectBones(keys []string) {
	e.boneKeys = keys
	e.editSelection(func(m *sculpt.LoadedMesh) {
		sources := make([]sculpt.BoneSource, 0, len(keys))
		for _, key := range keys {
			if source, ok := sculpt.ParseBoneSource(key); ok {
				sources = append(sources, source)
			}
		}
		m.SelectBones(sources)
	})
}

func (e *bodyShapeEditor) clearSelection() {
	e.boneKeys = nil
	e.editSelection(func(m *sculpt.LoadedMesh) { m.ClearSelection() })
}

func (e *bodyShapeEditor) undo() {
	if e.mesh != nil && e.history.UndoInto(e.mesh.SelectionWeights) {
		e.boneKeys = nil
		e.update(bodyShapeMask)
	}
}

func (e *bodyShapeEditor) redo() {
	if e.mesh != nil && e.history.RedoInto(e.mesh.SelectionWeights) {
		e.boneKeys = nil
		e.update(bodyShapeMask)
	}
}

// setOperation switches the deformation, keeping only the axis scale and taper of the sliders.
func (e *bodyShapeEditor) setOperation(op sculpt.DeformOperation) {
	e.op = op
	e.control.Amount, e.control.Translation, e.control.Anatomy = 0, sculpt.Vec3{}, nil
	e.update(bodyShapeShape)
}

func (e *bodyShapeEditor) setControl(change func(c *sculpt.DeformControl)) {
	change(&e.control)
	e.update(0)
}

func (e *bodyShapeEditor) setAnatomy(index int, value float64) {
	e.setControl(func(c *sculpt.DeformControl) {
		anatomy := slices.Clone(c.Anatomy)
		for len(anatomy) <= index {
			anatomy = append(anatomy, 0)
		}
		anatomy[index] = value
		c.Anatomy = anatomy
	})
}

func (e *bodyShapeEditor) rotate(delta vec3) {
	e.view.rotate(delta)
	e.update(bodyShapeShape)
}

func (e *bodyShapeEditor) resetView() {
	e.view.resetView()
	e.update(bodyShapeShape)
}

// hit casts the ray under a point of the viewport against the mesh as drawn.
func (e *bodyShapeEditor) hit(x, y float32) (sculpt.RayHit, bool) {
	if e.mesh == nil {
		return sculpt.RayHit{}, false
	}
	origin, direction := e.view.modelRay(x, y)
	return sculpt.Raycast(e.mesh.PreviewPositions, e.mesh.Indices, sculpt.Vec3(origin), sculpt.Vec3(direction))
}

// showRing puts the brush cursor on the surface under the pointer, or hides it off the mesh.
func (e *bodyShapeEditor) showRing(hit sculpt.RayHit, ok bool) {
	if !ok || !e.brushOn {
		e.view.setRing(nil)
		return
	}
	normal := vec3(hit.FaceNormal)
	color := [4]float32{0x3b / 255.0, 0x82 / 255.0, 0xf6 / 255.0, 1}
	if e.brush.Mode == sculpt.BrushErase {
		color = [4]float32{0xef / 255.0, 0x44 / 255.0, 0x44 / 255.0, 1}
	}
	const opacity = 0.85
	e.view.setRing(&viewerRing{
		center: vec3(hit.Point).add(normal.scale(0.001)),
		normal: normal,
		radius: e.brush.Radius,
		color:  [4]float32{color[0] * opacity, color[1] * opacity, color[2] * opacity, opacity},
	})
}

func (e *bodyShapeEditor) hover(x, y float32) {
	if !e.brushOn {
		e.view.setRing(nil)
		return
	}
	e.showRing(e.hit(x, y))
}

func (e *bodyShapeEditor) beginStroke() {
	if e.mesh != nil && e.strokeBefore == nil {
		e.strokeBefore = slices.Clone(e.mesh.SelectionWeights)
	}
}

// dab applies the brush or picks a component at a point of the viewport.
func (e *bodyShapeEditor) dab(x, y float32) {
	hit, ok := e.hit(x, y)
	e.showRing(hit, ok)
	if !ok {
		return
	}
	settings := e.brush
	settings.ShowOriginal = e.showOriginal
	if e.mesh.Stroke(sculpt.StrokeFromHit(hit), settings) {
		e.update(bodyShapeMask)
	}
}

func (e *bodyShapeEditor) endStroke() {
	before := e.strokeBefore
	e.strokeBefore, e.painting = nil, false
	if before != nil && e.commitSelection(before) {
		e.update(bodyShapeMask)
	}
}

func (e *bodyShapeEditor) setRadius(radius float64) {
	e.brush.Radius = min(max(radius, 0.01), 2)
	if ring := e.view.ring; ring != nil {
		next := *ring
		next.radius = e.brush.Radius
		e.view.setRing(&next)
	}
}

func (e *bodyShapeEditor) export() {
	m, session := e.mesh, e.session
	if m == nil || session == nil || e.exporting {
		return
	}
	// The export takes the deformation even while the original shows, as the React tool did.
	metrics := m.UpdatePreview(e.regions, false)
	summary := sculpt.ChangeSummary(e.op, e.control, metrics)
	writeLog := true
	input := tools.BodyShapeCommitPositionsInput{
		SessionID: session.SessionID, WriteChangeLog: &writeLog, ChangeSummary: &summary,
	}
	for index, segment := range m.Segments {
		input.Meshes = append(input.Meshes, tools.BodyShapeMeshPositions{
			MeshID: segment.MeshID, Positions: slices.Clone(m.SegmentPreview(index)),
		})
	}
	e.update(0)

	e.exporting = true
	s := e.shell
	s.background(func(ctx context.Context) {
		result, err := s.deps.Tools.BodyShapeCommitPositions(ctx, input)
		fairy.Go(func() {
			e.exporting = false
			if err != nil {
				e.report(err, "export", map[string]any{"meshIds": m.MeshIDs, "movedVertices": metrics.MovedVertices})
				s.toasts.Show(kit.ToastError, T("page.tools.body_shape.toast.export_failed"), err.Error())
				return
			}
			modRoot := lo.FromPtr(result.ModRoot)
			var description string
			switch {
			case modRoot != "":
				description = T("page.tools.body_shape.toast.exported_as_mod", "path", modRoot)
			case lo.FromPtr(result.ChangeLogPath) != "":
				description = T("page.tools.body_shape.toast.exported_with_log")
			default:
				bytes := lo.SumBy(result.Meshes, func(mesh tools.BodyShapeExportedMesh) int {
					return mesh.PositionBytes
				})
				description = T("page.tools.body_shape.toast.exported_description", "bytes", bytes)
			}
			s.toasts.Show(kit.ToastSuccess, T("page.tools.body_shape.toast.exported"), description)
			switch {
			case e.onExported != nil:
				e.onExported()
			case modRoot != "":
				e.pathDraft = modRoot
				e.loadMod(modRoot)
			}
		})
	})
}
