package ui

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
)

const (
	gbSidebarWidth  float32 = 320
	gbNarrowWidth   float32 = 1024
	gbWideWidth     float32 = 1536
	gbCardGap       float32 = 16
	gbContentPad    float32 = 16
	gbScrollbarRoom float32 = 10
)

// Icons the generated Lucide set lacks, from lucide (ISC licence).
const gbIconHead = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" ` +
	`stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`

var (
	gbLogOutIcon = svg.Lazy(gbIconHead +
		`<path d="m16 17 5-5-5-5"/><path d="M21 12H9"/><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/></svg>`)
	gbKeyIcon = svg.Lazy(gbIconHead + `<path d="M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 ` +
		`1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 ` +
		`0-4-4z"/><circle cx="16.5" cy="7.5" r=".5" fill="currentColor"/></svg>`)
)

func (s *Shell) gameBananaPage(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	width, _ := cx.Size()
	if width < gbNarrowWidth {
		return fairy.Col(fairy.Col(
			fairy.Text(T("page.gamebanana.narrow_window.title")).
				FontSize(16).
				LineHeight(24).
				Medium().
				TextAlign(fairy.TextCenter),
			fairy.Text(T("page.gamebanana.narrow_window.description")).
				FontSize(14).
				LineHeight(20).
				Color(t.MutedForeground).
				TextAlign(fairy.TextCenter),
		).MaxW(448).WFull().Pad(24).Gap(8).Align(fairy.Center).Radius(t.RadiusXl()).Border(1, t.Border).BorderDashed()).
			WFull().HFull().Center().Pad(16)
	}
	s.startGameBanana()
	s.saveGameBananaView()

	header := fairy.Col(s.gbToolbar()).WFull().Pad(8).BorderEdges(0, 0, 1, 0, t.Border).Shrink(0)
	if g.modURLOpen {
		header.Child(s.gbModURLForm())
	}

	showSidebar := s.gbShowSidebar()
	mainWidth := width - navWidth - 1
	if showSidebar {
		mainWidth -= gbSidebarWidth + 1
	}
	var main *fairy.El
	switch {
	case g.mod != nil:
		main = s.gbModPanel(mainWidth)
	case g.categoryID > 0:
		main = s.gbCategoryPanel(width, mainWidth, showSidebar)
	default:
		main = s.gbHomePanel(width, mainWidth)
	}
	body := fairy.Row(main.Grow(1).MinW(0).HFull()).Grow(1).MinH(0).WFull()
	if showSidebar {
		var sidebar *fairy.El
		if g.mod != nil {
			sidebar = s.gbFilesSidebar()
		} else {
			sidebar = s.gbCategorySidebar()
		}
		body.Child(sidebar.W(gbSidebarWidth).Shrink(0).HFull().BorderEdges(0, 0, 0, 1, t.Border))
	}

	s.gbDialogs(cx)
	return fairy.Col(header, body).WFull().HFull()
}

func (s *Shell) gbShowSidebar() bool {
	g := &s.gameBanana
	if g.categoryID == 0 || g.mod != nil || g.category.loading || g.category.err != nil {
		return true
	}
	return g.category.data != nil && len(g.category.data.Categories) > 0
}

func (s *Shell) gbGameLabel() string {
	g := &s.gameBanana
	if g.overview.data != nil && g.overview.data.Profile.Name != "" {
		return g.overview.data.Profile.Name
	}
	if g.game != "" {
		return strings.ToUpper(g.game)
	}
	return T("page.gamebanana.title")
}

// gbBreadcrumbs names the last category from its loaded profile, as the React route did.
func (s *Shell) gbBreadcrumbs() []gbCrumb {
	g := &s.gameBanana
	if g.categoryID == 0 {
		return nil
	}
	name := ""
	if g.category.data != nil {
		name = g.category.data.Profile.Name
	}
	if name == "" && g.overview.data != nil {
		for _, c := range g.overview.data.Profile.RootCategories {
			if int(c.ID) == g.categoryID {
				name = c.Name
			}
		}
	}
	if len(g.breadcrumbs) == 0 {
		if name == "" {
			return nil
		}
		return []gbCrumb{{g.categoryID, name}}
	}
	crumbs := append([]gbCrumb(nil), g.breadcrumbs...)
	if name != "" {
		crumbs[len(crumbs)-1].name = name
	}
	return crumbs
}

func (s *Shell) gbProfileURL() string {
	g := &s.gameBanana
	switch {
	case g.mod != nil && g.modView.data != nil && g.modView.data.Profile.ProfileURL != "":
		return g.modView.data.Profile.ProfileURL
	case g.categoryID > 0 && g.category.data != nil && g.category.data.Profile.ProfileURL != "":
		return g.category.data.Profile.ProfileURL
	case g.overview.data != nil:
		return g.overview.data.Profile.ProfileURL
	}
	return ""
}

func (s *Shell) gbGoBack() {
	g := &s.gameBanana
	if g.mod != nil {
		g.mod = nil
		return
	}
	if crumbs := s.gbBreadcrumbs(); len(crumbs) > 1 {
		s.gbSelectBreadcrumb(len(crumbs) - 2)
		return
	}
	s.gbResetToGameHome()
}

func (s *Shell) gbToolbar() *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	left := fairy.Row().Gap(12).Align(fairy.Center).Grow(1).MinW(0)
	if g.mod != nil || g.categoryID > 0 {
		left.Child(kit.Button(T("page.gamebanana.back"), s.gbGoBack, kit.Ghost, kit.Small,
			kit.Icon(lucide.ArrowLeft)).H(36).PadXY(8, 0))
	}

	games := s.deps.GameBanana.GetGameRegistry()
	items := make([]kit.MenuItem, 0, len(games))
	for _, game := range games {
		key := game.Key
		items = append(items, kit.MenuItem{
			Label: strings.ToUpper(key), Radio: true, Checked: key == g.game,
			OnClick: func() { s.gbSelectGame(key) },
		})
	}
	// The menubar frames its trigger with 3px of padding, and only the trigger takes the hover fill.
	trigger := fairy.Row(fairy.Row(
		fairy.Text(T("page.gamebanana.game_menu")).FontSize(12).LineHeight(16).Color(t.MutedForeground),
		fairy.Text(s.gbGameLabel()).FontSize(14).LineHeight(20).Medium().Ellipsis().MaxW(176),
	).HFull().PadXY(12, 2).Gap(8).Align(fairy.Center).Radius(t.RadiusSm()).HoverFill(t.Muted)).
		H(36).Pad(3).Radius(t.RadiusLg()).Border(1, t.Border).Cursor(fairy.CursorPointer).Shrink(0)
	left.Child(kit.DropdownMenu("gamebanana.game", trigger, items))
	left.Child(s.gbBreadcrumbBar())

	right := fairy.Row(fairy.Text(s.gbStageLabel()).FontSize(12).LineHeight(16).Color(t.MutedForeground).
		H(32).PadXY(10, 0).Center().Medium().Fill(t.Muted).Border(1, t.Border).
		Radii(t.RadiusLg(), 0, 0, t.RadiusLg())).Gap(0).Align(fairy.Center).Shrink(0).Role(fairy.RoleGroup)
	groupBorder := t.Border
	if t.Dark {
		groupBorder = t.Input
	}
	urlTurn := float32(0)
	if g.modURLOpen {
		urlTurn = math.Pi
	}
	// Buttons after the first drop their left border, as in the React ButtonGroup.
	right.Child(kit.Button("", s.gbToggleModURL, kit.Outline, kit.Icon(lucide.Link),
		kit.Tooltip(T("page.gamebanana.open_mod_url.button"))).
		Child(fairy.Icon(lucide.ChevronDown).Size(16, 16).Rotate(urlTurn).ScaleMotion(motion.Snappy)).
		Radius(0).BorderEdges(1, 1, 1, 0, groupBorder).
		Label(T("page.gamebanana.open_mod_url.button")).Expanded(g.modURLOpen))
	if link := s.gbProfileURL(); link != "" {
		right.Child(kit.Button("", func() { s.gbOpenExternal(link) }, kit.Outline, kit.Icon(lucide.ExternalLink),
			kit.Tooltip(T("page.gamebanana.open_profile"))).Label(T("page.gamebanana.open_profile")).
			Radius(0).BorderEdges(1, 1, 1, 0, groupBorder))
	}
	if status := g.session.data; status != nil && status.Authenticated {
		right.Child(fairy.Text(status.Username).FontSize(12).LineHeight(16).Ellipsis().MaxW(160).H(32).PadXY(10, 0).
			Center().Medium().Fill(t.Muted).BorderEdges(1, 1, 1, 0, t.Border))
		right.Child(kit.Button("", func() { g.logoutConfirm = true }, kit.Outline, kit.Icon(gbLogOutIcon),
			kit.Loading(g.loggingOut), kit.Disabled(g.loggingOut), kit.Tooltip(T("page.gamebanana.logout"))).
			Label(T("page.gamebanana.logout")).Radii(0, t.RadiusLg(), t.RadiusLg(), 0).
			BorderEdges(1, 1, 1, 0, groupBorder))
	} else {
		right.Child(kit.Button(T("page.gamebanana.auth.sign_in"), s.gbSignInFromToolbar, kit.Outline,
			kit.Icon(lucide.LogIn), kit.Loading(g.signingIn), kit.Disabled(g.signingIn),
			kit.Tooltip(T("page.gamebanana.auth.guest_notice"))).Radius(0).
			BorderEdges(1, 1, 1, 0, groupBorder))
		right.Child(kit.Button("", s.gbOpenManualRMC, kit.Outline, kit.Icon(gbKeyIcon),
			kit.Tooltip(T("page.gamebanana.auth.manual_rmc.button"))).
			Label(T("page.gamebanana.auth.manual_rmc.button")).
			Radii(0, t.RadiusLg(), t.RadiusLg(), 0).BorderEdges(1, 1, 1, 0, groupBorder))
	}
	return fairy.Row(left, right).WFull().Gap(12).Align(fairy.Start)
}

func (s *Shell) gbStageLabel() string {
	g := &s.gameBanana
	switch {
	case g.mod != nil:
		return T("page.gamebanana.stage.mod")
	case g.categoryID > 0:
		return T("page.gamebanana.stage.category")
	}
	return T("page.gamebanana.stage.game")
}

func (s *Shell) gbBreadcrumbBar() *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	link := func(text string, onClick func()) *fairy.El {
		return fairy.Text(text).
			FontSize(14).
			LineHeight(20).
			Color(t.MutedForeground).
			Ellipsis().
			MinW(0).
			HoverColor(t.Foreground).
			Cursor(fairy.CursorPointer).
			OnClick(onClick)
	}
	separator := func() *fairy.El {
		return fairy.Icon(lucide.ChevronRight).Size(14, 14).Color(t.MutedForeground).Shrink(0).Role(fairy.RoleHidden)
	}
	bar := fairy.Row(link(s.gbGameLabel(), s.gbResetToGameHome)).Gap(6).Align(fairy.Center).MinW(0)
	crumbs := s.gbBreadcrumbs()
	modName := ""
	if g.mod != nil && g.modView.data != nil {
		modName = g.modView.data.Profile.Name
	}
	for i, crumb := range crumbs {
		bar.Child(separator())
		last := i == len(crumbs)-1
		switch {
		case last && modName != "":
			bar.Child(link(crumb.name, func() { g.mod = nil }))
		case last:
			bar.Child(fairy.Text(crumb.name).FontSize(14).LineHeight(20).Ellipsis().MinW(0))
		default:
			index := i
			bar.Child(link(crumb.name, func() { s.gbSelectBreadcrumb(index) }))
		}
	}
	if modName != "" {
		bar.Child(separator(), fairy.Text(modName).FontSize(14).LineHeight(20).Ellipsis().MinW(0))
	}
	return bar
}

func (s *Shell) gbToggleModURL() {
	g := &s.gameBanana
	g.modURLError = ""
	g.modURLOpen = !g.modURLOpen
}

func (s *Shell) gbOpenManualRMC() {
	g := &s.gameBanana
	g.manualError, g.manualOpen = "", true
}

func (s *Shell) gbModURLForm() *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	field := kit.TextField(g.modURL, func(v string) { g.modURL = v },
		kit.Placeholder(T("page.gamebanana.open_mod_url.placeholder")), kit.Invalid(g.modURLError != ""),
		kit.Label(T("page.gamebanana.open_mod_url.title"))).
		OnSubmit(func(string) { s.gbOpenModURL() }).AutoFocus()
	col := fairy.Col(field).Gap(4).Grow(1).MinW(0)
	if g.modURLError != "" {
		col.Child(fairy.Text(g.modURLError).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	return fairy.Row(col, kit.Button(T("page.gamebanana.open_mod_url.confirm"), s.gbOpenModURL)).
		WFull().Gap(8).Align(fairy.Start).MarginEdges(8, 0, 0, 0).PadEdges(8, 0, 0, 0).
		BorderEdges(1, 0, 0, 0, t.Border)
}

func (s *Shell) gbDialogs(cx *fairy.Cx) {
	t := kit.T()
	g := &s.gameBanana
	if g.manualOpen {
		const prefix = "page.gamebanana.auth.manual_rmc."
		closeDialog := func() {
			if !g.manualSaving {
				g.manualOpen, g.manualError = false, ""
			}
		}
		body := fairy.Col(
			kit.TextField(g.manualValue, func(v string) { g.manualValue = v }, kit.Disabled(g.manualSaving),
				kit.Invalid(g.manualError != ""), kit.Label(T(prefix+"title"))).Password().
				OnSubmit(func(string) { s.gbSaveManualRMC() }).AutoFocus(),
		).Gap(6)
		if g.manualError != "" {
			body.Child(fairy.Text(g.manualError).FontSize(14).LineHeight(20).Color(t.Destructive))
		}
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(prefix + "title"),
			Description: T(prefix + "description"),
			Body:        body,
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), closeDialog, kit.Outline, kit.Disabled(g.manualSaving)),
				kit.Button(T("g.save"), s.gbSaveManualRMC, kit.Loading(g.manualSaving), kit.Disabled(g.manualSaving)),
			},
			OnClose: closeDialog,
			Key:     "gamebanana.manual-rmc",
		})
	}
	if g.logoutConfirm {
		dismiss := func() { g.logoutConfirm = false }
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T("page.gamebanana.logout_confirm.title"),
			Alert:       true,
			Description: T("page.gamebanana.logout_confirm.description"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline),
				kit.Button(T("page.gamebanana.logout"), func() {
					g.logoutConfirm = false
					s.gbLogout()
				}, kit.Destructive),
			},
			OnClose: dismiss,
			Key:     "gamebanana.logout",
		})
	}
	s.gbLightbox(cx)
}

// gbColumns is the card count per row at the React grid breakpoints for the window width.
func gbColumns(windowWidth float32, narrow, wide int) int {
	if windowWidth >= gbWideWidth {
		return wide
	}
	return narrow
}

func gbCardWidth(mainWidth float32, columns int) float32 {
	inner := mainWidth - 2*gbContentPad - gbScrollbarRoom
	return max(160, (inner-gbCardGap*float32(columns-1))/float32(columns))
}

func (s *Shell) gbHomePanel(windowWidth, mainWidth float32) *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	totalPages := -1
	disableNext := true
	if feed := g.subfeed.data; feed != nil {
		if feed.Metadata.PerPage > 0 {
			totalPages = int((feed.Metadata.RecordCount + feed.Metadata.PerPage - 1) / feed.Metadata.PerPage)
		}
		disableNext = feed.Metadata.IsComplete
	}
	header := fairy.Row(
		fairy.Text(T("page.gamebanana.latest_feed")).FontSize(14).LineHeight(20).Medium(),
		fairy.Spacer(),
		s.gbPagination("home", g.subfeedPage, totalPages, g.subfeedPage <= 1, disableNext,
			func(page int) { g.subfeedPage = max(1, page) }),
	).WFull().PadXY(16, 8).Gap(12).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border).Shrink(0)

	content := fairy.Col().WFull().Pad(gbContentPad).Gap(12)
	if g.subfeed.loading {
		content.Child(gbOverviewSkeleton())
	}
	if g.subfeed.err != nil {
		content.Child(gbErrorState(g.subfeed.err))
	}
	if feed := g.subfeed.data; feed != nil {
		columns := gbColumns(windowWidth, 3, 4)
		cardWidth := gbCardWidth(mainWidth, columns)
		grid := fairy.Row().Wrap().Gap(gbCardGap).WFull()
		for _, record := range feed.Records {
			if record.ModelName != "Mod" {
				continue
			}
			grid.Child(s.gbPreviewCard(record, cardWidth))
		}
		content.Child(grid)
	}
	return fairy.Col(header, fairy.Col(content).Key("gamebanana.home").Grow(1).MinH(0).WFull().ScrollY()).
		Key("gamebanana.home-panel")
}

var gbModSorts = []string{"Generic_Newest", "Generic_MostLiked", "Generic_MostDownloaded", "Generic_MostViewed"}

// gbFilteredMods filters the loaded page by name, as the React search box did.
func (s *Shell) gbFilteredMods() []gbSubmission {
	g := &s.gameBanana
	if g.category.data == nil {
		return nil
	}
	records := g.category.data.Index.Records
	query := strings.ToLower(strings.TrimSpace(g.modSearch))
	if query == "" {
		return records
	}
	var out []gbSubmission
	for _, record := range records {
		if strings.Contains(strings.ToLower(record.Name), query) {
			out = append(out, record)
		}
	}
	return out
}

func (s *Shell) gbCategoryPanel(windowWidth, mainWidth float32, hasSidebar bool) *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	loaded := g.category.data != nil
	searching := strings.TrimSpace(g.modSearch) != ""
	totalPages := -1
	complete := false
	if loaded {
		meta := g.category.data.Index.Metadata
		if meta.PerPage > 0 {
			totalPages = int((meta.RecordCount + meta.PerPage - 1) / meta.PerPage)
		}
		complete = meta.IsComplete
	}
	page := g.modsPage
	if searching {
		page, totalPages = 1, 1
	}
	sorts := make([]kit.Item, 0, len(gbModSorts))
	for _, sort := range gbModSorts {
		sorts = append(sorts, kit.Item{Value: sort, Label: T("page.gamebanana.mod_sort." + sort)})
	}
	header := fairy.Row(
		fairy.Spacer(),
		kit.Select("gamebanana.mod-sort", g.modsSort, sorts, func(v string) { g.modsSort, g.modsPage = v, 1 },
			kit.Label(T("page.gamebanana.mod_sort.label"))).W(192),
		kit.TextField(g.modSearch, func(v string) { g.modSearch = v }, kit.Icon(lucide.Search),
			kit.Placeholder(T("page.gamebanana.search_mods"))).W(256),
		s.gbPagination("category", page, totalPages, !loaded || searching || g.modsPage <= 1,
			!loaded || searching || complete, func(p int) {
				if loaded && !searching {
					g.modsPage = max(1, p)
				}
			}),
	).WFull().PadXY(16, 8).Gap(8).Align(fairy.Center).Wrap().BorderEdges(0, 0, 1, 0, t.Border).Shrink(0)

	content := fairy.Col().WFull().Pad(gbContentPad).Gap(16)
	if g.category.loading {
		content.Child(gbOverviewSkeleton())
	}
	if g.category.err != nil {
		content.Child(gbErrorState(g.category.err))
	}
	if loaded {
		mods := s.gbFilteredMods()
		if len(mods) == 0 {
			content.Child(gbEmptyState(T("page.gamebanana.no_results")))
		}
		columns := gbColumns(windowWidth, 4, 5)
		if hasSidebar {
			columns = gbColumns(windowWidth, 3, 4)
		}
		cardWidth := gbCardWidth(mainWidth, columns)
		grid := fairy.Row().Wrap().Gap(gbCardGap).WFull()
		for _, record := range mods {
			grid.Child(s.gbPreviewCard(record, cardWidth))
		}
		content.Child(grid)
	}
	return fairy.Col(header, fairy.Col(content).Key("gamebanana.category").Grow(1).MinH(0).WFull().ScrollY()).
		Key("gamebanana.category-panel")
}

// gbPreviewCard is a submission with its first screenshot, as PreviewCard drew it.
func (s *Shell) gbPreviewCard(item gbSubmission, width float32) *fairy.El {
	t := kit.T()
	height := width * 10 / 16
	media := fairy.Box().WFull().H(height).Fill(t.Muted.Alpha(0.4)).Clip()
	previewURL := gbSubmissionPreviewURL(item.Preview)
	if img := s.gbImage(previewURL, width, height, fairy.FitCover); img != nil {
		hover := fairy.Local[bool]("gamebanana.card.hover:" + strconv.Itoa(int(item.ID)))
		zoom := float32(1)
		if *hover {
			zoom = 1.02
		}
		media.Child(fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitCover).Scale(zoom).
			ScaleMotion(motion.Bezier(0.2, 0.4, 0, 0.2, 1)).OnHover(func(in bool) { *hover = in }))
	} else if previewURL == "" {
		// A preview that is loading leaves the box empty, as the React card only marked a missing one.
		media.Child(
			fairy.Col(fairy.Icon(lucide.Image).Size(32, 32), fairy.Text("No preview").FontSize(12).LineHeight(16)).
				WFull().HFull().Center().Gap(8).Color(t.MutedForeground),
		)
	}
	subtitle := item.Description
	if subtitle == "" && item.SubCategory != nil {
		subtitle = item.SubCategory.Name
	}
	if subtitle == "" && item.RootCategory != nil {
		subtitle = item.RootCategory.Name
	}
	meta := fairy.Row(fairy.Text(item.Submitter.Name).FontSize(12).LineHeight(16)).Gap(8).Wrap()
	if ts := gbSubmissionTimestamp(item); ts != nil {
		meta.Child(fairy.Text(gbFormatDate(int64(*ts))).FontSize(12).LineHeight(16))
	}
	stats := fairy.Row().Gap(12).Wrap()
	for _, stat := range []struct {
		icon  *svg.Vector
		value *gbNumber
	}{{lucide.Heart, item.LikeCount}, {lucide.MessageSquare, item.PostCount}, {lucide.Eye, item.ViewCount}} {
		if stat.value != nil {
			stats.Child(
				fairy.Row(fairy.Icon(stat.icon).Size(12, 12), fairy.Text(gbFormatNumber(*stat.value)).FontSize(12).LineHeight(16)).
					Gap(4).
					Align(fairy.Center),
			)
		}
	}
	text := fairy.Col(
		fairy.Row(
			fairy.Col(
				fairy.Text(item.Name).FontSize(16).LineHeight(24).Medium().Ellipsis(),
				fairy.Text(subtitle).FontSize(12).Color(t.MutedForeground).LineHeight(16).MaxH(32).Clip(),
			).Gap(4).Grow(1).MinW(0),
			fairy.Icon(lucide.ExternalLink).Size(16, 16).Color(t.MutedForeground).Shrink(0),
		).Gap(12).Align(fairy.Start),
		meta.FontSize(12).Color(t.MutedForeground),
		stats.FontSize(12).Color(t.MutedForeground),
	).Pad(16).Gap(12)
	id, model := int(item.ID), item.ModelName
	return fairy.Col(media, text).Key("gamebanana.card." + strconv.Itoa(id)).W(width).Radius(16).Clip().
		Fill(t.Muted.Alpha(0.3)).HoverFill(t.Muted.Alpha(0.6)).Animate(motion.Fast).Cursor(fairy.CursorPointer).
		Label(item.Name).OnClick(func() { s.gbSelectMod(id, model) })
}

func (s *Shell) gbCategorySidebar() *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	nested := g.categoryID > 0
	var categories []gbCategory
	loading, err := g.overview.loading, g.overview.err
	if nested {
		loading, err = g.category.loading, g.category.err
		if g.category.data != nil {
			categories = g.category.data.Categories
		}
	} else if g.overview.data != nil {
		categories = g.overview.data.Profile.RootCategories
	}
	query := strings.ToLower(strings.TrimSpace(g.categorySearch))

	head := fairy.Row(fairy.Text(T("page.gamebanana.category_panel_title")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground).
		PadXY(4, 0), fairy.Spacer()).
		MinH(28).
		Align(fairy.Center)
	if nested {
		head.Child(kit.Button(T("page.gamebanana.root_categories"), s.gbResetToGameHome, kit.Ghost, kit.Small))
	}
	top := fairy.Col(head, kit.TextField(g.categorySearch, func(v string) { g.categorySearch = v },
		kit.Icon(lucide.Search), kit.Placeholder(T("page.gamebanana.search_categories")),
		kit.Label(T("page.gamebanana.search_categories")))).Pad(12).Gap(12).Shrink(0)

	list := fairy.Col().PadEdges(0, 12, 12, 12).Gap(2)
	switch {
	case loading:
		for range 3 {
			list.Child(kit.Skeleton().WFull().H(48))
		}
	case err != nil:
		list.Child(gbErrorState(err))
	default:
		shown := 0
		for _, category := range categories {
			if query != "" && !strings.Contains(strings.ToLower(category.Name), query) {
				continue
			}
			shown++
			list.Child(s.gbCategoryRow(category))
		}
		if shown == 0 {
			empty := T("page.gamebanana.no_categories")
			if query != "" {
				empty = T("page.gamebanana.no_category_results")
			}
			list.Child(gbEmptyState(empty))
		}
	}
	return fairy.Col(top, fairy.Col(list).Key("gamebanana.categories").Grow(1).MinH(0).WFull().ScrollY())
}

func (s *Shell) gbCategoryRow(category gbCategory) *fairy.El {
	t := kit.T()
	id, name := int(category.ID), category.Name
	active := s.gameBanana.categoryID == id
	icon := fairy.Box().Size(40, 40).Radius(20).Fill(t.Muted).Clip().Shrink(0)
	if img := s.gbImage(category.IconURL, 40, 40, fairy.FitCover); img != nil {
		icon.Child(fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitCover))
	} else {
		icon.Child(fairy.Text("Icon").FontSize(14).LineHeight(20).Color(t.MutedForeground)).Center()
	}
	row := fairy.Row(icon, fairy.Text(name).FontSize(14).LineHeight(20).Medium().Ellipsis().MinW(0)).
		Key("gamebanana.category."+strconv.Itoa(id)).
		WFull().PadXY(12, 8).Align(fairy.Center).Justify(fairy.JustifyBetween).
		Radius(6).Animate(motion.Fast).Cursor(fairy.CursorPointer).Selected(active)
	if category.ItemCount != nil {
		row.Child(
			fairy.Text(gbFormatNumber(*category.ItemCount)).
				FontSize(12).
				LineHeight(16).
				Color(t.MutedForeground).
				Shrink(0),
		)
	}
	if active {
		row.Fill(t.Primary.Alpha(0.1)).Color(t.Primary)
	} else {
		row.HoverFill(t.Muted.Alpha(0.5))
	}
	return row.OnClick(func() {
		if id > 0 {
			s.gbSelectCategory(id, name)
		}
	})
}

// gbPagination is the previous and next buttons around an editable page number. A negative totalPages is
// unknown and hides the total; zero is a loaded empty feed and shows "/ 0", as the React buttons did.
func (s *Shell) gbPagination(
	id string,
	page, totalPages int,
	disablePrev, disableNext bool,
	onPage func(int),
) *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	edit, editing := g.pageEdits[id]
	value := strconv.Itoa(page)
	if editing {
		value = edit
	}
	commit := func() {
		text, ok := g.pageEdits[id]
		delete(g.pageEdits, id)
		if !ok {
			return
		}
		n, valid := parseInt(text)
		if !valid || n < 1 {
			return
		}
		if totalPages > 0 {
			n = min(n, totalPages)
		}
		if n != page {
			onPage(n)
		}
	}
	field := fairy.Input(value, func(v string) { g.pageEdits[id] = v }).Key("gamebanana.page."+id).
		W(56).H(28).PadXY(10, 0).TextAlign(fairy.TextCenter).FontSize(14).LineHeight(20).Radius(t.RadiusMd()).
		Border(1, t.Input).FocusBorder(t.Accent).Fill(inputFill(t)).
		OnSubmit(func(string) {
			commit()
			s.win.Focus("")
		}).
		OnInputKey(func(k keyboard.KeyEvent) bool {
			// Escape drops the typed page and leaves the field, as the React page field did.
			if !k.Is(keyboard.KeyEscape, 0) {
				return false
			}
			delete(g.pageEdits, id)
			s.win.Focus("")
			return true
		}).OnBlur(commit)
	number := fairy.Row(field).Gap(4).Align(fairy.Center)
	if totalPages >= 0 {
		number.Child(fairy.Text("/ " + strconv.Itoa(totalPages)).FontSize(12).LineHeight(16).Color(t.MutedForeground))
	}
	return fairy.Row(
		kit.Button(T("page.gamebanana.previous"), func() { onPage(page - 1) }, kit.Outline, kit.Small,
			kit.Disabled(disablePrev)),
		number,
		kit.Button(T("page.gamebanana.next"), func() { onPage(page + 1) }, kit.Outline, kit.Small,
			kit.Disabled(disableNext)),
	).Gap(8).Align(fairy.Center).Shrink(0)
}

func gbOverviewSkeleton() *fairy.El {
	return fairy.Col(kit.Skeleton().WFull().H(96), kit.Skeleton().WFull().H(96), kit.Skeleton().WFull().H(96)).
		WFull().Gap(12)
}

func gbEmptyState(text string) *fairy.El {
	t := kit.T()
	return fairy.Box(fairy.Text(text).FontSize(14).LineHeight(20).Color(t.MutedForeground).TextAlign(fairy.TextCenter)).
		WFull().
		Pad(24).
		Center().
		Radius(t.Radius).
		Border(1, t.Border).
		BorderDashed()
}

func gbErrorState(err error) *fairy.El {
	t := kit.T()
	description, details := gbErrorPresentation(err)
	col := fairy.Col(fairy.Text(T("page.gamebanana.error_title")).FontSize(14).LineHeight(20).Medium().Color(t.Foreground)).
		WFull().
		Pad(24).
		Gap(10).
		Align(fairy.Center).
		Radius(t.RadiusMd()).
		Border(1, t.Input).
		Color(t.MutedForeground)
	if description != "" {
		col.Child(fairy.Text(description).FontSize(14).LineHeight(20).TextAlign(fairy.TextCenter))
	}
	if details != "" {
		col.Child(
			fairy.Text(details).FontSize(12).LineHeight(16).WFull().PadXY(12, 8).Radius(6).Fill(t.Muted.Alpha(0.5)),
		)
	}
	return col.Child(
		fairy.Row(fairy.Icon(lucide.RefreshCw).Size(16, 16), fairy.Text(T("page.gamebanana.retry_later")).FontSize(14).LineHeight(20)).
			Gap(8).
			Align(fairy.Center),
	)
}

func gbFormatNumber(n gbNumber) string { return catalog.Integer(int64(n)) }

// gbFormatDate writes a Unix time as Intl.DateTimeFormat did with a short month: "Nov 9, 2026" in English.
func gbFormatDate(seconds int64) string {
	date := time.Unix(seconds, 0)
	lang := catalog.Language()
	switch {
	case strings.HasPrefix(lang, "ko"):
		return date.Format("2006년 1월 2일")
	case strings.HasPrefix(lang, "ja"), strings.HasPrefix(lang, "zh"):
		return date.Format("2006年1月2日")
	}
	return date.Format("Jan 2, 2006")
}

func gbSubmissionTimestamp(item gbSubmission) *gbNumber {
	switch {
	case item.DateAdded != nil:
		return item.DateAdded
	case item.DateModified != nil:
		return item.DateModified
	}
	return item.DateUpdated
}
