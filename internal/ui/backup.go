package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/backup"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

const (
	backupStatus    = "status"
	backupSchedule  = "schedule"
	backupTargets   = "targets"
	backupSnapshots = "snapshots"
)

var backupIntervals = []string{"6h", "12h", "24h", "7d"}

// backupKnownErrors are the codes the backup service answers with, which the locales name.
var backupKnownErrors = []string{
	"BACKUP_RUNNING", "BACKUP_NOT_LOGGED_IN", "BACKUP_NO_TARGETS", "BACKUP_TARGET_MISSING",
	"BACKUP_UNREADABLE_FILE", "BACKUP_NOT_DIRECTORY", "BACKUP_UPLOAD_FAILED", "BACKUP_INVALID_DEVICE_NAME",
	"RESTORE_DESTINATION_NOT_EMPTY", "RESTORE_HASH_MISMATCH",
}

type backupScreen struct {
	section string
	// active is set while the screen shows; opening it reads the overview again, as the React query refetched it.
	active   bool
	overview *backup.Overview
	loadErr  error
	loading  bool

	snapshots       []backup.Snapshot
	snapshotsErr    error
	snapshotsBusy   bool
	snapshotsLoaded bool

	restore *backupRestore
	// nameDraft and keepDraft hold the device name and keep count while they are edited.
	nameDraft *string
	keepDraft *string
}

type backupRestore struct {
	snapshot backup.Snapshot
	targets  []backup.ManifestTarget
	loaded   bool
	err      error
	// chosen is nil until the user changes it, which restores every folder.
	chosen      []string
	destination string
	busy        bool
}

// backupSectionOf reads the section of a /backup path; an unknown or missing one is the status section.
func backupSectionOf(path string) string {
	rest, _ := strings.CutPrefix(path, string(RouteBackup)+"/")
	section, _, _ := strings.Cut(rest, "/")
	if slices.Contains([]string{backupSchedule, backupTargets, backupSnapshots}, section) {
		return section
	}
	return backupStatus
}

// backupErrorMessage names a backup failure in the user's language when the service answered one of its codes.
func backupErrorMessage(message string) string {
	for _, code := range backupKnownErrors {
		if strings.Contains(message, code) {
			return T("page.backup.error." + code)
		}
	}
	return message
}

func (s *Shell) registerBackupEvents(on func(string, func([]any))) {
	b := &s.backup
	on(backup.EventStatus, func(data []any) {
		if status, ok := first(data).(backup.Status); ok && b.overview != nil {
			b.overview.Status = status
		}
	})
	on(backup.EventResult, func(data []any) {
		result, _ := first(data).(backup.RunResult)
		if b.overview != nil {
			s.loadBackupOverview()
		}
		if b.snapshotsLoaded {
			s.loadBackupSnapshots()
		}
		if result.Outcome == backup.OutcomeFailed {
			s.toasts.Show(kit.ToastError, T("page.backup.toast.failed"), backupErrorMessage(result.Error))
		}
	})
	on("backup:restore", func(data []any) {
		result, _ := first(data).(map[string]any)
		outcome, _ := result["outcome"].(string)
		switch outcome {
		case backup.OutcomeCompleted:
			destination, _ := result["destination"].(string)
			s.toasts.Show(kit.ToastSuccess, T("page.backup.toast.restored"), destination)
		case backup.OutcomeFailed:
			message, _ := result["error"].(string)
			s.toasts.Show(kit.ToastError, T("page.backup.toast.restore_failed"), backupErrorMessage(message))
		}
	})
}

func (s *Shell) backupFailed(err error, action string, fields map[string]any) {
	err = infra.ReportError(s.deps.Log, err, "Backup", infra.Diagnostic{Operation: "backup." + action, Fields: fields})
	s.toasts.Show(kit.ToastError, T("page.backup.title"), backupErrorMessage(err.Error()))
}

func (s *Shell) loadBackupOverview() {
	b := &s.backup
	if b.loading || s.deps.Backup == nil {
		return
	}
	b.loading = true
	go func() {
		overview, err := s.deps.Backup.GetOverview(context.Background())
		fairy.Go(func() {
			b.loading = false
			if err != nil {
				b.loadErr = infra.ReportError(s.deps.Log, err, "Backup", infra.Diagnostic{Operation: "backup.overview"})
				return
			}
			b.overview, b.loadErr = &overview, nil
		})
	}()
}

// backupFocused reads the overview and a listed snapshot list again when the window comes back to the front,
// as React refetched them on focus.
func (s *Shell) backupFocused() {
	if !s.backup.active {
		return
	}
	s.loadBackupOverview()
	if s.backup.snapshotsLoaded {
		s.loadBackupSnapshots()
	}
}

func (s *Shell) loadBackupSnapshots() {
	b := &s.backup
	if b.snapshotsBusy || s.deps.Backup == nil {
		return
	}
	b.snapshotsBusy = true
	go func() {
		snapshots, err := s.deps.Backup.ListSnapshots(context.Background())
		fairy.Go(func() {
			b.snapshotsBusy, b.snapshotsLoaded = false, true
			if err != nil {
				b.snapshotsErr = infra.ReportError(s.deps.Log, err, "Backup",
					infra.Diagnostic{Operation: "backup.snapshots"})
				return
			}
			b.snapshots, b.snapshotsErr = snapshots, nil
		})
	}()
}

// backupAction runs a service call that changes the overview, then reads the overview again.
func (s *Shell) backupAction(action string, fields map[string]any, run func(context.Context) error) {
	s.background(func(ctx context.Context) {
		err := run(ctx)
		fairy.Go(func() {
			if err != nil {
				s.backupFailed(err, action, fields)
			}
			s.loadBackupOverview()
		})
	})
}

func (s *Shell) backupPage(cx *fairy.Cx) *fairy.El {
	t := kit.T()
	b := &s.backup
	if b.section == "" {
		b.section = backupStatus
	}
	if !b.active {
		b.active = true
		s.loadBackupOverview()
		if b.snapshotsLoaded {
			s.loadBackupSnapshots()
		}
	}
	sections := []struct {
		id    string
		icon  *svg.Vector
		label string
	}{
		{backupStatus, lucide.Gauge, T("page.backup.status.title")},
		{backupSchedule, lucide.CalendarClock, T("page.backup.schedule.title")},
		{backupTargets, lucide.Folder, T("page.backup.targets.title")},
		{backupSnapshots, lucide.History, T("page.backup.snapshots.title")},
	}
	items := fairy.Col().Gap(2).Role(fairy.RoleList)
	nav := fairy.Col(
		fairy.Text(strings.ToUpper(T("page.backup.nav.sections"))).FontSize(10).LineHeight(15).LetterSpacing(1).
			Medium().Color(t.MutedForeground).PadEdges(0, 8, 8, 8),
		items,
	).Grow(1).PadXY(8, 12)
	title := ""
	for _, section := range sections {
		active := b.section == section.id
		if active {
			title = section.label
		}
		item := fairy.Row(
			fairy.Box(fairy.Icon(section.icon).Size(14, 14)).Size(20, 20).Center().Radius(4).
				Fill(pick(active, t.Accent.Alpha(0.2), t.Secondary)).Color(pick(active, t.Accent, t.MutedForeground)).
				Animate(motion.Fast),
			fairy.Text(section.label).FontSize(12).LineHeight(16).Ellipsis().Grow(1).MinW(0),
		).Key("backup.nav:"+section.id).PadXY(8, 8).Gap(10).Align(fairy.Center).Radius(6).Cursor(fairy.CursorPointer).
			HoverFill(t.SidebarAccent).Animate(motion.Fast).Selected(active).Label(section.label).OnClick(func() {
			if b.section != section.id {
				b.section = section.id
				s.loadBackupOverview()
			}
		})
		if active {
			item.Fill(t.SidebarAccent).Child(fairy.Icon(lucide.ChevronRight).Size(12, 12).Color(t.Accent))
		}
		items.Child(item)
	}
	sidebar := fairy.Col(
		fairy.Row(
			fairy.Box(fairy.Icon(lucide.DatabaseBackup).Size(14, 14).Color(t.Accent)).Size(28, 28).Center().Radius(4).
				Fill(t.Accent.Alpha(0.2)),
			fairy.Text(T("page.backup.title")).FontSize(14).LineHeight(20).LetterSpacing(-0.35).SemiBold(),
		).Pad(16).Gap(10).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border),
		nav,
	).W(256).Shrink(0).HFull().Fill(t.Background.Alpha(0.5)).BorderEdges(0, 1, 0, 0, t.Border)

	var content *fairy.El
	switch {
	case b.section == backupSnapshots:
		content = s.backupSnapshotsCard()
	case b.loadErr != nil:
		content = fairy.Text(backupErrorMessage(b.loadErr.Error())).FontSize(14).LineHeight(20).Color(t.Destructive)
	case b.overview == nil:
		content = fairy.Text(T("page.backup.loading")).FontSize(14).LineHeight(20).Color(t.MutedForeground)
	case b.section == backupSchedule:
		content = s.backupScheduleCard()
	case b.section == backupTargets:
		content = s.backupTargetsCard()
	default:
		content = s.backupStatusCard()
	}
	column := fairy.Col().WFull().MaxW(672).Pad(16).Gap(24)
	if b.section == backupStatus {
		column.Child(fairy.Text(T("page.backup.description")).FontSize(14).LineHeight(20).Color(t.MutedForeground))
	}
	column.Child(content)
	s.showBackupRestore(cx)

	main := fairy.Col(
		fairy.Row(
			fairy.Text(T("page.backup.title")).FontSize(12).LineHeight(16).Medium(),
			fairy.Text("/").FontSize(12).LineHeight(16).Color(t.MutedForeground),
			fairy.Text(title).FontSize(12).LineHeight(16),
		).H(40).Shrink(0).PadXY(16, 0).Gap(6).Align(fairy.Center).Font(t.MonoFont).
			BorderEdges(0, 0, 1, 0, t.Border),
		stableScrollY(fairy.Col(column).Key("backup.scroll").Grow(1).MinH(0).WFull().Align(fairy.Center)),
	).Grow(1).MinW(0).HFull().Fill(t.Background.Alpha(0.7))
	return fairy.Row(sidebar, main).WFull().HFull()
}

// backupCard uses the same section layout as the settings pages, with an optional header action.
func backupCard(title, description string, action *fairy.El, rows ...*fairy.El) *fairy.El {
	t := kit.T()
	label := fairy.Col(fairy.Text(title).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground)).
		Gap(2).Grow(1).MinW(0)
	if description != "" {
		label.Child(fairy.Text(description).FontSize(12).LineHeight(16).Color(t.MutedForeground))
	}
	head := fairy.Row(label).Gap(16).Align(fairy.Center).PadEdges(0, 4, 8, 4)
	if action != nil {
		head.Child(action.Shrink(0))
	}
	return fairy.Col(head, settingsGroup("", rows...)).WFull().Gap(4).FontSize(14)
}

func backupDate(at time.Time) string { return formatDateTime(at.Local(), true) }

func (s *Shell) backupStatusCard() *fairy.El {
	t := kit.T()
	b := &s.backup
	overview := b.overview
	status := overview.Status
	running := status.State != backup.StateIdle && status.State != ""
	auto := s.settingsLoaded(setting.KeyBackupEnabled) && s.boolSetting(setting.KeyBackupEnabled)

	badge := kit.Badge(T("page.backup.status.auto_off"), kit.Secondary)
	if auto {
		badge = kit.Badge(T("page.backup.status.auto_on"), kit.Default)
	}
	var action *fairy.El
	if running {
		action = kit.Button(T("g.cancel"), func() { go s.deps.Backup.Cancel() }, kit.Outline, kit.Small,
			kit.Icon(lucide.X))
	} else {
		action = kit.Button(T("page.backup.status.run_now"), func() {
			s.backupAction("run", nil, s.deps.Backup.RunNow)
		}, kit.Small, kit.Icon(lucide.CloudUpload))
	}

	var rows []*fairy.El
	if running {
		bytes := status.State == backup.StateUploading || status.State == backup.StateRestoring
		amount := ""
		var progress float64 = -1
		switch {
		case bytes:
			amount = formatSize(status.Bytes) + " / " + formatSize(status.TotalBytes)
			if status.TotalBytes > 0 {
				progress = min(1, float64(status.Bytes)/float64(status.TotalBytes))
			}
		case status.Total > 0:
			amount = itoa(status.Processed) + " / " + itoa(status.Total)
		}
		if progress < 0 && status.Total > 0 {
			progress = min(1, float64(status.Processed)/float64(status.Total))
		}
		block := fairy.Col(fairy.Row(
			fairy.Spinner().Size(16, 16).Color(t.MutedForeground),
			fairy.Text(T("page.backup.state."+status.State)).FontSize(14).LineHeight(20),
			fairy.Spacer(),
			fairy.Text(amount).FontSize(12).LineHeight(16).Color(t.MutedForeground),
		).Gap(8).Align(fairy.Center)).Gap(8).PadXY(0, 12)
		if progress >= 0 {
			block.Child(kit.Progress(progress).WFull())
		}
		rows = append(rows, block)
	}

	details := fairy.Col().Gap(8).PadXY(0, 12)
	last := overview.LastRun
	keys := []string{"page.backup.status.last_run", "page.backup.status.next_run"}
	if last != nil && last.Outcome == backup.OutcomeFailed && last.Error != "" {
		keys = append(keys, "page.backup.status.error")
	}
	line := func(key string, value *fairy.El) {
		label := fairy.Col(fairy.Text(T(key)).FontSize(14).LineHeight(20).Color(t.MutedForeground).NoWrap()).Shrink(0)
		// Zero-height samples give every row the same intrinsic label width, even when its value wraps.
		for _, sample := range keys {
			label.Child(fairy.Text(T(sample)).FontSize(14).NoWrap().H(0).Opacity(0).Role(fairy.RoleHidden))
		}
		details.Child(fairy.Row(
			label,
			value.FontSize(14).Grow(1).MinW(0),
		).Gap(24).Align(fairy.Start))
	}
	if last == nil {
		line("page.backup.status.last_run",
			fairy.Text(T("page.backup.status.never")).LineHeight(20).Color(t.MutedForeground))
	} else {
		value := fairy.Row(fairy.Text(backupDate(last.At)).LineHeight(20)).Gap(8).Align(fairy.Center).Wrap()
		variant := kit.Outline
		if last.Outcome == backup.OutcomeFailed {
			variant = kit.Destructive
		}
		value.Child(kit.Badge(T("page.backup.outcome."+last.Outcome), variant))
		if last.Outcome == backup.OutcomeCompleted {
			value.Child(fairy.Text(T("page.backup.status.files", "count", last.FileCount) + " · " +
				formatSize(last.TotalSize)).FontSize(12).LineHeight(16).Color(t.MutedForeground))
		}
		line("page.backup.status.last_run", value)
		if last.Outcome == backup.OutcomeFailed && last.Error != "" {
			line(
				"page.backup.status.error",
				fairy.Text(backupErrorMessage(last.Error)).LineHeight(20).Color(t.Destructive),
			)
		}
	}
	next := T("page.backup.status.not_scheduled")
	if overview.NextRunAt != nil {
		next = backupDate(*overview.NextRunAt)
	}
	line("page.backup.status.next_run", fairy.Text(next).LineHeight(20))
	rows = append(rows, details)
	// The React section header put the action in a grid cell spanning the title and description rows, so the
	// title stays at the top instead of centring on the taller button.
	head := fairy.Row(
		fairy.Row(
			fairy.Text(T("page.backup.status.title")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
			badge,
		).Gap(8).Align(fairy.Center),
		fairy.Spacer(), action,
	).Gap(8).Align(fairy.Start).PadEdges(0, 4, 8, 4)
	return fairy.Col(head, settingsGroup("", rows...)).WFull().Gap(4).FontSize(14)
}

func (s *Shell) backupScheduleCard() *fairy.El {
	b := &s.backup
	keys := []string{
		setting.KeyBackupEnabled, setting.KeyBackupInterval, setting.KeyBackupOnStartup,
		setting.KeyBackupWatchChanges, setting.KeyBackupKeepCount,
	}
	if !s.settingsLoaded(keys...) {
		return s.settingsLoading()
	}
	prefix := "page.backup.schedule."
	intervals := make([]kit.Item, len(backupIntervals))
	for i, value := range backupIntervals {
		intervals[i] = kit.Item{Value: value, Label: T(prefix + "interval.options." + value)}
	}

	keep := itoa(s.intSetting(setting.KeyBackupKeepCount))
	if b.keepDraft != nil {
		keep = *b.keepDraft
	}
	saveKeep := func() {
		if b.keepDraft == nil {
			return
		}
		draft := *b.keepDraft
		b.keepDraft = nil
		if n, ok := parseInt(draft); ok {
			n = min(30, max(1, n))
			if n != s.intSetting(setting.KeyBackupKeepCount) {
				s.setSetting(setting.KeyBackupKeepCount, n)
			}
		}
	}
	keepField := kit.TextField(keep, func(text string) { b.keepDraft = &text },
		kit.Label(T(prefix+"keep_count.title"))).Key("backup.keep").W(96).Grow(0).OnBlur(saveKeep).
		OnSubmit(func(string) { saveKeep() })

	name := b.overview.DeviceName
	if b.nameDraft != nil {
		name = *b.nameDraft
	}
	saveName := func() {
		if b.nameDraft == nil {
			return
		}
		trimmed := strings.TrimSpace(*b.nameDraft)
		b.nameDraft = nil
		if trimmed == b.overview.DeviceName {
			return
		}
		s.backupAction("rename-device", map[string]any{"name": trimmed}, func(ctx context.Context) error {
			return s.deps.Backup.RenameDevice(ctx, trimmed)
		})
	}
	nameField := kit.TextField(name, func(text string) {
		if len([]rune(text)) <= 80 {
			b.nameDraft = &text
		}
	}, kit.Label(T(prefix+"device_name.title"))).Key("backup.device").W(224).Grow(0).OnBlur(saveName).
		OnSubmit(func(string) { saveName() })

	return backupCard(T(prefix+"title"), "", nil,
		s.settingSwitch(setting.KeyBackupEnabled, T(prefix+"enabled.title"), T(prefix+"enabled.description")),
		settingRow(T(prefix+"interval.title"), T(prefix+"interval.description"),
			s.settingSelect(setting.KeyBackupInterval, intervals, 160, kit.Label(T(prefix+"interval.title")))),
		s.settingSwitch(setting.KeyBackupOnStartup, T(prefix+"on_startup.title"), T(prefix+"on_startup.description")),
		s.settingSwitch(setting.KeyBackupWatchChanges, T(prefix+"watch_changes.title"),
			T(prefix+"watch_changes.description")),
		settingRow(T(prefix+"keep_count.title"), T(prefix+"keep_count.description"), keepField),
		settingRow(T(prefix+"device_name.title"), T(prefix+"device_name.description"), nameField),
	)
}

func (s *Shell) backupTargetsCard() *fairy.El {
	t := kit.T()
	targets := s.backup.overview.Targets
	add := kit.Button(T("page.backup.targets.add_folder"), func() {
		go func() {
			result, err := s.deps.Dialog.SelectDirectory("backup-target")
			if err != nil {
				fairy.Go(func() { s.backupFailed(err, "pick-target", nil) })
				return
			}
			if result.Canceled || result.FilePath == "" {
				return
			}
			fairy.Go(func() {
				s.backupAction("add-target", map[string]any{"path": result.FilePath}, func(ctx context.Context) error {
					return s.deps.Backup.AddCustomPath(ctx, result.FilePath)
				})
			})
		}()
	}, kit.Outline, kit.Small, kit.Icon(lucide.FolderPlus))

	var rows []*fairy.El
	if len(targets) == 0 {
		rows = append(
			rows,
			fairy.Text(T("page.backup.targets.empty")).FontSize(14).LineHeight(20).Color(t.MutedForeground).WFull().
				TextAlign(fairy.TextCenter).PadXY(0, 24),
		)
	}
	row := func(target backup.Target, icon *svg.Vector, control *fairy.El) *fairy.El {
		title := fairy.Row(fairy.Text(target.Label).FontSize(14).LineHeight(20).Medium().Ellipsis().MinW(0)).
			Gap(8).
			Align(fairy.Center)
		if target.Missing {
			title.Child(kit.Badge(T("page.backup.targets.missing"), kit.Destructive))
		}
		if target.Excluded {
			title.Child(kit.Badge(T("page.backup.targets.excluded"), kit.Secondary))
		}
		return fairy.Row(
			fairy.Icon(icon).Size(16, 16).Color(t.MutedForeground).Shrink(0),
			fairy.Col(title, fairy.Text(target.Path).FontSize(12).LineHeight(16).Color(t.MutedForeground).Ellipsis().MinW(0)).
				Grow(1).
				MinW(0),
			control.Shrink(0),
		).Key("backup.target:"+target.ID).Gap(12).Align(fairy.Center).PadXY(0, 12)
	}
	for _, target := range targets {
		if target.Kind != "game" {
			continue
		}
		game := target.Label
		if target.Game != nil {
			game = *target.Game
		}
		rows = append(rows, row(target, lucide.Gamepad, kit.Switch(!target.Excluded, func(included bool) {
			s.backupAction("exclude-game", map[string]any{"game": game}, func(ctx context.Context) error {
				return s.deps.Backup.SetGameExcluded(ctx, game, !included)
			})
		}).Label(T("page.backup.targets.include", "name", target.Label))))
	}
	for _, target := range targets {
		if target.Kind != "custom" {
			continue
		}
		label := T("page.backup.targets.remove", "name", target.Label)
		rows = append(rows, row(target, lucide.Folder, kit.IconButton(lucide.Trash, func() {
			s.backupAction("remove-target", map[string]any{"id": target.ID}, func(ctx context.Context) error {
				return s.deps.Backup.RemoveCustomPath(ctx, target.ID)
			})
		}, kit.Ghost, kit.Small, kit.Tooltip(label)).Label(label)))
	}
	return backupCard(T("page.backup.targets.title"), T("page.backup.targets.description"), add, rows...)
}

func (s *Shell) backupBusy() bool {
	o := s.backup.overview
	return o != nil && o.Status.State != backup.StateIdle && o.Status.State != ""
}

func (s *Shell) backupSnapshotsCard() *fairy.El {
	t := kit.T()
	b := &s.backup
	if !b.snapshotsLoaded && !b.snapshotsBusy && b.snapshotsErr == nil {
		s.loadBackupSnapshots()
	}
	busy := s.backupBusy()
	refreshLabel := T("page.backup.snapshots.refresh")
	refresh := kit.IconButton(lucide.RefreshCw, s.loadBackupSnapshots, kit.Ghost, kit.Small,
		kit.Loading(b.snapshotsBusy), kit.Tooltip(refreshLabel)).Label(refreshLabel)

	note := func(text string, color fairy.Color) *fairy.El {
		return fairy.Text(text).
			FontSize(14).
			LineHeight(20).
			Color(color).
			WFull().
			TextAlign(fairy.TextCenter).
			PadXY(0, 24)
	}
	var rows []*fairy.El
	switch {
	case b.snapshotsErr != nil:
		rows = append(rows, note(backupErrorMessage(b.snapshotsErr.Error()), t.Destructive))
	case len(b.snapshots) == 0 && !b.snapshotsLoaded:
		rows = append(rows, note(T("page.backup.snapshots.loading"), t.MutedForeground))
	case len(b.snapshots) == 0:
		rows = append(rows, note(T("page.backup.snapshots.empty"), t.MutedForeground))
	}
	if b.snapshotsErr == nil {
		for _, snapshot := range b.snapshots {
			rows = append(rows, s.backupSnapshotRow(snapshot, busy))
		}
	}
	return backupCard(T("page.backup.snapshots.title"), T("page.backup.snapshots.description"), refresh, rows...)
}

func snapshotDate(snapshot backup.Snapshot) string {
	if snapshot.CompletedAt != nil {
		return backupDate(*snapshot.CompletedAt)
	}
	return backupDate(snapshot.CreatedAt)
}

func (s *Shell) backupSnapshotRow(snapshot backup.Snapshot, busy bool) *fairy.El {
	t := kit.T()
	title := fairy.Row(
		fairy.Text(snapshotDate(snapshot)).FontSize(14).LineHeight(20).Medium(),
		kit.Badge(T("page.backup.trigger."+snapshot.Trigger), kit.Outline),
	).Gap(8).Align(fairy.Center)
	if snapshot.State == "PENDING" {
		title.Child(kit.Badge(T("page.backup.snapshots.pending"), kit.Secondary))
	}
	details := T("page.backup.status.files", "count", snapshot.FileCount) + " · " + formatSize(snapshot.TotalSize)
	var skipped int32
	for _, count := range snapshot.Skipped {
		skipped += count
	}
	if skipped > 0 {
		details += " · " + T("page.backup.snapshots.skipped", "count", skipped)
	}
	deleteLabel := T("page.backup.snapshots.delete")
	return fairy.Row(
		fairy.Col(title, fairy.Text(details).FontSize(12).LineHeight(16).Color(t.MutedForeground)).Grow(1).MinW(0),
		kit.Button(T("page.backup.snapshots.restore"), func() {
			s.openBackupRestore(snapshot)
		}, kit.Outline, kit.Small, kit.Icon(lucide.ArchiveRestore), kit.Disabled(busy || snapshot.State != "COMPLETED")),
		kit.IconButton(lucide.Trash, func() { s.confirmDeleteSnapshot(snapshot) }, kit.Ghost, kit.Small,
			kit.Disabled(busy), kit.Tooltip(deleteLabel)).Label(deleteLabel),
	).Key("backup.snapshot:"+snapshot.ID).Gap(12).Align(fairy.Center).PadXY(0, 12)
}

func (s *Shell) confirmDeleteSnapshot(snapshot backup.Snapshot) {
	s.Confirm(Confirmation{
		Title:        T("page.backup.snapshots.delete_confirm.title"),
		Description:  T("page.backup.snapshots.delete_confirm.description", "date", snapshotDate(snapshot)),
		ConfirmLabel: T("page.backup.snapshots.delete"),
		Danger:       true,
		OnResult: func(confirmed bool) {
			if !confirmed {
				return
			}
			go func() {
				err := s.deps.Backup.DeleteSnapshot(context.Background(), snapshot.ID)
				fairy.Go(func() {
					if err != nil {
						s.backupFailed(err, "delete-snapshot", map[string]any{"snapshotId": snapshot.ID})
					}
					s.loadBackupSnapshots()
				})
			}()
		},
	})
}

func (s *Shell) openBackupRestore(snapshot backup.Snapshot) {
	r := &backupRestore{snapshot: snapshot}
	s.backup.restore = r
	go func() {
		targets, err := s.deps.Backup.GetSnapshotTargets(context.Background(), snapshot.ID)
		fairy.Go(func() {
			r.loaded = true
			if err != nil {
				r.err = infra.ReportError(s.deps.Log, err, "Backup", infra.Diagnostic{
					Operation: "backup.snapshot-targets", Fields: map[string]any{"snapshotId": snapshot.ID},
				})
				return
			}
			r.targets = targets
		})
	}()
}

func (r *backupRestore) chosenIDs() []string {
	if r.chosen != nil {
		return r.chosen
	}
	ids := make([]string, len(r.targets))
	for i, target := range r.targets {
		ids[i] = target.ID
	}
	return ids
}

func (s *Shell) showBackupRestore(cx *fairy.Cx) {
	t := kit.T()
	r := s.backup.restore
	if r == nil {
		return
	}
	closeDialog := func() {
		if s.backup.restore == r {
			s.backup.restore = nil
		}
	}
	chosen := r.chosenIDs()
	list := fairy.Col().Gap(8)
	switch {
	case r.err != nil:
		list.Child(fairy.Text(backupErrorMessage(r.err.Error())).FontSize(14).LineHeight(20).Color(t.Destructive))
	case !r.loaded:
		list.Child(fairy.Spinner().Size(16, 16))
	}
	for _, target := range r.targets {
		on := slices.Contains(chosen, target.ID)
		list.Child(fairy.Row(
			kit.Checkbox(on, func(checked bool) {
				next := slices.DeleteFunc(slices.Clone(chosen), func(id string) bool { return id == target.ID })
				if checked {
					next = append(next, target.ID)
				}
				r.chosen = next
			}).Label(target.Label),
			fairy.Text(target.Label).FontSize(14).LineHeight(20).Medium().Shrink(0),
			fairy.Text(target.SourcePath).
				FontSize(12).
				LineHeight(16).
				Color(t.MutedForeground).
				Ellipsis().
				MinW(0).
				TooltipWhenTruncated(),
		).Gap(8).Align(fairy.Center))
	}
	destination := r.destination
	if destination == "" {
		destination = T("page.backup.restore.no_folder")
	}
	pickFolder := func() {
		go func() {
			result, err := s.deps.Dialog.SelectDirectory("backup-destination")
			fairy.Go(func() {
				switch {
				case err != nil:
					s.backupFailed(err, "pick-destination", nil)
				case !result.Canceled && result.FilePath != "":
					r.destination = result.FilePath
				}
			})
		}()
	}
	start := func() {
		if r.busy {
			return
		}
		r.busy = true
		ids, dest, id := chosen, r.destination, r.snapshot.ID
		go func() {
			err := s.deps.Backup.Restore(context.Background(), id, ids, dest)
			fairy.Go(func() {
				r.busy = false
				if err != nil {
					s.backupFailed(err, "restore", map[string]any{"snapshotId": id, "destination": dest})
					return
				}
				s.toasts.Show(kit.ToastInfo, T("page.backup.restore.started"), "")
				closeDialog()
			})
		}()
	}
	body := fairy.Col(list, fairy.Row(
		kit.Button(T("page.backup.restore.choose_folder"), pickFolder, kit.Outline, kit.Small,
			kit.Icon(lucide.FolderOpen)),
		fairy.Text(destination).
			FontSize(12).
			LineHeight(16).
			Color(t.MutedForeground).
			Ellipsis().
			MinW(0),
	).Gap(8).Align(fairy.Center)).Gap(16)
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T("page.backup.restore.title"),
		Description: T("page.backup.restore.description"),
		Body:        body,
		Width:       384,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), closeDialog, kit.Outline),
			kit.Button(T("page.backup.restore.start"), start,
				kit.Disabled(r.destination == "" || len(chosen) == 0 || r.busy), kit.Loading(r.busy)),
		},
		OnClose: closeDialog,
		Key:     "backup.restore",
	})
}
