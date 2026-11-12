package drive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

func TestCreateDirsDecodesCreatedDirectoryPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/create-dirs" {
			http.NotFound(w, request)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"dir-1","path":"sub"}]}`)
	}))
	defer server.Close()
	drive := uploadServiceTestDrive(server, transfer.New())
	created, err := drive.CreateDirs(
		context.Background(),
		"dest",
		[]UploadDirectory{{Path: "sub", Name: "sub", ParentPath: ""}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].ID != "dir-1" || created[0].Path != "sub" {
		t.Fatalf("created = %+v", created)
	}
}

func TestPlanDirectoryCreateBatchesSplitsDirectChildren(t *testing.T) {
	directories := make([]UploadDirectory, directoryCreateBatchSize+1)
	for index := range directories {
		name := fmt.Sprintf("child-%d", index)
		directories[index] = UploadDirectory{Path: name, Name: name, ParentPath: ""}
	}
	batches, err := planDirectoryCreateBatches(directories)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0].Dirs) != directoryCreateBatchSize || len(batches[1].Dirs) != 1 {
		t.Fatalf("batches = %d, %d", len(batches[0].Dirs), len(batches[1].Dirs))
	}
	if batches[0].ParentPath != "" || batches[1].ParentPath != "" {
		t.Fatalf("parents = %q, %q", batches[0].ParentPath, batches[1].ParentPath)
	}
}

func TestPlanDirectoryCreateBatchesOrdersParentsFirst(t *testing.T) {
	batches, err := planDirectoryCreateBatches([]UploadDirectory{
		{Path: "root/sub", Name: "sub", ParentPath: "root"},
		{Path: "root", Name: "root", ParentPath: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || batches[0].ParentPath != "" || batches[1].ParentPath != "root" {
		t.Fatalf("batches = %+v", batches)
	}
}

func TestPlanDirectoryCreateBatchesRejectsMissingParent(t *testing.T) {
	if _, err := planDirectoryCreateBatches([]UploadDirectory{
		{Path: "root/sub", Name: "sub", ParentPath: "root"},
	}); err == nil {
		t.Fatal("a child whose parent is not in the upload was accepted")
	}
}

func TestCreateDirsPostsParentBeforeChildrenAndChunksSiblings(t *testing.T) {
	var mu sync.Mutex
	type capturedDir struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	type captured struct {
		ParentID string        `json:"parentId"`
		Dirs     []capturedDir `json:"dirs"`
	}
	var requests []captured
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/create-dirs" {
			http.NotFound(w, request)
			return
		}
		var body captured
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, body)
		serial := len(requests)
		mu.Unlock()
		rows := make([]map[string]string, len(body.Dirs))
		for index, dir := range body.Dirs {
			rows[index] = map[string]string{
				"id":   fmt.Sprintf("id-%d-%d", serial, index),
				"path": dir.Path,
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": rows})
	}))
	defer server.Close()
	drive := uploadServiceTestDrive(server, transfer.New())
	siblings := make([]UploadDirectory, directoryCreateBatchSize+1)
	for index := range siblings {
		name := fmt.Sprintf("child-%d", index)
		siblings[index] = UploadDirectory{Path: "root/" + name, Name: name, ParentPath: "root"}
	}
	directories := append([]UploadDirectory{{Path: "root", Name: "root", ParentPath: ""}}, siblings...)
	created, err := drive.CreateDirs(context.Background(), "dest", directories)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %d", len(requests))
	}
	if requests[0].ParentID != "dest" || len(requests[0].Dirs) != 1 || requests[0].Dirs[0].Path != "root" {
		t.Fatalf("root request = %+v", requests[0])
	}
	rootID := "id-1-0"
	if requests[1].ParentID != rootID || len(requests[1].Dirs) != directoryCreateBatchSize {
		t.Fatalf("first child request = parent %s, %d dirs", requests[1].ParentID, len(requests[1].Dirs))
	}
	if requests[2].ParentID != rootID || len(requests[2].Dirs) != 1 ||
		requests[2].Dirs[0].Path != requests[2].Dirs[0].Name {
		t.Fatalf("second child request = %+v", requests[2])
	}
	for _, request := range requests {
		for _, dir := range request.Dirs {
			if dir.Path != dir.Name || strings.Contains(dir.Path, "/") {
				t.Fatalf("directory path = %+v", dir)
			}
		}
	}
	if len(created) != len(directories) || created[0].Path != "root" || created[0].ID != rootID {
		t.Fatalf("created = %+v", created[:1])
	}
	if created[len(created)-1].Path != siblings[len(siblings)-1].Path {
		t.Fatalf("last path = %s", created[len(created)-1].Path)
	}
}

func TestStartUploadRunsThroughTransferQueue(t *testing.T) {
	var completedEvent *uploadCompletedEvent
	completedEventCount := 0
	completedAtEvent := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/akasha/content/dest":
			_, _ = io.WriteString(w, `{"children":[]}`)
		case "/akasha/v2/upload-rules":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(testUploadRules()); err != nil {
				t.Fatal(err)
			}
		case "/akasha/v2/sse/drive/files:plan":
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			fileID := uploadPlanClientID(raw)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(
				w,
				"event: complete\ndata: {\"items\":[{\"clientId\":\"%s\",\"status\":\"pending\",\"intentId\":\"intent\"}],\"uploads\":[{\"intentId\":\"intent\",\"url\":%q,\"method\":\"POST\",\"form\":{\"token\":\"token\",\"sha256\":\"hash\"}}]}\n\n",
				fileID,
				server.URL+"/v2/uploads/intent",
			)
		case "/v2/uploads/intent":
			if err := request.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	transfers := transfer.New()
	drive := uploadServiceTestDrive(server, transfers)
	drive.eventEmit = func(name string, data ...any) {
		if name != "drive:upload-completed" || len(data) != 1 {
			return
		}
		event, ok := data[0].(uploadCompletedEvent)
		if ok {
			completedEventCount++
			completedEvent = &event
			record, found := transfers.Get(event.PID)
			completedAtEvent = found && record.Status == transfer.StatusCompleted
		}
	}
	path := filepath.Join(t.TempDir(), "mod.ini")
	if err := os.WriteFile(path, []byte("upload"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := drive.StartUpload(context.Background(), StartUploadParams{
		DestID:           "dest",
		Paths:            []string{path},
		ConflictStrategy: UploadConflictSuffix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, ok := transfers.Get(result.PID)
	if !ok || record.Status != transfer.StatusCompleted || record.TransferredSize != int64(len("upload")) ||
		record.TransferredFiles != 1 {
		t.Fatalf("record = %+v, ok = %v", record, ok)
	}
	if record.CurrentID != "dest" {
		t.Fatalf("record current ID = %q, want dest", record.CurrentID)
	}
	if completedEvent == nil || completedEvent.PID != result.PID || completedEvent.CurrentID != "dest" {
		t.Fatalf("completed event = %+v", completedEvent)
	}
	if completedEventCount != 1 || !completedAtEvent {
		t.Fatalf("completed event count = %d, completed at event = %v", completedEventCount, completedAtEvent)
	}
}

func TestUploadPlanValidationFailureIsLoggedWithStageAndContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/akasha/content/dest":
			_, _ = io.WriteString(w, `{"children":[]}`)
		case "/akasha/v2/upload-rules":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testUploadRules())
		case "/akasha/v2/sse/drive/files:plan":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(
				w,
				"event: progress\ndata: {\"phase\":\"file_validation\",\"processed\":1,\"total\":1}\n\n",
			)
			_, _ = io.WriteString(
				w,
				"event: error\ndata: {\"code\":\"upload_file_too_large\",\"message\":\"server rejected file\"}\n\n",
			)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	var logOutput bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &logOutput, DisableFile: true})
	transfers := transfer.NewWithOptions(transfer.Options{Log: log})
	drive := uploadServiceTestDrive(server, transfers)
	drive.UseLog(log)
	path := filepath.Join(t.TempDir(), "validation.ini")
	if err := os.WriteFile(path, []byte("upload"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := drive.StartUpload(context.Background(), StartUploadParams{
		DestID: "dest", Paths: []string{path}, ConflictStrategy: UploadConflictSuffix,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, ok := transfers.Get(result.PID)
	if !ok || record.Status != transfer.StatusError || record.ErrorCode != "upload_file_too_large" {
		t.Fatalf("record = %+v, ok = %v", record, ok)
	}
	got := logOutput.String()
	for _, want := range []string{" WARN ", `"operation":"upload"`, `"stage":"plan/file_validation"`, result.PID, `"destinationId":"dest"`, `"errorCode":"upload_file_too_large"`, "server rejected file"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log missing %q: %s", want, got)
		}
	}
	if strings.Count(got, "server rejected file") != 1 {
		t.Fatalf("failure logged more than once: %s", got)
	}
}

func TestStartUploadRejectsUnreadableSourceBeforeRequest(t *testing.T) {
	t.Parallel()
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()
	drive := uploadServiceTestDrive(server, transfer.New())
	_, err := drive.StartUpload(context.Background(), StartUploadParams{
		DestID: "dest",
		Paths:  []string{filepath.Join(t.TempDir(), "missing.ini")},
	})
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != "DRIVE_FN_STARTUPLOAD_FAILED" || api.Message != "Path is not readable" {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("unreadable upload source reached the Drive backend")
	}
}

func uploadServiceTestDrive(server *httptest.Server, transfers *transfer.Transfer) *Drive {
	return NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: server.Client(),
			BackendURL: server.URL,
			Status:     infra.BackendOnline,
		}),
		FS:       platform.NewFS(),
		Transfer: transfers,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	})
}

func TestUploadRetryRehashesChangedSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mod.ini")
	writeUploadFile(t, path, "before")
	prep, err := PrepareUpload([]string{path}, nil, UploadConflictSuffix, testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	transfers := transfer.New()
	t.Cleanup(func() { _ = transfers.ServiceShutdown() })
	if _, err := transfers.Create(transfer.CreateParams{
		PID: prep.PID, Type: "upload", InitialStatus: transfer.StatusPending,
	}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var hashes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/v2/sse/drive/files:plan" {
			http.NotFound(w, request)
			return
		}
		var body struct {
			Files []struct{ ClientID, SHA256 string }
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Files) != 1 {
			t.Errorf("plan = %+v, %v", body, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		hashes = append(hashes, body.Files[0].SHA256)
		w.Header().Set("Content-Type", "text/event-stream")
		if len(hashes) == 1 {
			_, _ = io.WriteString(w, "event: error\ndata: {\"code\":\"test_stop\"}\n\n")
			return
		}
		_, _ = fmt.Fprintf(w,
			"event: complete\ndata: {\"items\":[{\"clientId\":%q,\"status\":\"exists\"}],\"uploads\":[]}\n\n",
			body.Files[0].ClientID,
		)
	}))
	defer server.Close()
	drive := uploadServiceTestDrive(server, transfers)
	state := &uploadRunnerState{}
	restart := &uploadRestartData{Params: StartUploadParams{DestID: "dest"}, Preparation: prep, RequestID: "retry"}
	if err := drive.runUpload(t.Context(), transfers, prep.PID, restart, testUploadRules(), state); err == nil {
		t.Fatal("first plan unexpectedly succeeded")
	}
	writeUploadFile(t, path, "after!")
	modified := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	if err := drive.runUpload(t.Context(), transfers, prep.PID, restart, testUploadRules(), state); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hashes) != 2 || hashes[1] != fmt.Sprintf("%x", sha256.Sum256([]byte("after!"))) {
		t.Fatalf("retry hashes = %v; want current source hash", hashes)
	}
}

func TestUploadSourceSizeChangeStopsBeforePlan(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mod.ini")
	writeUploadFile(t, path, "before")
	prep, err := PrepareUpload([]string{path}, nil, UploadConflictSuffix, testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	writeUploadFile(t, path, "changed size")
	transfers := transfer.New()
	t.Cleanup(func() { _ = transfers.ServiceShutdown() })
	if _, err := transfers.Create(transfer.CreateParams{
		PID: prep.PID, Type: "upload", InitialStatus: transfer.StatusPending,
	}); err != nil {
		t.Fatal(err)
	}
	drive := NewWithOptions(Options{Transfer: transfers})
	err = drive.runUpload(t.Context(), transfers, prep.PID, &uploadRestartData{
		Params: StartUploadParams{DestID: "dest"}, Preparation: prep,
	}, testUploadRules(), &uploadRunnerState{})
	var changed *UploadV2Error
	if !errors.As(err, &changed) || changed.Code != "upload_source_changed" {
		t.Fatalf("size change reached an unconfigured plan client: %v", err)
	}
	record, _ := transfers.Get(prep.PID)
	if record.ErrorCode != "upload_source_changed" || record.Error != "mod.ini" || record.PreparationProgress != nil {
		t.Fatalf("changed source record = %+v", record)
	}
}

func uploadPlanClientID(raw []byte) string {
	var request struct {
		Files []struct {
			ClientID string `json:"clientId"`
		} `json:"files"`
	}
	_ = json.Unmarshal(raw, &request)
	if len(request.Files) == 0 {
		return ""
	}
	return request.Files[0].ClientID
}
