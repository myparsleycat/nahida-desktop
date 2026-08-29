package mod

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

var customDownloadSlots = make(chan struct{}, 16)

type abortError struct {
	message string
}

func (e abortError) Error() string { return e.message }
func (e abortError) Name() string  { return "AbortError" }

func newAbortError() error {
	return abortError{message: "The operation was aborted."}
}

type rangeDownloader interface {
	CheckRangeSupport(ctx context.Context, rawURL string) (bool, error)
	Download(ctx context.Context, request parallelDownloadRequest) error
}

type parallelDownloadRequest struct {
	URL              string
	SavePath         string
	FileSize         int64
	Header           http.Header
	OnProgress       func(int64)
	BandwidthLimiter infra.DownloadLimiter
	SlowChunkMonitor *transfer.SlowChunkMonitor
	FileID           string
	CohortKey        string
}

type customDownloadFileOptions struct {
	URL              string
	SavePath         string
	FileSize         *int64
	SupportsRange    *bool
	Downloader       rangeDownloader
	HTTP             *infra.Client
	Header           http.Header
	OnProgress       func(bytes int64)
	BandwidthLimiter infra.DownloadLimiter
	SlowChunkMonitor *transfer.SlowChunkMonitor
	FileID           string
	CohortKey        string
}

func downloadCustomFile(ctx context.Context, opts customDownloadFileOptions) error {
	rangeSupported := false
	probed := false
	if opts.SupportsRange != nil {
		rangeSupported = *opts.SupportsRange
	} else if opts.Downloader != nil {
		value, err := opts.Downloader.CheckRangeSupport(ctx, opts.URL)
		if err != nil {
			return err
		}
		rangeSupported = value
		probed = true
	}
	_ = probed

	fileSize := int64(0)
	if opts.FileSize != nil {
		fileSize = *opts.FileSize
	}
	if rangeSupported && fileSize > 0 {
		if opts.Downloader == nil {
			return errors.New("range downloader is not configured")
		}
		return opts.Downloader.Download(ctx, parallelDownloadRequest{
			URL: opts.URL, SavePath: opts.SavePath, FileSize: fileSize, Header: opts.Header,
			OnProgress: opts.OnProgress, BandwidthLimiter: opts.BandwidthLimiter,
			SlowChunkMonitor: opts.SlowChunkMonitor, FileID: opts.FileID, CohortKey: opts.CohortKey,
		})
	}
	return downloadCustomFileRegular(ctx, opts, fileSize)
}

func downloadCustomFileRegular(ctx context.Context, opts customDownloadFileOptions, fileSize int64) error {
	if ctx.Err() != nil {
		return newAbortError()
	}
	if opts.HTTP == nil {
		return errors.New("http service is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(opts.SavePath), 0o755); err != nil {
		return err
	}
	chunkSize := fileSize
	if chunkSize <= 0 {
		chunkSize = int64(^uint64(0) >> 1)
	}
	fileID := opts.FileID
	if fileID == "" {
		fileID = opts.SavePath
	}
	cohort := opts.CohortKey
	if cohort == "" {
		cohort = "custom"
	}
	slowReconnects := 0
	errorRetries := 0

	for {
		if ctx.Err() != nil {
			return newAbortError()
		}
		attemptCtx, attemptCancel := context.WithCancel(ctx)
		monitorKey := ""
		if opts.SlowChunkMonitor != nil {
			allowAbsolute := false
			registered := opts.SlowChunkMonitor.Register(transfer.SlowChunkRegistration{
				FileID: fileID, ChunkIndex: 0, ChunkSize: chunkSize, CohortKey: cohort,
				AttemptContext: attemptCtx, AttemptCancel: attemptCancel,
				SlowReconnects: slowReconnects, AllowAbsoluteAbort: &allowAbsolute,
			})
			monitorKey = registered.Key
		}
		attemptBytes, err := downloadCustomFileAttempt(attemptCtx, opts, monitorKey)
		attemptCancel()
		var snapshot transfer.SlowChunkSnapshot
		var monitored bool
		if monitorKey != "" {
			snapshot, monitored = opts.SlowChunkMonitor.Get(monitorKey)
			opts.SlowChunkMonitor.Unregister(monitorKey)
		}
		if err == nil {
			return nil
		}
		_ = os.Remove(opts.SavePath)
		if attemptBytes > 0 && opts.OnProgress != nil {
			opts.OnProgress(-attemptBytes)
		}
		if ctx.Err() != nil {
			return newAbortError()
		}
		if monitored && snapshot.AbortedSlowChunk && slowReconnects < transfer.SlowChunkMaxReconnects {
			slowReconnects++
			if waitErr := transfer.SleepWithAbort(ctx, transfer.SlowReconnectDelay()); waitErr != nil {
				return newAbortError()
			}
			continue
		}
		if !errors.Is(err, context.Canceled) && errorRetries < 2 {
			errorRetries++
			if waitErr := transfer.SleepWithAbort(ctx, time.Duration(1<<errorRetries)*time.Second); waitErr != nil {
				return newAbortError()
			}
			continue
		}
		return err
	}
}

func downloadCustomFileAttempt(ctx context.Context, opts customDownloadFileOptions, monitorKey string) (int64, error) {
	select {
	case customDownloadSlots <- struct{}{}:
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	defer func() { <-customDownloadSlots }()
	noRetries := 0
	response, err := opts.HTTP.Fetch(ctx, opts.URL, infra.FetchOptions{
		Method: http.MethodGet, Header: opts.Header, DisableHTTPErrors: true, RetryLimit: &noRetries,
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return 0, fmt.Errorf("failed to download file: %s", response.Status)
	}
	output, err := os.Create(opts.SavePath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = output.Close() }()
	var attemptBytes int64
	buf := make([]byte, 64*1024)
	for {
		n, readErr := response.Body.Read(buf)
		if n > 0 {
			if opts.BandwidthLimiter != nil {
				if limitErr := opts.BandwidthLimiter.Take(ctx, int64(n), func() {
					if monitorKey != "" {
						opts.SlowChunkMonitor.SetPhase(monitorKey, transfer.SlowChunkPhaseBandwidthWait)
					}
				}); limitErr != nil {
					return attemptBytes, limitErr
				}
			}
			if monitorKey != "" {
				opts.SlowChunkMonitor.SetPhase(monitorKey, transfer.SlowChunkPhaseDiskWrite)
			}
			if _, writeErr := output.Write(buf[:n]); writeErr != nil {
				return attemptBytes, writeErr
			}
			attemptBytes += int64(n)
			if monitorKey != "" {
				opts.SlowChunkMonitor.RecordSample(monitorKey, attemptBytes)
				opts.SlowChunkMonitor.SetPhase(monitorKey, transfer.SlowChunkPhaseNetwork)
			}
			if opts.OnProgress != nil {
				opts.OnProgress(int64(n))
			}
		}
		if readErr == io.EOF {
			return attemptBytes, nil
		}
		if readErr != nil {
			return attemptBytes, readErr
		}
	}
}
