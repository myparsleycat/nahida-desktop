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
	"nahida.live/desktop/internal/xxmi/inject"
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
	if _, err := client.callLocked(context.Background(), operationKeys, nil); err == nil {
		t.Fatal("callLocked = nil, want a transport error")
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("missing disconnect callback")
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

func TestLaunchXXMICancelUnblocksReadAndStatus(t *testing.T) {
	t.Parallel()

	client, received := stalledXXMILaunch(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	launchErr := startStalledLaunch(client, ctx)
	waitForLaunchRequest(t, received)

	connected := make(chan bool, 1)
	go func() { connected <- client.Connected() }()
	cancel()

	select {
	case err := <-launchErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("LaunchXXMI = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("LaunchXXMI did not return after cancel")
	}
	select {
	case got := <-connected:
		if got {
			t.Fatal("Connected still reported a helper after the cancelled launch closed it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Connected stayed blocked after cancel")
	}
}

func TestCloseUnblocksLaunchXXMI(t *testing.T) {
	t.Parallel()

	client, received := stalledXXMILaunch(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	launchErr := startStalledLaunch(client, ctx)
	waitForLaunchRequest(t, received)

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
		t.Fatal("Close blocked on the launch")
	}
	select {
	case err := <-launchErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("LaunchXXMI = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("LaunchXXMI did not return after Close")
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

func stalledXXMILaunch(t *testing.T) (*Client, <-chan struct{}) {
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
		if err != nil || request.Operation != operationXXMILaunch {
			return
		}
		close(received)
	}()
	return client, received
}

func startStalledLaunch(client *Client, ctx context.Context) <-chan error {
	launchErr := make(chan error, 1)
	go func() {
		_, err := client.LaunchXXMI(ctx, inject.LaunchSpec{TimeoutSeconds: 600})
		launchErr <- err
	}()
	return launchErr
}

func waitForLaunchRequest(t *testing.T, received <-chan struct{}) {
	t.Helper()
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("helper did not receive the launch request")
	}
}
