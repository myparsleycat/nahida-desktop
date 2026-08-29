package drive

import (
	"errors"
	"fmt"
)

const DeletionBatchSize = 500

type DeletionAccepted struct {
	Kind             string `json:"kind"`
	DeletionJobID    string `json:"deletionJobId"`
	Status           string `json:"status"`
	DeletionJobToken string `json:"deletionJobToken,omitempty"`
}

type DeletionCompleted struct {
	Kind         string `json:"kind"`
	DeletedCount int    `json:"deletedCount"`
}

type DeletionResult struct {
	Accepted  *DeletionAccepted
	Completed *DeletionCompleted
}

type BatchDeletionOutcome struct {
	RequestedIDs []string           `json:"requestedIds"`
	AcceptedIDs  []string           `json:"acceptedIds"`
	Jobs         []DeletionAccepted `json:"jobs"`
	ErrorMessage string             `json:"errorMessage,omitempty"`
}

type deletionRequester func(page []string) (*DeletionAccepted, error)

func runDeletionBatches(ids []string, request deletionRequester, batchSize int) BatchDeletionOutcome {
	if batchSize <= 0 {
		batchSize = DeletionBatchSize
	}
	requested := uniqueStrings(ids)
	if len(requested) == 0 {
		return BatchDeletionOutcome{RequestedIDs: requested, AcceptedIDs: []string{}, Jobs: []DeletionAccepted{}}
	}

	accepted := make([]string, 0, len(requested))
	jobs := make([]DeletionAccepted, 0)
	for _, page := range chunkStrings(requested, batchSize) {
		job, err := request(page)
		if err != nil {
			return BatchDeletionOutcome{
				RequestedIDs: requested,
				AcceptedIDs:  accepted,
				Jobs:         jobs,
				ErrorMessage: err.Error(),
			}
		}
		if job != nil {
			jobs = append(jobs, *job)
		}
		accepted = append(accepted, page...)
	}
	return BatchDeletionOutcome{RequestedIDs: requested, AcceptedIDs: accepted, Jobs: jobs}
}

func requireBatchAccepted(outcome BatchDeletionOutcome) (BatchDeletionOutcome, error) {
	if len(outcome.AcceptedIDs) == 0 {
		msg := outcome.ErrorMessage
		if msg == "" {
			msg = "delete_failed"
		}
		return outcome, errors.New(msg)
	}
	return outcome, nil
}

type edenError struct {
	Status int
	Value  any
}

func resolveDeletionResult(data any, err *edenError) (DeletionResult, error) {
	if err != nil {
		if err.Status == 202 {
			if accepted := asDeletionAccepted(err.Value); accepted != nil {
				return DeletionResult{Accepted: accepted}, nil
			}
			if completed := asDeletionCompleted(err.Value); completed != nil {
				return DeletionResult{Completed: completed}, nil
			}
		}
		return DeletionResult{}, errors.New(toErrorMessage(err.Value))
	}

	if accepted := asDeletionAccepted(data); accepted != nil {
		return DeletionResult{Accepted: accepted}, nil
	}
	if completed := asDeletionCompleted(data); completed != nil {
		return DeletionResult{Completed: completed}, nil
	}
	return DeletionResult{}, errors.New("unexpected_deletion_response")
}

func requireAccepted(result DeletionResult) (*DeletionAccepted, error) {
	if result.Accepted != nil {
		return result.Accepted, nil
	}
	if result.Completed != nil {
		return nil, nil
	}
	return nil, errors.New("unexpected_deletion_response")
}

func asDeletionAccepted(value any) *DeletionAccepted {
	record, ok := asRecord(value)
	if !ok {
		return nil
	}
	jobID, _ := record["deletionJobId"].(string)
	if jobID == "" {
		return nil
	}
	status, _ := record["status"].(string)
	if status != "pending" {
		return nil
	}
	out := &DeletionAccepted{
		Kind:          "accepted",
		DeletionJobID: jobID,
		Status:        "pending",
	}
	if token, ok := record["deletionJobToken"].(string); ok {
		out.DeletionJobToken = token
	}
	return out
}

func asDeletionCompleted(value any) *DeletionCompleted {
	record, ok := asRecord(value)
	if !ok {
		return nil
	}
	status, _ := record["status"].(string)
	if status != "completed" {
		return nil
	}
	count := 0
	if n, ok := asInt(record["deletedCount"]); ok {
		count = n
	}
	return &DeletionCompleted{Kind: "completed", DeletedCount: count}
}

func uniqueStrings(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func chunkStrings(ids []string, size int) [][]string {
	if size <= 0 {
		return [][]string{ids}
	}
	var pages [][]string
	for i := 0; i < len(ids); i += size {
		end := i + size
		if end > len(ids) {
			end = len(ids)
		}
		pages = append(pages, ids[i:end])
	}
	return pages
}

func formatPartialDeleteLog(outcome BatchDeletionOutcome, action string) map[string]any {
	return map[string]any{
		"channel":     "drive:delete:items",
		"action":      action,
		"stage":       "delete-items-partial",
		"ids":         outcome.RequestedIDs,
		"acceptedIds": outcome.AcceptedIDs,
		"failedCount": len(outcome.RequestedIDs) - len(outcome.AcceptedIDs),
		"jobCount":    len(outcome.Jobs),
		"error":       outcome.ErrorMessage,
	}
}

func invalidActionError() error {
	return fmt.Errorf("INVALID_ACTION")
}
