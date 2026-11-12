package ui

import (
	"cmp"
	"context"
	"embed"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/tools"
	fixtool "nahida.live/desktop/internal/tools/fix_tool"
)

const (
	wuwaPrefix = "page.mod.dialog.wuwa-fix-runner."
	zzmiPrefix = "page.mod.dialog.zzmi-fix-runner."
	// restoreAllKey asks to roll back to the oldest Wuwa backup group.
	restoreAllKey       = "__RESTORE_ALL__"
	fixInspectionsEvent = "tools:fix-inspections"
)

// fixRunner runs fix tool scripts, presets and the Wuwa and ZZMI mod fixers on one mod folder at a time, and
// shows their output in a log dialog.
type fixRunner struct {
	scripts      []db.ScriptBasicRow
	presets      []db.ScriptPresetWithScripts
	listsLoading bool

	modPath   string
	preparing bool

	showLog bool
	logs    []string
	running bool
	// kind is "script", "wuwa" or "zzmi"; the ZZMI fixer reads no input.
	kind  string
	input string

	wuwa *wuwaFixer
	zzmi *zzmiFixer
	// zzmiTab is the tab the next ZZMI fixer opens on, which a fix inspection names; "" opens the hash fix.
	zzmiTab string
}

type wuwaFixer struct {
	prepare tools.WuwaFixerPrepareResult
	// dialog is "install", "update" or "options".
	dialog  string
	tab     string
	options tools.WuwaFixerOptions

	backups       []tools.WuwaBackupGroup
	backupSize    tools.WuwaBackupSize
	backupsLoaded bool
	backupsBusy   bool
	busy          bool
	pending       string
	advanced      bool
	cleanConfirm  bool
	cleanInput    string
}

type zzmiFixer struct {
	prepare    tools.ZZMIFixerPrepareResult
	tab        string
	updateBusy bool

	backups       []tools.ZZMIBackupSession
	backupsLoaded bool
	backupsBusy   bool
	busy          bool
	conflicts     []tools.ZZMIFixerRestoreConflict
	restore       *zzmiTarget
	remove        *zzmiTarget
	removeAll     bool
}

type zzmiTarget struct {
	session string
	entry   *string
}

// modFixerOf names the mod fixer of the importer: ZZMI for ZZMI, Wuwa for WWMI or a game without an importer.
func modFixerOf(importer string) (id, label string) {
	switch strings.ToUpper(importer) {
	case "ZZMI":
		return "zzmi", T("page.mod.character-sidebar.zzmi-mod-fixer")
	case "", "WWMI":
		return "wuwa", T("page.mod.character-sidebar.wuwa-mod-fixer")
	}
	return "", ""
}

func (s *Shell) modFixer() (id, label string) {
	game := s.gameConfig()
	if game == nil || s.deps.Tools == nil || s.isNteGame() {
		return "", ""
	}
	return modFixerOf(deref(game.Importer))
}

func (s *Shell) registerFixEvents(on func(string, func([]any))) {
	on(fixInspectionsEvent, func(data []any) {
		if snapshot, ok := first(data).(tools.FixInspectionSnapshot); ok {
			s.setFixInspections(snapshot)
		}
	})
	on(fixtool.LogEventName, func(data []any) {
		r := &s.mods.fix
		event, ok := first(data).(fixtool.FixToolLogEvent)
		if !ok || !r.showLog {
			return
		}
		if event.ReplaceLast && len(r.logs) > 0 {
			r.logs[len(r.logs)-1] = event.Message
			return
		}
		r.logs = append(r.logs, event.Message)
	})
}

// loadFixLists reads the fix tool scripts and presets that the mod context menu offers.
func (s *Shell) loadFixLists() {
	r := &s.mods.fix
	if r.listsLoading || s.deps.Tools == nil {
		return
	}
	r.listsLoading = true
	s.backgroundRead(func(ctx context.Context) {
		scripts, err := s.deps.Tools.GetScripts(ctx)
		if err != nil {
			s.modError(err, "read-fix-scripts", nil)
		}
		presets, presetErr := s.deps.Tools.GetPresets(ctx)
		if presetErr != nil {
			s.modError(presetErr, "read-fix-presets", nil)
		}
		fairy.Go(func() {
			r.listsLoading = false
			if err == nil {
				r.scripts = scripts
			}
			if presetErr == nil {
				r.presets = presets
			}
		})
	})
}

// fixMenuItems is the Fix group of the mod context menu.
func (s *Shell) fixMenuItems(path string) []kit.MenuItem {
	r := &s.mods.fix
	s.loadFixLists()
	presets := make([]kit.MenuItem, 0, len(r.presets))
	for _, preset := range r.presets {
		id := preset.ID
		presets = append(presets, kit.MenuItem{Label: preset.Name, OnClick: func() { s.runFixScript(true, id, path) }})
	}
	if len(presets) == 0 {
		presets = append(presets, kit.MenuItem{Label: T("page.mod.context-menu.no_presets"), Disabled: true})
	}
	scripts := make([]kit.MenuItem, 0, len(r.scripts))
	for _, script := range r.scripts {
		id := script.ID
		scripts = append(scripts, kit.MenuItem{Label: script.Name, OnClick: func() { s.runFixScript(false, id, path) }})
	}
	if len(scripts) == 0 {
		scripts = append(scripts, kit.MenuItem{Label: T("page.mod.context-menu.no_fix_tools"), Disabled: true})
	}
	items := []kit.MenuItem{
		{Label: T("page.mod.context-menu.group-fix"), Heading: true},
		{Label: T("page.mod.context-menu.preset"), Submenu: presets},
		{Label: T("page.mod.context-menu.fix_tool"), Submenu: scripts},
	}
	if id, label := s.modFixer(); id != "" {
		items = append(items, kit.MenuItem{
			Label: label, Icon: lucide.Wrench, Image: modFixerImage(id, 16), Disabled: r.preparing,
			OnClick: func() { s.openModFixer(path) },
		})
	}
	return append(items, kit.MenuItem{Separator: true})
}

// modCardToolItems is the tools menu of a mod card: the mod's tools, then the fix presets and scripts.
func (s *Shell) modCardToolItems(info mod.ModInfo) []kit.MenuItem {
	r := &s.mods.fix
	s.loadFixLists()
	items := append([]kit.MenuItem{{Label: T("page.mod.card.mod_tools"), Heading: true}}, s.modFileToolItems(info)...)
	if id, label := s.modFixer(); id != "" {
		items = append(items, kit.MenuItem{
			Label: label, Icon: lucide.Wrench, Image: modFixerImage(id, 16), Disabled: r.preparing,
			OnClick: func() { s.openModFixer(info.Path) },
		})
	}

	items = append(items,
		kit.MenuItem{Separator: true},
		kit.MenuItem{Label: T("page.mod.card.preset_count", "total", len(r.presets)), Heading: true},
	)
	for _, preset := range r.presets {
		id := preset.ID
		items = append(items, kit.MenuItem{Label: preset.Name, OnClick: func() { s.runFixScript(true, id, info.Path) }})
	}
	items = append(
		items,
		kit.MenuItem{Label: T("page.mod.card.fix_tool_count", "total", len(r.scripts)), Heading: true},
	)
	for _, script := range r.scripts {
		id := script.ID
		items = append(
			items,
			kit.MenuItem{Label: script.Name, OnClick: func() { s.runFixScript(false, id, info.Path) }},
		)
	}
	return items
}

// modFixerMenu is the Tools submenu of a folder, which runs the mod fixer on the whole folder.
func (s *Shell) modFixerMenu(path string) *kit.MenuItem {
	id, label := s.modFixer()
	if id == "" {
		return nil
	}
	return &kit.MenuItem{
		Label: T("page.mod.character-sidebar.tools"), Icon: lucide.Wrench,
		Submenu: []kit.MenuItem{{
			Label: label, Icon: lucide.Wrench, Image: modFixerImage(id, 16), Disabled: s.mods.fix.preparing,
			OnClick: func() { s.openModFixer(path) },
		}},
	}
}

func (s *Shell) startFixLog(kind, path string) {
	r := &s.mods.fix
	r.modPath, r.kind = path, kind
	r.showLog, r.logs, r.running, r.input = true, nil, true, ""
}

func (s *Shell) runFixScript(preset bool, id, path string) {
	r := &s.mods.fix
	if r.running {
		return
	}
	s.startFixLog("script", path)
	s.background(func(ctx context.Context) {
		var err error
		if preset {
			err = s.deps.Tools.RunPreset(ctx, id, path)
		} else {
			err = s.deps.Tools.RunScript(ctx, id, path)
		}
		fairy.Go(func() {
			r.running = false
			if err != nil && !infra.IsCancellationError(err) {
				s.modError(err, "run-fix-script", map[string]any{"id": id, "preset": preset, "path": path})
			}
			s.refreshListing()
		})
	})
}

func (s *Shell) cancelFixRun() {
	go s.deps.Tools.CancelRun()
}

func (s *Shell) sendFixInput() {
	r := &s.mods.fix
	if !r.running || r.kind == "zzmi" {
		return
	}
	input := r.input
	r.input = ""
	go s.deps.Tools.SendInput(input + "\r\n")
}

func (s *Shell) fixLogDialog(cx *fairy.Cx) {
	r := &s.mods.fix
	if !r.showLog {
		return
	}
	t := kit.T()
	lines := fairy.Col().Gap(8).Pad(12)
	for _, line := range r.logs {
		ink := t.Foreground
		switch lower := strings.ToLower(line); {
		case strings.Contains(lower, "warning"):
			ink = t.Warning
		case strings.Contains(lower, "error"):
			ink = t.Destructive
		case strings.Contains(lower, "complete"):
			ink = t.Success
		}
		lines.Child(fairy.Row(
			fairy.Icon(lucide.ChevronRight).Size(16, 16).Shrink(0),
			fairy.Text(line).Color(ink).Wrap().Grow(1).MinW(0),
		).Gap(4))
	}
	if r.running {
		lines.Child(fairy.Text(T("page.mod.log-dialog.running")).Color(t.Accent))
	}
	body := fairy.Col(
		fairy.Col(lines).Key("mod.fix-log").H(360).ScrollY().StickToBottom().Radius(6).Border(1, t.Border).
			Fill(t.Muted).Font(monoFont).FontSize(12),
	).Gap(12)
	if r.kind != "zzmi" {
		placeholder := T(wuwaPrefix + "log.input_placeholder")
		field := kit.TextField(r.input, func(v string) { r.input = v }, kit.Placeholder(placeholder),
			kit.Disabled(!r.running)).Label(placeholder).Grow(1).MinW(0)
		field.OnSubmit(func(string) { s.sendFixInput() })
		if r.running {
			field.AutoFocus()
		}
		send := T(wuwaPrefix + "log.send_input")
		body.Child(fairy.Row(field, kit.IconButton(lucide.SquareTerminal, s.sendFixInput, kit.Outline,
			kit.Disabled(!r.running), kit.Tooltip(send)).Label(send)).Gap(8))
	}
	action := kit.Button(T("g.close"), func() { r.showLog = false })
	if r.running {
		action = kit.Button(T("g.cancel"), s.cancelFixRun, kit.Destructive)
	}
	// As React's alert dialog, only the action and Escape close the log: a click beside it does nothing, and
	// Escape cancels a running tool instead of hiding its log.
	kit.ShowDialog(cx, kit.DialogProps{
		Title:   T("page.mod.log-dialog.title"),
		Body:    body,
		Width:   640,
		Actions: []*fairy.El{action},
		Key:     "mod.fix-log",
		OnEscape: func() {
			if r.running {
				s.cancelFixRun()
				return
			}
			r.showLog = false
		},
	})
}

func (s *Shell) openModFixer(path string) {
	switch id, _ := s.modFixer(); id {
	case "zzmi":
		s.openZZMIFixer(path)
	case "wuwa":
		s.openWuwaFixer(path)
	}
}

func (s *Shell) fixerFailed(err error, operation, path string) {
	if infra.IsCancellationError(err) {
		return
	}
	s.modError(err, operation, map[string]any{"path": path})
	s.toasts.Show(kit.ToastError, err.Error(), "")
}

func (s *Shell) openWuwaFixer(path string) {
	r := &s.mods.fix
	if r.preparing {
		return
	}
	game := s.gameConfig()
	if game == nil {
		return
	}
	r.modPath, r.preparing = path, true
	importer := game.Importer
	s.background(func(ctx context.Context) {
		result, err := s.deps.Tools.WuwaFixerPrepareRun(ctx, importer)
		fairy.Go(func() {
			r.preparing = false
			if err != nil {
				s.fixerFailed(err, "wuwa-fixer-prepare", path)
				return
			}
			w := &wuwaFixer{prepare: result}
			r.wuwa = w
			switch {
			case !result.Supported:
				r.wuwa = nil
				s.toasts.Show(kit.ToastError, T(wuwaPrefix+"unsupported_game"), "")
			case !result.Installed:
				w.dialog = "install"
			case result.UpdateAvailable:
				w.dialog = "update"
			default:
				w.openOptions()
			}
		})
	})
}

func (w *wuwaFixer) openOptions() {
	w.dialog, w.tab, w.options = "options", "fix", tools.WuwaFixerOptions{AeroFix: "none"}
	w.pending, w.advanced, w.cleanConfirm, w.cleanInput = "", false, false, ""
}

func (s *Shell) installWuwaFixer(w *wuwaFixer) {
	r := &s.mods.fix
	if r.preparing {
		return
	}
	game := s.gameConfig()
	if game == nil {
		return
	}
	r.preparing = true
	importer := game.Importer
	path := r.modPath
	s.background(func(ctx context.Context) {
		_, err := s.deps.Tools.WuwaFixerInstallOrUpdate(ctx)
		var result tools.WuwaFixerPrepareResult
		if err == nil {
			result, err = s.deps.Tools.WuwaFixerPrepareRun(ctx, importer)
		}
		fairy.Go(func() {
			r.preparing = false
			if err != nil {
				s.fixerFailed(err, "wuwa-fixer-install", path)
				return
			}
			w.prepare = result
			w.openOptions()
		})
	})
}

func (s *Shell) runWuwaFixer(w *wuwaFixer) {
	r := &s.mods.fix
	if r.running || w.busy || r.modPath == "" {
		return
	}
	path, options := r.modPath, w.options
	r.wuwa = nil
	s.startFixLog("wuwa", path)
	s.background(func(ctx context.Context) {
		err := s.deps.Tools.WuwaFixerRun(ctx, path, options)
		fairy.Go(func() {
			r.running = false
			if err != nil {
				s.fixerFailed(err, "wuwa-fixer-run", path)
			}
			s.refreshListing()
		})
	})
}

func (s *Shell) loadWuwaBackups(w *wuwaFixer) {
	r := &s.mods.fix
	if w.backupsBusy {
		return
	}
	w.backupsBusy = true
	path := r.modPath
	s.background(func(ctx context.Context) {
		groups, err := s.deps.Tools.WuwaFixerScanBackups(ctx, path)
		var size tools.WuwaBackupSize
		if err == nil {
			size, err = s.deps.Tools.WuwaFixerGetBackupSize(ctx, path)
		}
		fairy.Go(func() {
			w.backupsBusy, w.backupsLoaded = false, true
			if err != nil {
				s.fixerFailed(err, "wuwa-fixer-scan-backups", path)
				return
			}
			w.backups, w.backupSize = groups, size
		})
	})
}

func (s *Shell) wuwaBackupAction(w *wuwaFixer, operation string, action func(context.Context, string) error,
	success string,
) {
	r := &s.mods.fix
	if w.busy {
		return
	}
	w.busy = true
	path := r.modPath
	s.background(func(ctx context.Context) {
		err := action(ctx, path)
		fairy.Go(func() {
			w.busy, w.pending, w.cleanConfirm, w.cleanInput = false, "", false, ""
			if err != nil {
				s.fixerFailed(err, operation, path)
				return
			}
			if success != "" {
				s.toasts.Show(kit.ToastSuccess, T(success), "")
			}
			s.loadWuwaBackups(w)
		})
	})
}

func (s *Shell) rollbackWuwa(w *wuwaFixer, key string) {
	if key == restoreAllKey && len(w.backups) > 0 {
		key = w.backups[len(w.backups)-1].GroupKey
	}
	s.wuwaBackupAction(w, "wuwa-fixer-rollback", func(ctx context.Context, path string) error {
		return s.deps.Tools.WuwaFixerRollbackToGroup(ctx, path, key)
	}, wuwaPrefix+"rollback.success")
}

func (s *Shell) wuwaDialogs(cx *fairy.Cx) {
	r := &s.mods.fix
	w := r.wuwa
	if w == nil {
		return
	}
	close := func() {
		if !r.preparing && !w.busy {
			r.wuwa = nil
		}
	}
	unknown := T("g.unknown")
	switch w.dialog {
	case "install":
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(wuwaPrefix + "install.title"),
			Alert:       true,
			Description: T(wuwaPrefix + "install.description"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(r.preparing)),
				kit.Button(T(wuwaPrefix+"install.action"), func() { s.installWuwaFixer(w) }, kit.Loading(r.preparing)),
			},
			OnClose: close,
			Key:     "mod.wuwa-install",
		})
	case "update":
		lines := []string{
			T(wuwaPrefix + "update.description"),
			T(wuwaPrefix+"update.installed") + ": " + derefOr(w.prepare.InstalledVersion, unknown),
			T(wuwaPrefix+"update.latest") + ": " + derefOr(w.prepare.LatestVersion, unknown),
		}
		if reset := rateResetText(w.prepare.RateState); w.prepare.RateLimited && reset != "" {
			lines = append(lines, T(wuwaPrefix+"update.rate_limited_until", "time", reset))
		}
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(wuwaPrefix + "update.title"),
			Alert:       true,
			Description: strings.Join(lines, "\n"),
			Width:       480,
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(r.preparing)),
				kit.Button(T(wuwaPrefix+"update.continue_without_update"), w.openOptions, kit.Outline,
					kit.Disabled(r.preparing)),
				kit.Button(T(wuwaPrefix+"update.action"), func() { s.installWuwaFixer(w) }, kit.Loading(r.preparing)),
			},
			OnClose: close,
			Key:     "mod.wuwa-update",
		})
	case "options":
		s.wuwaOptionsDialog(cx, w, close)
	}
}

func derefOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

// rateResetText is when the GitHub rate limit lifts, or "" when it is not known.
func rateResetText(state *tools.GitHubRateState) string {
	if state == nil {
		return ""
	}
	reset := state.RetryAt
	if state.Remaining <= 0 {
		reset = max(reset, state.Reset)
	}
	if reset <= 0 {
		return ""
	}
	return formatDateTime(time.Unix(reset, 0), true)
}

func (s *Shell) wuwaOptionsDialog(cx *fairy.Cx, w *wuwaFixer, close func()) {
	r := &s.mods.fix
	t := kit.T()
	unknown := T("g.unknown")
	versions := fairy.Row(
		kit.Badge(T(wuwaPrefix+"options.tool_version", "version", derefOr(w.prepare.InstalledVersion, unknown)),
			kit.Secondary),
		fairy.Text(T(wuwaPrefix+"options.config_version", "version", derefOr(w.prepare.ConfigVersion, unknown))).
			FontSize(12).Color(t.MutedForeground),
	).Gap(8).Align(fairy.Center).Wrap()
	tabs := kit.Tabs(w.tab, []kit.Item{
		{Value: "fix", Label: T(wuwaPrefix + "tabs.fix")},
		{Value: "rollback", Label: T(wuwaPrefix + "tabs.rollback")},
	}, func(v string) {
		w.tab = v
		// The backups are read again each time the tab opens, as they may have changed meanwhile.
		if v == "rollback" {
			s.loadWuwaBackups(w)
		}
	})
	body := fairy.Col(versions, tabs).Gap(12)
	if w.tab == "rollback" {
		body.Child(s.wuwaRollback(w))
	} else {
		body.Child(s.wuwaOptions(w))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(wuwaPrefix + "options.title"),
		Body:  body,
		Width: 576,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline),
			kit.Button(T(wuwaPrefix+"options.run"), func() { s.runWuwaFixer(w) },
				kit.Disabled(r.running || w.busy)),
		},
		OnClose: close,
		Key:     "mod.wuwa-options",
	})
}

// optionCard toggles its option when clicked anywhere but on the controls below its title, as the React card did.
func optionCard(label, description string, on, disabled bool, toggle func(), extra ...*fairy.El) *fairy.El {
	t := kit.T()
	text := fairy.Col(fairy.Text(label).FontSize(14).SemiBold()).Gap(2).Grow(1).MinW(0)
	if description != "" {
		text.Child(fairy.Text(description).FontSize(12).Color(t.MutedForeground).Wrap())
	}
	card := fairy.Col(fairy.Row(text, kit.Switch(on, func(bool) { toggle() }, kit.Disabled(disabled)).Label(label)).
		Gap(16).Align(fairy.Start)).Gap(10).Pad(16).Radius(8).Fill(t.Muted.Alpha(0.5)).HoverFill(t.Muted).
		Animate(motion.Fast).Cursor(fairy.CursorPointer).Focusable().Role(fairy.RoleCheckBox).Checked(on).
		Label(label).OnClick(toggle)
	if len(extra) > 0 {
		card.Child(fairy.Col(extra...).Gap(10).Cursor(fairy.CursorDefault).OnClick(func() {}))
	}
	if disabled {
		card.Opacity(0.5).Disabled(true)
	}
	return card
}

func (s *Shell) wuwaOptions(w *wuwaFixer) *fairy.El {
	t := kit.T()
	o := &w.options
	const p = wuwaPrefix + "options."
	rendering := []*fairy.El{}
	if o.Rendering33 {
		rendering = append(rendering,
			fairy.Text(T(p+"rendering_33.warning")).FontSize(12).Medium().Color(t.Warning).Wrap(),
			fairy.Row(
				fairy.Icon(lucide.TriangleAlert).Size(14, 14).Color(t.Warning).Shrink(0),
				fairy.Text(T(p+"rendering_33.description")).
					FontSize(11).
					Color(t.MutedForeground).
					Wrap().
					Grow(1).
					MinW(0),
			).Gap(6))
	}
	aero := o.AeroFix != "none" && o.AeroFix != ""
	aeroExtra := []*fairy.El{}
	if aero {
		aeroExtra = append(aeroExtra,
			fairy.Text(T(p+"aero_fix.warning")).FontSize(12).Medium().Color(t.Destructive).Wrap(),
			kit.Segmented(o.AeroFix, []kit.Item{
				{Value: "1", Label: T(p + "aero_fix.options.texcoord_override")},
				{Value: "2", Label: T(p + "aero_fix.options.texture_mirror_flip")},
			}, func(v string) { o.AeroFix = v }),
			fairy.Row(fairy.Icon(lucide.Info).Size(14, 14).Color(t.MutedForeground).Shrink(0),
				fairy.Text(T(p+"aero_fix.tip")).FontSize(11).Color(t.MutedForeground).Wrap().Grow(1).MinW(0)).Gap(6))
	}
	return fairy.Col(
		optionCard(T(p+"derived_hashes.label"), T(p+"derived_hashes.description"), o.DerivedHashes, o.StableTexture,
			func() {
				o.DerivedHashes = !o.DerivedHashes
				if o.DerivedHashes {
					o.StableTexture = false
				}
			}),
		optionCard(T(p+"stable_texture.label"), T(p+"stable_texture.description"), o.StableTexture, o.DerivedHashes,
			func() {
				o.StableTexture = !o.StableTexture
				if o.StableTexture {
					o.DerivedHashes = false
				}
			}),
		optionCard(T(p+"rendering_33.label"), "", o.Rendering33, false, func() { o.Rendering33 = !o.Rendering33 },
			rendering...),
		optionCard(T(p+"aemeath_mech.label"), T(p+"aemeath_mech.description"), o.AemeathMech, false,
			func() { o.AemeathMech = !o.AemeathMech }),
		optionCard(T(p+"aero_fix.label"), "", aero, false, func() {
			if aero {
				o.AeroFix = "none"
			} else {
				o.AeroFix = "1"
			}
		}, aeroExtra...),
	).Gap(12).MaxH(440).ScrollY()
}

func (s *Shell) wuwaRollback(w *wuwaFixer) *fairy.El {
	t := kit.T()
	const p = wuwaPrefix + "rollback."
	head := fairy.Row(
		kit.Button(T(p+"refresh"), func() { s.loadWuwaBackups(w) }, kit.Outline, kit.Small, kit.Icon(lucide.RefreshCw),
			kit.Loading(w.backupsBusy), kit.Disabled(w.busy)),
		fairy.Box().Grow(1),
	).Gap(8).Align(fairy.Center)
	if w.backupSize.Count > 0 {
		head.Child(fairy.Text(T(p+"backup_size_label") + " " + formatBackupSize(w.backupSize.Bytes)).FontSize(12).
			Color(t.MutedForeground))
	}

	list := fairy.Col().Gap(12)
	switch {
	case w.backupsBusy && !w.backupsLoaded:
		list.Child(fairy.Text(T(p+"scanning")).FontSize(14).Color(t.MutedForeground).PadXY(0, 40).Center())
	case len(w.backups) == 0:
		list.Child(fairy.Col(
			fairy.Icon(lucide.Info).Size(40, 40).Color(t.MutedForeground.Alpha(0.3)),
			fairy.Text(T(p+"no_backups")).FontSize(12).Color(t.MutedForeground),
		).Gap(8).Align(fairy.Center).PadXY(0, 48))
	default:
		for index, group := range w.backups {
			list.Child(s.wuwaBackupCard(w, index, group))
		}
	}

	advancedLabel := fairy.Row(
		fairy.Icon(lucide.ChevronRight).Size(12, 12).Rotate(pickAngle(w.advanced)).ScaleMotion(motion.Fast),
		fairy.Text(T(p+"advanced")).SemiBold(),
		fairy.Box().Grow(1),
		fairy.Text(T(p+"advanced_hint")).FontSize(10).Opacity(0.6),
	).Gap(8).Align(fairy.Center).PadXY(4, 8).FontSize(12).Color(t.MutedForeground).
		HoverFill(t.Muted.Alpha(0.5)).HoverColor(t.Foreground).Animate(motion.Fast).Cursor(fairy.CursorPointer).
		Role(fairy.RoleButton).
		Label(T(p + "advanced")).Expanded(w.advanced).OnClick(func() { w.advanced = !w.advanced })
	col := fairy.Col(head, fairy.Col(list).MaxH(360).ScrollY(), kit.Separator(), advancedLabel).Gap(12)
	if w.advanced {
		col.Child(s.wuwaAdvanced(w))
	}
	return col
}

func pickAngle(open bool) float32 {
	if open {
		return math.Pi / 2
	}
	return 0
}

func (s *Shell) wuwaBackupCard(w *wuwaFixer, index int, group tools.WuwaBackupGroup) *fairy.El {
	t := kit.T()
	const p = wuwaPrefix + "rollback."
	pending := w.pending == group.GroupKey
	badges := fairy.Row(
		kit.Badge(formatGroupKey(group.GroupKey), kit.Outline),
		kit.Badge(T(p+"file_count", "n", len(group.Files)), kit.Secondary),
	).Gap(6).Wrap().Grow(1).MinW(0)
	if index == 0 {
		badges.Child(kit.Badge(T(p+"latest_state"), kit.WithTone(kit.ToneSuccess)))
	}
	actions := fairy.Row().Gap(8).Shrink(0)
	if pending {
		actions.Child(kit.Button(T(p+"confirm_restore"), func() { s.rollbackWuwa(w, group.GroupKey) }, kit.Destructive,
			kit.Small, kit.Disabled(w.busy)))
		actions.Child(
			kit.Button(T("g.cancel"), func() { w.pending = "" }, kit.Outline, kit.Small, kit.Disabled(w.busy)),
		)
	} else {
		key := group.GroupKey
		actions.Child(kit.Button(T(p+"restore"), func() { w.pending = key }, kit.Outline, kit.Small,
			kit.Disabled(w.busy)))
	}

	files := fairy.Col().Gap(10).Font(monoFont).FontSize(11)
	for _, dir := range groupFilesByDir(group.Files, s.mods.fix.modPath) {
		files.Child(fairy.Col(
			fairy.Row(fairy.Icon(lucide.Folder).Size(12, 12).Shrink(0), fairy.Text(dir.name+"/").SemiBold().Wrap()).
				Gap(4).
				Color(t.Accent),
			fairy.Text(strings.Join(dir.files, ", ")).Color(t.MutedForeground).Wrap().PadEdges(0, 0, 0, 16),
		).Gap(2))
	}
	card := fairy.Col(
		fairy.Row(badges, actions).Gap(12).Align(fairy.Center),
		kit.Separator(),
		fairy.Col(files).MaxH(150).ScrollY().Pad(12).Radius(8).Fill(t.Background.Alpha(0.6)),
	).Gap(12).Pad(16).Radius(8).Fill(t.Muted.Alpha(0.5))
	if pending {
		card.Border(1, t.Warning.Alpha(0.6))
		warning := fairy.Col(fairy.Text(T(p + "warn_restore")).Bold()).Gap(2).Grow(1).MinW(0)
		if index > 0 {
			warning.Child(fairy.Text(T(p+"warn_clean_newer", "n", index)).FontSize(10))
		}
		card.Child(fairy.Row(fairy.Icon(lucide.TriangleAlert).Size(16, 16).Shrink(0), warning).Gap(8).Pad(10).
			Radius(8).Border(1, t.Warning.Alpha(0.2)).Fill(t.Warning.Alpha(0.12)).Color(t.Warning).FontSize(11))
	}
	return card
}

func (s *Shell) wuwaAdvanced(w *wuwaFixer) *fairy.El {
	t := kit.T()
	const p = wuwaPrefix + "rollback."
	row := func(title, description string, danger bool, control *fairy.El) *fairy.El {
		e := fairy.Row(fairy.Col(
			fairy.Text(title).FontSize(12).Bold(),
			fairy.Text(description).FontSize(10).Color(t.MutedForeground).Wrap(),
		).Gap(2).Grow(1).MinW(0), control.Shrink(0)).Gap(12).Pad(12).Radius(8).Align(fairy.Start)
		if danger {
			return e.Border(1, t.Destructive.Alpha(0.3)).Fill(t.Destructive.Alpha(0.05))
		}
		return e.Fill(t.Background)
	}
	none := fairy.Text(T(p + "no_backups_short")).FontSize(10).Color(t.MutedForeground).AlignSelf(fairy.Center)

	var restore *fairy.El
	switch {
	case len(w.backups) == 0:
		restore = none
	case w.pending == restoreAllKey:
		restore = fairy.Col(
			fairy.Text(T(p+"warn_delete_all", "n", len(w.backups))).FontSize(10).Bold().Color(t.Destructive),
			fairy.Row(
				kit.Button(T("g.confirm"), func() { s.rollbackWuwa(w, restoreAllKey) }, kit.Destructive, kit.Small,
					kit.Disabled(w.busy)),
				kit.Button(T("g.cancel"), func() { w.pending = "" }, kit.Outline, kit.Small, kit.Disabled(w.busy)),
			).Gap(6),
		).Gap(6).Align(fairy.End)
	default:
		restore = kit.Button(T(p+"restore_all_action"), func() { w.pending = restoreAllKey }, kit.Outline, kit.Small,
			kit.Disabled(w.busy))
	}

	var clean *fairy.El
	switch {
	case w.backupSize.Count == 0:
		clean = fairy.Text(T(p + "no_backups_short")).FontSize(10).Color(t.MutedForeground).AlignSelf(fairy.Center)
	case w.cleanConfirm:
		cleanup := func() {
			if strings.ToUpper(w.cleanInput) != "WIPE" {
				return
			}
			s.wuwaBackupAction(w, "wuwa-fixer-clean-backups", s.deps.Tools.WuwaFixerCleanBackups, "")
		}
		clean = fairy.Col(
			fairy.Text(T(p+"warn_clean")).FontSize(10).Bold().Color(t.Destructive),
			fairy.Row(
				kit.TextField(w.cleanInput, func(v string) { w.cleanInput = v }, kit.Placeholder("WIPE"), kit.Small,
					kit.Disabled(w.busy)).W(64).Grow(0).Font(monoFont),
				kit.Button(T("g.confirm"), cleanup, kit.Destructive, kit.Small,
					kit.Disabled(w.busy || strings.ToUpper(w.cleanInput) != "WIPE")),
				kit.Button(T("g.cancel"), func() { w.cleanConfirm, w.cleanInput = false, "" }, kit.Outline, kit.Small,
					kit.Disabled(w.busy)),
			).Gap(6).Align(fairy.Center),
		).Gap(6).Align(fairy.End)
	default:
		clean = kit.Button(T(p+"clean_action"), func() { w.cleanConfirm = true }, kit.Outline, kit.Small,
			kit.Disabled(w.busy))
	}
	return fairy.Col(
		row(T(p+"restore_all"), T(p+"restore_all_desc"), false, restore),
		row(T(p+"clean_backups"), T(p+"clean_backups_desc"), true, clean),
	).Gap(12).Pad(12).Radius(8).Fill(t.Muted.Alpha(0.3))
}

// formatGroupKey shows the hh-mm time at the end of a backup group key as hh:mm.
func formatGroupKey(key string) string {
	if len(key) >= 6 && key[len(key)-6] == ' ' && key[len(key)-3] == '-' {
		return key[:len(key)-3] + ":" + key[len(key)-2:]
	}
	return key
}

func formatBackupSize(bytes int64) string {
	if bytes > 1048576 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/1048576)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}

type backupDir struct {
	name  string
	files []string
}

// groupFilesByDir lists the backed-up files by their folder relative to base, in order of first appearance.
func groupFilesByDir(files []tools.WuwaBackupFile, base string) []backupDir {
	var dirs []backupDir
	for _, file := range files {
		relative := file.OriginalPath
		if platform.SameOrChildPath(base, relative) {
			relative = strings.TrimLeft(relative[len(strings.TrimRight(base, `\/`)):], `\/`)
		}
		dir := filepath.ToSlash(filepath.Dir(relative))
		i := slices.IndexFunc(dirs, func(d backupDir) bool { return d.name == dir })
		if i < 0 {
			dirs = append(dirs, backupDir{name: dir})
			i = len(dirs) - 1
		}
		dirs[i].files = append(dirs[i].files, filepath.Base(relative))
	}
	return dirs
}

func (s *Shell) openZZMIFixer(path string) {
	r := &s.mods.fix
	tab := cmp.Or(r.zzmiTab, "hash")
	r.zzmiTab = ""
	if r.preparing {
		return
	}
	r.modPath, r.preparing = path, true
	s.background(func(ctx context.Context) {
		result, err := s.deps.Tools.ZZMIFixerPrepare(ctx, path, false)
		fairy.Go(func() {
			r.preparing = false
			if err != nil {
				s.fixerFailed(err, "zzmi-fixer-prepare", path)
				return
			}
			r.zzmi = &zzmiFixer{prepare: result, tab: tab}
		})
	})
}

func (s *Shell) refreshZZMIRules(z *zzmiFixer, activate bool) {
	r := &s.mods.fix
	if z.updateBusy {
		return
	}
	z.updateBusy = true
	path := r.modPath
	s.background(func(ctx context.Context) {
		var err error
		if activate {
			_, err = s.deps.Tools.ZZMIFixerActivateLatestRules(ctx)
		}
		var result tools.ZZMIFixerPrepareResult
		if err == nil {
			result, err = s.deps.Tools.ZZMIFixerPrepare(ctx, path, !activate)
		}
		fairy.Go(func() {
			z.updateBusy = false
			if err != nil {
				s.fixerFailed(err, "zzmi-fixer-rules", path)
				return
			}
			z.prepare = result
			if activate {
				s.toasts.Show(kit.ToastSuccess, T(zzmiPrefix+"update.success"), "")
			}
		})
	})
}

func (s *Shell) runZZMIFixer(z *zzmiFixer, tool string) {
	r := &s.mods.fix
	if r.running || z.updateBusy || r.modPath == "" {
		return
	}
	path := r.modPath
	r.zzmi = nil
	s.startFixLog("zzmi", path)
	s.background(func(ctx context.Context) {
		result, err := s.deps.Tools.ZZMIFixerRun(ctx, tools.ZZMIFixerRunInput{Path: path, Tool: tool})
		fairy.Go(func() {
			r.running = false
			if err != nil {
				s.fixerFailed(err, "zzmi-fixer-run", path)
				return
			}
			if n := len(result.Warnings); n > 0 {
				s.toasts.Show(kit.ToastWarning, T(zzmiPrefix+"run.warning", "count", n), "")
			}
			s.refreshListing()
		})
	})
}

func (s *Shell) loadZZMIBackups(z *zzmiFixer) {
	r := &s.mods.fix
	if z.backupsBusy {
		return
	}
	z.backupsBusy = true
	path := r.modPath
	s.background(func(ctx context.Context) {
		sessions, err := s.deps.Tools.ZZMIFixerListBackups(ctx, path)
		fairy.Go(func() {
			z.backupsBusy, z.backupsLoaded = false, true
			if err != nil {
				s.fixerFailed(err, "zzmi-fixer-list-backups", path)
				return
			}
			z.backups = sessions
		})
	})
}

func (s *Shell) zzmiBackupAction(z *zzmiFixer, operation string, action func(context.Context, string) error) {
	r := &s.mods.fix
	if z.busy {
		return
	}
	z.busy = true
	path := r.modPath
	s.background(func(ctx context.Context) {
		err := action(ctx, path)
		fairy.Go(func() {
			z.busy = false
			if err != nil {
				s.fixerFailed(err, operation, path)
				return
			}
			s.loadZZMIBackups(z)
		})
	})
}

// restoreZZMI restores a backup session or one entry of it. Files changed after the fixer ran are listed
// first, and restored over only with force.
func (s *Shell) restoreZZMI(z *zzmiFixer, target zzmiTarget, force bool) {
	r := &s.mods.fix
	if z.busy {
		return
	}
	z.busy = true
	path := r.modPath
	s.background(func(ctx context.Context) {
		result, err := s.deps.Tools.ZZMIFixerRestore(ctx, tools.ZZMIFixerRestoreInput{
			Path: path, SessionID: target.session, EntryID: target.entry, Force: force,
		})
		fairy.Go(func() {
			z.busy = false
			if err != nil {
				s.fixerFailed(err, "zzmi-fixer-restore", path)
				return
			}
			if len(result.Conflicts) > 0 {
				z.conflicts, z.restore = result.Conflicts, &target
			} else {
				z.conflicts, z.restore = nil, nil
				s.toasts.Show(kit.ToastSuccess, T(zzmiPrefix+"rollback.success"), "")
			}
			s.loadZZMIBackups(z)
			s.refreshListing()
		})
	})
}

func (s *Shell) zzmiDialogs(cx *fairy.Cx) {
	r := &s.mods.fix
	z := r.zzmi
	if z == nil {
		return
	}
	t := kit.T()
	rules := z.prepare.Rules
	source := T(zzmiPrefix + "rules.source_" + rules.ActiveSource)
	if rules.ActiveSource == "" {
		source = T(zzmiPrefix + "rules.source_embedded")
	}
	commit := "--------"
	if len(rules.ActiveCommit) >= 8 {
		commit = rules.ActiveCommit[:8]
	} else if rules.ActiveCommit != "" {
		commit = rules.ActiveCommit
	}
	tag := rules.ActiveTag
	if tag == "" {
		tag = T("g.unknown")
	}
	body := fairy.Col(fairy.Row(
		kit.Badge(tag, kit.Secondary),
		fairy.Text(commit).Font(monoFont).Color(t.MutedForeground),
		fairy.Text(T(zzmiPrefix+"rules.source", "source", source)).Color(t.MutedForeground).Grow(1).MinW(0),
		kit.Button(T(zzmiPrefix+"rules.refresh"), func() { s.refreshZZMIRules(z, false) }, kit.Outline, kit.Small,
			kit.Icon(lucide.RefreshCw), kit.Loading(z.updateBusy)),
	).Gap(8).Align(fairy.Center).Wrap().PadXY(12, 8).Radius(8).Fill(t.Muted.Alpha(0.5)).FontSize(12)).Gap(12)
	if rules.UpdateAvailable {
		body.Child(fairy.Row(
			fairy.Col(
				fairy.Text(T(zzmiPrefix+"update.available")).SemiBold(),
				fairy.Text(rules.ActiveTag+" → "+derefOr(rules.LatestTag, T("g.unknown"))).
					FontSize(12).
					Color(t.MutedForeground),
			).Gap(2).Grow(1).MinW(0),
			kit.Button(T(zzmiPrefix+"update.action"), func() { s.refreshZZMIRules(z, true) }, kit.Small,
				kit.Loading(z.updateBusy)),
		).Gap(12).Align(fairy.Center).Pad(12).Radius(8).Border(1, t.Accent.Alpha(0.4)).Fill(t.Accent.Alpha(0.1)))
	}
	if reason := deref(rules.IncompatibilityReason); reason != "" {
		body.Child(fairy.Row(fairy.Icon(lucide.TriangleAlert).Size(16, 16).Shrink(0),
			fairy.Text(reason).Wrap().Grow(1).MinW(0)).Gap(8).Pad(12).Radius(8).Border(1, t.Warning.Alpha(0.4)).
			Fill(t.Warning.Alpha(0.12)).Color(t.Warning).FontSize(12))
	}
	body.Child(kit.Tabs(z.tab, []kit.Item{
		{Value: "hash", Label: T(zzmiPrefix + "tabs.hash")},
		{Value: "jane", Label: T(zzmiPrefix + "tabs.jane")},
		{Value: "dialyn", Label: T(zzmiPrefix + "tabs.dialyn")},
		{Value: "rollback", Label: T(zzmiPrefix + "tabs.rollback")},
	}, func(v string) {
		z.tab = v
		if v == "rollback" {
			s.loadZZMIBackups(z)
		}
	}))
	if z.tab == "rollback" {
		body.Child(s.zzmiRollback(z))
	} else {
		tool := z.tab
		body.Child(fairy.Col(
			fairy.Row(
				fairy.Box(fairy.Icon(lucide.Wrench).Size(20, 20)).
					Pad(8).
					Radius(8).
					Fill(t.Accent.Alpha(0.1)).
					Color(t.Accent),
				fairy.Col(
					fairy.Text(T(zzmiPrefix+tool+".title")).SemiBold(),
					fairy.Text(T(zzmiPrefix+tool+".description")).FontSize(14).Color(t.MutedForeground).Wrap(),
				).Gap(4).Grow(1).MinW(0),
			).Gap(12).Align(fairy.Start),
			fairy.Row(kit.Button(T(zzmiPrefix+"run.action"), func() { s.runZZMIFixer(z, tool) },
				kit.Disabled(z.updateBusy))).Justify(fairy.JustifyEnd),
		).Gap(16))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       "ZZMI Mod Fixer",
		TitleLead:   fairy.ImageEl(modFixerImage("zzmi", 28)).Fit(fairy.FitContain).Size(28, 28).Radius(4),
		Description: r.modPath,
		Body:        body,
		Width:       720,
		OnClose:     func() { r.zzmi = nil },
		Key:         "mod.zzmi-fixer",
	})
	s.zzmiConfirmDialogs(cx, z)
}

func (s *Shell) zzmiRollback(z *zzmiFixer) *fairy.El {
	t := kit.T()
	const p = zzmiPrefix + "rollback."
	var total int64
	for _, session := range z.backups {
		total += session.Size
	}
	head := fairy.Row(
		fairy.Text(T(p+"summary", "count", len(z.backups), "size", formatSize(total))).FontSize(12).
			Color(t.MutedForeground).Grow(1).MinW(0),
		kit.Button(T(zzmiPrefix+"rules.refresh"), func() { s.loadZZMIBackups(z) }, kit.Outline, kit.Small,
			kit.Icon(lucide.RefreshCw), kit.Disabled(z.busy)),
		kit.Button(
			T(p+"delete_all"),
			func() { z.removeAll = true },
			kit.Destructive,
			kit.Small,
			kit.Icon(lucide.Trash),
			kit.Disabled(len(z.backups) == 0 || z.busy),
		),
	).Gap(8).Align(fairy.Center)

	list := fairy.Col().Gap(8)
	switch {
	case z.backupsBusy && !z.backupsLoaded:
		list.Child(fairy.Box(kit.Spinner(20)).PadXY(0, 32).Center())
	case len(z.backups) == 0:
		list.Child(fairy.Text(T(p+"empty")).FontSize(14).Color(t.MutedForeground).PadXY(0, 32).Center())
	}
	for _, session := range z.backups {
		id := session.ID
		variant := kit.Secondary
		if session.Status != "completed" {
			variant = kit.Destructive
		}
		status := T(p + "status." + session.Status)
		entries := fairy.Col().Gap(4).Pad(8).Radius(6).Fill(t.Muted.Alpha(0.4))
		for _, entry := range session.Entries {
			entryID := entry.ID
			entries.Child(fairy.Row(
				fairy.Text(entry.RelativePath).Font(monoFont).Ellipsis().Grow(1).MinW(0),
				kit.Badge(entry.Kind, kit.Outline),
				kit.IconButton(lucide.RotateCcw, func() { s.restoreZZMI(z, zzmiTarget{session: id, entry: &entryID}, false) },
					kit.Ghost, kit.Small, kit.Disabled(z.busy), kit.Tooltip(T(p+"restore_entry"))).
					Label(T(p+"restore_entry")),
				kit.IconButton(lucide.Trash, func() { z.remove = &zzmiTarget{session: id, entry: &entryID} }, kit.Ghost,
					kit.Small, kit.Disabled(z.busy), kit.Tooltip(T("g.delete"))).
					Label(T("g.delete")),
			).Gap(8).Align(fairy.Center).FontSize(12))
		}
		list.Child(fairy.Col(
			fairy.Row(
				fairy.Icon(lucide.Clock).Size(16, 16).Color(t.Accent).Shrink(0),
				fairy.Col(
					fairy.Row(fairy.Text(T(zzmiPrefix+"tool."+session.Tool)).FontSize(14).SemiBold(),
						kit.Badge(status, variant)).Gap(8).Align(fairy.Center).Wrap(),
					fairy.Text(formatSessionTime(session.CreatedAt)+" · "+session.RuleTag+" · "+formatSize(session.Size)).
						FontSize(11).
						Color(t.MutedForeground),
				).Gap(2).Grow(1).MinW(0),
				kit.Button(T(p+"restore_session"), func() { s.restoreZZMI(z, zzmiTarget{session: id}, false) },
					kit.Outline, kit.Small, kit.Icon(lucide.RotateCcw), kit.Disabled(z.busy)),
				kit.IconButton(lucide.Trash, func() { z.remove = &zzmiTarget{session: id} }, kit.Ghost, kit.Small,
					kit.Disabled(z.busy), kit.Tooltip(T("g.delete"))).Label(T("g.delete")),
			).Gap(8).Align(fairy.Start),
			entries,
		).Gap(12).Pad(12).Radius(8).Fill(t.Muted.Alpha(0.5)))
	}
	return fairy.Col(head, fairy.Col(list).H(380).ScrollY()).Gap(12)
}

func formatSessionTime(value string) string {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return formatDateTime(parsed.Local(), true)
	}
	return value
}

func (s *Shell) zzmiConfirmDialogs(cx *fairy.Cx, z *zzmiFixer) {
	t := kit.T()
	const p = zzmiPrefix + "rollback."
	if target := z.restore; target != nil {
		dismiss := func() { z.restore, z.conflicts = nil, nil }
		files := fairy.Col().Gap(2).Font(monoFont).FontSize(12)
		for _, conflict := range z.conflicts {
			files.Child(fairy.Text(conflict.OriginalPath).Wrap())
		}
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(p + "conflict_title"),
			Alert:       true,
			Description: T(p+"conflict_description", "count", len(z.conflicts)),
			Body:        fairy.Col(files).MaxH(192).ScrollY().Pad(8).Radius(4).Border(1, t.Border),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline),
				kit.Button(T(p+"force"), func() { s.restoreZZMI(z, *target, true) }, kit.Loading(z.busy)),
			},
			OnClose: dismiss,
			Key:     "mod.zzmi-conflicts",
		})
	}
	if target := z.remove; target != nil {
		dismiss := func() { z.remove = nil }
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(p + "delete_title"),
			Alert:       true,
			Description: T(p + "delete_description"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline),
				kit.Button(T("g.delete"), func() {
					z.remove = nil
					s.zzmiBackupAction(z, "zzmi-fixer-delete-backup", func(ctx context.Context, path string) error {
						return s.deps.Tools.ZZMIFixerDeleteBackup(ctx, tools.ZZMIFixerDeleteBackupInput{
							Path: path, SessionID: target.session, EntryID: target.entry,
						})
					})
				}, kit.Destructive),
			},
			OnClose: dismiss,
			Key:     "mod.zzmi-delete",
		})
	}
	if z.removeAll {
		dismiss := func() { z.removeAll = false }
		kit.ShowDialog(cx, kit.DialogProps{
			Title:       T(p + "delete_all_title"),
			Alert:       true,
			Description: T(p + "delete_all_description"),
			Actions: []*fairy.El{
				kit.Button(T("g.cancel"), dismiss, kit.Outline),
				kit.Button(T("g.delete"), func() {
					z.removeAll = false
					s.zzmiBackupAction(z, "zzmi-fixer-delete-all-backups", s.deps.Tools.ZZMIFixerDeleteAllBackups)
				}, kit.Destructive),
			},
			OnClose: dismiss,
			Key:     "mod.zzmi-delete-all",
		})
	}
}

//go:embed assets/fixer
var fixerAssets embed.FS

// modFixerImage is the logo of the wuwa or zzmi fixer that its menu entries and dialog show.
func modFixerImage(id string, size float32) *fairy.Image {
	file := "assets/fixer/" + id + ".png"
	return fairy.Images.Sized("fixer:"+id, size, size, fairy.FitContain, func() ([]byte, error) {
		return fixerAssets.ReadFile(file)
	})
}
