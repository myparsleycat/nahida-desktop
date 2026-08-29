package drive

import (
	"errors"
	"fmt"
	"testing"
)

func TestResolveDeletionResultAcceptedFromData(t *testing.T) {
	t.Parallel()

	got, err := resolveDeletionResult(map[string]any{"deletionJobId": "job-1", "status": "pending"}, nil)
	if err != nil {
		t.Fatalf("resolveDeletionResult: %v", err)
	}
	if got.Accepted == nil || got.Accepted.DeletionJobID != "job-1" || got.Accepted.Status != "pending" || got.Accepted.DeletionJobToken != "" {
		t.Fatalf("accepted = %+v", got.Accepted)
	}
}

func TestResolveDeletionResultAcceptedFromEdenErrorChannel(t *testing.T) {
	t.Parallel()

	got, err := resolveDeletionResult(nil, &edenError{
		Status: 202,
		Value: map[string]any{
			"deletionJobId":    "job-2",
			"status":           "pending",
			"deletionJobToken": "token",
		},
	})
	if err != nil {
		t.Fatalf("resolveDeletionResult: %v", err)
	}
	if got.Accepted == nil || got.Accepted.DeletionJobID != "job-2" || got.Accepted.DeletionJobToken != "token" {
		t.Fatalf("accepted = %+v", got.Accepted)
	}
}

func TestResolveDeletionResultCompletedPayload(t *testing.T) {
	t.Parallel()

	got, err := resolveDeletionResult(map[string]any{"status": "completed", "deletedCount": 0}, nil)
	if err != nil {
		t.Fatalf("resolveDeletionResult: %v", err)
	}
	if got.Completed == nil || got.Completed.DeletedCount != 0 {
		t.Fatalf("completed = %+v", got.Completed)
	}
}

func TestResolveDeletionResultThrowsNon202(t *testing.T) {
	t.Parallel()

	_, err := resolveDeletionResult(nil, &edenError{Status: 404, Value: "items_not_found"})
	if err == nil || err.Error() != "items_not_found" {
		t.Fatalf("err = %v", err)
	}
}

func TestRunDeletionBatchesChunksRequests(t *testing.T) {
	t.Parallel()

	var pages [][]string
	ids := make([]string, DeletionBatchSize+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%d", i)
	}
	outcome := runDeletionBatches(ids, func(page []string) (*DeletionAccepted, error) {
		copied := append([]string(nil), page...)
		pages = append(pages, copied)
		return &DeletionAccepted{Kind: "accepted", DeletionJobID: "job-" + page[0], Status: "pending"}, nil
	}, DeletionBatchSize)

	if len(pages) != 2 {
		t.Fatalf("pages = %d", len(pages))
	}
	if len(pages[0]) != DeletionBatchSize || len(pages[1]) != 1 {
		t.Fatalf("page sizes = %d, %d", len(pages[0]), len(pages[1]))
	}
	if len(outcome.AcceptedIDs) != len(ids) {
		t.Fatalf("accepted = %d", len(outcome.AcceptedIDs))
	}
	for i, id := range ids {
		if outcome.AcceptedIDs[i] != id {
			t.Fatalf("accepted[%d] = %q", i, outcome.AcceptedIDs[i])
		}
	}
	if outcome.ErrorMessage != "" {
		t.Fatalf("errorMessage = %q", outcome.ErrorMessage)
	}
}

func TestRunDeletionBatchesKeepsEarlierAccepted(t *testing.T) {
	t.Parallel()

	outcome := runDeletionBatches([]string{"a1", "b1"}, func(page []string) (*DeletionAccepted, error) {
		if page[0] == "b1" {
			return nil, errors.New("second_batch_failed")
		}
		return &DeletionAccepted{Kind: "accepted", DeletionJobID: "job-a", Status: "pending"}, nil
	}, 1)

	if len(outcome.AcceptedIDs) != 1 || outcome.AcceptedIDs[0] != "a1" {
		t.Fatalf("accepted = %v", outcome.AcceptedIDs)
	}
	if len(outcome.Jobs) != 1 {
		t.Fatalf("jobs = %d", len(outcome.Jobs))
	}
	if outcome.ErrorMessage != "second_batch_failed" {
		t.Fatalf("errorMessage = %q", outcome.ErrorMessage)
	}
	if _, err := requireBatchAccepted(outcome); err != nil {
		t.Fatalf("requireBatchAccepted: %v", err)
	}
}

func TestRequireBatchAcceptedNothingAccepted(t *testing.T) {
	t.Parallel()

	outcome := runDeletionBatches([]string{"a"}, func([]string) (*DeletionAccepted, error) {
		return nil, errors.New("boom")
	}, DeletionBatchSize)
	if len(outcome.AcceptedIDs) != 0 {
		t.Fatalf("accepted = %v", outcome.AcceptedIDs)
	}
	_, err := requireBatchAccepted(outcome)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}
}

func TestRunDeletionBatchesTreatsNilJobAsAccepted(t *testing.T) {
	t.Parallel()

	outcome := runDeletionBatches([]string{"a1", "a2"}, func([]string) (*DeletionAccepted, error) {
		return nil, nil
	}, DeletionBatchSize)
	if len(outcome.AcceptedIDs) != 2 || outcome.AcceptedIDs[0] != "a1" || outcome.AcceptedIDs[1] != "a2" {
		t.Fatalf("accepted = %v", outcome.AcceptedIDs)
	}
	if len(outcome.Jobs) != 0 {
		t.Fatalf("jobs = %v", outcome.Jobs)
	}
	if outcome.ErrorMessage != "" {
		t.Fatalf("errorMessage = %q", outcome.ErrorMessage)
	}
}
