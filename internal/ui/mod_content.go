package ui

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	_ "golang.org/x/image/bmp" // Mod previews may be BMP; fairy registers the other preview formats.

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/agent"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/mod/appliedtool"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
)

const (
	modCardHeight float32 = 400
	// modPreviewMaxWidth bounds the decoded width of a card's preview, which is only as wide as one column.
	modPreviewMaxWidth float32 = 1024
	modCardGap         float32 = 12
	modRowHeight       float32 = 56
	modIniWidth        float32 = 170
	// modBackdropBlur is the blur of the 64-pixel card backdrop, about the 16-pixel CSS blur on a 400-pixel card.
	modBackdropBlur float32 = 2.5
	modSizeColumn   float32 = 112
	modDateColumn   float32 = 208
	modThumbColumn  float32 = 56
)

var (
	// modDetailsMotion is the 200ms ease-out of the card details widening on hover.
	modDetailsMotion = motion.Bezier(0.2, 0, 0, 0.2, 1)
	// modIniMotion is the 200ms transition of the INI list sliding open or shut.
	modIniMotion = motion.Bezier(0.2, 0.4, 0, 0.2, 1)

	previewImageExtensions = []string{".png", ".jpg", ".jpeg", ".webp", ".avif", ".avifs", ".gif", ".bmp"}
	previewVideoExtensions = []string{".mp4", ".webm", ".ogg", ".avi", ".mkv", ".mov"}
)

func previewExtension(path string) string { return strings.ToLower(filepath.Ext(path)) }

func isPreviewImage(path string) bool {
	return slices.Contains(previewImageExtensions, previewExtension(path))
}

func isPreviewVideo(path string) bool {
	return slices.Contains(previewVideoExtensions, previewExtension(path))
}

func isPreviewMedia(path string) bool { return isPreviewImage(path) || isPreviewVideo(path) }

// hasOwnPreview reports whether preview is the folder's own preview.* file rather than one found below it.
func hasOwnPreview(folder string, preview *string) bool {
	if preview == nil {
		return false
	}
	name := filepath.Base(*preview)
	return platform.SamePathFold(filepath.Dir(*preview), folder) &&
		strings.HasPrefix(strings.ToLower(name), "preview.") && !strings.Contains(name[len("preview."):], ".")
}

// previewThumb draws the preview at path with rounded corners, or fallback while it loads or when there is
// none. version is part of the cache key, so a replaced file is read again. imageKey, when set, names an image
// preview in the cache instead of its path and version.
func (s *Shell) previewThumb(
	path *string,
	version any,
	imageKey func(file string) string,
	size, radius float32,
	fallback *fairy.El,
) *fairy.El {
	if path == nil || !isPreviewImage(*path) {
		if path != nil && isPreviewVideo(*path) {
			if video := s.modVideo(*path, version); video != nil && video.State() != fairy.VideoLoading {
				return fairy.VideoView(video).WFull().HFull().Fit(fairy.FitCover).Radius(radius)
			}
			return fairy.Icon(lucide.Film).Size(float32(size)/2, float32(size)/2).Color(kit.T().MutedForeground)
		}
		return fallback
	}
	file := *path
	key := fmt.Sprint("mod:", file, ":", version)
	if imageKey != nil {
		key = imageKey(file)
	}
	img := s.sizedModPreview(key, file, size, size, fairy.FitCover)
	if img == nil {
		_ = s.previewImageErr(key, file, size, size, fairy.FitCover)
		return fallback
	}
	return fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitCover).Radius(radius)
}

type modFilterKey struct {
	listing   *mod.FolderGroup
	search    string
	sortType  string
	desc      bool
	downloads string
}

// filteredMods sorts and filters the listing as the React screen did: mods being downloaded first, then enabled
// mods, then by search relevance, then by the chosen order. The result is kept until one of its inputs changes.
func (s *Shell) filteredMods() []mod.ModInfo {
	m := &s.mods
	if m.listing == nil {
		return nil
	}
	downloads := s.modDownloadsIn(m.listing.Path)
	key := modFilterKey{m.listing, m.search, m.sortType, m.sortDesc, downloadIdentity(downloads)}
	if m.filterKey == key {
		return m.filtered
	}
	listed, downloading, placeholders := withDownloads(m.listing.Mods, downloads)
	m.downloading, m.placeholders = downloading, placeholders
	for path := range m.mergeSelected {
		if downloading[strings.ToLower(path)] {
			delete(m.mergeSelected, path)
		}
	}
	query := strings.ToLower(m.search)
	type scored struct {
		mod   mod.ModInfo
		score int
	}
	items := make([]scored, 0, len(listed))
	for _, info := range listed {
		score := 0
		if query != "" {
			if score = searchScore(newSearchIndex(info.Name), query); score == 0 {
				continue
			}
		}
		items = append(items, scored{info, score})
	}
	less := platform.NewLocaleLess()
	slices.SortStableFunc(items, func(a, b scored) int {
		if da, db := downloading[strings.ToLower(a.mod.Path)], downloading[strings.ToLower(b.mod.Path)]; da != db {
			if da {
				return -1
			}
			return 1
		}
		if a.mod.IsEnabled != b.mod.IsEnabled {
			if a.mod.IsEnabled {
				return -1
			}
			return 1
		}
		if query != "" && a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		var c int
		switch m.sortType {
		case "date":
			c = cmp.Compare(a.mod.Mtime, b.mod.Mtime)
		case "size":
			c = cmp.Compare(a.mod.Size, b.mod.Size)
		default:
			switch {
			case less(a.mod.Name, b.mod.Name):
				c = -1
			case less(b.mod.Name, a.mod.Name):
				c = 1
			}
		}
		if m.sortDesc {
			return -c
		}
		return c
	})
	out := make([]mod.ModInfo, len(items))
	for i, item := range items {
		out[i] = item.mod
	}
	m.filterKey, m.filtered = key, out
	return out
}

func (s *Shell) setModSort(sortType string) {
	m := &s.mods
	m.sortType = sortType
	m.sortDesc = sortType != "name"
}

func (s *Shell) modHeader() *fairy.El {
	t := kit.T()
	m := &s.mods
	group := m.group
	search := fairy.Row(
		fairy.Input(m.search, s.setModSearch).Key("mod.search").Placeholder(T("g.search")).FontSize(14).LineHeight(20).
			PlaceholderColor(t.MutedForeground).Grow(1).MinW(0).Label(T("g.search")).OnSubmit(s.submitModSearch),
		fairy.Icon(lucide.Search).Size(16, 16).Color(t.MutedForeground),
	).W(180).H(32).PadXY(10, 0).Gap(6).Align(fairy.Center).Radius(t.RadiusMd()).Fill(t.Card).
		Border(1, t.Input)

	sortIcon := map[string][2]*svg.Vector{
		"name": {lucide.ArrowUpAZ, lucide.ArrowDownAZ},
		"date": {lucide.ArrowUp10, lucide.ArrowDown10},
		"size": {lucide.ArrowUpWideNarrow, lucide.ArrowDownWideNarrow},
	}[m.sortType]
	orderIcon := sortIcon[0]
	if m.sortDesc {
		orderIcon = sortIcon[1]
	}
	viewIcon, viewLabel := lucide.List, T("page.mod.view.list")
	if m.list {
		viewIcon, viewLabel = lucide.LayoutGrid, T("page.mod.view.grid")
	}
	items := []kit.MenuItem{
		{
			Label: T("page.mod.all_enabled"), Icon: lucide.Circle, Loading: m.bulkBusy, Disabled: m.bulkBusy,
			OnClick: func() { s.setAllMods(group.Path, true) },
		},
		{
			Label: T("page.mod.all_disabled"), Icon: lucide.CircleOff, Loading: m.bulkBusy, Disabled: m.bulkBusy,
			OnClick: func() { s.setAllMods(group.Path, false) },
		},
		{Separator: true},
	}
	if s.canMerge() {
		label := T("page.mod.merge.enter") + " (" + T("g.beta") + ")"
		if m.mergeMode {
			label = T("page.mod.merge.exit")
		}
		items = append(items, kit.MenuItem{
			Label: label, Icon: lucide.Layers, OnClick: func() { s.setMergeMode(!m.mergeMode) },
		})
	}
	items = append(items, kit.MenuItem{Label: T("g.download"), Icon: lucide.Download, OnClick: s.openURLDownload})
	if fixer := s.modFixerMenu(group.Path); fixer != nil {
		fixer.Label = T("page.mod.content-header.tools")
		items = append(items, kit.MenuItem{Separator: true}, *fixer)
	}
	more := kit.DropdownMenu(
		"mod.content-menu",
		kit.IconButton(lucide.Ellipsis, nil, kit.Outline).
			Label(T("page.mod.character-sidebar.more-options")),
		items,
	)
	title := fairy.Row(fairy.Text(group.Name).FontSize(24).LineHeight(32).SemiBold().Ellipsis().MinW(0)).
		Gap(12).
		Align(fairy.Center).
		Grow(1).
		MinW(0)
	if m.mergeMode {
		title.Child(s.mergeHeader())
	}
	sort := fairy.Row(
		kit.Select("mod.sort", m.sortType, []kit.Item{
			{Value: "name", Label: T("g.name")},
			{Value: "date", Label: T("g.date")},
			{Value: "size", Label: T("g.size")},
		}, s.setModSort).W(80),
		kit.IconButton(orderIcon, func() { m.sortDesc = !m.sortDesc }, kit.Outline).Label(T("page.mod.sort_order")),
		kit.VSeparator(),
		kit.IconButton(viewIcon, func() { m.list = !m.list }, kit.Outline, kit.Tooltip(viewLabel)).Label(viewLabel).
			MarginEdges(0, 0, 0, 4),
	).Gap(4).Align(fairy.Center)
	return fairy.Row(
		title,
		search,
		kit.VSeparator(),
		sort,
		kit.VSeparator(),
		kit.IconButton(lucide.Folder, func() { s.openPath(group.Path) }, kit.Outline,
			kit.Tooltip(T("page.mod.context-menu.open-folder"))).Label(T("page.mod.context-menu.open-folder")).
			MarginEdges(0, 0, 0, 4),
		more,
	).Key("mod.header").H(48).Shrink(0).PadXY(12, 0).Gap(8).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border)
}

// submitModSearch toggles the only mod left by the search on its own, disabling the rest of the folder, and
// clears the search. It does nothing in merge mode, as the React shortcut did.
func (s *Shell) submitModSearch(string) {
	m := &s.mods
	if m.search == "" || m.mergeMode {
		return
	}
	mods := s.filteredMods()
	if len(mods) != 1 || s.isModDownloading(mods[0].Path) {
		return
	}
	s.toggleMod(mods[0], true)
	s.setModSearch("")
}

// setModSearch filters the mods and scrolls back to the first one, as the React list and grid did.
func (s *Shell) setModSearch(value string) {
	m := &s.mods
	if m.search == value {
		return
	}
	m.search = value
	if m.group != nil {
		s.win.ScrollToRowInstant(s.modViewKey(), 0)
	}
}

// modViewKey names the list or grid of the selected folder. The folder is part of the key, so another folder
// starts at the top.
func (s *Shell) modViewKey() string {
	m := &s.mods
	path := ""
	if m.group != nil {
		path = m.group.Path
	}
	if m.list {
		return "mod.list:" + path
	}
	return "mod.grid:" + path
}

func (s *Shell) modBody(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	m := &s.mods
	if m.group == nil {
		return fairy.Col(fairy.Text(T("page.mod.empty_selection")).FontSize(16).Color(t.MutedForeground)).
			Grow(1).
			MinH(0).
			WFull().
			Center()
	}
	if m.listing == nil {
		return s.modSkeleton()
	}
	mods := s.filteredMods()
	if m.list {
		return fairy.Col(
			s.modListHead(),
			fairy.VirtualList(len(mods), modRowHeight, func(i int) *fairy.El { return s.modRow(mods[i]) }).
				Key(s.modViewKey()).Grow(1).MinH(0),
		).Grow(1).MinH(0).WFull().FontSize(14)
	}

	if !s.settingsLoaded(setting.KeyModGridLayoutMode, setting.KeyModGridResponsiveBaseWidth,
		setting.KeyModGridFixedCardWidth, setting.KeyModGridFixedColumnCount) {
		return s.modSkeleton()
	}
	clampWidth := func(key string, fallback int) float32 {
		value := s.intSetting(key)
		if value <= 0 {
			value = fallback
		}
		return float32(min(max(value, 240), 640))
	}
	grid := fairy.VirtualGrid(
		len(mods),
		0,
		modCardHeight,
		modCardGap,
		func(i int) *fairy.El { return s.modCard(mods[i]) },
	)
	switch s.stringSetting(setting.KeyModGridLayoutMode) {
	case "fixed_card_width":
		grid = fairy.VirtualGrid(len(mods), clampWidth(setting.KeyModGridFixedCardWidth, 360), modCardHeight,
			modCardGap, func(i int) *fairy.El { return s.modCard(mods[i]) }).GridCenter()
	case "fixed_column_count":
		columns := s.intSetting(setting.KeyModGridFixedColumnCount)
		if columns <= 0 {
			columns = 4
		}
		grid.GridColumns(min(max(columns, 1), 8))
	default:
		grid = fairy.VirtualGrid(len(mods), clampWidth(setting.KeyModGridResponsiveBaseWidth, 400), modCardHeight,
			modCardGap, func(i int) *fairy.El { return s.modCard(mods[i]) }).GridFill()
	}
	return grid.Key(s.modViewKey()).Grow(1).MinH(0).WFull().Pad(12)
}

func (s *Shell) modSkeleton() *fairy.El {
	rows := fairy.Col().Grow(1).MinH(0).WFull().Pad(12).Gap(8)
	for range 12 {
		rows.Child(kit.Skeleton().H(48).WFull())
	}
	return rows
}

// modTint is the card and row colour of a mod: green when enabled, red when disabled, and yellow when enabled
// with a missing dependency, which loads it broken.
func modTint(dark, enabled, missingDependency bool) fairy.Color {
	switch {
	case enabled && missingDependency:
		return pick(dark, fairy.Hex(0x4d4208), fairy.Hex(0xeedea1))
	case enabled:
		return pick(dark, fairy.Hex(0x0d430d), fairy.Hex(0xbfe8bf))
	}
	return pick(dark, fairy.Hex(0x58151b), fairy.Hex(0xffd0d2))
}

func pick(dark bool, darkColor, lightColor fairy.Color) fairy.Color {
	if dark {
		return darkColor
	}
	return lightColor
}

func missingDependencies(info mod.ModInfo) []string {
	var names []string
	for _, dependency := range info.Dependencies {
		if !dependency.Installed {
			names = append(names, dependency.Name)
		}
	}
	return names
}

func (s *Shell) modCard(info mod.ModInfo) *fairy.El {
	t := kit.T()
	m := &s.mods
	missing := missingDependencies(info)
	hovered := m.hover == info.Path
	nte := s.isNteGame()

	title := fairy.Row().Gap(4).Align(fairy.Center).Grow(1).MinW(0)
	if len(missing) > 0 {
		title.Child(fairy.Icon(lucide.TriangleAlert).Size(16, 16).Shrink(0).Color(t.Destructive).
			Tooltip(T("page.mod.dialog.missing-dependencies.badge") + "\n" + strings.Join(missing, ", ")))
	}
	for _, icon := range appliedToolIconsOf(info, t) {
		title.Child(icon)
	}
	title.Child(fairy.Text(info.Name).FontSize(14).LineHeight(20).SemiBold().Ellipsis().MinW(0).TooltipWhenTruncated())
	header := fairy.Row(title).H(32).Shrink(0).Gap(4).Align(fairy.Center).PadEdges(0, 0, 4, 0)
	header.Child(kit.IconButton(lucide.Sparkles, func() {
		s.openAgentScope(agent.AgentScope{Type: "mod", ModPath: info.Path, ModName: info.Name})
	}, kit.Ghost, kit.Small).Label(T("page.agent.title")).Tooltip(T("page.agent.title")))
	if !nte && s.deps.Tools != nil {
		header.Child(
			kit.DropdownMenuFunc("mod.tools:"+info.Path,
				kit.IconButton(lucide.Wrench, nil, kit.Ghost, kit.Small).Label(T("page.mod.card.mod_tools")),
				func() []kit.MenuItem { return s.modCardToolItems(info) }, kit.MaxWidth(208)),
			kit.IconButton(lucide.Box, func() { s.openModelViewer(info.Path) }, kit.Ghost, kit.Small,
				kit.Loading(s.modelViewerOpening(info.Path))).
				Label(T("page.mod.context-menu.open-model-viewer")).
				Tooltip(T("page.mod.context-menu.open-model-viewer")),
		)
	}
	header.Child(
		kit.IconButton(lucide.Trash, func() { s.deleteMod(info) }, kit.Ghost, kit.Small).
			Label(T("g.delete")).Tooltip(T("g.delete")),
		kit.IconButton(lucide.Folder, func() { s.openPath(info.Path) }, kit.Ghost, kit.Small).
			Label(T("page.mod.context-menu.open-folder")).Tooltip(T("page.mod.context-menu.open-folder")),
	)

	var imageKey string
	if info.Preview != nil && isPreviewImage(*info.Preview) {
		imageKey = modPreviewKey(info, *info.Preview)
	}
	preview := s.modPreview(info, hovered, imageKey)
	body := fairy.Row(preview).Grow(1).MinH(0).Gap(8)
	if !nte && len(info.Inis) > 0 {
		key := modListingKey(m.group.Path, info.Path)
		shut := m.iniShut[key]
		label := T("page.mod.card.collapse_ini_list")
		if shut {
			label = T("page.mod.card.expand_ini_list")
		}
		toggle := fairy.Box().Absolute().Top(0).Bottom(0).Left(-12).W(24).Cursor(fairy.CursorResizeEW).Label(label).
			Tooltip(label).OnClick(func() { m.iniShut[key] = !shut })

		// The preview and the divider follow the list's width as it slides open or shut.
		preview.AnimateLayout(modIniMotion)
		slot := fairy.Row().W(modIniWidth).Shrink(0).HFull().Clip().AnimateLayout(modIniMotion)
		if shut {
			slot.W(0)
		} else {
			slot.Child(s.modIniList(info).Key("list").Enter(fairy.Fade().With(modIniMotion)).
				Exit(fairy.Fade().With(modIniMotion)))
		}
		body.Child(fairy.Box(toggle).W(1).Shrink(0).HFull().Fill(t.Border).AnimateLayout(modIniMotion), slot)
	}

	card := fairy.Col().
		Child(s.modCardBackdrop(info, imageKey), header, body).
		Key("mod.card:" + info.Path).
		WFull().
		HFull().
		Pad(4).
		Radius(4).
		Clip().
		Fill(modTint(s.dark, info.IsEnabled, len(missing) > 0)).
		Animate(motion.Fast).
		Cursor(fairy.CursorPointer).
		Focusable().
		Label(info.Name).
		Checked(info.IsEnabled).
		OnHover(func(on bool) { s.hoverMod(info.Path, on) }).
		OnClick(func() { s.clickMod(info) }).
		OnKey(func(k keyboard.KeyEvent) bool { return s.modKey(info, k) })

	// The shadow stays set so that its colour fades in and out with the hover.
	shadow := fairy.Hex(0x000000).Alpha(0)
	if hovered {
		shadow = shadow.Alpha(0.18)
	}
	card.Shadow(0, 4, 16, 0, shadow)
	if s.isModDownloading(info.Path) {
		card.Fill(t.Muted).Cursor(fairy.CursorWait).Disabled(true)
		if snapshot := s.modDownloadTransfer(info.Path); snapshot != nil {
			card.Child(modDownloadOverlay(snapshot, false))
		}
		return card
	}
	if m.mergeMode && m.mergeSelected[info.Path] {
		card.Ring(2, t.Primary)
	}
	return kit.ContextMenu("mod.card:"+info.Path, card, func() []kit.MenuItem { return s.modMenuItems(info) },
		kit.Width(224))
}

var appliedToolIcons = []struct {
	id, label string
	icon      *svg.Vector
}{
	{appliedtool.BodyShape, "page.mod.applied-tools.body-shape", lucide.PersonStanding},
	{appliedtool.TouchProfile, "page.mod.applied-tools.touch-profile", lucide.Hand},
	{appliedtool.LiquidEmitter, "page.mod.applied-tools.liquid-emitter", lucide.Droplets},
}

// appliedToolIconsOf marks the tools whose output is in the mod, as ModAppliedToolIcons did.
func appliedToolIconsOf(info mod.ModInfo, t *kit.Theme) []*fairy.El {
	var icons []*fairy.El
	for _, tool := range appliedToolIcons {
		if slices.Contains(info.AppliedTools, tool.id) {
			icons = append(icons, fairy.Icon(tool.icon).Size(16, 16).Shrink(0).Color(t.MutedForeground).
				Label(T(tool.label)).Tooltip(T(tool.label)))
		}
	}
	return icons
}

// modCardBackdrop is the mod's preview image blurred and faded over the whole card, or nil while it loads or
// when the preview is not an image (imageKey is empty).
func (s *Shell) modCardBackdrop(info mod.ModInfo, imageKey string) *fairy.El {
	if imageKey == "" {
		return nil
	}
	// Under the preview's own key the cache scales the decoded preview down instead of reading and decoding the
	// file again, so the blur waits until the preview has loaded.
	file := *info.Preview
	if s.sizedModPreview(imageKey, file, modPreviewMaxWidth, modCardHeight, fairy.FitContain) == nil {
		return nil
	}
	img := fairy.Images.Blurred(imageKey, 64, 64, modBackdropBlur, func() ([]byte, error) { return os.ReadFile(file) })
	if img == nil {
		return nil
	}
	opacity := float32(0.1)
	if s.dark {
		opacity = 0.25
	}
	return fairy.ImageEl(img).Inset(0).Fit(fairy.FitFill).Radius(4).Opacity(opacity).
		PassThrough()
}

func (s *Shell) hoverMod(path string, on bool) {
	m := &s.mods
	switch {
	case on:
		m.hover = path
	case m.hover == path:
		m.hover = ""
	}
}

func (s *Shell) clickMod(info mod.ModInfo) {
	if m := &s.mods; m.mergeMode {
		m.mergeSelected[info.Path] = !m.mergeSelected[info.Path]
		return
	}
	s.toggleMod(info, s.win.Mods()&keyboard.ModCtrl != 0)
}

// modPreview shows the mod's preview image with its details over the bottom edge, and buttons that step
// through the other images of the mod while the pointer is over the card.
func (s *Shell) modPreview(info mod.ModInfo, hovered bool, imageKey string) *fairy.El {
	t := kit.T()
	fallback := fairy.Col(
		fairy.Icon(lucide.Image).Size(48, 48).Color(t.MutedForeground.Alpha(0.5)),
		fairy.Text(T("page.mod.no-preview")).FontSize(13).Color(t.MutedForeground),
		kit.Button(T("page.mod.context-menu.paste-preview"), func() { s.pastePreview(info) }, kit.Outline, kit.Small,
			kit.Icon(lucide.Clipboard)),
	).Gap(6).Align(fairy.Center)
	content, modelPreview := s.cardModelPreview(info, fallback)
	if !modelPreview {
		content = s.previewImage(info, imageKey, fallback)
	}
	area := fairy.Box(content).Grow(1).MinW(0).HFull().Center().Clip()

	// Without dependencies the details fade in over the full width on hover. With them, the dependency line
	// stays and the box widens to the full width while the size and date line grows open above it; the
	// dependency line moves down inside the rising box by the same amount, so it holds still.
	stat := fairy.Col(fairy.Text(formatSize(int64(info.Size)) + " · " + formatModTime(info.Mtime, true)).Ellipsis()).
		Clip().Opacity(0).Animate(modDetailsMotion)
	// The React card's badges were bg-background/35 over backdrop-blur; without the blur the box stays legible at 0.7.
	details := glass(fairy.Col(stat).Absolute().Left(0).Bottom(0).PadXY(8, 4).Gap(2).Radius(4).Clip().
		FontSize(12).Color(t.MutedForeground).PassThrough(), t.Background.Alpha(0.35), t.Background.Alpha(0.7), 8)
	switch {
	case len(info.Dependencies) == 0:
		details.Right(0).Opacity(0).Animate(motion.Fast)
		stat.Opacity(1)
		if hovered {
			details.Opacity(1)
		}
	case hovered:
		details.Right(0).AnimateLayout(modDetailsMotion)
		stat.Opacity(1).AnimateLayout(modDetailsMotion)
	default:
		details.AnimateLayout(modDetailsMotion).Gap(0)
		stat.Size(0, 0).AnimateLayout(modDetailsMotion)
	}
	if len(info.Dependencies) > 0 {
		var spans []fairy.Span
		for i, dependency := range info.Dependencies {
			if i > 0 {
				spans = append(spans, fairy.Span{Text: ", "})
			}
			span := fairy.Span{Text: dependency.Name}
			if !dependency.Installed {
				span.Color = t.Destructive
			}
			spans = append(spans, span)
		}
		details.Child(
			fairy.Row(fairy.Icon(lucide.Package).Size(12, 12).Shrink(0), fairy.RichText(spans...).Ellipsis().MinW(0)).
				Gap(4).Align(fairy.Center).AnimateLayout(modDetailsMotion),
		)
	}
	if !s.isDownloadPlaceholder(info.Path) {
		area.Child(details)
	}

	if info.Preview != nil && isPreviewMedia(*info.Preview) && len(info.PreviewImages) > 1 {
		step := func(icon *svg.Vector, label string, direction int) *fairy.El {
			return kit.IconButton(icon, func() { s.stepPreview(info, direction) }, kit.Secondary, kit.Small).
				Label(label).Radius(16)
		}
		steps := fairy.Row(
			step(lucide.ChevronLeft, T("page.mod.previous-preview"), -1),
			step(lucide.ChevronRight, T("page.mod.next-preview"), 1),
		).Inset(0).PadXY(8, 0).Align(fairy.Center).Justify(fairy.JustifyBetween).PassThrough().
			Opacity(0).Animate(motion.Fast)
		if hovered {
			steps.Opacity(1)
		} else {
			// Kept for the fade but out of reach while transparent.
			steps.Disabled(true)
		}
		area.Child(steps)
	}
	if modelPreview {
		return kit.ContextMenu("mod.preview:"+info.Path, area, func() []kit.MenuItem {
			return s.modelPreviewMenuItems(info)
		})
	}
	if info.Preview == nil || !isPreviewMedia(*info.Preview) {
		return area
	}
	return kit.ContextMenu("mod.preview:"+info.Path, area, func() []kit.MenuItem { return s.previewMenuItems(info) })
}

// modVideo plays the preview video at path muted and looping, or returns nil when it cannot play now.
func (s *Shell) modVideo(path string, version any) *fairy.Video {
	return s.modVideos.video(fmt.Sprint(path, ":", version), path, s.deps.Log, "Mod.preview")
}

func (s *Shell) previewImage(info mod.ModInfo, imageKey string, fallback *fairy.El) *fairy.El {
	if imageKey == "" {
		if info.Preview != nil && isPreviewVideo(*info.Preview) {
			if video := s.modVideo(*info.Preview, info.Mtime); video != nil {
				if video.State() == fairy.VideoLoading {
					return fairy.Spinner().Size(20, 20)
				}
				return fairy.VideoView(video).WFull().HFull()
			}
			return fairy.Col(
				fairy.Icon(lucide.Film).Size(48, 48).Color(kit.T().MutedForeground),
				fairy.Text(filepath.Base(*info.Preview)).FontSize(12).Color(kit.T().MutedForeground).Ellipsis(),
			).Gap(6).Align(fairy.Center)
		}
		return fallback
	}
	img := s.sizedModPreview(imageKey, *info.Preview, modPreviewMaxWidth, modCardHeight, fairy.FitContain)
	if img == nil {
		if s.previewImageErr(imageKey, *info.Preview, modPreviewMaxWidth, modCardHeight, fairy.FitContain) != nil {
			return fallback
		}
		return fairy.Spinner().Size(20, 20)
	}
	return fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain)
}

// modPreviewKey names an image of the mod in the cache by the mod's folder and the image's path inside it, which
// keeps a DISABLED variant folder within the mod apart. Toggling the mod or a folder above it only adds or drops
// the DISABLED prefix, so with the preview file's own version the folder leaves the prefix out and the cache
// outlives the rename. The preview version includes file identity to distinguish separate copies. The light
// listing has no such version and keys by the path.
func modPreviewKey(info mod.ModInfo, file string) string {
	if info.PreviewVersion == "" {
		return fmt.Sprint("mod:", file, ":", info.Mtime)
	}
	if rel, err := filepath.Rel(info.Path, file); err == nil {
		file = rel
	}
	folder := modname.StripDisabledPath(filepath.ToSlash(info.Path))
	return fmt.Sprint("mod:", folder, "|", file, ":", info.PreviewVersion)
}

// maxModPreviewPaths bounds the keys sizedModPreview tracks; past it the map starts over.
const maxModPreviewPaths = 4096

type modPreviewPath struct {
	file  string
	retry bool
}

// sizedModPreview loads the mod preview at file under key. The key outlives a toggle, but a load requested
// before the rename can fail at the old path. Keep the cached image while that read is pending, and retry
// failures even when Sized returns a smaller cached image instead of nil.
func (s *Shell) sizedModPreview(key, file string, w, h float32, fit fairy.Fit) *fairy.Image {
	read := func() ([]byte, error) { return os.ReadFile(file) }
	img := fairy.Images.Sized(key, w, h, fit, read)
	previous, seen := s.modPreviewPaths[key]
	changed := seen && !platform.SamePathFold(previous.file, file)
	if seen && !changed && !previous.retry {
		return img
	}

	if s.modPreviewPaths == nil || len(s.modPreviewPaths) >= maxModPreviewPaths {
		s.modPreviewPaths = map[string]modPreviewPath{}
	}
	previous.file = file
	previous.retry = previous.retry || changed
	if previous.retry && (img == nil || fairy.Images.SizedErr(key, w, h, fit) != nil) {
		fairy.Images.Forget(key)
		img = fairy.Images.Sized(key, w, h, fit, read)
		previous.retry = false
	}
	s.modPreviewPaths[key] = previous
	return img
}

// maxPreviewImageFailures bounds the logged keys kept; past it the set starts over and may log a key again.
const maxPreviewImageFailures = 1024

// previewImageErr returns the error of the failed fairy.Images.Sized load with these arguments and logs it the
// first time it is seen. The cards only show a placeholder for it, so the log is the only trace of the cause.
func (s *Shell) previewImageErr(key, path string, w, h float32, fit fairy.Fit) error {
	err := fairy.Images.SizedErr(key, w, h, fit)
	if err == nil || s.previewImageFailures[key] {
		return err
	}
	if s.previewImageFailures == nil || len(s.previewImageFailures) >= maxPreviewImageFailures {
		s.previewImageFailures = map[string]bool{}
	}
	s.previewImageFailures[key] = true
	_ = infra.ReportError(s.deps.Log, err, "Mod.preview", infra.Diagnostic{
		Severity:  infra.DiagnosticWarn,
		Operation: "preview.image",
		Fields:    map[string]any{"path": path, "width": w, "height": h, "deviceGeneration": fairy.DeviceGeneration()},
	})
	return err
}

func (s *Shell) modIniList(info mod.ModInfo) *fairy.El {
	t := kit.T()
	list := fairy.Col().Grow(1).MinH(0).ScrollY().Gap(8)
	groupFill, groupShadow, badgeBorder := fairy.RGBA(
		255,
		255,
		255,
		0.35,
	), fairy.RGBA(
		0,
		0,
		0,
		0.05,
	), fairy.RGBA(
		0,
		0,
		0,
		0.15,
	)
	if t.Dark {
		groupFill, badgeBorder = t.Foreground.Alpha(0.05), fairy.RGBA(255, 255, 255, 0.15)
	}
	for i, ini := range info.Inis {
		if i > 0 {
			list.Child(kit.Separator())
		}
		path := ini.Path
		groups := fairy.Col().Gap(10)
		for _, toggle := range ini.ToggleKeys {
			box := fairy.Col(fairy.Row(
				fairy.Text(itoa(len(toggle.Values))).FontSize(12).W(20).H(20).Center().TextAlign(fairy.TextCenter).
					Radius(4).Border(1, badgeBorder),
				fairy.Text(toggle.SectionName).Ellipsis().Grow(1).MinW(0).TooltipWhenTruncated(),
			).Gap(4).Align(fairy.Center)).Gap(4).Pad(4).Radius(8).Fill(groupFill).Shadow(0, 1, 2, 0, groupShadow)
			for _, binding := range []struct {
				variable, placeholder string
				value                 *string
			}{
				{"key", "page.mod.ini.add_key", toggle.Key},
				{"back", "page.mod.ini.add_back_key", toggle.Back},
			} {
				box.Child(s.modKeyTrigger(info, path, toggle.SectionName, binding.variable, deref(binding.value),
					T(binding.placeholder)))
			}
			groups.Child(box)
		}
		list.Child(fairy.Col(
			fairy.Row(
				fairy.Text(ini.Name).Opacity(0.8).Grow(1).MinW(0).Ellipsis(),
				kit.IconButton(lucide.FileCog, func() { s.openPath(path) }, kit.Ghost, kit.Small).
					Label(T("page.mod.ini.open_file")).Tooltip(T("page.mod.ini.open_file")),
			).Gap(4).Align(fairy.Center),
			groups,
		).Gap(4))
	}

	panel := fairy.Col().W(modIniWidth).Shrink(0).HFull().Gap(8).Pad(6).Radius(4).Fill(t.Background.Alpha(0.1)).
		FontSize(13)
	if s.deps.Tools != nil {
		panel.Child(s.togglePresetCardMenu(info))
	}
	return panel.Child(list)
}

// modKeyTrigger shows one key binding of a toggle section, or placeholder when it is unset, and opens the key
// dialog when clicked.
func (s *Shell) modKeyTrigger(info mod.ModInfo, iniPath, section, variable, value, placeholder string) *fairy.El {
	t := kit.T()
	border, fill, hover := fairy.RGBA(0, 0, 0, 0.05), fairy.RGBA(255, 255, 255, 0.3), fairy.RGBA(255, 255, 255, 0.6)
	chipFill, chipText := fairy.RGBA(0, 0, 0, 0.06), t.Foreground.Alpha(0.75)
	if t.Dark {
		border, fill, hover = fairy.RGBA(255, 255, 255, 0.2), t.Foreground.Alpha(0.05), t.Foreground.Alpha(0.1)
		chipFill, chipText = t.Muted, t.MutedForeground
	}
	row := fairy.Row().WFull().MinH(30).Pad(4).Gap(4).Align(fairy.Center).Radius(8).Border(1, border).Fill(fill).
		HoverFill(hover).Animate(motion.Fast).Cursor(fairy.CursorPointer).Focusable().Role(fairy.RoleButton).
		OnClick(func() { s.openModKeyEdit(info, iniPath, section, variable, value) })
	labels := keyLabelsOf(value)
	if len(labels) == 0 {
		return row.Label(placeholder).Child(
			fairy.Row(
				fairy.Icon(lucide.Plus).Size(12, 12).Shrink(0),
				fairy.Text(placeholder).FontSize(12).Ellipsis().MinW(0),
			).PadXY(6, 0).Gap(6).Align(fairy.Center).MinW(0).Color(t.MutedForeground),
		)
	}
	keys := fairy.Row().Gap(4).Wrap().Align(fairy.Center).MinW(0)
	for _, label := range labels {
		keys.Child(kit.Kbd(label).Fill(chipFill).Color(chipText))
	}
	return row.Label(variable+": "+strings.Join(labels, " ")).
		Child(fairy.Text(variable+":").FontSize(14).Shrink(0), keys)
}

func (s *Shell) modListHead() *fairy.El {
	t := kit.T()
	m := &s.mods
	column := func(field, label string, width float32, end bool) *fairy.El {
		ink := t.MutedForeground
		row := fairy.Row(fairy.Text(label).FontSize(14).LineHeight(20).NoWrap()).Gap(8).Align(fairy.Center)
		if m.sortType == field {
			ink = t.Primary
			icon := lucide.ArrowUp
			if m.sortDesc {
				icon = lucide.ArrowDown
			}
			row.Child(fairy.Icon(icon).Size(16, 16))
		}
		cell := fairy.Row(row).HFull().Align(fairy.Center).PadXY(8, 0).Color(ink).Cursor(fairy.CursorPointer).
			Role(fairy.RoleButton).Label(label).OnClick(func() {
			if m.sortType != field {
				s.setModSort(field)
				return
			}
			m.sortDesc = !m.sortDesc
		})
		if end {
			cell.Justify(fairy.JustifyEnd)
		}
		if width > 0 {
			return cell.W(width).Shrink(0)
		}
		return cell.Grow(1).MinW(0)
	}
	return fairy.Row(
		fairy.Box().W(modThumbColumn).Shrink(0),
		column("name", T("g.name"), 0, false).PadEdges(0, 8, 0, 12),
		column("size", T("g.size"), modSizeColumn, true),
		column("date", T("g.date"), modDateColumn, true).PadEdges(0, 12, 0, 8),
	).H(32).Shrink(0).WFull()
}

func (s *Shell) modRow(info mod.ModInfo) *fairy.El {
	t := kit.T()
	imageKey := func(file string) string { return modPreviewKey(info, file) }
	thumb := fairy.Box(s.previewThumb(info.Preview, info.Mtime, imageKey, 40, 4,
		fairy.Icon(lucide.Folder).Size(20, 20).Color(t.MutedForeground))).Size(40, 40).Radius(4).Fill(t.Muted).
		Center().Clip()
	if info.Preview != nil && isPreviewMedia(*info.Preview) {
		thumb.Fill(t.Secondary).Cursor(fairy.CursorPointer).Label(T("page.mod.context-menu.open-preview-lightbox")).
			OnClick(func() { s.openModLightbox(info) })
	}
	name := fairy.Row().Grow(1).MinW(0).PadXY(8, 0).Gap(4).Align(fairy.Center)
	for _, icon := range appliedToolIconsOf(info, t) {
		name.Child(icon)
	}
	name.Child(
		fairy.Text(info.Name).FontSize(14).LineHeight(20).Medium().Ellipsis().Grow(1).MinW(0).TooltipWhenTruncated(),
	)
	size, date := formatSize(int64(info.Size)), formatModTime(info.Mtime, false)
	if s.isDownloadPlaceholder(info.Path) {
		size, date = "—", "—"
	}
	row := fairy.Row(
		fairy.Box(thumb).W(modThumbColumn).Shrink(0).PadEdges(0, 0, 0, 8).Center(),
		name,
		fairy.Text(size).FontSize(14).LineHeight(20).NoWrap().Color(t.MutedForeground).W(modSizeColumn).Shrink(0).
			TextAlign(fairy.TextEnd).PadXY(8, 0),
		fairy.Text(date).FontSize(14).LineHeight(20).NoWrap().Color(t.MutedForeground).W(modDateColumn).Shrink(0).
			TextAlign(fairy.TextEnd).PadEdges(0, 12, 0, 8),
	).Key("mod.row:" + info.Path).H(modRowHeight).WFull().Align(fairy.Center).Cursor(fairy.CursorPointer).Focusable().
		Fill(modTint(s.dark, info.IsEnabled, len(missingDependencies(info)) > 0)).
		HoverFill(modTint(s.dark, info.IsEnabled, len(missingDependencies(info)) > 0).Mix(pick(s.dark,
			fairy.Hex(0xffffff), fairy.Hex(0x000000)), 0.1)).
		Animate(motion.Fast).
		Label(info.Name).Checked(info.IsEnabled).
		OnClick(func() { s.clickMod(info) }).
		OnKey(func(k keyboard.KeyEvent) bool { return s.modKey(info, k) })
	if s.isModDownloading(info.Path) {
		row.Fill(t.Muted).HoverFill(t.Muted).Cursor(fairy.CursorWait).Disabled(true)
		if snapshot := s.modDownloadTransfer(info.Path); snapshot != nil {
			row.Child(modDownloadOverlay(snapshot, true).Cursor(fairy.CursorDefault))
		}
		return row
	}
	if s.mods.mergeMode && s.mods.mergeSelected[info.Path] {
		row.Ring(2, t.Primary)
	}
	return kit.ContextMenu("mod.row:"+info.Path, row, func() []kit.MenuItem { return s.modMenuItems(info) },
		kit.Width(224))
}

// modKey toggles the focused mod alone with Ctrl+Enter; Enter and Space click it.
func (s *Shell) modKey(info mod.ModInfo, k keyboard.KeyEvent) bool {
	if !k.Is(keyboard.KeyEnter, keyboard.ModCtrl) {
		return false
	}
	if s.mods.mergeMode {
		s.clickMod(info)
		return true
	}
	s.toggleMod(info, true)
	return true
}
