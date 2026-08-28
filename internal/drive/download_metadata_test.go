package drive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

func TestFetchDirectoryDownloadMetadataDecodesJSONAndZstdCBORChunks(t *testing.T) {
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
	dirsEvent, _ := json.Marshal(downloadChunkEnvelope{Compressed: true, Data: base64.StdEncoding.EncodeToString(compressed), Type: "cbor"})
	filesJSON, _ := json.Marshal([]transfer.DownloadFile{{ID: "file", FileID: "file", ParentID: &parent, Name: "a.bin", Size: 7, URL: "https://download.invalid/a"}})
	filesEvent, _ := json.Marshal(downloadChunkEnvelope{Data: string(filesJSON)})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("uuid") != "root" || request.URL.Query().Get("linkId") != "link" || request.Header.Get("nhd-link-token") != "secret" {
			t.Fatalf("query = %v, link token = %q", request.URL.Query(), request.Header.Get("nhd-link-token"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: metadata\ndata: {\"root\":{\"id\":\"root\",\"parentId\":null,\"name\":\"Root\"},\"totalBytes\":7}\n\nevent: dirs\ndata: %s\n\nevent: files\ndata: %s\n\nevent: complete\ndata: {}\n\n", dirsEvent, filesEvent)
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline})})
	metadata, err := drive.fetchDirectoryDownloadMetadata(context.Background(), "root", &DownloadLink{LinkID: "link", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Root.ID != "root" || metadata.TotalBytes != 7 || len(metadata.Dirs) != 1 || metadata.Dirs[0].Name != "Sub" || len(metadata.Files) != 1 || metadata.Files[0].Name != "a.bin" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestFetchDownloadMetadataBatchesFilesAndBuildsBatchRoot(t *testing.T) {
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
			files[index] = transfer.DownloadFile{ID: id, Name: id + ".bin", Size: int64(index + 1), URL: "https://download.invalid/" + id}
		}
		if err := json.NewEncoder(w).Encode(files); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline})})
	items := make([]DownloadItem, 0, downloadFileBatchLimit+1)
	for index := 0; index <= downloadFileBatchLimit; index++ {
		items = append(items, DownloadItem{ID: fmt.Sprintf("file-%d", index)})
	}
	metadata, err := drive.fetchDownloadMetadata(context.Background(), items, nil)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Root.ID != "batch-root" || len(metadata.Files) != len(items) || metadata.Files[0].ParentID == nil || *metadata.Files[0].ParentID != "batch-root" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestFetchFileDownloadMetadataRejectsMissingSelectedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"one","name":"one.bin","size":1,"url":"https://download.invalid/one"}]`)
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline})})
	_, err := drive.fetchDownloadMetadata(context.Background(), []DownloadItem{{ID: "one"}, {ID: "two"}}, nil)
	if err == nil {
		t.Fatal("expected missing selected file error")
	}
}
