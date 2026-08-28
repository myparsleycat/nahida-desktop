package drive

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

// CopyFromURLParams is Electron DriveCopyFromUrlParams.
type CopyFromURLParams struct {
	URL                     string   `json:"url"`
	DestinationID           string   `json:"destinationId"`
	Password                string   `json:"password,omitempty"`
	CollectionID            string   `json:"collectionId,omitempty"`
	ItemID                  string   `json:"itemId,omitempty"`
	CreateCollectionFolders *bool    `json:"createCollectionFolders,omitempty"`
	OperationID             string   `json:"operationId,omitempty"`
	SelectedIDs             []string `json:"selectedIds,omitempty"`
}

func (p CopyFromURLParams) createFolders() bool {
	if p.CreateCollectionFolders == nil {
		return true
	}
	return *p.CreateCollectionFolders
}

// CopyFromURLResult is Electron DriveCopyFromUrlResult.
type CopyFromURLResult struct {
	Source        string `json:"source"`
	Copied        int    `json:"copied"`
	DestinationID string `json:"destinationId"`
}

type copyOperation struct {
	cancel       context.CancelFunc
	source       string
	lastProgress *DriveCopyProgress
	canceled     bool
}

// DriveCopyProgress is Electron DriveCopyProgress.
type DriveCopyProgress struct {
	OperationID string `json:"operationId"`
	Source      string `json:"source"`
	Phase       string `json:"phase"`
	Current     int    `json:"current"`
	Total       int    `json:"total"`
	ItemName    string `json:"itemName,omitempty"`
	CopiedFiles int    `json:"copiedFiles,omitempty"`
	Message     string `json:"message,omitempty"`
}

func (d *Drive) beginCopy(parent context.Context, operationID, source string) (context.Context, string, context.CancelFunc, func()) {
	id := strings.TrimSpace(operationID)
	if id == "" {
		id = newOperationID()
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	d.mu.Lock()
	if d.ops == nil {
		d.ops = make(map[string]*copyOperation)
	}
	d.ops[id] = &copyOperation{cancel: cancel, source: source}
	d.mu.Unlock()
	return ctx, id, cancel, func() {
		d.mu.Lock()
		delete(d.ops, id)
		d.mu.Unlock()
		cancel()
	}
}

func (d *Drive) emitCopyProgress(operationID string, progress DriveCopyProgress) {
	progress.OperationID = operationID
	d.mu.Lock()
	if op := d.ops[operationID]; op != nil {
		copy := progress
		op.lastProgress = &copy
	}
	d.mu.Unlock()
	if d.log != nil {
		d.log.Info(progress, "Drive:CopyFromUrl:Progress")
	}
	if d.eventEmit != nil {
		d.eventEmit("drive:copy-progress", progress)
	}
}

func (d *Drive) emitCopyCanceledProgress(operationID string) {
	d.mu.Lock()
	op := d.ops[operationID]
	if op == nil || op.canceled {
		d.mu.Unlock()
		return
	}
	op.canceled = true
	progress := DriveCopyProgress{Source: op.source, Phase: "canceled", Total: 1}
	if op.lastProgress != nil {
		progress.Current = op.lastProgress.Current
		progress.Total = op.lastProgress.Total
		progress.ItemName = op.lastProgress.ItemName
		progress.CopiedFiles = op.lastProgress.CopiedFiles
	}
	d.mu.Unlock()
	d.emitCopyProgress(operationID, progress)
}

// CancelCopyFromURL is Electron drive.fn.cancelCopyFromUrl.
func (d *Drive) CancelCopyFromURL(operationID string) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	op, ok := d.ops[operationID]
	d.mu.Unlock()
	if !ok || op == nil {
		if d.log != nil {
			d.log.Warn(map[string]any{"operationId": operationID}, "Drive:CopyFromUrl:CancelOperationNotFound")
		}
		return false
	}
	if d.log != nil {
		d.log.Info(map[string]any{"operationId": operationID, "source": op.source}, "Drive:CopyFromUrl:CancelRequested")
	}
	op.cancel()
	d.emitCopyCanceledProgress(operationID)
	return true
}

func copyCanceledError() *DriveAPIError {
	return newDriveAPIError(codeCopyCanceled, msgCopyCanceled, 0, nil)
}

func isCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(err.Error(), "context canceled")
}

// CopyFromURL is Electron drive.fn.copyFromUrl.
func (d *Drive) CopyFromURL(ctx context.Context, params CopyFromURLParams) (result CopyFromURLResult, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:copyFromUrl")
	source, err := ParseDriveSourceUrl(params.URL)
	if err != nil {
		return CopyFromURLResult{}, err
	}
	opCtx, operationID, _, done := d.beginCopy(ctx, params.OperationID, source.Type)
	defer done()
	params.OperationID = operationID
	d.emitCopyProgress(operationID, DriveCopyProgress{Source: source.Type, Phase: "preparing", Total: max(1, len(params.SelectedIDs))})

	result, err = d.runCopyFromURL(opCtx, params, source)
	if err != nil {
		if opCtx.Err() != nil || isCanceled(err) {
			d.emitCopyCanceledProgress(operationID)
			d.setImportTransferFailure(operationID, transfer.StatusCanceled, "")
			return CopyFromURLResult{}, copyCanceledError()
		}
		d.emitCopyFailure(operationID, err)
		d.setImportTransferFailure(operationID, transfer.StatusError, err.Error())
		return CopyFromURLResult{}, err
	}
	d.emitCopyProgress(operationID, DriveCopyProgress{Source: source.Type, Phase: "completed", Current: result.Copied, Total: max(1, result.Copied), CopiedFiles: result.Copied})
	return result, nil
}

// CopyFromURLMany is Electron drive.fn.copyFromUrlMany.
func (d *Drive) CopyFromURLMany(ctx context.Context, params CopyFromURLParams) (result CopyFromURLResult, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:copyFromUrlMany")
	if len(params.SelectedIDs) == 0 {
		return d.CopyFromURL(ctx, params)
	}
	source, err := ParseDriveSourceUrl(params.URL)
	if err != nil {
		return CopyFromURLResult{}, err
	}
	opCtx, operationID, _, done := d.beginCopy(ctx, params.OperationID, source.Type)
	defer done()
	params.OperationID = operationID
	d.emitCopyProgress(operationID, DriveCopyProgress{Source: source.Type, Phase: "preparing", Total: len(params.SelectedIDs)})

	result, err = d.runCopyFromURLMany(opCtx, params, source)
	if err != nil {
		if opCtx.Err() != nil || isCanceled(err) {
			d.emitCopyCanceledProgress(operationID)
			d.setImportTransferFailure(operationID, transfer.StatusCanceled, "")
			return CopyFromURLResult{}, copyCanceledError()
		}
		d.emitCopyFailure(operationID, err)
		d.setImportTransferFailure(operationID, transfer.StatusError, err.Error())
		return CopyFromURLResult{}, err
	}
	d.emitCopyProgress(operationID, DriveCopyProgress{Source: source.Type, Phase: "completed", Current: len(params.SelectedIDs), Total: len(params.SelectedIDs), CopiedFiles: result.Copied})
	return result, nil
}

func (d *Drive) emitCopyFailure(operationID string, err error) {
	d.mu.Lock()
	op := d.ops[operationID]
	progress := DriveCopyProgress{Phase: "error", Total: 1, Message: err.Error()}
	if op != nil {
		progress.Source = op.source
		if op.lastProgress != nil {
			progress.Current = op.lastProgress.Current
			progress.Total = op.lastProgress.Total
			progress.ItemName = op.lastProgress.ItemName
			progress.CopiedFiles = op.lastProgress.CopiedFiles
		}
	}
	d.mu.Unlock()
	d.emitCopyProgress(operationID, progress)
}

func (d *Drive) setImportTransferFailure(operationID string, status transfer.Status, message string) {
	if d.transfer == nil {
		return
	}
	record, ok := d.transfer.Get(operationID)
	if !ok || record.Status == transfer.StatusCanceled || record.Status == transfer.StatusPaused {
		return
	}
	updates := transfer.Updates{Status: &status}
	if message != "" {
		updates.Error = &message
	}
	_ = d.transfer.Update(operationID, updates)
}

func (d *Drive) runCopyFromURL(ctx context.Context, params CopyFromURLParams, source DriveSource) (CopyFromURLResult, error) {
	if ctx.Err() != nil {
		return CopyFromURLResult{}, copyCanceledError()
	}
	password := params.Password
	if source.Type == "link" {
		if len(params.SelectedIDs) > 0 {
			access, err := d.requestSharedLinkAccess(ctx, source.ID, password)
			if err != nil {
				return CopyFromURLResult{}, err
			}
			copied, err := d.copyRemoteImportMany(ctx, remoteImportMany{
				Mode:          "link",
				SrcIDs:        params.SelectedIDs,
				DestinationID: params.DestinationID,
				LinkID:        source.ID,
				LinkToken:     access.Token,
				OperationID:   params.OperationID,
				SourceNames:   params.SelectedIDs,
			})
			if err != nil {
				return CopyFromURLResult{}, err
			}
			return CopyFromURLResult{Source: "link", Copied: copied, DestinationID: params.DestinationID}, nil
		}
		access, err := d.requestSharedLinkAccess(ctx, source.ID, password)
		if err != nil {
			return CopyFromURLResult{}, err
		}
		sourceID := params.ItemID
		if sourceID == "" {
			sourceID = access.Parent.ID
		}
		copied, err := d.copyRemoteImport(ctx, remoteImport{
			Mode:          "link",
			SourceID:      sourceID,
			DestinationID: params.DestinationID,
			LinkID:        source.ID,
			LinkToken:     access.Token,
			OperationID:   params.OperationID,
			SourceName:    access.Parent.Name,
			TotalItems:    1,
		})
		if err != nil {
			return CopyFromURLResult{}, err
		}
		return CopyFromURLResult{Source: "link", Copied: copied, DestinationID: params.DestinationID}, nil
	}

	if len(params.SelectedIDs) > 0 {
		modAccess, err := d.requestModOverview(ctx, source.ID, password)
		if err != nil {
			return CopyFromURLResult{}, err
		}
		copied, err := d.copyRemoteImportMany(ctx, remoteImportMany{
			Mode:          "mod",
			SrcIDs:        params.SelectedIDs,
			DestinationID: params.DestinationID,
			ModToken:      modAccess.Token,
			ModSig:        modAccess.Sig,
			OperationID:   params.OperationID,
			SourceNames:   params.SelectedIDs,
		})
		if err != nil {
			return CopyFromURLResult{}, err
		}
		return CopyFromURLResult{Source: "mod", Copied: copied, DestinationID: params.DestinationID}, nil
	}

	modAccess, err := d.requestModOverview(ctx, source.ID, password)
	if err != nil {
		return CopyFromURLResult{}, err
	}
	overview := modAccess.Data
	public := make([]ModCollection, 0, len(overview.Collections))
	for _, col := range overview.Collections {
		if !col.Private {
			public = append(public, col)
		}
	}
	selected := public
	if params.CollectionID != "" {
		selected = selected[:0]
		for _, col := range public {
			if col.ID == params.CollectionID {
				selected = append(selected, col)
			}
		}
		if len(selected) == 0 {
			return CopyFromURLResult{}, newDriveAPIError(codeCollectionNotFound, msgCollectionNotFound, 0, nil)
		}
	}

	type sourceItem struct {
		ID            string
		Name          string
		DestinationID string
	}
	var sources []sourceItem
	if params.ItemID != "" {
		sources = []sourceItem{{ID: params.ItemID, Name: params.ItemID, DestinationID: params.DestinationID}}
	} else {
		for _, col := range selected {
			root, err := d.requestModItem(ctx, col.RootID, modAccess.Token, modAccess.Sig)
			if err != nil {
				return CopyFromURLResult{}, err
			}
			destID := params.DestinationID
			if params.createFolders() {
				folderID, err := d.getOrCreateCollectionFolder(ctx, params.DestinationID, col.Name)
				if err != nil {
					return CopyFromURLResult{}, err
				}
				destID = folderID
			}
			children, _ := root["children"].([]any)
			dirs := 0
			for _, child := range children {
				rec, ok := asRecord(child)
				if !ok {
					continue
				}
				isDir, _ := rec["isDir"].(bool)
				if !isDir {
					continue
				}
				id, _ := rec["id"].(string)
				name, _ := rec["name"].(string)
				if id == "" {
					continue
				}
				sources = append(sources, sourceItem{ID: id, Name: name, DestinationID: destID})
				dirs++
			}
			if dirs == 0 {
				sources = append(sources, sourceItem{ID: col.RootID, Name: col.Name, DestinationID: destID})
			}
		}
	}
	if len(sources) == 0 {
		return CopyFromURLResult{}, newDriveAPIError(codeCollectionEmpty, msgCollectionEmpty, 0, nil)
	}

	copied := 0
	for itemIndex, item := range sources {
		n, err := d.copyRemoteImport(ctx, remoteImport{
			Mode:          "mod",
			SourceID:      item.ID,
			DestinationID: item.DestinationID,
			ModToken:      modAccess.Token,
			ModSig:        modAccess.Sig,
			OperationID:   params.OperationID,
			SourceName:    item.Name,
			ItemIndex:     itemIndex,
			TotalItems:    len(sources),
		})
		if err != nil {
			return CopyFromURLResult{}, err
		}
		copied += n
	}
	return CopyFromURLResult{Source: "mod", Copied: copied, DestinationID: params.DestinationID}, nil
}

func (d *Drive) runCopyFromURLMany(ctx context.Context, params CopyFromURLParams, source DriveSource) (CopyFromURLResult, error) {
	password := params.Password
	if source.Type == "link" {
		access, err := d.requestSharedLinkAccess(ctx, source.ID, password)
		if err != nil {
			return CopyFromURLResult{}, err
		}
		copied, err := d.copyRemoteImportMany(ctx, remoteImportMany{
			Mode:          "link",
			SrcIDs:        params.SelectedIDs,
			DestinationID: params.DestinationID,
			LinkID:        source.ID,
			LinkToken:     access.Token,
			OperationID:   params.OperationID,
			SourceNames:   params.SelectedIDs,
		})
		if err != nil {
			return CopyFromURLResult{}, err
		}
		return CopyFromURLResult{Source: "link", Copied: copied, DestinationID: params.DestinationID}, nil
	}
	modAccess, err := d.requestModOverview(ctx, source.ID, password)
	if err != nil {
		return CopyFromURLResult{}, err
	}
	copied, err := d.copyRemoteImportMany(ctx, remoteImportMany{
		Mode:          "mod",
		SrcIDs:        params.SelectedIDs,
		DestinationID: params.DestinationID,
		ModToken:      modAccess.Token,
		ModSig:        modAccess.Sig,
		OperationID:   params.OperationID,
		SourceNames:   params.SelectedIDs,
	})
	if err != nil {
		return CopyFromURLResult{}, err
	}
	return CopyFromURLResult{Source: "mod", Copied: copied, DestinationID: params.DestinationID}, nil
}

type remoteImport struct {
	Mode          string
	SourceID      string
	DestinationID string
	LinkID        string
	LinkToken     string
	ModToken      string
	ModSig        string
	OperationID   string
	SourceName    string
	ItemIndex     int
	TotalItems    int
}

type remoteImportMany struct {
	Mode          string
	SrcIDs        []string
	DestinationID string
	LinkID        string
	LinkToken     string
	ModToken      string
	ModSig        string
	OperationID   string
	SourceNames   []string
}

func (d *Drive) copyRemoteImport(ctx context.Context, in remoteImport) (int, error) {
	if ctx.Err() != nil {
		return 0, copyCanceledError()
	}
	query := url.Values{}
	query.Set("mode", in.Mode)
	query.Set("src", in.SourceID)
	query.Set("dest", in.DestinationID)
	extra := make(http.Header)
	if in.Mode == "link" && in.LinkID != "" && in.LinkToken != "" {
		query.Set("linkId", in.LinkID)
		query.Set("linkToken", in.LinkToken)
	}
	if in.Mode == "mod" {
		if in.ModSig != "" {
			extra.Set("x-sig", in.ModSig)
		}
		if in.ModToken != "" {
			extra.Set("x-token", in.ModToken)
		}
	}
	totalItems := max(1, in.TotalItems)
	d.emitCopyProgress(in.OperationID, DriveCopyProgress{
		Source: in.Mode, Phase: "copying", Current: in.ItemIndex, Total: totalItems,
		ItemName: in.SourceName, CopiedFiles: in.ItemIndex,
	})
	if err := d.consumeImportSSE(ctx, http.MethodGet, "/akasha/common/sse/import", query, extra, nil, importStream{
		Operation:     "import " + in.Mode + " source " + in.SourceID,
		OperationID:   in.OperationID,
		Source:        in.Mode,
		SourceName:    in.SourceName,
		DestinationID: in.DestinationID,
		ItemIndex:     in.ItemIndex,
		TotalItems:    totalItems,
		VerifyResult:  true,
	}); err != nil {
		return 0, err
	}
	return 1, nil
}

func (d *Drive) copyRemoteImportMany(ctx context.Context, in remoteImportMany) (int, error) {
	if ctx.Err() != nil {
		return 0, copyCanceledError()
	}
	if len(in.SrcIDs) == 0 {
		return 0, nil
	}
	transferCreated := d.createImportTransfer(in.OperationID, in.DestinationID, in.SourceNames, ctx)
	if transferCreated {
		defer d.transfer.ClearCancel(in.OperationID)
	}
	d.emitCopyProgress(in.OperationID, DriveCopyProgress{
		Source: in.Mode, Phase: "copying", Total: len(in.SrcIDs),
	})
	extra := make(http.Header)
	extra.Set("Content-Type", "application/json")
	if in.Mode == "mod" {
		if in.ModSig != "" {
			extra.Set("x-sig", in.ModSig)
		}
		if in.ModToken != "" {
			extra.Set("x-token", in.ModToken)
		}
	}
	body := map[string]any{
		"mode": in.Mode,
		"src":  in.SrcIDs,
		"dest": in.DestinationID,
	}
	if in.Mode == "link" && in.LinkID != "" && in.LinkToken != "" {
		body["linkId"] = in.LinkID
		body["linkToken"] = in.LinkToken
	}
	if transferCreated {
		status := transfer.StatusProgress
		totalSize := int64(1)
		_ = d.transfer.Update(in.OperationID, transfer.Updates{Status: &status, TotalSize: &totalSize})
	}
	transferPID := ""
	if transferCreated {
		transferPID = in.OperationID
	}
	if err := d.consumeImportSSE(ctx, http.MethodPost, "/akasha/common/sse/import-many", nil, extra, body, importStream{
		Operation:     "import-many " + in.Mode,
		OperationID:   in.OperationID,
		Source:        in.Mode,
		DestinationID: in.DestinationID,
		TotalItems:    len(in.SrcIDs),
		TransferPID:   transferPID,
	}); err != nil {
		return 0, err
	}
	return len(in.SrcIDs), nil
}

type importStream struct {
	Operation     string
	OperationID   string
	Source        string
	SourceName    string
	DestinationID string
	ItemIndex     int
	TotalItems    int
	TransferPID   string
	VerifyResult  bool
}

func (d *Drive) createImportTransfer(operationID, destinationID string, sourceNames []string, ctx context.Context) bool {
	if d.transfer == nil || operationID == "" {
		return false
	}
	displayName := "가져오기"
	if len(sourceNames) == 1 {
		displayName += ": " + sourceNames[0]
	} else if len(sourceNames) > 1 {
		displayName += ": " + sourceNames[0] + " +" + strconv.Itoa(len(sourceNames)-1) + "개"
	}
	_, err := d.transfer.Create(transfer.CreateParams{
		PID: operationID, Type: "download", Name: displayName, CurrentID: destinationID,
		InitialStatus: transfer.StatusPreparing, Data: transfer.Data{}, ManualStart: true,
	})
	if err != nil {
		if d.log != nil {
			d.log.Warn(map[string]any{"operationId": operationID, "error": err.Error()}, "Drive:CopyFromUrl:TransferCreateSkipped")
		}
		return false
	}
	d.mu.Lock()
	op := d.ops[operationID]
	d.mu.Unlock()
	if op != nil {
		if err := d.transfer.AttachCancel(operationID, op.cancel); err != nil && d.log != nil {
			d.log.Warn(map[string]any{"operationId": operationID, "error": err.Error()}, "Drive:CopyFromUrl:TransferCancelAttachFailed")
		}
	}
	return true
}

func (d *Drive) consumeImportSSE(ctx context.Context, method, path string, query url.Values, extra http.Header, body any, stream importStream) error {
	if d == nil || d.http == nil {
		return errDriveHTTPUnconfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return copyCanceledError()
	}
	rawURL := strings.TrimRight(d.http.BackendURL(), "/") + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	header := make(http.Header)
	for k, vs := range extra {
		header[k] = append([]string(nil), vs...)
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = strings.NewReader(string(raw))
		header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Fetch(ctx, rawURL, infra.FetchOptions{
		Method:            method,
		Header:            header,
		Body:              rdr,
		DisableHTTPErrors: true,
	})
	if err != nil {
		if isCanceled(err) {
			return copyCanceledError()
		}
		return CreateDriveAPIError(err, stream.Operation, 0)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return CreateDriveAPIError(decodeAPIValue(resp.Header.Get("Content-Type"), raw), stream.Operation, resp.StatusCode)
	}
	if resp.Body == nil {
		return newDriveAPIError(codeImportInvalidResponse, "The server import stream was empty.", 0, nil)
	}
	stopWatch := watchCancel(ctx, resp.Body)
	defer stopWatch()
	preexistingChildIDs := map[string]struct{}{}
	if stream.VerifyResult {
		preexistingChildIDs = d.listDestinationChildIDs(ctx, stream.DestinationID)
	}
	completed := false
	var expectedSize *int64
	lastProcessedFiles := 0
	err = parseSSE(resp.Body, func(event, data string) error {
		if ctx.Err() != nil {
			return copyCanceledError()
		}
		parsed := parseRemoteImportData(data)
		switch event {
		case "metadata":
			if size, ok := remoteImportNumber(parsed, "totalExpectedSize"); ok {
				expectedSize = &size
				if stream.TransferPID != "" && size > 0 {
					zero := int64(0)
					_ = d.transfer.Update(stream.TransferPID, transfer.Updates{TotalSize: &size, TransferredSize: &zero})
				}
			}
		case "error":
			serverMessage := remoteImportErrorMessage(parsed)
			if stream.VerifyResult && expectedSize != nil && d.hasRemoteImportResult(ctx, stream.DestinationID, *expectedSize, preexistingChildIDs, stream.SourceName) {
				completed = true
				d.emitCopyProgress(stream.OperationID, DriveCopyProgress{
					Source: stream.Source, Phase: "copying", Current: stream.ItemIndex + 1,
					Total: stream.TotalItems, ItemName: stream.SourceName, CopiedFiles: stream.ItemIndex + 1,
					Message: "Server reported an error after the files were copied.",
				})
				return errVerifiedImport
			}
			return CreateDriveAPIError(serverMessage, stream.Operation, 0)
		case "complete":
			completed = true
			current := stream.TotalItems
			if stream.VerifyResult {
				current = stream.ItemIndex + 1
			}
			d.emitCopyProgress(stream.OperationID, DriveCopyProgress{
				Source: stream.Source, Phase: "copying", Current: current, Total: stream.TotalItems,
				ItemName: stream.SourceName, CopiedFiles: current,
			})
			if stream.TransferPID != "" {
				totalSize := int64(0)
				if size, ok := remoteImportNumber(parsed, "totalSize"); ok {
					totalSize = size
				} else if expectedSize != nil {
					totalSize = *expectedSize
				}
				if totalSize <= 0 {
					totalSize = 1
				}
				status := transfer.StatusCompleted
				progress := float64(100)
				_ = d.transfer.Update(stream.TransferPID, transfer.Updates{
					Status: &status, TotalSize: &totalSize, TransferredSize: &totalSize,
					TransferredFiles: &stream.TotalItems, Progress: &progress,
				})
			}
			return nil
		}
		processedFiles, ok := remoteImportInt(parsed, "processedFiles")
		if !ok {
			processedFiles = lastProcessedFiles
		}
		lastProcessedFiles = processedFiles
		current := min(processedFiles, stream.TotalItems)
		if stream.VerifyResult {
			current = stream.ItemIndex
		}
		d.emitCopyProgress(stream.OperationID, DriveCopyProgress{
			Source: stream.Source, Phase: "copying", Current: current, Total: stream.TotalItems,
			ItemName: stream.SourceName, CopiedFiles: max(processedFiles, stream.ItemIndex),
			Message: remoteImportStatus(event, parsed),
		})
		if stream.TransferPID != "" {
			status := transfer.StatusProgress
			updates := transfer.Updates{Status: &status, TransferredFiles: &processedFiles}
			if size, sizeOK := remoteImportNumber(parsed, "currentTotalSize"); sizeOK && size > 0 && expectedSize != nil && *expectedSize > 0 {
				size = min(size, *expectedSize)
				updates.TransferredSize = &size
			}
			_ = d.transfer.Update(stream.TransferPID, updates)
		}
		return nil
	})
	if ctx.Err() != nil || isCanceled(err) {
		return copyCanceledError()
	}
	if errors.Is(err, errVerifiedImport) {
		err = nil
	}
	if err != nil {
		return err
	}
	if !completed {
		return newDriveAPIError(codeImportInvalidResponse, importIncompleteMessage(stream.Operation), 0, nil)
	}
	return nil
}

func watchCancel(ctx context.Context, body io.Closer) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = body.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func importIncompleteMessage(operation string) string {
	if strings.HasPrefix(operation, "import-many") {
		return "The server import did not complete."
	}
	if rest, ok := strings.CutPrefix(operation, "import "); ok {
		// "import link source SOURCE"
		if _, source, found := strings.Cut(rest, " source "); found {
			return "The server import for " + source + " ended before completion."
		}
	}
	return "The server import did not complete."
}

func parseRemoteImportData(value string) any {
	trim := strings.TrimSpace(value)
	if trim == "" {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(trim), &v) == nil {
		return v
	}
	return trim
}

func remoteImportErrorMessage(value any) any {
	if value == nil {
		return "The server import failed."
	}
	if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	record, ok := asRecord(value)
	if !ok {
		return "The server import failed."
	}
	for _, key := range []string{"message", "error", "code"} {
		if s, ok := record[key].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return "The server import failed."
}

var errVerifiedImport = errors.New("remote import verified after server error")

func remoteImportNumber(value any, key string) (int64, bool) {
	record, ok := asRecord(value)
	if !ok {
		return 0, false
	}
	switch number := record[key].(type) {
	case float64:
		return int64(number), true
	case float32:
		return int64(number), true
	case int:
		return int64(number), true
	case int64:
		return number, true
	case json.Number:
		value, err := number.Int64()
		return value, err == nil
	default:
		return 0, false
	}
}

func remoteImportInt(value any, key string) (int, bool) {
	number, ok := remoteImportNumber(value, key)
	return int(number), ok
}

func remoteImportStatus(event string, value any) string {
	if event != "status" {
		return ""
	}
	if status, ok := value.(string); ok {
		return status
	}
	record, ok := asRecord(value)
	if !ok {
		return ""
	}
	status, _ := record["status"].(string)
	return status
}

func (d *Drive) listDestinationChildIDs(ctx context.Context, destinationID string) map[string]struct{} {
	ids := make(map[string]struct{})
	item, err := d.GetItem(ctx, destinationID)
	if err != nil {
		if d.log != nil {
			d.log.Warn(map[string]any{"destinationId": destinationID, "error": err.Error()}, "Drive:CopyFromUrl:ListDestinationChildrenFailed")
		}
		return ids
	}
	for _, child := range driveItemChildren(item) {
		if id, _ := child["id"].(string); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func (d *Drive) hasRemoteImportResult(ctx context.Context, destinationID string, expectedSize int64, preexistingChildIDs map[string]struct{}, sourceName string) bool {
	if ctx.Err() != nil {
		return false
	}
	destination, err := d.GetItem(ctx, destinationID)
	if err != nil {
		d.logImportVerificationFailure(destinationID, "", expectedSize, err)
		return false
	}
	newDirectories := make([]map[string]any, 0)
	for _, child := range driveItemChildren(destination) {
		id, _ := child["id"].(string)
		name, _ := child["name"].(string)
		isDir, _ := child["isDir"].(bool)
		if !isDir || id == "" || !isCollectionFolderName(name, sourceName) {
			continue
		}
		if _, existed := preexistingChildIDs[id]; !existed {
			newDirectories = append(newDirectories, child)
		}
	}
	for _, child := range newDirectories {
		if size, ok := remoteImportNumber(child, "size"); ok && size == expectedSize {
			return true
		}
	}
	pending := make([]string, 0, len(newDirectories))
	for _, child := range newDirectories {
		size, _ := remoteImportNumber(child, "size")
		if size > expectedSize {
			id, _ := child["id"].(string)
			pending = append(pending, id)
		}
	}
	visited := map[string]struct{}{destinationID: {}}
	for len(pending) > 0 && len(visited) < 128 {
		if ctx.Err() != nil {
			return false
		}
		last := len(pending) - 1
		itemID := pending[last]
		pending = pending[:last]
		if _, ok := visited[itemID]; ok || itemID == "" {
			continue
		}
		visited[itemID] = struct{}{}
		item, getErr := d.GetItem(ctx, itemID)
		if getErr != nil {
			d.logImportVerificationFailure(destinationID, itemID, expectedSize, getErr)
			return false
		}
		for _, child := range driveItemChildren(item) {
			isDir, _ := child["isDir"].(bool)
			if !isDir {
				continue
			}
			size, _ := remoteImportNumber(child, "size")
			if size == expectedSize {
				return true
			}
			if size > expectedSize {
				id, _ := child["id"].(string)
				pending = append(pending, id)
			}
		}
	}
	return false
}

func driveItemChildren(value any) []map[string]any {
	record, ok := asRecord(value)
	if !ok {
		return nil
	}
	raw, _ := record["children"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, child := range raw {
		if item, ok := asRecord(child); ok {
			out = append(out, item)
		}
	}
	return out
}

func (d *Drive) logImportVerificationFailure(destinationID, itemID string, expectedSize int64, err error) {
	if d.log == nil {
		return
	}
	d.log.Warn(map[string]any{
		"destinationId": destinationID, "itemId": itemID, "expectedSize": expectedSize,
		"error": err.Error(), "stage": "verify-server-copy",
	}, "Drive:CopyFromUrl:ServerImportVerificationFailed")
}

func parseSSE(r io.Reader, fn func(event, data string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event, data string
	hasData := false
	flush := func() error {
		if !hasData && event == "" {
			return nil
		}
		name := event
		if name == "" {
			name = "message"
		}
		err := fn(name, data)
		event = ""
		data = ""
		hasData = false
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if hasData {
				data += "\n" + value
			} else {
				data = value
				hasData = true
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return scanner.Err()
}

func (d *Drive) getOrCreateCollectionFolder(ctx context.Context, parentID, name string) (string, error) {
	if ctx.Err() != nil {
		return "", copyCanceledError()
	}
	sanitized := name
	if d.fs != nil {
		sanitized = d.fs.SanitizeWindowsFilename(name, " ")
	}
	current, err := d.GetItem(ctx, parentID)
	if err != nil {
		return "", err
	}
	if existing := findCollectionFolder(current, sanitized); existing != "" {
		return existing, nil
	}
	before := dirIDs(current)
	if _, err := d.CreateDir(ctx, parentID, sanitized); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", copyCanceledError()
	}
	updated, err := d.GetItem(ctx, parentID)
	if err != nil {
		return "", err
	}
	if created := findNewCollectionFolder(updated, before, sanitized); created != "" {
		return created, nil
	}
	if existing := findCollectionFolder(updated, sanitized); existing != "" {
		return existing, nil
	}
	return "", newDriveAPIError("DRIVE_COLLECTION_FOLDER_CREATE_FAILED", `The collection folder "`+sanitized+`" could not be created.`, 0, nil)
}

func findCollectionFolder(item any, name string) string {
	record, ok := asRecord(item)
	if !ok {
		return ""
	}
	children, _ := record["children"].([]any)
	for _, child := range children {
		rec, ok := asRecord(child)
		if !ok {
			continue
		}
		isDir, _ := rec["isDir"].(bool)
		childName, _ := rec["name"].(string)
		id, _ := rec["id"].(string)
		if isDir && childName == name && id != "" {
			return id
		}
	}
	return ""
}

func dirIDs(item any) map[string]struct{} {
	out := map[string]struct{}{}
	record, ok := asRecord(item)
	if !ok {
		return out
	}
	children, _ := record["children"].([]any)
	for _, child := range children {
		rec, ok := asRecord(child)
		if !ok {
			continue
		}
		isDir, _ := rec["isDir"].(bool)
		id, _ := rec["id"].(string)
		if isDir && id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func findNewCollectionFolder(item any, before map[string]struct{}, name string) string {
	record, ok := asRecord(item)
	if !ok {
		return ""
	}
	children, _ := record["children"].([]any)
	for _, child := range children {
		rec, ok := asRecord(child)
		if !ok {
			continue
		}
		isDir, _ := rec["isDir"].(bool)
		id, _ := rec["id"].(string)
		childName, _ := rec["name"].(string)
		if !isDir || id == "" {
			continue
		}
		if _, existed := before[id]; existed {
			continue
		}
		if isCollectionFolderName(childName, name) {
			return id
		}
	}
	return ""
}

func isCollectionFolderName(actual, expected string) bool {
	if actual == expected {
		return true
	}
	prefix := expected + " ("
	if !strings.HasPrefix(actual, prefix) || !strings.HasSuffix(actual, ")") {
		return false
	}
	inner := actual[len(prefix) : len(actual)-1]
	if inner == "" {
		return false
	}
	for _, r := range inner {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func newOperationID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "op"
	}
	return hex.EncodeToString(b[:])
}
