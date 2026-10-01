package drive

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/google/uuid"
)

const storageTemporaryFailure = "storage_temporarily_unavailable"

const uploadRecoveryLimit = 2

// executeUploadPlanWithRecovery replans only files whose storage failed
// transiently. Completed files keep their credits and permanent failures remain
// visible even when other files recover. Aborted bundles require a user restart.
func (d *Drive) executeUploadPlanWithRecovery(
	ctx context.Context,
	destinationID string,
	files []FinalUploadFile,
	plan UploadPlan,
	rules UploadRules,
	concurrency int,
	onProgress func(UploadExecutionProgress),
) (map[string]string, error) {
	rejections := make(map[string]string)
	var terminal []error
	for attempt := 0; ; attempt++ {
		refused, err := d.executeUploadPlanV2(ctx, files, plan, rules, concurrency, onProgress)
		for id, reason := range refused {
			rejections[id] = reason
		}
		if err == nil {
			return rejections, errors.Join(terminal...)
		}
		if ctx.Err() != nil || attempt == uploadRecoveryLimit {
			return rejections, errors.Join(append(terminal, err)...)
		}

		retryIDs, permanent := splitUploadRecovery(err, plan)
		terminal = append(terminal, permanent...)
		files = slices.DeleteFunc(slices.Clone(files), func(file FinalUploadFile) bool {
			return !retryIDs[file.FID]
		})
		if len(files) == 0 {
			return rejections, errors.Join(terminal...)
		}

		delay := retryDelay(attempt+2, 16*time.Second) + time.Duration(rand.Int64N(int64(time.Second)))
		if waitErr := d.sleep(ctx, delay); waitErr != nil {
			return rejections, errors.Join(append(terminal, err, waitErr)...)
		}
		var planErr error
		plan, planErr = d.planUploadV2(ctx, destinationID, uuid.NewString(), files, rules, nil)
		if planErr != nil {
			return rejections, errors.Join(append(terminal, planErr)...)
		}
		concurrency = max(1, min(4, concurrency/2))
	}
}

func splitUploadRecovery(failure error, plan UploadPlan) (map[string]bool, []error) {
	bundled := make(map[string]bool)
	for _, bundle := range plan.Bundles {
		for _, id := range bundle.MemberClientIDs {
			bundled[id] = true
		}
	}
	for _, item := range plan.Items {
		if item.BundleID != "" {
			bundled[item.ClientID] = true
		}
	}
	retryIDs := make(map[string]bool)
	var permanent []error
	var visit func(error)
	visit = func(err error) {
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, child := range joined.Unwrap() {
				visit(child)
			}
			return
		}
		var member *packMemberError
		var uploadErr *UploadV2Error
		if !errors.As(err, &member) || !errors.As(err, &uploadErr) || uploadErr.Code != storageTemporaryFailure {
			permanent = append(permanent, err)
			return
		}
		if len(member.files) == 0 ||
			slices.ContainsFunc(member.files, func(file FinalUploadFile) bool { return bundled[file.FID] }) {
			permanent = append(permanent, err)
			return
		}
		for _, file := range member.files {
			retryIDs[file.FID] = true
		}
	}
	visit(failure)
	return retryIDs, permanent
}
