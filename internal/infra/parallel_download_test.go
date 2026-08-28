package infra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type parallelRoundTrip func(*http.Request) (*http.Response, error)

func (f parallelRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParallelSchedulerRebalancesLargestRunningSegment(t *testing.T) {
	savePath := filepath.Join(t.TempDir(), "adaptive.bin")
	segment := &parallelSegment{
		id: 0, start: 0, end: int64(parallelMinSegmentSize*3 - 1),
		chunkPath: savePath + ".chunk0", status: "running",
	}
	completed := int64(parallelMinSegmentSize)
	segment.transferred.Store(completed)
	canceled := make(chan struct{})
	segment.setAttemptCancel(func() { close(canceled) })
	scheduler := newParallelScheduler([]*parallelSegment{segment}, true, savePath)

	acquired := make(chan *parallelSegment, 1)
	go func() { acquired <- scheduler.acquire(context.Background()) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("idle worker did not request a rebalance")
	}
	if err := os.WriteFile(segment.chunkPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(segment.chunkPath, completed); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.split(segment, os.RemoveAll); err != nil {
		t.Fatal(err)
	}

	var next *parallelSegment
	select {
	case next = <-acquired:
	case <-time.After(time.Second):
		t.Fatal("idle worker did not acquire a rebalanced segment")
	}
	if segment.end != completed-1 || next == nil || next.start != completed {
		t.Fatalf("completed=%d..%d next=%#v", segment.start, segment.end, next)
	}
	ordered := scheduler.ordered()
	if len(ordered) != 3 || ordered[1].start != completed || ordered[2].end != int64(parallelMinSegmentSize*3-1) {
		t.Fatalf("segments = %#v", ordered)
	}
}

func textResponse(req *http.Request, status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestParallelDownloaderRunsSegmentsConcurrentlyAndReportsProgress(t *testing.T) {
	const size = int64(12 * 1024 * 1024)
	var active atomic.Int32
	var peak atomic.Int32
	downloader := NewParallelDownloader()
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		var start, end int64
		if _, err := fmt.Sscanf(req.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			return nil, err
		}
		current := active.Add(1)
		for current > peak.Load() && !peak.CompareAndSwap(peak.Load(), current) {
		}
		time.Sleep(30 * time.Millisecond)
		active.Add(-1)
		length := end - start + 1
		return &http.Response{
			StatusCode: http.StatusPartialContent,
			Status:     "206 Partial Content",
			Header:     http.Header{"Content-Range": []string{fmt.Sprintf("bytes %d-%d/%d", start, end, size)}},
			Body:       io.NopCloser(bytes.NewReader(make([]byte, length))),
			Request:    req,
		}, nil
	})}
	destination := filepath.Join(t.TempDir(), "parallel.bin")
	var progress int64
	if err := downloader.Download(context.Background(), ParallelDownloadOptions{
		URL: "https://example.test/parallel.bin", SavePath: destination, FileSize: size, MaxChunks: 2,
		OnProgress: func(bytes int64) { progress += bytes },
	}); err != nil {
		t.Fatal(err)
	}
	if peak.Load() < 2 {
		t.Fatalf("peak concurrent requests = %d, want at least 2", peak.Load())
	}
	if progress != size {
		t.Fatalf("progress = %d, want %d", progress, size)
	}
	if info, err := os.Stat(destination); err != nil || info.Size() != size {
		t.Fatalf("downloaded file = %#v, error = %v", info, err)
	}

	active.Store(0)
	peak.Store(0)
	downloader.SetRequestConcurrency(1)
	limitedDestination := filepath.Join(t.TempDir(), "limited.bin")
	if err := downloader.Download(context.Background(), ParallelDownloadOptions{
		URL: "https://example.test/limited.bin", SavePath: limitedDestination, FileSize: size, MaxChunks: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 1 {
		t.Fatalf("request-limited peak = %d, want 1", peak.Load())
	}
}

func TestParallelDownloaderDoesNotCacheNegativeRangeProbes(t *testing.T) {
	var heads int
	var headerURLs []string
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(rawURL string) (map[string]string, error) {
		headerURLs = append(headerURLs, rawURL)
		return map[string]string{"Authorization": "Bearer token"}, nil
	}
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodHead {
			t.Fatalf("method = %s", req.Method)
		}
		heads++
		return textResponse(req, 200, nil, ""), nil
	})}

	ok, err := downloader.CheckRangeSupport(context.Background(), "https://example.test/files/abc?signature=one")
	if err != nil || ok {
		t.Fatalf("first probe = %v %v", ok, err)
	}
	ok, err = downloader.CheckRangeSupport(context.Background(), "https://example.test/files/abc?signature=two")
	if err != nil || ok {
		t.Fatalf("second probe = %v %v", ok, err)
	}
	if heads != 2 || len(headerURLs) != 2 {
		t.Fatalf("heads=%d headers=%d", heads, len(headerURLs))
	}
}

func TestParallelDownloaderReusesTheProbeForSignedURLsThatShareAResourcePath(t *testing.T) {
	var heads int
	var firstURL string
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(rawURL string) (map[string]string, error) {
		return map[string]string{"Authorization": "Bearer token"}, nil
	}
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		heads++
		if firstURL == "" {
			firstURL = req.URL.String()
		}
		header := make(http.Header)
		header.Set("Accept-Ranges", "bytes")
		return textResponse(req, 200, header, ""), nil
	})}

	ok, err := downloader.CheckRangeSupport(context.Background(), "https://example.test/files/abc?signature=one")
	if err != nil || !ok {
		t.Fatalf("first probe = %v %v", ok, err)
	}
	ok, err = downloader.CheckRangeSupport(context.Background(), "https://example.test/files/abc?signature=two")
	if err != nil || !ok {
		t.Fatalf("second probe = %v %v", ok, err)
	}
	if heads != 1 {
		t.Fatalf("heads = %d", heads)
	}
	if firstURL != "https://example.test/files/abc?signature=one" {
		t.Fatalf("head url = %s", firstURL)
	}
}

func TestParallelDownloaderAppendsTheRemainingRangeToAPreservedChunk(t *testing.T) {
	dir := t.TempDir()
	chunkPath := filepath.Join(dir, "file.chunk0")
	if err := os.WriteFile(chunkPath, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotRange string
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) {
		return map[string]string{"Authorization": "Bearer token"}, nil
	}
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		gotRange = req.Header.Get("Range")
		header := make(http.Header)
		header.Set("Content-Range", "bytes 3-5/6")
		return textResponse(req, 206, header, "def"), nil
	})}
	var lastTransferred, lastIncremental int64
	if err := downloader.downloadChunk(context.Background(), downloadChunkArgs{
		URL: "https://n3.nahida.live/132/123412341234", Start: 0, End: 5, FileSize: 6,
		ResumeBytes: 3, ChunkPath: chunkPath,
		OnProgress: func(transferred, incremental int64) {
			lastTransferred, lastIncremental = transferred, incremental
		},
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(chunkPath)
	if string(raw) != "abcdef" {
		t.Fatalf("chunk = %q", raw)
	}
	if gotRange != "bytes=3-5" {
		t.Fatalf("range = %s", gotRange)
	}
	if lastTransferred != 6 || lastIncremental != 3 {
		t.Fatalf("progress = %d %d", lastTransferred, lastIncremental)
	}
}

func TestParallelDownloaderRejectsAMismatchedContentRangeWithoutChangingThePreservedChunk(t *testing.T) {
	dir := t.TempDir()
	chunkPath := filepath.Join(dir, "file.chunk0")
	if err := os.WriteFile(chunkPath, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) { return map[string]string{}, nil }
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Range", "bytes 0-5/6")
		return textResponse(req, 206, header, "abcdef"), nil
	})}
	err := downloader.downloadChunk(context.Background(), downloadChunkArgs{
		URL: "https://n3.nahida.live/132/123412341234", Start: 0, End: 5, FileSize: 6,
		ResumeBytes: 3, ChunkPath: chunkPath,
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected Content-Range") {
		t.Fatalf("error = %v", err)
	}
	raw, _ := os.ReadFile(chunkPath)
	if string(raw) != "abc" {
		t.Fatalf("chunk changed: %q", raw)
	}
}

type errorAfterRead struct {
	data []byte
	done bool
}

func (r *errorAfterRead) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(p, r.data), nil
	}
	return 0, errors.New("connection reset")
}

func (r *errorAfterRead) Close() error { return nil }

func TestParallelDownloaderPreservesBytesWrittenBeforeAConnectionFailure(t *testing.T) {
	dir := t.TempDir()
	chunkPath := filepath.Join(dir, "file.chunk0")
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) { return map[string]string{}, nil }
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Range", "bytes 0-5/6")
		return &http.Response{
			StatusCode: 206, Status: "206 Partial Content", Header: header,
			Body: &errorAfterRead{data: []byte("abc")}, Request: req,
		}, nil
	})}
	err := downloader.downloadChunk(context.Background(), downloadChunkArgs{
		URL: "https://n3.nahida.live/132/123412341234", Start: 0, End: 5, FileSize: 6,
		ResumeBytes: 0, ChunkPath: chunkPath,
		OnProgress: func(transferred, incremental int64) {},
	})
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("error = %v", err)
	}
	raw, _ := os.ReadFile(chunkPath)
	if string(raw) != "abc" {
		t.Fatalf("chunk = %q", raw)
	}
}

func TestParallelDownloaderStopsRetryingMismatchedContentRangesWhenThePartialChunkCannotBeDeleted(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(savePath+".chunk0", []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath+".chunk-meta.json", []byte(`{"resource":"https://n3.nahida.live/132/123412341234","fileSize":6}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) { return map[string]string{}, nil }
	downloader.Remove = func(string) error { return errors.New("busy") }
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		header := make(http.Header)
		header.Set("Content-Range", "bytes 0-4/6")
		return textResponse(req, 206, header, "abcdef"), nil
	})}
	err := downloader.Download(context.Background(), ParallelDownloadOptions{
		URL: "https://n3.nahida.live/132/123412341234", SavePath: savePath, FileSize: 6, MaxChunks: 1, Adaptive: false,
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected Content-Range") {
		t.Fatalf("error = %v", err)
	}
	if requests.Load() > 3 {
		t.Fatalf("requests = %d", requests.Load())
	}
}

func TestParallelDownloaderDiscardsLeftoverChunksThatDoNotMatchTheCurrentResource(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(savePath+".chunk0", []byte("stale!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath+".chunk-meta.json", []byte(`{"resource":"https://other.example/file","fileSize":6}`), 0o600); err != nil {
		t.Fatal(err)
	}
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) { return map[string]string{}, nil }
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Range", "bytes 0-5/6")
		return textResponse(req, 206, header, "abcdef"), nil
	})}
	if err := downloader.Download(context.Background(), ParallelDownloadOptions{
		URL: "https://n3.nahida.live/132/123412341234", SavePath: savePath, FileSize: 6, MaxChunks: 1, Adaptive: false,
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(savePath)
	if string(raw) != "abcdef" {
		t.Fatalf("file = %q", raw)
	}
}

func TestParallelDownloaderDoesNotResumeLeftoverChunksWhenMismatchedArtifactsCannotBeDeleted(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(savePath+".chunk0", []byte("stale!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath+".chunk-meta.json", []byte(`{"resource":"https://other.example/file","fileSize":6}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var requests int
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) { return map[string]string{}, nil }
	downloader.Remove = func(string) error { return errors.New("busy") }
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		requests++
		return textResponse(req, 206, nil, ""), nil
	})}
	err := downloader.Download(context.Background(), ParallelDownloadOptions{
		URL: "https://n3.nahida.live/132/123412341234", SavePath: savePath, FileSize: 6, MaxChunks: 1, Adaptive: false,
	})
	if err == nil || err.Error() != "failed to discard leftover download chunks" {
		t.Fatalf("error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestParallelDownloaderRejectsAShortResponseBodyEvenWhenContentRangeLooksComplete(t *testing.T) {
	dir := t.TempDir()
	chunkPath := filepath.Join(dir, "file.chunk0")
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) { return map[string]string{}, nil }
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Range", "bytes 0-5/6")
		return textResponse(req, 206, header, "abc"), nil
	})}
	err := downloader.downloadChunk(context.Background(), downloadChunkArgs{
		URL: "https://n3.nahida.live/132/123412341234", Start: 0, End: 5, FileSize: 6,
		ResumeBytes: 0, ChunkPath: chunkPath,
	})
	if err == nil || !strings.Contains(err.Error(), "ended early") {
		t.Fatalf("error = %v", err)
	}
	raw, _ := os.ReadFile(chunkPath)
	if string(raw) != "abc" {
		t.Fatalf("chunk = %q", raw)
	}
}

func TestParallelDownloaderResumesLeftoverChunksThatMatchTheCurrentResource(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "file.bin")
	if err := os.WriteFile(savePath+".chunk0", []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath+".chunk-meta.json", []byte(`{"resource":"https://n3.nahida.live/132/123412341234","fileSize":6}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotRange string
	downloader := NewParallelDownloader()
	downloader.GetHeaders = func(string) (map[string]string, error) { return map[string]string{}, nil }
	downloader.Client = &http.Client{Transport: parallelRoundTrip(func(req *http.Request) (*http.Response, error) {
		gotRange = req.Header.Get("Range")
		header := make(http.Header)
		header.Set("Content-Range", "bytes 3-5/6")
		return textResponse(req, 206, header, "def"), nil
	})}
	if err := downloader.Download(context.Background(), ParallelDownloadOptions{
		URL: "https://n3.nahida.live/132/123412341234", SavePath: savePath, FileSize: 6, MaxChunks: 1, Adaptive: false,
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(savePath)
	if string(raw) != "abcdef" {
		t.Fatalf("file = %q", raw)
	}
	if gotRange != "bytes=3-5" {
		t.Fatalf("range = %s", gotRange)
	}
}
