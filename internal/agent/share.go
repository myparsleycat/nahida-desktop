package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// The two routes of the training contribution: the plan hands out the presigned
// PUT targets of a session's images, and the submit stores the transcript after
// the bytes are in the bucket.
const (
	trainingPlanPath    = "/desktop/agent-conversations/uploads"
	trainingSubmitPath  = "/desktop/agent-conversations"
	trainingStorageHost = "s3.ap-tokyo-1.megas4.com"
	trainingBucketHost  = "nahida-agent-usercontents." + trainingStorageHost

	// maxTrainingResponseBytes bounds how much of a backend answer is read back.
	maxTrainingResponseBytes = 1 << 20
)

// ShareSessionResult reports what one contribution stored.
type ShareSessionResult struct {
	ID       string `json:"id"`
	Messages int    `json:"messages"`
	Images   int    `json:"images"`
}

// trainingEntry is one event of a contributed transcript. It mirrors the
// desktop agent's durable events rather than the renderer projection: tool
// starts keep their arguments and tool ends their result, so the training
// dataset sees the call and its outcome.
type trainingEntry struct {
	Sequence     int64              `json:"sequence"`
	TurnID       string             `json:"turnId"`
	Type         string             `json:"type"`
	Role         string             `json:"role,omitempty"`
	Text         string             `json:"text,omitempty"`
	Reasoning    string             `json:"reasoning,omitempty"`
	ToolName     string             `json:"toolName,omitempty"`
	ToolCallID   string             `json:"toolCallId,omitempty"`
	Arguments    json.RawMessage    `json:"arguments,omitempty"`
	Result       json.RawMessage    `json:"result,omitempty"`
	ChangedFiles []string           `json:"changedFiles,omitempty"`
	Error        string             `json:"error,omitempty"`
	Images       []trainingImageRef `json:"images,omitempty"`
	CreatedAt    string             `json:"createdAt"`
}

// trainingImageRef is a transcript's reference to one uploaded image. The bytes
// travel through the presigned PUT plan, never through this document.
type trainingImageRef struct {
	Ref      string `json:"ref"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType"`
	Bytes    int    `json:"bytes"`
}

// trainingDocument is the transcript as the backend stores it.
type trainingDocument struct {
	ClientSessionID string          `json:"clientSessionId"`
	Title           string          `json:"title"`
	Scope           AgentScope      `json:"scope"`
	CreatedAt       string          `json:"createdAt"`
	UpdatedAt       string          `json:"updatedAt"`
	Provider        string          `json:"provider"`
	Model           string          `json:"model"`
	AppVersion      string          `json:"appVersion"`
	Platform        string          `json:"platform"`
	Locale          string          `json:"locale"`
	MessageCount    int             `json:"messageCount"`
	Entries         []trainingEntry `json:"entries"`
}

// trainingUpload is one image read from the app data directory and waiting for
// its presigned PUT.
type trainingUpload struct {
	ref      string
	name     string
	mimeType string
	data     []byte
}

type trainingPlanRequest struct {
	ClientSessionID string              `json:"clientSessionId"`
	Images          []trainingPlanImage `json:"images"`
}

type trainingPlanImage struct {
	Ref         string `json:"ref"`
	Name        string `json:"name,omitempty"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

type trainingPlanResponse struct {
	Prefix  string               `json:"prefix"`
	Objects []trainingPlanObject `json:"objects"`
}

type trainingPlanObject struct {
	Ref         string            `json:"ref"`
	Key         string            `json:"key"`
	URL         string            `json:"url"`
	ContentType string            `json:"contentType"`
	Size        int64             `json:"size"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type trainingSubmitRequest struct {
	Document trainingDocument    `json:"document"`
	Images   []trainingImageMeta `json:"images"`
}

type trainingImageMeta struct {
	Ref      string `json:"ref"`
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

// SubmitSessionForTraining sends one agent conversation to the Nahida backend as
// training data. The caller has already told the user what is sent and taken
// their consent; this method only performs the transfer.
//
// The transcript keeps the tool calls and their results, but images are
// uploaded separately through presigned PUTs so the bucket credentials never
// leave the server. A conversation with no images still submits its text.
func (s *Service) SubmitSessionForTraining(ctx context.Context, sessionID string) (ShareSessionResult, error) {
	if s.remote == nil {
		return ShareSessionResult{}, errors.New("the Nahida backend is not configured")
	}
	row, events, settings, err := s.trainingSnapshot(ctx, sessionID)
	if err != nil {
		return ShareSessionResult{}, err
	}

	entries, uploads, err := s.trainingEntries(events)
	if err != nil {
		return ShareSessionResult{}, err
	}
	if len(entries) == 0 {
		return ShareSessionResult{}, errors.New("this conversation has nothing to share yet")
	}

	objects, err := s.planTrainingImages(ctx, sessionID, uploads)
	if err != nil {
		return ShareSessionResult{}, err
	}
	if err := s.uploadTrainingImages(ctx, objects, uploads); err != nil {
		return ShareSessionResult{}, err
	}

	document := trainingDocument{
		ClientSessionID: row.ID,
		Title:           row.Title,
		Scope:           rowScope(row),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Provider:        settings.Provider,
		Model:           settings.Model,
		AppVersion:      platform.AppVersion,
		Platform:        "windows",
		Locale:          platform.SystemLocale(),
		MessageCount:    countTrainingMessages(entries),
		Entries:         entries,
	}

	images := make([]trainingImageMeta, 0, len(uploads))
	for _, upload := range uploads {
		images = append(images, trainingImageMeta{
			Ref: upload.ref, Name: upload.name, MimeType: upload.mimeType, Size: int64(len(upload.data)),
		})
	}
	id, err := s.submitTrainingConversation(ctx, document, images)
	if err != nil {
		return ShareSessionResult{}, err
	}
	return ShareSessionResult{ID: id, Messages: document.MessageCount, Images: len(images)}, nil
}

// trainingSnapshot reads the session and its events while the same per-session lock excludes
// revert mutations and run lifecycle transitions. Once a run is inactive, it cannot append more
// events, so the returned rows remain one coherent contribution snapshot after the lock is released.
func (s *Service) trainingSnapshot(
	ctx context.Context,
	sessionID string,
) (db.AgentSessionRow, []db.AgentEventRow, AgentSettingsView, error) {
	lock := s.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	client, err := s.dbClient()
	if err != nil {
		return db.AgentSessionRow{}, nil, AgentSettingsView{}, err
	}
	row, err := client.AgentSessions.Get(ctx, sessionID)
	if err != nil {
		return db.AgentSessionRow{}, nil, AgentSettingsView{}, err
	}
	if row == nil {
		return db.AgentSessionRow{}, nil, AgentSettingsView{}, errors.New("agent session not found")
	}
	events, err := client.AgentEvents.List(ctx, sessionID)
	if err != nil {
		return db.AgentSessionRow{}, nil, AgentSettingsView{}, err
	}
	if s.sessionSummary(*row).Running || hasUnfinishedRun(events) {
		return db.AgentSessionRow{}, nil, AgentSettingsView{}, errors.New("the agent is still responding to this chat")
	}
	if strings.TrimSpace(row.Revert) != "" {
		return db.AgentSessionRow{}, nil, AgentSettingsView{}, errors.New(
			"finish or discard the staged revert before sharing this conversation",
		)
	}
	settings, err := readSettings(ctx, client, s.crypto)
	if err != nil {
		return db.AgentSessionRow{}, nil, AgentSettingsView{}, err
	}
	return *row, events, settings, nil
}

func hasUnfinishedRun(events []db.AgentEventRow) bool {
	unfinished := make(map[string]struct{})
	for _, event := range events {
		switch event.EventType {
		case "turn/start":
			unfinished[event.TurnID] = struct{}{}
		case "turn/end", "turn/error", "turn/cancelled", "turn/interrupted", "turn/awaiting-approval":
			delete(unfinished, event.TurnID)
		}
	}
	return len(unfinished) > 0
}

// trainingEntries folds the durable events into the transcript and collects the images it references.
func (s *Service) trainingEntries(events []db.AgentEventRow) ([]trainingEntry, []trainingUpload, error) {
	refByPath := make(map[string]string)
	uploads := make([]trainingUpload, 0)
	entries := make([]trainingEntry, 0, len(events))
	for _, event := range events {
		entry, paths, ok := trainingEntryFromEvent(event)
		if !ok {
			continue
		}
		for _, image := range paths {
			ref, ok, err := s.trainingImageRef(image, refByPath, &uploads)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				entry.Images = append(entry.Images, ref)
			}
		}
		entries = append(entries, entry)
	}
	return entries, uploads, nil
}

// trainingImageRef reads one referenced image once and returns its transcript
// reference. A file that cannot be read is reported and skipped rather than
// failing the whole contribution.
func (s *Service) trainingImageRef(
	image AgentImage,
	refByPath map[string]string,
	uploads *[]trainingUpload,
) (trainingImageRef, bool, error) {
	if strings.TrimSpace(image.Path) == "" {
		return trainingImageRef{}, false, nil
	}
	if ref, ok := refByPath[image.Path]; ok {
		return trainingImageRef{Ref: ref, Name: image.Name, MimeType: image.MIMEType, Bytes: image.Bytes}, true, nil
	}
	if s.appData == nil {
		return trainingImageRef{}, false, nil
	}
	data, err := s.appData.ReadFile(image.Path)
	if err != nil {
		_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "agent-training", Stage: "read-image",
			Fields: map[string]any{"path": image.Path, "mimeType": image.MIMEType},
		})
		return trainingImageRef{}, false, nil
	}
	ref := fmt.Sprintf("img-%d", len(*uploads))
	refByPath[image.Path] = ref
	*uploads = append(*uploads, trainingUpload{
		ref: ref, name: image.Name, mimeType: image.MIMEType, data: data,
	})
	return trainingImageRef{Ref: ref, Name: image.Name, MimeType: image.MIMEType, Bytes: len(data)}, true, nil
}

// trainingEntryFromEvent translates one durable event. It returns the images the
// event references so the caller can collect them.
func trainingEntryFromEvent(event db.AgentEventRow) (trainingEntry, []AgentImage, bool) {
	entry := trainingEntry{
		Sequence:  event.Sequence,
		TurnID:    event.TurnID,
		Type:      event.EventType,
		CreatedAt: event.CreatedAt,
	}
	var payload struct {
		Text         string          `json:"text"`
		Reasoning    string          `json:"reasoning"`
		Images       []AgentImage    `json:"images"`
		ToolName     string          `json:"toolName"`
		ToolCallID   string          `json:"toolCallId"`
		Arguments    json.RawMessage `json:"arguments"`
		Result       json.RawMessage `json:"result"`
		ChangedFiles []string        `json:"changedFiles"`
		Error        string          `json:"error"`
	}
	_ = json.Unmarshal([]byte(event.Payload), &payload)

	switch event.EventType {
	case "turn/start":
		entry.Role, entry.Text = "user", payload.Text
		return entry, payload.Images, true
	case "message/assistant":
		entry.Role, entry.Text, entry.Reasoning = "assistant", payload.Text, payload.Reasoning
		return entry, nil, true
	case "tool/start":
		var call ToolCall
		_ = json.Unmarshal([]byte(event.Payload), &call)
		entry.ToolName, entry.ToolCallID, entry.Arguments = call.Name, call.ID, call.Arguments
		return entry, nil, true
	case "tool/end":
		entry.ToolName, entry.ToolCallID, entry.Result = payload.ToolName, payload.ToolCallID, payload.Result
		entry.ChangedFiles, entry.Error = payload.ChangedFiles, payload.Error
		return entry, payload.Images, true
	case "turn/error", "turn/cancelled", "turn/interrupted":
		entry.Error = payload.Error
		return entry, nil, true
	default:
		return trainingEntry{}, nil, false
	}
}

func countTrainingMessages(entries []trainingEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Role == "user" || entry.Role == "assistant" {
			count++
		}
	}
	return count
}

// planTrainingImages asks the backend for the presigned PUT targets. An empty
// image list skips the round trip: the text alone is a valid contribution.
func (s *Service) planTrainingImages(
	ctx context.Context,
	sessionID string,
	uploads []trainingUpload,
) (map[string]trainingPlanObject, error) {
	if len(uploads) == 0 {
		return map[string]trainingPlanObject{}, nil
	}
	request := trainingPlanRequest{ClientSessionID: sessionID, Images: make([]trainingPlanImage, 0, len(uploads))}
	for _, upload := range uploads {
		request.Images = append(request.Images, trainingPlanImage{
			Ref: upload.ref, Name: upload.name, ContentType: upload.mimeType, Size: int64(len(upload.data)),
		})
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	response, err := s.backendRequest(ctx, http.MethodPost, trainingPlanPath, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	data, status, err := readTrainingResponse(response)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, trainingError(status, data)
	}

	var plan trainingPlanResponse
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("decode the upload plan: %w", err)
	}
	byRef := make(map[string]trainingPlanObject, len(plan.Objects))
	for _, object := range plan.Objects {
		byRef[object.Ref] = object
	}
	return byRef, nil
}

// uploadTrainingImages PUTs the image bytes to the URLs the plan signed.
func (s *Service) uploadTrainingImages(
	ctx context.Context,
	objects map[string]trainingPlanObject,
	uploads []trainingUpload,
) error {
	for _, upload := range uploads {
		object, ok := objects[upload.ref]
		if !ok || object.URL == "" {
			return fmt.Errorf("the server did not sign an upload for %s", upload.ref)
		}
		header := make(http.Header, len(object.Headers))
		for name, value := range object.Headers {
			header.Set(name, value)
		}
		if err := s.uploadTrainingImage(ctx, object.URL, header, upload.data); err != nil {
			return fmt.Errorf("upload image %s: %w", upload.ref, err)
		}
	}
	return nil
}

// uploadTrainingImage performs one presigned PUT and closes its response.
func (s *Service) uploadTrainingImage(ctx context.Context, rawURL string, header http.Header, data []byte) error {
	uploadURL, err := validateTrainingUploadURL(rawURL)
	if err != nil {
		return err
	}

	uploadClient := *s.remote.HTTPClient()
	sharedRedirectPolicy := uploadClient.CheckRedirect
	uploadClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		redirectURL, redirectErr := validateTrainingUploadURL(request.URL.String())
		if redirectErr != nil {
			return fmt.Errorf("reject training upload redirect: %w", redirectErr)
		}
		if !strings.EqualFold(redirectURL.Hostname(), uploadURL.Hostname()) {
			return errors.New("training upload redirects cannot change hosts")
		}
		if sharedRedirectPolicy != nil {
			return sharedRedirectPolicy(request, via)
		}
		return nil
	}

	retryLimit := 0
	response, err := s.remote.Fetch(ctx, uploadURL.String(), infra.FetchOptions{
		Method:            http.MethodPut,
		Header:            header,
		Body:              bytes.NewReader(data),
		DisableHTTPErrors: true,
		HTTPClient:        &uploadClient,
		RetryLimit:        &retryLimit,
	})
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body, status, err := readTrainingResponse(response)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return trainingError(status, body)
	}
	return nil
}

func validateTrainingUploadURL(rawURL string) (*url.URL, error) {
	uploadURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse the training upload URL: %w", err)
	}
	if !strings.EqualFold(uploadURL.Scheme, "https") {
		return nil, errors.New("the training upload URL must use HTTPS")
	}
	if uploadURL.Opaque != "" || uploadURL.Host == "" || uploadURL.User != nil || uploadURL.Fragment != "" {
		return nil, errors.New("the training upload URL is invalid")
	}
	if port := uploadURL.Port(); port != "" && port != "443" {
		return nil, errors.New("the training upload URL must use the HTTPS port")
	}

	host := strings.ToLower(strings.TrimSuffix(uploadURL.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, errors.New("the training upload URL cannot target a local host")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()) {
		return nil, errors.New("the training upload URL cannot target a private address")
	}
	if host != trainingStorageHost && host != trainingBucketHost {
		return nil, fmt.Errorf("training upload host %q is not approved", host)
	}
	return uploadURL, nil
}

func (s *Service) submitTrainingConversation(
	ctx context.Context,
	document trainingDocument,
	images []trainingImageMeta,
) (string, error) {
	body, err := json.Marshal(trainingSubmitRequest{Document: document, Images: images})
	if err != nil {
		return "", err
	}
	response, err := s.backendRequest(ctx, http.MethodPost, trainingSubmitPath, body, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	data, status, err := readTrainingResponse(response)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", trainingError(status, data)
	}

	var stored struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return "", fmt.Errorf("decode the stored conversation: %w", err)
	}
	if strings.TrimSpace(stored.ID) == "" {
		return "", errors.New("the server did not return a stored conversation id")
	}
	return stored.ID, nil
}

// backendRequest sends one JSON request to the Nahida backend. The client adds
// the session token for the API host, so the caller only builds the body.
func (s *Service) backendRequest(
	ctx context.Context,
	method, path string,
	body []byte,
	header http.Header,
) (*http.Response, error) {
	if header == nil {
		header = make(http.Header)
	}
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json")
	}
	rawURL := strings.TrimRight(s.remote.BackendURL(), "/") + path
	return s.remote.Fetch(ctx, rawURL, infra.FetchOptions{
		Method:            method,
		Header:            header,
		Body:              bytes.NewReader(body),
		DisableHTTPErrors: true,
	})
}

// readTrainingResponse reads a response's bounded body. The caller closes the
// body.
func readTrainingResponse(response *http.Response) ([]byte, int, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, maxTrainingResponseBytes))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read the server response: %w", err)
	}
	return data, response.StatusCode, nil
}

// trainingError builds the error a client shows. The backend's envelope message
// is preferred because it explains a refusal; a bare status is the fallback.
func trainingError(status int, body []byte) error {
	var envelope struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != "" {
		return errors.New(envelope.Error)
	}
	return fmt.Errorf("the server refused the conversation (status %d)", status)
}
