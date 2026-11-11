package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// Confirmation is a modal question with a confirm and a cancel button, laid out as the React AlertDialog every
// confirmation used.
type Confirmation struct {
	Title       string
	Description string
	// Body, when set, builds content under the description on every frame the dialog shows.
	Body func() *fairy.El
	// ConfirmLabel and CancelLabel default to the common Confirm and Cancel texts.
	ConfirmLabel string
	CancelLabel  string
	// Danger styles the confirm button as destructive.
	Danger bool
	// Width defaults to 384.
	Width float32
	// OnResult runs on the UI thread with false when the dialog is cancelled or dismissed.
	OnResult func(confirmed bool)
	// Run, when set, runs on a background goroutine after the confirm button while the dialog stays open with a
	// busy confirm button. The dialog closes when it succeeds; an error stays in the dialog under Failed, or the
	// error's text when Failed is empty. OnResult(true) runs only after a success.
	Run    func() error
	Failed string

	id      int
	running bool
	err     error
}

// Confirm shows c after the confirmations already queued. It may be called from any goroutine.
func (s *Shell) Confirm(c Confirmation) {
	fairy.Go(func() {
		s.confirmSeq++
		c.id = s.confirmSeq
		s.confirms = append(s.confirms, c)
	})
}

func (s *Shell) showConfirm(cx *fairy.Cx) {
	if len(s.confirms) == 0 {
		return
	}
	c := s.confirms[0]
	// A second click that lands before the dialog leaves must not answer the next confirmation.
	answer := func(confirmed bool) {
		if len(s.confirms) == 0 || s.confirms[0].id != c.id || s.confirms[0].running {
			return
		}
		if confirmed && c.Run != nil {
			s.runConfirm(c)
			return
		}
		s.confirms = s.confirms[1:]
		if c.OnResult != nil {
			c.OnResult(confirmed)
		}
	}
	confirmLabel, cancelLabel := c.ConfirmLabel, c.CancelLabel
	if confirmLabel == "" {
		confirmLabel = T("g.confirm")
	}
	if cancelLabel == "" {
		cancelLabel = T("g.cancel")
	}
	variant := kit.Default
	if c.Danger {
		variant = kit.Destructive
	}
	width := c.Width
	if width == 0 {
		width = 384
	}
	var body *fairy.El
	if c.Body != nil {
		body = c.Body()
	}
	if c.err != nil {
		failed := c.Failed
		if failed == "" {
			failed = c.err.Error()
		}
		body = fairy.Col().
			Child(body, fairy.Text(failed).FontSize(14).LineHeight(20).Color(kit.T().Destructive)).
			Gap(16)
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       c.Title,
		Description: c.Description,
		Body:        body,
		Width:       width,
		Actions: []*fairy.El{
			kit.Button(cancelLabel, func() { answer(false) }, kit.Outline, kit.Disabled(c.running)),
			kit.Button(confirmLabel, func() { answer(true) }, variant, kit.Loading(c.running)),
		},
		OnClose: func() { answer(false) },
		Alert:   true,
		Key:     "ui.confirm",
	})
}

// runConfirm runs the action of the confirmation at the head of the queue and closes it once the action
// succeeds.
func (s *Shell) runConfirm(c Confirmation) {
	s.confirms[0].running, s.confirms[0].err = true, nil
	go func() {
		err := c.Run()
		fairy.Go(func() {
			i := slices.IndexFunc(s.confirms, func(q Confirmation) bool { return q.id == c.id })
			if i < 0 {
				return
			}
			s.confirms[i].running = false
			if err != nil {
				s.confirms[i].err = err
				return
			}
			s.confirms = slices.Delete(s.confirms, i, i+1)
			if c.OnResult != nil {
				c.OnResult(true)
			}
		})
	}()
}

// promptUpdate asks to restart into the downloaded update. Later dismisses the prompt on the backend, so it
// is not offered again for this download.
func (s *Shell) promptUpdate() {
	if s.updateOffered {
		return
	}
	s.updateOffered = true
	description := T("updater.toast.available.description")
	if version := s.updater.ReleaseVersion; version != nil {
		description += "\n(" + platform.AppVersion + " → " + strings.TrimPrefix(*version, "v") + ")"
	}
	showOriginal := false
	s.Confirm(Confirmation{
		Title:        T("updater.toast.available.title"),
		Description:  description,
		Width:        576,
		Body:         func() *fairy.El { return s.releaseNotes(&showOriginal) },
		ConfirmLabel: T("updater.toast.available.action"),
		CancelLabel:  T("g.later"),
		// The dialog stays open while the swap helper starts, so a failed install can be retried or dismissed.
		Run: func() error {
			err := s.deps.Updater.InstallUpdate(context.Background())
			if err == nil {
				return nil
			}
			return infra.ReportError(s.deps.Log, err, "Updater.InstallUpdate", infra.Diagnostic{
				Operation: "updater.install", Stage: "shell",
			})
		},
		OnResult: func(confirmed bool) {
			if !confirmed {
				s.updateOffered = false
				go s.deps.Updater.DismissUpdateDialog()
			}
		},
	})
}

func (s *Shell) releaseNotes(showOriginal *bool) *fairy.El {
	notes := s.updater.ReleaseNotes
	if notes == nil {
		return nil
	}
	t := kit.T()
	original, translated := deref(notes.Original), deref(notes.Translated)
	text := translated
	if text == "" || *showOriginal {
		text = original
	}
	if text == "" {
		return nil
	}
	head := fairy.Row(fairy.Text(T("updater.toast.available.releaseNotesTitle")).FontSize(14).LineHeight(20).
		Medium().Grow(1)).Gap(12).Align(fairy.Center)
	if original != "" && translated != "" {
		label := T("updater.toast.available.showOriginal")
		if *showOriginal {
			label = T("updater.toast.available.showTranslation")
		}
		head.Child(kit.Button(label, func() { *showOriginal = !*showOriginal }, kit.Ghost, kit.Small))
	}
	return fairy.Col(
		head,
		fairy.Col(s.releaseNotesContent(text)).H(256).ScrollY().PadXY(12, 10).
			Radius(t.RadiusMd()).Fill(t.Muted.Alpha(0.5)),
	).Gap(8)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Shell) downloadUpdate() {
	if s.updateBusy {
		return
	}
	s.updateBusy = true
	go func() {
		err := s.deps.Updater.DownloadUpdate(context.Background())
		if err != nil && !infra.IsCancellationError(err) {
			err = infra.ReportError(s.deps.Log, err, "Updater.DownloadUpdate", infra.Diagnostic{
				Operation: "updater.download", Stage: "shell",
			})
			s.toasts.Show(kit.ToastError, T("updater.titlebar.downloadAction"), err.Error())
		}
		fairy.Go(func() { s.updateBusy = false })
	}()
}
