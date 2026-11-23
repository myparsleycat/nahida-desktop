// Package httpcache is a private HTTP cache on disk for remote images, following RFC 9111 as browsers do.
// A response stays fresh for its max-age or Expires lifetime, or for a tenth of its age since Last-Modified
// (at most a week) when it declares neither. A stale entry is revalidated with If-None-Match and
// If-Modified-Since, and is used anyway when the server cannot be reached or answers with a server error,
// unless it is marked must-revalidate or no-cache.
package httpcache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"nahida.live/desktop/internal/infra"
)

const (
	defaultMaxBodyBytes = 32 << 20
	revalidateTimeout   = 30 * time.Second
)

type Options struct {
	Client *infra.Client
	Log    *infra.Log
	// Dir holds the entry files. When it is empty, nothing is stored and every Get goes to the network.
	Dir      string
	MaxBytes int64
}

// Request describes what a caller accepts.
type Request struct {
	// MaxBytes limits the response body; a larger body fails the Get. Zero means 32 MiB.
	MaxBytes int64
	// Validate accepts a response before Get returns it, whether it came from the network or the cache. A
	// network response is stored only when the Validate of the request that fetched it accepts it.
	Validate func(status int, header http.Header, body []byte) error
}

// Cache is safe for concurrent use.
type Cache struct {
	client *infra.Client
	log    *infra.Log
	dir    string
	now    func() time.Time

	flight        singleflight.Group
	storeFailures infra.DiagnosticThrottle
	staleServed   infra.DiagnosticThrottle

	ctx        context.Context
	cancel     context.CancelFunc
	background sync.WaitGroup

	mu       sync.Mutex
	closed   bool
	maxBytes int64
	index    map[string]indexEntry
	total    int64
	scanned  bool
	busy     map[string]int
	// changedBeforeScan holds the entries written or removed before the first scan finished.
	changedBeforeScan map[string]struct{}
	// clearPending holds the busy entries Clear could not remove yet.
	clearPending map[string]struct{}
}

type response struct {
	status int
	header http.Header
	body   []byte
	// stored reports that body came from an entry on disk rather than from the network.
	stored bool
}

// ResponseError is a response whose body could not be read, at stage read, or was larger than
// Request.MaxBytes, at stage size.
type ResponseError struct {
	Stage  string
	Status int
	Err    error
}

func (e *ResponseError) Error() string { return e.Err.Error() }

func (e *ResponseError) Unwrap() error { return e.Err }

// New returns a cache over opts.Dir and starts indexing the entries already there.
func New(opts Options) *Cache {
	return newCache(opts, time.Now)
}

func newCache(opts Options, now func() time.Time) *Cache {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Cache{
		client:   opts.Client,
		log:      opts.Log,
		dir:      opts.Dir,
		now:      now,
		ctx:      ctx,
		cancel:   cancel,
		maxBytes: opts.MaxBytes,
		index:    map[string]indexEntry{},
		busy:     map[string]int{},

		changedBeforeScan: map[string]struct{}{},
		clearPending:      map[string]struct{}{},
	}
	if c.dir != "" {
		c.background.Go(c.scan)
	}
	return c
}

// Close stops background revalidation and indexing and waits for them.
func (c *Cache) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.cancel()
	c.background.Wait()
}

// Get returns the body of rawURL from the cache when it is fresh, and otherwise from the network,
// revalidating a stored copy when there is one.
func (c *Cache) Get(ctx context.Context, rawURL string, req Request) ([]byte, error) {
	if req.MaxBytes <= 0 {
		req.MaxBytes = defaultMaxBodyBytes
	}
	key := cacheKey(rawURL)
	r, err := c.share(ctx, key, req)
	if err == nil && r.stored && c.accept(key, r, req) != nil {
		// A request with another Validate stored what this one rejects; the entry is dropped and fetched again.
		c.remove(entryName(key))
		r, err = c.share(ctx, key, req)
	}
	if err != nil {
		return nil, err
	}
	if err := c.accept(key, r, req); err != nil {
		return nil, err
	}
	return r.body, nil
}

// share loads key once for all concurrent requests for it.
func (c *Cache) share(ctx context.Context, key string, req Request) (response, error) {
	result, err, _ := c.flight.Do(key, func() (any, error) { return c.load(ctx, key, req) })
	r, _ := result.(response)
	return r, err
}

// accept applies the request's own limits to a response that may have been loaded for another request.
func (c *Cache) accept(key string, r response, req Request) error {
	if int64(len(r.body)) > req.MaxBytes {
		return infra.AnnotateError(
			&ResponseError{Stage: "size", Status: r.status, Err: fmt.Errorf("response exceeds %d bytes", req.MaxBytes)},
			infra.Diagnostic{
				Operation: "http-cache", Stage: "size",
				Fields: map[string]any{"url": infra.SanitizeLogURL(key), "status": r.status},
			},
		)
	}
	if req.Validate != nil {
		return req.Validate(r.status, r.header, r.body)
	}
	return nil
}

func (c *Cache) load(ctx context.Context, key string, req Request) (response, error) {
	if c.dir == "" {
		return c.fetch(ctx, key, "", nil, req)
	}
	name := entryName(key)
	c.hold(name)
	defer c.release(name)

	cached := c.read(ctx, name, key)
	if cached != nil {
		now := c.now()
		switch {
		case cached.fresh(now):
			c.touch(name)
			return cached.response(), nil
		case cached.withinStaleWhileRevalidate(now):
			c.touch(name)
			c.revalidateLater(key, req)
			return cached.response(), nil
		}
	}
	return c.fetch(ctx, key, name, cached, req)
}

// fetch requests key from the network, conditionally when cached is not nil, and stores what may be stored.
func (c *Cache) fetch(ctx context.Context, key, name string, cached *entry, req Request) (response, error) {
	header := http.Header{}
	if cached != nil {
		if etag := cached.Header.Get("ETag"); etag != "" {
			header.Set("If-None-Match", etag)
		}
		if modified := cached.Header.Get("Last-Modified"); modified != "" {
			header.Set("If-Modified-Since", modified)
		}
	}
	requested := c.now()
	resp, err := c.client.Fetch(ctx, key, infra.FetchOptions{Header: header, DisableHTTPErrors: true})
	if err != nil {
		if cached != nil && cached.servableOnError() && !infra.IsCancellationError(err) {
			c.noteStale(key, err)
			return cached.response(), nil
		}
		return response{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	received := c.now()

	switch {
	case cached != nil && resp.StatusCode == http.StatusNotModified:
		_, _ = io.Copy(io.Discard, resp.Body)
		cached.refresh(resp.Header, requested, received)
		c.write(ctx, name, cached)
		return cached.response(), nil
	case cached != nil && resp.StatusCode >= http.StatusInternalServerError && cached.servableOnError():
		c.noteStale(key, fmt.Errorf("revalidation response: HTTP %d", resp.StatusCode))
		return cached.response(), nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, req.MaxBytes+1))
	if err != nil {
		return response{}, infra.AnnotateError(
			&ResponseError{Stage: "read", Status: resp.StatusCode, Err: fmt.Errorf("read response: %w", err)},
			infra.Diagnostic{
				Operation: "http-cache", Stage: "read",
				Fields: map[string]any{
					"url": infra.SanitizeLogURL(key), "status": resp.StatusCode, "bytes": len(body),
					"contentType": resp.Header.Get("Content-Type"),
				},
			},
		)
	}
	fetched := response{status: resp.StatusCode, header: resp.Header, body: body}
	if name != "" && (resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone) {
		c.remove(name)
	}
	if err := c.accept(key, fetched, req); err != nil {
		// A proxy or captive portal can answer with its own page instead of the resource.
		success := resp.StatusCode >= 200 && resp.StatusCode < 300
		if cached != nil && success && cached.servableOnError() {
			c.noteStale(key, err)
			return cached.response(), nil
		}
		return fetched, nil
	}

	if name != "" && storable(resp.StatusCode, resp.Header) {
		c.write(ctx, name, &entry{
			URL: key, Status: resp.StatusCode, Header: keptHeaders(resp.Header),
			RequestTime: requested, ResponseTime: received, body: body,
		})
	}
	return fetched, nil
}

// revalidateLater refreshes key in the background after a stale entry was used under stale-while-revalidate.
func (c *Cache) revalidateLater(key string, req Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.background.Go(func() {
		ctx, cancel := context.WithTimeout(c.ctx, revalidateTimeout)
		defer cancel()
		_, _, _ = c.flight.Do("revalidate:"+key, func() (any, error) {
			name := entryName(key)
			c.hold(name)
			defer c.release(name)
			if cached := c.read(ctx, name, key); cached != nil && !cached.fresh(c.now()) {
				_, _ = c.fetch(ctx, key, name, cached, req)
			}
			return nil, nil
		})
	})
}

func (c *Cache) noteStale(key string, err error) {
	host := ""
	if u, parseErr := url.Parse(key); parseErr == nil {
		host = u.Host
	}
	c.staleServed.Report(c.log, err, "HTTPCache", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "http-cache", Stage: "serve-stale",
		Fields: map[string]any{"path": host, "url": infra.SanitizeLogURL(key)},
	})
}

func (e *entry) response() response {
	return response{status: e.Status, header: e.Header.Clone(), body: e.body, stored: true}
}
