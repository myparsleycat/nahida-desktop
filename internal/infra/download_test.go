package infra

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func testDownload(client *http.Client) *Download {
	retryLimit := 0
	download := NewDownload()
	download.UseClient(NewClientWithOptions(ClientOptions{
		HTTPClient: client,
		RetryLimit: &retryLimit,
	}))
	return download
}

func TestDownloadFileResumesValidatedRange(t *testing.T) {
	content := []byte("hello world")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=6-" {
			t.Errorf("Range = %q", got)
		}
		w.Header().Set("Content-Range", "bytes 6-10/11")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[6:])
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(destination+".ntmp", content[:6], 0o644); err != nil {
		t.Fatal(err)
	}
	download := testDownload(server.Client())
	if err := download.File(context.Background(), DownloadRequest{
		URL:         server.URL,
		Destination: destination,
		Size:        int64(len(content)),
		Resume:      true,
	}); err != nil {
		t.Fatalf("File() error = %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded = %q", got)
	}
}

func TestDownloadFileRestartsWhenServerIgnoresRange(t *testing.T) {
	content := []byte("complete payload")
	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Header.Get("Range"))
		mu.Unlock()
		_, _ = w.Write(content)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "file.bin")
	partial := []byte("stale")
	if err := os.WriteFile(destination+".ntmp", partial, 0o644); err != nil {
		t.Fatal(err)
	}
	progress := int64(len(partial))
	if err := testDownload(server.Client()).File(context.Background(), DownloadRequest{
		URL:         server.URL,
		Destination: destination,
		Size:        int64(len(content)),
		Resume:      true,
		Progress:    func(bytes int64) { progress += bytes },
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded = %q", got)
	}
	if progress != int64(len(content)) {
		t.Fatalf("progress = %d, want %d", progress, len(content))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0] != "bytes=5-" || requests[1] != "" {
		t.Fatalf("request ranges = %v", requests)
	}
}

func TestDownloadFileRestartsWhenResumedContentRangeDoesNotMatch(t *testing.T) {
	content := []byte("complete")
	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Header.Get("Range"))
		mu.Unlock()
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-7/8")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(content)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(destination+".ntmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testDownload(server.Client()).File(context.Background(), DownloadRequest{
		URL:         server.URL,
		Destination: destination,
		Size:        int64(len(content)),
		Resume:      true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded = %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0] != "bytes=5-" || requests[1] != "" {
		t.Fatalf("request ranges = %v", requests)
	}
}

func TestDownloadFileRetriesWithoutRangeAfter416(t *testing.T) {
	content := []byte("complete")
	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Header.Get("Range"))
		mu.Unlock()
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(destination+".ntmp", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testDownload(server.Client()).File(context.Background(), DownloadRequest{
		URL:         server.URL,
		Destination: destination,
		Size:        int64(len(content)),
		Resume:      true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded = %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0] == "" || requests[1] != "" {
		t.Fatalf("request ranges = %v", requests)
	}
}

func TestDownloadFileTreats416AsCompleteWhenTempCoversFullSize(t *testing.T) {
	content := []byte("complete")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Range") != "bytes=8-" {
			t.Errorf("Range = %q", r.Header.Get("Range"))
		}
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "file.bin")
	if err := os.WriteFile(destination+".ntmp", content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testDownload(server.Client()).File(context.Background(), DownloadRequest{
		URL:         server.URL,
		Destination: destination,
		Size:        int64(len(content)),
		Resume:      true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded = %q", got)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestDownloadFileDecompressesGzipAndZstd(t *testing.T) {
	content := bytes.Repeat([]byte("nahida"), 128)
	var gzipBody bytes.Buffer
	gzipWriter := gzip.NewWriter(&gzipBody)
	if _, err := gzipWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	zstdEncoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	zstdBody := zstdEncoder.EncodeAll(content, nil)
	if err := zstdEncoder.Close(); err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		compression string
		body        []byte
	}{
		"gzip": {compression: "gzip", body: gzipBody.Bytes()},
		"zstd": {compression: "zstd", body: zstdBody},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(test.body)
			}))
			defer server.Close()
			destination := filepath.Join(t.TempDir(), "file.bin")
			if err := testDownload(server.Client()).File(context.Background(), DownloadRequest{
				URL:         server.URL,
				Destination: destination,
				Compression: test.compression,
			}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(destination)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, content) {
				t.Fatalf("decompressed content differs: got %d bytes", len(got))
			}
		})
	}
}

func TestDownloadFileReturnsHTTPErrorWithoutRetryingPermanentFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()
	err := testDownload(server.Client()).File(context.Background(), DownloadRequest{
		URL:         server.URL,
		Destination: filepath.Join(t.TempDir(), "file.bin"),
	})
	var httpErr *DownloadHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusNotFound {
		t.Fatalf("File() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestDownloadFileDoesNotUseSharedClientRetries(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	retryWait := time.Duration(0)
	download := NewDownload()
	download.UseClient(NewClientWithOptions(ClientOptions{
		HTTPClient: server.Client(),
		RetryWait:  &retryWait,
	}))
	noOuterRetries := 0
	err := download.File(context.Background(), DownloadRequest{
		URL:         server.URL,
		Destination: filepath.Join(t.TempDir(), "file.bin"),
		Retries:     &noOuterRetries,
	})
	var httpErr *DownloadHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("File() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestDownloadFileCancellationKeepsPartialForResume(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "partial")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	destination := filepath.Join(t.TempDir(), "file.bin")
	done := make(chan error, 1)
	progressed := make(chan struct{}, 1)
	go func() {
		done <- testDownload(server.Client()).File(ctx, DownloadRequest{
			URL:         server.URL,
			Destination: destination,
			Resume:      true,
			Progress: func(int64) {
				select {
				case progressed <- struct{}{}:
				default:
				}
			},
		})
	}()
	<-progressed
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("File() error = %v", err)
	}
	info, err := os.Stat(destination + ".ntmp")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("partial download is empty")
	}
}
