package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"nahida.live/desktop/internal/infra"
)

var errDriveHTTPUnconfigured = errors.New("drive http is not configured")

// MoveMany is Electron drive.fn.moveMany.
func (d *Drive) MoveMany(ctx context.Context, ids []string, destID string) (result any, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:moveMany")
	return d.postMany(ctx, "/akasha/content/move_many", ids, destID, "fn:moveMany")
}

// CopyMany is Electron drive.fn.copyMany.
func (d *Drive) CopyMany(ctx context.Context, ids []string, destID string) (result any, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:copyMany")
	return d.postMany(ctx, "/akasha/content/copy_many", ids, destID, "fn:copyMany")
}

func (d *Drive) postMany(ctx context.Context, path string, ids []string, destID, operation string) (any, error) {
	data, edenErr, err := d.doJSON(ctx, http.MethodPost, path, nil, map[string]any{
		"uuids":  ids,
		"target": destID,
	})
	if err != nil {
		return nil, CreateDriveAPIError(err, operation, 0)
	}
	if edenErr != nil {
		return nil, CreateDriveAPIError(edenErr.asAny(), operation, edenErr.Status)
	}
	return data, nil
}

// ResolveImportSourceParams is Electron DriveResolveImportSourceParams.
type ResolveImportSourceParams struct {
	URL      string `json:"url"`
	Password string `json:"password,omitempty"`
}

// ResolveLinkResult is Electron DriveResolveLinkResult.
type ResolveLinkResult struct {
	Source string           `json:"source"`
	LinkID string           `json:"linkId"`
	Token  string           `json:"token"`
	Parent SharedLinkParent `json:"parent"`
}

// ResolveModResult is Electron DriveResolveModResult.
type ResolveModResult struct {
	Source  string      `json:"source"`
	ModID   string      `json:"modId"`
	ModData ModOverview `json:"modData"`
	Token   string      `json:"token,omitempty"`
	Sig     string      `json:"sig,omitempty"`
}

// SharedLinkParent is the parent object on a shared-link access response.
type SharedLinkParent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SharedLinkAccess is Electron SharedLinkAccess.
type SharedLinkAccess struct {
	Token  string           `json:"token"`
	Parent SharedLinkParent `json:"parent"`
}

// ModCollection is one collection on a mod overview.
type ModCollection struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	RootID  string `json:"rootId"`
	Private bool   `json:"private,omitempty"`
}

// ModOverview is Electron ModOverview.
type ModOverview struct {
	Collections []ModCollection `json:"collections"`
}

// ModAccess is requestModOverview's return value.
type ModAccess struct {
	Data  ModOverview
	Token string
	Sig   string
}

// DriveImportContent is Electron DriveImportContent.
type DriveImportContent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDir     bool   `json:"isDir"`
	Size      *int64 `json:"size"`
	MimeType  string `json:"mimeType"`
	ParentID  string `json:"parentId"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Ancestor is one ancestor entry on a list-children result.
type Ancestor struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Name     string `json:"name"`
	Depth    int    `json:"depth"`
}

// ListChildrenResult is Electron DriveListChildrenResult.
type ListChildrenResult struct {
	Content   DriveImportContent   `json:"content"`
	Children  []DriveImportContent `json:"children"`
	Ancestors []Ancestor           `json:"ancestors"`
}

// ListLinkChildrenParams is Electron DriveListLinkChildrenParams.
type ListLinkChildrenParams struct {
	LinkID    string `json:"linkId"`
	LinkToken string `json:"linkToken"`
	ItemID    string `json:"itemId"`
}

// ListModChildrenParams is Electron DriveListModChildrenParams.
type ListModChildrenParams struct {
	ItemID   string `json:"itemId"`
	ModToken string `json:"modToken,omitempty"`
	ModSig   string `json:"modSig,omitempty"`
}

// ResolveImportSource is Electron drive.fn.resolveImportSource.
func (d *Drive) ResolveImportSource(ctx context.Context, params ResolveImportSourceParams) (result any, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:resolveImportSource")
	source, err := ParseDriveSourceUrl(params.URL)
	if err != nil {
		return nil, err
	}
	if source.Type == "link" {
		access, err := d.requestSharedLinkAccess(ctx, source.ID, params.Password)
		if err != nil {
			return nil, err
		}
		return ResolveLinkResult{
			Source: "link",
			LinkID: source.ID,
			Token:  access.Token,
			Parent: access.Parent,
		}, nil
	}
	modAccess, err := d.requestModOverview(ctx, source.ID, params.Password)
	if err != nil {
		return nil, err
	}
	return ResolveModResult{
		Source:  "mod",
		ModID:   source.ID,
		ModData: modAccess.Data,
		Token:   modAccess.Token,
		Sig:     modAccess.Sig,
	}, nil
}

// ListLinkChildren is Electron drive.fn.listLinkChildren.
func (d *Drive) ListLinkChildren(ctx context.Context, params ListLinkChildrenParams) (result ListChildrenResult, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:listLinkChildren")
	return d.requestLinkContent(ctx, params.LinkID, params.LinkToken, params.ItemID)
}

// ListModChildren is Electron drive.fn.listModChildren.
func (d *Drive) ListModChildren(ctx context.Context, params ListModChildrenParams) (result ListChildrenResult, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:listModChildren")
	return d.requestModContent(ctx, params.ItemID, params.ModToken, params.ModSig)
}

func (d *Drive) requestSharedLinkAccess(ctx context.Context, linkID, password string) (SharedLinkAccess, error) {
	data, edenErr, err := d.doJSON(ctx, http.MethodPost, "/akasha/link/"+url.PathEscape(linkID), nil, map[string]any{
		"password": password,
		"cftoken":  "",
	})
	if err != nil {
		return SharedLinkAccess{}, remapLinkAccessError(CreateDriveAPIError(err, "shared link access", 0))
	}
	if edenErr != nil {
		return SharedLinkAccess{}, remapLinkAccessError(CreateDriveAPIError(edenErr.asAny(), "shared link access", edenErr.Status))
	}
	access, ok := asSharedLinkAccess(data)
	if !ok {
		return SharedLinkAccess{}, newDriveAPIError(codeLinkInvalidResponse, msgLinkInvalidResponse, 0, nil)
	}
	return access, nil
}

func (d *Drive) requestModOverview(ctx context.Context, modID, password string) (ModAccess, error) {
	query := url.Values{}
	hasPassword := strings.TrimSpace(password) != ""
	if hasPassword {
		query.Set("password", EncodeNahidaPassword(password))
	}
	data, headers, edenErr, err := d.doJSONHeaders(ctx, http.MethodGet, "/akasha/mod/"+url.PathEscape(modID), query, nil, nil)
	if err != nil {
		return ModAccess{}, remapModOverviewError(CreateDriveAPIError(err, "mod overview", 0), hasPassword)
	}
	if edenErr != nil {
		return ModAccess{}, remapModOverviewError(CreateDriveAPIError(edenErr.asAny(), "mod overview", edenErr.Status), hasPassword)
	}
	overview, ok := asModOverview(data)
	if !ok {
		return ModAccess{}, newDriveAPIError(codeModInvalidResponse, msgModInvalidResponse, 0, nil)
	}
	return ModAccess{
		Data:  overview,
		Token: headers.Get("x-token"),
		Sig:   headers.Get("x-sig"),
	}, nil
}

func (d *Drive) requestLinkContent(ctx context.Context, linkID, linkToken, itemID string) (ListChildrenResult, error) {
	path := "/akasha/link/" + url.PathEscape(linkID) + "/content/" + url.PathEscape(itemID)
	extra := make(http.Header)
	extra.Set("nhd-link-token", linkToken)
	data, _, edenErr, err := d.doJSONHeaders(ctx, http.MethodGet, path, nil, extra, nil)
	if err != nil {
		return ListChildrenResult{}, CreateDriveAPIError(err, "link content", 0)
	}
	if edenErr != nil {
		return ListChildrenResult{}, CreateDriveAPIError(edenErr.asAny(), "link content", edenErr.Status)
	}
	result, ok := asLinkContent(data)
	if !ok {
		return ListChildrenResult{}, newDriveAPIError(codeLinkContentInvalid, msgLinkContentInvalid, 0, nil)
	}
	return result, nil
}

func (d *Drive) requestModContent(ctx context.Context, itemID, modToken, modSig string) (ListChildrenResult, error) {
	extra := make(http.Header)
	if modToken != "" {
		extra.Set("x-token", modToken)
	}
	if modSig != "" {
		extra.Set("x-sig", modSig)
	}
	data, _, edenErr, err := d.doJSONHeaders(ctx, http.MethodGet, "/akasha/mod/item/"+url.PathEscape(itemID), nil, extra, nil)
	if err != nil {
		return ListChildrenResult{}, CreateDriveAPIError(err, "mod content", 0)
	}
	if edenErr != nil {
		return ListChildrenResult{}, CreateDriveAPIError(edenErr.asAny(), "mod content", edenErr.Status)
	}
	result, ok := asModContent(data)
	if !ok {
		return ListChildrenResult{}, newDriveAPIError(codeModContentInvalid, msgModContentInvalid, 0, nil)
	}
	return result, nil
}

func (d *Drive) requestModItem(ctx context.Context, itemID, token, sig string) (map[string]any, error) {
	extra := make(http.Header)
	if token != "" {
		extra.Set("x-token", token)
	}
	if sig != "" {
		extra.Set("x-sig", sig)
	}
	data, _, edenErr, err := d.doJSONHeaders(ctx, http.MethodGet, "/akasha/mod/item/"+url.PathEscape(itemID), nil, extra, nil)
	if err != nil {
		return nil, CreateDriveAPIError(err, "mod item", 0)
	}
	if edenErr != nil {
		return nil, CreateDriveAPIError(edenErr.asAny(), "mod item", edenErr.Status)
	}
	if !isModItem(data) {
		return nil, newDriveAPIError(codeModInvalidResponse, "The collection item response was invalid.", 0, nil)
	}
	item, _ := data.(map[string]any)
	return item, nil
}

func remapLinkAccessError(err *DriveAPIError) error {
	if err == nil {
		return nil
	}
	code := strings.ToLower(err.Code)
	message := strings.ToLower(err.Message)
	if code == "drive_link_password_required" || strings.Contains(message, "missing_password") {
		return newDriveAPIError(codeLinkPasswordRequired, msgLinkPasswordRequired, err.Status, err)
	}
	if code == "drive_link_invalid_password" || strings.Contains(message, "invalid_password") {
		return newDriveAPIError(codeLinkInvalidPassword, msgLinkInvalidPassword, err.Status, err)
	}
	return err
}

func remapModOverviewError(err *DriveAPIError, hasPassword bool) error {
	if err == nil {
		return nil
	}
	code := strings.ToLower(err.Code)
	message := strings.ToLower(err.Message)
	if code == "drive_mod_password_required" || strings.Contains(message, "password required") || strings.Contains(message, "missing_password") {
		return newDriveAPIError(codeModPasswordRequired, msgModPasswordRequired, err.Status, err)
	}
	if code == "drive_mod_invalid_password" || strings.Contains(message, "invalid password") || strings.Contains(message, "invalid_password") {
		return newDriveAPIError(codeModInvalidPassword, msgModInvalidPassword, err.Status, err)
	}
	if hasPassword && err.Status == 500 && strings.Contains(message, "internal server error") {
		return newDriveAPIError(codeModInvalidPassword, msgCollectionBadPassword, err.Status, err)
	}
	return err
}

func asSharedLinkAccess(value any) (SharedLinkAccess, bool) {
	record, ok := asRecord(value)
	if !ok {
		return SharedLinkAccess{}, false
	}
	token, _ := record["token"].(string)
	parentRec, ok := asRecord(record["parent"])
	if !ok || token == "" {
		return SharedLinkAccess{}, false
	}
	id, _ := parentRec["id"].(string)
	name, _ := parentRec["name"].(string)
	if id == "" || name == "" {
		return SharedLinkAccess{}, false
	}
	return SharedLinkAccess{Token: token, Parent: SharedLinkParent{ID: id, Name: name}}, true
}

func asModOverview(value any) (ModOverview, bool) {
	record, ok := asRecord(value)
	if !ok {
		return ModOverview{}, false
	}
	raw, ok := record["collections"].([]any)
	if !ok {
		return ModOverview{}, false
	}
	cols := make([]ModCollection, 0, len(raw))
	for _, item := range raw {
		rec, ok := asRecord(item)
		if !ok {
			return ModOverview{}, false
		}
		id, _ := rec["id"].(string)
		name, _ := rec["name"].(string)
		rootID, _ := rec["rootId"].(string)
		if id == "" || name == "" || rootID == "" {
			return ModOverview{}, false
		}
		private, _ := rec["private"].(bool)
		cols = append(cols, ModCollection{ID: id, Name: name, RootID: rootID, Private: private})
	}
	return ModOverview{Collections: cols}, true
}

func isModItem(value any) bool {
	record, ok := asRecord(value)
	if !ok {
		return false
	}
	children, ok := record["children"].([]any)
	if !ok {
		return false
	}
	for _, child := range children {
		rec, ok := asRecord(child)
		if !ok {
			return false
		}
		id, _ := rec["id"].(string)
		name, _ := rec["name"].(string)
		if id == "" || name == "" {
			return false
		}
		if _, ok := rec["isDir"].(bool); !ok {
			return false
		}
	}
	return true
}

func asLinkContent(value any) (ListChildrenResult, bool) {
	record, ok := asRecord(value)
	if !ok {
		return ListChildrenResult{}, false
	}
	if _, ok := asRecord(record["content"]); !ok {
		return ListChildrenResult{}, false
	}
	if _, ok := record["children"].([]any); !ok {
		return ListChildrenResult{}, false
	}
	if _, ok := record["ancestors"].([]any); !ok {
		return ListChildrenResult{}, false
	}
	return normalizeListChildren(record, true)
}

func asModContent(value any) (ListChildrenResult, bool) {
	record, ok := asRecord(value)
	if !ok {
		return ListChildrenResult{}, false
	}
	if _, ok := asRecord(record["content"]); !ok {
		return ListChildrenResult{}, false
	}
	if _, ok := record["children"].([]any); !ok {
		return ListChildrenResult{}, false
	}
	return normalizeListChildren(record, false)
}

func normalizeListChildren(record map[string]any, requireAncestors bool) (ListChildrenResult, bool) {
	contentRec, ok := asRecord(record["content"])
	if !ok {
		return ListChildrenResult{}, false
	}
	content, ok := normalizeContentItem(contentRec)
	if !ok {
		return ListChildrenResult{}, false
	}
	rawChildren, _ := record["children"].([]any)
	children := make([]DriveImportContent, 0, len(rawChildren))
	for _, child := range rawChildren {
		rec, ok := asRecord(child)
		if !ok {
			return ListChildrenResult{}, false
		}
		item, ok := normalizeContentItem(rec)
		if !ok {
			return ListChildrenResult{}, false
		}
		children = append(children, item)
	}
	rawAncestors, _ := record["ancestors"].([]any)
	if requireAncestors && rawAncestors == nil {
		return ListChildrenResult{}, false
	}
	ancestors := make([]Ancestor, 0, len(rawAncestors))
	for _, raw := range rawAncestors {
		rec, ok := asRecord(raw)
		if !ok {
			continue
		}
		id := stringify(rec["id"])
		name := stringify(rec["name"])
		if id == "" || name == "" {
			continue
		}
		depth, _ := asInt(rec["depth"])
		ancestors = append(ancestors, Ancestor{
			ID:       id,
			ParentID: stringify(rec["parentId"]),
			Name:     name,
			Depth:    depth,
		})
	}
	return ListChildrenResult{Content: content, Children: children, Ancestors: ancestors}, true
}

func normalizeContentItem(raw map[string]any) (DriveImportContent, bool) {
	id := stringify(raw["id"])
	name := stringify(raw["name"])
	if id == "" || name == "" {
		return DriveImportContent{}, false
	}
	isDir, _ := raw["isDir"].(bool)
	item := DriveImportContent{
		ID:        id,
		Name:      name,
		IsDir:     isDir,
		MimeType:  firstString(raw["mimeType"], raw["mime"]),
		ParentID:  stringify(raw["parentId"]),
		CreatedAt: stringify(raw["createdAt"]),
		UpdatedAt: stringify(raw["updatedAt"]),
	}
	if raw["size"] != nil {
		if n, ok := asInt64(raw["size"]); ok {
			item.Size = &n
		}
	}
	return item, true
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func firstString(values ...any) string {
	for _, v := range values {
		if s := stringify(v); s != "" {
			return s
		}
	}
	return ""
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func (d *Drive) doJSONHeaders(ctx context.Context, method, path string, query url.Values, extra http.Header, body any) (any, http.Header, *edenError, error) {
	resp, decoded, edenErr, err := d.doRequest(ctx, method, path, query, extra, body)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
		return decoded, resp.Header.Clone(), edenErr, err
	}
	return decoded, nil, edenErr, err
}

func (d *Drive) doJSON(ctx context.Context, method, path string, query url.Values, body any) (any, *edenError, error) {
	resp, decoded, edenErr, err := d.doRequest(ctx, method, path, query, nil, body)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return decoded, edenErr, err
}

func (d *Drive) doRequest(ctx context.Context, method, path string, query url.Values, extra http.Header, body any) (*http.Response, any, *edenError, error) {
	if d == nil || d.http == nil {
		return nil, nil, nil, errDriveHTTPUnconfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rawURL := strings.TrimRight(d.http.BackendURL(), "/") + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}

	header := make(http.Header)
	for k, vs := range extra {
		header[k] = append([]string(nil), vs...)
	}
	var bodyBytes []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, nil, err
		}
		bodyBytes = raw
		header.Set("Content-Type", "application/json")
	}

	var rdr io.Reader
	if bodyBytes != nil {
		rdr = bytes.NewReader(bodyBytes)
	}
	resp, err := d.http.Fetch(ctx, rawURL, infra.FetchOptions{
		Method:            method,
		Header:            header,
		Body:              rdr,
		DisableHTTPErrors: true,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	resp.Body = io.NopCloser(strings.NewReader(""))
	decoded, decodeErr := decodeAPIBodyWithError(resp.Header.Get("Content-Type"), raw)
	if decodeErr != nil {
		diagnostic := infra.HTTPDiagnostic(method, rawURL, "decode-response", resp)
		diagnostic.Severity = infra.DiagnosticWarn
		_ = infra.ReportError(d.log, decodeErr, "Drive", diagnostic)
	}
	if decodeErr != nil && isCborContentType(resp.Header.Get("Content-Type")) && query.Get("res") != "json" {
		retryQuery := cloneValues(query)
		retryQuery.Set("res", "json")
		return d.doRequest(ctx, method, path, retryQuery, extra, body)
	}
	if resp.StatusCode >= 400 {
		return resp, decoded, &edenError{Status: resp.StatusCode, Value: decoded}, nil
	}
	return resp, decoded, nil, nil
}

func cloneValues(query url.Values) url.Values {
	if len(query) == 0 {
		return url.Values{}
	}
	cloned := make(url.Values, len(query))
	for key, values := range query {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}
