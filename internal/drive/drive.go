package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

const defaultDirRetries = 3

// SearchParams matches Electron drive.get.search.
type SearchParams struct {
	Q      string `json:"q"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// Options wire Drive to the existing HTTP client and filename check.
type Options struct {
	HTTP             *infra.Client
	FS               *platform.FS
	Log              *infra.Log
	Transfer         *transfer.Transfer
	Download         *infra.Download
	ParallelDownload *infra.ParallelDownloader
	Archive          *infra.Archive
	Dialog           *platform.Dialog
	UploadSettings   UploadSettings
	PathSelector     PathSelector
	EventEmit        func(name string, data ...any)
	Sleep            func(context.Context, time.Duration) error
	Now              func() time.Time
	DirRetries       *int
}

// Drive is the Wails service for Electron Drive CRUD, copy/import, and
// transfer preparation/execution.
type Drive struct {
	http             *infra.Client
	fs               *platform.FS
	log              *infra.Log
	transfer         *transfer.Transfer
	download         *infra.Download
	parallelDownload *infra.ParallelDownloader
	archive          *infra.Archive
	dialog           *platform.Dialog
	settings         UploadSettings
	paths            PathSelector
	eventEmit        func(string, ...any)
	sleep            func(context.Context, time.Duration) error
	now              func() time.Time
	dirRetries       int

	mu            sync.Mutex
	ops           map[string]*copyOperation
	uploadRulesMu sync.Mutex
	uploadRules   *UploadRules
}

func New() *Drive {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *Drive {
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	retries := defaultDirRetries
	if opts.DirRetries != nil {
		retries = *opts.DirRetries
	}
	parallelDownload := opts.ParallelDownload
	if parallelDownload == nil {
		parallelDownload = infra.NewParallelDownloader()
	}
	if opts.HTTP != nil {
		if parallelDownload.Client == nil {
			parallelDownload.Client = opts.HTTP.HTTPClient()
		}
		if parallelDownload.GetHeaders == nil {
			parallelDownload.GetHeaders = func(rawURL string) (map[string]string, error) {
				header, err := opts.HTTP.GetHeaders(rawURL)
				if err != nil {
					return nil, err
				}
				out := make(map[string]string, len(header))
				for key := range header {
					out[key] = header.Get(key)
				}
				return out, nil
			}
		}
	}
	dialog := opts.Dialog
	if dialog == nil {
		dialog = platform.NewDialog()
	}
	return &Drive{
		http:             opts.HTTP,
		fs:               opts.FS,
		log:              opts.Log,
		transfer:         opts.Transfer,
		download:         opts.Download,
		parallelDownload: parallelDownload,
		archive:          opts.Archive,
		dialog:           dialog,
		settings:         opts.UploadSettings,
		paths:            opts.PathSelector,
		eventEmit:        opts.EventEmit,
		sleep:            sleep,
		now:              now,
		dirRetries:       retries,
		ops:              make(map[string]*copyOperation),
	}
}

type UploadSettings interface {
	GetUploadConcurrency(context.Context) (int, error)
	GetDownloadConcurrency(context.Context) (int, error)
}

// PathSelector is Electron desktop.lib.pathSelector for download destination
// selection. A nil path means the user cancelled.
type PathSelector interface {
	SelectDownloadPath(ctx context.Context, suggestedName, source string, suggestedNames []string, selectFile bool) (path *string, fileName *string, err error)
}

//wails:ignore
func (d *Drive) UseHTTP(c *infra.Client) {
	if d == nil {
		return
	}
	d.http = c
}

//wails:ignore
func (d *Drive) UseFS(fs *platform.FS) {
	if d == nil {
		return
	}
	d.fs = fs
}

//wails:ignore
func (d *Drive) UseLog(log *infra.Log) {
	if d == nil {
		return
	}
	d.log = log
}

//wails:ignore
func (d *Drive) UsePathSelector(paths PathSelector) {
	if d == nil {
		return
	}
	d.paths = paths
}

func (d *Drive) GetItem(ctx context.Context, itemID string) (result any, err error) {
	defer normalizeDriveBoundaryError(&err, "get:item")
	data, edenErr, err := d.doJSON(ctx, http.MethodGet, "/akasha/content/"+url.PathEscape(itemID), nil, nil)
	if err != nil {
		return nil, CreateDriveAPIError(err, "get:item", 0)
	}
	if edenErr != nil {
		return nil, CreateDriveAPIError(edenErr.asAny(), "get:item", edenErr.Status)
	}
	return data, nil
}

func (d *Drive) Search(ctx context.Context, itemID string, params SearchParams) (result any, err error) {
	defer normalizeDriveBoundaryError(&err, "get:search")
	query := url.Values{}
	query.Set("q", params.Q)
	if params.Limit > 0 {
		query.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Cursor != "" {
		query.Set("cursor", params.Cursor)
	}
	data, edenErr, err := d.doJSON(ctx, http.MethodGet, "/akasha/content/"+url.PathEscape(itemID)+"/search", query, nil)
	if err != nil {
		return nil, CreateDriveAPIError(err, "get:search", 0)
	}
	if edenErr != nil {
		return nil, CreateDriveAPIError(edenErr.asAny(), "get:search", edenErr.Status)
	}
	return data, nil
}

func (d *Drive) CreateDir(ctx context.Context, parentID, name string) (result any, err error) {
	defer normalizeDriveBoundaryError(&err, "post:dir")
	if err := d.assertFilename(name); err != nil {
		return nil, err
	}
	body := map[string]any{
		"parentId": parentID,
		"dirs": []map[string]string{{
			"path": parentID,
			"name": name,
		}},
	}
	var last any
	for attempt := 0; attempt <= d.dirRetries; attempt++ {
		if ctx.Err() != nil {
			if last != nil {
				return nil, CreateDriveAPIError(last, "post:dir", 0)
			}
			return nil, CreateDriveAPIError(ctx.Err(), "post:dir", 0)
		}
		data, edenErr, err := d.doJSON(ctx, http.MethodPost, "/akasha/dir/create_many", nil, body)
		if err == nil && edenErr == nil {
			return data, nil
		}
		if err != nil {
			last = err
		} else {
			last = edenErr.asAny()
		}
		if attempt < d.dirRetries {
			delay := time.Duration(1<<attempt) * time.Second
			if sleepErr := d.sleep(ctx, delay); sleepErr != nil {
				return nil, CreateDriveAPIError(last, "post:dir", 0)
			}
		}
	}
	return nil, CreateDriveAPIError(last, "post:dir", 0)
}

func (d *Drive) Rename(ctx context.Context, itemID, name string) (result any, err error) {
	defer normalizeDriveBoundaryError(&err, "patch:rename")
	if err := d.assertFilename(name); err != nil {
		return nil, err
	}
	body := map[string]string{"rename": name}
	data, edenErr, err := d.doJSON(ctx, http.MethodPost, "/akasha/content/rename/"+url.PathEscape(itemID), nil, body)
	if err != nil {
		return nil, CreateDriveAPIError(err, "patch:rename", 0)
	}
	if edenErr != nil {
		return nil, CreateDriveAPIError(edenErr.asAny(), "patch:rename", edenErr.Status)
	}
	return data, nil
}

func (d *Drive) DeleteItems(ctx context.Context, ids []string, action string) (outcome BatchDeletionOutcome, err error) {
	defer normalizeDriveBoundaryError(&err, "delete:items")
	switch action {
	case "trash":
		return d.trashItems(ctx, ids)
	case "delete":
		return d.deleteItems(ctx, ids)
	default:
		return BatchDeletionOutcome{}, invalidActionError()
	}
}

func (d *Drive) trashItems(ctx context.Context, ids []string) (BatchDeletionOutcome, error) {
	_, edenErr, err := d.doJSON(ctx, http.MethodPost, "/akasha/content/trash/trash_many", nil, map[string]any{
		"uuids": ids,
	})
	if err != nil {
		return BatchDeletionOutcome{}, CreateDriveAPIError(err, "delete:items", 0)
	}
	if edenErr != nil {
		return BatchDeletionOutcome{}, CreateDriveAPIError(edenErr.Value, "delete:items", edenErr.Status)
	}
	outIDs := append([]string(nil), ids...)
	return BatchDeletionOutcome{RequestedIDs: outIDs, AcceptedIDs: outIDs, Jobs: []DeletionAccepted{}}, nil
}

func (d *Drive) deleteItems(ctx context.Context, ids []string) (BatchDeletionOutcome, error) {
	outcome := runDeletionBatches(ids, func(page []string) (*DeletionAccepted, error) {
		return d.deleteManyPage(ctx, page)
	}, DeletionBatchSize)
	accepted, err := requireBatchAccepted(outcome)
	if err != nil {
		return BatchDeletionOutcome{}, CreateDriveAPIError(err, "delete:items", 0)
	}
	if accepted.ErrorMessage != "" && d.log != nil {
		d.log.Warn(formatPartialDeleteLog(accepted, "delete"), "Drive:delete:items:partial")
	}
	return accepted, nil
}

func (d *Drive) deleteManyPage(ctx context.Context, page []string) (*DeletionAccepted, error) {
	data, edenErr, err := d.doJSON(ctx, http.MethodPost, "/akasha/content/delete_many", nil, map[string]any{
		"uuids": page,
	})
	if err != nil {
		return nil, err
	}
	result, err := resolveDeletionResult(data, edenErr)
	if err != nil {
		return nil, err
	}
	return requireAccepted(result)
}

func (d *Drive) assertFilename(name string) error {
	fs := d.fs
	if fs == nil {
		fs = platform.NewFS()
	}
	return fs.AssertValidWindowsFilename(name)
}

func (e *edenError) asAny() any {
	if e == nil {
		return nil
	}
	return map[string]any{
		"status": e.Status,
		"value":  e.Value,
	}
}

func isCborContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "cbor")
}

func decodeAPIValue(contentType string, raw []byte) any {
	value, _ := decodeAPIBody(contentType, raw)
	return value
}

func decodeAPIBody(contentType string, raw []byte) (any, bool) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, true
	}
	if isCborContentType(contentType) {
		mode, err := (cbor.DecOptions{DefaultMapType: reflect.TypeOf(map[string]any(nil))}).DecMode()
		if err != nil {
			return nil, false
		}
		var value any
		if err := mode.Unmarshal(raw, &value); err != nil {
			return nil, false
		}
		return value, true
	}
	var value any
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	return strings.TrimSpace(string(raw)), true
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
