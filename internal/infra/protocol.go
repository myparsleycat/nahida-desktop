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

type Protocol struct {
	mu       sync.RWMutex
	sessions map[string]map[string]memoryProtocolEntry
	http     *Client
	log      *Log
}

func NewProtocol() *Protocol {
	return &Protocol{sessions: make(map[string]map[string]memoryProtocolEntry)}
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

func (p *Protocol) CreateModelViewerMemorySession() string {
	if p == nil {
		return ""
	}
	id := uuid.NewString()
	p.mu.Lock()
	if p.sessions == nil {
		p.sessions = make(map[string]map[string]memoryProtocolEntry)
	}
	p.sessions[id] = make(map[string]memoryProtocolEntry)
	p.mu.Unlock()
	return id
}

func (p *Protocol) WriteModelViewerMemoryBuffer(sessionID, bufferID string, data []byte, contentType string) (string, error) {
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
		return "", fmt.Errorf("missing model viewer memory session: %s", sessionID)
	}
	session[bufferID] = memoryProtocolEntry{data: bytes.Clone(data), contentType: normalizeContentType(contentType)}
	p.mu.Unlock()
	return "/protocol/model-viewer-memory/" + url.PathEscape(sessionID) + "/" + url.PathEscape(bufferID), nil
}

func (p *Protocol) CleanupModelViewerMemorySession(sessionID string) {
	if p == nil || sessionID == "" {
		return
	}
	p.mu.Lock()
	delete(p.sessions, sessionID)
	p.mu.Unlock()
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
	case strings.HasPrefix(path, "model-viewer-memory/"):
		p.serveMemory(w, request, strings.TrimPrefix(path, "model-viewer-memory/"))
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
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
	p.mu.RLock()
	entry, exists := p.sessions[sessionID][bufferID]
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
		http.Error(w, "http service unavailable", http.StatusServiceUnavailable)
		return
	}
	response, err := client.Fetch(request.Context(), rawURL, FetchOptions{DisableHTTPErrors: true})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		http.Error(w, "upstream image error", response.StatusCode)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxProtocolWebResponse+1))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if len(raw) > maxProtocolWebResponse {
		http.Error(w, "upstream image is too large", http.StatusRequestEntityTooLarge)
		return
	}
	contentType := normalizeContentType(response.Header.Get("Content-Type"))
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		contentType = http.DetectContentType(raw)
	}
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "upstream response is not an image", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
	_, _ = w.Write(raw)
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
