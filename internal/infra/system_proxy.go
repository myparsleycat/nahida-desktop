package infra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/proxy"
)

type systemProxyConfig struct {
	autoDetect    bool
	autoConfigURL string
	proxy         string
	bypass        string
}

// Keep the OS boundary injectable so tests never change the user's proxy or VPN.
type systemProxyResolver struct {
	readConfig  func() (systemProxyConfig, error)
	resolveAuto func(context.Context, *url.URL, systemProxyConfig) (string, error)
}

var errNoAutoProxy = errors.New("no Windows automatic proxy discovered")

var errUnsupportedPAC = errors.New("unsupported Windows PAC proxy result")

func (s systemProxyResolver) proxiesForRequest(request *http.Request) ([]*url.URL, error) {
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	config, err := s.readConfig()
	if err != nil {
		return nil, fmt.Errorf("read Windows system proxy: %w", err)
	}

	// Match the browser's implicit loopback exception; local app endpoints must
	// remain reachable even when a VPN installs a catch-all system proxy.
	host := strings.ToLower(request.URL.Hostname())
	ip := net.ParseIP(host)
	if !strings.Contains(strings.ToLower(config.bypass), "<-loopback>") &&
		(host == "localhost" || strings.HasSuffix(host, ".localhost") || ip.IsLoopback()) {
		return nil, nil
	}
	if config.autoConfigURL != "" || config.autoDetect {
		endpoint, err := s.resolveAuto(request.Context(), request.URL, config)
		if err == nil {
			if strings.TrimSpace(endpoint) == "" {
				return nil, errUnsupportedPAC
			}
			return parseSystemProxies(endpoint, request.URL.Scheme)
		}
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		if errors.Is(err, errUnsupportedPAC) {
			return nil, err
		}
		// A configured static proxy is the Windows fallback when automatic
		// configuration fails. Never replace a broken configured PAC with DIRECT.
		if config.proxy == "" && (config.autoConfigURL != "" || !errors.Is(err, errNoAutoProxy)) {
			return nil, fmt.Errorf("resolve Windows automatic proxy: %w", err)
		}
	}
	if systemProxyBypass(request.URL, config.bypass) {
		return nil, nil
	}
	return parseSystemProxies(config.proxy, request.URL.Scheme)
}

func (n *ProxyNetwork) useSystemProxy(resolve func(*http.Request) ([]*url.URL, error)) {
	n.system = &systemProxyTransport{base: n.Transport, resolve: resolve, transports: make(map[string]*http.Transport)}
	direct := n.dial
	n.dial = func(ctx context.Context, network, target string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodConnect, "https://"+target+"/", nil)
		if err != nil {
			return nil, err
		}
		candidates, err := n.system.resolve(request)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return direct(ctx, network, target)
		}
		var failures []error
		for _, endpoint := range candidates {
			// DIRECT is permitted only as the initial decision, never after proxy failure.
			if endpoint == nil {
				if len(failures) == 0 {
					return direct(ctx, network, target)
				}
				continue
			}
			conn, err := dialSystemProxy(ctx, direct, endpoint, network, target)
			if err == nil {
				return conn, nil
			}
			failures = append(failures, err)
			if ctx.Err() != nil || !proxyDialFailed(err) {
				break
			}
		}
		return nil, errors.Join(failures...)
	}
}

func dialSystemProxy(
	ctx context.Context,
	direct proxyDial,
	endpoint *url.URL,
	network, target string,
) (net.Conn, error) {
	if endpoint.Scheme == "http" || endpoint.Scheme == "https" {
		return httpProxyDial(proxyConnectDial(direct), endpoint)(ctx, network, target)
	}
	var auth *proxy.Auth
	if endpoint.User != nil {
		password, _ := endpoint.User.Password()
		auth = &proxy.Auth{User: endpoint.User.Username(), Password: password}
	}
	dialer, err := proxy.SOCKS5(
		"tcp",
		endpoint.Host,
		auth,
		&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
	)
	if err != nil {
		return nil, err
	}
	return dialer.(proxy.ContextDialer).DialContext(ctx, network, target)
}

// Only recognized prefixes are mappings: an '=' in URL credentials is data.
func parseSystemProxies(value, scheme string) ([]*url.URL, error) {
	var matched, fallback, socks []string
	for _, entry := range strings.FieldsFunc(value, func(r rune) bool { return r == ';' || unicode.IsSpace(r) }) {
		key, endpoint, mapping := strings.Cut(entry, "=")
		key = strings.ToLower(key)
		mapping = mapping && (key == "http" || key == "https" || key == "ftp" || key == "socks")
		if !mapping {
			fallback = append(fallback, entry)
			continue
		}
		if endpoint == "" {
			return nil, errors.New("empty Windows system proxy endpoint")
		}
		if key == scheme {
			matched = append(matched, endpoint)
		}
		if key == "socks" {
			if !strings.Contains(endpoint, "://") {
				endpoint = "socks4://" + endpoint
			}
			socks = append(socks, endpoint)
		}
	}
	entries := matched
	if len(entries) == 0 {
		entries = fallback
	}
	if len(entries) == 0 {
		entries = socks
	}
	var candidates []*url.URL
	for _, entry := range entries {
		if entry == "DIRECT" {
			candidates = append(candidates, nil)
			continue
		}
		endpoint, err := systemProxyURL(entry)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, endpoint)
	}
	return candidates, nil
}

func systemProxyURL(value string) (*url.URL, error) {
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Hostname() == "" || endpoint.Path != "" || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" {
		// Do not echo configuration strings: they can contain proxy credentials.
		return nil, errors.New("invalid Windows system proxy endpoint")
	}
	port := endpoint.Port()
	switch endpoint.Scheme {
	case "http":
		if port == "" {
			port = "80"
		}
	case "https":
		if port == "" {
			port = "443"
		}
	case "socks5", "socks5h":
		if port == "" {
			port = "1080"
		}
	default:
		return nil, errors.New("unsupported Windows system proxy protocol")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return nil, errors.New("invalid Windows system proxy port")
	}
	endpoint.Host = net.JoinHostPort(endpoint.Hostname(), port)
	return endpoint, nil
}

func systemProxyBypass(target *url.URL, list string) bool {
	host := strings.ToLower(target.Hostname())
	port := target.Port()
	if port == "" {
		port = "80"
		if target.Scheme == "https" {
			port = "443"
		}
	}
	for _, rule := range strings.FieldsFunc(strings.ToLower(list), func(r rune) bool { return r == ';' || unicode.IsSpace(r) }) {
		if rule == "<local>" {
			if !strings.Contains(host, ".") && net.ParseIP(host) == nil {
				return true
			}
			continue
		}
		if rule == "<-loopback>" {
			continue
		}
		if scheme, remainder, ok := strings.Cut(rule, "://"); ok {
			if scheme != target.Scheme {
				continue
			}
			rule = remainder
		}
		if prefix, err := netip.ParsePrefix(rule); err == nil {
			if address, err := netip.ParseAddr(host); err == nil && prefix.Contains(address) {
				return true
			}
			continue
		}
		if ruleHost, rulePort, err := net.SplitHostPort(rule); err == nil {
			if rulePort != port {
				continue
			}
			rule = ruleHost
		} else {
			rule = strings.Trim(rule, "[]")
		}
		pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(rule), `\*`, ".*") + "$"
		if matched, _ := regexp.MatchString(pattern, host); matched {
			return true
		}
	}
	return false
}
