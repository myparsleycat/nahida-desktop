package ui

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
)

const (
	folderRowHeight     float32 = 56
	folderSectionHeight float32 = 32
	// classifiedSubGroupDepth mirrors internal/mod/classifications.go: how many levels below a top-level folder
	// can carry an assignment.
	classifiedSubGroupDepth = 2
)

// folderRow is a line of the folder sidebar: a classification section header or a folder.
type folderRow struct {
	section    bool
	key        string
	name       string
	unassigned bool
	count      int
	shut       bool

	group        mod.FolderGroup
	depth        int
	parentName   string
	collapsePath string
}

func (r folderRow) rowKey() string {
	if r.section {
		return "section:" + r.key
	}
	return "mod.folder:" + r.group.Path
}

func (s *Shell) activeClassification() *mod.Classification {
	for i := range s.mods.classifications {
		if s.mods.classifications[i].Active {
			return &s.mods.classifications[i]
		}
	}
	return nil
}

func modCount(g mod.FolderGroup) int { return g.ModCount }

// visibleFolders filters and sorts folders as the sidebar menu asks.
func (s *Shell) visibleFolders(groups []mod.FolderGroup, less func(a, b string) bool) []mod.FolderGroup {
	m := &s.mods
	out := make([]mod.FolderGroup, 0, len(groups))
	for _, g := range groups {
		if !m.hideEmpty || modCount(g) > 0 || g.HasSubGroups || g.HasManualSubGroups {
			out = append(out, g)
		}
	}
	slices.SortStableFunc(out, func(a, b mod.FolderGroup) int {
		var c int
		switch m.folderSort {
		case "mod-count":
			c = cmp.Compare(a.ModCount, b.ModCount)
		case "enabled-mod-count":
			c = cmp.Compare(a.EnabledModCount, b.EnabledModCount)
		default:
			switch {
			case less(a.Name, b.Name):
				c = -1
			case less(b.Name, a.Name):
				c = 1
			}
		}
		if m.folderDesc {
			return -c
		}
		return c
	})
	return out
}

// folderRows builds the sidebar lines, mirroring buildVisibleSidebarRows of the React screen: expanded folders
// list their sub folders, collapsed ones only the folders marked as manual groups, and a search keeps the
// matching folders with the parents that lead to them.
func (s *Shell) folderRows() []folderRow {
	m := &s.mods
	less := platform.NewLocaleLess()
	query := strings.ToLower(strings.TrimSpace(m.sidebarSearch))
	searching := query != ""
	matches := func(g mod.FolderGroup) bool { return strings.Contains(strings.ToLower(g.Name), query) }
	classification := s.activeClassification()

	var classified []mod.FolderGroup
	classifiedPaths := map[string]bool{}
	if classification != nil {
		ids := map[string]bool{}
		for _, g := range classification.Groups {
			ids[g.ID] = true
		}
		for _, g := range m.characters {
			for _, sub := range g.ClassifiedSubGroups {
				if ids[sub.Classifications[classification.ID]] {
					classified = append(classified, sub)
					classifiedPaths[sub.Path] = true
				}
			}
		}
	}

	var rows []folderRow
	var visit func(g mod.FolderGroup, depth int, parentName, collapsePath string)
	visit = func(g mod.FolderGroup, depth int, parentName, collapsePath string) {
		showSub := m.expanded[g.Path] || searching && m.persistent[g.Path]
		var children []mod.FolderGroup
		switch {
		case showSub:
			children = s.subGroupsOf(g.Path, false)
		case g.HasManualSubGroups:
			children = s.subGroupsOf(g.Path, true)
		}
		if searching && !showSub {
			children = slices.DeleteFunc(slices.Clone(children), func(c mod.FolderGroup) bool { return !matches(c) })
		}
		children = slices.DeleteFunc(slices.Clone(children), func(c mod.FolderGroup) bool {
			return classifiedPaths[c.Path]
		})
		children = s.visibleFolders(children, less)
		showParent := !searching || matches(g)
		if !showParent && len(children) == 0 {
			return
		}
		if showParent {
			rows = append(rows, folderRow{group: g, depth: depth, parentName: parentName, collapsePath: collapsePath})
		}
		for _, child := range children {
			visit(child, depth+1, g.Name, g.Path)
		}
	}

	if classification == nil {
		for _, g := range s.visibleFolders(m.characters, less) {
			visit(g, 0, "", "")
		}
		return rows
	}

	listed := append(slices.Clone(m.characters), classified...)
	byPath := map[string]mod.FolderGroup{}
	for _, g := range listed {
		byPath[g.Path] = g
	}
	type section struct {
		key, name string
		groups    []mod.FolderGroup
	}
	sections := make([]*section, 0, len(classification.Groups)+1)
	byID := map[string]*section{}
	for _, g := range classification.Groups {
		sec := &section{key: classification.ID + ":" + g.ID, name: g.Name}
		sections = append(sections, sec)
		byID[g.ID] = sec
	}
	unassigned := &section{key: classification.ID + ":"}
	for _, g := range listed {
		if sec := byID[g.Classifications[classification.ID]]; sec != nil {
			sec.groups = append(sec.groups, g)
		} else {
			unassigned.groups = append(unassigned.groups, g)
		}
	}
	sections = append(sections, unassigned)
	for _, sec := range sections {
		visible := s.visibleFolders(sec.groups, less)
		if sec == unassigned && len(visible) == 0 {
			continue
		}
		// Searching ignores collapse so matches inside a collapsed section stay reachable.
		shut := !searching && m.collapsed[sec.key]
		header := len(rows)
		rows = append(rows, folderRow{
			section: true, key: sec.key, name: sec.name, unassigned: sec == unassigned, count: len(visible), shut: shut,
		})
		if shut {
			continue
		}
		for _, g := range visible {
			parent := ""
			if classifiedPaths[g.Path] {
				parent = listedParentName(g.Path, byPath)
			}
			visit(g, 0, parent, "")
		}
		if searching && len(rows) == header+1 {
			rows = rows[:header]
		}
	}
	return rows
}

// listedParentName names the parent of a classified sub folder. A listed parent keeps the DISABLED prefix the
// backend left in its name when a sibling has the same name without it.
func listedParentName(path string, listed map[string]mod.FolderGroup) string {
	parent := parentPath(path)
	if g, ok := listed[parent]; ok {
		return g.Name
	}
	if parent == "" {
		return ""
	}
	return modname.StripDisabled(parentBase(parent))
}

func parentBase(path string) string {
	i := strings.LastIndexAny(path, `\/`)
	return path[i+1:]
}

// canAssignClassification reports whether the backend accepts an assignment for the folder at path.
func canAssignClassification(modRoot, path string) bool {
	split := func(value string) []string {
		return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return r == '\\' || r == '/' })
	}
	root, target := split(modRoot), split(path)
	levels := len(target) - len(root)
	return levels >= 1 && levels <= classifiedSubGroupDepth+1 && slices.Equal(root, target[:len(root)])
}

func (s *Shell) modSidebar(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	m := &s.mods
	width := s.modSidebarWidth()
	rows := s.folderRows()

	// A folder selected other than by a click on it, such as by a download or the search, is scrolled into
	// view once it is listed.
	if m.group != nil && m.scrolledFolder != m.group.Path {
		if i := s.folderListIndex(rows, m.group.Path); i >= 0 {
			m.scrolledFolder = m.group.Path
			s.win.ScrollToRow("mod.folders", i)
		}
	}

	var list *fairy.El
	switch {
	case m.charactersBusy && len(m.characters) == 0 && s.folderGridLayout():
		list = folderGridSkeleton(width)
	case m.charactersBusy && len(m.characters) == 0:
		skeleton := fairy.Col().Key("mod.folders").Grow(1)
		for range 8 {
			skeleton.Child(fairy.Row(
				kit.Skeleton().Size(40, 40).Radius(20),
				kit.Skeleton().H(16).Grow(1),
				kit.Skeleton().Size(32, 16),
			).H(folderRowHeight).PadEdges(8, 16, 8, 8).Gap(12).Align(fairy.Center))
		}
		list = skeleton
	case s.folderGridLayout():
		list = s.folderGrid(rows)
	default:
		list = fairy.VirtualListMeasured(len(rows), folderRowHeight,
			func(i int) string { return rows[i].rowKey() },
			func(i int) *fairy.El { return s.folderRowView(rows[i]) },
		).Key("mod.folders").Grow(1).MinH(0)
	}

	handleHover := fairy.Local[bool]("mod.sidebar.resize")
	line := fairy.Box().W(1).HFull().Fill(t.Border).Animate(motion.Fast)
	switch {
	case s.win.Focused("mod.sidebar.resize"):
		line.Fill(t.Primary)
	case *handleHover:
		line.Fill(t.Primary.Alpha(0.7))
	}
	handle := fairy.Box(line).Key("mod.sidebar.resize").
		Absolute().Top(0).Bottom(0).Right(-2).W(5).Center().Cursor(fairy.CursorResizeEW).
		Role(fairy.RoleSeparator).Label(T("page.mod.sidebar.resize")).Focusable().
		RangeValue(float64(width), float64(modSidebarWidthMin), float64(modSidebarWidthMax), 16, nil).
		OnHover(func(on bool) { *handleHover = on }).
		OnKey(s.resizeModSidebarKey).
		OnDrag(func(e fairy.DragEvent) { s.resizeModSidebar(e) })

	return fairy.Col(
		s.folderToolbar(),
		list,
		s.gameSelector(),
		handle,
	).Key("mod.sidebar").W(width).Shrink(0).HFull().BorderEdges(0, 1, 0, 0, t.Border).
		Fill(t.Background.Alpha(0.5))
}

func (s *Shell) resizeModSidebar(e fairy.DragEvent) {
	m := &s.mods
	switch e.Phase {
	case fairy.DragStart:
		m.resizeFrom = s.modSidebarWidth()
		m.sidebarWidth = m.resizeFrom
	case fairy.DragMove:
		m.sidebarWidth = min(max(float32(int(m.resizeFrom+e.DX)), modSidebarWidthMin), modSidebarWidthMax)
	case fairy.DragEnd:
		if e.Canceled {
			m.sidebarWidth = 0
			return
		}
		s.setSetting(setting.KeyModCharacterSidebarWidth, int(m.sidebarWidth))
		m.sidebarWidth = 0
	}
}

func (s *Shell) folderToolbar() *fairy.El {
	t := kit.T()
	m := &s.mods
	search := fairy.Row(
		fairy.Input(m.sidebarSearch, s.setFolderSearch).
			Key("mod.folder-search").
			Placeholder(T("g.search")).
			FontSize(14).
			LineHeight(20).
			PlaceholderColor(t.MutedForeground).
			Grow(1).
			MinW(0).
			Label(T("g.search")),
		fairy.Icon(lucide.Search).Size(16, 16).Color(t.MutedForeground),
	).H(32).Grow(1).MinW(0).PadEdges(0, 8, 0, 10).Gap(6).Align(fairy.Center).Radius(t.RadiusMd()).
		Fill(inputFill(t)).Border(1, t.Border)
	return fairy.Row(search, s.folderMenu()).H(48).Shrink(0).PadXY(8, 0).Gap(4).Align(fairy.Center)
}

// setFolderSearch filters the folders. When a pause in typing leaves one folder matching, it is selected.
func (s *Shell) setFolderSearch(value string) {
	m := &s.mods
	m.sidebarSearch = value
	query := strings.ToLower(strings.TrimSpace(value))
	if query == "" {
		return
	}
	s.markDownloadPick()
	time.AfterFunc(300*time.Millisecond, func() {
		fairy.Go(func() {
			if m.sidebarSearch != value {
				return
			}
			var match *mod.FolderGroup
			for _, row := range s.folderRows() {
				if row.section || !strings.Contains(strings.ToLower(row.group.Name), query) {
					continue
				}
				if match != nil {
					return
				}
				group := row.group
				match = &group
			}
			if match != nil {
				s.selectGroup(match)
			}
		})
	})
}

func (s *Shell) folderMenu() *fairy.El {
	m := &s.mods
	game := s.gameConfig()
	sortItem := func(key, label string) kit.MenuItem {
		return kit.MenuItem{
			Label:   label,
			Radio:   true,
			Checked: m.folderSort == key,
			OnClick: func() { m.folderSort = key },
		}
	}
	classificationItems := []kit.MenuItem{{
		Label: T("page.mod.character-sidebar.classification.none"), Radio: true,
		Checked: s.activeClassification() == nil, OnClick: func() { s.setActiveClassification(nil) },
	}}
	for _, c := range m.classifications {
		id := c.ID
		classificationItems = append(classificationItems, kit.MenuItem{
			Label: c.Name, Radio: true, Checked: c.Active, OnClick: func() { s.setActiveClassification(&id) },
		})
	}
	classificationItems = append(classificationItems, kit.MenuItem{Separator: true}, kit.MenuItem{
		Label: T("page.mod.character-sidebar.classification.manage"), Icon: lucideIcon("settings-2"),
		Disabled: m.game == "", OnClick: s.openClassificationEditor,
	})
	items := []kit.MenuItem{
		{
			Label: T("page.mod.character-sidebar.create-folder"), Icon: lucide.FolderPlus, Disabled: game == nil,
			OnClick: func() {
				if game := s.gameConfig(); game != nil {
					s.openFolderDraft(mod.FolderGroup{Name: game.Game, Path: game.ModFolderPath})
				}
			},
		},
		{Separator: true},
		{
			Label:        T("page.mod.character-sidebar.sort.label"),
			Icon:         lucide.ArrowUpDown,
			SubmenuWidth: 192,
			Submenu: []kit.MenuItem{
				{Label: T("page.mod.character-sidebar.sort.field"), Heading: true},
				sortItem("name", T("page.mod.character-sidebar.sort.name")),
				sortItem("mod-count", T("page.mod.character-sidebar.sort.mod-count")),
				sortItem("enabled-mod-count", T("page.mod.character-sidebar.sort.enabled-mod-count")),
				{Separator: true},
				{Label: T("page.mod.character-sidebar.sort.direction"), Heading: true},
				{
					Label: T("page.mod.character-sidebar.sort.ascending"), Icon: lucide.ArrowUp, Radio: true,
					Checked: !m.folderDesc, OnClick: func() { m.folderDesc = false },
				},
				{
					Label: T("page.mod.character-sidebar.sort.descending"), Icon: lucide.ArrowDown, Radio: true,
					Checked: m.folderDesc, OnClick: func() { m.folderDesc = true },
				},
			},
		},
		{
			Label: T("page.mod.character-sidebar.classification.label"), Icon: lucide.Tags, SubmenuWidth: 192,
			Submenu: classificationItems,
		},
		{Separator: true},
		{
			Label: T("page.mod.character-sidebar.hide-empty-folders"), Icon: lucide.FolderX, Checked: m.hideEmpty,
			OnClick: func() { m.hideEmpty = !m.hideEmpty },
		},
		s.folderLayoutItem(),
	}
	label := T("page.mod.character-sidebar.more-options")
	return kit.DropdownMenu("mod.folder-menu", kit.IconButton(lucide.Ellipsis, nil, kit.Ghost, kit.Tooltip(label)).
		Label(label), items, kit.Width(224), kit.AlignEnd)
}

func (s *Shell) setActiveClassification(id *string) {
	game := s.mods.game
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.SetActiveClassification(ctx, game, id)
		if err != nil {
			s.modError(err, "set-active-classification", map[string]any{"game": game})
		}
		fairy.Go(func() {
			if err != nil {
				s.showClassificationError(err)
			}
			s.loadClassifications()
		})
	})
}

func (s *Shell) showClassificationError(err error) {
	code := "failed"
	for _, candidate := range []string{
		"INVALID_CLASSIFICATION_NAME", "CLASSIFICATION_NAME_EXISTS", "CLASSIFICATION_GROUP_NAME_EXISTS",
		"CLASSIFICATION_NOT_FOUND", "INVALID_CLASSIFICATION_PATH",
	} {
		if strings.Contains(err.Error(), candidate) {
			code = candidate
			break
		}
	}
	s.toasts.Show(kit.ToastError, T("page.mod.hooks.use-mod-mutations.classification."+code), "")
}

func (s *Shell) folderRowView(row folderRow) *fairy.El {
	t := kit.T()
	m := &s.mods
	if row.section {
		name := row.name
		if row.unassigned {
			name = T("page.mod.character-sidebar.classification.unassigned")
		}
		chevron := lucide.ChevronDown
		if row.shut {
			chevron = lucide.ChevronRight
		}
		key := row.key
		return fairy.Row(
			fairy.Icon(chevron).Size(16, 16),
			fairy.Text(name).Ellipsis().Grow(1).MinW(0),
			fairy.Text(itoa(row.count)),
		).H(folderSectionHeight).WFull().PadXY(8, 0).Gap(4).Align(fairy.Center).FontSize(12).SemiBold().
			Color(t.MutedForeground).HoverColor(t.Foreground).Cursor(fairy.CursorPointer).Expanded(!row.shut).
			OnClick(func() { m.collapsed[key] = !m.collapsed[key] })
	}

	g := row.group
	selected := m.group != nil && m.group.Path == g.Path
	disabled := !g.IsEnabled
	ink := t.Foreground
	if disabled {
		ink = t.MutedForeground
	}
	avatar := fairy.Box(s.previewThumb(g.Preview, s.mods.previewVersion, 40, 20, fairy.Text("?").Bold())).
		Size(40, 40).Shrink(0).Radius(20).Fill(t.Muted).Center().Clip()
	if disabled {
		avatar.Opacity(0.5)
	}
	name := fairy.Col(fairy.Text(g.Name).Ellipsis().Color(ink).FontSize(14).LineHeight(20)).Grow(1).MinW(0)
	if row.depth == 0 && row.parentName != "" {
		name.Child(fairy.Text(row.parentName).Ellipsis().FontSize(12).LineHeight(16).Color(t.MutedForeground))
	}
	count := fairy.Text(itoa(g.EnabledModCount) + " / " + itoa(g.ModCount)).
		FontSize(12).
		LineHeight(16).
		Color(t.MutedForeground).
		NoWrap()

	indent := float32(8)
	if row.depth > 0 {
		indent = float32(row.depth*16 + 8)
	}
	item := fairy.Row(avatar, name, count).Key(row.rowKey()).H(folderRowHeight).WFull().
		PadEdges(8, 16, 8, indent).Gap(12).Align(fairy.Center).HoverFill(hoverFill(t)).Animate(motion.Fast).
		Cursor(fairy.CursorPointer).Selected(selected).Expanded(m.expanded[g.Path]).Label(g.Name).
		OnClick(func() { s.clickFolder(row) }).
		OnFileDrop(func(drop fairy.FileDrop) { s.dropFolderPreview(g, drop.Paths) }).
		DropFill(t.Accent.Alpha(0.12)).DropBorder(t.Accent)
	if selected {
		item.Fill(t.Muted)
	}
	if m.dropTarget == row.rowKey() {
		item.Child(folderDropHint(g.Name))
	}
	if row.depth > 0 {
		item = fairy.Box(
			fairy.Box().
				Absolute().
				Top(0).
				Bottom(0).
				Left(float32((row.depth-1)*16+16)).
				W(1).
				Fill(t.Border.Alpha(t.Border.A*0.5)),
			item,
		).WFull()
	}
	return kit.ContextMenu("mod.folder:"+g.Path, item, func() []kit.MenuItem { return s.folderMenuItems(g) },
		kit.Width(224))
}

// clickFolder follows the React sidebar: Alt toggles the row's parent open, a click on an open folder closes
// it, Ctrl opens and selects it, and any other click selects it.
func (s *Shell) clickFolder(row folderRow) {
	m := &s.mods
	g := row.group
	mods := s.win.Mods()
	searching := strings.TrimSpace(m.sidebarSearch) != ""
	if mods&keyboard.ModAlt != 0 {
		path := cmp.Or(row.collapsePath, g.Path)
		m.expanded[path] = !m.expanded[path]
		return
	}
	if !searching && mods&keyboard.ModCtrl == 0 && (m.persistent[g.Path] || m.expanded[g.Path]) {
		m.expanded[g.Path] = !m.expanded[g.Path]
		return
	}
	if mods&keyboard.ModCtrl != 0 {
		m.expanded[g.Path] = true
	}
	if row.collapsePath != "" && searching && m.persistent[row.collapsePath] {
		m.expanded[row.collapsePath] = true
	}
	s.markDownloadPick()
	m.scrolledFolder = g.Path
	s.selectGroup(&g)
	if searching {
		m.sidebarSearch = ""
		path := g.Path
		fairy.Go(func() {
			if i := s.folderListIndex(s.folderRows(), path); i >= 0 {
				s.win.ScrollToRow("mod.folders", i)
			}
		})
	}
}

func (s *Shell) folderMenuItems(g mod.FolderGroup) []kit.MenuItem {
	m := &s.mods
	togglingFolder := m.busyPaths[g.Path]
	busy := m.bulkBusy || togglingFolder
	items := []kit.MenuItem{
		{
			Label:    T("page.mod.all_enabled"),
			Icon:     lucide.Circle,
			Loading:  m.bulkBusy,
			Disabled: busy,
			OnClick:  func() { s.setAllMods(g.Path, true) },
		},
		{
			Label:    T("page.mod.all_disabled"),
			Icon:     lucide.CircleOff,
			Loading:  m.bulkBusy,
			Disabled: busy,
			OnClick:  func() { s.setAllMods(g.Path, false) },
		},
	}
	if !s.isNteGame() {
		label, icon := T("page.mod.character-sidebar.disable-folder"), lucide.CircleOff
		if !g.IsEnabled {
			label, icon = T("page.mod.character-sidebar.enable-folder"), lucide.Circle
		}
		items = append(items, kit.MenuItem{
			Label: label, Icon: icon, Loading: togglingFolder, Disabled: busy,
			OnClick: func() { s.toggleFolder(g) },
		})
	}
	items = append(items,
		kit.MenuItem{Separator: true},
		kit.MenuItem{
			Label: T("page.mod.character-sidebar.open-in-explorer"), Icon: lucide.Folder,
			OnClick: func() { s.openPath(g.Path) },
		},
		kit.MenuItem{
			Label: T("page.mod.character-sidebar.create-folder"), Icon: lucide.FolderPlus,
			OnClick: func() { s.openFolderDraft(g) },
		},
		kit.MenuItem{
			Label: T("page.mod.character-sidebar.delete-folder"), Icon: lucide.Trash, Danger: true,
			OnClick: func() { s.deleteFolder(g) },
		},
		kit.MenuItem{Separator: true},
	)
	if !m.persistent[g.Path] {
		label, icon := T("page.mod.character-sidebar.expand-subgroups"), lucide.FolderTree
		if m.expanded[g.Path] {
			label, icon = T("page.mod.character-sidebar.collapse-subgroups"), lucide.FolderMinus
		}
		items = append(
			items,
			kit.MenuItem{Label: label, Icon: icon, OnClick: func() { m.expanded[g.Path] = !m.expanded[g.Path] }},
		)
	}
	persist, persistIcon := T("page.mod.character-sidebar.persist-subgroups"), lucide.FolderTree
	if m.persistent[g.Path] {
		persist, persistIcon = T("page.mod.character-sidebar.unpersist-subgroups"), lucide.FolderMinus
	}
	items = append(
		items,
		kit.MenuItem{Label: persist, Icon: persistIcon, OnClick: func() { s.togglePersistent(g.Path) }},
	)
	if g.IsManualSubGroup {
		items = append(items, kit.MenuItem{
			Label: T("page.mod.character-sidebar.unmark-manual-subgroup"), Icon: lucide.FolderMinus,
			OnClick: func() { s.setManualSubGroup(g.Path, false) },
		})
	}
	if sub := s.classificationMenu(g); sub != nil {
		items = append(items, kit.MenuItem{Separator: true}, *sub)
	}
	if fixer := s.modFixerMenu(g.Path); fixer != nil {
		items = append(items, kit.MenuItem{Separator: true}, *fixer)
	}
	return items
}

// classificationMenu assigns the folder to one group per classification. A folder too deep to assign keeps the
// menu while it carries an assignment, so it can be cleared.
func (s *Shell) classificationMenu(g mod.FolderGroup) *kit.MenuItem {
	m := &s.mods
	canAssign := true
	if game := s.gameConfig(); game != nil {
		canAssign = canAssignClassification(game.ModFolderPath, g.Path)
	}
	assigned := false
	for _, c := range m.classifications {
		if g.Classifications[c.ID] != "" {
			assigned = true
		}
	}
	if len(m.classifications) == 0 || !canAssign && !assigned {
		return nil
	}
	var subs []kit.MenuItem
	for _, c := range m.classifications {
		current := g.Classifications[c.ID]
		known := slices.ContainsFunc(c.Groups, func(item mod.ClassificationGroup) bool { return item.ID == current })
		classificationID := c.ID
		items := []kit.MenuItem{{
			Label: T("page.mod.character-sidebar.classification.unassigned"), Radio: true, Checked: !known,
			OnClick: func() { s.assignClassification(g.Path, classificationID, nil) },
		}}
		for _, item := range c.Groups {
			id := item.ID
			items = append(items, kit.MenuItem{
				Label: item.Name, Radio: true, Checked: known && current == id, Disabled: !canAssign,
				OnClick: func() { s.assignClassification(g.Path, classificationID, &id) },
			})
		}
		subs = append(subs, kit.MenuItem{Label: c.Name, Submenu: items})
	}
	return &kit.MenuItem{Label: T("page.mod.character-sidebar.classification.assign"), Icon: lucide.Tags, Submenu: subs}
}

func (s *Shell) assignClassification(path, classificationID string, groupID *string) {
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.SetCharacterClassification(ctx, path, classificationID, groupID)
		fairy.Go(func() {
			if err != nil {
				s.showClassificationError(err)
				if strings.Contains(err.Error(), "CLASSIFICATION_NOT_FOUND") {
					s.loadClassifications()
				}
				return
			}
			s.refreshFolders()
		})
	})
}

func (s *Shell) togglePersistent(path string) {
	m := &s.mods
	if m.persistent[path] {
		delete(m.persistent, path)
	} else {
		m.persistent[path] = true
		m.expanded[path] = true
	}
	s.savePersistentGroups()
}

func (s *Shell) setAllMods(path string, enabled bool) {
	m := &s.mods
	if m.bulkBusy {
		return
	}
	m.bulkBusy = true
	s.background(func(ctx context.Context) {
		var err error
		if enabled {
			err = s.deps.Mod.EnableAll(ctx, path)
		} else {
			err = s.deps.Mod.DisableAll(ctx, path)
		}
		fairy.Go(func() {
			m.bulkBusy = false
			if err != nil {
				s.toasts.Show(kit.ToastError, err.Error(), "")
			}
			if m.group != nil && m.group.Path == path {
				s.refreshListing()
			}
			if _, ok := m.subGroups[path]; ok {
				s.loadSubGroups(path, false)
			}
		})
	})
}

// toggleFolder renames the folder with or without the DISABLED prefix and follows the new path at once, as the
// route drops a selection missing from the listed folders.
func (s *Shell) toggleFolder(g mod.FolderGroup) {
	m := &s.mods
	m.busyPaths[g.Path] = true
	s.background(func(ctx context.Context) {
		newPath, err := s.deps.Mod.ToggleFolder(ctx, g.Path)
		fairy.Go(func() {
			delete(m.busyPaths, g.Path)
			if err != nil {
				s.showModToggleError(err)
				return
			}
			for i := range m.characters {
				if m.characters[i].Path == g.Path {
					m.characters[i].Path, m.characters[i].IsEnabled = newPath, !m.characters[i].IsEnabled
				}
			}
			s.renameFolderPath(g.Path, newPath)
			s.refreshFolders()
			s.refreshFixInspections()
		})
	})
}

func (s *Shell) setManualSubGroup(path string, enabled bool) {
	m := &s.mods
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.SetManualSubGroup(ctx, path, enabled)
		fairy.Go(func() {
			if err != nil {
				s.toasts.Show(kit.ToastError, err.Error(), "")
				return
			}
			if !enabled && m.group != nil && m.group.Path == path {
				parent := parentPath(path)
				s.selectGroup(&mod.FolderGroup{Name: modname.StripDisabled(parentBase(parent)), Path: parent,
					IsEnabled: !modname.IsDisabled(parentBase(parent))})
			}
			clear(m.manualGroups)
			s.refreshFolders()
			s.refreshListing()
		})
	})
}

func (s *Shell) deleteFolder(g mod.FolderGroup) {
	s.confirmTrash(g.Path, T("page.mod.dialog.delete-folder.title"),
		T("page.mod.dialog.delete-folder.description", "name", g.Name),
		T("page.mod.dialog.delete-folder.#.failed"), func() {
			if parent := parentPath(g.Path); parent != "" {
				if _, ok := s.mods.subGroups[parent]; ok {
					s.loadSubGroups(parent, false)
				}
			}
			s.refreshFolders()
			s.refreshFixInspections()
		})
}

// dropFolderPreview makes a dropped image or video the folder's preview, asking first when it has its own.
func (s *Shell) dropFolderPreview(g mod.FolderGroup, paths []string) {
	if len(paths) != 1 || !isPreviewMedia(paths[0]) {
		s.toasts.Show(kit.ToastWarning, T("page.mod.toast.preview-drop.unsupported"), "")
		return
	}
	path := paths[0]
	if !hasOwnPreview(g.Path, g.Preview) {
		s.saveFolderPreview(g, path)
		return
	}
	s.Confirm(Confirmation{
		Title:       T("page.mod.dialog.overwrite-preview.title"),
		Description: T("page.mod.dialog.overwrite-preview.description", "name", g.Name),
		OnResult: func(confirmed bool) {
			if confirmed {
				s.saveFolderPreview(g, path)
			}
		},
	})
}

func (s *Shell) saveFolderPreview(g mod.FolderGroup, file string) {
	m := &s.mods
	s.background(func(ctx context.Context) {
		_, err := s.deps.Mod.PastePreview(ctx, g.Path, file, "path", g.Preview)
		if err != nil {
			s.modError(err, "drop-folder-preview", map[string]any{"path": g.Path, "file": file})
		}
		fairy.Go(func() {
			if err != nil {
				s.toasts.Show(kit.ToastError, T("page.mod.toast.preview-drop.error"), "")
				return
			}
			m.previewVersion++
			if parent := parentPath(g.Path); parent != "" {
				if _, ok := m.subGroups[parent]; ok {
					s.loadSubGroups(parent, false)
				}
				if _, ok := m.manualGroups[parent]; ok {
					s.loadSubGroups(parent, true)
				}
			}
			s.refreshFolders()
			s.refreshListing()
		})
	})
}

type folderDraft struct {
	parent mod.FolderGroup
	name   string
	busy   bool
}

func (s *Shell) openFolderDraft(parent mod.FolderGroup) {
	s.mods.folderDraft = &folderDraft{parent: parent}
}

// folderNameError checks a trimmed folder name before it is sent; the backend checks the Windows naming rules.
func folderNameError(name string) string {
	switch {
	case name == "":
		return T("page.mod.dialog.create-folder.#.invalid-name")
	case len([]rune(name)) > 255:
		return T("page.mod.dialog.create-folder.#.too-long")
	}
	return ""
}

func (s *Shell) folderDraftDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.folderDraft
	if d == nil {
		return
	}
	name := strings.TrimSpace(d.name)
	invalid := ""
	if d.name != "" {
		invalid = folderNameError(name)
	}
	submit := func(string) {
		if d.busy || name == "" || folderNameError(name) != "" {
			return
		}
		d.busy = true
		parent := d.parent.Path
		s.background(func(ctx context.Context) {
			path, err := s.deps.FS.Mkdir(parent, name)
			fairy.Go(func() {
				d.busy = false
				if err != nil {
					s.showCreateFolderError(err)
					return
				}
				m.folderDraft = nil
				m.expanded[parent] = true
				if _, ok := m.subGroups[parent]; ok {
					s.loadSubGroups(parent, false)
				}
				s.refreshFolders()
				s.selectGroup(&mod.FolderGroup{Name: name, Path: path, IsEnabled: true})
			})
		})
	}
	field := kit.TextField(d.name, func(v string) { d.name = v },
		kit.Placeholder(T("page.mod.dialog.create-folder.name-placeholder")), kit.Invalid(invalid != ""),
		kit.Disabled(d.busy))
	field.AutoFocus().OnSubmit(submit)
	body := fairy.Col(field).Gap(6)
	if invalid != "" {
		body.Child(fairy.Text(invalid).FontSize(12).Color(kit.T().Destructive))
	}
	close := func() {
		if !d.busy {
			m.folderDraft = nil
		}
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T("page.mod.dialog.create-folder.title"),
		Description: T("page.mod.dialog.create-folder.description", "name", d.parent.Name),
		Body:        body,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T("g.create"), func() { submit(d.name) }, kit.Loading(d.busy),
				kit.Disabled(name == "" || invalid != "")),
		},
		OnClose: close,
		Key:     "mod.create-folder",
	})
}

func (s *Shell) showCreateFolderError(err error) {
	switch {
	case errors.Is(err, platform.ErrInvalidGroupName), strings.Contains(err.Error(), "INVALID_WINDOWS_FILENAME"):
		s.toasts.Show(kit.ToastError, T("page.mod.dialog.create-folder.#.invalid-name"), "")
	case strings.Contains(err.Error(), "ALREADY_EXISTS"):
		s.toasts.Show(kit.ToastWarning, T("page.mod.dialog.create-folder.#.already-exists"), "")
	default:
		s.toasts.Show(kit.ToastError, T("page.mod.dialog.create-folder.#.failed"), "")
	}
}

// switchModSearch moves focus between the folder search and the mod search, starting at the folder search.
func (s *Shell) switchModSearch() {
	if s.win.Focused("mod.folder-search") && s.mods.group != nil {
		s.win.Focus("mod.search")
		return
	}
	s.win.Focus("mod.folder-search")
}
