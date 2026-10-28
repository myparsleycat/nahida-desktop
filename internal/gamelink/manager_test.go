package gamelink

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// fakeDLL serves one end of an in-memory pipe the way the DLL does.
type fakeDLL struct {
	pid      uint32
	versions [2]uint16
	conn     net.Conn
}

func (f *fakeDLL) serve(t *testing.T) {
	t.Helper()
	frame, err := ReadFrame(f.conn)
	if err != nil {
		return
	}
	hello, err := Decode(frame)
	if err != nil || hello.Type() != TypeHello {
		t.Errorf("first frame %v, %v", hello, err)
		return
	}
	if f.versions[0] > ProtocolVersion {
		f.send(frame.RequestID, ErrorReply{Code: ErrorUnsupportedVersion, Detail: "2-2"})
		_ = f.conn.Close()
		return
	}
	f.send(frame.RequestID, HelloReply{
		Version: ProtocolVersion, DLLVersion: "test", PID: f.pid, RenderAPI: RenderD3D11,
		Capabilities: CapabilityPickD3D11,
	})
	for {
		frame, err := ReadFrame(f.conn)
		if err != nil {
			return
		}
		if frame.Type == TypePickArm {
			f.send(frame.RequestID, PickArmed{CaptureID: 5})
			f.send(0, PickFailed{CaptureID: 5, Code: "NO_ELIGIBLE_DRAW"})
		}
	}
}

func (f *fakeDLL) send(id uint32, m Message) {
	frame, _ := Encode(id, m)
	data, _ := AppendFrame(nil, frame)
	_, _ = f.conn.Write(data)
}

func TestConnHandshakeChecksVersionAndPID(t *testing.T) {
	cases := map[string]struct {
		server fakeDLL
		want   error
	}{
		"newer DLL":    {fakeDLL{pid: 10, versions: [2]uint16{2, 2}}, ErrIncompatible},
		"another pid":  {fakeDLL{pid: 11, versions: [2]uint16{1, 1}}, ErrForeignServer},
		"same version": {fakeDLL{pid: 10, versions: [2]uint16{1, 1}}, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, server := net.Pipe()
			tc.server.conn = server
			go tc.server.serve(t)
			defer func() { _ = server.Close() }()

			conn, err := newConn(t.Context(), client, 10, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("newConn error %v, want %v", err, tc.want)
			}
			if conn != nil {
				_ = conn.Close()
			}
		})
	}
}

type fakeProcesses struct {
	mu   sync.Mutex
	pids map[uint32]string
}

func (f *fakeProcesses) GameProcesses(context.Context, map[string][]string) (map[uint32]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pids := make(map[uint32]string, len(f.pids))
	for pid, importer := range f.pids {
		pids[pid] = importer
	}
	return pids, nil
}

func (f *fakeProcesses) set(pids map[uint32]string) {
	f.mu.Lock()
	f.pids = pids
	f.mu.Unlock()
}

func TestManagerFollowsGameProcesses(t *testing.T) {
	processes := &fakeProcesses{pids: map[uint32]string{20: "GIMI"}}
	var servers sync.WaitGroup
	t.Cleanup(servers.Wait)
	manager := NewManager(Options{
		Processes:     processes,
		RetryInterval: 10 * time.Millisecond,
		IdleInterval:  10 * time.Millisecond,
		Dial: func(ctx context.Context, pid uint32, handler func(Message)) (*Conn, error) {
			client, server := net.Pipe()
			servers.Go(func() {
				(&fakeDLL{pid: pid, versions: [2]uint16{1, 1}, conn: server}).serve(t)
			})
			return newConn(ctx, client, pid, handler)
		},
	})
	messages := make(chan Message, 4)
	manager.Subscribe(Subscriber{OnMessage: func(importer string, pid uint32, m Message) {
		if importer == "GIMI" && pid == 20 {
			messages <- m
		}
	}})
	manager.Start(t.Context())
	t.Cleanup(manager.Shutdown)

	waitFor(t, func() bool { return manager.connected("GIMI") != nil })
	reply, err := manager.Request(t.Context(), "GIMI", PickArm{})
	if err != nil || reply != (PickArmed{CaptureID: 5}) {
		t.Fatalf("PickArm reply %#v, %v", reply, err)
	}
	select {
	case m := <-messages:
		if failed, ok := m.(PickFailed); !ok || failed.CaptureID != 5 {
			t.Fatalf("unsolicited message %#v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no unsolicited message")
	}

	processes.set(nil)
	manager.Wake()
	waitFor(t, func() bool { return len(manager.States()) == 0 })
	if _, err := manager.Request(t.Context(), "GIMI", PickArm{}); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Request after the game exited: %v", err)
	}
}

func TestManagerShutdownWaitsForMessageHandlers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		gameStopped bool
	}{
		{name: "connected game"},
		{name: "stopped game", gameStopped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan struct{})
				unblock := sync.OnceFunc(func() { close(release) })
				processes := &fakeProcesses{pids: map[uint32]string{20: "GIMI"}}
				manager := NewManager(Options{
					Processes: processes,
					Dial: func(ctx context.Context, pid uint32, handler func(Message)) (*Conn, error) {
						client, server := net.Pipe()
						go func() {
							defer func() { _ = server.Close() }()
							(&fakeDLL{pid: pid, versions: [2]uint16{1, 1}, conn: server}).serve(t)
						}()
						return newConn(ctx, client, pid, handler)
					},
				})
				manager.Subscribe(Subscriber{OnMessage: func(string, uint32, Message) {
					close(entered)
					<-release
					close(completed)
				}})
				manager.Start(t.Context())
				defer manager.Shutdown()
				defer unblock()
				synctest.Wait()
				if _, err := manager.Request(t.Context(), "GIMI", PickArm{}); err != nil {
					t.Fatal(err)
				}
				<-entered
				if tc.gameStopped {
					processes.set(nil)
					manager.Wake()
					synctest.Wait()
					if states := manager.States(); len(states) != 0 {
						t.Fatalf("stopped game still listed: %+v", states)
					}
				}

				shutdown := make(chan struct{})
				go func() {
					manager.Shutdown()
					close(shutdown)
				}()
				synctest.Wait()
				select {
				case <-shutdown:
					t.Error("Shutdown returned before the message handler completed")
				default:
				}

				unblock()
				<-shutdown
				select {
				case <-completed:
				default:
					t.Error("message handler still running after Shutdown")
				}
			})
		})
	}
}

func TestManagerWaitsForAnotherClientToReleaseThePipe(t *testing.T) {
	var held atomic.Bool
	held.Store(true)
	var servers sync.WaitGroup
	t.Cleanup(servers.Wait)
	manager := NewManager(Options{
		Processes:     &fakeProcesses{pids: map[uint32]string{20: "GIMI"}},
		RetryInterval: 10 * time.Millisecond,
		IdleInterval:  10 * time.Millisecond,
		Dial: func(ctx context.Context, pid uint32, handler func(Message)) (*Conn, error) {
			if held.Load() {
				return nil, ErrPipeBusy
			}
			client, server := net.Pipe()
			servers.Go(func() {
				(&fakeDLL{pid: pid, versions: [2]uint16{1, 1}, conn: server}).serve(t)
			})
			return newConn(ctx, client, pid, handler)
		},
	})
	changes := make(chan struct{}, 16)
	manager.Subscribe(Subscriber{OnChange: func() { changes <- struct{}{} }})
	manager.Start(t.Context())
	t.Cleanup(manager.Shutdown)

	status := func() Status {
		states := manager.States()
		if len(states) != 1 {
			return ""
		}
		return states[0].Status
	}
	waitFor(t, func() bool { return status() == StatusBusy })
	drained := len(changes)
	if drained < 2 {
		t.Fatalf("%d change notifications before busy, want the new link and the busy state", drained)
	}

	held.Store(false)
	waitFor(t, func() bool { return status() == StatusConnected })
	if len(changes) == drained {
		t.Fatal("no change notification for the connection")
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
