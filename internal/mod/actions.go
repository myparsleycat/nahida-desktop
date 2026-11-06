package mod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func (m *Mod) Toggle(ctx context.Context, modPath string) (string, error) {
	defer m.InvalidateLibraries()

	game, modPath, err := m.ownedTarget(ctx, modPath)
	if err != nil {
		return "", err
	}
	defer m.groups.lock(modGroupPath(*game, modPath))()
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
	defer m.InvalidateLibraries()

	game, modPath, err := m.ownedTarget(ctx, modPath)
	if err != nil {
		return "", err
	}
	defer m.groups.lock(modGroupPath(*game, modPath))()
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
	defer m.InvalidateLibraries()

	defer m.groups.lock(filepath.Dir(modPath))()
	return m.disableWithShaders(ctx, modPath)
}

// Enable is the inverse internal boundary used by generated-mod rollback.
//
//wails:ignore
func (m *Mod) Enable(ctx context.Context, modPath string) (string, error) {
	defer m.InvalidateLibraries()

	game, modPath, err := m.ownedTarget(ctx, modPath)
	if err != nil {
		return "", err
	}
	defer m.groups.lock(modGroupPath(*game, modPath))()
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
	defer m.InvalidateLibraries()

	game, modPath, err := m.ownedTarget(ctx, modPath)
	if err != nil {
		return "", err
	}
	if isNTEImporter(game.Importer) {
		roots := nteRootsFor(*game)
		groupPath := modGroupPath(*game, modPath)
		defer m.groups.lock(groupPath)()
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
	// The sibling list read below must still be current when the siblings are renamed.
	defer m.groups.lock(modGroupPath(*game, modPath))()
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
	defer m.InvalidateLibraries()

	game, modPath, err := m.ownedTarget(ctx, modPath)
	if err != nil {
		return "", err
	}
	defer m.groups.lock(modGroupPath(*game, modPath))()
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
	boundary := m.previewBoundary(ctx, modPath)
	if err := os.Rename(modPath, next); err != nil {
		return "", m.lockedFolderError(err, modPath)
	}
	renamed, err := finishFolderRename(modPath, next, boundary, m.reportMetadataCleanup)
	if err != nil {
		m.logActionError(err, "Mod:rename:preview")
	}
	return renamed, err
}

func (m *Mod) EnableAll(ctx context.Context, groupPath string) error {
	defer m.InvalidateLibraries()

	game, groupPath, err := m.ownedTarget(ctx, groupPath)
	if err != nil {
		return err
	}
	defer m.groups.lock(groupPath)()
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
	defer m.InvalidateLibraries()

	game, groupPath, err := m.ownedTarget(ctx, groupPath)
	if err != nil {
		return err
	}
	defer m.groups.lock(groupPath)()
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

// groupLocks serializes the actions that read a group's folders and rename them, so two actions on one
// group never act on the same stale listing.
type groupLocks struct {
	mu    sync.Mutex
	locks map[string]*groupLock
}

type groupLock struct {
	mu   sync.Mutex
	refs int
}

// modGroupPath is the group whose lock covers modPath. An NTE pak wrapper belongs to the group
// that holds it, which is the folder ExclusiveToggle lists.
func modGroupPath(game GameConfig, modPath string) string {
	groupPath := filepath.Dir(modPath)
	if isNTEImporter(game.Importer) && isNtePakWrapper(nteRootsFor(game), groupPath) {
		return filepath.Dir(groupPath)
	}
	return groupPath
}

func (l *groupLocks) lock(groupPath string) (unlock func()) {
	key := filepath.Clean(groupPath)
	if physical, err := platform.PhysicalPath(key); err == nil {
		key = physical
	}
	key = strings.ToLower(key)

	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*groupLock)
	}
	entry := l.locks[key]
	if entry == nil {
		entry = &groupLock{}
		l.locks[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
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
	if errors.Is(err, ErrModFolderLocked) {
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
	return m.transfer != nil && m.transfer.IsActiveDownloadDestination(path)
}

func (m *Mod) guardActiveDownloadActions(paths []string) ([]bool, func()) {
	if m.transfer == nil {
		return make([]bool, len(paths)), func() {}
	}
	return m.transfer.GuardDownloadDestinations(paths)
}

func (m *Mod) logActionError(err error, where string) {
	_ = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: where, Stage: "item-action",
	})
}

func (m *Mod) enable(ctx context.Context, path string) (string, error) {
	name := stripDisabled(filepath.Base(path))
	if name == filepath.Base(path) {
		return filepath.Clean(path), nil
	}
	boundary := m.previewBoundary(ctx, path)
	result, err := renameUnique(path, name)
	if err != nil {
		return "", m.lockedFolderError(err, path)
	}
	renamed, err := finishFolderRename(path, result, boundary, m.reportMetadataCleanup)
	if err != nil {
		m.logActionError(err, "Mod:enable:preview")
	}
	return renamed, err
}

func finishFolderRename(oldPath, newPath, boundary string, reports ...func(error)) (string, error) {
	if err := relocateSelectedPreviews(oldPath, newPath, boundary, reports...); err != nil {
		if undoErr := os.Rename(newPath, oldPath); undoErr != nil {
			return "", errors.Join(err, fmt.Errorf("restore renamed folder: %w", undoErr))
		}
		return "", err
	}
	return newPath, nil
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
	boundary := m.previewBoundary(ctx, path)
	result, err := renameUnique(path, prefix+filepath.Base(path))
	if err != nil {
		return "", m.lockedFolderError(err, path)
	}
	renamed, err := finishFolderRename(path, result, boundary, m.reportMetadataCleanup)
	if err != nil {
		m.logActionError(err, "Mod:disable:preview")
	}
	return renamed, err
}

func (m *Mod) enableWithShaders(ctx context.Context, path string) (string, error) {
	if !isDisabled(filepath.Base(path)) {
		return filepath.Clean(path), nil
	}
	if err := m.compression.restoreBeforeEnable(ctx, path); err != nil {
		return "", err
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
	if copyShaderFixes {
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
	result, err := m.enable(ctx, path)
	if err != nil {
		if copyShaderFixes {
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
	// Only a mod that had its shaders installed gets them back when the rename fails; reinstalling would
	// otherwise copy shaders the user never had installed.
	manifest, manifestErr := m.shaders.readShaderFixesModManifest(path)
	installed := manifest != nil || manifestErr != nil
	if _, err := m.shaders.HandleShaders(path, false); err != nil {
		if _, rollbackErr := m.shaders.HandleShaders(path, true); rollbackErr != nil {
			m.logShaderError(rollbackErr, "Mod:disable:rollbackShaders:"+path)
		}
		return "", err
	}
	result, err := m.disable(ctx, path)
	if err != nil {
		if installed {
			if _, rollbackErr := m.shaders.HandleShaders(path, true); rollbackErr != nil {
				m.logShaderError(rollbackErr, "Mod:disable:rollbackShaders:"+path)
			}
		}
		return "", err
	}
	m.compression.scheduleScope(result)
	return result, nil
}

func (m *Mod) lockedFolderError(err error, modPath string) error {
	if err == nil {
		return nil
	}
	lock := m.fs.IsLockedPathError(err, modPath)
	if !lock.IsLocked {
		return err
	}
	if len(lock.Processes) == 0 {
		return ErrModFolderLocked
	}
	names := make([]string, len(lock.Processes))
	for i, proc := range lock.Processes {
		names[i] = proc.Name
	}
	return fmt.Errorf("%w|%s", ErrModFolderLocked, strings.Join(names, ", "))
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
	return platform.SamePathFold(a, b)
}
