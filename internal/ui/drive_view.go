package ui

import (
	"context"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
)

const (
	driveRowHeight    float32 = 61
	driveSizeMinWidth float32 = 88
	driveDateMinWidth float32 = 128
	driveGridCell     float32 = 168
	driveGridGap      float32 = 16
	driveImageTimeout         = 20 * time.Second
	driveImageMaxSize         = 32 << 20
	driveConflictView         = 6
)

var driveFolderColor = fairy.Hex(0xfacc15)

type driveColumns struct{ size, date float32 }

func (s *Shell) drivePage(cx *fairy.Cx, route Route) *fairy.El {
	t := kit.T()
	v := s.driveView(route)
	s.showDrive(v)
	s.showDriveDialogs(cx, v)

	var body *fairy.El
	switch {
	case v.loadErr != nil && (v.listing == nil || v.listed != s.driveFolder(v)):
		body = s.driveError(v)
	case v.listing == nil:
		body = fairy.Col(fairy.Spinner().Size(28, 28)).WFull().HFull().Center()
	default:
		body = fairy.Col(
			fairy.Row(s.driveBreadcrumb(v), s.driveHeadButtons(v)).H(48).Shrink(0).WFull().PadXY(8, 0).Gap(8).
				Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border),
			s.driveContent(cx, v),
		).WFull().HFull()
	}
	return body.OnKey(func(k keyboard.KeyEvent) bool { return s.driveKey(v, k) })
}

func (s *Shell) driveError(v *driveView) *fairy.El {
	t := kit.T()
	return fairy.Col(
		fairy.Icon(lucide.ServerCrash).Size(48, 48).Color(t.MutedForeground),
		fairy.Text(v.loadErr.Error()).Color(t.MutedForeground).TextAlign(fairy.TextCenter).MaxW(480),
		kit.Button(T("page.drive.import.retry"), func() { s.loadDriveFolder(v) }, kit.Outline,
			kit.Icon(lucide.RefreshCw)),
	).WFull().HFull().Center().Gap(12).Pad(24)
}

func (s *Shell) driveBreadcrumb(v *driveView) *fairy.El {
	t := kit.T()
	crumbs := s.driveAncestors(v)
	current := crumbs[len(crumbs)-1]
	row := fairy.Row().Grow(1).MinW(0).Gap(4).Align(fairy.Center)
	if len(crumbs) > 1 {
		back := T("page.drive.import.parent_folder")
		row.Child(kit.IconButton(lucide.ChevronLeft, func() { s.driveBack(v, crumbs) }, kit.Ghost,
			kit.Tooltip(back)).Label(back))
	}
	menu := make([]kit.MenuItem, 0, len(crumbs))
	for i := len(crumbs) - 1; i >= 0; i-- {
		crumb := crumbs[i]
		menu = append(menu, kit.MenuItem{
			Label:   crumb.Name,
			Icon:    lucide.Folder,
			Checked: crumb.ID == current.ID,
			OnClick: func() { s.openDriveCrumb(v, crumbs, crumb.ID) },
		})
	}
	trigger := fairy.Row(
		fairy.Icon(lucide.Folder).Size(16, 16).Shrink(0),
		fairy.Text(current.Name).Medium().Ellipsis().MinW(0).TooltipWhenTruncated(),
		fairy.Icon(lucide.ChevronDown).Size(16, 16).Shrink(0).Opacity(0.5),
	).H(32).MinW(0).PadXY(9, 0).Gap(14).Align(fairy.Center).Radius(t.RadiusLg()).HoverFill(hoverFill(t)).
		Cursor(fairy.CursorPointer).Label(current.Name)
	return row.Child(kit.DropdownMenu("drive.crumbs", trigger, menu, kit.MaxWidth(400)).MinW(0))
}

func (s *Shell) driveHeadButtons(v *driveView) *fairy.El {
	t := kit.T()
	d := &s.drive
	folder := s.driveFolder(v)
	canSubdirs := folder != shareRootID
	placeholder := T("page.drive.head_buttons.search_in_dir_placeholder")
	if canSubdirs && d.includeSubdirs {
		placeholder = T("page.drive.head_buttons.search_in_subdirs_placeholder")
	}
	search := fairy.Row(
		fairy.Icon(lucide.Search).Size(16, 16).Color(t.MutedForeground).Shrink(0),
		fairy.Input(v.search, func(text string) { s.setDriveSearch(v, text) }).
			Key(driveSearchKey).
			FontSize(14).
			LineHeight(20).
			Placeholder(placeholder).
			PlaceholderColor(t.MutedForeground).
			Label(placeholder).
			Grow(1).
			MinW(0),
	).W(200).H(36).PadEdges(0, 10, 0, 8).Gap(4).Align(fairy.Center).Radius(t.RadiusMd()).
		Border(1, t.Border)
	row := fairy.Row(search).Shrink(0).Gap(4).Align(fairy.Center)
	if canSubdirs {
		label := T("page.drive.head_buttons.search_include_subdirs")
		toggle := kit.IconButton(lucide.FolderTree, func() {
			d.includeSubdirs = !d.includeSubdirs
			s.searchDrive(v)
		}, kit.Ghost, kit.Tooltip(label)).Size(36, 36).Label(label).Checked(d.includeSubdirs)
		if d.includeSubdirs {
			toggle.Fill(t.Muted).Color(t.Primary)
		} else {
			toggle.Color(t.MutedForeground)
		}
		row.Child(toggle)
	}

	importLabel := T("page.drive.head_buttons.import")
	row.Child(kit.IconButton(lucide.Link, func() { s.openDriveImport(v, "") }, kit.Ghost,
		kit.Tooltip(importLabel)).Label(importLabel).MarginEdges(0, 0, 0, 4))
	layoutIcon := lucide.LayoutGrid
	if d.grid {
		layoutIcon = lucide.List
	}
	row.Child(kit.IconButton(layoutIcon, func() { d.grid = !d.grid }, kit.Ghost))

	download := T("page.drive.head_buttons.dropdown_menu.download")
	row.Child(kit.DropdownMenu("drive.download", kit.IconButton(lucide.Download, nil, kit.Ghost).Label(download),
		[]kit.MenuItem{{Label: download, Icon: lucide.Download, OnClick: func() {
			s.downloadDriveItems(v.selectedItems(s.driveDisplay(v)))
		}}}))

	prefix := "page.drive.head_buttons.dropdown_menu.make_new."
	noUpload := !canUploadTo(folder) || v.pickBusy
	row.Child(kit.DropdownMenu("drive.new", kit.Button(T(prefix+"title"), nil, kit.Ghost), []kit.MenuItem{
		{Label: T(prefix + "new_dir"), Icon: lucide.Folder, OnClick: func() { s.openDriveMkdir(v) }},
		{Separator: true},
		{Label: T(prefix + "upload_dir"), Icon: lucide.Upload, Disabled: noUpload,
			OnClick: func() { s.pickDriveUpload(v, true) }},
		{Label: T(prefix + "upload_file"), Icon: lucide.Upload, Disabled: noUpload,
			OnClick: func() { s.pickDriveUpload(v, false) }},
	}))
	return row
}

func (s *Shell) driveContent(cx *fairy.Cx, v *driveView) *fairy.El {
	t := kit.T()
	d := &s.drive
	folder := s.driveFolder(v)
	items := s.driveDisplay(v)
	area := fairy.Col().Key(driveAreaKey).Grow(1).MinH(0).WFull().Focusable().Clip().
		OnPointerDown(func(fairy.PointerEvent) { v.selected, v.anchor = nil, -1 })

	subdir := s.driveSubdirSearch(v) && len([]rune(strings.TrimSpace(v.search))) >= driveSearchMinLength
	searched := subdir && v.resultsFor != ""
	switch {
	case len(items) > 0:
		if !d.grid {
			area.Child(s.driveListHead(v.columns), s.measureDriveColumns(v, items))
		}
		area.Child(s.driveItems(cx, v, items, searched && v.resultsMore))
	case searched && v.resultsErr != nil:
		area.Child(driveEmpty(nil, driveSearchError(v.resultsErr), ""))
	case searched:
		area.Child(driveEmpty(lucide.Folder, T("page.drive.head_buttons.search_no_results"), ""))
	case subdir:
		area.Child(fairy.Col(fairy.Spinner().Size(24, 24)).Grow(1).WFull().Center())
	case v.share && folder == shareRootID && len(v.listing.children) == 0:
		area.Child(driveEmpty(lucide.Share2, T("page.share_drive.no_accessible_drives"), ""))
	default:
		area.Child(driveEmpty(lucide.Folder, T("page.drive.no_contents_section_message.0"),
			T("page.drive.no_contents_section_message.1")))
	}

	if canUploadTo(folder) {
		area.OnFileDrop(func(drop fairy.FileDrop) { s.uploadDrivePaths(folder, drop.Paths) }).
			DropFill(t.Accent.Alpha(0.08)).DropBorder(t.Accent)
		if cx.FileDropTarget() == driveAreaKey {
			name := v.listing.content.Name
			if name == "" || v.listing.content.ID == "" {
				name = T(lo.Ternary(v.share, "page.share_drive.title", "page.drive.title"))
			}
			area.Child(
				dropOverlay(T("page.drive.dad_section.title", "name", name), T("page.drive.dad_section.description")),
			)
		}
	}
	if len(d.clipboard.items) > 0 {
		area.Child(s.driveClipboardBar(folder))
	}
	return kit.ContextMenu("drive.ctx", area, func() []kit.MenuItem { return s.driveMenu(v, folder) })
}

func driveEmpty(icon *svg.Vector, title, description string) *fairy.El {
	t := kit.T()
	col := fairy.Col().Grow(1).WFull().Center().Pad(24)
	if icon != nil {
		col.Child(fairy.Icon(icon).Size(80, 80).MarginEdges(0, 0, 16, 0))
	}
	col.Child(fairy.Text(title).FontSize(18).LineHeight(28).TextAlign(fairy.TextCenter))
	if description != "" {
		col.Child(
			fairy.Text(description).FontSize(16).LineHeight(24).Color(t.MutedForeground).TextAlign(fairy.TextCenter),
		)
	}
	return col
}

// measureDriveColumns lets the widest formatted value set each column before the virtual rows are laid out.
func (s *Shell) measureDriveColumns(v *driveView, items []driveItem) *fairy.El {
	var firstResult *driveItem
	if len(v.results) > 0 {
		firstResult = &v.results[0]
	}
	version := struct {
		listing     *driveListing
		firstResult *driveItem
		count       int
		search      string
		language    string
		includeSubs bool
	}{v.listing, firstResult, len(items), v.search, catalog.Language(), s.drive.includeSubdirs}
	return fairy.Memo("drive.column-measure", version, func() *fairy.El {
		column := func(format func(driveItem) string, pad, minWidth float32, measured *float32) *fairy.El {
			texts := fairy.Col().MinW(minWidth).PadXY(pad/2, 0).Shrink(0).Role(fairy.RoleHidden)
			seen := make(map[string]bool)
			for _, item := range items {
				label := format(item)
				if !seen[label] {
					texts.Child(fairy.Text(label).FontSize(14).LineHeight(20).NoWrap().H(1))
					seen[label] = true
				}
			}
			return texts.OnLayout(func(width, _ float32) {
				if *measured != width {
					*measured = width
					s.win.Post(func() {})
				}
			})
		}
		return fairy.Row(column(driveSize, 16, driveSizeMinWidth, &v.columns.size),
			column(driveDate, 20, driveDateMinWidth, &v.columns.date)).
			Absolute().Top(0).Left(0).Right(0).H(1).Opacity(0).PassThrough().Clip().Role(fairy.RoleHidden)
	})
}

func (s *Shell) driveListHead(widths driveColumns) *fairy.El {
	t := kit.T()
	d := &s.drive
	column := func(field, label string, width float32) *fairy.El {
		ink := t.MutedForeground
		row := fairy.Row(fairy.Text(label).FontSize(14).LineHeight(20).NoWrap()).Gap(6).Align(fairy.Center)
		if d.sortField == field {
			ink = t.Primary
			icon := lucide.ArrowUp
			if d.sortDesc {
				icon = lucide.ArrowDown
			}
			row.Child(fairy.Icon(icon).Size(16, 16))
		}
		cell := fairy.Row(row).HFull().Align(fairy.Center).PadXY(8, 0).Color(ink).Cursor(fairy.CursorPointer).
			Role(fairy.RoleButton).Label(label).OnClick(func() {
			if d.sortField == field {
				d.sortDesc = !d.sortDesc
				return
			}
			d.sortField, d.sortDesc = field, field != "name"
		})
		if width == 0 {
			return cell.Grow(1).MinW(0).PadEdges(0, 8, 0, 12)
		}
		return cell.W(width).Shrink(0).Justify(fairy.JustifyEnd)
	}
	return fairy.Row(
		column("name", T("page.drive.list_head.name"), 0),
		column("size", T("page.drive.list_head.size"), max(widths.size, driveSizeMinWidth)),
		column("date", T("page.drive.list_head.date"), max(widths.date, driveDateMinWidth)).PadEdges(0, 12, 0, 8),
	).H(32).Shrink(0).WFull()
}

// driveItems lists or tiles items. more adds the button that loads the next page of search results.
func (s *Shell) driveItems(cx *fairy.Cx, v *driveView, items []driveItem, more bool) *fairy.El {
	count := len(items)
	// The grid has no full-width cell, so its button sits under the grid; the list ends with it as a row.
	if more && !s.drive.grid {
		count++
	}
	loadMore := func() *fairy.El {
		return fairy.Row(kit.Button(T("page.drive.head_buttons.search_load_more"), func() {
			s.fetchDriveResults(v, s.driveFolder(v), v.searchApplied, v.resultsCursor)
		}, kit.Outline, kit.Loading(v.resultsBusy), kit.Disabled(v.resultsBusy))).WFull().Justify(fairy.JustifyCenter).
			Pad(12)
	}
	build := func(i int) *fairy.El {
		if i == len(items) {
			return loadMore()
		}
		if s.drive.grid {
			return s.driveTile(v, items, i)
		}
		return s.driveRow(v, items, i)
	}
	var list *fairy.El
	if s.drive.grid {
		width, _ := cx.Size()
		columns := 2
		switch {
		case width >= 1280:
			columns = 5
		case width >= 1024:
			columns = 4
		case width >= 768:
			columns = 3
		}
		cell := max((width-navWidth-1-32-driveGridGap*float32(columns-1))/float32(columns), 1)
		list = fairy.VirtualGrid(count, 0, cell, driveGridGap, build).GridColumns(columns).Pad(16)
	} else {
		list = fairy.VirtualList(count, driveRowHeight, build)
	}
	// Keeps the last rows reachable above the floating clipboard bar.
	if len(s.drive.clipboard.items) > 0 {
		list.PadEdges(0, 0, 56, 0)
	}
	// The key changes with the folder, so a folder opens scrolled to the top.
	list.Key(driveListKey + v.listed).Grow(1).MinH(0).WFull()
	if more && s.drive.grid {
		return fairy.Col(list, loadMore().Shrink(0)).Grow(1).MinH(0).WFull()
	}
	return list
}

func (s *Shell) driveCut(id string) bool {
	c := &s.drive.clipboard
	return c.cut && slices.ContainsFunc(c.items, func(item driveItem) bool { return item.ID == id })
}

// driveItemEvents gives a row or tile the React list's pointer behaviour.
func (s *Shell) driveItemEvents(e *fairy.El, v *driveView, items []driveItem, i int) *fairy.El {
	item := items[i]
	selected := v.isSelected(item.ID)
	if s.driveCut(item.ID) {
		e.Opacity(0.5)
	}
	return e.Key("drive.item:" + item.ID).Cursor(fairy.CursorPointer).Selected(selected).Label(item.Name).
		OnPointerDown(func(p fairy.PointerEvent) {
			if p.Button == 1 && !v.isSelected(item.ID) {
				v.selected, v.anchor = []string{item.ID}, i
			}
		}).
		OnClick(func() { clickDriveItem(v, items, i, s.win.Mods()) }).
		OnDoubleClick(func() {
			if !s.driveDialogOpen() {
				s.openDriveItem(v, item)
			}
		})
}

func (s *Shell) driveRow(v *driveView, items []driveItem, i int) *fairy.El {
	t := kit.T()
	item := items[i]
	rowHighlight := pick(s.dark, fairy.Hex(0xffffff), fairy.Hex(0x000000)).Alpha(0.1)
	row := fairy.Row(
		fairy.Row(
			fairy.Box(s.driveIcon(v, item, 44, false)).Size(44, 44).Shrink(0).Center(),
			fairy.Text(item.Name).FontSize(16).LineHeight(24).Ellipsis().MinW(0).Grow(1).TooltipWhenTruncated(),
		).Grow(1).MinW(0).Gap(12).Align(fairy.Center).PadEdges(0, 8, 0, 12),
		fairy.Text(driveSize(item)).NoWrap().FontSize(14).LineHeight(20).Color(t.MutedForeground).
			W(max(v.columns.size, driveSizeMinWidth)).Shrink(0).
			TextAlign(fairy.TextEnd).PadXY(8, 0),
		fairy.Text(driveDate(item)).NoWrap().FontSize(14).LineHeight(20).Color(t.MutedForeground).
			W(max(v.columns.date, driveDateMinWidth)).Shrink(0).
			TextAlign(fairy.TextEnd).PadEdges(0, 12, 0, 8),
	).H(driveRowHeight).WFull().Align(fairy.Center).HoverFill(rowHighlight)
	if v.isSelected(item.ID) {
		row.Fill(rowHighlight)
	}
	return s.driveItemEvents(row, v, items, i)
}

func (s *Shell) driveTile(v *driveView, items []driveItem, i int) *fairy.El {
	t := kit.T()
	item := items[i]
	name := fairy.Row(fairy.Text(item.Name).FontSize(14).LineHeight(20).Ellipsis().MinW(0).TooltipWhenTruncated()).
		MinW(0).PadXY(8, 3).Radius(999).
		Fill(pick(s.dark, fairy.Hex(0x18181b), fairy.Hex(0xf4f4f5))).Color(t.Primary)
	tile := fairy.Col(fairy.Box(
		s.driveIcon(v, item, 128, true),
		fairy.Row(name).Absolute().Left(0).Right(0).Bottom(0).Justify(fairy.JustifyCenter),
	).WFull().Grow(1).MinH(0).Center().Clip()).
		WFull().HFull().Pad(8).Radius(t.RadiusMd()).HoverFill(t.Secondary)
	if v.isSelected(item.ID) {
		tile.Fill(t.Secondary)
	}
	return s.driveItemEvents(tile, v, items, i)
}

// driveIcon is an item's preview, or its kind's icon. In the list, a click on a preview shows it larger.
func (s *Shell) driveIcon(v *driveView, item driveItem, size float32, cover bool) *fairy.El {
	t := kit.T()
	icon := func(vec *svg.Vector, color fairy.Color) *fairy.El {
		pixels := size
		if cover {
			pixels = 32
			if vec == lucide.Folder {
				pixels = 68
			}
		}
		return fairy.Icon(vec).Size(pixels, pixels).Color(color)
	}
	spinnerSize := float32(20)
	if cover {
		spinnerSize = 32
	}
	switch {
	case item.Storing:
		return fairy.Spinner().
			Size(spinnerSize, spinnerSize).
			Tooltip(T("page.drive.storing_tooltip")).
			Label(T("page.drive.storing_tooltip"))
	case v.loading && item.IsDir && item.ID == s.driveFolder(v):
		return fairy.Spinner().Size(spinnerSize, spinnerSize)
	case item.IsDir && !item.hasPreview():
		return icon(lucide.Folder, driveFolderColor)
	case item.hasPreview():
		src := item.Thumb
		if cover {
			src = item.Cover
		}
		if src == "" {
			src = item.Image
		}
		var preview *fairy.El
		box := float32(48)
		if cover {
			box = driveGridCell
		}
		if video := s.driveVideo(item.Video); video != nil && video.State() != fairy.VideoLoading {
			preview = fairy.VideoView(video).WFull().HFull()
			if !cover {
				preview.Fit(fairy.FitCover)
			}
		} else if src == "" {
			preview = icon(lucide.Film, t.MutedForeground)
		} else if img := s.httpImage("drive:", src, box, box); img != nil {
			preview = fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain)
		} else if fairy.Images.SizedErr("drive:"+src, box, box, fairy.FitContain) != nil {
			preview = icon(lucide.ImageOff, t.MutedForeground)
		} else {
			preview = fairy.Spinner().Size(16, 16)
		}
		if cover {
			return preview
		}
		return fairy.Box(preview).Size(48, 48).Radius(6).Clip().Cursor(fairy.CursorPointer).Center().
			OnClick(func() { s.drive.preview = &item })
	case cover:
		return icon(lucide.File, fairy.Hex(0x60a5fa))
	case strings.HasPrefix(item.MimeType, "text"):
		return icon(lucide.FileText, fairy.Hex(0x60a5fa))
	}
	return icon(lucide.File, t.MutedForeground)
}

// httpImage loads a remote image through the shared HTTP client for a w×h box drawn with FitContain, keyed by
// prefix and URL.
func (s *Shell) httpImage(prefix, src string, w, h float32) *fairy.Image {
	u, err := url.Parse(src)
	if src == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	return fairy.Images.Sized(prefix+src, w, h, fairy.FitContain, func() ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), driveImageTimeout)
		defer cancel()
		response, err := s.deps.HTTP.Fetch(ctx, src, infra.FetchOptions{})
		if err != nil {
			return nil, err
		}
		defer func() { _ = response.Body.Close() }()
		return io.ReadAll(io.LimitReader(response.Body, driveImageMaxSize))
	})
}

func driveSize(item driveItem) string {
	return formatSize(item.Size)
}

func driveDate(item driveItem) string {
	if item.Updated.IsZero() {
		return ""
	}
	return formatDateTime(item.Updated.Local(), true)
}

// driveMenu is the context menu for the selection, or for the folder when nothing is selected.
func (s *Shell) driveMenu(v *driveView, folder string) []kit.MenuItem {
	selected := v.selectedItems(s.driveDisplay(v))
	prefix := "page.drive.context_menu."
	if len(selected) == 0 {
		items := []kit.MenuItem{{Label: T(prefix + "new_dir"), Icon: lucide.Folder,
			OnClick: func() { s.openDriveMkdir(v) }}}
		if len(s.drive.clipboard.items) > 0 {
			items = append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
				Label: T(prefix + "paste"), Icon: lucide.Clipboard, Disabled: folder == shareRootID,
				OnClick: func() { s.pasteDrive(folder) },
			})
		}
		return items
	}
	remove := kit.MenuItem{Label: T(prefix + "delete"), Icon: lucide.Trash, Danger: true,
		OnClick: func() { s.deleteDriveItems(v, selected) }}
	if v.share {
		remove.Label = T(prefix + "trash")
	}
	download := kit.MenuItem{Label: T(prefix + "download"), Icon: lucide.Download,
		OnClick: func() { s.downloadDriveItems(selected) }}
	copyItem := kit.MenuItem{Label: T(prefix + "copy"), Icon: lucide.Copy,
		OnClick: func() { s.setDriveClipboard(selected, false) }}
	cut := kit.MenuItem{Label: T(prefix + "cut"), Icon: lucide.Scissors,
		OnClick: func() { s.setDriveClipboard(selected, true) }}
	if len(selected) > 1 {
		return []kit.MenuItem{download, copyItem, cut, {Separator: true}, remove}
	}
	item := selected[0]
	var items []kit.MenuItem
	if item.IsDir {
		items = append(items, kit.MenuItem{Label: T(prefix + "open"), Icon: lucide.FolderOpen,
			OnClick: func() { s.openDriveFolder(v, item.ID, "") }}, kit.MenuItem{Separator: true})
	}
	return append(items, download, kit.MenuItem{Separator: true}, copyItem, cut,
		kit.MenuItem{Label: T(prefix + "rename"), Icon: lucide.Pencil, OnClick: func() { s.openDriveRename(item) }},
		kit.MenuItem{Separator: true}, remove)
}

func (s *Shell) driveClipboardBar(folder string) *fairy.El {
	t := kit.T()
	c := &s.drive.clipboard
	icon, action := lucide.Copy, "copy"
	if c.cut {
		icon, action = lucide.Scissors, "cut"
	}
	name := c.items[0].Name
	message := T("page.drive.clipboard."+action+"_single", "name", name)
	if len(c.items) > 1 {
		message = T("page.drive.clipboard."+action+"_multiple", "name", name, "count", len(c.items)-1)
	}
	bar := fairy.Row(
		fairy.Icon(icon).Size(14, 14).Shrink(0),
		fairy.Text(message).FontSize(12).LineHeight(16).Ellipsis().MinW(0).Grow(1),
		kit.Button(T("g.cancel"), func() { s.drive.clipboard = driveClipboard{} }, kit.Ghost, kit.XS),
		kit.Button(T("page.drive.context_menu.paste"), func() { s.pasteDrive(folder) }, kit.XS,
			kit.Disabled(folder == shareRootID || s.drive.pasting), kit.Loading(s.drive.pasting)),
	).WFull().MaxW(640).PadXY(12, 6).Gap(12).Align(fairy.Center).Radius(t.RadiusMd()).Fill(t.Popover).
		Border(1, t.Border).Shadow(0, 4, 12, 0, shadowColor(t))
	return fairy.Row(bar).Absolute().Left(0).Right(0).Bottom(0).Justify(fairy.JustifyCenter).PadEdges(8, 16, 16, 16).
		PassThrough()
}

func (s *Shell) showDriveDialogs(cx *fairy.Cx, v *driveView) {
	d := &s.drive
	if r := d.rename; r != nil {
		s.showDriveRename(cx, r)
	}
	if m := d.mkdir; m != nil {
		s.showDriveMkdir(cx, m)
	}
	if c := d.conflict; c != nil {
		s.showDriveConflict(cx, c)
	}
	if p := d.preview; p != nil {
		s.showDrivePreview(cx, p)
	}
	if d.importer != nil {
		s.showDriveImport(cx, v)
	}
}

func (s *Shell) showDriveRename(cx *fairy.Cx, r *driveRename) {
	t := kit.T()
	closeDialog := func() {
		if s.drive.rename == r {
			s.drive.rename = nil
		}
	}
	message := driveRenameError(r)
	name := fairy.Input(r.name, func(text string) { r.name, r.touched = limitRunes(text, 200), true }).
		Key("drive.rename.name").
		Placeholder(T("page.drive.dialog.rename.name_input_placeholder")).
		PlaceholderColor(t.MutedForeground).
		Label(T("page.drive.dialog.rename.title")).
		AutoFocus().
		OnSubmit(func(string) { s.submitDriveRename() })
	fields := fairy.Row(driveDialogInput(name, 40).WFull().MinW(0)).Gap(16)
	if !r.item.IsDir {
		ext := fairy.Input(r.ext, func(text string) { r.ext = limitRunes(text, 50) }).Key("drive.rename.ext").
			Placeholder(T("page.drive.dialog.rename.extension")).PlaceholderColor(t.MutedForeground).
			Label(T("page.drive.dialog.rename.extension")).
			OnSubmit(func(string) { s.submitDriveRename() })
		fields.Child(driveDialogInput(ext, 40).WPct(0.25).MinW(0))
	}
	body := fairy.Col(fields).Gap(8)
	if r.touched && message != "" {
		body.Child(fairy.Text(message).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T("page.drive.dialog.rename.title"),
		Body: fairy.Col(body, fairy.Row(
			kit.Button(T("g.cancel"), closeDialog, kit.Outline),
			kit.Button(T("g.confirm"), s.submitDriveRename, kit.Disabled(r.busy || message != ""), kit.Loading(r.busy)),
		).Gap(8).Justify(fairy.JustifyEnd)).Gap(16),
		OnClose: closeDialog,
		Key:     "drive.rename",
	})
}

func (s *Shell) showDriveMkdir(cx *fairy.Cx, m *driveMkdir) {
	t := kit.T()
	closeDialog := func() {
		if s.drive.mkdir == m {
			s.drive.mkdir = nil
		}
	}
	message := s.driveMkdirError(m)
	name := fairy.Input(m.name, func(text string) { m.name, m.touched = limitRunes(text, 255), true }).
		Key("drive.mkdir.name").
		Placeholder(T("page.drive.dialog.create_dir.name_input_placeholder")).
		PlaceholderColor(t.MutedForeground).
		Label(T("page.drive.dialog.create_dir.title")).
		AutoFocus().
		OnSubmit(func(string) { s.submitDriveMkdir() })
	body := fairy.Col(driveDialogInput(name, 32)).Gap(8)
	if m.touched && message != "" {
		body.Child(fairy.Text(message).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T("page.drive.dialog.create_dir.title"),
		Body: fairy.Col(body, fairy.Row(
			kit.Button(T("g.cancel"), closeDialog, kit.Outline),
			kit.Button(T("g.confirm"), s.submitDriveMkdir, kit.Disabled(m.busy || message != ""), kit.Loading(m.busy)),
		).Gap(8).Justify(fairy.JustifyEnd)).Gap(16),
		OnClose: closeDialog,
		Key:     "drive.mkdir",
	})
}

// limitRunes cuts text to at most n characters, as an input's maxLength did.
func limitRunes(text string, n int) string {
	if runes := []rune(text); len(runes) > n {
		return string(runes[:n])
	}
	return text
}

// driveField frames an input as kit.TextField does, keeping the input itself addressable by its key.
func driveField(input *fairy.El, invalid bool) *fairy.El {
	t := kit.T()
	border := t.Input
	if invalid {
		border = t.Destructive
	}
	return input.H(40).PadXY(10, 0).Justify(fairy.JustifyCenter).Radius(t.RadiusMd()).Fill(t.Card).Border(1, border).
		FocusBorder(t.Accent)
}

func driveDialogInput(input *fairy.El, height float32) *fairy.El {
	t := kit.T()
	if t.Dark {
		input.Fill(t.Input.Alpha(t.Input.A * 0.3))
	}
	return input.H(height).FontSize(14).LineHeight(20).PadXY(10, 0).Justify(fairy.JustifyCenter).
		Radius(t.RadiusLg()).Border(1, t.Border).FocusBorder(t.Ring)
}

func (s *Shell) showDriveConflict(cx *fairy.Cx, c *driveConflict) {
	t := kit.T()
	prefix := "page.drive.dialog.conflict_name."
	answer := func(strategy drive.UploadConflictStrategy) func() {
		return func() {
			if s.drive.conflict != c {
				return
			}
			s.drive.conflict = nil
			if strategy != "" {
				c.answer(strategy)
			}
		}
	}
	body := fairy.Col(
		fairy.Text(T(prefix+"description", "count", len(c.names))).FontSize(14).LineHeight(20).Color(t.MutedForeground),
		fairy.Col(
			fairy.Text(T(prefix+"option_suffix")).FontSize(14).LineHeight(20),
			fairy.Text(T(prefix+"option_skip")).FontSize(14).LineHeight(20),
		).MarginEdges(8, 0, 0, 0),
	).Gap(6)
	if len(c.names) > 0 {
		preview := c.names[:min(len(c.names), driveConflictView)]
		list := fairy.Col(
			fairy.Text(T(prefix+"preview_label")).FontSize(12).LineHeight(16),
			fairy.Text(strings.Join(preview, ", ")).FontSize(12).LineHeight(16).Wrap(),
		).Color(t.MutedForeground).MarginEdges(2, 0, 0, 0)
		if hidden := len(c.names) - len(preview); hidden > 0 {
			list.Child(fairy.Text(T(prefix+"preview_more", "count", hidden)).FontSize(12).LineHeight(16))
		}
		body.Child(list)
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:    T(prefix + "title"),
		Alert:    true,
		Body:     body,
		OnEscape: answer(""),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), answer(""), kit.Outline),
			kit.Button(T(prefix+"action_skip"), answer(drive.UploadConflictSkip)),
			kit.Button(T(prefix+"action_suffix"), answer(drive.UploadConflictSuffix)),
		},
		Key: "drive.conflict",
	})
}

func driveExtensionList(extensions []string) *fairy.El {
	labels := make([]string, len(extensions))
	for i, ext := range extensions {
		labels[i] = ext
		if ext == "" {
			labels[i] = T("page.drive.dialog.unsupported_extensions.none")
		}
	}
	return fairy.Text(strings.Join(labels, ", ")).FontSize(14).LineHeight(20).Color(kit.T().MutedForeground).Wrap()
}

// showDrivePreview shows an item's preview video or image over the window until clicked or dismissed.
func (s *Shell) showDrivePreview(cx *fairy.Cx, item *driveItem) {
	t := kit.T()
	closePreview := func() { s.drive.preview = nil }
	windowW, windowH := cx.Size()
	var video *fairy.Video
	if path := s.cachedDriveVideo(item.Video); path != "" {
		video = s.driveVideos.pinned(item.Video, path, s.deps.Log, "Drive.preview")
	}
	var img *fairy.Image
	if item.Video == "" || s.driveVideoFailed(item.Video) {
		img = s.httpImage("drive:", item.Image, windowW, windowH)
	}
	var content *fairy.El
	switch {
	case video != nil && video.State() != fairy.VideoLoading:
		content = fairy.VideoView(video).WFull().HFull()
	case item.Video != "" && !s.driveVideoFailed(item.Video):
		content = fairy.Spinner().Size(28, 28)
	case img != nil:
		content = fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain)
	case item.Image == "" || fairy.Images.SizedErr("drive:"+item.Image, windowW, windowH, fairy.FitContain) != nil:
		content = fairy.Icon(lucide.ImageOff).Size(48, 48).Color(t.MutedForeground)
	default:
		content = fairy.Spinner().Size(28, 28)
	}
	lightbox{key: "drive.preview", label: item.Name, content: content, close: closePreview}.show(cx)
}
