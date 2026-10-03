//go:build windows

package xxmi

import (
	"context"
	"strings"
)

// snapshotProcessNames enumerates all games in one snapshot, including games started by another launcher.
func snapshotProcessNames(ctx context.Context) (map[string]bool, error) {
	processes := map[string]bool{}
	err := forEachProcess(ctx, func(name string, _ int) (bool, error) {
		processes[strings.ToLower(name)] = true
		return false, ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return processes, nil
}
