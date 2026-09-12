package infra

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSystemProxyCredentialsAndCandidates(t *testing.T) {
	for _, value := range []string{"http://user:abc=def@127.0.0.1:7890", "https=http://user:abc=def@127.0.0.1:7890"} {
		candidates, err := parseSystemProxies(value, "https")
		if err != nil || len(candidates) != 1 || candidates[0] == nil {
			t.Fatalf("missing proxy: %v", err)
		}
		password, _ := candidates[0].User.Password()
		if password != "abc=def" {
			t.Fatal("proxy password changed")
		}
	}
	candidates, err := parseSystemProxies("first:80;second:81;DIRECT", "https")
	if err != nil || len(candidates) != 3 || candidates[0].Host != "first:80" || candidates[1].Host != "second:81" ||
		candidates[2] != nil {
		t.Fatal("candidate list lost")
	}
}

func TestSystemProxyPACFailover(t *testing.T) {
	origin := httptest.NewTLSServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(w, r.Body) }),
	)
	defer origin.Close()
	upstream := directProxyTestNetwork()
	upstream.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "https://"))
	}
	proxyRelay, err := StartProxyRelay(upstream, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxyRelay.Close() }()
	var proxyHits atomic.Int32
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		if r.Method == http.MethodConnect {
			proxyRelay.ServeHTTP(w, r)
			return
		}
		_, _ = io.Copy(w, r.Body)
	}))
	defer healthy.Close()
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddress := dead.Addr().String()
	_ = dead.Close()
	pac := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		_, _ = fmt.Fprintf(
			w,
			`function FindProxyForURL(url,host){return "PROXY %s; PROXY %s; DIRECT";}`,
			deadAddress,
			strings.TrimPrefix(healthy.URL, "http://"),
		)
	}))
	defer pac.Close()
	resolver := systemProxyResolver{readConfig: func() (systemProxyConfig, error) {
		return systemProxyConfig{autoConfigURL: pac.URL + "/proxy.pac", bypass: "<-loopback>"}, nil
	}, resolveAuto: resolveWindowsAutoProxy}
	network := directProxyTestNetwork()
	network.Transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	network.Transport.TLSClientConfig.ServerName = "example.com"
	network.useSystemProxy(resolver.proxiesForRequest)
	relay, err := StartProxyRelay(network, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	browser := network.Transport.Clone()
	endpoint, _ := url.Parse("http://" + relay.listener.Addr().String())
	browser.Proxy = http.ProxyURL(endpoint)
	defer browser.CloseIdleConnections()
	for _, transport := range []http.RoundTripper{network.HTTPTransport(), browser} {
		for _, scheme := range []string{"http", "https"} {
			// A non-rewindable body must survive a failed first dial without being consumed.
			body := &failoverTestBody{Reader: strings.NewReader("upload payload")}
			request, _ := http.NewRequest(http.MethodPost, scheme+"://destination.invalid/upload", body)
			response, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || string(data) != "upload payload" {
				t.Fatalf("body=%q err=%v", data, err)
			}
		}
	}
	if proxyHits.Load() != 4 {
		t.Fatalf("healthy proxy hits=%d", proxyHits.Load())
	}
	healthy.Close()
	// WinHTTP implicitly bypasses loopback during PAC evaluation. Freeze the
	// already resolved external-host candidates to test DIRECT fallback against
	// a reachable origin without relying on that native exception.
	external, _ := http.NewRequest(http.MethodGet, "https://destination.invalid/file", nil)
	candidates, err := resolver.proxiesForRequest(external)
	if err != nil {
		t.Fatal(err)
	}
	network.system.resolve = func(*http.Request) ([]*url.URL, error) { return candidates, nil }
	network.system.CloseIdleConnections()
	browser.CloseIdleConnections()
	for _, transport := range []http.RoundTripper{network.HTTPTransport(), browser} {
		response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusBadGateway {
				t.Fatal("DIRECT fallback used after failed proxies")
			}
		}
	}
}

func TestSystemProxyDirectFailureTriesNextCandidate(t *testing.T) {
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		_, _ = io.Copy(w, r.Body)
	}))
	defer proxy.Close()
	endpoint, _ := url.Parse(proxy.URL)
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddress := dead.Addr().String()
	_ = dead.Close()

	var directDials atomic.Int32
	network := directProxyTestNetwork()
	network.Transport.DialContext = func(ctx context.Context, protocol, address string) (net.Conn, error) {
		if strings.HasPrefix(address, "destination.invalid:") {
			directDials.Add(1)
			address = deadAddress
		}
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, protocol, address)
	}
	// A PAC list may prefer DIRECT and still name a proxy for when it fails.
	network.useSystemProxy(func(*http.Request) ([]*url.URL, error) { return []*url.URL{nil, endpoint}, nil })
	defer network.system.CloseIdleConnections()
	request, _ := http.NewRequest(
		http.MethodPost,
		"http://destination.invalid/upload",
		&failoverTestBody{Reader: strings.NewReader("upload payload")},
	)
	response, err := (&http.Client{Transport: network.HTTPTransport(), Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(data) != "upload payload" {
		t.Fatalf("body=%q err=%v", data, err)
	}
	if directDials.Load() != 1 || proxyHits.Load() != 1 {
		t.Fatalf("direct dials=%d proxy hits=%d", directDials.Load(), proxyHits.Load())
	}
}

type failoverTestBody struct {
	*strings.Reader
	closed atomic.Bool
}

func (b *failoverTestBody) Read(data []byte) (int, error) {
	if b.closed.Load() {
		return 0, fmt.Errorf("body already closed")
	}
	return b.Reader.Read(data)
}

func (b *failoverTestBody) Close() error { b.closed.Store(true); return nil }

func TestSystemProxyDoesNotReplayAfterConnection(t *testing.T) {
	var secondHits atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secondHits.Add(1) }))
	defer second.Close()
	for _, status := range []int{0, 407, 502} {
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
		}))
		a, _ := url.Parse(first.URL)
		b, _ := url.Parse(second.URL)
		network := directProxyTestNetwork()
		network.useSystemProxy(func(*http.Request) ([]*url.URL, error) { return []*url.URL{a, b}, nil })
		request, _ := http.NewRequest(
			http.MethodPost,
			"http://destination.invalid/",
			&failoverTestBody{Reader: strings.NewReader("once")},
		)
		response, _ := (&http.Client{Transport: network.HTTPTransport(), Timeout: time.Second}).Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		network.system.CloseIdleConnections()
		first.Close()
	}
	if secondHits.Load() != 0 {
		t.Fatal("request replayed after reaching first proxy")
	}
}
