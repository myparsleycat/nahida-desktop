package backup

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/watcher"
)

const (
	// scheduleTick is how often the scheduler checks whether a backup is due.
	scheduleTick = time.Minute
	// startupDelay lets the app settle before the startup catch-up runs.
	startupDelay = 30 * time.Second
	// failureBackoff is how long an automatic run waits after one that failed
	// or that the user cancelled.
	failureBackoff = time.Hour
	// watchQuiet is how long the watched folders must stay unchanged before a
	// change triggers a backup.
	watchQuiet = 10 * time.Minute
	// watchMinGap is the least time between two backups a change triggers.
	watchMinGap = time.Hour
	// watchDebounce folds a burst of file events into one.
	watchDebounce = 2 * time.Second
)

// schedule runs until ctx ends: a catch-up shortly after startup, then a check
// every minute for a due interval or a settled change.
func (b *Backup) schedule(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(startupDelay):
	}
	b.tick(ctx, true)

	ticker := time.NewTicker(scheduleTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tick(ctx, false)
		}
	}
}

// tick starts the backup that is due, if any, and keeps the folder watch in
// step with the settings and targets.
func (b *Backup) tick(ctx context.Context, startup bool) {
	client, err := b.requireClient()
	if err != nil {
		return
	}
	enabled := b.settingBool(ctx, setting.KeyBackupEnabled)
	b.syncWatch(ctx, client, enabled && b.settingBool(ctx, setting.KeyBackupWatchChanges))
	if !enabled {
		return
	}

	trigger, err := b.dueTrigger(ctx, client, startup)
	if err != nil {
		b.report(err, "schedule", "due", nil)
		return
	}
	if trigger == "" {
		return
	}
	err = b.start(ctx, trigger)
	if err != nil && !errors.Is(err, ErrRunning) && !errors.Is(err, ErrNotLoggedIn) {
		b.report(err, "schedule", "start", map[string]any{"trigger": trigger})
	}
	if err == nil && trigger == "watch" {
		b.mu.Lock()
		b.changed = time.Time{}
		b.mu.Unlock()
	}
}

// dueTrigger answers which automatic backup should run now, or "".
func (b *Backup) dueTrigger(ctx context.Context, client *db.Client, startup bool) (string, error) {
	now := b.now()
	lastSuccess, err := b.lastSuccessAt(ctx, client)
	if err != nil {
		return "", err
	}
	lastRun, err := b.lastRun(ctx, client)
	if err != nil {
		return "", err
	}
	interrupted := lastRun != nil && (lastRun.Outcome == OutcomeFailed || lastRun.Outcome == OutcomeCancelled)
	if interrupted && now.Sub(lastRun.At) < failureBackoff {
		return "", nil
	}

	b.mu.Lock()
	changed := b.changed
	b.mu.Unlock()

	interval := b.interval(ctx)
	onStartup := b.settingBool(ctx, setting.KeyBackupOnStartup)
	if startup {
		if onStartup && now.Sub(lastSuccess) >= interval {
			return "startup", nil
		}
		return "", nil
	}
	if now.Sub(b.scheduleBase(lastSuccess, onStartup)) >= interval {
		return "schedule", nil
	}
	if !changed.IsZero() && now.Sub(changed) >= watchQuiet && now.Sub(lastSuccess) >= watchMinGap {
		return "watch", nil
	}
	return "", nil
}

// scheduleBase is when the interval of the next scheduled backup starts. Without
// the startup catch-up, time the app was closed does not count, so a backup
// missed while it was closed waits a full interval after the app starts.
func (b *Backup) scheduleBase(lastSuccess time.Time, onStartup bool) time.Time {
	b.mu.Lock()
	startedAt := b.startedAt
	b.mu.Unlock()
	if !onStartup && lastSuccess.Before(startedAt) {
		return startedAt
	}
	return lastSuccess
}

// nextRunAt answers when the next scheduled backup is due, or nil when the
// automatic backup is off.
func (b *Backup) nextRunAt(ctx context.Context, client *db.Client) (*time.Time, error) {
	if !b.settingBool(ctx, setting.KeyBackupEnabled) {
		return nil, nil
	}
	lastSuccess, err := b.lastSuccessAt(ctx, client)
	if err != nil {
		return nil, err
	}
	next := b.scheduleBase(lastSuccess, b.settingBool(ctx, setting.KeyBackupOnStartup)).Add(b.interval(ctx))
	if next.Before(b.now()) {
		next = b.now()
	}
	return &next, nil
}

// syncWatch watches the included targets while change detection is on, and
// closes the watch otherwise.
func (b *Backup) syncWatch(ctx context.Context, client *db.Client, wanted bool) {
	roots := []string{}
	if wanted {
		targets, err := b.targets(ctx, client)
		if err != nil {
			b.report(err, "watch", "targets", nil)
			return
		}
		roots = lo.Map(includedTargets(targets), func(target Target, _ int) string { return target.Path })
		slices.Sort(roots)
	}

	b.watchMu.Lock()
	defer b.watchMu.Unlock()
	if slices.Equal(roots, b.watchRoots) {
		return
	}
	if b.watch != nil {
		b.report(b.watch.Close(), "watch", "close", nil)
		b.watch = nil
	}
	b.watchRoots = roots
	if len(roots) == 0 {
		return
	}

	watch, err := watcher.WatchTree(roots, watcher.TreeConfig{
		Depth:    -1,
		Debounce: watchDebounce,
		OnError: func(err error) {
			b.report(err, "watch", "read", map[string]any{"roots": roots})
		},
	}, func(watcher.Event) {
		b.mu.Lock()
		b.changed = b.now()
		b.mu.Unlock()
	})
	if err != nil {
		b.report(err, "watch", "open", map[string]any{"roots": roots})
		b.watchRoots = nil
		return
	}
	b.watch = watch
}

func (b *Backup) closeWatch() error {
	b.watchMu.Lock()
	defer b.watchMu.Unlock()
	b.watchRoots = nil
	if b.watch == nil {
		return nil
	}
	err := b.watch.Close()
	b.watch = nil
	return err
}
