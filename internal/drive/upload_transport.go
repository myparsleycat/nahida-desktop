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
	"strings"
	"time"

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

func (r *uploadProgressReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	if read > 0 && r.onProgress != nil {
		r.onProgress(int64(read))
	}
	return read, err
}

func (d *Drive) uploadIntent(ctx context.Context, upload UploadPlanEntry, file FinalUploadFile, onProgress func(int64)) error {
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return err
	}
	if file.Size >= rules.DirectThreshold() {
		return d.uploadParts(ctx, upload, file, rules, onProgress)
	}
	data, compression, err := prepareDirectUpload(file)
	if err != nil {
		return err
	}
	return d.uploadPreparedDirect(ctx, upload, file, data, compression, onProgress)
}

func (d *Drive) uploadPreparedDirect(ctx context.Context, upload UploadPlanEntry, file FinalUploadFile, data []byte, compression string, onProgress func(int64)) error {
	fields := [][2]string{{"token", upload.Form.Token}}
	if compression != "" {
		fields = append(fields, [2]string{"compAlg", compression})
	}
	for attempt := 0; attempt <= uploadRetryLimit; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		uploadedPayload := int64(0)
		reportedLogical := int64(0)
		result, sendErr := d.sendMultipart(ctx, upload.URL, http.MethodPost, fields, bytes.NewReader(data), int64(len(data)), file.Name, func(bytes int64) {
			uploadedPayload += bytes
			target := file.Size
			if len(data) > 0 {
				target = min(file.Size, uploadedPayload*file.Size/int64(len(data)))
			}
			if onProgress != nil && target != reportedLogical {
				onProgress(target - reportedLogical)
			}
			reportedLogical = target
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

func (d *Drive) uploadParts(ctx context.Context, upload UploadPlanEntry, file FinalUploadFile, rules UploadRules, onProgress func(int64)) (returnErr error) {
	handle, err := os.Open(filepath.FromSlash(file.FullPath))
	if err != nil {
		return fmt.Errorf("open upload file %q: %w", file.Name, err)
	}
	defer func() { _ = handle.Close() }()
	partSize := rules.PartSize()
	totalParts := int((file.Size + partSize - 1) / partSize)
	if file.Size > rules.MaxFileSize || totalParts > rules.Parts.MaxParts {
		return &UploadV2Error{Code: "file_too_large", Message: file.Name + ": file_too_large"}
	}
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
				section := io.NewSectionReader(handle, start, size)
				result, sendErr := d.sendMultipart(
					ctx,
					fmt.Sprintf("%s/parts/%d", strings.TrimRight(upload.URL, "/"), index),
					http.MethodPut,
					[][2]string{{"token", upload.Form.Token}, {"totalParts", fmt.Sprintf("%d", totalParts)}},
					section,
					size,
					file.Name,
					func(bytes int64) { attemptReported += bytes; report(bytes) },
				)
				if sendErr != nil {
					if attemptReported > 0 {
						report(-attemptReported)
					}
					if ctx.Err() != nil || attempt == uploadRetryLimit {
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
		result, sendErr := d.sendJSON(ctx, strings.TrimRight(upload.URL, "/")+"/complete", map[string]any{"token": upload.Form.Token})
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
		if !resetAfterMissingManifest && (result.reason == "chunk_manifest_not_found" || result.reason == "chunks_incomplete") {
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

func (d *Drive) sendMultipart(ctx context.Context, rawURL, method string, fields [][2]string, file io.Reader, fileSize int64, filename string, onProgress func(int64)) (uploadHTTPResult, error) {
	return d.sendMultipartField(ctx, rawURL, method, fields, file, fileSize, filename, "file", onProgress)
}

func (d *Drive) sendMultipartField(ctx context.Context, rawURL, method string, fields [][2]string, file io.Reader, fileSize int64, filename, fieldName string, onProgress func(int64)) (uploadHTTPResult, error) {
	if d == nil || d.http == nil {
		return uploadHTTPResult{}, errDriveHTTPUnconfigured
	}
	boundary := "----nahida-desktop-" + uuid.NewString()
	prefix, suffix, err := multipartEnvelope(boundary, fields, filename, fieldName)
	if err != nil {
		return uploadHTTPResult{}, err
	}
	body := io.MultiReader(
		bytes.NewReader(prefix),
		&uploadProgressReader{reader: file, onProgress: onProgress},
		bytes.NewReader(suffix),
	)
	header := make(http.Header)
	header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	response, err := d.http.Stream(ctx, rawURL, method, header, body, int64(len(prefix))+fileSize+int64(len(suffix)))
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

func prepareDirectUpload(file FinalUploadFile) ([]byte, string, error) {
	data, err := os.ReadFile(filepath.FromSlash(file.FullPath))
	if err != nil {
		return nil, "", fmt.Errorf("read upload file %q: %w", file.Name, err)
	}
	if file.Size <= 100 || isPreviewUploadFile(data, file.Name) {
		return data, "", nil
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, "", err
	}
	compressed := encoder.EncodeAll(data, nil)
	if err := encoder.Close(); err != nil {
		return nil, "", err
	}
	return compressed, "zstd", nil
}

func isPreviewUploadFile(data []byte, name string) bool {
	signatures := [][]byte{
		{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
		{0x47, 0x49, 0x46, 0x38},
		{0xff, 0xd8, 0xff},
		{0x42, 0x4d},
		{0x49, 0x49, 0x2a, 0x00},
		{0x4d, 0x4d, 0x00, 0x2a},
		{0x00, 0x00, 0x01, 0x00},
		{0x1a, 0x45, 0xdf, 0xa3},
	}
	for _, signature := range signatures {
		if bytes.HasPrefix(data, signature) {
			return true
		}
	}
	if len(data) >= 8 && string(data[4:8]) == "ftyp" {
		return true
	}
	extension := strings.ToLower(filepath.Ext(name))
	switch extension {
	case ".gif", ".jpg", ".jpeg", ".tif", ".tiff", ".png", ".webp", ".bmp", ".ico", ".mp4", ".webm", ".ogg", ".mov", ".avi", ".flv", ".mkv":
		return true
	default:
		return false
	}
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
	return result.status == 0 || result.status == http.StatusAccepted || result.status == http.StatusRequestTimeout || result.status == http.StatusTooManyRequests || result.status == 524 || result.status >= 500
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
