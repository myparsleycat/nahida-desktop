package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResp(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func cborResp(req *http.Request, status int, value any) *http.Response {
	raw, err := cbor.Marshal(value)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/cbor"}},
		Body:       io.NopCloser(bytes.NewReader(raw)),
		Request:    req,
	}
}

func zeroRetries() *int {
	n := 0
	return &n
}

func testClient(t *testing.T, transport http.RoundTripper) *infra.Client {
	t.Helper()
	zero := 0
	none := time.Duration(0)
	return infra.NewClientWithOptions(infra.ClientOptions{
		Version:    "test-version",
		BackendURL: "http://localhost",
		Status:     infra.BackendOnline,
		Token:      func() (string, error) { return "stored-token", nil },
		RetryLimit: &zero,
		RetryWait:  &none,
		Transport:  transport,
	})
}

func testDrive(t *testing.T, transport http.RoundTripper) *Drive {
	t.Helper()
	return NewWithOptions(Options{
		HTTP:       testClient(t, transport),
		FS:         platform.NewFS(),
		DirRetries: zeroRetries(),
		Sleep:      func(context.Context, time.Duration) error { return nil },
	})
}

func readBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return raw
}

func TestGetItemIssuesElectronPath(t *testing.T) {
	t.Parallel()

	var got *http.Request
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return jsonResp(r, 200, `{"id":"item-1","name":"Folder"}`), nil
	}))

	data, err := d.GetItem(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got == nil {
		t.Fatal("no request")
	}
	if got.Method != http.MethodGet {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/content/item-1" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer stored-token" {
		t.Fatalf("Authorization = %q", got.Header.Get("Authorization"))
	}
	item, ok := data.(map[string]any)
	if !ok || item["id"] != "item-1" || item["name"] != "Folder" {
		t.Fatalf("data = %#v", data)
	}
}

func TestGetItemDecodesCborBody(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"content":   map[string]any{"id": "item-1", "name": "Folder", "isDir": true},
		"parent":    nil,
		"ancestors": []any{map[string]any{"id": "item-1", "parentId": nil, "name": "Folder", "depth": 0}},
		"children":  []any{},
	}

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("res") == "json" {
			t.Fatal("should not fall back to JSON when CBOR decodes")
		}
		return cborResp(r, 200, payload), nil
	}))

	data, err := d.GetItem(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	item, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", data)
	}
	ancestors, ok := item["ancestors"].([]any)
	if !ok || len(ancestors) != 1 {
		t.Fatalf("ancestors = %#v", item["ancestors"])
	}
}

func TestGetItemRetriesCborAsJSON(t *testing.T) {
	t.Parallel()

	var sawJSON bool
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("res") == "json" {
			sawJSON = true
			return jsonResp(r, 200, `{
				"content":{"id":"item-1","name":"Folder","isDir":true},
				"ancestors":[{"id":"item-1","parentId":null,"name":"Folder","depth":0}],
				"children":[]
			}`), nil
		}
		return &http.Response{
			Status:     http.StatusText(200),
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"application/cbor"}},
			Body:       io.NopCloser(bytes.NewReader([]byte{0xff, 0xff})),
			Request:    r,
		}, nil
	}))

	data, err := d.GetItem(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if !sawJSON {
		t.Fatal("did not retry as JSON")
	}
	item, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", data)
	}
	ancestors, ok := item["ancestors"].([]any)
	if !ok || len(ancestors) != 1 {
		t.Fatalf("ancestors = %#v", item["ancestors"])
	}
}

func TestSearchIssuesElectronPathAndQuery(t *testing.T) {
	t.Parallel()

	var got *http.Request
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return jsonResp(r, 200, `{"items":[]}`), nil
	}))

	data, err := d.Search(context.Background(), "root-1", SearchParams{Q: "venti", Limit: 25, Cursor: "cur-1"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.Method != http.MethodGet {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/content/root-1/search" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	q := got.URL.Query()
	if q.Get("q") != "venti" || q.Get("limit") != "25" || q.Get("cursor") != "cur-1" {
		t.Fatalf("query = %v", q)
	}
	out, ok := data.(map[string]any)
	if !ok || out["items"] == nil {
		t.Fatalf("data = %#v", data)
	}
}

func TestCreateDirIssuesElectronBody(t *testing.T) {
	t.Parallel()

	var got *http.Request
	var body []byte
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		body = readBody(t, r)
		return jsonResp(r, 200, `{"created":1}`), nil
	}))

	data, err := d.CreateDir(context.Background(), "parent-1", "New Folder")
	if err != nil {
		t.Fatalf("CreateDir: %v", err)
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/dir/create_many" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body: %v", err)
	}
	if payload["parentId"] != "parent-1" {
		t.Fatalf("parentId = %#v", payload["parentId"])
	}
	dirs, ok := payload["dirs"].([]any)
	if !ok || len(dirs) != 1 {
		t.Fatalf("dirs = %#v", payload["dirs"])
	}
	dir, _ := dirs[0].(map[string]any)
	if dir["path"] != "parent-1" || dir["name"] != "New Folder" {
		t.Fatalf("dir = %#v", dir)
	}
	out, ok := data.(map[string]any)
	if !ok || out["created"] != float64(1) {
		t.Fatalf("data = %#v", data)
	}
}

func TestRenameIssuesElectronBody(t *testing.T) {
	t.Parallel()

	var got *http.Request
	var body []byte
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		body = readBody(t, r)
		return jsonResp(r, 200, `{"ok":true}`), nil
	}))

	if _, err := d.Rename(context.Background(), "item-9", "Renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/content/rename/item-9" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body: %v", err)
	}
	if payload["rename"] != "Renamed" {
		t.Fatalf("body = %#v", payload)
	}
}

func TestCreateDirAndRenameRejectInvalidWindowsNames(t *testing.T) {
	t.Parallel()

	called := false
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResp(r, 200, `{}`), nil
	}))

	_, err := d.CreateDir(context.Background(), "parent-1", "bad:name")
	if !errors.Is(err, platform.ErrInvalidWindowsFilename) {
		t.Fatalf("CreateDir err = %v", err)
	}
	_, err = d.Rename(context.Background(), "item-1", "CON")
	if !errors.Is(err, platform.ErrInvalidWindowsFilename) {
		t.Fatalf("Rename err = %v", err)
	}
	if called {
		t.Fatal("invalid names left the process")
	}
}

func TestTrashIssuesElectronBody(t *testing.T) {
	t.Parallel()

	var got *http.Request
	var body []byte
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		body = readBody(t, r)
		return jsonResp(r, 200, `{}`), nil
	}))

	out, err := d.DeleteItems(context.Background(), []string{"a", "b"}, "trash")
	if err != nil {
		t.Fatalf("DeleteItems trash: %v", err)
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/content/trash/trash_many" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body: %v", err)
	}
	uuids, _ := payload["uuids"].([]any)
	if len(uuids) != 2 || uuids[0] != "a" || uuids[1] != "b" {
		t.Fatalf("uuids = %#v", payload["uuids"])
	}
	if len(out.RequestedIDs) != 2 || out.RequestedIDs[0] != "a" || len(out.Jobs) != 0 {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestDeleteItemsBatchesAcrossDeletionBatchSize(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var pages [][]string
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/akasha/content/delete_many" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload struct {
			UUIDs []string `json:"uuids"`
		}
		if err := json.Unmarshal(readBody(t, r), &payload); err != nil {
			t.Fatalf("body: %v", err)
		}
		mu.Lock()
		pages = append(pages, append([]string(nil), payload.UUIDs...))
		mu.Unlock()
		return jsonResp(r, 200, `{"status":"completed","deletedCount":`+fmt.Sprintf("%d", len(payload.UUIDs))+`}`), nil
	}))

	ids := make([]string, DeletionBatchSize+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%d", i)
	}
	out, err := d.DeleteItems(context.Background(), ids, "delete")
	if err != nil {
		t.Fatalf("DeleteItems: %v", err)
	}
	if len(pages) != 2 || len(pages[0]) != DeletionBatchSize || len(pages[1]) != 1 {
		t.Fatalf("pages = %v", []int{len(pages), len(pages[0]), len(pages[1])})
	}
	if len(out.AcceptedIDs) != len(ids) || len(out.Jobs) != 0 {
		t.Fatalf("outcome accepted=%d jobs=%d", len(out.AcceptedIDs), len(out.Jobs))
	}
}

func TestDeleteItemsAccepts202Payload(t *testing.T) {
	t.Parallel()

	var path string
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path = r.URL.Path
		return jsonResp(r, 202, `{"deletionJobId":"job-9","status":"pending","deletionJobToken":"tok"}`), nil
	}))

	out, err := d.DeleteItems(context.Background(), []string{"id-1"}, "delete")
	if err != nil {
		t.Fatalf("DeleteItems: %v", err)
	}
	if len(out.Jobs) != 1 || out.Jobs[0].DeletionJobID != "job-9" || out.Jobs[0].DeletionJobToken != "tok" {
		t.Fatalf("jobs = %+v", out.Jobs)
	}
	if len(out.AcceptedIDs) != 1 || out.AcceptedIDs[0] != "id-1" {
		t.Fatalf("accepted = %v", out.AcceptedIDs)
	}
	if path != "/akasha/content/delete_many" {
		t.Fatalf("path = %q", path)
	}
}

func TestDeleteItemsUnexpectedPayload(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 200, `{"status":"mystery"}`), nil
	}))

	_, err := d.DeleteItems(context.Background(), []string{"id-1"}, "delete")
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Message != "unexpected_deletion_response" {
		t.Fatalf("message = %q", api.Message)
	}
}

func TestDeleteItemsRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	called := false
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return jsonResp(r, 200, `{}`), nil
	}))

	_, err := d.DeleteItems(context.Background(), []string{"id-1"}, "archive")
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != "DRIVE_DELETE_ITEMS_FAILED" || api.Message != "INVALID_ACTION" {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("unknown action left the process")
	}
}

func TestGetItemNormalizesBackendUnavailable(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 503, `{"message":"no"}`), nil
	}))

	_, err := d.GetItem(context.Background(), "item-1")
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeBackendUnavailable || api.Status != 503 {
		t.Fatalf("api = %+v", api)
	}
}

func TestGetItemNormalizesMissingPassword(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 401, `{"code":"MISSING_PASSWORD","message":"Password required"}`), nil
	}))

	_, err := d.GetItem(context.Background(), "item-1")
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeLinkPasswordRequired || api.Status != 401 {
		t.Fatalf("api = %+v", api)
	}
}

func TestNewWiresHTTPAndFilenameCheck(t *testing.T) {
	t.Parallel()

	var got *http.Request
	httpClient := testClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return jsonResp(r, 200, `{"id":"x"}`), nil
	}))
	d := New()
	d.UseHTTP(httpClient)
	d.UseFS(platform.NewFS())

	if _, err := d.GetItem(context.Background(), "x"); err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got == nil || got.URL.Path != "/akasha/content/x" {
		t.Fatalf("request = %+v", got)
	}
	if _, err := d.Rename(context.Background(), "x", "a<b"); !errors.Is(err, platform.ErrInvalidWindowsFilename) {
		t.Fatalf("Rename err = %v", err)
	}
}
