package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"nahida.live/desktop/internal/infra"
)

const (
	planPath   = "/feedback/uploads"
	submitPath = "/feedback"

	storageHost = "s3.ap-tokyo-1.megas4.com"
	bucketHost  = "user-feedback-attachments." + storageHost

	// planBatch is how many files one plan request may declare.
	planBatch     = 500
	uploadWorkers = 4
	// maxResponseBytes bounds how much of a backend answer is read back. A full
	// plan answer carries one signed URL per file.
	maxResponseBytes = 4 << 20

	progressInterval = 100 * time.Millisecond
)

// fileDeclaration is one attachment as both the plan and the report declare
// it. The backend derives the object key of a media file, and finds the stored
// content of a mod file, from these fields, so the two requests have to send
// them unchanged.
type fileDeclaration struct {
	Ref    string `json:"ref"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type planRequest struct {
	ReportID string            `json:"reportId"`
	Files    []fileDeclaration `json:"files"`
}

type planObject struct {
	Ref     string            `json:"ref"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type reportPayload struct {
	ReportID    string            `json:"reportId"`
	Category    string            `json:"category"`
	Source      string            `json:"source"`
	Body        string            `json:"body"`
	AppVersion  string            `json:"appVersion"`
	Platform    string            `json:"platform"`
	Locale      string            `json:"locale"`
	Environment map[string]string `json:"environment"`
	Attachments []fileDeclaration `json:"attachments"`
}

// upload plans the media files in batches and PUTs each batch before asking for
// the next, so a signed URL is used soon after it was issued.
func (s *Service) upload(ctx context.Context, reportID string, uploads []upload, progress *progress) error {
	for start := 0; start < len(uploads); start += planBatch {
		batch := uploads[start:min(start+planBatch, len(uploads))]
		objects, err := s.plan(ctx, reportID, batch)
		if err != nil {
			return err
		}

		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(uploadWorkers)
		for _, item := range batch {
			object, ok := objects[item.declaration.Ref]
			if !ok || object.URL == "" {
				return fmt.Errorf("the server did not sign an upload for %s", item.declaration.Ref)
			}
			group.Go(func() error {
				if err := s.put(groupCtx, object, item, progress); err != nil {
					return fmt.Errorf("upload %s: %w", item.source, err)
				}
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) plan(ctx context.Context, reportID string, batch []upload) (map[string]planObject, error) {
	body, err := json.Marshal(planRequest{ReportID: reportID, Files: declarations(batch)})
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	data, err := s.backend(ctx, planPath, header, body)
	if err != nil {
		return nil, err
	}

	var plan struct {
		Objects []planObject `json:"objects"`
	}
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("decode the upload plan: %w", err)
	}
	byRef := make(map[string]planObject, len(plan.Objects))
	for _, object := range plan.Objects {
		byRef[object.Ref] = object
	}
	return byRef, nil
}

// put streams one file to its signed URL. The declared size is part of the
// signature, so the body is sent with exactly that length.
func (s *Service) put(ctx context.Context, object planObject, item upload, progress *progress) error {
	if err := validateUploadURL(object.URL); err != nil {
		return err
	}
	file, err := os.Open(item.source)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	header := make(http.Header, len(object.Headers))
	for name, value := range object.Headers {
		header.Set(name, value)
	}
	sent := &countingReader{reader: io.LimitReader(file, item.declaration.Size), progress: progress}
	response, err := s.remote.Stream(ctx, object.URL, http.MethodPut, header, sent, item.declaration.Size)
	if err != nil {
		progress.add(-sent.count)
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		progress.add(-sent.count)
		return responseError(response.StatusCode, body)
	}
	return nil
}

// send posts the report itself: the JSON payload part and, when there is one,
// the log file part.
func (s *Service) send(ctx context.Context, payload reportPayload, log []byte) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("payload", string(encoded)); err != nil {
		return "", err
	}
	if len(log) > 0 {
		part, err := writer.CreateFormFile("log", "desktop.log")
		if err != nil {
			return "", err
		}
		if _, err := part.Write(log); err != nil {
			return "", err
		}
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	header := make(http.Header)
	header.Set("Content-Type", writer.FormDataContentType())
	data, err := s.backend(ctx, submitPath, header, body.Bytes())
	if err != nil {
		return "", err
	}
	var stored struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", fmt.Errorf("decode the stored report: %w", err)
	}
	if strings.TrimSpace(stored.ID) == "" {
		return "", errors.New("the server did not return a report id")
	}
	return stored.ID, nil
}

// backend posts one request to the Nahida backend and returns the body of a
// successful answer. The client adds the session token for the API host.
func (s *Service) backend(ctx context.Context, path string, header http.Header, body []byte) ([]byte, error) {
	response, err := s.remote.Fetch(ctx, strings.TrimRight(s.remote.BackendURL(), "/")+path, infra.FetchOptions{
		Method:            http.MethodPost,
		Header:            header,
		Body:              bytes.NewReader(body),
		DisableHTTPErrors: true,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read the server response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, responseError(response.StatusCode, data)
	}
	return data, nil
}

// responseError prefers the backend's envelope message, which explains a
// refusal; a bare status is the fallback.
func responseError(status int, body []byte) error {
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
		return fmt.Errorf("%s (status %d)", envelope.Error, status)
	}
	return fmt.Errorf("the server refused the request (status %d)", status)
}

// validateUploadURL keeps the file bytes from being sent anywhere but the
// feedback bucket, whatever a plan answer names.
func validateUploadURL(rawURL string) error {
	uploadURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse the upload URL: %w", err)
	}
	if !strings.EqualFold(uploadURL.Scheme, "https") || uploadURL.User != nil {
		return errors.New("the upload URL must use HTTPS")
	}
	if port := uploadURL.Port(); port != "" && port != "443" {
		return errors.New("the upload URL must use the HTTPS port")
	}
	host := strings.ToLower(strings.TrimSuffix(uploadURL.Hostname(), "."))
	if host != storageHost && host != bucketHost {
		return fmt.Errorf("upload host %q is not approved", host)
	}
	return nil
}

// progress sums the bytes sent across the upload workers and emits them at a
// bounded rate.
type progress struct {
	emit     func(name string, data ...any)
	total    int64
	uploaded atomic.Int64

	mu   sync.Mutex
	last time.Time
}

func (s *Service) newProgress(media []upload, mods []modUpload) *progress {
	state := &progress{emit: s.emit}
	for _, item := range media {
		state.total += item.declaration.Size
	}
	// Hashing reads a mod file once before it is sent, so its bytes count twice.
	for _, item := range mods {
		state.total += 2 * item.source.size
	}
	return state
}

func (p *progress) add(bytes int64) {
	p.uploaded.Add(bytes)
	p.report(false)
}

func (p *progress) report(force bool) {
	if p.emit == nil {
		return
	}
	p.mu.Lock()
	now := time.Now()
	if !force && now.Sub(p.last) < progressInterval {
		p.mu.Unlock()
		return
	}
	p.last = now
	p.mu.Unlock()
	p.emit(EventProgress, Progress{UploadedBytes: p.uploaded.Load(), TotalBytes: p.total})
}

type countingReader struct {
	reader   io.Reader
	progress *progress
	count    int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	if read > 0 {
		r.count += int64(read)
		r.progress.add(int64(read))
	}
	return read, err
}
