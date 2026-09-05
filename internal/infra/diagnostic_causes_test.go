package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestDiagnosticHiddenCausePreservesContract(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	public := errors.New("LOGIN_FAILED")
	cause := errors.New("read response: unexpected EOF")
	err := AnnotateError(WithCause(public, cause), Diagnostic{Operation: "login", Stage: "decode"})
	if !errors.Is(err, public) || errors.Is(err, cause) || err.Error() != public.Error() {
		t.Fatal("diagnostic cause changed the public contract")
	}
	var message string
	if decodeErr := json.Unmarshal(log.ServiceErrorMarshaler("Auth")(err), &message); decodeErr != nil || message != public.Error() {
		t.Fatalf("wire error = %q, %v", message, decodeErr)
	}
	for _, expected := range []string{"LOGIN_FAILED", "unexpected EOF", "decode", "causes"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("missing %q in %s", expected, output.String())
		}
	}
}

func TestDiagnosticJoinedFailuresDoNotDisappear(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	first := ReportError(log, errors.New("already recorded"), "Test", Diagnostic{})
	output.Reset()
	err := errors.Join(first, context.Canceled, AnnotateError(errors.New("restore denied"), Diagnostic{Stage: "rollback"}))
	if IsCancellationError(err) || IsReportedError(err) {
		t.Fatal("new rollback failure was suppressed")
	}
	_ = ReportError(log, err, "Test", Diagnostic{})
	if !strings.Contains(output.String(), "restore denied") || strings.Contains(output.String(), "already recorded") || strings.Contains(output.String(), "context canceled") {
		t.Fatalf("unexpected record: %s", output.String())
	}
	if !IsCancellationError(errors.Join(context.Canceled, errors.New("DRIVE_COPY_CANCELED"))) {
		t.Fatal("pure cancellation is not silent")
	}
}

func TestDiagnosticSiblingStagesAndLimits(t *testing.T) {
	err := errors.Join(
		AnnotateError(errors.New("primary"), Diagnostic{Stage: "write", Fields: map[string]any{"path": "first"}}),
		AnnotateError(errors.New(strings.Repeat("x", 5000)), Diagnostic{Stage: "rollback", Fields: map[string]any{"path": "second"}}),
	)
	records, _, _ := collectDiagnosticCauses(err, Diagnostic{}, false)
	if len(records) != 2 || records[0]["stage"] != "write" || records[1]["stage"] != "rollback" || records[0]["path"] != "first" {
		t.Fatalf("sibling context lost: %#v", records)
	}
	if len(records[1]["error"].(string)) > 4200 {
		t.Fatal("message was not bounded")
	}
	cycle := &diagnosticCycleError{}
	cycle.cause = cycle
	_, truncated, _ := collectDiagnosticCauses(cycle, Diagnostic{}, false)
	if !truncated {
		t.Fatal("cycle was not bounded")
	}
}

type diagnosticCycleError struct{ cause error }

func (*diagnosticCycleError) Error() string   { return "cycle" }
func (e *diagnosticCycleError) Unwrap() error { return e.cause }

func TestDiagnosticRedactsNestedCauses(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	err := WithCause(errors.New("public failure"), errors.New("GET https://user:pass@example.com/login?state=private-state&code=private-code: denied"))
	_ = ReportError(log, err, "Test", Diagnostic{Fields: map[string]any{"state": "private-state", "cookie": "session=private-cookie", "stack": strings.Repeat("x", 20<<10)}})
	for _, secret := range []string{"user:pass", "private-state", "private-code", "private-cookie"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("leaked %q", secret)
		}
	}
	if strings.Contains(output.String(), strings.Repeat("x", 17<<10)) {
		t.Fatal("stack limit was bypassed")
	}
}

func TestDiagnosticQuotedURLPreservesJSON(t *testing.T) {
	for _, address := range []string{
		"https://example.com/file",
		"https://user:private-password@example.com/file?token=private-token&state=private-state#private-fragment",
	} {
		t.Run(address, func(t *testing.T) {
			var output bytes.Buffer
			log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
			failure := &url.Error{Op: "Get", URL: address, Err: errors.New("network failed")}
			_ = ReportError(log, WithCause(errors.New("download failed"), failure), "Test", Diagnostic{Stage: "request"})
			line := output.String()
			var record struct {
				Causes []struct {
					Error string `json:"error"`
				} `json:"causes"`
			}
			if err := json.Unmarshal([]byte(line[strings.Index(line, "{"):]), &record); err != nil {
				t.Fatalf("invalid log JSON: %v: %s", err, line)
			}
			if len(record.Causes) != 2 || record.Causes[0].Error != `Get "https://example.com/file": network failed` {
				t.Fatalf("quoted error lost: %#v", record)
			}
			if strings.Contains(line, "private-") {
				t.Fatalf("secret leaked: %s", line)
			}
		})
	}
}

func TestDiagnosticRemainingBranchSeverity(t *testing.T) {
	for _, wrapper := range []string{"join", "wrapped-join", "hidden-cause"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%t", wrapper, explicit), func(t *testing.T) {
				var output bytes.Buffer
				log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
				first := ReportError(log, errors.New("expected validation"), "Test", Diagnostic{Severity: DiagnosticWarn})
				output.Reset()
				log.SetLevel("error")
				diagnostic := Diagnostic{Stage: "rollback"}
				if explicit {
					diagnostic.Severity = DiagnosticError
				}
				cleanup := AnnotateError(errors.New("rollback I/O failure"), diagnostic)
				err := errors.Join(first, cleanup)
				if wrapper == "wrapped-join" {
					err = fmt.Errorf("request failed: %w", err)
				}
				if wrapper == "hidden-cause" {
					err = WithCause(first, cleanup)
				}
				reported := ReportError(log, err, "Test", Diagnostic{})
				if !strings.Contains(output.String(), " ERROR ") || !strings.Contains(output.String(), "rollback I/O failure") {
					t.Fatalf("new failure was suppressed: %s", output.String())
				}
				output.Reset()
				_ = ReportError(log, reported, "Test", Diagnostic{})
				if output.Len() != 0 {
					t.Fatalf("duplicate failure: %s", output.String())
				}
			})
		}
	}
}

func TestDiagnosticServiceBoundaryIgnoresReportedMetadata(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	first := ReportError(log, errors.New("public failure"), "Test", Diagnostic{Severity: DiagnosticWarn, Stage: "validation"})
	output.Reset()
	log.SetLevel("error")
	err := WithCause(first, AnnotateError(errors.New("rollback failed"), Diagnostic{Severity: DiagnosticError, Stage: "rollback"}))
	var public string
	if decodeErr := json.Unmarshal(log.ServiceErrorMarshaler("Test")(err), &public); decodeErr != nil || public != "public failure" {
		t.Fatalf("wire contract changed: %q, %v", public, decodeErr)
	}
	line := output.String()
	if !strings.Contains(line, " ERROR ") || !strings.Contains(line, "rollback failed") || !strings.Contains(line, `"stage":"rollback"`) {
		t.Fatalf("missing cleanup diagnosis: %s", line)
	}
	if strings.Contains(line, "public failure") || strings.Contains(line, "validation") {
		t.Fatalf("already reported context leaked into new failure: %s", line)
	}
}

func TestDiagnosticSeverityPreservesActivePolicies(t *testing.T) {
	for _, test := range []struct {
		name       string
		failure    func(*Log) error
		diagnostic Diagnostic
		level      string
	}{
		{name: "remaining-warning", failure: func(log *Log) error {
			first := ReportError(log, errors.New("old error"), "Test", Diagnostic{Severity: DiagnosticError})
			return errors.Join(first, AnnotateError(errors.New("new warning"), Diagnostic{Severity: DiagnosticWarn}))
		}, level: "WARN"},
		{name: "fallback-policy", failure: func(*Log) error { return fmt.Errorf("retrying: %w", errors.New("network failed")) }, diagnostic: Diagnostic{Severity: DiagnosticWarn}, level: "WARN"},
		{name: "mixed-branches", failure: func(*Log) error {
			return errors.Join(AnnotateError(errors.New("validation"), Diagnostic{Severity: DiagnosticWarn}), AnnotateError(errors.New("rollback failed"), Diagnostic{Severity: DiagnosticError}))
		}, level: "ERROR"},
		{name: "error-owner", failure: func(*Log) error { return AnnotateError(errors.New("validation"), Diagnostic{Severity: DiagnosticWarn}) }, diagnostic: Diagnostic{Severity: DiagnosticError}, level: "ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
			failure := test.failure(log)
			output.Reset()
			_ = ReportError(log, failure, "Test", test.diagnostic)
			if !strings.Contains(output.String(), " "+test.level+" ") {
				t.Fatalf("wrong level: %s", output.String())
			}
		})
	}
}
