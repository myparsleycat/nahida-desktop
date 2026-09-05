package infra

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
)

type DiagnosticSeverity string

const (
	DiagnosticWarn  DiagnosticSeverity = "warn"
	DiagnosticError DiagnosticSeverity = "error"
)

// Diagnostic describes safe, allow-listed context for one backend failure.
//
//wails:ignore
type Diagnostic struct {
	Severity  DiagnosticSeverity
	Operation string
	Stage     string
	Fields    map[string]any
	// Causes are diagnostic-only: they never change the public error contract.
	Causes []error
}

type diagnosticError struct {
	err        error
	diagnostic Diagnostic
	reported   bool
}

func (e *diagnosticError) Error() string { return e.err.Error() }
func (e *diagnosticError) Unwrap() error { return e.err }

// DiagnosticReported is intentionally structural so packages that cannot
// import infra (notably transfer) can still suppress duplicate fallback logs.
//
//wails:ignore
func (e *diagnosticError) DiagnosticReported() bool { return e != nil && e.reported }

// AnnotateError adds diagnostic context without logging yet. The final owner
// can add more context and emit a single record.
//
//wails:ignore
func AnnotateError(err error, diagnostic Diagnostic) error {
	if err == nil {
		return nil
	}
	return &diagnosticError{err: err, diagnostic: diagnostic}
}

// ReportError emits one diagnostic record and marks the returned error so a
// later Wails or transfer boundary does not emit it again.
//
//wails:ignore
func ReportError(log *Log, err error, where string, diagnostic Diagnostic) error {
	if err == nil {
		return nil
	}
	records, truncated, severity := collectDiagnosticCauses(err, diagnostic, false)
	if len(records) == 0 {
		return err
	}
	if log == nil {
		return AnnotateError(err, diagnostic)
	}
	merged := mergeDiagnostics(err, diagnostic)
	if severity == "" || truncated {
		severity = DiagnosticError
	}
	merged.Severity = severity
	record := make(map[string]any, len(merged.Fields)+3)
	for key, value := range records[0] {
		record[key] = value
	}
	for key, value := range merged.Fields {
		if key == "stack" || key == "stackTrace" {
			if stack, ok := value.(string); ok {
				value = limitDiagnosticText(stack, 16<<10)
			}
		}
		record[key] = value
	}
	if merged.Operation != "" {
		record["operation"] = merged.Operation
	}
	if merged.Stage != "" {
		record["stage"] = merged.Stage
	}
	record["error"] = records[0]["error"]
	if (merged.Stage != "" && records[0]["stage"] != merged.Stage) || (merged.Operation != "" && records[0]["operation"] != merged.Operation) {
		// Preserve the inner context without repeating the same error text.
		origin := make(map[string]any, len(records[0]))
		for key, value := range records[0] {
			if key != "error" {
				origin[key] = value
			}
		}
		origin["errorRef"] = "error"
		record["causes"] = append([]map[string]any{origin}, records[1:]...)
	} else if len(records) > 1 {
		record["causes"] = records[1:]
	}
	if truncated {
		record["causesTruncated"] = true
	}
	if severity == DiagnosticWarn {
		log.Warn(record, where)
	} else {
		log.Error(record, where)
	}
	return &diagnosticError{err: err, diagnostic: merged, reported: true}
}

// IsReportedError reports whether a domain boundary already emitted the error.
//
//wails:ignore
func IsReportedError(err error) bool {
	records, _, _ := collectDiagnosticCauses(err, Diagnostic{}, false)
	all, _, _ := collectDiagnosticCauses(err, Diagnostic{}, true)
	return len(records) == 0 && len(all) > 0
}

// IsCancellationError identifies normal user or shutdown cancellation, which
// is deliberately omitted from desktop.log.
//
//wails:ignore
func IsCancellationError(err error) bool {
	if err == nil {
		return false
	}
	records, truncated, _ := collectDiagnosticCauses(err, Diagnostic{}, true)
	return len(records) == 0 && !truncated
}

func isCancellationMessage(err error) bool {
	if named, ok := err.(interface{ Name() string }); ok && named.Name() == "AbortError" {
		return true
	}
	if err == context.Canceled { //nolint:errorlint // The bounded walker has already unwrapped this exact node.
		return true
	}
	message := strings.ToUpper(strings.TrimSpace(err.Error()))
	for _, code := range []string{
		"DRIVE_COPY_CANCELED",
		"GAMEBANANA_LOGIN_CANCELLED",
		"CUSTOM_DOWNLOAD_ABORTED",
		"CUSTOM_DOWNLOAD_CANCELED",
	} {
		if message == code || strings.HasPrefix(message, code+":") {
			return true
		}
	}
	return false
}

// ClassifyError supplies the common WARN/ERROR fallback policy. Domain owners
// should explicitly classify known validation and policy errors.
//
//wails:ignore
func ClassifyError(err error) DiagnosticSeverity {
	if err == nil {
		return DiagnosticError
	}
	var annotated *diagnosticError
	if errors.As(err, &annotated) && annotated.diagnostic.Severity != "" {
		return annotated.diagnostic.Severity
	}
	var classified interface{ DiagnosticSeverity() DiagnosticSeverity }
	if errors.As(err, &classified) {
		return classified.DiagnosticSeverity()
	}
	if errors.Is(err, os.ErrPermission) {
		return DiagnosticWarn
	}
	if errors.Is(err, os.ErrNotExist) {
		return DiagnosticWarn
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		return DiagnosticWarn
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.Status >= 400 && httpErr.Status < 500 {
		return DiagnosticWarn
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return DiagnosticError
	}
	if isExpectedErrorMessage(err.Error()) {
		return DiagnosticWarn
	}
	return DiagnosticError
}

func isExpectedErrorMessage(message string) bool {
	upper := strings.ToUpper(strings.TrimSpace(message))
	if upper == "" {
		return false
	}
	for _, fragment := range []string{
		"NOT FOUND", "NOT_FOUND", "IS REQUIRED", "_REQUIRED",
		"NOT WRITABLE", "PERMISSION DENIED", "ACCESS DENIED", "UNAUTHORIZED", "FORBIDDEN",
		"UNSUPPORTED", "NOT SUPPORTED", "CONFLICT", "ALREADY EXISTS", "_IN_PROGRESS",
		"NO_UPLOADABLE_FILES", "DOWNLOAD_URL_REQUIRED", "PATH IS NOT WRITABLE",
	} {
		if strings.Contains(upper, fragment) {
			return true
		}
	}
	return false
}

// ServiceErrorMarshaler is the final diagnostic boundary for synchronous
// Wails calls. It preserves the JSON representation of the original returned
// error while logging an unreported failure once.
//
//wails:ignore
func (l *Log) ServiceErrorMarshaler(service string) func(error) []byte {
	return func(err error) []byte {
		if err == nil {
			return nil
		}
		if !IsCancellationError(err) && !IsReportedError(err) {
			diagnostic := mergeDiagnostics(err, Diagnostic{Fields: map[string]any{"service": service}})
			if diagnostic.Operation == "" {
				diagnostic.Operation = "wails-call"
			}
			_ = ReportError(l, err, service, diagnostic)
		}
		original := originalDiagnosticError(err)
		data, marshalErr := json.Marshal(&original)
		if marshalErr != nil {
			return nil
		}
		if string(data) == "{}" {
			data, marshalErr = json.Marshal(original.Error())
			if marshalErr != nil {
				return nil
			}
		}
		return data
	}
}

func mergeDiagnostics(err error, extra Diagnostic) Diagnostic {
	chain := make([]Diagnostic, 0, 2)
	visitDiagnosticErrors(err, func(annotated *diagnosticError) {
		chain = append(chain, annotated.diagnostic)
	})
	merged := Diagnostic{Fields: map[string]any{}}
	for index := len(chain) - 1; index >= 0; index-- {
		mergeDiagnostic(&merged, chain[index])
	}
	mergeDiagnostic(&merged, extra)
	return merged
}

func visitDiagnosticErrors(err error, visit func(*diagnosticError)) {
	// Only merge the outer linear chain. Sibling metadata belongs to its cause.
	for range 32 {
		if err == nil {
			return
		}
		if annotated, ok := err.(*diagnosticError); ok { //nolint:errorlint
			if annotated.reported {
				return
			}
			visit(annotated)
		}
		err = errors.Unwrap(err)
	}
}

func mergeDiagnostic(target *Diagnostic, source Diagnostic) {
	if source.Severity != "" {
		target.Severity = source.Severity
	}
	if source.Operation != "" {
		target.Operation = source.Operation
	}
	if source.Stage != "" {
		target.Stage = source.Stage
	}
	if target.Fields == nil {
		target.Fields = make(map[string]any)
	}
	for key, value := range source.Fields {
		target.Fields[key] = value
	}
}

func originalDiagnosticError(err error) error {
	for range 32 {
		// Only peel our outer marker. Peeling arbitrary wrapped causes would
		// change the JSON shape Wails exposes to the renderer.
		annotated, ok := err.(*diagnosticError) //nolint:errorlint
		if !ok || annotated == nil || annotated.err == nil {
			return err
		}
		err = annotated.err
	}
	return err
}
