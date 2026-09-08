package infra

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProxyRelay adapts WebView HTTP proxy traffic to the same upstream policy as Go.
type ProxyRelay struct {
	server    *http.Server
	listener  net.Listener
	transport http.RoundTripper
	dial      proxyDial
	log       *Log
	mu        sync.Mutex
	tunnels   map[net.Conn]io.Closer
	closed    bool
}

func StartProxyRelay(network *ProxyNetwork, log *Log) (*ProxyRelay, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r := &ProxyRelay{listener: listener, transport: BlockedProxyTransport{}, log: log, tunnels: make(map[net.Conn]io.Closer)}
	if network != nil {
		r.transport, r.dial = network.Transport, network.dial
	}
	r.server = &http.Server{Handler: r, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = ReportError(log, err, "ProxyRelay", Diagnostic{Operation: "serve"})
		}
	}()
	return r, nil
}

func (r *ProxyRelay) BrowserArguments() []string {
	return []string{
		"--proxy-server=http://" + r.listener.Addr().String(),
		"--proxy-bypass-list=<-loopback>;http://wails.localhost:80",
		"--host-resolver-rules=\"MAP * ~NOTFOUND, EXCLUDE 127.0.0.1, EXCLUDE wails.localhost\"",
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
	}
}

func (r *ProxyRelay) Close() error {
	err := r.server.Close()
	r.mu.Lock()
	r.closed = true
	for conn, upstream := range r.tunnels {
		_ = conn.Close()
		_ = upstream.Close()
	}
	r.mu.Unlock()
	if transport, ok := r.transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return err
}

func (r *ProxyRelay) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodConnect {
		host, port, err := net.SplitHostPort(request.Host)
		number, parseErr := strconv.Atoi(port)
		if err != nil || parseErr != nil || host == "" || number < 1 || number > 65535 || strings.ContainsAny(host, "/\\@?# \t\r\n") {
			http.Error(w, "invalid CONNECT target", http.StatusBadRequest)
			return
		}
		if r.dial == nil {
			http.Error(w, "proxy.configurationInvalid", http.StatusBadGateway)
			return
		}
		upstream, err := r.dial(request.Context(), "tcp", request.Host)
		if err != nil {
			r.fail(w, request, err)
			return
		}
		defer func() { _ = upstream.Close() }()
		r.tunnel(w, request, upstream, nil)
		return
	}
	if request.URL.Scheme != "http" || request.URL.Host == "" || request.URL.User != nil {
		http.Error(w, "invalid proxy request", http.StatusBadRequest)
		return
	}
	outgoing := request.Clone(request.Context())
	outgoing.RequestURI = ""
	upgrade := strings.EqualFold(outgoing.Header.Get("Upgrade"), "websocket")
	stripProxyHeaders(outgoing.Header)
	if upgrade {
		outgoing.Header.Set("Connection", "Upgrade")
		outgoing.Header.Set("Upgrade", "websocket")
	}
	response, err := r.transport.RoundTrip(outgoing)
	if err != nil {
		r.fail(w, request, err)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusSwitchingProtocols {
		stream, ok := response.Body.(io.ReadWriteCloser)
		if !ok || !upgrade {
			http.Error(w, "invalid upstream upgrade", http.StatusBadGateway)
			return
		}
		stripProxyHeaders(response.Header)
		response.Header.Set("Connection", "Upgrade")
		response.Header.Set("Upgrade", "websocket")
		r.tunnel(w, request, stream, response)
		return
	}
	stripProxyHeaders(response.Header)
	for key, values := range response.Header {
		w.Header()[key] = values
	}
	w.WriteHeader(response.StatusCode)
	// Flush each write so SSE and other streaming bodies are not buffered by the relay.
	if _, err := io.Copy(proxyFlushWriter{w}, response.Body); err != nil {
		// A normal return would emit a terminating chunk and hide the truncation.
		panic(http.ErrAbortHandler)
	}
}

type proxyFlushWriter struct{ http.ResponseWriter }

func (w proxyFlushWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	return n, err
}

func stripProxyHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for name := range strings.SplitSeq(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}

func (r *ProxyRelay) fail(w http.ResponseWriter, request *http.Request, err error) {
	_ = ReportError(r.log, err, "ProxyRelay", Diagnostic{Operation: "forward", Fields: map[string]any{"host": request.URL.Hostname(), "method": request.Method}})
	http.Error(w, "proxy.connectionFailed", http.StatusBadGateway)
}

func (r *ProxyRelay) tunnel(w http.ResponseWriter, request *http.Request, upstream io.ReadWriteCloser, response *http.Response) {
	conn, buffer, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.tunnels[conn] = upstream
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.tunnels, conn); r.mu.Unlock() }()
	stop := context.AfterFunc(request.Context(), func() { _ = conn.Close(); _ = upstream.Close() })
	defer stop()
	if response == nil {
		_, err = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	} else {
		_, err = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
		if err == nil {
			err = response.Header.Write(buffer)
		}
		if err == nil {
			_, err = buffer.WriteString("\r\n")
		}
	}
	if err != nil || buffer.Flush() != nil {
		return
	}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, buffer); _ = upstream.Close(); close(done) }()
	_, _ = io.Copy(conn, upstream)
	_ = conn.Close()
	_ = upstream.Close()
	<-done
}
