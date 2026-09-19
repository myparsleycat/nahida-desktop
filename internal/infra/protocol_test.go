package infra

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtocolServesLocalFileAndRange(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	service := NewProtocol()
	request := httptest.NewRequest(http.MethodGet, service.LocalFileURL(path, true), nil)
	request.Header.Set("Range", "bytes=2-5")
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, request)
	response := recorder.Result()
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if response.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("range = %d %q", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestProtocolMemorySessionLifecycle(t *testing.T) {
	t.Parallel()
	service := NewProtocol()
	session := service.CreateMemorySession()
	bufferURL, err := service.StoreMemoryBuffer(session, "mesh data.bin", []byte("mesh"), "model/gltf-binary")
	if err != nil {
		t.Fatalf("WriteModelViewerMemoryBuffer: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, bufferURL, nil)
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "mesh" {
		t.Fatalf("memory response = %d %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "model/gltf-binary" {
		t.Fatalf("Content-Type = %q", got)
	}

	service.CleanupMemorySession(session)
	recorder = httptest.NewRecorder()
	service.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, bufferURL, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("after cleanup status = %d", recorder.Code)
	}
	if _, err := service.StoreMemoryBuffer(session, "missing", nil, ""); err == nil {
		t.Fatal("write to cleaned session succeeded")
	}
}

func TestProtocolMemoryUploadLifecycle(t *testing.T) {
	t.Parallel()
	service := NewProtocol()
	session := service.CreateMemorySession()
	uploadURL, err := service.CreateMemoryUpload(session, "positions", 4)
	if err != nil {
		t.Fatalf("CreateMemoryUpload: %v", err)
	}
	request := httptest.NewRequest(http.MethodPut, uploadURL, strings.NewReader("mesh"))
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d: %s", recorder.Code, recorder.Body.String())
	}
	data, err := service.TakeMemoryUpload(session, "positions")
	if err != nil || string(data) != "mesh" {
		t.Fatalf("TakeMemoryUpload = %q, %v", data, err)
	}
	if _, err := service.TakeMemoryUpload(session, "positions"); err == nil {
		t.Fatal("upload was consumed twice")
	}
}

func TestProtocolMemoryDownloadSupportsHeadAndRange(t *testing.T) {
	t.Parallel()
	service := NewProtocol()
	session := service.CreateMemorySession()
	bufferURL, err := service.StoreMemoryBuffer(session, "numbers", []byte("0123456789"), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	head := httptest.NewRecorder()
	service.ServeHTTP(head, httptest.NewRequest(http.MethodHead, bufferURL, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "10" {
		t.Fatalf("HEAD = %d len=%d headers=%v", head.Code, head.Body.Len(), head.Header())
	}
	rangeRequest := httptest.NewRequest(http.MethodGet, bufferURL, nil)
	rangeRequest.Header.Set("Range", "bytes=3-6")
	ranged := httptest.NewRecorder()
	service.ServeHTTP(ranged, rangeRequest)
	if ranged.Code != http.StatusPartialContent || ranged.Body.String() != "3456" {
		t.Fatalf("range = %d %q", ranged.Code, ranged.Body.String())
	}
	if ranged.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", ranged.Header().Get("Cache-Control"))
	}
}

func TestProtocolRejectsInvalidMemoryUploads(t *testing.T) {
	t.Parallel()
	service := NewProtocol()
	session := service.CreateMemorySession()

	wrongTypeURL, err := service.CreateMemoryUpload(session, "wrong-type", 4)
	if err != nil {
		t.Fatal(err)
	}
	wrongType := httptest.NewRecorder()
	service.ServeHTTP(wrongType, httptest.NewRequest(http.MethodPut, wrongTypeURL, strings.NewReader("mesh")))
	if wrongType.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("wrong content type = %d", wrongType.Code)
	}

	shortURL, err := service.CreateMemoryUpload(session, "short", 5)
	if err != nil {
		t.Fatal(err)
	}
	shortRequest := httptest.NewRequest(http.MethodPut, shortURL, strings.NewReader("tiny"))
	shortRequest.Header.Set("Content-Type", "application/octet-stream")
	short := httptest.NewRecorder()
	service.ServeHTTP(short, shortRequest)
	if short.Code != http.StatusNoContent {
		t.Fatalf("short upload = %d", short.Code)
	}
	if _, err = service.TakeMemoryUpload(session, "short"); err == nil {
		t.Fatal("short upload remained consumable")
	}

	oversizeURL, err := service.CreateMemoryUpload(session, "oversize", 4)
	if err != nil {
		t.Fatal(err)
	}
	oversizeRequest := httptest.NewRequest(http.MethodPut, oversizeURL, strings.NewReader("toolong"))
	oversizeRequest.Header.Set("Content-Type", "application/octet-stream")
	oversize := httptest.NewRecorder()
	service.ServeHTTP(oversize, oversizeRequest)
	if oversize.Code != http.StatusBadRequest {
		t.Fatalf("oversize upload = %d", oversize.Code)
	}
	if _, err = service.TakeMemoryUpload(session, "oversize"); err == nil {
		t.Fatal("oversize upload remained consumable")
	}

	duplicateURL, err := service.CreateMemoryUpload(session, "duplicate", 4)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		request := httptest.NewRequest(http.MethodPut, duplicateURL, strings.NewReader("mesh"))
		request.Header.Set("Content-Type", "application/octet-stream")
		recorder := httptest.NewRecorder()
		service.ServeHTTP(recorder, request)
		want := http.StatusNoContent
		if attempt == 1 {
			want = http.StatusConflict
		}
		if recorder.Code != want {
			t.Fatalf("duplicate attempt %d = %d, want %d", attempt, recorder.Code, want)
		}
	}

	wrongMethod := httptest.NewRecorder()
	service.ServeHTTP(wrongMethod, httptest.NewRequest(http.MethodPost, duplicateURL, nil))
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method = %d", wrongMethod.Code)
	}

	service.CleanupMemorySession(session)
	afterCleanup := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, duplicateURL, strings.NewReader("mesh"))
	request.Header.Set("Content-Type", "application/octet-stream")
	service.ServeHTTP(afterCleanup, request)
	if afterCleanup.Code != http.StatusNotFound {
		t.Fatalf("after cleanup = %d", afterCleanup.Code)
	}
}

func TestProtocolMemoryUploadAcceptsMissingContentLengthAndChunks(t *testing.T) {
	t.Parallel()
	service := NewProtocol()
	session := service.CreateMemorySession()
	uploadURL, err := service.CreateMemoryUpload(session, "positions", 8)
	if err != nil {
		t.Fatal(err)
	}

	first := httptest.NewRequest(http.MethodPut, uploadURL, strings.NewReader("mesh"))
	first.Header.Set("Content-Type", "application/octet-stream")
	first.ContentLength = -1
	first.Header.Del("Content-Length")
	firstRecorder := httptest.NewRecorder()
	service.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusNoContent {
		t.Fatalf("first chunk = %d %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	if _, err := service.TakeMemoryUpload(session, "positions"); err == nil {
		t.Fatal("partial upload was consumable")
	}

	second := httptest.NewRequest(http.MethodPut, uploadURL, strings.NewReader("data"))
	second.Header.Set("Content-Type", "application/octet-stream")
	second.ContentLength = -1
	second.Header.Del("Content-Length")
	secondRecorder := httptest.NewRecorder()
	service.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusNoContent {
		t.Fatalf("second chunk = %d %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	got, err := service.TakeMemoryUpload(session, "positions")
	if err != nil || string(got) != "meshdata" {
		t.Fatalf("TakeMemoryUpload = %q, %v", got, err)
	}

	overflowURL, err := service.CreateMemoryUpload(session, "overflow", 8)
	if err != nil {
		t.Fatal(err)
	}
	overflow := httptest.NewRequest(http.MethodPut, overflowURL, strings.NewReader("meshdata!"))
	overflow.Header.Set("Content-Type", "application/octet-stream")
	overflow.ContentLength = -1
	overflow.Header.Del("Content-Length")
	overflowRecorder := httptest.NewRecorder()
	service.ServeHTTP(overflowRecorder, overflow)
	if overflowRecorder.Code != http.StatusBadRequest {
		t.Fatalf("overflow chunk = %d %s", overflowRecorder.Code, overflowRecorder.Body.String())
	}
	if _, err := service.TakeMemoryUpload(session, "overflow"); err == nil {
		t.Fatal("overflow upload remained consumable")
	}
}

func TestProtocolRejectsOversizedMemoryUploadSlot(t *testing.T) {
	t.Parallel()
	service := NewProtocol()
	session := service.CreateMemorySession()
	if _, err := service.CreateMemoryUpload(session, "too-large", maxMemoryUploadBytes+1); err == nil {
		t.Fatal("oversized upload slot succeeded")
	}
}

func TestProtocolRejectsNonImageWebResponse(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html></html>")
	}))
	defer upstream.Close()
	client := NewClientWithOptions(ClientOptions{HTTPClient: upstream.Client()})
	service := NewProtocol()
	service.Configure(client, nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/nahida/image-web?url="+upstream.URL, nil)
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestProtocolUnconfiguredWebImagePreservesUnavailableResponse(t *testing.T) {
	var output bytes.Buffer
	service := NewProtocol()
	service.Configure(nil, NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/nahida/image-web?url=https://example.com/image.png", nil)
	service.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable ||
		strings.TrimSpace(recorder.Body.String()) != "http service unavailable" {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{"http service unavailable", "prepare-web-image", `"method":"GET"`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
}

func TestProtocolSniffsWebImageContentWhenTheHeaderIsGeneric(t *testing.T) {
	t.Parallel()
	content := isoBMFFContent("avif", "avif", "mif1")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(content)
	}))
	defer upstream.Close()
	service := NewProtocol()
	service.Configure(NewClientWithOptions(ClientOptions{HTTPClient: upstream.Client()}), nil)

	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nahida/image-web?url="+upstream.URL, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/avif" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestProtocolRejectsScriptableWebImageContent(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`)
	}))
	defer upstream.Close()
	service := NewProtocol()
	service.Configure(NewClientWithOptions(ClientOptions{HTTPClient: upstream.Client()}), nil)

	recorder := httptest.NewRecorder()
	service.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nahida/image-web?url="+upstream.URL, nil))
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestContentTypeForFilePrefersContentOverExtension(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fileName string
		content  []byte
		want     string
	}{
		{
			name:     "avif download without a descriptive name",
			fileName: "preview.bin",
			content:  isoBMFFContent("avif", "avif", "mif1"),
			want:     "image/avif",
		},
		{
			name:     "mp4 whose brands omit the mp4 marker",
			fileName: "clip.bin",
			content:  isoBMFFContent("isom", "isom"),
			want:     "video/mp4",
		},
		{
			name:     "text keeps the extension mapping",
			fileName: "notes.txt",
			content:  []byte("0123456789"),
			want:     "text/plain; charset=utf-8",
		},
		{
			name:     "matroska keeps the WebView2 type when content is unrecognized",
			fileName: "movie.mkv",
			content:  []byte("not a container"),
			want:     "video/x-matroska",
		},
		{
			name:     "detected matroska is normalized to the WebView2 type",
			fileName: "movie.mkv",
			content:  ebmlContent("matroska"),
			want:     "video/x-matroska",
		},
		{
			name:     "detected webm keeps the pinned type",
			fileName: "clip.webm",
			content:  ebmlContent("webm"),
			want:     "video/webm",
		},
		{
			name:     "detected avi keeps the pinned type",
			fileName: "clip.avi",
			content:  aviContent(),
			want:     "video/x-msvideo",
		},
		{
			name:     "glb keeps the model viewer type",
			fileName: "mesh.glb",
			content:  glbContent(),
			want:     "model/gltf-binary",
		},
		{
			name:     "gltf keeps the model viewer type",
			fileName: "model.gltf",
			content:  []byte(`{"asset":{"version":"2.0"}}`),
			want:     "model/gltf+json",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := contentTypeOfFile(t, test.fileName, test.content); got != test.want {
				t.Fatalf("contentTypeForFile = %q, want %q", got, test.want)
			}
		})
	}
}

func contentTypeOfFile(t *testing.T, fileName string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), fileName)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	return contentTypeForFile(path, file)
}
