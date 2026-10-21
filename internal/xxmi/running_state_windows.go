//go:build windows

package xxmi

import (
	"context"
	"maps"
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

// GameProcesses returns the importer of every running game process, keyed by PID. Image names come from
// each importer's launcher settings, and extra adds importers XXMI does not own, such as NTE. It reads the
// process snapshot instead of opening each process, which anti-cheat drivers of some games deny.
//
//wails:ignore
func (x *XXMI) GameProcesses(ctx context.Context, extra map[string][]string) (map[uint32]string, error) {
	importers, err := x.gameProcessNames(ctx)
	if err != nil {
		return nil, err
	}
	maps.Copy(importers, extra)
	byName := map[string]string{}
	for key, names := range importers {
		for _, name := range names {
			byName[strings.ToLower(strings.TrimSpace(name))] = key
		}
	}

	games := map[uint32]string{}
	err = forEachProcess(ctx, func(name string, pid int) (bool, error) {
		if key, ok := byName[strings.ToLower(name)]; ok {
			games[uint32(pid)] = key
		}
		return false, ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return games, nil
}
