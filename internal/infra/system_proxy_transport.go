package infra

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Each proxy has its own pool. Resolve before every request so Windows changes
// apply to new requests without replaying requests already sent to an origin.
type systemProxyTransport struct {
	base       *http.Transport
	resolve    func(*http.Request) ([]*url.URL, error)
	mu         sync.Mutex
	transports map[string]*http.Transport
}

func (s *systemProxyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	candidates, err := s.resolve(request)
	if err != nil {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, err
	}
	if len(candidates) == 0 {
		candidates = []*url.URL{nil}
	}
	var failures []error
	for _, endpoint := range candidates {
		if endpoint == nil && len(failures) > 0 {
			continue
		}
		var connected atomic.Bool
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }}
		attempt := request.Clone(httptrace.WithClientTrace(request.Context(), trace))
		// Transport closes a body even if dialing fails. Retain ownership until the
		// attempt acquires a connection; never replay a request after that point.
		if request.Body != nil {
			attempt.Body = proxyAttemptBody{request.Body, &connected}
		}
		response, err := s.transport(endpoint).RoundTrip(attempt)
		if err == nil {
			return response, nil
		}
		failures = append(failures, err)
		if connected.Load() {
			return nil, errors.Join(failures...)
		}
		// DIRECT is skipped once a proxy failed, but a failed DIRECT dial still
		// leaves the remaining candidates worth trying.
		if request.Context().Err() != nil || !proxyDialFailed(err) {
			break
		}
	}
	if request.Body != nil {
		_ = request.Body.Close()
	}
	return nil, errors.Join(failures...)
}

func proxyDialFailed(err error) bool {
	for err != nil {
		var operation *net.OpError
		if !errors.As(err, &operation) {
			return false
		}
		if operation.Op == "dial" {
			return true
		}
		err = operation.Err
	}
	return false
}

type proxyAttemptBody struct {
	io.ReadCloser
	connected *atomic.Bool
}

func (b proxyAttemptBody) Close() error {
	if b.connected.Load() {
		return b.ReadCloser.Close()
	}
	return nil
}

func (s *systemProxyTransport) transport(endpoint *url.URL) *http.Transport {
	key := ""
	if endpoint != nil {
		key = endpoint.String()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if transport := s.transports[key]; transport != nil {
		return transport
	}
	transport := s.base.Clone()
	transport.Proxy = http.ProxyURL(endpoint)
	if endpoint != nil {
		transport.DialContext = proxyConnectDial(transport.DialContext)
	}
	s.transports[key] = transport
	return transport
}

// Leave time for other PAC candidates within the request's overall deadline.
func proxyConnectDial(dial proxyDial) proxyDial {
	if dial == nil {
		dial = (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return dial(ctx, network, address)
	}
}

func (s *systemProxyTransport) CloseIdleConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, transport := range s.transports {
		transport.CloseIdleConnections()
	}
	s.base.CloseIdleConnections()
}
