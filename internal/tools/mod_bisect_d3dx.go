package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/watcher"
)

type d3dxGuard struct {
	game        string
	path        string
	initial     []byte
	backupPath  string
	watcher     *watcher.Watcher
	cancel      context.CancelFunc
	restoreMu   sync.Mutex
	restoreWake chan struct{}
	restoreDone chan struct{}
}

func (t *Tools) startD3dxGuardLocked(ctx context.Context, game db.GamePathRow) error {
	if t.xxmi == nil || game.Importer == nil || strings.TrimSpace(*game.Importer) == "" || t.appData == nil {
		return nil
	}
	importers, err := t.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		return err
	}
	var d3dxPath string
	for _, importer := range importers {
		if importer.Key == *game.Importer {
			d3dxPath = filepath.Join(importer.ImporterFolder, "d3dx_user.ini")
			break
		}
	}
	if d3dxPath == "" {
		return nil
	}
	initial, err := os.ReadFile(d3dxPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	backupPath := t.d3dxBackupPath(game.Game)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(backupPath, initial, 0o600); err != nil {
		return err
	}
	guardCtx, cancel := context.WithCancel(context.Background())
	guard := &d3dxGuard{
		game: game.Game, path: filepath.Clean(d3dxPath), initial: initial, backupPath: backupPath,
		cancel: cancel, restoreWake: make(chan struct{}, 1), restoreDone: make(chan struct{}),
	}
	go t.runD3dxRestoreWorker(guardCtx, guard)
	service, err := watcher.WatchFile(d3dxPath, watcher.FileConfig{
		Ops: watcher.Create | watcher.Write | watcher.Rename,
		OnError: func(err error) {
			t.logError(err, "ModBisect:d3dxWatcher")
		},
	}, func(watcher.Event) {
		queueD3dxRestore(guard)
	})
	if err != nil {
		cancel()
		<-guard.restoreDone
		_ = os.Remove(backupPath)
		return err
	}
	guard.watcher = service
	t.d3dx = guard
	return nil
}

func queueD3dxRestore(guard *d3dxGuard) {
	if guard == nil {
		return
	}
	select {
	case guard.restoreWake <- struct{}{}:
	default:
	}
}

func (t *Tools) runD3dxRestoreWorker(ctx context.Context, guard *d3dxGuard) {
	defer close(guard.restoreDone)
	for {
		select {
		case <-ctx.Done():
			return
		case <-guard.restoreWake:
			guard.restoreMu.Lock()
			t.restoreD3dx(ctx, guard)
			guard.restoreMu.Unlock()
		}
	}
}

func (t *Tools) restoreD3dx(ctx context.Context, guard *d3dxGuard) {
	timer := time.NewTimer(300 * time.Millisecond)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
		return
	case <-timer.C:
	}
	var lastErr error
	for attempt := range 6 {
		current, err := os.ReadFile(guard.path)
		if err == nil && string(current) == string(guard.initial) {
			return
		}
		if err == nil {
			err = os.WriteFile(guard.path, guard.initial, 0o600)
		}
		if err == nil {
			return
		}
		lastErr = err
		if attempt == 5 {
			break
		}
		timer.Reset(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
	t.logError(lastErr, "ModBisect:d3dxRestore")
}

func (t *Tools) stopD3dxGuardLocked(game string) error {
	guard := t.d3dx
	if guard == nil || guard.game != game {
		return nil
	}
	t.d3dx = nil
	guard.cancel()
	_ = guard.watcher.Close()
	<-guard.restoreDone
	guard.restoreMu.Lock()
	defer guard.restoreMu.Unlock()
	backup, err := os.ReadFile(guard.backupPath)
	if err == nil {
		if writeErr := os.WriteFile(guard.path, backup, 0o600); writeErr != nil {
			return writeErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(guard.backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (t *Tools) recoverD3dxBackupLocked(ctx context.Context, game db.GamePathRow) error {
	backupPath := t.d3dxBackupPath(game.Game)
	if backupPath == "" {
		return nil
	}
	backup, err := os.ReadFile(backupPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if t.xxmi == nil || game.Importer == nil {
		return nil
	}
	importers, err := t.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		return err
	}
	for _, importer := range importers {
		if importer.Key != *game.Importer {
			continue
		}
		path := filepath.Join(importer.ImporterFolder, "d3dx_user.ini")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		if err := os.WriteFile(path, backup, 0o600); err != nil {
			return err
		}
		return os.Remove(backupPath)
	}
	return nil
}

func (t *Tools) d3dxBackupPath(game string) string {
	if t == nil || t.appData == nil {
		return ""
	}
	safe := strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*', 0:
			return '_'
		default:
			return r
		}
	}, strings.TrimSpace(game))
	if safe == "" || safe == "." || safe == ".." {
		safe = fmt.Sprintf("game-%s", sha256Hex([]byte(game))[:12])
	}
	path, err := t.appData.Resolve(filepath.Join(appdata.ModBisectDir, safe+".d3dx_user.ini.bak"))
	if err != nil {
		return ""
	}
	return path
}
