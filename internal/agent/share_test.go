package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == trainingPlanPath:
			var request trainingPlanRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			plan := trainingPlanResponse{Prefix: "agent-conversation/user/session"}
			for _, image := range request.Images {
				plan.Objects = append(plan.Objects, trainingPlanObject{
					Ref: image.Ref, Key: "agent-conversation/user/session/" + image.Ref + ".png",
					URL: baseURL + "/put/" + image.Ref, ContentType: image.ContentType,
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
	baseURL = server.URL

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
		BackendURL: server.URL, HTTPClient: server.Client(),
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

// TestSubmitSessionForTrainingSkipsRevertedTurns keeps the staged revert, which
// hides the events at and after its boundary on the session screen, out of the
// contribution as well.
func TestSubmitSessionForTrainingSkipsRevertedTurns(t *testing.T) {
	ctx := t.Context()

	var submit trainingSubmitRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == trainingSubmitPath {
			_ = json.NewDecoder(r.Body).Decode(&submit)
			writeShareJSON(t, w, map[string]string{"id": "stored-2"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
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
	if _, err := service.SubmitSessionForTraining(ctx, "session-1"); err != nil {
		t.Fatalf("SubmitSessionForTraining: %v", err)
	}
	if len(submit.Document.Entries) != 1 || submit.Document.Entries[0].Text != "kept" {
		t.Fatalf("entries = %+v, want the event before the boundary alone", submit.Document.Entries)
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
