package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
)

// packMemberError is the failure of one member of a pack, naming the files that
// member stands for.
type packMemberError struct {
	files []FinalUploadFile
	err   error
}

func (e *packMemberError) Error() string {
	return e.files[0].Name + ": " + e.err.Error()
}

func (e *packMemberError) Unwrap() error {
	return e.err
}

func newPackMemberError(member preparedUpload, err error) error {
	return &packMemberError{files: append([]FinalUploadFile{member.source}, member.copies...), err: err}
}

type intentPackResult struct {
	IntentID string `json:"intentId"`
	Status   string `json:"status"`
	FileID   string `json:"fileId,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// uploadPack sends the members as one pack. Beside the logical bytes, onProgress
// carries the change in members sent in full but not yet answered by the
// server; onReady says whether its member is still counted among those.
func (d *Drive) uploadPack(
	ctx context.Context,
	members []preparedUpload,
	onProgress func(bytes int64, sentFiles int),
	onReady func(source FinalUploadFile, copies []FinalUploadFile, sent bool),
) error {
	if len(members) < 2 {
		return errors.New("upload pack requires at least two members")
	}
	packURL, err := packUploadURL(members[0].upload.URL)
	if err != nil {
		return err
	}
	var payload bytes.Buffer
	entries := make([]map[string]any, len(members))
	for index, member := range members {
		_, _ = payload.Write(member.data)
		entry := map[string]any{
			"intentId":     member.upload.IntentID,
			"token":        member.upload.Form.Token,
			"sha256":       member.upload.Form.SHA256,
			"payloadBytes": member.payloadBytes,
		}
		if member.compression != "" {
			entry["compAlg"] = member.compression
		}
		entries[index] = entry
	}
	manifest, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		return err
	}

	for attempt := 0; attempt <= uploadRetryLimit; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var uploadedPayload int64
		var reportedLogical int64
		var reportedSent int
		withdraw := func() {
			if (reportedLogical > 0 || reportedSent > 0) && onProgress != nil {
				onProgress(-reportedLogical, -reportedSent)
			}
		}
		result, sendErr := d.sendMultipart(ctx, packURL, http.MethodPost, multipartUpload{
			fields:    [][2]string{{"manifest", string(manifest)}},
			file:      bytes.NewReader(payload.Bytes()),
			fileSize:  int64(payload.Len()),
			filename:  "pack.bin",
			fieldName: "pack",
			onProgress: func(uploaded int64) {
				uploadedPayload += uploaded
				target := logicalBytesForPackProgress(members, uploadedPayload)
				sent := sentPackMembers(members, uploadedPayload)
				if onProgress != nil && (target != reportedLogical || sent != reportedSent) {
					onProgress(target-reportedLogical, sent-reportedSent)
				}
				reportedLogical = target
				reportedSent = sent
			},
		})
		if sendErr != nil {
			withdraw()
			if ctx.Err() != nil || attempt == uploadRetryLimit {
				return sendErr
			}
			if sleepErr := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); sleepErr != nil {
				return errors.Join(sendErr, sleepErr)
			}
			continue
		}
		if result.status >= 200 && result.status < 300 {
			packResults, parseErr := decodeIntentPackResults(result.payload)
			if parseErr != nil {
				withdraw()
				return parseErr
			}
			failures := make([]error, 0)
			for index, member := range members {
				packResult, found := uniqueIntentPackResult(packResults, member.upload.IntentID)
				credited := creditedLogicalBytesForMember(members, index, uploadedPayload)
				sent := index < reportedSent
				withdrawMember := func() {
					if onProgress == nil {
						return
					}
					if sent {
						onProgress(-credited, -1)
					} else if credited > 0 {
						onProgress(-credited, 0)
					}
				}
				if found && packResult.Status == "completed" {
					if credited < member.logicalSize && onProgress != nil {
						onProgress(member.logicalSize-credited, 0)
					}
					if onReady != nil {
						onReady(member.source, slices.Clone(member.copies), sent)
					}
					continue
				}
				if found && (packResult.Status == "pending" || packResult.Status == "processing") {
					uploadRequired, waitErr := d.waitUploadIntent(ctx, member.upload, member.recoverable)
					if waitErr == nil && !uploadRequired {
						if credited < member.logicalSize && onProgress != nil {
							onProgress(member.logicalSize-credited, 0)
						}
						if onReady != nil {
							onReady(member.source, slices.Clone(member.copies), sent)
						}
						continue
					}
					withdrawMember()
					if waitErr != nil {
						failures = append(failures, newPackMemberError(member, waitErr))
						continue
					}
					if err := d.uploadPreparedDirect(
						ctx,
						member.upload,
						member.source,
						member.data,
						member.compression,
						member.recoverable,
						func(bytes int64) {
							if onProgress != nil {
								onProgress(bytes, 0)
							}
						},
					); err != nil {
						failures = append(failures, newPackMemberError(member, err))
						continue
					}
					if onReady != nil {
						onReady(member.source, slices.Clone(member.copies), false)
					}
					continue
				}
				withdrawMember()
				reason := "pack_result_missing"
				if found {
					reason = packResult.Reason
					if reason == "" {
						reason = packResult.Status
					}
				}
				failures = append(failures, newPackMemberError(member, &UploadV2Error{Code: reason, Message: reason}))
			}
			return errors.Join(failures...)
		}
		withdraw()
		// A failure of the whole pack names no member, so no recovery pass replans it.
		if !retryableUploadResult(result, false) || attempt == uploadRetryLimit {
			return uploadResultError(result)
		}
		if err := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); err != nil {
			return err
		}
	}
	return errors.New("pack upload exhausted retries")
}

func decodeIntentPackResults(payload map[string]any) ([]intentPackResult, error) {
	if payload == nil {
		return nil, errors.New("pack_result_missing")
	}
	raw, ok := payload["results"]
	if !ok {
		return nil, errors.New("pack_result_missing")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var results []intentPackResult
	if err := json.Unmarshal(encoded, &results); err != nil {
		return nil, fmt.Errorf("decode pack results: %w", err)
	}
	return results, nil
}

func uniqueIntentPackResult(results []intentPackResult, intentID string) (intentPackResult, bool) {
	var found intentPackResult
	count := 0
	for _, result := range results {
		if result.IntentID == intentID {
			found = result
			count++
		}
	}
	return found, count == 1
}
