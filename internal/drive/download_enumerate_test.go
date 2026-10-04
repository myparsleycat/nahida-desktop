package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/transfer"
)

// folderDownloadServer serves one folder walk and the files it lists. The walk
// pauses before the files named in held until release is closed, and a file
// named in blocked waits for its request to be cancelled or for unblock.
type folderDownloadServer struct {
	*httptest.Server
	walks     atomic.Int32
	fileGets  map[string]*atomic.Int32
	release   chan struct{}
	unblock   chan struct{}
	holdFrom  int
	blocked   string
	blockOnce atomic.Bool
}

func newFolderDownloadServer(t *testing.T, names []string, holdFrom int, blocked string) *folderDownloadServer {
	t.Helper()
	server := &folderDownloadServer{
		fileGets: make(map[string]*atomic.Int32, len(names)),
		release:  make(chan struct{}),
		unblock:  make(chan struct{}),
		holdFrom: holdFrom,
		blocked:  blocked,
	}
	for _, name := range names {
		server.fileGets[name] = &atomic.Int32{}
	}
	root := "root"
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		name, isFile := strings.CutPrefix(request.URL.Path, "/files/")
		if isFile {
			server.fileGets[name].Add(1)
			// Only the first request blocks, so a resumed download can finish.
			if name == server.blocked && server.blockOnce.CompareAndSwap(false, true) {
				select {
				case <-request.Context().Done():
					return
				case <-server.unblock:
				}
			}
			_, _ = w.Write([]byte(name))
			return
		}
		if request.URL.Path != "/akasha/dir/download" {
			http.NotFound(w, request)
			return
		}

		walk := server.walks.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(
			w,
			"event: metadata\ndata: {\"root\":{\"id\":\"root\",\"parentId\":null,\"name\":\"Pack\"},\"totalBytes\":",
			len(strings.Join(names, "")),
			"}\n\n",
		)
		for index, name := range names {
			// Only the first walk is held, so a restarted walk runs to the end.
			if index == server.holdFrom && walk == 1 {
				w.(http.Flusher).Flush()
				select {
				case <-request.Context().Done():
					return
				case <-server.release:
				}
			}
			files, _ := json.Marshal([]transfer.DownloadFile{{
				ID: name, ParentID: &root, Name: name, Size: int64(len(name)), URL: server.URL + "/files/" + name,
			}})
			event, _ := json.Marshal(downloadChunkEnvelope{Data: string(files)})
			_, _ = fmt.Fprintf(w, "event: files\ndata: %s\n\n", event)
		}
		_, _ = fmt.Fprint(w, "event: complete\ndata: {}\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

func waitForTransfer(
	t *testing.T,
	transfers *transfer.Transfer,
	pid, what string,
	done func(transfer.Record) bool,
) transfer.Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		record, ok := transfers.Get(pid)
		if ok && done(record) {
			return record
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: %+v", what, record)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runDownloadQueue(t *testing.T, transfers *transfer.Transfer) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- transfers.ProcessQueue(context.Background()) }()
	return done
}

func waitForQueue(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("download queue did not finish")
	}
}

func startFolderDownload(t *testing.T, drive *Drive, target string) string {
	t.Helper()
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{ID: "root", Name: "Pack", IsDir: true}}, TargetPath: target,
	})
	if err != nil || result.Status != "started" {
		t.Fatalf("download = %+v, %v", result, err)
	}
	return result.PID
}

func TestFolderDownloadReportsTotalsWhileTheWalkIsStillStreaming(t *testing.T) {
	names := []string{"a.bin", "b.bin"}
	server := newFolderDownloadServer(t, names, 1, "")
	transfers := transfer.New()
	drive := downloadServiceTestDrive(t, server.Server, transfers)
	target := t.TempDir()

	// The transfer exists, with its destination reserved, before any walk.
	pid := startFolderDownload(t, drive, target)
	queued, _ := transfers.Get(pid)
	want := []transfer.DestinationTarget{{Path: filepath.Join(target, "Pack"), Kind: transfer.DestinationDirectory}}
	if queued.Status != transfer.StatusPending || queued.TotalFiles != 0 ||
		!slices.Equal(queued.DestinationTargets, want) || server.walks.Load() != 0 {
		t.Fatalf("queued transfer = %+v, walks = %d", queued, server.walks.Load())
	}

	done := runDownloadQueue(t, transfers)
	walking := waitForTransfer(t, transfers, pid, "the first file chunk", func(record transfer.Record) bool {
		return record.TotalFiles == 1
	})
	if walking.Status != transfer.StatusPreparing || walking.TotalSize != int64(len("a.binb.bin")) {
		t.Fatalf("walking transfer = %+v", walking)
	}
	if downloadSpoolCount(t, drive) != 1 {
		t.Fatal("the walk is not writing a spool")
	}

	close(server.release)
	waitForQueue(t, done)
	record, _ := transfers.Get(pid)
	if record.Status != transfer.StatusCompleted || record.TotalFiles != 2 || record.TransferredFiles != 2 {
		t.Fatalf("completed transfer = %+v", record)
	}
	for _, name := range names {
		got, err := os.ReadFile(filepath.Join(target, "Pack", name))
		if err != nil || string(got) != name {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
	if downloadSpoolCount(t, drive) != 0 {
		t.Fatal("a completed download left its spool behind")
	}
}

func TestFolderDownloadWithoutANameTakesTheServerName(t *testing.T) {
	server := newFolderDownloadServer(t, []string{"a.bin"}, -1, "")
	transfers := transfer.New()
	drive := downloadServiceTestDrive(t, server.Server, transfers)
	target := t.TempDir()

	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{ID: "root", IsDir: true}}, TargetPath: target,
	})
	if err != nil || result.Status != "started" {
		t.Fatalf("download = %+v, %v", result, err)
	}
	if queued, _ := transfers.Get(result.PID); len(queued.DestinationTargets) != 0 {
		t.Fatalf("an unnamed folder reserved %+v before its name was known", queued.DestinationTargets)
	}

	waitForQueue(t, runDownloadQueue(t, transfers))
	record, _ := transfers.Get(result.PID)
	want := []transfer.DestinationTarget{{Path: filepath.Join(target, "Pack"), Kind: transfer.DestinationDirectory}}
	if record.Status != transfer.StatusCompleted || record.Name != "Pack" ||
		!slices.Equal(record.DestinationTargets, want) {
		t.Fatalf("completed transfer = %+v", record)
	}
	if got, err := os.ReadFile(filepath.Join(target, "Pack", "a.bin")); err != nil || string(got) != "a.bin" {
		t.Fatalf("a.bin = %q, %v", got, err)
	}
}

func TestFolderDownloadPausedDuringTheWalkStartsItOver(t *testing.T) {
	server := newFolderDownloadServer(t, []string{"a.bin", "b.bin"}, 1, "")
	transfers := transfer.New()
	drive := downloadServiceTestDrive(t, server.Server, transfers)
	pid := startFolderDownload(t, drive, t.TempDir())

	done := runDownloadQueue(t, transfers)
	waitForTransfer(t, transfers, pid, "the first file chunk", func(record transfer.Record) bool {
		return record.TotalFiles == 1
	})
	if err := transfers.Pause(pid); err != nil {
		t.Fatal(err)
	}
	waitForQueue(t, done)
	if record, _ := transfers.Get(pid); record.Status != transfer.StatusPaused {
		t.Fatalf("paused transfer = %+v", record)
	}
	if downloadSpoolCount(t, drive) != 0 {
		t.Fatal("an interrupted walk left a partial spool behind")
	}

	if err := transfers.Resume(pid); err != nil {
		t.Fatal(err)
	}
	waitForQueue(t, runDownloadQueue(t, transfers))
	record, _ := transfers.Get(pid)
	if record.Status != transfer.StatusCompleted || record.TotalFiles != 2 || server.walks.Load() != 2 {
		t.Fatalf("resumed transfer = %+v, walks = %d", record, server.walks.Load())
	}
}

func TestFolderDownloadPausedWhileDownloadingResumesFromItsSpool(t *testing.T) {
	server := newFolderDownloadServer(t, []string{"a.bin", "b.bin"}, -1, "b.bin")
	transfers := transfer.New()
	drive := downloadServiceTestDrive(t, server.Server, transfers)
	drive.settings = fixedDownloadSettings{concurrency: 1}
	target := t.TempDir()
	pid := startFolderDownload(t, drive, target)

	done := runDownloadQueue(t, transfers)
	waitForTransfer(t, transfers, pid, "the first file to finish", func(record transfer.Record) bool {
		return record.TransferredFiles == 1 && server.fileGets["b.bin"].Load() == 1
	})
	if err := transfers.Pause(pid); err != nil {
		t.Fatal(err)
	}
	waitForQueue(t, done)
	if !transfers.IsIndexCompleted(pid, 0) || transfers.IsIndexCompleted(pid, 1) {
		t.Fatal("completion is not tracked by spool position")
	}
	if downloadSpoolCount(t, drive) != 1 {
		t.Fatal("a paused download did not keep its spool")
	}

	if err := transfers.Resume(pid); err != nil {
		t.Fatal(err)
	}
	waitForQueue(t, runDownloadQueue(t, transfers))
	record, _ := transfers.Get(pid)
	if record.Status != transfer.StatusCompleted || record.TransferredFiles != 2 {
		t.Fatalf("resumed transfer = %+v", record)
	}
	if server.walks.Load() != 1 || server.fileGets["a.bin"].Load() != 1 {
		t.Fatalf("walks = %d, a.bin requests = %d", server.walks.Load(), server.fileGets["a.bin"].Load())
	}
	if got, err := os.ReadFile(filepath.Join(target, "Pack", "b.bin")); err != nil || string(got) != "b.bin" {
		t.Fatalf("b.bin = %q, %v", got, err)
	}
	if downloadSpoolCount(t, drive) != 0 {
		t.Fatal("a completed download left its spool behind")
	}
}
