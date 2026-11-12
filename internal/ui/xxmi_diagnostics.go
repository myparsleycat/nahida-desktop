package ui

import (
	"context"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/feedback"
	"nahida.live/desktop/internal/xxmi"
)

var diagnosticsErrorPattern = regexp.MustCompile(`(XXMI_[A-Z_]+)(?::([^\n]*))?`)

// diagnosticsPage holds the dialogs of the importer's logging session.
type diagnosticsPage struct {
	start  *diagnosticsStart
	report *diagnosticsReport
}

type diagnosticsStart struct {
	// editing is whether the dialog changes the options of an armed session instead of starting one.
	editing    bool
	level      xxmi.DiagnosticsLevel
	debugLocks bool
	frameTimes bool
	hashDumps  bool
	busy       bool
}

type diagnosticsReport struct {
	startedAt  string
	body       string
	includeLog bool
	// mods are the picked folders in the order they were picked.
	mods     []xxmi.DiagnosticsMod
	expanded map[string]bool
	lists    map[string]*query[[]xxmi.DiagnosticsMod]
	// bundle belongs to one session and one size bound, so it is read again when either changes.
	bundle    query[xxmi.DiagnosticsBundle]
	bundleFor string
	progress  *float64
	cancelled bool
}

// diagnosticsMessage explains an XXMI_ error code, or returns "" to show the error text.
func diagnosticsMessage(err error) string {
	match := diagnosticsErrorPattern.FindStringSubmatch(err.Error())
	if match == nil {
		return ""
	}
	key := "page.setting.xxmi.diagnostics.errors." + match[1]
	if !catalog.Has(key) {
		return ""
	}
	return T(key, "detail", match[2])
}

func (s *Shell) diagnosticsSession(key string) *query[*xxmi.DiagnosticsSession] {
	return fetch(s, keyed(s.xxmi.diagnostics, key), func(ctx context.Context) (*xxmi.DiagnosticsSession, error) {
		return s.deps.XXMI.GetDiagnostics(ctx, key)
	})
}

// diagnosticsNotice points at a running or collected logging session from the importer's header.
func (s *Shell) diagnosticsNotice(key string, open func()) *fairy.El {
	t := kit.T()
	const prefix = "page.setting.xxmi.diagnostics."
	session := s.diagnosticsSession(key).value
	if session == nil {
		return nil
	}
	title := prefix + "noticeCollected"
	if session.State == xxmi.DiagnosticsArmed {
		title = prefix + "noticeArmed"
	}
	return fairy.Row(
		fairy.Icon(lucide.Bug).Size(16, 16).Shrink(0).MarginEdges(2, 0, 0, 0),
		fairy.Col(fairy.Text(T(title)).FontSize(14).LineHeight(20).Medium(),
			kit.Button(T(prefix+"noticeOpen"), open, kit.Link, kit.Small).H(20).Pad(0).AlignSelf(fairy.Start),
		).Gap(2).Grow(1).MinW(0),
	).Gap(8).PadXY(10, 8).Align(fairy.Start).Radius(8).Border(1, t.Border).Fill(t.Card).Color(t.CardForeground)
}

func (s *Shell) diagnosticsSection(key string, running, launchDisabled bool) *fairy.El {
	t := kit.T()
	p := &s.xxmi.importer
	const prefix = "page.setting.xxmi.diagnostics."
	support := fetch(s, keyed(s.xxmi.diagSupport, key), func(ctx context.Context) (xxmi.DiagnosticsSupport, error) {
		return s.deps.XXMI.DiagnosticsSupport(ctx, key)
	})
	session := s.diagnosticsSession(key).value
	supported := support.value.Supported
	armed := session != nil && session.State == xxmi.DiagnosticsArmed
	collected := session != nil && session.State == xxmi.DiagnosticsCollected

	content := fairy.Col().Gap(12).PadXY(0, 4)
	if session == nil && support.loaded && !supported {
		reason := support.value.Reason
		message := prefix + "unsupported." + reason
		if reason == "" || !catalog.Has(message) {
			message = prefix + "unsupported.provider"
		}
		content.Child(xxmiAlert(lucide.TriangleAlert, false, "", T(message)))
	}
	if armed {
		// A launch is recorded before the game starts, so a game running without one was started before the
		// session and read d3dx.ini without its options.
		state := prefix + "armedReady"
		switch {
		case running && session.LaunchedAt != "":
			state = prefix + "armedRunning"
		case running:
			state = prefix + "armedRelaunch"
		case session.LaunchedAt != "":
			state = prefix + "armedLaunched"
		}
		title := T(prefix+"armedTitle", "level", T(prefix+"level."+string(session.Level)+".label"))
		content.Child(xxmiAlert(lucide.Bug, false, title,
			T(state)+"\n"+T(prefix+"hangHint")+"\n"+T(prefix+"lastLaunchHint")))
	}
	if collected {
		content.Child(xxmiAlert(lucide.Bug, false, T(prefix+"collectedTitle"), T(prefix+"collectedDescription")))
	}

	openStart := func() {
		start := &diagnosticsStart{level: xxmi.DiagnosticsInfo, hashDumps: true}
		if armed {
			start = &diagnosticsStart{editing: true, level: session.Level, debugLocks: session.DebugLocks,
				frameTimes: session.FrameTimes, hashDumps: session.HashDumps}
		}
		p.diag.start = start
	}
	openReport := func() {
		p.diag.report = &diagnosticsReport{startedAt: session.StartedAt, includeLog: true,
			expanded: map[string]bool{}, lists: map[string]*query[[]xxmi.DiagnosticsMod]{}}
	}
	actions := fairy.Row().Wrap().Gap(8).RowGap(8)
	if session == nil && supported {
		actions.Child(kit.Button(T(prefix+"start"), openStart, kit.Small, kit.Icon(lucide.Bug)))
	}
	if armed {
		actions.Child(
			kit.Button(T(prefix+"launch"), func() { s.startImporter(key) }, kit.Outline, kit.Small,
				kit.Icon(lucide.Play), kit.Disabled(running || launchDisabled),
				kit.Loading(s.xxmi.guard.starting[key])),
			kit.Button(T(prefix+"collect"), openReport, kit.Small, kit.Disabled(running)),
			kit.Button(T(prefix+"changeOptions"), openStart, kit.Outline, kit.Small),
		)
	}
	if collected {
		actions.Child(kit.Button(T(prefix+"report"), openReport, kit.Small))
		if supported {
			actions.Child(kit.Button(T(prefix+"restart"), openStart, kit.Outline, kit.Small))
		}
	}
	if session != nil {
		label := prefix + "discard"
		if armed {
			label = prefix + "end"
		}
		actions.Child(kit.Button(T(label), func() { s.endDiagnostics(key) }, kit.Outline, kit.Small,
			kit.Loading(s.xxmi.busy["diagnostics-end:"+key])))
	}
	content.Child(actions)
	if armed && running {
		content.Child(fairy.Text(T(prefix + "collectWhenClosed")).FontSize(12).LineHeight(16).Color(t.MutedForeground))
	}

	return fairy.Col(
		fairy.Col(
			fairy.Text(T(prefix+"title")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
			fairy.Text(T(prefix+"description")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
		).Gap(2).PadEdges(0, 4, 8, 4),
		settingsGroup("", content),
	).Gap(4)
}

func (s *Shell) endDiagnostics(key string) {
	s.xxmiAction("diagnostics-end:"+key, func(ctx context.Context) error {
		err := s.deps.XXMI.EndDiagnostics(ctx, key)
		fairy.Go(keyed(s.xxmi.diagnostics, key).invalidate)
		return err
	}, func() {
		s.toasts.Show(kit.ToastSuccess, T("page.setting.xxmi.diagnostics.ended"), "")
	}, diagnosticsMessage)
}

func (s *Shell) diagnosticsDialogs(cx *fairy.Cx, key string, canLaunch bool) {
	p := &s.xxmi.importer
	if start := p.diag.start; start != nil {
		s.diagnosticsStartDialog(cx, key, start, canLaunch)
	}
	if report := p.diag.report; report != nil {
		s.diagnosticsReportDialog(cx, key, report)
	}
}

func (s *Shell) diagnosticsStartDialog(cx *fairy.Cx, key string, d *diagnosticsStart, canLaunch bool) {
	t := kit.T()
	p := &s.xxmi.importer
	const prefix = "page.setting.xxmi.diagnostics."
	hashDumpsAvailable := keyed(s.xxmi.diagSupport, key).value.HashDumps

	levels := fairy.Col().Gap(8).Role(fairy.RoleGroup)
	for _, level := range []xxmi.DiagnosticsLevel{xxmi.DiagnosticsInfo, xxmi.DiagnosticsDebug} {
		selected := d.level == level
		choose := func() {
			if !d.busy {
				d.level = level
			}
		}
		title := fairy.Row(fairy.Text(T(prefix + "level." + string(level) + ".label")).FontSize(14).LineHeight(20).
			Medium()).Gap(8).
			Align(fairy.Center)
		if level == xxmi.DiagnosticsInfo {
			title.Child(kit.Badge(T(prefix+"recommended"), kit.Secondary))
		}
		card := fairy.Col(title,
			fairy.Text(T(prefix+"level."+string(level)+".description")).FontSize(12).LineHeight(16).
				Color(t.MutedForeground).Wrap(),
		).Gap(2).PadXY(12, 10).Radius(8).Border(1, t.Border).HoverFill(t.Muted.Alpha(0.5)).
			Animate(motion.Fast).
			Role(fairy.RoleRadio).Checked(selected).Label(T(prefix + "level." + string(level) + ".label")).
			Focusable().Cursor(fairy.CursorPointer).OnClick(choose).Disabled(d.busy)
		if selected {
			card.Border(1, t.Primary).Fill(t.Muted.Alpha(0.5))
		}
		if d.busy {
			card.Opacity(0.5)
		}
		levels.Child(card)
	}
	body := fairy.Col(levels,
		toggleRow(T(prefix+"debugLocks"), T(prefix+"debugLocksDescription"), d.debugLocks,
			func(on bool) { d.debugLocks = on }, d.busy).Pad(0),
		toggleRow(T(prefix+"frameTimes"), T(prefix+"frameTimesDescription"), d.frameTimes,
			func(on bool) { d.frameTimes = on }, d.busy).Pad(0),
	).Gap(16)
	if hashDumpsAvailable {
		body.Child(toggleRow(T(prefix+"hashDumps"), T(prefix+"hashDumpsDescription"), d.hashDumps,
			func(on bool) { d.hashDumps = on }, d.busy).Pad(0))
	}
	body.Child(fairy.Text(T(prefix + "alwaysOn")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())

	launchNow := !d.editing && canLaunch
	confirm := prefix + "start"
	switch {
	case d.editing:
		confirm = prefix + "apply"
	case launchNow:
		confirm = prefix + "startAndLaunch"
	}
	start := func() {
		if d.busy {
			return
		}
		d.busy = true
		options := xxmi.DiagnosticsOptions{Level: d.level, DebugLocks: d.debugLocks, FrameTimes: d.frameTimes,
			HashDumps: hashDumpsAvailable && d.hashDumps}
		s.background(func(ctx context.Context) {
			err := s.deps.XXMI.StartDiagnostics(ctx, key, options)
			fairy.Go(func() {
				d.busy = false
				if err != nil {
					s.xxmiFail(err, "diagnostics-start", map[string]any{"importer": key, "level": options.Level},
						diagnosticsMessage(err))
					return
				}
				s.toasts.Show(kit.ToastSuccess, T(prefix+"started"), "")
				if p.diag.start == d {
					p.diag.start = nil
				}
				keyed(s.xxmi.diagnostics, key).invalidate()
				if launchNow {
					s.startImporter(key)
				}
			})
		})
	}
	dismiss := func() {
		if !d.busy {
			p.diag.start = nil
		}
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "startTitle"), Description: T(prefix + "startDescription"), Body: body, Width: 512,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), dismiss, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T(confirm), start, kit.Loading(d.busy)),
		},
		OnClose:         dismiss,
		HideCloseButton: d.busy,
		Key:             "xxmi.diagnostics-start",
	})
}

func (s *Shell) diagnosticsReportDialog(cx *fairy.Cx, key string, r *diagnosticsReport) {
	t := kit.T()
	p := &s.xxmi.importer
	f := &s.settings.feedback
	const prefix = "page.setting.xxmi.diagnostics."
	const feedbackPrefix = "page.setting.feedback."
	if f.rules == nil && !f.rulesFailed {
		s.loadFeedbackRules()
	}
	rules := f.rules
	sending := r.progress != nil

	// The bundle is sized to the rules, so without them it is never collected.
	var bundle *xxmi.DiagnosticsBundle
	if rules != nil {
		maxBytes := rules.MaxMediaBytes
		if bundleFor := r.startedAt + "\n" + strconv.FormatInt(maxBytes, 10); r.bundleFor != bundleFor {
			r.bundle, r.bundleFor = query[xxmi.DiagnosticsBundle]{}, bundleFor
		}
		// Collecting again returns the bundle already written, so a second read does no harm.
		q := fetch(s, &r.bundle, func(ctx context.Context) (xxmi.DiagnosticsBundle, error) {
			return s.deps.XXMI.CollectDiagnostics(ctx, key, maxBytes)
		})
		if q.current {
			bundle = &q.value
		}
	}

	body := fairy.Col().Gap(16)
	switch {
	case f.rulesFailed:
		body.Child(xxmiAlert(lucide.TriangleAlert, true, "", T(feedbackPrefix+"error.rules_unavailable"),
			kit.Button(T(feedbackPrefix+"retry_rules"), s.loadFeedbackRules, kit.Ghost, kit.Small)))
	case r.bundle.err != nil:
		message := diagnosticsMessage(r.bundle.err)
		if message == "" {
			message = r.bundle.err.Error()
		}
		body.Child(xxmiAlert(lucide.TriangleAlert, true, "", message))
	case bundle == nil:
		body.Child(fairy.Row(kit.Spinner(16), fairy.Text(T(prefix+"collecting")).Color(t.MutedForeground)).Gap(8).
			Align(fairy.Center))
	default:
		folder := filepath.Dir(bundle.Path)
		log := "-"
		if bundle.LogBytes > 0 {
			log = formatSize(bundle.LogBytes)
		}
		summary := fairy.Col(
			fairy.Row(
				fairy.Text(filepath.Base(bundle.Path)).
					FontSize(14).
					LineHeight(20).
					Medium().
					Grow(1).
					MinW(0).
					Ellipsis().
					Tooltip(bundle.Path),
				fairy.Text(formatSize(bundle.Size)).FontSize(12).LineHeight(16).Color(t.MutedForeground).Shrink(0),
				kit.IconButton(lucide.FolderOpen, func() { s.openPath(folder) }, kit.Ghost, kit.XS,
					kit.Tooltip(T(prefix+"openFolder"))).Label(T(prefix+"openFolder")),
			).Gap(8).Align(fairy.Center),
			fairy.Text(T(prefix+"bundleSummary", "log", log, "dumps", bundle.Dumps, "events", bundle.CrashEvents)).
				FontSize(12).LineHeight(16).Color(t.MutedForeground),
		).Gap(4).PadXY(12, 8).Radius(6).Fill(t.Muted.Alpha(0.5))
		if bundle.LogTruncated {
			summary.Child(fairy.Text(T(prefix + "logTruncated")).FontSize(12).LineHeight(16).Color(t.MutedForeground))
		}
		body.Child(summary)
	}

	modNote := feedbackPrefix + "attachments.mod_login_required"
	if s.signedIn() {
		modNote = prefix + "modsDescription"
	}
	body.Child(
		fairy.Col(
			fairy.Text(T(feedbackPrefix+"body.label")).FontSize(14).LineHeight(20).Medium(),
			kit.TextArea(r.body, func(value string) { r.body = value },
				kit.Placeholder(T(prefix+"bodyPlaceholder")), kit.Disabled(sending),
				kit.Label(T(feedbackPrefix+"body.label"))).MinH(112).MaxH(240).ScrollY(),
		).Gap(8),
		fairy.Col(
			fairy.Text(T(prefix+"mods")).FontSize(14).LineHeight(20).Medium(),
			fairy.Text(T(modNote)).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
			s.diagnosticsModPicker(key, r, !s.signedIn() || sending),
		).Gap(8),
		kit.Checkbox(r.includeLog, func(on bool) { r.includeLog = on }, kit.Label(T(feedbackPrefix+"log.label")),
			kit.Description(T(feedbackPrefix+"log.description")), kit.Disabled(sending)),
	)
	if r.progress != nil {
		body.Child(kit.Progress(*r.progress / 100))
	}
	problem := ""
	if bundle != nil {
		problem = feedbackProblem(rules, f.rulesFailed, r.body, 1, bundle.Size)
	}
	if problem != "" {
		body.Child(fairy.Text(problem).FontSize(12).LineHeight(16).Color(t.Destructive))
	}

	dismiss := func() {
		if r.progress == nil && p.diag.report == r {
			p.diag.report = nil
			keyed(s.xxmi.diagnostics, key).invalidate()
		}
	}
	cancel := kit.Button(T("g.close"), dismiss, kit.Outline)
	submitLabel := T(feedbackPrefix + "submit")
	if sending {
		cancel = kit.Button(T(feedbackPrefix+"cancel"), func() {
			r.cancelled = true
			go s.deps.Feedback.Cancel()
		}, kit.Outline)
		submitLabel = T(feedbackPrefix + "sending")
	}
	send := func() {
		if bundle == nil || rules == nil {
			return
		}
		s.sendDiagnosticsReport(key, r, *bundle, *rules)
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "reportTitle"), Description: T(prefix + "reportDescription"),
		Body: fairy.Col(body).MaxH(560).ScrollY(), Width: 576,
		Actions: []*fairy.El{cancel, kit.Button(submitLabel, send, kit.Loading(sending),
			kit.Disabled(sending || rules == nil || bundle == nil || strings.TrimSpace(r.body) == "" ||
				problem != ""))},
		OnClose:         dismiss,
		HideCloseButton: sending,
		Key:             "xxmi.diagnostics-report",
	})
}

func (s *Shell) sendDiagnosticsReport(key string, r *diagnosticsReport, bundle xxmi.DiagnosticsBundle,
	rules feedback.Rules,
) {
	if r.progress != nil {
		return
	}
	request := feedback.Request{
		Category: "xxmi-libs", Body: r.body, IncludeLog: r.includeLog, Files: []string{bundle.Path},
		ModDirs: []feedback.ModDir{}, Locale: catalog.Language(),
	}
	if s.signedIn() {
		for _, mod := range r.mods {
			request.ModDirs = append(request.ModDirs, feedback.ModDir{Path: mod.Path, Name: mod.Relative})
		}
	}
	zero := 0.0
	r.progress, r.cancelled = &zero, false
	go func() {
		result, err := s.deps.Feedback.Submit(context.Background(), request)
		var endErr error
		if err == nil {
			endErr = s.deps.XXMI.EndDiagnostics(context.Background(), key)
		}
		fairy.Go(func() {
			r.progress = nil
			if err != nil {
				if !r.cancelled {
					s.toasts.Show(kit.ToastError, T("page.setting.feedback.failed"), feedbackFailure(err, rules))
				}
				return
			}
			s.feedbackSent(result)
			if endErr != nil {
				s.xxmiFail(endErr, "diagnostics-end", map[string]any{"importer": key}, diagnosticsMessage(endErr))
			}
			p := &s.xxmi.importer
			if p.diag.report == r {
				p.diag.report = nil
			}
			keyed(s.xxmi.diagnostics, key).invalidate()
		})
	}()
}

// diagnosticsCarries reports whether a picked ancestor folder carries mod. A report walks past a link inside the
// folder it attaches, so an ancestor carries a folder only from the folder's deepest link down.
func diagnosticsCarries(ancestor string, mod xxmi.DiagnosticsMod) bool {
	return strings.HasPrefix(mod.Relative, ancestor+"/") &&
		(mod.LinkedAt == "" || ancestor == mod.LinkedAt || strings.HasPrefix(ancestor, mod.LinkedAt+"/"))
}

func (r *diagnosticsReport) toggle(mod xxmi.DiagnosticsMod, checked bool) {
	// A picked folder is sent whole, so the folders it carries leave the selection.
	next := slices.DeleteFunc(slices.Clone(r.mods), func(other xxmi.DiagnosticsMod) bool {
		return other.Relative == mod.Relative || diagnosticsCarries(mod.Relative, other)
	})
	if checked {
		next = append(next, mod)
	}
	r.mods = next
}

// diagnosticsModPicker picks folders at any depth of the importer's Mods folder.
func (s *Shell) diagnosticsModPicker(key string, r *diagnosticsReport, disabled bool) *fairy.El {
	t := kit.T()
	const prefix = "page.setting.xxmi.diagnostics."
	tree := fairy.Col(s.diagnosticsModList(key, r, "", 0, disabled)...).MaxH(224).ScrollY().Pad(4).Radius(6).
		Border(1, t.Border)
	picker := fairy.Col(tree).Gap(8)
	if len(r.mods) > 0 {
		chips := fairy.Row(fairy.Text(T(prefix+"modsSelected", "count", len(r.mods))).FontSize(12).LineHeight(16).Color(t.MutedForeground)).
			Wrap().
			Gap(6).
			RowGap(6).
			Align(fairy.Center)
		for _, mod := range r.mods {
			chips.Child(fairy.Row(
				fairy.Text(mod.Relative).FontSize(12).LineHeight(16).MinW(0).Ellipsis().Tooltip(mod.Path),
				kit.IconButton(lucide.X, func() { r.toggle(mod, false) }, kit.Ghost, kit.XS, kit.Disabled(disabled),
					kit.Label(T("page.setting.feedback.attachments.remove"))),
			).Key("xxmi.diagnostics-mod:"+mod.Relative).Gap(2).Align(fairy.Center).PadEdges(2, 2, 2, 8).Radius(6).
				Fill(t.Muted.Alpha(0.5)).MinW(0).Shrink(1))
		}
		picker.Child(chips)
	}
	return picker
}

func (s *Shell) diagnosticsModList(key string, r *diagnosticsReport, parent string, depth int,
	disabled bool,
) []*fairy.El {
	t := kit.T()
	const prefix = "page.setting.xxmi.diagnostics."
	indent := float32(depth*16 + 8)
	list := fetch(s, keyed(r.lists, parent), func(ctx context.Context) ([]xxmi.DiagnosticsMod, error) {
		return s.deps.XXMI.ListDiagnosticsMods(ctx, key, parent)
	})
	switch {
	case !list.loaded && list.err != nil:
		return []*fairy.El{
			fairy.Text(list.err.Error()).
				FontSize(12).
				LineHeight(16).
				Color(t.Destructive).
				Wrap().
				PadEdges(4, 0, 4, indent),
		}
	case !list.loaded:
		return []*fairy.El{fairy.Row(kit.Spinner(14)).PadEdges(4, 0, 4, indent)}
	case len(list.value) == 0 && depth == 0:
		return []*fairy.El{
			fairy.Text(T(prefix+"noMods")).FontSize(12).LineHeight(16).Color(t.MutedForeground).PadXY(8, 4),
		}
	}

	var rows []*fairy.El
	for _, mod := range list.value {
		open := r.expanded[mod.Relative]
		checked, covered, partial := false, false, false
		for _, picked := range r.mods {
			checked = checked || picked.Relative == mod.Relative
			covered = covered || diagnosticsCarries(picked.Relative, mod)
			partial = partial || strings.HasPrefix(picked.Relative, mod.Relative+"/")
		}
		locked := disabled || covered
		toggle := fairy.Box().Size(24, 24).Shrink(0)
		if mod.HasChildren {
			turn, label := float32(0), prefix+"expandMod"
			if open {
				turn, label = math.Pi/2, prefix+"collapseMod"
			}
			toggle = kit.IconButton(lucide.ChevronRight, func() { r.expanded[mod.Relative] = !open }, kit.Ghost,
				kit.XS, kit.Label(T(label, "name", mod.Name))).Expanded(open).Rotate(turn).
				ScaleMotion(motion.Snappy)
		}
		name := fairy.Row(fairy.Text(mod.Name).FontSize(14).LineHeight(20).Grow(1).MinW(0).Ellipsis().Tooltip(mod.Path)).
			Gap(8).
			Grow(1).
			MinW(0).
			Align(fairy.Center)
		if mod.LinkedAt == mod.Relative {
			name.Child(kit.Badge(T(prefix+"modLinked"), kit.Outline).Tooltip(T(prefix + "modLinkedHint")))
		}
		if mod.Disabled {
			name.Child(kit.Badge(T(prefix+"modDisabled"), kit.Outline))
		}
		if !locked {
			name.Cursor(fairy.CursorPointer).OnClick(func() { r.toggle(mod, !checked) })
		}
		rows = append(rows, fairy.Row(
			toggle,
			fairy.Row(
				kit.Checkbox(checked || covered, func(on bool) { r.toggle(mod, on) },
					kit.Disabled(locked), kit.Indeterminate(!checked && !covered && partial)).Label(mod.Name),
				name,
			).Gap(8).Grow(1).MinW(0).Align(fairy.Center).PadXY(0, 4),
		).Key("xxmi.diagnostics-tree:"+mod.Relative).Gap(4).Align(fairy.Center).PadEdges(0, 8, 0, float32(depth*16)).
			Radius(6).HoverFill(t.Muted.Alpha(0.5)))
		if open {
			rows = append(rows, s.diagnosticsModList(key, r, mod.Relative, depth+1, disabled)...)
		}
	}
	return rows
}
