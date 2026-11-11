package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/setting"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// Overlay menus are authored against a window of this size; see modelViewerOverlayWidth in the backend.
const (
	viewerOverlayMenuReferenceWidth  = 1920
	viewerOverlayMenuReferenceHeight = 1080
	viewerOverlayMenuMinSize         = 0.5
	viewerOverlayMenuMaxSize         = 3
	// viewerOverlayMenuWheelSize is the exponent a wheel notch with Ctrl scales a menu by.
	viewerOverlayMenuWheelSize   = 0.1
	viewerOverlayMenuKeySize     = 0.05
	viewerOverlayMenuHandle      = 16
	viewerOverlayMenuMinInterval = 16 * time.Millisecond
)

// viewerOverlayMenuPlacement is where a menu floats over the viewer: x and y are fractions of the viewer's size
// and size multiplies the size the menu has in the game, so a resized viewer keeps the menu in proportion.
type viewerOverlayMenuPlacement struct{ x, y, size float64 }

// viewerOverlayMenuView is the page a menu shows and where the user put it, if they moved it.
type viewerOverlayMenuView struct {
	page      int
	placement *viewerOverlayMenuPlacement
}

// viewerOverlayMenus is the state of the menus a mod draws over the game, by their index in the transport.
type viewerOverlayMenus struct {
	views map[int]*viewerOverlayMenuView
	// hoverMenu and hoverButton name the button under the pointer; hoverButton is -1 for none.
	hoverMenu, hoverButton int
	// over is the floating menu under the pointer, or -1.
	over int
	// draft is the placement of menu draftMenu while it is dragged or resized.
	draft     *viewerOverlayMenuPlacement
	draftMenu int
	// dragLeft, dragTop and dragSize are where a drag of a menu or of its corner began.
	dragLeft, dragTop, dragSize float64
	// sliderStart is the position of a slider when a drag of its handle began.
	sliderStart float64
	// started is when the flip books began playing, and ticking that a redraw for their next frame is due.
	started time.Time
	ticking bool
}

func newViewerOverlayMenus() viewerOverlayMenus {
	return viewerOverlayMenus{views: map[int]*viewerOverlayMenuView{}, hoverButton: -1, over: -1, started: time.Now()}
}

func (o *viewerOverlayMenus) view(index int) *viewerOverlayMenuView {
	view := o.views[index]
	if view == nil {
		view = &viewerOverlayMenuView{}
		o.views[index] = view
	}
	return view
}

// overlayInViewer reports whether the menus float over the viewer instead of standing above the toggles.
func (v *ModelViewer) overlayInViewer() bool {
	if s := v.shell; s != nil && s.settingsLoaded(setting.KeyModelViewerOverlayInViewer) {
		return s.boolSetting(setting.KeyModelViewerOverlayInViewer)
	}
	return true
}

// shownOverlayMenus returns the indices of the menus of the selected form.
func (v *ModelViewer) shownOverlayMenus() []int {
	if v.scene == nil {
		return nil
	}
	var shown []int
	for index, menu := range v.scene.transport.OverlayMenus {
		if formVisible(menu.Form, v.form) {
			shown = append(shown, index)
		}
	}
	return shown
}

func (v *ModelViewer) hasOverlayMenuPlacement() bool {
	for _, index := range v.shownOverlayMenus() {
		if view := v.overlayMenus.views[index]; view != nil && view.placement != nil {
			return true
		}
	}
	return false
}

func (v *ModelViewer) resetOverlayMenuPlacement() {
	for _, view := range v.overlayMenus.views {
		view.placement = nil
	}
}

// viewerImage loads the picture at source, a file path or a URL of the viewer's session. It returns nil while
// the picture loads and when it cannot be read.
func (v *ModelViewer) viewerImage(source string, flipX, flipY bool) *fairy.Image {
	if source == "" {
		return nil
	}
	read := func() ([]byte, error) {
		if strings.Contains(source, "://") {
			return v.read(v.ctx, source)
		}
		return os.ReadFile(source)
	}
	// The session in the key reads a file again when the mod is opened again, since its art may have changed.
	key := "model-viewer:" + v.scene.transport.MemorySessionID + ":" + source
	if !flipX && !flipY {
		return fairy.Images.Load(key, 0, 0, read)
	}
	return fairy.Images.Load(fmt.Sprintf("%s|flip:%t:%t", key, flipX, flipY), 0, 0, func() ([]byte, error) {
		data, err := read()
		if err != nil {
			return nil, err
		}
		return flipViewerImage(data, flipX, flipY)
	})
}

// flipViewerImage mirrors an encoded picture, since an element draws its image upright.
func flipViewerImage(data []byte, flipX, flipY bool) ([]byte, error) {
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	flipped := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			fromX, fromY := x, y
			if flipX {
				fromX = width - 1 - x
			}
			if flipY {
				fromY = height - 1 - y
			}
			flipped.Set(x, y, source.At(bounds.Min.X+fromX, bounds.Min.Y+fromY))
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, flipped); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// overlayMenuNumber is the number state holds for a variable, and NaN for none, which matches no page or value.
func overlayMenuNumber(state map[string]any, variable string) float64 {
	if number, ok := viewerNumber(state[variable]); ok {
		return number
	}
	return math.NaN()
}

func overlayMenuSliderPosition(value float64, mode modelviewer.ModelViewerOverlaySliderMode) float64 {
	position := (value - mode.Min) / (mode.Max - mode.Min)
	if math.IsNaN(position) || math.IsInf(position, 0) {
		return 0
	}
	return min(max(position, 0), 1)
}

// viewerOverlayMenuSlider is a slider of a page in the mode the state selects.
type viewerOverlayMenuSlider struct {
	slider   modelviewer.ModelViewerOverlaySlider
	mode     modelviewer.ModelViewerOverlaySliderMode
	track    modelviewer.ModelViewerOverlaySliderPart
	position float64
}

func overlayMenuSliders(page modelviewer.ModelViewerOverlayPage, state map[string]any) []viewerOverlayMenuSlider {
	var sliders []viewerOverlayMenuSlider
	for _, slider := range page.Sliders {
		if len(slider.Modes) == 0 || slider.Handle < 0 || slider.Handle >= len(page.Quads) {
			continue
		}
		mode := slider.Modes[0]
		for _, candidate := range slider.Modes {
			if overlayMenuNumber(state, candidate.WhenVariable) == candidate.WhenValue {
				mode = candidate
				break
			}
		}
		for _, part := range slider.Parts {
			if part.Quad == slider.Handle {
				sliders = append(sliders, viewerOverlayMenuSlider{
					slider: slider, mode: mode, track: part,
					position: overlayMenuSliderPosition(overlayMenuNumber(state, mode.Variable), mode),
				})
				break
			}
		}
	}
	return sliders
}

// selectOverlayMenuValue selects value of the variable id, which the toggles may not list.
func (v *ModelViewer) selectOverlayMenuValue(id string, value float64) {
	variable := modelviewer.ModelViewerVariable{ID: id}
	for _, candidate := range v.scene.transport.Variables {
		if candidate.ID == id {
			variable = candidate
			break
		}
	}
	v.selectValue(variable, value)
}

// overlayMenuPanel draws page of menu index width logical pixels wide: its quads in the state of the toggles,
// with its buttons and slider handles over them.
func (v *ModelViewer) overlayMenuPanel(index int, width float32) *fairy.El {
	menu := v.scene.transport.OverlayMenus[index]
	view := v.overlayMenus.view(index)
	height := width * float32(menu.Height/menu.Width)
	panel := fairy.Box().Size(width, height).Clip()
	if len(menu.Pages) == 0 || menu.Width <= 0 || menu.Height <= 0 {
		return panel
	}
	pageIndex := view.page
	if pageIndex < 0 || pageIndex >= len(menu.Pages) {
		pageIndex = 0
	}
	page := menu.Pages[pageIndex]
	scale := float64(width) / menu.Width
	state := v.state
	place := func(el *fairy.El, x, y, w, h float64) *fairy.El {
		return el.Absolute().Left(float32(x*scale)).Top(float32(y*scale)).Size(float32(w*scale), float32(h*scale))
	}

	var hover []modelviewer.ModelViewerOverlayHover
	if o := &v.overlayMenus; o.hoverMenu == index && o.hoverButton >= 0 && o.hoverButton < len(page.Buttons) {
		hover = page.Buttons[o.hoverButton].Hover
	}
	sliders := overlayMenuSliders(page, state)
	type span struct{ x, width float64 }
	moved := map[int]span{}
	for _, slider := range sliders {
		for _, part := range slider.slider.Parts {
			moved[part.Quad] = span{
				x:     part.StartX + (part.EndX-part.StartX)*slider.position,
				width: part.StartWidth + (part.EndWidth-part.StartWidth)*slider.position,
			}
		}
	}

	for quadIndex, quad := range page.Quads {
		path, opacity := quad.Path, quad.Opacity
		for _, quadState := range quad.States {
			if overlayMenuNumber(state, quadState.Variable) == quadState.Value {
				path, opacity = quadState.Path, quadState.Opacity
				break
			}
		}
		for _, change := range hover {
			if change.Quad == quadIndex {
				path = change.Path
				break
			}
		}
		x, w := quad.X, quad.Width
		if at, ok := moved[quadIndex]; ok {
			x, w = at.x, at.width
		}
		img := v.viewerImage(path, quad.FlipX, quad.FlipY)
		if img == nil {
			continue
		}

		var el *fairy.El
		if book := quad.Flipbook; book != nil && book.Columns > 0 && book.Rows > 0 {
			frame := v.flipbookFrame(*book)
			cellW, cellH := float32(w*scale), float32(quad.Height*scale)
			el = fairy.Box(fairy.ImageEl(img).Fit(fairy.FitFill).Absolute().
				Left(-float32(frame%book.Columns)*cellW).Top(-float32(frame/book.Columns)*cellH).
				Size(cellW*float32(book.Columns), cellH*float32(book.Rows))).Clip()
		} else {
			el = fairy.ImageEl(img).Fit(fairy.FitFill)
		}
		panel.Child(place(el, x, quad.Y, w, quad.Height).Opacity(float32(opacity)).PassThrough())
	}

	for buttonIndex, button := range page.Buttons {
		label := T("page.tools.model_viewer.overlay_switch_page")
		if button.Variable != "" {
			label = button.Label
			for _, variable := range v.scene.transport.Variables {
				if variable.ID == button.Variable {
					label = variable.Label
					break
				}
			}
		}
		el := fairy.Box().Key(fmt.Sprintf("model-viewer.overlay:%d:%d:%d", index, pageIndex, buttonIndex)).
			Role(fairy.RoleButton).Label(label).Cursor(fairy.CursorPointer).
			OnHover(func(in bool) {
				o := &v.overlayMenus
				if in {
					o.hoverMenu, o.hoverButton = index, buttonIndex
				} else if o.hoverMenu == index && o.hoverButton == buttonIndex {
					o.hoverButton = -1
				}
			}).
			OnClick(func() {
				if button.Page != nil {
					view.page = *button.Page
					v.overlayMenus.hoverButton = -1
				}
				if button.Variable == "" {
					return
				}
				current := overlayMenuNumber(v.state, button.Variable)
				for _, transition := range button.Transitions {
					if transition.From == current {
						v.selectOverlayMenuValue(button.Variable, transition.To)
						break
					}
				}
			})
		panel.Child(place(el, button.X, button.Y, button.Width, button.Height))
	}

	for _, slider := range sliders {
		handle := page.Quads[slider.slider.Handle]
		mode, track, position := slider.mode, slider.track, slider.position
		travel := track.EndX - track.StartX
		if travel == 0 {
			continue
		}
		choose := func(next float64) {
			next = min(max(next, 0), 1)
			v.selectOverlayMenuValue(mode.Variable, mode.Min+(mode.Max-mode.Min)*next)
		}
		x, w := handle.X, handle.Width
		if at, ok := moved[slider.slider.Handle]; ok {
			x, w = at.x, at.width
		}
		el := fairy.Box().
			Key(fmt.Sprintf("model-viewer.overlay-slider:%d:%d:%d", index, pageIndex, slider.slider.Handle)).
			Role(fairy.RoleSlider).
			Label(mode.Label).
			Cursor(fairy.CursorResizeEW).
			Focusable().
			OnDrag(func(d fairy.DragEvent) {
				if d.Phase == fairy.DragStart {
					v.overlayMenus.sliderStart = position
				}
				if d.Canceled {
					choose(v.overlayMenus.sliderStart)
					return
				}
				choose(v.overlayMenus.sliderStart + float64(d.DX)/scale/travel)
			}).
			OnKey(func(k keyboard.KeyEvent) bool {
				switch k.Key {
				case keyboard.KeyLeft:
					choose(position - 0.01)
				case keyboard.KeyRight:
					choose(position + 0.01)
				default:
					return false
				}
				return true
			})
		panel.Child(place(el, x, handle.Y, w, handle.Height))
	}
	return panel
}

// flipbookFrame is the cell of a flip book to show now. A book that plays keeps the viewer redrawing.
func (v *ModelViewer) flipbookFrame(book modelviewer.ModelViewerOverlayFlipbook) int {
	count := book.FrameEnd - book.FrameStart + 1
	if book.FPS <= 0 || count <= 1 {
		return max(book.FrameStart, 0)
	}
	o := &v.overlayMenus
	if !o.ticking {
		o.ticking = true
		interval := max(time.Duration(float64(time.Second)/book.FPS), viewerOverlayMenuMinInterval)
		time.AfterFunc(interval, func() { fairy.Go(func() { o.ticking = false }) })
	}
	return max(book.FrameStart, 0) + int(time.Since(o.started).Seconds()*book.FPS)%count
}

// floatingOverlayMenu floats menu index over a viewer viewW by viewH logical pixels. Dragging moves it; Ctrl with
// the wheel and its bottom-right corner resize it.
func (v *ModelViewer) floatingOverlayMenu(t kit.Theme, index int, viewW, viewH float64) *fairy.El {
	menu := v.scene.transport.OverlayMenus[index]
	scale := min(viewW/viewerOverlayMenuReferenceWidth, viewH/viewerOverlayMenuReferenceHeight)
	baseW, baseH := menu.Width*scale, menu.Height*scale
	if !(baseW > 0) || !(baseH > 0) {
		return nil
	}
	o := &v.overlayMenus
	view := o.view(index)
	clampSize := func(next, roomW, roomH float64) float64 {
		largest := min(viewerOverlayMenuMaxSize, roomW/baseW, roomH/baseH)
		return max(min(next, largest), min(viewerOverlayMenuMinSize, largest))
	}
	placedAt := func(left, top, size float64) viewerOverlayMenuPlacement {
		return viewerOverlayMenuPlacement{
			x:    max(min(left, viewW-baseW*size), 0) / viewW,
			y:    max(min(top, viewH-baseH*size), 0) / viewH,
			size: size,
		}
	}

	shown := viewerOverlayMenuPlacement{
		x: menu.X / viewerOverlayMenuReferenceWidth, y: menu.Y / viewerOverlayMenuReferenceHeight, size: 1,
	}
	switch {
	case o.draft != nil && o.draftMenu == index:
		shown = *o.draft
	case view.placement != nil:
		shown = *view.placement
	}
	size := clampSize(shown.size, viewW, viewH)
	placed := placedAt(shown.x*viewW, shown.y*viewH, size)
	left, top := placed.x*viewW, placed.y*viewH
	width := baseW * size
	place := func(next viewerOverlayMenuPlacement) { view.placement, o.draft = &next, nil }
	draft := func(next viewerOverlayMenuPlacement) { o.draft, o.draftMenu = &next, index }
	resizeBy := func(step float64) {
		place(placedAt(left, top, clampSize(size+step, viewW-left, viewH-top)))
	}

	resizeLabel := T("page.tools.model_viewer.overlay_resize")
	corner := fairy.Box(fairy.Box().Size(8, 8).BorderEdges(0, 2, 2, 0, t.Foreground.Alpha(0.8))).
		Key(fmt.Sprintf("model-viewer.overlay-resize:%d", index)).
		Absolute().Right(0).Bottom(0).Size(viewerOverlayMenuHandle, viewerOverlayMenuHandle).Pad(2).
		Align(fairy.End).Justify(fairy.JustifyEnd).Fill(t.Background.Alpha(0.7)).
		Role(fairy.RoleSlider).Label(resizeLabel).Cursor(fairy.CursorResizeNS).Focusable().
		OnDrag(func(d fairy.DragEvent) {
			if d.Phase == fairy.DragStart {
				o.dragSize = size
			}
			// Follows the diagonal, since a flat menu would jump if the pointer's height set its size.
			grown := float64(d.DX)*baseW + float64(d.DY)*baseH
			next := placedAt(left, top, clampSize(o.dragSize+grown/(baseW*baseW+baseH*baseH), viewW-left, viewH-top))
			switch {
			case d.Canceled:
				o.draft = nil
			case d.Phase == fairy.DragEnd:
				place(next)
			default:
				draft(next)
			}
		}).
		OnKey(func(k keyboard.KeyEvent) bool {
			switch k.Key {
			case keyboard.KeyRight, keyboard.KeyUp:
				resizeBy(viewerOverlayMenuKeySize)
			case keyboard.KeyLeft, keyboard.KeyDown:
				resizeBy(-viewerOverlayMenuKeySize)
			default:
				return false
			}
			return true
		})
	if o.over != index {
		corner.Opacity(0)
	}

	return fairy.Box(v.overlayMenuPanel(index, float32(width)), corner).
		Key(fmt.Sprintf("model-viewer.overlay-menu:%d", index)).
		Absolute().Left(float32(left)).Top(float32(top)).W(float32(width)).Cursor(fairy.CursorMove).
		OnHover(func(in bool) {
			if in {
				o.over = index
			} else if o.over == index {
				o.over = -1
			}
		}).
		OnDrag(func(d fairy.DragEvent) {
			if d.Phase == fairy.DragStart {
				o.dragLeft, o.dragTop = left, top
			}
			next := placedAt(o.dragLeft+float64(d.DX), o.dragTop+float64(d.DY), size)
			switch {
			case d.Canceled:
				o.draft = nil
			case d.Phase == fairy.DragEnd:
				place(next)
			default:
				draft(next)
			}
		}).
		OnWheel(func(e fairy.WheelEvent) bool {
			if e.Mods&keyboard.ModCtrl == 0 {
				return false
			}
			next := clampSize(size*math.Exp(-float64(e.DY)*viewerOverlayMenuWheelSize), viewW, viewH)
			if next == size || o.draft != nil {
				return true
			}
			// The point under the cursor stays there, so a button being aimed at does not run away.
			cursorX, cursorY := left+float64(e.LocalX), top+float64(e.LocalY)
			ratio := next / size
			place(placedAt(cursorX-(cursorX-left)*ratio, cursorY-(cursorY-top)*ratio, next))
			return true
		})
}

// variantTiles is the grid of toggles a mod's own menu art frames: a tile steps its toggle through its values.
func (v *ModelViewer) variantTiles(t kit.Theme, variables []modelviewer.ModelViewerVariable, width float32) *fairy.El {
	assets := v.scene.transport.UIAssets
	const columns, gap, pad = 3, 12, 16
	side := (width - 2*pad - (columns-1)*gap) / columns
	white := fairy.Hex(0xffffff)
	black := fairy.Hex(0x000000)

	grid := fairy.Col().Gap(gap).Pad(pad)
	for at := 0; at < len(variables); at += columns {
		row := fairy.Row().Gap(gap)
		for _, variable := range variables[at:min(at+columns, len(variables))] {
			current, set := v.state[variable.ID]
			if !set {
				current = variable.DefaultValue
			}
			frame := assets.SlotPath
			if fmt.Sprint(current) != fmt.Sprint(variable.DefaultValue) {
				for _, path := range []string{assets.SlotActivePath, assets.SlotHoverPath} {
					if path != "" {
						frame = path
						break
					}
				}
			}

			tile := fairy.Box().Key("model-viewer.tile:"+variable.ID).Size(side, max(side, 80)).Shrink(0).
				Clip().Radius(6).Border(1, t.Border).Fill(black.Alpha(0.2)).HoverFill(black.Alpha(0.3)).
				Role(fairy.RoleButton).Label(variable.Label).Focusable().Cursor(fairy.CursorPointer).
				OnClick(func() {
					if len(variable.Values) == 0 {
						return
					}
					next := 0
					for i, entry := range variable.Values {
						if fmt.Sprint(entry.Value) == fmt.Sprint(current) {
							next = (i + 1) % len(variable.Values)
							break
						}
					}
					v.selectValue(variable, variable.Values[next].Value)
				})
			if img := v.viewerImage(frame, false, false); img != nil {
				tile.Child(fairy.ImageEl(img).Fit(fairy.FitCover).Absolute().Inset(0).PassThrough())
			}
			if img := v.viewerImage(variable.IconPath, false, false); img != nil {
				tile.Child(fairy.ImageEl(img).Fit(fairy.FitContain).Absolute().Inset(12).PassThrough())
			}
			tile.Child(fairy.Row(
				fairy.Box(fairy.Text(variable.Label).FontSize(11).Color(white).Ellipsis()).
					MinW(0).
					PadXY(8, 4).
					Radius(4).
					Fill(black.Alpha(0.6)),
			).Absolute().Left(8).Right(8).Bottom(8).Justify(fairy.JustifyCenter))
			row.Child(tile)
		}
		grid.Child(row)
	}

	box := fairy.Box().Clip().Radius(6).Border(1, t.Border)
	if img := v.viewerImage(assets.BackgroundPath, false, false); img != nil {
		box.Child(fairy.ImageEl(img).Fit(fairy.FitCover).Absolute().Inset(0).PassThrough())
	}
	return box.Child(grid)
}
