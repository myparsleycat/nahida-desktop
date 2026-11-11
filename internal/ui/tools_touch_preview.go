package ui

import (
	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	touchprofile "nahida.live/desktop/internal/tools/touch_profile"
)

// touchViewport is the Touch Profile mesh preview: the body shape editor's mesh viewport with a weight heatmap.
type touchViewport struct {
	view sculptViewport
	// shown is what the viewport holds, so the mesh and its colours are replaced only when it changes.
	shown touchShade
}

// touchShade is the mesh the preview shows and what its heatmap colours depend on.
type touchShade struct {
	mesh    *touchMeshView
	preview *touchPreviewData
	zone    string
	// bone is the bone whose weights within threshold are shown, or -1.
	bone      int64
	threshold [2]float64
}

func (vp *touchViewport) close() {
	vp.view.close()
	vp.shown = touchShade{}
}

func (s *Shell) touchMenuBar(tp *touchProfile) *fairy.El {
	view := &tp.view.view
	return kit.Menubar(tp.key+".menu", []kit.MenubarMenu{{
		Label: T("page.tools.model_viewer.menu.model"),
		Items: func() []kit.MenuItem {
			items := []kit.MenuItem{{Heading: true, Label: T("page.tools.model_viewer.menu.rotate")}}
			for _, rotation := range viewerRotations {
				items = append(items, kit.MenuItem{
					Label:   T("page.tools.model_viewer.rotate_actions." + rotation.label),
					OnClick: func() { view.rotate(rotation.delta) },
				})
			}
			return append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
				Label: T("page.tools.model_viewer.menu.reset"), Icon: lucide.RotateCcw, OnClick: view.resetView,
			})
		},
	}}).Shrink(0)
}

func (tp *touchProfile) touchShade() touchShade {
	shade := touchShade{bone: -1}
	switch display := tp.displayPreview(); {
	case tp.phase == touchSelect && tp.meshPreview != nil:
		shade.mesh = tp.meshPreview
		if shade.bone = tp.highlightBone(); shade.bone >= 0 {
			shade.threshold = tp.threshold
		}
	case display != nil:
		shade.mesh, shade.preview, shade.zone = display.mesh, tp.activePreview(), tp.activeZoneID()
	}
	return shade
}

// sync puts shade's mesh and heatmap into the viewport when they changed.
func (vp *touchViewport) sync(shade touchShade) {
	if shade == vp.shown {
		return
	}
	if shade.mesh == nil {
		vp.view.clear()
		vp.shown = shade
		return
	}
	if shade.mesh != vp.shown.mesh {
		mesh := shade.mesh.mesh
		indices := mesh.Indices
		if len(indices) == 0 {
			indices = nil
		}
		vp.view.show(mesh.SessionID+"\x00"+mesh.ComponentID, mesh.Positions, indices)
	}
	weights := touchDisplayWeights(shade)
	colors := make([]float32, len(weights)*3)
	for i, weight := range weights {
		heat := touchHeat(weight)
		copy(colors[i*3:i*3+3], heat[:])
	}
	vp.view.setColors(colors)
	vp.shown = shade
}

func (s *Shell) touchViewport(tp *touchProfile) *fairy.El {
	t := kit.T()
	vp := &tp.view
	shade := tp.touchShade()
	vp.sync(shade)
	box := fairy.Box().MinH(320).Radius(6).Fill(t.Muted.Alpha(0.3)).Clip()

	if shade.mesh == nil {
		message := T(touchPrefix + "preview_empty")
		switch {
		case tp.loading:
			message = T(touchPrefix + "analyzing")
		case tp.previewLoading:
			message = T(touchPrefix + "preview_loading")
		}
		box.Child(
			fairy.Text(message).FontSize(14).LineHeight(20).Color(t.MutedForeground).TextAlign(fairy.TextCenter).Wrap().
				Absolute().Inset(0).Pad(24).Center(),
		)
	} else {
		background := t.Background.Mix(t.Muted, 0.5)
		mesh := shade.mesh.mesh
		box.Child(vp.view.surface([4]float32{background.R, background.G, background.B, 1}, s.deps.Log,
			map[string]any{"sessionId": mesh.SessionID, "componentId": mesh.ComponentID}).
			Key(tp.key + ".surface").WFull().HFull().Cursor(fairy.CursorMove).
			DragButtons(fairy.MouseLeft | fairy.MouseRight | fairy.MouseMiddle).OnDrag(vp.view.drag).
			OnWheel(func(w fairy.WheelEvent) bool {
				vp.view.camera.zoom(float64(w.DY))
				return true
			}))
	}

	if tp.loading || tp.previewLoading || tp.meshLoading {
		label := T(touchPrefix + "preview_loading")
		if tp.loading {
			label = T(touchPrefix + "analyzing")
		}
		box.Child(
			fairy.Box(fairy.Row(kit.Spinner(16), fairy.Text(label).FontSize(14).LineHeight(20)).Gap(8).Align(fairy.Center).
				PadXY(12, 8).Radius(6).Border(1, t.Border).Fill(t.Background.Alpha(0.9))).
				Absolute().
				Inset(0).
				Center().
				Fill(fairy.RGBA(0, 0, 0, 0.2)),
		)
	}
	if tp.phase == touchSelect && tp.meshError != "" {
		box.Child(
			fairy.Text(tp.meshError).
				FontSize(12).
				LineHeight(16).
				Color(t.Destructive).
				TextAlign(fairy.TextCenter).
				Wrap().
				Absolute().
				Left(0).
				Right(0).
				Bottom(0).
				Pad(8),
		)
	}
	return box
}

// touchDisplayWeights is the strongest weight of the shown masks at each vertex, as the React viewport showed.
func touchDisplayWeights(shade touchShade) []float32 {
	mesh := shade.mesh.mesh
	if shade.bone >= 0 {
		return touchprofile.BoneWeights(mesh, uint32(shade.bone), shade.threshold)
	}
	count := len(mesh.Positions) / 3
	weights := make([]float32, count)
	if shade.preview == nil {
		return weights
	}
	for _, zone := range shade.preview.preview.Zones {
		if shade.zone != allTouchZones && zone.ID != shade.zone {
			continue
		}
		for i := range min(count, len(zone.Weights)) {
			weights[i] = max(weights[i], min(max(zone.Weights[i], 0), 1))
		}
	}
	return weights
}

// touchHeat is the heatmap colour of a weight in linear RGB: grey for none, then blue, green and red.
func touchHeat(weight float32) [3]float32 {
	w := min(max(weight, 0), 1)
	switch {
	case w <= 0:
		return [3]float32{.12, .12, .14}
	case w < .5:
		t := w / .5
		return [3]float32{.1 * t, .25 + .35*t, .85 - .2*t}
	case w < .85:
		t := (w - .5) / .35
		return [3]float32{.1 + .85*t, .6 + .3*t, .65 * (1 - t)}
	}
	t := (w - .85) / .15
	return [3]float32{.95, .9 * (1 - t), .05 * (1 - t)}
}
