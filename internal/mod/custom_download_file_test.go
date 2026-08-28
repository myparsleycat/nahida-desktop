package mod

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
)

type fakeRangeDownloader struct {
	checkCalls    int
	downloadCalls []parallelDownloadRequest
	downloadErr   error
}

func (f *fakeRangeDownloader) CheckRangeSupport(context.Context, string) (bool, error) {
	f.checkCalls++
	return false, nil
}

func (f *fakeRangeDownloader) Download(_ context.Context, request parallelDownloadRequest) error {
	f.downloadCalls = append(f.downloadCalls, request)
	return f.downloadErr
}

func TestDownloadCustomFileReusesTheCallersRangeProbeResult(t *testing.T) {
	downloader := &fakeRangeDownloader{}
	supports := true
	size := int64(1024)
	err := downloadCustomFile(context.Background(), customDownloadFileOptions{
		URL:           "https://example.test/file.bin",
		SavePath:      "file.bin",
		FileSize:      &size,
		SupportsRange: &supports,
		Downloader:    downloader,
	})
	if err != nil {
		t.Fatal(err)
	}
	if downloader.checkCalls != 0 {
		t.Fatalf("CheckRangeSupport calls = %d", downloader.checkCalls)
	}
	if len(downloader.downloadCalls) != 1 || downloader.downloadCalls[0].URL != "https://example.test/file.bin" || downloader.downloadCalls[0].FileSize != 1024 {
		t.Fatalf("download calls = %#v", downloader.downloadCalls)
	}
}

func TestDownloadCustomFileSkipsTheRangeProbeWhenRangeSupportIsExplicitlyFalse(t *testing.T) {
	downloader := &fakeRangeDownloader{}
	supports := false
	size := int64(1024)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := downloadCustomFile(ctx, customDownloadFileOptions{
		URL:           "https://example.test/file.bin",
		SavePath:      "file.bin",
		FileSize:      &size,
		SupportsRange: &supports,
		Downloader:    downloader,
	})
	var aborted abortError
	if !errors.As(err, &aborted) || aborted.Name() != "AbortError" {
		t.Fatalf("error = %v", err)
	}
	if downloader.checkCalls != 0 || len(downloader.downloadCalls) != 0 {
		t.Fatalf("downloader was used: checks=%d downloads=%d", downloader.checkCalls, len(downloader.downloadCalls))
	}
}

func TestDownloadCustomFileRegularReportsSuccessfulProgress(t *testing.T) {
	content := []byte("custom download")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()
	supports := false
	destination := filepath.Join(t.TempDir(), "custom.bin")
	client := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client()})
	var progress int64
	err := downloadCustomFile(context.Background(), customDownloadFileOptions{
		URL: server.URL, SavePath: destination, SupportsRange: &supports, HTTP: client,
		OnProgress: func(bytes int64) { progress += bytes },
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress != int64(len(content)) {
		t.Fatalf("progress = %d, want %d", progress, len(content))
	}
	if downloaded, readErr := os.ReadFile(destination); readErr != nil || string(downloaded) != string(content) {
		t.Fatalf("downloaded = %q, error = %v", downloaded, readErr)
	}
}

func TestDownloadCustomFileAttemptDoesNotUseSharedClientRetries(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	retryWait := time.Duration(0)
	client := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: server.Client(),
		RetryWait:  &retryWait,
	})
	_, err := downloadCustomFileAttempt(context.Background(), customDownloadFileOptions{
		URL: server.URL, SavePath: filepath.Join(t.TempDir(), "custom.bin"), HTTP: client,
	}, "")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("download error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
