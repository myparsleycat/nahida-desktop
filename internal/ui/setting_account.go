package ui

import (
	"context"
	"unicode/utf8"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
)

const accountURL = "https://nahida.live/u"

func (s *Shell) accountSettings() *fairy.El {
	t := kit.T()
	centered := func(children ...*fairy.El) *fairy.El {
		return fairy.Col(children...).WFull().Align(fairy.Center).Gap(16).Pad(16)
	}
	login := kit.Button(T("page.setting.acc.not_logged_in.login"), s.startLogin)
	switch {
	case !s.sessionKnown:
		return centered(fairy.Spinner().Size(24, 24).Color(t.MutedForeground))
	case s.session == nil && s.backendDown():
		second := login
		if s.hasToken {
			second = kit.Button(T("page.setting.acc.logout"), s.startLogout)
		}
		return centered(
			fairy.Text(T("page.setting.acc.server_unreachable")).FontSize(16).LineHeight(24).Color(t.MutedForeground),
			fairy.Row(kit.Button(T("page.setting.acc.retry"), s.refreshSession), second).Gap(8),
		)
	case s.session == nil:
		return centered(fairy.Text(T("page.setting.acc.not_logged_in.description")).FontSize(16).LineHeight(24).
			Color(t.MutedForeground), login)
	}

	user := s.session.User
	initial := ""
	if user.Name != "" {
		first, _ := utf8.DecodeRuneInString(user.Name)
		initial = string(first)
	}
	avatar := fairy.Box(fairy.Text(initial).LineHeight(20).Color(t.MutedForeground)).Size(32, 32).Center().Radius(16).
		Fill(t.Muted).FontSize(14).Role(fairy.RoleNone)
	if user.Image != nil {
		if img := s.gbImage(*user.Image, 32, 32, fairy.FitCover); img != nil {
			avatar = fairy.ImageEl(img).Size(32, 32).Fit(fairy.FitCover).Radius(16).Clip().Role(fairy.RoleNone)
		}
	}
	profile := fairy.Row(
		avatar,
		fairy.Col(fairy.Text(user.Name).LineHeight(20), fairy.Text(user.Email).LineHeight(20)).
			FontSize(14).
			Grow(1).
			MinW(0),
		kit.Button(T("page.setting.acc.my_account"), func() { s.openExternal(accountURL) }, kit.Secondary),
	).Gap(12).Align(fairy.Center).PadXY(0, 12)
	return settingsBody(
		settingsGroup("", profile),
		fairy.Row(kit.Button(T("page.setting.acc.logout"), s.startLogout)).Justify(fairy.JustifyEnd),
	)
}

func (s *Shell) startLogout() {
	go func() {
		if err := s.deps.Auth.StartLogout(context.Background()); err != nil {
			err = infra.ReportError(s.deps.Log, err, "Auth.StartLogout", infra.Diagnostic{Operation: "auth.logout"})
			s.toasts.Show(kit.ToastError, T("page.setting.acc.logout"), err.Error())
		}
	}()
}

// refreshSession retries the session; without a stored token it only probes whether the server is back.
func (s *Shell) refreshSession() {
	hasToken := s.hasToken
	go func() {
		ctx := context.Background()
		if !hasToken {
			status := infra.BackendStatus(s.deps.Auth.Probe(ctx))
			fairy.Go(func() { s.backend = status })
		}
		session, err := s.deps.Auth.GetSession(ctx)
		if err != nil {
			s.report(err, "refresh-session")
		}
		fairy.Go(func() { s.setSession(session, session != nil || s.hasToken) })
	}()
}
