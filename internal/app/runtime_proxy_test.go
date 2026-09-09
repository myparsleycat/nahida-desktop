package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

func TestProxyStartupBlocksDamagedSettingsAndFreezesArguments(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = settings.Close() }()
	damaged := "broken"
	if err := settings.Client().Settings.Upsert(ctx, "network_proxy", &damaged); err != nil {
		t.Fatal(err)
	}
	var args []string
	client := infra.NewClient()
	rt := &runtime{setting: settings, http: client}
	if err := rt.initProxy(ctx, func(value []string) error { args = value; return nil }); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.proxyRelay.Close() }()
	if _, ok := client.HTTPClient().Transport.(infra.BlockedProxyTransport); !ok {
		t.Fatal("damaged configuration permits external traffic")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--proxy-server=http://127.0.0.1:") || strings.Contains(joined, "direct://") {
		t.Fatal("WebView not fail closed")
	}
	if err := settings.SetProxySettings(
		ctx,
		setting.ProxySettingsInput{Type: "http", PasswordAction: "clear"},
	); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.HTTPClient().Transport.(infra.BlockedProxyTransport); !ok {
		t.Fatal("save changed live policy")
	}
	if response, err := client.HTTPClient().Get("http://127.0.0.1:1"); err == nil {
		_ = response.Body.Close()
		t.Fatal("blocked transport connected")
	}
}

func TestProxyStartupKeepsSharedClientIdentity(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = settings.Close() }()
	if err := settings.SetProxySettings(
		ctx,
		setting.ProxySettingsInput{Enabled: true, Type: "socks5h", Host: "127.0.0.1", Port: 1, PasswordAction: "clear"},
	); err != nil {
		t.Fatal(err)
	}
	client := infra.NewClient()
	retained := client.HTTPClient()
	rt := &runtime{setting: settings, http: client}
	if err := rt.initProxy(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.proxyRelay.Close() }()
	if retained != client.HTTPClient() {
		t.Fatal("streaming/downloader client identity changed")
	}
	if _, ok := retained.Transport.(*http.Transport); !ok {
		t.Fatal("missing upstream transport")
	}
}

func TestProxyStartupDisabledUsesRelayAndIgnoresEnvironment(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = settings.Close() }()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "direct")
	}))
	defer origin.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	var args []string
	client := infra.NewClient()
	rt := &runtime{setting: settings, http: client}
	if err := rt.initProxy(ctx, func(value []string) error { args = value; return nil }); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rt.proxyRelay.Close() }()
	if rt.proxyRelay == nil {
		t.Fatal("disabled proxy skipped the relay")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--proxy-server=http://127.0.0.1:") {
		t.Fatal("WebView still using the system proxy")
	}
	transport, ok := client.HTTPClient().Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("Go client still using the environment proxy")
	}
	response, err := (&http.Client{Transport: transport, Timeout: time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(body) != "direct" {
		t.Fatalf("body=%s err=%v", body, err)
	}
}
