package infra

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

func testClient(t *testing.T, opts ClientOptions) *Client {
	t.Helper()
	if opts.Version == "" {
		opts.Version = "test-version"
	}
	if opts.RetryLimit == nil {
		zero := 0
		opts.RetryLimit = &zero
	}
	if opts.RetryWait == nil {
		none := time.Duration(0)
		opts.RetryWait = &none
	}
	if opts.Status == "" {
		opts.Status = BackendOnline
	}
	if opts.Token == nil {
		opts.Token = func() (string, error) { return "stored-token", nil }
	}
	return NewClientWithOptions(opts)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type probeReader struct {
	onRead func()
	value  string
	offset int
}

func (r *probeReader) Read(buffer []byte) (int, error) {
	if r.offset >= len(r.value) {
		return 0, io.EOF
	}
	if r.onRead != nil {
		r.onRead()
		r.onRead = nil
	}
	written := copy(buffer, r.value[r.offset:])
	r.offset += written
	return written, nil
}

func textResp(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func closeBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
}

func closeHTTPErr(t *testing.T, err error) {
	t.Helper()
	var he *HTTPError
	if errors.As(err, &he) {
		closeBody(t, he.Response)
	}
}

func TestSetStatusNotifiesOnChangeOnly(t *testing.T) {
	t.Parallel()

	var got []BackendStatus
	c := testClient(t, ClientOptions{Status: BackendOnline})
	c.UseOnStatus(func(s BackendStatus) { got = append(got, s) })
	c.SetOnline()
	c.SetOffline()
	c.SetOffline()
	if len(got) != 1 || got[0] != BackendOffline {
		t.Fatalf("notifications = %v", got)
	}
}

func TestFetchKeepsCallerAuthorization(t *testing.T) {
	t.Parallel()

	var got http.Header
	tokenCalls := 0
	c := testClient(t, ClientOptions{
		Token: func() (string, error) {
			tokenCalls++
			return "stored-token", nil
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			got = r.Header.Clone()
			return textResp(r, 200, "ok"), nil
		}),
	})

	h := make(http.Header)
	h.Set("Authorization", "Bearer pinned-token")
	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/auth/get-session", FetchOptions{
		Header: h,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)

	if tokenCalls != 0 {
		t.Fatalf("token lookup called %d times", tokenCalls)
	}
	if got.Get("Authorization") != "Bearer pinned-token" {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("User-Agent") != "Nahida Desktop/test-version" {
		t.Fatalf("User-Agent = %q", got.Get("User-Agent"))
	}
}

func TestFetchResolvesAuthorizationFromToken(t *testing.T) {
	t.Parallel()

	var got http.Header
	tokenCalls := 0
	c := testClient(t, ClientOptions{
		Token: func() (string, error) {
			tokenCalls++
			return "stored-token", nil
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			got = r.Header.Clone()
			return textResp(r, 200, "ok"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)

	if tokenCalls != 1 {
		t.Fatalf("token lookup called %d times", tokenCalls)
	}
	if got.Get("Authorization") != "Bearer stored-token" {
		t.Fatalf("Authorization = %q", got.Get("Authorization"))
	}
	if got.Get("User-Agent") != "Nahida Desktop/test-version" {
		t.Fatalf("User-Agent = %q", got.Get("User-Agent"))
	}
}

func TestFetchPreservesCallerUserAgent(t *testing.T) {
	t.Parallel()

	const want = "custom-browser-user-agent"
	var got string
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			got = r.Header.Get("User-Agent")
			return textResp(r, http.StatusOK, "ok"), nil
		}),
	})
	header := make(http.Header)
	header.Set("User-Agent", want)
	response, err := c.Fetch(context.Background(), "https://example.com/resource", FetchOptions{Header: header})
	if err != nil {
		t.Fatal(err)
	}
	closeBody(t, response)
	if got != want {
		t.Fatalf("User-Agent = %q, want %q", got, want)
	}
}

func TestStreamDoesNotPrebufferBody(t *testing.T) {
	t.Parallel()
	readStarted := false
	body := &probeReader{onRead: func() { readStarted = true }, value: "streamed"}
	client := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if readStarted {
				t.Fatal("body was read before transport received the request")
			}
			got, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "streamed" || request.ContentLength != int64(len("streamed")) {
				t.Fatalf("body = %q, content length = %d", got, request.ContentLength)
			}
			return textResp(request, http.StatusOK, "ok"), nil
		}),
	})
	response, err := client.Stream(
		context.Background(),
		"https://uploads.example/file",
		http.MethodPost,
		nil,
		body,
		int64(len("streamed")),
	)
	if err != nil {
		t.Fatal(err)
	}
	closeBody(t, response)
}

func TestFetchNormalizesNHDAPIResponseCBORToJSON(t *testing.T) {
	t.Parallel()

	body, err := cbor.Marshal(map[string]any{
		"results": []any{map[string]any{"intentId": "intent", "status": "completed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response := textResp(request, http.StatusOK, string(body))
			response.Header.Set("Content-Type", "application/cbor; charset=binary")
			response.ContentLength = int64(len(body))
			return response, nil
		}),
	})
	response, err := client.Fetch(context.Background(), "https://api.nahida.live/akasha/v2/uploads:pack", FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(t, response)
	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var payload struct {
		Results []struct {
			IntentID string `json:"intentId"`
			Status   string `json:"status"`
		} `json:"results"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Results) != 1 || payload.Results[0].IntentID != "intent" || payload.Results[0].Status != "completed" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestFetchLeavesNonNHDAPIResponseCBORUnchanged(t *testing.T) {
	t.Parallel()

	body, err := cbor.Marshal(map[string]any{"value": "raw-cbor"})
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response := textResp(request, http.StatusOK, string(body))
			response.Header.Set("Content-Type", "application/cbor")
			return response, nil
		}),
	})
	response, err := client.Fetch(context.Background(), "https://example.com/data.cbor", FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(t, response)
	if got := response.Header.Get("Content-Type"); got != "application/cbor" {
		t.Fatalf("Content-Type = %q", got)
	}
	got, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("body changed: %x != %x", got, body)
	}
}

func TestStreamNormalizesNHDAPIResponseCBORToJSON(t *testing.T) {
	t.Parallel()

	body, err := cbor.Marshal(map[string]any{"status": "completed"})
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			_, _ = io.Copy(io.Discard, request.Body)
			response := textResp(request, http.StatusOK, string(body))
			response.Header.Set("Content-Type", "application/cbor")
			return response, nil
		}),
	})
	response, err := client.Stream(
		context.Background(),
		"https://api.nahida.live/akasha/v2/uploads:pack",
		http.MethodPost,
		nil,
		strings.NewReader("payload"),
		int64(len("payload")),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(t, response)
	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "completed" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestFetchRejectsInvalidNHDAPICBOR(t *testing.T) {
	t.Parallel()

	client := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response := textResp(request, http.StatusOK, "\xff\xff")
			response.Header.Set("Content-Type", "application/cbor")
			return response, nil
		}),
	})
	response, err := client.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	closeBody(t, response)
	if err == nil || !strings.Contains(err.Error(), "decode CBOR response") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchRetriesInvalidNHDAPICBORAsJSON(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	client := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `{"value":"same-body"}` {
				t.Fatalf("body = %q", body)
			}
			if request.Header.Get("Authorization") != "Bearer stored-token" {
				t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
			}
			if requests.Add(1) == 1 {
				response := textResp(request, http.StatusOK, "\xff\xff")
				response.Header.Set("Content-Type", "application/cbor")
				return response, nil
			}
			if request.URL.Query().Get("res") != "json" {
				t.Fatalf("fallback URL = %q", request.URL)
			}
			response := textResp(request, http.StatusOK, `{"value":"json-fallback"}`)
			response.Header.Set("Content-Type", "application/json")
			return response, nil
		}),
	})
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	response, err := client.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{
		Method: http.MethodPost,
		Header: header,
		Body:   strings.NewReader(`{"value":"same-body"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(t, response)
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["value"] != "json-fallback" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestFetchSkips401RefreshForCBORPasswordRequired(t *testing.T) {
	t.Parallel()

	body, err := cbor.Marshal(map[string]any{"code": "password_required"})
	if err != nil {
		t.Fatal(err)
	}
	refreshed := 0
	client := testClient(t, ClientOptions{
		RefreshSession: func() error {
			refreshed++
			return nil
		},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response := textResp(request, http.StatusUnauthorized, string(body))
			response.Header.Set("Content-Type", "application/cbor")
			return response, nil
		}),
	})
	response, err := client.Fetch(context.Background(), "https://api.nahida.live/akasha/link/id", FetchOptions{
		DisableHTTPErrors: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeBody(t, response)
	if refreshed != 0 {
		t.Fatalf("refresh called %d times", refreshed)
	}
}

func TestGetHeadersOmitsBearerOnNonNHD(t *testing.T) {
	t.Parallel()

	c := testClient(t, ClientOptions{})
	h, err := c.GetHeaders("https://github.com/rate_limit")
	if err != nil {
		t.Fatalf("GetHeaders: %v", err)
	}
	if h.Get("Authorization") != "" {
		t.Fatalf("Authorization = %q", h.Get("Authorization"))
	}
	if h.Get("User-Agent") != "Nahida Desktop/test-version" {
		t.Fatalf("User-Agent = %q", h.Get("User-Agent"))
	}
}

func TestGetHeadersDoesNotLeakBearerToPrefixLookalike(t *testing.T) {
	t.Parallel()
	c := testClient(t, ClientOptions{Token: func() (string, error) {
		return "stored-token", nil
	}})
	for _, rawURL := range []string{
		"https://api.nahida.live.attacker.invalid/file",
		"https://api.nahida.live@attacker.invalid/file",
		"http://localhost.attacker.invalid/file",
		"https://localhost/file",
		"http://api.nahida.live/file",
	} {
		header, err := c.GetHeaders(rawURL)
		if err != nil {
			t.Fatalf("GetHeaders(%q): %v", rawURL, err)
		}
		if got := header.Get("Authorization"); got != "" {
			t.Fatalf("GetHeaders(%q) Authorization = %q", rawURL, got)
		}
	}
}

func TestGetHeadersAllowsExactNHDOrigins(t *testing.T) {
	t.Parallel()
	c := testClient(t, ClientOptions{Token: func() (string, error) {
		return "stored-token", nil
	}})
	for _, rawURL := range []string{
		"https://api.nahida.live/file",
		"https://api.nahida.live:443/file",
		"http://localhost/file",
		"http://localhost:3000/file",
	} {
		header, err := c.GetHeaders(rawURL)
		if err != nil {
			t.Fatalf("GetHeaders(%q): %v", rawURL, err)
		}
		if got := header.Get("Authorization"); got != "Bearer stored-token" {
			t.Fatalf("GetHeaders(%q) Authorization = %q", rawURL, got)
		}
	}
}

func TestFetchShortCircuitsWhenOffline(t *testing.T) {
	t.Parallel()

	called := false
	tokenCalls := 0
	c := testClient(t, ClientOptions{
		Status: BackendOffline,
		Token: func() (string, error) {
			tokenCalls++
			return "stored-token", nil
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			return textResp(r, 200, "ok"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	closeBody(t, resp)
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("err = %v", err)
	}
	var api *APIError
	if !errors.As(err, &api) || api.Status != 503 {
		t.Fatalf("APIError = %#v", err)
	}
	if tokenCalls != 0 {
		t.Fatalf("token lookup called %d times", tokenCalls)
	}
	if called {
		t.Fatal("transport called")
	}
}

func TestFetchShortCircuitsWhenMaintenance(t *testing.T) {
	t.Parallel()

	called := false
	c := testClient(t, ClientOptions{
		Status: BackendMaintenance,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			return textResp(r, 200, "ok"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	closeBody(t, resp)
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("transport called")
	}
}

func TestFetchSendsSessionWhenOffline(t *testing.T) {
	t.Parallel()

	called := 0
	c := testClient(t, ClientOptions{
		Status: BackendOffline,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			called++
			return textResp(r, 200, "ok"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/auth/get-session", FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)
	if called != 1 {
		t.Fatalf("transport called %d times", called)
	}
}

func TestFetchProbesOnNHD503(t *testing.T) {
	t.Parallel()

	probed := 0
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, 503, "Service Unavailable"), nil
		}),
		Probe: func() { probed++ },
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{
		DisableHTTPErrors: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)

	if probed != 1 {
		t.Fatalf("probe called %d times", probed)
	}
	if c.GetStatus() != BackendOnline {
		t.Fatalf("status = %q, want online (503 probes, does not setOffline)", c.GetStatus())
	}
}

func TestFetchSetsOfflineOnNHD502(t *testing.T) {
	t.Parallel()

	probed := 0
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, 502, "Bad Gateway"), nil
		}),
		Probe: func() { probed++ },
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{
		DisableHTTPErrors: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)

	if probed != 0 {
		t.Fatalf("probe called %d times", probed)
	}
	if c.GetStatus() != BackendOffline {
		t.Fatalf("status = %q", c.GetStatus())
	}
}

func TestFetchProbesOnRejectedNHD503(t *testing.T) {
	t.Parallel()

	probed := 0
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, 503, "Service Unavailable"), nil
		}),
		Probe: func() { probed++ },
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	if err == nil {
		closeBody(t, resp)
		t.Fatal("expected error")
	}
	closeHTTPErr(t, err)
	closeBody(t, resp)

	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 503 {
		t.Fatalf("err = %v", err)
	}
	if probed != 1 {
		t.Fatalf("probe called %d times", probed)
	}
	if c.GetStatus() != BackendOnline {
		t.Fatalf("status = %q, want online (503 probes, does not setOffline)", c.GetStatus())
	}
}

func TestFetchSetsOfflineOnRejectedNHD502(t *testing.T) {
	t.Parallel()

	probed := 0
	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, 502, "Bad Gateway"), nil
		}),
		Probe: func() { probed++ },
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	if err == nil {
		closeBody(t, resp)
		t.Fatal("expected error")
	}
	closeHTTPErr(t, err)
	closeBody(t, resp)
	if probed != 0 {
		t.Fatalf("probe called %d times", probed)
	}
	if c.GetStatus() != BackendOffline {
		t.Fatalf("status = %q", c.GetStatus())
	}
}

func TestFetchRefreshesSessionOnNHD401(t *testing.T) {
	t.Parallel()

	refreshed := 0
	c := testClient(t, ClientOptions{
		RefreshSession: func() error {
			refreshed++
			return nil
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, 401, "unauthorized"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{
		DisableHTTPErrors: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)
	if refreshed != 1 {
		t.Fatalf("refresh called %d times", refreshed)
	}
}

func TestFetchSkips401RefreshForPasswordRequired(t *testing.T) {
	t.Parallel()

	refreshed := 0
	c := testClient(t, ClientOptions{
		RefreshSession: func() error {
			refreshed++
			return nil
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, 401, "password_required"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{
		DisableHTTPErrors: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)
	if refreshed != 0 {
		t.Fatalf("refresh called %d times", refreshed)
	}
}

func TestFetchRewritesCloudflare524(t *testing.T) {
	t.Parallel()

	c := testClient(t, ClientOptions{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return textResp(r, 524, "timeout"), nil
		}),
	})
	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "cloudflare timeout. but it's ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestFetchRetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	limit := 2
	c := testClient(t, ClientOptions{
		RetryLimit: &limit,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if n.Add(1) < 3 {
				return textResp(r, 503, "unavailable"), nil
			}
			return textResp(r, 200, "ok"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)
	if n.Load() != 3 {
		t.Fatalf("attempts = %d", n.Load())
	}
}

func TestFetchRetryLimitOverrideMakesOneShotRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	clientLimit := 2
	requestLimit := 0
	c := testClient(t, ClientOptions{
		RetryLimit: &clientLimit,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests.Add(1)
			return textResp(r, http.StatusServiceUnavailable, "unavailable"), nil
		}),
	})

	resp, err := c.Fetch(context.Background(), "https://api.nahida.live/api/drive", FetchOptions{
		DisableHTTPErrors: true,
		RetryLimit:        &requestLimit,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	closeBody(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestProbeSetsMaintenanceFromBody(t *testing.T) {
	t.Parallel()

	c := testClient(t, ClientOptions{
		BackendURL: "https://api.nahida.live",
		Status:     BackendUnknown,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/status" {
				t.Fatalf("path = %q", r.URL.Path)
			}
			if r.Header.Get("User-Agent") != "Nahida Desktop/test-version" {
				t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
			}
			resp := textResp(r, 200, `{"status":"maintenance"}`)
			resp.Header.Set("Content-Type", "application/json")
			return resp, nil
		}),
	})

	if got := c.Probe(context.Background()); got != BackendMaintenance {
		t.Fatalf("Probe = %q", got)
	}
}

func TestProbeSetsMaintenanceFromCBORBody(t *testing.T) {
	t.Parallel()

	body, err := cbor.Marshal(map[string]any{"status": "maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, ClientOptions{
		BackendURL: "https://api.nahida.live",
		Status:     BackendUnknown,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			response := textResp(request, http.StatusOK, string(body))
			response.Header.Set("Content-Type", "application/cbor")
			return response, nil
		}),
	})
	if got := client.Probe(context.Background()); got != BackendMaintenance {
		t.Fatalf("Probe = %q", got)
	}
}
