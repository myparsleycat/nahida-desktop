package infra

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	githubMetadataLimit = 8 << 20
	githubCachePrefix   = "github:http-cache:v1:"
)

type githubPolicyKey struct{}
type githubInfoKey struct{}

type githubRequestPolicy struct {
	operation  string
	refresh    bool
	maxAge     time.Duration
	allowStale bool
}

// GitHubResponseInfo identifies the actual successful check behind a cached response.
type GitHubResponseInfo struct {
	FetchedAt time.Time
	Cached    bool
	Stale     bool
}

func WithGitHubOperation(ctx context.Context, operation string) context.Context {
	policy := githubPolicy(ctx)
	policy.operation = operation
	return context.WithValue(ctx, githubPolicyKey{}, policy)
}

func WithGitHubRefresh(ctx context.Context, refresh bool) context.Context {
	policy := githubPolicy(ctx)
	policy.refresh = refresh
	return context.WithValue(ctx, githubPolicyKey{}, policy)
}

func WithGitHubMaxAge(ctx context.Context, maxAge time.Duration) context.Context {
	policy := githubPolicy(ctx)
	policy.maxAge = maxAge
	return context.WithValue(ctx, githubPolicyKey{}, policy)
}

func WithGitHubStaleFallback(ctx context.Context) context.Context {
	policy := githubPolicy(ctx)
	policy.allowStale = true
	return context.WithValue(ctx, githubPolicyKey{}, policy)
}

func WithGitHubResponseInfo(ctx context.Context, info *GitHubResponseInfo) context.Context {
	return context.WithValue(ctx, githubInfoKey{}, info)
}

func githubPolicy(ctx context.Context) githubRequestPolicy {
	policy, _ := ctx.Value(githubPolicyKey{}).(githubRequestPolicy)
	if policy.maxAge <= 0 {
		policy.maxAge = time.Hour
	}
	return policy
}

// GitHubRateError is shared by the provider transport and the release client.
type GitHubRateError struct {
	State *GitHubRateState
}

func (e *GitHubRateError) Error() string { return "GitHub API rate limit is exhausted" }

type githubCacheEntry struct {
	Version       int              `json:"version"`
	Body          json.RawMessage  `json:"body,omitempty"`
	ContentType   string           `json:"contentType,omitempty"`
	FetchedAt     time.Time        `json:"fetchedAt"`
	AttemptedAt   time.Time        `json:"attemptedAt"`
	Failures      int              `json:"failures"`
	NextTry       time.Time        `json:"nextTry"`
	FailureStatus int              `json:"failureStatus,omitempty"`
	RateFailure   *GitHubRateState `json:"rateFailure,omitempty"`
}

type githubTransportResult struct {
	status    int
	header    http.Header
	body      []byte
	fetchedAt time.Time
	cached    bool
	err       error
}

type githubRequestCounts struct {
	Requests int `json:"requests"`
	Cached   int `json:"cached"`
	Blocked  int `json:"blocked"`
}

type githubTransport struct {
	base *http.Client
	rate *GitHubRateCoordinator
}

// HTTPClient preserves the application's proxy and redirect policy while adding
// a shared GitHub-only gate and public release-metadata cache.
func (c *GitHubRateCoordinator) HTTPClient(base *http.Client) *http.Client {
	client := *base
	client.Transport = &githubTransport{base: base, rate: c}
	return &client
}

func (t *githubTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := t.base.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if request.URL.Scheme != "https" || !strings.EqualFold(request.URL.Host, "api.github.com") {
		return base.RoundTrip(request)
	}
	ctx, done, err := t.rate.requestContext(request.Context())
	if err != nil {
		return nil, err
	}
	request = request.Clone(ctx)
	if err := request.Context().Err(); err != nil {
		done()
		return nil, err
	}
	if !githubMetadataRequest(request) {
		response, err := t.rate.githubRoundTrip(request, base)
		if response == nil {
			done()
		} else {
			response.Body = &githubLifecycleBody{ReadCloser: response.Body, done: done}
		}
		return response, err
	}
	defer done()

	policy := githubPolicy(request.Context())
	maxAge := policy.maxAge
	if policy.refresh {
		maxAge = time.Minute
	}
	key := githubMetadataCacheKey(request)
	for {
		_, result, hit := t.cachedMetadata(request, key, policy)
		if !hit {
			flight := t.rate.flights.DoChan(key, func() (any, error) {
				ctx, done, err := t.rate.sharedContext(request.Context())
				if err != nil {
					return githubTransportResult{err: err}, nil
				}
				defer done()
				return t.metadata(request.Clone(ctx), base, key, policy), nil
			})
			select {
			case <-request.Context().Done():
				return nil, request.Context().Err()
			case value := <-flight:
				var ok bool
				result, ok = value.Val.(githubTransportResult)
				if !ok {
					return nil, errors.New("invalid GitHub metadata result")
				}
			}
		}
		if err := request.Context().Err(); err != nil {
			return nil, err
		}
		if errors.Is(result.err, context.Canceled) {
			return nil, result.err
		}
		// A joined automatic lookup may have used a longer TTL than this caller.
		if result.cached {
			if policy.refresh {
				allowed, state, err := t.rate.CanUseGitHubAPI(request.Context(), GitHubRateCheckOptions{})
				if err != nil {
					return nil, err
				}
				if !allowed {
					t.rate.recordGitHubRequest(request, "blocked", 0, 0, state)
					return nil, &GitHubRateError{State: state}
				}
			}
			age := t.rate.currentTime().Sub(result.fetchedAt)
			if age < 0 || age >= maxAge {
				continue
			}
		}
		stale := false
		if policy.allowStale && (result.err != nil || result.status >= 400) {
			entry := t.rate.loadGitHubCache(request.Context(), key)
			if len(entry.Body) > 0 && validGitHubMetadata(request, entry.Body) {
				result = cachedGitHubResult(entry)
				stale = true
			}
		}
		if result.err != nil {
			return nil, result.err
		}
		if info, ok := request.Context().Value(githubInfoKey{}).(*GitHubResponseInfo); ok {
			*info = GitHubResponseInfo{FetchedAt: result.fetchedAt, Cached: result.cached, Stale: stale}
		}
		if result.cached {
			t.rate.recordGitHubRequest(request, "cache", result.status, 0, nil)
		}
		return &http.Response{
			StatusCode:    result.status,
			Status:        fmt.Sprintf("%d %s", result.status, http.StatusText(result.status)),
			Header:        result.header.Clone(),
			Body:          io.NopCloser(bytes.NewReader(result.body)),
			ContentLength: int64(len(result.body)),
			Request:       request,
		}, nil
	}
}

func githubMetadataCacheKey(request *http.Request) string {
	identity := request.URL.String() + "\n" + request.Header.Get(
		"Accept",
	) + "\n" + request.Header.Get(
		"X-GitHub-Api-Version",
	)
	digest := sha256.Sum256([]byte(identity))
	return githubCachePrefix + hex.EncodeToString(digest[:])
}

func githubMetadataRequest(request *http.Request) bool {
	if request.Method != http.MethodGet || request.Header.Get("Authorization") != "" ||
		request.Header.Get("Cookie") != "" {
		return false
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "repos" || parts[1] == "" || parts[2] == "" {
		return false
	}
	return len(parts) == 4 && (parts[3] == "releases" || parts[3] == "tags") ||
		len(parts) == 5 && parts[3] == "releases" && parts[4] == "latest"
}

func (t *githubTransport) cachedMetadata(
	request *http.Request,
	key string,
	policy githubRequestPolicy,
) (githubCacheEntry, githubTransportResult, bool) {
	c := t.rate
	entry := c.loadGitHubCache(request.Context(), key)
	if len(entry.Body) > 0 && !validGitHubMetadata(request, entry.Body) {
		c.mu.Lock()
		log := c.log
		c.mu.Unlock()
		c.warnRefresh(log, errors.New("invalid GitHub metadata cache shape"))
		entry = githubCacheEntry{}
		c.mu.Lock()
		c.cache[key] = entry
		c.mu.Unlock()
	}
	now := c.currentTime()
	if entry.RateFailure != nil && c.IsRateLimited(entry.RateFailure) {
		state, err := c.GetRateState(request.Context())
		if err != nil {
			return entry, githubTransportResult{err: err}, true
		}
		if state == nil {
			copyState := *entry.RateFailure
			state = &copyState
		} else {
			state.RetryAt = max(state.RetryAt, GitHubRetryTime(entry.RateFailure).Unix())
		}
		if err := c.saveRateState(request.Context(), state); err != nil {
			c.mu.Lock()
			log := c.log
			c.mu.Unlock()
			c.warnRefresh(log, err)
		}
	}
	if policy.refresh || entry.Failures > 0 {
		allowed, state, err := c.CanUseGitHubAPI(request.Context(), GitHubRateCheckOptions{})
		if err != nil {
			return entry, githubTransportResult{err: err}, true
		}
		if !allowed {
			c.recordGitHubRequest(request, "blocked", 0, 0, state)
			return entry, githubTransportResult{err: &GitHubRateError{State: state}}, true
		}
	}
	if entry.Failures > 0 && now.Before(entry.NextTry) {
		c.recordGitHubRequest(request, "blocked", entry.FailureStatus, 0, nil)
		if entry.FailureStatus != 0 {
			return entry, githubTransportResult{status: entry.FailureStatus, header: make(http.Header)}, true
		}
		return entry, githubTransportResult{err: errors.New("GitHub metadata retry is cooling down")}, true
	}
	maxAge := policy.maxAge
	if policy.refresh {
		maxAge = time.Minute
	}
	if entry.Failures == 0 && len(entry.Body) > 0 && now.Sub(entry.FetchedAt) >= 0 &&
		now.Sub(entry.FetchedAt) < maxAge {
		return entry, cachedGitHubResult(entry), true
	}

	return entry, githubTransportResult{}, false
}

func (t *githubTransport) metadata(
	request *http.Request,
	base http.RoundTripper,
	key string,
	policy githubRequestPolicy,
) githubTransportResult {
	entry, result, hit := t.cachedMetadata(request, key, policy)
	if hit {
		return result
	}
	c := t.rate
	now := c.currentTime()
	entry.Version = 1
	entry.AttemptedAt = now
	response, err := c.githubRoundTrip(request, base)
	result = githubTransportResult{err: err, header: make(http.Header)}
	if response != nil {
		result.status, result.header = response.StatusCode, response.Header.Clone()
		result.body, err = io.ReadAll(io.LimitReader(response.Body, githubMetadataLimit+1))
		_ = response.Body.Close()
		if err == nil && len(result.body) > githubMetadataLimit {
			err = fmt.Errorf("GitHub metadata response exceeds %d bytes", githubMetadataLimit)
		}
		if err == nil && result.status == http.StatusOK && !validGitHubMetadata(request, result.body) {
			err = errors.New("invalid GitHub metadata JSON")
		}
		result.err = err
	}

	// http.Client follows these responses outside RoundTrip; the final URL owns its metadata cache.
	if result.err == nil && result.header.Get("Location") != "" {
		switch result.status {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
			http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			return result
		}
	}

	if result.err == nil && result.status == http.StatusOK {
		entry.Body = bytes.Clone(result.body)
		entry.ContentType = result.header.Get("Content-Type")
		entry.FetchedAt = c.currentTime()
		entry.Failures, entry.FailureStatus = 0, 0
		entry.RateFailure = nil
		entry.NextTry = time.Time{}
		result.fetchedAt = entry.FetchedAt
	} else if !errors.Is(result.err, context.Canceled) {
		entry.Failures = min(entry.Failures+1, 5)
		entry.FailureStatus = 0
		entry.RateFailure = nil
		if result.status >= 400 {
			entry.FailureStatus = result.status
		}
		entry.NextTry = c.currentTime().Add(githubFailureWait(entry.Failures))
		var rateErr *GitHubRateError
		if errors.As(result.err, &rateErr) {
			entry.NextTry = maxTime(entry.NextTry, GitHubRetryTime(rateErr.State))
			copyState := *rateErr.State
			entry.RateFailure = &copyState
		}
	}
	if !errors.Is(result.err, context.Canceled) {
		c.saveGitHubCache(request.Context(), key, entry)
	}
	return result
}

func cachedGitHubResult(entry githubCacheEntry) githubTransportResult {
	return githubTransportResult{
		status:    http.StatusOK,
		header:    http.Header{"Content-Type": []string{entry.ContentType}},
		body:      bytes.Clone(entry.Body),
		fetchedAt: entry.FetchedAt,
		cached:    true,
	}
}

func validGitHubMetadata(request *http.Request, body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if !json.Valid(trimmed) || len(trimmed) == 0 {
		return false
	}
	if strings.HasSuffix(request.URL.Path, "/latest") {
		return trimmed[0] == '{'
	}
	return trimmed[0] == '['
}

func githubFailureWait(failures int) time.Duration {
	return min(time.Minute*time.Duration(1<<uint(min(max(failures-1, 0), 4))), 15*time.Minute)
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (c *GitHubRateCoordinator) loadGitHubCache(ctx context.Context, key string) githubCacheEntry {
	c.mu.Lock()
	entry, found := c.cache[key]
	store, log := c.store, c.log
	c.mu.Unlock()
	if found || store == nil {
		return entry
	}
	raw, err := store.GetValue(ctx, key)
	if err != nil {
		c.warnRefresh(log, err)
	}
	if raw != nil {
		err = json.Unmarshal([]byte(*raw), &entry)
		if err == nil && (entry.Version != 1 || len(entry.Body) > githubMetadataLimit ||
			len(entry.Body) > 0 && (!json.Valid(entry.Body) || entry.FetchedAt.IsZero()) || entry.Failures < 0 || entry.Failures > 5) {
			err = errors.New("invalid GitHub metadata cache record")
		}
		if err != nil {
			c.warnRefresh(log, err)
			entry = githubCacheEntry{}
		}
	}
	c.mu.Lock()
	if existing, loaded := c.cache[key]; loaded {
		c.mu.Unlock()
		return existing
	}
	c.cache[key] = entry
	c.mu.Unlock()
	return entry
}

func (c *GitHubRateCoordinator) saveGitHubCache(ctx context.Context, key string, entry githubCacheEntry) {
	c.mu.Lock()
	c.cache[key] = entry
	store, log := c.store, c.log
	c.mu.Unlock()
	if store == nil {
		return
	}
	// The network deadline must not discard its persisted failure cooldown.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		stop := context.AfterFunc(c.ctx, cancel)
		defer func() { stop(); cancel() }()
		ctx = persist
	}
	raw, err := json.Marshal(entry)
	if err == nil {
		err = store.Upsert(ctx, key, string(raw), c.currentTime().UTC().Format(time.RFC3339Nano))
	}
	c.warnRefresh(log, err)
}

func (c *GitHubRateCoordinator) githubRoundTrip(request *http.Request, base http.RoundTripper) (*http.Response, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, context.Canceled
	}
	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case c.gate <- struct{}{}:
	}
	defer func() { <-c.gate }()
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	allowed, state, err := c.CanUseGitHubAPI(request.Context(), GitHubRateCheckOptions{})
	if err != nil {
		return nil, err
	}
	if !allowed {
		c.recordGitHubRequest(request, "blocked", 0, 0, state)
		return nil, &GitHubRateError{State: state}
	}
	started := c.currentTime()
	response, err := base.RoundTrip(request)
	status := 0
	if response != nil {
		status = response.StatusCode
		state, err = c.captureGitHubResponse(request.Context(), response)
		if err != nil {
			_ = response.Body.Close()
			response = nil
		}
	}
	c.recordGitHubRequest(request, "request", status, c.currentTime().Sub(started), state)
	return response, err
}

func (c *GitHubRateCoordinator) captureGitHubResponse(
	ctx context.Context,
	response *http.Response,
) (*GitHubRateState, error) {
	previous, loadErr := c.GetRateState(ctx)
	if loadErr != nil {
		c.mu.Lock()
		log := c.log
		c.mu.Unlock()
		c.warnRefresh(log, loadErr)
	}
	state := extractGitHubRateState(response.Header)
	if state == nil || state.Resource != "core" {
		state = previous
	}
	limitedStatus := response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests
	secondary := limitedStatus && response.Header.Get("Retry-After") != ""
	if limitedStatus && !secondary && !c.IsRateLimited(state) {
		body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body = &githubPeekBody{
			Reader: io.MultiReader(bytes.NewReader(body), response.Body),
			closer: response.Body,
		}
		if err != nil {
			return state, err
		}
		message := strings.ToLower(string(body))
		secondary = strings.Contains(message, "secondary rate limit") || strings.Contains(message, "abuse detection")
	}
	secondary = secondary || response.StatusCode == http.StatusTooManyRequests &&
		(state == nil || state.Remaining > 0 || !c.IsRateLimited(state))
	if state == nil && secondary {
		state = &GitHubRateState{Remaining: 1, Resource: "core"}
	}
	if state == nil {
		return nil, nil
	}
	if previous != nil {
		state.RetryAt = previous.RetryAt
	}
	if secondary {
		c.mu.Lock()
		c.secondaryFailures = min(c.secondaryFailures+1, 5)
		failures := c.secondaryFailures
		c.mu.Unlock()
		until := c.currentTime().Add(githubFailureWait(failures))
		if hinted, ok := githubRetryAfter(response.Header.Get("Retry-After"), c.currentTime()); ok {
			until = maxTime(until, hinted)
		}
		state.RetryAt = max(state.RetryAt, until.Unix())
	} else if response.StatusCode >= 200 && response.StatusCode < 300 {
		state.RetryAt = 0
		c.mu.Lock()
		c.secondaryFailures = 0
		c.mu.Unlock()
	}
	state.UpdatedAt = c.currentTime().UTC().Format(time.RFC3339Nano)
	if saveErr := c.saveRateState(ctx, state); saveErr != nil {
		c.mu.Lock()
		log := c.log
		c.mu.Unlock()
		c.warnRefresh(log, saveErr)
	}
	if limitedStatus && c.IsRateLimited(state) {
		return state, &GitHubRateError{State: state}
	}
	return state, nil
}

type githubPeekBody struct {
	io.Reader
	closer io.Closer
}

type githubLifecycleBody struct {
	io.ReadCloser
	done func()
	once sync.Once
}

func (b *githubLifecycleBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if err != nil {
		b.once.Do(b.done)
	}
	return n, err
}

func (b *githubLifecycleBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.done)
	return err
}

func (b *githubPeekBody) Close() error { return b.closer.Close() }

func githubRetryAfter(raw string, now time.Time) (time.Time, bool) {
	if seconds, err := strconv.ParseInt(
		raw,
		10,
		64,
	); err == nil && seconds >= 0 &&
		seconds <= int64((1<<63-1)/time.Second) {
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	until, err := http.ParseTime(raw)
	return until, err == nil
}

func (c *GitHubRateCoordinator) recordGitHubRequest(
	request *http.Request,
	kind string,
	status int,
	elapsed time.Duration,
	state *GitHubRateState,
) {
	policy := githubPolicy(request.Context())
	operation := policy.operation
	if operation == "" {
		operation = "github-metadata"
	}
	c.mu.Lock()
	count := c.counts[operation]
	switch kind {
	case "request":
		count.Requests++
	case "cache":
		count.Cached++
	case "blocked":
		count.Blocked++
	}
	c.counts[operation] = count
	log := c.log
	if state == nil && c.state != nil {
		copyState := *c.state
		state = &copyState
	}
	limited := c.IsRateLimited(state)
	warn := state != nil && (state.Remaining <= 10 || limited)
	var counts map[string]githubRequestCounts
	if warn {
		counts = make(map[string]githubRequestCounts, len(c.counts))
		for name, value := range c.counts {
			counts[name] = value
		}
	}
	c.mu.Unlock()
	if log == nil {
		return
	}
	fields := map[string]any{"operation": operation, "endpoint": SanitizeLogURL(request.URL.String()), "kind": kind,
		"status": status, "elapsedMs": elapsed.Milliseconds(), "cacheHit": kind == "cache"}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "repos" {
		fields["repository"] = parts[1] + "/" + parts[2]
	}
	if state != nil {
		fields["remaining"], fields["reset"], fields["retryAt"] = state.Remaining, state.Reset, state.RetryAt
	}
	log.Debug(fields, "GitHub")
	var budgetErr error
	if warn {
		fields["counts"] = counts
		budgetErr = errors.New("GitHub API request budget is low")
		if limited {
			budgetErr = &GitHubRateError{State: state}
		}
	}
	c.budgetDiagnostic.Report(log, budgetErr, "GitHub", Diagnostic{
		Severity: DiagnosticWarn, Operation: "github-budget", Stage: "rate", Fields: fields,
	})
}
