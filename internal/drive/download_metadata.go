package drive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/klauspost/compress/zstd"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

const (
	downloadFileBatchLimit = 100
	downloadBatchRootID    = "batch-root"
)

type DownloadLink struct {
	LinkID string `json:"linkId" cbor:"linkId"`
	Token  string `json:"token" cbor:"token"`
}

// DownloadModAccess carries the mod gate's credentials, sent as the x-token
// and x-sig headers.
type DownloadModAccess struct {
	Token string `json:"token,omitempty"`
	Sig   string `json:"sig,omitempty"`
	Grant string `json:"grant,omitempty"`
}

type DownloadMetadata struct {
	Root       transfer.Root           `json:"root" cbor:"root"`
	TotalBytes int64                   `json:"totalBytes" cbor:"totalBytes"`
	Files      []transfer.DownloadFile `json:"files" cbor:"files"`
	Dirs       []transfer.Directory    `json:"dirs" cbor:"dirs"`
}

type DownloadItem struct {
	ID    string `json:"id"`
	IsDir bool   `json:"isDir"`
	Name  string `json:"name"`
	Size  *int64 `json:"size,omitempty"`
}

type downloadChunkEnvelope struct {
	Compressed bool   `json:"compressed"`
	Data       string `json:"data"`
	Type       string `json:"type"`
}

// downloadMetadataSink receives a folder walk as it streams in, so the caller
// never holds more than one chunk of the file list.
type downloadMetadataSink struct {
	root  func(root transfer.Root, totalBytes int64)
	files func(files []transfer.DownloadFile) error
	dirs  func(directories []transfer.Directory)
}

func (d *Drive) directoryDownloadRequest(itemID string, link *DownloadLink) (string, http.Header) {
	query := url.Values{"uuid": []string{itemID}}
	header := make(http.Header)
	if link != nil {
		query.Set("linkId", link.LinkID)
		header.Set("nhd-link-token", link.Token)
	}
	return strings.TrimRight(d.http.BackendURL(), "/") + "/akasha/dir/download?" + query.Encode(), header
}

// modDownloadRequest addresses a mod folder walk. The backend emits the same
// frames as the drive folder walk, gated by the mod's access token and
// signature instead of a share link.
func (d *Drive) modDownloadRequest(itemID string, access DownloadModAccess) (string, http.Header) {
	header := make(http.Header)
	if access.Token != "" {
		header.Set("x-token", access.Token)
	}
	if access.Sig != "" {
		header.Set("x-sig", access.Sig)
	}
	if access.Grant != "" {
		header.Set("x-mod-download-grant", access.Grant)
	}
	return strings.TrimRight(d.http.BackendURL(), "/") + "/akasha/mod/download/" + url.PathEscape(itemID), header
}

func (d *Drive) streamDownloadMetadata(
	ctx context.Context,
	rawURL string,
	header http.Header,
	sink downloadMetadataSink,
) error {
	response, err := d.http.Fetch(
		ctx,
		rawURL,
		infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true},
	)
	if err != nil {
		return err
	}
	if response.Body == nil {
		return errors.New("download metadata stream is empty")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, readErr := io.ReadAll(response.Body)
		return infra.WithCause(
			CreateDriveAPIError(
				decodeAPIValue(response.Header.Get("Content-Type"), raw),
				"download metadata",
				response.StatusCode,
			),
			infra.AnnotateError(readErr, infra.HTTPDiagnostic(http.MethodGet, "", "read-error-response", response)),
		)
	}
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return err
	}
	defer decoder.Close()

	hasRoot := false
	parseErr := parseSSE(response.Body, func(event, data string) error {
		switch event {
		case "metadata":
			var head struct {
				Root       transfer.Root `json:"root"`
				TotalBytes int64         `json:"totalBytes"`
			}
			if err := json.Unmarshal([]byte(data), &head); err != nil {
				return fmt.Errorf("decode download metadata: %w", err)
			}
			if head.Root.ID != "" {
				hasRoot = true
				sink.root(head.Root, head.TotalBytes)
			}
		case "files":
			var files []transfer.DownloadFile
			if err := decodeDownloadChunk(decoder, data, &files); err != nil {
				return fmt.Errorf("decode download files: %w", err)
			}
			return sink.files(files)
		case "dirs":
			var directories []transfer.Directory
			if err := decodeDownloadChunk(decoder, data, &directories); err != nil {
				return fmt.Errorf("decode download directories: %w", err)
			}
			sink.dirs(directories)
		case "error":
			if data == "" {
				data = "download metadata stream failed"
			}
			return errors.New(data)
		}
		return nil
	})
	if parseErr != nil {
		return parseErr
	}
	if !hasRoot {
		return errors.New("root directory information was not received")
	}
	return nil
}

func decodeDownloadChunk(decoder *zstd.Decoder, eventData string, target any) error {
	var envelope downloadChunkEnvelope
	if err := json.Unmarshal([]byte(eventData), &envelope); err != nil {
		return err
	}
	if !envelope.Compressed {
		return json.Unmarshal([]byte(envelope.Data), target)
	}
	compressed, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return err
	}
	raw, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return err
	}
	if strings.EqualFold(envelope.Type, "cbor") {
		return cbor.Unmarshal(raw, target)
	}
	return json.Unmarshal(bytes.TrimSpace(raw), target)
}

func (d *Drive) fetchFileDownloadMetadataBatch(
	ctx context.Context,
	ids []string,
	link *DownloadLink,
) ([]transfer.DownloadFile, error) {
	if len(ids) == 0 {
		return []transfer.DownloadFile{}, nil
	}
	query := url.Values{}
	header := make(http.Header)
	if link != nil {
		query.Set("linkId", link.LinkID)
		header.Set("nhd-link-token", link.Token)
	}
	data, _, edenErr, err := d.doJSONHeaders(
		ctx,
		http.MethodPost,
		"/akasha/file/downloads",
		query,
		header,
		map[string]any{"ids": ids},
	)
	if err != nil {
		return nil, err
	}
	if edenErr != nil {
		return nil, CreateDriveAPIError(edenErr.asAny(), "file downloads", edenErr.Status)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var files []transfer.DownloadFile
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil, fmt.Errorf("decode file downloads: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("file download URL not received")
	}
	return files, nil
}

// enumerateDownload walks everything the download covers and writes the file
// list to a spool, reporting the running totals as chunks arrive.
func (d *Drive) enumerateDownload(
	ctx context.Context,
	params StartDownloadParams,
	layout downloadLayout,
	spoolPath string,
	onProgress func(files int, totalBytes int64),
) (_ *downloadPlan, err error) {
	if params.Data == nil && (d == nil || d.http == nil) {
		return nil, errDriveHTTPUnconfigured
	}
	writer, err := newSpoolWriter(spoolPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			writer.abort()
		}
	}()
	plan := writer.plan
	fs := driveFS(d)
	items := params.Items
	single := len(items) == 1
	batchRoot := downloadBatchRootID

	sanitize := func(name string) string {
		return fs.SanitizeWindowsFilename(name, " ")
	}
	add := func(file transfer.DownloadFile) error {
		if file.FileID == "" {
			file.FileID = file.ID
		}
		return writer.add(file)
	}
	// rootName settles the name of a single item. A folder the caller could
	// not name up front still must not collide with what is already there.
	rootName := func(serverName string) string {
		if layout.rootName != "" {
			return layout.rootName
		}
		if items[0].IsDir {
			return fs.GetUniqueName(sanitize(serverName), layout.used)
		}
		return sanitize(serverName)
	}
	topLevelName := func(id, serverName string) string {
		if name := layout.names[id]; name != "" {
			return name
		}
		name := fs.GetUniqueName(sanitize(serverName), layout.used)
		layout.used = append(layout.used, name)
		return name
	}
	streamFolder := func(rawURL string, header http.Header) error {
		return d.streamDownloadMetadata(ctx, rawURL, header, downloadMetadataSink{
			root: func(root transfer.Root, totalBytes int64) {
				directory := transfer.Directory{ID: root.ID, ParentID: root.ParentID}
				if single {
					root.Name = rootName(root.Name)
					plan.root = root
					directory.Name = root.Name
				} else {
					directory.ParentID = &batchRoot
					directory.Name = topLevelName(root.ID, root.Name)
				}
				plan.dirs = append(plan.dirs, directory)
				plan.totalBytes += totalBytes
				onProgress(plan.fileCount, plan.totalBytes)
			},
			files: func(files []transfer.DownloadFile) error {
				for _, file := range files {
					file.Name = sanitize(file.Name)
					if err := add(file); err != nil {
						return err
					}
				}
				onProgress(plan.fileCount, plan.totalBytes)
				return nil
			},
			dirs: func(directories []transfer.Directory) {
				for _, directory := range directories {
					directory.Name = sanitize(directory.Name)
					plan.dirs = append(plan.dirs, directory)
				}
			},
		})
	}

	switch {
	case params.Data != nil:
		data := params.Data
		plan.root = data.Root
		plan.totalBytes = data.TotalBytes
		// A root without a name is not a folder: its children land in the target
		// itself, so an empty name must stay empty.
		switch {
		case single && (layout.rootName != "" || data.Root.Name != ""):
			plan.root.Name = rootName(data.Root.Name)
		case data.Root.Name != "":
			plan.root.Name = sanitize(data.Root.Name)
		}
		isTopLevel := func(parentID *string) bool {
			return !single && parentID != nil && *parentID == downloadBatchRootID
		}

		plan.dirs = make([]transfer.Directory, 0, len(data.Dirs))
		for _, directory := range data.Dirs {
			switch {
			case single && directory.ID == plan.root.ID:
				directory.Name = plan.root.Name
			case isTopLevel(directory.ParentID):
				directory.Name = topLevelName(directory.ID, directory.Name)
			default:
				directory.Name = sanitize(directory.Name)
			}
			plan.dirs = append(plan.dirs, directory)
		}

		onlyFile := len(data.Dirs) == 0 && len(data.Files) == 1
		for _, file := range data.Files {
			switch {
			case single && !items[0].IsDir && len(data.Files) == 1 && plan.root.Name != "":
				file.Name = plan.root.Name
			case isTopLevel(file.ParentID):
				file.Name = topLevelName(file.ID, file.Name)
			default:
				file.Name = sanitize(file.Name)
			}
			if onlyFile || plan.root.Name == "" && file.ParentID != nil && *file.ParentID == plan.root.ID {
				plan.rootFiles = append(plan.rootFiles, file.Name)
			}
			if err = add(file); err != nil {
				return nil, err
			}
		}
	case params.Mod != nil:
		if !single || !items[0].IsDir {
			return nil, errors.New("mod download requires a single folder")
		}
		rawURL, header := d.modDownloadRequest(items[0].ID, *params.Mod)
		if err = streamFolder(rawURL, header); err != nil {
			return nil, err
		}
	case single && items[0].IsDir:
		rawURL, header := d.directoryDownloadRequest(items[0].ID, params.Link)
		if err = streamFolder(rawURL, header); err != nil {
			return nil, err
		}
	case single:
		var files []transfer.DownloadFile
		files, err = d.fetchFileDownloadMetadataBatch(ctx, []string{items[0].ID}, params.Link)
		if err != nil {
			return nil, err
		}
		file := files[0]
		file.Name = rootName(file.Name)
		plan.root = transfer.Root{ID: file.ID, Name: file.Name}
		plan.totalBytes = transfer.LogicalFileBytes(file)
		plan.rootFiles = []string{file.Name}
		if err = add(file); err != nil {
			return nil, err
		}
	default:
		plan.root = transfer.Root{ID: downloadBatchRootID}
		fileIDs := make([]string, 0, len(items))
		for _, item := range items {
			if !item.IsDir {
				fileIDs = append(fileIDs, item.ID)
				continue
			}
			rawURL, header := d.directoryDownloadRequest(item.ID, params.Link)
			if err = streamFolder(rawURL, header); err != nil {
				return nil, err
			}
		}

		for ids := range slices.Chunk(fileIDs, downloadFileBatchLimit) {
			var files []transfer.DownloadFile
			files, err = d.fetchFileDownloadMetadataBatch(ctx, ids, params.Link)
			if err != nil {
				return nil, err
			}
			returned := make(map[string]struct{}, len(files))
			for _, file := range files {
				returned[file.ID] = struct{}{}
			}
			for _, id := range ids {
				if _, exists := returned[id]; !exists {
					return nil, errors.New("some selected files could not be fetched")
				}
			}
			for _, file := range files {
				file.ParentID = &batchRoot
				file.Name = topLevelName(file.ID, file.Name)
				plan.totalBytes += transfer.LogicalFileBytes(file)
				plan.rootFiles = append(plan.rootFiles, file.Name)
				if err = add(file); err != nil {
					return nil, err
				}
			}
			onProgress(plan.fileCount, plan.totalBytes)
		}
	}

	onProgress(plan.fileCount, plan.totalBytes)
	return writer.finish()
}
