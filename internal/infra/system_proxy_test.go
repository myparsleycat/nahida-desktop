package infra

import (
	"context"
	"errors"
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

func TestSystemProxySelection(t *testing.T) {
	for _, tc := range []struct {
		name, value, scheme, want string
		invalid                   bool
	}{
		{"none", "", "https", "", false},
		{"shared", "127.0.0.1:7890", "https", "http://127.0.0.1:7890", false},
		{"scheme", "http=one:80;https=two:8080", "https", "http://two:8080", false},
		{"scheme default port", "https=two", "https", "http://two:80", false},
		{"unmapped scheme", "http=one:80", "https", "", false},
		{"TLS proxy", "https=https://secure:443", "https", "https://secure:443", false},
		{"SOCKS5", "socks=socks5://localhost:1080", "https", "socks5://localhost:1080", false},
		{"IPv6", "[::1]:7890", "https", "http://[::1]:7890", false},
		{"ordered proxies", "first:80 second:81", "http", "http://first:80", false},
		{"bad port", "host:99999", "https", "", true},
		{"bad URL", "http://host/path", "https", "", true},
		{"empty endpoint", "https=", "https", "", true},
		{"legacy SOCKS", "socks=localhost:1080", "https", "", true},
		{"redact credentials", "ftp://secret:password@host", "https", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, err := parseSystemProxy(tc.value, tc.scheme)
			if (err != nil) != tc.invalid {
				t.Fatalf("endpoint=%v err=%v", endpoint, err)
			}
			if err != nil {
				if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "password") {
					t.Fatal("proxy credentials exposed")
				}
				return
			}
			got := ""
			if endpoint != nil {
				got = endpoint.String()
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSystemProxyBypass(t *testing.T) {
	for _, tc := range []struct {
		target, list string
		want         bool
	}{
		{"http://intranet/", "<local>", true},
		{"http://example.com/", "<local>", false},
		{"http://10.0.0.1/", "<local>", false},
		{"https://api.example.com/", "*.example.com;localhost", true},
		{"https://example.com/", "*.example.com", false},
		{"https://EXAMPLE.COM/", "https://example.com:443", true},
		{"http://example.com/", "https://example.com:443", false},
		{"http://example.com:8080/", "example.com:80", false},
		{"http://10.2.3.4/", "10.*;192.168.*", true},
		{"http://10.2.3.4/", "10.0.0.0/8", true},
		{"http://[::1]:8080/", "[::1]:8080", true},
		{"http://[::1]:8080/", "[::1]", true},
		{"https://notexample.com/", "example.com", false},
	} {
		t.Run(tc.target+"/"+tc.list, func(t *testing.T) {
			target, _ := url.Parse(tc.target)
			if got := systemProxyBypass(target, tc.list); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestSystemProxyAutomaticPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  systemProxyConfig
		auto    string
		autoErr error
		want    string
		wantErr bool
	}{
		{"PAC proxy", systemProxyConfig{autoConfigURL: "http://pac/config"}, "proxy:80", nil, "http://proxy:80", false},
		{"PAC direct", systemProxyConfig{autoConfigURL: "http://pac/config", proxy: "static:80"}, "DIRECT", nil, "", false},
		{"PAC failure blocks", systemProxyConfig{autoConfigURL: "http://pac/config"}, "", errors.New("bad script"), "", true},
		{"PAC failure uses static", systemProxyConfig{autoConfigURL: "http://pac/config", proxy: "static:80"}, "", errors.New("bad script"), "http://static:80", false},
		{"WPAD absent", systemProxyConfig{autoDetect: true}, "", errNoAutoProxy, "", false},
		{"WPAD absent uses static", systemProxyConfig{autoDetect: true, proxy: "static:80"}, "", errNoAutoProxy, "http://static:80", false},
		{"WPAD discovered but broken", systemProxyConfig{autoDetect: true}, "", errors.New("download failed"), "", true},
		{"explicit PAC not discovery", systemProxyConfig{autoConfigURL: "http://pac/config", autoDetect: true}, "", errNoAutoProxy, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := systemProxyResolver{
				readConfig:  func() (systemProxyConfig, error) { return tc.config, nil },
				resolveAuto: func(context.Context, *url.URL, systemProxyConfig) (string, error) { return tc.auto, tc.autoErr },
			}
			request, _ := http.NewRequest(http.MethodGet, "https://destination.invalid/file", nil)
			endpoint, err := resolver.proxyForRequest(request)
			if (err != nil) != tc.wantErr {
				t.Fatalf("endpoint=%v err=%v", endpoint, err)
			}
			got := ""
			if endpoint != nil {
				got = endpoint.String()
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestSystemProxyConfigurationRefreshAndFailure(t *testing.T) {
	config := systemProxyConfig{proxy: "first:80"}
	var readErr error
	resolver := systemProxyResolver{readConfig: func() (systemProxyConfig, error) { return config, readErr }}
	request, _ := http.NewRequest(http.MethodGet, "https://destination.invalid/", nil)
	for _, address := range []string{"first:80", "second:81", ""} {
		config.proxy = address
		endpoint, err := resolver.proxyForRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		if address == "" {
			if endpoint != nil {
				t.Fatal("removed system proxy retained")
			}
		} else if endpoint == nil || endpoint.Host != address {
			t.Fatalf("stale system proxy: %v", endpoint)
		}
	}
	readErr = errors.New("settings unavailable")
	if _, err := resolver.proxyForRequest(request); !errors.Is(err, readErr) {
		t.Fatalf("read failure became direct: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.proxyForRequest(request.WithContext(ctx)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func directProxyTestNetwork() *ProxyNetwork {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &ProxyNetwork{Transport: transport, dial: (&net.Dialer{Timeout: time.Second}).DialContext}
}

func TestSystemProxyHTTPAndHTTPSUseSamePolicy(t *testing.T) {
	var originHits, proxyHits, directHits atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originHits.Add(1)
		_, _ = io.WriteString(w, "proxied")
	}))
	defer origin.Close()
	upstream := directProxyTestNetwork()
	// The test proxy maps a non-resolving hostname to a local TLS server.
	upstream.dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(origin.URL, "https://"))
	}
	proxyRelay, err := StartProxyRelay(upstream, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proxyRelay.Close() }()
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		if r.Method == http.MethodConnect {
			proxyRelay.ServeHTTP(w, r)
			return
		}
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxyServer.Close()
	config := systemProxyConfig{proxy: proxyServer.URL}
	resolver := systemProxyResolver{readConfig: func() (systemProxyConfig, error) { return config, nil }}
	network := directProxyTestNetwork()
	network.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if strings.HasPrefix(address, "destination.invalid:") {
			directHits.Add(1)
			address = strings.TrimPrefix(origin.URL, "https://")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	network.Transport.DialContext = network.dial
	network.useSystemProxy(resolver.proxiesForRequest)
	// Preserve httptest's certificate trust and use its certificate's DNS name.
	network.Transport.TLSClientConfig = origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	network.Transport.TLSClientConfig.ServerName = "example.com"
	relay, err := StartProxyRelay(network, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	browserTransport := network.Transport.Clone()
	browserEndpoint, _ := url.Parse("http://" + relay.listener.Addr().String())
	browserTransport.Proxy = http.ProxyURL(browserEndpoint)
	defer browserTransport.CloseIdleConnections()
	for _, transport := range []http.RoundTripper{network.HTTPTransport(), browserTransport} {
		client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
		for _, scheme := range []string{"http", "https"} {
			response, err := client.Get(scheme + "://destination.invalid/file")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || string(body) != "proxied" {
				t.Fatalf("body=%q err=%v", body, err)
			}
		}
	}
	if proxyHits.Load() != 4 || originHits.Load() != 2 {
		t.Fatalf("proxy=%d origin=%d", proxyHits.Load(), originHits.Load())
	}

	// A dead selected proxy must not cause a direct connection, even when the
	// direct destination is reachable through our injected test dialer.
	proxyServer.Close()
	network.system.CloseIdleConnections()
	browserTransport.CloseIdleConnections()
	for _, transport := range []http.RoundTripper{network.HTTPTransport(), browserTransport} {
		for _, scheme := range []string{"http", "https"} {
			response, err := (&http.Client{Transport: transport, Timeout: time.Second}).Get(
				scheme + "://destination.invalid/file",
			)
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode != http.StatusBadGateway {
					t.Fatal("dead system proxy was bypassed")
				}
			}
		}
	}
	if directHits.Load() != 0 || originHits.Load() != 2 {
		t.Fatal("system proxy failure leaked direct traffic")
	}
}

// Single-result assertions in the selection tests inspect the first candidate.
func parseSystemProxy(value, scheme string) (*url.URL, error) {
	candidates, err := parseSystemProxies(value, scheme)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	return candidates[0], nil
}

func (s systemProxyResolver) proxyForRequest(request *http.Request) (*url.URL, error) {
	candidates, err := s.proxiesForRequest(request)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	return candidates[0], nil
}
