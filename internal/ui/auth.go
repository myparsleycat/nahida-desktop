package ui

import (
	"context"
	"errors"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/clipboard"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/auth"
	"nahida.live/desktop/internal/infra"
)

// loginState is the sign-in started from the auth screen, the account settings or the agent share dialog. seq
// tells the current sign-in apart from a stopped or replaced one, whose result is ignored. page stays empty until
// the backend returns the sign-in page.
type loginState struct {
	busy       bool
	page       string
	openFailed bool
	copied     bool
	copySeq    int
	cancel     context.CancelFunc
	seq        int
}

func (s *Shell) authPage() *fairy.El {
	t := kit.T()
	if s.login.busy {
		return fairy.Col(s.loginProgress()).Grow(1).WFull().Center().Pad(16)
	}
	return fairy.Col(
		fairy.Icon(lucide.LogIn).Size(48, 48).Color(t.Foreground),
		kit.Button(T("page.setting.acc.not_logged_in.login"), s.startLogin),
	).Grow(1).WFull().Center().Gap(24)
}

// loginProgress shows the running sign-in: a spinner while the backend prepares it, then the sign-in page link
// with copy, reopen and cancel actions.
func (s *Shell) loginProgress() *fairy.El {
	t := kit.T()
	cancel := kit.Button(T("page.auth.cancel"), s.stopLogin, kit.Ghost)
	if s.login.page == "" {
		return fairy.Col(
			fairy.Spinner().Size(32, 32).Color(t.MutedForeground),
			fairy.Text(T("page.auth.preparing")).FontSize(14).LineHeight(20).Color(t.MutedForeground),
			cancel,
		).Align(fairy.Center).Gap(16)
	}

	description := fairy.Text(T("page.auth.waiting_description")).Color(t.MutedForeground)
	if s.login.openFailed {
		description = fairy.Text(T("page.auth.open_failed")).Color(t.Destructive)
	}
	copyIcon := lucide.Copy
	if s.login.copied {
		copyIcon = lucide.Check
	}
	link := fairy.Row(
		fairy.Text(s.login.page).FontSize(12).LineHeight(16).Color(t.MutedForeground).Selectable().
			Grow(1).MinW(0).PadEdges(6, 0, 6, 0),
		kit.Button("", s.copyLoginLink, kit.Ghost, kit.Small, kit.Icon(copyIcon), kit.Tooltip(T("page.auth.copy_link"))).
			Label(T("page.auth.copy_link")),
	).WFull().Align(fairy.Start).Gap(8).PadEdges(4, 4, 4, 12).Radius(t.RadiusMd()).Fill(t.Muted.Alpha(0.5)).
		Border(1, t.Border)
	return fairy.Col(
		fairy.Row(
			fairy.Spinner().Size(18, 18).Color(t.MutedForeground),
			fairy.Text(T("page.auth.waiting_title")).FontSize(18).LineHeight(28).SemiBold(),
		).Align(fairy.Center).Gap(10),
		description.FontSize(13).LineHeight(20).TextAlign(fairy.TextCenter).WFull(),
		link,
		fairy.Row(
			kit.Button(T("page.auth.reopen"), s.reopenLoginPage, kit.Outline, kit.Icon(lucide.ExternalLink)),
			cancel,
		).Gap(8),
	).WFull().MaxW(440).Align(fairy.Center).Gap(16)
}

// startLogin opens the web sign-in and waits for its deep link. A successful sign-in from the auth screen
// returns to the previous screen.
func (s *Shell) startLogin() {
	// Auth.StartLogin registers here on the UI thread, so the latest click owns the login and the one it replaces
	// ends as replaced.
	ctx, cancel := context.WithCancel(context.Background())
	s.login = loginState{busy: true, cancel: cancel, seq: s.login.seq + 1}
	seq := s.login.seq
	run := s.deps.Auth.StartLogin(ctx, func(page string, openErr error) {
		fairy.Go(func() {
			if s.login.seq == seq && s.login.busy {
				s.login.page, s.login.openFailed = page, openErr != nil
			}
		})
	})

	go func() {
		defer cancel()
		err := run()
		cancelled := infra.IsCancellationError(err)
		expired := errors.Is(err, auth.ErrLoginExpired)
		if err != nil && !cancelled && !expired {
			err = infra.ReportError(s.deps.Log, err, "Auth.StartLogin", infra.Diagnostic{Operation: "auth.login"})
		}
		fairy.Go(func() {
			if s.login.seq != seq {
				return
			}
			s.login = loginState{seq: seq}
			switch {
			// Its auth:update handler has already run when it succeeded.
			case err == nil && s.signedIn():
				if s.router.Current() == RouteAuth && !s.router.Back() {
					s.navigate(RouteMod)
				}
			case expired:
				s.toasts.Show(kit.ToastInfo, T("page.auth.expired"), "")
			case err != nil && !cancelled:
				s.toasts.Show(kit.ToastError, T("page.auth.error"), err.Error())
			}
		})
	}()
}

func (s *Shell) stopLogin() {
	if s.login.cancel != nil {
		s.login.cancel()
	}
	s.login = loginState{seq: s.login.seq + 1}
}

func (s *Shell) copyLoginLink() {
	if s.login.page == "" {
		return
	}
	if !clipboard.SetText(s.login.page) {
		s.toasts.Show(kit.ToastError, T("page.auth.copy_failed"), "")
		return
	}
	s.toasts.Show(kit.ToastSuccess, T("page.auth.copied"), "")
	s.login.copied = true
	s.login.copySeq++
	seq, copySeq := s.login.seq, s.login.copySeq
	time.AfterFunc(1500*time.Millisecond, func() {
		fairy.Go(func() {
			if s.login.seq == seq && s.login.copySeq == copySeq {
				s.login.copied = false
			}
		})
	})
}

func (s *Shell) reopenLoginPage() {
	page, seq := s.login.page, s.login.seq
	if page == "" {
		return
	}
	go func() {
		err := s.deps.Platform.OpenExternal(page)
		if err != nil {
			_ = infra.ReportError(s.deps.Log, err, "Auth", infra.Diagnostic{
				Severity:  infra.DiagnosticWarn,
				Operation: "reopen-login-page",
				Fields:    map[string]any{"url": infra.SanitizeLogURL(page)},
			})
		}
		fairy.Go(func() {
			if s.login.seq == seq && s.login.page == page {
				s.login.openFailed = err != nil
			}
		})
	}()
}
