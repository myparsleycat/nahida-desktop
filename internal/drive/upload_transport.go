package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
)

const (
	uploadRetryLimit    = 3
	uploadCompleteLimit = 15 * time.Minute
)

type uploadHTTPResult struct {
	status  int
	reason  string
	payload map[string]any
}

type uploadProgressReader struct {
	reader     io.Reader
	onProgress func(int64)
}

type backupSourceReader struct {
	reader    io.Reader
	remaining int64
}

func (r *backupSourceReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.remaining -= int64(n)
	if err != nil && (err != io.EOF || r.remaining > 0) {
		return n, fmt.Errorf("%w: %w", ErrBackupSourceRead, err)
	}
	return n, err
}

func (r *uploadProgressReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	if read > 0 && r.onProgress != nil {
		r.onProgress(int64(read))
	}
	return read, err
}

func (d *Drive) uploadIntent(
	ctx context.Context,
	upload UploadPlanEntry,
	file FinalUploadFile,
	onProgress func(int64),
) error {
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return err
	}
	if file.Size >= rules.DirectUploadMaxLogicalBytes {
		return d.uploadParts(ctx, upload, file, rules, onProgress)
	}
	data, compression, useParts, err := prepareUploadRoute(file, upload, rules.Compression, rules.MaxUploadBodyBytes)
	if err != nil {
		return err
	}
	if useParts {
		return d.uploadParts(ctx, upload, file, rules, onProgress)
	}
	return d.uploadPreparedDirect(ctx, upload, file, data, compression, onProgress)
}

func (d *Drive) uploadPreparedDirect(
	ctx context.Context,
	upload UploadPlanEntry,
	file FinalUploadFile,
	data []byte,
	compression string,
	onProgress func(int64),
) error {
	fields := directUploadFields(upload, compression)
	for attempt := 0; attempt <= uploadRetryLimit; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		uploadedPayload := int64(0)
		reportedLogical := int64(0)
		result, sendErr := d.sendMultipart(ctx, upload.URL, http.MethodPost, multipartUpload{
			fields:    fields,
			file:      bytes.NewReader(data),
			fileSize:  int64(len(data)),
			filename:  file.Name,
			fieldName: "file",
			onProgress: func(bytes int64) {
				uploadedPayload += bytes
				target := file.Size
				if len(data) > 0 {
					target = min(file.Size, uploadedPayload*file.Size/int64(len(data)))
				}
				if onProgress != nil && target != reportedLogical {
					onProgress(target - reportedLogical)
				}
				reportedLogical = target
			},
		})
		if sendErr != nil {
			if reportedLogical > 0 && onProgress != nil {
				onProgress(-reportedLogical)
			}
			if ctx.Err() != nil || attempt == uploadRetryLimit {
				return sendErr
			}
			if err := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); err != nil {
				return errors.Join(sendErr, err)
			}
			continue
		}
		if result.status >= 200 && result.status < 300 && result.status != http.StatusAccepted {
			if reportedLogical < file.Size && onProgress != nil {
				onProgress(file.Size - reportedLogical)
			}
			return nil
		}
		if result.status == http.StatusAccepted {
			uploadRequired, waitErr := d.waitUploadIntent(ctx, upload)
			if waitErr != nil {
				if reportedLogical > 0 && onProgress != nil {
					onProgress(-reportedLogical)
				}
				return waitErr
			}
			if !uploadRequired {
				if reportedLogical < file.Size && onProgress != nil {
					onProgress(file.Size - reportedLogical)
				}
				return nil
			}
		}
		if reportedLogical > 0 && onProgress != nil {
			onProgress(-reportedLogical)
		}
		if !retryableUploadResult(result) || attempt == uploadRetryLimit {
			return uploadResultError(result)
		}
		if err := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); err != nil {
			return err
		}
	}
	return errors.New("direct upload exhausted retries")
}

func (d *Drive) waitUploadIntent(ctx context.Context, upload UploadPlanEntry) (bool, error) {
	started := d.now()
	for attempt := 0; d.now().Sub(started) < uploadCompleteLimit; attempt++ {
		result, err := d.sendJSON(
			ctx,
			strings.TrimRight(upload.URL, "/")+"/status",
			map[string]any{"token": upload.Form.Token},
		)
		if err != nil {
			if ctx.Err() != nil {
				return false, err
			}
			result = uploadHTTPResult{reason: err.Error()}
		}

		status, _ := result.payload["status"].(string)
		nextAction, _ := result.payload["nextAction"].(string)
		switch {
		case result.status >= 200 && result.status < 300 && status == "completed":
			return false, nil
		case nextAction == "upload", uploadStatusEndpointUnavailable(result):
			return true, nil
		case !retryableUploadResult(result):
			return false, uploadResultError(result)
		}

		delay := retryDelay(min(attempt, 4), 30*time.Second)
		if retryAfter, ok := result.payload["retryAfterMs"].(float64); ok && retryAfter > 0 {
			delay = min(time.Duration(retryAfter)*time.Millisecond, 30*time.Second)
		}
		if err := d.sleep(ctx, delay); err != nil {
			return false, err
		}
	}
	return false, &UploadV2Error{Code: "upload_processing_timeout"}
}

func (d *Drive) uploadParts(
	ctx context.Context,
	upload UploadPlanEntry,
	file FinalUploadFile,
	rules UploadRules,
	onProgress func(int64),
) (returnErr error) {
	handle, err := os.Open(filepath.FromSlash(file.FullPath))
	if err != nil {
		return fmt.Errorf("%w: open upload file %q: %w", ErrBackupSourceRead, file.Name, err)
	}
	defer func() { _ = handle.Close() }()
	partSize, ok := rules.partSizeForFile(file.Size)
	if !ok || file.Size > rules.MaxFileSize {
		return &UploadV2Error{Code: "file_too_large", Message: file.Name + ": file_too_large"}
	}
	totalParts := uploadPartCount(file.Size, partSize)
	reported := int64(0)
	report := func(bytes int64) {
		reported += bytes
		if onProgress != nil {
			onProgress(bytes)
		}
	}
	defer func() {
		if returnErr != nil && reported > 0 {
			report(-reported)
		}
	}()
	sendAllParts := func() (bool, error) {
		for index := range totalParts {
			start := int64(index) * partSize
			size := min(partSize, file.Size-start)
			completedEarly := false
			for attempt := 0; attempt <= uploadRetryLimit; attempt++ {
				attemptReported := int64(0)
				section := &backupSourceReader{reader: io.NewSectionReader(handle, start, size), remaining: size}
				partURL := fmt.Sprintf("%s/parts/%d", strings.TrimRight(upload.URL, "/"), index)
				result, sendErr := d.sendMultipart(ctx, partURL, http.MethodPut, multipartUpload{
					fields: [][2]string{
						{"token", upload.Form.Token},
						{"totalParts", strconv.Itoa(totalParts)},
					},
					file:      section,
					fileSize:  size,
					filename:  file.Name,
					fieldName: "file",
					onProgress: func(bytes int64) {
						attemptReported += bytes
						report(bytes)
					},
				})
				if sendErr != nil {
					if attemptReported > 0 {
						report(-attemptReported)
					}
					if ctx.Err() != nil || errors.Is(sendErr, ErrBackupSourceRead) || attempt == uploadRetryLimit {
						return false, sendErr
					}
					if sleepErr := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); sleepErr != nil {
						return false, errors.Join(sendErr, sleepErr)
					}
					continue
				}
				if status, _ := result.payload["status"].(string); status == "completed" {
					if reported < file.Size {
						report(file.Size - reported)
					}
					completedEarly = true
					break
				}
				if result.status >= 200 && result.status < 300 {
					break
				}
				if attemptReported > 0 {
					report(-attemptReported)
				}
				if !retryableUploadResult(result) || attempt == uploadRetryLimit {
					return false, uploadResultError(result)
				}
				if err := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); err != nil {
					return false, err
				}
			}
			if completedEarly {
				return true, nil
			}
		}
		return false, nil
	}

	completed, err := sendAllParts()
	if err != nil {
		return err
	}
	if completed {
		return nil
	}

	started := d.now()
	resetAfterMissingManifest := false
	for attempt := 0; d.now().Sub(started) < uploadCompleteLimit; attempt++ {
		result, sendErr := d.sendJSON(
			ctx,
			strings.TrimRight(upload.URL, "/")+"/complete",
			map[string]any{"token": upload.Form.Token},
		)
		if sendErr != nil {
			if ctx.Err() != nil {
				return sendErr
			}
			result = uploadHTTPResult{reason: sendErr.Error()}
		}
		if result.status >= 200 && result.status < 300 && result.status != http.StatusAccepted {
			if reported < file.Size {
				report(file.Size - reported)
			}
			return nil
		}
		if result.status == http.StatusAccepted {
			nextAction, _ := result.payload["nextAction"].(string)
			if nextAction == "poll" {
				uploadRequired, waitErr := d.waitUploadIntent(ctx, upload)
				if waitErr != nil {
					return waitErr
				}
				if !uploadRequired {
					if reported < file.Size {
						report(file.Size - reported)
					}
					return nil
				}
				continue
			}
		}
		if !resetAfterMissingManifest &&
			(result.reason == "chunk_manifest_not_found" || result.reason == "chunks_incomplete") {
			resetAfterMissingManifest = true
			if reported > 0 {
				report(-reported)
			}
			completed, resendErr := sendAllParts()
			if resendErr != nil {
				return resendErr
			}
			if completed {
				return nil
			}
			continue
		}
		if !retryableUploadResult(result) {
			return uploadResultError(result)
		}
		if err := d.sleep(ctx, retryDelay(min(attempt, 4), 30*time.Second)); err != nil {
			return err
		}
	}
	return &UploadV2Error{Code: "complete_timeout"}
}

// multipartUpload is the body of one multipart/form-data upload request: the
// non-file fields plus the part the payload is streamed into.
type multipartUpload struct {
	fields     [][2]string
	file       io.Reader
	fileSize   int64
	filename   string
	fieldName  string
	onProgress func(int64)
}

func (d *Drive) sendMultipart(
	ctx context.Context,
	rawURL, method string,
	upload multipartUpload,
) (uploadHTTPResult, error) {
	if d == nil || d.http == nil {
		return uploadHTTPResult{}, errDriveHTTPUnconfigured
	}
	boundary := "----nahida-desktop-" + uuid.NewString()
	prefix, suffix, err := multipartEnvelope(boundary, upload.fields, upload.filename, upload.fieldName)
	if err != nil {
		return uploadHTTPResult{}, err
	}
	body := io.MultiReader(
		bytes.NewReader(prefix),
		&uploadProgressReader{reader: upload.file, onProgress: upload.onProgress},
		bytes.NewReader(suffix),
	)
	header := make(http.Header)
	header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	contentLength := int64(len(prefix)) + upload.fileSize + int64(len(suffix))
	response, err := d.http.Stream(ctx, rawURL, method, header, body, contentLength)
	if err != nil {
		return uploadHTTPResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return uploadHTTPResult{}, err
	}
	return parseUploadHTTPResult(response.StatusCode, raw), nil
}

func (d *Drive) sendJSON(ctx context.Context, rawURL string, value any) (uploadHTTPResult, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return uploadHTTPResult{}, err
	}
	response, err := d.http.Fetch(ctx, rawURL, infraFetchJSON(http.MethodPost, body))
	if err != nil {
		return uploadHTTPResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return uploadHTTPResult{}, err
	}
	return parseUploadHTTPResult(response.StatusCode, raw), nil
}

func infraFetchJSON(method string, body []byte) infra.FetchOptions {
	return infra.FetchOptions{
		Method:            method,
		Header:            http.Header{"Content-Type": []string{"application/json"}},
		Body:              bytes.NewReader(body),
		DisableHTTPErrors: true,
	}
}

func prepareUploadRoute(
	file FinalUploadFile,
	upload UploadPlanEntry,
	compression UploadCompressionRules,
	maxBody int64,
) ([]byte, string, bool, error) {
	data, algorithm, err := prepareDirectUpload(file, compression)
	if err != nil {
		return nil, "", false, err
	}
	if directUploadExceedsMaxBody(file, data, algorithm, upload, maxBody) {
		return nil, "", true, nil
	}
	return data, algorithm, false, nil
}

func directUploadFields(upload UploadPlanEntry, compression string) [][2]string {
	fields := [][2]string{{"token", upload.Form.Token}}
	if compression != "" {
		fields = append(fields, [2]string{"compAlg", compression})
	}
	return fields
}

func directUploadExceedsMaxBody(
	file FinalUploadFile,
	data []byte,
	compression string,
	upload UploadPlanEntry,
	maxBody int64,
) bool {
	if maxBody <= 0 {
		return true
	}
	size, err := multipartRequestSize(directUploadFields(upload, compression), int64(len(data)), file.Name, "file")
	if err != nil {
		return true
	}
	return size > maxBody
}

func multipartRequestSize(fields [][2]string, fileSize int64, filename, fieldName string) (int64, error) {
	boundary := "----nahida-desktop-" + "00000000-0000-0000-0000-000000000000"
	prefix, suffix, err := multipartEnvelope(boundary, fields, filename, fieldName)
	if err != nil {
		return 0, err
	}
	return int64(len(prefix)) + fileSize + int64(len(suffix)), nil
}

func uploadPartCount(fileSize, partSize int64) int {
	if fileSize <= 0 || partSize <= 0 {
		return 0
	}
	return int((fileSize-1)/partSize + 1)
}

func multipartEnvelope(boundary string, fields [][2]string, filename, fieldName string) ([]byte, []byte, error) {
	var prefix bytes.Buffer
	writer := multipart.NewWriter(&prefix)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, nil, err
	}
	if _, err := writer.CreateFormFile(fieldName, sanitizeMultipartValue(filename)); err != nil {
		return nil, nil, err
	}
	var suffix bytes.Buffer
	suffixWriter := multipart.NewWriter(&suffix)
	if err := suffixWriter.SetBoundary(boundary); err != nil {
		return nil, nil, err
	}
	for _, field := range fields {
		if err := suffixWriter.WriteField(field[0], field[1]); err != nil {
			return nil, nil, err
		}
	}
	if err := suffixWriter.Close(); err != nil {
		return nil, nil, err
	}
	return prefix.Bytes(), append([]byte("\r\n"), suffix.Bytes()...), nil
}

func sanitizeMultipartValue(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return value
}

func prepareDirectUpload(file FinalUploadFile, compression UploadCompressionRules) ([]byte, string, error) {
	data, err := os.ReadFile(filepath.FromSlash(file.FullPath))
	if err != nil {
		return nil, "", fmt.Errorf("%w: read upload file %q: %w", ErrBackupSourceRead, file.Name, err)
	}
	if skipUploadCompression(data, file.Size, compression) {
		return data, "", nil
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(compression.Level)))
	if err != nil {
		return nil, "", err
	}
	compressed := encoder.EncodeAll(data, nil)
	if err := encoder.Close(); err != nil {
		return nil, "", err
	}
	return compressed, uploadCompressionAlgorithm, nil
}

// skipUploadCompression applies the published rules in their own order: tiny
// files, then the media prefixes, then the already-compressed containers.
func skipUploadCompression(data []byte, size int64, compression UploadCompressionRules) bool {
	if size <= compression.SkipMaxBytes {
		return true
	}
	mimeType := uploadMimeType(data)
	if mimeType == "" {
		return false
	}
	for _, prefix := range compression.SkipMimePrefixes {
		if strings.HasPrefix(mimeType, prefix) {
			return true
		}
	}
	return slices.Contains(compression.SkipMimeTypes, mimeType)
}

// uploadMimeType reports the bare type the leading bytes identify. Content that
// identifies nothing, or only the generic binary type, answers nothing so the
// caller compresses it — the server stores an untyped file compressed the same
// way.
func uploadMimeType(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	detected := mimetype.Detect(data)
	if detected == nil || detected.Is("application/octet-stream") {
		return ""
	}
	// Detections such as text/plain carry a charset parameter; the published
	// patterns name bare types.
	baseType, _, _ := strings.Cut(detected.String(), ";")
	return strings.TrimSpace(baseType)
}

func parseUploadHTTPResult(status int, raw []byte) uploadHTTPResult {
	result := uploadHTTPResult{status: status}
	if len(bytes.TrimSpace(raw)) == 0 {
		return result
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) == nil {
		result.payload = payload
		for _, key := range []string{"reason", "error", "message"} {
			if text, ok := payload[key].(string); ok && text != "" {
				result.reason = text
				break
			}
		}
		return result
	}
	result.reason = string(raw)
	return result
}

func retryableUploadResult(result uploadHTTPResult) bool {
	return result.status == 0 || result.status == http.StatusAccepted || result.status == http.StatusRequestTimeout ||
		result.status == http.StatusTooManyRequests ||
		result.status == 524 ||
		result.status >= 500
}

func uploadStatusEndpointUnavailable(result uploadHTTPResult) bool {
	return result.status == http.StatusNotFound && result.reason != "intent_not_found"
}

func uploadResultError(result uploadHTTPResult) error {
	code := result.reason
	if result.payload != nil {
		if payloadCode, ok := result.payload["code"].(string); ok && payloadCode != "" {
			code = payloadCode
		}
	}
	if code == "" {
		code = fmt.Sprintf("http_%d", result.status)
	}
	return &UploadV2Error{Code: code, Message: result.reason}
}

func retryDelay(attempt int, capDuration time.Duration) time.Duration {
	delay := time.Duration(1<<attempt) * time.Second
	return min(delay, capDuration)
}
