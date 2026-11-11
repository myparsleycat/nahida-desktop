package ui

import (
	"context"
	"regexp"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

const githubTokenURL = "https://github.com/settings/personal-access-tokens/new"

var (
	proxyErrorFields = []string{
		"host",
		"port",
		"username",
		"credentials",
		"passwordResetRequired",
		"passwordAction",
		"type",
	}
	proxyHostRejected = regexp.MustCompile(`[\s/\\@?#]`)
)

type networkSection struct {
	proxy        *setting.ProxySettings
	proxyLoading bool
	proxyFailed  bool
	draft        setting.ProxySettingsInput
	port         string
	saving       bool
	proxyError   string

	github        *setting.GitHubTokenStatus
	githubLoading bool
	githubFailed  bool
	token         string
	tokenSaving   bool
	tokenError    string
}

func (s *Shell) networkSettings() *fairy.El {
	return settingsBody(s.proxySettings(), s.githubTokenSettings())
}

func networkField(label string, control *fairy.El, description string) *fairy.El {
	// The React label is inline inside a space-y-1 block, so only the control's margin separates it from the
	// description; the label sits directly on the control.
	field := fairy.Col(fairy.Text(label).FontSize(14).LineHeight(20), control.Label(label))
	if description != "" {
		field.Child(fairy.Text(description).FontSize(12).LineHeight(16).Color(kit.T().MutedForeground).
			MarginEdges(4, 0, 0, 0))
	}
	return field
}

func (s *Shell) loadProxy() {
	net := &s.settings.network
	if net.proxyLoading {
		return
	}
	net.proxyLoading, net.proxyFailed = true, false
	go func() {
		proxy, err := s.deps.Settings.GetProxySettings(context.Background())
		if err != nil {
			_ = infra.ReportError(s.deps.Log, err, "Setting.GetProxySettings", infra.Diagnostic{
				Operation: "settings.proxy", Stage: "load",
			})
		}
		fairy.Go(func() {
			net.proxyLoading = false
			if err != nil {
				net.proxyFailed = true
				return
			}
			net.useProxy(proxy)
			net.draft.PasswordAction = "keep"
			if proxy.ConfigurationInvalid {
				net.draft.PasswordAction = "clear"
			}
		})
	}()
}

// useProxy shows saved proxy settings and starts a draft from them with an empty password.
func (net *networkSection) useProxy(proxy setting.ProxySettings) {
	net.proxy = &proxy
	net.draft = setting.ProxySettingsInput{
		Enabled:        proxy.Enabled,
		Type:           proxy.Type,
		Host:           proxy.Host,
		Port:           proxy.Port,
		Username:       proxy.Username,
		PasswordAction: "keep",
	}
	net.port = ""
	if proxy.Port != 0 {
		net.port = itoa(proxy.Port)
	}
}

func (s *Shell) proxySettings() *fairy.El {
	const prefix = "page.setting.network."
	t := kit.T()
	net := &s.settings.network
	switch {
	case net.proxyFailed:
		return settingsGroup(T(prefix+"title"), fairy.Row(
			fairy.Text(T(prefix+"loadFailed")).FontSize(14).LineHeight(20).Color(t.Destructive),
			kit.Button(T(prefix+"retry"), s.loadProxy, kit.Outline),
		).Gap(8).Align(fairy.Center).PadXY(0, 12))
	case net.proxy == nil:
		s.loadProxy()
		return settingsGroup(T(prefix+"title"), fairy.Text(T(prefix+"loading")).FontSize(14).LineHeight(20).
			Color(t.MutedForeground).PadXY(0, 12))
	}

	saved, draft := net.proxy, &net.draft
	form := fairy.Col().Gap(16).PadXY(0, 12)
	if saved.ConfigurationInvalid {
		form.Child(fairy.Text(T(prefix + "invalid")).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	if saved.RestartRequired {
		form.Child(fairy.Text(T(prefix + "restart")).FontSize(14).LineHeight(20).Color(t.MutedForeground))
	}
	form.Child(fairy.Text(T(prefix + "description")).FontSize(12).LineHeight(16).Color(t.MutedForeground))

	disabled := kit.Disabled(net.saving)
	types := []kit.Item{
		{Value: "http", Label: "HTTP"},
		{Value: "socks5", Label: "SOCKS5"},
		{Value: "socks5h", Label: "SOCKS5h"},
	}
	passwordPlaceholder := ""
	if saved.HasPassword && draft.PasswordAction == "keep" {
		passwordPlaceholder = T(prefix + "passwordSaved")
	}
	// Enter in any field saves, as the React form submitted.
	submit := func(string) { s.saveProxy() }
	passwordButtons := fairy.Row(kit.Button(T(prefix+"clearPassword"), func() {
		draft.Password, draft.PasswordAction = "", "clear"
	}, kit.Outline, disabled)).Gap(8)
	if saved.HasPassword {
		passwordButtons.Child(kit.Button(T(prefix+"keepPassword"), func() {
			draft.Password, draft.PasswordAction = "", "keep"
		}, kit.Outline, disabled))
	}
	password := fairy.Col(
		networkField(T(prefix+"password"), kit.TextField(draft.Password, func(value string) {
			draft.Password, draft.PasswordAction = value, "replace"
		}, kit.Placeholder(passwordPlaceholder), disabled).Password().OnSubmit(submit), ""),
		passwordButtons,
	).Gap(4)
	if draft.PasswordAction == "clear" {
		password.Child(fairy.Text(T(prefix + "passwordWillClear")).FontSize(12).LineHeight(16).Color(t.MutedForeground))
	}

	form.Child(
		fairy.Row(fairy.Text(T(prefix+"enabled")).FontSize(14).LineHeight(20).Grow(1),
			kit.Switch(draft.Enabled, func(on bool) { draft.Enabled = on }, disabled).Label(T(prefix+"enabled"))).
			Align(fairy.Center),
		networkField(T(prefix+"type"), kit.Select("settings.proxy.type", draft.Type, types,
			func(value string) { draft.Type = value }, disabled).AlignSelf(fairy.Start), T(prefix+"dns")),
		networkField(T(prefix+"host"), kit.TextField(draft.Host, func(value string) { draft.Host = value },
			disabled, kit.Invalid(net.proxyError == "host")).OnSubmit(submit), ""),
		networkField(T(prefix+"port"), kit.TextField(net.port, func(value string) {
			net.port = value
			draft.Port, _ = parseInt(value)
		}, disabled, kit.Invalid(net.proxyError == "port")).OnSubmit(submit), ""),
		networkField(T(prefix+"username"), kit.TextField(draft.Username, func(value string) { draft.Username = value },
			disabled, kit.Invalid(net.proxyError == "username")).OnSubmit(submit), ""),
		password,
	)
	if net.proxyError != "" {
		form.Child(fairy.Text(T(prefix + "errors." + net.proxyError)).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	label := T(prefix + "save")
	if net.saving {
		label = T(prefix + "saving")
	}
	form.Child(kit.Button(label, s.saveProxy, kit.Disabled(net.saving)).AlignSelf(fairy.Start))
	return settingsGroup(T(prefix+"title"), form)
}

// proxyDraftError is the first field of draft the server would refuse, checked before saving.
func proxyDraftError(draft setting.ProxySettingsInput, saved setting.ProxySettings, port string) string {
	if !draft.Enabled {
		return ""
	}
	host := strings.TrimSpace(draft.Host)
	if host == "" || proxyHostRejected.MatchString(host) {
		return "host"
	}
	if n, ok := parseInt(port); !ok || n < 1 || n > 65535 {
		return "port"
	}
	needsUsername := draft.PasswordAction == "keep" && saved.HasPassword
	if draft.PasswordAction == "replace" {
		needsUsername = draft.Password != ""
	}
	if draft.Username == "" && needsUsername {
		return "username"
	}
	return ""
}

func (s *Shell) saveProxy() {
	net := &s.settings.network
	if net.saving || net.proxy == nil {
		return
	}
	if code := proxyDraftError(net.draft, *net.proxy, net.port); code != "" {
		net.proxyError = code
		return
	}
	net.saving, net.proxyError = true, ""
	input := net.draft
	go func() {
		ctx := context.Background()
		err := s.deps.Settings.SetProxySettings(ctx, input)
		var proxy setting.ProxySettings
		if err == nil {
			proxy, err = s.deps.Settings.GetProxySettings(ctx)
		}
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Setting.SetProxySettings", infra.Diagnostic{
				Operation: "settings.proxy", Stage: "save",
			})
		}
		fairy.Go(func() {
			net.saving = false
			if err != nil {
				net.proxyError = "saveFailed"
				for _, field := range proxyErrorFields {
					if strings.Contains(err.Error(), "proxy."+field) {
						net.proxyError = field
						break
					}
				}
				return
			}
			net.useProxy(proxy)
			if proxy.RestartRequired {
				s.confirmRestart()
			}
		})
	}()
}

func (s *Shell) confirmRestart() {
	const prefix = "page.setting.network."
	s.Confirm(Confirmation{
		Title:        T(prefix + "restartDialog.title"),
		Description:  T(prefix + "restart"),
		ConfirmLabel: T(prefix + "restartDialog.action"),
		Failed:       T(prefix + "restartDialog.failed"),
		Run: func() error {
			err := s.deps.Restart()
			if err != nil {
				err = infra.ReportError(s.deps.Log, err, "Window.Restart", infra.Diagnostic{
					Operation: "settings.proxy", Stage: "restart",
				})
			}
			return err
		},
	})
}

func (s *Shell) loadGitHubToken() {
	net := &s.settings.network
	if net.githubLoading {
		return
	}
	net.githubLoading, net.githubFailed = true, false
	go func() {
		status, err := s.deps.Settings.GetGitHubTokenStatus(context.Background())
		if err != nil {
			_ = infra.ReportError(s.deps.Log, err, "Setting.GetGitHubTokenStatus", infra.Diagnostic{
				Operation: "settings.github-token", Stage: "load",
			})
		}
		fairy.Go(func() {
			net.githubLoading = false
			if err != nil {
				net.githubFailed = true
				return
			}
			net.github = &status
		})
	}()
}

func (s *Shell) githubTokenSettings() *fairy.El {
	const prefix = "page.setting.network.githubToken."
	t := kit.T()
	net := &s.settings.network
	if net.github == nil && !net.githubFailed {
		s.loadGitHubToken()
	}

	form := fairy.Col(fairy.RichText(
		fairy.Span{Text: T(prefix+"description") + " "},
		fairy.Span{Text: T(prefix + "create"), Underline: true, OnClick: func() { s.openExternal(githubTokenURL) }},
	).FontSize(12).LineHeight(16).Color(t.MutedForeground)).Gap(16).PadXY(0, 12)
	if net.githubFailed {
		form.Child(fairy.Row(
			fairy.Text(T("page.setting.network.loadFailed")).FontSize(14).LineHeight(20).Color(t.Destructive),
			kit.Button(T("page.setting.network.retry"), s.loadGitHubToken, kit.Outline),
		).Gap(8).Align(fairy.Center))
	}
	status := net.github
	if status != nil && status.Rejected {
		form.Child(fairy.Text(T(prefix + "rejected")).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	if status != nil && status.Unreadable {
		form.Child(fairy.Text(T(prefix + "unreadable")).FontSize(14).LineHeight(20).Color(t.Destructive))
	}

	configured := status != nil && status.Configured
	disabled := kit.Disabled(net.tokenSaving || status == nil)
	placeholder := ""
	if configured {
		placeholder = T(prefix + "savedPlaceholder")
	}
	form.Child(networkField(T(prefix+"label"), kit.TextField(net.token, func(value string) { net.token = value },
		kit.Placeholder(placeholder), disabled,
		kit.Invalid(net.tokenError == "invalid" || net.tokenError == "empty")).Password().
		OnSubmit(func(string) { s.saveGitHubToken("replace") }), ""))
	if net.tokenError != "" {
		form.Child(fairy.Text(T(prefix + "errors." + net.tokenError)).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	label := T("page.setting.network.save")
	if net.tokenSaving {
		label = T("page.setting.network.saving")
	}
	buttons := fairy.Row(kit.Button(label, func() { s.saveGitHubToken("replace") }, disabled)).Gap(8)
	if configured {
		buttons.Child(kit.Button(T(prefix+"remove"), func() { s.saveGitHubToken("clear") }, kit.Outline, disabled))
	}
	form.Child(buttons)
	return settingsGroup(T(prefix+"title"), form)
}

func (s *Shell) saveGitHubToken(action string) {
	net := &s.settings.network
	if net.tokenSaving || net.github == nil {
		return
	}
	if action == "replace" && strings.TrimSpace(net.token) == "" {
		net.tokenError = "empty"
		return
	}
	net.tokenSaving, net.tokenError = true, ""
	input := setting.GitHubTokenInput{Action: action}
	if action == "replace" {
		input.Token = net.token
	}
	go func() {
		limit, err := s.deps.Settings.SetGitHubToken(context.Background(), input)
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Setting.SetGitHubToken", infra.Diagnostic{
				Operation: "settings.github-token", Stage: action,
			})
		}
		fairy.Go(func() {
			net.tokenSaving = false
			if err != nil {
				net.tokenError = "saveFailed"
				for _, code := range []string{"invalid", "empty"} {
					if strings.Contains(err.Error(), "githubToken."+code) {
						net.tokenError = code
						break
					}
				}
				return
			}
			net.token = ""
			if action == "replace" {
				s.toasts.Show(kit.ToastSuccess, T("page.setting.network.githubToken.saved", "limit", limit), "")
			}
			net.github = nil
			s.loadGitHubToken()
		})
	}()
}
