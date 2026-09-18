package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/platform"
)

func newSettingsService(t *testing.T, options Options) *Service {
	t.Helper()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if options.Crypto == nil {
		options.Crypto = platform.NewCrypto()
	}
	service := New(options)
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	return service
}

func headerSettingsInput(headers []AgentHeaderInput) UpdateAgentSettingsInput {
	return UpdateAgentSettingsInput{
		Provider: providerCustom, Protocol: "openai-compatible", Endpoint: "https://example.com/v1",
		Model: "test-model", ContextWindowSize: 131072, MaxOutputTokens: 4096, Reasoning: "auto",
		Headers: headers,
	}
}

// createNamedSession opens a session that already carries a real title. A fresh session would also
// start a background title-summary provider request, which races the single provider request the
// send tests assert on.
func createNamedSession(t *testing.T, service *Service, scope AgentScope) AgentSessionSummary {
	t.Helper()
	ctx := t.Context()
	session, err := service.CreateSession(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RenameSession(ctx, session.ID, "Test session"); err != nil {
		t.Fatal(err)
	}
	return session
}

func TestSettingsHeaderSecretsRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service := newSettingsService(t, Options{})
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.UpdateSettings(ctx, headerSettingsInput([]AgentHeaderInput{
		{Name: "X-Title", Value: "Nahida Desktop"},
		{Name: "api-key", Secret: true, SecretAction: "replace", Value: "header-secret"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Headers) != 2 || saved.Headers[0].Value != "Nahida Desktop" || !saved.Headers[0].Configured {
		t.Fatalf("headers = %#v", saved.Headers)
	}
	if !saved.Headers[1].Secret || !saved.Headers[1].Configured || saved.Headers[1].Value != "" {
		t.Fatalf("secret header = %#v", saved.Headers[1])
	}
	stored, err := client.Settings.GetValue(ctx, settingsKey)
	if err != nil || stored == nil {
		t.Fatalf("stored settings = %v, %v", stored, err)
	}
	if strings.Contains(*stored, "header-secret") {
		t.Fatal("secret header value leaked into stored settings")
	}
	secrets, err := readHeaderSecrets(ctx, client, service.crypto)
	if err != nil || secrets["api-key"] != "header-secret" {
		t.Fatalf("secrets = %#v, %v", secrets, err)
	}

	kept, err := service.UpdateSettings(ctx, headerSettingsInput([]AgentHeaderInput{
		{Name: "X-Title", Value: "Nahida Desktop"},
		{Name: "api-key", Secret: true, SecretAction: "keep"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !kept.Headers[1].Configured {
		t.Fatalf("kept header = %#v", kept.Headers[1])
	}
	if values := headerValues(kept.Headers, secrets, "", ""); values["api-key"] != "header-secret" {
		t.Fatalf("resolved values = %#v", values)
	}

	cleared, err := service.UpdateSettings(ctx, headerSettingsInput([]AgentHeaderInput{
		{Name: "api-key", Secret: true, SecretAction: "clear"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Headers[0].Configured {
		t.Fatalf("cleared header = %#v", cleared.Headers[0])
	}
	if secrets, err := readHeaderSecrets(ctx, client, service.crypto); err != nil || len(secrets) != 0 {
		t.Fatalf("secrets after clear = %#v, %v", secrets, err)
	}
}

func TestUpdateSettingsRejectsInvalidHeaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		headers []AgentHeaderInput
	}{
		{name: "empty name", headers: []AgentHeaderInput{{Name: " "}}},
		{name: "invalid characters", headers: []AgentHeaderInput{{Name: "X Bad Header"}}},
		{name: "duplicate names", headers: []AgentHeaderInput{{Name: "X-Title"}, {Name: "x-title"}}},
		{name: "control characters", headers: []AgentHeaderInput{{Name: "X-Title", Value: "value\r\nX-Injected: 1"}}},
		{
			name:    "secret without value",
			headers: []AgentHeaderInput{{Name: "api-key", Secret: true, SecretAction: "replace"}},
		},
		{
			name:    "invalid secret action",
			headers: []AgentHeaderInput{{Name: "api-key", Secret: true, SecretAction: "reset"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := newSettingsService(t, Options{})
			if _, err := service.UpdateSettings(
				context.Background(),
				headerSettingsInput(test.headers),
			); err == nil {
				t.Fatal("UpdateSettings unexpectedly accepted invalid headers")
			}
		})
	}
}

func TestTestProviderResolvesStoredHeaderSecrets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	requests := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- request.Header.Clone()
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = response.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	service := newSettingsService(t, Options{HTTP: server.Client()})
	input := headerSettingsInput([]AgentHeaderInput{
		{Name: "X-Title", Value: "Nahida Desktop"},
		{Name: "api-key", Secret: true, SecretAction: "replace", Value: "header-secret"},
		{Name: "X-Session", Value: "{{ session_id }}"},
	})
	input.Endpoint = server.URL
	if _, err := service.UpdateSettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.Headers = []AgentHeaderInput{
		{Name: "X-Title", Value: "Nahida Desktop"},
		{Name: "api-key", Secret: true, SecretAction: "keep"},
		{Name: "X-Session", Value: "{{ session_id }}"},
	}
	if _, err := service.TestProvider(ctx, input); err != nil {
		t.Fatal(err)
	}
	headers := <-requests
	if headers.Get("X-Title") != "Nahida Desktop" || headers.Get("api-key") != "header-secret" {
		t.Fatalf("request headers = %#v", headers)
	}
	if values := headers.Values("X-Session"); len(values) != 1 || values[0] != "" {
		t.Fatalf("connection test session header = %#v", values)
	}
}

func TestHeaderValuesExpandPlaceholders(t *testing.T) {
	t.Parallel()
	values := headerValues([]AgentHeaderView{
		{Name: "X-Sess-Id", Value: "{{ session_id }}"},
		{Name: "X-Compact", Value: "{{session_id}}"},
		{Name: "X-Turn-Id", Value: "{{ turn_id }}"},
		{Name: "X-Tagged", Value: "run-{{ session_id }}-{{ turn_id }}-tail"},
		{Name: "X-Limited", Value: "{{ session_id, 6 }}"},
		{Name: "X-Limited-Turn", Value: "{{ turn_id(2) }}"},
		{Name: "X-Unknown", Value: "{{ root_id }}"},
		{Name: "X-Literal", Value: "{{}}"},
		{Name: "X-Plain", Value: "plain"},
		{Name: "X-Secret", Secret: true},
	}, map[string]string{"X-Secret": "key-{{ turn_id }}"}, "session-123", "turn-456")
	want := map[string]string{
		"X-Sess-Id":      "session-123",
		"X-Compact":      "session-123",
		"X-Turn-Id":      "turn-456",
		"X-Tagged":       "run-session-123-turn-456-tail",
		"X-Limited":      "sessio",
		"X-Limited-Turn": "tu",
		"X-Unknown":      "{{ root_id }}",
		"X-Literal":      "{{}}",
		"X-Plain":        "plain",
		"X-Secret":       "key-turn-456",
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("values = %#v, want %#v", values, want)
	}
}

func TestExpandHeaderValueLimitsPlaceholderLength(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "comma separator", value: "{{ session_id, 8 }}", want: "session-"},
		{name: "parentheses", value: "{{ session_id(8) }}", want: "session-"},
		{name: "no spaces", value: "{{session_id,8}}", want: "session-"},
		{name: "space before separator", value: "{{ session_id , 8 }}", want: "session-"},
		{name: "turn id", value: "{{ turn_id(3) }}", want: "tur"},
		{name: "zero limit", value: "{{ session_id, 0 }}", want: ""},
		{name: "limit beyond value length", value: "{{ session_id, 64 }}", want: "session-123"},
		{name: "several references", value: "{{ session_id, 7 }}-{{ turn_id(4) }}", want: "session-turn"},
		{name: "unknown variable", value: "{{ root_id, 4 }}", want: "{{ root_id, 4 }}"},
		{name: "missing limit", value: "{{ session_id, }}", want: "{{ session_id, }}"},
		{name: "negative limit", value: "{{ session_id, -2 }}", want: "{{ session_id, -2 }}"},
		{name: "non-numeric limit", value: "{{ session_id(soon) }}", want: "{{ session_id(soon) }}"},
		{name: "unclosed parentheses", value: "{{ session_id(8 }}", want: "{{ session_id(8 }}"},
		{name: "extra argument", value: "{{ session_id, 4, 2 }}", want: "{{ session_id, 4, 2 }}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := expandHeaderValue(test.value, "session-123", "turn-456"); got != test.want {
				t.Fatalf("expandHeaderValue(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestExpandHeaderValueCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()
	if got := expandHeaderValue("{{ session_id, 3 }}", "가나다라마", ""); got != "가나다" {
		t.Fatalf("expandHeaderValue = %q, want %q", got, "가나다")
	}
}

func TestSendExpandsHeaderPlaceholders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		received <- request.Header.Clone()
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = response.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = response.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	service := newSettingsService(t, Options{HTTP: server.Client()})
	t.Cleanup(func() { _ = service.ServiceShutdown() })
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(data); err != nil {
		t.Fatal(err)
	}
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	modRoot := filepath.Join(t.TempDir(), "Mods")
	if err := os.MkdirAll(modRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "Test Game", ModFolderPath: modRoot}); err != nil {
		t.Fatal(err)
	}
	input := headerSettingsInput([]AgentHeaderInput{
		{Name: "X-Sess-Id", Value: "{{ session_id }}"},
		{Name: "X-Turn-Id", Value: "{{ turn_id }}"},
		{Name: "X-Sess-Prefix", Value: "{{ session_id, 8 }}"},
	})
	input.Endpoint = server.URL
	if _, err := service.UpdateSettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	session := createNamedSession(t, service, AgentScope{Type: "global"})
	run, err := service.Send(ctx, session.ID, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case headers := <-received:
		if headers.Get("X-Sess-Id") != session.ID {
			t.Fatalf("X-Sess-Id = %q, session id = %q", headers.Get("X-Sess-Id"), session.ID)
		}
		if headers.Get("X-Turn-Id") != run.RunID {
			t.Fatalf("X-Turn-Id = %q, run id = %q", headers.Get("X-Turn-Id"), run.RunID)
		}
		if prefix := headers.Get("X-Sess-Prefix"); len(prefix) != 8 || !strings.HasPrefix(session.ID, prefix) {
			t.Fatalf("X-Sess-Prefix = %q, session id = %q", prefix, session.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("provider request was not received")
	}
}

func TestSendClampsMaxOutputToAvailableContext(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	received := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
			return
		}
		received <- body.MaxTokens
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = response.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = response.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	service := newSettingsService(t, Options{HTTP: server.Client()})
	t.Cleanup(func() { _ = service.ServiceShutdown() })
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(data); err != nil {
		t.Fatal(err)
	}
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	modRoot := filepath.Join(t.TempDir(), "Mods")
	if err := os.MkdirAll(modRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "Test Game", ModFolderPath: modRoot}); err != nil {
		t.Fatal(err)
	}
	input := headerSettingsInput(nil)
	input.Endpoint = server.URL
	input.ContextWindowSize = 400_000
	input.MaxOutputTokens = 384_000
	if _, err := service.UpdateSettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	session := createNamedSession(t, service, AgentScope{Type: "global"})
	if _, err := service.Send(ctx, session.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}

	select {
	case maxTokens := <-received:
		if maxTokens <= 0 || maxTokens >= input.MaxOutputTokens {
			t.Fatalf("provider max tokens = %d, want a positive value below %d", maxTokens, input.MaxOutputTokens)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("provider request was not received")
	}
}
