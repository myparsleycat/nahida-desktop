package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

const (
	uploadPlanPageSize = 500
)

var (
	nteFilePattern  = regexp.MustCompile(`(?i)^(.*)\.(pak|utoc|ucas)$`)
	nteShardPattern = regexp.MustCompile(`(?i)_s[1-9][0-9]*$`)
)

type UploadV2Error struct {
	Code    string
	Message string
	status  int
}

//wails:ignore
func (e *UploadV2Error) DiagnosticSeverity() infra.DiagnosticSeverity {
	if e == nil {
		return infra.DiagnosticError
	}
	if e.status >= 400 && e.status < 500 {
		return infra.DiagnosticWarn
	}
	code := strings.ToLower(e.Code)
	if strings.HasPrefix(code, "http_4") ||
		strings.Contains(code, "validation") || strings.Contains(code, "permission") ||
		strings.Contains(code, "policy") || strings.Contains(code, "unsupported") ||
		strings.Contains(code, "conflict") || strings.Contains(code, "too_large") ||
		strings.HasPrefix(code, "invalid_") || strings.HasSuffix(code, "_invalid") {
		return infra.DiagnosticWarn
	}
	return infra.DiagnosticError
}

func (e *UploadV2Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

type UploadPlanItem struct {
	ClientID string `json:"clientId"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	ItemID   string `json:"itemId,omitempty"`
	IntentID string `json:"intentId,omitempty"`
	BundleID string `json:"bundleId,omitempty"`
}

type UploadPlanEntry struct {
	IntentID string `json:"intentId"`
	URL      string `json:"url"`
	Method   string `json:"method"`
	Form     struct {
		Token  string `json:"token"`
		SHA256 string `json:"sha256"`
	} `json:"form"`
}

type NTEBundle struct {
	ID              string   `json:"id"`
	MemberClientIDs []string `json:"memberClientIds"`
	CompleteURL     string   `json:"completeUrl"`
	AbortURL        string   `json:"abortUrl"`
	Form            struct {
		Token string `json:"token"`
	} `json:"form"`
}

type UploadPlan struct {
	Items   []UploadPlanItem
	Uploads map[string]UploadPlanEntry
	Bundles map[string]NTEBundle
}

type UploadPlanProgress struct {
	Phase     transfer.PlanPhase
	Processed int
	Total     int
}

func paginateUploadFiles(files []FinalUploadFile, pageSize int) ([][]FinalUploadFile, error) {
	if pageSize <= 0 {
		pageSize = uploadPlanPageSize
	}
	grouped := make(map[string]int)
	units := make([][]FinalUploadFile, 0, len(files))
	for _, file := range files {
		key := nteGroupKey(file)
		if key == "" {
			units = append(units, []FinalUploadFile{file})
			continue
		}
		if index, ok := grouped[key]; ok {
			units[index] = append(units[index], file)
			continue
		}
		grouped[key] = len(units)
		units = append(units, []FinalUploadFile{file})
	}
	pages := make([][]FinalUploadFile, 0)
	page := make([]FinalUploadFile, 0, pageSize)
	for _, unit := range units {
		if len(unit) > pageSize {
			return nil, &UploadV2Error{Code: "nte_bundle_too_large"}
		}
		if len(page) > 0 && len(page)+len(unit) > pageSize {
			pages = append(pages, page)
			page = make([]FinalUploadFile, 0, pageSize)
		}
		page = append(page, unit...)
		if len(page) >= pageSize {
			pages = append(pages, page)
			page = make([]FinalUploadFile, 0, pageSize)
		}
	}
	if len(page) > 0 {
		pages = append(pages, page)
	}
	return pages, nil
}

func nteGroupKey(file FinalUploadFile) string {
	match := nteFilePattern.FindStringSubmatch(file.Name)
	if len(match) != 3 {
		return ""
	}
	base := match[1]
	if strings.EqualFold(match[2], "ucas") {
		base = nteShardPattern.ReplaceAllString(base, "")
	}
	return file.ParentID + "\x00" + strings.ToLower(norm.NFC.String(base))
}

func (d *Drive) planUploadV2(ctx context.Context, currentID, requestID string, files []FinalUploadFile, onProgress func(UploadPlanProgress)) (UploadPlan, error) {
	if d == nil || d.http == nil {
		return UploadPlan{}, errDriveHTTPUnconfigured
	}
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return UploadPlan{}, err
	}
	for _, file := range files {
		maxSize := rules.MaxFileSize
		if limit, ok := rules.MaxSizeFor(file.Name); ok {
			maxSize = limit
		}
		if file.Size > maxSize {
			return UploadPlan{}, &UploadV2Error{Code: "upload_file_too_large", Message: file.Name + ": upload_file_too_large"}
		}
	}
	pages, err := paginateUploadFiles(files, min(rules.MaxPlanFiles, max(len(files), 1)))
	if err != nil {
		return UploadPlan{}, err
	}
	result := UploadPlan{
		Items:   make([]UploadPlanItem, 0, len(files)),
		Uploads: make(map[string]UploadPlanEntry),
		Bundles: make(map[string]NTEBundle),
	}
	planned := 0
	for _, page := range pages {
		payloadFiles := make([]map[string]any, len(page))
		for i, file := range page {
			payloadFiles[i] = map[string]any{
				"clientId": file.FID,
				"name":     file.Name,
				"sha256":   file.SHA256,
				"size":     file.Size,
				"parentId": file.ParentID,
				"path":     file.Path,
			}
		}
		body, marshalErr := json.Marshal(map[string]any{
			"requestId":    requestID,
			"current":      currentID,
			"capabilities": []string{"nte-bundle-v1"},
			"files":        payloadFiles,
		})
		if marshalErr != nil {
			return UploadPlan{}, marshalErr
		}
		rawURL := strings.TrimRight(d.http.BackendURL(), "/") + "/akasha/v2/sse/drive/files:plan"
		response, fetchErr := d.http.Fetch(ctx, rawURL, infra.FetchOptions{
			Method:            http.MethodPost,
			Header:            http.Header{"Content-Type": []string{"application/json"}},
			Body:              bytes.NewReader(body),
			DisableHTTPErrors: true,
		})
		if fetchErr != nil {
			return UploadPlan{}, fetchErr
		}
		if response.Body == nil {
			return UploadPlan{}, errors.New("upload plan failed: empty response stream")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			raw, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			return UploadPlan{}, infra.AnnotateError(infra.WithCause(uploadV2APIError(decodeAPIValue(response.Header.Get("Content-Type"), raw), response.StatusCode), readErr), infra.HTTPDiagnostic(http.MethodPost, rawURL, "read-upload-plan-error", response))
		}
		completed := false
		parseErr := parseSSE(response.Body, func(event, data string) error {
			switch event {
			case "error":
				return uploadV2APIError(parseRemoteImportData(data), 0)
			case "progress":
				var progress struct {
					Phase     transfer.PlanPhase `json:"phase"`
					Processed int                `json:"processed"`
					Total     int                `json:"total"`
				}
				if err := json.Unmarshal([]byte(data), &progress); err != nil {
					return fmt.Errorf("decode upload plan progress: %w", err)
				}
				if onProgress != nil {
					onProgress(UploadPlanProgress{Phase: progress.Phase, Processed: planned + progress.Processed, Total: len(files)})
				}
			case "complete":
				var complete struct {
					Items      []UploadPlanItem  `json:"items"`
					Uploads    []UploadPlanEntry `json:"uploads"`
					NTEBundles []NTEBundle       `json:"nteBundles"`
				}
				if err := json.Unmarshal([]byte(data), &complete); err != nil {
					return fmt.Errorf("decode upload plan result: %w", err)
				}
				result.Items = append(result.Items, complete.Items...)
				for _, upload := range complete.Uploads {
					result.Uploads[upload.IntentID] = upload
				}
				for _, bundle := range complete.NTEBundles {
					result.Bundles[bundle.ID] = bundle
				}
				completed = true
			}
			return nil
		})
		closeErr := response.Body.Close()
		if parseErr != nil {
			return UploadPlan{}, parseErr
		}
		if closeErr != nil {
			return UploadPlan{}, closeErr
		}
		if !completed {
			return UploadPlan{}, errors.New("upload plan did not complete")
		}
		planned += len(page)
	}
	return result, nil
}

func uploadV2APIError(value any, status int) error {
	code := ""
	message := "upload plan failed"
	if text, ok := value.(string); ok {
		message = text
		if regexp.MustCompile(`(?i)^[a-z][a-z0-9_]+$`).MatchString(text) {
			code = text
		}
	}
	if record, ok := asRecord(value); ok {
		for _, key := range []string{"code", "error", "reason"} {
			if text, textOK := record[key].(string); textOK && text != "" {
				code = text
				break
			}
		}
		if text, ok := record["message"].(string); ok && text != "" {
			message = text
		}
	}
	if code == "" {
		code = fmt.Sprintf("http_%d", status)
	}
	return &UploadV2Error{Code: code, Message: message, status: status}
}
