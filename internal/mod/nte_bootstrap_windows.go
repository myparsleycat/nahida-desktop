//go:build windows

package mod

import (
	"context"
	"fmt"
	"slices"

	"nahida.live/desktop/internal/elevated"
)

func elevatedCopyNteBootstrapFiles(
	ctx context.Context,
	lease *elevated.FileLease,
	copies []nteBootstrapFileCopy,
) error {
	ops := make([]elevated.FileOp, 0, len(copies))
	for _, file := range copies {
		op, err := elevated.NewCopyOp(file.sourcePath, file.targetPath)
		if err != nil {
			return fmt.Errorf("NTE_BOOTSTRAP_ELEVATED_COPY_FAILED: %w", err)
		}
		ops = append(ops, op)
	}
	if err := lease.Apply(ctx, ops); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_ELEVATED_COPY_FAILED: %w", err)
	}
	return nil
}

func elevatedRollbackNteBootstrapFiles(
	ctx context.Context,
	lease *elevated.FileLease,
	snapshots []nteBootstrapSnapshot,
) error {
	ops := make([]elevated.FileOp, 0, len(snapshots))
	for _, snapshot := range slices.Backward(snapshots) {
		if !snapshot.existed {
			ops = append(ops, elevated.FileOp{Kind: elevated.FileOpRemove, Target: snapshot.targetPath})
			continue
		}
		op, err := elevated.NewCopyOp(snapshot.backupPath, snapshot.targetPath)
		if err != nil {
			return fmt.Errorf("NTE_BOOTSTRAP_ELEVATED_ROLLBACK_FAILED: %w", err)
		}
		ops = append(ops, op)
	}
	if err := lease.Apply(ctx, ops); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_ELEVATED_ROLLBACK_FAILED: %w", err)
	}
	return nil
}
