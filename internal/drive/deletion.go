package drive

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
)

const DeletionBatchSize = 500

type DeletionAccepted struct {
	Kind             string `json:"kind"`
	DeletionJobID    string `json:"deletionJobId"`
	Status           string `json:"status"`
	DeletionJobToken string `json:"deletionJobToken,omitempty"`
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

// resolveDeletionJob returns the job the server accepted for one page of a batch
// delete. A nil job with a nil error means the page was already deleted, so there
// is nothing left to schedule.
func resolveDeletionJob(data any, err *edenError) (*DeletionAccepted, error) {
	if err != nil {
		if err.Status == 202 {
			if accepted := asDeletionAccepted(err.Value); accepted != nil {
				return accepted, nil
			}
			if isDeletionCompleted(err.Value) {
				return nil, nil
			}
		}
		return nil, errors.New(toErrorMessage(err.Value))
	}

	if accepted := asDeletionAccepted(data); accepted != nil {
		return accepted, nil
	}
	if isDeletionCompleted(data) {
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

func isDeletionCompleted(value any) bool {
	record, ok := asRecord(value)
	if !ok {
		return false
	}
	status, _ := record["status"].(string)
	return status == "completed"
}

func uniqueStrings(ids []string) []string {
	return lo.Uniq(ids)
}

func chunkStrings(ids []string, size int) [][]string {
	if size <= 0 {
		return [][]string{ids}
	}
	return lo.Chunk(ids, size)
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
