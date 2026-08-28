package drive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

func TestMoveManyIssuesElectronPathAndBody(t *testing.T) {
	t.Parallel()

	var got *http.Request
	var body []byte
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		body = readBody(t, r)
		return jsonResp(r, 200, `{"moved":2}`), nil
	}))

	data, err := d.MoveMany(context.Background(), []string{"a", "b"}, "dest-1")
	if err != nil {
		t.Fatalf("MoveMany: %v", err)
	}
	if got == nil {
		t.Fatal("no request")
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/content/move_many" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer stored-token" {
		t.Fatalf("Authorization = %q", got.Header.Get("Authorization"))
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body: %v", err)
	}
	uuids, _ := payload["uuids"].([]any)
	if len(uuids) != 2 || uuids[0] != "a" || uuids[1] != "b" {
		t.Fatalf("uuids = %#v", payload["uuids"])
	}
	if payload["target"] != "dest-1" {
		t.Fatalf("target = %#v", payload["target"])
	}
	out, ok := data.(map[string]any)
	if !ok || out["moved"] != float64(2) {
		t.Fatalf("data = %#v", data)
	}
}

func TestCopyManyIssuesElectronPathAndBody(t *testing.T) {
	t.Parallel()

	var got *http.Request
	var body []byte
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		body = readBody(t, r)
		return jsonResp(r, 200, `{"copied":1}`), nil
	}))

	data, err := d.CopyMany(context.Background(), []string{"item-9"}, "folder-2")
	if err != nil {
		t.Fatalf("CopyMany: %v", err)
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/content/copy_many" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body: %v", err)
	}
	uuids, _ := payload["uuids"].([]any)
	if len(uuids) != 1 || uuids[0] != "item-9" || payload["target"] != "folder-2" {
		t.Fatalf("payload = %#v", payload)
	}
	out, ok := data.(map[string]any)
	if !ok || out["copied"] != float64(1) {
		t.Fatalf("data = %#v", data)
	}
}

func TestMoveManyWrapsBackendError(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 503, `{"message":"no"}`), nil
	}))

	_, err := d.MoveMany(context.Background(), []string{"a"}, "dest")
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeBackendUnavailable || api.Status != 503 {
		t.Fatalf("api = %+v", api)
	}
}

func TestCopyManyWrapsBackendError(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 400, `{"message":"bad dest"}`), nil
	}))

	_, err := d.CopyMany(context.Background(), []string{"a"}, "dest")
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Status != 400 || api.Message != "bad dest" {
		t.Fatalf("api = %+v", api)
	}
}

func TestResolveImportSourceLinkIssuesPostAndPassword(t *testing.T) {
	t.Parallel()

	var got *http.Request
	var body []byte
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		body = readBody(t, r)
		return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
	}))

	data, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL:      "https://nahida.live/akasha/link/qjsEdvLpcAxr",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("ResolveImportSource: %v", err)
	}
	if got.Method != http.MethodPost {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/link/qjsEdvLpcAxr" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer stored-token" {
		t.Fatalf("Authorization = %q", got.Header.Get("Authorization"))
	}
	if got.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", got.Header.Get("Content-Type"))
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body: %v", err)
	}
	if payload["password"] != "secret" || payload["cftoken"] != "" {
		t.Fatalf("payload = %#v", payload)
	}
	out, ok := data.(ResolveLinkResult)
	if !ok || out.Source != "link" || out.LinkID != "qjsEdvLpcAxr" || out.Token != "link-tok" {
		t.Fatalf("data = %#v", data)
	}
	if out.Parent.ID != "parent-1" || out.Parent.Name != "Shared" {
		t.Fatalf("parent = %+v", out.Parent)
	}
}

func TestResolveImportSourceModIssuesEncodedPasswordAndReadsHeaders(t *testing.T) {
	t.Parallel()

	var got *http.Request
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		resp := jsonResp(r, 200, `{"collections":[{"id":"c1","name":"Col","rootId":"root-1","private":false}]}`)
		resp.Header.Set("x-token", "mod-tok")
		resp.Header.Set("x-sig", "mod-sig")
		return resp, nil
	}))

	data, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL:      "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
		Password: "gayshin",
	})
	if err != nil {
		t.Fatalf("ResolveImportSource: %v", err)
	}
	if got.Method != http.MethodGet {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/mod/WmVWMjAzthuFpKZiE-AKj" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	wantPassword := EncodeNahidaPassword("gayshin")
	if got.URL.Query().Get("password") != wantPassword {
		t.Fatalf("password query = %q, want %q", got.URL.Query().Get("password"), wantPassword)
	}
	out, ok := data.(ResolveModResult)
	if !ok || out.Source != "mod" || out.ModID != "WmVWMjAzthuFpKZiE-AKj" {
		t.Fatalf("data = %#v", data)
	}
	if out.Token != "mod-tok" || out.Sig != "mod-sig" {
		t.Fatalf("headers token=%q sig=%q", out.Token, out.Sig)
	}
	if len(out.ModData.Collections) != 1 || out.ModData.Collections[0].RootID != "root-1" {
		t.Fatalf("modData = %+v", out.ModData)
	}
}

func TestResolveImportSourceLinkPasswordRequired(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 401, `{"code":"MISSING_PASSWORD","message":"Password required"}`), nil
	}))

	_, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL: "https://nahida.live/akasha/link/qjsEdvLpcAxr",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeLinkPasswordRequired || api.Message != msgLinkPasswordRequired {
		t.Fatalf("api = %+v", api)
	}
}

func TestResolveImportSourceLinkInvalidPassword(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 401, `{"code":"INVALID_PASSWORD","message":"invalid_password"}`), nil
	}))

	_, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL:      "https://nahida.live/akasha/link/qjsEdvLpcAxr",
		Password: "wrong",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeLinkInvalidPassword || api.Message != msgLinkInvalidPassword {
		t.Fatalf("api = %+v", api)
	}
}

func TestResolveImportSourceModPasswordRequired(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 401, `{"message":"password required"}`), nil
	}))

	_, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL: "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeModPasswordRequired || api.Message != msgModPasswordRequired {
		t.Fatalf("api = %+v", api)
	}
}

func TestResolveImportSourceModInvalidPassword(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 401, `{"message":"invalid_password"}`), nil
	}))

	_, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL:      "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
		Password: "nope",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeModInvalidPassword || api.Message != msgModInvalidPassword {
		t.Fatalf("api = %+v", api)
	}
}

func TestResolveImportSourceMod500WithPasswordIsInvalidPassword(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 500, `{"message":"Internal Server Error"}`), nil
	}))

	_, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL:      "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
		Password: "maybe",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeModInvalidPassword || api.Message != msgCollectionBadPassword || api.Status != 500 {
		t.Fatalf("api = %+v", api)
	}
}

func TestResolveImportSourceLinkInvalidResponse(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 200, `{"token":123}`), nil
	}))

	_, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL: "https://nahida.live/akasha/link/qjsEdvLpcAxr",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeLinkInvalidResponse {
		t.Fatalf("api = %+v", api)
	}
}

func TestResolveImportSourceModInvalidResponse(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 200, `{"collections":"nope"}`), nil
	}))

	_, err := d.ResolveImportSource(context.Background(), ResolveImportSourceParams{
		URL: "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v", err)
	}
	if api.Code != codeModInvalidResponse {
		t.Fatalf("api = %+v", api)
	}
}

func TestListLinkChildrenIssuesTokenHeader(t *testing.T) {
	t.Parallel()

	var got *http.Request
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return jsonResp(r, 200, `{
			"content":{"id":"item-1","name":"Root","isDir":true},
			"children":[{"id":"c1","name":"Child","isDir":false,"size":12,"mimeType":"text/plain"}],
			"ancestors":[{"id":"item-1","parentId":null,"name":"Root","depth":0}]
		}`), nil
	}))

	out, err := d.ListLinkChildren(context.Background(), ListLinkChildrenParams{
		LinkID:    "qjsEdvLpcAxr",
		LinkToken: "link-tok",
		ItemID:    "item-1",
	})
	if err != nil {
		t.Fatalf("ListLinkChildren: %v", err)
	}
	if got.Method != http.MethodGet {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/link/qjsEdvLpcAxr/content/item-1" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	if got.Header.Get("nhd-link-token") != "link-tok" {
		t.Fatalf("nhd-link-token = %q", got.Header.Get("nhd-link-token"))
	}
	if got.Header.Get("Authorization") != "Bearer stored-token" {
		t.Fatalf("Authorization = %q", got.Header.Get("Authorization"))
	}
	if out.Content.ID != "item-1" || out.Content.Name != "Root" || !out.Content.IsDir {
		t.Fatalf("content = %+v", out.Content)
	}
	if len(out.Children) != 1 || out.Children[0].ID != "c1" || out.Children[0].Size == nil || *out.Children[0].Size != 12 {
		t.Fatalf("children = %+v", out.Children)
	}
	if len(out.Ancestors) != 1 || out.Ancestors[0].ID != "item-1" {
		t.Fatalf("ancestors = %+v", out.Ancestors)
	}
}

func TestListModChildrenIssuesTokenAndSigHeaders(t *testing.T) {
	t.Parallel()

	var got *http.Request
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return jsonResp(r, 200, `{
			"content":{"id":"mod-item","name":"Mod","isDir":true},
			"children":[{"id":"f1","name":"File","isDir":false}]
		}`), nil
	}))

	out, err := d.ListModChildren(context.Background(), ListModChildrenParams{
		ItemID:   "mod-item",
		ModToken: "mod-tok",
		ModSig:   "mod-sig",
	})
	if err != nil {
		t.Fatalf("ListModChildren: %v", err)
	}
	if got.Method != http.MethodGet {
		t.Fatalf("method = %q", got.Method)
	}
	if got.URL.Path != "/akasha/mod/item/mod-item" {
		t.Fatalf("path = %q", got.URL.Path)
	}
	if got.Header.Get("x-token") != "mod-tok" || got.Header.Get("x-sig") != "mod-sig" {
		t.Fatalf("headers token=%q sig=%q", got.Header.Get("x-token"), got.Header.Get("x-sig"))
	}
	if out.Content.ID != "mod-item" || len(out.Children) != 1 || out.Children[0].Name != "File" {
		t.Fatalf("out = %+v", out)
	}
}

func TestListLinkChildrenInvalidPayload(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 200, `{"content":{}}`), nil
	}))

	_, err := d.ListLinkChildren(context.Background(), ListLinkChildrenParams{
		LinkID: "qjsEdvLpcAxr", LinkToken: "t", ItemID: "i",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != codeLinkContentInvalid {
		t.Fatalf("err = %v", err)
	}
}

func TestListModChildrenInvalidPayload(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResp(r, 200, `{"children":[]}`), nil
	}))

	_, err := d.ListModChildren(context.Background(), ListModChildrenParams{ItemID: "i"})
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != codeModContentInvalid {
		t.Fatalf("err = %v", err)
	}
}

func sseResp(r *http.Request, body string) *http.Response {
	return &http.Response{
		Status:     http.StatusText(200),
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func TestCopyFromURLLinkIssuesImportSSEAndCompletes(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var reqs []*http.Request
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		mu.Lock()
		reqs = append(reqs, clone)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/link/qjsEdvLpcAxr":
			return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/content/dest-1":
			return jsonResp(r, 200, `{"id":"dest-1","children":[]}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/common/sse/import":
			return sseResp(r, "event: metadata\ndata: {\"totalExpectedSize\":10}\n\nevent: complete\ndata: {\"ok\":true}\n\n"), nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	}))

	out, err := d.CopyFromURL(context.Background(), CopyFromURLParams{
		URL:           "https://nahida.live/akasha/link/qjsEdvLpcAxr",
		DestinationID: "dest-1",
	})
	if err != nil {
		t.Fatalf("CopyFromURL: %v", err)
	}
	if out.Source != "link" || out.Copied != 1 || out.DestinationID != "dest-1" {
		t.Fatalf("out = %+v", out)
	}
	if len(reqs) != 3 {
		t.Fatalf("reqs = %d", len(reqs))
	}
	var importReq *http.Request
	for _, req := range reqs {
		if req.URL.Path == "/akasha/common/sse/import" {
			importReq = req
		}
	}
	if importReq == nil {
		t.Fatal("no import request")
	}
	if importReq.Method != http.MethodGet {
		t.Fatalf("import method = %q", importReq.Method)
	}
	q := importReq.URL.Query()
	if q.Get("mode") != "link" || q.Get("src") != "parent-1" || q.Get("dest") != "dest-1" {
		t.Fatalf("query = %v", q)
	}
	if q.Get("linkId") != "qjsEdvLpcAxr" || q.Get("linkToken") != "link-tok" {
		t.Fatalf("link query = %v", q)
	}
	if importReq.Header.Get("Authorization") != "Bearer stored-token" {
		t.Fatalf("Authorization = %q", importReq.Header.Get("Authorization"))
	}
}

func TestCopyFromURLManyLinkIssuesImportManyBody(t *testing.T) {
	t.Parallel()

	var importReq *http.Request
	var importBody []byte
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/link/qjsEdvLpcAxr":
			_, _ = io.Copy(io.Discard, r.Body)
			return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/content/dest-1":
			return jsonResp(r, 200, `{"id":"dest-1","children":[]}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/common/sse/import-many":
			importReq = r.Clone(r.Context())
			importBody = readBody(t, r)
			return sseResp(r, "event: complete\ndata: {\"totalSize\":3}\n\n"), nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	}))

	out, err := d.CopyFromURLMany(context.Background(), CopyFromURLParams{
		URL:           "https://nahida.live/akasha/link/qjsEdvLpcAxr",
		DestinationID: "dest-9",
		SelectedIDs:   []string{"s1", "s2"},
	})
	if err != nil {
		t.Fatalf("CopyFromURLMany: %v", err)
	}
	if out.Source != "link" || out.Copied != 2 {
		t.Fatalf("out = %+v", out)
	}
	if importReq == nil {
		t.Fatal("no import-many request")
	}
	if importReq.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", importReq.Header.Get("Content-Type"))
	}
	var payload map[string]any
	if err := json.Unmarshal(importBody, &payload); err != nil {
		t.Fatalf("body: %v", err)
	}
	if payload["mode"] != "link" || payload["dest"] != "dest-9" {
		t.Fatalf("payload = %#v", payload)
	}
	src, _ := payload["src"].([]any)
	if len(src) != 2 || src[0] != "s1" || src[1] != "s2" {
		t.Fatalf("src = %#v", payload["src"])
	}
	if payload["linkId"] != "qjsEdvLpcAxr" || payload["linkToken"] != "link-tok" {
		t.Fatalf("link fields = %#v", payload)
	}
}

func TestCopyFromURLModSelectedIssuesImportManyHeaders(t *testing.T) {
	t.Parallel()

	var importReq *http.Request
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/akasha/mod/"):
			resp := jsonResp(r, 200, `{"collections":[{"id":"c1","name":"Col","rootId":"root-1"}]}`)
			resp.Header.Set("x-token", "mod-tok")
			resp.Header.Set("x-sig", "mod-sig")
			return resp, nil
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/common/sse/import-many":
			importReq = r.Clone(r.Context())
			_, _ = io.Copy(io.Discard, r.Body)
			return sseResp(r, "event: complete\ndata: {}\n\n"), nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	}))

	out, err := d.CopyFromURL(context.Background(), CopyFromURLParams{
		URL:           "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
		DestinationID: "dest-1",
		SelectedIDs:   []string{"only-one"},
	})
	if err != nil {
		t.Fatalf("CopyFromURL: %v", err)
	}
	if out.Source != "mod" || out.Copied != 1 {
		t.Fatalf("out = %+v", out)
	}
	if importReq.Header.Get("x-token") != "mod-tok" || importReq.Header.Get("x-sig") != "mod-sig" {
		t.Fatalf("headers token=%q sig=%q", importReq.Header.Get("x-token"), importReq.Header.Get("x-sig"))
	}
}

func TestCopyFromURLCancelMidFlight(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/link/qjsEdvLpcAxr":
			_, _ = io.Copy(io.Discard, r.Body)
			return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/content/dest-1":
			return jsonResp(r, 200, `{"id":"dest-1","children":[]}`), nil
		case r.URL.Path == "/akasha/common/sse/import":
			close(started)
			pr, pw := io.Pipe()
			go func() {
				_, _ = io.WriteString(pw, "event: metadata\ndata: {}\n\n")
				<-release
				_ = pw.Close()
			}()
			return &http.Response{
				Status:     http.StatusText(200),
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	}))

	errCh := make(chan error, 1)
	go func() {
		_, err := d.CopyFromURL(context.Background(), CopyFromURLParams{
			URL:           "https://nahida.live/akasha/link/qjsEdvLpcAxr",
			DestinationID: "dest-1",
			OperationID:   "op-cancel-1",
		})
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("import SSE never started")
	}
	if !d.CancelCopyFromURL("op-cancel-1") {
		t.Fatal("CancelCopyFromURL returned false")
	}
	close(release)

	var err error
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("copy did not finish after cancel")
	}
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != codeCopyCanceled {
		t.Fatalf("err = %v", err)
	}
}

func TestCopyFromURLIncompleteStream(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/akasha/link/"):
			_, _ = io.Copy(io.Discard, r.Body)
			return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/content/dest-1":
			return jsonResp(r, 200, `{"id":"dest-1","children":[]}`), nil
		case r.URL.Path == "/akasha/common/sse/import":
			return sseResp(r, "event: status\ndata: {\"status\":\"working\"}\n\n"), nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	}))

	_, err := d.CopyFromURL(context.Background(), CopyFromURLParams{
		URL:           "https://nahida.live/akasha/link/qjsEdvLpcAxr",
		DestinationID: "dest-1",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != codeImportInvalidResponse {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(api.Message, "parent-1") {
		t.Fatalf("message = %q", api.Message)
	}
}

func TestCopyFromURLVerifiesCopiedDirectoryAfterSSEError(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	destinationGets := 0
	var progress []DriveCopyProgress
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/link/qjsEdvLpcAxr":
			_, _ = io.Copy(io.Discard, r.Body)
			return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/content/dest-1":
			mu.Lock()
			destinationGets++
			current := destinationGets
			mu.Unlock()
			if current == 1 {
				return jsonResp(r, 200, `{"id":"dest-1","children":[{"id":"old","name":"Shared","isDir":true,"size":10}]}`), nil
			}
			return jsonResp(r, 200, `{"id":"dest-1","children":[{"id":"old","name":"Shared","isDir":true,"size":10},{"id":"new","name":"Shared (1)","isDir":true,"size":10}]}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/common/sse/import":
			return sseResp(r, "event: metadata\ndata: {\"totalExpectedSize\":10}\n\nevent: error\ndata: {\"message\":\"late failure\"}\n\n"), nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	})
	d := NewWithOptions(Options{
		HTTP: testClient(t, transport), FS: platform.NewFS(), DirRetries: zeroRetries(),
		Sleep: func(context.Context, time.Duration) error { return nil },
		EventEmit: func(name string, data ...any) {
			if name != "drive:copy-progress" || len(data) != 1 {
				return
			}
			if event, ok := data[0].(DriveCopyProgress); ok {
				progress = append(progress, event)
			}
		},
	})

	out, err := d.CopyFromURL(context.Background(), CopyFromURLParams{
		URL: "https://nahida.live/akasha/link/qjsEdvLpcAxr", DestinationID: "dest-1", OperationID: "verified-op",
	})
	if err != nil {
		t.Fatalf("CopyFromURL: %v", err)
	}
	if out.Copied != 1 || destinationGets != 2 {
		t.Fatalf("out=%+v destinationGets=%d", out, destinationGets)
	}
	foundVerified := false
	for _, event := range progress {
		if strings.Contains(event.Message, "files were copied") {
			foundVerified = true
		}
	}
	if !foundVerified || len(progress) == 0 || progress[len(progress)-1].Phase != "completed" {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestCopyFromURLManyCreatesAndUpdatesTransfer(t *testing.T) {
	t.Parallel()

	transfers := transfer.New()
	var progress []DriveCopyProgress
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/link/qjsEdvLpcAxr":
			_, _ = io.Copy(io.Discard, r.Body)
			return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/common/sse/import-many":
			_, _ = io.Copy(io.Discard, r.Body)
			return sseResp(r, "event: metadata\ndata: {\"totalExpectedSize\":100}\n\nevent: status\ndata: {\"status\":\"copying\",\"processedFiles\":1,\"currentTotalSize\":40}\n\nevent: complete\ndata: {\"totalSize\":100}\n\n"), nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	})
	d := NewWithOptions(Options{
		HTTP: testClient(t, transport), FS: platform.NewFS(), Transfer: transfers, DirRetries: zeroRetries(),
		Sleep: func(context.Context, time.Duration) error { return nil },
		EventEmit: func(name string, data ...any) {
			if name == "drive:copy-progress" && len(data) == 1 {
				progress = append(progress, data[0].(DriveCopyProgress))
			}
		},
	})

	out, err := d.CopyFromURLMany(context.Background(), CopyFromURLParams{
		URL: "https://nahida.live/akasha/link/qjsEdvLpcAxr", DestinationID: "dest-9",
		SelectedIDs: []string{"s1", "s2"}, OperationID: "import-many-op",
	})
	if err != nil {
		t.Fatalf("CopyFromURLMany: %v", err)
	}
	if out.Copied != 2 {
		t.Fatalf("out = %+v", out)
	}
	record, ok := transfers.Get("import-many-op")
	if !ok || record.Status != transfer.StatusCompleted || record.TotalSize != 100 || record.TransferredSize != 100 || record.TransferredFiles != 2 || record.Progress != 100 {
		t.Fatalf("transfer = %+v, ok=%v", record, ok)
	}
	if len(progress) < 4 || progress[0].Phase != "preparing" || progress[len(progress)-1].Phase != "completed" {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestCopyFromURLManyTransferCancelStopsImport(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	transfers := transfer.New()
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/link/qjsEdvLpcAxr":
			_, _ = io.Copy(io.Discard, r.Body)
			return jsonResp(r, 200, `{"token":"link-tok","parent":{"id":"parent-1","name":"Shared"}}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/common/sse/import-many":
			_, _ = io.Copy(io.Discard, r.Body)
			close(started)
			pr, pw := io.Pipe()
			go func() {
				_, _ = io.WriteString(pw, "event: metadata\ndata: {\"totalExpectedSize\":10}\n\n")
				<-release
				_ = pw.Close()
			}()
			return &http.Response{
				Status: http.StatusText(200), StatusCode: 200,
				Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: pr, Request: r,
			}, nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	})
	d := NewWithOptions(Options{
		HTTP: testClient(t, transport), FS: platform.NewFS(), Transfer: transfers,
		DirRetries: zeroRetries(), Sleep: func(context.Context, time.Duration) error { return nil },
	})
	errCh := make(chan error, 1)
	go func() {
		_, err := d.CopyFromURLMany(context.Background(), CopyFromURLParams{
			URL: "https://nahida.live/akasha/link/qjsEdvLpcAxr", DestinationID: "dest-9",
			SelectedIDs: []string{"s1", "s2"}, OperationID: "cancel-transfer-op",
		})
		errCh <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("import-many SSE never started")
	}
	if err := transfers.Cancel("cancel-transfer-op"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	close(release)
	select {
	case err := <-errCh:
		var api *DriveAPIError
		if !errors.As(err, &api) || api.Code != codeCopyCanceled {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("copy did not stop after transfer cancel")
	}
	record, ok := transfers.Get("cancel-transfer-op")
	if !ok || record.Status != transfer.StatusCanceled {
		t.Fatalf("transfer = %+v, ok=%v", record, ok)
	}
}

func TestCopyFromURLModCollectionCreatesFolderAndImports(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var reqs []*http.Request
	destGets := 0
	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		mu.Lock()
		reqs = append(reqs, clone)
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/mod/WmVWMjAzthuFpKZiE-AKj":
			resp := jsonResp(r, 200, `{"collections":[{"id":"c1","name":"Col","rootId":"root-1","private":false}]}`)
			resp.Header.Set("x-token", "mod-tok")
			resp.Header.Set("x-sig", "mod-sig")
			return resp, nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/mod/item/root-1":
			if r.Header.Get("x-token") != "mod-tok" || r.Header.Get("x-sig") != "mod-sig" {
				t.Fatalf("mod item headers token=%q sig=%q", r.Header.Get("x-token"), r.Header.Get("x-sig"))
			}
			return jsonResp(r, 200, `{"children":[{"id":"child-dir","name":"Outfit","isDir":true}]}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/content/dest-1":
			mu.Lock()
			destGets++
			n := destGets
			mu.Unlock()
			if n == 1 {
				return jsonResp(r, 200, `{"id":"dest-1","children":[]}`), nil
			}
			return jsonResp(r, 200, `{"id":"dest-1","children":[{"id":"folder-1","name":"Col","isDir":true}]}`), nil
		case r.Method == http.MethodPost && r.URL.Path == "/akasha/dir/create_many":
			return jsonResp(r, 200, `{"created":1}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/content/folder-1":
			return jsonResp(r, 200, `{"id":"folder-1","children":[]}`), nil
		case r.Method == http.MethodGet && r.URL.Path == "/akasha/common/sse/import":
			return sseResp(r, "event: complete\ndata: {}\n\n"), nil
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected")
		}
	}))

	out, err := d.CopyFromURL(context.Background(), CopyFromURLParams{
		URL:           "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
		DestinationID: "dest-1",
	})
	if err != nil {
		t.Fatalf("CopyFromURL: %v", err)
	}
	if out.Source != "mod" || out.Copied != 1 || out.DestinationID != "dest-1" {
		t.Fatalf("out = %+v", out)
	}
	var importReq *http.Request
	for _, r := range reqs {
		if r.URL.Path == "/akasha/common/sse/import" {
			importReq = r
		}
	}
	if importReq == nil {
		t.Fatal("no import SSE request")
	}
	q := importReq.URL.Query()
	if q.Get("mode") != "mod" || q.Get("src") != "child-dir" || q.Get("dest") != "folder-1" {
		t.Fatalf("import query = %v", q)
	}
	if importReq.Header.Get("x-token") != "mod-tok" || importReq.Header.Get("x-sig") != "mod-sig" {
		t.Fatalf("import headers token=%q sig=%q", importReq.Header.Get("x-token"), importReq.Header.Get("x-sig"))
	}
}

func TestCopyFromURLModCollectionNotFound(t *testing.T) {
	t.Parallel()

	d := testDrive(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/akasha/mod/") {
			return jsonResp(r, 200, `{"collections":[{"id":"c1","name":"Col","rootId":"root-1","private":false}]}`), nil
		}
		t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		return nil, errors.New("unexpected")
	}))

	_, err := d.CopyFromURL(context.Background(), CopyFromURLParams{
		URL:           "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
		DestinationID: "dest-1",
		CollectionID:  "missing",
	})
	var api *DriveAPIError
	if !errors.As(err, &api) || api.Code != codeCollectionNotFound {
		t.Fatalf("err = %v", err)
	}
}
