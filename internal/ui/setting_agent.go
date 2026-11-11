package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/agent"
)

var agentReasoningOrder = []string{"auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"}

var agentProtocols = []kit.Item{
	{Value: "openai-responses", Label: "OpenAI Responses"},
	{Value: "openai-compatible", Label: "OpenAI-compatible"},
	{Value: "anthropic", Label: "Anthropic Messages"},
}

// agentSettingsSection is the agent settings form. settings is the draft the form edits; it is saved only by Save.
type agentSettingsSection struct {
	loading  bool
	loadErr  error
	settings *agent.AgentSettingsView
	catalog  *agent.AgentProviderCatalogView
	// customDraft remembers the custom provider's fields while another provider is selected.
	customDraft  *agent.AgentSettingsView
	apiKey       string
	apiKeyAction string
	authMode     string
	login        *agent.AgentLoginView
	test         *agent.AgentProviderTestView
	headers      []agentHeaderDraft
	servers      []agent.MCPServerView
	skills       []agent.SkillView
	mcp          *agent.MCPServerInput
	mcpArguments string
	mcpSaving    bool
	// mcpTesting holds the ids of the MCP servers whose connection test runs.
	mcpTesting map[string]bool
	busy       bool
	// loginCancel marks the pending account login as cancelled by the user.
	loginCancel context.CancelFunc
}

type agentHeaderDraft struct {
	name, value  string
	secret       bool
	secretAction string
	configured   bool
}

func agentHeaderDrafts(headers []agent.AgentHeaderView) []agentHeaderDraft {
	drafts := make([]agentHeaderDraft, 0, len(headers))
	for _, header := range headers {
		draft := agentHeaderDraft{
			name: header.Name, value: header.Value, secret: header.Secret, configured: header.Configured,
		}
		if header.Secret {
			draft.secretAction = "keep"
		}
		drafts = append(drafts, draft)
	}
	return drafts
}

func agentProviderOf(catalog *agent.AgentProviderCatalogView, id string) *agent.AgentProviderView {
	if catalog == nil {
		return nil
	}
	for i := range catalog.Providers {
		if catalog.Providers[i].ID == id {
			return &catalog.Providers[i]
		}
	}
	return nil
}

func agentModelOf(provider *agent.AgentProviderView, id string) *agent.CatalogModel {
	if provider == nil {
		return nil
	}
	for i := range provider.Models {
		if provider.Models[i].ID == id {
			return &provider.Models[i]
		}
	}
	return nil
}

// applyAgentModelDefaults fills the limits a catalog model declares so the user only picks a model id. The
// reasoning effort stays only while the new model declares it.
func applyAgentModelDefaults(settings agent.AgentSettingsView, model agent.CatalogModel) agent.AgentSettingsView {
	settings.Model, settings.Protocol = model.ID, model.Protocol
	settings.ContextWindowSize, settings.MaxOutputTokens = model.ContextWindow, model.MaxOutputTokens
	settings.SupportsImages = model.SupportsImages
	if !slices.Contains(model.Reasoning, settings.Reasoning) {
		settings.Reasoning = "auto"
	}
	return settings
}

func (s *Shell) loadAgentSettings() {
	sec := &s.settings.agent
	if sec.loading {
		return
	}
	sec.loading, sec.loadErr = true, nil
	type loaded struct {
		settings agent.AgentSettingsView
		catalog  agent.AgentProviderCatalogView
		servers  []agent.MCPServerView
		skills   []agent.SkillView
	}
	agentCall(s, "settings.load", nil, func(ctx context.Context) (loaded, error) {
		var out loaded
		var err error
		if out.settings, err = s.deps.Agent.GetSettings(ctx); err != nil {
			return out, err
		}
		if out.catalog, err = s.deps.Agent.ListProviders(ctx); err != nil {
			return out, err
		}
		if out.servers, err = s.deps.Agent.ListMCPServers(ctx); err != nil {
			return out, err
		}
		out.skills = s.deps.Agent.ListSkills()
		return out, nil
	}, func(out loaded, err error) {
		sec.loading = false
		if err != nil {
			sec.loadErr = err
			return
		}
		sec.settings, sec.catalog = &out.settings, &out.catalog
		sec.headers = agentHeaderDrafts(out.settings.Headers)
		sec.authMode = "key"
		if out.settings.Credential.Kind == "oauth" {
			sec.authMode = "account"
		}
		sec.apiKey, sec.apiKeyAction = "", "keep"
		sec.servers, sec.skills = out.servers, out.skills
	})
}

// applyStoredAgentSettings shows saved settings unless the user has meanwhile selected another provider.
func (sec *agentSettingsSection) applyStored(next agent.AgentSettingsView) {
	if sec.settings == nil || next.Provider != sec.settings.Provider {
		return
	}
	sec.settings = &next
	sec.headers = agentHeaderDrafts(next.Headers)
}

func (sec *agentSettingsSection) replaceProvider(provider agent.AgentProviderView) {
	if sec.catalog == nil {
		return
	}
	for i := range sec.catalog.Providers {
		if sec.catalog.Providers[i].ID == provider.ID {
			sec.catalog.Providers[i] = provider
		}
	}
}

// followPlanModels replaces a model that a plan login does not serve with the newest one it does.
func (sec *agentSettingsSection) followPlanModels() {
	if sec.settings == nil {
		return
	}
	provider := agentProviderOf(sec.catalog, sec.settings.Provider)
	if provider == nil || provider.Credential.Kind != "oauth" || len(provider.Models) == 0 ||
		agentModelOf(provider, sec.settings.Model) != nil {
		return
	}
	next := applyAgentModelDefaults(*sec.settings, provider.Models[0])
	sec.settings = &next
}

func (sec *agentSettingsSection) input() agent.UpdateAgentSettingsInput {
	st := sec.settings
	input := agent.UpdateAgentSettingsInput{
		Provider: st.Provider, Protocol: st.Protocol, Endpoint: st.Endpoint, Model: st.Model,
		ContextWindowSize: st.ContextWindowSize, MaxOutputTokens: st.MaxOutputTokens, Reasoning: st.Reasoning,
		SupportsImages: st.SupportsImages, AutoRunScripts: st.AutoRunScripts,
	}
	for _, header := range sec.headers {
		name := strings.TrimSpace(header.name)
		if name == "" {
			continue
		}
		h := agent.AgentHeaderInput{Name: name, Secret: header.secret}
		if !header.secret || header.secretAction == "replace" {
			h.Value = header.value
		}
		if header.secret {
			h.SecretAction = header.secretAction
		}
		input.Headers = append(input.Headers, h)
	}
	return input
}

// keyAction is the API key change the form shows. The account mode of a provider that supports it hides the
// key field, so a key typed before switching to it is not applied.
func (sec *agentSettingsSection) keyAction() (action, key string) {
	provider := agentProviderOf(sec.catalog, sec.settings.Provider)
	if provider != nil && provider.SupportsOAuth && sec.authMode == "account" {
		return "keep", ""
	}
	return sec.apiKeyAction, sec.apiKey
}

func (s *Shell) saveAgentSettings() {
	sec := &s.settings.agent
	if sec.settings == nil || sec.busy {
		return
	}
	input := sec.input()
	action, key := sec.keyAction()
	sec.busy = true
	type saved struct {
		provider *agent.AgentProviderView
		settings agent.AgentSettingsView
	}
	agentCall(s, "settings.save", map[string]any{"provider": input.Provider, "model": input.Model},
		func(ctx context.Context) (saved, error) {
			var out saved
			if _, err := s.deps.Agent.UpdateSettings(ctx, input); err != nil {
				return out, err
			}
			if action != "keep" {
				credential := agent.UpdateAgentCredentialInput{ProviderID: input.Provider, Action: action}
				if action == "replace" {
					credential.APIKey = key
				}
				provider, err := s.deps.Agent.UpdateProviderCredential(ctx, credential)
				if err != nil {
					return out, err
				}
				out.provider = &provider
			}
			var err error
			out.settings, err = s.deps.Agent.GetSettings(ctx)
			return out, err
		}, func(out saved, err error) {
			sec.busy = false
			s.refreshAgent()
			if err != nil {
				s.agentFailed(err)
				return
			}
			if out.provider != nil {
				sec.replaceProvider(*out.provider)
			}
			sec.applyStored(out.settings)
			sec.apiKey, sec.apiKeyAction = "", "keep"
			s.toasts.Show(kit.ToastSuccess, T("page.agent.settings_saved"), "")
		})
}

func (s *Shell) testAgentProvider() {
	sec := &s.settings.agent
	if sec.settings == nil || sec.busy {
		return
	}
	input := sec.input()
	if action, key := sec.keyAction(); action == "replace" {
		input.APIKey = key
	}
	sec.busy = true
	agentCall(s, "settings.test", map[string]any{"provider": input.Provider, "model": input.Model},
		func(ctx context.Context) (agent.AgentProviderTestView, error) {
			return s.deps.Agent.TestProvider(ctx, input)
		}, func(result agent.AgentProviderTestView, err error) {
			sec.busy, sec.test = false, nil
			if err != nil {
				s.agentFailed(err)
				return
			}
			sec.test = &result
		})
}

func (s *Shell) selectAgentProvider(id string) {
	sec := &s.settings.agent
	current := sec.settings
	if current == nil || id == current.Provider {
		return
	}
	sec.test, sec.apiKey, sec.apiKeyAction = nil, "", "keep"
	sec.headers = agentHeaderDrafts(current.Headers)
	if current.Provider == "custom" {
		draft := *current
		sec.customDraft = &draft
	}
	provider := agentProviderOf(sec.catalog, id)
	if provider == nil || provider.Custom {
		next := *current
		next.Provider = id
		if sec.customDraft != nil {
			next = *sec.customDraft
			next.AutoRunScripts = current.AutoRunScripts
		}
		sec.settings, sec.authMode = &next, "key"
		return
	}
	sec.authMode = "key"
	if provider.Credential.Kind == "oauth" {
		sec.authMode = "account"
	}
	next := *current
	next.Provider, next.Endpoint, next.Protocol = provider.ID, provider.Endpoint, provider.DefaultProtocol
	if len(provider.Models) > 0 {
		next = applyAgentModelDefaults(next, provider.Models[0])
	}
	sec.settings = &next
}

func (s *Shell) selectAgentModel(value string) {
	sec := &s.settings.agent
	if sec.settings == nil {
		return
	}
	next := *sec.settings
	if model := agentModelOf(agentProviderOf(sec.catalog, next.Provider), value); model != nil {
		next = applyAgentModelDefaults(next, *model)
	} else {
		next.Model = value
	}
	sec.settings = &next
}

func (s *Shell) connectAgentAccount() {
	sec := &s.settings.agent
	if sec.settings == nil || sec.busy {
		return
	}
	providerID := sec.settings.Provider
	sec.busy = true
	agentCall(s, "settings.login", map[string]any{"provider": providerID},
		func(ctx context.Context) (agent.AgentLoginView, error) {
			return s.deps.Agent.StartProviderLogin(ctx, providerID)
		}, func(login agent.AgentLoginView, err error) {
			if err != nil {
				sec.busy = false
				s.agentFailed(err)
				return
			}
			sec.login = &login
			cancelled, cancel := context.WithCancel(context.Background())
			sec.loginCancel = cancel
			agentCall(s, "settings.login-complete", map[string]any{"provider": providerID},
				func(ctx context.Context) (agent.AgentProviderCatalogView, error) {
					if _, err := s.deps.Agent.CompleteProviderLogin(ctx); err != nil {
						// The backend reports a cancelled login with a plain error.
						if cancelled.Err() != nil {
							return agent.AgentProviderCatalogView{}, cancelled.Err()
						}
						return agent.AgentProviderCatalogView{}, err
					}
					return s.deps.Agent.ListProviders(ctx)
				}, func(catalog agent.AgentProviderCatalogView, err error) {
					cancel()
					sec.busy, sec.login, sec.loginCancel = false, nil, nil
					s.refreshAgent()
					if err != nil {
						s.agentFailed(err)
						return
					}
					sec.catalog = &catalog
					sec.followPlanModels()
					sec.authMode = "account"
				})
		})
}

func (s *Shell) cancelAgentLogin() {
	if cancel := s.settings.agent.loginCancel; cancel != nil {
		cancel()
	}
	agentCall(s, "settings.login-cancel", nil, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.deps.Agent.CancelProviderLogin(ctx)
	}, func(_ struct{}, err error) { s.agentFailed(err) })
}

func (s *Shell) copyAgentLogin() {
	sec := &s.settings.agent
	if sec.login == nil {
		return
	}
	if s.win == nil || !s.win.SetClipboardText(sec.login.URL) {
		s.toasts.Show(kit.ToastError, T("page.agent.login_copy_failed"), "")
		return
	}
	s.toasts.Show(kit.ToastSuccess, T("page.agent.login_copied"), "")
}

func (s *Shell) signOutAgentProvider() {
	sec := &s.settings.agent
	if sec.settings == nil || sec.busy {
		return
	}
	providerID := sec.settings.Provider
	sec.busy = true
	type signedOut struct {
		provider agent.AgentProviderView
		settings agent.AgentSettingsView
	}
	agentCall(s, "settings.sign-out", map[string]any{"provider": providerID},
		func(ctx context.Context) (signedOut, error) {
			var out signedOut
			var err error
			if out.provider, err = s.deps.Agent.SignOutProvider(ctx, providerID); err != nil {
				return out, err
			}
			out.settings, err = s.deps.Agent.GetSettings(ctx)
			return out, err
		}, func(out signedOut, err error) {
			sec.busy = false
			s.refreshAgent()
			if err != nil {
				s.agentFailed(err)
				return
			}
			sec.replaceProvider(out.provider)
			sec.applyStored(out.settings)
		})
}

func (s *Shell) refreshAgentCatalog() {
	sec := &s.settings.agent
	if sec.busy {
		return
	}
	sec.busy = true
	agentCall(s, "settings.catalog-refresh", nil, func(ctx context.Context) (agent.AgentProviderCatalogView, error) {
		return s.deps.Agent.RefreshProviderCatalog(ctx)
	}, func(catalog agent.AgentProviderCatalogView, err error) {
		sec.busy = false
		if err != nil {
			s.agentFailed(err)
			return
		}
		sec.catalog = &catalog
		sec.followPlanModels()
		s.toasts.Show(kit.ToastSuccess, T("page.agent.catalog_refreshed"), "")
	})
}

func (s *Shell) editMCPServer(server agent.MCPServerView) {
	sec := &s.settings.agent
	input := agent.MCPServerInput{
		ID: server.ID, Name: server.Name, Transport: server.Transport, Executable: server.Executable,
		Arguments: server.Arguments, WorkingDirectory: server.WorkingDirectory, Endpoint: server.Endpoint,
		Enabled: server.Enabled,
	}
	for _, entry := range server.Entries {
		e := agent.MCPEntryInput{Name: entry.Name, Value: entry.Value, Secret: entry.Secret}
		if entry.Secret {
			e.SecretAction = "keep"
		}
		input.Entries = append(input.Entries, e)
	}
	sec.mcp, sec.mcpArguments = &input, strings.Join(server.Arguments, "\n")
}

func (s *Shell) saveMCPServer() {
	sec := &s.settings.agent
	if sec.mcp == nil || sec.mcpSaving {
		return
	}
	sec.mcpSaving = true
	draft := sec.mcp
	input := *draft
	input.Arguments = nil
	for line := range strings.SplitSeq(sec.mcpArguments, "\n") {
		if line = strings.TrimRight(line, "\r"); line != "" {
			input.Arguments = append(input.Arguments, line)
		}
	}
	agentCall(s, "settings.mcp-save", map[string]any{"server": input.Name, "transport": input.Transport},
		func(ctx context.Context) ([]agent.MCPServerView, error) {
			if _, err := s.deps.Agent.UpsertMCPServer(ctx, input); err != nil {
				return nil, err
			}
			return s.deps.Agent.ListMCPServers(ctx)
		}, func(servers []agent.MCPServerView, err error) {
			sec.mcpSaving = false
			if err != nil {
				s.agentFailed(err)
				return
			}
			if sec.mcp == draft {
				sec.mcp = nil
			}
			sec.servers = servers
		})
}

func (s *Shell) testMCPServer(id string) {
	sec := &s.settings.agent
	if sec.mcpTesting[id] {
		return
	}
	if sec.mcpTesting == nil {
		sec.mcpTesting = map[string]bool{}
	}
	sec.mcpTesting[id] = true
	agentCall(s, "settings.mcp-test", map[string]any{"serverId": id},
		func(ctx context.Context) (agent.MCPServerView, error) { return s.deps.Agent.TestMCPServer(ctx, id) },
		func(result agent.MCPServerView, err error) {
			delete(sec.mcpTesting, id)
			switch {
			case err != nil:
				s.agentFailed(err)
			case result.Error != "":
				s.toasts.Show(kit.ToastError, result.Error, "")
			default:
				s.toasts.Show(kit.ToastSuccess, T("page.agent.mcp_connected", "count", result.ToolCount), "")
			}
		})
}

func (s *Shell) deleteMCPServer(id string) {
	sec := &s.settings.agent
	agentCall(s, "settings.mcp-delete", map[string]any{"serverId": id},
		func(ctx context.Context) (struct{}, error) { return struct{}{}, s.deps.Agent.DeleteMCPServer(ctx, id) },
		func(_ struct{}, err error) {
			if err != nil {
				s.agentFailed(err)
				return
			}
			sec.servers = slices.DeleteFunc(sec.servers, func(server agent.MCPServerView) bool {
				return server.ID == id
			})
		})
}

func (s *Shell) reloadAgentSkills() {
	sec := &s.settings.agent
	go func() {
		skills := s.deps.Agent.ReloadSkills()
		fairy.Go(func() { sec.skills = skills })
	}()
}

func (s *Shell) openAgentSkillsFolder() {
	agentCall(s, "settings.skills-folder", nil,
		func(context.Context) (struct{}, error) { return struct{}{}, s.deps.Agent.OpenUserSkillsFolder() },
		func(_ struct{}, err error) { s.agentFailed(err) })
}

func (s *Shell) agentSettings() *fairy.El {
	t := kit.T()
	sec := &s.settings.agent
	switch {
	case sec.loadErr != nil:
		return settingsBody(fairy.Row(
			fairy.Text(T("shell.setting_load_failed")).Color(t.Destructive),
			kit.Button(T("page.setting.acc.retry"), s.loadAgentSettings, kit.Outline, kit.Small),
		).Gap(8).Align(fairy.Center))
	case sec.settings == nil:
		s.loadAgentSettings()
		return fairy.Col(kit.Spinner(20)).WFull().PadXY(0, 64).Align(fairy.Center)
	}
	return settingsBody(
		s.agentProviderSettings(),
		s.agentMCPSettings(),
		s.agentSkillSettings(),
	).PadEdges(16, 16, 48, 16)
}

func agentSettingsGroup(title, description string, rows ...*fairy.El) *fairy.El {
	t := kit.T()
	head := fairy.Col(
		fairy.Text(title).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
		fairy.Text(description).FontSize(12).LineHeight(16).Color(t.MutedForeground),
	).Gap(2).PadEdges(0, 4, 8, 4)
	body := fairy.Col(rows...).Gap(16).PadXY(0, 4)
	return fairy.Col(head, settingsGroup("", body)).Gap(4).FontSize(14)
}

// agentFieldLabel matches the React inline span, an 18px box 1px into the block whose line box puts the
// control 20px below the field top.
func agentFieldLabel(label string) *fairy.El {
	return fairy.Text(label).FontSize(12).LineHeight(16).Medium().MarginEdges(2, 0, 2, 0)
}

func agentField(label string, control *fairy.El) *fairy.El {
	// The hidden input or datalist after the control takes space-y-1.5's margin.
	return fairy.Col(agentFieldLabel(label), control.Label(label).MarginEdges(0, 0, 6, 0))
}

func agentCheck(label, hint string, checked bool, onChange func(bool)) *fairy.El {
	t := kit.T()
	row := fairy.Col(kit.Checkbox(checked, onChange, kit.Label(label)).FontSize(12).H(16)).Gap(4)
	if hint != "" {
		row.Child(fairy.Text(hint).FontSize(11).LineHeight(15.71).Color(t.MutedForeground))
	}
	return row
}

func (s *Shell) agentProviderSettings() *fairy.El {
	t := kit.T()
	sec := &s.settings.agent
	st := sec.settings
	active := agentProviderOf(sec.catalog, st.Provider)
	builtin := (active == nil || !active.Custom) && st.Provider != "custom"
	disabled := kit.Disabled(sec.busy)

	var providers []kit.Item
	if sec.catalog != nil {
		for _, provider := range sec.catalog.Providers {
			name := provider.Name
			if provider.Custom {
				name = T("page.agent.provider_custom")
			}
			if provider.Credential.Kind != "" && provider.Credential.Kind != "none" {
				name += " · " + T("page.agent.provider_connected")
			}
			providers = append(providers, kit.Item{Value: provider.ID, Label: name})
		}
	}
	rows := []*fairy.El{
		agentField(T("page.agent.provider_select"),
			kit.Select("settings.agent.provider", st.Provider, providers, s.selectAgentProvider).WFull()),
	}

	model := fairy.Box(kit.TextField(st.Model, s.selectAgentModel).WFull()).WFull()
	if builtin && active != nil && len(active.Models) > 0 {
		items := make([]kit.MenuItem, 0, len(active.Models))
		for _, m := range active.Models {
			id := m.ID
			label := m.Name
			if label == "" || label == id {
				label = id
			} else {
				label = id + " · " + label
			}
			items = append(items, kit.MenuItem{Label: label, Checked: id == st.Model, OnClick: func() {
				s.selectAgentModel(id)
			}})
		}
		// The React field was an input with a datalist, whose picker arrow shows inside the field only while
		// the pointer is over it.
		hovered := fairy.Local[bool]("settings.agent.model-hover")
		model.OnHover(func(h bool) { *hovered = h })
		picker := kit.IconButton(lucide.ChevronDown, nil, kit.Ghost, kit.XS, kit.Tooltip(T("page.agent.model")))
		if !*hovered {
			picker.Opacity(0)
		}
		model.Child(kit.DropdownMenu("settings.agent.models", picker, items).Absolute().Top(4).Right(4))
	}
	rows = append(rows, agentField(T("page.agent.model"), model))
	hint := fairy.Row(fairy.Text(T("page.agent.model_hint")).FontSize(11).LineHeight(15.71).Grow(1).MinW(0)).
		Gap(8).
		Align(fairy.Center).
		FontSize(11).
		Color(t.MutedForeground)
	if sec.catalog != nil && sec.catalog.Catalog.UpdatedAt != "" {
		date := sec.catalog.Catalog.UpdatedAt
		hint.Child(fairy.Row(
			fairy.Text(T("page.agent.catalog_updated", "date", date[:min(len(date), 10)])).
				FontSize(11).
				LineHeight(15.71),
			kit.Button("", s.refreshAgentCatalog, kit.Ghost, disabled, kit.Tooltip(T("page.agent.catalog_refresh"))).
				Child(fairy.Icon(lucide.RefreshCw).Size(12, 12)).W(32).PadEdges(0, 0, 0, 0).
				Label(T("page.agent.catalog_refresh")),
		).Gap(4).Align(fairy.Center))
	}
	rows = append(rows, hint)

	// A built-in provider derives the protocol, endpoint, limits and image support from the catalog model.
	if !builtin {
		number := func(value int, set func(int)) *fairy.El {
			return kit.TextField(itoa(value), func(text string) {
				n, _ := parseInt(strings.TrimSpace(text))
				set(n)
			})
		}
		rows = append(rows,
			fairy.Row(
				agentField(T("page.agent.protocol"), kit.Select("settings.agent.protocol", st.Protocol, agentProtocols,
					func(value string) { st.Protocol = value }).WFull()).Grow(1).Basis(0),
				agentField(T("page.agent.endpoint"),
					kit.TextField(st.Endpoint, func(value string) { st.Endpoint = value })).Grow(1).Basis(0),
			).Gap(12),
			agentCheck(T("page.agent.supports_images"), T("page.agent.supports_images_hint"), st.SupportsImages,
				func(on bool) { st.SupportsImages = on }),
			fairy.Row(
				agentField(T("page.agent.context_window"),
					number(st.ContextWindowSize, func(n int) { st.ContextWindowSize = n })).Grow(1).Basis(0),
				agentField(T("page.agent.max_output"),
					number(st.MaxOutputTokens, func(n int) { st.MaxOutputTokens = n })).Grow(1).Basis(0),
			).Gap(12),
		)
	}

	// A built-in model declares the efforts its endpoint accepts; the stored effort stays listed so the form
	// never hides the current value.
	selected := agentModelOf(active, st.Model)
	var efforts []kit.Item
	for _, value := range agentReasoningOrder {
		if !builtin || value == "auto" || value == st.Reasoning ||
			(selected != nil && slices.Contains(selected.Reasoning, value)) {
			efforts = append(efforts, kit.Item{Value: value, Label: value})
		}
	}
	if !builtin || len(efforts) > 1 {
		rows = append(rows, agentField(T("page.agent.reasoning"),
			kit.Select("settings.agent.reasoning", st.Reasoning, efforts, func(value string) { st.Reasoning = value }).
				WFull()))
	}
	rows = append(rows,
		agentCheck(T("page.agent.auto_run_scripts"), T("page.agent.auto_run_scripts_hint"), st.AutoRunScripts,
			func(on bool) { st.AutoRunScripts = on }),
		s.agentCredentialSettings(active),
	)
	if !builtin {
		rows = append(rows, s.agentHeaderSettings())
	}
	if sec.test != nil {
		rows = append(
			rows,
			fairy.Text(T("page.agent.test_result", "model", sec.test.Model, "latency", sec.test.Latency)).
				FontSize(11).LineHeight(15.71).Color(t.MutedForeground),
		)
	}
	rows = append(rows, fairy.Row(
		fairy.Spacer(),
		kit.Button(T("page.agent.test_connection"), s.testAgentProvider, kit.Outline, disabled),
		kit.Button(T("g.save"), s.saveAgentSettings, kit.Icon(lucide.Save), disabled),
	).Gap(8))
	return agentSettingsGroup(T("page.agent.provider"), T("page.agent.provider_hint"), rows...)
}

func (s *Shell) agentCredentialSettings(active *agent.AgentProviderView) *fairy.El {
	t := kit.T()
	sec := &s.settings.agent
	credential := sec.settings.Credential
	if active != nil {
		credential = active.Credential
	}
	disabled := kit.Disabled(sec.busy)
	body := fairy.Col().Gap(8)
	col := fairy.Col(agentFieldLabel(T("page.agent.api_key")), body)
	oauth := active != nil && active.SupportsOAuth
	if oauth {
		radios := fairy.Row().Gap(12).Role(fairy.RoleGroup).FocusGroup()
		for _, item := range []kit.Item{
			{Value: "account", Label: T("page.agent.auth_account")},
			{Value: "key", Label: T("page.agent.auth_key")},
		} {
			selectMode := func() { sec.authMode = item.Value }
			radios.Child(fairy.Row(
				kit.Radio(sec.authMode == item.Value, selectMode).Label(item.Label),
				fairy.Text(item.Label).FontSize(12).LineHeight(16),
			).Gap(6).Align(fairy.Center).H(16).OnClick(selectMode))
		}
		body.Child(radios)
	}
	if oauth && sec.authMode == "account" {
		if credential.Kind == "oauth" {
			label := credential.AccountLabel
			if label == "" {
				label = T("page.agent.provider_connected")
			}
			body.Child(fairy.Row(
				fairy.Text(label).FontSize(12).LineHeight(16).NoWrap().Ellipsis().Grow(1).MinW(0),
				kit.Button(
					T("page.agent.sign_out"),
					s.signOutAgentProvider,
					kit.Ghost,
					kit.Small,
					kit.Icon(lucide.LogOut),
					disabled,
				),
			).Gap(8).Align(fairy.Center).Pad(8).Radius(t.RadiusMd()).Fill(t.Muted.Alpha(0.5)).FontSize(12))
			return col
		}
		body.Child(kit.Button(T("page.agent.connect_chatgpt"), s.connectAgentAccount, kit.Outline,
			kit.Disabled(sec.busy || sec.login != nil)).AlignSelf(fairy.Start))
		if sec.login != nil {
			body.Child(fairy.Col(
				fairy.Text(T("page.agent.login_waiting")),
				fairy.Row(
					kit.Button(T("page.agent.login_copy_link"), s.copyAgentLogin, kit.Ghost, kit.Small),
					kit.Button(T("page.agent.login_cancel"), s.cancelAgentLogin, kit.Ghost, kit.Small),
				).Gap(4),
			).Gap(4).FontSize(11).LineHeight(15.71).Color(t.MutedForeground))
		}
		return col
	}

	keep := T("page.agent.keep")
	if credential.Kind != "" && credential.Kind != "none" {
		keep = T("page.agent.secret_configured")
	}
	actions := []kit.Item{{Value: "keep", Label: keep}, {Value: "replace", Label: T("page.agent.replace")}}
	if credential.Kind != "oauth" {
		actions = append(actions, kit.Item{Value: "clear", Label: T("page.agent.clear")})
	}
	body.Child(kit.Select("settings.agent.key-action", sec.apiKeyAction, actions,
		func(value string) { sec.apiKeyAction = value }).WFull().Label(T("page.agent.api_key")).MarginEdges(0, 0, 8, 0))
	if sec.apiKeyAction == "replace" {
		body.Child(kit.TextField(sec.apiKey, func(value string) { sec.apiKey = value }).Password().
			Key("settings.agent.api-key").Label(T("page.agent.api_key_value")))
	}
	return col
}

func (s *Shell) agentHeaderSettings() *fairy.El {
	t := kit.T()
	sec := &s.settings.agent
	col := fairy.Col(
		fairy.Text(T("page.agent.custom_headers")).FontSize(12).LineHeight(16).Medium(),
		fairy.Text(T("page.agent.custom_headers_hint")).FontSize(11).LineHeight(15.71).Color(t.MutedForeground),
	).Gap(6)
	for i := range sec.headers {
		header := &sec.headers[i]
		placeholder := T("page.agent.header_value")
		if header.secret && header.configured && header.secretAction == "keep" {
			placeholder = T("page.agent.configured")
		}
		value := kit.TextField(header.value, func(text string) {
			header.value = text
			if header.secret {
				header.secretAction = "replace"
			}
		}, kit.Placeholder(placeholder))
		if header.secret {
			value.Password()
		}
		index := i
		col.Child(fairy.Row(
			kit.TextField(header.name, func(text string) { header.name = text },
				kit.Placeholder(T("page.agent.header_name"))).Grow(1).Basis(0).MinW(0),
			value.Grow(1).Basis(0).MinW(0),
			kit.Checkbox(header.secret, func(on bool) {
				header.secret, header.value = on, ""
				header.secretAction = ""
				if on {
					header.secretAction = "keep"
				}
			}, kit.Label(T("page.agent.secret"))).FontSize(12),
			kit.IconButton(lucide.Trash, func() {
				if index < len(sec.headers) {
					sec.headers = slices.Delete(sec.headers, index, index+1)
				}
			}, kit.Ghost, kit.Tooltip(T("g.delete"))),
		).Gap(8).Align(fairy.Center))
	}
	return col.Child(kit.Button(T("page.agent.add_header"), func() {
		sec.headers = append(sec.headers, agentHeaderDraft{})
	}, kit.Ghost, kit.Small, kit.Icon(lucide.Plus)).AlignSelf(fairy.Start))
}

func (s *Shell) agentMCPSettings() *fairy.El {
	t := kit.T()
	sec := &s.settings.agent
	list := fairy.Col().Gap(2)
	for _, server := range sec.servers {
		status := T("page.agent.disabled")
		if server.Enabled {
			status = T("page.agent.enabled")
		}
		list.Child(fairy.Row(
			fairy.Icon(lucide.Server).Size(16, 16).Color(t.MutedForeground).Shrink(0),
			fairy.Col(
				fairy.Text(server.Name).FontSize(14).LineHeight(20).Medium().NoWrap().Ellipsis(),
				fairy.Text(server.Transport+" · "+status).FontSize(11).LineHeight(15.71).Color(t.MutedForeground),
			).Grow(1).MinW(0).Cursor(fairy.CursorPointer).Label(server.Name).OnClick(func() { s.editMCPServer(server) }),
			kit.Button(T("page.agent.test_connection"), func() { s.testMCPServer(server.ID) }, kit.Ghost, kit.Small,
				kit.Loading(sec.mcpTesting[server.ID])),
			kit.IconButton(lucide.Trash, func() { s.deleteMCPServer(server.ID) }, kit.Ghost,
				kit.Tooltip(T("g.delete"))),
		).Gap(8).Align(fairy.Center).PadXY(8, 6).Radius(t.RadiusMd()).HoverFill(hoverFill(t)))
	}
	list.Child(kit.Button(T("page.agent.add_server"), func() {
		sec.mcp, sec.mcpArguments = &agent.MCPServerInput{Transport: "stdio"}, ""
	}, kit.Outline, kit.Icon(lucide.Plus)).AlignSelf(fairy.Start).MarginEdges(8, 0, 0, 0))
	rows := []*fairy.El{list}
	if sec.mcp != nil {
		rows = append(rows, s.mcpEditor(sec.mcp))
	}
	return agentSettingsGroup("MCP", T("page.agent.mcp_warning"), rows...)
}

func (s *Shell) mcpEditor(draft *agent.MCPServerInput) *fairy.El {
	t := kit.T()
	sec := &s.settings.agent
	transports := []kit.Item{
		{Value: "stdio", Label: "stdio"},
		{Value: "streamable-http", Label: "Streamable HTTP"},
		{Value: "blender", Label: T("page.agent.transport_blender")},
	}
	editor := fairy.Col(fairy.Row(
		kit.TextField(draft.Name, func(value string) { draft.Name = value },
			kit.Placeholder(T("page.agent.mcp_name"))).Grow(1).Basis(0).MinW(0),
		kit.Select("settings.agent.mcp-transport", draft.Transport, transports,
			func(value string) { draft.Transport = value }).Grow(1).Basis(0).MinW(0).
			Label(T("page.agent.mcp_transport")),
	).Gap(8)).Gap(12).Pad(12).Radius(t.RadiusMd()).Fill(t.Muted.Alpha(0.5))

	switch draft.Transport {
	case "stdio":
		editor.Child(
			kit.TextField(draft.Executable, func(value string) { draft.Executable = value },
				kit.Placeholder(T("page.agent.mcp_executable"))),
			kit.TextArea(sec.mcpArguments, func(value string) { sec.mcpArguments = value },
				kit.Placeholder(T("page.agent.mcp_arguments"))).MinH(60),
			kit.TextField(draft.WorkingDirectory, func(value string) { draft.WorkingDirectory = value },
				kit.Placeholder(T("page.agent.mcp_working_directory"))),
		)
	case "blender":
		editor.Child(fairy.Text(T("page.agent.blender_hint")).FontSize(11).LineHeight(15.71).Color(t.MutedForeground))
	default:
		editor.Child(kit.TextField(draft.Endpoint, func(value string) { draft.Endpoint = value },
			kit.Placeholder("https://…")))
	}

	entries := fairy.Col().Gap(8)
	for i := range draft.Entries {
		entry := &draft.Entries[i]
		placeholder := T("page.agent.header_value")
		if entry.Secret {
			placeholder = T("page.agent.mcp_secret_value")
		}
		value := kit.TextField(entry.Value, func(text string) {
			entry.Value = text
			if entry.Secret {
				entry.SecretAction = "replace"
			}
		}, kit.Placeholder(placeholder))
		if entry.Secret {
			value.Password()
		}
		index := i
		entries.Child(fairy.Row(
			kit.TextField(entry.Name, func(text string) { entry.Name = text },
				kit.Placeholder(T("page.agent.mcp_name"))).Grow(1).Basis(0).MinW(0),
			value.Grow(1).Basis(0).MinW(0),
			kit.Checkbox(entry.Secret, func(on bool) {
				entry.Secret, entry.SecretAction = on, ""
				// A value typed before marking the entry secret is saved as the secret, not dropped.
				if on && entry.Value != "" {
					entry.SecretAction = "replace"
				} else if on {
					entry.SecretAction = "keep"
				}
			}, kit.Label(T("page.agent.secret"))).FontSize(12),
			kit.IconButton(lucide.Trash, func() {
				if index < len(draft.Entries) {
					draft.Entries = slices.Delete(draft.Entries, index, index+1)
				}
			}, kit.Ghost, kit.Tooltip(T("g.delete"))),
		).Gap(8).Align(fairy.Center))
	}
	entries.Child(kit.Button(T("page.agent.add_entry"), func() {
		draft.Entries = append(draft.Entries, agent.MCPEntryInput{})
	}, kit.Ghost, kit.Small, kit.Icon(lucide.Plus)).AlignSelf(fairy.Start))
	return editor.Child(
		entries,
		settingRow(T("page.agent.enabled"), "",
			kit.Switch(draft.Enabled, func(on bool) { draft.Enabled = on }).Label(T("page.agent.enabled"))).
			PadXY(0, 0),
		fairy.Row(
			fairy.Spacer(),
			kit.Button(T("g.cancel"), func() { sec.mcp = nil }, kit.Ghost),
			kit.Button(T("g.save"), s.saveMCPServer, kit.Disabled(sec.mcpSaving), kit.Loading(sec.mcpSaving)),
		).Gap(8),
	)
}

func (s *Shell) agentSkillSettings() *fairy.El {
	t := kit.T()
	sec := &s.settings.agent
	list := fairy.Col().Gap(12)
	for _, skill := range sec.skills {
		description := skill.Description
		if skill.Error != "" {
			description = skill.Error
		}
		list.Child(fairy.Col(
			fairy.Row(
				fairy.Text(skill.Name).FontSize(14).LineHeight(20).Medium().Grow(1).MinW(0),
				fairy.Text(skill.Source).
					FontSize(10).
					LineHeight(15).
					Color(t.MutedForeground),
			).Gap(8).Align(fairy.Center),
			fairy.Text(description).FontSize(12).LineHeight(16).Color(t.MutedForeground),
		).Gap(4))
	}
	return agentSettingsGroup(T("page.agent.skills"), T("page.agent.skills_hint"), list, fairy.Row(
		kit.Button(T("page.agent.reload"), s.reloadAgentSkills, kit.Outline, kit.Icon(lucide.RefreshCw)),
		kit.Button(T("page.agent.open_skills_folder"), s.openAgentSkillsFolder, kit.Outline,
			kit.Icon(lucide.FolderOpen)),
	).Gap(8))
}
