package agent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
)

// newImageSessionService wires a settings service with an app data store and one mod folder, so
// sessions can resolve their scope and store images.
func newImageSessionService(t *testing.T, options Options) (*Service, *appdata.Store) {
	t.Helper()
	ctx := t.Context()
	service := newSettingsService(t, options)
	t.Cleanup(func() { _ = service.ServiceShutdown() })
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(store); err != nil {
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
	return service, store
}

func TestSendRejectsImagesWhenTheModelDoesNotAcceptThem(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	service, store := newImageSessionService(t, Options{})
	input := headerSettingsInput(nil)
	if _, err := service.UpdateSettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	session, err := service.CreateSession(ctx, AgentScope{Type: "global"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Send(ctx, session.ID, "look", []AgentImageInput{{
		Name: "shot.png", MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(testPNG),
	}})
	if err == nil {
		t.Fatal("Send accepted images while the model is not marked as accepting them")
	}
	if _, statErr := os.Stat(
		filepath.Join(store.Root(), imageDirectory, session.ID),
	); !errors.Is(
		statErr,
		os.ErrNotExist,
	) {
		t.Fatalf("rejected attachments were stored: %v", statErr)
	}
}

func TestSendDeliversAttachedImagesToTheProvider(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode provider request: %v", err)
			return
		}
		// A fresh session also asks for a summary title; this test inspects the turn request.
		if isTitleRequest(body) {
			writeAssistantText(response, "Image attachment")
			return
		}
		received <- body
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = response.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = response.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	service, _ := newImageSessionService(t, Options{HTTP: server.Client()})
	input := headerSettingsInput(nil)
	input.Endpoint = server.URL
	input.SupportsImages = true
	if _, err := service.UpdateSettings(ctx, input); err != nil {
		t.Fatal(err)
	}
	session := createNamedSession(t, service, AgentScope{Type: "global"})
	if _, err := service.Send(ctx, session.ID, "look", []AgentImageInput{{
		Name: "shot.png", MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(testPNG),
	}}); err != nil {
		t.Fatal(err)
	}

	select {
	case body := <-received:
		messages := body["messages"].([]any)
		parts, ok := messages[1].(map[string]any)["content"].([]any)
		if !ok || len(parts) != 2 {
			t.Fatalf("user content = %#v", messages[1])
		}
		want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(testPNG)
		if parts[1].(map[string]any)["image_url"].(map[string]any)["url"] != want {
			t.Fatalf("image part = %#v", parts[1])
		}
	case <-time.After(10 * time.Second):
		t.Fatal("provider request was not received")
	}

	// The turn is replayed from durable events, so the stored image must come back on the next
	// request instead of being dropped with the event payload.
	client, err := service.dbClient()
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.AgentEvents.List(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	messages := service.messagesFromEvents(events, true, true)
	if len(messages) == 0 || len(messages[0].Images) != 1 || messages[0].Images[0].MIMEType != "image/png" {
		t.Fatalf("replayed messages = %#v", messages)
	}
	if messages[0].Images[0].Data != base64.StdEncoding.EncodeToString(testPNG) {
		t.Fatalf("replayed image data = %q", messages[0].Images[0].Data)
	}

	// A text-only provider must never receive image parts.
	textOnly := service.messagesFromEvents(events, false, true)
	if len(textOnly) == 0 || len(textOnly[0].Images) != 0 {
		t.Fatalf("text-only messages = %#v", textOnly)
	}
	entries := projectEvents(events)
	if len(entries) == 0 || len(entries[0].Images) != 1 || entries[0].Images[0].Src == "" {
		t.Fatalf("projected entries = %#v", entries)
	}
}
