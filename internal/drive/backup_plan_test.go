package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	if !slices.Equal(
		denied,
		[]BackupRejection{{ClientID: "1", Reason: "denied_file_type", Denied: true, Permanent: true}},
	) {
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

func TestUploadBackupFilesReportsRefusedUploads(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) string {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte("content of "+name), 0o600); err != nil {
			t.Fatal(err)
		}
		return full
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v2/uploads/dup":
			w.WriteHeader(http.StatusUnsupportedMediaType)
			_, _ = io.WriteString(w, `{"code":"unsupported_file_type"}`)
			return
		case "/v2/uploads:pack":
			_, _ = io.WriteString(w, `{"results":[`+
				`{"intentId":"pack-ok","status":"completed"},`+
				`{"intentId":"pack-refused","status":"failed","reason":"unsupported_file_type"},`+
				`{"intentId":"pack-broken","status":"failed","reason":"storage_unavailable"}]}`)
			return
		}
		var page struct {
			Files []struct {
				ClientID string `json:"clientId"`
				Path     string `json:"path"`
			} `json:"files"`
		}
		if err := json.NewDecoder(request.Body).Decode(&page); err != nil {
			t.Error(err)
			return
		}
		items := make([]UploadPlanItem, 0, len(page.Files))
		uploads := []UploadPlanEntry{}
		for _, file := range page.Files {
			switch file.Path {
			case "tool.exe":
				items = append(
					items,
					UploadPlanItem{ClientID: file.ClientID, Status: "denied", Reason: "denied_file_type"},
				)
				continue
			case "big.ini":
				items = append(
					items,
					UploadPlanItem{ClientID: file.ClientID, Status: "denied", Reason: "quota_exceeded"},
				)
				continue
			}
			// The two dup files share their content, so they share one intent.
			intent := strings.TrimSuffix(file.Path, filepath.Ext(file.Path))
			if strings.HasPrefix(intent, "dup") {
				intent = "dup"
				if slices.ContainsFunc(
					uploads,
					func(upload UploadPlanEntry) bool { return upload.IntentID == intent },
				) {
					items = append(items, UploadPlanItem{ClientID: file.ClientID, Status: "pending", IntentID: intent})
					continue
				}
			}
			items = append(items, UploadPlanItem{ClientID: file.ClientID, Status: "pending", IntentID: intent})
			uploads = append(uploads, uploadPlanEntry(intent, server.URL+"/v2/uploads/"+intent, "token", file.Path))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "uploads": uploads})
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})
	// A pack holds three files, so the first three uploads travel as one pack
	// and the dup intent is sent directly.
	rules := testUploadRules()
	rules.Pack.MaxFiles = 3
	drive.setUploadRules(rules)
	files := []BackupUploadFile{
		{ClientID: "0", RelPath: "tool.exe", FullPath: write("tool.exe")},
		{ClientID: "1", RelPath: "pack-ok.ini", FullPath: write("pack-ok.ini")},
		{ClientID: "2", RelPath: "pack-refused.ini", FullPath: write("pack-refused.ini")},
		{ClientID: "3", RelPath: "pack-broken.ini", FullPath: write("pack-broken.ini")},
		{ClientID: "4", RelPath: "dup.xyz", FullPath: write("dup.xyz")},
		{ClientID: "5", RelPath: "dup-copy.ini", FullPath: write("dup-copy.ini")},
		{ClientID: "6", RelPath: "big.ini", FullPath: write("big.ini")},
	}
	for index := range files {
		info, err := os.Stat(files[index].FullPath)
		if err != nil {
			t.Fatal(err)
		}
		files[index].Size = info.Size()
		files[index].SHA256 = files[index].RelPath
	}
	files[5].SHA256 = files[4].SHA256

	rejections, err := drive.UploadBackupFiles(t.Context(), "snap", files, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "storage_unavailable") {
		t.Fatalf("UploadBackupFiles error = %v, want the transient pack failure", err)
	}
	// The copy of the refused content under another extension is not
	// refused, and a denial the server may lift is not permanent.
	want := []BackupRejection{
		{ClientID: "0", Reason: "denied_file_type", Denied: true, Permanent: true},
		{ClientID: "2", Reason: "unsupported_file_type", Permanent: true},
		{ClientID: "4", Reason: "unsupported_file_type", Permanent: true},
		{ClientID: "6", Reason: "quota_exceeded", Denied: true},
	}
	slices.SortFunc(rejections, func(a, b BackupRejection) int { return strings.Compare(a.ClientID, b.ClientID) })
	if !slices.Equal(rejections, want) {
		t.Fatalf("rejections = %+v, want %+v", rejections, want)
	}
}

func TestUploadBackupFilesKeepsRefusalsBesideALaterPlanFailure(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		pages++
		if pages == 2 {
			http.Error(w, "planning failed", http.StatusBadRequest)
			return
		}
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
			"items": []UploadPlanItem{
				{ClientID: page.Files[0].ClientID, Status: "denied", Reason: "denied_file_type"},
			},
			"uploads": []any{},
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

	rejections, err := drive.UploadBackupFiles(t.Context(), "snap", []BackupUploadFile{
		{ClientID: "0", RelPath: "tool.exe", SHA256: "a"},
		{ClientID: "1", RelPath: "later.ini", SHA256: "b"},
	}, nil, nil)
	if !errors.Is(err, ErrBackupPlanning) {
		t.Fatalf("error = %v, want backup planning failure", err)
	}
	if !slices.Equal(rejections, []BackupRejection{
		{ClientID: "0", Reason: "denied_file_type", Denied: true, Permanent: true},
	}) {
		t.Fatalf("rejections = %+v, want the first page's denial", rejections)
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
