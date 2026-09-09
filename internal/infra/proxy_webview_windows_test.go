//go:build windows && proxyintegration

package infra

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

// Run explicitly with: go test -tags proxyintegration ./internal/infra -run TestProxyNativeWebView -count=1
// Every case runs in its own process and isolated WebView profile, with hidden windows.
func TestProxyNativeWebView(t *testing.T) {
	kind := os.Getenv("NAHIDA_PROXY_WEBVIEW_TEST")
	if kind == "" {
		for _, kind := range []string{"http", "socks5", "socks5h"} {
			t.Run(kind, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProxyNativeWebView$", "-test.v")
				command.Env = append(
					os.Environ(),
					"NAHIDA_PROXY_WEBVIEW_TEST="+kind,
					"NAHIDA_PROXY_WEBVIEW_PROFILE="+t.TempDir(),
				)
				t.Cleanup(func() { time.Sleep(time.Second) }) // Wait for browser descendants to exit after the helper.
				command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("WebView integration: %v\n%s", err, output)
				}
				t.Log(string(output))
			})
		}
		return
	}
	// testing runs this function on a worker goroutine, unlike the real main.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, 2); err != nil {
		t.Fatal(err)
	}
	defer windows.CoUninitialize()
	var app *application.App
	var completed atomic.Int32
	var failed atomic.Bool
	var originURL string
	script := func(id string) string {
		return fmt.Sprintf(`<!doctype html><html><body>Proxy integration<script>
  const base=%q, id=%q;
  (async()=>{
   await new Promise((resolve,reject)=>{const image=new Image();image.onload=resolve;image.onerror=reject;image.src=base+'/image';document.body.append(image)});
   const result=await fetch(base+'/fetch');if(await result.text()!=='proxied')throw Error('fetch');
   await new Promise((resolve,reject)=>{const source=new EventSource(base+'/sse');source.onmessage=()=>{source.close();resolve()};source.onerror=()=>{source.close();reject(Error('SSE'))}});
   await new Promise((resolve,reject)=>{const ws=new WebSocket(base.replace('http:','ws:')+'/ws');ws.onopen=()=>ws.send('hello');ws.onmessage=(event)=>{ws.close();event.data==='hello'?resolve():reject(Error('echo'))};ws.onerror=()=>reject(Error('websocket'))});
   await fetch(base+'/done?id='+id);
  })().catch(error=>fetch(base+'/fail?error='+encodeURIComponent(String(error))));
  </script></body></html>`, originURL, id)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log("origin request", r.Method, r.URL.Path)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, script("remote"))
		case "/image":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = io.WriteString(
				w,
				`<svg xmlns="http://www.w3.org/2000/svg" width="2" height="2"><rect width="2" height="2" fill="red"/></svg>`,
			)
		case "/fetch":
			_, _ = io.WriteString(w, "proxied")
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: ok\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/ws":
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
			if err != nil {
				return
			}
			defer func() { _ = conn.CloseNow() }()
			kind, data, err := conn.Read(r.Context())
			if err == nil {
				_ = conn.Write(r.Context(), kind, data)
			}
		case "/done":
			if completed.Add(1) == 2 {
				go app.Quit()
			}
		case "/fail":
			failed.Store(true)
			t.Log("browser failure:", r.URL.Query().Get("error"))
			go app.Quit()
		}
	}))
	defer origin.Close()
	originURL = strings.Replace(origin.URL, "127.0.0.1", "localhost", 1)
	var network *ProxyNetwork
	var err error
	var proxyHits atomic.Int32
	var destinations <-chan string
	if kind == "http" {
		var direct *ProxyNetwork
		direct, err = NewProxyNetwork(ProxyConfig{Type: "http"})
		if err != nil {
			t.Fatal(err)
		}
		var upstream *ProxyRelay
		upstream, err = StartProxyRelay(direct, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = upstream.Close() }()
		proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Log("upstream proxy request", r.Method, r.URL.Host, r.URL.Path)
			if r.Host != strings.TrimPrefix(originURL, "http://") {
				http.Error(w, "test destination denied", http.StatusBadGateway)
				return
			}
			if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
				w.WriteHeader(407)
				return
			}
			proxyHits.Add(1)
			upstream.ServeHTTP(w, r)
		}))
		defer proxyServer.Close()
		config := proxyConfigFor(strings.TrimPrefix(proxyServer.URL, "http://"), kind)
		config.Username, config.Password = "user", "pass"
		network, err = NewProxyNetwork(config)
	} else {
		address, seen := socksTestServer(t, strings.TrimPrefix(origin.URL, "http://"), "user", "pass")
		destinations = seen
		config := proxyConfigFor(address, kind)
		config.Username, config.Password = "user", "pass"
		network, err = NewProxyNetwork(config)
	}
	if err != nil {
		t.Fatal(err)
	}
	relay, err := StartProxyRelay(network, NewLogWithOptions(LogOptions{Writer: os.Stdout, DisableFile: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	profile := os.Getenv("NAHIDA_PROXY_WEBVIEW_PROFILE")
	app = application.New(
		application.Options{
			Name:    "Nahida proxy integration",
			Windows: application.WindowsOptions{WebviewUserDataPath: profile, DisableQuitOnLastWindowClosed: true},
			Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				t.Log("asset request", r.URL.Path)
				_, _ = io.WriteString(w, script("main"))
			})},
		},
	)
	if err := app.SetWindowsBrowserArguments(relay.BrowserArguments()); err != nil {
		t.Fatal(err)
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		app.Window.NewWithOptions(
			application.WebviewWindowOptions{Title: "Proxy test main", URL: "/", Hidden: true, Width: 400, Height: 300},
		)
		app.Window.NewWithOptions(
			application.WebviewWindowOptions{
				Title:               "Proxy test remote",
				URL:                 originURL + "/page",
				Hidden:              true,
				Width:               400,
				Height:              300,
				DisableWailsRuntime: true,
			},
		)
	})
	timeout := time.AfterFunc(25*time.Second, func() { failed.Store(true); app.Quit() })
	defer timeout.Stop()
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if failed.Load() || completed.Load() != 2 {
		t.Fatalf("completed=%d failed=%v", completed.Load(), failed.Load())
	}
	if kind == "http" && proxyHits.Load() < 9 {
		t.Fatalf("too few proxied requests: %d", proxyHits.Load())
	}
	if destinations != nil {
		_, expectedPort, _ := net.SplitHostPort(strings.TrimPrefix(originURL, "http://"))
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		var collected []string
		matched := false
		for !matched {
			select {
			case dest := <-destinations:
				collected = append(collected, dest)
				_, port, _ := net.SplitHostPort(dest)
				matched = port == expectedPort
			case <-timer.C:
				t.Fatal("WebView bypassed SOCKS proxy")
			}
		}
	drain:
		for {
			select {
			case dest := <-destinations:
				collected = append(collected, dest)
			default:
				break drain
			}
		}
		proxied := 0
		for _, dest := range collected {
			host, port, _ := net.SplitHostPort(dest)
			if port != expectedPort {
				continue
			}
			proxied++
			if kind == "socks5h" && host != "localhost" {
				t.Fatal("WebView SOCKS5h resolved destination locally")
			}
			if kind == "socks5" && net.ParseIP(host) == nil {
				t.Fatal("WebView SOCKS5 did not resolve destination locally")
			}
		}
		if proxied == 0 {
			t.Fatal("WebView bypassed SOCKS proxy")
		}
	}
	t.Logf("%s: both native WebViews loaded images, fetch, SSE and WebSocket through authenticated proxy", kind)
}
