package ui

import (
	"testing"

	"nahida.live/desktop/internal/transfer"
)

func TestTransferActivity(t *testing.T) {
	t.Parallel()

	download := func(status transfer.Status, progress float64) transfer.Snapshot {
		return transfer.Snapshot{Type: "download", Status: status, Progress: progress, Name: "a"}
	}
	upload := func(status transfer.Status, phase transfer.UploadPhase) transfer.Snapshot {
		return transfer.Snapshot{Type: "upload", Status: status, UploadPhase: phase, Name: "b"}
	}
	failedDownload := download(transfer.StatusError, 0)
	failedDownload.Name = "  a very long transfer name  "
	failedUpload := upload(transfer.StatusError, "")
	speeding := transfer.Snapshot{
		Type: "download", Status: transfer.StatusProgress, Speed: 2048, TotalSize: 200, TransferredSize: 50,
	}
	partial := download(transfer.StatusCompleted, 100)
	partial.FailedFiles = 1

	tests := []struct {
		name   string
		items  []transfer.Snapshot
		label  string
		detail string
		status activityStatus
	}{
		{
			name: "failures of both kinds", items: []transfer.Snapshot{failedDownload, failedUpload},
			label:  "titlebar.activity.transfer.transfer_error",
			detail: T("page.transfer.item.failed_files", "count", 2), status: activityError,
		},
		{
			name: "one failed download", items: []transfer.Snapshot{failedDownload},
			label: "titlebar.activity.transfer.download_error", detail: "a very long tran…", status: activityError,
		},
		{
			name:  "all paused",
			items: []transfer.Snapshot{download(transfer.StatusPaused, 30), upload(transfer.StatusPaused, "")},
			label: "titlebar.activity.transfer.paused", status: activityPaused,
		},
		{
			name: "all finalizing", items: []transfer.Snapshot{download(transfer.StatusProgress, 100)},
			label: "titlebar.activity.transfer.finalizing", detail: "100%", status: activityRunning,
		},
		{
			name:   "upload preparing",
			items:  []transfer.Snapshot{upload(transfer.StatusProgress, transfer.UploadPreparing)},
			label:  "page.transfer.item.upload_activity.preparing",
			detail: "0%",
			status: activityRunning,
		},
		{
			name: "download speed and progress", items: []transfer.Snapshot{speeding},
			label:  "titlebar.activity.transfer.downloading",
			detail: formatSize(2048) + "/s · 25%", status: activityRunning,
		},
		{
			name: "mixed transfers with a failed file",
			items: []transfer.Snapshot{
				download(transfer.StatusPending, 0),
				upload(transfer.StatusPending, ""),
				partial,
			},
			label:  "titlebar.activity.transfer.transferring",
			detail: "0% · " + T("page.transfer.item.failed_files", "count", 1),
			status: activityError,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := transferActivity(tc.items)
			if got == nil {
				t.Fatal("transferActivity = nil")
			}
			if got.label != T(tc.label) || got.detail != tc.detail || got.status != tc.status {
				t.Fatalf("transferActivity = %q %q %v, want %q %q %v",
					got.label, got.detail, got.status, T(tc.label), tc.detail, tc.status)
			}
		})
	}

	if got := transferActivity([]transfer.Snapshot{download(transfer.StatusCompleted, 100)}); got != nil {
		t.Fatalf("finished transfers show %+v", *got)
	}
}
