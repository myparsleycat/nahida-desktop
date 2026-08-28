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

type intentPackResult struct {
	IntentID string `json:"intentId"`
	Status   string `json:"status"`
	FileID   string `json:"fileId,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func (d *Drive) uploadPack(
	ctx context.Context,
	members []preparedUpload,
	onProgress func(int64),
	onReady func(FinalUploadFile, []FinalUploadFile),
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
		result, sendErr := d.sendMultipartField(
			ctx,
			packURL,
			http.MethodPost,
			[][2]string{{"manifest", string(manifest)}},
			bytes.NewReader(payload.Bytes()),
			int64(payload.Len()),
			"pack.bin",
			"pack",
			func(uploaded int64) {
				uploadedPayload += uploaded
				target := logicalBytesForPackProgress(members, uploadedPayload)
				if onProgress != nil && target != reportedLogical {
					onProgress(target - reportedLogical)
				}
				reportedLogical = target
			},
		)
		if sendErr != nil {
			if reportedLogical > 0 && onProgress != nil {
				onProgress(-reportedLogical)
			}
			if ctx.Err() != nil || attempt == uploadRetryLimit {
				return sendErr
			}
			if sleepErr := d.sleep(ctx, retryDelay(attempt, 8*time.Second)); sleepErr != nil {
				return errors.Join(sendErr, sleepErr)
			}
			continue
		}
		if result.status >= 200 && result.status < 300 && result.status != http.StatusAccepted {
			packResults, parseErr := decodeIntentPackResults(result.payload)
			if parseErr != nil {
				if reportedLogical > 0 && onProgress != nil {
					onProgress(-reportedLogical)
				}
				return parseErr
			}
			failures := make([]error, 0)
			for index, member := range members {
				packResult, found := uniqueIntentPackResult(packResults, member.upload.IntentID)
				credited := creditedLogicalBytesForMember(members, index, uploadedPayload)
				if found && packResult.Status == "completed" {
					if credited < member.logicalSize && onProgress != nil {
						onProgress(member.logicalSize - credited)
					}
					if onReady != nil {
						onReady(member.source, slices.Clone(member.copies))
					}
					continue
				}
				if credited > 0 && onProgress != nil {
					onProgress(-credited)
				}
				reason := "pack_result_missing"
				if found {
					reason = packResult.Reason
					if reason == "" {
						reason = packResult.Status
					}
				}
				failures = append(failures, fmt.Errorf("%s: %s", member.source.Name, reason))
			}
			return errors.Join(failures...)
		}
		if reportedLogical > 0 && onProgress != nil {
			onProgress(-reportedLogical)
		}
		if !retryableUploadResult(result) || attempt == uploadRetryLimit {
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
