package drive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"nahida.live/desktop/internal/infra"
)

// BackupAPIError is a refusal of a backup route. Missing carries the content
// hashes a refused commit still waits for, and LatestSnapshotID the device's
// newest complete snapshot when the snapshot a run was taken against is no
// longer it ("" when the device has none).
type BackupAPIError struct {
	Status           int
	Code             string
	Missing          []string
	LatestSnapshotID string
}

// ErrBackupPlanning means the snapshot did not register every scanned file.
// A caller must abort it rather than commit the pages registered so far.
var ErrBackupPlanning = errors.New("backup file planning failed")

// ErrBackupSourceRead means a local file could not be read during upload.
var ErrBackupSourceRead = errors.New("backup source read failed")

func (e *BackupAPIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("backup request failed with status %d", e.Status)
	}
	return e.Code
}

func backupAPIError(edenErr *edenError) error {
	apiErr := &BackupAPIError{Status: edenErr.Status}
	if record, ok := asRecord(edenErr.Value); ok {
		apiErr.Code, _ = record["error"].(string)
		if missing, ok := record["missing"].([]any); ok {
			for _, value := range missing {
				if sha, ok := value.(string); ok {
					apiErr.Missing = append(apiErr.Missing, sha)
				}
			}
		}
		apiErr.LatestSnapshotID, _ = record["latestSnapshotId"].(string)
	}
	if text, ok := edenErr.Value.(string); ok && apiErr.Code == "" {
		apiErr.Code = text
	}
	return apiErr
}

// BackupJSON calls a backup route with a JSON body and decodes the answer into
// out, which may be nil.
//
//wails:ignore
func (d *Drive) BackupJSON(ctx context.Context, method, route string, body, out any) error {
	decoded, edenErr, err := d.doJSON(ctx, method, route, nil, body)
	if err != nil {
		return err
	}
	if edenErr != nil {
		return backupAPIError(edenErr)
	}
	if out == nil || decoded == nil {
		return nil
	}
	raw, err := json.Marshal(decoded)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// BackupRules is what a backup run takes from the server's upload rules.
type BackupRules struct {
	// Filter answers why a file may not be backed up, or "" when it may, so a
	// backup holds exactly the files the drive would accept.
	Filter func(name string, size int64) string
	// Version names the rules as the server published them. A refusal made
	// under one version is worth retrying once the version changes.
	Version string
}

// BackupRules asks the server for its upload rules afresh, so a run sees rules
// that changed while the app was open.
//
//wails:ignore
func (d *Drive) BackupRules(ctx context.Context) (BackupRules, error) {
	rules, decoded, err := d.rulesFetcher()(ctx)
	if err != nil {
		return BackupRules{}, err
	}
	// The whole answer is hashed, not only the fields this client reads, and
	// its object keys are marshaled sorted, so the same rules name one version.
	raw, err := json.Marshal(decoded)
	if err != nil {
		return BackupRules{}, err
	}
	sum := sha256.Sum256(raw)

	allowed := extensionMaxSizes(rules, nil)
	return BackupRules{
		Filter: func(name string, size int64) string {
			if isSystemFile(name) {
				return "system_file"
			}
			return string(classifyUploadFile(name, size, allowed, false, rules.MaxFileSize))
		},
		Version: hex.EncodeToString(sum[:]),
	}, nil
}

// BackupRejection is a file of a snapshot the server refused. Denied is set
// when the plan refused it, so the snapshot leaves it out; otherwise its upload
// was refused and the commit still misses its content. Permanent is set when
// the same content is refused again under the same upload rules, so offering
// it again is pointless until they change.
type BackupRejection struct {
	ClientID  string
	Reason    string
	Denied    bool
	Permanent bool
}

// BackupUploadFile is one hashed file of a backup snapshot.
type BackupUploadFile struct {
	ClientID   string
	TargetID   string
	RelPath    string
	FullPath   string
	Size       int64
	SHA256     string
	ModifiedAt time.Time
}

// BackupDeletedFile is one file of the base snapshot a new snapshot no longer
// has.
type BackupDeletedFile struct {
	TargetID string
	RelPath  string
}

// UploadBackupFiles announces what a pending snapshot changes against its base
// page by page: the added or changed files, whose content the server does not
// hold yet is uploaded, then the removed ones. The .pak, .utoc and .ucas files
// of one NTE archive set stay on the same page, since the server bundles them
// per folder and base name and the bundle completes once all of them arrived. A page whose uploads fail does
// not stop the next one; the joined failures are answered at the end, and the
// commit tells which content is still missing. The files the server refused
// are answered too, whether the plan or the upload refused them, also beside
// an error that stops the run.
//
//wails:ignore
func (d *Drive) UploadBackupFiles(
	ctx context.Context,
	snapshotID string,
	files []BackupUploadFile,
	deleted []BackupDeletedFile,
	onProgress func(bytes int64),
) ([]BackupRejection, error) {
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return nil, err
	}
	pageSize := max(rules.MaxPlanFiles, 1)
	route := "/backup/snapshots/" + url.PathEscape(snapshotID) + "/files:plan"

	byClientID := make(map[string]BackupUploadFile, len(files))
	finals := make([]FinalUploadFile, len(files))
	for index, file := range files {
		byClientID[file.ClientID] = file
		// The parent is the folder inside the target, which is what an
		// archive set is grouped under on both sides.
		finals[index] = FinalUploadFile{
			UploadFile: UploadFile{
				FID:      file.ClientID,
				Path:     file.RelPath,
				Name:     path.Base(file.RelPath),
				Size:     file.Size,
				FullPath: filepath.ToSlash(file.FullPath),
			},
			ParentID: file.TargetID + "/" + path.Dir(file.RelPath),
			SHA256:   file.SHA256,
		}
	}
	pages, err := paginateUploadFiles(finals, pageSize)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBackupPlanning, err)
	}

	var rejections []BackupRejection
	denied := map[string]struct{}{}
	var failures []error
	start := 0
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return rejections, err
		}

		payload := make([]map[string]any, len(page))
		for index, final := range page {
			file := byClientID[final.FID]
			payload[index] = map[string]any{
				"clientId":   file.ClientID,
				"targetId":   file.TargetID,
				"path":       file.RelPath,
				"sha256":     file.SHA256,
				"size":       file.Size,
				"modifiedAt": file.ModifiedAt.UTC().Format(time.RFC3339Nano),
			}
		}

		var response struct {
			Items      []UploadPlanItem  `json:"items"`
			Uploads    []UploadPlanEntry `json:"uploads"`
			NTEBundles []NTEBundle       `json:"nteBundles"`
		}
		body := map[string]any{"capabilities": []string{"nte-bundle-v1"}, "files": payload}
		if err := d.BackupJSON(ctx, http.MethodPost, route, body, &response); err != nil {
			return rejections, fmt.Errorf("%w: page starting at file %d: %w", ErrBackupPlanning, start, err)
		}
		if len(response.Items) != len(page) {
			return rejections, fmt.Errorf(
				"%w: page starting at file %d returned %d of %d files",
				ErrBackupPlanning,
				start,
				len(response.Items),
				len(page),
			)
		}
		requested := make(map[string]struct{}, len(page))
		for _, file := range page {
			requested[file.FID] = struct{}{}
		}
		for _, item := range response.Items {
			if _, ok := requested[item.ClientID]; !ok {
				return rejections, fmt.Errorf("%w: unexpected or duplicate file %s", ErrBackupPlanning, item.ClientID)
			}
			delete(requested, item.ClientID)
			if item.Status != "exists" &&
				(item.Status != "pending" || item.IntentID == "") &&
				(item.Status != "denied" || item.Reason == "") {
				return rejections, fmt.Errorf(
					"%w: file %s: %s (%s)",
					ErrBackupPlanning,
					item.ClientID,
					item.Status,
					item.Reason,
				)
			}
			if item.Status == "denied" {
				rejections = append(
					rejections,
					BackupRejection{
						ClientID:  item.ClientID,
						Reason:    item.Reason,
						Denied:    true,
						Permanent: permanentUploadRejection(item.Reason),
					},
				)
				denied[item.ClientID] = struct{}{}
			}
		}
		plan := UploadPlan{
			Items:   response.Items,
			Uploads: make(map[string]UploadPlanEntry, len(response.Uploads)),
			Bundles: make(map[string]NTEBundle, len(response.NTEBundles)),
		}
		for _, upload := range response.Uploads {
			plan.Uploads[upload.IntentID] = upload
		}
		for _, bundle := range response.NTEBundles {
			plan.Bundles[bundle.ID] = bundle
		}

		refused, err := d.executeUploadPlanV2(
			ctx,
			page,
			plan,
			rules,
			d.uploadConcurrency(ctx),
			func(progress UploadExecutionProgress) {
				if onProgress != nil && progress.Bytes != 0 {
					onProgress(progress.Bytes)
				}
			},
		)
		for _, file := range page {
			// A plan denial is refused by the upload run too; it is answered once.
			_, planned := denied[file.FID]
			if reason, ok := refused[file.FID]; ok && !planned {
				rejections = append(rejections, BackupRejection{ClientID: file.FID, Reason: reason, Permanent: true})
			}
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return rejections, ctxErr
			}
			if errors.Is(err, ErrBackupSourceRead) {
				return rejections, err
			}
			failures = append(failures, err)
		}
		start += len(page)
	}

	for start := 0; start < len(deleted); start += pageSize {
		if err := ctx.Err(); err != nil {
			return rejections, err
		}
		page := deleted[start:min(start+pageSize, len(deleted))]
		payload := make([]map[string]string, len(page))
		for index, file := range page {
			payload[index] = map[string]string{"targetId": file.TargetID, "path": file.RelPath}
		}
		body := map[string]any{"files": []any{}, "deleted": payload}
		if err := d.BackupJSON(ctx, http.MethodPost, route, body, nil); err != nil {
			return rejections, fmt.Errorf("%w: removals starting at %d: %w", ErrBackupPlanning, start, err)
		}
	}
	return rejections, errors.Join(failures...)
}

// BackupDownload is where one file of a snapshot is served from, as the
// download route of a backup answers it.
type BackupDownload struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	URL        string  `json:"url"`
	Size       int64   `json:"size"`
	UncompSize *int64  `json:"uncompSize"`
	CompAlg    *string `json:"compAlg"`
}

// DownloadBackupFile writes one file of a snapshot to destination. A CDN
// failure is retried once through a presigned URL.
//
//wails:ignore
func (d *Drive) DownloadBackupFile(
	ctx context.Context,
	snapshotID string,
	file BackupDownload,
	destination string,
	onProgress func(bytes int64),
) error {
	if d == nil || d.download == nil {
		return errors.New("download service is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if file.Size == 0 {
		return os.WriteFile(destination, nil, 0o644)
	}

	fetch := func(source BackupDownload) error {
		var reported int64
		retries := downloadErrorRetries
		err := d.download.File(ctx, infra.DownloadRequest{
			URL:         source.URL,
			Destination: destination,
			Size:        source.Size,
			Compression: stringValue(source.CompAlg),
			Header:      http.Header{},
			Resume:      false,
			Retries:     &retries,
			Progress: func(bytes int64) {
				reported += bytes
				if onProgress != nil {
					onProgress(bytes)
				}
			},
		})
		if err != nil && reported != 0 && onProgress != nil {
			onProgress(-reported)
		}
		return err
	}

	err := fetch(file)
	if err == nil || ctx.Err() != nil {
		return err
	}
	var presigned []BackupDownload
	presignErr := d.BackupJSON(
		ctx,
		http.MethodPost,
		"/backup/snapshots/"+url.PathEscape(snapshotID)+"/files:download",
		map[string]any{"ids": []string{file.ID}, "presign": true},
		&presigned,
	)
	if presignErr != nil || len(presigned) == 0 {
		return errors.Join(err, presignErr)
	}
	if retryErr := fetch(presigned[0]); retryErr != nil {
		return errors.Join(err, retryErr)
	}
	return nil
}
