package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		_, _ = io.WriteString(w, `{"data":[{"id":"dir-1","path":"root/sub"}]}`)
	}))
	defer server.Close()
	drive := uploadServiceTestDrive(server, transfer.New())
	created, err := drive.CreateDirs(context.Background(), "dest", []UploadDirectory{{Path: "root/sub", Name: "sub", ParentPath: "root"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].ID != "dir-1" || created[0].Path != "root/sub" {
		t.Fatalf("created = %+v", created)
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
			_, _ = fmt.Fprintf(w, "event: complete\ndata: {\"items\":[{\"clientId\":\"%s\",\"status\":\"pending\",\"intentId\":\"intent\"}],\"uploads\":[{\"intentId\":\"intent\",\"url\":%q,\"method\":\"POST\",\"form\":{\"token\":\"token\",\"sha256\":\"hash\"}}]}\n\n", fileID, server.URL+"/v2/uploads/intent")
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
	if !ok || record.Status != transfer.StatusCompleted || record.TransferredSize != int64(len("upload")) || record.TransferredFiles != 1 {
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
