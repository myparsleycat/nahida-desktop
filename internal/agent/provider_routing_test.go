package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// codexTestToken builds an unsigned JWT payload carrying account claims.
func codexTestToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestAdapterRoutesChatGPTLoginThroughCodex(t *testing.T) {
	t.Parallel()
	type capturedRequest struct {
		path   string
		header http.Header
		body   map[string]any
	}
	requests := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		requests <- capturedRequest{path: request.URL.Path, header: request.Header.Clone(), body: body}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	adapter := newModelAdapter(modelAdapterConfig{
		settings: AgentSettingsView{
			Provider: providerOpenAI, Protocol: protocolOpenAIResponses,
			Endpoint: "https://api.openai.com/v1", Model: "gpt-5.5",
		},
		credential: providerCredential{
			Type: credentialKindOAuth, Access: "access-token", Refresh: "refresh-token",
			Expires: time.Now().Add(time.Hour).UnixMilli(), AccountID: "acc-1",
		},
		sessionID:     "session-1",
		oauthEndpoint: server.URL,
	}, server.Client())
	if _, err := adapter.Complete(context.Background(), ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 1024,
	}, func(string, string) {}); err != nil {
		t.Fatal(err)
	}

	captured := <-requests
	if captured.path != "/responses" {
		t.Fatalf("request path = %q", captured.path)
	}
	if value := captured.header.Get("Authorization"); value != "Bearer access-token" {
		t.Fatalf("authorization = %q", value)
	}
	if value := captured.header.Get("ChatGPT-Account-Id"); value != "acc-1" {
		t.Fatalf("account header = %q", value)
	}
	if value := captured.header.Get("originator"); value != codexOriginator {
		t.Fatalf("originator = %q", value)
	}
	if value := captured.header.Get("session-id"); value != "session-1" {
		t.Fatalf("session header = %q", value)
	}
	if _, present := captured.body["max_output_tokens"]; present {
		t.Fatalf("codex request kept the output limit: %#v", captured.body)
	}
	if captured.body["store"] != false {
		t.Fatalf("codex request is not stateless: %#v", captured.body)
	}
	if captured.body["instructions"] != "test" {
		t.Fatalf("instructions = %#v", captured.body["instructions"])
	}
}

func TestAdapterKeepsAPIKeyRequestsOnThePublicEndpoint(t *testing.T) {
	t.Parallel()
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- request.URL.Path
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	adapter := newModelAdapter(modelAdapterConfig{
		settings: AgentSettingsView{
			Provider: providerOpenAI, Protocol: protocolOpenAIResponses,
			Endpoint: server.URL, Model: "gpt-5.5",
		},
		credential: providerCredential{Type: credentialKindAPI, Key: "sk-test"},
	}, server.Client())
	if _, err := adapter.Complete(context.Background(), ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 1024,
	}, func(string, string) {}); err != nil {
		t.Fatal(err)
	}
	if path := <-requests; path != "/responses" {
		t.Fatalf("request path = %q", path)
	}
}

func TestAdapterRefreshesRejectedOAuthTokenOnce(t *testing.T) {
	t.Parallel()
	attempts := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		token := request.Header.Get("Authorization")
		attempts <- token
		if token != "Bearer fresh-token" {
			response.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(response, `{"error":"expired"}`)
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	refreshes := 0
	adapter := newModelAdapter(modelAdapterConfig{
		settings: AgentSettingsView{
			Provider: providerOpenAI, Protocol: protocolOpenAIResponses, Endpoint: server.URL, Model: "gpt-5.5",
		},
		credential: providerCredential{
			Type: credentialKindOAuth, Access: "stale-token", Refresh: "refresh-token",
			Expires: time.Now().Add(time.Hour).UnixMilli(),
		},
		oauthEndpoint: server.URL,
		refresh: func(context.Context, providerCredential) (providerCredential, error) {
			refreshes++
			return providerCredential{
				Type: credentialKindOAuth, Access: "fresh-token", Refresh: "refresh-token",
				Expires: time.Now().Add(time.Hour).UnixMilli(),
			}, nil
		},
	}, server.Client())
	if _, err := adapter.Complete(context.Background(), ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 1024,
	}, func(string, string) {}); err != nil {
		t.Fatal(err)
	}
	if refreshes != 1 {
		t.Fatalf("refreshes = %d", refreshes)
	}
	if first := <-attempts; first != "Bearer stale-token" {
		t.Fatalf("first attempt = %q", first)
	}
	if second := <-attempts; second != "Bearer fresh-token" {
		t.Fatalf("second attempt = %q", second)
	}
}

func TestAdapterRefreshesExpiringTokenBeforeRequest(t *testing.T) {
	t.Parallel()
	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authorization <- request.Header.Get("Authorization")
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	adapter := newModelAdapter(modelAdapterConfig{
		settings: AgentSettingsView{
			Provider: providerOpenAI, Protocol: protocolOpenAIResponses, Endpoint: server.URL, Model: "gpt-5.5",
		},
		credential: providerCredential{
			Type: credentialKindOAuth, Access: "expiring-token", Refresh: "refresh-token",
			Expires: time.Now().Add(time.Minute).UnixMilli(),
		},
		oauthEndpoint: server.URL,
		refresh: func(context.Context, providerCredential) (providerCredential, error) {
			return providerCredential{
				Type: credentialKindOAuth, Access: "renewed-token", Refresh: "refresh-token",
				Expires: time.Now().Add(time.Hour).UnixMilli(),
			}, nil
		},
	}, server.Client())
	if _, err := adapter.Complete(context.Background(), ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 1024,
	}, func(string, string) {}); err != nil {
		t.Fatal(err)
	}
	if value := <-authorization; value != "Bearer renewed-token" {
		t.Fatalf("authorization = %q", value)
	}
}

func TestAdapterDoesNotRetryAPIKeyRejection(t *testing.T) {
	t.Parallel()
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(response, `{"error":"bad key"}`)
	}))
	defer server.Close()

	adapter := newModelAdapter(modelAdapterConfig{
		settings: AgentSettingsView{
			Provider: providerAnthropic, Protocol: protocolAnthropic, Endpoint: server.URL, Model: "claude-sonnet-5",
		},
		credential: providerCredential{Type: credentialKindAPI, Key: "sk-ant"},
		refresh: func(context.Context, providerCredential) (providerCredential, error) {
			t.Fatal("an API key request was refreshed")
			return providerCredential{}, nil
		},
	}, server.Client())
	err := func() error {
		_, err := adapter.Complete(context.Background(), ModelRequest{
			System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 1024,
		}, func(string, string) {})
		return err
	}()
	if err == nil {
		t.Fatal("Complete unexpectedly accepted a rejected request")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestProviderHeadersIncludeBuiltinDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider string
		want     map[string]string
	}{
		{
			provider: providerOpenRouter,
			want:     map[string]string{"HTTP-Referer": "https://nahida.live/", "X-Title": "Nahida Desktop"},
		},
		{
			provider: providerGo,
			want:     map[string]string{"x-opencode-session": "session-9"},
		},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			t.Parallel()
			headers := providerHeaders(test.provider, nil, nil, "session-9", "turn-1")
			for name, want := range test.want {
				if headers[name] != want {
					t.Fatalf("header %s = %q, want %q", name, headers[name], want)
				}
			}
		})
	}
}

func TestProviderHeadersUseConfiguredValuesOnlyForCustom(t *testing.T) {
	t.Parallel()
	configured := []AgentHeaderView{{Name: "X-Title", Value: "Custom Title"}}

	builtin := providerHeaders(providerOpenRouter, configured, nil, "", "")
	if builtin["X-Title"] != "Nahida Desktop" {
		t.Fatalf("built-in X-Title = %q", builtin["X-Title"])
	}
	if builtin["HTTP-Referer"] != "https://nahida.live/" {
		t.Fatalf("HTTP-Referer = %q", builtin["HTTP-Referer"])
	}

	custom := providerHeaders(providerCustom, configured, nil, "", "")
	if custom["X-Title"] != "Custom Title" {
		t.Fatalf("custom X-Title = %q", custom["X-Title"])
	}
	if len(custom) != 1 {
		t.Fatalf("custom headers = %#v", custom)
	}
}

func TestProviderHeadersUseSecretValuesForCustom(t *testing.T) {
	t.Parallel()
	headers := providerHeaders(providerCustom, []AgentHeaderView{
		{Name: "api-key", Secret: true},
	}, map[string]string{"api-key": "header-secret"}, "session-9", "turn-1")
	if headers["api-key"] != "header-secret" {
		t.Fatalf("api-key = %q", headers["api-key"])
	}
}

func TestProviderHeadersSkipEmptySessionReference(t *testing.T) {
	t.Parallel()
	headers := providerHeaders(providerGo, nil, nil, "", "")
	if _, present := headers["x-opencode-session"]; present {
		t.Fatalf("headers = %#v", headers)
	}
}

// oauthTestServer serves the token endpoint of a ChatGPT login.
func oauthTestServer(t *testing.T, accessToken, idToken string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/oauth/token" {
			t.Errorf("unexpected path %q", request.URL.Path)
			response.WriteHeader(http.StatusNotFound)
			return
		}
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if request.Form.Get("client_id") == "" {
			t.Error("token request carried no client id")
		}
		switch request.Form.Get("grant_type") {
		case "authorization_code":
			if request.Form.Get("code_verifier") == "" {
				t.Error("code exchange carried no PKCE verifier")
			}
		case "refresh_token":
			if request.Form.Get("refresh_token") == "" {
				t.Error("refresh carried no refresh token")
			}
		default:
			t.Errorf("unexpected grant type %q", request.Form.Get("grant_type"))
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"access_token": accessToken, "refresh_token": "refresh-2", "id_token": idToken, "expires_in": 3600,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// completeLogin drives the browser half of the flow: it reads the callback address and state from
// the authorization URL and calls the loopback listener like a browser would.
func completeLogin(t *testing.T, authorizeURL, code string) error {
	t.Helper()
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := url.Parse(parsed.Query().Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	callback := *redirect
	query := callback.Query()
	query.Set("code", code)
	query.Set("state", parsed.Query().Get("state"))
	callback.RawQuery = query.Encode()

	response, err := http.Get(callback.String())
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	return nil
}

func TestProviderLoginStoresAccountCredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	idToken := codexTestToken(t, map[string]any{
		"chatgpt_account_id": "acc-42", "email": "user@example.com",
	})
	issuer := oauthTestServer(t, "access-1", idToken)
	service := newSettingsService(t, Options{})
	service.oauth = providerOAuthConfig{
		Issuer: issuer.URL, ClientID: "test-client", CallbackPort: 0, Originator: codexOriginator,
	}

	login, err := service.StartProviderLogin(ctx, providerOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if login.Provider != providerOpenAI || login.URL == "" {
		t.Fatalf("login = %#v", login)
	}
	if err := completeLogin(t, login.URL, "code-1"); err != nil {
		t.Fatal(err)
	}

	settings, err := service.CompleteProviderLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Credential.Kind != credentialKindOAuth || settings.Credential.AccountLabel != "user@example.com" {
		t.Fatalf("credential = %#v", settings.Credential)
	}

	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	credential, err := credentialFor(ctx, client, service.crypto, providerOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Access != "access-1" || credential.Refresh != "refresh-2" || credential.AccountID != "acc-42" {
		t.Fatalf("credential = %#v", credential)
	}
	if credential.Expires <= time.Now().UnixMilli() {
		t.Fatalf("credential expiry = %d", credential.Expires)
	}
}

func TestProviderLoginRejectsMismatchedState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	issuer := oauthTestServer(t, "access-1", codexTestToken(t, map[string]any{"chatgpt_account_id": "acc-1"}))
	service := newSettingsService(t, Options{})
	service.oauth = providerOAuthConfig{Issuer: issuer.URL, ClientID: "test-client", CallbackPort: 0}

	login, err := service.StartProviderLogin(ctx, providerOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if err := completeLogin(t, login.URL, "code-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteProviderLogin(ctx); err != nil {
		t.Fatal(err)
	}

	second, err := service.StartProviderLogin(ctx, providerOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(second.URL)
	if err != nil {
		t.Fatal(err)
	}
	callback := parsed.Query().Get("redirect_uri") + "?code=code-2&state=forged"
	response, err := http.Get(callback)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback status = %d", response.StatusCode)
	}
	if _, err := service.CompleteProviderLogin(ctx); err == nil {
		t.Fatal("CompleteProviderLogin accepted a forged state")
	}
}

func TestProviderLoginRejectsAPIKeyOnlyProvider(t *testing.T) {
	t.Parallel()
	service := newSettingsService(t, Options{})
	if _, err := service.StartProviderLogin(context.Background(), providerAnthropic); err == nil {
		t.Fatal("StartProviderLogin accepted a provider without account login")
	}
}

func TestSignOutProviderRemovesAccountCredential(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{
		providerOpenAI: {Type: credentialKindOAuth, Access: "access", Refresh: "refresh", Email: "user@example.com"},
	}); err != nil {
		t.Fatal(err)
	}

	provider, err := service.SignOutProvider(ctx, providerOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if provider.Credential.Kind != "none" {
		t.Fatalf("credential = %#v", provider.Credential)
	}
	if _, err := service.SignOutProvider(ctx, "unknown-provider"); err == nil {
		t.Fatal("SignOutProvider accepted an unknown provider")
	}
}

func TestCredentialRefresherStoresRefreshedTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	issuer := oauthTestServer(t, "access-2", "")
	service := newSettingsService(t, Options{HTTP: issuer.Client()})
	service.oauth = providerOAuthConfig{Issuer: issuer.URL, ClientID: "test-client"}
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	stored := providerCredential{
		Type: credentialKindOAuth, Access: "access-1", Refresh: "refresh-1",
		Expires: time.Now().Add(-time.Minute).UnixMilli(),
	}
	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{
		providerOpenAI: stored,
	}); err != nil {
		t.Fatal(err)
	}

	refreshed, err := service.credentialRefresher(client, providerOpenAI)(ctx, stored)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Access != "access-2" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	credentials, err := readCredentials(ctx, client, service.crypto)
	if err != nil {
		t.Fatal(err)
	}
	if credentials[providerOpenAI].Access != "access-2" || credentials[providerOpenAI].Refresh != "refresh-2" {
		t.Fatalf("stored = %#v", credentials[providerOpenAI])
	}
}

func TestCredentialRefresherDoesNotRestoreSignedOutLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	issuer := oauthTestServer(t, "access-2", "")
	service := newSettingsService(t, Options{HTTP: issuer.Client()})
	service.oauth = providerOAuthConfig{Issuer: issuer.URL, ClientID: "test-client"}
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	stored := providerCredential{
		Type: credentialKindOAuth, Access: "access-1", Refresh: "refresh-1",
		Expires: time.Now().Add(-time.Minute).UnixMilli(),
	}
	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{
		providerOpenAI: stored,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SignOutProvider(ctx, providerOpenAI); err != nil {
		t.Fatal(err)
	}

	refreshed, err := service.credentialRefresher(client, providerOpenAI)(ctx, stored)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Access != "access-2" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	credentials, err := readCredentials(ctx, client, service.crypto)
	if err != nil {
		t.Fatal(err)
	}
	if credential, ok := credentials[providerOpenAI]; ok {
		t.Fatalf("signed-out credential was restored: %#v", credential)
	}
}

func TestRefreshProviderCredentialRequiresLogin(t *testing.T) {
	t.Parallel()
	if _, err := refreshProviderCredential(
		context.Background(),
		http.DefaultClient,
		defaultOpenAIOAuth,
		providerCredential{Type: credentialKindAPI, Key: "sk"},
	); err == nil {
		t.Fatal("refreshProviderCredential accepted an API key credential")
	}
}

func TestRefreshProviderCredentialKeepsAccountIdentity(t *testing.T) {
	t.Parallel()
	issuer := oauthTestServer(t, "access-2", codexTestToken(t, map[string]any{"chatgpt_account_id": "acc-7"}))
	refreshed, err := refreshProviderCredential(context.Background(), issuer.Client(), providerOAuthConfig{
		Issuer: issuer.URL, ClientID: "test-client",
	}, providerCredential{
		Type: credentialKindOAuth, Access: "access-1", Refresh: "refresh-1",
		Expires: time.Now().Add(-time.Minute).UnixMilli(), AccountID: "acc-old", Email: "user@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Access != "access-2" || refreshed.Refresh != "refresh-2" || refreshed.AccountID != "acc-7" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	if refreshed.Expires <= time.Now().UnixMilli() {
		t.Fatalf("refreshed expiry = %d", refreshed.Expires)
	}
}

func TestRefreshProviderCredentialKeepsStoredIdentityWithoutNewClaims(t *testing.T) {
	t.Parallel()
	issuer := oauthTestServer(t, "access-2", "")
	refreshed, err := refreshProviderCredential(context.Background(), issuer.Client(), providerOAuthConfig{
		Issuer: issuer.URL, ClientID: "test-client",
	}, providerCredential{
		Type: credentialKindOAuth, Access: "access-1", Refresh: "refresh-1",
		Expires: time.Now().Add(-time.Minute).UnixMilli(), AccountID: "acc-old", Email: "user@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccountID != "acc-old" || refreshed.Email != "user@example.com" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
}

func TestRefreshProviderCredentialKeepsStoredRefreshToken(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(response, `{"access_token":"access-2","expires_in":3600}`)
	}))
	defer server.Close()

	refreshed, err := refreshProviderCredential(context.Background(), server.Client(), providerOAuthConfig{
		Issuer: server.URL, ClientID: "test-client",
	}, providerCredential{
		Type: credentialKindOAuth, Access: "access-1", Refresh: "refresh-1", Email: "user@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Access != "access-2" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	if refreshed.Refresh != "refresh-1" {
		t.Fatalf("refresh token = %q", refreshed.Refresh)
	}
	if refreshed.Email != "user@example.com" {
		t.Fatalf("email = %q", refreshed.Email)
	}
}

func TestCredentialRefresherReusesFreshStoredTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var tokenRequests atomic.Int32
	issuer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		tokenRequests.Add(1)
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(response, `{"access_token":"access-2","refresh_token":"refresh-2","expires_in":3600}`)
	}))
	defer issuer.Close()

	service := newSettingsService(t, Options{HTTP: issuer.Client()})
	service.oauth = providerOAuthConfig{Issuer: issuer.URL, ClientID: "test-client"}
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCredentials(ctx, client, service.crypto, map[string]providerCredential{
		providerOpenAI: {
			Type: credentialKindOAuth, Access: "access-2", Refresh: "refresh-2",
			Expires: time.Now().Add(time.Hour).UnixMilli(),
		},
	}); err != nil {
		t.Fatal(err)
	}

	refreshed, err := service.credentialRefresher(client, providerOpenAI)(ctx, providerCredential{
		Type: credentialKindOAuth, Access: "access-1", Refresh: "refresh-1",
		Expires: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Access != "access-2" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	if tokenRequests.Load() != 0 {
		t.Fatalf("token requests = %d", tokenRequests.Load())
	}
}

func TestAdapterDeclaresAnthropicVersionWithoutKey(t *testing.T) {
	t.Parallel()
	headers := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		headers <- request.Header.Clone()
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	adapter := newModelAdapter(modelAdapterConfig{
		settings: AgentSettingsView{
			Provider: providerCustom, Protocol: protocolAnthropic, Endpoint: server.URL, Model: "claude-sonnet-5",
		},
	}, server.Client())
	if _, err := adapter.Complete(context.Background(), ModelRequest{
		System: "test", Messages: []Message{{Role: "user", Content: "test"}}, MaxOutputTokens: 1024,
	}, func(string, string) {}); err != nil {
		t.Fatal(err)
	}
	captured := <-headers
	if captured.Get("anthropic-version") != anthropicVersion {
		t.Fatalf("anthropic-version = %q", captured.Get("anthropic-version"))
	}
	if captured.Get("Authorization") != "" {
		t.Fatalf("authorization = %q", captured.Get("Authorization"))
	}
}

func TestCatalogCacheSurvivesMissingAppData(t *testing.T) {
	t.Parallel()
	service := newSettingsService(t, Options{})
	if _, err := service.catalogCachePath(); err == nil {
		t.Fatal("catalogCachePath returned a path without application data")
	}
	service.loadCatalogCache()
	if catalog().Source == "" {
		t.Fatal("embedded catalog was not loaded")
	}
}
