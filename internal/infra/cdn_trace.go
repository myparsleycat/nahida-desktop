package infra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	cdnTracePath    = "/cdn-cgi/trace"
	cdnTraceMaxBody = 16 << 10
)

// CDNTrace is the Wails-facing diagnostic for Cloudflare cdn-cgi/trace
// through the app HTTP client.
type CDNTrace struct {
	http *Client
}

func NewCDNTrace(httpClient *Client) *CDNTrace {
	return &CDNTrace{http: httpClient}
}

func (t *CDNTrace) Get(ctx context.Context) (raw string, returnErr error) {
	if t == nil || t.http == nil {
		return "", errors.New("http client unavailable")
	}
	c := t.http
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	endpoint := strings.TrimRight(c.BackendURL(), "/") + cdnTracePath
	stage := "prepare"
	var diagnosticResponse *http.Response
	defer func() {
		returnErr = AnnotateError(returnErr, HTTPDiagnostic(http.MethodGet, endpoint, stage, diagnosticResponse))
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.userAgent())

	stage = "request"
	resp, err := c.HTTPClient().Do(req)
	diagnosticResponse = resp
	if err != nil {
		return "", err
	}
	defer drainClose(resp.Body)

	stage = "response"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &HTTPError{Response: resp, Status: resp.StatusCode}
	}

	stage = "read"
	body, err := io.ReadAll(io.LimitReader(resp.Body, cdnTraceMaxBody+1))
	if err != nil {
		return "", err
	}
	if len(body) > cdnTraceMaxBody {
		return "", fmt.Errorf("cdn-cgi/trace response exceeds %d bytes", cdnTraceMaxBody)
	}

	return string(body), nil
}
