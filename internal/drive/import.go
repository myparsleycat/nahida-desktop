package drive

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	stage := "parse-source"
	defer func() {
		if err != nil {
			err = d.reportCopyFailure(err, params, "copy-from-url", stage)
		}
	}()
	defer normalizeDriveBoundaryError(&err, "fn:copyFromUrl")
	source, err := ParseDriveSourceUrl(params.URL)
	if err != nil {
		return CopyFromURLResult{}, err
	}
	opCtx, operationID, _, done := d.beginCopy(ctx, params.OperationID, source.Type)
	defer done()
	params.OperationID = operationID
	stage = "copy"
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
	if len(params.SelectedIDs) == 0 {
		return d.CopyFromURL(ctx, params)
	}
	stage := "parse-source"
	defer func() {
		if err != nil {
			err = d.reportCopyFailure(err, params, "copy-from-url-many", stage)
		}
	}()
	defer normalizeDriveBoundaryError(&err, "fn:copyFromUrlMany")
	source, err := ParseDriveSourceUrl(params.URL)
	if err != nil {
		return CopyFromURLResult{}, err
	}
	opCtx, operationID, _, done := d.beginCopy(ctx, params.OperationID, source.Type)
	defer done()
	params.OperationID = operationID
	stage = "copy"
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
	if err := d.transfer.Update(operationID, updates); err != nil {
		_ = infra.ReportError(d.log, err, "Drive", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: "copy-from-url", Stage: "record-failure",
			Fields: map[string]any{"operationId": operationID, "status": status},
		})
	}
}

func (d *Drive) reportCopyFailure(err error, params CopyFromURLParams, operation, stage string) error {
	fields := map[string]any{
		"operationId":    params.OperationID,
		"destinationId":  params.DestinationID,
		"itemId":         params.ItemID,
		"collectionId":   params.CollectionID,
		"selectedCount":  len(params.SelectedIDs),
		"sourceEndpoint": infra.SanitizeLogURL(params.URL),
	}
	if cause := copyFailureCause(err); cause != "" {
		fields["cause"] = cause
	}
	return infra.ReportError(d.log, err, "Drive", infra.Diagnostic{
		Operation: operation,
		Stage:     stage,
		Fields:    fields,
	})
}

func copyFailureCause(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(err) {
		err = cause
	}
	if err.Error() == message {
		return ""
	}
	return err.Error()
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

func newOperationID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "op"
	}
	return hex.EncodeToString(b[:])
}
