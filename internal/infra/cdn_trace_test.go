package infra

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCDNTraceGetReturnsBody(t *testing.T) {
	t.Parallel()

	const body = "fl=966f38\nh=api.nahida.live\nip=39.125.175.15\n"
	var gotReq *http.Request
	c := testClient(t, ClientOptions{
		Status: BackendOffline,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotReq = r.Clone(r.Context())
			return textResp(r, http.StatusOK, body), nil
		}),
	})

	got, err := NewCDNTrace(c).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != body {
		t.Fatalf("body = %q", got)
	}
	if gotReq == nil {
		t.Fatal("request was not sent")
	}
	if gotReq.Method != http.MethodGet {
		t.Fatalf("method = %q", gotReq.Method)
	}
	if gotReq.URL.String() != "https://api.nahida.live/cdn-cgi/trace" {
		t.Fatalf("url = %q", gotReq.URL)
	}
	if gotReq.Header.Get("User-Agent") != "Nahida Desktop/test-version" {
		t.Fatalf("user-agent = %q", gotReq.Header.Get("User-Agent"))
	}
	if gotReq.Header.Get("Authorization") != "" {
		t.Fatalf("authorization = %q", gotReq.Header.Get("Authorization"))
	}
	if c.GetStatus() != BackendOffline {
		t.Fatalf("status = %q, want %q", c.GetStatus(), BackendOffline)
	}
}

func TestCDNTraceGetRejectsNon2xx(t *testing.T) {
	t.Parallel()

	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, http.StatusServiceUnavailable, "nope"), nil
		}),
	})
	_, err := NewCDNTrace(c).Get(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("error = %v", err)
	}
}

func TestCDNTraceGetRejectsTransportError(t *testing.T) {
	t.Parallel()

	want := errors.New("dial boom")
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, want
		}),
	})
	_, err := NewCDNTrace(c).Get(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestCDNTraceGetRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, http.StatusOK, strings.Repeat("x", cdnTraceMaxBody+1)), nil
		}),
	})
	_, err := NewCDNTrace(c).Get(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
	}
}

func TestCDNTraceGetRequiresClient(t *testing.T) {
	t.Parallel()

	_, err := (*CDNTrace)(nil).Get(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = NewCDNTrace(nil).Get(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCDNTraceGetUsesConfiguredBackendURL(t *testing.T) {
	t.Parallel()

	var gotURL string
	c := testClient(t, ClientOptions{
		BackendURL: "https://edge.example.test",
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotURL = r.URL.String()
			return textResp(r, http.StatusOK, "colo=NRT\n"), nil
		}),
	})
	if _, err := NewCDNTrace(c).Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotURL != "https://edge.example.test/cdn-cgi/trace" {
		t.Fatalf("url = %q", gotURL)
	}
}

func TestCDNTraceGetDoesNotCallTokenLookup(t *testing.T) {
	t.Parallel()

	c := testClient(t, ClientOptions{
		Token: func() (string, error) {
			t.Fatal("token lookup should not run")
			return "", nil
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, http.StatusOK, "warp=off\n"), nil
		}),
	})
	if _, err := NewCDNTrace(c).Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestCDNTraceGetClosesResponseBody(t *testing.T) {
	t.Parallel()

	body := &closeTrackingBody{Reader: strings.NewReader("loc=KR\n")}
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp := textResp(r, http.StatusOK, "")
			resp.Body = body
			return resp, nil
		}),
	})
	if _, err := NewCDNTrace(c).Get(context.Background()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
}

func TestCDNTraceGetClosesOversizedBodyWithoutDraining(t *testing.T) {
	t.Parallel()

	trailing := &unreadTrailingReader{}
	body := &closeTrackingBody{Reader: io.MultiReader(
		strings.NewReader(strings.Repeat("x", cdnTraceMaxBody+1)),
		trailing,
	)}
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp := textResp(r, http.StatusOK, "")
			resp.Body = body
			return resp, nil
		}),
	})
	_, err := NewCDNTrace(c).Get(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v", err)
	}
	if !body.closed {
		t.Fatal("response body was not closed")
	}
	if trailing.reads != 0 {
		t.Fatalf("trailing reads = %d", trailing.reads)
	}
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

type unreadTrailingReader struct {
	reads int
}

func (r *unreadTrailingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("trailing data was drained")
}
