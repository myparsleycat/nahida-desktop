package httpcache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type testOrigin struct {
	server   *httptest.Server
	requests atomic.Int32
}

// newTestCache serves handler and returns a cache over a fresh directory whose clock the test controls.
func newTestCache(
	t *testing.T,
	handler func(w http.ResponseWriter, r *http.Request, now time.Time),
) (*Cache, *testClock, *testOrigin) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	origin := &testOrigin{}
	origin.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin.requests.Add(1)
		handler(w, r, clock.Now())
	}))
	t.Cleanup(origin.server.Close)
	noRetry := 0
	c := newCache(Options{
		Client: infra.NewClientWithOptions(
			infra.ClientOptions{HTTPClient: origin.server.Client(), RetryLimit: &noRetry},
		),
		Dir: t.TempDir(),
	}, clock.Now)
	t.Cleanup(c.Close)
	return c, clock, origin
}

func successOnly(status int, _ http.Header, _ []byte) error {
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

func get(t *testing.T, c *Cache, url string, validate func(int, http.Header, []byte) error) ([]byte, error) {
	t.Helper()
	return c.Get(context.Background(), url, Request{Validate: validate})
}

func TestHeuristicFreshnessThenRevalidation(t *testing.T) {
	t.Parallel()
	picture := []byte("picture-bytes")
	var conditional atomic.Int32
	c, clock, origin := newTestCache(t, func(w http.ResponseWriter, r *http.Request, now time.Time) {
		w.Header().Set("Date", now.Format(http.TimeFormat))
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", now.Add(-100*24*time.Hour).Format(http.TimeFormat))
		_, _ = w.Write(picture)
	})
	url := origin.server.URL + "/a.png"

	for range 2 {
		if body, err := get(t, c, url, successOnly); err != nil || !bytes.Equal(body, picture) {
			t.Fatalf("Get() = %q, %v", body, err)
		}
	}
	if got := origin.requests.Load(); got != 1 {
		t.Fatalf("origin served %d requests while the entry was fresh, want 1", got)
	}

	// A tenth of 100 days exceeds the one-week cap, so the entry is stale after eight days.
	clock.Advance(8 * 24 * time.Hour)
	for range 2 {
		if body, err := get(t, c, url, successOnly); err != nil || !bytes.Equal(body, picture) {
			t.Fatalf("Get() after expiry = %q, %v", body, err)
		}
	}
	if got, cond := origin.requests.Load(), conditional.Load(); got != 2 || cond != 1 {
		t.Fatalf("origin served %d requests (%d conditional), want one revalidation that refreshes the entry",
			got, cond)
	}
}

func TestResponsesThatMustNotBeStored(t *testing.T) {
	t.Parallel()
	c, _, origin := newTestCache(t, func(w http.ResponseWriter, r *http.Request, _ time.Time) {
		w.Header().Set("Cache-Control", "max-age=3600")
		if r.URL.Path == "/no-store" {
			w.Header().Set("Cache-Control", "no-store")
		}
		_, _ = w.Write([]byte("<html>blocked</html>"))
	})
	rejectHTML := func(_ int, _ http.Header, body []byte) error {
		if bytes.HasPrefix(body, []byte("<html")) {
			return fmt.Errorf("not an image")
		}
		return nil
	}

	for range 2 {
		if _, err := get(t, c, origin.server.URL+"/no-store", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := get(t, c, origin.server.URL+"/page", rejectHTML); err == nil {
		t.Fatal("Get() accepted a response its Validate rejected")
	}
	if _, err := get(t, c, origin.server.URL+"/page", nil); err != nil {
		t.Fatal(err)
	}
	if got := origin.requests.Load(); got != 4 {
		t.Fatalf("origin served %d requests, want 4: neither no-store nor a rejected response may be cached", got)
	}
}

func TestStaleEntryServedWhenRevalidationFails(t *testing.T) {
	t.Parallel()
	var failing atomic.Bool
	c, clock, origin := newTestCache(t, func(w http.ResponseWriter, r *http.Request, now time.Time) {
		if failing.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Date", now.Format(http.TimeFormat))
		w.Header().Set("Cache-Control", "max-age=60")
		if r.URL.Path == "/strict" {
			w.Header().Set("Cache-Control", "max-age=60, must-revalidate")
		}
		_, _ = w.Write([]byte("cached " + r.URL.Path))
	})

	for _, path := range []string{"/lenient", "/strict"} {
		if _, err := get(t, c, origin.server.URL+path, successOnly); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(2 * time.Minute)
	failing.Store(true)

	body, err := get(t, c, origin.server.URL+"/lenient", successOnly)
	if err != nil || string(body) != "cached /lenient" {
		t.Fatalf("Get() during a server error = %q, %v, want the stale entry", body, err)
	}
	if _, err := get(t, c, origin.server.URL+"/strict", successOnly); err == nil {
		t.Fatal("Get() served a must-revalidate entry that could not be validated")
	}

	origin.server.Close()
	body, err = get(t, c, origin.server.URL+"/lenient", successOnly)
	if err != nil || string(body) != "cached /lenient" {
		t.Fatalf("Get() with the origin unreachable = %q, %v, want the stale entry", body, err)
	}
}

func TestDamagedEntryIsFetchedAgain(t *testing.T) {
	t.Parallel()
	c, _, origin := newTestCache(t, func(w http.ResponseWriter, _ *http.Request, _ time.Time) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("whole picture"))
	})
	url := origin.server.URL + "/a.png"
	if _, err := get(t, c, url, nil); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(c.dir, entryName(cacheKey(url)))
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data[:len(data)-3], 0o600); err != nil {
		t.Fatal(err)
	}

	body, err := get(t, c, url, nil)
	if err != nil || string(body) != "whole picture" {
		t.Fatalf("Get() over a truncated entry = %q, %v", body, err)
	}
	if got := origin.requests.Load(); got != 2 {
		t.Fatalf("origin served %d requests, want the truncated entry fetched again", got)
	}
	if _, err := get(t, c, url, nil); err != nil || origin.requests.Load() != 2 {
		t.Fatalf("the refetched entry was not stored again: %v, %d requests", err, origin.requests.Load())
	}
}

func TestEvictionKeepsRecentlyUsedEntries(t *testing.T) {
	t.Parallel()
	c, clock, origin := newTestCache(t, func(w http.ResponseWriter, _ *http.Request, _ time.Time) {
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(bytes.Repeat([]byte{'x'}, 4000))
	})
	waitScanned(t, c)

	load := func(name string) {
		t.Helper()
		clock.Advance(time.Second)
		if _, err := get(t, c, origin.server.URL+"/"+name, nil); err != nil {
			t.Fatal(err)
		}
	}
	load("a")
	load("b")
	entrySize := c.Usage() / 2
	c.SetMaxBytes(entrySize*2 + entrySize/2)
	load("a")
	load("c")

	stored := func(name string) bool {
		_, err := os.Stat(filepath.Join(c.dir, entryName(cacheKey(origin.server.URL+"/"+name))))
		return err == nil
	}
	if !stored("a") || stored("b") || !stored("c") {
		t.Fatalf("stored a=%v b=%v c=%v, want the least recently used b evicted", stored("a"), stored("b"), stored("c"))
	}
	if usage := c.Usage(); usage > entrySize*2+entrySize/2 {
		t.Fatalf("Usage() = %d over the limit", usage)
	}
}

func waitScanned(t *testing.T, c *Cache) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		scanned := c.scanned
		c.mu.Unlock()
		if scanned {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the cache index")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStoredResponseRejectedByLaterRequestIsFetchedAgain(t *testing.T) {
	t.Parallel()
	var served atomic.Int32
	c, _, origin := newTestCache(t, func(w http.ResponseWriter, _ *http.Request, _ time.Time) {
		w.Header().Set("Cache-Control", "max-age=3600")
		if served.Add(1) == 1 {
			_, _ = w.Write([]byte("<html>blocked</html>"))
			return
		}
		_, _ = w.Write([]byte("picture"))
	})
	url := origin.server.URL + "/a.png"
	rejectHTML := func(_ int, _ http.Header, body []byte) error {
		if bytes.HasPrefix(body, []byte("<html")) {
			return fmt.Errorf("not an image")
		}
		return nil
	}

	if _, err := get(t, c, url, nil); err != nil {
		t.Fatal(err)
	}
	body, err := get(t, c, url, rejectHTML)
	if err != nil || string(body) != "picture" {
		t.Fatalf("Get() over a stored page its Validate rejects = %q, %v, want the refetched picture", body, err)
	}
	if body, err := get(t, c, url, rejectHTML); err != nil || string(body) != "picture" {
		t.Fatalf("Get() after the refetch = %q, %v, want the stored picture", body, err)
	}
}

func TestCancelledCallerLeavesSharedLoadRunning(t *testing.T) {
	t.Parallel()
	arrived, release := make(chan struct{}, 1), make(chan struct{})
	defer close(release)
	c, _, origin := newTestCache(t, func(w http.ResponseWriter, _ *http.Request, _ time.Time) {
		arrived <- struct{}{}
		<-release
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write([]byte("picture"))
	})
	url := origin.server.URL + "/a.png"

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := c.Get(firstCtx, url, Request{})
		firstErr <- err
	}()
	<-arrived
	type result struct {
		body []byte
		err  error
	}
	second := make(chan result, 1)
	go func() {
		body, err := c.Get(context.Background(), url, Request{})
		second <- result{body, err}
	}()
	waitWaiters(t, c, 2)

	cancelFirst()
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Get() = %v, want context.Canceled", err)
	}
	release <- struct{}{}
	if r := <-second; r.err != nil || string(r.body) != "picture" {
		t.Fatalf("other Get() = %q, %v, want the shared load to finish", r.body, r.err)
	}
	if got := origin.requests.Load(); got != 1 {
		t.Fatalf("origin served %d requests, want one shared request", got)
	}
}

func waitWaiters(t *testing.T, c *Cache, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		waiters := 0
		for _, shared := range c.calls {
			waiters += shared.waiters
		}
		c.mu.Unlock()
		if waiters == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d callers of a shared load", want)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestScanSkipsEntriesRemovedAfterListing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := entryName("https://example.com/a.png")
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}
	// Without a Dir, New starts no scan, so this test runs its two halves around the removal.
	c := newCache(Options{}, time.Now)
	c.dir = dir

	found, ok := c.listEntries()
	if !ok || len(found) != 1 {
		t.Fatalf("listEntries() = %v, %v, want the one entry", found, ok)
	}
	c.remove(name)
	c.indexFound(found)
	if usage := c.Usage(); usage != 0 {
		t.Fatalf("Usage() = %d after the listed entry was removed, want 0", usage)
	}
}
