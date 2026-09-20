//go:build windows

package elevated

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
)

type failingConn struct{}

func (failingConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (failingConn) Write([]byte) (int, error)        { return 0, errors.New("pipe write failed") }
func (failingConn) Close() error                     { return nil }
func (failingConn) LocalAddr() net.Addr              { return nil }
func (failingConn) RemoteAddr() net.Addr             { return nil }
func (failingConn) SetDeadline(time.Time) error      { return nil }
func (failingConn) SetReadDeadline(time.Time) error  { return nil }
func (failingConn) SetWriteDeadline(time.Time) error { return nil }

func TestCallLockedInvalidatesConnectionOnTransportError(t *testing.T) {
	t.Parallel()

	client := &Client{conn: failingConn{}, secret: "secret"}
	if _, err := client.callLocked(context.Background(), operationKeys, nil); err == nil {
		t.Fatal("callLocked = nil, want a transport error")
	}
	if client.conn != nil || client.process != 0 || client.pid != 0 || client.secret != "" {
		t.Fatalf("connection state = %+v, want it cleared", client)
	}
}

func TestSendKeysRequiresStartedHelper(t *testing.T) {
	t.Parallel()

	client := NewClient()
	_, err := client.SendKeys(context.Background(), platform.KeyRequest{})
	if !errors.Is(err, platform.ErrElevatedHelperRequired) {
		t.Fatalf("SendKeys = %v, want %v", err, platform.ErrElevatedHelperRequired)
	}
}

func TestCallLockedPreservesStableErrorCode(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	defer func() { _ = server.Close() }()

	go func() {
		if _, err := readMessage(server); err != nil {
			return
		}
		_ = writeMessage(server, message{
			Version:   protocolVersion,
			ID:        1,
			ErrorCode: platform.ErrWindowNotFound.Error(),
			Error:     `no visible window matches title "Game"`,
		})
	}()

	caller := &Client{conn: client, secret: "secret"}
	_, err := caller.callLocked(context.Background(), operationKeys, nil)
	if !errors.Is(err, platform.ErrWindowNotFound) {
		t.Fatalf("callLocked = %v, want %v", err, platform.ErrWindowNotFound)
	}
	if !strings.HasPrefix(err.Error(), platform.ErrWindowNotFound.Error()) {
		t.Fatalf("callLocked error = %q, want the stable code first", err)
	}
}
