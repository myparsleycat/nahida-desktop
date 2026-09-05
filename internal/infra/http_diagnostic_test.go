package infra

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProbeFailureRecordsStatusAndKeepsDecision(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	client := testClient(t, ClientOptions{Log: log, BackendURL: "https://example.com", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{Request: req, StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<html>"))}, nil
	})}})
	status := client.Probe(context.Background())
	if status != BackendOnline {
		t.Fatalf("decision changed: %s", status)
	}
	for _, want := range []string{"probe", "text/html", `"status":200`, "invalid character"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %s: %s", want, output.String())
		}
	}
}

func TestTimeoutRewritePreservesOriginalMetadata(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	client := testClient(t, ClientOptions{Log: log})
	req, err := http.NewRequest(http.MethodGet, "https://example.com/items?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	response := client.rewriteTimeout(&http.Response{Request: req, StatusCode: 524, Header: http.Header{"Content-Type": {"text/html"}, "Cf-Ray": {"request-id"}}, Body: io.NopCloser(strings.NewReader("timeout"))})
	if response.StatusCode != 200 {
		t.Fatal("rewrite behavior changed")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"524", "rewrite-timeout", "request-id"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %s: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "secret") {
		t.Fatal("query was logged")
	}
}

func TestUnauthorizedBodyReadFailureIsRecorded(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	client := testClient(t, ClientOptions{Log: log})
	client.afterUnauthorized("https://example.com/items", true, false, &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(diagnosticBrokenReader{})})
	if !strings.Contains(output.String(), "injected response read failure") {
		t.Fatal(output.String())
	}
}

type diagnosticBrokenReader struct{}

func (diagnosticBrokenReader) Read([]byte) (int, error) {
	return 0, errors.New("injected response read failure")
}
