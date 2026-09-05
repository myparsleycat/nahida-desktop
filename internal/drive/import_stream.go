package drive

import (
	"bufio"
	"context"
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
			_ = infra.ReportError(d.log, err, "Drive:CopyFromUrl:TransferCreateSkipped", infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "copy-from-url", Fields: map[string]any{"operationId": operationID}})
		}
		return false
	}
	d.mu.Lock()
	op := d.ops[operationID]
	d.mu.Unlock()
	if op != nil {
		if err := d.transfer.AttachCancel(operationID, op.cancel); err != nil && d.log != nil {
			_ = infra.ReportError(d.log, err, "Drive:CopyFromUrl:TransferCancelAttachFailed", infra.Diagnostic{Severity: infra.DiagnosticWarn, Operation: "copy-from-url", Fields: map[string]any{"operationId": operationID}})
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
		raw, readErr := io.ReadAll(resp.Body)
		return infra.WithCause(CreateDriveAPIError(decodeAPIValue(resp.Header.Get("Content-Type"), raw), stream.Operation, resp.StatusCode), infra.AnnotateError(readErr, infra.HTTPDiagnostic(http.MethodGet, "", "read-error-response", resp)))
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

var errVerifiedImport = errors.New("remote import verified after server error")

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
