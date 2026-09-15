package tools

import (
	"context"

	touchprofile "nahida.live/desktop/internal/tools/touch_profile"
)

type (
	TouchProfileLoadInput                    = touchprofile.TouchProfileLoadInput
	TouchProfilePreviewInput                 = touchprofile.TouchProfilePreviewInput
	TouchProfileAnalyzeInput                 = touchprofile.TouchProfileAnalyzeInput
	TouchProfileUpdateZoneSettingsBatchInput = touchprofile.TouchProfileUpdateZoneSettingsBatchInput
	TouchProfileUpdateResult                 = touchprofile.TouchProfileUpdateResult
	TouchProfileApplyInput                   = touchprofile.TouchProfileApplyInput
	TouchProfileRollbackInput                = touchprofile.TouchProfileRollbackInput
	TouchProfileOK                           = touchprofile.TouchProfileOK
	TouchModInspection                       = touchprofile.TouchModInspection
	TouchMeshDescriptor                      = touchprofile.TouchMeshDescriptor
	TouchDraft                               = touchprofile.TouchDraft
	TouchProfilePreviewDescriptor            = touchprofile.TouchProfilePreviewDescriptor
	TouchApplyResult                         = touchprofile.TouchApplyResult
	TouchRollbackResult                      = touchprofile.TouchRollbackResult
)

func (t *Tools) TouchProfilePrepare(
	ctx context.Context,
	input TouchProfileLoadInput,
) (TouchModInspection, error) {
	return t.touchProfile.TouchProfilePrepare(ctx, input)
}

func (t *Tools) TouchProfileGetMeshDescriptor(
	ctx context.Context,
	input TouchProfilePreviewInput,
) (TouchMeshDescriptor, error) {
	return t.touchProfile.TouchProfileGetMeshDescriptor(ctx, input)
}

func (t *Tools) TouchProfileAnalyzeComponents(
	ctx context.Context,
	input TouchProfileAnalyzeInput,
) (TouchDraft, error) {
	return t.touchProfile.TouchProfileAnalyzeComponents(ctx, input)
}

func (t *Tools) TouchProfileUpdateZoneSettingsBatch(
	ctx context.Context,
	input TouchProfileUpdateZoneSettingsBatchInput,
) (TouchProfileUpdateResult, error) {
	return t.touchProfile.TouchProfileUpdateZoneSettingsBatch(ctx, input)
}

func (t *Tools) TouchProfileGetPreviewDescriptor(
	ctx context.Context,
	input TouchProfilePreviewInput,
) (TouchProfilePreviewDescriptor, error) {
	return t.touchProfile.TouchProfileGetPreviewDescriptor(ctx, input)
}

func (t *Tools) TouchProfileDiscardDraft(ctx context.Context, sessionID string) (TouchProfileOK, error) {
	return t.touchProfile.TouchProfileDiscardDraft(ctx, sessionID)
}

func (t *Tools) TouchProfileCloseSession(ctx context.Context, sessionID string) (TouchProfileOK, error) {
	return t.touchProfile.TouchProfileCloseSession(ctx, sessionID)
}

func (t *Tools) TouchProfileApply(ctx context.Context, input TouchProfileApplyInput) (TouchApplyResult, error) {
	return t.touchProfile.TouchProfileApply(ctx, input)
}

func (t *Tools) TouchProfileRegenerate(ctx context.Context, input TouchProfileApplyInput) (TouchApplyResult, error) {
	return t.touchProfile.TouchProfileRegenerate(ctx, input)
}

func (t *Tools) TouchProfileRollback(
	ctx context.Context,
	input TouchProfileRollbackInput,
) (TouchRollbackResult, error) {
	return t.touchProfile.TouchProfileRollback(ctx, input)
}

func (t *Tools) shutdownTouchProfiles() error {
	if t == nil || t.touchProfile == nil {
		return nil
	}
	return t.touchProfile.Shutdown()
}
