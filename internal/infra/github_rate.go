package infra

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const githubCoreRateKey = "github:core-rate"

type githubRateStore interface {
	GetValue(ctx context.Context, key string) (*string, error)
	Upsert(ctx context.Context, key, value, updatedAt string) error
}

type GitHubRateState struct {
	Limit     int64  `json:"limit"`
	Remaining int64  `json:"remaining"`
	Reset     int64  `json:"reset"`
	Used      int64  `json:"used"`
	Resource  string `json:"resource"`
	UpdatedAt string `json:"updatedAt"`
	RetryAt   int64  `json:"retryAt,omitempty"`
}

type githubRateRecord struct {
	GitHubRateState
	SecondaryFailures int `json:"secondaryFailures,omitempty"`
}

type GitHubRateCoordinator struct {
	mu                sync.Mutex
	store             githubRateStore
	log               *Log
	diagnostic        DiagnosticThrottle
	state             *GitHubRateState
	gate              chan struct{}
	now               func() time.Time
	cache             map[string]githubCacheEntry
	flights           singleflight.Group
	secondaryFailures int
	counts            map[string]githubRequestCounts
	budgetDiagnostic  DiagnosticThrottle
	ctx               context.Context
	cancel            context.CancelFunc
	workers           sync.WaitGroup
	closed            bool
	// identity separates the anonymous and per-token rate budgets; it is empty
	// while requests are anonymous, including after GitHub rejects the token.
	token           string
	identity        string
	tokenRejected   bool
	onTokenRejected func()
}

func NewGitHubRateCoordinator() *GitHubRateCoordinator {
	ctx, cancel := context.WithCancel(context.Background())
	return &GitHubRateCoordinator{
		now:  time.Now,
		gate: make(chan struct{}, 1),
		cache: make(
			map[string]githubCacheEntry,
		),
		counts: make(map[string]githubRequestCounts),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Close cancels and joins GitHub requests before the store is closed.
func (c *GitHubRateCoordinator) Close() {
	c.mu.Lock()
	c.closed = true
	c.cancel()
	c.mu.Unlock()
	c.workers.Wait()
}

func (c *GitHubRateCoordinator) requestContext(ctx context.Context) (context.Context, func(), error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, nil, context.Canceled
	}
	c.workers.Add(1)
	c.mu.Unlock()
	shared, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	return shared, func() { stop(); cancel(); c.workers.Done() }, nil
}

func (c *GitHubRateCoordinator) sharedContext(ctx context.Context) (context.Context, func(), error) {
	parent, done, err := c.requestContext(context.WithoutCancel(ctx))
	if err != nil {
		return nil, nil, err
	}
	shared, cancel := context.WithTimeout(parent, 30*time.Second)
	return shared, func() { cancel(); done() }, nil
}

func (c *GitHubRateCoordinator) UseAppState(store githubRateStore) {
	c.mu.Lock()
	c.store = store
	c.state = nil
	c.secondaryFailures = 0
	clear(c.cache)
	c.mu.Unlock()
}

func (c *GitHubRateCoordinator) UseLog(log *Log) {
	c.mu.Lock()
	c.log = log
	c.mu.Unlock()
}

// UseToken authenticates api.github.com requests with token; an empty token
// makes them anonymous. Each token has its own persisted rate budget.
func (c *GitHubRateCoordinator) UseToken(token string) {
	identity := githubTokenIdentity(token)
	c.mu.Lock()
	previous := githubTokenIdentity(c.token)
	store := c.store
	c.token, c.tokenRejected = token, false
	c.switchIdentityLocked(identity)
	c.mu.Unlock()

	// The anonymous budget is kept; a replaced token's budget is never read again.
	deleter, ok := store.(interface {
		Delete(ctx context.Context, key string) error
	})
	if previous != "" && previous != identity && ok {
		if err := deleter.Delete(context.Background(), githubRateKey(previous)); err != nil {
			c.mu.Lock()
			log := c.log
			c.mu.Unlock()
			c.warnRefresh(log, err)
		}
	}
}

// OnTokenRejected registers fn to run once each time GitHub rejects the
// configured token.
func (c *GitHubRateCoordinator) OnTokenRejected(fn func()) {
	c.mu.Lock()
	c.onTokenRejected = fn
	c.mu.Unlock()
}

func githubTokenIdentity(token string) string {
	if token == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:8])
}

// TokenRejected reports whether GitHub rejected the configured token, which
// leaves requests anonymous until the token changes.
func (c *GitHubRateCoordinator) TokenRejected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokenRejected
}

func (c *GitHubRateCoordinator) switchIdentityLocked(identity string) {
	if c.identity == identity {
		return
	}
	c.identity = identity
	c.state = nil
	c.secondaryFailures = 0
}

// credentials returns the identity and token to send; the token is empty when
// requests are anonymous.
func (c *GitHubRateCoordinator) credentials() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokenRejected {
		return c.identity, ""
	}
	return c.identity, c.token
}

func (c *GitHubRateCoordinator) rejectToken(identity string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity != identity || c.tokenRejected {
		return false
	}
	c.tokenRejected = true
	c.switchIdentityLocked("")
	return true
}

func githubRateKey(identity string) string {
	if identity == "" {
		return githubCoreRateKey
	}
	return githubCoreRateKey + ":" + identity
}

func (c *GitHubRateCoordinator) currentIdentity() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.identity
}

func (c *GitHubRateCoordinator) GetRateState(ctx context.Context) (*GitHubRateState, error) {
	return c.rateState(ctx, c.currentIdentity())
}

// rateState returns identity's recorded state. The in-memory copy, including
// the secondary backoff, is used and filled only while identity is current.
func (c *GitHubRateCoordinator) rateState(ctx context.Context, identity string) (*GitHubRateState, error) {
	c.mu.Lock()
	store, log := c.store, c.log
	if c.identity == identity && c.state != nil {
		state := *c.state
		c.mu.Unlock()
		return &state, nil
	}
	c.mu.Unlock()
	if store == nil {
		return nil, nil
	}
	raw, err := store.GetValue(ctx, githubRateKey(identity))
	if err != nil || raw == nil {
		return nil, err
	}
	state := decodeGitHubRateState(*raw, func(err error) { c.warnRefresh(log, err) })
	var record githubRateRecord
	_ = json.Unmarshal([]byte(*raw), &record)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.identity != identity {
		return state, nil
	}
	if c.state == nil && state != nil {
		copyState := *state
		c.state = &copyState
		c.secondaryFailures = min(max(record.SecondaryFailures, 0), 5)
	}
	if c.state != nil {
		copyState := *c.state
		state = &copyState
	}
	return state, nil
}

func decodeGitHubRateState(raw string, reports ...func(error)) *GitHubRateState {
	var state GitHubRateState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		for _, report := range reports {
			report(err)
		}
		return nil
	}
	return &state
}

func (c *GitHubRateCoordinator) IsRateLimited(state *GitHubRateState) bool {
	return state != nil && GitHubRetryTime(state).After(c.currentTime())
}

// GitHubRetryTime includes both the primary reset and secondary backoff.
func GitHubRetryTime(state *GitHubRateState) time.Time {
	if state == nil {
		return time.Time{}
	}
	retry := state.RetryAt
	if state.Remaining <= 0 {
		retry = max(retry, state.Reset)
	}
	if retry == 0 {
		return time.Time{}
	}
	return time.Unix(retry, 0).UTC()
}

func (c *GitHubRateCoordinator) currentTime() time.Time {
	return c.now()
}

func (c *GitHubRateCoordinator) CanUseGitHubAPI(ctx context.Context) (bool, *GitHubRateState, error) {
	return c.canUseGitHubAPI(ctx, c.currentIdentity())
}

func (c *GitHubRateCoordinator) canUseGitHubAPI(ctx context.Context, identity string) (bool, *GitHubRateState, error) {
	state, err := c.rateState(ctx, identity)
	if err != nil {
		return false, nil, err
	}
	return !c.IsRateLimited(state), state, nil
}

// saveRateState records state for identity. A response that arrives after the
// identity changed leaves the current state alone; it is persisted only for the
// anonymous budget, since a replaced token's budget is deleted.
func (c *GitHubRateCoordinator) saveRateState(ctx context.Context, identity string, state *GitHubRateState) error {
	if state == nil {
		return nil
	}
	c.mu.Lock()
	store := c.store
	copyState := *state
	record := githubRateRecord{GitHubRateState: copyState}
	if c.identity == identity {
		c.state = &copyState
		record.SecondaryFailures = c.secondaryFailures
	} else if identity != "" {
		store = nil
	}
	c.mu.Unlock()
	if store == nil {
		return nil
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return store.Upsert(ctx, githubRateKey(identity), string(raw), c.currentTime().UTC().Format(time.RFC3339Nano))
}

func (c *GitHubRateCoordinator) warnRefresh(log *Log, err error) {
	if err == nil {
		return
	}
	c.diagnostic.Report(
		log,
		err,
		"GitHubRateCoordinator",
		Diagnostic{Severity: DiagnosticWarn, Operation: "github-rate", Stage: "refresh"},
	)
}

func extractGitHubRateState(header http.Header) *GitHubRateState {
	parse := func(name string) (int64, bool) {
		value := header.Get(name)
		if value == "" {
			return 0, false
		}
		number, err := strconv.ParseInt(value, 10, 64)
		return number, err == nil
	}
	limit, okLimit := parse("X-RateLimit-Limit")
	remaining, okRemaining := parse("X-RateLimit-Remaining")
	reset, okReset := parse("X-RateLimit-Reset")
	used, okUsed := parse("X-RateLimit-Used")
	if !okLimit || !okRemaining || !okReset || !okUsed {
		return nil
	}
	resource := header.Get("X-RateLimit-Resource")
	if resource == "" {
		resource = "core"
	}
	return &GitHubRateState{
		Limit:     limit,
		Remaining: remaining,
		Reset:     reset,
		Used:      used,
		Resource:  resource,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func formatGitHubRateReset(state *GitHubRateState) string {
	if state == nil {
		return "unknown"
	}
	return GitHubRetryTime(state).Format("2006-01-02T15:04:05.000Z")
}
