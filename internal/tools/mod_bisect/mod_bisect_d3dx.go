package modbisect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/watcher"
)

const d3dxBackupSuffix = ".d3dx_user.ini.bak"

type d3dxGuard struct {
	gameID      string
	path        string
	initial     []byte
	backupPath  string
	watcher     *watcher.Watcher
	cancel      context.CancelFunc
	restoreMu   sync.Mutex
	restoreWake chan struct{}
	restoreDone chan struct{}
}

func (t *Service) startD3dxGuardLocked(ctx context.Context, game db.GamePathRow) error {
	if t.appData == nil {
		return nil
	}
	d3dxPath, err := t.d3dxUserPath(ctx, game)
	if err != nil || d3dxPath == "" {
		return err
	}
	backupPath, err := t.d3dxBackupPath(game.ID)
	if err != nil {
		return err
	}
	pending, err := t.pendingD3dxBackupPath(game, backupPath)
	if err != nil {
		return err
	}
	if pending != "" {
		if err := t.restoreD3dxBackup(ctx, d3dxPath, pending); err != nil {
			return err
		}
	}

	initial, err := os.ReadFile(d3dxPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		return err
	}
	if err := platform.WriteFileAtomic(backupPath, initial, 0o600, func(err error) {
		t.reportCleanup(err, "startD3dxGuardLocked")
	}); err != nil {
		return err
	}
	guardCtx, cancel := context.WithCancel(context.Background())
	guard := &d3dxGuard{
		gameID: game.ID, path: filepath.Clean(d3dxPath), initial: initial, backupPath: backupPath,
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
		t.reportCleanup(os.Remove(backupPath), "startD3dxGuardLocked")
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

func (t *Service) runD3dxRestoreWorker(ctx context.Context, guard *d3dxGuard) {
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

func (t *Service) restoreD3dx(ctx context.Context, guard *d3dxGuard) {
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

func (t *Service) stopD3dxGuardLocked(gameID string) error {
	guard := t.d3dx
	if guard == nil || guard.gameID != gameID {
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
		if writeErr := platform.WriteFileAtomic(guard.path, backup, 0o600, func(err error) {
			t.reportCleanup(err, "stopD3dxGuardLocked")
		}); writeErr != nil {
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

func (t *Service) recoverD3dxBackupLocked(ctx context.Context, game db.GamePathRow) error {
	if t.appData == nil {
		return nil
	}
	backupPath, err := t.d3dxBackupPath(game.ID)
	if err != nil {
		return err
	}
	pending, err := t.pendingD3dxBackupPath(game, backupPath)
	if err != nil || pending == "" {
		return err
	}
	d3dxPath, err := t.d3dxUserPath(ctx, game)
	if err != nil || d3dxPath == "" {
		return err
	}
	return t.restoreD3dxBackup(ctx, d3dxPath, pending)
}

func (t *Service) d3dxUserPath(ctx context.Context, game db.GamePathRow) (string, error) {
	if t.xxmi == nil || game.Importer == nil || strings.TrimSpace(*game.Importer) == "" {
		return "", nil
	}
	importers, err := t.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		return "", err
	}
	for _, importer := range importers {
		if importer.Key != *game.Importer {
			continue
		}
		path := filepath.Join(importer.ImporterFolder, "d3dx_user.ini")
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return "", nil
		} else if err != nil {
			return "", err
		}
		return path, nil
	}
	return "", nil
}

func (t *Service) restoreD3dxBackup(ctx context.Context, path, backupPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		return err
	}
	if err := platform.WriteFileAtomic(path, backup, 0o600, func(err error) {
		t.reportCleanup(err, "restoreD3dxBackup")
	}); err != nil {
		return err
	}
	return os.Remove(backupPath)
}

func (t *Service) pendingD3dxBackupPath(game db.GamePathRow, backupPath string) (string, error) {
	paths := []string{backupPath}
	if game.LegacyName != nil {
		legacyPath, err := t.d3dxLegacyBackupPath(*game.LegacyName)
		if err != nil {
			return "", err
		}
		if legacyPath != "" {
			paths = append(paths, legacyPath)
		}
	}
	pending := ""
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("D3DX backup is not a regular file: %q", path)
		}
		if pending != "" {
			return "", fmt.Errorf("both D3DX backups exist: %q and %q", pending, path)
		}
		pending = path
	}
	return pending, nil
}

func (t *Service) d3dxBackupPath(gameID string) (string, error) {
	id, err := uuid.Parse(gameID)
	if err != nil {
		return "", fmt.Errorf("invalid game ID %q for D3DX backup: %w", gameID, err)
	}
	// A legacy display name can look like a UUID.
	return t.appData.Resolve(filepath.Join(appdata.ModBisectDir, "by-id", id.String()+d3dxBackupSuffix))
}

func (t *Service) d3dxLegacyBackupPath(game string) (string, error) {
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
	filename := safe + d3dxBackupSuffix
	if !platform.NewFS().IsValidWindowsFilename(filename) || !filepath.IsLocal(filename) {
		return "", nil
	}
	return t.appData.Resolve(filepath.Join(appdata.ModBisectDir, filename))
}
