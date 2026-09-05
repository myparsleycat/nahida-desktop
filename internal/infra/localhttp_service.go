package infra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type LocalHTTPOptions struct {
	Address       string
	Version       string
	HandleMessage func(context.Context, []byte) (string, error)
	Log           *Log
}

type LocalHTTP struct {
	mu       sync.Mutex
	opts     LocalHTTPOptions
	server   *http.Server
	listener net.Listener
	runCtx   context.Context
	cancel   context.CancelFunc
}

func NewLocalHTTP() *LocalHTTP { return NewLocalHTTPWithOptions(LocalHTTPOptions{}) }

func NewLocalHTTPWithOptions(opts LocalHTTPOptions) *LocalHTTP {
	if opts.Address == "" {
		opts.Address = "[::1]:1027"
	}
	return &LocalHTTP{opts: opts}
}

// UseHandler wires the application-level CBOR message handler before Start.
//
//wails:ignore
func (s *LocalHTTP) UseHandler(handler func(context.Context, []byte) (string, error)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.opts.HandleMessage = handler
	s.mu.Unlock()
}

// Start binds the extension bridge. It is idempotent.
//
//wails:ignore
func (s *LocalHTTP) Start() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}
	listener, err := net.Listen("tcp", s.opts.Address)
	if err != nil {
		return fmt.Errorf("listen local HTTP bridge: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", s.withCORS(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, s.opts.Version) }))
	mux.HandleFunc("/ping", s.withCORS(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "pong") }))
	mux.HandleFunc("/ws", s.withCORS(s.handleWebSocket))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	runCtx, cancel := context.WithCancel(context.Background())
	log := s.opts.Log
	s.server, s.listener = server, listener
	s.runCtx, s.cancel = runCtx, cancel
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			_ = ReportError(log, serveErr, "LocalHTTP", Diagnostic{
				Severity: DiagnosticError, Operation: "serve", Stage: "listener",
			})
		}
	}()
	return nil
}

func (s *LocalHTTP) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *LocalHTTP) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(32 << 20)
	s.mu.Lock()
	readCtx := s.runCtx
	s.mu.Unlock()
	if readCtx == nil {
		readCtx = r.Context()
	}
	for {
		messageType, payload, readErr := conn.Read(readCtx)
		if readErr != nil {
			s.reportWebSocketFailure(readCtx, readErr, "read")
			return
		}
		if messageType != websocket.MessageBinary {
			s.reportWebSocketFailure(readCtx, conn.Write(readCtx, websocket.MessageText, []byte("invalid data")), "write-validation-response")
			continue
		}
		s.mu.Lock()
		handler, log := s.opts.HandleMessage, s.opts.Log
		s.mu.Unlock()
		response := "error"
		if handler != nil {
			result, handleErr := handler(readCtx, payload)
			if result != "" {
				response = result
			}
			if handleErr != nil {
				_ = ReportError(log, handleErr, "LocalHTTP", Diagnostic{
					Severity: DiagnosticError, Operation: "websocket-download", Stage: "handle-message",
				})
			}
		}
		if err := conn.Write(readCtx, websocket.MessageText, []byte(response)); err != nil {
			s.reportWebSocketFailure(readCtx, err, "write-response")
			return
		}
	}
}

// ServiceShutdown stops accepting connections and waits for handlers.
//
//wails:ignore
func (s *LocalHTTP) ServiceShutdown() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	server := s.server
	cancel := s.cancel
	s.server, s.listener, s.runCtx, s.cancel = nil, nil, nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

//wails:ignore
func (s *LocalHTTP) Address() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.opts.Address
}

func (s *LocalHTTP) reportWebSocketFailure(ctx context.Context, err error, stage string) {
	if err == nil || ctx.Err() != nil {
		return
	}
	status := websocket.CloseStatus(err)
	if status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway {
		return
	}
	s.mu.Lock()
	log := s.opts.Log
	s.mu.Unlock()
	_ = ReportError(log, err, "LocalHTTP", Diagnostic{Severity: DiagnosticWarn, Operation: "websocket-download", Stage: stage, Fields: map[string]any{"closeStatus": status}})
}
