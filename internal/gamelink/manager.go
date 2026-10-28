package gamelink

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	defaultRetryInterval = 2 * time.Second
	defaultIdleInterval  = 30 * time.Second
	dialTimeout          = 5 * time.Second
)

var ErrNotConnected = errors.New("game not connected to the desktop channel")

// Processes lists the running game processes by PID with their importer key.
type Processes interface {
	GameProcesses(ctx context.Context, extra map[string][]string) (map[uint32]string, error)
}

type DialFunc func(ctx context.Context, pid uint32, handler func(Message)) (*Conn, error)

type Options struct {
	Log       *infra.Log
	Processes Processes
	// Dial defaults to the named pipe; tests connect to an in-memory server.
	Dial          DialFunc
	RetryInterval time.Duration
	IdleInterval  time.Duration
}

type Status string

const (
	StatusConnecting Status = "connecting"
	// StatusBusy means another client holds the pipe; the manager keeps retrying.
	StatusBusy         Status = "busy"
	StatusConnected    Status = "connected"
	StatusIncompatible Status = "incompatible"
)

type State struct {
	Importer string
	PID      uint32
	Status   Status
	// Info is the DLL's handshake reply while Status is connected.
	Info HelloReply
}

// Subscriber callbacks run outside the manager's lock. OnMessage gets what the
// DLL sends without a request, from that connection's read goroutine.
type Subscriber struct {
	OnMessage func(importer string, pid uint32, m Message)
	OnChange  func()
}

// Manager keeps one connection to every running game whose DLL serves the
// channel, and is the only owner of those connections.
type Manager struct {
	opts     Options
	throttle infra.DiagnosticThrottle
	wake     chan struct{}

	mu          sync.Mutex
	links       map[uint32]*link
	subscribers map[int]Subscriber
	nextSub     int
	cancel      context.CancelFunc
	done        chan struct{}
	dials       sync.WaitGroup
}

type link struct {
	pid      uint32
	importer string
	status   Status
	conn     *Conn
	dialing  bool
}

func NewManager(opts Options) *Manager {
	if opts.Dial == nil {
		opts.Dial = Dial
	}
	if opts.RetryInterval <= 0 {
		opts.RetryInterval = defaultRetryInterval
	}
	if opts.IdleInterval <= 0 {
		opts.IdleInterval = defaultIdleInterval
	}
	return &Manager{
		opts: opts, wake: make(chan struct{}, 1), links: map[uint32]*link{}, subscribers: map[int]Subscriber{},
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		m.run(ctx)
	}(m.done)
}

// Shutdown closes every connection and waits for the manager's goroutines.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.cancel, m.done = nil, nil
	m.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	m.dials.Wait()

	m.mu.Lock()
	links := m.links
	m.links = map[uint32]*link{}
	m.mu.Unlock()
	for _, l := range links {
		if l.conn != nil {
			_ = l.conn.Close()
		}
	}
}

// Wake makes the manager look for game processes now, for example when XXMI
// sees a game start or stop.
func (m *Manager) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) Subscribe(s Subscriber) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextSub
	m.nextSub++
	m.subscribers[id] = s
	return func() {
		m.mu.Lock()
		delete(m.subscribers, id)
		m.mu.Unlock()
	}
}

func (m *Manager) States() []State {
	m.mu.Lock()
	defer m.mu.Unlock()
	states := make([]State, 0, len(m.links))
	for _, l := range m.links {
		state := State{Importer: l.importer, PID: l.pid, Status: l.status}
		if l.conn != nil {
			state.Info = l.conn.Info()
		}
		states = append(states, state)
	}
	slices.SortFunc(states, func(a, b State) int {
		return cmp.Or(cmp.Compare(a.Importer, b.Importer), cmp.Compare(a.PID, b.PID))
	})
	return states
}

// Request sends a message to the connected game of an importer.
func (m *Manager) Request(ctx context.Context, importer string, msg Message) (Message, error) {
	conn := m.connected(importer)
	if conn == nil {
		return nil, ErrNotConnected
	}
	return conn.Request(ctx, msg)
}

func (m *Manager) connected(importer string) *Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pid := range slices.Sorted(maps.Keys(m.links)) {
		if l := m.links[pid]; l.importer == importer && l.conn != nil {
			return l.conn
		}
	}
	return nil
}

func (m *Manager) run(ctx context.Context) {
	for {
		pending := m.reconcile(ctx)
		interval := m.opts.IdleInterval
		if pending {
			interval = m.opts.RetryInterval
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// reconcile matches the links to the running game processes and dials the ones
// not connected. It reports whether a game is still waiting to connect.
func (m *Manager) reconcile(ctx context.Context) bool {
	processes, err := m.opts.Processes.GameProcesses(ctx, nil)
	m.throttle.Report(m.opts.Log, err, "GameLink.Reconcile", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "list-game-processes", Stage: "snapshot",
	})
	if err != nil {
		return true
	}

	m.mu.Lock()
	changed := false
	var closing []*Conn
	for pid, l := range m.links {
		if processes[pid] == l.importer {
			continue
		}
		if l.conn != nil {
			closing = append(closing, l.conn)
		}
		delete(m.links, pid)
		changed = true
	}
	pending := false
	for pid, importer := range processes {
		l := m.links[pid]
		if l == nil {
			l = &link{pid: pid, importer: importer, status: StatusConnecting}
			m.links[pid] = l
			changed = true
		}
		if l.status != StatusConnecting && l.status != StatusBusy {
			continue
		}
		pending = true
		if !l.dialing && ctx.Err() == nil {
			l.dialing = true
			m.dials.Add(1)
			go m.dial(ctx, l)
		}
	}
	m.mu.Unlock()

	for _, conn := range closing {
		_ = conn.Close()
	}
	if changed {
		m.notifyChange()
	}
	return pending
}

func (m *Manager) dial(ctx context.Context, l *link) {
	defer m.dials.Done()
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	conn, err := m.opts.Dial(dialCtx, l.pid, func(msg Message) { m.dispatch(l.importer, l.pid, msg) })
	cancel()
	if err == nil {
		// Keep callbacks within this dial's lifetime even after reconciliation removes the link.
		defer func() {
			_ = conn.Close()
			<-conn.readDone
		}()
	}

	m.mu.Lock()
	l.dialing = false
	current := m.links[l.pid] == l
	previous := l.status
	switch {
	case !current:
	case err == nil:
		l.conn, l.status = conn, StatusConnected
	case errors.Is(err, ErrIncompatible) || errors.Is(err, ErrForeignServer):
		l.status = StatusIncompatible
	case errors.Is(err, ErrPipeBusy):
		l.status = StatusBusy
	case errors.Is(err, ErrNotListening):
		l.status = StatusConnecting
	}
	changed := l.status != previous
	m.mu.Unlock()

	if err == nil && !current {
		_ = conn.Close()
		return
	}
	if err != nil {
		// A game without the channel never creates the pipe, and another client may hold it for a while;
		// both are waiting states rather than failures.
		if !errors.Is(err, ErrNotListening) && !errors.Is(err, ErrPipeBusy) && current {
			m.throttle.Report(m.opts.Log, err, "GameLink.Dial", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "connect-game", Stage: "dial",
				Fields: map[string]any{"importer": l.importer, "pid": l.pid},
			})
		}
		if changed {
			m.notifyChange()
		}
		return
	}

	m.notifyChange()
	select {
	case <-conn.Done():
	case <-ctx.Done():
		return
	}
	m.mu.Lock()
	if l.conn == conn {
		l.conn, l.status = nil, StatusConnecting
	}
	m.mu.Unlock()
	if err := conn.Err(); !errors.Is(err, ErrClosed) {
		m.throttle.Report(m.opts.Log, err, "GameLink.Connection", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "connect-game", Stage: "read",
			Fields: map[string]any{"importer": l.importer, "pid": l.pid},
		})
	}
	m.notifyChange()
	m.Wake()
}

func (m *Manager) dispatch(importer string, pid uint32, msg Message) {
	for _, s := range m.subscriberList() {
		if s.OnMessage != nil {
			s.OnMessage(importer, pid, msg)
		}
	}
}

func (m *Manager) notifyChange() {
	for _, s := range m.subscriberList() {
		if s.OnChange != nil {
			s.OnChange()
		}
	}
}

func (m *Manager) subscriberList() []Subscriber {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Collect(maps.Values(m.subscribers))
}
