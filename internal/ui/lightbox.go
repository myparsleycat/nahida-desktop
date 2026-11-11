package ui

import (
	"strconv"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"
)

// lightbox shows a preview large over the window, as the React PreviewLightbox did: a click anywhere outside
// its buttons or Escape closes it, and a gallery steps with its arrows or the Left and Right keys.
type lightbox struct {
	key   string
	label string
	// content is the picture, laid out inside 80% of the window.
	content *fairy.El
	// index and count place the picture in a gallery; a count above 1 shows the arrows and the counter.
	index, count int
	step         func(delta int)
	close        func()
}

func (l lightbox) show(cx *fairy.Cx) {
	white := fairy.Hex(0xffffff)
	button := func(icon *svg.Vector, label string, onClick func()) *fairy.El {
		return fairy.Box(fairy.Icon(icon).Size(24, 24)).Size(36, 36).Center().Radius(kit.T().RadiusMd()).Color(white).
			HoverFill(white.Alpha(0.2)).Cursor(fairy.CursorPointer).Label(label).OnClick(onClick)
	}
	windowW, windowH := cx.Size()
	root := fairy.Col(fairy.Box(l.content).W(windowW * 0.8).H(windowH * 0.8).Center()).
		Key(l.key).WFull().HFull().Center().Fill(fairy.RGBA(0, 0, 0, 0.8)).Role(fairy.RoleDialog).Label(l.label).
		Enter(fairy.Fade()).Focusable().AutoFocus().OnClick(l.close)
	gallery := l.count > 1 && l.step != nil
	if gallery {
		root.Child(fairy.Row(
			button(lucide.ChevronLeft, T("g.previous"), func() { l.step(-1) }),
			button(lucide.ChevronRight, T("g.next"), func() { l.step(1) }),
		).Inset(0).PadXY(16, 0).Align(fairy.Center).Justify(fairy.JustifyBetween).Role(fairy.RoleNone))
		root.Child(fairy.Row(
			fairy.Text(strconv.Itoa(l.index+1)+" / "+strconv.Itoa(l.count)).FontSize(12).Color(white).PadXY(12, 4).
				Radius(999).Fill(fairy.RGBA(0, 0, 0, 0.5)),
		).Left(0).Right(0).Bottom(24).Justify(fairy.JustifyCenter).Role(fairy.RoleNone))
		root.OnKey(func(k keyboard.KeyEvent) bool {
			switch {
			case k.Is(keyboard.KeyLeft, 0):
				l.step(-1)
			case k.Is(keyboard.KeyRight, 0):
				l.step(1)
			default:
				return false
			}
			return true
		})
	}
	root.Child(button(lucide.X, T("g.close"), l.close).Top(40).Right(16))
	cx.Overlay(root, l.close)
}
