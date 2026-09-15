package tools

import (
	"context"

	zzmifixer "nahida.live/desktop/internal/tools/zzmi_fixer"
)

type (
	ZZMIFixerRuleStatus        = zzmifixer.ZZMIFixerRuleStatus
	ZZMIFixerPrepareResult     = zzmifixer.ZZMIFixerPrepareResult
	ZZMIFixerRunInput          = zzmifixer.ZZMIFixerRunInput
	ZZMIFixerRunResult         = zzmifixer.ZZMIFixerRunResult
	ZZMIBackupEntry            = zzmifixer.ZZMIBackupEntry
	ZZMIBackupSession          = zzmifixer.ZZMIBackupSession
	ZZMIFixerRestoreInput      = zzmifixer.ZZMIFixerRestoreInput
	ZZMIFixerRestoreConflict   = zzmifixer.ZZMIFixerRestoreConflict
	ZZMIFixerRestoreResult     = zzmifixer.ZZMIFixerRestoreResult
	ZZMIFixerDeleteBackupInput = zzmifixer.ZZMIFixerDeleteBackupInput
)

func (t *Tools) ZZMIFixerPrepare(
	ctx context.Context,
	targetPath string,
	forceRefresh bool,
) (ZZMIFixerPrepareResult, error) {
	return t.zzmi.ZZMIFixerPrepare(ctx, targetPath, forceRefresh)
}

func (t *Tools) ZZMIFixerActivateLatestRules(ctx context.Context) (ZZMIFixerRuleStatus, error) {
	return t.zzmi.ZZMIFixerActivateLatestRules(ctx)
}

func (t *Tools) ZZMIFixerRun(ctx context.Context, input ZZMIFixerRunInput) (ZZMIFixerRunResult, error) {
	return t.zzmi.ZZMIFixerRun(ctx, input)
}

func (t *Tools) ZZMIFixerListBackups(ctx context.Context, targetPath string) ([]ZZMIBackupSession, error) {
	return t.zzmi.ZZMIFixerListBackups(ctx, targetPath)
}

func (t *Tools) ZZMIFixerGetBackup(ctx context.Context, targetPath, sessionID string) (ZZMIBackupSession, error) {
	return t.zzmi.ZZMIFixerGetBackup(ctx, targetPath, sessionID)
}

func (t *Tools) ZZMIFixerRestore(
	ctx context.Context,
	input ZZMIFixerRestoreInput,
) (ZZMIFixerRestoreResult, error) {
	return t.zzmi.ZZMIFixerRestore(ctx, input)
}

func (t *Tools) ZZMIFixerDeleteBackup(ctx context.Context, input ZZMIFixerDeleteBackupInput) error {
	return t.zzmi.ZZMIFixerDeleteBackup(ctx, input)
}

func (t *Tools) ZZMIFixerDeleteAllBackups(ctx context.Context, targetPath string) error {
	return t.zzmi.ZZMIFixerDeleteAllBackups(ctx, targetPath)
}

//wails:ignore
func (t *Tools) CleanupZZMIAbandonedStaging(ctx context.Context) error {
	return t.zzmi.CleanupZZMIAbandonedStaging(ctx)
}
