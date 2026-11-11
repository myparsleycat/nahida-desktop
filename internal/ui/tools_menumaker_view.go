package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/tools/menumaker"
)

const (
	menuMakerNarrowWidth  = 1024
	menuMakerSlotMinH     = 116
	menuMakerSlotRowGap   = 8
	menuMakerINILineGuess = 20
)

// lucideIcon draws an icon of the full Lucide catalog, which has icons fairy's generated set leaves out.
func lucideIcon(name string) *svg.Vector {
	return menuMakerIconVector(menuMakerIcon{Kind: "lucide", Name: name})
}

func menuMakerUIColor(hex string, alpha int) fairy.Color {
	c := menuMakerColor(hex, alpha)
	return fairy.RGBA(c.R, c.G, c.B, float32(c.A)/255)
}

func menuMakerPreviewScale(settings menumaker.MenuMakerSettings) float32 {
	return float32(
		max(0.25, min(3, 1920/float64(max(1, settings.BaseWidth)), 1080/float64(max(1, settings.BaseHeight)))),
	)
}

func (s *Shell) menuMakerToolPage(cx *fairy.Cx) *fairy.El {
	m := &s.tools.menuMaker
	if !m.loaded {
		s.menuMakerReady(nil)
		return fairy.Col(kit.Spinner(20)).Grow(1).Center()
	}
	t := kit.T()
	width, _ := cx.Size()
	narrow := width < menuMakerNarrowWidth

	page := fairy.Col(s.menuMakerHeader()).Grow(1).MinH(0).WFull().OnLayout(func(w, _ float32) {
		if (w < menuMakerNarrowWidth) != (m.width < menuMakerNarrowWidth) || m.width == 0 {
			fairy.Go(func() {})
		}
		m.width = w
	})
	if narrow {
		page.Child(fairy.Row(menuMakerPanelButtons(m.narrowPanel, []kit.Item{
			{Value: "inspector", Label: T("page.tools.menu_maker.inspector")},
			{Value: "preview", Label: T("page.tools.menu_maker.preview")},
			{Value: "editor", Label: T("page.tools.menu_maker.editor")},
			{Value: "ini", Label: T("page.tools.menu_maker.ini")},
		}, func(value string) {
			m.narrowPanel = value
			if value == "editor" || value == "ini" {
				m.sidePanel = value
			}
		}).Grow(1)).Pad(4).BorderEdges(0, 0, 1, 0, t.Border))
	}
	if m.genError != "" {
		page.Child(fairy.Text(m.genError).Color(t.Destructive).FontSize(14).LineHeight(20).Wrap().PadXY(16, 8))
	}

	inspector := fairy.Col(s.menuMakerInspector(), s.menuMakerPanelImageButtons()).Gap(16).Pad(12).ScrollY()
	preview := fairy.Col(s.menuMakerPreview()).Pad(16).Fill(t.Muted.Alpha(0.3)).ScrollY()
	side := s.menuMakerSidePanel(width >= 640)
	main := fairy.Row().Grow(1).MinH(0).WFull()
	switch {
	case !narrow:
		pageWidth := m.width
		if pageWidth == 0 {
			pageWidth = width
		}
		main.Child(
			inspector.W(290).Shrink(0).BorderEdges(0, 1, 0, 0, t.Border),
			preview.Grow(1).MinW(360),
			side.W(max(300, pageWidth*0.38)).Shrink(0).BorderEdges(0, 0, 0, 1, t.Border),
		)
	case m.narrowPanel == "inspector":
		main.Child(inspector.Grow(1))
	case m.narrowPanel == "preview":
		main.Child(preview.Grow(1))
	default:
		main.Child(side.Grow(1))
	}
	page.Child(main)

	s.menuMakerIconPickerDialog(cx)
	s.menuMakerCropDialog(cx)
	s.menuMakerDraftsDialog(cx)
	s.menuMakerCompareDialog(cx)
	return page
}

func (s *Shell) menuMakerHeader() *fairy.El {
	m := &s.tools.menuMaker
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	empty := m.document == nil

	title := fairy.Col(fairy.Text(T(prefix + "title")).SemiBold().FontSize(14).LineHeight(20).Ellipsis()).
		Grow(1).
		MinW(0)
	switch {
	case m.source != nil:
		title.Child(fairy.Text(m.source.Path).FontSize(12).LineHeight(16).Color(t.MutedForeground).
			Ellipsis().TooltipWhenTruncated())
	case m.modName != "":
		title.Child(
			fairy.Text(m.modName + " · " + m.modPath).FontSize(12).LineHeight(16).Color(t.MutedForeground).Ellipsis().
				TooltipWhenTruncated(),
		)
	}
	header := fairy.Row(title).
		Wrap().
		Gap(8).
		RowGap(8).
		Align(fairy.Center).
		Pad(12).
		BorderEdges(0, 0, 1, 0, t.Border)
	if m.scan != nil {
		stats := m.scan.Stats
		header.Child(kit.Badge(fmt.Sprintf("INI %d · TXT %d · %s %d", stats.INI, stats.TXT,
			T(prefix+"excluded"), stats.Disabled), kit.Secondary))
	}
	button := func(label string, icon *svg.Vector, onClick func(), disabled bool, opts ...kit.Option) *fairy.El {
		return kit.Button(label, onClick, append([]kit.Option{kit.Outline, kit.Small, kit.Icon(icon),
			kit.Disabled(disabled)}, opts...)...)
	}
	header.Child(
		menuMakerButtonGroup(true,
			button(T(prefix+"choose_file"), lucideIcon("file-code"), s.menuMakerChooseFile, m.busy),
			button(T(prefix+"choose_folder"), lucide.FolderOpen, s.menuMakerChooseFolder, m.busy),
		),
		menuMakerButtonGroup(true,
			button(T(prefix+"drafts"), lucideIcon("history"), func() { m.draftsOpen = true }, false),
			button(T(prefix+"compare"), lucide.RefreshCw, func() { m.compareOpen = true }, empty),
		),
		menuMakerButtonGroup(true,
			button("INI", lucide.Save, func() { s.menuMakerSave(false) }, empty || m.busy),
			button("ZIP", lucideIcon("archive"), func() { s.menuMakerSave(true) }, empty || m.busy),
		),
		kit.Button(T(prefix+"apply"), s.menuMakerApply, kit.Small, kit.Icon(lucide.Upload), kit.Loading(m.busy),
			kit.Disabled(empty || m.busy || !m.sourceAvailable)),
	)
	return header
}

// menuMakerButtonGroup joins buttons as the React ButtonGroup did: buttons after the first drop their left border.
func menuMakerButtonGroup(outlined bool, buttons ...*fairy.El) *fairy.El {
	t := kit.T()
	border := t.Border
	if t.Dark {
		border = t.Input
	}
	for i, button := range buttons {
		left, right := float32(0), float32(0)
		if i == 0 {
			left = 8
		}
		if i == len(buttons)-1 {
			right = 8
		}
		button.Radii(left, right, right, left)
		if outlined && i > 0 {
			button.BorderEdges(1, 1, 1, 0, border)
		}
	}
	return fairy.Row(buttons...).Role(fairy.RoleGroup)
}

func menuMakerPanelButtons(value string, items []kit.Item, onChange func(string)) *fairy.El {
	buttons := make([]*fairy.El, 0, len(items))
	for _, item := range items {
		variant := kit.Ghost
		if item.Value == value {
			variant = kit.Secondary
		}
		opts := []kit.Option{variant, kit.Small}
		if item.Icon != nil {
			opts = append(opts, kit.Icon(item.Icon))
		}
		buttons = append(buttons, kit.Button(item.Label, func() { onChange(item.Value) }, opts...).
			Grow(1).Basis(0).MinW(0))
	}
	return menuMakerButtonGroup(false, buttons...)
}

func menuMakerSection(title string, children ...*fairy.El) *fairy.El {
	t := kit.T()
	return fairy.Col(append([]*fairy.El{
		fairy.Text(strings.ToUpper(title)).FontSize(16).LineHeight(24).SemiBold().Color(t.MutedForeground).Heading(3),
	}, children...)...).Gap(8)
}

func menuMakerField(label string, control *fairy.El) *fairy.El {
	t := kit.T()
	return fairy.Col(fairy.Text(label).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(), control.Label(label)).
		Gap(4).
		Grow(1).
		Basis(0).
		MinW(0)
}

func menuMakerGrid(children ...*fairy.El) *fairy.El { return fairy.Row(children...).Gap(8) }

func menuMakerItems(values ...string) []kit.Item {
	items := make([]kit.Item, len(values))
	for i, value := range values {
		items[i] = kit.Item{Value: value, Label: value}
	}
	return items
}

// menuMakerNumber is a number field that applies its value, clamped, when it loses focus or Enter is pressed.
func (s *Shell) menuMakerNumber(key, label string, value, lo, hi, step float64, set func(float64)) *fairy.El {
	m := &s.tools.menuMaker
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if draft, ok := m.numbers[key]; ok {
		text = draft
	}
	commit := func() {
		draft, ok := m.numbers[key]
		if !ok {
			return
		}
		delete(m.numbers, key)
		n, err := strconv.ParseFloat(strings.TrimSpace(draft), 64)
		if err != nil {
			return
		}
		n = min(hi, max(lo, math.Round(n/step)*step))
		set(n)
	}
	field := kit.TextField(text, func(v string) { m.numbers[key] = v }).Key("menu-maker.number." + key).
		OnBlur(commit).OnSubmit(func(string) { commit() })
	return menuMakerField(label, field)
}

func (s *Shell) menuMakerInspector() *fairy.El {
	m := &s.tools.menuMaker
	st := &m.settings
	const prefix = "page.tools.menu_maker."
	set := s.menuMakerSetSettings
	number := func(key string, value, lo, hi, step float64, apply func(*menumaker.MenuMakerSettings, float64)) *fairy.El {
		return s.menuMakerNumber(key, T(prefix+key), value, lo, hi, step, func(n float64) {
			set(func(settings *menumaker.MenuMakerSettings) { apply(settings, n) })
		})
	}
	toggle := func(key string, value bool, apply func(*menumaker.MenuMakerSettings, bool)) *fairy.El {
		return fairy.Row(
			fairy.Text(T(prefix+key)).FontSize(12).Grow(1).MinW(0).Wrap(),
			kit.Switch(value, func(on bool) {
				set(func(settings *menumaker.MenuMakerSettings) { apply(settings, on) })
			}).Label(T(prefix+key)),
		).Gap(12).Align(fairy.Center).PadXY(0, 4)
	}
	choose := func(key, label, value string, values []string, apply func(*menumaker.MenuMakerSettings, string)) *fairy.El {
		return menuMakerField(label, kit.Select("menu-maker."+key, value, menuMakerItems(values...),
			func(v string) { set(func(settings *menumaker.MenuMakerSettings) { apply(settings, v) }) }))
	}

	panel := menuMakerSection(T(prefix+"panel"),
		menuMakerField(T(prefix+"menu_title"), kit.TextField(st.Title, func(v string) {
			set(func(settings *menumaker.MenuMakerSettings) { settings.Title = v })
		}, kit.Placeholder(T(prefix+"menu_title_placeholder"))).Key("menu-maker.title")),
		menuMakerGrid(
			menuMakerField(T(prefix+"menu_key"), kit.TextField(st.MenuKey, func(v string) {
				set(func(settings *menumaker.MenuMakerSettings) { settings.MenuKey = v })
			}).Key("menu-maker.menu-key")),
			choose("click-modifier", T(prefix+"click_modifier"), st.ClickModifier,
				[]string{"alt", "ctrl", "shift", "none"},
				func(settings *menumaker.MenuMakerSettings, v string) { settings.ClickModifier = v }),
		),
		menuMakerGrid(
			number("columns", float64(st.Columns), 1, 8, 1,
				func(settings *menumaker.MenuMakerSettings, n float64) { settings.Columns = int(n) }),
			number("gap", float64(st.Gap), 0, 64, 1,
				func(settings *menumaker.MenuMakerSettings, n float64) { settings.Gap = int(n) }),
			number("scale", st.PanelScale, 0.5, 2, 0.1,
				func(settings *menumaker.MenuMakerSettings, n float64) { settings.PanelScale = n }),
		),
		menuMakerGrid(
			number("base_width", float64(st.BaseWidth), 640, 7680, 1,
				func(settings *menumaker.MenuMakerSettings, n float64) { settings.BaseWidth = int(n) }),
			number("base_height", float64(st.BaseHeight), 360, 4320, 1,
				func(settings *menumaker.MenuMakerSettings, n float64) { settings.BaseHeight = int(n) }),
		),
		menuMakerGrid(
			choose("fallback", T(prefix+"fallback"), st.FallbackType,
				[]string{"cycle", "toggle", "hold", "activate"},
				func(settings *menumaker.MenuMakerSettings, v string) { settings.FallbackType = v }),
			choose("alignment", T(prefix+"button_alignment"), st.SlotAlignment, []string{"left", "center", "right"},
				func(settings *menumaker.MenuMakerSettings, v string) { settings.SlotAlignment = v }),
		),
		toggle("remove_keys", st.RemoveOriginalKeys,
			func(settings *menumaker.MenuMakerSettings, on bool) { settings.RemoveOriginalKeys = on }),
		toggle("key_hint", st.ShowKeyHint,
			func(settings *menumaker.MenuMakerSettings, on bool) { settings.ShowKeyHint = on }),
		toggle("hide_upload_label", st.HideUploadLabel,
			func(settings *menumaker.MenuMakerSettings, on bool) { settings.HideUploadLabel = on }),
	)

	palette := &st.Palette
	colors := []struct {
		key   string
		value *string
	}{
		{"accent", &palette.Accent}, {"panelBackground", &palette.PanelBackground},
		{"panelBorder", &palette.PanelBorder}, {"slotBackground", &palette.SlotBackground},
		{"slotHover", &palette.SlotHover}, {"slotBorder", &palette.SlotBorder},
		{"title", &palette.Title}, {"titleShadow", &palette.TitleShadow},
	}
	alphas := []struct {
		key   string
		value *int
	}{
		{"panelBackgroundAlpha", &palette.PanelBackgroundAlpha}, {"panelBorderAlpha", &palette.PanelBorderAlpha},
		{"slotBackgroundAlpha", &palette.SlotBackgroundAlpha}, {"slotHoverAlpha", &palette.SlotHoverAlpha},
		{"slotBorderAlpha", &palette.SlotBorderAlpha},
	}
	paletteSection := menuMakerSection(T(prefix + "palette"))
	var cells []*fairy.El
	for _, entry := range colors {
		label := T(prefix + "palette_" + entry.key)
		cells = append(cells, menuMakerField(label, kit.ColorPicker("menu-maker.color."+entry.key,
			menuMakerUIColor(*entry.value, 255), func(c fairy.Color, final bool) {
				*entry.value = strings.ToLower(kit.HexString(c, false))
				if final {
					m.settingsDirty = true
					s.menuMakerChanged()
				}
			}, kit.ColorSwatchField)))
	}
	for _, entry := range alphas {
		cells = append(cells, s.menuMakerNumber(entry.key, T(prefix+"palette_"+entry.key), float64(*entry.value),
			0, 255, 1, func(n float64) {
				*entry.value = int(n)
				m.settingsDirty = true
				s.menuMakerChanged()
			}))
	}
	for i, pair := range slices.Collect(slices.Chunk(cells, 2)) {
		row := menuMakerGrid(pair...)
		if len(pair) == 1 {
			row.Child(fairy.Box().Grow(1).Basis(0))
		}
		// The opacity fields were a second grid with mt-2 below the colours.
		if i == len(colors)/2 {
			row.MarginEdges(8, 0, 0, 0)
		}
		paletteSection.Child(row)
	}
	return fairy.Col(panel, paletteSection).Gap(20)
}

func (s *Shell) menuMakerPanelImageButtons() *fairy.El {
	m := &s.tools.menuMaker
	row := fairy.Row(kit.Button(T("page.tools.menu_maker.panel_image"), func() { s.menuMakerPickImage("panel", "") },
		kit.Outline, kit.Small, kit.Icon(lucide.Image)).Grow(1)).Gap(8)
	if m.settings.PanelImageDataURL != "" {
		row.Child(kit.IconButton(lucide.X, func() {
			m.settings.PanelImageDataURL, m.settings.panelImage = "", nil
			m.settingsDirty = true
			s.menuMakerChanged()
		}, kit.Ghost, kit.Small, kit.Tooltip(T("g.delete"))).Label(T("g.delete")))
	}
	return row
}

// menuMakerImage decodes a data URL image once and keeps it in *cache.
func menuMakerImage(dataURL string, cache **fairy.Image) *fairy.Image {
	if *cache != nil || dataURL == "" {
		return *cache
	}
	if img, err := decodeDataURL(dataURL); err == nil {
		*cache = fairy.NewImage(img)
	}
	return *cache
}

func (s *Shell) menuMakerPreview() *fairy.El {
	m := &s.tools.menuMaker
	if m.document == nil {
		return s.menuMakerEmptyState()
	}
	settings := m.settings
	palette := settings.Palette
	geometry := menumaker.MenuMakerGeometry{SlotSize: menuMakerBaseSlotSize, Padding: 16}
	if m.preview != nil {
		geometry = m.preview.Geometry
	}
	scale := menuMakerPreviewScale(settings.MenuMakerSettings)
	px := func(v int) float32 { return float32(v) * scale }

	panel := fairy.Col().Size(px(geometry.PanelWidth), px(geometry.PanelHeight)).Shrink(0).Radius(4).Clip().
		Fill(menuMakerUIColor(palette.PanelBackground, palette.PanelBackgroundAlpha)).
		Shadow(0, 25*scale, 50*scale, -12*scale, fairy.RGBA(0, 0, 0, 0.25))
	if img := menuMakerImage(settings.PanelImageDataURL, &m.settings.panelImage); img != nil {
		panel.Child(fairy.ImageEl(img).Fit(fairy.FitCover).Absolute().Inset(0).
			Opacity(float32(palette.PanelBackgroundAlpha) / 255))
	}
	if title := strings.TrimSpace(settings.Title); title != "" {
		panel.Child(
			fairy.Row(fairy.Text(title).SemiBold().FontSize(18*scale).Color(menuMakerUIColor(palette.Title, 255)).
				Ellipsis()).H(px(geometry.TitleHeight)).Align(fairy.Center).PadXY(16*scale, 0).Shrink(0),
		)
	}
	columns := max(1, settings.Columns)
	grid := fairy.Col().Gap(px(geometry.ScaledGap)).Pad(px(geometry.Padding))
	active := slices.DeleteFunc(slices.Clone(m.slots), func(slot menuMakerSlot) bool { return slot.Skip })
	for row := range slices.Chunk(active, columns) {
		line := fairy.Row().Gap(px(geometry.ScaledGap))
		for _, slot := range row {
			line.Child(s.menuMakerPreviewSlot(slot, px(geometry.SlotSize)))
		}
		grid.Child(line)
	}
	panel.Child(grid)
	panel.Child(fairy.Box().Absolute().Inset(0).Radius(4).PassThrough().
		Border(2*scale, menuMakerUIColor(palette.PanelBorder, palette.PanelBorderAlpha)))
	return fairy.Col(panel).Align(fairy.Center)
}

func (s *Shell) menuMakerPreviewSlot(slot menuMakerSlot, size float32) *fairy.El {
	m := &s.tools.menuMaker
	settings := m.settings
	palette := settings.Palette
	align := fairy.TextCenter
	switch settings.SlotAlignment {
	case "left":
		align = fairy.TextStart
	case "right":
		align = fairy.TextEnd
	}
	box := fairy.Col().Size(size, size).Shrink(0).Center().Pad(4).Radius(2).Clip().
		Fill(menuMakerUIColor(palette.SlotBackground, palette.SlotBackgroundAlpha)).
		Border(1, menuMakerUIColor(palette.SlotBorder, palette.SlotBorderAlpha))
	box.Child(s.menuMakerSlotIcon(slot, size*0.42, size*0.45))
	if slot.Icon.Kind != "upload" || !settings.HideUploadLabel {
		box.Child(fairy.Text(slot.Name).FontSize(max(6, size*8/menuMakerBaseSlotSize)).Font("Inter").
			Color(fairy.RGBA(255, 255, 255, 1)).Ellipsis().TextAlign(align).WFull().PadEdges(2, 0, 0, 0))
	}
	if settings.ShowKeyHint {
		box.Child(fairy.Text(slot.Key).FontSize(max(5, size*7/menuMakerBaseSlotSize)).Font("Inter").
			Color(menuMakerUIColor(palette.Accent, 255)).Ellipsis().TextAlign(align).WFull())
	}
	return box
}

// menuMakerSlotIcon draws a slot's icon: a vector of iconSize or an uploaded image of imageSize.
func (s *Shell) menuMakerSlotIcon(slot menuMakerSlot, iconSize, imageSize float32) *fairy.El {
	m := &s.tools.menuMaker
	if slot.Icon.Kind == "upload" {
		i := slices.IndexFunc(m.slots, func(other menuMakerSlot) bool { return other.ID == slot.ID })
		if i >= 0 {
			if img := menuMakerImage(slot.Icon.DataURL, &m.slots[i].image); img != nil {
				return fairy.ImageEl(img).Size(imageSize, imageSize).Fit(fairy.FitCover).Radius(4)
			}
		}
		return fairy.Icon(lucide.Image).Size(iconSize, iconSize).Color(kit.T().MutedForeground)
	}
	if vec := menuMakerIconVector(slot.Icon); vec != nil {
		return fairy.Icon(vec).Size(iconSize, iconSize).Color(menuMakerUIColor(slot.Icon.Color, 255))
	}
	return fairy.Icon(lucide.Image).Size(iconSize, iconSize).Color(kit.T().MutedForeground)
}

func (s *Shell) menuMakerEmptyState() *fairy.El {
	m := &s.tools.menuMaker
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	card := fairy.Col(fairy.Text(T(prefix+"empty_title")).FontSize(16).LineHeight(24).Medium().MarginEdges(0, 0, 8, 0)).
		Pad(24).WFull().MaxW(576).Radius(8).Fill(t.Card).Border(1, t.Border).BorderDashed()
	switch {
	case m.scan != nil && len(m.scan.Files) > 0:
		files := fairy.Col().Gap(8)
		card.Child(files)
		for _, file := range m.scan.Files {
			files.Child(fairy.Row(
				fairy.Text(file.RelativePath).Ellipsis().Grow(1).MinW(0).TooltipWhenTruncated(),
				fairy.Text(strings.ToUpper(file.Kind)).FontSize(12).Color(t.MutedForeground),
			).Key("menu-maker.file."+file.Path).Gap(12).Align(fairy.Center).PadXY(12, 12).Radius(t.RadiusMd()).
				Border(1, t.Border).
				HoverFill(hoverFill(t)).Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().
				Label(file.RelativePath).OnClick(func() { s.menuMakerLoadSource(file.Path) }))
		}
	case m.scan != nil:
		root := m.scan.RootPath
		card.Child(kit.Button(T(prefix+"include_txt"), func() { s.menuMakerScan(root, true) }, kit.Outline,
			kit.Disabled(m.busy)).AlignSelf(fairy.Start).MarginEdges(12, 0, 0, 0))
	case m.busy:
		card.Child(kit.Spinner(16))
	}
	return fairy.Col(card).Align(fairy.Center)
}

func (s *Shell) menuMakerSidePanel(twoColumns bool) *fairy.El {
	m := &s.tools.menuMaker
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	tabs := fairy.Row(menuMakerPanelButtons(m.sidePanel, []kit.Item{
		{Value: "editor", Label: T(prefix + "editor"), Icon: lucide.Pencil},
		{Value: "ini", Label: T(prefix + "ini_preview"), Icon: lucideIcon("file-code")},
	}, func(value string) {
		m.sidePanel = value
		if m.narrowPanel == "editor" || m.narrowPanel == "ini" {
			m.narrowPanel = value
		}
	}).Grow(1)).Pad(4).BorderEdges(0, 0, 1, 0, t.Border)
	panel := fairy.Col(tabs).MinH(0)
	if m.sidePanel == "ini" {
		return panel.Child(s.menuMakerINIView().Grow(1))
	}
	return panel.Child(s.menuMakerSlotEditor(twoColumns).Grow(1).MinH(0))
}

func (s *Shell) menuMakerINIView() *fairy.El {
	m := &s.tools.menuMaker
	text := T("page.tools.menu_maker.no_preview")
	switch {
	case m.genError != "":
		text = m.genError
	case m.preview != nil && m.preview.INIText != "":
		text = m.preview.INIText
	}
	return menuMakerTextLines("menu-maker.ini", text).Pad(12)
}

// menuMakerTextLines shows text as wrapped monospace lines, building only the lines in view.
func menuMakerTextLines(key, text string) *fairy.El {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return fairy.VirtualListMeasured(len(lines), menuMakerINILineGuess, nil, func(i int) *fairy.El {
		line := lines[i]
		if line == "" {
			line = " "
		}
		return fairy.Text(line).Font(monoFont).FontSize(11).LineHeight(20).WFull()
	}).Key(key).MinH(0)
}

func (s *Shell) menuMakerSlotEditor(twoColumns bool) *fairy.El {
	m := &s.tools.menuMaker
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	empty := m.document == nil
	none := len(m.selected) == 0
	toolbar := fairy.Row(
		fairy.Text(fmt.Sprintf("%d %s", len(m.slots), T(prefix+"slots"))).FontSize(12).Color(t.MutedForeground),
		kit.Select("menu-maker.merge-mode", m.mergeMode, []kit.Item{
			{Value: "guiOnly", Label: T(prefix + "merge_gui_only")},
			{Value: "allKeys", Label: T(prefix + "merge_all_keys")},
		}, func(v string) { m.mergeMode = v }, kit.Small, kit.Disabled(empty)).Label(T(prefix+"merge")),
		menuMakerButtonGroup(true, kit.Button(T(prefix+"merge"), s.menuMakerMerge, kit.Outline, kit.Small,
			kit.Icon(lucideIcon("merge")), kit.Disabled(len(m.selected) < 2)),
			kit.Button(T(prefix+"skip"), func() {
				s.menuMakerMapSelected(func(slot *menuMakerSlot) { slot.Skip = true })
			}, kit.Outline, kit.Small, kit.Icon(lucideIcon("ban")), kit.Disabled(none)),
			kit.Button(T(prefix+"reset"), func() {
				s.menuMakerMapSelected(func(slot *menuMakerSlot) {
					slot.Skip = false
					slot.Icon, slot.image = menuMakerIcon{
						Kind:  "lucide",
						Name:  "circle-dot",
						Color: menuMakerDefaultColor,
					}, nil
				})
			}, kit.Outline, kit.Small, kit.Icon(lucide.RotateCcw), kit.Disabled(none)),
			kit.Button(T(prefix+"auto_icon"), func() {
				s.menuMakerMapSelected(func(slot *menuMakerSlot) {
					slot.Icon, slot.image = suggestMenuMakerIcon(slot.MenuMakerSlot), nil
				})
			}, kit.Outline, kit.Small, kit.Icon(lucideIcon("wand-sparkles")), kit.Disabled(none))),
	).Wrap().Gap(8).RowGap(8).Align(fairy.Center)

	body := fairy.Col(toolbar).Gap(12).Pad(12)
	if empty {
		return body.Child(fairy.Text(T(prefix + "no_preview")).FontSize(12).Color(t.MutedForeground))
	}
	list := fairy.Col().Gap(menuMakerSlotRowGap)
	columns := 1
	if twoColumns {
		columns = 2
	}
	for at := 0; at < len(m.slots); at += columns {
		row := fairy.Row().Gap(menuMakerSlotRowGap)
		for col := range columns {
			i := at + col
			card := fairy.Box()
			if i < len(m.slots) {
				card = s.menuMakerSlotCard(i, columns, m.slots[i])
			}
			row.Child(card.Basis(0).Grow(1).MinW(0))
		}
		list.Child(row)
	}
	return fairy.Col(body.Child(list)).ScrollY()
}

func (s *Shell) menuMakerSlotCard(index, columns int, slot menuMakerSlot) *fairy.El {
	m := &s.tools.menuMaker
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	palette := m.settings.Palette
	id := slot.ID
	skipLabel := T(prefix + "skip")
	if slot.Skip {
		skipLabel = T(prefix + "restore")
	}

	size := fairy.Local[[2]float32]("menu-maker.slot-size:" + id)
	blockDrag := func(fairy.DragEvent) {}
	top := fairy.Row(
		kit.Checkbox(slices.Contains(m.selected, id), func(on bool) {
			m.selected = slices.DeleteFunc(m.selected, func(other string) bool { return other == id })
			if on {
				m.selected = append(m.selected, id)
			}
		}).Label(T(prefix+"select_slot")).OnDrag(blockDrag),
		fairy.Text(fmt.Sprintf("%d handler", len(slot.Handlers))).FontSize(10).LineHeight(15).
			Color(t.MutedForeground).Grow(1).MinW(0).Ellipsis(),
		kit.Button(skipLabel, func() {
			s.menuMakerUpdateSlot(id, func(slot *menuMakerSlot) { slot.Skip = !slot.Skip })
		}, kit.Ghost, kit.XS).Color(t.MutedForeground).OnDrag(blockDrag),
	).Gap(4).Align(fairy.Center)

	iconButton := fairy.Box(s.menuMakerSlotIcon(slot, 16, 32)).Size(36, 36).Shrink(0).Center().Radius(t.RadiusMd()).
		AlignSelf(fairy.Center).MarginEdges(4, 0, 4, 0).
		HoverFill(hoverFill(t)).
		Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().Label(T(prefix + "icon_picker")).
		OnClick(func() { s.openMenuMakerIconPicker(id) }).OnDrag(blockDrag)
	name := kit.TextField(slot.Name, func(v string) {
		s.menuMakerUpdateSlot(id, func(slot *menuMakerSlot) { slot.Name = v })
	}, kit.Small).Key("menu-maker.slot-name." + id).Label(slot.Name).
		H(28).FontSize(12).LineHeight(16).TextAlign(fairy.TextCenter).Grow(0).Shrink(0).OnDrag(blockDrag)
	key := kit.TextField(slot.Key, func(v string) {
		next, err := menumaker.SetSlotKey(slot.MenuMakerSlot, v)
		if err != nil {
			s.toasts.Show(kit.ToastError, T(prefix+"multi_key_locked"), "")
			return
		}
		s.menuMakerUpdateSlot(id, func(slot *menuMakerSlot) { slot.MenuMakerSlot = next })
	}, kit.Small).Key("menu-maker.slot-key."+id).Font(monoFont).Label(slot.Key).
		H(24).PadXY(4, 0).FontSize(10).LineHeight(15).TextAlign(fairy.TextCenter).Grow(1).MinW(0).OnDrag(blockDrag)
	upload := kit.IconButton(lucide.Upload, func() { s.menuMakerPickImage("slot", id) }, kit.Ghost, kit.XS,
		kit.Tooltip(T(prefix+"upload_icon"))).Label(T(prefix + "upload_icon")).OnDrag(blockDrag)

	row := fairy.Col(
		top,
		iconButton,
		name,
		fairy.Row(key, upload).Gap(4).Align(fairy.Center).MarginEdges(4, 0, 0, 0),
	).Key("menu-maker.slot."+id).MinH(menuMakerSlotMinH).Pad(8).Radius(4).
		Fill(menuMakerUIColor(palette.SlotBackground, palette.SlotBackgroundAlpha)).
		Border(1, menuMakerUIColor(palette.SlotBorder, 255)).
		OnLayout(func(w, h float32) { *size = [2]float32{w, h} }).
		OnDrag(func(e fairy.DragEvent) { s.menuMakerDragSlot(index, columns, *size, e) })
	if slot.Skip {
		row.Opacity(0.4)
	}
	if m.drag.active && m.drag.index == index {
		row.Ring(2, t.Accent)
	}
	return row
}

func (s *Shell) menuMakerDragSlot(index, columns int, size [2]float32, e fairy.DragEvent) {
	m := &s.tools.menuMaker
	d := &m.drag
	sourceValid := func() bool { return d.from >= 0 && d.from < len(m.slots) }
	switch e.Phase {
	case fairy.DragStart:
		if size[0] <= 0 || size[1] <= 0 {
			return
		}
		*d = menuMakerDrag{
			active: true, index: index, from: index, columns: columns,
			stepX: size[0] + menuMakerSlotRowGap, stepY: size[1] + menuMakerSlotRowGap,
		}
	case fairy.DragMove:
		if !d.active || !sourceValid() || d.columns != columns {
			d.active = false
			return
		}
		row := d.from/d.columns + int(e.DY/d.stepY+sign(e.DY)*0.5)
		col := d.from%d.columns + int(e.DX/d.stepX+sign(e.DX)*0.5)
		d.index = -1
		if target := row*d.columns + col; row >= 0 && col >= 0 && col < d.columns && target < len(m.slots) {
			d.index = target
		}
	case fairy.DragEnd:
		if !d.active || !sourceValid() || d.columns != columns {
			d.active = false
			return
		}
		d.active = false
		if !e.Canceled && d.index >= 0 && d.index < len(m.slots) && d.index != d.from {
			slot := m.slots[d.from]
			m.slots = slices.Insert(slices.Delete(m.slots, d.from, d.from+1), d.index, slot)
			s.menuMakerChanged()
		}
	}
}
