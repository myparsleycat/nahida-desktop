package ui

import (
	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/setting"
)

const (
	folderGridGap      float32 = 6
	folderGridPad      float32 = 8
	folderGridMinCell  float32 = 120
	folderGridNameLine float32 = 16
)

// folderLine is a line of the sidebar grid: a section header or up to one row of folder cells.
type folderLine struct {
	section *folderRow
	cells   []folderRow
}

func (l folderLine) key() string {
	if l.section != nil {
		return l.section.rowKey()
	}
	return "grid:" + l.cells[0].rowKey()
}

func (s *Shell) folderGridLayout() bool {
	return s.settingsLoaded(setting.KeyModSidebarLayout) && s.stringSetting(setting.KeyModSidebarLayout) == "grid"
}

// folderGridColumns fits cells of at least 120 pixels into width, and at least two, as the React grid's
// repeat(auto-fill, minmax(min(120px, 50%), 1fr)) did.
func folderGridColumns(width float32) (int, float32) {
	inner := width - 1 - 2*folderGridPad
	minimum := min(folderGridMinCell, (inner-folderGridGap)/2)
	columns := max(int((inner+folderGridGap)/(minimum+folderGridGap)), 1)
	cell := (inner - folderGridGap*float32(columns-1)) / float32(columns)
	return columns, cell
}

// folderLines groups the sidebar rows into grid lines. Sections span the full width and end the line before them.
func folderLines(rows []folderRow, columns int) []folderLine {
	var lines []folderLine
	var cells []folderRow
	flush := func() {
		if len(cells) > 0 {
			lines = append(lines, folderLine{cells: cells})
			cells = nil
		}
	}
	for i := range rows {
		if rows[i].section {
			flush()
			lines = append(lines, folderLine{section: &rows[i]})
			continue
		}
		cells = append(cells, rows[i])
		if len(cells) == columns {
			flush()
		}
	}
	flush()
	return lines
}

// folderListIndex is the line of the sidebar list built from rows that shows the folder at path, or -1.
func (s *Shell) folderListIndex(rows []folderRow, path string) int {
	if !s.folderGridLayout() {
		for i, row := range rows {
			if !row.section && row.group.Path == path {
				return i
			}
		}
		return -1
	}
	columns, _ := folderGridColumns(s.modSidebarWidth())
	for i, line := range folderLines(rows, columns) {
		for _, cell := range line.cells {
			if cell.group.Path == path {
				return i
			}
		}
	}
	return -1
}

func (s *Shell) folderGrid(rows []folderRow) *fairy.El {
	columns, cell := folderGridColumns(s.modSidebarWidth())
	lines := folderLines(rows, columns)
	return fairy.VirtualListMeasured(len(lines), cell+folderGridNameLine+folderGridGap,
		func(i int) string { return lines[i].key() },
		func(i int) *fairy.El {
			line := lines[i]
			if line.section != nil {
				return s.folderRowView(*line.section)
			}
			row := fairy.Row().Gap(folderGridGap).PadEdges(0, folderGridPad, folderGridGap, folderGridPad)
			if i == 0 || lines[i-1].section != nil {
				row.PadEdges(folderGridPad, folderGridPad, folderGridGap, folderGridPad)
			}
			for _, folder := range line.cells {
				row.Child(s.folderCellView(folder, cell))
			}
			return row
		},
	).Key("mod.folders").Grow(1).MinH(0)
}

func folderGridSkeleton(width float32) *fairy.El {
	columns, cell := folderGridColumns(width)
	grid := fairy.Row().Key("mod.folders").Grow(1).Wrap().Gap(folderGridGap).Pad(folderGridPad)
	for range max(columns*3, 8) {
		grid.Child(fairy.Col(
			kit.Skeleton().Size(cell-8, cell-8).Radius(8),
			kit.Skeleton().H(12).W(cell*0.6),
		).W(cell).Pad(4).Gap(4).Align(fairy.Center))
	}
	return grid
}

// folderCellView is a folder of the sidebar grid: its preview as a square with the mod count, and its name.
func (s *Shell) folderCellView(row folderRow, size float32) *fairy.El {
	t := kit.T()
	m := &s.mods
	g := row.group
	selected := m.group != nil && m.group.Path == g.Path
	preview := fairy.Box(s.previewThumb(g.Preview, m.previewVersion, size-8, 8,
		fairy.Text("?").Bold())).WFull().HFull().Center()
	if !g.IsEnabled {
		preview.Opacity(0.5)
	}
	badge := func(text string) *fairy.El {
		return fairy.Text(text).FontSize(11).Ellipsis().PadXY(6, 2).Radius(4).Fill(t.Background.Alpha(0.85)).
			Color(t.MutedForeground).Shadow(0, 1, 2, 0, shadowColor(t))
	}
	square := fairy.Box(preview).Size(size-8, size-8).Radius(8).Fill(t.Muted).Clip()
	if row.depth == 0 && row.parentName != "" {
		square.Child(badge(row.parentName).Absolute().Top(6).Left(6).MaxW(size - 8 - 44))
	}
	square.Child(badge(itoa(g.EnabledModCount) + " / " + itoa(g.ModCount)).NoWrap().Absolute().Bottom(6).Right(6))

	ink := t.Foreground
	if !g.IsEnabled {
		ink = t.MutedForeground
	}
	fill, hover := t.Muted.Alpha(0.4), t.Accent.Alpha(0.4)
	if row.depth > 0 {
		fill = t.Muted.Alpha(0.2)
	}
	item := fairy.Col(
		square,
		fairy.Text(g.Name).
			FontSize(12).
			LineHeight(16).
			Medium().
			Color(ink).
			Ellipsis().
			TextAlign(fairy.TextCenter).
			WFull().
			TooltipWhenTruncated(),
	).Key(row.rowKey()).W(size).Pad(4).Gap(2).Align(fairy.Center).Radius(8).Fill(fill).HoverFill(hover).
		Animate(motion.Fast).Cursor(fairy.CursorPointer).Focusable().Selected(selected).
		Expanded(m.expanded[g.Path]).Label(g.Name).
		OnClick(func() { s.clickFolder(row) }).
		OnFileDrop(func(drop fairy.FileDrop) { s.dropFolderPreview(g, drop.Paths) }).
		DropFill(t.Accent.Alpha(0.12)).DropBorder(t.Accent)
	if row.depth > 0 {
		item.BorderEdges(0, 0, 0, 4, t.Primary.Alpha(0.4))
	}
	if selected {
		item.Fill(t.Accent.Alpha(0.5)).HoverFill(t.Accent.Alpha(0.5)).Ring(1, t.Primary)
	}
	if m.dropTarget == row.rowKey() {
		item.Child(folderDropHint(g.Name))
	}
	return kit.ContextMenu("mod.folder:"+g.Path, item, func() []kit.MenuItem { return s.folderMenuItems(g) },
		kit.Width(224))
}

// folderLayoutItem switches the sidebar between the row list and the preview grid.
func (s *Shell) folderLayoutItem() kit.MenuItem {
	next, icon := "grid", lucide.LayoutGrid
	if s.folderGridLayout() {
		next, icon = "row", lucide.List
	}
	return kit.MenuItem{
		Label: T("page.mod.character-sidebar.toggle-layout", "mode", T("page.setting.mod.layout.sidebar.modes."+next)),
		Icon:  icon,
		OnClick: func() {
			s.setSetting(setting.KeyModSidebarLayout, next)
		},
	}
}

// resizeModSidebarKey moves the sidebar edge with the arrow keys, by 64 pixels with Shift, and to either limit
// with Home and End.
func (s *Shell) resizeModSidebarKey(k keyboard.KeyEvent) bool {
	width := s.modSidebarWidth()
	switch {
	case k.Is(keyboard.KeyLeft, 0):
		width -= 16
	case k.Is(keyboard.KeyRight, 0):
		width += 16
	case k.Is(keyboard.KeyLeft, keyboard.ModShift):
		width -= 64
	case k.Is(keyboard.KeyRight, keyboard.ModShift):
		width += 64
	case k.Is(keyboard.KeyHome, 0):
		width = modSidebarWidthMin
	case k.Is(keyboard.KeyEnd, 0):
		width = modSidebarWidthMax
	default:
		return false
	}
	s.setSetting(setting.KeyModCharacterSidebarWidth, int(min(max(width, modSidebarWidthMin), modSidebarWidthMax)))
	return true
}
