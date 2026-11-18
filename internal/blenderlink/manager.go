package blenderlink

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

const (
	// EventState carries State() whenever the server starts or stops or a client connects,
	// disconnects or sends a new scene.
	EventState = "blender-link:state"
	// DiscoveryFile is the app data file the add-on reads the pipe name and token from.
	DiscoveryFile = "blender-link.json"

	maxConnections = 4
	acceptBackoff  = time.Second
)

// Link identifies one connected Blender instance.
type Link struct {
	Session        uint64
	AddonVersion   string
	BlenderVersion string
	PID            uint32
}

// Result is a handler's answer to a scene or an object; Status is AckApplied, AckQueued or
// AckUnmatched.
type Result struct {
	Status         AckStatus
	ViewerAttached bool
	Message        string
}

// Handler receives complete scenes, objects and image pixels. Its methods run on the connection's
// goroutine, and the client waits for each to return before it sends the next request. ctx ends when
// the client disconnects or the server shuts down. An error rejects the request; the client keeps
// streaming.
type Handler interface {
	Scene(ctx context.Context, link Link, scene Scene) (Result, error)
	Object(ctx context.Context, link Link, update ObjectUpdate) (Result, error)
	Texture(ctx context.Context, link Link, update TextureUpdate) (Result, error)
	ClientError(ctx context.Context, link Link, report ClientError)
	Disconnected(link Link)
}

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	AppData   *appdata.Store
	// Handler defaults to one that queues everything.
	Handler Handler
}

type State struct {
	Listening bool
	Links     []LinkState
}

type LinkState struct {
	Link
	Exporter       string
	Game           string
	OutputFolder   string
	SceneRev       uint32
	Objects        int
	ViewerAttached bool
	// Errors are the scene's own errors, the ones its exporter would report.
	Errors []string
}

type discovery struct {
	Pipe  string `json:"pipe"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
	Proto int    `json:"proto"`
}

// Manager owns the pipe server, its discovery file and the client connections.
type Manager struct {
	opts     Options
	throttle infra.DiagnosticThrottle
	// lifecycle serializes Start and Shutdown.
	lifecycle sync.Mutex
	// token is written before the accept loop starts and read only by its sessions.
	token string

	mu       sync.Mutex
	listener net.Listener
	cancel   context.CancelFunc
	done     chan struct{}
	conns    map[net.Conn]struct{}
	sessions map[uint64]*session
	links    map[uint64]LinkState
	nextID   uint64
	serving  sync.WaitGroup

	addon addonEnv
	// addonMu serializes add-on installs and removals.
	addonMu sync.Mutex
	// addonThrottle is apart from throttle because a success resets every episode of the throttle it reports to.
	addonThrottle infra.DiagnosticThrottle
	// runningThrottle is apart from addonThrottle so a working poll does not reset a failing listing's episode.
	runningThrottle infra.DiagnosticThrottle
}

func NewManager(opts Options) *Manager {
	return &Manager{
		opts: opts, conns: map[net.Conn]struct{}{}, sessions: map[uint64]*session{}, links: map[uint64]LinkState{},
		addon: defaultAddonEnv(),
	}
}

// Start creates the pipe and the discovery file and accepts clients until Shutdown or ctx ends.
func (m *Manager) Start(ctx context.Context) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	running := m.listener != nil
	m.mu.Unlock()
	if running {
		return nil
	}

	stage := "listen"
	listener, pipe, err := listenPipe()
	if err == nil {
		stage = "write discovery"
		m.token = randomString(32)
		err = m.writeDiscovery(pipe)
		if err != nil {
			_ = listener.Close()
		}
	}
	if err != nil {
		return infra.ReportError(m.opts.Log, fmt.Errorf("start blender link: %w", err), "BlenderLink.Start",
			infra.Diagnostic{Operation: "BlenderLink.Start", Stage: stage, Fields: map[string]any{"pipe": pipe}},
		)
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m.mu.Lock()
	m.listener, m.cancel, m.done = listener, cancel, done
	m.mu.Unlock()
	go func() {
		defer close(done)
		m.accept(ctx, listener)
	}()
	m.emit()
	return nil
}

func (m *Manager) writeDiscovery(pipe string) error {
	data, err := json.Marshal(discovery{Pipe: pipe, Token: m.token, PID: os.Getpid(), Proto: ProtocolVersion})
	if err != nil {
		return err
	}
	return m.opts.AppData.WriteFile(DiscoveryFile, data, 0o600)
}

// Shutdown stops accepting, closes every connection, waits for their goroutines and removes the
// discovery file.
func (m *Manager) Shutdown() {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	listener, cancel, done := m.listener, m.cancel, m.done
	m.listener, m.cancel, m.done = nil, nil, nil
	m.mu.Unlock()
	if listener == nil {
		return
	}

	cancel()
	_ = listener.Close()
	<-done
	m.serving.Wait()
	if err := m.removeDiscovery(); err != nil {
		m.throttle.Report(m.opts.Log, err, "BlenderLink.Shutdown", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "BlenderLink.Shutdown", Stage: "remove discovery",
		})
	}
	m.emit()
}

// removeDiscovery leaves a file that another desktop process has rewritten since.
func (m *Manager) removeDiscovery() error {
	path, err := m.opts.AppData.Resolve(DiscoveryFile)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read blender link discovery file: %w", err)
	}
	var current discovery
	if err := json.Unmarshal(data, &current); err != nil {
		return fmt.Errorf("parse blender link discovery file: %w", err)
	}
	if !tokensEqual(current.Token, m.token) {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove blender link discovery file: %w", err)
	}
	return nil
}

func (m *Manager) accept(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		if err != nil {
			m.throttle.Report(m.opts.Log, err, "BlenderLink.Accept", infra.Diagnostic{Operation: "BlenderLink.Accept"})
			select {
			case <-ctx.Done():
				return
			case <-time.After(acceptBackoff):
			}
			continue
		}

		m.mu.Lock()
		if len(m.conns) >= maxConnections {
			m.mu.Unlock()
			_ = conn.Close()
			continue
		}
		m.conns[conn] = struct{}{}
		m.nextID++
		id := m.nextID
		m.serving.Add(1)
		m.mu.Unlock()
		go m.serve(ctx, conn, id)
	}
}

func (m *Manager) serve(ctx context.Context, conn net.Conn, id uint64) {
	defer m.serving.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer func() {
		_ = conn.Close()
		m.mu.Lock()
		delete(m.conns, conn)
		m.mu.Unlock()
	}()

	pid, err := pipeClientPID(conn)
	if err != nil {
		m.reportConnection(err, id, Link{Session: id}, "client pid")
		return
	}
	s := newSession(ctx, m, conn, id, pid)
	if err := s.serve(); err != nil && ctx.Err() == nil && !isDisconnect(err) {
		m.reportConnection(err, id, Link{Session: id, PID: pid, AddonVersion: s.link.AddonVersion}, "serve")
	}
}

func (m *Manager) reportConnection(err error, id uint64, link Link, stage string) {
	m.throttle.Report(m.opts.Log, err, "BlenderLink.Connection", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "BlenderLink.Connection", Stage: stage,
		Fields: map[string]any{"session": id, "pid": link.PID, "addon_version": link.AddonVersion},
	})
}

func (m *Manager) reportHandlerError(err error, operation string, fields map[string]any) {
	if infra.IsCancellationError(err) {
		return
	}
	m.throttle.Report(m.opts.Log, err, operation, infra.Diagnostic{Operation: operation, Fields: fields})
}

func (m *Manager) handler() Handler {
	if m.opts.Handler == nil {
		return queueHandler{}
	}
	return m.opts.Handler
}

func (m *Manager) attach(s *session) {
	m.mu.Lock()
	m.sessions[s.id] = s
	m.links[s.id] = s.state()
	m.mu.Unlock()
	m.emit()
}

func (m *Manager) publish(s *session) {
	m.mu.Lock()
	m.links[s.id] = s.state()
	m.mu.Unlock()
	m.emit()
}

func (m *Manager) detach(s *session) {
	m.mu.Lock()
	delete(m.sessions, s.id)
	delete(m.links, s.id)
	m.mu.Unlock()
	m.handler().Disconnected(s.link)
	m.emit()
}

// RequestResync sets RESYNC_ALL on the next reply to the given session, or to every session for
// 0, so the add-on sends its scene and every object again.
func (m *Manager) RequestResync(session uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if session == 0 || session == id {
			s.resync.Store(true)
		}
	}
}

func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := State{Listening: m.listener != nil, Links: make([]LinkState, 0, len(m.links))}
	for _, link := range m.links {
		state.Links = append(state.Links, link)
	}
	slices.SortFunc(state.Links, func(a, b LinkState) int { return cmp.Compare(a.Session, b.Session) })
	return state
}

func (m *Manager) emit() {
	if m.opts.EventEmit != nil {
		m.opts.EventEmit(EventState, m.State())
	}
}

type queueHandler struct{}

func (queueHandler) Scene(context.Context, Link, Scene) (Result, error) {
	return Result{Status: AckQueued}, nil
}

func (queueHandler) Object(context.Context, Link, ObjectUpdate) (Result, error) {
	return Result{Status: AckQueued}, nil
}

func (queueHandler) Texture(context.Context, Link, TextureUpdate) (Result, error) {
	return Result{Status: AckQueued}, nil
}

func (queueHandler) ClientError(context.Context, Link, ClientError) {}

func (queueHandler) Disconnected(Link) {}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
