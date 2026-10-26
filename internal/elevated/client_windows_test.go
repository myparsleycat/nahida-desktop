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

	"golang.org/x/sys/windows"

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

func TestClassifyStartErrorMarksAntivirusBlocks(t *testing.T) {
	t.Parallel()

	lock := errors.New("lock elevated helper executable")
	tests := []struct {
		name    string
		err     error
		blocked bool
	}{
		{name: "infected", err: errors.Join(lock, windows.ERROR_VIRUS_INFECTED), blocked: true},
		{name: "deleted", err: errors.Join(lock, windows.ERROR_VIRUS_DELETED), blocked: true},
		{name: "access denied", err: errors.Join(lock, windows.ERROR_ACCESS_DENIED)},
		{name: "nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := classifyStartError(tt.err)
			if errors.Is(got, ErrBlockedByAntivirus) != tt.blocked {
				t.Fatalf("classifyStartError(%v) = %v, blocked want %v", tt.err, got, tt.blocked)
			}
			if tt.err != nil && !errors.Is(got, tt.err) {
				t.Fatalf("classifyStartError(%v) = %v, want the original error kept", tt.err, got)
			}
			if tt.err == nil && got != nil {
				t.Fatalf("classifyStartError(nil) = %v, want nil", got)
			}
		})
	}
}

func TestCallLockedInvalidatesConnectionOnTransportError(t *testing.T) {
	t.Parallel()

	client := &Client{conn: failingConn{}, secret: "secret"}
	if _, err := client.callLocked(context.Background(), operationPing, nil); err == nil {
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

func TestEnsureReadyDoesNotLaunchWhenWanted(t *testing.T) {
	t.Parallel()

	client := NewClient()
	client.wanted = true
	err := client.EnsureReady(context.Background())
	if !errors.Is(err, platform.ErrElevatedHelperRequired) {
		t.Fatalf("EnsureReady = %v, want %v", err, platform.ErrElevatedHelperRequired)
	}
	if client.Connected() {
		t.Fatal("EnsureReady connected the helper")
	}
}

func TestWatchProcessIgnoresProcessWhenStopSignaled(t *testing.T) {
	t.Parallel()

	process, err := windows.CreateEvent(nil, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := windows.CreateEvent(nil, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := &Client{conn: failingConn{}, wanted: true, generation: 1}
	client.watchProcess(process, stop, 1)
	if client.conn == nil {
		t.Fatal("watchProcess closed the connection after a requested stop")
	}
}

func TestWatchProcessDropsCurrentConnectionOnProcessExit(t *testing.T) {
	t.Parallel()

	process, err := windows.CreateEvent(nil, 1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := &Client{conn: failingConn{}, wanted: true, generation: 1}
	client.watchProcess(process, stop, 1)
	if client.conn != nil {
		t.Fatal("watchProcess left the connection open after process exit")
	}
}

func TestCloseLockedInvalidatesStaleWatcher(t *testing.T) {
	t.Parallel()

	client := &Client{conn: failingConn{}, wanted: true, generation: 3}
	oldGeneration := client.generation
	if err := client.closeLocked(); err != nil {
		t.Fatal(err)
	}
	if client.generation == oldGeneration {
		t.Fatal("closeLocked left the watcher generation unchanged")
	}

	client.conn = failingConn{}
	client.dropUnexpected(oldGeneration)
	if client.conn == nil {
		t.Fatal("stale watcher closed the replacement connection")
	}
}

func TestCloseLockedClearsWatchStop(t *testing.T) {
	t.Parallel()

	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(stop) }()

	client := &Client{conn: failingConn{}, watchStop: stop, wanted: true}
	if err := client.closeLocked(); err != nil {
		t.Fatal(err)
	}
	if client.watchStop != 0 {
		t.Fatalf("watchStop = %v, want 0", client.watchStop)
	}
	if err := windows.SetEvent(stop); err != nil {
		t.Fatalf("closeLocked closed the watcher event: %v", err)
	}
}

func TestCallLockedNotifiesDisconnectOnTransportError(t *testing.T) {
	t.Parallel()

	disconnected := make(chan struct{}, 1)
	client := &Client{conn: failingConn{}, secret: "secret", wanted: true}
	client.UseDisconnect(func() { disconnected <- struct{}{} })
	if _, err := client.callLocked(context.Background(), operationPing, nil); err == nil {
		t.Fatal("callLocked = nil, want a transport error")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("missing disconnect callback")
	}
}

func TestSendKeysPreservesStableErrorCode(t *testing.T) {
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
	_, err := caller.SendKeys(context.Background(), platform.KeyRequest{})
	if !errors.Is(err, platform.ErrWindowNotFound) {
		t.Fatalf("SendKeys = %v, want %v", err, platform.ErrWindowNotFound)
	}
	if !strings.HasPrefix(err.Error(), platform.ErrWindowNotFound.Error()) {
		t.Fatalf("SendKeys error = %q, want the stable code first", err)
	}
}

type deadlineConn struct {
	net.Conn
	deadlines chan time.Time
}

func (c deadlineConn) SetDeadline(deadline time.Time) error {
	select {
	case c.deadlines <- deadline:
	default:
	}
	return c.Conn.SetDeadline(deadline)
}

func TestSendKeysOutlastsTheHelperInputTimeout(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	defer func() { _ = server.Close() }()
	go func() {
		request, err := readMessage(server)
		if err != nil {
			return
		}
		_ = writeMessage(server, message{Version: protocolVersion, ID: request.ID, OK: true, Payload: []byte("{}")})
	}()

	conn := deadlineConn{Conn: client, deadlines: make(chan time.Time, 1)}
	caller := &Client{conn: conn, secret: "secret"}
	started := time.Now()
	if _, err := caller.SendKeys(context.Background(), platform.KeyRequest{}); err != nil {
		t.Fatal(err)
	}
	helperTimeout := InputHandler(nil).Timeout
	if budget := (<-conn.deadlines).Sub(started); budget <= helperTimeout {
		t.Fatalf("call deadline = %v after start, want longer than the helper's %v", budget, helperTimeout)
	}
}

func TestCallCancelUnblocksReadAndStatus(t *testing.T) {
	t.Parallel()

	client, received := stalledCall(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callErr := startStalledCall(client, ctx)
	waitForRequest(t, received)

	connected := make(chan bool, 1)
	go func() { connected <- client.Connected() }()
	cancel()

	select {
	case err := <-callErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after cancel")
	}
	select {
	case got := <-connected:
		if got {
			t.Fatal("Connected still reported a helper after the cancelled call closed it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connected stayed blocked after cancel")
	}
}

func TestCloseUnblocksCall(t *testing.T) {
	t.Parallel()

	client, received := stalledCall(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	callErr := startStalledCall(client, ctx)
	waitForRequest(t, received)

	connected := make(chan bool, 1)
	go func() { connected <- client.Connected() }()
	closeErr := make(chan error, 1)
	go func() { closeErr <- client.Close() }()

	select {
	case err := <-closeErr:
		if err != nil {
			t.Fatalf("Close = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked on the call")
	}
	select {
	case err := <-callErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after Close")
	}
	select {
	case got := <-connected:
		if got {
			t.Fatal("Connected still reported a helper after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connected stayed blocked after Close")
	}
}

func stalledCall(t *testing.T) (*Client, <-chan struct{}) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	client := NewClient()
	client.conn = clientConn
	client.secret = "secret"

	received := make(chan struct{})
	go func() {
		request, err := readMessage(serverConn)
		if err != nil || request.Operation != testOperation {
			return
		}
		close(received)
	}()
	return client, received
}

func startStalledCall(client *Client, ctx context.Context) <-chan error {
	callErr := make(chan error, 1)
	go func() {
		_, err := client.Call(ctx, testOperation, nil)
		callErr <- err
	}()
	return callErr
}

func waitForRequest(t *testing.T, received <-chan struct{}) {
	t.Helper()
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("helper did not receive the request")
	}
}
