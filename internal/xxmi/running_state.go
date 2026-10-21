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

	"nahida.live/desktop/internal/db"
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
	importers := make(map[string][]string, len(rows))
	for _, row := range rows {
		cfg, err := storedImporterConfig(row)
		if err != nil {
			return nil, err
		}
		if cfg.Enabled {
			importers[row.Key] = cfg.gameProcessNames(row.Key)
		}
	}
	return x.runningImporters(ctx, importers)
}

// gameProcessNames maps each importer to the image names its game runs under, following the settings of the
// selected launcher. An importer without settings matches every executable its package knows about.
func (x *XXMI) gameProcessNames(ctx context.Context) (map[string][]string, error) {
	importers := make(map[string][]string, len(importerPackages))
	for key, spec := range importerPackages {
		importers[key] = packageProcessNames(spec)
	}
	client, err := x.settingsClient()
	if err != nil {
		return nil, err
	}
	mode, err := launcherMode(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("read launcher mode: %w", err)
	}

	if mode == LauncherExternal {
		launcher, err := x.loadExternalLauncher(ctx)
		if err != nil {
			return nil, err
		}
		if launcher == nil {
			return importers, nil
		}
		for key, importer := range launcher.parsed.Importers {
			if names := importer.Importer.processNames(); len(names) > 0 {
				importers[key] = names
			}
		}
		return importers, nil
	}

	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list importers: %w", err)
	}
	for _, row := range rows {
		cfg, err := storedImporterConfig(row)
		if err != nil {
			return nil, err
		}
		if names := cfg.gameProcessNames(row.Key); len(names) > 0 {
			importers[row.Key] = names
		}
	}
	return importers, nil
}

// storedImporterConfig reads the fields of a stored importer row that game detection needs. Schema upgrades in
// decodeImporterConfig leave them unchanged, so the row does not need the importer defaults.
func storedImporterConfig(row db.XXMIImporterRow) (ImporterConfig, error) {
	var cfg ImporterConfig
	if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
		return ImporterConfig{}, fmt.Errorf("read importer %s: %w", row.Key, err)
	}
	return cfg, nil
}

// gameProcessNames lists every image name launchProcessName can pick: the process override and the
// executables of the importer package. The pick depends on the game folder a launch resolves, which a store
// client may supply at launch time, so detection watches all of them instead of guessing the folder.
func (cfg ImporterConfig) gameProcessNames(key string) []string {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return nil
	}
	names := packageProcessNames(spec)
	override := strings.TrimSpace(cfg.GameProcessExe)
	if override != "" &&
		!slices.ContainsFunc(names, func(name string) bool { return strings.EqualFold(name, override) }) {
		names = append(names, override)
	}
	return names
}

func packageProcessNames(spec importerPackageSpec) []string {
	return slices.Concat(spec.processNames, spec.gameExeNames)
}

func (x *XXMI) runningImporters(ctx context.Context, importers map[string][]string) (map[string]bool, error) {
	keys := slices.Collect(maps.Keys(importers))
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
	for key, names := range importers {
		if running[key] {
			continue
		}
		running[key] = slices.ContainsFunc(names, func(name string) bool { return processes[strings.ToLower(name)] })
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
