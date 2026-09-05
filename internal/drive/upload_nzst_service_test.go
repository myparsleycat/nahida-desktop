package drive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

func TestUploadNZSTServiceTransportsAndRestartCleanup(t *testing.T) {
	for _, mode := range []string{"direct", "parts", "pack", "failure", "pause", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			tempRoot := t.TempDir()
			t.Setenv("TMP", tempRoot)
			t.Setenv("TEMP", tempRoot)
			root := t.TempDir()
			archive := filepath.Join(root, "file.ini.nzst")
			content := []byte("payload")
			writeUploadNZST(t, archive, content)
			paths := []string{archive}
			if mode == "pack" {
				other := filepath.Join(root, "other.ini.nzst")
				writeUploadNZST(t, other, content)
				paths = append(paths, other)
			}
			transfers := transfer.New()
			var pid string
			var requests atomic.Int32
			var restart atomic.Bool
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				switch {
				case request.URL.Path == "/akasha/content/dest":
					_, _ = io.WriteString(w, `{"children":[]}`)
				case request.URL.Path == "/akasha/v2/sse/drive/files:plan":
					var plan struct {
						Files []struct {
							ClientID, Name, SHA256 string
							Size                   int64
						}
					}
					if err := json.NewDecoder(request.Body).Decode(&plan); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					items := make([]map[string]any, 0, len(plan.Files))
					uploads := make([]map[string]any, 0, len(plan.Files))
					for index, file := range plan.Files {
						if strings.HasSuffix(file.Name, ".nzst") || file.Size != int64(len(content)) || file.SHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) {
							t.Errorf("plan file = %+v", file)
						}
						intent := fmt.Sprintf("intent-%d", index)
						items = append(items, map[string]any{"clientId": file.ClientID, "status": "pending", "intentId": intent})
						uploads = append(uploads, map[string]any{"intentId": intent, "url": server.URL + "/v2/uploads/" + intent, "method": "POST", "form": map[string]string{"token": "test", "sha256": file.SHA256}})
					}
					payload, err := json.Marshal(map[string]any{"items": items, "uploads": uploads})
					if err != nil {
						t.Error(err)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "event: complete\ndata: %s\n\n", payload)
				case strings.HasSuffix(request.URL.Path, "/complete"):
					_, _ = io.WriteString(w, `{}`)
				default:
					requests.Add(1)
					entries, err := filepath.Glob(filepath.Join(tempRoot, "nahida-drive-upload-*", "source-*"))
					if err != nil || len(entries) != len(paths) {
						t.Errorf("temporary sources during upload = %v, %v", entries, err)
					}
					if !restart.Load() {
						switch mode {
						case "failure":
							w.WriteHeader(http.StatusBadRequest)
							return
						case "pause":
							if err := transfers.Pause(pid); err != nil {
								t.Error(err)
							}
							return
						case "cancel":
							if err := transfers.Cancel(pid); err != nil {
								t.Error(err)
							}
							return
						}
					}
					if err := request.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						return
					}
					defer func() { _ = request.MultipartForm.RemoveAll() }()
					if mode == "pack" {
						if request.URL.Path != "/v2/uploads:pack" {
							t.Errorf("expected pack route, got %s", request.URL.Path)
						}
						var manifest struct {
							Entries []struct {
								IntentID, CompAlg string
								PayloadBytes      int64
							}
						}
						if err := json.Unmarshal([]byte(request.FormValue("manifest")), &manifest); err != nil {
							t.Error(err)
							return
						}
						file, _, err := request.FormFile("pack")
						if err != nil {
							t.Error(err)
							return
						}
						defer func() { _ = file.Close() }()
						results := make([]map[string]string, 0, len(manifest.Entries))
						for _, entry := range manifest.Entries {
							assertUploadNZSTPayload(t, io.LimitReader(file, entry.PayloadBytes), entry.CompAlg, content)
							results = append(results, map[string]string{"intentId": entry.IntentID, "status": "completed"})
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
						return
					}
					if mode == "parts" && !strings.HasSuffix(request.URL.Path, "/parts/0") {
						t.Errorf("expected parts route, got %s", request.URL.Path)
					}
					file, header, err := request.FormFile("file")
					if err != nil {
						t.Error(err)
						return
					}
					defer func() { _ = file.Close() }()
					if header.Filename != "file.ini" {
						t.Errorf("filename = %q", header.Filename)
					}
					assertUploadNZSTPayload(t, file, request.FormValue("compAlg"), content)
					_, _ = io.WriteString(w, `{}`)
				}
			}))
			defer server.Close()
			drive := uploadServiceTestDrive(server, transfers)
			rules := testUploadRules()
			if mode == "parts" {
				rules.MaxUploadBodyBytes = 32
			}
			drive.setUploadRules(rules)
			conflicts, err := drive.GetUploadConflicts(t.Context(), GetUploadConflictsParams{DestID: "dest", Paths: paths})
			if err != nil || len(conflicts.SkippedExtensions) != 0 {
				t.Fatalf("conflicts = %+v, %v", conflicts, err)
			}
			result, err := drive.StartUpload(t.Context(), StartUploadParams{DestID: "dest", Paths: paths})
			if err != nil {
				t.Fatal(err)
			}
			pid = result.PID
			expect := transfer.StatusCompleted
			switch mode {
			case "failure":
				expect = transfer.StatusError
			case "pause":
				expect = transfer.StatusPaused
			case "cancel":
				expect = transfer.StatusCanceled
			}
			for range 2 {
				if err := transfers.ProcessQueue(t.Context()); err != nil {
					t.Fatal(err)
				}
				record, ok := transfers.Get(pid)
				if !ok || record.Status != expect {
					t.Fatalf("record = %+v, want %s", record, expect)
				}
				entries, err := filepath.Glob(filepath.Join(tempRoot, "nahida-drive-upload-*"))
				if err != nil || len(entries) != 0 {
					t.Fatalf("temporary sources leaked = %v, %v", entries, err)
				}
				if _, err := os.Stat(archive); err != nil {
					t.Fatal(err)
				}
				if expect == transfer.StatusCompleted {
					if record.TotalSize != int64(len(content)*len(paths)) || record.TransferredSize != record.TotalSize {
						t.Fatalf("sizes = %+v", record)
					}
					break
				}
				// Same length, different bytes: a new decoded file must not reuse
				// the previous execution's cached hash.
				content = []byte("changed")
				writeUploadNZST(t, archive, content)
				restart.Store(true)
				if mode == "pause" {
					err = transfers.Resume(pid)
				} else {
					err = transfers.Retry(pid)
				}
				if err != nil {
					t.Fatal(err)
				}
				expect = transfer.StatusCompleted
			}
			if requests.Load() == 0 {
				t.Fatal("no upload request")
			}
		})
	}
}

func assertUploadNZSTPayload(t *testing.T, reader io.Reader, compression string, want []byte) {
	t.Helper()
	if compression == "zstd" {
		decoder, err := zstd.NewReader(reader)
		if err != nil {
			t.Error(err)
			return
		}
		defer decoder.Close()
		reader = decoder
	}
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("uploaded content = %q, want %q, err = %v", got, want, err)
	}
}

func TestUploadNZSTRestoreFailureCleansUpBeforeRemoteMutation(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TMP", tempRoot)
	t.Setenv("TEMP", tempRoot)
	root := t.TempDir()
	archive := filepath.Join(root, "file.ini.nzst")
	writeUploadNZST(t, archive, []byte("original"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/content/dest" {
			t.Errorf("restore failure reached remote operation: %s", request.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"children":[]}`)
	}))
	defer server.Close()
	var logs bytes.Buffer
	transfers := transfer.New()
	drive := uploadServiceTestDrive(server, transfers)
	drive.UseLog(infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true}))
	drive.setUploadRules(testUploadRules())
	result, err := drive.StartUpload(t.Context(), StartUploadParams{DestID: "dest", Paths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	writeUploadFile(t, archive, "corrupt after preparation")
	if err := transfers.ProcessQueue(t.Context()); err != nil {
		t.Fatal(err)
	}
	record, ok := transfers.Get(result.PID)
	if !ok || record.Status != transfer.StatusError {
		t.Fatalf("record = %+v", record)
	}
	entries, err := filepath.Glob(filepath.Join(tempRoot, "nahida-drive-upload-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("leaked temporary files: %v, %v", entries, err)
	}
	for _, field := range []string{`"stage":"restore-sources"`, `"destinationId":"dest"`, `"name":"file.ini"`, `"inputPath":`, `"tempPath":`, `"cleanupRegistered":true`} {
		if !strings.Contains(logs.String(), field) {
			t.Errorf("missing diagnostic %s: %s", field, logs.String())
		}
	}
}

func TestUploadNZSTResumeSkipsDeletedCompletedSources(t *testing.T) {
	for _, completedName := range []string{"done.ini", "done.ini.nzst"} {
		for _, allCompleted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/all-completed=%t", completedName, allCompleted), func(t *testing.T) {
				tempRoot := t.TempDir()
				t.Setenv("TMP", tempRoot)
				t.Setenv("TEMP", tempRoot)
				root := t.TempDir()
				done := filepath.Join(root, completedName)
				pending := filepath.Join(root, "pending.ini.nzst")
				if isUploadNZST(done) {
					writeUploadNZST(t, done, []byte("done"))
				} else {
					writeUploadFile(t, done, "done")
				}
				writeUploadNZST(t, pending, []byte("pending"))
				prep, err := PrepareUpload([]string{done, pending}, nil, "", testUploadRules(), nil, false)
				if err != nil {
					t.Fatal(err)
				}
				transfers := transfer.New()
				restart := &uploadRestartData{Params: StartUploadParams{DestID: "dest"}, Preparation: prep, RequestID: "resume"}
				_, err = transfers.Create(transfer.CreateParams{
					PID: prep.PID, Type: "upload", InitialStatus: transfer.StatusPending, RestartData: restart,
					Data: transfer.Data{Files: []transfer.DownloadFile{
						{ID: prep.Files[0].FID, Name: "done.ini", Size: 4},
						{ID: prep.Files[1].FID, Name: "pending.ini", Size: 7},
					}},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := transfers.MarkFileCompleted(prep.PID, prep.Files[0].FID); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(done); err != nil {
					t.Fatal(err)
				}
				if allCompleted {
					if err := transfers.MarkFileCompleted(prep.PID, prep.Files[1].FID); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(pending); err != nil {
						t.Fatal(err)
					}
				}
				var planned atomic.Bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					planned.Store(true)
					if allCompleted {
						t.Error("all-completed resume made a remote request")
						w.WriteHeader(500)
						return
					}
					var plan struct {
						Files []struct {
							ClientID, Name, SHA256 string
							Size                   int64
						}
					}
					if err := json.NewDecoder(request.Body).Decode(&plan); err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					if len(plan.Files) != 1 || plan.Files[0].ClientID != prep.Files[1].FID || plan.Files[0].Name != "pending.ini" || plan.Files[0].Size != 7 || plan.Files[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("pending"))) {
						t.Errorf("resume plan = %+v", plan)
					}
					record, _ := transfers.Get(prep.PID)
					if record.TransferredFiles != 1 || record.TransferredSize != 4 {
						t.Errorf("completed progress lost: %+v", record)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "event: complete\ndata: {\"items\":[{\"clientId\":%q,\"status\":\"exists\"}],\"uploads\":[]}\n\n", prep.Files[1].FID)
				}))
				defer server.Close()
				drive := uploadServiceTestDrive(server, transfers)
				drive.setUploadRules(testUploadRules())
				state := &uploadRunnerState{hashes: map[string]string{prep.Files[0].FID: "cached-done", prep.Files[1].FID: "cached-pending"}}
				if err := transfers.RegisterRunner(prep.PID, func(ctx context.Context, transfers *transfer.Transfer, pid string) error {
					return drive.runUpload(ctx, transfers, pid, restart, state)
				}); err != nil {
					t.Fatal(err)
				}
				if err := transfers.ProcessQueue(t.Context()); err != nil {
					t.Fatal(err)
				}
				record, ok := transfers.Get(prep.PID)
				if !ok || record.Status != transfer.StatusCompleted || record.TransferredFiles != 2 || record.TransferredSize != 11 || record.TotalSize != 11 {
					t.Fatalf("resume record = %+v", record)
				}
				if planned.Load() == allCompleted {
					t.Fatalf("planned = %v, all completed = %v", planned.Load(), allCompleted)
				}
				entries, err := filepath.Glob(filepath.Join(tempRoot, "nahida-drive-upload-*"))
				if err != nil || len(entries) != 0 {
					t.Fatalf("leaked temporary sources = %v, %v", entries, err)
				}
			})
		}
	}
}
