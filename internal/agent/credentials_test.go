package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSettingsDefaultToOpenAIWithCatalogModel(t *testing.T) {
	t.Parallel()
	service := newSettingsService(t, Options{})
	settings, err := service.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != providerOpenAI || settings.Protocol != protocolOpenAIResponses {
		t.Fatalf("settings = %#v", settings)
	}
	if model, ok := catalogModelFor(providerOpenAI, settings.Model); !ok {
		t.Fatalf("default model %q is not in the catalog", settings.Model)
	} else if settings.ContextWindowSize != model.ContextWindow || settings.SupportsImages != model.SupportsImages {
		t.Fatalf("settings = %#v, catalog model = %#v", settings, model)
	}
	if settings.Credential.Kind != "none" {
		t.Fatalf("credential = %#v", settings.Credential)
	}
}

func TestUpdateSettingsForcesBuiltinProviderRouting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})

	zen := catalogModelsFor(providerZen)
	if len(zen) == 0 {
		t.Fatal("zen catalog is empty")
	}
	anthropicModel := CatalogModel{}
	for _, model := range zen {
		if model.Protocol == protocolAnthropic {
			anthropicModel = model
			break
		}
	}
	if anthropicModel.ID == "" {
		t.Fatal("zen catalog has no anthropic-routed model")
	}

	saved, err := service.UpdateSettings(ctx, UpdateAgentSettingsInput{
		Provider: providerZen, Protocol: protocolOpenAIResponses, Endpoint: "https://example.com/v1",
		Model: anthropicModel.ID, ContextWindowSize: 131072, MaxOutputTokens: 4096, Reasoning: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Endpoint != "https://opencode.ai/zen/v1" || saved.Protocol != protocolAnthropic {
		t.Fatalf("settings = %#v", saved)
	}

	if _, err := service.UpdateSettings(ctx, UpdateAgentSettingsInput{
		Provider: "unknown-provider", Protocol: protocolOpenAICompatible, Endpoint: "https://example.com/v1",
		Model: "test-model", ContextWindowSize: 131072, MaxOutputTokens: 4096, Reasoning: "auto",
	}); err == nil {
		t.Fatal("unsupported provider was accepted")
	}
}

func TestSettingsMigrateLegacyAPIKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}

	// A settings blob written before providers existed has no provider key at all.
	legacy := `{
	  "protocol": "openai-responses", "endpoint": "https://api.openai.com/v1", "model": "gpt-5.2",
	  "contextWindowSize": 400000, "maxOutputTokens": 128000, "reasoning": "auto"
	}`
	encrypted, err := service.crypto.EncryptString("sk-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Settings.ApplyBatch(ctx, map[string]*string{
		settingsKey: &legacy, legacyAPIKeyKey: &encrypted,
	}, nil); err != nil {
		t.Fatal(err)
	}

	settings, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != providerOpenAI {
		t.Fatalf("provider = %q", settings.Provider)
	}
	if settings.Credential.Kind != credentialKindAPI {
		t.Fatalf("credential = %#v", settings.Credential)
	}
	credential, err := credentialFor(ctx, client, service.crypto, providerOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Key != "sk-legacy" {
		t.Fatalf("credential = %#v", credential)
	}
	if row, err := client.Settings.GetValue(ctx, legacyAPIKeyKey); err != nil || row != nil {
		t.Fatalf("legacy key row = %v, %v", row, err)
	}
	if row, err := client.Settings.GetValue(ctx, credentialsKey); err != nil || row == nil || *row == "" {
		t.Fatalf("credential row = %v, %v", row, err)
	}
}

func TestSettingsDropUndecryptableLegacyAPIKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	broken := "not-a-dpapi-blob"
	if err := client.Settings.ApplyBatch(ctx, map[string]*string{legacyAPIKeyKey: &broken}, nil); err != nil {
		t.Fatal(err)
	}

	settings, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider == "" {
		t.Fatalf("settings = %#v", settings)
	}
	if row, err := client.Settings.GetValue(ctx, legacyAPIKeyKey); err != nil || row != nil {
		t.Fatalf("legacy key row = %v, %v", row, err)
	}
}

func TestSettingsKeepCustomEndpointAfterMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}

	legacy := `{
	  "protocol": "openai-compatible", "endpoint": "https://example.com/v1", "model": "local-model",
	  "contextWindowSize": 32768, "maxOutputTokens": 4096, "reasoning": "auto"
	}`
	encrypted, err := service.crypto.EncryptString("local-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Settings.ApplyBatch(ctx, map[string]*string{
		settingsKey: &legacy, legacyAPIKeyKey: &encrypted,
	}, nil); err != nil {
		t.Fatal(err)
	}

	settings, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != providerCustom || settings.Endpoint != "https://example.com/v1" {
		t.Fatalf("settings = %#v", settings)
	}
	if settings.Credential.Kind != credentialKindAPI {
		t.Fatalf("credential = %#v", settings.Credential)
	}
}

func TestUpdateProviderCredentialActions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}

	saved, err := service.UpdateProviderCredential(ctx, UpdateAgentCredentialInput{
		ProviderID: providerAnthropic, Action: "replace", APIKey: "sk-ant",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != providerAnthropic || saved.Credential.Kind != credentialKindAPI {
		t.Fatalf("provider = %#v", saved)
	}

	stored, err := client.Settings.GetValue(ctx, credentialsKey)
	if err != nil || stored == nil {
		t.Fatalf("stored credentials = %v, %v", stored, err)
	}
	if *stored == "" || contains([]string{*stored}, "sk-ant") {
		t.Fatalf("stored credentials leaked the key: %q", *stored)
	}

	if _, err := service.UpdateProviderCredential(ctx, UpdateAgentCredentialInput{
		ProviderID: providerAnthropic, Action: "keep",
	}); err != nil {
		t.Fatal(err)
	}
	if credential, err := credentialFor(ctx, client, service.crypto, providerAnthropic); err != nil ||
		credential.Key != "sk-ant" {
		t.Fatalf("credential = %#v, %v", credential, err)
	}

	if _, err := service.UpdateProviderCredential(ctx, UpdateAgentCredentialInput{
		ProviderID: providerAnthropic, Action: "replace",
	}); err == nil {
		t.Fatal("replace without a key was accepted")
	}

	cleared, err := service.UpdateProviderCredential(ctx, UpdateAgentCredentialInput{
		ProviderID: providerAnthropic, Action: "clear",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Credential.Kind != "none" {
		t.Fatalf("credential = %#v", cleared.Credential)
	}
	if _, err := service.UpdateProviderCredential(ctx, UpdateAgentCredentialInput{
		ProviderID: "unknown-provider", Action: "replace", APIKey: "sk",
	}); err == nil {
		t.Fatal("unknown provider credential was accepted")
	}

	// The custom provider keeps the single API key it had before providers existed.
	custom, err := service.UpdateProviderCredential(ctx, UpdateAgentCredentialInput{
		ProviderID: providerCustom, Action: "replace", APIKey: "custom-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if custom.Credential.Kind != credentialKindAPI {
		t.Fatalf("custom provider = %#v", custom)
	}
}

func TestUpdateProviderCredentialKeepsOAuthLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{
		providerOpenAI: {
			Type: credentialKindOAuth, Access: "access", Refresh: "refresh",
			Expires: 4_102_444_800_000, AccountID: "acc-1", Email: "user@example.com",
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.UpdateProviderCredential(ctx, UpdateAgentCredentialInput{
		ProviderID: providerOpenAI, Action: "clear",
	}); err == nil {
		t.Fatal("clearing an account login was accepted")
	}

	settings, err := service.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Credential.Kind != credentialKindOAuth || settings.Credential.AccountLabel != "user@example.com" {
		t.Fatalf("credential = %#v", settings.Credential)
	}
}

func TestListProvidersHidesChatGPTPlanModelsForAPIKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}

	catalogView, err := service.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if catalogView.Catalog.UpdatedAt == "" {
		t.Fatal("catalog status is empty")
	}
	providers := catalogView.Providers
	if len(providers) != len(providerSpecs)+1 {
		t.Fatalf("providers = %d", len(providers))
	}
	if last := providers[len(providers)-1]; !last.Custom || last.ID != providerCustom {
		t.Fatalf("custom provider = %#v", last)
	}
	for _, provider := range providers {
		if provider.Custom {
			continue
		}
		if provider.ID == providerOpenAI {
			if !provider.SupportsOAuth || provider.Credential.Kind != "none" {
				t.Fatalf("openai provider = %#v", provider)
			}
			// Without a ChatGPT login the plan-only filter does not apply, so every shipped
			// model stays selectable.
			if len(provider.Models) != len(catalogModelsFor(providerOpenAI)) {
				t.Fatalf("openai models = %d", len(provider.Models))
			}
			continue
		}
		if provider.SupportsOAuth {
			t.Fatalf("provider %s unexpectedly supports OAuth", provider.ID)
		}
		if len(provider.Models) == 0 {
			t.Fatalf("provider %s has no models", provider.ID)
		}
	}

	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{
		providerOpenAI: {Type: credentialKindOAuth, Access: "access", Refresh: "refresh", Expires: 4_102_444_800_000},
	}); err != nil {
		t.Fatal(err)
	}
	catalogView, err = service.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range catalogView.Providers {
		if provider.ID != providerOpenAI {
			continue
		}
		visible := make(map[string]bool, len(provider.Models))
		for _, model := range provider.Models {
			if !model.OAuth {
				t.Fatalf("plan model %s stayed visible with a ChatGPT login", model.ID)
			}
			visible[model.ID] = true
		}
		// The newest frontier models declare a pro request mode, which must not hide them.
		for _, wanted := range []string{"gpt-6-astra", "gpt-5.6-luna"} {
			if !visible[wanted] {
				t.Fatalf("%s is missing from the ChatGPT plan list", wanted)
			}
		}
	}
}

func TestCredentialStoreRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}

	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{}); err != nil {
		t.Fatal(err)
	}
	if row, err := client.Settings.GetValue(ctx, credentialsKey); err != nil || row != nil {
		t.Fatalf("empty credential row = %v, %v", row, err)
	}

	want := map[string]providerCredential{
		providerGo: {Type: credentialKindAPI, Key: "go-key"},
	}
	if err := writeCredentials(ctx, client, service.crypto, want); err != nil {
		t.Fatal(err)
	}
	got, err := readCredentials(ctx, client, service.crypto)
	if err != nil {
		t.Fatal(err)
	}
	if got[providerGo].Key != "go-key" {
		t.Fatalf("credentials = %#v", got)
	}
}

func TestNeedsRefreshUsesMargin(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	if (providerCredential{Type: credentialKindAPI}).needsRefresh(now) {
		t.Fatal("api key credential needs no refresh")
	}
	if !(providerCredential{Type: credentialKindOAuth}).needsRefresh(now) {
		t.Fatal("missing access token needs refresh")
	}
	valid := providerCredential{Type: credentialKindOAuth, Access: "access", Expires: now.Add(time.Hour).UnixMilli()}
	if valid.needsRefresh(now) {
		t.Fatal("valid token needs no refresh")
	}
	expiring := providerCredential{
		Type:    credentialKindOAuth,
		Access:  "access",
		Expires: now.Add(time.Minute).UnixMilli(),
	}
	if !expiring.needsRefresh(now) {
		t.Fatal("expiring token needs refresh")
	}
}

func TestCredentialRowIsEncryptedAtRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{
		providerAnthropic: {Type: credentialKindAPI, Key: "secret-key"},
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := client.Settings.GetValue(ctx, credentialsKey)
	if err != nil || raw == nil {
		t.Fatalf("credential row = %v, %v", raw, err)
	}
	if strings.Contains(*raw, "secret-key") {
		t.Fatal("credential row stores the key in clear text")
	}
	credentials, err := readCredentials(ctx, client, service.crypto)
	if err != nil {
		t.Fatal(err)
	}
	if credentials[providerAnthropic].Key != "secret-key" {
		t.Fatalf("credentials = %#v", credentials)
	}
}
