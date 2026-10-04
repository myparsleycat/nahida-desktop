package drive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

func enumerateDownloadForTest(
	t *testing.T,
	drive *Drive,
	params StartDownloadParams,
) (*downloadPlan, []transfer.DownloadFile, error) {
	t.Helper()
	spoolPath := filepath.Join(t.TempDir(), "test"+downloadSpoolExt)
	plan, err := drive.enumerateDownload(
		context.Background(),
		params,
		downloadLayout{},
		spoolPath,
		func(int, int64) {},
	)
	if err != nil {
		if _, statErr := os.Stat(spoolPath); !os.IsNotExist(statErr) {
			t.Fatalf("failed enumeration left its spool behind: %v", statErr)
		}
		return nil, nil, err
	}
	var files []transfer.DownloadFile
	for item, readErr := range plan.files() {
		if readErr != nil {
			t.Fatal(readErr)
		}
		files = append(files, item.file)
	}
	return plan, files, nil
}

func TestEnumerateDownloadDecodesJSONAndZstdCBORChunks(t *testing.T) {
	parent := "root"
	directories := []transfer.Directory{{ID: "dir", ParentID: &parent, Name: "Sub"}}
	cborData, err := cbor.Marshal(directories)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	compressed := encoder.EncodeAll(cborData, nil)
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	dirsEvent, _ := json.Marshal(
		downloadChunkEnvelope{Compressed: true, Data: base64.StdEncoding.EncodeToString(compressed), Type: "cbor"},
	)
	filesJSON, _ := json.Marshal(
		[]transfer.DownloadFile{
			{ID: "file", ParentID: &parent, Name: "a.bin", Size: 7, URL: "https://download.invalid/a"},
		},
	)
	filesEvent, _ := json.Marshal(downloadChunkEnvelope{Data: string(filesJSON)})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("uuid") != "root" || request.URL.Query().Get("linkId") != "link" ||
			request.Header.Get("nhd-link-token") != "secret" {
			t.Fatalf("query = %v, link token = %q", request.URL.Query(), request.Header.Get("nhd-link-token"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(
			w,
			"event: metadata\ndata: {\"root\":{\"id\":\"root\",\"parentId\":null,\"name\":\"Root\"},\"totalBytes\":7}\n\nevent: dirs\ndata: %s\n\nevent: files\ndata: %s\n\nevent: complete\ndata: {}\n\n",
			dirsEvent,
			filesEvent,
		)
	}))
	defer server.Close()
	drive := NewWithOptions(
		Options{
			HTTP: infra.NewClientWithOptions(
				infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline},
			),
		},
	)
	plan, files, err := enumerateDownloadForTest(t, drive, StartDownloadParams{
		Items: []DownloadItem{{ID: "root", IsDir: true}},
		Link:  &DownloadLink{LinkID: "link", Token: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.root.ID != "root" || plan.root.Name != "Root" || plan.totalBytes != 7 || plan.fileCount != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if len(plan.dirs) != 2 || plan.dirs[0].ID != "root" || plan.dirs[0].Name != "Root" || plan.dirs[1].Name != "Sub" {
		t.Fatalf("directories = %+v", plan.dirs)
	}
	if len(files) != 1 || files[0].Name != "a.bin" || files[0].FileID != "file" {
		t.Fatalf("files = %+v", files)
	}
}

func TestEnumerateDownloadReportsStreamFailure(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "error event", body: "event: error\ndata: walk failed\n\n", want: "walk failed"},
		{
			name: "missing root",
			body: "event: complete\ndata: {}\n\n",
			want: "root directory information was not received",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(
				infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline},
			)})
			_, _, err := enumerateDownloadForTest(t, drive, StartDownloadParams{
				Items: []DownloadItem{{ID: "root", IsDir: true}},
			})
			if err == nil || err.Error() != test.want {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}
}

func TestEnumerateDownloadSendsModCredentialsAndPrependsRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/mod/download/item" || request.Header.Get("x-token") != "token" ||
			request.Header.Get("x-sig") != "sig" {
			t.Errorf("path = %q, x-token = %q, x-sig = %q",
				request.URL.Path, request.Header.Get("x-token"), request.Header.Get("x-sig"))
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(
			w,
			"event: metadata\ndata: {\"root\":{\"id\":\"item\",\"parentId\":null,\"name\":\"Mod\"},\"totalBytes\":0}\n\nevent: complete\ndata: {}\n\n",
		)
	}))
	defer server.Close()
	drive := NewWithOptions(
		Options{
			HTTP: infra.NewClientWithOptions(
				infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline},
			),
		},
	)

	plan, _, err := enumerateDownloadForTest(t, drive, StartDownloadParams{
		Items: []DownloadItem{{ID: "item", IsDir: true, Name: "Mod"}},
		Mod:   &DownloadModAccess{Token: "token", Sig: "sig"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.root.ID != "item" || len(plan.dirs) != 1 || plan.dirs[0].ID != "item" {
		t.Fatalf("plan = %+v", plan)
	}

	if _, _, err := enumerateDownloadForTest(t, drive, StartDownloadParams{
		Items: []DownloadItem{{ID: "file", Name: "a.bin"}},
		Mod:   &DownloadModAccess{},
	}); err == nil {
		t.Fatal("mod file download was accepted")
	}
}

func TestFetchPresignedDownloadURLUsesModRouteAndCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/mod/file/download" || request.URL.Query().Get("itemId") != "item" ||
			request.URL.Query().Get("presign") != "true" || request.Header.Get("x-token") != "token" ||
			request.Header.Get("x-sig") != "sig" {
			t.Errorf("path = %q, query = %v, x-token = %q, x-sig = %q", request.URL.Path, request.URL.Query(),
				request.Header.Get("x-token"), request.Header.Get("x-sig"))
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"url":"https://download.invalid/fresh"}`)
	}))
	defer server.Close()
	drive := NewWithOptions(
		Options{
			HTTP: infra.NewClientWithOptions(
				infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline},
			),
		},
	)

	got, err := drive.fetchPresignedDownloadURL(
		context.Background(),
		"item",
		downloadAccess{Mod: &DownloadModAccess{Token: "token", Sig: "sig"}},
	)
	if err != nil || got != "https://download.invalid/fresh" {
		t.Fatalf("presigned URL = %q, %v", got, err)
	}
}

func TestEnumerateDownloadBatchesFilesAndBuildsBatchRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/file/downloads" {
			http.NotFound(w, request)
			return
		}
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		files := make([]transfer.DownloadFile, len(body.IDs))
		for index, id := range body.IDs {
			files[index] = transfer.DownloadFile{
				ID:   id,
				Name: id + ".bin",
				Size: int64(index + 1),
				URL:  "https://download.invalid/" + id,
			}
		}
		if err := json.NewEncoder(w).Encode(files); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	drive := NewWithOptions(
		Options{
			HTTP: infra.NewClientWithOptions(
				infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline},
			),
		},
	)
	items := make([]DownloadItem, 0, downloadFileBatchLimit+1)
	for index := 0; index <= downloadFileBatchLimit; index++ {
		items = append(items, DownloadItem{ID: fmt.Sprintf("file-%d", index)})
	}
	plan, files, err := enumerateDownloadForTest(t, drive, StartDownloadParams{Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if plan.root.ID != "batch-root" || len(files) != len(items) || len(plan.rootFiles) != len(items) ||
		files[0].ParentID == nil || *files[0].ParentID != "batch-root" || files[0].FileID != "file-0" {
		t.Fatalf("plan = %+v, first file = %+v", plan, files[0])
	}
	// 1+2+...+100 for the first batch, then 1 for the single file of the second.
	if want := int64(downloadFileBatchLimit*(downloadFileBatchLimit+1)/2 + 1); plan.totalBytes != want {
		t.Fatalf("total bytes = %d, want %d", plan.totalBytes, want)
	}
}

func TestEnumerateDownloadRejectsMissingSelectedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"one","name":"one.bin","size":1,"url":"https://download.invalid/one"}]`)
	}))
	defer server.Close()
	drive := NewWithOptions(
		Options{
			HTTP: infra.NewClientWithOptions(
				infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline},
			),
		},
	)
	_, _, err := enumerateDownloadForTest(
		t,
		drive,
		StartDownloadParams{Items: []DownloadItem{{ID: "one"}, {ID: "two"}}},
	)
	if err == nil {
		t.Fatal("expected missing selected file error")
	}
}
