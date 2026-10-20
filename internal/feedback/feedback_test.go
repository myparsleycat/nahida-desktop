package feedback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
)

// testRules carries a media kind the client has no constant for, the way a
// kind the server adds later reaches it.
const testRules = `{"mediaExtensions":[` +
	`{"ext":".png","kind":"image","contentType":"image/png"},` +
	`{"ext":".zip","kind":"archive","contentType":"application/zip"}],` +
	`"maxBodyLength":100,"maxMediaFiles":20,"maxMediaBytes":1048576,` +
	`"maxModFiles":2000,"maxPlanFiles":500,"maxLogBytes":16777216}`

// backend stands in for the API and the bucket behind one transport, so the
// test sees exactly what would leave the machine.
type backend struct {
	mu      sync.Mutex
	planned []fileDeclaration
	puts    map[string]string
	payload reportPayload
	log     string
}

func (b *backend) RoundTrip(request *http.Request) (*http.Response, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var body []byte
	if request.Body != nil {
		read, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = read
	}
	switch {
	case request.URL.Path == rulesPath && request.Method == http.MethodGet:
		return answer(http.StatusOK, testRules), nil
	case request.URL.Host == bucketHost:
		if request.ContentLength != int64(len(body)) {
			return answer(http.StatusForbidden, `{}`), nil
		}
		b.puts[request.URL.Path] = request.Header.Get("Content-Type") + "|" + string(body)
		return answer(http.StatusOK, ``), nil
	case request.URL.Path == planPath:
		var plan planRequest
		if err := json.Unmarshal(body, &plan); err != nil {
			return nil, err
		}
		b.planned = append(b.planned, plan.Files...)
		objects := make([]planObject, 0, len(plan.Files))
		for _, file := range plan.Files {
			objects = append(objects, planObject{
				Ref:     file.Ref,
				URL:     "https://" + bucketHost + "/" + plan.ReportID + "/" + file.Ref + "?X-Amz-Signature=s",
				Headers: map[string]string{"Content-Type": "application/x-test"},
			})
		}
		encoded, err := json.Marshal(map[string]any{"objects": objects})
		return answer(http.StatusOK, string(encoded)), err
	case request.URL.Path == submitPath:
		_, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil {
			return nil, err
		}
		form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(form.Value["payload"][0]), &b.payload); err != nil {
			return nil, err
		}
		if files := form.File["log"]; len(files) > 0 {
			file, err := files[0].Open()
			if err != nil {
				return nil, err
			}
			log, err := io.ReadAll(file)
			if err != nil {
				return nil, err
			}
			b.log = string(log)
		}
		return answer(http.StatusOK, `{"id":"stored-id"}`), nil
	}
	return answer(http.StatusNotFound, `{"error":"not found"}`), nil
}

// mods stands in for the akasha upload intents. It stores every file but the
// refused ones.
type mods struct {
	reportID string
	files    []drive.FeedbackModFile
	refuse   map[string]bool
}

func (m *mods) UploadFeedbackMod(
	_ context.Context,
	reportID string,
	files []drive.FeedbackModFile,
	onProgress func(int64),
) (drive.FeedbackModResult, error) {
	m.reportID, m.files = reportID, files
	var result drive.FeedbackModResult
	for _, file := range files {
		if m.refuse[file.Ref] {
			result.Refused++
			continue
		}
		onProgress(file.Size)
		result.Stored = append(result.Stored, file.Ref)
	}
	return result, nil
}

func answer(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newService(t *testing.T, remote *backend, logPath string) *Service {
	t.Helper()

	return newServiceWithMods(t, remote, logPath, &mods{})
}

func newServiceWithMods(t *testing.T, remote *backend, logPath string, uploader ModUploader) *Service {
	t.Helper()

	retries := 0
	return New(Options{
		Remote: infra.NewClientWithOptions(infra.ClientOptions{
			Transport:  remote,
			BackendURL: "https://api.nahida.live",
			RetryLimit: &retries,
			Status:     infra.BackendOnline,
		}),
		LogPath: func() (string, error) { return logPath, nil },
		ModFileRule: func(context.Context) (ModFileRule, error) {
			return func(name string, _ int64) bool { return strings.HasSuffix(name, ".ini") }, nil
		},
		Mods: uploader,
	})
}

func TestSubmitUploadsWhatItDeclares(t *testing.T) {
	dir := t.TempDir()
	image := writeFile(t, filepath.Join(dir, "shot.png"), "png-bytes")
	mod := filepath.Join(dir, "My Mod")
	writeFile(t, filepath.Join(mod, "merged.ini"), "[TextureOverride]")
	writeFile(t, filepath.Join(mod, "sub", "extra.ini"), "x")
	writeFile(t, filepath.Join(mod, "tool.exe"), "denied by the rules")
	writeFile(t, filepath.Join(mod, "empty.ini"), "")
	logPath := writeFile(t, filepath.Join(dir, "desktop.log"), "2026-10-12 ERROR Authorization: Bearer secret-token\n")

	remote := &backend{puts: make(map[string]string)}
	uploader := &mods{refuse: map[string]bool{"f1": true}}
	result, err := newServiceWithMods(t, remote, logPath, uploader).Submit(t.Context(), Request{
		Category: "desktop", Body: "it broke", IncludeLog: true,
		Files: []string{image}, ModDirs: []string{mod}, Locale: "ko",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Two files the rules refuse before the upload and one the server refuses.
	if result.ID != "stored-id" || result.SkippedModFiles != 3 {
		t.Fatalf("result = %+v", result)
	}

	// Only the media go to the feedback bucket, and the report has to repeat
	// their declarations unchanged: the backend derives the object keys from
	// them and verifies the objects under those.
	if len(remote.planned) != 1 || len(remote.payload.Attachments) != 2 {
		t.Fatalf("planned = %+v, submitted = %+v", remote.planned, remote.payload.Attachments)
	}
	if remote.payload.Attachments[0] != remote.planned[0] {
		t.Errorf("media attachment = %+v, planned %+v", remote.payload.Attachments[0], remote.planned[0])
	}
	prefix := "/" + remote.payload.ReportID + "/"
	if got := remote.puts[prefix+"m0"]; got != "application/x-test|png-bytes" || len(remote.puts) != 1 {
		t.Errorf("bucket objects = %q", remote.puts)
	}

	if uploader.reportID != remote.payload.ReportID || len(uploader.files) != 2 {
		t.Fatalf("mod upload = %s %+v", uploader.reportID, uploader.files)
	}
	paths := []string{uploader.files[0].Path, uploader.files[1].Path}
	if paths[0] != "My Mod/merged.ini" || paths[1] != "My Mod/sub/extra.ini" {
		t.Errorf("mod paths = %q", paths)
	}
	// The backend finds the stored content of a mod file by its hash, so the
	// report declares the one the upload planned.
	sum := sha256.Sum256([]byte("[TextureOverride]"))
	want := fileDeclaration{
		Ref: "f0", Kind: "mod", Path: "My Mod/merged.ini", Size: 17, SHA256: hex.EncodeToString(sum[:]),
	}
	if got := remote.payload.Attachments[1]; got != want || uploader.files[0].SHA256 != want.SHA256 {
		t.Errorf("mod attachment = %+v, planned %+v, want %+v", got, uploader.files[0], want)
	}
	if remote.payload.Source != "desktop" || remote.payload.Category != "desktop" || remote.payload.Body != "it broke" {
		t.Errorf("payload = %+v", remote.payload)
	}
	if strings.Contains(remote.log, "secret-token") || !strings.Contains(remote.log, "ERROR") {
		t.Errorf("log part = %q", remote.log)
	}
}

func TestSubmitLeavesTheLogOutUnlessAsked(t *testing.T) {
	logPath := writeFile(t, filepath.Join(t.TempDir(), "desktop.log"), "line\n")
	remote := &backend{puts: make(map[string]string)}

	if _, err := newService(
		t,
		remote,
		logPath,
	).Submit(t.Context(), Request{Category: "service", Body: "text"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if remote.log != "" {
		t.Fatalf("log part = %q, want none", remote.log)
	}
}

func TestSubmitRefusesBeforeUploading(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		request Request
		code    string
	}{
		{"website category", Request{Category: "website", Body: "x"}, codeInvalidCategory},
		{"blank body", Request{Category: "desktop", Body: "  "}, codeEmptyBody},
		{"long body", Request{Category: "desktop", Body: strings.Repeat("가", 101)}, codeBodyTooLong},
		{
			"unsupported file",
			Request{Category: "desktop", Body: "x", Files: []string{writeFile(t, filepath.Join(dir, "a.exe"), "x")}},
			codeUnsupportedFile,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			remote := &backend{puts: make(map[string]string)}
			_, err := newService(t, remote, "").Submit(t.Context(), test.request)
			if err == nil || !strings.HasPrefix(err.Error(), test.code+":") {
				t.Fatalf("Submit error = %v, want code %s", err, test.code)
			}
			if len(remote.planned) != 0 || len(remote.puts) != 0 {
				t.Fatalf("a refused report reached the backend")
			}
		})
	}
}

func TestDescribeTakesTheMediaKindsFromTheServer(t *testing.T) {
	dir := t.TempDir()
	paths := []string{
		writeFile(t, filepath.Join(dir, "crash.ZIP"), "x"),
		writeFile(t, filepath.Join(dir, "notes.txt"), "x"),
	}
	described, err := newService(t, &backend{puts: make(map[string]string)}, "").Describe(t.Context(), paths)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if described[0].Kind != "archive" || described[1].Kind != KindUnsupported {
		t.Fatalf("kinds = %q, %q, want archive, unsupported", described[0].Kind, described[1].Kind)
	}
}

func TestValidateUploadURLKeepsBytesInTheFeedbackBucket(t *testing.T) {
	allowed := "https://" + bucketHost + "/report/u/r/media/a.png?X-Amz-Signature=s"
	if err := validateUploadURL(allowed); err != nil {
		t.Fatalf("validateUploadURL(%s): %v", allowed, err)
	}
	for _, refused := range []string{
		"http://" + bucketHost + "/a",
		"https://evil.example/a",
		"https://" + bucketHost + ".evil.example/a",
		"https://user@" + bucketHost + "/a",
		"https://" + bucketHost + ":8443/a",
	} {
		if err := validateUploadURL(refused); err == nil {
			t.Errorf("validateUploadURL(%s) = nil, want a refusal", refused)
		}
	}
}

func TestReadTailStartsAtALineBoundary(t *testing.T) {
	path := writeFile(t, filepath.Join(t.TempDir(), "desktop.log"), "first line\nsecond line\nthird line\n")

	got, err := readTail(path, 20)
	if err != nil {
		t.Fatalf("readTail: %v", err)
	}
	if string(got) != "third line\n" {
		t.Fatalf("tail = %q", got)
	}
}
