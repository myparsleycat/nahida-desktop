package ui

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
)

// conflictDraft is the conflict finder: a bisect of the game's mods that keeps one mod enabled throughout.
type conflictDraft struct {
	mod  mod.ModInfo
	game string
}

func (d *conflictDraft) fields() map[string]any {
	return map[string]any{"game": d.game, "mod": d.mod.Path}
}

func (s *Shell) openConflictFinder(info mod.ModInfo) {
	m := &s.mods
	m.conflict = &conflictDraft{mod: info, game: m.game}
	if s.deps.Tools == nil {
		return
	}
	s.background(func(context.Context) {
		snapshot := s.deps.Tools.BisectGetState()
		fairy.Go(func() { s.setBisect(snapshot) })
	})
}

// setBisect shows snapshot unless the shown state is newer. Events, action results and state reads can arrive out
// of order.
func (s *Shell) setBisect(snapshot *tools.BisectSnapshot) {
	m := &s.mods
	if snapshot == nil || m.bisect != nil && snapshot.Version < m.bisect.Version {
		return
	}
	m.bisect = snapshot
}

// bisectHasSession reports whether the backend holds a bisect session, including a finished one not finalized yet.
func bisectHasSession(snapshot *tools.BisectSnapshot) bool {
	return snapshot != nil && snapshot.Active
}

// runBisect runs one bisect action and shows the snapshot it returns. A failed action reads the state again,
// since it may have changed meanwhile. One action runs at a time across the tools page and the conflict finder.
func (s *Shell) runBisect(
	fields map[string]any,
	operation string,
	action func(context.Context) (tools.BisectSnapshot, error),
	failed func(error),
) {
	m := &s.mods
	if m.bisectBusy || s.deps.Tools == nil {
		return
	}
	m.bisectBusy = true
	s.background(func(ctx context.Context) {
		snapshot, err := action(ctx)
		var current *tools.BisectSnapshot
		if err != nil {
			current = s.deps.Tools.BisectGetState()
		}
		fairy.Go(func() {
			m.bisectBusy = false
			if err == nil {
				s.setBisect(&snapshot)
				return
			}
			s.setBisect(current)
			if failed != nil {
				failed(err)
				return
			}
			if errors.Is(err, tools.ErrBisectStateChanged) {
				s.toasts.Show(kit.ToastWarning, T("page.tools.mod_bisect.state_changed"), "")
				return
			}
			s.modError(err, operation, fields)
			// A rename failure that leaves the files out of sync returns the raw rename error; the state says so.
			message := err.Error()
			if errors.Is(err, tools.ErrBisectOutOfSync) || current != nil && current.OutOfSync {
				message = T("page.tools.mod_bisect.out_of_sync")
			}
			s.toasts.Show(kit.ToastError, message, "")
		})
	})
}

func isExcludeValidationMessage(message string) bool {
	for _, text := range []string{
		"Exclude path is empty.", "Exclude path must be inside the selected importer.",
		"Exclude path cannot be the importer root.", "Exclude path does not exist.",
	} {
		if strings.Contains(message, text) {
			return true
		}
	}
	return false
}

func (s *Shell) conflictDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.conflict
	if d == nil {
		return
	}
	t := kit.T()
	snapshot := m.bisect
	status := tools.BisectStatus("idle")
	if snapshot != nil {
		status = snapshot.Status
	}
	hasSession := bisectHasSession(snapshot)
	busy := m.bisectBusy
	canStart := d.game != "" && d.mod.IsEnabled && !hasSession && status != "reverting"
	const prefix = "page.mod.dialog.find-conflict."

	description := T(prefix+"description", "name", d.mod.Name)
	if hasSession {
		description = T(prefix + "session-active")
	}
	body := fairy.Col().Gap(16)
	if !d.mod.IsEnabled {
		body.Child(fairy.Text(T(prefix+"mod-disabled-warning")).FontSize(12).Color(t.Warning).Wrap().Pad(8).
			Radius(6).Border(1, t.Warning.Alpha(0.2)).Fill(t.Warning.Alpha(0.12)))
	}
	start := func() {
		modPath := d.mod.Path
		s.runBisect(d.fields(), "bisect-start", func(ctx context.Context) (tools.BisectSnapshot, error) {
			return s.deps.Tools.BisectStart(ctx, d.game, []string{modPath})
		}, func(err error) {
			if isExcludeValidationMessage(err.Error()) {
				s.toasts.Show(kit.ToastWarning, T("page.tools.mod_bisect.exclude_invalid"), "")
				return
			}
			s.modError(err, "bisect-start", map[string]any{"game": d.game, "mod": modPath})
			s.toasts.Show(kit.ToastError, err.Error(), "")
		})
	}
	cancel := func() { s.runBisect(d.fields(), "bisect-cancel", s.deps.Tools.BisectCancel, nil) }
	body.Child(fairy.Row(
		kit.Button(T("page.tools.mod_bisect.start"), start, kit.Disabled(!canStart || busy)),
		kit.Button(T("page.tools.mod_bisect.cancel"), cancel, kit.Destructive, kit.Disabled(!hasSession || busy)),
		fairy.Text(T("page.tools.mod_bisect.status."+string(status))).Font(monoFont).FontSize(12).
			Color(bisectStatusColor(status)),
	).Gap(8).Wrap().Align(fairy.Center))
	if card := s.bisectStatusCard(d.fields(), snapshot); card != nil {
		body.Child(card)
	}

	close := func() { m.conflict = nil }
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + "title"),
		Description: description,
		Body:        body,
		Width:       704,
		OnClose:     close,
		Key:         "mod.find-conflict",
	})
}

func bisectStatusColor(status tools.BisectStatus) fairy.Color {
	t := kit.T()
	switch status {
	case "round":
		return t.Warning
	case "done":
		return t.Success
	case "reverting", "scanning":
		return t.Accent
	default:
		return t.MutedForeground
	}
}

// bisectRelative shows path relative to the mod folder of the bisect.
func bisectRelative(root *string, path string) string {
	if root == nil || !platform.SameOrChildPath(*root, path) {
		return path
	}
	return strings.TrimLeft(path[len(strings.TrimRight(*root, `\/`)):], `\/`)
}

func (s *Shell) bisectStatusCard(fields map[string]any, snapshot *tools.BisectSnapshot) *fairy.El {
	if snapshot == nil {
		return nil
	}
	t := kit.T()
	const prefix = "page.tools.mod_bisect."
	busy := s.mods.bisectBusy
	version := snapshot.Version
	outOfSync := snapshot.OutOfSync
	outOfSyncNote := func() *fairy.El {
		return fairy.Text(T(prefix + "out_of_sync")).FontSize(12).LineHeight(16).Color(t.Destructive).Wrap().Pad(8).
			Radius(6).Fill(t.Destructive.Alpha(0.1))
	}
	note := func(key string) *fairy.El {
		return fairy.Text(T(prefix + key)).FontSize(14).Color(t.MutedForeground).Wrap()
	}
	section := func(title, description string, children ...*fairy.El) *fairy.El {
		return fairy.Col(
			fairy.Text(title).Medium(),
			fairy.Text(description).FontSize(13).Color(t.MutedForeground).Wrap(),
			fairy.Col(children...).Gap(8),
		).Gap(6)
	}
	switch status := snapshot.Status; {
	case status == "round":
		hints := fairy.Col(fairy.Text(T(prefix+"disabled_count", "count", len(snapshot.CurrentBatch))).FontSize(12).
			Color(t.MutedForeground)).Gap(4)
		if n := len(snapshot.ExcludePaths); n > 0 {
			hints.Child(fairy.Text(T(prefix+"exclude_count", "count", n)).FontSize(12).Color(t.MutedForeground))
		}
		batch := fairy.Col().Gap(2).Font(monoFont).FontSize(12)
		for _, path := range snapshot.CurrentBatch {
			batch.Child(fairy.Text(bisectRelative(snapshot.ModRootPath, path)).Wrap())
		}
		respond := func(fixed bool) {
			s.runBisect(fields, "bisect-respond", func(ctx context.Context) (tools.BisectSnapshot, error) {
				return s.deps.Tools.BisectRespond(ctx, version, fixed)
			}, nil)
		}
		undo := func() {
			s.runBisect(fields, "bisect-undo", func(ctx context.Context) (tools.BisectSnapshot, error) {
				return s.deps.Tools.BisectUndoLastRound(ctx, version)
			}, nil)
		}
		answerDisabled := busy || outOfSync
		children := []*fairy.El{hints, fairy.Col(batch).H(224).ScrollY().Pad(8).Radius(6).Fill(t.Muted.Alpha(0.5))}
		if outOfSync {
			children = append(children, outOfSyncNote())
		}
		children = append(children, fairy.Row(
			kit.Button(T(prefix+"undo"), undo, kit.Outline,
				kit.Disabled(answerDisabled || snapshot.UndoStackDepth == 0)),
			kit.Button(T(prefix+"still_broken"), func() { respond(false) }, kit.Destructive,
				kit.Disabled(answerDisabled)),
			kit.Button(T(prefix+"problem_fixed"), func() { respond(true) }, kit.Disabled(answerDisabled)),
		).Gap(8).Wrap())
		return section(
			T(prefix+"round_title", "round", snapshot.Round, "batchSize", snapshot.BatchSize, "remaining",
				len(snapshot.Candidates)),
			T(prefix+"round_description"),
			children...,
		)
	case status == "scanning":
		return note("scanning")
	case status == "done" && snapshot.FinalBadPath != nil:
		bad := *snapshot.FinalBadPath
		base := filepath.Base(bad)
		disabled := "DISABLED "
		if s.stringSetting(setting.KeyModDisabledPrefixStyle) == "underscore" {
			disabled = "DISABLED_"
		}
		finalize := func(keep []string) {
			s.runBisect(fields, "bisect-finalize", func(ctx context.Context) (tools.BisectSnapshot, error) {
				return s.deps.Tools.BisectFinalize(ctx, version, keep)
			}, nil)
		}
		children := []*fairy.El{
			fairy.Text(T(prefix+"keep_disabled_hint", "from", base, "to", disabled+base)).FontSize(12).
				Color(t.MutedForeground).Wrap(),
			fairy.Text(T(prefix + "new_bisect_hint")).FontSize(12).Color(t.MutedForeground).Wrap(),
		}
		if outOfSync {
			children = append(children, outOfSyncNote())
		}
		children = append(children, fairy.Row(
			kit.Button(T(prefix+"keep_disabled"), func() { finalize([]string{bad}) }, kit.Outline,
				kit.Disabled(busy)),
			kit.Button(T(prefix+"re_enable_all"), func() { finalize(nil) }, kit.Disabled(busy)),
		).Gap(8).Wrap())
		return section(
			T(prefix+"done_title"),
			T(prefix+"done_description", "path", bisectRelative(snapshot.ModRootPath, bad)),
			children...,
		)
	case status == "done" && snapshot.Error != nil && *snapshot.Error == "All enabled INIs were excluded":
		return note("all_excluded")
	case status == "done" && snapshot.Error != nil:
		return note("bisect_inconclusive")
	case status == "done":
		return note("no_inis_found")
	case status == "cancelled":
		return note("cancelled")
	case status == "reverting":
		return note("reverting")
	}
	return nil
}

// urlDownloadDraft is the form that downloads an archive from a URL into the selected folder.
type urlDownloadDraft struct {
	groupName, groupPath string
	url                  string
	touched              bool
	busy                 bool
}

var downloadURLCodes = regexp.MustCompile(
	`\b(DOWNLOAD_URL_REQUIRED|INVALID_DOWNLOAD_URL|UNSUPPORTED_DOWNLOAD_URL_PROTOCOL|DOWNLOAD_URL_HTML_PAGE)\b`)

// isDownloadURL accepts an absolute http or https URL with a host, as the backend does. The length cap only
// bounds an accidental large paste.
func isDownloadURL(value string) bool {
	if value == "" || len(value) > 16_384 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return scheme == "http" || scheme == "https"
}

func (s *Shell) openURLDownload() {
	m := &s.mods
	if m.group == nil {
		return
	}
	m.urlDownload = &urlDownloadDraft{groupName: m.group.Name, groupPath: m.group.Path}
}

func (s *Shell) submitURLDownload(d *urlDownloadDraft) {
	m := &s.mods
	d.touched = true
	address := strings.TrimSpace(d.url)
	if d.busy || d.groupPath == "" || !isDownloadURL(address) {
		return
	}
	d.busy = true
	s.background(func(ctx context.Context) {
		_, err := s.deps.Mod.DownloadFromURL(ctx, address, d.groupPath)
		fairy.Go(func() {
			d.busy = false
			if err == nil {
				if m.urlDownload == d {
					m.urlDownload = nil
				}
				return
			}
			const prefix = "page.mod.content-header.download_dialog."
			switch downloadURLCodes.FindString(err.Error()) {
			case "DOWNLOAD_URL_HTML_PAGE":
				s.toasts.Show(kit.ToastWarning, T(prefix+"file_only"), "")
			case "DOWNLOAD_URL_REQUIRED", "INVALID_DOWNLOAD_URL":
				s.toasts.Show(kit.ToastError, T(prefix+"invalid_url"), "")
			case "UNSUPPORTED_DOWNLOAD_URL_PROTOCOL":
				s.toasts.Show(kit.ToastError, T(prefix+"unsupported_protocol"), "")
			default:
				s.modError(err, "download-from-url", map[string]any{"groupPath": d.groupPath})
				s.toasts.Show(kit.ToastError, err.Error(), "")
			}
		})
	})
}

func (s *Shell) urlDownloadDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.urlDownload
	if d == nil {
		return
	}
	const prefix = "page.mod.content-header.download_dialog."
	close := func() {
		if !d.busy {
			m.urlDownload = nil
		}
	}
	invalid := d.touched && !isDownloadURL(strings.TrimSpace(d.url))
	field := kit.TextField(d.url, func(v string) { d.url, d.touched = v, true },
		kit.Placeholder(T(prefix+"url_placeholder")), kit.Invalid(invalid))
	field.AutoFocus().OnSubmit(func(string) { s.submitURLDownload(d) })
	target := d.groupName
	if target == "" {
		target = T(prefix + "no_target")
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "title"),
		Body: fairy.Col(
			kit.Field(T(prefix+"url_label"), field, kit.Invalid(invalid),
				kit.Description(errorText(invalid, T(prefix+"invalid_url")))),
			kit.Field(T(prefix+"target_label"), kit.TextField(target, nil)),
		).Gap(16),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T("g.download"), func() { s.submitURLDownload(d) }, kit.Loading(d.busy),
				kit.Disabled(d.groupPath == "" || invalid)),
		},
		OnClose: close,
		Key:     "mod.url-download",
	})
}
