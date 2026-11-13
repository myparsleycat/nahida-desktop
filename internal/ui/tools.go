package ui

import (
	"regexp"
	"slices"
	"strings"

	"github.com/samber/lo"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/tools"
)

type toolID string

const (
	tool4001Fixer    toolID = "4001-fixer"
	toolPersist      toolID = "toggle-persistence"
	toolMenuMaker    toolID = "menu-maker"
	toolToggleViewer toolID = "toggle-viewer-generator"
	toolFixTool      toolID = "fix-tool-manager"
	toolBisect       toolID = "mod-bisect"
	toolBodyShape    toolID = "body-shape"
	toolTouchProfile toolID = "touch-profile"
)

type toolInfo struct {
	id       toolID
	nameKey  string
	initials string
	beta     bool
	// page tools take the whole screen, as the React frontend gave them their own route.
	page bool
	// unported tools are listed but show the not-ported notice.
	unported bool
}

var toolList = []toolInfo{
	{id: tool4001Fixer, nameKey: "page.tools.4001_fixer.title", initials: "41"},
	{id: toolPersist, nameKey: "page.setting.xxmi.persistToggles", initials: "TP"},
	{id: toolMenuMaker, nameKey: "page.tools.menu_maker.title", initials: "MM", page: true},
	{id: toolToggleViewer, nameKey: "page.tools.toggle_viewer_generator.title", initials: "TV"},
	{id: toolFixTool, nameKey: "page.tools.fix-tool-manager.title", initials: "FT"},
	{id: toolBisect, nameKey: "page.tools.mod_bisect.title", initials: "MB"},
	{id: toolBodyShape, nameKey: "page.tools.body_shape.title", initials: "BS", beta: true},
	{id: toolTouchProfile, nameKey: "page.tools.touch_profile.title", initials: "AT", beta: true},
}

const toolsSidebarBreakpoint = 768
const toolsWideViewport = 1024

func toolName(tool toolInfo) string {
	if tool.beta {
		return T(tool.nameKey) + " (" + T("g.beta") + ")"
	}
	return T(tool.nameKey)
}

func toolByID(id toolID) (toolInfo, bool) {
	i := slices.IndexFunc(toolList, func(tool toolInfo) bool { return tool.id == id })
	if i < 0 {
		return toolInfo{}, false
	}
	return toolList[i], true
}

// toolsScreen is the state of the tools screen. active is "" for the dashboard grid.
type toolsScreen struct {
	active toolID
	// sidebarClosed hides the tool list in a narrow window, as the React dashboard did below its md breakpoint.
	sidebarClosed bool

	persist      persistTool
	toggleViewer toggleViewerTool
	bisect       bisectTool
	fixer4001    fixer4001Tool
	fixTool      fixToolManager
	menuMaker    menuMakerTool
	// bodyShape is the tools page's body shape editor and bodyShapeDialog the one the mod screen opens; nil
	// while not shown.
	bodyShape       *bodyShapeEditor
	bodyShapeDialog *bodyShapeEditor
	touch           touchProfile
	// touchDialog is the Touch Profile dialog the mod menu opened for one mod; nil while closed.
	touchDialog *touchProfile
	// liquidDialog is the Liquid Emitter dialog the mod menu opened; nil while closed.
	liquidDialog *liquidTool
	// textureRun is the texture process dialog running a batch, which receives the upscale progress.
	textureRun *textureDialog
	// textureProgress is the backend's texture resize state, including runs started from the mod screen.
	textureProgress *tools.TextureResizeProgressEvent
}

// navigateTools opens the tool a /tools path names, such as /tools/menu-maker.
func (s *Shell) navigateTools(path string) {
	rest, _ := strings.CutPrefix(path, string(RouteTools))
	id := toolID(strings.Trim(rest, "/"))
	if _, ok := toolByID(id); ok || id == "" {
		s.openTool(id)
	}
}

func (s *Shell) openTool(id toolID) {
	if id != toolBodyShape {
		s.closeBodyShapePage()
	}
	s.tools.active = id
}

func (s *Shell) toolsPage(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	active, hasActive := toolByID(s.tools.active)
	// A page tool was its own React route outside the dashboard, without the tools sidebar or breadcrumb.
	if hasActive && active.page && !active.unported {
		return s.toolBody(cx, active).Key("tools." + string(active.id)).Grow(1).MinW(0).HFull().
			Fill(t.Background.Alpha(0.7))
	}

	items := fairy.Col().Gap(2).Role(fairy.RoleList)
	for _, tool := range toolList {
		items.Child(s.toolNavItem(tool, hasActive && tool.id == active.id))
	}
	aside := fairy.Col(
		fairy.Row(
			fairy.Box(fairy.Icon(lucide.Wrench).Size(14, 14).Color(t.Accent)).Size(28, 28).Center().Radius(4).
				Fill(t.Accent.Alpha(0.2)),
			fairy.Text("Tools").FontSize(14).LineHeight(20).LetterSpacing(-0.35).SemiBold().Color(t.SidebarForeground),
		).Shrink(0).Gap(10).Align(fairy.Center).Pad(16).BorderEdges(0, 0, 1, 0, t.SidebarBorder),
		fairy.Col(
			fairy.Text(strings.ToUpper(T("page.tools.dashboard.tools_label"))).
				FontSize(10).
				LineHeight(15).
				LetterSpacing(1).
				Medium().
				Color(t.MutedForeground).
				PadEdges(0, 8, 8, 8),
			items,
		).Grow(1).MinH(0).ScrollY().PadXY(8, 12),
	).W(256).HFull().Shrink(0).Fill(t.Background.Alpha(0.5)).BorderEdges(0, 1, 0, 0, t.Border)
	width, _ := cx.Size()
	narrow := width < toolsSidebarBreakpoint

	header := fairy.Row()
	if narrow {
		icon, label := lo.Ternary(
			s.tools.sidebarClosed,
			lucide.Menu,
			lucide.X,
		), T(
			"page.tools.dashboard.toggle_sidebar",
		)
		header.Child(kit.IconButton(icon, func() { s.tools.sidebarClosed = !s.tools.sidebarClosed }, kit.Ghost,
			kit.Small, kit.Tooltip(label)).Label(label))
	}
	header.Child(
		fairy.Text(T("page.tools.dashboard.tools_label")).FontSize(12).LineHeight(16).Medium().Color(t.Foreground).
			Key("tools.header.root").
			Cursor(fairy.CursorPointer).OnClick(func() { s.openTool("") }),
	)
	header.H(40).Shrink(0).PadXY(16, 0).Gap(6).Align(fairy.Center).FontSize(12).Font(monoFont).
		Color(t.MutedForeground).BorderEdges(0, 0, 1, 0, t.Border)
	var body *fairy.El
	if hasActive {
		header.Child(fairy.Text("/").FontSize(12).LineHeight(16),
			fairy.Text(toolName(active)).FontSize(12).LineHeight(16).Color(t.Foreground))
		body = s.toolBody(cx, active).Key("tools." + string(active.id)).Grow(1).MinH(0)
	} else {
		body = s.toolsGrid(width)
	}
	main := fairy.Col(header, body).Grow(1).MinW(0).HFull().Fill(t.Background.Alpha(0.7))
	if narrow && s.tools.sidebarClosed {
		return fairy.Row(main).WFull().HFull()
	}
	return fairy.Row(aside, main).WFull().HFull()
}

func (s *Shell) toolNavItem(tool toolInfo, active bool) *fairy.El {
	t := kit.T()
	chip, ink := t.Secondary, t.MutedForeground
	if active {
		chip, ink = t.Accent, t.AccentForeground
	}
	name := toolName(tool)
	hovered := fairy.Local[bool]("tools.nav.hover:" + string(tool.id))
	row := fairy.Row(
		fairy.Text(tool.initials).Size(20, 20).Center().TextAlign(fairy.TextCenter).Radius(4).
			Fill(chip).Color(ink).Font(monoFont).FontSize(9).LineHeight(13.5).Bold().Animate(motion.Fast),
		fairy.Text(name).FontSize(12).LineHeight(16).Grow(1).MinW(0).Ellipsis(),
	).Key("tools.nav."+string(tool.id)).PadXY(8, 8).Gap(10).Align(fairy.Center).Radius(t.RadiusMd()).
		HoverFill(t.SidebarAccent).Color(t.SidebarForeground).Animate(motion.Fast).
		Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().Label(name).Selected(active).
		OnHover(func(in bool) { *hovered = in }).
		OnClick(func() { s.openTool(tool.id) })
	switch {
	case active:
		row.Fill(t.SidebarAccent).Color(t.SidebarAccentForeground).
			Child(fairy.Icon(lucide.ChevronRight).Size(12, 12).Color(t.Accent))
	case tool.page && !tool.unported:
		row.Child(fairy.Icon(lucide.ExternalLink).Size(12, 12).Color(t.MutedForeground).
			Opacity(toolHoverOpacity(*hovered)).Animate(motion.Fast))
	}
	return row
}

func toolHoverOpacity(hovered bool) float32 {
	if hovered {
		return 1
	}
	return 0
}

func (s *Shell) toolsGrid(width float32) *fairy.El {
	t := kit.T()
	grid := fairy.Col().Gap(12).Pad(16)
	columns := 1
	if width >= 640 {
		columns = 2
	}
	var row *fairy.El
	for i, tool := range toolList {
		if i%columns == 0 {
			row = fairy.Row().Gap(12)
			grid.Child(row)
		}
		hovered := fairy.Local[bool]("tools.card.hover:" + string(tool.id))
		subtitle := T("page.tools.dashboard.inline_tool")
		chip, ink := t.Secondary, t.MutedForeground
		var trailing *fairy.El
		switch {
		case tool.unported:
			subtitle = T("shell.not_ported")
		case tool.page:
			subtitle = T("page.tools.dashboard.external_page")
			trailing = fairy.Icon(lucide.ExternalLink).Size(16, 16).Color(t.MutedForeground)
		default:
			trailing = fairy.Icon(lucide.ChevronRight).Size(16, 16).Color(t.MutedForeground)
			if *hovered {
				chip, ink = t.Accent.Alpha(0.2), t.Accent
				trailing.Color(t.Accent)
			}
		}
		name := toolName(tool)
		card := fairy.Row(
			fairy.Text(tool.initials).Size(36, 36).Center().TextAlign(fairy.TextCenter).Radius(t.RadiusMd()).
				Fill(chip).Color(ink).Font(monoFont).FontSize(14).LineHeight(20).Bold().Animate(motion.Fast),
			fairy.Col(
				fairy.Text(name).FontSize(14).LineHeight(20).Medium().Wrap(),
				fairy.Text(subtitle).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
			).Gap(2).Grow(1).MinW(0),
		).Key("tools.card." + string(tool.id)).Basis(0).Grow(1).MinW(0).Pad(16).Gap(12).Align(fairy.Center).
			Radius(t.RadiusLg()).HoverFill(t.Muted.Alpha(0.5)).Animate(motion.Fast).
			Cursor(fairy.CursorPointer).Label(name).
			Role(fairy.RoleButton).Focusable().
			OnHover(func(in bool) { *hovered = in }).
			OnClick(func() { s.openTool(tool.id) })
		if trailing != nil {
			card.Child(trailing.Shrink(0).Opacity(toolHoverOpacity(*hovered)).Animate(motion.Fast))
		}
		row.Child(card)
	}
	if len(toolList)%columns != 0 {
		row.Child(fairy.Box().Basis(0).Grow(1).MinW(0))
	}
	return fairy.Col(fairy.Col(grid).WFull().MaxW(768)).Grow(1).MinH(0).WFull().Align(fairy.Center).ScrollY()
}

func (s *Shell) toolBody(cx *fairy.Cx, tool toolInfo) *fairy.El {
	switch tool.id {
	case toolMenuMaker:
		return s.menuMakerToolPage(cx)
	case toolBodyShape:
		return s.bodyShapePage().page(cx)
	case toolTouchProfile:
		return s.touchProfileToolPage(cx)
	case toolPersist:
		return s.persistToolPage()
	case toolToggleViewer:
		return s.toggleViewerToolPage(cx)
	case toolBisect:
		return s.bisectToolPage()
	case tool4001Fixer:
		return s.fixer4001ToolPage(cx)
	case toolFixTool:
		return s.fixToolPage(cx)
	}
	return notPorted(lucide.Wrench, toolName(tool))
}

func toolsViewportSplit(viewport, panel *fairy.El, stacked bool) *fairy.El {
	if stacked {
		return fairy.Col(viewport.Basis(0).Grow(1), panel.Basis(0).Grow(1).MinH(0)).
			Gap(12).Grow(1).MinH(0).WFull()
	}
	return fairy.Row(viewport.Grow(1).MinW(0), panel.W(360).Shrink(0)).Gap(12).Grow(1).MinH(0).WFull()
}

// toolPage lays out an inline tool: a scrolling column of sections.
func toolPage(children ...*fairy.El) *fairy.El {
	return fairy.Col(children...).Pad(16).Gap(24).WFull().ScrollY()
}

// toolHeading is a tool's title and description, with an optional control on the right.
func toolHeading(title, description string, control *fairy.El) *fairy.El {
	t := kit.T()
	row := fairy.Row(fairy.Col(
		fairy.Text(title).FontSize(18).LineHeight(28).SemiBold(),
		fairy.Text(description).FontSize(14).LineHeight(20).Color(t.MutedForeground).Wrap(),
	).Gap(4).Grow(1).MinW(0)).Gap(24).Align(fairy.Start)
	if control != nil {
		row.Child(control.Shrink(0).MarginEdges(4, 0, 0, 0))
	}
	return row
}

// toolSection uses SectionContent's flow layout for logs and step-by-step tools.
func toolSection(title string, children ...*fairy.El) *fairy.El {
	t := kit.T()
	section := fairy.Col().Gap(4)
	if title != "" {
		section.Child(fairy.Text(title).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground).
			PadEdges(0, 4, 8, 4))
	}
	return section.Child(fairy.Col(children...).Gap(12).PadEdges(4, 14, 4, 30).
		BorderEdges(0, 0, 0, 1, t.Foreground.Alpha(0.2)))
}

// toolXXMIMissing replaces a tool that needs the XXMI path with a pointer to the XXMI screen.
func (s *Shell) toolXXMIMissing(title, message string) *fairy.El {
	t := kit.T()
	return fairy.Col(
		fairy.Icon(lucide.FolderSearch).Size(32, 32).Color(t.MutedForeground),
		fairy.Col(
			fairy.Text(title).FontSize(14).LineHeight(20).Medium().TextAlign(fairy.TextCenter),
			fairy.Text(message).FontSize(14).LineHeight(20).Color(t.MutedForeground).TextAlign(fairy.TextCenter).Wrap(),
		).Gap(4).Align(fairy.Center),
		kit.Button(T("page.setting.xxmi.xxmiPath"), func() { s.navigatePath(string(RouteXXMI)) }, kit.Outline,
			kit.Small, kit.IconRight(lucide.ArrowRight)),
	).WFull().HFull().Center().Gap(12).Pad(16)
}

// toolLogPattern matches backend log entries such as "[Jan 2, 2006, 3:04:05 PM] [INFO] message".
var toolLogPattern = regexp.MustCompile(`^\[(.+?)\] \[(INFO|ERROR)\] ((?s).*)$`)

// toolLogList shows a tool's log, newest first.
func toolLogList(logs []string, empty string) *fairy.El {
	t := kit.T()
	if len(logs) == 0 {
		return fairy.Text(empty).FontSize(12).LineHeight(16).Color(t.MutedForeground)
	}
	list := fairy.Col().Gap(6).Pad(8).Radius(6).Fill(t.Muted.Alpha(0.5)).Font(monoFont).FontSize(12)
	for _, log := range slices.Backward(logs) {
		match := toolLogPattern.FindStringSubmatch(log)
		if match == nil {
			list.Child(fairy.Text(log).LineHeight(16).Wrap())
			continue
		}
		ink := t.MutedForeground
		message := fairy.Text(match[3]).LineHeight(16).Wrap().Grow(1).MinW(0)
		if match[2] == "ERROR" {
			ink = t.Destructive
			message.Color(t.Destructive)
		}
		list.Child(fairy.Row(
			fairy.Text(match[1]).LineHeight(16).Color(t.MutedForeground).Shrink(0),
			fairy.Text(match[2]).LineHeight(16).Medium().Color(ink).W(40).Shrink(0),
			message,
		).Gap(8).Wrap())
	}
	return list
}
