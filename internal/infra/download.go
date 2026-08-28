package infra

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	downloadBufferSize = 64 * 1024
	downloadRetries    = 2
)

var contentRangePattern = regexp.MustCompile(`(?i)^bytes\s+(\d+)-(\d+)/(\d+)$`)

type DownloadLimiter interface {
	Take(context.Context, int64, func()) error
}

type DownloadRequest struct {
	URL         string
	Destination string
	Size        int64
	Compression string
	Header      http.Header
	Progress    func(bytes int64)
	OnWait      func()
	OnResume    func()
	// OnResponse receives the total response length before body copying starts.
	// A value <= 0 means the server did not provide a usable Content-Length.
	OnResponse func(contentLength int64)
	Resume     bool
	Retries    *int
}

type DownloadHTTPError struct {
	Status     int
	StatusText string
}

func (e *DownloadHTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusText == "" {
		return fmt.Sprintf("download failed: %d", e.Status)
	}
	return fmt.Sprintf("download failed: %d %s", e.Status, e.StatusText)
}

type Download struct {
	client  *Client
	limiter DownloadLimiter
}

type limitedProgressReader struct {
	ctx      context.Context
	reader   io.Reader
	limiter  DownloadLimiter
	onWait   func()
	onResume func()
	progress func(int64)
}

func (r *limitedProgressReader) Read(buffer []byte) (int, error) {
	read, readErr := r.reader.Read(buffer)
	if read <= 0 {
		return read, readErr
	}
	if r.limiter != nil {
		if err := r.limiter.Take(r.ctx, int64(read), r.onWait); err != nil {
			return 0, err
		}
		if r.onResume != nil {
			r.onResume()
		}
	}
	if r.progress != nil {
		r.progress(int64(read))
	}
	return read, readErr
}

func NewDownload() *Download {
	return &Download{client: NewClient()}
}

//wails:ignore
func (d *Download) UseClient(client *Client) {
	if client != nil {
		d.client = client
	}
}

//wails:ignore
func (d *Download) UseLimiter(limiter DownloadLimiter) {
	d.limiter = limiter
}

func (d *Download) File(ctx context.Context, request DownloadRequest) error {
	if request.URL == "" {
		return errors.New("download URL is required")
	}
	if request.Destination == "" {
		return errors.New("download destination is required")
	}
	if request.Compression != "" && request.Compression != "gzip" && request.Compression != "zstd" {
		return fmt.Errorf("unsupported download compression %q", request.Compression)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(request.Destination), 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}

	temporaryPath := request.Destination + ".ntmp"
	retries := downloadRetries
	if request.Retries != nil {
		retries = max(0, *request.Retries)
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = d.downloadAttempt(ctx, request, temporaryPath)
		if lastErr == nil {
			if err := replaceFile(temporaryPath, request.Destination); err != nil {
				return fmt.Errorf("finalize download: %w", err)
			}
			return nil
		}
		var httpErr *DownloadHTTPError
		if errors.As(lastErr, &httpErr) && httpErr.Status >= 400 && httpErr.Status < 500 && httpErr.Status != http.StatusRequestTimeout && httpErr.Status != http.StatusTooManyRequests {
			break
		}
		if attempt < retries {
			delay := time.Duration(1<<attempt) * time.Second
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return lastErr
}

func (d *Download) downloadAttempt(ctx context.Context, request DownloadRequest, temporaryPath string) error {
	resumeFrom := int64(0)
	if request.Resume && request.Compression == "" {
		if info, err := os.Stat(temporaryPath); err == nil {
			resumeFrom = info.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect partial download: %w", err)
		}
		if request.Size > 0 && resumeFrom > request.Size {
			resetBytes := min(resumeFrom, request.Size)
			if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove oversized partial download: %w", err)
			}
			if request.Progress != nil && resetBytes > 0 {
				request.Progress(-resetBytes)
			}
			resumeFrom = 0
		}
	} else if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale partial download: %w", err)
	}

	response, err := d.fetch(ctx, request, resumeFrom)
	if err != nil {
		return err
	}
	appendFile := resumeFrom > 0
	if appendFile && response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		drainAndClose(response.Body)
		if request.Size > 0 && resumeFrom == request.Size {
			return nil
		}
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("reset rejected partial download: %w", err)
		}
		if request.Progress != nil {
			request.Progress(-resumeFrom)
		}
		response, err = d.fetch(ctx, request, 0)
		if err != nil {
			return err
		}
		appendFile = false
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		drainAndClose(response.Body)
		return &DownloadHTTPError{Status: response.StatusCode, StatusText: http.StatusText(response.StatusCode)}
	}
	if appendFile && (response.StatusCode != http.StatusPartialContent || !expectedContentRange(response.Header.Get("Content-Range"), resumeFrom, request.Size)) {
		drainAndClose(response.Body)
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("reset invalid partial download: %w", err)
		}
		if request.Progress != nil {
			request.Progress(-resumeFrom)
		}
		response, err = d.fetch(ctx, request, 0)
		if err != nil {
			return err
		}
		appendFile = false
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			drainAndClose(response.Body)
			return &DownloadHTTPError{Status: response.StatusCode, StatusText: http.StatusText(response.StatusCode)}
		}
	}
	defer func() { _ = response.Body.Close() }()
	if request.OnResponse != nil {
		request.OnResponse(downloadContentLength(response, request, appendFile))
	}

	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendFile {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	output, err := os.OpenFile(temporaryPath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("open partial download: %w", err)
	}
	copyErr := d.copyResponse(ctx, request, response.Body, output)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return fmt.Errorf("close partial download: %w", closeErr)
	}
	if request.Compression == "" && request.Size > 0 {
		info, statErr := os.Stat(temporaryPath)
		if statErr != nil {
			return fmt.Errorf("inspect completed download: %w", statErr)
		}
		if info.Size() != request.Size {
			return fmt.Errorf("download size mismatch: got %d, want %d", info.Size(), request.Size)
		}
	}
	return nil
}

func downloadContentLength(response *http.Response, request DownloadRequest, appendFile bool) int64 {
	if appendFile && request.Size > 0 {
		return request.Size
	}
	if response.ContentLength > 0 {
		return response.ContentLength
	}
	length, err := strconv.ParseInt(strings.TrimSpace(response.Header.Get("Content-Length")), 10, 64)
	if err != nil || length <= 0 {
		return -1
	}
	return length
}

func (d *Download) fetch(ctx context.Context, request DownloadRequest, resumeFrom int64) (*http.Response, error) {
	header := request.Header.Clone()
	if header == nil {
		header = make(http.Header)
	}
	if resumeFrom > 0 {
		header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}
	client := d.client
	if client == nil {
		client = NewClient()
	}
	noRetries := 0
	return client.Fetch(ctx, request.URL, FetchOptions{
		Method:            http.MethodGet,
		Header:            header,
		DisableHTTPErrors: true,
		RetryLimit:        &noRetries,
	})
}

func (d *Download) copyResponse(ctx context.Context, request DownloadRequest, body io.Reader, output io.Writer) error {
	networkReader := &limitedProgressReader{
		ctx:      ctx,
		reader:   body,
		limiter:  d.limiter,
		onWait:   request.OnWait,
		onResume: request.OnResume,
		progress: request.Progress,
	}
	reader := io.Reader(networkReader)
	closeDecoder := func() {}
	switch request.Compression {
	case "gzip":
		decoder, err := gzip.NewReader(networkReader)
		if err != nil {
			return fmt.Errorf("create gzip decoder: %w", err)
		}
		reader = decoder
		closeDecoder = func() { _ = decoder.Close() }
	case "zstd":
		decoder, err := zstd.NewReader(networkReader)
		if err != nil {
			return fmt.Errorf("create zstd decoder: %w", err)
		}
		reader = decoder
		closeDecoder = decoder.Close
	}
	defer closeDecoder()

	buffer := make([]byte, downloadBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := reader.Read(buffer)
		if read > 0 {
			written, writeErr := output.Write(buffer[:read])
			if writeErr != nil {
				return fmt.Errorf("write download: %w", writeErr)
			}
			if written != read {
				return io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read download: %w", readErr)
		}
	}
}

func expectedContentRange(value string, resumeFrom, fileSize int64) bool {
	match := contentRangePattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 4 {
		return false
	}
	start, err1 := strconv.ParseInt(match[1], 10, 64)
	end, err2 := strconv.ParseInt(match[2], 10, 64)
	total, err3 := strconv.ParseInt(match[3], 10, 64)
	return err1 == nil && err2 == nil && err3 == nil && start == resumeFrom && end == fileSize-1 && total == fileSize
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}
