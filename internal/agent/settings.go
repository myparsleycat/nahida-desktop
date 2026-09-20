package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	settingsKey = "agent_llm_settings"
	// legacyAPIKeyKey held the single API key before providers existed; it is migrated into the
	// provider credential map and removed.
	legacyAPIKeyKey  = "agent_llm_api_key"
	headerSecretsKey = "agent_llm_header_secrets"
)

// defaultAgentSettings seeds a fresh install with OpenAI and a catalog model.
func defaultAgentSettings() AgentSettingsView {
	settings := AgentSettingsView{
		Provider:          providerOpenAI,
		Protocol:          protocolOpenAIResponses,
		Endpoint:          "https://api.openai.com/v1",
		Model:             "gpt-5.2",
		ContextWindowSize: 131072,
		MaxOutputTokens:   16384,
		Reasoning:         "auto",
	}
	return applyCatalogDefaults(settings)
}

// applyCatalogDefaults fills the limits and image support a catalog model declares, keeping the
// stored numbers when the model is unknown.
func applyCatalogDefaults(settings AgentSettingsView) AgentSettingsView {
	model, ok := catalogModelFor(settings.Provider, settings.Model)
	if !ok {
		return settings
	}
	if model.ContextWindow > 0 {
		settings.ContextWindowSize = model.ContextWindow
	}
	if model.MaxOutputTokens > 0 {
		settings.MaxOutputTokens = model.MaxOutputTokens
	}
	settings.SupportsImages = model.SupportsImages
	return settings
}

func readSettings(ctx context.Context, client *db.Client, crypto *platform.Crypto) (AgentSettingsView, error) {
	settings := defaultAgentSettings()
	row, err := client.Settings.GetValue(ctx, settingsKey)
	if err != nil {
		return settings, err
	}
	if row != nil && *row != "" {
		if err := json.Unmarshal([]byte(*row), &settings); err != nil {
			return defaultAgentSettings(), fmt.Errorf("decode agent settings: %w", err)
		}
		// Settings stored before providers existed carry no provider key; keep them on the
		// provider their endpoint already points at instead of defaulting to OpenAI.
		var fields map[string]json.RawMessage
		if json.Unmarshal([]byte(*row), &fields) == nil {
			if _, stored := fields["provider"]; !stored {
				settings.Provider = providerFromLegacySettings(settings)
			}
		}
	}
	settings = normalizeSettings(settings)
	if err := applyHeaderStatus(ctx, client, crypto, &settings); err != nil {
		return settings, err
	}
	credentials, err := readCredentials(ctx, client, crypto)
	if err != nil {
		return settings, err
	}
	return applyCredential(settings, credentials[settings.Provider]), nil
}

// providerFromLegacySettings maps settings saved before providers existed onto a provider id.
func providerFromLegacySettings(settings AgentSettingsView) string {
	endpoint := strings.TrimRight(strings.TrimSpace(settings.Endpoint), "/")
	for _, spec := range providerSpecs {
		if strings.EqualFold(endpoint, spec.Endpoint) && settings.Protocol == spec.DefaultProtocol {
			return spec.ID
		}
	}
	return providerCustom
}

// normalizeSettings keeps stored settings consistent with the provider they select: a built-in
// provider always uses its own endpoint and the protocol its model needs.
func normalizeSettings(settings AgentSettingsView) AgentSettingsView {
	settings.Provider = strings.TrimSpace(settings.Provider)
	settings.Model = strings.TrimSpace(settings.Model)
	if spec, ok := providerSpecFor(settings.Provider); ok {
		settings.Endpoint = spec.Endpoint
		settings.Protocol = resolveProtocol(settings.Provider, settings.Model)
		return settings
	}
	settings.Provider = providerCustom
	settings.Protocol = strings.TrimSpace(settings.Protocol)
	settings.Endpoint = strings.TrimRight(strings.TrimSpace(settings.Endpoint), "/")
	return settings
}

// applyHeaderStatus marks each configured header, without ever copying a secret into the view.
func applyHeaderStatus(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
	settings *AgentSettingsView,
) error {
	secrets, err := readHeaderSecrets(ctx, client, crypto)
	if err != nil {
		return err
	}
	for index := range settings.Headers {
		header := &settings.Headers[index]
		if header.Secret {
			header.Configured = secrets[header.Name] != ""
			continue
		}
		header.Configured = header.Value != ""
	}
	return nil
}

func updateSettings(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
	input UpdateAgentSettingsInput,
) (AgentSettingsView, error) {
	if err := validateHeaders(input.Headers); err != nil {
		return AgentSettingsView{}, err
	}
	view, err := settingsFromInput(input)
	if err != nil {
		return AgentSettingsView{}, err
	}
	if err := validateSettings(view); err != nil {
		return AgentSettingsView{}, err
	}
	secrets, err := resolveHeaderSecrets(ctx, client, crypto, input.Headers)
	if err != nil {
		return AgentSettingsView{}, err
	}
	// The credential summary is derived from the credential store and never persisted here.
	view.Credential = AgentCredentialView{}
	encoded, err := json.Marshal(view)
	if err != nil {
		return AgentSettingsView{}, err
	}
	value := string(encoded)
	upserts := map[string]*string{settingsKey: &value}
	var deleteKeys []string
	if len(secrets) > 0 {
		secretData, err := json.Marshal(secrets)
		if err != nil {
			return AgentSettingsView{}, err
		}
		encrypted, err := crypto.EncryptString(string(secretData))
		if err != nil {
			return AgentSettingsView{}, fmt.Errorf("encrypt agent header secrets: %w", err)
		}
		upserts[headerSecretsKey] = &encrypted
	} else {
		deleteKeys = append(deleteKeys, headerSecretsKey)
	}
	if err := client.Settings.ApplyBatch(ctx, upserts, deleteKeys); err != nil {
		return AgentSettingsView{}, err
	}
	return readSettings(ctx, client, crypto)
}

// updateCredential stores or clears the API key of one provider.
func updateCredential(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
	input UpdateAgentCredentialInput,
) error {
	providerID := strings.TrimSpace(input.ProviderID)
	if providerID != providerCustom && !isBuiltinProvider(providerID) {
		return fmt.Errorf("unsupported agent provider %q", input.ProviderID)
	}
	credentials, err := readCredentials(ctx, client, crypto)
	if err != nil {
		return err
	}
	stored := credentials[providerID]
	switch input.Action {
	case "", "keep":
	case "replace":
		key := strings.TrimSpace(input.APIKey)
		if key == "" {
			return errors.New("API key is required")
		}
		credentials[providerID] = providerCredential{Type: credentialKindAPI, Key: key}
	case "clear":
		// Clearing drops only the API key; an account login stays connected.
		if stored.Type == credentialKindOAuth {
			return errors.New("provider is connected with an account")
		}
		delete(credentials, providerID)
	default:
		return fmt.Errorf("invalid API key action %q", input.Action)
	}
	return writeCredentials(ctx, client, crypto, credentials)
}

// migrateLegacyAPIKey moves the single API key stored before providers existed into the provider
// credential map and drops the old row. The caller must hold credentialMu.
func (s *Service) migrateLegacyAPIKey(ctx context.Context, client *db.Client) error {
	value, err := client.Settings.GetValue(ctx, legacyAPIKeyKey)
	if err != nil || value == nil || *value == "" {
		return err
	}
	key, err := s.crypto.DecryptString(*value)
	if err != nil {
		// A row this machine can no longer decrypt would fail every settings read, so drop it and
		// let the user enter the key again.
		_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "agent-settings", Stage: "migrate-api-key",
			Fields: map[string]any{"setting": legacyAPIKeyKey},
		})
		return client.Settings.ApplyBatch(ctx, nil, []string{legacyAPIKeyKey})
	}
	settings, err := readSettings(ctx, client, s.crypto)
	if err != nil {
		return err
	}
	credentials, err := readCredentials(ctx, client, s.crypto)
	if err != nil {
		return err
	}
	if key != "" {
		if _, exists := credentials[settings.Provider]; !exists {
			credentials[settings.Provider] = providerCredential{Type: credentialKindAPI, Key: key}
		}
	}
	if err := writeCredentials(ctx, client, s.crypto, credentials); err != nil {
		return err
	}
	return client.Settings.ApplyBatch(ctx, nil, []string{legacyAPIKeyKey})
}

// applyPlanModelDefault points the stored settings at a model the connected plan serves. Without
// it a fresh install keeps its default model after sign-in and the first turn fails on the Codex
// endpoint.
func applyPlanModelDefault(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
	providerID string,
) error {
	settings, err := readSettings(ctx, client, crypto)
	if err != nil {
		return err
	}
	if settings.Provider != providerID {
		return nil
	}
	model := planDefaultModel(providerID, settings.Model)
	if model == "" {
		return nil
	}
	settings.Model = model
	settings.Protocol = resolveProtocol(providerID, model)
	settings = applyCatalogDefaults(settings)
	settings.Credential = AgentCredentialView{}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	value := string(encoded)
	return client.Settings.ApplyBatch(ctx, map[string]*string{settingsKey: &value}, nil)
}

func settingsFromInput(input UpdateAgentSettingsInput) (AgentSettingsView, error) {
	providerID := strings.TrimSpace(input.Provider)
	if providerID != providerCustom && !isBuiltinProvider(providerID) {
		return AgentSettingsView{}, fmt.Errorf("unsupported agent provider %q", input.Provider)
	}
	view := AgentSettingsView{
		Provider:          providerID,
		Protocol:          strings.TrimSpace(input.Protocol),
		Endpoint:          strings.TrimRight(strings.TrimSpace(input.Endpoint), "/"),
		Model:             strings.TrimSpace(input.Model),
		ContextWindowSize: input.ContextWindowSize,
		MaxOutputTokens:   input.MaxOutputTokens,
		Reasoning:         input.Reasoning,
		SupportsImages:    input.SupportsImages,
		Headers:           headerViews(input.Headers),
	}
	return normalizeSettings(view), nil
}

func headerViews(headers []AgentHeaderInput) []AgentHeaderView {
	views := make([]AgentHeaderView, 0, len(headers))
	for _, header := range headers {
		name := strings.TrimSpace(header.Name)
		if header.Secret {
			views = append(views, AgentHeaderView{Name: name, Secret: true})
			continue
		}
		views = append(views, AgentHeaderView{Name: name, Value: header.Value})
	}
	return views
}

func readHeaderSecrets(ctx context.Context, client *db.Client, crypto *platform.Crypto) (map[string]string, error) {
	value, err := client.Settings.GetValue(ctx, headerSecretsKey)
	if err != nil || value == nil || *value == "" {
		return map[string]string{}, err
	}
	plain, err := crypto.DecryptString(*value)
	if err != nil {
		return nil, fmt.Errorf("decrypt agent header secrets: %w", err)
	}
	secrets := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &secrets); err != nil {
		return nil, fmt.Errorf("decode agent header secrets: %w", err)
	}
	return secrets, nil
}

// resolveHeaderSecrets rebuilds the stored secret values for the submitted headers, keeping a
// stored value unless the entry replaces or clears it.
func resolveHeaderSecrets(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
	headers []AgentHeaderInput,
) (map[string]string, error) {
	stored, err := readHeaderSecrets(ctx, client, crypto)
	if err != nil {
		return nil, err
	}
	secrets := map[string]string{}
	for _, header := range headers {
		if !header.Secret {
			continue
		}
		name := strings.TrimSpace(header.Name)
		switch header.SecretAction {
		case "", "keep":
			if stored[name] != "" {
				secrets[name] = stored[name]
			}
		case "replace":
			secrets[name] = header.Value
		case "clear":
		default:
			return nil, fmt.Errorf("invalid header secret action %q", header.SecretAction)
		}
	}
	return secrets, nil
}

// headerPlaceholder matches {{ ... }} references inside configured header values.
var headerPlaceholder = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)

// expandHeaderValue substitutes the runtime values a header value may reference. A reference may
// keep only the leading characters of the value, written as {{ session_id, 8 }} or
// {{ session_id(8) }}; characters are counted as runes. Unknown or malformed placeholders are
// kept as written so literal braces still pass through.
func expandHeaderValue(value, sessionID, turnID string) string {
	if !strings.Contains(value, "{{") {
		return value
	}
	return headerPlaceholder.ReplaceAllStringFunc(value, func(match string) string {
		name, limit, ok := parseHeaderPlaceholder(strings.Trim(match, "{}"))
		if !ok {
			return match
		}
		switch name {
		case "session_id":
			return leadingRunes(sessionID, limit)
		case "turn_id":
			return leadingRunes(turnID, limit)
		default:
			return match
		}
	})
}

// parseHeaderPlaceholder splits the body of a {{ ... }} reference into the variable name and an
// optional limit on how many leading characters it keeps, written as "session_id, 8" or
// "session_id(8)". The limit is negative when the reference sets none.
func parseHeaderPlaceholder(body string) (string, int, bool) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", 0, false
	}
	name, argument, limited := body, "", false
	if open := strings.IndexByte(body, '('); open >= 0 && strings.HasSuffix(body, ")") {
		name, limited = strings.TrimSpace(body[:open]), true
		argument = strings.TrimSpace(body[open+1 : len(body)-1])
	} else if comma := strings.IndexByte(body, ','); comma >= 0 {
		name, limited = strings.TrimSpace(body[:comma]), true
		argument = strings.TrimSpace(body[comma+1:])
	}
	if name == "" {
		return "", 0, false
	}
	if !limited {
		return name, -1, true
	}
	limit, err := strconv.Atoi(argument)
	if err != nil || limit < 0 {
		return "", 0, false
	}
	return name, limit, true
}

// leadingRunes keeps the first limit characters of value; a negative limit keeps all of them.
func leadingRunes(value string, limit int) string {
	if limit < 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

// headerValues resolves the configured headers for one provider request. Secret values come from
// storage, and both secret and plain values may reference the current conversation as
// {{ session_id }} and {{ turn_id }}; the connection test has no session or turn and expands both
// placeholders to empty values.
func headerValues(headers []AgentHeaderView, secrets map[string]string, sessionID, turnID string) map[string]string {
	values := make(map[string]string, len(headers))
	for _, header := range headers {
		value := header.Value
		if header.Secret {
			value = secrets[header.Name]
		}
		if value != "" {
			values[header.Name] = expandHeaderValue(value, sessionID, turnID)
		}
	}
	return values
}

// providerHeaders resolves the headers of one provider request: the headers the provider itself
// needs, plus the user's custom headers when the custom provider is active. A custom entry may
// override a provider default.
func providerHeaders(
	providerID string,
	headers []AgentHeaderView,
	secrets map[string]string,
	sessionID, turnID string,
) map[string]string {
	values := map[string]string{}
	if providerID == providerCustom {
		values = headerValues(headers, secrets, sessionID, turnID)
	}
	if spec, ok := providerSpecFor(providerID); ok {
		for _, header := range spec.Headers {
			if _, overridden := values[header.Name]; overridden {
				continue
			}
			if value := expandHeaderValue(header.Value, sessionID, turnID); value != "" {
				values[header.Name] = value
			}
		}
	}
	return values
}

// credentialFor returns the stored credential of one provider.
func credentialFor(
	ctx context.Context,
	client *db.Client,
	crypto *platform.Crypto,
	providerID string,
) (providerCredential, error) {
	credentials, err := readCredentials(ctx, client, crypto)
	if err != nil {
		return providerCredential{}, err
	}
	return credentials[providerID], nil
}

func validateSettings(settings AgentSettingsView) error {
	if settings.Provider != providerCustom && !isBuiltinProvider(settings.Provider) {
		return fmt.Errorf("unsupported agent provider %q", settings.Provider)
	}
	switch settings.Protocol {
	case protocolOpenAIResponses, protocolOpenAICompatible, protocolAnthropic:
	default:
		return fmt.Errorf("unsupported agent protocol %q", settings.Protocol)
	}
	endpoint, err := url.Parse(settings.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("endpoint must be an HTTP(S) URL without query or fragment")
	}
	if settings.Model == "" {
		return errors.New("model is required")
	}
	if settings.ContextWindowSize < 1024 || settings.ContextWindowSize > 4_000_000 {
		return errors.New("context window size must be between 1,024 and 4,000,000")
	}
	if settings.MaxOutputTokens < 1 || settings.MaxOutputTokens >= settings.ContextWindowSize {
		return errors.New("max output tokens must be positive and smaller than the context window")
	}
	switch settings.Reasoning {
	case "auto", "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("unsupported reasoning effort %q", settings.Reasoning)
	}
	if spec, builtin := providerSpecFor(settings.Provider); builtin {
		if settings.Endpoint != spec.Endpoint {
			return fmt.Errorf("provider %s uses %s", spec.ID, spec.Endpoint)
		}
		if expected := resolveProtocol(settings.Provider, settings.Model); expected != settings.Protocol {
			return fmt.Errorf("model %s uses protocol %s", settings.Model, expected)
		}
	}
	return nil
}

func validateHeaders(headers []AgentHeaderInput) error {
	seen := map[string]bool{}
	for _, header := range headers {
		name := strings.TrimSpace(header.Name)
		if !validHeaderName(name) {
			return fmt.Errorf("invalid header name %q", header.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("duplicate header %q", name)
		}
		seen[key] = true
		if invalidHeaderValue(header.Value) {
			return fmt.Errorf("header %q contains unsupported characters", name)
		}
		switch header.SecretAction {
		case "", "keep", "clear":
		case "replace":
			if header.Value == "" {
				return fmt.Errorf("header value is required for %s", name)
			}
		default:
			return fmt.Errorf("invalid header secret action %q", header.SecretAction)
		}
	}
	return nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", character):
		default:
			return false
		}
	}
	return true
}

func invalidHeaderValue(value string) bool {
	for _, character := range value {
		if (character < 0x20 && character != '\t') || character == 0x7f {
			return true
		}
	}
	return false
}
