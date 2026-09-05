package infra

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// WithCause preserves the returned error and its JSON while retaining a cause
// that an existing domain contract deliberately hides from callers.
//
//wails:ignore
func WithCause(err, cause error) error {
	if err == nil || cause == nil {
		return err
	}
	return AnnotateError(err, Diagnostic{Causes: []error{cause}})
}

func collectDiagnosticCauses(err error, extra Diagnostic, includeReported bool) ([]map[string]any, bool, DiagnosticSeverity) {
	remaining, truncated := 32, false
	var severity DiagnosticSeverity
	seen := make(map[error]bool)
	var walk func(error, Diagnostic) []map[string]any
	walk = func(current error, inherited Diagnostic) []map[string]any {
		if current == nil {
			return nil
		}
		if remaining == 0 {
			truncated = true
			return nil
		}
		remaining--
		if reflect.TypeOf(current).Comparable() {
			if seen[current] {
				truncated = true
				return nil
			}
			seen[current] = true
			defer delete(seen, current)
		}
		if marker, ok := current.(interface{ DiagnosticReported() bool }); ok && marker.DiagnosticReported() && !includeReported {
			return nil
		}
		if annotated, ok := current.(*diagnosticError); ok { //nolint:errorlint // Inspect this node, not a descendant.
			context := Diagnostic{Fields: map[string]any{}}
			mergeDiagnostic(&context, inherited)
			mergeDiagnostic(&context, annotated.diagnostic)
			if inherited.Severity == DiagnosticError {
				context.Severity = DiagnosticError
			}
			result := walk(annotated.err, context)
			for _, cause := range annotated.diagnostic.Causes {
				result = append(result, walk(cause, context)...)
			}
			return result
		}
		// A domain policy applies to its own causes, while sibling branches
		// and their explicit overrides retain independent severity.
		if inherited.Severity == "" {
			if classified, ok := current.(interface{ DiagnosticSeverity() DiagnosticSeverity }); ok {
				inherited.Severity = classified.DiagnosticSeverity()
			}
		}
		if source, ok := current.(interface{ DiagnosticSource() error }); ok && source.DiagnosticSource() != nil {
			return walk(source.DiagnosticSource(), inherited)
		}
		if joined, ok := current.(interface{ Unwrap() []error }); ok {
			var result []map[string]any
			for _, cause := range joined.Unwrap() {
				result = append(result, walk(cause, inherited)...)
			}
			return result
		}
		var children []map[string]any
		if cause := errors.Unwrap(current); cause != nil {
			children = walk(cause, inherited)
			if len(children) == 0 {
				return nil
			}
		}
		if isCancellationMessage(current) {
			return children
		}
		if len(children) == 0 {
			branchSeverity := inherited.Severity
			if branchSeverity == "" {
				branchSeverity = ClassifyError(current)
			}
			if severity == "" || branchSeverity == DiagnosticError {
				severity = branchSeverity
			}
		}
		record := make(map[string]any, len(inherited.Fields)+4)
		for key, value := range inherited.Fields {
			if key == "stackTrace" || key == "stack" {
				if stack, ok := value.(string); ok {
					value = limitDiagnosticText(stack, 16<<10)
				}
			}
			record[key] = value
		}
		record["error"] = limitDiagnosticText(current.Error(), 4<<10)
		record["errorType"] = fmt.Sprintf("%T", current)
		if inherited.Operation != "" {
			record["operation"] = inherited.Operation
		}
		if inherited.Stage != "" {
			record["stage"] = inherited.Stage
		}
		return append([]map[string]any{record}, children...)
	}
	result := walk(err, extra)
	for _, cause := range extra.Causes {
		result = append(result, walk(cause, extra)...)
	}
	if truncated && len(result) == 0 {
		result = []map[string]any{{"error": "diagnostic cause traversal limit reached", "errorType": "diagnostic-limit"}}
	}
	return result, truncated, severity
}

func limitDiagnosticText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "") + " [truncated]"
}

// HTTPDiagnostic captures response metadata without consuming the body or
// collecting request credentials, query strings, or arbitrary headers.
//
//wails:ignore
func HTTPDiagnostic(method, endpoint, stage string, response *http.Response) Diagnostic {
	fields := map[string]any{"method": method, "endpoint": SanitizeLogURL(endpoint)}
	if response != nil {
		fields["status"] = response.StatusCode
		fields["contentType"] = response.Header.Get("Content-Type")
		for _, key := range []string{"CF-Ray", "X-Request-ID", "X-Correlation-ID"} {
			if value := response.Header.Get(key); value != "" {
				fields[key] = limitDiagnosticText(value, 256)
			}
		}
	}
	return Diagnostic{Operation: "http-request", Stage: stage, Fields: fields}
}
