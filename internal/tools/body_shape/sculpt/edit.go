package sculpt

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"

	bodyshape "nahida.live/desktop/internal/tools/body_shape"
)

// BoneSource is one bone of one segment's blend buffer.
type BoneSource struct {
	Segment int
	BoneID  uint32
}

var boneKeyPattern = regexp.MustCompile(`^bone:(\d+):(\d+)$`)

// Key is the stable "bone:<segment>:<bone>" identifier of a bone item.
func (s BoneSource) Key() string {
	return fmt.Sprintf("bone:%d:%d", s.Segment, s.BoneID)
}

func ParseBoneSource(key string) (BoneSource, bool) {
	match := boneKeyPattern.FindStringSubmatch(key)
	if match == nil {
		return BoneSource{}, false
	}
	segment, err := strconv.Atoi(match[1])
	if err != nil {
		return BoneSource{}, false
	}
	bone, err := strconv.ParseUint(match[2], 10, 32)
	if err != nil {
		return BoneSource{}, false
	}
	return BoneSource{Segment: segment, BoneID: uint32(bone)}, true
}

// BoneWeights returns a bone's weights over the whole merged mesh, zero outside
// its segment. Results are cached; callers must not modify them.
func (m *LoadedMesh) BoneWeights(source BoneSource) []float32 {
	if cached, ok := m.boneWeights[source]; ok {
		return cached
	}

	weights := make([]float32, m.VertexCount)
	if source.Segment >= 0 && source.Segment < len(m.Segments) {
		segment := m.Segments[source.Segment]
		if segment.BlendBytes != nil {
			copy(
				weights[segment.VertexOffset:],
				ExtractBoneWeights(segment.BlendBytes, source.BoneID, segment.VertexCount, segment.BlendStride),
			)
		}
	}
	if m.boneWeights == nil {
		m.boneWeights = make(map[BoneSource][]float32)
	}
	m.boneWeights[source] = weights
	return weights
}

// BoneHighlight is the heatmap region for previewing a bone's weights.
func (m *LoadedMesh) BoneHighlight(source BoneSource) ActiveRegionDeform {
	weights := m.BoneWeights(source)
	return ActiveRegionDeform{
		ID:        source.Key(),
		Weights:   weights,
		Amount:    1,
		AxisScale: Vec3{1, 1, 1},
		Pivot:     ComputeRegionPivot(m.OriginalPositions, weights, m.BoundsCenter),
	}
}

// SelectBones replaces the selection with the strongest weight of the bones.
func (m *LoadedMesh) SelectBones(sources []BoneSource) {
	selection := make([]float32, m.VertexCount)
	for _, source := range sources {
		for index, weight := range m.BoneWeights(source) {
			selection[index] = max(selection[index], weight)
		}
	}
	copy(m.SelectionWeights, selection)
	SyncWeldedWeights(m.SelectionWeights, m.WeldMap, BrushPaint)
}

// HasSelection reports whether any vertex is selected.
func (m *LoadedMesh) HasSelection() bool {
	return slices.ContainsFunc(m.SelectionWeights, func(w float32) bool { return w > 0 })
}

func (m *LoadedMesh) ClearSelection() {
	clear(m.SelectionWeights)
}

// SelectionTool decides what a click on the mesh selects.
type SelectionTool int

const (
	// ToolBrush paints with a radius brush.
	ToolBrush SelectionTool = iota
	// ToolComponent selects or clears the whole connected part under the cursor.
	ToolComponent
)

// BrushReach decides how far the brush reaches.
type BrushReach int

const (
	// ReachSurface spreads along connected surface facing the viewer.
	ReachSurface BrushReach = iota
	// ReachThrough paints every vertex within the radius, including hidden ones.
	ReachThrough
)

// StrokeSettings are the brush controls for one dab.
type StrokeSettings struct {
	Tool     SelectionTool
	Reach    BrushReach
	Mode     BrushMode
	Radius   float64
	Strength float64
	MirrorX  bool
	// ShowOriginal makes the through brush measure on the original positions,
	// which the viewport then shows.
	ShowOriginal bool
}

// StrokeInput is where a dab hit the mesh, in mesh space.
type StrokeInput struct {
	Point Vec3
	// Normal is the hit triangle's normal; nil skips the facing filter.
	Normal *Vec3
	// Vertices are the hit triangle's vertices; the nearest seeds the brush.
	Vertices []uint32
}

// StrokeFromHit turns a raycast hit into a stroke input.
func StrokeFromHit(hit RayHit) StrokeInput {
	normal := hit.FaceNormal
	return StrokeInput{Point: hit.Point, Normal: &normal, Vertices: hit.Vertices()}
}

// Stroke applies one brush dab or component pick to the selection, mirrors it
// across x = 0 when asked and keeps seams welded. It reports whether the
// selection changed.
func (m *LoadedMesh) Stroke(input StrokeInput, settings StrokeSettings) bool {
	if settings.Tool == ToolComponent {
		return m.selectComponent(input, settings)
	}

	seed, hasSeed := NearestVertex(m.PreviewPositions, input.Vertices, input.Point)
	surface := settings.Reach == ReachSurface
	var changed []uint32
	if surface && m.Adjacency != nil && hasSeed {
		changed = ApplyGeodesicBrush(GeodesicBrushOptions{
			Positions:       m.OriginalPositions,
			Adjacency:       m.Adjacency,
			Weights:         m.SelectionWeights,
			Seeds:           []uint32{seed},
			Radius:          settings.Radius,
			Strength:        settings.Strength,
			Mode:            settings.Mode,
			FrontFacingOnly: true,
			Normals:         m.OriginalNormals,
			HitNormal:       input.Normal,
			Workspace:       m.GeodesicWorkspace,
		})
	} else {
		options := BrushStrokeOptions{
			Positions: m.PreviewPositions,
			Weights:   m.SelectionWeights,
			HitPoint:  input.Point,
			Radius:    settings.Radius,
			Strength:  settings.Strength,
			Mode:      settings.Mode,
		}
		if settings.ShowOriginal {
			options.Positions = m.OriginalPositions
		}
		if surface {
			options.HitNormal = input.Normal
			options.Normals = m.OriginalNormals
		}
		changed = ApplyBrushStroke(options)
	}
	if len(changed) == 0 {
		return false
	}
	m.finishSelectionEdit(changed, settings)
	return true
}

func (m *LoadedMesh) selectComponent(input StrokeInput, settings StrokeSettings) bool {
	if m.ComponentIDs == nil || input.Vertices == nil {
		return false
	}
	seed, ok := NearestVertex(m.PreviewPositions, input.Vertices, input.Point)
	if !ok || int(seed) >= len(m.ComponentIDs) {
		return false
	}

	mode := ComponentAdd
	if settings.Mode == BrushErase {
		mode = ComponentSubtract
	}
	changed := SelectConnectedComponent(m.SelectionWeights, m.ComponentIDs, m.ComponentIDs[seed], mode, 1)
	if len(changed) == 0 {
		return false
	}
	m.finishSelectionEdit(changed, settings)
	return true
}

func (m *LoadedMesh) finishSelectionEdit(changed []uint32, settings StrokeSettings) {
	if settings.MirrorX {
		MirrorWeightsAcrossX(
			m.OriginalPositions, m.SelectionWeights, changed, symmetryTolerance, settings.Mode, m.SymmetryMap,
		)
	}
	SyncWeldedWeights(m.SelectionWeights, m.WeldMap, settings.Mode)
}

// SmoothSelection blurs the selection edge over the surface. It reports false
// when the mesh has no adjacency.
func (m *LoadedMesh) SmoothSelection() bool {
	if m.Adjacency == nil {
		return false
	}
	SmoothSelectionWeights(m.SelectionWeights, m.Adjacency, 0.5, 2)
	SyncWeldedWeights(m.SelectionWeights, m.WeldMap, BrushPaint)
	return true
}

// GrowSelection extends the selection by one ring of vertices.
func (m *LoadedMesh) GrowSelection() bool {
	if m.Adjacency == nil {
		return false
	}
	GrowSelectionWeights(m.SelectionWeights, m.Adjacency, 1, 1)
	SyncWeldedWeights(m.SelectionWeights, m.WeldMap, BrushPaint)
	return true
}

// ShrinkSelection removes one ring of vertices from the selection edge.
func (m *LoadedMesh) ShrinkSelection() bool {
	if m.Adjacency == nil {
		return false
	}
	ShrinkSelectionWeights(m.SelectionWeights, m.Adjacency, 1, 1)
	SyncWeldedWeights(m.SelectionWeights, m.WeldMap, BrushErase)
	return true
}

// DeformOperation is a deformation offered in the editor. The values are the
// change summary's operation names and the suffix of the label keys.
type DeformOperation string

const (
	DeformScale     DeformOperation = "scale"
	DeformInflate   DeformOperation = "inflate"
	DeformTranslate DeformOperation = "translate"
	DeformTaper     DeformOperation = "taper"
	DeformVolume    DeformOperation = DeformOperation(AnatomyVolume)
	DeformLimb      DeformOperation = DeformOperation(AnatomyLimb)
	DeformTorso     DeformOperation = DeformOperation(AnatomyTorso)
)

// DeformOperations lists the operations in display order.
var DeformOperations = []DeformOperation{
	DeformScale, DeformInflate, DeformTranslate, DeformTaper, DeformVolume, DeformLimb, DeformTorso,
}

// Anatomy returns the anatomy model of an anatomy operation.
func (op DeformOperation) Anatomy() (AnatomyModel, bool) {
	model := AnatomyModel(op)
	_, ok := AnatomyControls[model]
	return model, ok
}

func (op DeformOperation) regionOperation() RegionOperation {
	switch op {
	case DeformInflate:
		return OpInflate
	case DeformTranslate:
		return OpTranslate
	case DeformTaper:
		return OpTaper
	default:
		return OpScale
	}
}

// Operations lists the operations this mesh supports: inflate and volume need
// vertex normals, which an unindexed mesh lacks.
func (m *LoadedMesh) Operations() []DeformOperation {
	return slices.DeleteFunc(slices.Clone(DeformOperations), func(op DeformOperation) bool {
		return m.OriginalNormals == nil && (op == DeformInflate || op == DeformVolume)
	})
}

// DeformControl holds the slider values of the selection deformation.
type DeformControl struct {
	Amount      float64
	AxisScale   Vec3
	Translation Vec3
	TaperFactor float64
	// Anatomy holds the values of AnatomyControls[model] for anatomy operations.
	Anatomy []float64
}

func DefaultDeformControl() DeformControl {
	return DeformControl{AxisScale: Vec3{1, 0.15, 1}, TaperFactor: 0.5}
}

// SelectionPivots are the pivots of the current selection.
type SelectionPivots struct {
	Pivot Vec3
	// VertexPivots is nil unless the selection has separate parts.
	VertexPivots []float32
}

// SelectionPivots computes the pivots for the non-anatomy operations; it
// returns false without a selection.
func (m *LoadedMesh) SelectionPivots() (SelectionPivots, bool) {
	if !m.HasSelection() {
		return SelectionPivots{}, false
	}
	pivots := SelectionPivots{
		Pivot: ComputeRegionPivot(m.OriginalPositions, m.SelectionWeights, m.BoundsCenter),
	}
	if m.Adjacency != nil {
		pivots.VertexPivots = ComputeMaskPivots(m.OriginalPositions, m.SelectionWeights, m.Adjacency)
	}
	return pivots, true
}

// AnatomyBasis builds the anatomy fields of the selection for an anatomy
// operation, with up taken from the viewport orientation (roll, pitch, yaw in
// degrees). Rebuild it when the selection, operation or orientation changes,
// but not per brush dab.
func (m *LoadedMesh) AnatomyBasis(op DeformOperation, orientation Vec3) (*AnatomyBasis, bool) {
	model, ok := op.Anatomy()
	if !ok || !m.HasSelection() {
		return nil, false
	}
	return BuildAnatomyBasis(AnatomyOptions{
		Model:     model,
		Positions: m.OriginalPositions,
		Mask:      m.SelectionWeights,
		Up:        ModelSpaceUp(orientation),
		Adjacency: m.Adjacency,
		Normals:   m.OriginalNormals,
		WeldMap:   m.WeldMap,
	})
}

// ActiveRegions builds the selection's deformation. Anatomy operations need
// the combined anatomy displacement; the others need the selection pivots.
// It returns nil when the needed input is missing.
func (m *LoadedMesh) ActiveRegions(
	op DeformOperation,
	control DeformControl,
	pivots *SelectionPivots,
	displacement []float32,
) []ActiveRegionDeform {
	if _, ok := op.Anatomy(); ok {
		if displacement == nil {
			return nil
		}
		return []ActiveRegionDeform{{
			ID:           "selection",
			Weights:      m.SelectionWeights,
			Amount:       1,
			AxisScale:    Vec3{1, 1, 1},
			Pivot:        m.BoundsCenter,
			Operation:    OpDisplace,
			Displacement: displacement,
		}}
	}
	if pivots == nil {
		return nil
	}

	amount := control.Amount
	if op == DeformTranslate {
		amount = 1
	}
	return []ActiveRegionDeform{{
		ID:           "selection",
		Weights:      m.SelectionWeights,
		Amount:       amount,
		AxisScale:    control.AxisScale,
		Pivot:        pivots.Pivot,
		VertexPivots: pivots.VertexPivots,
		Operation:    op.regionOperation(),
		Normals:      m.OriginalNormals,
		Translation:  control.Translation,
		TaperFactor:  control.TaperFactor,
	}}
}

// UpdatePreview writes the deformed positions into PreviewPositions, or the
// originals when showOriginal is set or nothing deforms.
func (m *LoadedMesh) UpdatePreview(regions []ActiveRegionDeform, showOriginal bool) DeformMetrics {
	if showOriginal || len(regions) == 0 {
		copy(m.PreviewPositions, m.OriginalPositions)
		return DeformMetrics{VertexCount: m.VertexCount}
	}
	return ApplyMultiRegionDeform(m.OriginalPositions, m.PreviewPositions, regions, DefaultDisplacementEpsilon)
}

// ChangeSummary describes an export for the change log.
func ChangeSummary(op DeformOperation, control DeformControl, metrics DeformMetrics) bodyshape.BodyShapeChangeSummary {
	summary := bodyshape.BodyShapeChangeSummary{
		Operation:       string(op),
		Amount:          control.Amount,
		AxisScale:       control.AxisScale[:],
		Translation:     control.Translation[:],
		TaperFactor:     control.TaperFactor,
		MovedVertices:   metrics.MovedVertices,
		MaxDisplacement: metrics.MaxDisplacement,
	}
	if model, ok := op.Anatomy(); ok {
		summary.Controls = make(map[string]float64)
		for index, name := range AnatomyControls[model] {
			value := 0.0
			if index < len(control.Anatomy) {
				value = control.Anatomy[index]
			}
			summary.Controls[name] = value
		}
	}
	return summary
}
