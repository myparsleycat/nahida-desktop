package infra

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// ProxyConfig is an in-memory configuration. Never log or serialize credentials.
type ProxyConfig struct {
	Enabled  bool   `json:"enabled"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"-"`
	Password string `json:"-"`
}

func (p ProxyConfig) Validate() error {
	if p.Type != "http" && p.Type != "socks5" && p.Type != "socks5h" {
		return errors.New("proxy.type")
	}
	if !p.Enabled {
		return nil
	}
	if p.Host == "" || strings.TrimSpace(p.Host) != p.Host || strings.ContainsAny(p.Host, "/\\@?# \t\r\n") {
		return errors.New("proxy.host")
	}
	if strings.Contains(p.Host, ":") && net.ParseIP(p.Host) == nil {
		return errors.New("proxy.host")
	}
	if net.ParseIP(p.Host) == nil {
		name := strings.TrimSuffix(p.Host, ".")
		if len(name) > 253 {
			return errors.New("proxy.host")
		}
		for label := range strings.SplitSeq(name, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return errors.New("proxy.host")
			}
			for _, ch := range label {
				switch {
				case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
				default:
					return errors.New("proxy.host")
				}
			}
		}
	}
	if p.Port < 1 || p.Port > 65535 {
		return errors.New("proxy.port")
	}
	if p.Username == "" && p.Password != "" {
		return errors.New("proxy.username")
	}
	if p.Type == "http" && strings.ContainsAny(p.Username, ":\r\n") {
		return errors.New("proxy.username")
	}
	if p.Type != "http" && (len(p.Username) > 255 || len(p.Password) > 255 || (p.Username != "" && p.Password == "")) {
		return errors.New("proxy.credentials")
	}
	return nil
}

type proxyDial func(context.Context, string, string) (net.Conn, error)

// ProxyNetwork owns an immutable upstream policy for one application run.
type ProxyNetwork struct {
	Transport *http.Transport
	dial      proxyDial
}

func NewProxyNetwork(config ProxyConfig) (*ProxyNetwork, error) {
	return newProxyNetwork(config, net.DefaultResolver.LookupIPAddr)
}

func newProxyNetwork(config ProxyConfig, lookup func(context.Context, string) ([]net.IPAddr, error)) (*ProxyNetwork, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	direct := (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	n := &ProxyNetwork{Transport: transport, dial: direct}
	// Disabled still means "this process's policy", not the environment or OS proxy.
	transport.Proxy = nil
	if !config.Enabled {
		return n, nil
	}
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	if config.Type == "http" {
		endpoint := &url.URL{Scheme: "http", Host: address}
		if config.Username != "" {
			endpoint.User = url.UserPassword(config.Username, config.Password)
		}
		transport.Proxy = http.ProxyURL(endpoint)
		n.dial = func(ctx context.Context, _, target string) (net.Conn, error) {
			conn, err := direct(ctx, "tcp", address)
			if err != nil {
				return nil, err
			}
			stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
			defer stop()
			_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
			request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
			if config.Username != "" {
				request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(config.Username+":"+config.Password)))
			}
			if err = request.Write(conn); err != nil {
				_ = conn.Close()
				return nil, err
			}
			reader := bufio.NewReader(conn)
			// A successful CONNECT transfers ownership to the tunnel; closing its
			// HTTP body would consume or close the tunnel before TLS starts.
			response, err := http.ReadResponse(reader, request) //nolint:bodyclose
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			if response.StatusCode != http.StatusOK {
				_ = conn.Close()
				return nil, fmt.Errorf("proxy CONNECT status %d", response.StatusCode)
			}
			_ = conn.SetDeadline(time.Time{})
			return &bufferedProxyConn{Conn: conn, reader: reader}, nil
		}
	} else {
		var auth *proxy.Auth
		if config.Username != "" {
			auth = &proxy.Auth{User: config.Username, Password: config.Password}
		}
		socks, err := proxy.SOCKS5("tcp", address, auth, &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second})
		if err != nil {
			return nil, err
		}
		socksDial := socks.(proxy.ContextDialer).DialContext
		n.dial = func(ctx context.Context, network, target string) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			host, port, err := net.SplitHostPort(target)
			if err != nil {
				return nil, err
			}
			if config.Type == "socks5h" || net.ParseIP(host) != nil {
				return socksDial(ctx, network, target)
			}
			addresses, err := lookup(ctx, host)
			if err != nil {
				return nil, err
			}
			var failures []error
			for _, ip := range addresses {
				conn, err := socksDial(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				failures = append(failures, err)
				if ctx.Err() != nil {
					break
				}
			}
			if len(failures) == 0 {
				return nil, errors.New("proxy destination DNS returned no addresses")
			}
			return nil, errors.Join(failures...)
		}
		transport.DialContext = n.dial
	}
	return n, nil
}

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedProxyConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

// BlockedProxyTransport preserves offline settings access without a direct fallback.
type BlockedProxyTransport struct{}

func (BlockedProxyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("proxy.configurationInvalid")
}
