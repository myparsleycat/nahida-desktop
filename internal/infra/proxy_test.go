package infra

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func socksTestServer(t *testing.T, target, user, password string) (string, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	destinations := make(chan string, 32)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				header := make([]byte, 2)
				if _, err := io.ReadFull(reader, header); err != nil {
					return
				}
				methods := make([]byte, int(header[1]))
				if _, err := io.ReadFull(reader, methods); err != nil {
					return
				}
				method := byte(0)
				if user != "" {
					method = 2
				}
				if _, err := conn.Write([]byte{5, method}); err != nil {
					return
				}
				if method == 2 {
					if _, err := io.ReadFull(reader, header); err != nil {
						return
					}
					gotUser := make([]byte, int(header[1]))
					if _, err := io.ReadFull(reader, gotUser); err != nil {
						return
					}
					size, err := reader.ReadByte()
					if err != nil {
						return
					}
					gotPassword := make([]byte, int(size))
					if _, err := io.ReadFull(reader, gotPassword); err != nil {
						return
					}
					status := byte(0)
					if string(gotUser) != user || string(gotPassword) != password {
						status = 1
					}
					if _, err := conn.Write([]byte{1, status}); err != nil || status != 0 {
						return
					}
				}
				request := make([]byte, 4)
				if _, err := io.ReadFull(reader, request); err != nil {
					return
				}
				size := 4
				switch request[3] {
				case 3:
					length, err := reader.ReadByte()
					if err != nil {
						return
					}
					size = int(length)
				case 4:
					size = 16
				}
				host := make([]byte, size)
				if _, err := io.ReadFull(reader, host); err != nil {
					return
				}
				port := make([]byte, 2)
				if _, err := io.ReadFull(reader, port); err != nil {
					return
				}
				destination := string(host)
				if request[3] != 3 {
					destination = net.IP(host).String()
				}
				destinations <- net.JoinHostPort(destination, strconv.Itoa(int(binary.BigEndian.Uint16(port))))
				upstream, err := net.DialTimeout("tcp", target, time.Second)
				if err != nil {
					return
				}
				defer func() { _ = upstream.Close() }()
				if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80}); err != nil {
					return
				}
				done := make(chan struct{})
				go func() { _, _ = io.Copy(upstream, reader); _ = upstream.Close(); close(done) }()
				_, _ = io.Copy(conn, upstream)
				_ = conn.Close()
				<-done
			}()
		}
	}()
	return listener.Addr().String(), destinations
}

func receiveDestination(t *testing.T, destinations <-chan string) string {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case destination := <-destinations:
		return destination
	case <-timer.C:
		t.Fatal("no SOCKS destination")
		return ""
	}
}

func proxyConfigFor(address, kind string) ProxyConfig {
	host, port, _ := net.SplitHostPort(address)
	number, _ := strconv.Atoi(port)
	return ProxyConfig{Enabled: true, Type: kind, Host: host, Port: number}
}

func TestDisabledProxyIgnoresEnvironmentProxy(t *testing.T) {
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
	network, err := NewProxyNetwork(ProxyConfig{Type: "http"})
	if err != nil {
		t.Fatal(err)
	}
	defer network.Transport.CloseIdleConnections()
	if network.Transport.Proxy != nil {
		t.Fatal("disabled proxy still has a Proxy func")
	}
	response, err := (&http.Client{Transport: network.Transport, Timeout: time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || string(body) != "direct" {
		t.Fatalf("body=%s err=%v", body, err)
	}
}

func TestSOCKSDNSAndAuthentication(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "destination.invalid" {
			t.Errorf("Host changed: %s", r.Host)
		}
		_, _ = io.WriteString(w, "proxied")
	}))
	defer origin.Close()
	for _, kind := range []string{"socks5", "socks5h"} {
		for _, auth := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/auth=%v", kind, auth), func(t *testing.T) {
				user, password := "", ""
				if auth {
					user, password = "user", "password"
				}
				address, destinations := socksTestServer(t, strings.TrimPrefix(origin.URL, "http://"), user, password)
				config := proxyConfigFor(address, kind)
				config.Username, config.Password = user, password
				var lookups atomic.Int32
				network, err := newProxyNetwork(config, func(context.Context, string) ([]net.IPAddr, error) {
					lookups.Add(1)
					return []net.IPAddr{{IP: net.ParseIP("127.0.0.2")}}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				defer network.Transport.CloseIdleConnections()
				response, err := (&http.Client{Transport: network.Transport, Timeout: 3 * time.Second}).Get(
					"http://destination.invalid",
				)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if err != nil || string(body) != "proxied" {
					t.Fatalf("body=%s err=%v", body, err)
				}
				destination := receiveDestination(t, destinations)
				if kind == "socks5" && (destination != "127.0.0.2:80" || lookups.Load() != 1) {
					t.Fatalf("local DNS: %s lookups=%d", destination, lookups.Load())
				}
				if kind == "socks5h" && (destination != "destination.invalid:80" || lookups.Load() != 0) {
					t.Fatalf("remote DNS: %s lookups=%d", destination, lookups.Load())
				}
				if auth {
					config.Password = "wrong"
					rejected, err := NewProxyNetwork(config)
					if err != nil {
						t.Fatal(err)
					}
					defer rejected.Transport.CloseIdleConnections()
					if unexpected, err := (&http.Client{Transport: rejected.Transport, Timeout: time.Second}).Get(
						origin.URL,
					); err == nil {
						_ = unexpected.Body.Close()
						t.Fatal("incorrect password accepted")
					}
				}
			})
		}
	}
}

func TestHTTPProxyRelayTLSRedirectAndNoBypass(t *testing.T) {
	var directHits atomic.Int32
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to origin")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/done", http.StatusFound)
			return
		}
		_, _ = io.Copy(w, r.Body)
	}))
	defer origin.Close()
	directNetwork, err := NewProxyNetwork(ProxyConfig{Type: "http"})
	if err != nil {
		t.Fatal(err)
	}
	upstreamRelay, err := StartProxyRelay(directNetwork, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = upstreamRelay.Close() }()
	var proxyHits atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		upstreamRelay.ServeHTTP(w, r)
	}))
	config := proxyConfigFor(strings.TrimPrefix(proxyServer.URL, "http://"), "http")
	config.Username, config.Password = "user", "pass"
	t.Setenv("NO_PROXY", "*")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	network, err := NewProxyNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	relay, err := StartProxyRelay(network, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	endpoint, _ := url.Parse("http://" + relay.listener.Addr().String())
	transport := origin.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(endpoint)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get(origin.URL + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if proxyHits.Load() == 0 || directHits.Load() != 2 {
		t.Fatalf("proxy=%d origin=%d", proxyHits.Load(), directHits.Load())
	}
	transport.CloseIdleConnections()
	proxyServer.Close()
	before := directHits.Load()
	if unexpected, err := client.Get(origin.URL); err == nil {
		_ = unexpected.Body.Close()
		t.Fatal("request succeeded with dead proxy")
	}
	if directHits.Load() != before {
		t.Fatal("fell back to direct")
	}
}

func TestRelayHTTPStreamingAndWebSocket(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.CloseNow() }()
			kind, data, err := conn.Read(r.Context())
			if err == nil {
				_ = conn.Write(r.Context(), kind, data)
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer origin.Close()
	config := ProxyConfig{Type: "http"}
	network, err := NewProxyNetwork(config)
	if err != nil {
		t.Fatal(err)
	}
	relay, err := StartProxyRelay(network, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	endpoint, _ := url.Parse("http://" + relay.listener.Addr().String())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(endpoint)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, origin.URL, nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("stream: %q %v", line, err)
	}
	cancel()
	_ = response.Body.Close()
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer wsCancel()
	conn, handshake, err := websocket.Dial(wsCtx, origin.URL+"/ws", &websocket.DialOptions{HTTPClient: client})
	if handshake != nil && handshake.Body != nil {
		defer func() { _ = handshake.Body.Close() }()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := conn.Write(wsCtx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, message, err := conn.Read(wsCtx)
	if err != nil || string(message) != "hello" {
		t.Fatalf("websocket: %q %v", message, err)
	}
}

func TestProxyRelayAbortsTruncatedChunkedResponse(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n")
	}))
	defer origin.Close()
	network, err := NewProxyNetwork(ProxyConfig{Type: "http"})
	if err != nil {
		t.Fatal(err)
	}
	relay, err := StartProxyRelay(network, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	endpoint, _ := url.Parse("http://" + relay.listener.Addr().String())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(endpoint)
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err == nil {
		t.Fatalf("truncated body treated as success: %q", body)
	}
}

func TestProxyIPv6AndTLSName(t *testing.T) {
	origin := httptest.NewTLSServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.TLS.ServerName) }),
	)
	defer origin.Close()
	address, destinations := socksTestServer(t, strings.TrimPrefix(origin.URL, "https://"), "", "")
	network, err := newProxyNetwork(
		proxyConfigFor(address, "socks5"),
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("::1")}}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// The test certificate is for example.com, not the resolved IP.
	network.Transport.TLSClientConfig = &tls.Config{
		RootCAs: origin.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs,
	}
	defer network.Transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: network.Transport, Timeout: 3 * time.Second}).Get("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "example.com" || receiveDestination(t, destinations) != "[::1]:443" {
		t.Fatal("lost TLS SNI or IPv6 SOCKS target")
	}
}

func TestProxyApplicationDownloadRangeAndUpload(t *testing.T) {
	data := bytes.Repeat([]byte("proxy-data"), 900000)
	var ranges atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			_, _ = io.Copy(w, r.Body)
			return
		}
		if r.Header.Get("Range") != "" {
			ranges.Add(1)
		}
		http.ServeContent(w, r, "file.bin", time.Time{}, bytes.NewReader(data))
	}))
	defer origin.Close()
	address, _ := socksTestServer(t, strings.TrimPrefix(origin.URL, "http://"), "", "")
	network, err := NewProxyNetwork(proxyConfigFor(address, "socks5h"))
	if err != nil {
		t.Fatal(err)
	}
	defer network.Transport.CloseIdleConnections()
	client := NewClient()
	downloader := NewDownload()
	downloader.UseClient(client)
	parallel := NewParallelDownloader()
	parallel.Client = client.HTTPClient()
	client.UseTransport(network.Transport)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := "http://download.invalid/file.bin"
	single := filepath.Join(t.TempDir(), "single.bin")
	if err := downloader.File(ctx, DownloadRequest{URL: target, Destination: single}); err != nil {
		t.Fatal(err)
	}
	multi := filepath.Join(t.TempDir(), "parallel.bin")
	if err := parallel.Download(
		ctx,
		ParallelDownloadOptions{URL: target, SavePath: multi, FileSize: int64(len(data)), MaxChunks: 2},
	); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{single, multi} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("download mismatch: %s %v", path, err)
		}
	}
	if ranges.Load() < 2 {
		t.Fatalf("expected multiple Range requests, got %d", ranges.Load())
	}
	response, err := client.Stream(
		ctx,
		"http://upload.invalid/file",
		http.MethodPut,
		nil,
		strings.NewReader("upload"),
		6,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "upload" {
		t.Fatalf("upload: %s %v", body, err)
	}
}

func TestProxyRelayCloseTerminatesBothTunnelEnds(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	upstreamClosed := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.Copy(io.Discard, conn)
		close(upstreamClosed)
	}()
	network, err := NewProxyNetwork(ProxyConfig{Type: "http"})
	if err != nil {
		t.Fatal(err)
	}
	relay, err := StartProxyRelay(network, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = relay.Close() }()
	conn, err := net.DialTimeout("tcp", relay.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(
		conn,
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n",
		listener.Addr(),
		listener.Addr(),
	); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	if err := relay.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upstreamClosed:
	case <-time.After(time.Second):
		t.Fatal("upstream tunnel remained open")
	}
	buffer := make([]byte, 1)
	if _, err := conn.Read(buffer); err == nil {
		t.Fatal("downstream tunnel remained open")
	}
}
