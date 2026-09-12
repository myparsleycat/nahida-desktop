package app

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

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

func TestProxyStartupDisabledUsesSystemProxyAndIgnoresEnvironment(t *testing.T) {
	ctx := context.Background()
	settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = settings.Close() }()
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
		t.Fatal("WebView is not using the shared system proxy resolver")
	}
	// The shared client resolves the current user's Windows settings per request
	// and never consults the environment, so this test must not depend on the
	// host's proxy configuration. Requests through the resolver are covered in
	// internal/infra, where the OS boundary is pinned.
	transport := client.HTTPClient().Transport
	if _, direct := transport.(*http.Transport); direct {
		t.Fatal("Go client is missing the system proxy resolver")
	}
}
