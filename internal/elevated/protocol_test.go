package elevated

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

func TestMessageRoundTrip(t *testing.T) {
	t.Parallel()
	want := message{
		Version: protocolVersion, ID: 9, Operation: operationPing, Secret: "secret",
		ErrorCode: "WINDOW_NOT_FOUND", Error: "no visible window matches",
	}
	var buffer bytes.Buffer
	if err := writeMessage(&buffer, want); err != nil {
		t.Fatal(err)
	}
	got, err := readMessage(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || got.ID != want.ID || got.Operation != want.Operation ||
		got.Secret != want.Secret || got.ErrorCode != want.ErrorCode || got.Error != want.Error {
		t.Fatalf("message = %+v, want %+v", got, want)
	}
}

func TestReadMessageRejectsOversizeFrame(t *testing.T) {
	t.Parallel()
	var buffer bytes.Buffer
	_ = binary.Write(&buffer, binary.LittleEndian, uint32(maxMessageSize+1))
	if _, err := readMessage(&buffer); err == nil || !strings.Contains(err.Error(), "message size") {
		t.Fatalf("readMessage = %v, want size error", err)
	}
}

func TestSecretsEqual(t *testing.T) {
	t.Parallel()
	if !secretsEqual("same", "same") || secretsEqual("same", "different") {
		t.Fatal("constant-time secret comparison returned an unexpected result")
	}
}

func TestDialPipeUntilReadyWaitsForServerCreation(t *testing.T) {
	pipe := `\\.\pipe\nahida-elevated-test-` + strings.ReplaceAll(t.Name(), "/", "-")
	listenerReady := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		listener, err := winio.ListenPipe(pipe, nil)
		if err != nil {
			listenerReady <- err
			return
		}
		defer func() { _ = listener.Close() }()
		listenerReady <- nil
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialPipeUntilReady(ctx, pipe)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-listenerReady; err != nil {
		t.Fatal(err)
	}
}
