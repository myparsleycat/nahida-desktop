package drive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

const (
	parallelDownloadThreshold = 20 * 1024 * 1024
	downloadErrorRetries      = 2
	presignErrorRetries       = 1
)

func (d *Drive) downloadDriveFile(
	ctx context.Context,
	transfers *transfer.Transfer,
	file transfer.DownloadFile,
	destination string,
	link *DownloadLink,
	onProgress func(int64),
) error {
	if file.Size == 0 {
		if err := os.WriteFile(destination, nil, 0o644); err != nil {
			return err
		}
		return nil
	}

	partialPath := destination + ".ntmp"
	partialSize := localFileSize(partialPath)
	if file.CompAlg == nil && file.Size >= parallelDownloadThreshold && partialSize < parallelDownloadThreshold {
		header := driveDownloadHeader(link, true)
		supported, err := d.parallelDownload.CheckRangeSupportWithHeader(ctx, file.URL, header)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if supported {
			if partialSize > 0 {
				if err := os.Remove(partialPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					supported = false
				} else if onProgress != nil {
					onProgress(-partialSize)
				}
			}
			if supported {
				var parallelProgress int64
				err = d.parallelDownload.Download(ctx, infra.ParallelDownloadOptions{
					URL: file.URL, SavePath: destination, FileSize: file.Size, MaxChunks: 8, Adaptive: true,
					Header: header, BandwidthLimiter: transfers, SlowChunkMonitor: transfers.SlowChunks(),
					FileID: file.ID, CohortKey: "drive-parallel",
					OnProgress: func(bytes int64) {
						parallelProgress += bytes
						if onProgress != nil {
							onProgress(bytes)
						}
					},
				})
				if err == nil {
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if parallelProgress != 0 && onProgress != nil {
					onProgress(-parallelProgress)
				}
				if d.log != nil {
					d.log.Warn(fmt.Sprintf("Parallel download failed for %s; falling back to regular download: %v", file.Name, err), "Drive:Download:parallel-fallback")
				}
			}
		}
	}

	return d.downloadDriveFileWithSlowRetry(ctx, transfers, file, destination, link, onProgress)
}

func (d *Drive) downloadDriveFileWithSlowRetry(
	ctx context.Context,
	transfers *transfer.Transfer,
	file transfer.DownloadFile,
	destination string,
	link *DownloadLink,
	onProgress func(int64),
) error {
	currentURL := file.URL
	origin := file.URLOrigin
	if origin == "" {
		origin = "cdn"
	}
	partialPath := destination + ".ntmp"
	reported := localFileSize(partialPath)
	slowReconnects := 0
	errorRetries := 0
	refreshedExpiredPresign := false
	zeroRetries := 0

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx, attemptCancel := context.WithCancel(ctx)
		registered := transfers.SlowChunks().Register(transfer.SlowChunkRegistration{
			FileID: file.ID, ChunkIndex: 0, ChunkSize: file.Size, CohortKey: "drive",
			InitialTransferredBytes: reported, AttemptContext: attemptCtx,
			AttemptCancel: attemptCancel, SlowReconnects: slowReconnects,
		})
		monitorKey := registered.Key
		request := infra.DownloadRequest{
			URL: currentURL, Destination: destination, Size: file.Size,
			Compression: stringValue(file.CompAlg), Header: driveDownloadHeader(link, origin == "cdn"),
			Resume: file.CompAlg == nil, Retries: &zeroRetries,
			OnWait: func() {
				transfers.SlowChunks().SetPhase(monitorKey, transfer.SlowChunkPhaseBandwidthWait)
			},
			OnResume: func() {
				transfers.SlowChunks().SetPhase(monitorKey, transfer.SlowChunkPhaseNetwork)
			},
			Progress: func(bytes int64) {
				reported += bytes
				transfers.SlowChunks().RecordSample(monitorKey, reported)
				if onProgress != nil {
					onProgress(bytes)
				}
			},
		}
		err := d.download.File(attemptCtx, request)
		attemptCancel()
		snapshot, monitored := transfers.SlowChunks().Get(monitorKey)
		transfers.SlowChunks().Unregister(monitorKey)
		if err == nil {
			return nil
		}

		if file.CompAlg != nil {
			_ = os.Remove(partialPath)
			if reported != 0 && onProgress != nil {
				onProgress(-reported)
			}
			reported = 0
		} else {
			persisted := localFileSize(partialPath)
			if delta := persisted - reported; delta != 0 {
				if onProgress != nil {
					onProgress(delta)
				}
				reported = persisted
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if monitored && snapshot.AbortedSlowChunk {
			if origin == "cdn" && slowReconnects > 0 {
				freshURL, freshErr := d.fetchPresignedDownloadURL(ctx, file.ID, link)
				if freshErr == nil {
					currentURL = freshURL
					origin = "presign"
					slowReconnects = 0
					errorRetries = 0
					if waitErr := d.sleep(ctx, transfer.SlowReconnectDelay()); waitErr != nil {
						return waitErr
					}
					continue
				}
				err = errors.Join(err, freshErr)
			}
			if slowReconnects < transfer.SlowChunkMaxReconnects {
				slowReconnects++
				if waitErr := d.sleep(ctx, transfer.SlowReconnectDelay()); waitErr != nil {
					return waitErr
				}
				continue
			}
		}

		var httpErr *infra.DownloadHTTPError
		if errors.As(err, &httpErr) && httpErr.Status == http.StatusForbidden && origin == "presign" && !refreshedExpiredPresign {
			freshURL, freshErr := d.fetchPresignedDownloadURL(ctx, file.ID, link)
			if freshErr == nil {
				currentURL = freshURL
				refreshedExpiredPresign = true
				continue
			}
			err = errors.Join(err, freshErr)
		}

		maxRetries := downloadErrorRetries
		if origin == "presign" {
			maxRetries = presignErrorRetries
		}
		if !errors.Is(err, context.Canceled) && errorRetries < maxRetries {
			errorRetries++
			if waitErr := d.sleep(ctx, time.Duration(1<<errorRetries)*100*time.Millisecond); waitErr != nil {
				return waitErr
			}
			continue
		}
		if origin == "cdn" {
			freshURL, freshErr := d.fetchPresignedDownloadURL(ctx, file.ID, link)
			if freshErr == nil {
				currentURL = freshURL
				origin = "presign"
				errorRetries = 0
				if waitErr := d.sleep(ctx, transfer.SlowReconnectDelay()); waitErr != nil {
					return waitErr
				}
				continue
			}
			err = errors.Join(err, freshErr)
		}
		return err
	}
}

func driveDownloadHeader(link *DownloadLink, attach bool) http.Header {
	header := make(http.Header)
	if attach && link != nil && link.Token != "" {
		header.Set("nhd-link-token", link.Token)
	}
	return header
}

func localFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0
	}
	return info.Size()
}
