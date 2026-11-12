package ui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"slices"
	"time"

	"golang.org/x/image/draw"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/platform"
)

const (
	maxMenuMakerImageBytes = 32 << 20
	// menuMakerCropSourceSize bounds the picked image, which is only ever cropped to a few hundred pixels.
	menuMakerCropSourceSize = 1024
)

type menuMakerCrop struct {
	target string // slot or panel
	slotID string
	src    image.Image
	image  *fairy.Image

	zoom             float64
	offsetX, offsetY float64
	dragX, dragY     float64
}

// menuMakerPickImage asks for an image and opens the crop dialog for a slot icon or the panel background.
func (s *Shell) menuMakerPickImage(target, slotID string) {
	go func() {
		result, err := s.deps.Dialog.ShowOpenDialog(platform.OpenDialogOptions{
			Title: T("page.tools.menu_maker.crop"),
			Filters: []platform.FileFilter{{
				Name: "Image", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"},
			}},
			Properties: []string{"openFile"}, Role: "menu-maker-image",
		})
		if err == nil && (result.Canceled || len(result.FilePaths) == 0) {
			return
		}
		var src image.Image
		path := ""
		if err == nil {
			path = result.FilePaths[0]
			src, err = loadMenuMakerImage(path)
		}
		fairy.Go(func() {
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.pick-image", T("page.tools.menu_maker.load_failed"),
					map[string]any{"path": path})
				return
			}
			s.tools.menuMaker.crop = &menuMakerCrop{
				target: target, slotID: slotID, src: src, image: fairy.NewImage(src), zoom: 1,
			}
		})
	}()
}

func loadMenuMakerImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxMenuMakerImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxMenuMakerImageBytes {
		return nil, fmt.Errorf("image is larger than %d bytes", maxMenuMakerImageBytes)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Empty() {
		return nil, errors.New("image is empty")
	}
	if longest := max(b.Dx(), b.Dy()); longest > menuMakerCropSourceSize {
		scale := float64(menuMakerCropSourceSize) / float64(longest)
		w, h := max(1, int(math.Round(float64(b.Dx())*scale))), max(1, int(math.Round(float64(b.Dy())*scale)))
		small := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.BiLinear.Scale(small, small.Bounds(), src, b, draw.Src, nil)
		src = small
	}
	return src, nil
}

func (m *menuMakerTool) slotSize() int {
	if m.preview != nil && m.preview.Geometry.SlotSize > 0 {
		return m.preview.Geometry.SlotSize
	}
	return menuMakerBaseSlotSize
}

func (s *Shell) confirmMenuMakerCrop() {
	m := &s.tools.menuMaker
	c := m.crop
	if c == nil {
		return
	}
	size := m.slotSize()
	if c.target == "panel" {
		size = max(menuMakerBasePanelImageSize,
			int(math.Round(float64(menuMakerBasePanelImageSize*size)/menuMakerBaseSlotSize)))
	}
	dataURL, err := cropMenuMakerImage(c.src, size, c.zoom, c.offsetX, c.offsetY)
	if err != nil {
		s.menuMakerFailed(err, "menu-maker.crop", T("page.tools.menu_maker.load_failed"), nil)
		return
	}
	m.crop = nil
	if c.target == "panel" {
		m.settings.PanelImageDataURL, m.settings.panelImage = dataURL, nil
		m.settingsDirty = true
		s.menuMakerChanged()
		return
	}
	s.menuMakerUpdateSlot(c.slotID, func(slot *menuMakerSlot) {
		slot.Icon = menuMakerIcon{Kind: "upload", Name: "upload", Color: "#ffffff", DataURL: dataURL}
		slot.image = nil
	})
}

func (s *Shell) menuMakerCropDialog(cx *fairy.Cx) {
	m := &s.tools.menuMaker
	c := m.crop
	if c == nil {
		return
	}
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	b := c.src.Bounds()
	x, y, w, h := coveredImageRect(b.Dx(), b.Dy(), menuMakerCropPreviewSize, c.zoom, c.offsetX, c.offsetY)
	area := fairy.Box(
		fairy.ImageEl(c.image).Fit(fairy.FitFill).Absolute().Left(float32(x)).Top(float32(y)).
			Size(float32(w), float32(h)).PassThrough(),
	).Size(menuMakerCropPreviewSize, menuMakerCropPreviewSize).Clip().Radius(t.RadiusMd()).
		Fill(fairy.RGBA(0, 0, 0, 1)).Border(1, t.Border).Cursor(fairy.CursorMove).
		OnDrag(func(e fairy.DragEvent) {
			switch e.Phase {
			case fairy.DragStart:
				c.dragX, c.dragY = c.offsetX, c.offsetY
			case fairy.DragMove:
				c.offsetX, c.offsetY = c.dragX+float64(e.DX), c.dragY+float64(e.DY)
			case fairy.DragEnd:
				if e.Canceled {
					c.offsetX, c.offsetY = c.dragX, c.dragY
				}
			}
		}).
		OnWheel(func(e fairy.WheelEvent) bool {
			c.zoom = max(1, min(4, c.zoom-float64(e.DY)*0.1))
			return true
		})
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "crop"), Width: 420, Key: "menu-maker.crop",
		OnClose: func() { m.crop = nil },
		Body: fairy.Col(
			area,
			fairy.Text(T(prefix+"crop_hint")).
				FontSize(12).
				Color(t.MutedForeground).
				TextAlign(fairy.TextCenter).
				Wrap().
				WFull(),
		).Gap(8).Align(fairy.Center),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), func() { m.crop = nil }, kit.Outline),
			kit.Button(T("g.confirm"), s.confirmMenuMakerCrop),
		},
	})
}

func (s *Shell) menuMakerDraftsDialog(cx *fairy.Cx) {
	m := &s.tools.menuMaker
	if !m.draftsOpen {
		return
	}
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	list := fairy.Col().Gap(8)
	for _, draft := range m.drafts {
		list.Child(fairy.Row(
			fairy.Col(
				fairy.Text(draft.SourceName).FontSize(14).Ellipsis().TooltipWhenTruncated(),
				fairy.Text(time.UnixMilli(draft.UpdatedAt).Local().Format("2006-01-02 15:04:05")).FontSize(12).
					Color(t.MutedForeground),
			).Gap(2).Grow(1).MinW(0),
			kit.Button(T(prefix+"restore"), func() { s.menuMakerRestoreDraft(draft) }, kit.Small),
			kit.IconButton(lucide.X, func() {
				s.saveMenuMakerDrafts(slices.DeleteFunc(slices.Clone(m.drafts), func(d menuMakerDraft) bool {
					return d.ID == draft.ID
				}))
			}, kit.Ghost, kit.Tooltip(T("g.delete"))).Label(T("g.delete")),
		).Key("menu-maker.draft."+draft.ID).Gap(8).Align(fairy.Center).Pad(12).Radius(t.RadiusMd()).Border(1, t.Border))
	}
	var actions []*fairy.El
	if len(m.drafts) > 0 {
		actions = append(actions, kit.Button(T(prefix+"delete_all"), func() { s.saveMenuMakerDrafts(nil) },
			kit.Destructive))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "drafts"), Width: 672, Key: "menu-maker.drafts",
		OnClose: func() { m.draftsOpen = false },
		Body:    list,
		Actions: actions,
	})
}

func (s *Shell) menuMakerCompareDialog(cx *fairy.Cx) {
	m := &s.tools.menuMaker
	if !m.compareOpen || m.source == nil {
		return
	}
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	generated := ""
	if m.preview != nil {
		generated = m.preview.INIText
	}
	column := func(key, title, text string) *fairy.El {
		return fairy.Col(
			fairy.Text(title).FontSize(12).Medium(),
			menuMakerTextLines(key, text).H(520).Pad(12).Radius(t.RadiusMd()).Fill(t.Muted),
		).Gap(4).Grow(1).Basis(0).MinW(0)
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "compare"), Width: 1152, Key: "menu-maker.compare",
		OnClose: func() { m.compareOpen = false },
		Body: fairy.Row(
			column("menu-maker.compare.original", T(prefix+"original_logic"), m.source.Text),
			column("menu-maker.compare.generated", T(prefix+"generated_logic"), generated),
		).Gap(12),
	})
}
