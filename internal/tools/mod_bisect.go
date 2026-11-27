package tools

import (
	"context"

	modbisect "nahida.live/desktop/internal/tools/mod_bisect"
)

type (
	BisectStatus               = modbisect.BisectStatus
	BisectSnapshot             = modbisect.BisectSnapshot
	BisectRecoverConflictError = modbisect.BisectRecoverConflictError
)

var (
	ErrBisectOutOfSync    = modbisect.ErrBisectOutOfSync
	ErrBisectStateChanged = modbisect.ErrBisectStateChanged
)

func (t *Tools) BisectGetState() *BisectSnapshot {
	return t.bisect.BisectGetState()
}

func (t *Tools) BisectGameIDByName(ctx context.Context, name string) (string, error) {
	return t.bisect.BisectGameIDByName(ctx, name)
}

func (t *Tools) BisectValidateExcludePath(ctx context.Context, gameID, inputPath string) (string, error) {
	return t.bisect.BisectValidateExcludePath(ctx, gameID, inputPath)
}

func (t *Tools) BisectStart(ctx context.Context, gameID string, excludePaths []string) (BisectSnapshot, error) {
	return t.bisect.BisectStart(ctx, gameID, excludePaths)
}

func (t *Tools) BisectRespond(ctx context.Context, version uint64, fixed bool) (BisectSnapshot, error) {
	return t.bisect.BisectRespond(ctx, version, fixed)
}

func (t *Tools) BisectUndoLastRound(ctx context.Context, version uint64) (BisectSnapshot, error) {
	return t.bisect.BisectUndoLastRound(ctx, version)
}

func (t *Tools) BisectFinalize(ctx context.Context, version uint64, keepDisabled []string) (BisectSnapshot, error) {
	return t.bisect.BisectFinalize(ctx, version, keepDisabled)
}

func (t *Tools) BisectCancel(ctx context.Context) (BisectSnapshot, error) {
	return t.bisect.BisectCancel(ctx)
}

func (t *Tools) BisectRecover(ctx context.Context, gameID string) (int, error) {
	return t.bisect.BisectRecover(ctx, gameID)
}

// RecoverBisects restores interrupted sessions for every configured non-NTE game.
func (t *Tools) RecoverBisects(ctx context.Context) error {
	return t.bisect.RecoverBisects(ctx)
}

func (t *Tools) shutdownBisect() error {
	return t.bisect.Shutdown()
}
