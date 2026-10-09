package reshade

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/transfer"
)

func (r *ReShade) runTransfer(
	ctx context.Context, name, destination, url string, run func(context.Context, func(int64, int64)) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.transfer == nil {
		return errors.New("transfer service is not configured")
	}
	pid := uuid.NewString()
	_, err := r.transfer.Create(transfer.CreateParams{
		PID: pid, Type: "download", Name: name, Path: destination, InitialStatus: transfer.StatusPending,
		RestartData: true,
		Data:        transfer.Data{Files: []transfer.DownloadFile{{ID: pid, Name: name, URL: url}}},
	})
	if err != nil {
		return err
	}
	result := make(chan error, 1)
	err = r.transfer.RegisterRunner(pid, func(runCtx context.Context, queue *transfer.Transfer, pid string) error {
		// Each attempt owns its temporary files, so retries remain valid after the original caller returns.
		status := transfer.StatusProgress
		err := queue.ResetTransfer(pid)
		if err == nil {
			err = queue.UpdateRunning(pid, transfer.Updates{Status: &status})
		}
		if err == nil {
			err = run(runCtx, func(downloaded, total int64) {
				if runCtx.Err() == nil {
					_ = queue.UpdateRunning(pid, transfer.Updates{TransferredSize: &downloaded, TotalSize: &total})
				}
			})
		}
		if err == nil {
			err = runCtx.Err()
		}
		if err == nil {
			status = transfer.StatusCompleted
			progress, completed := float64(100), 1
			err = queue.UpdateRunning(pid, transfer.Updates{
				Status: &status, Progress: &progress, TransferredFiles: &completed,
			})
			r.emitStatus()
		}
		err = r.report(err, "Download", "transfer", map[string]any{"name": name, "path": destination})
		select {
		case result <- err:
		default:
		}
		return err
	})
	if err != nil {
		_ = r.transfer.Cancel(pid)
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			_ = r.transfer.Cancel(pid)
			return ctx.Err()
		case <-ticker.C:
			record, exists := r.transfer.Get(pid)
			if !exists || record.Status == transfer.StatusPaused || record.Status == transfer.StatusCanceled {
				return context.Canceled
			}
		}
	}
}
