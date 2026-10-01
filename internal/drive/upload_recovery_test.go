package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/transfer"
)

func TestUploadRecoveryReplansOnlyTemporaryFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		permanent  bool
		persistent bool
		cancel     bool
	}{
		{name: "recovered"},
		{name: "permanent failure remains", permanent: true},
		{name: "recovery is bounded", persistent: true},
		{name: "cancel while waiting", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var packs, plans, directs atomic.Int32
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/uploads:pack":
					packs.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					last := "completed"
					if tc.permanent {
						last = "failed"
					}
					_, _ = fmt.Fprintf(
						w,
						`{"results":[{"intentId":"one","status":"completed"},{"intentId":"two","status":"failed","reason":"storage_temporarily_unavailable"},{"intentId":"three","status":%q,"reason":"unsupported_file_type"}]}`,
						last,
					)
				case "/akasha/v2/sse/drive/files:plan":
					plans.Add(1)
					var body struct {
						RequestID string `json:"requestId"`
						Files     []struct {
							ClientID string `json:"clientId"`
						} `json:"files"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.RequestID == "" || len(body.Files) != 1 || body.Files[0].ClientID != "two" {
						t.Errorf("replanned completed/permanent files: %+v", body)
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(
						w,
						"event: complete\ndata: {\"items\":[{\"clientId\":\"two\",\"status\":\"pending\",\"intentId\":\"retry\"}],\"uploads\":[{\"intentId\":\"retry\",\"url\":%q,\"method\":\"POST\",\"form\":{\"token\":\"token\",\"sha256\":\"hash\"}}]}\n\n",
						server.URL+"/v2/uploads/retry",
					)
				case "/v2/uploads/retry":
					directs.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					if tc.persistent {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = io.WriteString(w, storageTemporaryFailure)
						return
					}
					_, _ = io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			drive := uploadServiceTestDrive(server, transfer.New())
			if tc.cancel {
				drive.sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
			}
			files := make([]FinalUploadFile, 0, 3)
			plan := UploadPlan{Uploads: make(map[string]UploadPlanEntry)}
			for _, id := range []string{"one", "two", "three"} {
				path := filepath.Join(t.TempDir(), id+".ini")
				if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
				files = append(
					files,
					FinalUploadFile{
						UploadFile: UploadFile{FID: id, Name: id + ".ini", FullPath: path, Size: 4},
						ParentID:   "dest",
						SHA256:     id,
					},
				)
				plan.Items = append(plan.Items, UploadPlanItem{ClientID: id, IntentID: id, Status: "pending"})
				plan.Uploads[id] = uploadPlanEntry(id, server.URL+"/v2/uploads/"+id, "token", "hash")
			}
			var credited int64
			ready := make(map[string]int)
			_, err := drive.executeUploadPlanWithRecovery(
				ctx,
				"dest",
				files,
				plan,
				testUploadRules(),
				8,
				func(progress UploadExecutionProgress) {
					credited += progress.Bytes
					if progress.FileID != "" {
						ready[progress.FileID]++
					}
				},
			)
			if packs.Load() != 1 || ready["one"] != 1 {
				t.Fatalf("successful file resent: packs=%d ready=%v", packs.Load(), ready)
			}
			if tc.cancel {
				if !errors.Is(err, context.Canceled) || plans.Load() != 0 {
					t.Fatalf("cancel: %v plans=%d", err, plans.Load())
				}
				return
			}
			if tc.persistent {
				if err == nil || plans.Load() != uploadRecoveryLimit || directs.Load() != uploadRecoveryLimit ||
					ready["two"] != 0 ||
					credited != 8 {
					t.Fatalf("bounded failure: %v plans=%d credits=%d ready=%v", err, plans.Load(), credited, ready)
				}
				return
			}
			if plans.Load() != 1 || directs.Load() != 1 || ready["two"] != 1 {
				t.Fatalf("recovery: %v plans=%d direct=%d ready=%v", err, plans.Load(), directs.Load(), ready)
			}
			if tc.permanent {
				if err == nil || !strings.Contains(err.Error(), "unsupported_file_type") || credited != 8 {
					t.Fatalf("permanent failure: %v credits=%d", err, credited)
				}
			} else if err != nil || credited != 12 {
				t.Fatalf("recovery result: %v credits=%d", err, credited)
			}
		})
	}
}

func TestStorageFailureKeepsTransportRetriesWithoutRecoveryOwner(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ids      []string
		bundled  bool
		recovery bool
		want     map[string]int
	}{
		{name: "direct plan execution", ids: []string{"one"}, want: map[string]int{"/v2/uploads/one": 2}},
		{
			name:     "whole pack",
			ids:      []string{"one", "two"},
			recovery: true,
			want:     map[string]int{"/v2/uploads:pack": 2},
		},
		{
			name:     "bundle member",
			ids:      []string{"one"},
			bundled:  true,
			recovery: true,
			want:     map[string]int{"/v2/uploads/one": 2, "/bundle/complete": 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			requests := make(map[string]int)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				mu.Lock()
				requests[r.URL.Path]++
				first := requests[r.URL.Path] == 1
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				if first {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"reason":"storage_temporarily_unavailable"}`)
					return
				}
				_, _ = io.WriteString(
					w,
					`{"results":[{"intentId":"one","status":"completed"},{"intentId":"two","status":"completed"}]}`,
				)
			}))
			defer server.Close()
			drive := uploadServiceTestDrive(server, transfer.New())
			files := make([]FinalUploadFile, 0, len(tc.ids))
			plan := UploadPlan{Uploads: make(map[string]UploadPlanEntry), Bundles: make(map[string]NTEBundle)}
			for _, id := range tc.ids {
				path := filepath.Join(t.TempDir(), id+".ini")
				if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
				files = append(
					files,
					FinalUploadFile{
						UploadFile: UploadFile{FID: id, Name: id + ".ini", FullPath: path, Size: 4},
						ParentID:   "dest",
						SHA256:     id,
					},
				)
				item := UploadPlanItem{ClientID: id, IntentID: id, Status: "pending"}
				if tc.bundled {
					item.BundleID = "bundle"
				}
				plan.Items = append(plan.Items, item)
				plan.Uploads[id] = uploadPlanEntry(id, server.URL+"/v2/uploads/"+id, "token", "hash")
			}
			if tc.bundled {
				plan.Bundles["bundle"] = NTEBundle{
					ID:              "bundle",
					MemberClientIDs: tc.ids,
					CompleteURL:     server.URL + "/bundle/complete",
					AbortURL:        server.URL + "/bundle/abort",
				}
			}

			ready := 0
			onProgress := func(progress UploadExecutionProgress) {
				if progress.FileID != "" {
					ready++
				}
			}
			var err error
			if tc.recovery {
				_, err = drive.executeUploadPlanWithRecovery(
					t.Context(),
					"dest",
					files,
					plan,
					testUploadRules(),
					8,
					onProgress,
				)
			} else {
				_, err = drive.executeUploadPlanV2(t.Context(), files, plan, testUploadRules(), 8, onProgress)
			}
			if err != nil || ready != len(tc.ids) || !maps.Equal(requests, tc.want) {
				t.Fatalf("transport retry: %v ready=%d requests=%v", err, ready, requests)
			}
		})
	}
}

func TestUploadRecoveryDoesNotRetryAbortedBundlesOrUnknownFailures(t *testing.T) {
	file := FinalUploadFile{UploadFile: UploadFile{FID: "one", Name: "one.utoc"}}
	plan := UploadPlan{Items: []UploadPlanItem{{ClientID: "one", BundleID: "bundle"}}}
	for _, code := range []string{storageTemporaryFailure, "pack_ingest_failed", "upload_failed", "sha256_mismatch"} {
		err := &packMemberError{files: []FinalUploadFile{file}, err: &UploadV2Error{Code: code}}
		ids, permanent := splitUploadRecovery(err, plan)
		if len(ids) != 0 || len(permanent) != 1 {
			t.Fatalf("bundle/unknown failure %q: retry=%v permanent=%v", code, ids, permanent)
		}
	}
	for _, code := range []string{"pack_ingest_failed", "upload_failed", "sha256_mismatch"} {
		t.Run(code, func(t *testing.T) {
			err := &packMemberError{files: []FinalUploadFile{file}, err: &UploadV2Error{Code: code}}
			ids, permanent := splitUploadRecovery(err, UploadPlan{})
			if len(ids) != 0 || len(permanent) != 1 {
				t.Fatalf("unknown failure %q: retry=%v permanent=%v", code, ids, permanent)
			}
		})
	}
}
