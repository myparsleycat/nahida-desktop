package infra

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const maxProtocolWebResponse = 64 << 20

type memoryProtocolEntry struct {
	data        []byte
	contentType string
}

type memoryUploadEntry struct {
	expected  int64
	data      []byte
	uploading bool
	ready     bool
}

type memoryProtocolSession struct {
	buffers map[string]memoryProtocolEntry
	uploads map[string]*memoryUploadEntry
}

const maxMemoryUploadBytes int64 = 512 << 20

type Protocol struct {
	mu       sync.RWMutex
	sessions map[string]*memoryProtocolSession
	http     *Client
	log      *Log
}

func NewProtocol() *Protocol {
	return &Protocol{sessions: make(map[string]*memoryProtocolSession)}
}

//wails:ignore
func (p *Protocol) Configure(httpClient *Client, log *Log) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.http, p.log = httpClient, log
	p.mu.Unlock()
}

func (p *Protocol) LocalFileURL(path string, original bool) string {
	query := url.Values{"path": []string{path}}
	if original {
		query.Set("orig", "true")
	}
	return "/protocol/local?" + query.Encode()
}

//wails:ignore
func (p *Protocol) CreateMemorySession() string {
	if p == nil {
		return ""
	}
	id := uuid.NewString()
	p.mu.Lock()
	if p.sessions == nil {
		p.sessions = make(map[string]*memoryProtocolSession)
	}
	p.sessions[id] = &memoryProtocolSession{buffers: make(map[string]memoryProtocolEntry), uploads: make(map[string]*memoryUploadEntry)}
	p.mu.Unlock()
	return id
}

//wails:ignore
func (p *Protocol) StoreMemoryBuffer(sessionID, bufferID string, data []byte, contentType string) (string, error) {
	if p == nil {
		return "", errors.New("protocol service is nil")
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(bufferID) == "" {
		return "", errors.New("memory session and buffer ids are required")
	}
	p.mu.Lock()
	session, exists := p.sessions[sessionID]
	if !exists {
		p.mu.Unlock()
		return "", fmt.Errorf("missing memory session: %s", sessionID)
	}
	session.buffers[bufferID] = memoryProtocolEntry{data: data, contentType: normalizeContentType(contentType)}
	p.mu.Unlock()
	return memoryProtocolURL(sessionID, bufferID), nil
}

//wails:ignore
func (p *Protocol) RemoveMemoryBuffer(sessionID, bufferID string) {
	if p == nil || sessionID == "" || bufferID == "" {
		return
	}
	p.mu.Lock()
	if session := p.sessions[sessionID]; session != nil {
		delete(session.buffers, bufferID)
		delete(session.uploads, bufferID)
	}
	p.mu.Unlock()
}

//wails:ignore
func (p *Protocol) CreateMemoryUpload(sessionID, uploadID string, expectedBytes int64) (string, error) {
	if p == nil {
		return "", errors.New("protocol service is nil")
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(uploadID) == "" {
		return "", errors.New("memory session and upload ids are required")
	}
	if expectedBytes < 0 || expectedBytes > maxMemoryUploadBytes {
		return "", fmt.Errorf("memory upload size must be between 0 and %d bytes", maxMemoryUploadBytes)
	}
	p.mu.Lock()
	session := p.sessions[sessionID]
	if session == nil {
		p.mu.Unlock()
		return "", fmt.Errorf("missing memory session: %s", sessionID)
	}
	if _, exists := session.uploads[uploadID]; exists {
		p.mu.Unlock()
		return "", fmt.Errorf("memory upload already exists: %s", uploadID)
	}
	session.uploads[uploadID] = &memoryUploadEntry{expected: expectedBytes}
	p.mu.Unlock()
	return memoryProtocolURL(sessionID, uploadID), nil
}

//wails:ignore
func (p *Protocol) TakeMemoryUpload(sessionID, uploadID string) ([]byte, error) {
	if p == nil {
		return nil, errors.New("protocol service is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	session := p.sessions[sessionID]
	if session == nil {
		return nil, fmt.Errorf("missing memory session: %s", sessionID)
	}
	upload := session.uploads[uploadID]
	if upload == nil || !upload.ready {
		return nil, fmt.Errorf("memory upload is not ready: %s", uploadID)
	}
	delete(session.uploads, uploadID)
	return upload.data, nil
}

//wails:ignore
func (p *Protocol) CleanupMemorySession(sessionID string) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	delete(p.sessions, sessionID)
	p.mu.Unlock()
}

func memoryProtocolURL(sessionID, entryID string) string {
	return "/protocol/memory/" + url.PathEscape(sessionID) + "/" + url.PathEscape(entryID)
}

func (p *Protocol) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if p == nil || request == nil {
		http.Error(w, "protocol service unavailable", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/")
	path = strings.TrimPrefix(path, "protocol/")
	switch {
	case path == "local":
		p.serveLocalFile(w, request, request.URL.Query().Get("path"))
	case strings.HasPrefix(path, "memory/"):
		p.serveMemory(w, request, strings.TrimPrefix(path, "memory/"))
	case path == "nahida/image-local", path == "nahida/video-local":
		p.serveLocalFile(w, request, request.URL.Query().Get("path"))
	case path == "nahida/image-web":
		p.serveWebImage(w, request)
	default:
		http.NotFound(w, request)
	}
}

func (p *Protocol) serveLocalFile(w http.ResponseWriter, request *http.Request, path string) {
	if strings.TrimSpace(path) == "" {
		http.Error(w, "path param is required", http.StatusBadRequest)
		return
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, request)
			return
		}
		p.reportProtocolFailure(err, request, "open-local-file", map[string]any{"path": path})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		p.reportProtocolFailure(err, request, "stat-local-file", map[string]any{"path": path})
		http.NotFound(w, request)
		return
	}
	contentType := contentTypeForFile(path, file)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, request, info.Name(), info.ModTime(), file)
}

func (p *Protocol) serveMemory(w http.ResponseWriter, request *http.Request, route string) {
	parts := strings.SplitN(route, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, request)
		return
	}
	sessionID, err1 := url.PathUnescape(parts[0])
	bufferID, err2 := url.PathUnescape(parts[1])
	if err1 != nil || err2 != nil {
		http.Error(w, "invalid memory buffer path", http.StatusBadRequest)
		return
	}
	if request.Method == http.MethodPut {
		p.serveMemoryUpload(w, request, sessionID, bufferID)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p.mu.RLock()
	session := p.sessions[sessionID]
	var entry memoryProtocolEntry
	var exists bool
	if session != nil {
		entry, exists = session.buffers[bufferID]
	}
	p.mu.RUnlock()
	if !exists {
		http.NotFound(w, request)
		return
	}
	contentType := entry.contentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, request, bufferID, time.Time{}, bytes.NewReader(entry.data))
}

func (p *Protocol) serveMemoryUpload(w http.ResponseWriter, request *http.Request, sessionID, uploadID string) {
	if normalizeContentType(request.Header.Get("Content-Type")) != "application/octet-stream" {
		http.Error(w, "content type must be application/octet-stream", http.StatusUnsupportedMediaType)
		return
	}
	p.mu.Lock()
	session := p.sessions[sessionID]
	if session == nil {
		p.mu.Unlock()
		http.NotFound(w, request)
		return
	}
	upload := session.uploads[uploadID]
	if upload == nil {
		p.mu.Unlock()
		http.NotFound(w, request)
		return
	}
	if upload.uploading || upload.ready {
		p.mu.Unlock()
		http.Error(w, "memory upload already used", http.StatusConflict)
		return
	}
	if request.ContentLength != upload.expected {
		delete(session.uploads, uploadID)
		p.mu.Unlock()
		http.Error(w, "content length does not match upload slot", http.StatusBadRequest)
		return
	}
	upload.uploading = true
	expected := upload.expected
	p.mu.Unlock()

	data, err := io.ReadAll(io.LimitReader(request.Body, expected+1))
	if err != nil || int64(len(data)) != expected {
		failure := err
		if failure == nil {
			failure = errors.New("memory upload length mismatch")
		}
		p.reportProtocolFailure(failure, request, "read-memory-upload", map[string]any{"expectedBytes": expected, "receivedBytes": len(data)})
		p.mu.Lock()
		if current := p.sessions[sessionID]; current != nil && current.uploads[uploadID] == upload {
			delete(current.uploads, uploadID)
		}
		p.mu.Unlock()
		http.Error(w, "upload body length does not match upload slot", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	current := p.sessions[sessionID]
	if current == nil || current.uploads[uploadID] != upload {
		p.mu.Unlock()
		http.NotFound(w, request)
		return
	}
	upload.data, upload.uploading, upload.ready = data, false, true
	p.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (p *Protocol) serveWebImage(w http.ResponseWriter, request *http.Request) {
	rawURL := request.URL.Query().Get("url")
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		http.Error(w, "url param must use HTTP or HTTPS", http.StatusBadRequest)
		return
	}
	p.mu.RLock()
	client := p.http
	p.mu.RUnlock()
	if client == nil {
		p.reportProtocolFailure(errors.New("http service unavailable"), request, "prepare-web-image", nil)
		http.Error(w, "http service unavailable", http.StatusServiceUnavailable)
		return
	}
	response, err := client.Fetch(request.Context(), rawURL, FetchOptions{DisableHTTPErrors: true})
	if err != nil {
		p.reportProtocolFailure(err, request, "fetch-web-image", map[string]any{"endpoint": SanitizeLogURL(rawURL)})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		p.reportProtocolFailure(&HTTPError{Status: response.StatusCode}, request, "web-image-response", map[string]any{"endpoint": SanitizeLogURL(rawURL), "status": response.StatusCode})
		http.Error(w, "upstream image error", response.StatusCode)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxProtocolWebResponse+1))
	if err != nil {
		p.reportProtocolFailure(err, request, "read-web-image", map[string]any{"endpoint": SanitizeLogURL(rawURL), "status": response.StatusCode})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if len(raw) > maxProtocolWebResponse {
		p.reportProtocolFailure(errors.New("upstream image is too large"), request, "validate-web-image-size", map[string]any{"endpoint": SanitizeLogURL(rawURL), "status": response.StatusCode, "limitBytes": maxProtocolWebResponse})
		http.Error(w, "upstream image is too large", http.StatusRequestEntityTooLarge)
		return
	}
	contentType := normalizeContentType(response.Header.Get("Content-Type"))
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		contentType = http.DetectContentType(raw)
	}
	if !strings.HasPrefix(contentType, "image/") {
		p.reportProtocolFailure(errors.New("upstream response is not an image"), request, "validate-web-image-type", map[string]any{"endpoint": SanitizeLogURL(rawURL), "status": response.StatusCode, "contentType": contentType})
		http.Error(w, "upstream response is not an image", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
	_, _ = w.Write(raw)
}

func (p *Protocol) reportProtocolFailure(err error, request *http.Request, stage string, fields map[string]any) {
	p.mu.RLock()
	log := p.log
	p.mu.RUnlock()
	if fields == nil {
		fields = make(map[string]any)
	}
	fields["method"] = request.Method
	_ = ReportError(log, err, "Protocol", Diagnostic{Operation: "serve-resource", Stage: stage, Fields: fields})
}

func contentTypeForFile(path string, file *os.File) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".glb":
		return "model/gltf-binary"
	case ".gltf":
		return "model/gltf+json"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	}
	if resolved := mime.TypeByExtension(ext); resolved != "" {
		return resolved
	}
	buffer := make([]byte, 512)
	read, _ := file.ReadAt(buffer, 0)
	return http.DetectContentType(buffer[:read])
}

func normalizeContentType(value string) string {
	value = strings.TrimSpace(strings.Split(value, ";")[0])
	if value == "" {
		return ""
	}
	if _, _, err := mime.ParseMediaType(value); err != nil {
		return ""
	}
	return strings.ToLower(value)
}

func (p *Protocol) ServiceShutdown() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	clear(p.sessions)
	p.mu.Unlock()
	return nil
}
