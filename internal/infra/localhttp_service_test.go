package infra

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLocalHTTPRoutesAndWebSocket(t *testing.T) {
	service := NewLocalHTTPWithOptions(LocalHTTPOptions{
		Address: "127.0.0.1:0",
		Version: "test-version",
		HandleMessage: func(_ context.Context, payload []byte) (string, error) {
			return "received " + string(payload), nil
		},
	})
	if err := service.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = service.ServiceShutdown() }()

	baseURL := "http://" + service.Address()
	for path, want := range map[string]string{"/version": "test-version", "/ping": "pong"} {
		response, err := http.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil || string(body) != want {
			t.Fatalf("GET %s = %q, %v; want %q", path, body, readErr, want)
		}
		if got := response.Header.Get("Access-Control-Allow-Origin"); got != "*" {
			t.Fatalf("GET %s CORS = %q", path, got)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, upgradeResponse, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(baseURL, "http")+"/ws", nil)
	if upgradeResponse != nil && upgradeResponse.Body != nil {
		_ = upgradeResponse.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := conn.Write(ctx, websocket.MessageText, []byte("wrong")); err != nil {
		t.Fatalf("write text: %v", err)
	}
	messageType, payload, err := conn.Read(ctx)
	if err != nil || messageType != websocket.MessageText || string(payload) != "invalid data" {
		t.Fatalf("text response = %v %q, %v", messageType, payload, err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("payload")); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	messageType, payload, err = conn.Read(ctx)
	if err != nil || messageType != websocket.MessageText || string(payload) != "received payload" {
		t.Fatalf("binary response = %v %q, %v", messageType, payload, err)
	}

	if err := service.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	defer readCancel()
	if _, _, err := conn.Read(readCtx); err == nil {
		t.Fatal("websocket remained open after shutdown")
	}
}
