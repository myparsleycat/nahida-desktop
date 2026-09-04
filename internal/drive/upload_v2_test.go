package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

func TestPaginateUploadFilesDoesNotSplitNTEBundle(t *testing.T) {
	files := make([]FinalUploadFile, 0, 502)
	for index := range 499 {
		files = append(files, FinalUploadFile{UploadFile: UploadFile{FID: fmt.Sprintf("ordinary-%d", index), Name: fmt.Sprintf("ordinary-%d.ini", index)}})
	}
	for _, name := range []string{"Game.pak", "Game.utoc", "Game_s1.ucas"} {
		files = append(files, FinalUploadFile{UploadFile: UploadFile{FID: name, Name: name}, ParentID: "parent"})
	}
	pages, err := paginateUploadFiles(files, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || len(pages[0]) != 499 || len(pages[1]) != 3 {
		t.Fatalf("page sizes = %v, %v", len(pages[0]), len(pages[1]))
	}
}

func TestNTEGroupKeyNormalizesUnicodeAndShardSuffix(t *testing.T) {
	composed := FinalUploadFile{UploadFile: UploadFile{Name: "Café.pak"}, ParentID: "parent"}
	decomposed := FinalUploadFile{UploadFile: UploadFile{Name: "Cafe\u0301_s2.ucas"}, ParentID: "parent"}
	if got, want := nteGroupKey(decomposed), nteGroupKey(composed); got != want {
		t.Fatalf("group keys differ: %q, %q", got, want)
	}
}

func TestPlanUploadV2ConsumesProgressAndCompleteEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/v2/sse/drive/files:plan" || request.Method != http.MethodPost {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body struct {
			RequestID    string            `json:"requestId"`
			Current      string            `json:"current"`
			Capabilities []string          `json:"capabilities"`
			Files        []json.RawMessage `json:"files"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.RequestID != "request-id" || body.Current != "destination" || len(body.Files) != 1 {
			t.Fatalf("plan body = %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: progress\ndata: {\"phase\":\"file_validation\",\"processed\":1,\"total\":1}\n\n")
		_, _ = io.WriteString(w, "event: complete\ndata: {\"items\":[{\"clientId\":\"client\",\"status\":\"pending\",\"intentId\":\"intent\"}],\"uploads\":[{\"intentId\":\"intent\",\"url\":\"https://upload.example/intent\",\"method\":\"POST\",\"form\":{\"token\":\"token\",\"sha256\":\"hash\"}}],\"nteBundles\":[]}\n\n")
	}))
	defer server.Close()
	client := infra.NewClientWithOptions(infra.ClientOptions{BackendURL: server.URL, HTTPClient: server.Client(), Status: infra.BackendOnline})
	drive := NewWithOptions(Options{HTTP: client})
	drive.setUploadRules(testUploadRules())
	var progress UploadPlanProgress
	plan, err := drive.planUploadV2(context.Background(), "destination", "request-id", []FinalUploadFile{{
		UploadFile: UploadFile{FID: "client", Name: "file.ini", Path: "file.ini", Size: 4},
		ParentID:   "destination",
		SHA256:     "hash",
	}}, func(update UploadPlanProgress) { progress = update })
	if err != nil {
		t.Fatal(err)
	}
	if progress.Phase != transfer.PlanFileValidation || progress.Processed != 1 || progress.Total != 1 {
		t.Fatalf("progress = %#v", progress)
	}
	if len(plan.Items) != 1 || plan.Items[0].IntentID != "intent" || plan.Uploads["intent"].Form.Token != "token" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanUploadV2BoundsPageSizeToFileCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/akasha/v2/sse/drive/files:plan" || request.Method != http.MethodPost {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		var body struct {
			Files []json.RawMessage `json:"files"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Files) != 1 {
			t.Fatalf("page size = %d, want 1", len(body.Files))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: complete\ndata: {\"items\":[{\"clientId\":\"client\",\"status\":\"pending\",\"intentId\":\"intent\"}],\"uploads\":[],\"nteBundles\":[]}\n\n")
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})
	rules := testUploadRules()
	rules.MaxPlanFiles = 1_000_000_000
	drive.setUploadRules(rules)
	if _, err := drive.planUploadV2(context.Background(), "destination", "request-id", []FinalUploadFile{{
		UploadFile: UploadFile{FID: "client", Name: "file.ini"},
	}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPlanUploadV2PreservesServerErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":"upload_denied","message":"denied"}`)
	}))
	defer server.Close()
	drive := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: server.URL,
		HTTPClient: server.Client(),
		Status:     infra.BackendOnline,
	})})
	drive.setUploadRules(testUploadRules())
	_, err := drive.planUploadV2(context.Background(), "destination", "request-id", []FinalUploadFile{{
		UploadFile: UploadFile{FID: "client", Name: "file.ini"},
	}}, nil)
	var uploadErr *UploadV2Error
	if !errors.As(err, &uploadErr) || uploadErr.Code != "upload_denied" {
		t.Fatalf("error = %#v", err)
	}
}

func TestUploadV2APIErrorClassifiesHTTPStatusWithoutChangingJSON(t *testing.T) {
	t.Parallel()

	err := uploadV2APIError(map[string]any{
		"code":    "quota_exceeded",
		"message": "quota reached",
	}, http.StatusUnprocessableEntity)
	var uploadErr *UploadV2Error
	if !errors.As(err, &uploadErr) {
		t.Fatalf("error = %#v", err)
	}
	if got := uploadErr.DiagnosticSeverity(); got != infra.DiagnosticWarn {
		t.Fatalf("DiagnosticSeverity() = %q, want warn", got)
	}
	encoded, marshalErr := json.Marshal(uploadErr)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if got, want := string(encoded), `{"Code":"quota_exceeded","Message":"quota reached"}`; got != want {
		t.Fatalf("json = %s, want %s", got, want)
	}

	serverErr := uploadV2APIError(map[string]any{"code": "backend_failure"}, http.StatusServiceUnavailable)
	if got := infra.ClassifyError(serverErr); got != infra.DiagnosticError {
		t.Fatalf("5xx severity = %q, want error", got)
	}
}
