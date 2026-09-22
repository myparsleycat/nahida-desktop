package agent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

// TestSubmitSessionForTrainingUploadsImagesAndStoresTheTranscript exercises the
// whole contribution against a fake backend: the images go to the presigned
// PUTs and the transcript with the tool calls goes to the submit route.
func TestSubmitSessionForTrainingUploadsImagesAndStoresTheTranscript(t *testing.T) {
	ctx := t.Context()

	var mu sync.Mutex
	uploaded := map[string][]byte{}
	var submit trainingSubmitRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == trainingPlanPath:
			var request trainingPlanRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			plan := trainingPlanResponse{Prefix: "agent-conversation/user/session"}
			for _, image := range request.Images {
				plan.Objects = append(plan.Objects, trainingPlanObject{
					Ref: image.Ref, Key: "agent-conversation/user/session/" + image.Ref + ".png",
					URL: "https://" + trainingStorageHost + "/put/" + image.Ref, ContentType: image.ContentType,
					Headers: map[string]string{"Content-Type": image.ContentType},
				})
			}
			writeShareJSON(t, w, plan)
		case strings.HasPrefix(r.URL.Path, "/put/"):
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			uploaded[strings.TrimPrefix(r.URL.Path, "/put/")] = body
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == trainingSubmitPath:
			_ = json.NewDecoder(r.Body).Decode(&submit)
			writeShareJSON(t, w, map[string]string{"id": "stored-1"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport := server.Client().Transport
	httpClient := &http.Client{Transport: shareRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() != trainingStorageHost {
			return serverTransport.RoundTrip(request)
		}
		forwarded := request.Clone(request.Context())
		forwardedURL := *request.URL
		forwardedURL.Scheme = serverURL.Scheme
		forwardedURL.Host = serverURL.Host
		forwarded.URL = &forwardedURL
		return serverTransport.RoundTrip(forwarded)
	})}

	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.AgentSessions.Insert(ctx, db.AgentSessionRow{
		ID: "session-1", ScopeType: "global", Title: "A chat", CreatedAt: "1", UpdatedAt: "2",
	}); err != nil {
		t.Fatal(err)
	}

	remote := infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL, HTTPClient: httpClient,
	})
	service := New(Options{Remote: remote})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(data); err != nil {
		t.Fatal(err)
	}
	stored, err := service.storeImages("session-1", []pendingImage{{
		Name: "shot.png", MIMEType: "image/png", Data: testPNG,
	}})
	if err != nil {
		t.Fatal(err)
	}

	appendEvent := func(turnID, eventType string, payload any) {
		t.Helper()
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
			SessionID: "session-1", TurnID: turnID, EventType: eventType, Payload: string(encoded), CreatedAt: "1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	call, err := json.Marshal(ToolCall{ID: "call-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)})
	if err != nil {
		t.Fatal(err)
	}
	appendEvent("turn-1", "turn/start", map[string]any{"text": "look", "images": stored})
	appendEvent("turn-1", "message/assistant", map[string]any{"text": "sure", "reasoning": "thinking"})
	if _, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
		SessionID: "session-1", TurnID: "turn-1", EventType: "tool/start", Payload: string(call), CreatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}
	appendEvent("turn-1", "tool/end", map[string]any{
		"toolName": "read_file", "toolCallId": "call-1", "result": map[string]any{"ok": true},
	})
	appendEvent("turn-1", "turn/end", map[string]any{"status": "completed"})

	result, err := service.SubmitSessionForTraining(ctx, "session-1")
	if err != nil {
		t.Fatalf("SubmitSessionForTraining: %v", err)
	}
	if result.ID != "stored-1" || result.Messages != 2 || result.Images != 1 {
		t.Fatalf("result = %+v", result)
	}

	mu.Lock()
	body := uploaded["img-0"]
	mu.Unlock()
	if string(body) != string(testPNG) {
		t.Errorf("uploaded bytes = %d, want %d", len(body), len(testPNG))
	}

	if submit.Document.ClientSessionID != "session-1" || submit.Document.MessageCount != 2 {
		t.Errorf("document = %+v", submit.Document)
	}
	if submit.Document.Provider != providerOpenAI || submit.Document.Model != "gpt-5.2" {
		t.Errorf("settings metadata = %q/%q", submit.Document.Provider, submit.Document.Model)
	}
	if len(submit.Images) != 1 || submit.Images[0].Ref != "img-0" || submit.Images[0].MimeType != "image/png" {
		t.Errorf("images = %+v", submit.Images)
	}
	if len(submit.Document.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(submit.Document.Entries))
	}
	if len(submit.Document.Entries[0].Images) != 1 || submit.Document.Entries[0].Images[0].Ref != "img-0" {
		t.Errorf("turn/start images = %+v", submit.Document.Entries[0].Images)
	}
	if submit.Document.Entries[2].Type != "tool/start" || len(submit.Document.Entries[2].Arguments) == 0 {
		t.Errorf("tool/start = %+v", submit.Document.Entries[2])
	}
	if submit.Document.Entries[3].Type != "tool/end" || len(submit.Document.Entries[3].Result) == 0 {
		t.Errorf("tool/end = %+v", submit.Document.Entries[3])
	}
}

func TestSubmitSessionForTrainingRejectsRevertedSession(t *testing.T) {
	ctx := t.Context()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	revert := `{"boundarySequence":2,"boundaryTurnId":"turn-2"}`
	if err := client.AgentSessions.Insert(ctx, db.AgentSessionRow{
		ID: "session-1", ScopeType: "global", Title: "A chat", CreatedAt: "1", UpdatedAt: "2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.AgentSessions.SetRevert(ctx, "session-1", revert, "2"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"kept", "dropped"} {
		payload, _ := json.Marshal(map[string]any{"text": text})
		if _, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
			SessionID: "session-1", TurnID: "turn", EventType: "message/assistant",
			Payload: string(payload), CreatedAt: "1",
		}); err != nil {
			t.Fatal(err)
		}
	}

	remote := infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL, HTTPClient: server.Client(),
	})
	service := New(Options{Remote: remote})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitSessionForTraining(ctx, "session-1"); err == nil ||
		!strings.Contains(err.Error(), "staged revert") {
		t.Fatalf("SubmitSessionForTraining error = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("backend requests = %d, want 0", requests.Load())
	}
}

func TestSubmitSessionForTrainingRejectsRunningSession(t *testing.T) {
	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.AgentSessions.Insert(ctx, db.AgentSessionRow{
		ID: "session-1", ScopeType: "global", Title: "A chat", CreatedAt: "1", UpdatedAt: "2",
	}); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"text": "still running"})
	var requests atomic.Int32
	remote := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: shareRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("unexpected request")
		})},
	})
	service := New(Options{Remote: remote})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
		SessionID: "session-1", TurnID: "run-1", EventType: "turn/start", Payload: string(payload), CreatedAt: "1",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SubmitSessionForTraining(ctx, "session-1"); err == nil ||
		!strings.Contains(err.Error(), "still responding") {
		t.Fatalf("unfinished run error = %v", err)
	}
	if _, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
		SessionID: "session-1", TurnID: "run-1", EventType: "turn/end", Payload: `{}`, CreatedAt: "2",
	}); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.workers["session-1"] = &sessionWorker{active: "run-2"}
	service.mu.Unlock()
	if _, err := service.SubmitSessionForTraining(ctx, "session-1"); err == nil ||
		!strings.Contains(err.Error(), "still responding") {
		t.Fatalf("active worker error = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("backend requests = %d, want 0", requests.Load())
	}
}

func TestSubmitSessionForTrainingReturnsSettingsErrorBeforeImageProcessing(t *testing.T) {
	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.AgentSessions.Insert(ctx, db.AgentSessionRow{
		ID: "session-1", ScopeType: "global", Title: "A chat", CreatedAt: "1", UpdatedAt: "2",
	}); err != nil {
		t.Fatal(err)
	}
	start, _ := json.Marshal(map[string]any{
		"text": "look", "images": []AgentImage{{Path: "missing.png", MIMEType: "image/png"}},
	})
	for _, event := range []db.AgentEventRow{
		{SessionID: "session-1", TurnID: "run-1", EventType: "turn/start", Payload: string(start), CreatedAt: "1"},
		{SessionID: "session-1", TurnID: "run-1", EventType: "turn/end", Payload: `{}`, CreatedAt: "2"},
	} {
		if _, err := client.AgentEvents.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	var requests atomic.Int32
	remote := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: shareRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("unexpected request")
		})},
	})
	service := New(Options{Remote: remote})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	invalidSettings := "{"
	if err := client.Settings.Upsert(ctx, settingsKey, &invalidSettings); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SubmitSessionForTraining(ctx, "session-1"); err == nil ||
		!strings.Contains(err.Error(), "decode agent settings") {
		t.Fatalf("SubmitSessionForTraining error = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("backend requests = %d, want 0", requests.Load())
	}
}

func TestSubmitTrainingConversationRejectsBlankStoredID(t *testing.T) {
	for _, storedID := range []string{"", " \t "} {
		t.Run(strconv.Quote(storedID), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeShareJSON(t, w, map[string]string{"id": storedID})
			}))
			defer server.Close()

			service := New(Options{Remote: infra.NewClientWithOptions(infra.ClientOptions{
				BackendURL: server.URL, HTTPClient: server.Client(),
			})})
			if _, err := service.submitTrainingConversation(t.Context(), trainingDocument{}, nil); err == nil ||
				!strings.Contains(err.Error(), "stored conversation id") {
				t.Fatalf("submitTrainingConversation error = %v", err)
			}
		})
	}
}

func TestUploadTrainingImageRejectsUnsafeInitialURL(t *testing.T) {
	var requests atomic.Int32
	service := New(Options{Remote: infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: shareRoundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("unexpected request")
		})},
	})})

	for _, rawURL := range []string{
		"http://" + trainingStorageHost + "/object",
		"https://localhost/object",
		"https://127.0.0.1/object",
		"https://attacker.example/object",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if err := service.uploadTrainingImage(t.Context(), rawURL, nil, []byte("image")); err == nil {
				t.Fatal("uploadTrainingImage accepted an unsafe URL")
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("transport requests = %d, want 0", requests.Load())
	}
}

func TestUploadTrainingImageRejectsUnsafeRedirects(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		location string
	}{
		{name: "cross-host 307", status: http.StatusTemporaryRedirect, location: "https://attacker.example/object"},
		{name: "cross-host 308", status: http.StatusPermanentRedirect, location: "https://attacker.example/object"},
		{
			name:     "HTTPS downgrade",
			status:   http.StatusTemporaryRedirect,
			location: "http://" + trainingStorageHost + "/object",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			service := New(Options{Remote: infra.NewClientWithOptions(infra.ClientOptions{
				HTTPClient: &http.Client{
					Transport: shareRoundTripFunc(func(request *http.Request) (*http.Response, error) {
						requests.Add(1)
						body, err := io.ReadAll(request.Body)
						if err != nil {
							return nil, err
						}
						if string(body) != "image" {
							t.Errorf("first request body = %q", body)
						}
						return redirectResponse(request, test.status, test.location), nil
					}),
				},
			})})

			if err := service.uploadTrainingImage(
				t.Context(), "https://"+trainingStorageHost+"/start", nil, []byte("image"),
			); err == nil {
				t.Fatal("uploadTrainingImage followed an unsafe redirect")
			}
			if requests.Load() != 1 {
				t.Fatalf("transport requests = %d, want 1", requests.Load())
			}
		})
	}
}

func TestUploadTrainingImageAllowsSameHostHTTPSRedirect(t *testing.T) {
	var requests atomic.Int32
	service := New(Options{Remote: infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: shareRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			if request.URL.Path == "/start" {
				return redirectResponse(
					request, http.StatusTemporaryRedirect, "https://"+trainingBucketHost+"/finish",
				), nil
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			if request.Method != http.MethodPut || string(body) != "image" {
				t.Errorf("redirected request = %s %q", request.Method, body)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		})},
	})})

	if err := service.uploadTrainingImage(
		t.Context(), "https://"+trainingBucketHost+"/start", nil, []byte("image"),
	); err != nil {
		t.Fatalf("uploadTrainingImage: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("transport requests = %d, want 2", requests.Load())
	}
}

func TestTrainingEntryFromEventKeepsToolArgumentsAndResult(t *testing.T) {
	start, _, ok := trainingEntryFromEvent(db.AgentEventRow{
		Sequence: 1, TurnID: "t", EventType: "tool/start",
		Payload: `{"id":"call-1","name":"read_file","arguments":{"path":"a"}}`,
	})
	if !ok || start.ToolName != "read_file" || string(start.Arguments) != `{"path":"a"}` {
		t.Fatalf("tool/start = %+v", start)
	}

	end, _, ok := trainingEntryFromEvent(db.AgentEventRow{
		Sequence: 2, TurnID: "t", EventType: "tool/end",
		Payload: `{"toolName":"read_file","toolCallId":"call-1","result":{"ok":true},"changedFiles":["a"]}`,
	})
	if !ok || end.ToolName != "read_file" || string(end.Result) != `{"ok":true}` || len(end.ChangedFiles) != 1 {
		t.Fatalf("tool/end = %+v", end)
	}

	if _, _, ok := trainingEntryFromEvent(db.AgentEventRow{EventType: "summary"}); ok {
		t.Fatal("a summary event became a training entry")
	}
}

func TestCountTrainingMessagesCountsUsersAndAssistantsOnly(t *testing.T) {
	entries := []trainingEntry{
		{Role: "user"}, {Role: "assistant"}, {Role: "assistant"}, {ToolName: "read_file"}, {Type: "tool/end"},
	}
	if got := countTrainingMessages(entries); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
}

func TestTrainingErrorPrefersTheEnvelopeMessage(t *testing.T) {
	if err := trainingError(
		http.StatusBadRequest,
		[]byte(`{"error":"entries are required","status":400}`),
	); err == nil ||
		err.Error() != "entries are required" {
		t.Fatalf("error = %v", err)
	}
	if err := trainingError(http.StatusInternalServerError, []byte("boom")); err == nil ||
		!strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v", err)
	}
}

func writeShareJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

type shareRoundTripFunc func(*http.Request) (*http.Response, error)

func (f shareRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func redirectResponse(request *http.Request, status int, location string) *http.Response {
	header := make(http.Header)
	header.Set("Location", location)
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}
}
