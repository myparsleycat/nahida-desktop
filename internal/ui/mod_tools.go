package ui

import (
	"context"
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
	busy bool
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
		fairy.Go(func() { m.bisect = snapshot })
	})
}

// runBisect runs one bisect action and shows the snapshot it returns. A failed action reads the state again,
// since it may have changed meanwhile.
func (s *Shell) runBisect(
	busy *bool,
	fields map[string]any,
	operation string,
	action func(context.Context) (tools.BisectSnapshot, error),
	failed func(error),
) {
	m := &s.mods
	if *busy || s.deps.Tools == nil {
		return
	}
	*busy = true
	s.background(func(ctx context.Context) {
		snapshot, err := action(ctx)
		var current *tools.BisectSnapshot
		if err != nil {
			current = s.deps.Tools.BisectGetState()
		}
		fairy.Go(func() {
			*busy = false
			if err == nil {
				m.bisect = &snapshot
				return
			}
			m.bisect = current
			if failed != nil {
				failed(err)
				return
			}
			s.modError(err, operation, fields)
			s.toasts.Show(kit.ToastError, err.Error(), "")
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
	sessionActive := status == "scanning" || status == "round" || status == "reverting"
	unfinalized := status == "done" && snapshot.FinalBadPath != nil
	canStart := d.game != "" && d.mod.IsEnabled && !sessionActive && !unfinalized
	canCancel := snapshot != nil && status != "idle" && status != "done" && status != "cancelled"
	const prefix = "page.mod.dialog.find-conflict."

	description := T(prefix+"description", "name", d.mod.Name)
	if sessionActive || unfinalized {
		description = T(prefix + "session-active")
	}
	body := fairy.Col().Gap(16)
	if !d.mod.IsEnabled {
		body.Child(fairy.Text(T(prefix+"mod-disabled-warning")).FontSize(12).Color(t.Warning).Wrap().Pad(8).
			Radius(6).Border(1, t.Warning.Alpha(0.2)).Fill(t.Warning.Alpha(0.12)))
	}
	start := func() {
		modPath := d.mod.Path
		s.runBisect(&d.busy, d.fields(), "bisect-start", func(ctx context.Context) (tools.BisectSnapshot, error) {
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
	cancel := func() { s.runBisect(&d.busy, d.fields(), "bisect-cancel", s.deps.Tools.BisectCancel, nil) }
	body.Child(fairy.Row(
		kit.Button(T("page.tools.mod_bisect.start"), start, kit.Disabled(!canStart || d.busy)),
		kit.Button(T("page.tools.mod_bisect.cancel"), cancel, kit.Destructive, kit.Disabled(!canCancel || d.busy)),
		fairy.Text(T("page.tools.mod_bisect.status."+string(status))).Font(monoFont).FontSize(12).
			Color(bisectStatusColor(status)),
	).Gap(8).Wrap().Align(fairy.Center))
	if card := s.bisectStatusCard(&d.busy, d.fields(), snapshot); card != nil {
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

func (s *Shell) bisectStatusCard(busy *bool, fields map[string]any, snapshot *tools.BisectSnapshot) *fairy.El {
	if snapshot == nil {
		return nil
	}
	t := kit.T()
	const prefix = "page.tools.mod_bisect."
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
			s.runBisect(busy, fields, "bisect-respond", func(ctx context.Context) (tools.BisectSnapshot, error) {
				return s.deps.Tools.BisectRespond(ctx, fixed)
			}, nil)
		}
		return section(
			T(prefix+"round_title", "round", snapshot.Round, "batchSize", snapshot.BatchSize, "remaining",
				len(snapshot.Candidates)),
			T(prefix+"round_description"),
			hints,
			fairy.Col(batch).H(224).ScrollY().Pad(8).Radius(6).Fill(t.Muted.Alpha(0.5)),
			fairy.Row(
				kit.Button(
					T(prefix+"undo"),
					func() { s.runBisect(busy, fields, "bisect-undo", s.deps.Tools.BisectUndoLastRound, nil) },
					kit.Outline,
					kit.Disabled(*busy || snapshot.UndoStackDepth == 0),
				),
				kit.Button(T(prefix+"still_broken"), func() { respond(false) }, kit.Destructive, kit.Disabled(*busy)),
				kit.Button(T(prefix+"problem_fixed"), func() { respond(true) }, kit.Disabled(*busy)),
			).Gap(8).Wrap(),
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
			s.runBisect(busy, fields, "bisect-finalize", func(ctx context.Context) (tools.BisectSnapshot, error) {
				return s.deps.Tools.BisectFinalize(ctx, keep)
			}, nil)
		}
		return section(
			T(prefix+"done_title"),
			T(prefix+"done_description", "path", bisectRelative(snapshot.ModRootPath, bad)),
			fairy.Text(T(prefix+"keep_disabled_hint", "from", base, "to", disabled+base)).FontSize(12).
				Color(t.MutedForeground).Wrap(),
			fairy.Text(T(prefix+"new_bisect_hint")).FontSize(12).Color(t.MutedForeground).Wrap(),
			fairy.Row(
				kit.Button(T(prefix+"keep_disabled"), func() { finalize([]string{bad}) }, kit.Outline,
					kit.Disabled(*busy)),
				kit.Button(T(prefix+"re_enable_all"), func() { finalize(nil) }, kit.Disabled(*busy)),
			).Gap(8).Wrap(),
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
