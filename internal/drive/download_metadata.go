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

const downloadFileBatchLimit = 100

type DownloadLink struct {
	LinkID string `json:"linkId" cbor:"linkId"`
	Token  string `json:"token" cbor:"token"`
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

func (d *Drive) fetchDirectoryDownloadMetadata(ctx context.Context, itemID string, link *DownloadLink) (DownloadMetadata, error) {
	if d == nil || d.http == nil {
		return DownloadMetadata{}, errDriveHTTPUnconfigured
	}
	query := url.Values{"uuid": []string{itemID}}
	header := make(http.Header)
	if link != nil {
		query.Set("linkId", link.LinkID)
		header.Set("nhd-link-token", link.Token)
	}
	rawURL := strings.TrimRight(d.http.BackendURL(), "/") + "/akasha/dir/download?" + query.Encode()
	response, err := d.http.Fetch(ctx, rawURL, infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true})
	if err != nil {
		return DownloadMetadata{}, err
	}
	if response.Body == nil {
		return DownloadMetadata{}, errors.New("download metadata stream is empty")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		raw, _ := io.ReadAll(response.Body)
		return DownloadMetadata{}, CreateDriveAPIError(decodeAPIValue(response.Header.Get("Content-Type"), raw), "download metadata", response.StatusCode)
	}
	metadata := DownloadMetadata{Files: []transfer.DownloadFile{}, Dirs: []transfer.Directory{}}
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
			metadata.Root = head.Root
			metadata.TotalBytes = head.TotalBytes
			hasRoot = head.Root.ID != ""
		case "files":
			var files []transfer.DownloadFile
			if err := decodeDownloadChunk(data, &files); err != nil {
				return fmt.Errorf("decode download files: %w", err)
			}
			for index := range files {
				if files[index].FileID == "" {
					files[index].FileID = files[index].ID
				}
			}
			metadata.Files = append(metadata.Files, files...)
		case "dirs":
			var directories []transfer.Directory
			if err := decodeDownloadChunk(data, &directories); err != nil {
				return fmt.Errorf("decode download directories: %w", err)
			}
			metadata.Dirs = append(metadata.Dirs, directories...)
		case "error":
			if data == "" {
				data = "download metadata stream failed"
			}
			return errors.New(data)
		}
		return nil
	})
	if parseErr != nil {
		return DownloadMetadata{}, parseErr
	}
	if !hasRoot {
		return DownloadMetadata{}, errors.New("root directory information was not received")
	}
	return metadata, nil
}

func decodeDownloadChunk(eventData string, target any) error {
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
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return err
	}
	raw, err := decoder.DecodeAll(compressed, nil)
	decoder.Close()
	if err != nil {
		return err
	}
	if strings.EqualFold(envelope.Type, "cbor") {
		return cbor.Unmarshal(raw, target)
	}
	return json.Unmarshal(bytes.TrimSpace(raw), target)
}

func (d *Drive) fetchFileDownloadMetadataBatch(ctx context.Context, ids []string, link *DownloadLink) ([]transfer.DownloadFile, error) {
	if len(ids) == 0 {
		return []transfer.DownloadFile{}, nil
	}
	query := url.Values{}
	header := make(http.Header)
	if link != nil {
		query.Set("linkId", link.LinkID)
		header.Set("nhd-link-token", link.Token)
	}
	data, _, edenErr, err := d.doJSONHeaders(ctx, http.MethodPost, "/akasha/file/downloads", query, header, map[string]any{"ids": ids})
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
	for index := range files {
		if files[index].FileID == "" {
			files[index].FileID = files[index].ID
		}
	}
	return files, nil
}

func (d *Drive) fetchDownloadMetadata(ctx context.Context, items []DownloadItem, link *DownloadLink) (DownloadMetadata, error) {
	unique := make(map[string]DownloadItem, len(items))
	ordered := make([]DownloadItem, 0, len(items))
	for _, item := range items {
		if _, exists := unique[item.ID]; exists {
			continue
		}
		unique[item.ID] = item
		ordered = append(ordered, item)
	}
	if len(ordered) == 1 {
		item := ordered[0]
		if item.IsDir {
			metadata, err := d.fetchDirectoryDownloadMetadata(ctx, item.ID, link)
			if err != nil {
				return DownloadMetadata{}, err
			}
			metadata.Dirs = append([]transfer.Directory{{ID: metadata.Root.ID, ParentID: metadata.Root.ParentID, Name: metadata.Root.Name}}, metadata.Dirs...)
			return metadata, nil
		}
		files, err := d.fetchFileDownloadMetadataBatch(ctx, []string{item.ID}, link)
		if err != nil {
			return DownloadMetadata{}, err
		}
		file := files[0]
		return DownloadMetadata{
			Root:       transfer.Root{ID: file.ID, Name: file.Name},
			TotalBytes: transfer.LogicalFileBytes(file),
			Files:      []transfer.DownloadFile{file},
			Dirs:       []transfer.Directory{},
		}, nil
	}

	const batchRootID = "batch-root"
	metadata := DownloadMetadata{
		Root:  transfer.Root{ID: batchRootID, Name: ""},
		Files: []transfer.DownloadFile{},
		Dirs:  []transfer.Directory{},
	}
	fileItems := make([]DownloadItem, 0)
	for _, item := range ordered {
		if item.IsDir {
			folder, err := d.fetchDirectoryDownloadMetadata(ctx, item.ID, link)
			if err != nil {
				return DownloadMetadata{}, err
			}
			parent := batchRootID
			metadata.Dirs = append(metadata.Dirs, transfer.Directory{ID: folder.Root.ID, ParentID: &parent, Name: folder.Root.Name})
			metadata.Dirs = append(metadata.Dirs, folder.Dirs...)
			metadata.Files = append(metadata.Files, folder.Files...)
			metadata.TotalBytes += folder.TotalBytes
			continue
		}
		fileItems = append(fileItems, item)
	}
	for start := 0; start < len(fileItems); start += downloadFileBatchLimit {
		end := min(start+downloadFileBatchLimit, len(fileItems))
		ids := make([]string, end-start)
		for index, item := range fileItems[start:end] {
			ids[index] = item.ID
		}
		files, err := d.fetchFileDownloadMetadataBatch(ctx, ids, link)
		if err != nil {
			return DownloadMetadata{}, err
		}
		returned := make(map[string]struct{}, len(files))
		for index := range files {
			returned[files[index].ID] = struct{}{}
			parent := batchRootID
			files[index].ParentID = &parent
			metadata.TotalBytes += transfer.LogicalFileBytes(files[index])
		}
		for _, id := range ids {
			if _, exists := returned[id]; !exists {
				return DownloadMetadata{}, errors.New("some selected files could not be fetched")
			}
		}
		metadata.Files = append(metadata.Files, files...)
	}
	metadata.Files = slices.Clip(metadata.Files)
	metadata.Dirs = slices.Clip(metadata.Dirs)
	return metadata, nil
}
