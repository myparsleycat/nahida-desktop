package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/platform"
)

const (
	DefaultBackendURL = "https://api.nahida.live"

	defaultHTTPTimeout = 100 * time.Second
	defaultRetryLimit  = 2
	defaultRetryWait   = 300 * time.Millisecond
	probeTimeout       = 5 * time.Second

	sessionPath = "/api/auth/get-session"

	backendUnavailableCode = "DRIVE_BACKEND_UNAVAILABLE"
	backendUnavailableMsg  = "The Nahida server is temporarily unavailable. Please try again shortly."
)

var authBodySep = regexp.MustCompile(`[_-]+`)

var retryStatus = map[int]struct{}{
	408: {},
	413: {},
	429: {},
	500: {},
	502: {},
	503: {},
	504: {},
}

// BackendStatus matches Electron BackendStatus.
type BackendStatus string

const (
	BackendUnknown     BackendStatus = "unknown"
	BackendOnline      BackendStatus = "online"
	BackendOffline     BackendStatus = "offline"
	BackendMaintenance BackendStatus = "maintenance"
)

// TokenLookup is injected from auth later. Keep it a function so infra
// does not import auth.
type TokenLookup func() (string, error)

// SessionRefresh is auth.getSession for the 401 hook.
type SessionRefresh func() error

// APIError is the Electron DriveApiError shape used at the HTTP boundary.
type APIError struct {
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func (e *APIError) Is(target error) bool {
	t, ok := target.(*APIError)
	if !ok || e == nil || t == nil {
		return false
	}
	return e.Code == t.Code
}

// ErrBackendUnavailable is the short-circuit error for offline/maintenance.
var ErrBackendUnavailable = &APIError{
	Code:    backendUnavailableCode,
	Message: backendUnavailableMsg,
	Status:  503,
}

// HTTPError is a ky-style status error when ThrowHTTPErrors is on.
type HTTPError struct {
	Response *http.Response
	Status   int
}

func (e *HTTPError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("Request failed with status code %d", e.Status)
}

// FetchOptions is the Go stand-in for ky/fetcher RequestInit.
type FetchOptions struct {
	Method            string
	Header            http.Header
	Body              io.Reader
	DisableHTTPErrors bool
	// RetryLimit overrides the client's retry limit for this request. A nil
	// value inherits the client policy; zero makes the request one-shot.
	RetryLimit *int
}

// ClientOptions configure an injectable HTTP boundary for tests and boot.
type ClientOptions struct {
	Version        string
	Token          TokenLookup
	RefreshSession SessionRefresh
	Log            *Log
	HTTPClient     *http.Client
	Transport      http.RoundTripper
	Timeout        time.Duration
	RetryLimit     *int
	RetryWait      *time.Duration
	Probe          func()
	BackendURL     string
	Status         BackendStatus
}

// Client is the Electron DesktopHttpService port.
type Client struct {
	mu             sync.Mutex
	version        string
	token          TokenLookup
	refreshSession SessionRefresh
	log            *Log
	http           *http.Client
	retryLimit     int
	retryWait      time.Duration
	probeFn        func()
	onStatus       func(BackendStatus)
	backendURL     string
	backend        BackendStatus
}

func NewClient() *Client {
	return NewClientWithOptions(ClientOptions{})
}

func NewClientWithOptions(opts ClientOptions) *Client {
	version := opts.Version
	if version == "" {
		version = platform.AppVersion
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultHTTPTimeout
	}
	retryLimit := defaultRetryLimit
	if opts.RetryLimit != nil {
		retryLimit = *opts.RetryLimit
	}
	retryWait := defaultRetryWait
	if opts.RetryWait != nil {
		retryWait = *opts.RetryWait
	}
	backendURL := opts.BackendURL
	if backendURL == "" {
		backendURL = DefaultBackendURL
	}
	status := opts.Status
	if status == "" {
		status = BackendUnknown
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:   timeout,
			Transport: opts.Transport,
		}
	}
	return &Client{
		version:        version,
		token:          opts.Token,
		refreshSession: opts.RefreshSession,
		log:            opts.Log,
		http:           httpClient,
		retryLimit:     retryLimit,
		retryWait:      retryWait,
		probeFn:        opts.Probe,
		backendURL:     backendURL,
		backend:        status,
	}
}

// BackendURL is the configured API origin.
func (c *Client) BackendURL() string {
	if c == nil || c.backendURL == "" {
		return DefaultBackendURL
	}
	return c.backendURL
}

// HTTPClient exposes the configured transport to internal streaming helpers
// that need to keep a response body open while applying their own retry and
// progress policies.
//
//wails:ignore
func (c *Client) HTTPClient() *http.Client {
	if c == nil || c.http == nil {
		return http.DefaultClient
	}
	return c.http
}

func (c *Client) UseToken(fn TokenLookup) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.token = fn
	c.mu.Unlock()
}

func (c *Client) UseRefreshSession(fn SessionRefresh) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.refreshSession = fn
	c.mu.Unlock()
}

func (c *Client) UseLog(log *Log) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.log = log
	c.mu.Unlock()
}

func (c *Client) UseProbe(fn func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.probeFn = fn
	c.mu.Unlock()
}

func (c *Client) UseOnStatus(fn func(BackendStatus)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onStatus = fn
	c.mu.Unlock()
}

func (c *Client) GetStatus() BackendStatus {
	if c == nil {
		return BackendUnknown
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.backend
}

func (c *Client) SetOnline() {
	c.SetStatus(BackendOnline)
}

func (c *Client) SetOffline() {
	c.SetStatus(BackendOffline)
}

func (c *Client) SetStatus(next BackendStatus) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.backend == next {
		c.mu.Unlock()
		return
	}
	c.backend = next
	fn := c.onStatus
	c.mu.Unlock()
	if fn != nil {
		fn(next)
	}
}

func (c *Client) userAgent() string {
	return "Nahida Desktop/" + c.version
}

func (c *Client) GetHeaders(rawURL string) (http.Header, error) {
	h := make(http.Header)
	h.Set("User-Agent", c.userAgent())
	if !isNHD(rawURL) {
		return h, nil
	}
	c.mu.Lock()
	tokenFn := c.token
	c.mu.Unlock()
	if tokenFn == nil {
		return h, nil
	}
	token, err := tokenFn()
	if err != nil {
		return nil, err
	}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	return h, nil
}

func (c *Client) Fetch(ctx context.Context, rawURL string, opts FetchOptions) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	nhd := isNHD(rawURL)
	session := isSessionRequest(rawURL)
	if nhd && !session {
		switch c.GetStatus() {
		case BackendOffline, BackendMaintenance:
			return nil, cloneAPIError(ErrBackendUnavailable)
		}
	}

	header, err := c.resolveHeaders(rawURL, opts.Header)
	if err != nil {
		return nil, err
	}
	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}

	var bodyBytes []byte
	if opts.Body != nil {
		bodyBytes, err = io.ReadAll(opts.Body)
		if err != nil {
			return nil, err
		}
	}

	retryLimit := c.retryLimit
	if opts.RetryLimit != nil {
		retryLimit = max(0, *opts.RetryLimit)
	}
	attempts := 1
	if canRetryMethod(method) {
		attempts = 1 + retryLimit
	}

	var resp *http.Response
	for attempt := range attempts {
		if attempt > 0 && c.retryWait > 0 {
			timer := time.NewTimer(c.retryWait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		var body io.Reader
		if bodyBytes != nil {
			body = bytes.NewReader(bodyBytes)
		}
		req, reqErr := http.NewRequestWithContext(ctx, method, rawURL, body)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header = header.Clone()

		resp, err = c.http.Do(req)
		if err != nil {
			if attempt+1 < attempts && isUnreachable(err) {
				continue
			}
			if nhd && isUnreachable(err) {
				c.SetOffline()
			}
			return nil, err
		}
		if attempt+1 < attempts && isRetryStatus(resp.StatusCode) {
			drainClose(resp.Body)
			continue
		}
		break
	}

	if resp == nil {
		return nil, errors.New("http: empty response")
	}
	resp = rewriteCloudflareTimeout(resp)
	c.afterUnauthorized(rawURL, nhd, session, resp)

	if !opts.DisableHTTPErrors && resp.StatusCode >= 400 {
		httpErr := &HTTPError{Response: resp, Status: resp.StatusCode}
		if nhd {
			c.noteBackendFromError(httpErr)
		}
		return nil, httpErr
	}
	if nhd {
		c.noteBackendFromResponse(resp)
	}
	return resp, nil
}

// Stream sends a one-shot request without buffering its body. Callers use it
// for large multipart uploads whose readers cannot be replayed automatically.
// The caller owns and must close the response body.
//
//wails:ignore
func (c *Client) Stream(ctx context.Context, rawURL, method string, header http.Header, body io.Reader, contentLength int64) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	nhd := isNHD(rawURL)
	session := isSessionRequest(rawURL)
	if nhd && !session {
		switch c.GetStatus() {
		case BackendOffline, BackendMaintenance:
			return nil, cloneAPIError(ErrBackendUnavailable)
		}
	}
	resolved, err := c.resolveHeaders(rawURL, header)
	if err != nil {
		return nil, err
	}
	if method == "" {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	request.Header = resolved
	if contentLength >= 0 {
		request.ContentLength = contentLength
	}
	response, err := c.http.Do(request)
	if err != nil {
		if nhd && isUnreachable(err) {
			c.SetOffline()
		}
		return nil, err
	}
	response = rewriteCloudflareTimeout(response)
	c.afterUnauthorized(rawURL, nhd, session, response)
	if nhd {
		c.noteBackendFromResponse(response)
	}
	return response, nil
}

// Probe hits BACKEND_URL/status. Periodic scheduling and IPC stay with auth.
func (c *Client) Probe(ctx context.Context) BackendStatus {
	if c == nil {
		return BackendUnknown
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	endpoint := strings.TrimRight(c.backendURL, "/") + "/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		c.SetOffline()
		return c.GetStatus()
	}
	req.Header.Set("User-Agent", c.userAgent())

	resp, err := c.http.Do(req)
	if err != nil {
		c.SetOffline()
		return c.GetStatus()
	}
	defer func() { _ = resp.Body.Close() }()

	var payload struct {
		Status string `json:"status"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)

	switch payload.Status {
	case "maintenance":
		c.SetStatus(BackendMaintenance)
	case "online":
		c.SetOnline()
	default:
		if resp.StatusCode > 0 && !isBackendUnavailable(resp.StatusCode) {
			c.SetOnline()
		} else {
			c.SetOffline()
		}
	}
	return c.GetStatus()
}

func (c *Client) resolveHeaders(rawURL string, caller http.Header) (http.Header, error) {
	out := caller.Clone()
	if out == nil {
		out = make(http.Header)
	}
	if hasAuthorization(out) {
		if out.Get("User-Agent") == "" {
			out.Set("User-Agent", c.userAgent())
		}
		return out, nil
	}
	callerUserAgent := out.Get("User-Agent")
	extra, err := c.GetHeaders(rawURL)
	if err != nil {
		return nil, err
	}
	for k, vs := range extra {
		out[k] = vs
	}
	if callerUserAgent != "" {
		out.Set("User-Agent", callerUserAgent)
	}
	return out, nil
}

func (c *Client) afterUnauthorized(rawURL string, nhd, session bool, resp *http.Response) {
	if resp == nil || resp.StatusCode != 401 || !nhd || session {
		return
	}
	body, err := peekBody(resp)
	if err != nil {
		body = ""
	}
	normalized := normalizeAuthBody(body)
	if strings.Contains(normalized, "password required") || strings.Contains(normalized, "missing password") {
		return
	}

	c.mu.Lock()
	refresh := c.refreshSession
	log := c.log
	c.mu.Unlock()
	if refresh == nil {
		return
	}
	if err := refresh(); err != nil && log != nil {
		log.Warn(map[string]any{
			"url":    rawURL,
			"status": resp.StatusCode,
			"stage":  "refresh-session",
			"error":  err.Error(),
		}, "Http:AuthRefreshFailed")
	}
}

func (c *Client) noteBackendFromResponse(resp *http.Response) {
	if resp == nil {
		return
	}
	if resp.StatusCode == 503 {
		c.triggerProbe()
		return
	}
	if isBackendUnavailable(resp.StatusCode) {
		c.SetOffline()
		return
	}
	c.SetOnline()
}

func (c *Client) noteBackendFromError(err error) {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.Status == 503 {
			c.triggerProbe()
			return
		}
		if isBackendUnavailable(httpErr.Status) {
			c.SetOffline()
		}
		return
	}
	if isUnreachable(err) {
		c.SetOffline()
	}
}

func (c *Client) triggerProbe() {
	c.mu.Lock()
	fn := c.probeFn
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func isNHD(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := strings.ToLower(u.Hostname())
	return (strings.EqualFold(u.Scheme, "https") && hostname == "api.nahida.live") ||
		(strings.EqualFold(u.Scheme, "http") && hostname == "localhost")
}

func isSessionRequest(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Path == sessionPath
}

func hasAuthorization(h http.Header) bool {
	if h == nil {
		return false
	}
	if _, ok := h["Authorization"]; ok {
		return true
	}
	for k := range h {
		if strings.EqualFold(k, "Authorization") {
			return true
		}
	}
	return false
}

func isBackendUnavailable(status int) bool {
	return status == 502 || status == 503 || status == 504
}

func isRetryStatus(status int) bool {
	_, ok := retryStatus[status]
	return ok
}

func canRetryMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodPut, http.MethodHead, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}

func rewriteCloudflareTimeout(resp *http.Response) *http.Response {
	if resp == nil || resp.StatusCode != 524 {
		return resp
	}
	drainClose(resp.Body)
	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Proto:      resp.Proto,
		ProtoMajor: resp.ProtoMajor,
		ProtoMinor: resp.ProtoMinor,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("cloudflare timeout. but it's ok")),
		Request:    resp.Request,
	}
}

func peekBody(resp *http.Response) (string, error) {
	if resp == nil || resp.Body == nil {
		return "", nil
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func normalizeAuthBody(s string) string {
	return authBodySep.ReplaceAllString(strings.ToLower(s), " ")
}

func drainClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func cloneAPIError(src *APIError) *APIError {
	if src == nil {
		return nil
	}
	cp := *src
	return &cp
}
