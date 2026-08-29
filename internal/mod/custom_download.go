package mod

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

type GameBananaDownloadProps struct {
	ItemID    int    `json:"itemId"`
	FileID    int    `json:"fileId"`
	ModelName string `json:"modelName,omitempty"`
}

func (m *Mod) DownloadFromURL(ctx context.Context, rawURL, groupPath string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", errors.New("DOWNLOAD_URL_REQUIRED")
	}
	if m.http == nil || m.transfer == nil {
		return "", errors.New("download services are not configured")
	}
	if err := validateDownloadURL(trimmed); err != nil {
		return "", err
	}
	if err := os.MkdirAll(groupPath, 0o755); err != nil {
		return "", err
	}
	head, err := m.headDownload(ctx, trimmed)
	if err != nil {
		return "", err
	}
	if isHTMLContentType(head.header) {
		return "", errors.New("DOWNLOAD_URL_HTML_PAGE")
	}
	suggested := parseDownloadFileName(head.finalURL, m.sanitizeName, head.header.Get("Content-Disposition"))
	savePath := createSiblingTempPath(groupPath, "download"+getDownloadTempExtension(suggested))
	stagingPath := createSiblingTempPath(groupPath, "staging")
	pid := uuid.NewString()
	size := int64(0)
	if head.size != nil {
		size = *head.size
	}
	if err := m.queueDownload(pid, suggested, groupPath, head.finalURL, size, func(runCtx context.Context, transfers *transfer.Transfer) error {
		return m.runGroupDownload(runCtx, transfers, pid, head, savePath, stagingPath, groupPath, suggested)
	}); err != nil {
		return "", err
	}
	return "started", nil
}

func (m *Mod) DownloadGameBananaFile(ctx context.Context, props GameBananaDownloadProps) (string, error) {
	if props.ItemID <= 0 || props.FileID <= 0 {
		return "", errors.New("invalid GameBanana download payload")
	}
	if m.gamebanana == nil {
		return "", errors.New("gamebanana service is not configured")
	}
	payload, err := m.gamebanana.GetDownloadFilePayload(ctx, gamebanana.DownloadFileInput{
		ItemID: props.ItemID, FileID: props.FileID, ModelName: props.ModelName,
	})
	if err != nil {
		return "", err
	}
	head, err := m.headDownload(ctx, payload.FileURL)
	if err != nil {
		return "", fmtGBHead(err)
	}
	if !head.ok {
		return "", errors.New("GAMEBANANA_DOWNLOAD_HEAD_FAILED:" + strconv.Itoa(head.status) + ":" + orUnknown(head.statusText))
	}
	suggested := parseDownloadFileName(head.finalURL, m.sanitizeName, head.header.Get("Content-Disposition"))
	result, err := m.paths.getSelectedPathWithModeModal(ctx, suggested, ptrString(payload.CategoryName), payload.ImporterKey, "gamebanana", nil, false)
	if err != nil {
		return "", err
	}
	if result.Path == nil {
		return "canceled", nil
	}
	destination := *result.Path
	finalName := suggested
	if result.FileName != nil && *result.FileName != "" {
		finalName = *result.FileName
	}
	stagingPath, stagedDownloadPath := getStagingPaths(finalName, m.sanitizeName)
	size := int64(0)
	if head.size != nil {
		size = *head.size
	}
	pid := uuid.NewString()
	if err := m.queueDownload(pid, finalName, destination, head.finalURL, size, func(runCtx context.Context, transfers *transfer.Transfer) error {
		return m.runGameBananaDownload(runCtx, transfers, pid, head, payload, stagingPath, stagedDownloadPath, destination, suggested, finalName)
	}); err != nil {
		return "", err
	}
	return "started", nil
}

func (m *Mod) HuiDownload(ctx context.Context, title, fileURL string) (string, error) {
	sanitized := m.sanitizeName(title)
	result, err := m.paths.getSelectedPathWithModeModal(ctx, sanitized, nil, nil, "hui", nil, false)
	if err != nil {
		return "", err
	}
	if result.Path == nil {
		return "canceled", nil
	}
	destination := *result.Path
	head, err := m.headDownload(ctx, fileURL)
	if err != nil {
		return "", err
	}
	if err := validateHuiHead(head); err != nil {
		return "", err
	}
	finalName := sanitized
	if result.FileName != nil && *result.FileName != "" {
		finalName = *result.FileName
	}
	stagingPath, stagedDownloadPath := getStagingPaths(finalName, m.sanitizeName)
	size := int64(0)
	if head.size != nil {
		size = *head.size
	}
	pid := uuid.NewString()
	if err := m.queueDownload(pid, finalName, destination, fileURL, size, func(runCtx context.Context, transfers *transfer.Transfer) error {
		return m.runHuiCustomDownload(runCtx, transfers, pid, head, stagingPath, stagedDownloadPath, destination, sanitized, finalName)
	}); err != nil {
		return "", err
	}
	return "started", nil
}

func validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" {
		return errors.New("INVALID_DOWNLOAD_URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Host == "" {
			return errors.New("INVALID_DOWNLOAD_URL")
		}
		return nil
	default:
		return errors.New("UNSUPPORTED_DOWNLOAD_URL_PROTOCOL")
	}
}

func validateHuiHead(head downloadHead) error {
	if head.ok {
		return nil
	}
	return huiHeadError(fmt.Sprintf("Failed to get real file URL: %s", orUnknown(head.statusText)))
}

type huiHeadError string

func (e huiHeadError) Error() string { return string(e) }

func (m *Mod) ResolveArchiveExtractPrompt(requestID string, mode *string) error {
	m.extractMu.Lock()
	ch := m.extractPrompts[requestID]
	delete(m.extractPrompts, requestID)
	m.extractMu.Unlock()
	if ch == nil {
		return errors.New("pending archive extract prompt not found")
	}
	if mode == nil {
		select {
		case ch <- "":
		default:
		}
		return nil
	}
	ch <- *mode
	return nil
}

type downloadHead struct {
	finalURL      string
	size          *int64
	supportsRange *bool
	header        http.Header
	ok            bool
	status        int
	statusText    string
}

func (m *Mod) headDownload(ctx context.Context, rawURL string) (downloadHead, error) {
	response, err := m.http.Fetch(ctx, rawURL, infra.FetchOptions{Method: http.MethodHead, DisableHTTPErrors: true})
	if err != nil {
		return downloadHead{}, err
	}
	defer func() { _ = response.Body.Close() }()
	finalURL := rawURL
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL.String()
	}
	statusText := http.StatusText(response.StatusCode)
	if statusText == "" {
		statusText = strings.TrimSpace(strings.TrimPrefix(response.Status, strconv.Itoa(response.StatusCode)))
	}
	head := downloadHead{
		finalURL: finalURL, header: response.Header.Clone(),
		ok:     response.StatusCode >= 200 && response.StatusCode < 300,
		status: response.StatusCode, statusText: statusText,
	}
	head.size = parseContentLength(response.Header.Get("Content-Length"))
	if head.ok {
		supported := strings.ToLower(strings.TrimSpace(response.Header.Get("Accept-Ranges"))) == "bytes"
		head.supportsRange = &supported
	}
	return head, nil
}

func (m *Mod) queueDownload(
	pid, name, dest, fileURL string,
	size int64,
	run func(context.Context, *transfer.Transfer) error,
) error {
	parent := pid
	data := transfer.Data{
		Root: &transfer.Root{ID: pid, Name: name},
		Files: []transfer.DownloadFile{{
			ID: pid, FileID: pid, ParentID: &parent, Name: name, Size: size, URL: fileURL,
		}},
		Dirs: []transfer.Directory{},
	}
	if _, err := m.transfer.Create(transfer.CreateParams{
		PID: pid, Type: "download", Name: name, Path: filepath.ToSlash(dest),
		CurrentID: pid, InitialStatus: transfer.StatusPending, Data: data,
	}); err != nil {
		return err
	}
	return m.transfer.RegisterRunner(pid, func(runCtx context.Context, transfers *transfer.Transfer, _ string) error {
		return run(runCtx, transfers)
	})
}

func (m *Mod) runGroupDownload(
	ctx context.Context,
	transfers *transfer.Transfer,
	pid string,
	head downloadHead,
	savePath, stagingPath, groupPath, suggested string,
) error {
	progress := transfer.StatusProgress
	_ = transfers.Update(pid, transfer.Updates{Status: &progress})
	defer func() {
		_ = os.RemoveAll(savePath)
		_ = os.RemoveAll(stagingPath)
	}()
	downloaded := int64(0)
	if err := m.downloadFileTo(ctx, head, savePath, pid, "custom", func(bytes int64) {
		downloaded += bytes
		now := downloaded
		_ = transfers.Update(pid, transfer.Updates{TransferredSize: &now})
	}); err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "CustomDownloader:downloadToGroup")
	}
	html, err := isHTMLResponseOrContent(head.header, savePath)
	if err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "CustomDownloader:downloadToGroup")
	}
	if html {
		return m.finishDownloadError(ctx, transfers, pid, errors.New("DOWNLOAD_URL_HTML_PAGE"), "CustomDownloader:downloadToGroup")
	}
	if err := os.MkdirAll(stagingPath, 0o755); err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "CustomDownloader:downloadToGroup")
	}
	shouldExtract := isArchiveByResponseOrContent(ctx, head.header, suggested, savePath, m.archive)
	extractedPath := filepath.Join(stagingPath, suggested)
	if shouldExtract {
		extracted, extractErr := m.extractDownloadedArchive(ctx, savePath, stagingPath)
		if extractErr != nil {
			return m.finishDownloadError(ctx, transfers, pid, extractErr, "CustomDownloader:downloadToGroup")
		}
		extractedPath = extracted
	} else if err := movePathOverwrite(savePath, extractedPath); err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "CustomDownloader:downloadToGroup")
	}
	entries, _ := os.ReadDir(stagingPath)
	if _, statErr := os.Stat(extractedPath); statErr != nil || len(entries) == 0 {
		return m.finishDownloadError(ctx, transfers, pid, errors.New("downloaded file did not produce staged content"), "CustomDownloader:downloadToGroup")
	}
	finalized, err := finalizeStagedDownload(stagingPath, groupPath)
	if err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "CustomDownloader:downloadToGroup")
	}
	if err := writeModDownloadMetadataToDirectories(finalized.DestinationPaths, map[string]any{
		"source": "mod", "downloadedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		_ = finalized.Restore()
		return m.finishDownloadError(ctx, transfers, pid, err, "CustomDownloader:downloadToGroup")
	}
	_ = finalized.Commit()
	return m.finishDownloadOK(transfers, pid, head, downloaded, groupPath, suggested)
}

func (m *Mod) runGameBananaDownload(
	ctx context.Context,
	transfers *transfer.Transfer,
	pid string,
	head downloadHead,
	payload gamebanana.DownloadFilePayload,
	stagingPath, stagedDownloadPath, destination, suggested, finalName string,
) error {
	progress := transfer.StatusProgress
	_ = transfers.Update(pid, transfer.Updates{Status: &progress})
	_ = os.MkdirAll(stagingPath, 0o755)
	defer func() { _ = os.RemoveAll(stagingPath) }()
	downloaded := int64(0)
	if err := m.downloadFileTo(ctx, head, stagedDownloadPath, pid, "gamebanana", func(bytes int64) {
		downloaded += bytes
		now := downloaded
		_ = transfers.Update(pid, transfer.Updates{TransferredSize: &now})
	}); err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB:context")
	}
	stagedPath := stagedDownloadPath
	if isArchiveByResponseOrContent(ctx, head.header, suggested, stagedDownloadPath, m.archive) {
		extracted, err := m.extractGBArchive(ctx, stagedDownloadPath)
		if err != nil {
			return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB:context")
		}
		renamed, err := applySelectedExtractedName(extracted, stagingPath, finalName, suggested, m.sanitizeName)
		if err != nil {
			return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB:context")
		}
		stagedPath = renamed
	}
	if payload.PreviewURL != nil && *payload.PreviewURL != "" {
		previewPath := filepath.Join(getPreviewTargetDir(stagedPath), "preview.jpg")
		previewHead := downloadHead{finalURL: *payload.PreviewURL, header: http.Header{}}
		if err := m.downloadFileTo(ctx, previewHead, previewPath, pid+":preview", "gamebanana-preview", nil); err != nil {
			return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB:context")
		}
	}
	finalized, err := finalizeStagedDownload(stagingPath, destination)
	if err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB:context")
	}
	metadata := map[string]any{
		"source": "gamebanana", "downloadedAt": time.Now().UTC().Format(time.RFC3339Nano),
		"mod":    map[string]any{"id": payload.ModID, "pageUrl": payload.ModPageURL, "version": payload.Version},
		"author": map[string]any{"name": payload.AuthorName, "url": payload.AuthorURL},
		"file":   map[string]any{"downloadUrl": payload.FileURL, "md5": payload.FileMD5},
	}
	if err := writeModDownloadMetadataToDirectories(finalized.DestinationPaths, metadata); err != nil {
		_ = finalized.Restore()
		return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB:context")
	}
	_ = finalized.Commit()
	return m.finishDownloadOK(transfers, pid, head, downloaded, destination, finalName)
}

func (m *Mod) runHuiCustomDownload(
	ctx context.Context,
	transfers *transfer.Transfer,
	pid string,
	head downloadHead,
	stagingPath, stagedDownloadPath, destination, originalTitle, finalName string,
) error {
	progress := transfer.StatusProgress
	_ = transfers.Update(pid, transfer.Updates{Status: &progress})
	_ = os.MkdirAll(stagingPath, 0o755)
	defer func() { _ = os.RemoveAll(stagingPath) }()
	downloaded := int64(0)
	if err := m.downloadFileTo(ctx, head, stagedDownloadPath, pid, "hui", func(bytes int64) {
		downloaded += bytes
		now := downloaded
		_ = transfers.Update(pid, transfer.Updates{TransferredSize: &now})
	}); err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB")
	}
	name := parseDownloadFileName(head.finalURL, m.sanitizeName, head.header.Get("Content-Disposition"))
	if isArchiveByResponseOrContent(ctx, head.header, name, stagedDownloadPath, m.archive) {
		extracted, err := m.archive.Extract(ctx, stagedDownloadPath, stagingPath, infra.ExtractOptions{}, nil)
		if err != nil {
			return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB")
		}
		if _, err := applySelectedExtractedName(extracted, stagingPath, finalName, originalTitle, m.sanitizeName); err != nil {
			return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB")
		}
		_ = os.Remove(stagedDownloadPath)
	}
	finalized, err := finalizeStagedDownload(stagingPath, destination)
	if err != nil {
		return m.finishDownloadError(ctx, transfers, pid, err, "GameBanana:downloadFromGB")
	}
	_ = finalized.Commit()
	return m.finishDownloadOK(transfers, pid, head, downloaded, destination, finalName)
}

func (m *Mod) downloadFileTo(
	ctx context.Context,
	head downloadHead,
	savePath, fileID, cohort string,
	onProgress func(int64),
) error {
	adapter := modRangeDownloader{inner: m.downloader}
	return downloadCustomFile(ctx, customDownloadFileOptions{
		URL: head.finalURL, SavePath: savePath, FileSize: head.size, SupportsRange: head.supportsRange,
		Downloader: adapter, HTTP: m.http, OnProgress: onProgress,
		BandwidthLimiter: m.transfer, SlowChunkMonitor: m.transfer.SlowChunks(), FileID: fileID, CohortKey: cohort,
	})
}

type modRangeDownloader struct {
	inner *infra.ParallelDownloader
}

func (a modRangeDownloader) CheckRangeSupport(ctx context.Context, rawURL string) (bool, error) {
	if a.inner == nil {
		return false, nil
	}
	return a.inner.CheckRangeSupport(ctx, rawURL)
}

func (a modRangeDownloader) Download(ctx context.Context, request parallelDownloadRequest) error {
	if a.inner == nil {
		return errors.New("range downloader is not configured")
	}
	return a.inner.Download(ctx, infra.ParallelDownloadOptions{
		URL: request.URL, SavePath: request.SavePath, FileSize: request.FileSize, MaxChunks: 8, Adaptive: true,
		Header: request.Header, OnProgress: request.OnProgress, BandwidthLimiter: request.BandwidthLimiter,
		SlowChunkMonitor: request.SlowChunkMonitor, FileID: request.FileID, CohortKey: request.CohortKey,
	})
}

func (m *Mod) extractDownloadedArchive(ctx context.Context, archivePath, groupPath string) (string, error) {
	mode, err := m.resolveArchiveExtractMode(ctx, archivePath)
	if err != nil {
		return "", err
	}
	flatten := mode == "flatten_single_root"
	return m.archive.Extract(ctx, archivePath, groupPath, infra.ExtractOptions{FlattenSingleRoot: &flatten}, nil)
}

func (m *Mod) extractGBArchive(ctx context.Context, archivePath string) (string, error) {
	extracted, err := m.archive.Extract(ctx, archivePath, filepath.Dir(archivePath), infra.ExtractOptions{}, nil)
	if err != nil {
		return "", err
	}
	_ = os.Remove(archivePath)
	return extracted, nil
}

func (m *Mod) resolveArchiveExtractMode(ctx context.Context, archivePath string) (string, error) {
	if m.settings == nil {
		return "flatten_single_root", nil
	}
	mode, err := m.settings.GetArchiveExtractPathMode(ctx)
	if err != nil {
		return "", err
	}
	if mode != "ask_every_time" {
		return mode, nil
	}
	single, err := m.archive.HasSingleTopLevelDirectory(ctx, archivePath)
	if err != nil {
		return "", err
	}
	if !single {
		return "flatten_single_root", nil
	}
	return m.promptArchiveExtractMode(archivePath)
}

func (m *Mod) promptArchiveExtractMode(archivePath string) (string, error) {
	requestID := uuid.NewString()
	ch := make(chan string, 1)
	m.extractMu.Lock()
	m.extractPrompts[requestID] = ch
	m.extractMu.Unlock()
	if m.emit != nil {
		m.emit("mod:archiveExtractPrompt", map[string]string{
			"requestId": requestID, "fileName": filepath.Base(archivePath),
		})
	}
	mode := <-ch
	if mode == "" {
		return "", errors.New("aborted")
	}
	return mode, nil
}

func (m *Mod) finishDownloadOK(transfers *transfer.Transfer, pid string, head downloadHead, downloaded int64, dest, name string) error {
	_ = transfers.MarkFileCompleted(pid, pid)
	completed := transfer.StatusCompleted
	hundred := 100.0
	transferred := downloaded
	if head.size != nil {
		transferred = *head.size
	}
	one := 1
	_ = transfers.Update(pid, transfer.Updates{Status: &completed, Progress: &hundred, TransferredSize: &transferred, TransferredFiles: &one})
	if m.emit != nil {
		m.emit("download:completed", map[string]string{"path": dest, "name": name})
	}
	return nil
}

func (m *Mod) finishDownloadError(ctx context.Context, transfers *transfer.Transfer, pid string, err error, where string) error {
	canceled := ctx.Err() != nil || isAbortErr(err)
	if canceled {
		status := transfer.StatusCanceled
		_ = transfers.Update(pid, transfer.Updates{Status: &status})
		return nil
	}
	if m.log != nil {
		m.log.Error(err.Error(), where)
	}
	failed := transfer.StatusError
	msg := err.Error()
	_ = transfers.Update(pid, transfer.Updates{Status: &failed, Error: &msg})
	return nil
}

func (m *Mod) sanitizeName(name string) string {
	if m.fs == nil {
		return name
	}
	return m.fs.SanitizeWindowsFilename(name, " ")
}

func isAbortErr(err error) bool {
	var aborted abortError
	return errors.As(err, &aborted) || errors.Is(err, context.Canceled) || (err != nil && err.Error() == "Aborted")
}

func fmtGBHead(err error) error {
	if err == nil {
		return nil
	}
	if strings.HasPrefix(err.Error(), "GAMEBANANA_DOWNLOAD_HEAD_FAILED:") {
		return err
	}
	return errors.New("GAMEBANANA_DOWNLOAD_HEAD_FAILED:" + err.Error())
}

func ptrString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func orUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "UNKNOWN"
	}
	return value
}
