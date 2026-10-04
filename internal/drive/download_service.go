package drive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/samber/lo"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

type StartDownloadParams struct {
	Items         []DownloadItem     `json:"items"`
	TargetPath    string             `json:"targetPath,omitempty"`
	Link          *DownloadLink      `json:"link,omitempty"`
	Mod           *DownloadModAccess `json:"mod,omitempty"`
	ModTicket     string             `json:"-"`
	Data          *DownloadMetadata  `json:"data,omitempty"`
	SuggestedName string             `json:"suggestedName,omitempty"`
	Source        string             `json:"source,omitempty"`
}

type StartDownloadResult struct {
	PID    string `json:"pid"`
	Status string `json:"status"`
}

func (d *Drive) StartDownload(ctx context.Context, params StartDownloadParams) (result StartDownloadResult, err error) {
	redemptionFailed := false
	defer func() {
		normalizeDriveBoundaryError(&err, "fn:startDownload")
		if redemptionFailed {
			err = &ModTicketRedemptionError{err: err}
		}
	}()
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
	params.Items = lo.UniqBy(params.Items, func(item DownloadItem) string { return item.ID })
	// An unnamed item stays unnamed, so the server's name is used once the walk
	// reports it.
	for index := range params.Items {
		if params.Items[index].Name != "" {
			params.Items[index].Name = fs.SanitizeWindowsFilename(params.Items[index].Name, " ")
		}
	}
	if params.ModTicket != "" {
		redemption, err := d.redeemModDownloadTicket(ctx, params.ModTicket)
		if err != nil {
			redemptionFailed = true
			return StartDownloadResult{}, err
		}
		params.Items = []DownloadItem{
			{ID: redemption.ItemID, IsDir: true, Name: fs.SanitizeWindowsFilename(redemption.Name, " ")},
		}
		params.Mod = &DownloadModAccess{Grant: redemption.Grant}
		params.Link = nil
		params.ModTicket = ""
		if params.SuggestedName == "" {
			params.SuggestedName = params.Items[0].Name
		}
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
	layout, canceled, err := d.reserveDownloadNames(ctx, params)
	if err != nil {
		return StartDownloadResult{}, err
	}
	if canceled {
		return StartDownloadResult{Status: "canceled"}, nil
	}
	name := layout.rootName
	if len(params.Items) != 1 {
		name = fmt.Sprintf("%d items", len(params.Items))
	}

	d.sweepDownloadSpools()
	pid := uuid.NewString()
	if _, err := d.transfer.Create(transfer.CreateParams{
		PID:                pid,
		Type:               "download",
		Name:               name,
		Path:               filepath.ToSlash(params.TargetPath),
		DestinationTargets: layout.targets(params.Items, params.TargetPath),
		CurrentID:          downloadCurrentID(params),
		InitialStatus:      transfer.StatusPending,
		RestartData:        params,
	}); err != nil {
		return StartDownloadResult{}, err
	}
	run := &downloadRun{params: params, layout: layout}
	if err := d.transfer.RegisterRunner(
		pid,
		func(runCtx context.Context, transfers *transfer.Transfer, runnerPID string) error {
			return d.runDownload(runCtx, transfers, runnerPID, run)
		},
	); err != nil {
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
	// The dialog still needs something to show for an unnamed item.
	names := make([]string, len(params.Items))
	for index, item := range params.Items {
		names[index] = driveFS(d).SanitizeWindowsFilename(item.Name, " ")
	}
	isSingle := len(params.Items) == 1
	suggestedName := params.SuggestedName
	if isSingle && suggestedName == "" {
		suggestedName = names[0]
	}
	source := params.Source
	if source == "" {
		source = "nahidaLive"
	}
	path, fileName, err := d.paths.SelectDownloadPath(
		ctx,
		suggestedName,
		source,
		names,
		isSingle && !params.Items[0].IsDir,
	)
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

// downloadLayout is the top-level naming a download settles before it is
// queued, so its destinations are reserved while it waits for its turn.
type downloadLayout struct {
	// rootName names a single item. It is empty when the caller did not name
	// the item, and the server's name is used once the walk reports it.
	rootName string
	// names maps each item of a multi-item download to its top-level name.
	names map[string]string
	// used lists the names already taken in the target directory.
	used []string
}

func (l downloadLayout) targets(items []DownloadItem, targetPath string) []transfer.DestinationTarget {
	target := func(item DownloadItem, name string) transfer.DestinationTarget {
		kind := transfer.DestinationFile
		if item.IsDir {
			kind = transfer.DestinationDirectory
		}
		return transfer.DestinationTarget{Path: filepath.Join(targetPath, name), Kind: kind}
	}
	if len(items) == 1 {
		if l.rootName == "" {
			return nil
		}
		return []transfer.DestinationTarget{target(items[0], l.rootName)}
	}
	targets := make([]transfer.DestinationTarget, 0, len(items))
	for _, item := range items {
		if name := l.names[item.ID]; name != "" {
			targets = append(targets, target(item, name))
		}
	}
	return targets
}

func (d *Drive) reserveDownloadNames(ctx context.Context, params StartDownloadParams) (downloadLayout, bool, error) {
	fs := driveFS(d)
	entries, err := os.ReadDir(params.TargetPath)
	if err != nil {
		return downloadLayout{}, false, err
	}
	layout := downloadLayout{used: make([]string, len(entries))}
	for index, entry := range entries {
		layout.used[index] = entry.Name()
	}

	if len(params.Items) == 1 {
		item := params.Items[0]
		name := item.Name
		if params.SuggestedName != "" {
			name = fs.SanitizeWindowsFilename(params.SuggestedName, " ")
		}
		if item.IsDir && name != "" {
			resolved, canceled, err := d.resolveDirectoryDownloadName(ctx, name, params.TargetPath, layout.used)
			if err != nil || canceled {
				return downloadLayout{}, canceled, err
			}
			name = resolved
		}
		layout.rootName = name
		return layout, false, nil
	}

	// Folders claim their names before files, so a clash renames the file.
	layout.names = make(map[string]string, len(params.Items))
	for _, wantDir := range []bool{true, false} {
		for _, item := range params.Items {
			if item.IsDir != wantDir || item.Name == "" {
				continue
			}
			name := fs.GetUniqueName(item.Name, layout.used)
			layout.names[item.ID] = name
			layout.used = append(layout.used, name)
		}
	}
	return layout, false, nil
}

// downloadRun is the state a download's runner keeps across pause, resume,
// and retry.
type downloadRun struct {
	params StartDownloadParams
	layout downloadLayout
	// plan is set once enumeration finishes, so a later run skips the walk.
	plan *downloadPlan
}

func (d *Drive) runDownload(
	ctx context.Context,
	transfers *transfer.Transfer,
	pid string,
	run *downloadRun,
) error {
	preparing := transfer.StatusPreparing
	if err := transfers.Update(
		pid,
		transfer.Updates{Status: &preparing, ClearError: true, ClearErrorCode: true},
	); err != nil {
		return d.reportDownloadFailure(transfers, pid, "prepare", err)
	}

	if run.plan == nil {
		spoolPath, err := d.downloadSpoolPath(pid)
		if err != nil {
			return d.failDownloadTransfer(transfers, pid, "metadata", err)
		}
		plan, err := d.enumerateDownload(ctx, run.params, run.layout, spoolPath, func(files int, totalBytes int64) {
			_ = transfers.Update(pid, transfer.Updates{TotalFiles: &files, TotalSize: &totalBytes})
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return d.failDownloadTransfer(transfers, pid, "metadata", err)
		}
		run.plan = plan
	}
	plan := run.plan

	destinationTargets, err := resolveDownloadDestinationTargets(plan, run.params.TargetPath)
	if err != nil {
		return d.failDownloadTransfer(transfers, pid, "resolve-target", err)
	}
	totalFiles, totalBytes := plan.fileCount, max(0, plan.totalBytes)
	updates := transfer.Updates{
		TotalFiles:         &totalFiles,
		TotalSize:          &totalBytes,
		DestinationTargets: destinationTargets,
	}
	// An item queued without a name takes the one the walk settled.
	if len(run.params.Items) == 1 && run.layout.rootName == "" && plan.root.Name != "" {
		updates.Name = &plan.root.Name
	}
	if err := transfers.Update(pid, updates); err != nil {
		return d.failDownloadTransfer(transfers, pid, "metadata", err)
	}
	if err := d.executeDownload(ctx, transfers, pid, run.params, plan); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return d.failDownloadTransfer(transfers, pid, "", err)
	}

	run.plan = nil
	d.reportCleanup(plan.remove(), "runDownload")
	return nil
}

func (d *Drive) resolveDirectoryDownloadName(
	ctx context.Context,
	name, targetPath string,
	existing []string,
) (string, bool, error) {
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

func (d *Drive) executeDownload(
	ctx context.Context,
	transfers *transfer.Transfer,
	pid string,
	params StartDownloadParams,
	plan *downloadPlan,
) error {
	paths, singleFile, err := resolveDownloadPaths(plan, params.TargetPath)
	if err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Stage: "resolve-target"})
	}
	record, _ := transfers.Get(pid)
	downloadedBytes := record.TransferredSize
	downloadedFiles := record.TransferredFiles
	if downloadedBytes == 0 {
		for item, err := range plan.files() {
			if err != nil {
				return infra.AnnotateError(err, infra.Diagnostic{Stage: "metadata"})
			}
			file := item.file
			if transfers.IsIndexCompleted(pid, item.index) || file.CompAlg != nil {
				continue
			}
			parentPath := paths[parentDownloadKey(file, plan.root.ID, singleFile)]
			if parentPath == "" {
				continue
			}
			if info, statErr := os.Stat(filepath.Join(parentPath, file.Name) + ".ntmp"); statErr == nil {
				downloadedBytes += min(info.Size(), file.Size)
			}
		}
	}
	progress := transfer.StatusProgress
	if err := transfers.Update(
		pid,
		transfer.Updates{Status: &progress, TransferredSize: &downloadedBytes, TransferredFiles: &downloadedFiles},
	); err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Stage: "prepare"})
	}

	concurrency := d.downloadConcurrency(ctx)
	d.parallelDownload.SetRequestConcurrency(concurrency)
	// A job is a file to download, or a directory to create when dirPath is set.
	type downloadJob struct {
		spooledFile
		dirPath string
	}
	jobs := make(chan downloadJob)
	var workers sync.WaitGroup
	var stateMu sync.Mutex
	failures := make([]error, 0)
	workerCount := min(max(1, concurrency), max(1, plan.fileCount+len(paths)))
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if job.dirPath != "" {
					if err := os.MkdirAll(job.dirPath, 0o755); err != nil {
						failure := infra.AnnotateError(
							fmt.Errorf("create download directory %q: %w", job.dirPath, err),
							infra.Diagnostic{Stage: "write"},
						)
						stateMu.Lock()
						failures = append(failures, failure)
						stateMu.Unlock()
					}
					continue
				}
				file := job.file
				if transfers.IsIndexCompleted(pid, job.index) {
					continue
				}
				parentPath := paths[parentDownloadKey(file, plan.root.ID, singleFile)]
				if parentPath == "" {
					failure := infra.AnnotateError(
						fmt.Errorf("download parent path missing for %s", file.Name),
						infra.Diagnostic{Stage: "resolve-target"},
					)
					stateMu.Lock()
					failures = append(failures, failure)
					stateMu.Unlock()
					_ = transfers.MarkFileFailed(pid, failure.Error())
					continue
				}
				if !singleFile {
					if err := os.MkdirAll(parentPath, 0o755); err != nil {
						failure := infra.AnnotateError(
							fmt.Errorf("create download directory %q: %w", parentPath, err),
							infra.Diagnostic{Stage: "write"},
						)
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
						failure := infra.AnnotateError(
							fmt.Errorf("download target is not a file: %s", destination),
							infra.Diagnostic{Stage: "write"},
						)
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
					_ = transfers.MarkIndexCompleted(pid, job.index)
					_ = transfers.Update(pid, transfer.Updates{TransferredSize: &bytesNow, TransferredFiles: &filesNow})
					continue
				}
				downloadErr := d.downloadDriveFile(
					ctx,
					transfers,
					file,
					destination,
					downloadAccess{Link: params.Link, Mod: params.Mod},
					func(bytes int64) {
						stateMu.Lock()
						downloadedBytes += bytes
						bytesNow := downloadedBytes
						stateMu.Unlock()
						_ = transfers.Update(pid, transfer.Updates{TransferredSize: &bytesNow})
					},
				)
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
				_ = transfers.MarkIndexCompleted(pid, job.index)
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
	var spoolErr error
	for item, err := range plan.scheduled() {
		if err != nil {
			spoolErr = infra.AnnotateError(err, infra.Diagnostic{Stage: "metadata"})
			break
		}
		if !queue(downloadJob{spooledFile: item}) {
			break
		}
	}
	if spoolErr == nil && ctx.Err() == nil && !singleFile {
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
	if spoolErr != nil {
		failures = append(failures, spoolErr)
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	completed := transfer.StatusCompleted
	hundred := 100.0
	total := plan.totalBytes
	totalFiles := plan.fileCount
	if err := transfers.Update(
		pid,
		transfer.Updates{
			Status:           &completed,
			TransferredSize:  &total,
			TransferredFiles: &totalFiles,
			Progress:         &hundred,
		},
	); err != nil {
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
		name := plan.root.Name
		if latest, ok := transfers.Get(pid); ok && latest.Name != "" {
			name = latest.Name
		}
		d.eventEmit("download:completed", map[string]any{"path": params.TargetPath, "name": name})
	}
	return nil
}

func resolveDownloadPaths(plan *downloadPlan, targetPath string) (map[string]string, bool, error) {
	paths := make(map[string]string, len(plan.dirs)+1)
	if plan.singleFile() {
		paths[plan.root.ID] = targetPath
		return paths, true, nil
	}
	rootPath := filepath.Join(targetPath, plan.root.Name)
	paths[plan.root.ID] = rootPath
	children := make(map[string][]transfer.Directory)
	for _, directory := range plan.dirs {
		if directory.ID == plan.root.ID || directory.ParentID == nil {
			continue
		}
		children[*directory.ParentID] = append(children[*directory.ParentID], directory)
	}
	stack := []string{plan.root.ID}
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

func resolveDownloadDestinationTargets(plan *downloadPlan, targetPath string) ([]transfer.DestinationTarget, error) {
	paths, singleFile, err := resolveDownloadPaths(plan, targetPath)
	if err != nil {
		return nil, err
	}
	rootPath := paths[plan.root.ID]
	if !singleFile && plan.root.Name != "" {
		return []transfer.DestinationTarget{{Path: rootPath, Kind: transfer.DestinationDirectory}}, nil
	}

	destinationTargets := make([]transfer.DestinationTarget, 0, len(plan.rootFiles))
	for _, directory := range plan.dirs {
		if directory.ParentID == nil || *directory.ParentID != plan.root.ID {
			continue
		}
		if path := paths[directory.ID]; path != "" {
			destinationTargets = append(destinationTargets, transfer.DestinationTarget{
				Path: path,
				Kind: transfer.DestinationDirectory,
			})
		}
	}
	for _, name := range plan.rootFiles {
		destinationTargets = append(destinationTargets, transfer.DestinationTarget{
			Path: filepath.Join(rootPath, name),
			Kind: transfer.DestinationFile,
		})
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

func (d *Drive) fetchPresignedDownloadURL(ctx context.Context, fileID string, access downloadAccess) (string, error) {
	path := "/akasha/file/download"
	query := url.Values{"uuid": []string{fileID}, "presign": []string{"true"}}
	header := make(http.Header)
	switch {
	case access.Mod != nil:
		// A mod file is presigned by its own route, gated by the mod credentials.
		path = "/akasha/mod/file/download"
		query = url.Values{"itemId": []string{fileID}, "presign": []string{"true"}}
		if access.Mod.Token != "" {
			header.Set("x-token", access.Mod.Token)
		}
		if access.Mod.Sig != "" {
			header.Set("x-sig", access.Mod.Sig)
		}
		if access.Mod.Grant != "" {
			header.Set("x-mod-download-grant", access.Mod.Grant)
		}
	case access.Link != nil:
		query.Set("linkId", access.Link.LinkID)
		header.Set("nhd-link-token", access.Link.Token)
	}
	data, _, edenErr, err := d.doJSONHeaders(ctx, http.MethodGet, path, query, header, nil)
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
