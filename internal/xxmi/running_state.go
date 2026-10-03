package xxmi

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/infra"
)

const (
	runningWatchInterval    = time.Second
	runningIdleInterval     = 30 * time.Second
	runningErrorLogInterval = time.Minute
)

// ServiceStartup starts one app-wide watcher; renderer pages only subscribe to changes.
//
//wails:ignore
func (x *XXMI) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	x.runningWatchMu.Lock()
	defer x.runningWatchMu.Unlock()
	if x.runningCancel != nil {
		return nil
	}
	ctx, x.runningCancel = context.WithCancel(ctx)
	x.runningDone = make(chan struct{})
	done := x.runningDone
	go func() {
		defer close(done)
		x.watchRunning(ctx, x.builtinRunningState)
	}()
	return nil
}

// ServiceShutdown joins the watcher before the settings store is closed.
//
//wails:ignore
func (x *XXMI) ServiceShutdown() error {
	x.runningWatchMu.Lock()
	defer x.runningWatchMu.Unlock()
	if x.runningCancel == nil {
		return nil
	}
	x.runningCancel()
	<-x.runningDone
	x.runningCancel = nil
	x.runningDone = nil
	return nil
}

func (x *XXMI) wakeRunningWatch() {
	select {
	case x.runningWake <- struct{}{}:
	default:
	}
}

// setLaunching marks an importer as starting, so it reads as running before its game process exists.
func (x *XXMI) setLaunching(key string, launching bool) {
	x.mu.Lock()
	if launching {
		if x.launching == nil {
			x.launching = make(map[string]bool)
		}
		x.launching[key] = true
	} else {
		delete(x.launching, key)
	}
	x.mu.Unlock()
	x.wakeRunningWatch()
}

func (x *XXMI) watchRunning(ctx context.Context, read func(context.Context) (map[string]bool, error)) {
	previous := map[string]bool{}
	var reported time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		current, err := read(ctx)
		if ctx.Err() != nil {
			return
		}
		interval := runningWatchInterval
		if err != nil {
			// Keep the last known state on failure, and report a persistent failure only once per interval.
			if reported.IsZero() || time.Since(reported) >= runningErrorLogInterval {
				reported = time.Now()
				_ = infra.ReportError(x.log, err, "XXMI.RunningState", infra.Diagnostic{
					Severity: infra.DiagnosticWarn, Operation: "watch-running-games", Stage: "refresh",
					Fields: map[string]any{"last_known": previous},
				})
			}
		} else {
			if !maps.Equal(previous, current) {
				previous = current
				if x.eventEmit != nil {
					x.eventEmit("xxmi:running-changed", current)
				}
			}

			// Nothing is watched in external launcher mode or without enabled importers. Mode switches and
			// config saves wake the watcher, so the slow tick only covers importer rows written elsewhere.
			if len(current) == 0 {
				interval = runningIdleInterval
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-x.runningWake:
		case <-time.After(interval):
		}
	}
}

func (x *XXMI) builtinRunningState(ctx context.Context) (map[string]bool, error) {
	client, err := x.settingsClient()
	if err != nil {
		return nil, err
	}
	mode, err := launcherMode(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("read launcher mode: %w", err)
	}
	if mode != LauncherBuiltin {
		return map[string]bool{}, nil
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list watched importers: %w", err)
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		var cfg struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			return nil, fmt.Errorf("read watched importer %s: %w", row.Key, err)
		}
		if cfg.Enabled {
			keys = append(keys, row.Key)
		}
	}
	return x.runningImporters(ctx, keys)
}

func (x *XXMI) runningImporters(ctx context.Context, keys []string) (map[string]bool, error) {
	running := make(map[string]bool, len(keys))
	if len(keys) == 0 {
		return running, nil
	}
	// The snapshot adds nothing while every importer is being launched.
	if x.markLaunching(running, keys) == len(keys) {
		return running, nil
	}

	processes, err := x.processSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot game processes: %w", err)
	}

	// Read the launches again: one may have started while the snapshot was taken.
	x.markLaunching(running, keys)
	for _, key := range keys {
		if running[key] {
			continue
		}
		spec, _ := lookupImporterPackage(key)
		running[key] = slices.ContainsFunc(slices.Concat(spec.gameExeNames, spec.processNames),
			func(name string) bool { return processes[strings.ToLower(name)] })
	}
	return running, nil
}

// markLaunching records which keys are being launched and returns how many are.
func (x *XXMI) markLaunching(running map[string]bool, keys []string) int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	count := 0
	for _, key := range keys {
		running[key] = x.launching[strings.ToUpper(strings.TrimSpace(key))]
		if running[key] {
			count++
		}
	}
	return count
}
