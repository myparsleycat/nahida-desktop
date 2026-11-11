package ui

import (
	"math"

	liquidemitter "nahida.live/desktop/internal/tools/liquid_emitter"
)

const (
	liquidAnchorColor = 0xffc240
	// The mod's other emitters, shown for reference while one is edited.
	liquidMutedColor = 0x9a9a9a
	liquidPathColor  = 0x4de8ff
	liquidBeadColor  = 0xffde14
	// The marker is sized in meters: an arrow of 18 cm, four fifths shaft and one fifth head.
	liquidArrowLength = 0.18
	liquidBeadCount   = 24
	// Tracers play at a tenth of real time so the path can be followed by eye.
	liquidPreviewSpeed = 0.1
)

// overlay draws the edited emitter and, muted, the mod's other emitters shown in the view.
func (lt *liquidTool) overlay() *viewerOverlayDraw {
	if lt.viewer.scene == nil {
		return nil
	}
	draw := &viewerOverlayDraw{}
	visible := lt.viewer.visibleMeshes()
	current := lt.current()
	for _, emitter := range lt.emitters() {
		if current != nil && emitter.PID == current.PID {
			continue
		}
		saved := liquidAnchorFromEmitter(emitter)
		if saved == nil {
			continue
		}
		form := ""
		if mesh := lt.mesh(saved.meshID); mesh != nil {
			form = mesh.Form
		}
		if formVisible(form, lt.viewer.form) && lt.drawn(saved.meshID, visible) {
			lt.marker(draw, *saved, overlayColor(liquidMutedColor), nil)
		}
	}

	anchor := lt.anchor()
	if inForm, hidden := lt.anchorView(visible); anchor != nil && inForm && !hidden {
		look := lt.draftLook()
		if look == nil {
			look = lt.savedLook()
		}
		var trajectory *liquidemitter.Look
		if lt.showTrajectory && look != nil && look.Nozzle != liquidemitter.NozzleOoze {
			trajectory = look
		}
		lt.marker(draw, *anchor, overlayColor(liquidAnchorColor), trajectory)
	}
	if len(draw.triangles) == 0 && len(draw.lines) == 0 {
		return nil
	}
	return draw
}

// marker draws an emitter's origin and direction and, with a look, the path its stream takes. A frame animation
// draws the anchor's triangle with another mesh on each frame, and toggles or shape keys move its vertices, so
// the marker moves with the surface shown now instead of staying where it was picked.
func (lt *liquidTool) marker(draw *viewerOverlayDraw, anchor liquidAnchor, color [4]float32, look *liquidemitter.Look) {
	shift := vec3{}
	if point, ok := lt.viewer.surfacePoint(lt.surfaceMeshes(anchor.meshID), anchor.faceIndex,
		anchor.barycentric); ok {
		shift = point.sub(anchor.point)
	}
	unit := lt.meshUnit()
	scale := 1 / unit
	origin := anchor.point.add(anchor.offset).add(shift)

	draw.triangles = overlaySphere(draw.triangles, origin, 0.006*scale, 16, 12, color)
	frame := newOverlayFrame(origin, liquidNormalize(anchor.direction), scale)
	shaft := liquidArrowLength * 0.8
	draw.triangles = overlayCone(draw.triangles, frame, 0.0015, 0.0015, 0, shaft, 8, color)
	draw.triangles = overlayCone(draw.triangles, frame, 0.008, 0, shaft, liquidArrowLength, 12, color)
	if look == nil {
		return
	}

	path := liquidFlowTrajectory(anchor.point.scale(unit), anchor.offset.scale(unit), anchor.direction, lt.down(),
		look.Force, look.Nozzle == liquidemitter.NozzleJet)
	toModel := func(p vec3) vec3 { return p.scale(scale).add(shift) }
	points := make([]vec3, len(path.points))
	for i, p := range path.points {
		points[i] = toModel(p)
	}
	draw.lines = overlayPolyline(draw.lines, points, overlayColor(liquidPathColor))
	bead := overlayColor(liquidBeadColor)
	draw.animated = func(seconds float64) []viewerOverlayVertex {
		clock := math.Mod(seconds*liquidPreviewSpeed, path.lifetime)
		var out []viewerOverlayVertex
		for i := range liquidBeadCount {
			age := math.Mod(clock+float64(i)*path.lifetime/liquidBeadCount, path.lifetime)
			out = overlaySphere(out, toModel(path.pointAt(age)), 0.0025*scale, 8, 6, bead)
		}
		return out
	}
}

// surfaceMeshes lists the meshes the game draws with the mesh's draw call, the mesh itself first so it keeps the
// marker while it is shown.
func (lt *liquidTool) surfaceMeshes(meshID string) []string {
	ids := []string{meshID}
	mesh := lt.mesh(meshID)
	if mesh == nil || mesh.DrawKey == "" {
		return ids
	}
	for _, other := range lt.viewer.scene.transport.Meshes {
		if other.DrawKey == mesh.DrawKey && other.ID != meshID {
			ids = append(ids, other.ID)
		}
	}
	return ids
}
