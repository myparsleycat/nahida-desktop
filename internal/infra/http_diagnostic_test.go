package infra

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	for _, want := range []string{" WARN ", "probe", "text/html", `"status":200`, "invalid character"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %s: %s", want, output.String())
		}
	}
}

func TestProbeTransportFailureStaysOffDesktopLog(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "desktop.log")
	log := NewLogWithOptions(LogOptions{Dest: dest, Writer: io.Discard})
	t.Cleanup(func() { _ = log.Close() })
	client := testClient(t, ClientOptions{
		Log:        log,
		BackendURL: "https://api.nahida.live",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("wsarecv: An existing connection was forcibly closed by the remote host.")
		})},
	})
	if status := client.Probe(context.Background()); status != BackendOffline {
		t.Fatalf("status = %s", status)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		data, _ := os.ReadFile(dest)
		if len(bytes.TrimSpace(data)) != 0 {
			t.Fatalf("wrote desktop.log: %s", data)
		}
	}
}

func TestProbeTransportFailureIsDebug(t *testing.T) {
	var output bytes.Buffer
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
	log.SetLevel("debug")
	client := testClient(t, ClientOptions{
		Log:        log,
		BackendURL: "https://api.nahida.live",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("wsarecv: An existing connection was forcibly closed by the remote host.")
		})},
	})
	if status := client.Probe(context.Background()); status != BackendOffline {
		t.Fatalf("status = %s", status)
	}
	got := output.String()
	if !strings.Contains(got, " DEBUG ") || !strings.Contains(got, `"operation":"probe"`) || strings.Contains(got, " WARN ") || strings.Contains(got, " ERROR ") {
		t.Fatalf("record = %s", got)
	}
}

func TestProbeNonReachabilityURLErrorIsWarn(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "certificate", err: x509.UnknownAuthorityError{}},
		{name: "scheme-mismatch", err: http.ErrSchemeMismatch},
		{name: "invalid-host", err: url.InvalidHostError("example")},
		{name: "escape", err: url.EscapeError("%")},
		{name: "redirect-loop", err: &url.Error{Op: "Get", URL: "https://api.nahida.live/status", Err: errors.New("stopped after 10 redirects")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true})
			log.SetLevel("debug")
			client := testClient(t, ClientOptions{
				Log:        log,
				BackendURL: "https://api.nahida.live",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return nil, test.err
				})},
			})
			if status := client.Probe(context.Background()); status != BackendOffline {
				t.Fatalf("status = %s", status)
			}
			got := output.String()
			if !strings.Contains(got, " WARN ") || !strings.Contains(got, `"operation":"probe"`) || strings.Contains(got, " DEBUG ") || strings.Contains(got, " ERROR ") {
				t.Fatalf("record = %s", got)
			}
		})
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
