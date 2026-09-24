package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestUploadBackupFilesSendsRemovalsAndReportsRefusals(t *testing.T) {
	var mu sync.Mutex
	var pages []struct {
		Files   []map[string]any    `json:"files"`
		Deleted []map[string]string `json:"deleted"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/backup/snapshots/snap/files:plan" || request.Method != http.MethodPost {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
			return
		}
		var page struct {
			Files   []map[string]any    `json:"files"`
			Deleted []map[string]string `json:"deleted"`
		}
		if err := json.NewDecoder(request.Body).Decode(&page); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		pages = append(pages, page)
		mu.Unlock()

		items := make([]UploadPlanItem, 0, len(page.Files))
		for _, file := range page.Files {
			id, _ := file["clientId"].(string)
			if file["path"] == "tool.exe" {
				items = append(items, UploadPlanItem{ClientID: id, Status: "denied", Reason: "denied_file_type"})
				continue
			}
			items = append(items, UploadPlanItem{ClientID: id, Status: "exists"})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "uploads": []any{}})
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})
	rules := testUploadRules()
	rules.MaxPlanFiles = 2
	drive.setUploadRules(rules)

	denied, err := drive.UploadBackupFiles(context.Background(), "snap", []BackupUploadFile{
		{ClientID: "0", TargetID: "t", RelPath: "a.ini", SHA256: "a"},
		{ClientID: "1", TargetID: "t", RelPath: "tool.exe", SHA256: "b"},
	}, []BackupDeletedFile{
		{TargetID: "t", RelPath: "x.ini"},
		{TargetID: "t", RelPath: "y.ini"},
		{TargetID: "t", RelPath: "z.ini"},
	}, nil)
	// A refused file is reported like any upload failure, which a run commits
	// through; local source failures and planning failures stop it.
	if errors.Is(err, ErrBackupPlanning) {
		t.Fatal(err)
	}
	if !slices.Equal(denied, []string{"1"}) {
		t.Fatalf("denied = %v", denied)
	}
	if len(pages) != 3 || len(pages[0].Files) != 2 || len(pages[0].Deleted) != 0 ||
		len(pages[1].Files) != 0 || len(pages[1].Deleted) != 2 || len(pages[2].Deleted) != 1 ||
		pages[2].Deleted[0]["path"] != "z.ini" {
		t.Fatalf("pages = %+v", pages)
	}
}

func TestUploadBackupFilesStopsAfterSourceReadFailure(t *testing.T) {
	var pages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		pages++
		var page struct {
			Files []struct {
				ClientID string `json:"clientId"`
			} `json:"files"`
		}
		if err := json.NewDecoder(request.Body).Decode(&page); err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":   []UploadPlanItem{{ClientID: page.Files[0].ClientID, Status: "pending", IntentID: "intent"}},
			"uploads": []UploadPlanEntry{{IntentID: "intent", URL: "https://example.invalid/upload"}},
		})
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})
	rules := testUploadRules()
	rules.MaxPlanFiles = 1
	drive.setUploadRules(rules)

	_, err := drive.UploadBackupFiles(t.Context(), "snap", []BackupUploadFile{
		{ClientID: "0", RelPath: "gone.ini", FullPath: filepath.Join(t.TempDir(), "gone.ini")},
		{ClientID: "1", RelPath: "later.ini"},
	}, nil, nil)
	if !errors.Is(err, ErrBackupSourceRead) || pages != 1 {
		t.Fatalf("source error = %v, planned pages = %d", err, pages)
	}
}

func TestBackupJSONReadsTheLatestSnapshotOfABaseMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":"base_mismatch","latestSnapshotId":"snap-2"}`)
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})

	err := drive.BackupJSON(context.Background(), http.MethodPost, "/backup/snapshots", map[string]any{}, nil)
	var apiErr *BackupAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != "base_mismatch" || apiErr.LatestSnapshotID != "snap-2" {
		t.Fatalf("err = %#v", err)
	}
}
