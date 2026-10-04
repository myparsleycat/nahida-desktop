package drive

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

// BenchmarkEnumerateDownloadMillionFiles walks a folder of a million files and
// reports the heap the finished plan retains next to what the same list costs
// as an in-memory slice.
func BenchmarkEnumerateDownloadMillionFiles(b *testing.B) {
	const (
		totalFiles = 1_000_000
		chunkSize  = 5_000
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The encoder lives only for the request, so its buffers are not
		// counted against the client once the walk ends.
		encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
		if err != nil {
			b.Error(err)
			return
		}
		defer func() { _ = encoder.Close() }()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(
			w,
			"event: metadata\ndata: {\"root\":{\"id\":\"root\",\"name\":\"Root\"},\"totalBytes\":1}\n\n",
		)
		parent := "root"
		files := make([]transfer.DownloadFile, chunkSize)
		for start := 0; start < totalFiles; start += chunkSize {
			for index := range files {
				id := fmt.Sprintf("00000000-0000-4000-8000-%012d", start+index)
				files[index] = transfer.DownloadFile{
					ID: id, FileID: id, ParentID: &parent, Name: id + ".bin", Size: 1024,
					URL: "https://download.invalid/files/" + id + "?signature=0123456789abcdef0123456789abcdef",
				}
			}
			raw, _ := cbor.Marshal(files)
			event, _ := json.Marshal(downloadChunkEnvelope{
				Compressed: true, Type: "cbor", Data: base64.StdEncoding.EncodeToString(encoder.EncodeAll(raw, nil)),
			})
			_, _ = fmt.Fprintf(w, "event: files\ndata: %s\n\n", event)
		}
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(
		infra.ClientOptions{HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline},
	)})
	heapMiB := func() float64 {
		// Pooled buffers survive one collection in the victim cache.
		runtime.GC()
		runtime.GC()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		return float64(stats.HeapAlloc) / (1 << 20)
	}

	b.ReportAllocs()
	for b.Loop() {
		baseline := heapMiB()
		plan, err := drive.enumerateDownload(
			b.Context(),
			StartDownloadParams{Items: []DownloadItem{{ID: "root", IsDir: true, Name: "Root"}}},
			downloadLayout{rootName: "Root"},
			b.TempDir()+"/bench"+downloadSpoolExt,
			func(int, int64) {},
		)
		if err != nil || plan.fileCount != totalFiles {
			b.Fatalf("plan = %+v, %v", plan, err)
		}
		b.ReportMetric(heapMiB()-baseline, "plan-MiB")

		list := make([]transfer.DownloadFile, 0, plan.fileCount)
		for item, err := range plan.files() {
			if err != nil {
				b.Fatal(err)
			}
			list = append(list, item.file)
		}
		b.ReportMetric(heapMiB()-baseline, "slice-MiB")
		runtime.KeepAlive(list)
		runtime.KeepAlive(plan)
	}
}
