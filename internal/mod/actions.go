package mod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

func (m *Mod) Toggle(ctx context.Context, modPath string) (string, error) {
	game, err := m.ownedPath(ctx, modPath)
	if err != nil {
		return "", err
	}
	release, err := m.rejectActiveDownloadAction(modPath)
	if err != nil {
		return "", err
	}
	defer release()
	if isNTEImporter(game.Importer) {
		return m.setNteModEnabled(ctx, modPath, !isNteModEnabled(modPath))
	}
	if isDisabled(filepath.Base(modPath)) {
		return m.enableWithShaders(ctx, modPath)
	}
	return m.disableWithShaders(ctx, modPath)
}

// Disable is an internal service boundary used by backend feature services
// that create an enabled replacement before retiring the source mod.
//
//wails:ignore
func (m *Mod) Disable(ctx context.Context, modPath string) (string, error) {
	game, err := m.ownedPath(ctx, modPath)
	if err != nil {
		return "", err
	}
	release, err := m.rejectActiveDownloadAction(modPath)
	if err != nil {
		return "", err
	}
	defer release()
	if isNTEImporter(game.Importer) {
		return m.setNteModEnabled(ctx, modPath, false)
	}
	return m.disableWithShaders(ctx, modPath)
}

// DisableUnmanaged is the internal counterpart for tools that accept an
// explicitly selected folder outside the configured game roots.
//
//wails:ignore
func (m *Mod) DisableUnmanaged(ctx context.Context, modPath string) (string, error) {
	return m.disableWithShaders(ctx, modPath)
}

// Enable is the inverse internal boundary used by generated-mod rollback.
//
//wails:ignore
func (m *Mod) Enable(ctx context.Context, modPath string) (string, error) {
	game, err := m.ownedPath(ctx, modPath)
	if err != nil {
		return "", err
	}
	release, err := m.rejectActiveDownloadAction(modPath)
	if err != nil {
		return "", err
	}
	defer release()
	if isNTEImporter(game.Importer) {
		return m.setNteModEnabled(ctx, modPath, true)
	}
	return m.enableWithShaders(ctx, modPath)
}

func (m *Mod) ExclusiveToggle(ctx context.Context, modPath string) (string, error) {
	game, err := m.ownedPath(ctx, modPath)
	if err != nil {
		return "", err
	}
	if isNTEImporter(game.Importer) {
		roots := nteRootsFor(*game)
		groupPath := filepath.Dir(modPath)
		if isNtePakWrapper(roots, groupPath) {
			groupPath = filepath.Dir(groupPath)
		}
		entries := collectNteModEntries(roots, groupPath)
		paths := make([]string, len(entries)+1)
		paths[0] = modPath
		for index, entry := range entries {
			paths[index+1] = entry.path
		}
		blocked, release := m.guardActiveDownloadActions(paths)
		if blocked[0] {
			release()
			return "", errors.New("MOD_DOWNLOAD_IN_PROGRESS")
		}
		defer release()
		return m.retryExclusiveToggleOperation(ctx, modPath, func() (string, error) {
			if isNteModEnabled(modPath) {
				return m.setNteModEnabled(ctx, modPath, false)
			}
			for index, entry := range entries {
				if blocked[index+1] || !isNteModEnabled(entry.path) {
					continue
				}
				if _, err := m.setNteModEnabled(ctx, entry.path, false); err != nil {
					m.logActionError(err, "Mod:setAllNte:"+entry.path)
				}
			}
			return m.setNteModEnabled(ctx, modPath, true)
		})
	}
	if !isDisabled(filepath.Base(modPath)) {
		release, err := m.rejectActiveDownloadAction(modPath)
		if err != nil {
			return "", err
		}
		defer release()
		return m.retryExclusiveToggleOperation(ctx, modPath, func() (string, error) {
			return m.disableWithShaders(ctx, modPath)
		})
	}
	entries, err := os.ReadDir(filepath.Dir(modPath))
	if err != nil {
		return "", err
	}
	paths := []string{modPath}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") && entry.IsDir() &&
			!samePath(filepath.Join(filepath.Dir(modPath), entry.Name()), modPath) && !isDisabled(entry.Name()) {
			paths = append(paths, filepath.Join(filepath.Dir(modPath), entry.Name()))
		}
	}
	blocked, release := m.guardActiveDownloadActions(paths)
	if blocked[0] {
		release()
		return "", errors.New("MOD_DOWNLOAD_IN_PROGRESS")
	}
	defer release()
	for index, path := range paths[1:] {
		if !blocked[index+1] {
			if _, err := m.retryExclusiveToggleOperation(ctx, path, func() (string, error) {
				return m.disableWithShaders(ctx, path)
			}); err != nil {
				m.logActionError(err, "Mod:exclusiveToggle:disable:"+path)
			}
		}
	}
	return m.retryExclusiveToggleOperation(ctx, modPath, func() (string, error) {
		return m.enableWithShaders(ctx, modPath)
	})
}

func (m *Mod) Rename(ctx context.Context, modPath, newName string) (string, error) {
	if _, err := m.ownedPath(ctx, modPath); err != nil {
		return "", err
	}
	release, err := m.rejectActiveDownloadAction(modPath)
	if err != nil {
		return "", err
	}
	defer release()
	newName = stripDisabled(newName)
	if newName == "" {
		return "", errors.New("INVALID_MOD_NAME")
	}
	if err := m.fs.AssertValidWindowsFilename(newName); err != nil {
		return "", err
	}
	if isDisabled(filepath.Base(modPath)) {
		newName = restoreDisabledPrefix(filepath.Base(modPath), newName)
	}
	if filepath.Base(modPath) == newName {
		return filepath.Clean(modPath), nil
	}
	next := filepath.Join(filepath.Dir(modPath), newName)
	if !samePath(modPath, next) {
		if _, err := os.Stat(next); err == nil {
			return "", fmt.Errorf("ALREADY_EXISTS:%s", newName)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := os.Rename(modPath, next); err != nil {
		return "", m.lockedFolderError(err, modPath)
	}
	return next, nil
}

func (m *Mod) EnableAll(ctx context.Context, groupPath string) error {
	game, err := m.ownedPath(ctx, groupPath)
	if err != nil {
		return err
	}
	if isNTEImporter(game.Importer) {
		m.setAllNteBestEffort(ctx, collectNteModEntries(nteRootsFor(*game), groupPath), true)
		return nil
	}
	entries, err := os.ReadDir(groupPath)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") && entry.IsDir() && isDisabled(entry.Name()) {
			paths = append(paths, filepath.Join(groupPath, entry.Name()))
		}
	}
	blocked, release := m.guardActiveDownloadActions(paths)
	defer release()
	for index, path := range paths {
		if !blocked[index] {
			if _, err := m.enableWithShaders(ctx, path); err != nil {
				m.logActionError(err, "Mod:enableAll:"+path)
			}
		}
	}
	return nil
}

func (m *Mod) DisableAll(ctx context.Context, groupPath string) error {
	game, err := m.ownedPath(ctx, groupPath)
	if err != nil {
		return err
	}
	if isNTEImporter(game.Importer) {
		m.setAllNteBestEffort(ctx, collectNteModEntries(nteRootsFor(*game), groupPath), false)
		return nil
	}
	entries, err := os.ReadDir(groupPath)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") && entry.IsDir() && !isDisabled(entry.Name()) {
			paths = append(paths, filepath.Join(groupPath, entry.Name()))
		}
	}
	blocked, release := m.guardActiveDownloadActions(paths)
	defer release()
	for index, path := range paths {
		if !blocked[index] {
			if _, err := m.disableWithShaders(ctx, path); err != nil {
				m.logActionError(err, "Mod:disableAll:"+path)
			}
		}
	}
	return nil
}

func (m *Mod) retryExclusiveToggleOperation(
	ctx context.Context,
	modPath string,
	operation func() (string, error),
) (string, error) {
	for attempt := 1; ; attempt++ {
		result, err := operation()
		if err == nil {
			return result, nil
		}
		if attempt >= 3 || !isRetryableExclusiveToggleError(err) {
			return "", m.lockedFolderError(err, modPath)
		}
		timer := time.NewTimer(time.Duration(attempt) * 50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

func isRetryableExclusiveToggleError(err error) bool {
	if err == nil {
		return false
	}
	if strings.HasPrefix(err.Error(), "MOD_FOLDER_LOCKED") {
		return true
	}
	return isRetryableTogglePlatformError(err)
}

func (m *Mod) setAllNteBestEffort(ctx context.Context, entries []nteModEntry, enabled bool) {
	paths := make([]string, len(entries))
	for index, entry := range entries {
		paths[index] = entry.path
	}
	blocked, release := m.guardActiveDownloadActions(paths)
	defer release()
	for index, entry := range entries {
		if isNteModEnabled(entry.path) == enabled || blocked[index] {
			continue
		}
		if _, err := m.setNteModEnabled(ctx, entry.path, enabled); err != nil {
			m.logActionError(err, "Mod:setAllNte:"+entry.path)
		}
	}
}

func (m *Mod) rejectActiveDownloadAction(path string) (func(), error) {
	blocked, release := m.guardActiveDownloadActions([]string{path})
	if blocked[0] {
		release()
		return nil, errors.New("MOD_DOWNLOAD_IN_PROGRESS")
	}
	return release, nil
}

func (m *Mod) isActiveDownloadDestination(path string) bool {
	return m != nil && m.transfer != nil && m.transfer.IsActiveDownloadDestination(path)
}

func (m *Mod) guardActiveDownloadActions(paths []string) ([]bool, func()) {
	if m == nil || m.transfer == nil {
		return make([]bool, len(paths)), func() {}
	}
	return m.transfer.GuardDownloadDestinations(paths)
}

func (m *Mod) logActionError(err error, where string) {
	if err != nil && m != nil && m.log != nil {
		_ = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
			Severity: infra.DiagnosticError, Operation: where, Stage: "item-action",
		})
	}
}

func (m *Mod) enable(path string) (string, error) {
	name := stripDisabled(filepath.Base(path))
	if name == filepath.Base(path) {
		return filepath.Clean(path), nil
	}
	result, err := renameUnique(path, name)
	if err != nil {
		return "", m.lockedFolderError(err, path)
	}
	return result, nil
}

func (m *Mod) disable(ctx context.Context, path string) (string, error) {
	if isDisabled(filepath.Base(path)) {
		return filepath.Clean(path), nil
	}
	style := "space"
	if m.settings != nil {
		value, err := m.settings.GetDisabledPrefixStyle(ctx)
		if err != nil {
			return "", err
		}
		style = value
	}
	prefix := "DISABLED "
	if style == "underscore" {
		prefix = "DISABLED_"
	}
	result, err := renameUnique(path, prefix+filepath.Base(path))
	if err != nil {
		return "", m.lockedFolderError(err, path)
	}
	return result, nil
}

func (m *Mod) enableWithShaders(ctx context.Context, path string) (string, error) {
	if !isDisabled(filepath.Base(path)) {
		return filepath.Clean(path), nil
	}
	copyShaderFixes := false
	if m.settings != nil {
		value, err := m.settings.GetCopyShaderFixesOnEnable(ctx)
		if err != nil {
			return "", err
		}
		copyShaderFixes = value
	}
	var processed []ShaderFixesProcessedFile
	if copyShaderFixes && m.shaders != nil {
		copied, err := m.shaders.HandleShaders(path, true)
		if err != nil {
			processed = processedFilesFromError(err)
			if rollbackErr := m.shaders.RollbackEnabledShaders(path, processed); rollbackErr != nil {
				m.logShaderError(rollbackErr, "Mod:enable:rollbackShaders:"+path)
			}
			return "", err
		}
		processed = copied
	}
	result, err := m.enable(path)
	if err != nil {
		if copyShaderFixes && m.shaders != nil {
			if rollbackErr := m.shaders.RollbackEnabledShaders(path, processed); rollbackErr != nil {
				m.logShaderError(rollbackErr, "Mod:enable:rollbackShaders:"+path)
			}
		}
		return "", err
	}
	return result, nil
}

func (m *Mod) disableWithShaders(ctx context.Context, path string) (string, error) {
	if isDisabled(filepath.Base(path)) {
		return filepath.Clean(path), nil
	}
	if m.shaders != nil {
		if _, err := m.shaders.HandleShaders(path, false); err != nil {
			if _, rollbackErr := m.shaders.HandleShaders(path, true); rollbackErr != nil {
				m.logShaderError(rollbackErr, "Mod:disable:rollbackShaders:"+path)
			}
			return "", err
		}
	}
	return m.disable(ctx, path)
}

func (m *Mod) lockedFolderError(err error, modPath string) error {
	if err == nil || m == nil || m.fs == nil {
		return err
	}
	lock := m.fs.IsLockedPathError(err, modPath)
	if !lock.IsLocked {
		return err
	}
	if len(lock.Processes) == 0 {
		return errors.New("MOD_FOLDER_LOCKED")
	}
	names := make([]string, len(lock.Processes))
	for i, proc := range lock.Processes {
		names[i] = proc.Name
	}
	return fmt.Errorf("MOD_FOLDER_LOCKED|%s", strings.Join(names, ", "))
}

func renameUnique(source, desiredName string) (string, error) {
	parent := filepath.Dir(source)
	destination := filepath.Join(parent, desiredName)
	for index := 2; ; index++ {
		_, err := os.Stat(destination)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		destination = filepath.Join(parent, fmt.Sprintf("%s (%d)", desiredName, index))
	}
	if err := os.Rename(source, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func samePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
