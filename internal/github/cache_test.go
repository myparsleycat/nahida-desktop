package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"nahida.live/desktop/internal/infra"
)

func TestReleaseTagsHonorsHourlyCacheAndRefreshCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		client := newTestClient(t, func(*http.Request) (int, string) {
			requests.Add(1)
			return http.StatusOK, testReleases
		})
		ctx := context.Background()
		for range 2 {
			if _, err := client.ReleaseTags(ctx, testRepo, false); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(2 * time.Minute)
		if _, err := client.ReleaseTags(ctx, testRepo, false); err != nil {
			t.Fatal(err)
		}
		if requests.Load() != 1 {
			t.Fatalf("ordinary requests = %d", requests.Load())
		}
		for range 2 {
			if _, err := client.ReleaseTags(ctx, testRepo, true); err != nil {
				t.Fatal(err)
			}
		}
		if requests.Load() != 2 {
			t.Fatalf("refresh requests = %d", requests.Load())
		}
		time.Sleep(time.Hour)
		if _, err := client.ReleaseTags(ctx, testRepo, false); err != nil {
			t.Fatal(err)
		}
		if requests.Load() != 3 {
			t.Fatalf("expired requests = %d", requests.Load())
		}
	})
}

func TestTagsHonorsCacheRefreshAndStaleFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		unavailable := false
		client := newTestClient(t, func(*http.Request) (int, string) {
			requests.Add(1)
			if unavailable {
				return http.StatusServiceUnavailable, "unavailable"
			}
			return http.StatusOK, `[{"name":"v6.8.0"}]`
		})
		fetch := func(refresh bool) {
			t.Helper()
			ctx := infra.WithGitHubRefresh(infra.WithGitHubStaleFallback(t.Context()), refresh)
			tags, err := client.Tags(ctx, testRepo)
			if err != nil || strings.Join(tags, ",") != "v6.8.0" {
				t.Fatalf("tags = %v, %v", tags, err)
			}
		}
		fetch(false)
		fetch(false)
		time.Sleep(2 * time.Minute)
		fetch(false)
		if requests.Load() != 1 {
			t.Fatalf("ordinary requests = %d, want 1", requests.Load())
		}
		fetch(true)
		fetch(true)
		if requests.Load() != 2 {
			t.Fatalf("refresh requests = %d, want 2", requests.Load())
		}
		time.Sleep(time.Hour)
		unavailable = true
		fetch(false)
		if requests.Load() != 3 {
			t.Fatalf("stale requests = %d, want 3", requests.Load())
		}
	})
}

func TestNilClientReportsMissingConfiguration(t *testing.T) {
	var client *Client
	if client.Configured() {
		t.Fatal("nil client reported as configured")
	}
	if _, err := client.ReleaseTags(context.Background(), testRepo, false); !errors.Is(err, errHTTPNotConfigured) {
		t.Fatalf("err = %v, want errHTTPNotConfigured", err)
	}
	if New(Options{Download: infra.NewDownload()}).Configured() {
		t.Fatal("client without HTTP reported as configured")
	}
}

func TestReleaseTagsReturnsIndependentCopies(t *testing.T) {
	client := newTestClient(t, func(*http.Request) (int, string) { return http.StatusOK, testReleases })
	first, err := client.ReleaseTags(context.Background(), testRepo, false)
	if err != nil {
		t.Fatal(err)
	}
	first[0] = "mutated"
	second, err := client.ReleaseTags(context.Background(), testRepo, false)
	if err != nil || second[0] != "v2" {
		t.Fatalf("second = %v, %v", second, err)
	}
}

func TestCachedReleasesPreservesDetailsAndReturnsIndependentAssets(t *testing.T) {
	client := newTestClient(t, func(*http.Request) (int, string) {
		return http.StatusOK, `[{"tag_name":"v2","body":"release notes","assets":[{"name":"package.zip"}]}]`
	})
	first, err := client.CachedReleases(context.Background(), testRepo, false)
	if err != nil || len(first) != 1 || first[0].Body != "release notes" {
		t.Fatalf("releases = %v, err = %v", first, err)
	}
	first[0].Assets[0].Name = "changed"
	second, err := client.CachedReleases(context.Background(), testRepo, false)
	if err != nil || second[0].Assets[0].Name != "package.zip" {
		t.Fatalf("cached releases = %v, err = %v", second, err)
	}
}

func TestReleaseTagsDeduplicatesInFlightFetch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	client := New(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		Status: infra.BackendOnline,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if requests.Add(1) == 1 {
				close(started)
			}
			<-release
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
				Body: io.NopCloser(strings.NewReader(`[{"tag_name":"v1"}]`)),
			}, nil
		})},
	})})

	results := make(chan error, 2)
	fetch := func() {
		_, err := client.ReleaseTags(context.Background(), testRepo, false)
		results <- err
	}
	go fetch()
	<-started
	go fetch()
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestReleaseTagsWaiterHonorsOwnCancellation(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	client := New(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		Status: infra.BackendOnline,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-release
			return &http.Response{
				StatusCode: http.StatusOK, Header: make(http.Header), Request: request, Body: http.NoBody,
			}, nil
		})},
	})})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ReleaseTags(ctx, testRepo, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestReleaseTagsBacksOffFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		client := newTestClient(t, func(*http.Request) (int, string) {
			if requests.Add(1) == 1 {
				return http.StatusBadGateway, "bad gateway"
			}
			return http.StatusOK, testReleases
		})
		for range 2 {
			if _, err := client.ReleaseTags(context.Background(), testRepo, false); err == nil {
				t.Fatal("expected failure")
			}
		}
		if requests.Load() != 1 {
			t.Fatalf("failed requests = %d", requests.Load())
		}
		time.Sleep(time.Minute)
		tags, err := client.ReleaseTags(context.Background(), testRepo, false)
		if err != nil || strings.Join(tags, ",") != "v2,v1" {
			t.Fatalf("tags = %v, %v", tags, err)
		}
	})
}
