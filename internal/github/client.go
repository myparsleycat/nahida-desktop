package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

const (
	apiVersion   = "2026-03-10"
	apiUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	maxJSONBytes    = 8 << 20
	maxDiscardBytes = 64 << 10
)

var (
	ErrRateLimited   = errors.New("GitHub API rate limit is exhausted")
	ErrTreeTruncated = errors.New("GitHub tree is truncated")

	errHTTPNotConfigured = errors.New("GitHub HTTP client is not configured")

	commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// RateLimitError reports a request skipped because the shared core-rate
// budget is exhausted. It matches ErrRateLimited.
type RateLimitError struct {
	State *infra.GitHubRateState
}

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }

func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// ResetAt is when both the primary and secondary GitHub limits allow retrying.
func (e *RateLimitError) ResetAt() time.Time {
	return infra.GitHubRetryTime(e.State)
}

// StatusError reports a non-2xx GitHub response.
type StatusError struct {
	Status int
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d", e.Status) }

type Options struct {
	HTTP *infra.Client
	// Download is used for release files; nil downloads through HTTP directly.
	Download *infra.Download
	// Rate shares GitHub API policy; nil creates a coordinator with an in-memory cache.
	Rate *infra.GitHubRateCoordinator
	Log  *infra.Log
}

type Client struct {
	http     *infra.Client
	download *infra.Download
	rate     *infra.GitHubRateCoordinator
	log      *infra.Log
}

func New(opts Options) *Client {
	download := opts.Download
	if download == nil && opts.HTTP != nil {
		download = infra.NewDownload()
		download.UseClient(opts.HTTP)
	}
	rate := opts.Rate
	if rate == nil {
		rate = infra.NewGitHubRateCoordinator()
		rate.UseLog(opts.Log)
	}
	return &Client{http: opts.HTTP, download: download, rate: rate, log: opts.Log}
}

// Configured reports whether c can call the API, fetch files and download files.
func (c *Client) Configured() bool {
	return c != nil && c.http != nil && c.download != nil
}

// Releases lists the repository's published releases, newest first, without
// drafts and prereleases.
func (c *Client) Releases(ctx context.Context, repo Repo) ([]Release, error) {
	releases, err := c.AllReleases(ctx, repo)
	if err != nil {
		return nil, err
	}
	published := releases[:0]
	for _, release := range releases {
		if release.Prerelease {
			continue
		}
		published = append(published, release)
	}
	return published, nil
}

// AllReleases lists published releases, including prereleases, newest first.
func (c *Client) AllReleases(ctx context.Context, repo Repo) ([]Release, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	var releases []Release
	if err := c.getJSON(ctx, repo.apiURL("releases"), &releases); err != nil {
		return nil, err
	}

	published := releases[:0]
	for _, release := range releases {
		if release.Draft {
			continue
		}
		published = append(published, release)
	}
	return published, nil
}

// LatestRelease returns the newest non-prerelease, non-draft release.
func (c *Client) LatestRelease(ctx context.Context, repo Repo) (Release, error) {
	if err := repo.Validate(); err != nil {
		return Release{}, err
	}
	var release Release
	if err := c.getJSON(ctx, repo.apiURL("releases/latest"), &release); err != nil {
		return Release{}, err
	}
	return release, nil
}

// ResolveTagCommit resolves a lightweight or annotated tag to its commit SHA.
func (c *Client) ResolveTagCommit(ctx context.Context, repo Repo, tag string) (string, error) {
	if err := repo.Validate(); err != nil {
		return "", err
	}
	var ref gitObject
	if err := c.getJSON(ctx, repo.apiURL("git/ref/tags/"+url.PathEscape(tag)), &ref); err != nil {
		return "", err
	}

	commit := ref.Object.SHA
	if ref.Object.Type == "tag" {
		if !strings.HasPrefix(ref.Object.URL, repo.apiURL("git/")) {
			return "", fmt.Errorf("unsafe annotated tag URL for %s", repo)
		}
		var annotated gitObject
		if err := c.getJSON(ctx, ref.Object.URL, &annotated); err != nil {
			return "", err
		}
		commit = annotated.Object.SHA
		if commit == "" {
			commit = annotated.SHA
		}
	}
	if !commitRE.MatchString(commit) {
		return "", fmt.Errorf("tag %s of %s resolved to an invalid commit", tag, repo)
	}
	return commit, nil
}

type gitObject struct {
	Object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
		URL  string `json:"url"`
	} `json:"object"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type Tree struct {
	Truncated bool        `json:"truncated"`
	Entries   []TreeEntry `json:"tree"`
}

type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// Tree returns the complete recursive tree of a commit.
func (c *Client) Tree(ctx context.Context, repo Repo, commit string) (Tree, error) {
	if err := repo.Validate(); err != nil {
		return Tree{}, err
	}
	if !commitRE.MatchString(commit) {
		return Tree{}, fmt.Errorf("invalid commit %q", commit)
	}
	var tree Tree
	if err := c.getJSON(ctx, repo.apiURL("git/trees/"+commit+"?recursive=1"), &tree); err != nil {
		return Tree{}, err
	}
	if tree.Truncated {
		return Tree{}, fmt.Errorf("%w: %s@%s", ErrTreeTruncated, repo, commit)
	}
	return tree, nil
}

// getJSON decodes a GitHub REST API response.
func (c *Client) getJSON(ctx context.Context, apiURL string, target any) error {
	data, err := c.GetBytes(ctx, apiURL, maxJSONBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return infra.AnnotateError(
			fmt.Errorf("decode GitHub response: %w", err),
			infra.HTTPDiagnostic(http.MethodGet, apiURL, "decode", nil),
		)
	}
	return nil
}

// GetBytes reads a GitHub REST API response of at most limit bytes. Only
// https://api.github.com URLs are accepted.
func (c *Client) GetBytes(ctx context.Context, apiURL string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.github.com") {
		return nil, infra.WithCause(fmt.Errorf("unsafe GitHub API URL %q", infra.SanitizeLogURL(apiURL)), err)
	}
	if c == nil || c.http == nil {
		return nil, errHTTPNotConfigured
	}
	header := make(http.Header)
	header.Set("Accept", "application/vnd.github+json")
	header.Set("X-GitHub-Api-Version", apiVersion)
	header.Set("User-Agent", apiUserAgent)
	response, err := c.http.Fetch(
		ctx,
		apiURL,
		infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true,
			RetryLimit: new(int), HTTPClient: c.rate.HTTPClient(c.http.HTTPClient())},
	)
	if err != nil {
		var rateErr *infra.GitHubRateError
		if errors.As(err, &rateErr) {
			return nil, infra.AnnotateError(&RateLimitError{State: rateErr.State},
				infra.HTTPDiagnostic(http.MethodGet, apiURL, "rate-limit", nil))
		}
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	return readResponse(response, apiURL, limit)
}

// RateState returns the last recorded core-rate state, if any.
func (c *Client) RateState(ctx context.Context) (*infra.GitHubRateState, error) {
	if c == nil {
		return nil, nil
	}
	return c.rate.GetRateState(ctx)
}

// IsRateLimited reports whether state exhausts the core-rate budget.
func (c *Client) IsRateLimited(state *infra.GitHubRateState) bool {
	if c == nil {
		return false
	}
	return c.rate.IsRateLimited(state)
}

func readResponse(response *http.Response, rawURL string, limit int64) ([]byte, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDiscardBytes))
		return nil, infra.AnnotateError(
			&StatusError{Status: response.StatusCode},
			infra.HTTPDiagnostic(http.MethodGet, rawURL, "status", response),
		)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, infra.AnnotateError(err, infra.HTTPDiagnostic(http.MethodGet, rawURL, "read-body", response))
	}
	if int64(len(data)) > limit {
		return nil, infra.AnnotateError(
			fmt.Errorf("response exceeds %d bytes", limit),
			infra.HTTPDiagnostic(http.MethodGet, rawURL, "read-body", response),
		)
	}
	return data, nil
}
