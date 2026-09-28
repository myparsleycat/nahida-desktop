package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
)

func TestReleaseTagsCachesForProcessLifetimeAndHonorsRefreshCooldown(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(*http.Request) (int, string) {
		requests.Add(1)
		return http.StatusOK, testReleases
	})
	now := time.Now()
	client.tags.now = func() time.Time { return now }
	ctx := context.Background()

	for range 2 {
		tags, err := client.ReleaseTags(ctx, testRepo, false)
		if err != nil || strings.Join(tags, ",") != "v2,v1" {
			t.Fatalf("tags = %v, %v", tags, err)
		}
	}
	now = now.Add(2 * tagRefreshCooldown)
	if _, err := client.ReleaseTags(ctx, testRepo, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("non-refresh read refetched: %d", requests.Load())
	}

	if _, err := client.ReleaseTags(ctx, testRepo, true); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("refresh after cooldown requests = %d, want 2", requests.Load())
	}
	if _, err := client.ReleaseTags(ctx, testRepo, true); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("refresh ignored cooldown: %d", requests.Load())
	}
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

func TestReleaseTagsDoesNotCacheFailures(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, func(*http.Request) (int, string) {
		if requests.Add(1) == 1 {
			return http.StatusBadGateway, "bad gateway"
		}
		return http.StatusOK, testReleases
	})
	if _, err := client.ReleaseTags(context.Background(), testRepo, false); err == nil {
		t.Fatal("expected first fetch to fail")
	}
	tags, err := client.ReleaseTags(context.Background(), testRepo, false)
	if err != nil || strings.Join(tags, ",") != "v2,v1" {
		t.Fatalf("tags = %v, %v", tags, err)
	}
}
