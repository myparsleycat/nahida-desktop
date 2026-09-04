package drive

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

type CreatedUploadDirectory struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type StartUploadParams struct {
	DestID               string                 `json:"destId"`
	Paths                []string               `json:"paths"`
	ConflictStrategy     UploadConflictStrategy `json:"conflictStrategy,omitempty"`
	AdditionalExtensions []string               `json:"additionalExtensions,omitempty"`
	AllowAllFiles        bool                   `json:"allowAllFiles,omitempty"`
}

type StartUploadResult struct {
	PID    string `json:"pid"`
	Status string `json:"status"`
}

type uploadCompletedEvent struct {
	PID       string `json:"pid"`
	CurrentID string `json:"currentId"`
}

type uploadRestartData struct {
	Params      StartUploadParams
	Preparation UploadPreparation
	RequestID   string
}

func (d *Drive) CreateDirs(ctx context.Context, parentID string, directories []UploadDirectory) ([]CreatedUploadDirectory, error) {
	if d == nil || d.http == nil {
		return nil, errDriveHTTPUnconfigured
	}
	if parentID == "" {
		return nil, errors.New("upload destination is required")
	}
	body := map[string]any{"parentId": parentID, "dirs": directories}
	var last error
	for attempt := 0; attempt <= d.dirRetries; attempt++ {
		data, edenErr, err := d.doJSON(ctx, http.MethodPost, "/akasha/create-dirs", nil, body)
		switch {
		case err != nil:
			last = err
		case edenErr != nil:
			last = CreateDriveAPIError(edenErr.asAny(), "post:dirs", edenErr.Status)
		default:
			created, decodeErr := decodeCreatedUploadDirectories(data)
			if decodeErr == nil {
				return created, nil
			}
			last = decodeErr
		}
		if attempt == d.dirRetries {
			break
		}
		if err := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); err != nil {
			return nil, errors.Join(last, err)
		}
	}
	return nil, CreateDriveAPIError(last, "post:dirs", 0)
}

func decodeCreatedUploadDirectories(value any) ([]CreatedUploadDirectory, error) {
	rows, ok := value.([]any)
	if !ok {
		if record, recordOK := asRecord(value); recordOK {
			for _, key := range []string{"dirs", "directories", "data"} {
				if nested, nestedOK := record[key].([]any); nestedOK {
					rows = nested
					ok = true
					break
				}
			}
		}
	}
	if !ok {
		return nil, errors.New("create directories returned an invalid response")
	}
	out := make([]CreatedUploadDirectory, 0, len(rows))
	for _, row := range rows {
		record, recordOK := asRecord(row)
		if !recordOK {
			return nil, errors.New("create directories returned an invalid entry")
		}
		id, idOK := record["id"].(string)
		path, pathOK := record["path"].(string)
		if !idOK || id == "" || !pathOK {
			return nil, errors.New("create directories entry is missing id or path")
		}
		out = append(out, CreatedUploadDirectory{ID: id, Path: filepath.ToSlash(path)})
	}
	return out, nil
}

func (d *Drive) StartUpload(ctx context.Context, params StartUploadParams) (result StartUploadResult, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:startUpload")
	if d == nil || d.transfer == nil {
		return StartUploadResult{}, errors.New("upload transfer service is not configured")
	}
	if params.DestID == "" {
		return StartUploadResult{}, errors.New("upload destination is required")
	}
	if len(params.Paths) == 0 {
		return StartUploadResult{}, errors.New("upload paths are required")
	}
	if err := ensureUploadSourceReadable(params.Paths[0]); err != nil {
		return StartUploadResult{}, err
	}
	item, err := d.GetItem(ctx, params.DestID)
	if err != nil {
		return StartUploadResult{}, err
	}
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return StartUploadResult{}, err
	}
	preparation, err := PrepareUpload(params.Paths, slices.Collect(stringsMapKeys(childNames(item))), params.ConflictStrategy, rules, params.AdditionalExtensions, params.AllowAllFiles)
	if err != nil {
		return StartUploadResult{}, err
	}
	if len(preparation.Files) == 0 {
		return StartUploadResult{}, errors.New("NO_UPLOADABLE_FILES")
	}
	requestID := uuid.NewString()
	restart := &uploadRestartData{Params: params, Preparation: preparation, RequestID: requestID}
	data := transfer.Data{
		Files: make([]transfer.DownloadFile, len(preparation.Files)),
		Dirs:  make([]transfer.Directory, len(preparation.Directories)),
	}
	for index, file := range preparation.Files {
		parentID := file.ParentPath
		data.Files[index] = transfer.DownloadFile{ID: file.FID, ParentID: &parentID, Name: file.Name, Size: file.Size}
	}
	for index, directory := range preparation.Directories {
		parentID := directory.ParentPath
		data.Dirs[index] = transfer.Directory{ParentID: &parentID, Name: directory.Name}
	}
	_, err = d.transfer.Create(transfer.CreateParams{
		PID:           preparation.PID,
		Type:          "upload",
		Name:          preparation.ProcessName,
		Path:          filepath.ToSlash(params.Paths[0]),
		CurrentID:     params.DestID,
		InitialStatus: transfer.StatusPreparing,
		Data:          data,
		RestartData:   restart,
	})
	if err != nil {
		return StartUploadResult{}, err
	}
	var state uploadRunnerState
	pending := transfer.StatusPending
	if err := d.transfer.Update(preparation.PID, transfer.Updates{Status: &pending}); err != nil {
		_ = d.transfer.Cancel(preparation.PID)
		return StartUploadResult{}, err
	}
	if err := d.transfer.RegisterRunner(preparation.PID, func(runCtx context.Context, transfers *transfer.Transfer, pid string) error {
		return d.runUpload(runCtx, transfers, pid, restart, &state)
	}); err != nil {
		_ = d.transfer.Cancel(preparation.PID)
		return StartUploadResult{}, err
	}
	return StartUploadResult{PID: preparation.PID, Status: "started"}, nil
}

type uploadRunnerState struct {
	mu     sync.Mutex
	hashes map[string]string
}

func (d *Drive) runUpload(ctx context.Context, transfers *transfer.Transfer, pid string, restart *uploadRestartData, state *uploadRunnerState) (returnErr error) {
	preparation := restart.Preparation
	status := transfer.StatusPreparing
	zero := 0
	if err := transfers.Update(pid, transfer.Updates{Status: &status, TransferredFiles: &zero, ClearError: true, ClearErrorCode: true}); err != nil {
		return d.reportUploadFailure(transfers, pid, "prepare", err)
	}
	created := []CreatedUploadDirectory{}
	var err error
	if len(preparation.Directories) > 0 {
		created, err = d.CreateDirs(ctx, restart.Params.DestID, preparation.Directories)
		if err != nil {
			return d.failUploadTransfer(transfers, pid, "create-dirs", err)
		}
	}
	parentIDs := make(map[string]string, len(created))
	for _, directory := range created {
		parentIDs[directory.Path] = directory.ID
	}
	parentFiles := make([]UploadFile, len(preparation.Files))
	copy(parentFiles, preparation.Files)

	state.mu.Lock()
	hashes := state.hashes
	state.mu.Unlock()
	if len(hashes) != len(parentFiles) {
		hashed, hashErr := HashUploadFiles(ctx, parentFiles, uploadHashConcurrency(), func(count int) {
			_ = transfers.Update(pid, transfer.Updates{TransferredFiles: &count})
		})
		if hashErr != nil {
			return d.failUploadTransfer(transfers, pid, "hash", hashErr)
		}
		hashes = make(map[string]string, len(hashed))
		for _, file := range hashed {
			hashes[file.FID] = file.SHA256
		}
		state.mu.Lock()
		state.hashes = hashes
		state.mu.Unlock()
	}
	finalFiles := make([]FinalUploadFile, len(parentFiles))
	for index, file := range parentFiles {
		parentID := restart.Params.DestID
		if file.ParentPath != "" {
			var ok bool
			parentID, ok = parentIDs[file.ParentPath]
			if !ok {
				return d.failUploadTransfer(transfers, pid, "resolve-parent", fmt.Errorf("created directory missing for %q", file.ParentPath))
			}
		}
		sha256 := hashes[file.FID]
		if sha256 == "" {
			return d.failUploadTransfer(transfers, pid, "hash", fmt.Errorf("hash missing for file %s", file.Name))
		}
		finalFiles[index] = FinalUploadFile{UploadFile: file, ParentID: parentID, SHA256: sha256}
	}

	var uploadedBytes int64
	uploadedFiles := 0
	incomplete := make([]FinalUploadFile, 0, len(finalFiles))
	for _, file := range finalFiles {
		if transfers.IsFileCompleted(pid, file.FID) {
			uploadedBytes += file.Size
			uploadedFiles++
			continue
		}
		incomplete = append(incomplete, file)
	}
	progressStatus := transfer.StatusProgress
	if err := transfers.Update(pid, transfer.Updates{Status: &progressStatus, TransferredSize: &uploadedBytes, TransferredFiles: &uploadedFiles}); err != nil {
		return d.reportUploadFailure(transfers, pid, "prepare", err)
	}
	if len(incomplete) > 0 {
		incomplete = redistributeUploadFiles(incomplete)
		stage := "plan"
		plan, planErr := d.planUploadV2(ctx, restart.Params.DestID, restart.RequestID, incomplete, func(progress UploadPlanProgress) {
			if progress.Phase != "" {
				stage = "plan/" + string(progress.Phase)
			}
			percentage := 0.0
			if progress.Total > 0 {
				percentage = float64(progress.Processed) / float64(progress.Total) * 100
			}
			_ = transfers.Update(pid, transfer.Updates{PlanPhase: &progress.Phase, PlanProgress: &percentage})
		})
		if planErr != nil {
			return d.failUploadTransfer(transfers, pid, stage, planErr)
		}
		_ = transfers.Update(pid, transfer.Updates{ClearPlanPhase: true, ClearPlanProgress: true})
		executeErr := d.executeUploadPlanV2(ctx, incomplete, plan, d.uploadConcurrency(ctx), func(progress UploadExecutionProgress) {
			state.mu.Lock()
			defer state.mu.Unlock()
			uploadedBytes += progress.Bytes
			if progress.FileID != "" && !transfers.IsFileCompleted(pid, progress.FileID) {
				if transfers.MarkFileCompleted(pid, progress.FileID) == nil {
					uploadedFiles++
				}
			}
			_ = transfers.Update(pid, transfer.Updates{TransferredSize: &uploadedBytes, TransferredFiles: &uploadedFiles})
		})
		if executeErr != nil {
			return d.failUploadTransfer(transfers, pid, "execute", executeErr)
		}
	}
	completed := transfer.StatusCompleted
	total := preparation.TotalSize
	hundred := 100.0
	if err := transfers.Update(pid, transfer.Updates{
		Status:            &completed,
		TransferredSize:   &total,
		TransferredFiles:  ptrInt(len(finalFiles)),
		Progress:          &hundred,
		ClearPlanPhase:    true,
		ClearPlanProgress: true,
	}); err != nil {
		return d.reportUploadFailure(transfers, pid, "finalize", err)
	}
	if d.eventEmit != nil {
		d.eventEmit("drive:upload-completed", uploadCompletedEvent{
			PID:       pid,
			CurrentID: restart.Params.DestID,
		})
	}
	return nil
}

func (d *Drive) uploadConcurrency(ctx context.Context) int {
	if d.settings == nil {
		return 8
	}
	value, err := d.settings.GetUploadConcurrency(ctx)
	if err != nil || value < 1 {
		return 8
	}
	return value
}

func uploadHashConcurrency() int {
	// Electron's Piscina pool sets minThreads=2 and leaves maxThreads at
	// availableParallelism*1.5. Since the fractional bound admits the next
	// integer worker, ceil reproduces its effective maximum.
	return max(2, int(math.Ceil(float64(runtime.GOMAXPROCS(0))*1.5)))
}

func (d *Drive) failUploadTransfer(transfers *transfer.Transfer, pid, stage string, failure error) error {
	if errors.Is(failure, context.Canceled) {
		return failure
	}
	status := transfer.StatusError
	message := failure.Error()
	code := ""
	var uploadErr *UploadV2Error
	if errors.As(failure, &uploadErr) {
		code = uploadErr.Code
	}
	updateErr := transfers.Update(pid, transfer.Updates{
		Status:            &status,
		Error:             &message,
		ErrorCode:         &code,
		ClearPlanPhase:    true,
		ClearPlanProgress: true,
	})
	reported := d.reportUploadFailure(transfers, pid, stage, failure)
	if updateErr == nil {
		return reported
	}
	updateReported := infra.ReportError(d.log, updateErr, "Drive", infra.Diagnostic{
		Severity:  infra.DiagnosticError,
		Operation: "upload",
		Stage:     "record-failure",
		Fields:    driveTransferFields(transfers, pid, code),
	})
	return errors.Join(reported, updateReported)
}

func (d *Drive) reportUploadFailure(transfers *transfer.Transfer, pid, stage string, failure error) error {
	code := ""
	var uploadErr *UploadV2Error
	if errors.As(failure, &uploadErr) {
		code = uploadErr.Code
	}
	return infra.ReportError(d.log, failure, "Drive", infra.Diagnostic{
		Operation: "upload",
		Stage:     stage,
		Fields:    driveTransferFields(transfers, pid, code),
	})
}

func driveTransferFields(transfers *transfer.Transfer, pid, code string) map[string]any {
	fields := map[string]any{"pid": pid}
	if code != "" {
		fields["errorCode"] = code
	}
	if transfers == nil {
		return fields
	}
	record, ok := transfers.Get(pid)
	if !ok {
		return fields
	}
	fields["destinationId"] = record.CurrentID
	fields["name"] = record.Name
	fields["path"] = record.Path
	fields["totalBytes"] = record.TotalSize
	fields["transferredBytes"] = record.TransferredSize
	fields["totalFiles"] = record.TotalFiles
	fields["transferredFiles"] = record.TransferredFiles
	return fields
}

func stringsMapKeys(values map[string]struct{}) func(func(string) bool) {
	return func(yield func(string) bool) {
		for value := range values {
			if !yield(value) {
				return
			}
		}
	}
}

func ptrInt(value int) *int {
	return &value
}
