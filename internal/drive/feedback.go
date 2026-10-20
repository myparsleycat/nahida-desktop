package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"sync"
)

// feedbackModPlanRoute plans the mod files of a problem report through the
// akasha upload intents.
const feedbackModPlanRoute = "/feedback/mod/files:plan"

// FeedbackModFile is one hashed file of a mod attached to a problem report.
type FeedbackModFile struct {
	// Ref is the attachment ref the report declares the file under.
	Ref string
	// Path is the slash-separated path below the attached folder's parent, so
	// it starts with the folder's own name.
	Path     string
	FullPath string
	Size     int64
	SHA256   string
}

// FeedbackModResult is what the server holds of a report's mod files once they
// are uploaded.
type FeedbackModResult struct {
	// Stored lists the refs whose content the server stores.
	Stored []string
	// Refused counts the files the server refused by the upload rules, which
	// the report leaves out the way a drive upload does.
	Refused int
}

// FeedbackPlanError is a refusal of the feedback plan route, carrying the
// backend's message.
type FeedbackPlanError struct {
	Status  int
	Message string
}

func (e *FeedbackPlanError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("the feedback plan failed with status %d", e.Status)
	}
	return fmt.Sprintf("%s (status %d)", e.Message, e.Status)
}

// UploadFeedbackMod uploads the mod files of one report page by page, the way a
// drive upload sends files: deduplicated, compressed, packed or split by the
// server's rules, and validated by the server. The .pak, .utoc and .ucas files
// of one NTE archive set stay on the same page, since the server bundles them
// per folder and base name. A file the server refuses by the upload rules is
// left out and counted; any other failure ends the run.
//
//wails:ignore
func (d *Drive) UploadFeedbackMod(
	ctx context.Context,
	reportID string,
	files []FeedbackModFile,
	onProgress func(bytes int64),
) (FeedbackModResult, error) {
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return FeedbackModResult{}, err
	}
	finals := make([]FinalUploadFile, len(files))
	byRef := make(map[string]FeedbackModFile, len(files))
	for index, file := range files {
		byRef[file.Ref] = file
		finals[index] = FinalUploadFile{
			UploadFile: UploadFile{
				FID:      file.Ref,
				Path:     file.Path,
				Name:     path.Base(file.Path),
				Size:     file.Size,
				FullPath: filepath.ToSlash(file.FullPath),
			},
			ParentID: path.Dir(file.Path),
			SHA256:   file.SHA256,
		}
	}
	pages, err := paginateUploadFiles(finals, max(rules.MaxPlanFiles, 1))
	if err != nil {
		return FeedbackModResult{}, err
	}

	var result FeedbackModResult
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		plan, err := d.planFeedbackMod(ctx, reportID, page, byRef)
		if err != nil {
			return result, err
		}
		replan := func(ctx context.Context, files []FinalUploadFile) (UploadPlan, error) {
			return d.planFeedbackMod(ctx, reportID, files, byRef)
		}
		stored, refused, err := d.uploadFeedbackPage(ctx, page, plan, rules, replan, onProgress)
		if err != nil {
			return result, err
		}
		result.Stored = append(result.Stored, stored...)
		result.Refused += refused
	}
	return result, nil
}

func (d *Drive) planFeedbackMod(
	ctx context.Context,
	reportID string,
	page []FinalUploadFile,
	byRef map[string]FeedbackModFile,
) (UploadPlan, error) {
	declarations := make([]map[string]any, len(page))
	for index, final := range page {
		file := byRef[final.FID]
		declarations[index] = map[string]any{
			"ref":    file.Ref,
			"kind":   "mod",
			"path":   file.Path,
			"sha256": file.SHA256,
			"size":   file.Size,
		}
	}
	body := map[string]any{
		"reportId":     reportID,
		"capabilities": []string{"nte-bundle-v1"},
		"files":        declarations,
	}
	decoded, edenErr, err := d.doJSON(ctx, http.MethodPost, feedbackModPlanRoute, nil, body)
	if err != nil {
		return UploadPlan{}, err
	}
	if edenErr != nil {
		planErr := &FeedbackPlanError{Status: edenErr.Status}
		if record, ok := asRecord(edenErr.Value); ok {
			planErr.Message, _ = record["error"].(string)
		}
		return UploadPlan{}, planErr
	}

	var response struct {
		Items      []UploadPlanItem  `json:"items"`
		Uploads    []UploadPlanEntry `json:"uploads"`
		NTEBundles []NTEBundle       `json:"nteBundles"`
	}
	raw, err := json.Marshal(decoded)
	if err != nil {
		return UploadPlan{}, err
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return UploadPlan{}, fmt.Errorf("decode the feedback plan: %w", err)
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
	return plan, nil
}

// uploadFeedbackPage runs one plan page and answers the refs it stored and how
// many files the server refused by its rules. The run reports a refusal as a
// failure too, so a page whose every file is settled one way or the other
// counts as done. The report keeps its plan request, so planning a file again
// after a storage failure hands it the intent it already holds.
func (d *Drive) uploadFeedbackPage(
	ctx context.Context,
	page []FinalUploadFile,
	plan UploadPlan,
	rules UploadRules,
	replan func(context.Context, []FinalUploadFile) (UploadPlan, error),
	onProgress func(bytes int64),
) ([]string, int, error) {
	planned := make(map[string]UploadPlanItem, len(plan.Items))
	for _, item := range plan.Items {
		planned[item.ClientID] = item
	}
	admitted := make([]FinalUploadFile, 0, len(page))
	denied := 0
	for _, file := range page {
		if item, ok := planned[file.FID]; ok && item.Status == "denied" && item.Reason != "" {
			denied++
			continue
		}
		admitted = append(admitted, file)
	}

	var storedMu sync.Mutex
	stored := make(map[string]struct{}, len(admitted))
	refused, err := d.runUploadPlanWithRecovery(ctx, admitted, plan, rules, d.uploadConcurrency(ctx),
		func(progress UploadExecutionProgress) {
			if progress.FileID != "" {
				storedMu.Lock()
				stored[progress.FileID] = struct{}{}
				storedMu.Unlock()
			}
			if onProgress != nil && progress.Bytes != 0 {
				onProgress(progress.Bytes)
			}
		}, replan)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, 0, ctxErr
		}
		for _, file := range admitted {
			_, done := stored[file.FID]
			_, rejected := refused[file.FID]
			if !done && !rejected {
				return nil, 0, err
			}
		}
	}

	refs := make([]string, 0, len(admitted))
	for _, file := range admitted {
		if _, done := stored[file.FID]; done {
			if _, rejected := refused[file.FID]; !rejected {
				refs = append(refs, file.FID)
			}
		}
	}
	return refs, denied + len(admitted) - len(refs), nil
}
