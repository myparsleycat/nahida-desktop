package tools

import (
	"context"

	wuwafixer "nahida.live/desktop/internal/tools/wuwa_fixer"
)

type (
	GitHubRateState        = wuwafixer.GitHubRateState
	WuwaFixerOptions       = wuwafixer.WuwaFixerOptions
	WuwaBackupFile         = wuwafixer.WuwaBackupFile
	WuwaBackupGroup        = wuwafixer.WuwaBackupGroup
	WuwaBackupSize         = wuwafixer.WuwaBackupSize
	WuwaFixerStatus        = wuwafixer.WuwaFixerStatus
	WuwaFixerPrepareResult = wuwafixer.WuwaFixerPrepareResult
)

func (t *Tools) WuwaFixerGetRateStatus(ctx context.Context) (*GitHubRateState, error) {
	return t.wuwa.WuwaFixerGetRateStatus(ctx)
}

func (t *Tools) WuwaFixerGetStatus(ctx context.Context, importer *string) (WuwaFixerStatus, error) {
	return t.wuwa.WuwaFixerGetStatus(ctx, importer)
}

func (t *Tools) WuwaFixerPrepareRun(ctx context.Context, importer *string) (WuwaFixerPrepareResult, error) {
	return t.wuwa.WuwaFixerPrepareRun(ctx, importer)
}

func (t *Tools) WuwaFixerInstallOrUpdate(ctx context.Context) (WuwaFixerStatus, error) {
	return t.wuwa.WuwaFixerInstallOrUpdate(ctx)
}

func (t *Tools) WuwaFixerRun(ctx context.Context, modPath string, options WuwaFixerOptions) error {
	return t.wuwa.WuwaFixerRun(ctx, modPath, options)
}

func (t *Tools) WuwaFixerScanBackups(ctx context.Context, modPath string) ([]WuwaBackupGroup, error) {
	return t.wuwa.WuwaFixerScanBackups(ctx, modPath)
}

func (t *Tools) WuwaFixerGetBackupSize(ctx context.Context, modPath string) (WuwaBackupSize, error) {
	return t.wuwa.WuwaFixerGetBackupSize(ctx, modPath)
}

func (t *Tools) WuwaFixerRollbackToGroup(ctx context.Context, modPath, groupKey string) error {
	return t.wuwa.WuwaFixerRollbackToGroup(ctx, modPath, groupKey)
}

func (t *Tools) WuwaFixerCleanBackups(ctx context.Context, modPath string) error {
	return t.wuwa.WuwaFixerCleanBackups(ctx, modPath)
}

//wails:ignore
func (t *Tools) StartWuwaAutoUpdateCheck() {
	t.wuwa.StartWuwaAutoUpdateCheck()
}

func (t *Tools) stopWuwaAutoUpdateCheck() error {
	if t == nil || t.wuwa == nil {
		return nil
	}
	return t.wuwa.Shutdown()
}
