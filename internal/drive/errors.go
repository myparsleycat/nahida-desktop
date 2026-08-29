package drive

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
)

const (
	codeBackendUnavailable      = "DRIVE_BACKEND_UNAVAILABLE"
	msgBackendUnavailable       = "The Nahida server is temporarily unavailable. Please try again shortly."
	codeLinkPasswordRequired    = "DRIVE_LINK_PASSWORD_REQUIRED"
	codeLinkInvalidPassword     = "DRIVE_LINK_INVALID_PASSWORD"
	codeModPasswordRequired     = "DRIVE_MOD_PASSWORD_REQUIRED"
	codeModInvalidPassword      = "DRIVE_MOD_INVALID_PASSWORD"
	codeLinkInvalidResponse     = "DRIVE_LINK_INVALID_RESPONSE"
	codeModInvalidResponse      = "DRIVE_MOD_INVALID_RESPONSE"
	codeLinkContentInvalid      = "DRIVE_LINK_CONTENT_INVALID"
	codeModContentInvalid       = "DRIVE_MOD_CONTENT_INVALID"
	codeCopyCanceled            = "DRIVE_COPY_CANCELED"
	codeImportInvalidResponse   = "DRIVE_IMPORT_INVALID_RESPONSE"
	codeCollectionNotFound      = "DRIVE_COLLECTION_NOT_FOUND"
	codeCollectionEmpty         = "DRIVE_COLLECTION_EMPTY"
	codeInvalidSourceURL        = "DRIVE_INVALID_SOURCE_URL"
	msgInvalidSourceURL         = "Enter a Nahida shared link or collection URL."
	msgLinkPasswordRequired     = "This shared link requires a password."
	msgLinkInvalidPassword      = "The shared link password is incorrect."
	msgModPasswordRequired      = "This mod requires a password."
	msgModInvalidPassword       = "The mod password is incorrect."
	msgCollectionBadPassword    = "The collection password is incorrect."
	msgLinkInvalidResponse      = "The shared link response was invalid."
	msgModInvalidResponse       = "The collection response was invalid."
	msgLinkContentInvalid       = "Invalid link content response."
	msgModContentInvalid        = "Invalid mod content response."
	msgCopyCanceled             = "The copy operation was canceled."
	msgCollectionNotFound       = "The requested collection was not found."
	msgCollectionEmpty          = "No public collections were found."
	defaultDriveFailureFallback = "Drive request failed"
)

var (
	driveCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)
	nonAlnum         = regexp.MustCompile(`[^A-Za-z0-9]+`)
	sepRuns          = regexp.MustCompile(`[_-]+`)
)

// DriveAPIError matches Electron DriveApiError.
type DriveAPIError struct {
	Code    string
	Message string
	Status  int
	Cause   error
}

func (e *DriveAPIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func (e *DriveAPIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newDriveAPIError(code, message string, status int, cause error) *DriveAPIError {
	return &DriveAPIError{Code: code, Message: message, Status: status, Cause: cause}
}

func IsBackendUnavailableStatus(status int) bool {
	return status == 502 || status == 503 || status == 504
}

// CreateDriveAPIError is the Go port of Electron createDriveApiError.
// status 0 means "unset", matching the optional Electron argument.
func CreateDriveAPIError(err any, operation string, status int) *DriveAPIError {
	var existing *DriveAPIError
	if asDriveAPIError(err, &existing) {
		return existing
	}

	resolvedStatus := status
	if resolvedStatus == 0 {
		resolvedStatus = getErrorStatus(err)
	}
	if IsBackendUnavailableStatus(resolvedStatus) {
		return newDriveAPIError(codeBackendUnavailable, msgBackendUnavailable, resolvedStatus, asError(err))
	}

	message := toErrorMessage(err)
	normalized := message
	if message == "[object Object]" || message == "" {
		normalized = defaultDriveFailureFallback
	}

	code := toDriveErrorCode(err)
	if code == "" {
		code = inferDriveErrorCode(normalized)
	}
	if code == "" {
		code = fmt.Sprintf("DRIVE_%s_FAILED", nonAlnum.ReplaceAllString(strings.ToUpper(operation), "_"))
	}
	return newDriveAPIError(code, normalized, resolvedStatus, asError(err))
}

func normalizeDriveBoundaryError(err *error, operation string) {
	if err != nil && *err != nil {
		*err = CreateDriveAPIError(*err, operation, 0)
	}
}

func asDriveAPIError(err any, dest **DriveAPIError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*DriveAPIError); ok && e != nil {
		*dest = e
		return true
	}
	if e, ok := err.(error); ok && errors.As(e, dest) && *dest != nil {
		return true
	}
	return false
}

func inferDriveErrorCode(message string) string {
	normalized := sepRuns.ReplaceAllString(strings.ToLower(message), " ")
	if strings.Contains(normalized, "password required") || strings.Contains(normalized, "missing password") {
		return codeLinkPasswordRequired
	}
	if strings.Contains(normalized, "invalid password") || strings.Contains(normalized, "incorrect password") {
		return codeLinkInvalidPassword
	}
	return ""
}

func getErrorStatus(err any) int {
	if err == nil {
		return 0
	}
	switch v := err.(type) {
	case *DriveAPIError:
		if v != nil {
			return v.Status
		}
	case *infra.HTTPError:
		if v != nil {
			return v.Status
		}
	case *infra.APIError:
		if v != nil {
			return v.Status
		}
	case map[string]any:
		if n, ok := asInt(v["status"]); ok {
			return n
		}
		if resp, ok := v["response"].(map[string]any); ok {
			if n, ok := asInt(resp["status"]); ok {
				return n
			}
		}
	}
	return 0
}

func toDriveErrorCode(err any) string {
	if s, ok := err.(string); ok && driveCodePattern.MatchString(s) {
		return normalizeDriveErrorCode(s)
	}
	if api, ok := err.(*infra.APIError); ok && api != nil && strings.TrimSpace(api.Code) != "" {
		return normalizeDriveErrorCode(api.Code)
	}
	record, ok := asRecord(err)
	if !ok {
		return ""
	}
	if code, ok := record["code"].(string); ok && strings.TrimSpace(code) != "" {
		return normalizeDriveErrorCode(code)
	}
	switch value := record["value"].(type) {
	case string:
		if driveCodePattern.MatchString(value) {
			return normalizeDriveErrorCode(value)
		}
	case map[string]any:
		if nested, ok := value["code"].(string); ok && strings.TrimSpace(nested) != "" {
			return normalizeDriveErrorCode(nested)
		}
	}
	return ""
}

func normalizeDriveErrorCode(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "MISSING_PASSWORD":
		return codeLinkPasswordRequired
	case "INVALID_PASSWORD":
		return codeLinkInvalidPassword
	default:
		return strings.TrimSpace(code)
	}
}

func toErrorMessage(err any) string {
	if msg := formatErrorMessage(err, map[uintptr]struct{}{}); msg != nil {
		return *msg
	}
	return "Unknown error"
}

func formatErrorMessage(err any, seen map[uintptr]struct{}) *string {
	if err == nil {
		return nil
	}
	switch v := err.(type) {
	case *DriveAPIError:
		if v == nil {
			return nil
		}
		msg := strings.TrimSpace(v.Message)
		if msg == "" {
			return nil
		}
		return &msg
	case error:
		msg := strings.TrimSpace(v.Error())
		if msg == "" {
			return nil
		}
		return &msg
	case string:
		msg := strings.TrimSpace(v)
		if msg == "" {
			return nil
		}
		return &msg
	case json.Number:
		s := v.String()
		return &s
	case bool:
		s := strconv.FormatBool(v)
		return &s
	case int:
		s := strconv.Itoa(v)
		return &s
	case int64:
		s := strconv.FormatInt(v, 10)
		return &s
	case float64:
		s := strconv.FormatFloat(v, 'f', -1, 64)
		return &s
	}

	record, ok := asRecord(err)
	if !ok {
		return nil
	}
	if markSeen(err, seen) {
		return nil
	}
	if nested := formatErrorMessage(record["value"], seen); nested != nil {
		return nested
	}
	for _, key := range []string{"message", "error", "detail", "title", "code"} {
		if s, ok := record[key].(string); ok && strings.TrimSpace(s) != "" {
			msg := strings.TrimSpace(s)
			return &msg
		}
	}
	raw, jsonErr := json.Marshal(record)
	if jsonErr != nil {
		return nil
	}
	serialized := string(raw)
	if serialized != "" && serialized != "{}" && serialized != "[]" {
		return &serialized
	}
	return nil
}

func asRecord(err any) (map[string]any, bool) {
	switch v := err.(type) {
	case map[string]any:
		return v, true
	case json.RawMessage:
		var out map[string]any
		if json.Unmarshal(v, &out) == nil && out != nil {
			return out, true
		}
	}
	return nil, false
}

func markSeen(err any, seen map[uintptr]struct{}) bool {
	ptr := identity(err)
	if ptr == 0 {
		return false
	}
	if _, ok := seen[ptr]; ok {
		return true
	}
	seen[ptr] = struct{}{}
	return false
}

func identity(v any) uintptr {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		if rv.IsNil() {
			return 0
		}
		return rv.Pointer()
	default:
		return 0
	}
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func asError(err any) error {
	if err == nil {
		return nil
	}
	if e, ok := err.(error); ok {
		return e
	}
	return errors.New(toErrorMessage(err))
}
