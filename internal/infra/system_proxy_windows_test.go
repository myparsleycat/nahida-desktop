package infra

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestWindowsProxyConfigurationRead(t *testing.T) {
	// Read-only smoke test for the ABI; never mutate or log the user's settings.
	if _, err := readWindowsProxyConfig(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsPACUnsupportedProtocolsFailClosed(t *testing.T) {
	for i, directive := range []string{"SOCKS5 127.0.0.1:7890", "SOCKS 127.0.0.1:7890", "SOCKS 127.0.0.1:7890; DIRECT"} {
		t.Run(directive, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
				_, _ = fmt.Fprintf(w, `function FindProxyForURL(url,host){return %q;}`, directive)
			}))
			defer server.Close()
			pacURL := fmt.Sprintf("%s/proxy-%d-%d.pac", server.URL, time.Now().UnixNano(), i)
			resolver := systemProxyResolver{readConfig: func() (systemProxyConfig, error) {
				return systemProxyConfig{autoConfigURL: pacURL, proxy: "127.0.0.1:1"}, nil
			}, resolveAuto: resolveWindowsAutoProxy}
			host := fmt.Sprintf("unsupported-%d-%d.invalid", time.Now().UnixNano(), i)
			request, _ := http.NewRequest(http.MethodGet, "https://"+host+"/file", nil)
			_, err := resolver.proxiesForRequest(request)
			if !errors.Is(err, errUnsupportedPAC) {
				t.Fatalf("unsupported PAC became direct/static: %v", err)
			}
		})
	}
}

func TestWindowsAutoProxyPAC(t *testing.T) {
	tag := time.Now().UnixNano()
	directHost := fmt.Sprintf("direct-%d.invalid", tag)
	proxiedHost := fmt.Sprintf("proxied-%d.invalid", tag)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		_, _ = fmt.Fprintf(w, `function FindProxyForURL(url, host) {
		if (host === %q) return "DIRECT";
		return "PROXY 127.0.0.1:7890";
		}`, directHost)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pacURL := fmt.Sprintf("%s/proxy-%d.pac", server.URL, tag)
	for _, tc := range []struct{ address, want string }{
		{"https://" + proxiedHost + "/file", "http://127.0.0.1:7890"},
		{"http://" + directHost + "/file", "DIRECT"},
	} {
		target, _ := url.Parse(tc.address)
		got, err := resolveWindowsAutoProxy(ctx, target, systemProxyConfig{autoConfigURL: pacURL})
		if err != nil || got != tc.want {
			t.Fatalf("proxy=%q want=%q err=%v", got, tc.want, err)
		}
	}
}

func TestWindowsAutoProxyCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target, _ := url.Parse(fmt.Sprintf("https://canceled-%d.invalid/", time.Now().UnixNano()))
	_, err := resolveWindowsAutoProxy(ctx, target, systemProxyConfig{autoConfigURL: "http://127.0.0.1:1/proxy.pac"})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
