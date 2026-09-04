package drive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

type StartDownloadParams struct {
	Items         []DownloadItem    `json:"items"`
	TargetPath    string            `json:"targetPath,omitempty"`
	Link          *DownloadLink     `json:"link,omitempty"`
	Data          *DownloadMetadata `json:"data,omitempty"`
	SuggestedName string            `json:"suggestedName,omitempty"`
	Source        string            `json:"source,omitempty"`
}

type StartDownloadResult struct {
	PID    string `json:"pid"`
	Status string `json:"status"`
}

func (d *Drive) StartDownload(ctx context.Context, params StartDownloadParams) (result StartDownloadResult, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:startDownload")
	if d == nil || d.transfer == nil || d.download == nil {
		return StartDownloadResult{}, errors.New("download services are not configured")
	}
	if len(params.Items) == 0 {
		return StartDownloadResult{Status: "canceled"}, nil
	}
	fs := d.fs
	if fs == nil {
		fs = platform.NewFS()
	}
	params.Items = slices.Clone(params.Items)
	for index := range params.Items {
		params.Items[index].Name = fs.SanitizeWindowsFilename(params.Items[index].Name, " ")
	}
	selected, err := d.resolveDownloadTarget(ctx, params)
	if err != nil {
		return StartDownloadResult{}, err
	}
	if selected.canceled {
		return StartDownloadResult{Status: "canceled"}, nil
	}
	params.TargetPath = fs.SanitizePath(selected.path)
	params.SuggestedName = selected.suggestedName
	targetPath, err := filepath.Abs(params.TargetPath)
	if err != nil {
		return StartDownloadResult{}, err
	}
	info, err := os.Stat(targetPath)
	if err != nil || !info.IsDir() {
		return StartDownloadResult{}, fmt.Errorf("download target is not a directory: %s", targetPath)
	}
	if !fs.IsPathWritable(targetPath) {
		return StartDownloadResult{}, fmt.Errorf("path is not writable: %s", targetPath)
	}
	params.TargetPath = filepath.Clean(targetPath)
	var metadata DownloadMetadata
	if params.Data == nil {
		metadata, err = d.fetchDownloadMetadata(ctx, params.Items, params.Link)
		if err != nil {
			return StartDownloadResult{}, err
		}
	} else {
		metadata = cloneDownloadMetadata(*params.Data)
	}
	prepared, err := d.prepareDownloadMetadata(ctx, nil, "", metadata, params)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if ctx.Err() != nil {
				return StartDownloadResult{}, ctx.Err()
			}
			return StartDownloadResult{Status: "canceled"}, nil
		}
		return StartDownloadResult{}, err
	}
	destinationTargets, err := resolveDownloadDestinationTargets(prepared, params.TargetPath)
	if err != nil {
		return StartDownloadResult{}, err
	}
	data := transfer.Data{Root: &prepared.Root, Files: slices.Clone(prepared.Files), Dirs: slices.Clone(prepared.Dirs)}
	name := prepared.Root.Name
	if len(params.Items) != 1 {
		name = fmt.Sprintf("%d items", len(params.Items))
	}
	pid := uuid.NewString()
	if _, err := d.transfer.Create(transfer.CreateParams{
		PID:                pid,
		Type:               "download",
		Name:               name,
		Path:               filepath.ToSlash(params.TargetPath),
		DestinationTargets: destinationTargets,
		CurrentID:          downloadCurrentID(params),
		InitialStatus:      transfer.StatusPreparing,
		Data:               data,
		RestartData:        params,
	}); err != nil {
		return StartDownloadResult{}, err
	}
	if err := d.transfer.SetData(pid, data, prepared.TotalBytes, name, destinationTargets); err != nil {
		_ = d.transfer.Cancel(pid)
		return StartDownloadResult{}, err
	}
	pending := transfer.StatusPending
	if err := d.transfer.Update(pid, transfer.Updates{Status: &pending}); err != nil {
		_ = d.transfer.Cancel(pid)
		return StartDownloadResult{}, err
	}
	if err := d.transfer.RegisterRunner(pid, func(runCtx context.Context, transfers *transfer.Transfer, runnerPID string) error {
		return d.runDownload(runCtx, transfers, runnerPID, params, prepared)
	}); err != nil {
		_ = d.transfer.Cancel(pid)
		return StartDownloadResult{}, err
	}
	return StartDownloadResult{PID: pid, Status: "started"}, nil
}

func downloadCurrentID(params StartDownloadParams) string {
	if params.Data != nil {
		return params.Data.Root.ID
	}
	if len(params.Items) == 1 {
		return params.Items[0].ID
	}
	return ""
}

type resolvedDownloadTarget struct {
	path          string
	suggestedName string
	canceled      bool
}

func (d *Drive) resolveDownloadTarget(ctx context.Context, params StartDownloadParams) (resolvedDownloadTarget, error) {
	if strings.TrimSpace(params.TargetPath) != "" {
		return resolvedDownloadTarget{path: params.TargetPath, suggestedName: params.SuggestedName}, nil
	}
	if d.paths == nil {
		return resolvedDownloadTarget{}, errors.New("download target path is required")
	}
	isSingle := len(params.Items) == 1
	suggestedName := params.SuggestedName
	if isSingle && suggestedName == "" {
		suggestedName = params.Items[0].Name
	}
	source := params.Source
	if source == "" {
		source = "nahidaLive"
	}
	names := make([]string, len(params.Items))
	for index, item := range params.Items {
		names[index] = item.Name
	}
	path, fileName, err := d.paths.SelectDownloadPath(ctx, suggestedName, source, names, isSingle && !params.Items[0].IsDir)
	if err != nil {
		return resolvedDownloadTarget{}, err
	}
	if path == nil || *path == "" {
		if d.log != nil {
			d.log.Info("Download cancelled by user selection", "Drive:Download")
		}
		return resolvedDownloadTarget{canceled: true}, nil
	}
	if isSingle && fileName != nil && *fileName != "" {
		suggestedName = *fileName
	}
	return resolvedDownloadTarget{path: *path, suggestedName: suggestedName}, nil
}

func (d *Drive) runDownload(
	ctx context.Context,
	transfers *transfer.Transfer,
	pid string,
	params StartDownloadParams,
	prepared DownloadMetadata,
) error {
	preparing := transfer.StatusPreparing
	if err := transfers.Update(pid, transfer.Updates{Status: &preparing, ClearError: true, ClearErrorCode: true}); err != nil {
		return d.reportDownloadFailure(transfers, pid, "prepare", err)
	}
	name := prepared.Root.Name
	if len(params.Items) != 1 {
		name = fmt.Sprintf("%d items", len(params.Items))
	}
	destinationTargets, err := resolveDownloadDestinationTargets(prepared, params.TargetPath)
	if err != nil {
		return d.failDownloadTransfer(transfers, pid, "resolve-target", err)
	}
	data := transfer.Data{Root: &prepared.Root, Files: slices.Clone(prepared.Files), Dirs: slices.Clone(prepared.Dirs)}
	if err := transfers.SetData(pid, data, prepared.TotalBytes, name, destinationTargets); err != nil {
		return d.failDownloadTransfer(transfers, pid, "metadata", err)
	}
	if err := d.executeDownload(ctx, transfers, pid, params, prepared); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return d.failDownloadTransfer(transfers, pid, "", err)
	}
	return nil
}

func (d *Drive) prepareDownloadMetadata(ctx context.Context, transfers *transfer.Transfer, pid string, metadata DownloadMetadata, params StartDownloadParams) (DownloadMetadata, error) {
	metadata = cloneDownloadMetadata(metadata)
	fs := d.fs
	if fs == nil {
		fs = platform.NewFS()
	}
	if metadata.Root.Name != "" {
		metadata.Root.Name = fs.SanitizeWindowsFilename(metadata.Root.Name, " ")
	}
	for index := range metadata.Files {
		metadata.Files[index].Name = fs.SanitizeWindowsFilename(metadata.Files[index].Name, " ")
	}
	for index := range metadata.Dirs {
		metadata.Dirs[index].Name = fs.SanitizeWindowsFilename(metadata.Dirs[index].Name, " ")
	}
	if len(params.Items) == 1 && params.SuggestedName != "" {
		name := fs.SanitizeWindowsFilename(params.SuggestedName, " ")
		metadata.Root.Name = name
		if !params.Items[0].IsDir && len(metadata.Files) == 1 {
			metadata.Files[0].Name = name
		}
	}
	entries, err := os.ReadDir(params.TargetPath)
	if err != nil {
		return DownloadMetadata{}, err
	}
	existing := make([]string, len(entries))
	for index, entry := range entries {
		existing[index] = entry.Name()
	}
	if len(params.Items) == 1 && params.Items[0].IsDir {
		name, canceled, resolveErr := d.resolveDirectoryDownloadName(ctx, metadata.Root.Name, params.TargetPath, existing)
		if resolveErr != nil {
			return DownloadMetadata{}, resolveErr
		}
		if canceled {
			if transfers != nil && pid != "" {
				_ = transfers.Cancel(pid)
			}
			return DownloadMetadata{}, context.Canceled
		}
		setDownloadRootName(&metadata, name)
	} else if len(params.Items) > 1 {
		used := append([]string(nil), existing...)
		for index := range metadata.Dirs {
			if metadata.Dirs[index].ParentID == nil || *metadata.Dirs[index].ParentID != "batch-root" {
				continue
			}
			metadata.Dirs[index].Name = fs.GetUniqueName(metadata.Dirs[index].Name, used)
			used = append(used, metadata.Dirs[index].Name)
		}
		for index := range metadata.Files {
			if metadata.Files[index].ParentID == nil || *metadata.Files[index].ParentID != "batch-root" {
				continue
			}
			metadata.Files[index].Name = fs.GetUniqueName(metadata.Files[index].Name, used)
			used = append(used, metadata.Files[index].Name)
		}
	}
	return metadata, nil
}

func (d *Drive) resolveDirectoryDownloadName(ctx context.Context, name, targetPath string, existing []string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	var existingName string
	for _, entry := range existing {
		if strings.EqualFold(entry, name) {
			existingName = entry
			break
		}
	}
	if existingName == "" {
		return name, false, nil
	}
	isDirectory := false
	if info, statErr := os.Stat(filepath.Join(targetPath, existingName)); statErr == nil {
		isDirectory = info.IsDir()
	}
	if !isDirectory {
		return driveFS(d).GetUniqueName(name, existing), false, nil
	}
	choice, err := d.dialog.ResolveDirectoryConflict(platform.DirectoryConflictOptions{Name: existingName})
	if err != nil {
		return "", false, err
	}
	switch choice {
	case platform.DirectoryConflictOverwrite:
		return existingName, false, nil
	case platform.DirectoryConflictRename:
		return driveFS(d).GetUniqueName(name, existing), false, nil
	case platform.DirectoryConflictCancel:
		return "", true, nil
	default:
		return "", false, fmt.Errorf("unsupported directory conflict choice %q", choice)
	}
}

func driveFS(d *Drive) *platform.FS {
	if d != nil && d.fs != nil {
		return d.fs
	}
	return platform.NewFS()
}

func setDownloadRootName(metadata *DownloadMetadata, name string) {
	metadata.Root.Name = name
	for index := range metadata.Dirs {
		if metadata.Dirs[index].ID == metadata.Root.ID {
			metadata.Dirs[index].Name = name
		}
	}
}

func (d *Drive) executeDownload(ctx context.Context, transfers *transfer.Transfer, pid string, params StartDownloadParams, metadata DownloadMetadata) error {
	paths, singleFile, err := resolveDownloadPaths(metadata, params.TargetPath)
	if err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Stage: "resolve-target"})
	}
	record, _ := transfers.Get(pid)
	downloadedBytes := record.TransferredSize
	downloadedFiles := record.TransferredFiles
	if downloadedBytes == 0 {
		for _, file := range metadata.Files {
			if transfers.IsFileCompleted(pid, file.ID) || file.CompAlg != nil {
				continue
			}
			parentPath := paths[parentDownloadKey(file, metadata.Root.ID, singleFile)]
			if parentPath == "" {
				continue
			}
			if info, statErr := os.Stat(filepath.Join(parentPath, file.Name) + ".ntmp"); statErr == nil {
				downloadedBytes += min(info.Size(), file.Size)
			}
		}
	}
	progress := transfer.StatusProgress
	if err := transfers.Update(pid, transfer.Updates{Status: &progress, TransferredSize: &downloadedBytes, TransferredFiles: &downloadedFiles}); err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Stage: "prepare"})
	}

	concurrency := d.downloadConcurrency(ctx)
	d.parallelDownload.SetRequestConcurrency(concurrency)
	type downloadJob struct {
		file    *transfer.DownloadFile
		dirPath string
	}
	jobs := make(chan downloadJob)
	var workers sync.WaitGroup
	var stateMu sync.Mutex
	failures := make([]error, 0)
	workerCount := min(max(1, concurrency), max(1, len(metadata.Files)+len(paths)))
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if job.file == nil {
					if err := os.MkdirAll(job.dirPath, 0o755); err != nil {
						failure := infra.AnnotateError(fmt.Errorf("create download directory %q: %w", job.dirPath, err), infra.Diagnostic{Stage: "write"})
						stateMu.Lock()
						failures = append(failures, failure)
						stateMu.Unlock()
					}
					continue
				}
				file := *job.file
				if transfers.IsFileCompleted(pid, file.ID) {
					continue
				}
				parentPath := paths[parentDownloadKey(file, metadata.Root.ID, singleFile)]
				if parentPath == "" {
					failure := infra.AnnotateError(fmt.Errorf("download parent path missing for %s", file.Name), infra.Diagnostic{Stage: "resolve-target"})
					stateMu.Lock()
					failures = append(failures, failure)
					stateMu.Unlock()
					_ = transfers.MarkFileFailed(pid, failure.Error())
					continue
				}
				if !singleFile {
					if err := os.MkdirAll(parentPath, 0o755); err != nil {
						failure := infra.AnnotateError(fmt.Errorf("create download directory %q: %w", parentPath, err), infra.Diagnostic{Stage: "write"})
						stateMu.Lock()
						failures = append(failures, failure)
						stateMu.Unlock()
						_ = transfers.MarkFileFailed(pid, failure.Error())
						continue
					}
				}
				destination := filepath.Join(parentPath, file.Name)
				if info, statErr := os.Stat(destination); statErr == nil {
					if !info.Mode().IsRegular() {
						failure := infra.AnnotateError(fmt.Errorf("download target is not a file: %s", destination), infra.Diagnostic{Stage: "write"})
						stateMu.Lock()
						failures = append(failures, failure)
						stateMu.Unlock()
						_ = transfers.MarkFileFailed(pid, failure.Error())
						continue
					}
					stateMu.Lock()
					downloadedBytes += transfer.LogicalFileBytes(file)
					downloadedFiles++
					bytesNow, filesNow := downloadedBytes, downloadedFiles
					stateMu.Unlock()
					_ = transfers.MarkFileCompleted(pid, file.ID)
					_ = transfers.Update(pid, transfer.Updates{TransferredSize: &bytesNow, TransferredFiles: &filesNow})
					continue
				}
				downloadErr := d.downloadDriveFile(ctx, transfers, file, destination, params.Link, func(bytes int64) {
					stateMu.Lock()
					downloadedBytes += bytes
					bytesNow := downloadedBytes
					stateMu.Unlock()
					_ = transfers.Update(pid, transfer.Updates{TransferredSize: &bytesNow})
				})
				if downloadErr != nil {
					if errors.Is(downloadErr, context.Canceled) {
						continue
					}
					failure := fmt.Errorf("%s: %w", file.Name, downloadErr)
					stateMu.Lock()
					failures = append(failures, failure)
					stateMu.Unlock()
					_ = transfers.MarkFileFailed(pid, failure.Error())
					continue
				}
				_ = transfers.MarkFileCompleted(pid, file.ID)
				stateMu.Lock()
				downloadedFiles++
				filesNow := downloadedFiles
				stateMu.Unlock()
				_ = transfers.Update(pid, transfer.Updates{TransferredFiles: &filesNow})
			}
		}()
	}
	queue := func(job downloadJob) bool {
		select {
		case <-ctx.Done():
			return false
		case jobs <- job:
			return true
		}
	}
	for _, file := range redistributeDownloadFiles(metadata.Files) {
		if !queue(downloadJob{file: &file}) {
			break
		}
	}
	if ctx.Err() == nil && !singleFile {
		for _, path := range paths {
			if !queue(downloadJob{dirPath: path}) {
				break
			}
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	completed := transfer.StatusCompleted
	hundred := 100.0
	total := metadata.TotalBytes
	totalFiles := len(metadata.Files)
	if err := transfers.Update(pid, transfer.Updates{Status: &completed, TransferredSize: &total, TransferredFiles: &totalFiles, Progress: &hundred}); err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Stage: "finalize"})
	}
	inspectionPaths := make([]string, 0, len(record.DestinationTargets))
	for _, target := range record.DestinationTargets {
		if target.Kind == transfer.DestinationDirectory {
			inspectionPaths = append(inspectionPaths, target.Path)
		}
	}
	if d.inspectAddedMods != nil && len(inspectionPaths) > 0 {
		d.inspectAddedMods(inspectionPaths)
	}
	if d.eventEmit != nil {
		name := metadata.Root.Name
		if latest, ok := transfers.Get(pid); ok && latest.Name != "" {
			name = latest.Name
		}
		d.eventEmit("download:completed", map[string]any{"path": params.TargetPath, "name": name})
	}
	return nil
}

func resolveDownloadPaths(metadata DownloadMetadata, targetPath string) (map[string]string, bool, error) {
	singleFile := len(metadata.Dirs) == 0 && len(metadata.Files) == 1
	paths := make(map[string]string, len(metadata.Dirs)+1)
	if singleFile {
		paths[metadata.Root.ID] = targetPath
		return paths, true, nil
	}
	rootPath := filepath.Join(targetPath, metadata.Root.Name)
	paths[metadata.Root.ID] = rootPath
	children := make(map[string][]transfer.Directory)
	for _, directory := range metadata.Dirs {
		if directory.ID == metadata.Root.ID || directory.ParentID == nil {
			continue
		}
		children[*directory.ParentID] = append(children[*directory.ParentID], directory)
	}
	stack := []string{metadata.Root.ID}
	for len(stack) > 0 {
		parentID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		parentPath := paths[parentID]
		for _, child := range children[parentID] {
			if _, exists := paths[child.ID]; exists {
				return nil, false, fmt.Errorf("duplicate download directory id %q", child.ID)
			}
			paths[child.ID] = filepath.Join(parentPath, child.Name)
			stack = append(stack, child.ID)
		}
	}
	return paths, false, nil
}

func resolveDownloadDestinationTargets(metadata DownloadMetadata, targetPath string) ([]transfer.DestinationTarget, error) {
	paths, singleFile, err := resolveDownloadPaths(metadata, targetPath)
	if err != nil {
		return nil, err
	}
	if singleFile {
		return []transfer.DestinationTarget{{
			Path: filepath.Join(targetPath, metadata.Files[0].Name),
			Kind: transfer.DestinationFile,
		}}, nil
	}
	if metadata.Root.Name != "" {
		return []transfer.DestinationTarget{{
			Path: paths[metadata.Root.ID],
			Kind: transfer.DestinationDirectory,
		}}, nil
	}

	destinationTargets := make([]transfer.DestinationTarget, 0)
	for _, directory := range metadata.Dirs {
		if directory.ParentID == nil || *directory.ParentID != metadata.Root.ID {
			continue
		}
		if path := paths[directory.ID]; path != "" {
			destinationTargets = append(destinationTargets, transfer.DestinationTarget{
				Path: path,
				Kind: transfer.DestinationDirectory,
			})
		}
	}
	rootPath := paths[metadata.Root.ID]
	for _, file := range metadata.Files {
		if file.ParentID != nil && *file.ParentID == metadata.Root.ID {
			destinationTargets = append(destinationTargets, transfer.DestinationTarget{
				Path: filepath.Join(rootPath, file.Name),
				Kind: transfer.DestinationFile,
			})
		}
	}
	return destinationTargets, nil
}

func parentDownloadKey(file transfer.DownloadFile, rootID string, singleFile bool) string {
	if singleFile {
		return rootID
	}
	if file.ParentID == nil {
		return ""
	}
	return *file.ParentID
}

func redistributeDownloadFiles(files []transfer.DownloadFile) []transfer.DownloadFile {
	const largeThreshold = 50 * 1024 * 1024
	large := make([]transfer.DownloadFile, 0)
	small := make([]transfer.DownloadFile, 0)
	for _, file := range files {
		if file.Size >= largeThreshold {
			large = append(large, file)
		} else {
			small = append(small, file)
		}
	}
	if len(large) == 0 || len(small) == 0 {
		return slices.Clone(files)
	}
	interval := max(1, len(small)/len(large))
	out := make([]transfer.DownloadFile, 0, len(files))
	for len(small) > 0 || len(large) > 0 {
		count := min(interval, len(small))
		out = append(out, small[:count]...)
		small = small[count:]
		if len(large) > 0 {
			out = append(out, large[0])
			large = large[1:]
		}
	}
	return out
}

func (d *Drive) fetchPresignedDownloadURL(ctx context.Context, fileID string, link *DownloadLink) (string, error) {
	query := url.Values{"uuid": []string{fileID}, "presign": []string{"true"}}
	header := make(http.Header)
	if link != nil {
		query.Set("linkId", link.LinkID)
		header.Set("nhd-link-token", link.Token)
	}
	data, _, edenErr, err := d.doJSONHeaders(ctx, http.MethodGet, "/akasha/file/download", query, header, nil)
	if err != nil {
		return "", err
	}
	if edenErr != nil {
		return "", CreateDriveAPIError(edenErr.asAny(), "presigned download", edenErr.Status)
	}
	record, ok := asRecord(data)
	if !ok {
		return "", errors.New("presigned download URL not received")
	}
	rawURL, ok := record["url"].(string)
	if !ok || strings.TrimSpace(rawURL) == "" {
		return "", errors.New("presigned download URL not received")
	}
	return rawURL, nil
}

func (d *Drive) downloadConcurrency(ctx context.Context) int {
	if d.settings == nil {
		return 32
	}
	value, err := d.settings.GetDownloadConcurrency(ctx)
	if err != nil || value < 1 {
		return 32
	}
	return value
}

func (d *Drive) failDownloadTransfer(transfers *transfer.Transfer, pid, stage string, failure error) error {
	if errors.Is(failure, context.Canceled) {
		return failure
	}
	status := transfer.StatusError
	message := failure.Error()
	updateErr := transfers.Update(pid, transfer.Updates{Status: &status, Error: &message})
	reported := d.reportDownloadFailure(transfers, pid, stage, failure)
	if updateErr == nil {
		return reported
	}
	updateReported := infra.ReportError(d.log, updateErr, "Drive", infra.Diagnostic{
		Severity:  infra.DiagnosticError,
		Operation: "download",
		Stage:     "record-failure",
		Fields:    driveTransferFields(transfers, pid, ""),
	})
	return errors.Join(reported, updateReported)
}

func (d *Drive) reportDownloadFailure(transfers *transfer.Transfer, pid, stage string, failure error) error {
	return infra.ReportError(d.log, failure, "Drive", infra.Diagnostic{
		Operation: "download",
		Stage:     stage,
		Fields:    driveTransferFields(transfers, pid, ""),
	})
}

func cloneDownloadMetadata(metadata DownloadMetadata) DownloadMetadata {
	metadata.Files = slices.Clone(metadata.Files)
	metadata.Dirs = slices.Clone(metadata.Dirs)
	return metadata
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
