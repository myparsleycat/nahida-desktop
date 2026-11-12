package ui

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

const transferRowHeight float32 = 96

var (
	transferPlanColor     = fairy.Hex(0x51a2ff)
	transferCompleteColor = fairy.Hex(0x05df72)
)

// The status a transfer row shows, as the React screen mapped the backend status.
const (
	transferUploading   = "uploading"
	transferDownloading = "downloading"
	transferPaused      = "paused"
	transferCompleted   = "completed"
	transferFailed      = "failed"
	transferQueued      = "queued"
	transferPreparing   = "preparing"
)

var transferFilterStatuses = []string{
	transferUploading, transferDownloading, transferPaused, transferCompleted, transferFailed, transferQueued,
}

// errorCodes whose transfer cannot succeed by retrying it.
var transferFinalErrors = []string{
	"invalid_nte_mod_file", "nte_client_upgrade_required", "upload_file_too_large", "nte_bundle_too_large",
	"upload_source_changed",
}

type transferScreen struct {
	started bool
	// received is set by the first transfer:update, which is newer than the list read when the screen opens.
	received bool
	items    []transfer.Snapshot

	tab      string
	search   string
	statuses []string
	// errorText is the failure shown in a dialog after clicking a failed status.
	errorText *string
}

func newTransferScreen() transferScreen { return transferScreen{tab: "all"} }

func (s *Shell) setTransfers(snapshots []transfer.Snapshot) {
	s.transfers.items, s.transfers.received = snapshots, true
	s.transfer = transfer.CalculateWindowProgress(snapshots)
}

func (s *Shell) startTransfers() {
	ts := &s.transfers
	if ts.started || s.deps.Transfer == nil {
		return
	}
	ts.started = true
	s.background(func(context.Context) {
		items := s.deps.Transfer.List()
		fairy.Go(func() {
			if !ts.received {
				ts.items = items
			}
		})
	})
}

// transferAction runs a queue action. The queue reports its changes through transfer:update.
func (s *Shell) transferAction(action, pid string, run func() error) {
	s.background(func(context.Context) {
		err := run()
		if err == nil {
			return
		}
		err = infra.ReportError(s.deps.Log, err, "Transfer", infra.Diagnostic{
			Operation: "transfer." + action, Fields: map[string]any{"pid": pid},
		})
		s.toasts.Show(kit.ToastError, T("page.transfer.title"), err.Error())
	})
}

func transferStatus(item transfer.Snapshot) string {
	switch item.Status {
	case transfer.StatusProgress:
		if item.Type == "upload" {
			return transferUploading
		}
		return transferDownloading
	case transfer.StatusCompleted:
		return transferCompleted
	case transfer.StatusPending:
		return transferQueued
	case transfer.StatusPaused:
		return transferPaused
	case transfer.StatusPreparing:
		return transferPreparing
	case transfer.StatusError, transfer.StatusCanceled:
	}
	return transferFailed
}

func transferStatusColor(status string) fairy.Color {
	t := kit.T()
	switch status {
	case transferUploading, transferDownloading, transferPaused:
		return t.Foreground
	case transferCompleted:
		return transferCompleteColor
	case transferFailed:
		return t.Destructive
	case transferQueued:
		return t.MutedForeground
	}
	return transferPlanColor
}

func formatETA(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return "--"
	}
	const prefix = "page.transfer.item.duration."
	total := int64(math.Ceil(seconds))
	var parts []string
	for _, unit := range []struct {
		size int64
		key  string
	}{{3600, "hours"}, {60, "minutes"}, {1, "seconds"}} {
		if n := total / unit.size; n > 0 {
			parts = append(parts, T(prefix+unit.key, "count", n))
		}
		total %= unit.size
	}
	if len(parts) == 0 {
		return T(prefix+"seconds", "count", 0)
	}
	return strings.Join(parts, " ")
}

func (s *Shell) filteredTransfers() []transfer.Snapshot {
	ts := &s.transfers
	query := strings.ToLower(ts.search)
	return slices.DeleteFunc(slices.Clone(ts.items), func(item transfer.Snapshot) bool {
		switch {
		case !strings.Contains(strings.ToLower(item.Name), query):
			return true
		case ts.tab == "uploads" && item.Type != "upload", ts.tab == "downloads" && item.Type != "download":
			return true
		}
		return len(ts.statuses) > 0 && !slices.Contains(ts.statuses, transferStatus(item))
	})
}

func (s *Shell) transferPage(cx *fairy.Cx) *fairy.El {
	s.startTransfers()
	t := kit.T()
	ts := &s.transfers
	items := s.filteredTransfers()
	windowWidth, _ := cx.Size()

	var body *fairy.El
	if len(items) == 0 {
		body = fairy.Col(s.transferEmpty()).Grow(1).MinH(0).WFull().Pad(16)
	} else {
		body = fairy.VirtualList(len(items), transferRowHeight, func(i int) *fairy.El { return s.transferRow(items[i]) }).
			Key("transfer.list").
			Grow(1).
			MinH(0).
			WFull().
			Pad(16)
	}
	if ts.errorText != nil {
		kit.ShowDialog(cx, kit.DialogProps{
			Title:   T("page.transfer.item.failed"),
			Body:    fairy.Text(*ts.errorText).LineHeight(20),
			Actions: []*fairy.El{kit.Button(T("g.close"), func() { ts.errorText = nil }, kit.Outline)},
			OnClose: func() { ts.errorText = nil },
			Key:     "transfer.error",
		})
	}
	// CSS includes the separator in the header's height; fairy draws it inside the box.
	return fairy.Col(
		fairy.Col(s.transferStats(), s.transferToolbar(windowWidth)).Shrink(0).Pad(16).Gap(24).
			BorderEdges(0, 0, 1, 0, t.Border),
		body,
	).WFull().HFull().FontSize(14)
}

func (s *Shell) transferStats() *fairy.El {
	t := kit.T()
	var uploadSpeed, downloadSpeed float64
	var transferred int64
	active := 0
	for _, item := range s.transfers.items {
		transferred += item.TransferredSize
		if item.Status == transfer.StatusProgress || item.Status == transfer.StatusPaused {
			active++
		}
		if item.Status != transfer.StatusProgress {
			continue
		}
		if item.Type == "upload" && item.Progress < 100 {
			uploadSpeed += item.Speed
		} else if item.Type == "download" {
			downloadSpeed += item.Speed
		}
	}
	card := func(icon *svg.Vector, color fairy.Color, label, value string) *fairy.El {
		return fairy.Row(
			fairy.Box(fairy.Icon(icon).Size(20, 20)).Size(40, 40).Shrink(0).Center().Radius(t.RadiusLg()).
				Fill(t.Secondary).Color(color),
			fairy.Col(
				fairy.Text(label).FontSize(12).LineHeight(16).Color(t.MutedForeground).Ellipsis(),
				fairy.Text(value).FontSize(18).LineHeight(28).SemiBold().Ellipsis(),
			).MinW(0),
		).Basis(0).Grow(1).MinW(0).Gap(12).Pad(16).Align(fairy.Center).Radius(t.RadiusLg()).Fill(t.Muted.Alpha(0.5))
	}
	return fairy.Row(
		card(lucide.ArrowUpFromLine, t.Foreground, T("page.transfer.stats_label.upload"),
			formatSize(int64(uploadSpeed))+"/s"),
		card(lucide.ArrowDownToLine, t.Foreground, T("page.transfer.stats_label.download"),
			formatSize(int64(downloadSpeed))+"/s"),
		card(
			lucide.HardDrive,
			t.MutedForeground,
			T("page.transfer.stats_label.total_transferred"),
			formatSize(transferred),
		),
		card(lucide.Clock, t.Foreground, T("page.transfer.stats_label.active_transfers"), itoa(active)),
	).WFull().Gap(12)
}

func (s *Shell) transferToolbar(windowWidth float32) *fairy.El {
	t := kit.T()
	ts := &s.transfers
	uploads, downloads, completed := 0, 0, 0
	for _, item := range ts.items {
		switch item.Type {
		case "upload":
			uploads++
		case "download":
			downloads++
		}
		if transferStatus(item) == transferCompleted {
			completed++
		}
	}
	tab := func(value, key string, icon *svg.Vector, count int) kit.Item {
		return kit.Item{Value: value, Label: T(key), Icon: icon, Badge: itoa(count)}
	}
	clearLabel := T("page.transfer.toolbar.clear_completed")
	clearDone := kit.IconButton(lucide.BrushCleaning, func() {
		s.transferAction("clear", "", s.deps.Transfer.Clear)
	}, kit.Outline, kit.Small, kit.Disabled(completed == 0), kit.Tooltip(clearLabel)).W(36).Label(clearLabel)
	if !t.Dark {
		clearDone.Fill(fairy.Transparent)
	}

	filterLabel := T("page.transfer.toolbar.status_filter.title")
	menu := []kit.MenuItem{{Label: filterLabel, Heading: true}, {Separator: true}}
	for _, status := range transferFilterStatuses {
		menu = append(menu, kit.MenuItem{
			Label:    T("page.transfer.toolbar.status_filter." + status),
			Checkbox: true,
			Checked:  slices.Contains(ts.statuses, status),
			OnClick: func() {
				if i := slices.Index(ts.statuses, status); i >= 0 {
					ts.statuses = slices.Delete(ts.statuses, i, i+1)
				} else {
					ts.statuses = append(ts.statuses, status)
				}
			},
		})
	}
	filterButton := kit.IconButton(lucide.Funnel, nil, kit.Outline, kit.Tooltip(filterLabel)).Label(filterLabel)
	if len(ts.statuses) > 0 {
		filterButton.Border(1, t.Primary)
	}

	compact := windowWidth < 640
	tabs := s.transferTabs([]kit.Item{
		tab("all", "page.transfer.toolbar.all", nil, len(ts.items)),
		tab("uploads", "page.transfer.toolbar.upload", lucide.ArrowUpFromLine, uploads),
		tab("downloads", "page.transfer.toolbar.download", lucide.ArrowDownToLine, downloads),
	}, compact)
	clear := fairy.Row(clearDone).Justify(fairy.JustifyEnd).Shrink(0)
	top := fairy.Row(tabs.Shrink(0), clear.Grow(1)).Wrap().Gap(16).Align(fairy.Center)
	if compact {
		top = fairy.Col(tabs.WFull(), clear).Gap(16)
	}
	return fairy.Col(
		top,
		fairy.Row(
			kit.TextField(ts.search, func(value string) { ts.search = value }, kit.Icon(lucide.Search),
				kit.Placeholder(T("g.search")), kit.Label(T("g.search"))).Grow(1).MinW(0),
			kit.DropdownMenu("transfer.filter", filterButton, menu, kit.Width(192), kit.AlignEnd),
		).Gap(8).Align(fairy.Center),
	).Gap(16)
}

func (s *Shell) transferTabs(items []kit.Item, compact bool) *fairy.El {
	t := kit.T()
	inactive := t.Foreground.Alpha(0.6)
	if t.Dark {
		inactive = t.MutedForeground
	}
	list := fairy.Row().Wrap().Pad(3).Radius(t.RadiusLg()).Fill(t.Secondary).Role(fairy.RoleTabList).FocusGroup()
	for _, item := range items {
		selected := s.transfers.tab == item.Value
		trigger := fairy.Row().
			Key(item.Value).
			H(26).
			PadXY(6, 0).
			Border(1, fairy.Transparent).
			Gap(8).
			Align(fairy.Center).
			Justify(fairy.JustifyCenter).
			Radius(t.RadiusMd()).
			Color(inactive).
			HoverColor(t.Foreground).
			Animate(motion.Fast).
			Role(fairy.RoleTab).
			Selected(selected).
			Focusable().
			FocusSelected(selected).
			FocusVisibleBorder(t.Ring).
			FocusRing(3, t.Ring.Alpha(0.5)).
			OnClick(func() { s.transfers.tab = item.Value })
		if compact {
			trigger.Grow(1)
		}
		if item.Icon != nil {
			trigger.Child(fairy.Icon(item.Icon).Size(16, 16).Shrink(0))
		}
		trigger.Child(fairy.Text(item.Label).FontSize(14).LineHeight(20).Medium().NoWrap(),
			kit.Badge(item.Badge, kit.Secondary).PadXY(6, 0).MarginEdges(0, 0, 0, 4))
		if selected {
			trigger.Color(t.Foreground).Fill(t.Background)
			// fairy paints a shadow under the whole box, so it would darken the translucent dark fill, while a CSS
			// shadow only shows outside the box; on the dark list it is not visible anyway.
			if t.Dark {
				trigger.Fill(opacity(t.Input, 0.3)).Border(1, t.Input)
			} else {
				trigger.Shadow(0, 1, 3, 0, fairy.RGBA(0, 0, 0, 0.1)).Shadow(0, 1, 2, -1, fairy.RGBA(0, 0, 0, 0.1))
			}
		} else {
			trigger.Cursor(fairy.CursorPointer)
		}
		list.Child(trigger)
	}
	return list
}

func (s *Shell) transferEmpty() *fairy.El {
	t := kit.T()
	icon := lucide.Funnel
	switch s.transfers.tab {
	case "uploads":
		icon = lucide.ArrowUpFromLine
	case "downloads":
		icon = lucide.ArrowDownToLine
	}
	return fairy.Col(
		fairy.Box(fairy.Icon(icon).Size(24, 24)).Size(48, 48).Center().Radius(999).Fill(t.Secondary).
			Color(t.MutedForeground),
		fairy.Text(T("page.transfer.empty_state.all_tabs")).FontSize(14).LineHeight(20).Medium(),
	).WFull().PadXY(0, 48).Gap(16).Align(fairy.Center).Radius(t.RadiusLg()).Border(1, t.Border).BorderDashed()
}

func (s *Shell) transferRow(item transfer.Snapshot) *fairy.El {
	t := kit.T()
	status := transferStatus(item)
	active := status == transferUploading || status == transferDownloading
	planning := status == transferUploading && item.PlanPhase != nil
	preparing := status == transferPreparing && item.Type == "upload" && item.PreparationProgress != nil
	finalizing := active && item.Progress >= 100
	activity := ""
	if status == transferUploading && !planning && !finalizing &&
		(item.UploadPhase == transfer.UploadPreparing || item.UploadPhase == transfer.UploadWaiting) {
		activity = string(item.UploadPhase)
	}
	progress := item.Progress
	if preparing {
		progress = item.PreparationProgress.Percent()
	}
	if planning {
		progress = 0
		if item.PlanProgress != nil {
			progress = *item.PlanProgress
		}
	}

	statusText := T("page.transfer.item." + status)
	switch {
	case preparing:
		statusText = T("page.transfer.item.reading_files",
			"processed", item.PreparationProgress.ProcessedFiles, "total", item.PreparationProgress.TotalFiles)
	case planning:
		statusText = T("page.transfer.item.planning." + string(*item.PlanPhase))
	case finalizing:
		statusText = T("page.transfer.item.finalizing")
	case activity != "":
		statusText = T("page.transfer.item.upload_activity." + activity)
	case status == transferPreparing && item.Type == "download" && item.TotalFiles > 0:
		statusText = T("page.transfer.item.discovering", "count", item.TotalFiles)
	case (status == transferPreparing || active) && item.TotalFiles > 0:
		statusText += " (" + itoa(item.TransferredFiles) + "/" + itoa(item.TotalFiles) + ")"
	}
	statusLabel := fairy.Text(statusText).
		FontSize(12).
		LineHeight(16).
		Medium().
		Color(transferStatusColor(status)).
		NoWrap().
		Shrink(0)
	if status == transferFailed {
		message := s.transferError(item)
		statusLabel.Cursor(fairy.CursorPointer).Focusable().OnClick(func() { s.transfers.errorText = &message })
	}

	meta := fairy.Row().Gap(8).Align(fairy.Center).MinW(0).Clip()
	if planning {
		meta.Child(fairy.Text(statusText).FontSize(12).LineHeight(16).Color(transferPlanColor).NoWrap())
	}
	if !planning && !finalizing && activity == "" && (active || status == transferPaused) {
		meta.Child(fairy.Text(formatSize(int64(item.Speed)) + "/s").FontSize(12).LineHeight(16).NoWrap().Shrink(0))
		meta.Child(fairy.Text(T("page.transfer.item.time_remaining", "time", formatETA(item.ETA))).
			FontSize(12).LineHeight(16).Ellipsis().MinW(0))
	}
	if status == transferCompleted {
		meta.Child(
			fairy.Text(T("page.transfer.item.completed")).FontSize(12).LineHeight(16).Color(t.MutedForeground).NoWrap(),
		)
	}
	if item.FailedFiles > 0 {
		meta.Child(
			fairy.Text(T("page.transfer.item.failed_files", "count", item.FailedFiles)).FontSize(12).LineHeight(16).
				Color(t.Destructive).NoWrap(),
		)
	} else if status == transferFailed {
		meta.Child(fairy.Text(T("page.transfer.item.failed")).FontSize(12).LineHeight(16).Color(t.Destructive).NoWrap())
	}

	bar := kit.Progress(progress / 100)
	if activity != "" {
		bar.Pulse(2, 0.5)
	}
	icon := lucide.ArrowDownToLine
	if item.Type == "upload" {
		icon = lucide.ArrowUpFromLine
	}
	return fairy.Row(
		fairy.Box(fairy.Icon(icon).Size(20, 20)).Size(40, 40).Shrink(0).Center().Radius(t.RadiusLg()).Fill(t.Secondary),
		fairy.Col(
			fairy.Row(fairy.Text(item.Name).FontSize(14).LineHeight(20).Medium().Ellipsis().Grow(1).MinW(0), statusLabel).
				Gap(8).
				Align(fairy.Center),
			bar.WFull(),
			fairy.Row(
				fairy.Text(formatSize(item.TotalSize)).FontSize(12).LineHeight(16).NoWrap().Shrink(0),
				fairy.Spacer(),
				meta,
				fairy.Text(strconv.FormatFloat(progress, 'f', 2, 64)+"%").
					FontSize(12).
					LineHeight(16).
					NoWrap().
					Shrink(0),
			).Gap(12).Align(fairy.Center).FontSize(12).Color(t.MutedForeground),
		).Grow(1).MinW(0).Gap(8),
		s.transferActions(item, status),
	).Key(item.PID).H(transferRowHeight-8).Gap(16).Pad(16).Align(fairy.Center).Radius(t.RadiusLg()).
		HoverFill(t.Muted.Alpha(0.5)).Animate(motion.Fast).MarginEdges(0, 0, 8, 0)
}

// transferError is the failure text, translated by its error code when the locale knows the code.
func (s *Shell) transferError(item transfer.Snapshot) string {
	if item.ErrorCode != "" {
		if key := "page.transfer.item.error." + item.ErrorCode; catalog.Has(key) {
			if item.ErrorCode == "upload_source_changed" {
				return T(key, "name", item.Error)
			}
			return T(key)
		}
		if item.Error == "" {
			return item.ErrorCode
		}
	}
	return item.Error
}

func (s *Shell) transferActions(item transfer.Snapshot, status string) *fairy.El {
	t := kit.T()
	pid := item.PID
	service := s.deps.Transfer
	failed, completed := status == transferFailed, status == transferCompleted
	canRetry := !slices.Contains(transferFinalErrors, item.ErrorCode)
	button := func(icon *svg.Vector, label string, onClick func()) *fairy.El {
		return kit.IconButton(icon, onClick, kit.Ghost, kit.Tooltip(label)).
			Label(label).
			Color(t.MutedForeground).
			HoverColor(t.Foreground)
	}
	pause := func() { s.transferAction("pause", pid, func() error { return service.Pause(pid) }) }
	resume := func() { s.transferAction("resume", pid, func() error { return service.Resume(pid) }) }
	retry := func() { s.transferAction("retry", pid, func() error { return service.Retry(pid) }) }
	cancel := func() { s.transferAction("cancel", pid, func() error { return service.Cancel(pid) }) }

	row := fairy.Row().Gap(4).Align(fairy.Center).Shrink(0)
	switch status {
	case transferPaused, transferQueued:
		row.Child(button(lucide.Play, T("page.transfer.item.action.resume"), resume))
	case transferUploading, transferDownloading, transferPreparing:
		row.Child(button(lucide.Pause, T("page.transfer.item.action.pause"), pause))
	}
	if failed && canRetry {
		row.Child(button(lucide.Play, T("page.transfer.item.dropdown_menu.retry"), retry))
	}
	if item.Type == "download" && completed && item.Path != "" {
		path := item.Path
		row.Child(button(lucide.Folder, T("page.transfer.item.action.open_folder"), func() { s.openPath(path) }))
	}

	var menu []kit.MenuItem
	if !completed && !failed {
		menu = append(menu, kit.MenuItem{Label: T("page.transfer.item.dropdown_menu.cancel"), OnClick: cancel})
	}
	if failed && canRetry {
		menu = append(menu, kit.MenuItem{Label: T("page.transfer.item.dropdown_menu.retry"), OnClick: retry})
	}
	if !failed && item.FailedFiles > 0 && canRetry {
		menu = append(
			menu,
			kit.MenuItem{Label: T("page.transfer.item.dropdown_menu.retry_failed_files"), OnClick: retry},
		)
	}
	removeLabel := T("page.transfer.item.dropdown_menu.remove_from_transfer")
	menu = append(menu, kit.MenuItem{Label: removeLabel, OnClick: cancel})
	row.Child(kit.DropdownMenu("transfer.menu:"+pid, button(lucide.Ellipsis, T("page.transfer.item.action.more"), nil),
		menu, kit.AlignEnd))
	return row.Child(button(lucide.X, removeLabel, cancel).HoverColor(t.Destructive))
}
