package tools

import (
	"context"

	modbisect "nahida.live/desktop/internal/tools/mod_bisect"
)

type (
	BisectStatus   = modbisect.BisectStatus
	BisectSnapshot = modbisect.BisectSnapshot
)

func (t *Tools) BisectGetState() *BisectSnapshot {
	return t.bisect.BisectGetState()
}

func (t *Tools) BisectValidateExcludePath(ctx context.Context, game, inputPath string) (string, error) {
	return t.bisect.BisectValidateExcludePath(ctx, game, inputPath)
}

func (t *Tools) BisectStart(ctx context.Context, game string, excludePaths []string) (BisectSnapshot, error) {
	return t.bisect.BisectStart(ctx, game, excludePaths)
}

func (t *Tools) BisectRespond(ctx context.Context, fixed bool) (BisectSnapshot, error) {
	return t.bisect.BisectRespond(ctx, fixed)
}

func (t *Tools) BisectUndoLastRound(ctx context.Context) (BisectSnapshot, error) {
	return t.bisect.BisectUndoLastRound(ctx)
}

func (t *Tools) BisectFinalize(ctx context.Context, keepDisabled []string) (BisectSnapshot, error) {
	return t.bisect.BisectFinalize(ctx, keepDisabled)
}

func (t *Tools) BisectCancel(ctx context.Context) (BisectSnapshot, error) {
	return t.bisect.BisectCancel(ctx)
}

func (t *Tools) BisectRecover(ctx context.Context, game string) (int, error) {
	return t.bisect.BisectRecover(ctx, game)
}

// RecoverBisects restores interrupted sessions for every configured non-NTE game.
//
//wails:ignore
func (t *Tools) RecoverBisects(ctx context.Context) error {
	return t.bisect.RecoverBisects(ctx)
}

func (t *Tools) shutdownBisect() error {
	if t == nil || t.bisect == nil {
		return nil
	}
	return t.bisect.Shutdown()
}
