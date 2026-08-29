package infra

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	session := service.CreateModelViewerMemorySession()
	bufferURL, err := service.WriteModelViewerMemoryBuffer(session, "mesh data.bin", []byte("mesh"), "model/gltf-binary")
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

	service.CleanupModelViewerMemorySession(session)
	recorder = httptest.NewRecorder()
	service.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, bufferURL, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("after cleanup status = %d", recorder.Code)
	}
	if _, err := service.WriteModelViewerMemoryBuffer(session, "missing", nil, ""); err == nil {
		t.Fatal("write to cleaned session succeeded")
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
