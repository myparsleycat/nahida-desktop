//go:build windows

package elevated

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi/inject"
)

func TestHelperDisconnectCancelsOperationAndWaitsForCleanup(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{operationKeys, operationXXMILaunch} {
		for _, shutdown := range []string{"cancel", "close"} {
			t.Run(operation+"/"+shutdown, func(t *testing.T) {
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
				work := func(ctx context.Context) error {
					close(started)
					<-ctx.Done()
					close(cancelled)
					<-cleanup
					return ctx.Err()
				}
				serverDone := make(chan error, 1)
				go func() {
					serverDone <- serveHelperConnection(serverCtx, serverConn, "secret", serverOperations{
						sendKeys: func(ctx context.Context, _ platform.KeyRequest) (platform.KeyResult, error) {
							return platform.KeyResult{}, work(ctx)
						},
						launchXXMI: func(ctx context.Context, _ inject.LaunchSpec) (inject.LaunchResult, error) {
							return inject.LaunchResult{}, work(ctx)
						},
					})
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
					if operation == operationKeys {
						_, err := client.SendKeys(ctx, platform.KeyRequest{HoldMs: 2000})
						operationDone <- err
						return
					}
					_, err := client.LaunchXXMI(ctx, inject.LaunchSpec{Mode: inject.ModeLegacy, TimeoutSeconds: 600})
					operationDone <- err
				}()
				waitForLaunchRequest(t, started)
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
			go func() { done <- serveHelperConnection(ctx, server, "secret", serverOperations{}) }()
			if err := writeMessage(client, message{
				Version: protocolVersion, ID: 1, Operation: operationKeys, Secret: test.secret,
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
	go func() { done <- serveHelperConnection(ctx, serverConn, "secret", serverOperations{}) }()
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
