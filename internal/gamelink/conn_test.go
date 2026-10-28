package gamelink

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

func TestConnRequestCancelsBlockedWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"cancellation", context.Canceled},
		{"deadline", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				conn, _ := newIdleConn(t)
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					_, err := conn.Request(ctx, PickArm{})
					done <- err
				}()
				synctest.Wait()
				if errors.Is(tc.want, context.Canceled) {
					cancel()
				} else {
					time.Sleep(time.Second)
				}
				synctest.Wait()

				select {
				case err := <-done:
					if !errors.Is(err, tc.want) {
						t.Fatalf("Request error %v, want %v", err, tc.want)
					}
				default:
					t.Fatal("Request did not cancel its blocked write")
				}
				select {
				case <-conn.Done():
				default:
					t.Fatal("canceled write left a possibly partial frame on the connection")
				}
			})
		})
	}
}

func TestConnQueuedRequestCancellationPreservesActiveRequest(t *testing.T) {
	t.Parallel()
	conn, server := newIdleConn(t)
	active := make(chan error, 1)
	go func() {
		_, err := conn.Request(t.Context(), PickArm{})
		active <- err
	}()
	var prefix [1]byte
	if _, err := io.ReadFull(server, prefix[:]); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queued := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := conn.Request(ctx, PickArm{})
		queued <- err
	}()
	<-started
	cancel()
	select {
	case err := <-queued:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued Request error %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued Request did not cancel while waiting to write")
	}

	frame, err := ReadFrame(io.MultiReader(bytes.NewReader(prefix[:]), server))
	if err != nil {
		t.Fatal(err)
	}
	(&fakeDLL{conn: server}).send(frame.RequestID, PickArmed{CaptureID: 5})
	if err := <-active; err != nil {
		t.Fatalf("queued cancellation interrupted the active Request: %v", err)
	}
}

func TestConnReplyTimeoutKeepsConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn, server := newIdleConn(t)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := conn.Request(ctx, PickArm{})
			done <- err
		}()
		if _, err := ReadFrame(server); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Request error %v, want deadline exceeded", err)
		}
		select {
		case <-conn.Done():
			t.Fatal("reply timeout closed the connection after a complete write")
		default:
		}
	})
}

func newIdleConn(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = server.Close() })
	go func() {
		frame, err := ReadFrame(server)
		if err != nil {
			t.Error(err)
			_ = server.Close()
			return
		}
		(&fakeDLL{conn: server}).send(frame.RequestID, HelloReply{Version: ProtocolVersion, PID: 10})
	}()
	conn, err := newConn(t.Context(), client, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, server
}
