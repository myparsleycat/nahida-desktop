package ui

import (
	"context"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
)

// loginStopDelay is how long a sign-in runs before the auth screen offers to stop it, as the React page did.
const loginStopDelay = 5 * time.Second

// loginState is the sign-in started from the auth screen or the account settings. seq tells the current
// sign-in apart from a stopped or replaced one, whose result is ignored.
type loginState struct {
	busy, stoppable bool
	cancel          context.CancelFunc
	seq             int
}

func (s *Shell) authPage() *fairy.El {
	t := kit.T()
	indicator := fairy.Icon(lucide.LogIn).Size(48, 48)
	if s.login.busy {
		indicator = fairy.Spinner().Size(48, 48)
	}
	buttons := fairy.Row(
		kit.Button(T("page.setting.acc.not_logged_in.login"), s.startLogin, kit.Disabled(s.login.busy)),
	).Gap(12)
	if s.login.stoppable {
		buttons.Child(kit.Button(T("page.auth.stop"), s.stopLogin))
	}
	return fairy.Col(indicator.Color(t.Foreground), buttons).Grow(1).WFull().Center().Gap(24)
}

// startLogin opens the web sign-in and waits for its deep link. A successful sign-in from the auth screen
// returns to the previous screen.
func (s *Shell) startLogin() {
	if s.login.cancel != nil {
		s.login.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.login.seq++
	seq := s.login.seq
	s.login.busy, s.login.stoppable, s.login.cancel = true, false, cancel

	time.AfterFunc(loginStopDelay, func() {
		fairy.Go(func() {
			if s.login.seq == seq && s.login.busy {
				s.login.stoppable = true
			}
		})
	})
	go func() {
		defer cancel()
		err := s.deps.Auth.StartLogin(ctx)
		cancelled := infra.IsCancellationError(err)
		if err != nil && !cancelled {
			err = infra.ReportError(s.deps.Log, err, "Auth.StartLogin", infra.Diagnostic{Operation: "auth.login"})
		}
		fairy.Go(func() {
			if s.login.seq != seq {
				return
			}
			s.login = loginState{seq: seq}
			switch {
			// StartLogin also returns nil when the sign-in page was never approved in time or another caller
			// replaced it; its auth:update handler has already run when it succeeded.
			case err == nil:
				if s.signedIn() && s.router.Current() == RouteAuth && !s.router.Back() {
					s.navigate(RouteMod)
				}
			case !cancelled:
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
