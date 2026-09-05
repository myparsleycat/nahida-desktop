package drive

import (
	"encoding/json"
	"strings"
)

func parseRemoteImportData(value string) any {
	trim := strings.TrimSpace(value)
	if trim == "" {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(trim), &v) == nil {
		return v
	}
	return trim
}

func remoteImportErrorMessage(value any) any {
	if value == nil {
		return "The server import failed."
	}
	if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	record, ok := asRecord(value)
	if !ok {
		return "The server import failed."
	}
	for _, key := range []string{"message", "error", "code"} {
		if s, ok := record[key].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return "The server import failed."
}

func remoteImportNumber(value any, key string) (int64, bool) {
	record, ok := asRecord(value)
	if !ok {
		return 0, false
	}
	switch number := record[key].(type) {
	case float64:
		return int64(number), true
	case float32:
		return int64(number), true
	case int:
		return int64(number), true
	case int64:
		return number, true
	case json.Number:
		value, err := number.Int64()
		return value, err == nil
	default:
		return 0, false
	}
}

func remoteImportInt(value any, key string) (int, bool) {
	number, ok := remoteImportNumber(value, key)
	return int(number), ok
}

func remoteImportStatus(event string, value any) string {
	if event != "status" {
		return ""
	}
	if status, ok := value.(string); ok {
		return status
	}
	record, ok := asRecord(value)
	if !ok {
		return ""
	}
	status, _ := record["status"].(string)
	return status
}
