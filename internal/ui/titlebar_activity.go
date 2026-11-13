package ui

import (
	"context"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	fixer4001 "nahida.live/desktop/internal/tools/fixer_4001"
	"nahida.live/desktop/internal/transfer"
	"nahida.live/desktop/internal/xxmi"
)

const (
	elevatedStatusEvent  = "elevated:status"
	maxVisibleActivities = 3
)

type activityStatus int

const (
	activityRunning activityStatus = iota
	activityPaused
	activityWarning
	activityError
)

// titlebarActivity is a badge in the title bar for background work, as the React titlebar activity store held them.
type titlebarActivity struct {
	id      string
	label   string
	detail  string
	tooltip string
	icon    *svg.Vector
	status  activityStatus
	// path is the frontend path a click opens when the click-navigate setting is on; "" opens nothing.
	path    string
	popover *activityPopover
}

type activityPopover struct {
	title, description string
	action, dismiss    *activityButton
	// autoOpen opens the popover when the badge first becomes visible; opened runs when it does.
	autoOpen bool
	opened   func()
}

type activityButton struct {
	label    string
	run      func()
	disabled bool
}

// activityState is the title bar's own state: open popovers, the badges shown in the last frame, and the elevated
// helper status, which no screen keeps.
type activityState struct {
	open  map[string]bool
	shown map[string]bool

	elevated      platform.ElevatedHelperStatus
	elevatedKnown bool
	starting      bool
	// prompted is set once the helper popover opened by itself, until the helper stops being missing.
	prompted bool
	// helperPrompt is set while the dialog asking to start the helper for an agent tool is queued or open.
	helperPrompt bool
}

func newActivityState() activityState {
	return activityState{open: map[string]bool{}, shown: map[string]bool{}}
}

// loadActivities reads the state of background work that may already run when the window opens. A live event that
// arrived first wins over each read.
func (s *Shell) loadActivities() {
	s.startTransfers()
	s.refreshFixInspections()
	s.background(func(ctx context.Context) {
		var elevated platform.ElevatedHelperStatus
		var elevatedErr error
		if s.deps.Input != nil {
			elevated, elevatedErr = s.deps.Input.GetElevatedHelperStatus(ctx)
			if elevatedErr != nil {
				s.report(elevatedErr, "read-elevated-helper")
			}
		}
		compression, compressionErr := s.deps.Mod.GetCompressionState(ctx)
		if compressionErr != nil {
			s.report(compressionErr, "read-compression")
		}
		var fixer tools.Fixer4001State
		var bisect *tools.BisectSnapshot
		var texture tools.TextureResizeProgressEvent
		if s.deps.Tools != nil {
			fixer = s.deps.Tools.FourThousandOneFixerGetState()
			bisect = s.deps.Tools.BisectGetState()
			texture = s.deps.Tools.GetTextureResizeState()
		}
		fairy.Go(func() {
			if s.deps.Input != nil && elevatedErr == nil && !s.activity.elevatedKnown {
				s.setElevatedHelper(elevated)
			}
			if compressionErr == nil && s.settings.compression == nil {
				s.settings.compression = &compression
			}
			if f := &s.tools.fixer4001; s.deps.Tools != nil && !f.received {
				f.task, f.progress, f.errorMessage = deref(fixer.ActiveTask), fixer.Progress, fixer.ErrorMessage
			}
			s.setBisect(bisect)
			if s.deps.Tools != nil && s.tools.textureProgress == nil {
				s.tools.textureProgress = &texture
			}
		})
	})
}

func (s *Shell) setElevatedHelper(status platform.ElevatedHelperStatus) {
	a := &s.activity
	a.elevated, a.elevatedKnown, a.starting = status, true, false
	if !status.Enabled || status.Running {
		a.prompted = false
	}
}

// startElevatedHelper turns the helper setting on, which starts the helper; the next status event ends starting.
func (s *Shell) startElevatedHelper() {
	a := &s.activity
	if a.starting {
		return
	}
	a.starting = true
	key := setting.KeyGeneralElevatedHelperEnabled
	s.background(func(ctx context.Context) {
		err := s.deps.Settings.Set(ctx, key, true)
		if err == nil {
			return
		}
		err = infra.ReportError(s.deps.Log, err, "Settings.Set", infra.Diagnostic{
			Operation: "settings.save", Stage: "start-elevated-helper", Fields: map[string]any{"key": key},
		})
		fairy.Go(func() {
			a.starting = false
			s.toasts.Show(kit.ToastError, T("shell.setting_save_failed"), err.Error())
		})
	})
}

func elevatedHelperRequired(message string) bool {
	code := platform.ErrElevatedHelperRequired.Error()
	return message == code || strings.HasPrefix(message, code+":")
}

// promptElevatedHelper asks to start the elevated helper after an agent tool needed it, as the React title bar
// did, whether or not the helper setting is on.
func (s *Shell) promptElevatedHelper() {
	a := &s.activity
	if a.helperPrompt || (a.elevated.Running && a.elevatedKnown) {
		return
	}
	a.helperPrompt = true
	const prefix = "titlebar.activity.elevatedHelper."
	description := T(prefix + "description")
	if a.elevated.BlockedByAntivirus {
		description = T(prefix + "blockedDescription")
	}
	key := setting.KeyGeneralElevatedHelperEnabled
	s.Confirm(Confirmation{
		Title:        T(prefix + "title"),
		Description:  description,
		ConfirmLabel: T(prefix + "start"),
		Run: func() error {
			err := s.deps.Settings.Set(context.Background(), key, true)
			if err != nil {
				err = infra.ReportError(s.deps.Log, err, "Settings.Set", infra.Diagnostic{
					Operation: "settings.save", Stage: "start-elevated-helper", Fields: map[string]any{"key": key},
				})
			}
			return err
		},
		OnResult: func(bool) { a.helperPrompt = false },
	})
}

// titlebarActivities lists the badges in the order the React store sorted them.
func (s *Shell) titlebarActivities() []titlebarActivity {
	var out []titlebarActivity
	add := func(a *titlebarActivity) {
		if a != nil {
			out = append(out, *a)
		}
	}
	add(transferActivity(s.transfers.items))
	add(s.elevatedActivity())
	add(s.xxmiFailureActivity())
	out = append(out, s.modFixActivities()...)
	add(fixer4001Activity(s.tools.fixer4001.task, s.tools.fixer4001.progress))
	add(bisectActivity(s.mods.bisect))
	add(compressionActivity(s.settings.compression))
	add(textureActivity(s.tools.textureProgress))
	add(s.xxmiUpdatesActivity())
	return out
}

func transferOpen(item transfer.Snapshot) bool {
	return item.Status == transfer.StatusPreparing || item.Status == transfer.StatusPending ||
		item.Status == transfer.StatusProgress
}

func transferFinalizing(item transfer.Snapshot) bool {
	return transferOpen(item) && item.Progress >= 100
}

func transferActivity(items []transfer.Snapshot) *titlebarActivity {
	var active, failed []transfer.Snapshot
	for _, item := range items {
		if transferOpen(item) || item.Status == transfer.StatusPaused {
			active = append(active, item)
		}
		if item.Status == transfer.StatusError || item.FailedFiles > 0 {
			failed = append(failed, item)
		}
	}
	isType := func(kind string) func(transfer.Snapshot) bool {
		return func(item transfer.Snapshot) bool { return item.Type == kind }
	}
	failedText := T("page.transfer.item.failed_files", "count", len(failed))

	if len(active) == 0 {
		if len(failed) == 0 {
			return nil
		}
		downloads, uploads := slices.ContainsFunc(
			failed,
			isType("download"),
		), slices.ContainsFunc(
			failed,
			isType("upload"),
		)
		label := "titlebar.activity.transfer.upload_error"
		switch {
		case downloads && uploads:
			label = "titlebar.activity.transfer.transfer_error"
		case downloads:
			label = "titlebar.activity.transfer.download_error"
		}
		detail := failedText
		if len(failed) == 1 {
			detail = truncateName(failed[0].Name, 16)
		}
		return &titlebarActivity{
			id: "transfer", label: T(label), detail: detail, icon: lucide.ArrowUpDown, status: activityError,
			path: string(RouteTransfer),
		}
	}

	open := slices.DeleteFunc(slices.Clone(active), func(item transfer.Snapshot) bool { return !transferOpen(item) })
	all := func(list []transfer.Snapshot, match func(transfer.Snapshot) bool) bool {
		return len(list) > 0 && !slices.ContainsFunc(list, func(item transfer.Snapshot) bool { return !match(item) })
	}
	uploadPhase := func(phase transfer.UploadPhase) func(transfer.Snapshot) bool {
		return func(item transfer.Snapshot) bool { return item.Type == "upload" && item.UploadPhase == phase }
	}
	paused := all(active, func(item transfer.Snapshot) bool { return item.Status == transfer.StatusPaused })
	downloads, uploads := slices.ContainsFunc(active, isType("download")), slices.ContainsFunc(active, isType("upload"))
	label := "titlebar.activity.transfer.transferring"
	switch {
	case paused:
		label = "titlebar.activity.transfer.paused"
	case all(open, transferFinalizing):
		label = "titlebar.activity.transfer.finalizing"
	case all(open, uploadPhase(transfer.UploadPreparing)):
		label = "page.transfer.item.upload_activity.preparing"
	case all(open, uploadPhase(transfer.UploadWaiting)):
		label = "page.transfer.item.upload_activity.waiting"
	case downloads && !uploads:
		label = "titlebar.activity.transfer.downloading"
	case uploads && !downloads:
		label = "titlebar.activity.transfer.uploading"
	}

	var speed float64
	for _, item := range active {
		if item.Status == transfer.StatusProgress && !transferFinalizing(item) &&
			item.UploadPhase != transfer.UploadPreparing && item.UploadPhase != transfer.UploadWaiting {
			speed += item.Speed
		}
	}
	var parts []string
	if speed > 0 {
		parts = append(parts, formatSize(int64(speed))+"/s")
	}
	if progress := transfer.AggregateProgress(items); progress != nil {
		parts = append(parts, itoa(int(math.Round(*progress)))+"%")
	}
	status := activityRunning
	switch {
	case len(failed) > 0:
		parts, status = append(parts, failedText), activityError
	case paused:
		status = activityPaused
	}
	return &titlebarActivity{
		id: "transfer", label: T(label), detail: strings.Join(parts, " · "), icon: lucide.ArrowUpDown, status: status,
		path: string(RouteTransfer),
	}
}

// truncateName keeps the first n characters of the trimmed name, as the React badges cut long names.
func truncateName(name string, n int) string {
	name = strings.TrimSpace(name)
	if runes := []rune(name); len(runes) > n {
		return string(runes[:n]) + "…"
	}
	return name
}

func (s *Shell) elevatedActivity() *titlebarActivity {
	a := &s.activity
	if !a.elevated.Enabled || a.elevated.Running {
		return nil
	}
	const prefix = "titlebar.activity.elevatedHelper."
	description := prefix + "description"
	if a.elevated.BlockedByAntivirus {
		description = prefix + "blockedDescription"
	}
	return &titlebarActivity{
		id: "elevated-helper", label: T(prefix + "label"), icon: lucide.ShieldAlert, status: activityWarning,
		popover: &activityPopover{
			title: T(prefix + "title"), description: T(description),
			action:   &activityButton{label: T(prefix + "start"), run: s.startElevatedHelper, disabled: a.starting},
			autoOpen: !a.prompted, opened: func() { a.prompted = true },
		},
	}
}

// xxmiFailureActivity shows the importers whose last launch failed to update XXMI or the mod runtimes. An update
// failure stays only while the update is still pending, so installing or skipping it clears the badge.
func (s *Shell) xxmiFailureActivity() *titlebarActivity {
	x := &s.xxmi
	var importers []string
	updateFailed, runtimesFailed := false, false
	for _, importer := range slices.Sorted(maps.Keys(x.launchFailures)) {
		pending := !x.updates.loaded || slices.ContainsFunc(x.updates.value, func(u xxmi.UpdateStatus) bool {
			return u.Importer == importer && u.Available && !u.Pinned
		})
		visible := false
		for _, stage := range x.launchFailures[importer] {
			switch {
			case stage == "mod-runtimes":
				runtimesFailed, visible = true, true
			case pending:
				updateFailed, visible = true, true
			}
		}
		if visible {
			importers = append(importers, importer)
		}
	}
	if len(importers) == 0 {
		return nil
	}
	const prefix = "titlebar.activity.xxmi."
	label := prefix + "modRuntimesFailed"
	var tooltips []string
	if updateFailed {
		label = prefix + "updateFailed"
		tooltips = append(tooltips, T(prefix+"updateFailedTooltip"))
	}
	if runtimesFailed {
		tooltips = append(tooltips, T(prefix+"modRuntimesFailedTooltip"))
	}
	path := string(RouteXXMI)
	if len(importers) == 1 {
		path += "/" + importers[0]
	}
	return &titlebarActivity{
		id: "xxmi:update-failure", label: T(label), detail: strings.Join(importers, ", "),
		tooltip: strings.Join(tooltips, " "), icon: lucide.CircleArrowUp, status: activityError, path: path,
	}
}

func (s *Shell) xxmiUpdatesActivity() *titlebarActivity {
	if s.deps.XXMI == nil {
		return nil
	}
	s.xxmiOverview()
	count := len(installableUpdates(s.xxmiUpdates()))
	if count == 0 {
		return nil
	}
	return &titlebarActivity{
		id: "xxmi:updates", label: T("titlebar.activity.xxmi.updates"), detail: itoa(count),
		tooltip: T("page.setting.xxmi.builtin.updatesPending", "count", count), icon: lucide.CircleArrowUp,
		status: activityWarning, path: string(RouteXXMI),
	}
}

func (s *Shell) modFixActivities() []titlebarActivity {
	var out []titlebarActivity
	for _, record := range s.mods.fixInspections.Inspections {
		out = append(out, titlebarActivity{
			id: "mod-fix:" + record.ModPath, label: T("titlebar.activity.modFix.label"),
			detail: truncateName(record.DisplayName, 8), icon: lucide.Wrench, status: activityWarning,
			popover: &activityPopover{
				title: T(
					"page.mod.fix_needed_toast.title",
					"name",
					record.DisplayName,
					"tool",
					record.Result.ToolName,
				),
				description: record.Result.Summary,
				action: &activityButton{
					label: T("page.mod.fix_needed_toast.action"), run: func() { s.openFixInspection(record) },
				},
				dismiss: &activityButton{
					label: T(
						"titlebar.activity.modFix.dismiss",
					),
					run: func() { s.dismissFixInspection(record.ModPath) },
				},
				autoOpen: true,
			},
		})
	}
	return out
}

func (s *Shell) setFixInspections(snapshot tools.FixInspectionSnapshot) {
	if snapshot.Revision >= s.mods.fixInspections.Revision {
		s.mods.fixInspections = snapshot
	}
}

// openFixInspection opens the mod fixer on the inspected mod, in the game whose importer and mod folder match it.
func (s *Shell) openFixInspection(record tools.FixInspectionRecord) {
	s.mods.pendingFix = &record
	s.navigate(RouteMod)
	s.applyPendingFix()
}

// applyPendingFix runs once the games are loaded. A fix that no game matches is dropped, so the fixer never opens
// later on its own.
func (s *Shell) applyPendingFix() {
	m := &s.mods
	record := m.pendingFix
	if !s.pendingFixReady() {
		return
	}
	m.pendingFix = nil
	i := slices.IndexFunc(m.games, func(game mod.GameConfig) bool {
		return strings.EqualFold(deref(game.Importer), record.Result.Importer) &&
			platform.SameOrChildPath(game.ModFolderPath, record.ModPath)
	})
	if i < 0 {
		return
	}
	s.selectGame(m.games[i].Game, true)
	if id, _ := s.modFixer(); id == "zzmi" {
		switch tool := record.Result.ActionTool; tool {
		case "hash", "jane", "dialyn":
			m.fix.zzmiTab = tool
		}
	}
	s.openModFixer(record.ModPath)
}

func (s *Shell) dismissFixInspection(path string) {
	if s.deps.Tools == nil {
		return
	}
	s.background(func(context.Context) { s.deps.Tools.DismissFixInspection(path) })
}

func fixer4001Activity(task, progress string) *titlebarActivity {
	if task == "" || fixer4001.IsTerminalProgressCode(progress) {
		return nil
	}
	const prefix = "titlebar.activity.fixer4001."
	label := map[string]string{
		"build-dll": "building", "diversify-dll": "diversifying", "restore-dll": "restoring",
	}[task]
	if label == "" {
		return nil
	}
	return &titlebarActivity{
		id: "tools:4001-fixer", label: T(prefix + label), tooltip: fixer4001ProgressText(progress), icon: lucide.Wrench,
		status: activityRunning, path: string(RouteTools) + "/" + string(tool4001Fixer),
	}
}

func bisectActivity(snapshot *tools.BisectSnapshot) *titlebarActivity {
	if snapshot == nil {
		return nil
	}
	switch snapshot.Status {
	case "scanning", "round", "reverting":
	default:
		return nil
	}
	return &titlebarActivity{
		id: "tools:mod-bisect", label: T("titlebar.activity.modBisect.running"), icon: lucide.GitCompare,
		status: activityRunning, path: string(RouteTools) + "/" + string(toolBisect),
	}
}

func compressionActivity(state *mod.CompressionState) *titlebarActivity {
	if state == nil {
		return nil
	}
	status := activityRunning
	switch state.Status {
	case "error":
		status = activityError
	case "compressing", "decompressing":
		if state.TotalFiles <= 0 && state.TotalBytes <= 0 {
			return nil
		}
	default:
		return nil
	}
	return &titlebarActivity{
		id:     "mod:compression",
		label:  T("titlebar.activity.modCompression." + state.Status),
		icon:   lucide.Archive,
		status: status,
		path:   "/setting/mod",
	}
}

func textureActivity(event *tools.TextureResizeProgressEvent) *titlebarActivity {
	if event == nil || event.Status != "running" {
		return nil
	}
	label := "resizing"
	switch operation := deref(event.Operation); operation {
	case "upscale":
		label = "upscaling"
	case "upscale_and_convert":
		label = "upscaling_and_converting"
	case "convert":
		label = "converting"
	case "resize_and_convert":
		label = "resizing_and_converting"
	}
	return &titlebarActivity{
		id: "tools:texture-resizer", label: T("titlebar.activity.textureResizer." + label),
		detail: deref(event.FileName), icon: lucide.Scaling, status: activityRunning, path: string(RouteMod),
	}
}

// activityClickNavigates reads the click-navigate setting, which is on until its stored value is known.
func (s *Shell) activityClickNavigates() bool {
	key := setting.KeyGeneralTitlebarActivityBadgeClickNavigate
	s.settingsLoaded(key)
	on, ok := s.settings.values[key].(bool)
	return !ok || on
}

// activityBadges shows the first activities and counts the rest, or returns nil when nothing runs.
func (s *Shell) activityBadges() *fairy.El {
	activities := s.titlebarActivities()
	visible := activities[:min(len(activities), maxVisibleActivities)]
	s.syncActivityPopovers(visible)
	if len(visible) == 0 {
		return nil
	}

	navigates := s.activityClickNavigates()
	row := fairy.Row().Key("ui.titlebar.activity").Gap(6).Align(fairy.Center).MinW(0).MarginEdges(0, 8, 0, 0)
	for _, activity := range visible {
		row.Child(s.activityBadge(activity, navigates))
	}
	if hidden := len(activities) - len(visible); hidden > 0 {
		row.Child(kit.Badge(T("titlebar.activity.more", "count", hidden), kit.Outline).FontSize(11).
			Color(kit.T().MutedForeground))
	}
	return row
}

// syncActivityPopovers opens a popover that opens by itself when its badge first shows, and forgets the open state
// of badges that are gone, as React mounted each popover with defaultOpen.
func (s *Shell) syncActivityPopovers(visible []titlebarActivity) {
	a := &s.activity
	shown := make(map[string]bool, len(visible))
	for _, activity := range visible {
		shown[activity.id] = true
		if p := activity.popover; p != nil && p.autoOpen && !a.shown[activity.id] {
			a.open[activity.id] = true
			if p.opened != nil {
				p.opened()
			}
		}
	}
	for id := range a.open {
		if !shown[id] {
			delete(a.open, id)
		}
	}
	a.shown = shown
}

// activityBadge is clickable only when it opens something; otherwise it is part of the window's drag region.
func (s *Shell) activityBadge(activity titlebarActivity, navigates bool) *fairy.El {
	t := kit.T()
	text := activity.label
	if activity.detail != "" {
		text += " · " + activity.detail
	}
	ink, hoverInk, border := t.MutedForeground, t.Foreground, opacity(t.Border, 0.6)
	if activity.status == activityError {
		ink, hoverInk, border = t.Destructive, t.Destructive, t.Destructive.Alpha(0.4)
	}
	badge := kit.Badge(text, kit.Secondary, kit.Icon(activity.icon)).Key(activity.id).MaxW(224).
		Fill(t.Muted.Alpha(0.7)).Border(1, border).Color(ink).FontSize(11)

	id := activity.id
	var onClick func()
	switch {
	case activity.popover != nil:
		onClick = func() { s.activity.open[id] = !s.activity.open[id] }
	case navigates && activity.path != "":
		path := activity.path
		onClick = func() { s.navigatePath(path) }
	default:
		return badge
	}
	badge.Cursor(fairy.CursorPointer).HoverFill(t.Muted).HoverColor(hoverInk).Animate(motion.Fast).Label(text).
		OnClick(onClick)
	if activity.popover == nil {
		if activity.tooltip != "" {
			badge.Tooltip(activity.tooltip)
		}
		return badge
	}
	open := s.activity.open[id]
	badge.Expanded(open)
	if !open {
		return badge
	}
	return badge.Popover(s.activityPanel(id, activity.popover), fairy.PopoverOptions{
		Side: fairy.SideBottom, Align: fairy.AlignStart, Gap: 6, OnDismiss: func() { delete(s.activity.open, id) },
	})
}

func (s *Shell) activityPanel(id string, p *activityPopover) *fairy.El {
	t := kit.T()
	closePanel := func() { delete(s.activity.open, id) }

	text := fairy.Col(fairy.Text(p.title).FontSize(14).SemiBold().Color(t.Foreground).Wrap()).Gap(4).Grow(1).MinW(0)
	if p.description != "" {
		text.Child(fairy.Text(p.description).FontSize(12).Color(t.MutedForeground).Wrap())
	}
	closeButton := fairy.Box(fairy.Icon(lucide.X).Size(14, 14)).Size(20, 20).Shrink(0).Center().Radius(4).
		Color(t.MutedForeground).HoverColor(t.Foreground).Cursor(fairy.CursorPointer).Label(T("g.close")).
		OnClick(closePanel)
	panel := fairy.Col(fairy.Row(text, closeButton).Gap(8).Align(fairy.Start))

	if p.action != nil || p.dismiss != nil {
		footer := fairy.Row(fairy.Spacer()).Gap(6).Align(fairy.Center).PadEdges(4, 0, 0, 0)
		if d := p.dismiss; d != nil {
			footer.Child(kit.Button(d.label, func() {
				d.run()
				closePanel()
			}, kit.Ghost, kit.Small).H(24))
		}
		if a := p.action; a != nil {
			footer.Child(kit.Button(a.label, a.run, kit.Small, kit.Icon(lucide.Wrench), kit.Disabled(a.disabled)).H(24))
		}
		panel.Child(footer)
	}
	// The React badges opened a PopoverContent, which was glass before the Mica backdrop.
	glass(panel, opacity(t.Popover, t.SurfaceAlpha), t.Card, t.SurfaceBlur)
	return panel.W(320).Pad(12).Gap(10).Radius(12).Border(1, t.Border).
		Shadow(0, 8, 24, 0, shadowColor(t)).Label(p.title)
}
