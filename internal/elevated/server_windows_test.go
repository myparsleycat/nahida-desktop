//go:build windows

package elevated

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

const testOperation = "test.block"

func TestHelperDisconnectCancelsOperationAndWaitsForCleanup(t *testing.T) {
	t.Parallel()

	for _, shutdown := range []string{"cancel", "close"} {
		t.Run(shutdown, func(t *testing.T) {
			t.Parallel()

			clientConn, serverConn := net.Pipe()
			serverCtx, cancelServer := context.WithCancel(t.Context())
			started := make(chan struct{})
			cancelled := make(chan struct{})
			cleanup := make(chan struct{})
			var releaseCleanup sync.Once
			t.Cleanup(func() {
				cancelServer()
				releaseCleanup.Do(func() { close(cleanup) })
				_ = clientConn.Close()
				_ = serverConn.Close()
			})
			handlers := map[string]Handler{testOperation: {
				Timeout: time.Hour,
				Run: func(ctx context.Context, _ Peer, _ json.RawMessage) (json.RawMessage, error) {
					close(started)
					<-ctx.Done()
					close(cancelled)
					<-cleanup
					return nil, ctx.Err()
				},
			}}
			serverDone := make(chan error, 1)
			go func() {
				serverDone <- serveHelperConnection(serverCtx, serverConn, "secret", Peer{}, handlers)
			}()
			client := NewClient()
			client.conn, client.secret = clientConn, "secret"
			if _, err := client.callLocked(t.Context(), operationHello, nil); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			operationDone := make(chan error, 1)
			go func() {
				_, err := client.Call(ctx, testOperation, nil)
				operationDone <- err
			}()
			waitForRequest(t, started)
			closeDone := make(chan error, 1)
			if shutdown == "cancel" {
				cancel()
			} else {
				go func() { closeDone <- client.Close() }()
			}
			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("disconnect did not cancel the server operation")
			}
			select {
			case err := <-serverDone:
				t.Fatalf("server returned before operation cleanup: %v", err)
			default:
			}

			releaseCleanup.Do(func() { close(cleanup) })
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("server did not return after operation cleanup")
			}
			select {
			case err := <-operationDone:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("operation = %v, want cancellation", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("client operation did not return after disconnect")
			}
			if shutdown == "close" {
				select {
				case err := <-closeDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("client Close did not return")
				}
			}
		})
	}
}

func TestHelperConnectionRejectsUnauthenticatedOperation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		secret string
	}{
		{name: "missing handshake", secret: "secret"},
		{name: "wrong secret", secret: "wrong"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, server := net.Pipe()
			defer func() { _ = client.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- serveHelperConnection(ctx, server, "secret", Peer{}, nil) }()
			if err := writeMessage(client, message{
				Version: protocolVersion, ID: 1, Operation: testOperation, Secret: test.secret,
			}); err != nil {
				t.Fatal(err)
			}
			response, err := readMessage(client)
			if err != nil {
				t.Fatal(err)
			}
			if response.OK || response.Error == "" {
				t.Fatalf("unauthenticated operation was accepted: %+v", response)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("server did not reject unauthenticated operation")
				}
			case <-ctx.Done():
				t.Fatal("server did not stop after rejecting unauthenticated operation")
			}
		})
	}
}

func TestHelperConnectionAcknowledgesStop(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveHelperConnection(ctx, serverConn, "secret", Peer{}, nil) }()
	client := NewClient()
	client.conn, client.secret = clientConn, "secret"
	defer func() { _ = clientConn.Close() }()
	if _, err := client.callLocked(ctx, operationHello, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close did not receive the stop acknowledgement: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server did not stop after acknowledgement")
	}
}

func TestHelperConnectionDispatchesRegisteredOperations(t *testing.T) {
	t.Parallel()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	handlers := map[string]Handler{
		"test.echo": {
			Timeout: time.Second,
			Run: func(_ context.Context, _ Peer, payload json.RawMessage) (json.RawMessage, error) {
				return payload, nil
			},
		},
		"test.fail": {
			Timeout: time.Second,
			Run: func(context.Context, Peer, json.RawMessage) (json.RawMessage, error) {
				return nil, errors.New("target is busy")
			},
			Classify: func(err error) (string, string) { return "TEST_BUSY", err.Error() },
		},
	}
	go func() { _ = serveHelperConnection(ctx, serverConn, "secret", Peer{}, handlers) }()
	client := NewClient()
	client.conn, client.secret = clientConn, "secret"
	if _, err := client.callLocked(ctx, operationHello, nil); err != nil {
		t.Fatal(err)
	}

	result, err := client.Call(ctx, "test.echo", json.RawMessage(`{"value":1}`))
	if err != nil || string(result) != `{"value":1}` {
		t.Fatalf("echo = %s, %v", result, err)
	}
	_, err = client.Call(ctx, "test.fail", nil)
	if err == nil || !strings.HasPrefix(err.Error(), "TEST_BUSY: elevated helper: target is busy") {
		t.Fatalf("classified failure = %v, want the handler code first", err)
	}
	_, err = client.Call(ctx, "test.unknown", nil)
	if err == nil || !strings.Contains(err.Error(), "operation is not allowed") {
		t.Fatalf("unregistered operation = %v, want it refused", err)
	}
}

func TestServerExitCodesNameEachStartupFailure(t *testing.T) {
	t.Parallel()

	seen := map[int]bool{}
	for _, exit := range serverExits {
		code := ServerExitCode(fmt.Errorf("%w: detail", exit.err))
		if code <= 1 || seen[code] {
			t.Fatalf("exit code for %q = %d, want a distinct code above 1", exit.err, code)
		}
		seen[code] = true
		if reason := serverExitReason(uint32(code)); reason != exit.err.Error() {
			t.Fatalf("reason for exit code %d = %q, want %q", code, reason, exit.err)
		}
	}
	if code := ServerExitCode(errors.New("unexpected")); code != 1 || serverExitReason(uint32(code)) != "" {
		t.Fatalf("unknown failure exit code = %d, want 1 without a reason", code)
	}
}
