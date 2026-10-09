package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"
)

const githubTestReleases = "https://api.github.com/repos/test/project/releases"

func TestGitHubMetadataFollowsRedirectsWithoutFailureCooldown(t *testing.T) {
	t.Parallel()
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			for _, latest := range []bool{false, true} {
				t.Run(fmt.Sprintf("latest=%t", latest), func(t *testing.T) {
					t.Parallel()
					store := newGitHubMemoryStore()
					original, moved, body := githubTestReleases, "https://api.github.com/repos/test/moved/releases", `[]`
					if latest {
						original, moved, body = original+"/latest", moved+"/latest", `{}`
					}
					var redirects, releases atomic.Int32
					base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
						if request.URL.String() == original {
							redirects.Add(1)
							header := make(http.Header)
							header.Set("Location", strings.TrimPrefix(moved, "https://api.github.com"))
							return githubTestResponse(request, status, header, "repository moved"), nil
						}
						if request.URL.String() != moved {
							return nil, fmt.Errorf("unexpected redirect target %s", request.URL)
						}
						releases.Add(1)
						return githubTestResponse(request, 200, make(http.Header), body), nil
					})}
					for restart := range 2 {
						rate := NewGitHubRateCoordinator()
						rate.UseAppState(store)
						client := rate.HTTPClient(base)
						for range 2 {
							got, err := githubTestGet(t.Context(), client, original)
							if err != nil || got != body {
								rate.Close()
								t.Fatalf("restart=%d body=%q err=%v", restart, got, err)
							}
						}
						rate.Close()
					}
					if redirects.Load() != 4 || releases.Load() != 1 {
						t.Fatalf("redirects=%d release requests=%d", redirects.Load(), releases.Load())
					}
					request, _ := http.NewRequest(http.MethodGet, original, nil)
					if raw, err := store.GetValue(
						t.Context(),
						githubMetadataCacheKey(request),
					); err != nil ||
						raw != nil {
						t.Fatalf("redirect persisted as metadata failure: raw=%v err=%v", raw, err)
					}
				})
			}
		})
	}
}

func TestGitHubMetadataCallerCancellationDoesNotCancelSharedFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newGitHubMemoryStore()
		rate := NewGitHubRateCoordinator()
		rate.UseAppState(store)
		defer rate.Close()
		started, release := make(chan struct{}), make(chan struct{})
		var shared context.Context
		var requests atomic.Int32
		client := rate.HTTPClient(
			&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests.Add(1)
				shared = request.Context()
				close(started)
				select {
				case <-release:
					return githubTestResponse(request, 200, make(http.Header), `[]`), nil
				case <-shared.Done():
					return nil, shared.Err()
				}
			})},
		)
		ctx, cancel := context.WithCancel(t.Context())
		leader, waiter := make(chan error, 1), make(chan error, 1)
		go func() { _, err := githubTestGet(ctx, client, githubTestReleases); leader <- err }()
		<-started
		go func() { _, err := githubTestGet(t.Context(), client, githubTestReleases); waiter <- err }()
		synctest.Wait()
		cancel()
		if err := <-leader; !errors.Is(err, context.Canceled) {
			t.Fatalf("leader cancellation=%v", err)
		}
		if err := shared.Err(); err != nil {
			t.Fatalf("caller canceled shared fetch: %v", err)
		}
		close(release)
		if err := <-waiter; err != nil || requests.Load() != 1 {
			t.Fatalf("waiter err=%v requests=%d", err, requests.Load())
		}
		entry := store.entry(t, githubTestReleases)
		if entry.Failures != 0 || !entry.NextTry.IsZero() || entry.FetchedAt.IsZero() {
			t.Fatalf("caller cancellation recorded as failure: %+v", entry)
		}
	})
}

func TestGitHubCachedResponseDoesNotOverwriteCurrentLimitOrCountAsRequest(t *testing.T) {
	t.Parallel()
	rate := NewGitHubRateCoordinator()
	defer rate.Close()
	reset := time.Now().Add(time.Hour)
	var requests atomic.Int32
	client := rate.HTTPClient(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			remaining, body := 40, `[]`
			if requests.Add(1) == 2 {
				remaining, body = 0, `{}`
			}
			return githubTestResponse(request, 200, githubTestHeaders(remaining, reset), body), nil
		})},
	)
	ctx := WithGitHubOperation(t.Context(), "fixture")
	if _, err := githubTestGet(ctx, client, githubTestReleases); err != nil {
		t.Fatal(err)
	}
	if _, err := githubTestGet(ctx, client, "https://api.github.com/repos/test/project/git/trees/commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := githubTestGet(ctx, client, githubTestReleases); err != nil {
		t.Fatal(err)
	}
	var limited *GitHubRateError
	if _, err := githubTestGet(WithGitHubRefresh(ctx, true), client, githubTestReleases); !errors.As(err, &limited) {
		t.Fatalf("refresh bypassed current limit: %v", err)
	}
	state, err := rate.GetRateState(ctx)
	if err != nil || state == nil || state.Remaining != 0 || state.Reset != reset.Unix() {
		t.Fatalf("cache overwrote state=%+v err=%v", state, err)
	}
	rate.mu.Lock()
	counts := rate.counts["fixture"]
	rate.mu.Unlock()
	if requests.Load() != 2 || counts.Requests != 2 || counts.Cached != 1 || counts.Blocked != 1 {
		t.Fatalf("requests=%d counts=%+v", requests.Load(), counts)
	}
}

func githubTestHeaders(remaining int, reset time.Time) http.Header {
	return http.Header{
		"X-Ratelimit-Limit": {"60"}, "X-Ratelimit-Remaining": {fmt.Sprint(remaining)},
		"X-Ratelimit-Reset": {fmt.Sprint(reset.Unix())}, "X-Ratelimit-Used": {fmt.Sprint(60 - remaining)},
		"X-Ratelimit-Resource": {"core"}, "Content-Type": {"application/json"},
	}
}

func githubTestResponse(request *http.Request, status int, header http.Header, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func githubTestGet(ctx context.Context, client *http.Client, url string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err == nil && response.StatusCode >= 400 {
		err = fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return string(body), err
}

func TestGitHubMetadataPersistsAcrossClientsAndHonorsRefresh(t *testing.T) {
	t.Parallel()
	store := newGitHubMemoryStore()
	var requests atomic.Int32
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0) }
	base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return githubTestResponse(request, 200, githubTestHeaders(40, now().Add(time.Hour)), `[{"tag_name":"v1"}]`), nil
	})}
	newClient := func() (*GitHubRateCoordinator, *http.Client) {
		rate := NewGitHubRateCoordinator()
		rate.now = now
		rate.UseAppState(store)
		t.Cleanup(rate.Close)
		return rate, rate.HTTPClient(base)
	}
	_, client := newClient()
	var initial GitHubResponseInfo
	ctx := WithGitHubResponseInfo(t.Context(), &initial)
	if _, err := githubTestGet(ctx, client, githubTestReleases); err != nil {
		t.Fatal(err)
	}
	clock.Add(120)
	rate, restarted := newClient()
	var info GitHubResponseInfo
	ctx = WithGitHubResponseInfo(t.Context(), &info)
	if _, err := githubTestGet(ctx, restarted, githubTestReleases); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || !info.Cached || !info.FetchedAt.Equal(initial.FetchedAt) {
		t.Fatalf("requests=%d info=%+v initial=%+v", requests.Load(), info, initial)
	}
	state, _ := rate.GetRateState(t.Context())
	if state == nil || state.Remaining != 40 {
		t.Fatalf("state=%+v", state)
	}
	ctx = WithGitHubRefresh(ctx, true)
	for range 2 {
		if _, err := githubTestGet(ctx, restarted, githubTestReleases); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("refresh requests=%d", requests.Load())
	}
	clock.Add(3600)
	if _, err := githubTestGet(t.Context(), restarted, githubTestReleases); err != nil || requests.Load() != 3 {
		t.Fatalf("expired requests=%d err=%v", requests.Load(), err)
	}
}

func TestGitHubMetadataFailurePersistsAndStaleIsDisplayOnly(t *testing.T) {
	t.Parallel()
	store := newGitHubMemoryStore()
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0) }
	var requests atomic.Int32
	base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		n := requests.Add(1)
		if n == 1 || n == 4 {
			return githubTestResponse(
				request,
				200,
				githubTestHeaders(40, now().Add(time.Hour)),
				`[{"tag_name":"v1"}]`,
			), nil
		}
		return githubTestResponse(request, 503, make(http.Header), "unavailable"), nil
	})}
	newClient := func() *http.Client {
		rate := NewGitHubRateCoordinator()
		rate.now = now
		rate.UseAppState(store)
		t.Cleanup(rate.Close)
		return rate.HTTPClient(base)
	}
	client := newClient()
	if _, err := githubTestGet(t.Context(), client, githubTestReleases); err != nil {
		t.Fatal(err)
	}
	clock.Add(120)
	refresh := WithGitHubRefresh(t.Context(), true)
	if _, err := githubTestGet(refresh, client, githubTestReleases); err == nil {
		t.Fatal("failed refresh reported success")
	}
	client = newClient()
	if _, err := githubTestGet(refresh, client, githubTestReleases); err == nil || requests.Load() != 2 {
		t.Fatalf("cooldown err=%v requests=%d", err, requests.Load())
	}
	var info GitHubResponseInfo
	ctx := WithGitHubResponseInfo(WithGitHubStaleFallback(t.Context()), &info)
	if body, err := githubTestGet(
		ctx,
		client,
		githubTestReleases,
	); err != nil || body != `[{"tag_name":"v1"}]` ||
		!info.Stale {
		t.Fatalf("display fallback body=%q err=%v info=%+v", body, err, info)
	}
	clock.Add(60)
	if _, err := githubTestGet(t.Context(), client, githubTestReleases); err == nil || requests.Load() != 3 {
		t.Fatalf("retry err=%v requests=%d", err, requests.Load())
	}
	clock.Add(60)
	if _, err := githubTestGet(refresh, client, githubTestReleases); err == nil || requests.Load() != 3 {
		t.Fatalf("second cooldown err=%v requests=%d", err, requests.Load())
	}
	clock.Add(60)
	if _, err := githubTestGet(t.Context(), client, githubTestReleases); err != nil || requests.Load() != 4 {
		t.Fatalf("recovery err=%v requests=%d", err, requests.Load())
	}
}

func TestGitHubTransportClassifiesLimitsAndPreservesBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		status     int
		remaining  int
		retryAfter string
		body       string
		limited    bool
	}{
		{"primary", 403, 0, "", `{"message":"API rate limit exceeded"}`, true},
		{"secondary", 403, 30, "", `{"message":"secondary rate limit"}`, true},
		{"long retry", 429, 30, "3600", `{}`, true},
		{"dated retry", 403, 30, time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat), `{}`, true},
		{"429 without headers", 429, -1, "", `{}`, true},
		{"permission", 403, 30, "", `{"message":"Resource not accessible"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rate := NewGitHubRateCoordinator()
			t.Cleanup(rate.Close)
			var requests atomic.Int32
			client := rate.HTTPClient(
				&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					requests.Add(1)
					header := githubTestHeaders(tc.remaining, time.Now().Add(time.Hour))
					if tc.remaining < 0 {
						header = make(http.Header)
					}
					if tc.retryAfter != "" {
						header.Set("Retry-After", tc.retryAfter)
					}
					return githubTestResponse(request, tc.status, header, tc.body), nil
				})},
			)
			body, err := githubTestGet(t.Context(), client, "https://api.github.com/repos/test/project/git/ref/tags/v1")
			var rateErr *GitHubRateError
			if errors.As(err, &rateErr) != tc.limited {
				t.Fatalf("body=%q err=%v", body, err)
			}
			if !tc.limited && body != tc.body {
				t.Fatalf("permission body changed: %q", body)
			}
			if tc.limited {
				state, _ := rate.GetRateState(t.Context())
				if state == nil || !GitHubRetryTime(state).After(time.Now()) {
					t.Fatalf("missing active retry state: %+v", state)
				}
				if tc.retryAfter == "3600" && GitHubRetryTime(state).Before(time.Now().Add(59*time.Minute)) {
					t.Fatalf("long Retry-After truncated: %+v", state)
				}
				_, _ = githubTestGet(t.Context(), client, githubTestReleases)
				if requests.Load() != 1 {
					t.Fatalf("limited request sent again: %d", requests.Load())
				}
			}
		})
	}
}

func TestGitHubResponseReadFailureReleasesRequest(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		for _, readErr := range []error{io.ErrUnexpectedEOF, context.DeadlineExceeded} {
			t.Run(fmt.Sprintf("%d/%s", status, readErr), func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					rate := NewGitHubRateCoordinator()
					body := &githubReadFailureBody{err: readErr}
					var response *http.Response
					client := rate.HTTPClient(&http.Client{Transport: roundTripFunc(
						func(request *http.Request) (*http.Response, error) {
							response = githubTestResponse(request, status, make(http.Header), "")
							response.Body = body
							return response, nil
						},
					)})
					_, err := githubTestGet(t.Context(), client,
						"https://api.github.com/repos/test/project/git/ref/tags/v1")
					if !errors.Is(err, readErr) {
						t.Errorf("request error = %v, want %v", err, readErr)
					}
					if !body.closed {
						t.Error("failed response body was not closed")
					}

					closed := make(chan struct{})
					go func() { rate.Close(); close(closed) }()
					synctest.Wait()
					select {
					case <-closed:
					default:
						t.Error("Close remained blocked after the failed request returned")
					}

					// Release a discarded response so a regression cannot hang the test itself.
					_ = response.Body.Close()
					<-closed
				})
			})
		}
	}
}

func TestGitHubGateRechecksQueuedRequestAndAllowsCancellation(t *testing.T) {
	t.Parallel()
	rate := NewGitHubRateCoordinator()
	t.Cleanup(rate.Close)
	started, release := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	client := rate.HTTPClient(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			close(started)
			<-release
			return githubTestResponse(request, 403, githubTestHeaders(0, time.Now().Add(time.Hour)), `{}`), nil
		})},
	)
	done := make(chan error, 2)
	go func() { _, err := githubTestGet(t.Context(), client, "https://api.github.com/first"); done <- err }()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	canceled := make(chan error, 1)
	go func() { _, err := githubTestGet(ctx, client, "https://api.github.com/canceled"); canceled <- err }()
	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancellation=%v", err)
	}
	go func() { _, err := githubTestGet(t.Context(), client, "https://api.github.com/second"); done <- err }()
	close(release)
	for range 2 {
		var limited *GitHubRateError
		if err := <-done; !errors.As(err, &limited) {
			t.Fatalf("queued result=%v", err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("queued requests=%d", requests.Load())
	}
}

func TestGitHubSharedFetchTimeoutAndShutdown(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rate := NewGitHubRateCoordinator()
				started := make(chan struct{})
				client := rate.HTTPClient(
					&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
						close(started)
						<-request.Context().Done()
						return nil, request.Context().Err()
					})},
				)
				done := make(chan error, 1)
				go func() { _, err := githubTestGet(t.Context(), client, githubTestReleases); done <- err }()
				<-started
				if stop {
					rate.Close()
				} else {
					time.Sleep(30 * time.Second)
				}
				err := <-done
				want := context.DeadlineExceeded
				if stop {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatalf("err=%v want=%v", err, want)
				}
				rate.Close()
			})
		})
	}
}

type githubFailingStore struct {
	mu     sync.Mutex
	writes int
}

func (s *githubFailingStore) GetValue(context.Context, string) (*string, error) { return nil, nil }

func (s *githubFailingStore) Upsert(context.Context, string, string, string) error {
	s.mu.Lock()
	s.writes++
	s.mu.Unlock()
	return errors.New("fixture database write failure")
}

func TestGitHubLimitSurvivesPersistenceFailure(t *testing.T) {
	t.Parallel()
	rate := NewGitHubRateCoordinator()
	rate.UseAppState(&githubFailingStore{})
	t.Cleanup(rate.Close)
	var requests atomic.Int32
	client := rate.HTTPClient(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			return githubTestResponse(request, 429, githubTestHeaders(30, time.Now().Add(time.Hour)), `{}`), nil
		})},
	)
	for range 2 {
		var limited *GitHubRateError
		if _, err := githubTestGet(t.Context(), client, githubTestReleases); !errors.As(err, &limited) {
			t.Fatalf("err=%v", err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestGitHubProviderSharesTransportLimit(t *testing.T) {
	t.Parallel()
	rate := NewGitHubRateCoordinator()
	t.Cleanup(rate.Close)
	var requests atomic.Int32
	httpClient := NewClientWithOptions(
		ClientOptions{
			HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests.Add(1)
				return githubTestResponse(request, 403, githubTestHeaders(0, time.Now().Add(time.Hour)), `{}`), nil
			})},
		},
	)
	provider, err := newGitHubUpdateProvider(false, httpClient, rate)
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Check(t.Context(), wailsupdater.CheckRequest{CurrentVersion: "1.0.0"})
	if err == nil {
		t.Fatal("provider ignored rate limit")
	}
	_, err = githubTestGet(t.Context(), rate.HTTPClient(httpClient.HTTPClient()), githubTestReleases)
	var limited *GitHubRateError
	if !errors.As(err, &limited) || requests.Load() != 1 {
		t.Fatalf("shared err=%v requests=%d", err, requests.Load())
	}
}

func TestGitHubCoreCaptureDoesNotOverwriteWithOtherResources(t *testing.T) {
	t.Parallel()
	rate := NewGitHubRateCoordinator()
	t.Cleanup(rate.Close)
	_, err := rate.CaptureResponse(t.Context(), githubTestHeaders(42, time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	header := githubTestHeaders(0, time.Now().Add(time.Hour))
	header.Set("X-RateLimit-Resource", "search")
	_, _ = rate.CaptureResponse(t.Context(), header)
	state, _ := rate.GetRateState(t.Context())
	if state == nil || state.Remaining != 42 {
		t.Fatalf("core overwritten: %+v", state)
	}
	old, _ := json.Marshal(GitHubRateState{Limit: 60, Remaining: 0, Reset: time.Now().Add(time.Hour).Unix()})
	if state = decodeGitHubRateState(string(old)); state == nil || !rate.IsRateLimited(state) {
		t.Fatalf("legacy record=%+v", state)
	}
}

func TestGitHubMixedPoliciesKeepCallerCacheEligibility(t *testing.T) {
	for _, leader := range []string{"automatic", "forced", "shorter age"} {
		t.Run(leader, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := newGitHubMemoryStore()
				fetchedAt := time.Now().Add(-10 * time.Minute)
				store.seedEntry(t, githubTestReleases, githubCacheEntry{
					Version: 1, Body: json.RawMessage(`[{"tag_name":"old"}]`), FetchedAt: fetchedAt,
				})
				reading, releaseRead := make(chan struct{}), make(chan struct{})
				var reads atomic.Int32
				store.beforeGet = func(ctx context.Context, key string) error {
					if strings.HasPrefix(key, githubCachePrefix) && reads.Add(1) == 1 {
						close(reading)
						select {
						case <-releaseRead:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return nil
				}
				started, releaseNetwork := make(chan struct{}), make(chan struct{})
				var requests atomic.Int32
				rate := NewGitHubRateCoordinator()
				defer rate.Close()
				rate.UseAppState(store)
				client := rate.HTTPClient(
					&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
						if requests.Add(1) == 1 {
							close(started)
						}
						select {
						case <-releaseNetwork:
							return githubTestResponse(request, 200, make(http.Header), `[{"tag_name":"new"}]`), nil
						case <-request.Context().Done():
							return nil, request.Context().Err()
						}
					})},
				)

				policies := []struct {
					name string
					ctx  context.Context
				}{
					{"automatic", t.Context()},
					{"forced", WithGitHubRefresh(t.Context(), true)},
					{"shorter age", WithGitHubMaxAge(t.Context(), 5*time.Minute)},
				}
				results := make(map[string]chan githubTestResult, len(policies))
				for _, policy := range policies {
					results[policy.name] = make(chan githubTestResult, 1)
				}
				start := func(name string) {
					for _, policy := range policies {
						if policy.name != name {
							continue
						}
						go func() {
							var result githubTestResult
							ctx := WithGitHubResponseInfo(policy.ctx, &result.info)
							result.body, result.err = githubTestGet(ctx, client, githubTestReleases)
							results[name] <- result
						}()
					}
				}
				start(leader)
				<-reading
				for _, policy := range policies {
					if policy.name != leader {
						start(policy.name)
					}
				}
				synctest.Wait()
				close(releaseRead)
				synctest.Wait()

				var automatic githubTestResult
				automaticFinished := false
				select {
				case automatic = <-results["automatic"]:
					automaticFinished = true
				default:
					t.Error("automatic caller waited for a refresh despite its eligible cached response")
				}
				select {
				case <-started:
				default:
					t.Error("stricter callers did not start a refresh")
				}
				for _, name := range []string{"forced", "shorter age"} {
					select {
					case result := <-results[name]:
						t.Errorf("%s finished before refresh: %+v", name, result)
						results[name] <- result
					default:
					}
				}
				close(releaseNetwork)
				if !automaticFinished {
					automatic = <-results["automatic"]
				}
				if automatic.err != nil || automatic.body != `[{"tag_name":"old"}]` ||
					!automatic.info.Cached || !automatic.info.FetchedAt.Equal(fetchedAt) {
					t.Errorf("automatic result = %+v", automatic)
				}
				for _, name := range []string{"forced", "shorter age"} {
					result := <-results[name]
					if result.err != nil || result.body != `[{"tag_name":"new"}]` || result.info.Cached ||
						!result.info.FetchedAt.Equal(time.Now()) {
						t.Errorf("%s result = %+v", name, result)
					}
				}
				if requests.Load() != 1 {
					t.Errorf("mixed-policy network requests = %d, want 1", requests.Load())
				}
			})
		})
	}
}

func TestGitHubShutdownCancelsAndJoinsNonmetadataAndQueuedRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newGitHubMemoryStore()
		rate := NewGitHubRateCoordinator()
		rate.UseAppState(store)
		started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var requests atomic.Int32
		client := rate.HTTPClient(
			&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if requests.Add(1) != 1 {
					return nil, errors.New("queued request reached the transport during shutdown")
				}
				close(started)
				select {
				case <-request.Context().Done():
					close(canceled)
					<-release
					return nil, request.Context().Err()
				case <-release:
					return githubTestResponse(request, 200, make(http.Header), `{}`), nil
				}
			})},
		)
		active, queued := make(chan error, 1), make(chan error, 1)
		go func() {
			_, err := githubTestGet(t.Context(), client, "https://api.github.com/repos/test/project/git/ref/tags/v1")
			active <- err
		}()
		<-started
		go func() {
			_, err := githubTestGet(t.Context(), client, "https://api.github.com/repos/test/project/git/trees/commit")
			queued <- err
		}()
		synctest.Wait()
		closed := make(chan struct{})
		go func() { rate.Close(); close(closed) }()
		synctest.Wait()
		select {
		case <-canceled:
		default:
			t.Error("shutdown did not cancel the admitted nonmetadata request")
		}
		select {
		case <-closed:
			t.Error("Close returned before the admitted transport exited")
		default:
		}
		close(release)
		synctest.Wait()
		select {
		case <-closed:
		default:
			t.Fatal("Close did not join the canceled requests")
		}
		for name, done := range map[string]chan error{"active": active, "queued": queued} {
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Errorf("%s shutdown error = %v", name, err)
			}
		}
		if requests.Load() != 1 {
			t.Errorf("shutdown network requests = %d, want only the admitted request", requests.Load())
		}

		store.close()
		for _, url := range []string{githubTestReleases, "https://api.github.com/repos/test/project/git/ref/tags/v2"} {
			ctx := WithGitHubStaleFallback(t.Context())
			if _, err := githubTestGet(ctx, client, url); !errors.Is(err, context.Canceled) {
				t.Errorf("request after Close = %v", err)
			}
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if store.afterClose != 0 {
			t.Errorf("store accesses after Close = %d", store.afterClose)
		}
	})
}

func TestGitHubShutdownRejectsStaleFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newGitHubMemoryStore()
		store.seedEntry(t, githubTestReleases, githubCacheEntry{
			Version: 1, Body: json.RawMessage(`[{"tag_name":"old"}]`), FetchedAt: time.Now().Add(-2 * time.Hour),
		})
		rate := NewGitHubRateCoordinator()
		rate.UseAppState(store)
		started := make(chan struct{})
		var requests atomic.Int32
		client := rate.HTTPClient(
			&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests.Add(1)
				close(started)
				<-request.Context().Done()
				return nil, request.Context().Err()
			})},
		)
		result := make(chan githubTestResult, 1)
		go func() {
			var value githubTestResult
			ctx := WithGitHubResponseInfo(WithGitHubStaleFallback(t.Context()), &value.info)
			value.body, value.err = githubTestGet(ctx, client, githubTestReleases)
			result <- value
		}()
		<-started
		rate.Close()
		value := <-result
		if !errors.Is(value.err, context.Canceled) || value.body != "" || value.info.Cached || value.info.Stale {
			t.Errorf("shutdown returned stale success: %+v", value)
		}
		store.close()
		if _, err := githubTestGet(
			WithGitHubStaleFallback(t.Context()),
			client,
			githubTestReleases,
		); !errors.Is(
			err,
			context.Canceled,
		) {
			t.Errorf("stale-enabled request after Close = %v", err)
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if store.afterClose != 0 || store.writes[githubCoreRateKey] != 0 || len(store.writes) != 0 {
			t.Errorf("shutdown store accesses = %d, writes = %v", store.afterClose, store.writes)
		}
		if requests.Load() != 1 {
			t.Errorf("shutdown requests = %d, want 1", requests.Load())
		}
	})
}

func TestGitHubNonmetadataBodyRetainsLifecycleUntilEOFOrClose(t *testing.T) {
	for _, finish := range []string{"EOF", "Close", "shutdown"} {
		t.Run(finish, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rate := NewGitHubRateCoordinator()
				var responseContext context.Context
				client := rate.HTTPClient(
					&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
						responseContext = request.Context()
						response := githubTestResponse(request, 200, make(http.Header), "body")
						response.Body = &githubContextReadCloser{
							ctx:    request.Context(),
							Reader: strings.NewReader("body"),
						}
						return response, nil
					})},
				)
				request, err := http.NewRequestWithContext(
					t.Context(), http.MethodGet, "https://api.github.com/repos/test/project/git/trees/commit", nil,
				)
				if err != nil {
					rate.Close()
					t.Fatal(err)
				}
				response, err := client.Do(request)
				if err != nil {
					rate.Close()
					t.Fatal(err)
				}
				defer func() { _ = response.Body.Close() }()
				if err := responseContext.Err(); err != nil {
					t.Errorf("RoundTrip canceled a live response body: %v", err)
				}
				if finish == "shutdown" {
					closed := make(chan struct{})
					go func() { rate.Close(); close(closed) }()
					synctest.Wait()
					select {
					case <-closed:
						t.Error("Close did not join the live response body")
					default:
					}
					if _, err := io.ReadAll(response.Body); !errors.Is(err, context.Canceled) {
						t.Errorf("body read after shutdown = %v", err)
					}
					_ = response.Body.Close()
					synctest.Wait()
					select {
					case <-closed:
					default:
						t.Error("Close remained blocked after the body was released")
					}
					return
				}

				if finish == "EOF" {
					body, err := io.ReadAll(response.Body)
					if err != nil || string(body) != "body" {
						t.Errorf("live body read: body=%q err=%v", body, err)
					}
				} else if err := response.Body.Close(); err != nil {
					t.Error(err)
				}
				if !errors.Is(responseContext.Err(), context.Canceled) {
					t.Errorf("%s did not release the request lifecycle: %v", finish, responseContext.Err())
				}
				_ = response.Body.Close()
				rate.Close()
			})
		})
	}
}

func TestGitHubTimeoutFailurePersistsAcrossCoordinatorRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newGitHubMemoryStore()
		started := make(chan struct{})
		var requests atomic.Int32
		base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if requests.Add(1) == 1 {
				close(started)
				<-request.Context().Done()
				return nil, request.Context().Err()
			}
			return githubTestResponse(request, 200, make(http.Header), `[{"tag_name":"recovered"}]`), nil
		})}
		rate := NewGitHubRateCoordinator()
		rate.UseAppState(store)
		client := rate.HTTPClient(base)
		attemptedAt := time.Now()
		done := make(chan error, 1)
		go func() { _, err := githubTestGet(t.Context(), client, githubTestReleases); done <- err }()
		<-started
		time.Sleep(29 * time.Second)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("fetch finished before its 30-second deadline: %v", err)
		default:
		}
		time.Sleep(time.Second)
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout error = %v", err)
		}
		rate.Close()
		entry := store.entry(t, githubTestReleases)
		if entry.Failures != 1 || !entry.AttemptedAt.Equal(attemptedAt) ||
			!entry.NextTry.Equal(time.Now().Add(time.Minute)) || !entry.FetchedAt.IsZero() || len(entry.Body) != 0 {
			t.Fatalf("persisted timeout entry = %+v", entry)
		}
		store.mu.Lock()
		rejected := store.rejectedContexts
		store.mu.Unlock()
		if rejected != 0 {
			t.Fatalf("persistence used an expired context %d times", rejected)
		}

		restarted := NewGitHubRateCoordinator()
		defer restarted.Close()
		restarted.UseAppState(store)
		client = restarted.HTTPClient(base)
		for _, advance := range []time.Duration{0, 59 * time.Second} {
			time.Sleep(advance)
			if _, err := githubTestGet(t.Context(), client, githubTestReleases); err == nil || requests.Load() != 1 {
				t.Fatalf("restart bypassed cooldown: err=%v requests=%d", err, requests.Load())
			}
		}
		time.Sleep(time.Second)
		if body, err := githubTestGet(
			t.Context(),
			client,
			githubTestReleases,
		); err != nil || body != `[{"tag_name":"recovered"}]` ||
			requests.Load() != 2 {
			t.Fatalf("cooldown recovery: body=%q err=%v requests=%d", body, err, requests.Load())
		}
		entry = store.entry(t, githubTestReleases)
		if entry.Failures != 0 || !entry.NextTry.IsZero() || !entry.FetchedAt.Equal(time.Now()) {
			t.Errorf("recovered cache entry = %+v", entry)
		}
	})
}

func TestGitHubEndpointRateFailureRestoresSharedGateAfterCoreWriteFailure(t *testing.T) {
	t.Parallel()
	for _, secondary := range []bool{false, true} {
		t.Run(fmt.Sprintf("secondary=%t", secondary), func(t *testing.T) {
			t.Parallel()
			store := newGitHubMemoryStore()
			store.failKey = githubCoreRateKey
			var clock atomic.Int64
			clock.Store(time.Now().Unix())
			now := func() time.Time { return time.Unix(clock.Load(), 0) }
			var requests atomic.Int32
			base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if requests.Add(1) > 1 {
					return githubTestResponse(request, 200, githubTestHeaders(40, now().Add(time.Hour)), `[]`), nil
				}
				if secondary {
					return githubTestResponse(
						request,
						429,
						make(http.Header),
						`{"message":"secondary rate limit"}`,
					), nil
				}
				return githubTestResponse(request, 403, githubTestHeaders(0, now().Add(time.Hour)), `{}`), nil
			})}
			rate := NewGitHubRateCoordinator()
			rate.now = now
			rate.UseAppState(store)
			_, err := githubTestGet(t.Context(), rate.HTTPClient(base), githubTestReleases)
			var initial *GitHubRateError
			if !errors.As(err, &initial) || initial.State == nil {
				rate.Close()
				t.Fatalf("initial rate failure = %v", err)
			}
			rate.Close()
			retryAt := GitHubRetryTime(initial.State)
			entry := store.entry(t, githubTestReleases)
			if entry.RateFailure == nil || entry.Failures != 1 || !entry.NextTry.Equal(retryAt) {
				t.Fatalf("persisted endpoint rate failure = %+v", entry)
			}
			store.mu.Lock()
			_, savedCore := store.values[githubCoreRateKey]
			coreAttempts := store.writes[githubCoreRateKey]
			store.mu.Unlock()
			if savedCore || coreAttempts == 0 {
				t.Fatalf(
					"selective failure did not reject core persistence: saved=%t attempts=%d",
					savedCore,
					coreAttempts,
				)
			}

			restarted := NewGitHubRateCoordinator()
			defer restarted.Close()
			restarted.now = now
			restarted.UseAppState(store)
			client := restarted.HTTPClient(base)
			for _, url := range []string{githubTestReleases, "https://api.github.com/repos/test/other/git/ref/tags/v1"} {
				_, err := githubTestGet(t.Context(), client, url)
				var limited *GitHubRateError
				if !errors.As(err, &limited) || limited.State == nil || !GitHubRetryTime(limited.State).Equal(retryAt) {
					t.Fatalf("restart rate error for %s = %v", url, err)
				}
			}
			if requests.Load() != 1 {
				t.Fatalf("reconstructed gate allowed network: requests=%d", requests.Load())
			}
			clock.Store(retryAt.Add(time.Second).Unix())
			if _, err := githubTestGet(t.Context(), client, githubTestReleases); err != nil || requests.Load() != 2 {
				t.Fatalf("rate recovery = %v, requests=%d", err, requests.Load())
			}
			entry = store.entry(t, githubTestReleases)
			if entry.RateFailure != nil || entry.Failures != 0 || !entry.NextTry.IsZero() {
				t.Errorf("successful retry retained failure = %+v", entry)
			}
		})
	}
}

func TestGitHubMetadataCacheSeparatesAcceptVersionAndQuery(t *testing.T) {
	t.Parallel()
	store := newGitHubMemoryStore()
	var requests atomic.Int32
	base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`[{"tag_name":"response-%d"}]`, requests.Add(1))
		return githubTestResponse(request, 200, make(http.Header), body), nil
	})}
	variants := []struct {
		name    string
		url     string
		accept  string
		version string
	}{
		{"baseline", githubTestReleases, "application/vnd.github+json", "2022-11-28"},
		{"accept", githubTestReleases, "application/vnd.github.raw+json", "2022-11-28"},
		{"version", githubTestReleases, "application/vnd.github+json", "2026-03-10"},
		{"query", githubTestReleases + "?per_page=100", "application/vnd.github+json", "2022-11-28"},
		{"page", githubTestReleases + "?per_page=100&page=2", "application/vnd.github+json", "2022-11-28"},
		{
			"tags",
			"https://api.github.com/repos/owner/repo/tags?per_page=100",
			"application/vnd.github+json",
			"2022-11-28",
		},
		{
			"tags page",
			"https://api.github.com/repos/owner/repo/tags?per_page=100&page=2",
			"application/vnd.github+json",
			"2022-11-28",
		},
	}
	want := make(map[string]string, len(variants))
	for restart := range 2 {
		rate := NewGitHubRateCoordinator()
		rate.UseAppState(store)
		client := rate.HTTPClient(base)
		for _, variant := range variants {
			for repeat := range 2 {
				var info GitHubResponseInfo
				request, err := http.NewRequestWithContext(
					WithGitHubResponseInfo(t.Context(), &info), http.MethodGet, variant.url, nil,
				)
				if err != nil {
					rate.Close()
					t.Fatal(err)
				}
				request.Header.Set("Accept", variant.accept)
				request.Header.Set("X-GitHub-Api-Version", variant.version)
				response, err := client.Do(request)
				if err != nil {
					rate.Close()
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if restart == 0 && repeat == 0 {
					want[variant.name] = fmt.Sprintf(`[{"tag_name":"response-%d"}]`, len(want)+1)
				}
				if err != nil || string(body) != want[variant.name] || info.Cached != (restart > 0 || repeat > 0) {
					t.Errorf(
						"%s restart=%d repeat=%d: body=%q err=%v info=%+v",
						variant.name,
						restart,
						repeat,
						body,
						err,
						info,
					)
				}
			}
		}
		rate.Close()
	}
	if requests.Load() != int32(len(variants)) {
		t.Errorf("cache identity requests = %d, want %d", requests.Load(), len(variants))
	}
}

func TestGitHubHeaderless429AfterExpiredPrimaryCreatesSecondaryBackoff(t *testing.T) {
	t.Parallel()
	store := newGitHubMemoryStore()
	now := time.Unix(time.Now().Unix(), 0)
	raw, err := json.Marshal(GitHubRateState{Remaining: 0, Reset: now.Add(-time.Minute).Unix(), Resource: "core"})
	if err != nil {
		t.Fatal(err)
	}
	store.values[githubCoreRateKey] = string(raw)
	rate := NewGitHubRateCoordinator()
	defer rate.Close()
	rate.now = func() time.Time { return now }
	rate.UseAppState(store)
	var requests atomic.Int32
	client := rate.HTTPClient(
		&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			return githubTestResponse(request, 429, make(http.Header), `{}`), nil
		})},
	)
	for _, url := range []string{githubTestReleases, "https://api.github.com/repos/test/other/releases"} {
		_, err := githubTestGet(t.Context(), client, url)
		var limited *GitHubRateError
		if !errors.As(err, &limited) || limited.State == nil ||
			GitHubRetryTime(limited.State).Before(now.Add(time.Minute)) {
			t.Fatalf("headerless 429 after expired primary: url=%s err=%v state=%+v", url, err, limited)
		}
	}
	if requests.Load() != 1 {
		t.Errorf("headerless 429 allowed another request: %d", requests.Load())
	}
}

func TestGitHubSecondaryBackoffEscalatesAcrossRestartsAndResetsOnSuccess(t *testing.T) {
	t.Parallel()
	store := newGitHubMemoryStore()
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0) }
	var requests atomic.Int32
	base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 3 {
			return githubTestResponse(request, 200, githubTestHeaders(30, now().Add(time.Hour)), `{}`), nil
		}
		return githubTestResponse(
			request, 403, githubTestHeaders(30, now().Add(time.Hour)), `{"message":"secondary rate limit"}`,
		), nil
	})}
	newCoordinator := func() *GitHubRateCoordinator {
		rate := NewGitHubRateCoordinator()
		rate.now = now
		rate.UseAppState(store)
		return rate
	}
	const endpoint = "https://api.github.com/repos/test/project/git/ref/tags/v1"
	for _, wait := range []time.Duration{time.Minute, 2 * time.Minute} {
		rate := newCoordinator()
		_, err := githubTestGet(t.Context(), rate.HTTPClient(base), endpoint)
		rate.Close()
		var limited *GitHubRateError
		if !errors.As(err, &limited) || limited.State == nil || !GitHubRetryTime(limited.State).Equal(now().Add(wait)) {
			t.Fatalf("secondary backoff after restart: want=%s err=%v state=%+v", wait, err, limited)
		}
		retryAt := GitHubRetryTime(limited.State)
		restarted := newCoordinator()
		before := requests.Load()
		_, err = githubTestGet(
			t.Context(),
			restarted.HTTPClient(base),
			"https://api.github.com/repos/test/other/releases",
		)
		restarted.Close()
		if !errors.As(err, &limited) || limited.State == nil || !GitHubRetryTime(limited.State).Equal(retryAt) ||
			requests.Load() != before {
			t.Fatalf("restart discarded active secondary backoff: err=%v requests=%d", err, requests.Load())
		}
		clock.Store(retryAt.Add(time.Second).Unix())
	}

	rate := newCoordinator()
	if _, err := githubTestGet(t.Context(), rate.HTTPClient(base), endpoint); err != nil {
		rate.Close()
		t.Fatalf("successful recovery = %v", err)
	}
	rate.Close()
	restarted := newCoordinator()
	defer restarted.Close()
	_, err := githubTestGet(t.Context(), restarted.HTTPClient(base), endpoint)
	var limited *GitHubRateError
	if !errors.As(err, &limited) || limited.State == nil ||
		!GitHubRetryTime(limited.State).Equal(now().Add(time.Minute)) {
		t.Fatalf("success did not reset persisted secondary backoff: err=%v state=%+v", err, limited)
	}
	if requests.Load() != 4 {
		t.Errorf("secondary/recovery requests = %d, want 4", requests.Load())
	}
}

func TestGitHubCorruptMetadataCacheCannotBecomeStaleSuccess(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"invalid JSON", "unsupported version", "missing timestamp", "wrong shape", "negative failures"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newGitHubMemoryStore()
			entry := githubCacheEntry{
				Version:   1,
				Body:      json.RawMessage(`[{"tag_name":"corrupt"}]`),
				FetchedAt: time.Now().Add(-2 * time.Hour),
			}
			switch name {
			case "unsupported version":
				entry.Version = 2
			case "missing timestamp":
				entry.FetchedAt = time.Time{}
			case "wrong shape":
				entry.Body = json.RawMessage(`{"tag_name":"wrong-endpoint"}`)
			case "negative failures":
				entry.Failures = -1
			}
			store.seedEntry(t, githubTestReleases, entry)
			if name == "invalid JSON" {
				store.mu.Lock()
				for key := range store.values {
					store.values[key] = "{"
				}
				store.mu.Unlock()
			}
			var requests atomic.Int32
			base := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if requests.Add(1) == 1 {
					return githubTestResponse(request, 503, make(http.Header), "unavailable"), nil
				}
				return githubTestResponse(request, 200, make(http.Header), `[{"tag_name":"repaired"}]`), nil
			})}
			rate := NewGitHubRateCoordinator()
			rate.UseAppState(store)
			var info GitHubResponseInfo
			ctx := WithGitHubResponseInfo(WithGitHubStaleFallback(t.Context()), &info)
			if body, err := githubTestGet(
				ctx,
				rate.HTTPClient(base),
				githubTestReleases,
			); err == nil || info.Stale || info.Cached || strings.Contains(body, "corrupt") ||
				strings.Contains(body, "wrong-endpoint") {
				rate.Close()
				t.Fatalf("corruption served as stale success: body=%q err=%v info=%+v", body, err, info)
			}
			rate.Close()
			restarted := NewGitHubRateCoordinator()
			defer restarted.Close()
			restarted.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
			restarted.UseAppState(store)
			client := restarted.HTTPClient(base)
			for range 2 {
				if body, err := githubTestGet(
					t.Context(),
					client,
					githubTestReleases,
				); err != nil ||
					body != `[{"tag_name":"repaired"}]` {
					t.Fatalf("corrupt cache recovery: body=%q err=%v", body, err)
				}
			}
			if requests.Load() != 2 {
				t.Errorf("corrupt cache requests = %d, want failed refresh plus one repair", requests.Load())
			}
		})
	}
}

type githubTestResult struct {
	body string
	err  error
	info GitHubResponseInfo
}

type githubReadFailureBody struct {
	err    error
	closed bool
}

func (b *githubReadFailureBody) Read([]byte) (int, error) { return 0, b.err }

func (b *githubReadFailureBody) Close() error {
	b.closed = true
	return nil
}

type githubContextReadCloser struct {
	ctx context.Context
	io.Reader
}

func (b *githubContextReadCloser) Read(buffer []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.Reader.Read(buffer)
}

func (b *githubContextReadCloser) Close() error { return nil }

// All data and synchronization belong to one test, including inside synctest bubbles.
type githubMemoryStore struct {
	mu               sync.Mutex
	values           map[string]string
	writes           map[string]int
	beforeGet        func(context.Context, string) error
	failKey          string
	closed           bool
	afterClose       int
	rejectedContexts int
}

func newGitHubMemoryStore() *githubMemoryStore {
	return &githubMemoryStore{values: make(map[string]string), writes: make(map[string]int)}
}

func (s *githubMemoryStore) GetValue(ctx context.Context, key string) (*string, error) {
	if s.beforeGet != nil {
		if err := s.beforeGet(ctx, key); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.afterClose++
		return nil, errors.New("fixture store is closed")
	}
	if err := ctx.Err(); err != nil {
		s.rejectedContexts++
		return nil, err
	}
	value, found := s.values[key]
	if !found {
		return nil, nil
	}
	return &value, nil
}

func (s *githubMemoryStore) Upsert(ctx context.Context, key, value, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.afterClose++
		return errors.New("fixture store is closed")
	}
	if err := ctx.Err(); err != nil {
		s.rejectedContexts++
		return err
	}
	s.writes[key]++
	if key == s.failKey {
		return errors.New("fixture core-rate write failure")
	}
	s.values[key] = value
	return nil
}

func (s *githubMemoryStore) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func (s *githubMemoryStore) seedEntry(t *testing.T, url string, entry githubCacheEntry) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.values[githubMetadataCacheKey(request)] = string(raw)
	s.mu.Unlock()
}

func (s *githubMemoryStore) entry(t *testing.T, url string) githubCacheEntry {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	raw, found := s.values[githubMetadataCacheKey(request)]
	s.mu.Unlock()
	if !found {
		t.Fatal("metadata cache entry was not persisted")
	}
	var entry githubCacheEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}
