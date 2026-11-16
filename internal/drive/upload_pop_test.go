package drive

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestUploadPopMatchesServerVectors(t *testing.T) {
	t.Parallel()
	popKey := func(content string) []byte {
		digest := newUploadPopHash()
		_, _ = io.WriteString(digest, content)
		return digest.Sum(nil)
	}
	if got := hex.EncodeToString(
		popKey(""),
	); got != "0fe2ccbb13cfec7fb2267d95590af7aa2995ca2edeee41f969a5012eca035e68" {
		t.Errorf("popKey(\"\") = %s", got)
	}
	key := popKey("abc")
	if got := hex.EncodeToString(key); got != "f5636cc7f69a5ce6a16e8c93b29d73b5ec23438f0f76f7612f56715ddfb38fbf" {
		t.Errorf("popKey(\"abc\") = %s", got)
	}

	nonce := make([]byte, 32)
	for index := range nonce {
		nonce[index] = byte(index)
	}
	upload := uploadPlanEntry("0192F3A4-5B6C-7D8E-9FA0-B1C2D3E4F506", "https://api.example/uploads/x", "token", "")
	upload.Challenge = &UploadChallenge{
		Version:  uploadPopVersion,
		Nonce:    hex.EncodeToString(nonce),
		ProveURL: "https://api.example/akasha/v2/uploads:prove",
	}
	claim, ok := uploadProofFor(upload, []FinalUploadFile{{PopKey: key}})
	if !ok {
		t.Fatal("no proof for a challenge with a known pop key")
	}
	if claim.Proof != "79da26bea2a560146e94269836144ce8889f188f35b72023e823722be6e2b1f9" {
		t.Errorf("proof = %s", claim.Proof)
	}
	if claim.IntentID != upload.IntentID || claim.Token != "token" {
		t.Errorf("claim = %+v", claim)
	}
}

func TestExecuteUploadPlanSendsBytesOnlyForUnprovenIntents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		status       int
		results      string
		wantUploaded []string
	}{
		{
			name:   "completed proof skips the bytes and a 202 is not resent",
			status: http.StatusAccepted,
			results: `{"results":[` +
				`{"intentId":"%one%","status":"completed","fileId":"stored"},` +
				`{"intentId":"%two%","status":"pending","reason":"upload_required","nextAction":"upload"}]}`,
			wantUploaded: []string{"two"},
		},
		{
			name:         "failed prove route falls back to the bytes",
			status:       http.StatusServiceUnavailable,
			wantUploaded: []string{"one", "two"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			intents := map[string]string{"one": uuid.NewString(), "two": uuid.NewString()}
			names := map[string]string{intents["one"]: "one", intents["two"]: "two"}
			var proveRequests atomic.Int32
			var uploadedMu sync.Mutex
			uploaded := make([]string, 0, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				switch {
				case request.URL.Path == "/akasha/v2/uploads:prove":
					proveRequests.Add(1)
					var body struct {
						Proofs []uploadProofClaim `json:"proofs"`
					}
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body.Proofs) != 2 {
						t.Errorf("proofs = %+v", body.Proofs)
					}
					for _, claim := range body.Proofs {
						if proof, err := hex.DecodeString(claim.Proof); err != nil || len(proof) != 32 ||
							claim.Token != "token-"+names[claim.IntentID] {
							t.Errorf("claim = %+v", claim)
						}
					}
					results := strings.NewReplacer("%one%", intents["one"], "%two%", intents["two"]).
						Replace(test.results)
					w.WriteHeader(test.status)
					_, _ = io.WriteString(w, results)
				case request.URL.Path == "/v2/uploads:pack":
					if err := request.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					var manifest struct {
						Entries []struct {
							IntentID string `json:"intentId"`
						} `json:"entries"`
					}
					_ = json.Unmarshal([]byte(request.FormValue("manifest")), &manifest)
					results := make([]map[string]string, 0, len(manifest.Entries))
					uploadedMu.Lock()
					for _, entry := range manifest.Entries {
						uploaded = append(uploaded, names[entry.IntentID])
						results = append(results, map[string]string{"intentId": entry.IntentID, "status": "completed"})
					}
					uploadedMu.Unlock()
					_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
				case strings.HasPrefix(request.URL.Path, "/v2/uploads/"):
					_, _ = io.Copy(io.Discard, request.Body)
					uploadedMu.Lock()
					uploaded = append(uploaded, names[strings.TrimPrefix(request.URL.Path, "/v2/uploads/")])
					uploadedMu.Unlock()
					_, _ = io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected %s %s", request.Method, request.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			directory := t.TempDir()
			sources := make([]UploadFile, 0, 2)
			for _, name := range []string{"one", "two"} {
				path := filepath.Join(directory, name+".ini")
				if err := os.WriteFile(path, []byte("content of "+name), 0o644); err != nil {
					t.Fatal(err)
				}
				sources = append(sources, UploadFile{
					FID: name, Name: name + ".ini", Size: int64(len("content of " + name)), FullPath: path,
				})
			}
			files, err := HashUploadFiles(t.Context(), sources, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			plan := UploadPlan{Uploads: map[string]UploadPlanEntry{}, Bundles: map[string]NTEBundle{}}
			for index, name := range []string{"one", "two"} {
				intentID := intents[name]
				plan.Items = append(plan.Items, UploadPlanItem{ClientID: name, Status: "pending", IntentID: intentID})
				upload := uploadPlanEntry(
					intentID,
					server.URL+"/v2/uploads/"+intentID,
					"token-"+name,
					files[index].SHA256,
				)
				upload.Challenge = &UploadChallenge{
					Version:  uploadPopVersion,
					Nonce:    strings.Repeat("ab", 32),
					ProveURL: server.URL + "/akasha/v2/uploads:prove",
				}
				plan.Uploads[intentID] = upload
			}

			var completedMu sync.Mutex
			completed := make([]string, 0, 2)
			_, err = uploadTestDrive(server).executeUploadPlanV2(
				context.Background(), files, plan, testUploadRules(), 4,
				func(progress UploadExecutionProgress) {
					if progress.FileID != "" {
						completedMu.Lock()
						completed = append(completed, progress.FileID)
						completedMu.Unlock()
					}
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if proveRequests.Load() != 1 {
				t.Errorf("prove requests = %d, want 1", proveRequests.Load())
			}
			slices.Sort(uploaded)
			if !slices.Equal(uploaded, test.wantUploaded) {
				t.Errorf("uploaded bytes of %v, want %v", uploaded, test.wantUploaded)
			}
			slices.Sort(completed)
			if !slices.Equal(completed, []string{"one", "two"}) {
				t.Errorf("completed = %v", completed)
			}
		})
	}
}
