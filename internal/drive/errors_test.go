package drive

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestDiagnosticWrapperPreservesDriveAPIErrorJSON(t *testing.T) {
	t.Parallel()

	apiErr := newDriveAPIError("DRIVE_POLICY", "rejected", 422, nil)
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: io.Discard, DisableFile: true})
	wrapped := infra.ReportError(log, apiErr, "Drive", infra.Diagnostic{Operation: "upload", Stage: "plan/file_validation"})
	got := log.ServiceErrorMarshaler("Drive")(wrapped)
	var original error = apiErr
	want, err := json.Marshal(&original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("marshaled = %s, want %s", got, want)
	}
}

func TestCreateDriveAPIErrorPreservesExisting(t *testing.T) {
	t.Parallel()

	original := newDriveAPIError("DRIVE_CUSTOM", "already normalized", 0, nil)
	got := CreateDriveAPIError(original, "copy", 0)
	if got != original {
		t.Fatalf("did not preserve existing DriveAPIError")
	}
}

func TestCreateDriveAPIErrorExtractsNestedAPICodes(t *testing.T) {
	t.Parallel()

	got := CreateDriveAPIError(map[string]any{
		"value": map[string]any{"code": "MISSING_PASSWORD", "message": "Password required"},
	}, "linkAccess", 401)
	if got.Code != codeLinkPasswordRequired {
		t.Fatalf("code = %q", got.Code)
	}
	if got.Status != 401 {
		t.Fatalf("status = %d", got.Status)
	}
	if got.Error() != "DRIVE_LINK_PASSWORD_REQUIRED: Password required" {
		t.Fatalf("error = %q", got.Error())
	}
}

func TestCreateDriveAPIErrorNormalizesInvalidPasswordCode(t *testing.T) {
	t.Parallel()

	got := CreateDriveAPIError(map[string]any{"code": "INVALID_PASSWORD"}, "linkAccess", 0)
	if got.Code != codeLinkInvalidPassword {
		t.Fatalf("code = %q", got.Code)
	}
}

func TestCreateDriveAPIErrorNormalizesPasswordMessages(t *testing.T) {
	t.Parallel()

	got := CreateDriveAPIError("password required", "mod overview", 401)
	if got.Code != codeLinkPasswordRequired {
		t.Fatalf("password required code = %q", got.Code)
	}
	got = CreateDriveAPIError(map[string]any{"error": "incorrect password"}, "mod overview", 500)
	if got.Code != codeLinkInvalidPassword {
		t.Fatalf("incorrect password code = %q", got.Code)
	}
}

func TestCreateDriveAPIErrorOperationFallback(t *testing.T) {
	t.Parallel()

	got := CreateDriveAPIError(map[string]any{}, "copyFromUrl", 0)
	if got.Code != "DRIVE_COPYFROMURL_FAILED" {
		t.Fatalf("code = %q", got.Code)
	}
	if got.Error() != "DRIVE_COPYFROMURL_FAILED: Unknown error" {
		t.Fatalf("error = %q", got.Error())
	}

	got = CreateDriveAPIError(map[string]any{}, "shared link access", 0)
	if got.Code != "DRIVE_SHARED_LINK_ACCESS_FAILED" {
		t.Fatalf("multiword code = %q", got.Code)
	}
}

func TestCreateDriveAPIErrorMapsBackendUnavailable(t *testing.T) {
	t.Parallel()

	for _, status := range []int{502, 503, 504} {
		got := CreateDriveAPIError("upstream", "get:item", status)
		if got.Code != codeBackendUnavailable {
			t.Fatalf("status %d code = %q", status, got.Code)
		}
		if got.Message != msgBackendUnavailable {
			t.Fatalf("status %d message = %q", status, got.Message)
		}
		if got.Status != status {
			t.Fatalf("status = %d", got.Status)
		}
	}

	got := CreateDriveAPIError(&infra.APIError{Code: "X", Message: "no", Status: 503}, "get:item", 0)
	if got.Code != codeBackendUnavailable || got.Status != 503 {
		t.Fatalf("APIError 503 = %+v", got)
	}
}

func TestDriveConversionKeepsDiagnosticSource(t *testing.T) {
	var output bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	original := newDriveAPIError("DRIVE_POLICY", "rejected", 422, nil)
	wrapper := infra.AnnotateError(infra.WithCause(original, errors.New("source response was truncated")), infra.Diagnostic{Stage: "read-response"})
	converted := CreateDriveAPIError(wrapper, "upload", 0)
	again := CreateDriveAPIError(infra.AnnotateError(converted, infra.Diagnostic{Stage: "outer"}), "upload", 0)
	if !errors.Is(again, original) {
		t.Fatal("nested normalization lost identity")
	}
	if !errors.Is(converted, original) {
		t.Fatal("lost original domain error identity")
	}
	expected, _ := json.Marshal(original)
	actual, _ := json.Marshal(converted)
	if !bytes.Equal(expected, actual) || converted.Error() != original.Error() {
		t.Fatal("domain wire contract changed")
	}
	log.ServiceErrorMarshaler("Drive")(converted)
	if !strings.Contains(output.String(), "source response was truncated") || !strings.Contains(output.String(), "read-response") {
		t.Fatal(output.String())
	}
}
