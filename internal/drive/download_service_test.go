package drive

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

type zeroDownloadReader struct{}

func (zeroDownloadReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func TestStartDownloadUsesParallelRangesAndSharedLinkToken(t *testing.T) {
	const size = int64(21 * 1024 * 1024)
	var ranges atomic.Int32
	var missingToken atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("nhd-link-token") != "shared-token" {
			missingToken.Store(true)
		}
		if request.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(request.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ranges.Add(1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.CopyN(w, zeroDownloadReader{}, end-start+1)
	}))
	defer server.Close()

	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	metadata := DownloadMetadata{
		Root: transfer.Root{ID: "file", Name: "parallel.bin"}, TotalBytes: size,
		Files: []transfer.DownloadFile{{ID: "file", FileID: "file", Name: "parallel.bin", Size: size, URL: server.URL + "/parallel.bin"}},
		Dirs:  []transfer.Directory{},
	}
	target := t.TempDir()
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{ID: "file", Name: "parallel.bin"}}, TargetPath: target,
		Data: &metadata, Link: &DownloadLink{LinkID: "link", Token: "shared-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, ok := transfers.Get(result.PID)
	if !ok || record.Status != transfer.StatusCompleted {
		t.Fatalf("transfer = %#v, exists=%v", record, ok)
	}
	if ranges.Load() < 2 {
		t.Fatalf("range requests = %d, want multiple segments", ranges.Load())
	}
	if missingToken.Load() {
		t.Fatal("shared link token was omitted from a download request")
	}
	if info, statErr := os.Stat(filepath.Join(target, "parallel.bin")); statErr != nil || info.Size() != size {
		t.Fatalf("downloaded file = %#v, error = %v", info, statErr)
	}
}

func TestCanceledDownloadDoesNotCreateQueuedEmptyDirectories(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	drive.settings = fixedDownloadSettings{concurrency: 1}
	target := t.TempDir()
	rootID := "root"
	metadata := DownloadMetadata{
		Root:       transfer.Root{ID: rootID, Name: "package"},
		TotalBytes: 1,
		Files: []transfer.DownloadFile{{
			ID: "file", Name: "file.bin", ParentID: &rootID, Size: 1, URL: server.URL,
		}},
		Dirs: []transfer.Directory{
			{ID: rootID, Name: "package"},
			{ID: "empty", ParentID: &rootID, Name: "empty"},
		},
	}
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items:      []DownloadItem{{ID: rootID, Name: "package", IsDir: true}},
		TargetPath: target,
		Data:       &metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- transfers.ProcessQueue(context.Background()) }()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	record, ok := transfers.Get(result.PID)
	if !ok || record.Status != transfer.StatusProgress {
		t.Fatalf("active transfer = %+v, ok=%v", record, ok)
	}
	wantDestination := filepath.Join(target, "package")
	if len(record.DestinationPaths) != 1 || record.DestinationPaths[0] != wantDestination {
		t.Fatalf("active destination paths = %#v, want %q", record.DestinationPaths, wantDestination)
	}
	if len(record.DestinationTargets) != 1 ||
		record.DestinationTargets[0].Path != wantDestination ||
		record.DestinationTargets[0].Kind != transfer.DestinationDirectory {
		t.Fatalf("active destination targets = %#v", record.DestinationTargets)
	}
	if err := transfers.Cancel(result.PID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("download queue did not stop after cancellation")
	}
	rootPath := filepath.Join(target, "package")
	if _, err := os.Stat(rootPath); err != nil {
		t.Fatalf("active file parent was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, "empty")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("queued empty directory exists after cancellation: %v", err)
	}
}

func TestDownloadDriveFileKeepsSuccessfulProgress(t *testing.T) {
	content := []byte("successful progress")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()
	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	destination := filepath.Join(t.TempDir(), "progress.bin")
	var progress int64
	err := drive.downloadDriveFile(context.Background(), transfers, transfer.DownloadFile{
		ID: "progress", Name: "progress.bin", Size: int64(len(content)), URL: server.URL,
	}, destination, nil, func(bytes int64) { progress += bytes })
	if err != nil {
		t.Fatal(err)
	}
	if progress != int64(len(content)) {
		t.Fatalf("progress = %d, want %d", progress, len(content))
	}
}

func TestDownloadDriveFileRollsBackCompressedAttemptProgressBeforeRetry(t *testing.T) {
	original := []byte("compressed retry content")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(original); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	valid := compressed.Bytes()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte("not-gzip"))
			return
		}
		_, _ = w.Write(valid)
	}))
	defer server.Close()
	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	drive.sleep = func(context.Context, time.Duration) error { return nil }
	destination := filepath.Join(t.TempDir(), "compressed.bin")
	algorithm := "gzip"
	var progress int64
	err := drive.downloadDriveFile(context.Background(), transfers, transfer.DownloadFile{
		ID: "compressed", Name: "compressed.bin", Size: int64(len(valid)), URL: server.URL, CompAlg: &algorithm,
	}, destination, nil, func(bytes int64) { progress += bytes })
	if err != nil {
		t.Fatal(err)
	}
	if progress != int64(len(valid)) {
		t.Fatalf("progress = %d, want %d", progress, len(valid))
	}
	if downloaded, readErr := os.ReadFile(destination); readErr != nil || !bytes.Equal(downloaded, original) {
		t.Fatalf("downloaded = %q, error = %v", downloaded, readErr)
	}
}

func TestStartDownloadRunsProvidedMetadataThroughTransferQueue(t *testing.T) {
	content := []byte("downloaded content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()
	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	var completedEvent map[string]any
	drive.eventEmit = func(name string, data ...any) {
		if name == "download:completed" && len(data) == 1 {
			completedEvent, _ = data[0].(map[string]any)
		}
	}
	metadata := DownloadMetadata{
		Root:       transfer.Root{ID: "file", Name: "original.bin"},
		TotalBytes: int64(len(content)),
		Files: []transfer.DownloadFile{{
			ID: "file", FileID: "file", Name: "original.bin", Size: int64(len(content)), URL: server.URL,
		}},
		Dirs: []transfer.Directory{},
	}
	target := t.TempDir()
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items:         []DownloadItem{{ID: "file", Name: "original.bin"}},
		TargetPath:    target,
		Data:          &metadata,
		SuggestedName: "renamed.bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "renamed.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("downloaded = %q", got)
	}
	record, ok := transfers.Get(result.PID)
	if !ok || record.Status != transfer.StatusCompleted || record.TransferredSize != int64(len(content)) || record.TransferredFiles != 1 {
		t.Fatalf("record = %+v, ok = %v", record, ok)
	}
	if completedEvent["path"] != target || completedEvent["name"] != "renamed.bin" {
		t.Fatalf("completion event = %#v", completedEvent)
	}
}

func TestDownloadWriteFailureLogsStageAndTransferContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	var output bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	transfers := transfer.NewWithOptions(transfer.Options{Log: log})
	drive := downloadServiceTestDrive(server, transfers)
	drive.UseLog(log)
	target := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, "blocked.bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := DownloadMetadata{
		Root:  transfer.Root{ID: "file", Name: "blocked.bin"},
		Files: []transfer.DownloadFile{{ID: "file", FileID: "file", Name: "blocked.bin"}},
		Dirs:  []transfer.Directory{},
	}
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{ID: "file", Name: "blocked.bin"}}, TargetPath: target, Data: &metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, ok := transfers.Get(result.PID)
	if !ok || record.Status != transfer.StatusError {
		t.Fatalf("record = %+v, ok = %v", record, ok)
	}
	got := output.String()
	for _, want := range []string{`"operation":"download"`, `"stage":"write"`, result.PID, `"destinationId":"file"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q: %s", want, got)
		}
	}
	if strings.Count(got, `"operation":"download"`) != 1 {
		t.Fatalf("failure logged more than once: %s", got)
	}
}

func TestTransferCancellationDoesNotCreateDriveFailureLog(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	transfers := transfer.NewWithOptions(transfer.Options{Log: log})
	_, err := transfers.Create(transfer.CreateParams{PID: "cancel", Type: "upload", Name: "cancel", InitialStatus: transfer.StatusProgress})
	if err != nil {
		t.Fatal(err)
	}
	drive := NewWithOptions(Options{Log: log})
	if got := drive.failUploadTransfer(transfers, "cancel", "execute", context.Canceled); !errors.Is(got, context.Canceled) {
		t.Fatalf("upload cancellation = %v", got)
	}
	if got := drive.failDownloadTransfer(transfers, "cancel", "download", context.Canceled); !errors.Is(got, context.Canceled) {
		t.Fatalf("download cancellation = %v", got)
	}
	if output.Len() != 0 {
		t.Fatalf("cancellation logged: %q", output.String())
	}
}

func TestFolderDownloadCompletionUsesCreatedDirectoryForInspection(t *testing.T) {
	content := []byte("downloaded content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	var inspectionPaths []string
	drive.UseFixInspection(func(paths []string) {
		inspectionPaths = slices.Clone(paths)
	})
	rootID := "root"
	metadata := DownloadMetadata{
		Root:       transfer.Root{ID: rootID, Name: "package"},
		TotalBytes: int64(len(content)),
		Files: []transfer.DownloadFile{{
			ID: "file", FileID: "file", ParentID: &rootID, Name: "mod.ini", Size: int64(len(content)), URL: server.URL,
		}},
		Dirs: []transfer.Directory{{ID: rootID, Name: "package"}},
	}
	target := t.TempDir()
	if _, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items:      []DownloadItem{{ID: rootID, Name: "package", IsDir: true}},
		TargetPath: target,
		Data:       &metadata,
	}); err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(target, "package")
	if len(inspectionPaths) != 1 || inspectionPaths[0] != want {
		t.Fatalf("queued folder inspection paths = %#v, want %q", inspectionPaths, want)
	}
}

func TestQueuedDownloadReservesDestinationBeforeRunnerStarts(t *testing.T) {
	content := []byte("queued content")
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(requestStarted)
			<-releaseRequest
		}
		_, _ = w.Write(content)
	}))
	defer server.Close()

	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	metadata := DownloadMetadata{
		Root:       transfer.Root{ID: "file", Name: "mod.bin"},
		TotalBytes: int64(len(content)),
		Files: []transfer.DownloadFile{{
			ID: "file", FileID: "file", Name: "mod.bin", Size: int64(len(content)), URL: server.URL,
		}},
	}
	target := t.TempDir()
	params := StartDownloadParams{
		Items: []DownloadItem{{ID: "file", Name: "mod.bin"}}, TargetPath: target, Data: &metadata,
	}
	first, err := drive.StartDownload(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- transfers.ProcessQueue(context.Background()) }()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("first download did not block")
	}
	second, err := drive.StartDownload(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(target, "mod.bin")
	queued, ok := transfers.Get(second.PID)
	if !ok || queued.Status != transfer.StatusPending {
		t.Fatalf("queued transfer = %+v, ok=%v", queued, ok)
	}
	if len(queued.DestinationTargets) != 1 || queued.DestinationTargets[0].Path != want ||
		queued.DestinationTargets[0].Kind != transfer.DestinationFile {
		t.Fatalf("queued destination targets = %#v, want %q", queued.DestinationTargets, want)
	}
	if !transfers.IsActiveDownloadDestination(want) {
		t.Fatalf("queued destination %q is not reserved", want)
	}
	close(releaseRequest)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("download queue did not finish")
	}
	if record, ok := transfers.Get(first.PID); !ok || record.Status != transfer.StatusCompleted {
		t.Fatalf("first transfer = %+v, ok=%v", record, ok)
	}
}

func TestDownloadFallsBackToFreshPresignedURLAfterForbidden(t *testing.T) {
	content := []byte("fresh")
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/stale":
			w.WriteHeader(http.StatusForbidden)
		case "/akasha/file/download":
			if request.URL.Query().Get("uuid") != "file" || request.URL.Query().Get("presign") != "true" {
				t.Fatalf("query = %v", request.URL.Query())
			}
			_, _ = io.WriteString(w, `{"url":"`+server.URL+`/fresh"}`)
		case "/fresh":
			_, _ = w.Write(content)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	metadata := DownloadMetadata{
		Root:       transfer.Root{ID: "file", Name: "file.bin"},
		TotalBytes: int64(len(content)),
		Files:      []transfer.DownloadFile{{ID: "file", Name: "file.bin", Size: int64(len(content)), URL: server.URL + "/stale"}},
		Dirs:       []transfer.Directory{},
	}
	target := t.TempDir()
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items:      []DownloadItem{{ID: "file", Name: "file.bin"}},
		TargetPath: target,
		Data:       &metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, _ := transfers.Get(result.PID)
	if record.Status != transfer.StatusCompleted {
		t.Fatalf("record = %+v", record)
	}
	got, err := os.ReadFile(filepath.Join(target, "file.bin"))
	if err != nil || string(got) != string(content) {
		t.Fatalf("downloaded = %q, error = %v", got, err)
	}
}

func TestDownloadSkipsAnExistingFileWithoutComparingItsSize(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	transfers := transfer.New()
	drive := downloadServiceTestDrive(server, transfers)
	target := t.TempDir()
	existing := filepath.Join(target, "file.bin")
	if err := os.WriteFile(existing, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	metadata := DownloadMetadata{
		Root:       transfer.Root{ID: "file", Name: "file.bin"},
		TotalBytes: 8,
		Files:      []transfer.DownloadFile{{ID: "file", Name: "file.bin", Size: 8, URL: server.URL}},
		Dirs:       []transfer.Directory{},
	}
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items:      []DownloadItem{{ID: "file", Name: "file.bin"}},
		TargetPath: target,
		Data:       &metadata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "already here" {
		t.Fatalf("existing file = %q, %v", got, err)
	}
	record, _ := transfers.Get(result.PID)
	if record.Status != transfer.StatusCompleted || record.TransferredFiles != 1 || record.TransferredSize != 8 {
		t.Fatalf("record = %+v", record)
	}
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestStartDownloadPromptsPathSelectorWhenTargetMissing(t *testing.T) {
	content := []byte("selected")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()
	transfers := transfer.New()
	target := t.TempDir()
	drive := downloadServiceTestDrive(server, transfers)
	drive.UsePathSelector(staticPathSelector{path: target, fileName: "picked.bin"})
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{ID: "file", Name: "original.bin"}},
		Data: &DownloadMetadata{
			Root:       transfer.Root{ID: "file", Name: "original.bin"},
			TotalBytes: int64(len(content)),
			Files:      []transfer.DownloadFile{{ID: "file", FileID: "file", Name: "original.bin", Size: int64(len(content)), URL: server.URL}},
			Dirs:       []transfer.Directory{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "picked.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("downloaded = %q", got)
	}
	if result.Status != "started" {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestStartDownloadCanceledWhenPathSelectorReturnsNil(t *testing.T) {
	transfers := transfer.New()
	drive := NewWithOptions(Options{
		FS:           platform.NewFS(),
		Transfer:     transfers,
		Download:     infra.NewDownload(),
		PathSelector: cancelPathSelector{},
	})
	result, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{ID: "file", Name: "file.bin"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "canceled" {
		t.Fatalf("status = %q", result.Status)
	}
	if got := transfers.List(); len(got) != 0 {
		t.Fatalf("transfers = %#v", got)
	}
}

func TestStartDownloadRequiresPathWhenSelectorMissing(t *testing.T) {
	drive := NewWithOptions(Options{
		FS:       platform.NewFS(),
		Transfer: transfer.New(),
		Download: infra.NewDownload(),
	})
	_, err := drive.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{ID: "file", Name: "file.bin"}},
	})
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != "DRIVE_FN_STARTDOWNLOAD_FAILED" || api.Message != "download target path is required" {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveDownloadPathsRejectsDuplicateDirectoryID(t *testing.T) {
	rootID := "root"
	metadata := DownloadMetadata{
		Root: transfer.Root{ID: rootID, Name: "Root"},
		Dirs: []transfer.Directory{
			{ID: "duplicate", ParentID: &rootID, Name: "one"},
			{ID: "duplicate", ParentID: &rootID, Name: "two"},
		},
	}
	if _, _, err := resolveDownloadPaths(metadata, t.TempDir()); err == nil {
		t.Fatal("expected duplicate directory error")
	}
}

func TestResolveDownloadDestinationTargets(t *testing.T) {
	target := filepath.Join(`C:\Mods`, "Character")
	rootID := "root"
	batchID := "batch-root"

	tests := []struct {
		name     string
		metadata DownloadMetadata
		want     []transfer.DestinationTarget
	}{
		{
			name: "single file",
			metadata: DownloadMetadata{
				Root:  transfer.Root{ID: "file", Name: "mod.zip"},
				Files: []transfer.DownloadFile{{ID: "file", Name: "mod.zip"}},
			},
			want: []transfer.DestinationTarget{{
				Path: filepath.Join(target, "mod.zip"),
				Kind: transfer.DestinationFile,
			}},
		},
		{
			name: "single renamed folder",
			metadata: DownloadMetadata{
				Root: transfer.Root{ID: rootID, Name: "Mod (2)"},
				Dirs: []transfer.Directory{{ID: rootID, Name: "Mod (2)"}},
			},
			want: []transfer.DestinationTarget{{
				Path: filepath.Join(target, "Mod (2)"),
				Kind: transfer.DestinationDirectory,
			}},
		},
		{
			name: "batch folders and file",
			metadata: DownloadMetadata{
				Root: transfer.Root{ID: batchID},
				Dirs: []transfer.Directory{
					{ID: "one", ParentID: &batchID, Name: "One"},
					{ID: "nested", ParentID: &rootID, Name: "Nested"},
					{ID: rootID, ParentID: &batchID, Name: "Two"},
				},
				Files: []transfer.DownloadFile{{ID: "file", ParentID: &batchID, Name: "readme.txt"}},
			},
			want: []transfer.DestinationTarget{
				{Path: filepath.Join(target, "One"), Kind: transfer.DestinationDirectory},
				{Path: filepath.Join(target, "Two"), Kind: transfer.DestinationDirectory},
				{Path: filepath.Join(target, "readme.txt"), Kind: transfer.DestinationFile},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveDownloadDestinationTargets(test.metadata, target)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("destination targets = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestPrepareDownloadMetadataResolvesExistingDirectoryConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		choice     platform.DirectoryConflictChoice
		wantName   string
		wantCancel bool
	}{
		{name: "overwrite preserves existing casing", choice: platform.DirectoryConflictOverwrite, wantName: "Folder"},
		{name: "new name uses unique suffix", choice: platform.DirectoryConflictRename, wantName: "folder (2)"},
		{name: "cancel cancels transfer", choice: platform.DirectoryConflictCancel, wantCancel: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target := t.TempDir()
			if err := os.Mkdir(filepath.Join(target, "Folder"), 0o755); err != nil {
				t.Fatal(err)
			}
			dialog := platform.NewDialog()
			var promptedName string
			dialog.UseDirectoryConflictResolver(func(opts platform.DirectoryConflictOptions) (platform.DirectoryConflictChoice, error) {
				promptedName = opts.Name
				return test.choice, nil
			})
			transfers := transfer.New()
			const pid = "directory-conflict"
			if _, err := transfers.Create(transfer.CreateParams{
				PID: pid, Type: "download", Name: "Folder", InitialStatus: transfer.StatusPreparing,
				Data: transfer.Data{}, ManualStart: true,
			}); err != nil {
				t.Fatal(err)
			}
			drive := NewWithOptions(Options{FS: platform.NewFS(), Dialog: dialog, Transfer: transfers})
			metadata := DownloadMetadata{
				Root: transfer.Root{ID: "root", Name: "folder"},
				Dirs: []transfer.Directory{{ID: "root", Name: "folder"}},
			}
			prepared, err := drive.prepareDownloadMetadata(context.Background(), transfers, pid, metadata, StartDownloadParams{
				Items: []DownloadItem{{ID: "root", Name: "folder", IsDir: true}}, TargetPath: target,
			})
			if promptedName != "Folder" {
				t.Fatalf("prompted name = %q", promptedName)
			}
			if test.wantCancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("err = %v", err)
				}
				record, ok := transfers.Get(pid)
				if !ok || record.Status != transfer.StatusCanceled {
					t.Fatalf("transfer = %+v, ok=%v", record, ok)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Root.Name != test.wantName || len(prepared.Dirs) != 1 || prepared.Dirs[0].Name != test.wantName {
				t.Fatalf("prepared = %+v", prepared)
			}
		})
	}
}

func TestPrepareDownloadMetadataUsesUniqueNameWhenConflictIsFile(t *testing.T) {
	t.Parallel()

	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "Folder"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	dialog := platform.NewDialog()
	dialog.UseDirectoryConflictResolver(func(platform.DirectoryConflictOptions) (platform.DirectoryConflictChoice, error) {
		t.Fatal("file conflict should not prompt")
		return "", nil
	})
	drive := NewWithOptions(Options{FS: platform.NewFS(), Dialog: dialog})
	metadata := DownloadMetadata{
		Root: transfer.Root{ID: "root", Name: "folder"},
		Dirs: []transfer.Directory{{ID: "root", Name: "folder"}},
	}
	prepared, err := drive.prepareDownloadMetadata(context.Background(), nil, "", metadata, StartDownloadParams{
		Items: []DownloadItem{{ID: "root", Name: "folder", IsDir: true}}, TargetPath: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Root.Name != "folder (2)" || prepared.Dirs[0].Name != "folder (2)" {
		t.Fatalf("prepared = %+v", prepared)
	}
}

func downloadServiceTestDrive(server *httptest.Server, transfers *transfer.Transfer) *Drive {
	client := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(client)
	return NewWithOptions(Options{
		HTTP:     client,
		FS:       platform.NewFS(),
		Transfer: transfers,
		Download: download,
	})
}

type staticPathSelector struct {
	path     string
	fileName string
}

func (s staticPathSelector) SelectDownloadPath(context.Context, string, string, []string, bool) (*string, *string, error) {
	path := s.path
	var fileName *string
	if s.fileName != "" {
		fileName = &s.fileName
	}
	return &path, fileName, nil
}

type cancelPathSelector struct{}

func (cancelPathSelector) SelectDownloadPath(context.Context, string, string, []string, bool) (*string, *string, error) {
	return nil, nil, nil
}

type fixedDownloadSettings struct {
	concurrency int
}

func (s fixedDownloadSettings) GetUploadConcurrency(context.Context) (int, error) {
	return 1, nil
}

func (s fixedDownloadSettings) GetDownloadConcurrency(context.Context) (int, error) {
	return s.concurrency, nil
}
