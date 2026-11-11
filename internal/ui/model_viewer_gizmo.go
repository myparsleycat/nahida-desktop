package ui

import (
	"math"
	"sort"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
)

const (
	viewerGizmoSize   = 88
	viewerGizmoRadius = 30
	viewerGizmoHead   = 20
)

// axisGizmo shows the world axes as the camera sees them. A head turns the camera to look along its axis.
func (v *ModelViewer) axisGizmo(t kit.Theme) *fairy.El {
	right, up, back := v.camera.basis()
	type part struct {
		depth float64
		el    *fairy.El
	}
	parts := make([]part, 0, 3*len(viewerAxes))
	const center = viewerGizmoSize / 2
	for _, axis := range viewerAxes {
		color := fairy.Hex(axis.color)
		x := float32(axis.direction.dot(right) * viewerGizmoRadius)
		y := float32(-axis.direction.dot(up) * viewerGizmoRadius)
		depth := axis.direction.dot(back)
		length := float32(math.Hypot(float64(x), float64(y)))
		line := fairy.Box().Absolute().Left(center+(x-length)/2).Top(center+y/2-1).Size(length, 2).Fill(color).
			Rotate(float32(math.Atan2(float64(y), float64(x)))).ScaleMotion(motion.Instant).PassThrough()

		parts = append(parts,
			part{depth / 2, line},
			part{depth, v.axisHead(axis, "+", axis.positive, center+x, center+y)},
			part{-depth, v.axisHead(axis, "-", axis.negative, center-x, center-y)},
		)
	}
	// The parts nearer the camera draw over the others.
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].depth < parts[j].depth })

	gizmo := fairy.Box().Absolute().Top(12).Right(12).Size(viewerGizmoSize, viewerGizmoSize).
		Radius(viewerGizmoSize / 2).HoverFill(t.Foreground.Alpha(0.15))
	for _, part := range parts {
		gizmo.Child(part.el)
	}
	return gizmo
}

// axisHead is the end of an axis centred at x, y of the gizmo; sign is "+" for the labelled end.
func (v *ModelViewer) axisHead(axis viewerAxis, sign string, view [2]float64, x, y float32) *fairy.El {
	color := fairy.Hex(axis.color)
	id := sign + axis.label
	var dot *fairy.El
	if sign == "+" {
		dot = fairy.Box(fairy.Text(axis.label).FontSize(11).Bold().Color(fairy.Hex(0x000000))).
			Size(viewerGizmoHead, viewerGizmoHead).Radius(viewerGizmoHead / 2).Fill(color).Center()
	} else {
		dot = fairy.Box().Size(14, 14).Radius(7).Fill(color.Alpha(1.0/3)).Border(2, color).Opacity(0.7)
	}
	if v.axisHover == id {
		dot.Scale(1.25)
	}

	return fairy.Box(dot).Key("model-viewer.axis:"+id).Absolute().
		Left(x-viewerGizmoHead/2).Top(y-viewerGizmoHead/2).Size(viewerGizmoHead, viewerGizmoHead).Center().
		Role(fairy.RoleButton).Label(T("page.tools.model_viewer.axis_view", "axis", id)).
		Cursor(fairy.CursorPointer).
		OnHover(func(in bool) {
			if in {
				v.axisHover = id
			} else if v.axisHover == id {
				v.axisHover = ""
			}
		}).
		OnClick(func() { v.viewAlong(view) })
}
