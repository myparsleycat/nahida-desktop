package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
	if err == nil || errors.Is(err, ErrBackupPlanning) || !strings.Contains(err.Error(), "denied_file_type") {
		t.Fatalf("UploadBackupFiles error = %v, want denied file failure", err)
	}
	if !slices.Equal(denied, []string{"1"}) {
		t.Fatalf("denied = %v", denied)
	}
	if len(pages) != 3 || len(pages[0].Files) != 2 || len(pages[0].Deleted) != 0 ||
		len(pages[1].Files) != 0 || len(pages[2].Files) != 0 {
		t.Fatalf("pages = %+v", pages)
	}
	wantDeleted := [][]map[string]string{
		{{"targetId": "t", "path": "x.ini"}, {"targetId": "t", "path": "y.ini"}},
		{{"targetId": "t", "path": "z.ini"}},
	}
	if !reflect.DeepEqual([][]map[string]string{pages[1].Deleted, pages[2].Deleted}, wantDeleted) {
		t.Fatalf("deleted pages = %+v, want %+v", pages, wantDeleted)
	}
}

func TestUploadBackupFilesReturnsNoErrorForSuccessfulPlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var page struct {
			Files []struct {
				ClientID string `json:"clientId"`
			} `json:"files"`
		}
		if err := json.NewDecoder(request.Body).Decode(&page); err != nil || len(page.Files) != 1 {
			t.Errorf("plan request = %+v, %v", page, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":   []UploadPlanItem{{ClientID: page.Files[0].ClientID, Status: "exists"}},
			"uploads": []any{},
		})
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})
	drive.setUploadRules(testUploadRules())

	denied, err := drive.UploadBackupFiles(t.Context(), "snap", []BackupUploadFile{
		{ClientID: "0", TargetID: "t", RelPath: "a.ini", SHA256: "a"},
	}, nil, nil)
	if err != nil || len(denied) != 0 {
		t.Fatalf("successful plan = denied %v, error %v", denied, err)
	}
}

func TestUploadBackupFilesKeepsAnNteSetOnOnePageAndCompletesItsBundle(t *testing.T) {
	var mu sync.Mutex
	var pages [][]string
	var capabilities [][]string
	completed := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/bundles/nte/complete" {
			mu.Lock()
			completed++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed"})
			return
		}
		var page struct {
			Capabilities []string `json:"capabilities"`
			Files        []struct {
				ClientID string `json:"clientId"`
				Path     string `json:"path"`
			} `json:"files"`
		}
		if err := json.NewDecoder(request.Body).Decode(&page); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		paths := make([]string, 0, len(page.Files))
		for _, file := range page.Files {
			paths = append(paths, file.Path)
		}
		pages = append(pages, paths)
		capabilities = append(capabilities, page.Capabilities)
		mu.Unlock()

		items := make([]UploadPlanItem, 0, len(page.Files))
		var members []string
		for _, file := range page.Files {
			if strings.HasPrefix(file.Path, "Mods/X/") {
				members = append(members, file.ClientID)
				items = append(
					items,
					UploadPlanItem{
						ClientID: file.ClientID,
						Status:   "pending",
						IntentID: "i" + file.ClientID,
						BundleID: "nte",
					},
				)
				continue
			}
			items = append(items, UploadPlanItem{ClientID: file.ClientID, Status: "exists"})
		}
		bundles := []map[string]any{}
		if len(members) > 0 {
			bundles = append(bundles, map[string]any{
				"id":              "nte",
				"memberClientIds": members,
				"completeUrl":     server.URL + "/bundles/nte/complete",
				"abortUrl":        server.URL + "/bundles/nte/abort",
				"form":            map[string]string{"token": "token"},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "uploads": []any{}, "nteBundles": bundles})
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

	denied, err := drive.UploadBackupFiles(t.Context(), "snap", []BackupUploadFile{
		{ClientID: "0", TargetID: "t", RelPath: "a.ini", SHA256: "a"},
		{ClientID: "1", TargetID: "t", RelPath: "Mods/X/x.utoc", SHA256: "b"},
		{ClientID: "2", TargetID: "t", RelPath: "Mods/X/x.ucas", SHA256: "c"},
	}, nil, nil)
	if err != nil || len(denied) != 0 {
		t.Fatalf("UploadBackupFiles = denied %v, error %v", denied, err)
	}
	want := [][]string{{"a.ini"}, {"Mods/X/x.utoc", "Mods/X/x.ucas"}}
	if !reflect.DeepEqual(pages, want) {
		t.Fatalf("pages = %v, want %v", pages, want)
	}
	for _, announced := range capabilities {
		if !slices.Equal(announced, []string{"nte-bundle-v1"}) {
			t.Fatalf("capabilities = %v", capabilities)
		}
	}
	if completed != 1 {
		t.Fatalf("bundle completed %d times, want once", completed)
	}
}

func TestUploadBackupFilesStopsAfterRemovalPageFailure(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var page struct {
			Deleted []map[string]string `json:"deleted"`
		}
		if err := json.NewDecoder(request.Body).Decode(&page); err != nil {
			t.Error(err)
			return
		}
		if len(page.Deleted) != 1 {
			t.Errorf("deleted page = %+v", page.Deleted)
			return
		}
		requested = append(requested, page.Deleted[0]["path"])
		if len(requested) == 2 {
			http.Error(w, "planning failed", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "uploads": []any{}})
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

	_, err := drive.UploadBackupFiles(t.Context(), "snap", nil, []BackupDeletedFile{
		{TargetID: "t", RelPath: "x.ini"},
		{TargetID: "t", RelPath: "y.ini"},
		{TargetID: "t", RelPath: "z.ini"},
	}, nil)
	if !errors.Is(err, ErrBackupPlanning) {
		t.Fatalf("error = %v, want backup planning failure", err)
	}
	if !slices.Equal(requested, []string{"x.ini", "y.ini"}) {
		t.Fatalf("requested removals = %v, want first two pages only", requested)
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
