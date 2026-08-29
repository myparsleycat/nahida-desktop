package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	githubCoreRateKey  = "github:core-rate"
	githubRateLimitURL = "https://api.github.com/rate_limit"
	githubRateMaxBody  = 2 << 20
)

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
}

type GitHubRateCheckOptions struct {
	RefreshIfMissing bool
}

type GitHubRateCoordinator struct {
	mu    sync.Mutex
	store githubRateStore
	http  *Client
	log   *Log
}

func NewGitHubRateCoordinator() *GitHubRateCoordinator {
	return &GitHubRateCoordinator{}
}

func (c *GitHubRateCoordinator) UseAppState(store githubRateStore) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.store = store
	c.mu.Unlock()
}

func (c *GitHubRateCoordinator) UseHTTP(httpClient *Client) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.http = httpClient
	c.mu.Unlock()
}

func (c *GitHubRateCoordinator) UseLog(log *Log) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.log = log
	c.mu.Unlock()
}

func (c *GitHubRateCoordinator) GetRateState(ctx context.Context) (*GitHubRateState, error) {
	if c == nil {
		return nil, nil
	}
	c.mu.Lock()
	store := c.store
	c.mu.Unlock()
	if store == nil {
		return nil, nil
	}
	raw, err := store.GetValue(ctx, githubCoreRateKey)
	if err != nil || raw == nil {
		return nil, err
	}
	return decodeGitHubRateState(*raw), nil
}

func decodeGitHubRateState(raw string) *GitHubRateState {
	var state GitHubRateState
	if json.Unmarshal([]byte(raw), &state) != nil {
		return nil
	}
	return &state
}

func (c *GitHubRateCoordinator) IsRateLimited(state *GitHubRateState) bool {
	return state != nil && state.Remaining <= 0 && time.Unix(state.Reset, 0).After(time.Now())
}

func (c *GitHubRateCoordinator) CanUseGitHubAPI(ctx context.Context, opts GitHubRateCheckOptions) (bool, *GitHubRateState, error) {
	if c == nil {
		return true, nil, nil
	}
	state, err := c.GetRateState(ctx)
	if err != nil {
		return false, nil, err
	}
	if state != nil || !opts.RefreshIfMissing {
		return !c.IsRateLimited(state), state, nil
	}
	refreshed := c.RefreshRateState(ctx)
	return !c.IsRateLimited(refreshed), refreshed, nil
}

func (c *GitHubRateCoordinator) RefreshRateState(ctx context.Context) *GitHubRateState {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	httpClient, log := c.http, c.log
	c.mu.Unlock()
	if httpClient == nil {
		state, _ := c.GetRateState(ctx)
		return state
	}
	response, err := httpClient.Fetch(ctx, githubRateLimitURL, FetchOptions{
		Method: http.MethodGet,
		Header: http.Header{"Accept": []string{"application/vnd.github+json"}},
	})
	if err != nil {
		c.warnRefresh(log, err)
		state, _ := c.GetRateState(ctx)
		return state
	}
	defer func() { _ = response.Body.Close() }()
	if state := extractGitHubRateState(response.Header); state != nil {
		if saveErr := c.saveRateState(ctx, state); saveErr != nil {
			c.warnRefresh(log, saveErr)
		}
		return state
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, githubRateMaxBody+1))
	if err != nil || len(body) > githubRateMaxBody {
		c.warnRefresh(log, err)
		state, _ := c.GetRateState(ctx)
		return state
	}
	state := normalizeGitHubRateState(body)
	if state == nil {
		state, _ := c.GetRateState(ctx)
		return state
	}
	if saveErr := c.saveRateState(ctx, state); saveErr != nil {
		c.warnRefresh(log, saveErr)
	}
	return state
}

func (c *GitHubRateCoordinator) saveRateState(ctx context.Context, state *GitHubRateState) error {
	if c == nil || state == nil {
		return nil
	}
	c.mu.Lock()
	store := c.store
	c.mu.Unlock()
	if store == nil {
		return nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.Upsert(ctx, githubCoreRateKey, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
}

func (c *GitHubRateCoordinator) warnRefresh(log *Log, err error) {
	if log == nil || err == nil {
		return
	}
	log.Warn(fmt.Sprintf("Failed to refresh GitHub rate state: %v", err), "GitHubRateCoordinator")
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

func normalizeGitHubRateState(raw []byte) *GitHubRateState {
	var payload struct {
		Rate *struct {
			Limit     *int64  `json:"limit"`
			Remaining *int64  `json:"remaining"`
			Reset     *int64  `json:"reset"`
			Used      *int64  `json:"used"`
			Resource  *string `json:"resource"`
		} `json:"rate"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Rate == nil {
		return nil
	}
	rate := payload.Rate
	if rate.Limit == nil || rate.Remaining == nil || rate.Reset == nil || rate.Used == nil {
		return nil
	}
	resource := "core"
	if rate.Resource != nil && *rate.Resource != "" {
		resource = *rate.Resource
	}
	return &GitHubRateState{
		Limit:     *rate.Limit,
		Remaining: *rate.Remaining,
		Reset:     *rate.Reset,
		Used:      *rate.Used,
		Resource:  resource,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func formatGitHubRateReset(state *GitHubRateState) string {
	if state == nil {
		return "unknown"
	}
	return time.Unix(state.Reset, 0).UTC().Format("2006-01-02T15:04:05.000Z")
}
