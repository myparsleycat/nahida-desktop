package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/hunting"
	"nahida.live/desktop/internal/platform"
)

const (
	agentSidebarWidth float32 = 256
	agentChatWidth    float32 = 880
	agentImageSize            = 512
)

var (
	agentAmber       = fairy.Hex(0xf59e0b)
	agentAmberStrong = fairy.Hex(0xd97706)
	agentViolet      = fairy.Hex(0xa78bfa)

	agentMotion100 = motion.Bezier(0.1, 0.4, 0, 0.2, 1)
	agentMotion120 = motion.Bezier(0.12, 0.4, 0, 0.2, 1)
)

var huntingCategoryLabels = map[hunting.Category]string{
	hunting.CategoryIndexBuffer:    "IB",
	hunting.CategoryVertexBuffer:   "VB",
	hunting.CategoryVertexShader:   "VS",
	hunting.CategoryPixelShader:    "PS",
	hunting.CategoryComputeShader:  "CS",
	hunting.CategoryGeometryShader: "GS",
	hunting.CategoryDomainShader:   "DS",
	hunting.CategoryHullShader:     "HS",
}

func (s *Shell) agentPage(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	a := &s.agent
	if !a.active {
		s.enterAgent()
	}
	s.showAgentDialogs(cx)
	width, height := cx.Size()
	return fairy.Row(
		s.agentSidebar(),
		fairy.Col().
			Child(s.agentHeader(width), s.agentChat(width, height)).
			Grow(1).
			MinW(0).
			HFull().
			Fill(t.Background.Alpha(0.7)),
	).WFull().HFull()
}

func (s *Shell) agentSidebar() *fairy.El {
	t := kit.T()
	a := &s.agent
	list := fairy.Col().Gap(12).PadEdges(2, 8, 18, 4).Role(fairy.RoleList).Label(T("page.agent.recent"))
	for _, group := range agentSessionGroups(a.sessions) {
		section := fairy.Col(
			fairy.Row(fairy.Icon(lucide.Folder).Size(15, 15).Shrink(0), fairy.Text(group.label).FontSize(12).LineHeight(16).NoWrap().Ellipsis().MinW(0)).
				H(32).
				PadXY(8, 0).
				Gap(6).
				Align(fairy.Center).
				FontSize(12).
				Medium().
				Color(t.MutedForeground),
		).Gap(2)
		for _, session := range group.sessions {
			section.Child(s.agentSessionRow(session))
		}
		list.Child(section)
	}
	return fairy.Col(
		fairy.Row(
			fairy.Icon(lucide.Sparkles).Size(22, 22).Color(t.Accent).Shrink(0),
			fairy.Text(T("page.agent.title")).FontSize(17).LetterSpacing(-0.17).SemiBold().NoWrap().Ellipsis().MinW(0),
		).H(60).Shrink(0).PadXY(4, 0).Gap(9).Align(fairy.Center).MarginEdges(0, 0, 8, 0),
		kit.Button(T("page.agent.new_chat"), s.createAgentSession, kit.Outline, kit.Icon(lucide.MessageSquarePlus)).
			AlignSelf(fairy.Stretch).
			H(38).
			Radius(12).Fill(t.Background).Border(1, t.Border).HoverFill(t.Muted).Shrink(0).MarginEdges(0, 2, 12, 2),
		fairy.Col(list).Key("agent.sessions").Grow(1).MinH(0).WFull().ScrollY(),
	).W(agentSidebarWidth).Shrink(0).HFull().PadXY(12, 6).BorderEdges(0, 1, 0, 0, opacity(t.Border, 0.78)).
		Fill(t.Background.Alpha(0.5))
}

func (s *Shell) agentSessionRow(session agent.AgentSessionSummary) *fairy.El {
	t := kit.T()
	a := &s.agent
	selected := a.snapshot != nil && a.snapshot.Summary.ID == session.ID
	hovered := fairy.Local[bool]("agent.session.hover:" + session.ID)
	menuID, triggerKey := "agent.session.menu:"+session.ID, "agent.session.trigger:"+session.ID
	revealed := *hovered || kit.DropdownMenuOpen(menuID) || s.win.Focused(triggerKey)

	dot := fairy.Box().Size(16, 20).Shrink(0).Center()
	if session.Running {
		dot.Child(agentPulseDot())
	}
	when := fairy.Text(agentRelativeTime(session.UpdatedAt)).FontSize(11).Color(t.MutedForeground).Shrink(0)
	if revealed {
		when.Opacity(0)
	}
	trigger := fairy.Box(fairy.Icon(lucide.Ellipsis).Size(14, 14)).Size(24, 24).Center().Radius(6).
		Color(t.MutedForeground).
		HoverFill(hoverFill(t)).Cursor(fairy.CursorPointer).Label(T("page.agent.session_menu")).
		Key(triggerKey).Focusable()
	if !revealed {
		trigger.Opacity(0)
	}
	menu := kit.DropdownMenu(menuID, trigger, []kit.MenuItem{
		{Label: T("page.agent.rename_session"), Icon: lucide.Pencil, OnClick: func() { s.openAgentRename(session) }},
		{
			Label: T("page.agent.delete_session"), Icon: lucide.Trash, Danger: true,
			OnClick: func() { s.deleteAgentSession(session) },
		},
	}, kit.Width(176), kit.AlignEnd)

	row := fairy.Row(
		dot,
		fairy.Text(session.Title).
			FontSize(13).
			LineHeight(20).
			NoWrap().
			Ellipsis().
			Grow(1).
			MinW(0).
			MarginEdges(0, 6, 0, 4),
		when,
	).Key("agent.session:"+session.ID).H(34).WFull().PadEdges(0, 8, 0, 8).Align(fairy.Center).Radius(8).
		HoverFill(t.Foreground.Alpha(0.07)).Animate(agentMotion100).Cursor(fairy.CursorPointer).
		Tooltip(T("page.agent.rename_hint")).Selected(selected).Label(session.Title).
		OnClick(func() {
			s.openAgentSnapshot(session.ID)
			s.refreshAgentSessions()
		}).
		OnDoubleClick(func() { s.openAgentRename(session) })
	if selected || kit.DropdownMenuOpen(menuID) {
		row.Fill(t.Foreground.Alpha(0.07))
	}
	// The menu trigger stays in the layout while hidden, so an open menu keeps its anchor when the pointer leaves.
	return fairy.Box(row, menu.Absolute().Right(5).Top(5)).WFull().OnHover(func(in bool) { *hovered = in })
}

func (s *Shell) openAgentRename(session agent.AgentSessionSummary) {
	s.agent.rename = &agentRename{target: session, value: session.Title}
}

func agentPulseDot() *fairy.El {
	t := kit.T()
	return fairy.Box().Size(7, 7).Radius(4).Fill(t.Accent).Shadow(0, 0, 0, 3, t.Accent.Alpha(0.16)).Pulse(2.8, 0.45)
}

func (s *Shell) agentEmpty() bool {
	a := &s.agent
	return !a.loading && len(s.agentVisibleEntries()) == 0
}

func (s *Shell) agentHeader(windowWidth float32) *fairy.El {
	t := kit.T()
	a := &s.agent
	if s.agentEmpty() {
		return nil
	}
	title := fairy.Row().Grow(1).MinW(0).Gap(7).Align(fairy.Center).FontSize(14).Medium()
	if a.snapshot != nil {
		if scope := a.snapshot.Summary.Scope; scope.Type == "mod" && scope.ModName != "" {
			title.Child(
				fairy.Text(scope.ModName).
					FontSize(14).
					LineHeight(20).
					Weight(400).
					Color(t.MutedForeground).
					NoWrap().
					Ellipsis().
					MinW(0),
				fairy.Text("/").FontSize(14).LineHeight(20).Weight(400).Color(t.MutedForeground),
			)
		}
	}
	name := T("page.agent.title")
	if a.snapshot != nil {
		name = a.snapshot.Summary.Title
	}
	title.Child(fairy.Text(name).FontSize(14).LineHeight(20).NoWrap().Ellipsis().MinW(0))
	if a.runID != "" {
		title.Child(agentPulseDot().MarginEdges(0, 0, 0, 2))
	}

	roots := fairy.Row().Gap(6).Align(fairy.Center).Shrink(1).MinW(0).
		MaxW(max(windowWidth-navWidth-1-agentSidebarWidth-48, 0) * 0.42).Clip()
	if a.snapshot != nil && windowWidth > 980 {
		for _, root := range a.snapshot.Roots {
			icon := lucide.Folder
			if root.ReadOnly {
				icon = lucide.FolderLock
			}
			roots.Child(
				fairy.Row(fairy.Icon(icon).Size(12, 12).Shrink(0), fairy.Text(root.Name).NoWrap().Ellipsis().MinW(0)).
					H(25).MaxW(180).PadXY(8, 0).Gap(4).Align(fairy.Center).Radius(13).Fill(t.Muted).
					FontSize(11).Color(t.MutedForeground).Tooltip(root.Path),
			)
		}
	}
	shareLabel := T("page.agent.share")
	if windowWidth <= 1180 {
		shareLabel = ""
	}
	share := kit.Button(shareLabel, func() {
		if !s.agentShareBlocked() {
			s.agent.share = &agentShare{target: a.snapshot.Summary}
		}
	}, kit.Outline, kit.Small, kit.Icon(lucide.HeartHandshake), kit.Disabled(s.agentShareBlocked())).
		H(26).Radius(13).PadXY(10, 0).Gap(6).
		FontSize(11).Weight(400).Color(t.MutedForeground).Label(T("page.agent.share"))

	tab := fairy.Col(
		fairy.Text(T("page.agent.chat")).FontSize(13).LineHeight(16).Medium().Color(t.Accent).PadEdges(0, 0, 9, 0),
		fairy.Box().H(2).WFull().Radius(1).Fill(t.Accent),
	).MarginEdges(10, 0, 0, 8)
	heading := fairy.Row(title).H(30).Gap(16).Align(fairy.Center)
	if windowWidth > 980 {
		heading.Child(roots)
	}
	heading.Child(share.Shrink(0))
	return fairy.Col(
		heading,
		tab,
	).Shrink(0).MinH(76).PadEdges(10, 28, 0, 20).BorderEdges(0, 0, 1, 0, t.Border)
}

// agentVisibleEntries leaves out the stored events that have nothing to show, such as the end of a turn.
func (s *Shell) agentVisibleEntries() []agentEntry {
	all := s.agentEntries()
	visible := all[:0:0]
	for _, entry := range all {
		if entry.Approval != nil || entry.Role == "user" || entry.Role == "assistant" ||
			strings.HasPrefix(entry.Type, "tool/") || entry.Error != "" {
			visible = append(visible, entry)
		}
	}
	return visible
}

func (s *Shell) agentChat(windowWidth, windowHeight float32) *fairy.El {
	t := kit.T()
	a := &s.agent
	mainWidth := max(windowWidth-navWidth-1-agentSidebarWidth, 0)
	chatWidth := min(max(mainWidth*0.64, 680), 920)
	margin := float32(32)
	if windowWidth <= 980 {
		margin = 20
	}
	pane := fairy.Box().Key("agent.chat-pane").Grow(1).MinH(0).WFull().Clip()
	empty := s.agentEmpty()
	if empty {
		height := fairy.Local[float32]("agent.empty-pane-height")
		pane.OnLayout(func(_, h float32) {
			if *height != h {
				*height = h
				s.win.Post(func() {})
			}
		})
		paneHeight := *height
		if paneHeight == 0 {
			paneHeight = windowHeight
		}
		hero := fairy.Col(
			fairy.Row(
				fairy.Box(fairy.Icon(lucide.Sparkles).Size(30, 30).Color(t.Accent)).Size(34, 34).Center().Shrink(0),
				fairy.Text(T("page.agent.hero_title")).FontSize(26).LineHeight(32).LetterSpacing(-0.65).Medium(),
			).Gap(10).Align(fairy.Center),
			fairy.Text(T("page.agent.empty")).
				FontSize(13).
				LineHeight(20).
				Color(t.MutedForeground).
				TextAlign(fairy.TextCenter).
				MaxW(520),
		).WFull().MaxW(chatWidth).Gap(8).Align(fairy.Center).PadXY(24, 0)
		// The React hero asked for clamp(72px, 23%, 180px) of a column whose height was not definite, so the
		// percentage fell back to the content height and the hero always sat at the 72px minimum.
		pane.Child(fairy.Col(hero).Absolute().Left(margin).Right(margin).Top(72).
			Align(fairy.Center).PassThrough())
		if reason := s.agentUnavailableReason(); reason != "" {
			pane.Child(fairy.Col(agentNotice(reason).MaxW(chatWidth).WFull()).
				Absolute().Top(28).Left(margin).Right(margin).Align(fairy.Center).PassThrough())
		}
		return pane.Child(fairy.Col(s.agentComposer(true).MaxW(712)).Absolute().
			Top(min(max(paneHeight*0.42, 190), 350)).Left(24).Right(24).Align(fairy.Center).PassThrough())
	}

	type chatRow struct {
		key   string
		build func() *fairy.El
	}
	rows := make([]chatRow, 0)
	if reason := s.agentUnavailableReason(); reason != "" {
		rows = append(rows, chatRow{key: "unavailable", build: func() *fairy.El { return agentNotice(reason) }})
	}
	for i, entry := range s.agentVisibleEntries() {
		key := agentEntryKey(entry, i)
		rows = append(rows, chatRow{key: key, build: func() *fairy.El {
			el := s.agentEntryView(entry, key)
			if entry.Reverted {
				el.Opacity(0.45)
			}
			return el
		}})
	}
	if a.runID != "" && len(a.live) == 0 {
		rows = append(rows, chatRow{key: "thinking", build: func() *fairy.El {
			return fairy.Text(T("page.agent.thinking")).FontSize(14).LineHeight(20).Medium().Color(t.Accent).H(26).
				Shimmer(1.8, t.Accent.Mix(fairy.RGB(255, 255, 255), 0.65))
		}})
	}
	composerHeight := fairy.Local[float32]("agent.composer-height")
	height := *composerHeight
	if height == 0 {
		height = 158
	}
	list := fairy.VirtualListMeasured(len(rows), 80, func(i int) string { return rows[i].key }, func(i int) *fairy.El {
		row := fairy.Col(rows[i].build().WFull().MaxW(chatWidth)).WFull().Align(fairy.Center)
		if i > 0 {
			return row.PadEdges(18, margin, 0, margin)
		}
		return row.PadXY(margin, 0)
	}).Key(agentChatKey).HFull().MinH(0).WFull().PadEdges(28, 0, height+22, 0).StickToBottom()
	pane.Child(list)
	if a.loading {
		pane.Child(fairy.Box(kit.Spinner(20).Color(t.MutedForeground)).Absolute().Inset(0).Center().PassThrough())
	}
	composer := fairy.Col(
		// Leave the source ScrollArea's 10px gutter clear of the composer fade.
		fairy.Box().Absolute().Top(0).Left(0).Right(10).H(36).
			Gradient(180, t.Background.Alpha(0), t.Background).PassThrough().Role(fairy.RoleHidden),
		fairy.Box().
			Absolute().
			Top(36).
			Left(0).
			Right(10).
			Bottom(0).
			Fill(t.Background).
			PassThrough().
			Role(fairy.RoleHidden),
		s.agentComposer(false).MaxW(chatWidth+32),
	).Key("agent.composer-overlay").
		Absolute().Left(0).Right(0).Bottom(0).Align(fairy.Center).PadEdges(36, 16, 12, 16).PassThrough().
		OnLayout(func(_, h float32) {
			if *composerHeight != h {
				*composerHeight = h
				s.win.Post(func() {})
			}
		})
	return pane.Child(composer)
}

func (s *Shell) agentUnavailableReason() string {
	if s.agent.snapshot == nil {
		return ""
	}
	return s.agent.snapshot.UnavailableReason
}

func agentNotice(text string) *fairy.El {
	t := kit.T()
	return fairy.Text(text).
		FontSize(13).
		LineHeight(20).
		Color(t.Destructive).
		PadXY(12, 10).
		Radius(10).
		Fill(t.Destructive.Alpha(0.08)).
		Border(1, t.Destructive.Alpha(0.3)).
		Selectable()
}

// agentEntryKey names an entry for the measured list. Live and stored entries of one turn have distinct
// sequences, and the pending user message has its own turn id.
func agentEntryKey(entry agentEntry, index int) string {
	if entry.ToolCallID != "" && strings.HasPrefix(entry.Type, "tool/") {
		return "tool:" + entry.TurnID + ":" + entry.ToolCallID
	}
	return fmt.Sprint(entry.TurnID, ":", entry.Sequence, ":", entry.Type, ":", index)
}

func (s *Shell) agentEntryView(entry agentEntry, key string) *fairy.El {
	t := kit.T()
	switch {
	case entry.Approval != nil:
		return s.agentApprovalCard(*entry.Approval)
	case entry.Role == "user":
		return s.agentUserMessage(entry, key)
	case entry.Role == "assistant":
		col := fairy.Col().WFull().MinW(0).Clip()
		if entry.Reasoning != "" {
			col.Child(s.agentDisclosure(key+":reasoning", entry.reasoningStreaming, lucide.Brain,
				T("page.agent.reasoning"), entry.reasoningStreaming,
				fairy.Text(entry.Reasoning).FontSize(13).LineHeight(20).Color(t.MutedForeground).WFull().Selectable()))
		}
		if entry.Text != "" {
			markdown := kit.Markdown(entry.Text, kit.MarkdownOptions{
				OnLink:     s.openAgentLink,
				Images:     s.agentMarkdownImage,
				Selectable: true,
				Streaming:  entry.streaming,
				Typography: &kit.MarkdownTypography{
					FontSize:   14,
					LineHeight: 22.75,
					Headings: [6]kit.MarkdownHeading{
						{FontSize: 20, LineHeight: 28, MarginTop: 24, MarginBottom: 12},
						{FontSize: 18, LineHeight: 25.2, MarginTop: 24, MarginBottom: 12},
						{FontSize: 16, LineHeight: 22.4, MarginTop: 24, MarginBottom: 12},
						{FontSize: 15, LineHeight: 21, MarginTop: 20, MarginBottom: 10, Muted: true},
						{FontSize: 15, LineHeight: 21, MarginTop: 20, MarginBottom: 10, Muted: true},
						{FontSize: 15, LineHeight: 21, MarginTop: 20, MarginBottom: 10, Muted: true},
					},
					BlockMargin:          12,
					ListIndent:           22,
					ListBlockMargin:      4,
					QuoteBorder:          2,
					QuotePadding:         12,
					InlineCodeScale:      0.875,
					InlineCodeBackground: t.Foreground.Alpha(0.08),
					CodeFontSize:         13,
					CodeLineHeight:       20.8,
					CodePadding:          12,
					CodeRadius:           8,
					CodeBackground:       t.Muted,
					PlainCode:            true,
					WrapCode:             true,
					TableCellPadding:     [2]float32{12, 8},
					RulesOnlyTable:       true,
					ImageRadius:          8,
				},
			}).WFull().MinW(0)
			if entry.Reasoning != "" {
				markdown.MarginEdges(10, 0, 0, 0)
			}
			col.Child(markdown)
		}
		if entry.streaming {
			col.Child(fairy.Box().Size(4, 16).Fill(t.Accent).MarginEdges(0, 0, 0, 4).Pulse(2, 0.5))
		}
		return col
	case strings.HasPrefix(entry.Type, "tool/"):
		return s.agentToolEntry(entry, key)
	}
	return agentNotice(entry.Error)
}

func (s *Shell) agentUserMessage(entry agentEntry, key string) *fairy.El {
	t := kit.T()
	bubble := fairy.Col().Gap(8).MaxW(640).MinW(0).PadXY(16, 10).Radius(22).Fill(t.Muted).FontSize(14).
		LineHeight(22)
	if images := s.agentImages(entry.Images, ""); images != nil {
		bubble.Child(images)
	}
	if entry.Text != "" {
		bubble.Child(fairy.Text(entry.Text).Selectable())
	}
	copyIcon, copyLabel := lucide.Copy, T("page.agent.copy_message")
	if s.agent.copied == key {
		copyIcon, copyLabel = lucide.Check, T("page.agent.copied")
	}
	revertDisabled := s.agent.runID != "" || s.agentPending() || s.agent.reverting
	actions := fairy.Row(
		agentIconButton(lucide.Undo2, T("page.agent.revert_message"), revertDisabled, func() { s.revertAgent(entry) }),
		agentIconButton(copyIcon, copyLabel, entry.Text == "", func() { s.copyAgentText(key, entry.Text) }),
	).Gap(4).PadXY(4, 0).Justify(fairy.JustifyEnd)
	return fairy.Col(fairy.Col(bubble, actions).Gap(4).Align(fairy.End).MinW(176)).WFull().Align(fairy.End)
}

func agentIconButton(icon *svg.Vector, label string, disabled bool, onClick func()) *fairy.El {
	return kit.IconButton(icon, onClick, kit.Ghost, kit.Small, kit.Tooltip(label), kit.Disabled(disabled)).
		Label(label)
}

// agentDisclosure is a collapsible block; it follows open until the user toggles it.
func (s *Shell) agentDisclosure(key string, open bool, icon *svg.Vector, title string, busy bool,
	content *fairy.El,
) *fairy.El {
	t := kit.T()
	if toggled, ok := s.agent.expanded[key]; ok {
		open = toggled
	}
	lead := fairy.Icon(icon).Size(14, 14)
	if busy {
		lead = fairy.Spinner().Size(14, 14)
	}
	turn := float32(0)
	if open {
		turn = math.Pi
	}
	chevron := fairy.Icon(lucide.ChevronDown).Size(14, 14).Rotate(turn).ScaleMotion(agentMotion120)
	summary := fairy.Row(lead, fairy.Text(title), chevron).H(26).Gap(7).Align(fairy.Center).AlignSelf(fairy.Start).
		Radius(6).
		Color(t.MutedForeground).HoverColor(t.Foreground).Cursor(fairy.CursorPointer).Expanded(open).Label(title).
		OnClick(func() {
			if s.agent.expanded == nil {
				s.agent.expanded = map[string]bool{}
			}
			s.agent.expanded[key] = !open
		})
	col := fairy.Col(summary).Gap(4).WFull().MinW(0).FontSize(13)
	if open {
		col.Child(fairy.Col(content).WFull().MinW(0).PadEdges(2, 0, 2, 21))
	}
	return col
}

func (s *Shell) agentToolEntry(entry agentEntry, key string) *fairy.El {
	t := kit.T()
	running := entry.Type == "tool/start"
	body := fairy.Col().Gap(6).WFull().MinW(0).Selectable()
	if entry.Error != "" {
		body.Child(fairy.Text(entry.Error).Color(t.Destructive))
	}
	if len(entry.ChangedFiles) > 0 {
		files := fairy.Col().Gap(2).PadEdges(4, 0, 4, 18)
		for _, file := range entry.ChangedFiles {
			files.Child(fairy.Text("• " + file).Color(t.MutedForeground))
		}
		body.Child(files)
	}
	if images := s.agentImages(entry.Images, entry.ToolName); images != nil {
		body.Child(images)
	}
	if len(entry.Result) > 0 {
		body.Child(agentCodeBlock(agentPrettyJSON(entry.Result), 256, true))
	}
	return s.agentDisclosure(key, running, lucide.Wrench, entry.ToolName, running, body)
}

func agentCodeBlock(text string, maxH float32, wrap bool) *fairy.El {
	t := kit.T()
	code := fairy.Text(text).Font(t.MonoFont).FontSize(11).LineHeight(17).Color(t.Foreground)
	if !wrap {
		code.NoWrap()
	}
	return fairy.Col(code).WFull().MaxH(maxH).ScrollY().PadXY(12, 10).Radius(8).Fill(t.CodeBackground)
}

func agentPrettyJSON(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(data)
}

// agentImages shows stored or pending images of an entry, or nil when it has none.
func (s *Shell) agentImages(images []agent.AgentImage, alt string) *fairy.El {
	if len(images) == 0 {
		return nil
	}
	t := kit.T()
	row := fairy.Row().Wrap().Gap(8)
	for _, image := range images {
		label := image.Name
		if label == "" {
			label = alt
		}
		if img := s.agentImage(image); img != nil {
			w, h := img.Size()
			scale := min(1, 256/float32(max(h, 1)))
			row.Child(fairy.ImageEl(img).W(float32(w)*scale).H(float32(h)*scale).MaxW(480).Fit(fairy.FitContain).
				Radius(10).Border(1, t.Border).Label(label))
			continue
		}
		row.Child(fairy.Box(fairy.Icon(lucide.Image).Size(20, 20).Color(textDisabled(t))).Size(96, 64).Center().
			Radius(10).Border(1, t.Border).Label(label))
	}
	return row
}

func (s *Shell) agentImage(image agent.AgentImage) *fairy.Image {
	if id, ok := strings.CutPrefix(image.Src, agentDraftPrefix); ok {
		data := s.agent.sentImages[id]
		return fairy.Images.Load(image.Src, agentImageSize, agentImageSize, func() ([]byte, error) {
			if data == nil {
				return nil, os.ErrNotExist
			}
			return data, nil
		})
	}
	src := image.Src
	if src == "" {
		return nil
	}
	return fairy.Images.Load("agent:"+src, agentImageSize, agentImageSize, func() ([]byte, error) {
		return os.ReadFile(src)
	})
}

const agentDraftPrefix = "agent.draft:"

func agentDraftSrc(id string) string { return agentDraftPrefix + id }

func (s *Shell) agentApprovalCard(approval agent.AgentApproval) *fairy.El {
	t := kit.T()
	busy := s.agent.deciding == approval.ID
	var args struct {
		Script   *string `json:"script"`
		Language string  `json:"language"`
	}
	_ = json.Unmarshal(approval.Arguments, &args)
	script := approval.ActionID == "script.run"
	scriptText := ""
	if args.Script != nil {
		scriptText = *args.Script
	}

	summary, impact := approval.Summary, approval.Impact
	if script {
		summary = T("page.agent.approval_script_python")
		if args.Language == "powershell" {
			summary = T("page.agent.approval_script_powershell")
		}
		impact = T("page.agent.approval_script_impact")
	}
	card := fairy.Col(
		fairy.Row(
			fairy.Icon(lucide.ShieldAlert).Size(16, 16).Color(agentAmberStrong),
			fairy.Text(T("page.agent.approval_title")).SemiBold(),
			fairy.Spacer(),
			fairy.Text(strings.ToUpper(T("page.agent.approval_status_"+approval.Status))).FontSize(10).Medium().
				Color(t.MutedForeground).PadXY(7, 2).Radius(10).Fill(t.Muted),
		).Gap(8).Align(fairy.Center),
		fairy.Text(summary).MarginEdges(4, 0, 0, 0),
	).Gap(7).WFull().Pad(16).Radius(14).Fill(agentAmber.Alpha(0.06)).Border(1, agentAmber.Alpha(0.42)).
		FontSize(13)
	if approval.Target != "" {
		card.Child(fairy.RichText(
			fairy.Span{Text: T("page.agent.approval_target") + ": ", Weight: 700, Color: t.Foreground},
			fairy.Span{Text: approval.Target},
		).FontSize(12).Color(t.MutedForeground))
	}
	card.Child(fairy.Text(impact).FontSize(12).Color(t.MutedForeground))
	if script {
		card.Child(agentCodeBlock(scriptText, 288, false).Fill(t.Background).Border(1, t.Border).
			Label(T("page.agent.approval_script")).MarginEdges(3, 0, 0, 0))
	}
	if approval.Error != "" {
		card.Child(fairy.Text(approval.Error).Color(t.Destructive))
	}
	if approval.Status == "pending" {
		buttons := fairy.Row(fairy.Spacer(), kit.Button(T("page.agent.reject"), func() {
			s.decideAgentApproval(approval, false, false)
		}, kit.Outline, kit.Small, kit.Icon(lucide.X), kit.Disabled(busy))).Gap(8).MarginEdges(7, 0, 0, 0)
		if script {
			buttons.Child(kit.Button(T("page.agent.approve_session"), func() {
				s.decideAgentApproval(approval, true, true)
			}, kit.Outline, kit.Small, kit.Disabled(busy)))
		}
		buttons.Child(kit.Button(T("page.agent.approve"), func() { s.decideAgentApproval(approval, true, false) },
			kit.Small, kit.Icon(lucide.Check), kit.Disabled(busy), kit.Loading(busy)))
		card.Child(buttons)
	}
	return card
}

func (s *Shell) agentComposer(empty bool) *fairy.El {
	t := kit.T()
	a := &s.agent
	snapshot := a.snapshot
	disabled := s.agentPending() || a.reverting || s.agentUnavailableReason() != ""
	canAttach := snapshot != nil && snapshot.SupportsImages

	panel := fairy.Col().Key("agent.composer").WFull().Gap(9).PadEdges(8, 8, 7, 8).Radius(22).
		Fill(t.Card.Mix(t.Muted, 0.04)).Border(1, t.Border).FocusBorder(t.Border.Mix(t.Accent, 0.45)).FocusWithin().
		Shadow(0, 8, 30, 0, shadowColor(t).Alpha(0.08)).Animate(agentMotion120)
	if canAttach {
		panel.OnFileDrop(func(drop fairy.FileDrop) { s.addAgentImages(drop.Paths) }).DropBorder(t.Accent)
	}
	if snapshot != nil && snapshot.Hunting != nil {
		panel.Child(s.huntingControls(*snapshot.Hunting))
	}
	if snapshot != nil && snapshot.Revert != nil {
		panel.Child(fairy.Row(
			fairy.Col(
				fairy.Text(T("page.agent.revert_pending", "count", snapshot.Revert.RevertedCount)).
					FontSize(12).
					Medium().
					NoWrap().
					Ellipsis(),
				fairy.Text(T("page.agent.revert_files_notice")).
					FontSize(11).
					Color(t.MutedForeground).
					NoWrap().
					Ellipsis(),
			).Grow(1).MinW(0),
			kit.Button(T("page.agent.revert_undo"), s.unrevertAgent, kit.Outline, kit.Small,
				kit.Disabled(a.reverting)),
		).Gap(12).Align(fairy.Center).PadXY(12, 8).Radius(14).Fill(t.Background))
	}
	if len(a.images) > 0 {
		thumbs := fairy.Row().Wrap().Gap(8).PadEdges(5, 7, 0, 7)
		for i, image := range a.images {
			data := image.data
			img := fairy.Images.Load(agentDraftSrc(image.id), 128, 128, func() ([]byte, error) { return data, nil })
			thumb := fairy.Box().Size(64, 64).Radius(10).Border(1, t.Border).Clip().Label(image.name)
			if img != nil {
				thumb.Child(fairy.ImageEl(img).Size(64, 64).Fit(fairy.FitCover))
			}
			index := i
			remove := fairy.Box(fairy.Icon(lucide.X).Size(12, 12)).Size(20, 20).Center().Radius(10).Fill(t.Background).
				Border(1, t.Border).Color(t.MutedForeground).HoverColor(t.Foreground).Animate(agentMotion100).
				Cursor(fairy.CursorPointer).Label(T("page.agent.remove_image")).OnClick(func() {
				if index < len(a.images) {
					a.images = append(a.images[:index:index], a.images[index+1:]...)
				}
			})
			thumbs.Child(fairy.Box(thumb, remove.Absolute().Top(-6).Right(-6)))
		}
		panel.Child(thumbs)
	}

	// Enter sends and Shift+Enter inserts a newline. Enter is taken even when the message cannot be sent, as in
	// the React composer.
	onChange := func(text string) { a.draft = text }
	if disabled {
		onChange = nil
	}
	input := kit.TextArea(a.draft, onChange,
		kit.Placeholder(T("page.agent.placeholder")), kit.Label(T("page.agent.placeholder")), kit.Disabled(disabled),
		kit.OnSubmit(func(string) { s.sendAgent() }),
		kit.OnPaste(func() bool { return canAttach && s.pasteAgentImage() }),
	).Key("agent.draft").MinH(52).MaxH(260).WFull().PadEdges(5, 7, 0, 7).Fill(fairy.Color{}).Border(0, fairy.Color{}).
		FocusBorder(fairy.Color{}).FontSize(14).LineHeight(24)
	panel.Child(input)

	left := fairy.Row().Gap(4).Align(fairy.Center).Grow(1).MinW(0)
	if canAttach {
		hint := T("page.agent.attach_hint", "count", agentMaxImages, "limit", agentMaxImageBytes>>20)
		left.Child(kit.IconButton(lucide.Paperclip, s.pickAgentImages, kit.Ghost, kit.Small, kit.Tooltip(hint),
			kit.Disabled(disabled)).Label(T("page.agent.attach_image")).Color(t.MutedForeground))
	}
	scope := T("page.agent.scope_global")
	if snapshot != nil && snapshot.Summary.Scope.Type == "mod" && snapshot.Summary.Scope.ModName != "" {
		scope = snapshot.Summary.Scope.ModName
	}
	left.Child(
		fairy.Row(fairy.Icon(lucide.Folder).Size(14, 14).Shrink(0), fairy.Text(scope).FontSize(12).LineHeight(16).NoWrap().Ellipsis().MinW(0)).
			H(28).
			MinW(0).
			PadXY(8, 0).
			Gap(5).
			Align(fairy.Center).
			FontSize(12).
			Medium().
			Color(t.MutedForeground),
	)

	right := fairy.Row().Gap(4).Align(fairy.Center).Shrink(0)
	if !empty {
		if meter := s.agentContextMeter(); meter != nil {
			right.Child(meter)
		}
	}
	round := func(icon *svg.Vector, label string, enabled bool, onClick func()) *fairy.El {
		b := fairy.Box(fairy.Icon(icon).Size(17, 17)).Size(34, 34).Center().Radius(17).Fill(t.Accent).
			Color(fairy.Hex(0xffffff)).Label(label).Tooltip(label).Animate(agentMotion100)
		if !enabled {
			return b.Opacity(0.35).Isolate().Disabled(true)
		}
		return b.HoverFill(accentHover(t)).Cursor(fairy.CursorPointer).OnClick(onClick)
	}
	if a.runID != "" {
		right.Child(round(lucide.Square, T("page.agent.stop"), true, s.cancelAgentRun))
	} else {
		right.Child(round(lucide.ArrowUp, T("page.agent.send"), s.agentCanSend(), s.sendAgent))
	}
	panel.Child(fairy.Row(left, right).Gap(12).Align(fairy.Center))
	return panel
}

func (s *Shell) huntingControls(h hunting.Snapshot) *fairy.El {
	t := kit.T()
	a := &s.agent
	busy := a.huntingBusy
	manual := h.Status == hunting.StatusManual
	cleanupFailed := !manual && !h.Cleanup.Complete
	retryable := cleanupFailed && h.Cleanup.Pending
	steps := func(category hunting.Category) int {
		for _, state := range h.Categories {
			if state.Category == category {
				return state.Steps
			}
		}
		return 0
	}
	label := func(category hunting.Category) string {
		if text, ok := huntingCategoryLabels[category]; ok {
			return text
		}
		return string(category)
	}

	description := T("page.agent.hunting_description")
	switch {
	case manual:
	case retryable:
		description = T("page.agent.hunting_cleanup_failed")
	case cleanupFailed:
		description = T("page.agent.hunting_cleanup_abandoned")
	default:
		description = T("page.agent.hunting_status_" + string(h.Status))
	}
	card := fairy.Col(fairy.Row(
		fairy.Box(fairy.Icon(lucide.Gamepad2).Size(16, 16)).Size(28, 28).Shrink(0).Center().Radius(9).
			Fill(t.Accent.Alpha(0.12)).Color(t.Accent).MarginEdges(2, 0, 0, 0),
		fairy.Col(
			fairy.Row(
				fairy.Text(T("page.agent.hunting_title")).FontSize(12).SemiBold(),
				fairy.Text(h.ImporterKey+" · "+h.WindowTitle).FontSize(10).Color(t.MutedForeground).NoWrap().Ellipsis().
					MinW(0),
			).Gap(8).Align(fairy.Center),
			fairy.Text(description).FontSize(11).LineHeight(16).Color(t.MutedForeground),
		).Gap(2).Grow(1).MinW(0),
	).Gap(10).Align(fairy.Start)).WFull().PadXY(12, 10).Radius(14).Fill(t.Accent.Alpha(0.05)).
		Border(1, t.Accent.Alpha(0.3))

	if manual {
		buttons := fairy.Row().Wrap().Gap(6).MarginEdges(10, 0, 0, 0)
		for _, category := range h.AvailableCategories {
			name := T("page.agent.hunting_category_" + string(category))
			content := fairy.Text(label(category))
			if busy == string(category) {
				content = fairy.Spinner().Size(14, 14)
			}
			b := fairy.Box(content).H(32).MinW(40).PadXY(10, 0).Center().Radius(9).FontSize(12).SemiBold().
				Label(name).Tooltip(name).Animate(agentMotion100)
			if h.SelectedCategory == category {
				b.Fill(t.Accent).Border(1, t.Accent).Color(t.AccentForeground)
			} else {
				b.Fill(t.Background).
					Border(1, t.Border).
					HoverBorder(t.Accent.Alpha(0.55)).
					HoverFill(t.Accent.Alpha(0.07))
			}
			if busy != "" {
				b.Opacity(0.45).Disabled(true)
			} else {
				b.Cursor(fairy.CursorPointer).OnClick(func() { s.huntingNext(category) })
			}
			if n := steps(category); n > 0 {
				b = fairy.Box(b, fairy.Text(itoa(n)).FontSize(9).LineHeight(16).MinW(16).TextAlign(fairy.TextCenter).
					PadXY(4, 0).Radius(8).Fill(t.Foreground).Color(t.Background).Absolute().Top(-6).Right(-6))
			}
			buttons.Child(b)
		}
		selected := T("page.agent.hunting_select_category")
		if h.SelectedCategory != "" {
			selected = T("page.agent.hunting_selected", "category", label(h.SelectedCategory),
				"steps", steps(h.SelectedCategory))
		}
		card.Child(buttons, fairy.Row(
			fairy.Text(selected).FontSize(11).Color(t.MutedForeground).NoWrap().Ellipsis().Grow(1).MinW(0),
			kit.Button(T("page.agent.hunting_found"), s.huntingFound, kit.Small, kit.Icon(lucide.Hash),
				kit.Loading(busy == "found"), kit.Disabled(busy != "" || h.SelectedCategory == "")),
			kit.Button(T("page.agent.hunting_cancel"), s.huntingCancel, kit.Outline, kit.Small,
				kit.Loading(busy == "cancel"), kit.Disabled(busy != "")),
		).Gap(8).Align(fairy.Center).MarginEdges(10, 0, 0, 0))
	}
	if h.Status == hunting.StatusCompleted && h.Hash != "" {
		copyIcon, copyLabel := lucide.Copy, T("page.agent.hunting_copy")
		if a.copied == "hunting" {
			copyIcon, copyLabel = lucide.Check, T("page.agent.copied")
		}
		card.Child(fairy.Row(
			fairy.Icon(lucide.Hash).Size(14, 14).Color(t.Accent),
			fairy.Text(label(h.SelectedCategory)).FontSize(11).Medium().Color(t.MutedForeground),
			fairy.Text(h.Hash).Font(t.MonoFont).FontSize(12).NoWrap().Ellipsis().Grow(1).MinW(0),
			agentIconButton(copyIcon, copyLabel, false, func() { s.copyAgentText("hunting", h.Hash) }),
		).Gap(8).Align(fairy.Center).PadXY(10, 6).Radius(9).Fill(t.Background).Border(1, t.Border).
			MarginEdges(10, 0, 0, 0))
	}
	if retryable {
		card.Child(fairy.Row(fairy.Spacer(), kit.Button(T("page.agent.hunting_retry_cleanup"), s.huntingCancel,
			kit.Outline, kit.Small, kit.Loading(busy == "cancel"), kit.Disabled(busy != ""),
			kit.Tooltip(h.Cleanup.Error))).MarginEdges(10, 0, 0, 0))
	}
	return card
}

// agentContextMeter is the composer's context occupancy reading, with its composition in a popover. It shows
// nothing until the usage and the context window are known.
func (s *Shell) agentContextMeter() *fairy.El {
	t := kit.T()
	a := &s.agent
	usage := (*agent.AgentContextUsage)(nil)
	if a.snapshot != nil {
		usage = a.snapshot.ContextUsage
		if a.usage != nil && a.usageSession == a.snapshot.Summary.ID {
			usage = a.usage
		}
	}
	percent, used, ok := agentContextOccupancy(usage)
	if !ok {
		return nil
	}
	reading := itoa(percent) + "%"
	label := T("page.agent.context_used", "percent", reading)
	meter := fairy.Row(
		kit.ProgressCircle(float64(percent)/100, 14).Color(t.Accent).Role(fairy.RoleHidden),
		fairy.Text(reading),
	).H(26).PadXY(8, 0).Gap(6).Align(fairy.Center).Radius(13).FontSize(12).Medium().Color(t.MutedForeground).
		HoverFill(t.Foreground.Alpha(0.07)).HoverColor(t.Foreground).Animate(agentMotion100).
		Cursor(fairy.CursorPointer).Label(label).Expanded(a.meterOpen).OnClick(func() { a.meterOpen = !a.meterOpen })
	if !a.meterOpen {
		return meter
	}

	type part struct {
		label  string
		tokens int
		tint   fairy.Color
	}
	parts := []part{
		{T("page.agent.context_system"), usage.SystemTokens, t.MutedForeground.Alpha(0.4)},
		{T("page.agent.context_tools"), usage.ToolsTokens, agentViolet},
		{T("page.agent.context_messages"), usage.MessageTokens, t.Accent},
	}
	total := usage.SystemTokens + usage.ToolsTokens + usage.MessageTokens
	bar := fairy.Row().H(4).WFull().Gap(1).Radius(2).Fill(t.Muted).Clip()
	const barWidth float32 = 232
	if total == 0 {
		bar.Child(fairy.Box().HFull().W(barWidth * float32(percent) / 100).Fill(t.MutedForeground.Alpha(0.4)))
	} else {
		for _, p := range parts {
			if width := barWidth * float32(percent) / 100 * float32(p.tokens) / float32(total); width > 0 {
				bar.Child(fairy.Box().HFull().W(max(width, 2)).Shrink(0).Radius(1).Fill(p.tint))
			}
		}
	}
	legend := fairy.Col()
	for _, p := range parts {
		legend.Child(fairy.Row(
			fairy.Box().Size(8, 8).Radius(2).Fill(p.tint),
			fairy.Text(p.label).Color(t.MutedForeground).Grow(1),
			fairy.Text("~"+agentFormatTokens(int64(p.tokens))),
		).Gap(6).Align(fairy.Center).PadXY(0, 2))
	}
	popover := fairy.Col(
		fairy.Row(
			fairy.Text(label).Color(t.MutedForeground).Grow(1).MinW(0),
			fairy.Text("~"+agentFormatTokens(used)+" / "+agentFormatTokens(int64(usage.ContextWindow))).Medium(),
		).Gap(6).Align(fairy.Center),
		bar.MarginEdges(10, 0, 10, 0),
		legend,
	).W(256).Pad(12).Radius(12).Fill(t.Card).Border(1, t.Border).FontSize(12).LineHeight(20).
		Shadow(0, 8, 24, 0, shadowColor(t)).Label(label)
	return meter.Popover(popover, fairy.PopoverOptions{
		Side: fairy.SideTop, Align: fairy.AlignEnd, Gap: 8, OnDismiss: func() { a.meterOpen = false },
	})
}

// agentFormatTokens writes a token count compactly in the UI language: 128K in English, 12.8만 in Korean.
func agentFormatTokens(value int64) string {
	if value <= 0 {
		return "0"
	}
	type unit struct {
		size   float64
		suffix string
	}
	var units []unit
	switch lang := catalog.Language(); {
	case strings.HasPrefix(lang, "ko"):
		units = []unit{{1e8, "억"}, {1e4, "만"}, {1e3, "천"}}
	case strings.HasPrefix(lang, "ja"):
		units = []unit{{1e8, "億"}, {1e4, "万"}}
	case strings.HasPrefix(lang, "zh"):
		units = []unit{{1e8, "亿"}, {1e4, "万"}}
	default:
		units = []unit{{1e9, "B"}, {1e6, "M"}, {1e3, "K"}}
	}
	for _, u := range units {
		if float64(value) >= u.size {
			scaled := math.Round(float64(value)/u.size*10) / 10
			return strings.TrimSuffix(fmt.Sprintf("%.1f", scaled), ".0") + u.suffix
		}
	}
	return fmt.Sprint(value)
}

func (s *Shell) showAgentDialogs(cx *fairy.Cx) {
	a := &s.agent
	if r := a.rename; r != nil {
		closeDialog := func() {
			if a.rename == r && !r.busy {
				a.rename = nil
			}
		}
		field := kit.TextField(r.value, func(value string) { r.value = string([]rune(value)[:min(len([]rune(value)), 80)]) },
			kit.Placeholder(T("page.agent.rename_placeholder")), kit.Disabled(r.busy)).
			Key("agent.rename.value").
			Label(T("page.agent.rename_placeholder")).
			AutoFocus().
			OnSubmit(func(string) { s.submitAgentRename() })
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T("page.agent.rename_title"),
			Description: T("page.agent.rename_description"),
			Body:        field,
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), closeDialog, kit.Outline, kit.Disabled(r.busy)),
				kit.Button(T("page.agent.rename_confirm"), s.submitAgentRename,
					kit.Disabled(r.busy || strings.TrimSpace(r.value) == ""), kit.Loading(r.busy)),
			},
			OnClose: closeDialog,
			Key:     "agent.rename",
		})
	}
	if sh := a.share; sh != nil {
		s.showAgentShare(cx, sh)
	}
}

func (s *Shell) showAgentShare(cx *fairy.Cx, sh *agentShare) {
	t := kit.T()
	a := &s.agent
	closeDialog := func() {
		if a.share == sh && !sh.busy {
			a.share = nil
		}
	}
	points := fairy.Col().Gap(4).PadEdges(0, 0, 0, 8)
	for _, key := range []string{"share_point_messages", "share_point_images", "share_point_metadata"} {
		points.Child(fairy.Text("• " + T("page.agent."+key)))
	}
	body := fairy.Col(fairy.Text(T("page.agent.share_details")), points, fairy.Text(T("page.agent.share_withdraw"))).
		Gap(12).FontSize(13).LineHeight(20).Color(t.MutedForeground)
	confirm := kit.Button(T("page.agent.share_confirm"), s.confirmAgentShare,
		kit.Disabled(sh.busy || s.agentShareBlocked()), kit.Loading(sh.busy))
	if !s.signedIn() {
		body.Child(fairy.Text(T("page.agent.share_login_required")).Color(t.Destructive))
		confirm = kit.Button(T("page.agent.share_login"), s.startLogin, kit.Disabled(s.login.busy),
			kit.Loading(s.login.busy))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T("page.agent.share_title"),
		Description: T("page.agent.share_description"),
		Body:        body,
		Actions:     []*fairy.El{kit.Button(T("g.cancel"), closeDialog, kit.Outline, kit.Disabled(sh.busy)), confirm},
		OnClose:     closeDialog,
		Key:         "agent.share",
	})
}

type agentSessionGroup struct {
	label    string
	sessions []agent.AgentSessionSummary
}

// agentSessionGroups groups conversations by mod, with the global ones under the global label, in the order the
// backend lists them.
func agentSessionGroups(sessions []agent.AgentSessionSummary) []agentSessionGroup {
	var groups []agentSessionGroup
	index := map[string]int{}
	for _, session := range sessions {
		key, label := "global", T("page.agent.scope_global")
		if session.Scope.Type == "mod" && session.Scope.ModName != "" {
			key, label = "mod:"+session.Scope.ModPath, session.Scope.ModName
			if session.Scope.ModPath == "" {
				key = "mod:" + session.Scope.ModName
			}
		}
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, agentSessionGroup{label: label})
		}
		groups[i].sessions = append(groups[i].sessions, session)
	}
	return groups
}

func agentRelativeTime(value string) string {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	minutes := max(0, int(math.Round(time.Since(at).Minutes())))
	if minutes < 1 {
		return "now"
	}
	if minutes < 60 {
		return itoa(minutes) + "m"
	}
	hours := int(math.Round(float64(minutes) / 60))
	if hours < 24 {
		return itoa(hours) + "h"
	}
	return itoa(int(math.Round(float64(hours)/24))) + "d"
}

func agentImagePickerOptions() platform.OpenDialogOptions {
	return platform.OpenDialogOptions{
		Title: T("page.agent.attach_image"),
		Filters: []platform.FileFilter{
			{Name: T("page.agent.attach_image"), Extensions: []string{"png", "jpg", "jpeg", "webp", "gif"}},
		},
		Properties: []string{"openFile", "multiSelections"},
		Role:       "agent-image",
	}
}

func (s *Shell) agentMarkdownImage(src string) *fairy.Image {
	return s.gbImage(src, agentChatWidth, 0, fairy.FitContain)
}
