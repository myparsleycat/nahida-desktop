package mod

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"nahida.live/desktop/internal/xxmi"
)

// SuspendImporterWatchers closes directory handles and drains compression before an importer is renamed.
// The returned function must run after commit or rollback, with moves only for a committed data migration.
//
//wails:ignore
func (m *Mod) SuspendImporterWatchers(ctx context.Context) (func([]xxmi.ImportedImporter) error, error) {
	m.watchMu.Lock()
	if err := ctx.Err(); err != nil {
		m.watchMu.Unlock()
		return nil, err
	}
	game, character := m.gameWatcher, m.characterWatcher
	m.gameWatcher, m.characterWatcher = nil, nil
	stopErr := errors.Join(closeManagedWatcher(game), closeManagedWatcher(character))
	c := m.compression
	wasStopped := c.stopped.Load()
	c.mu.Lock()
	loaded := c.loaded
	c.mu.Unlock()
	stopErr = errors.Join(stopErr, c.stop())
	// Keep new requests and synchronous compression work out until the tree is stable again.
	c.requestMu.Lock()
	c.opMu.Lock()

	var once sync.Once
	var resumeErr error
	resume := func(moved []xxmi.ImportedImporter) error {
		once.Do(func() {
			defer m.watchMu.Unlock()
			for _, saved := range []struct {
				previous *managedWatcher
				current  **managedWatcher
			}{{game, &m.gameWatcher}, {character, &m.characterWatcher}} {
				if saved.previous == nil {
					continue
				}
				path := xxmi.RelocateUserDataPath(saved.previous.root, moved)
				restored, err := newManagedWatcher(
					path, saved.previous.depth, saved.previous.eventName, saved.previous.emit, m.watcherReporter(path),
				)
				if err != nil {
					resumeErr = errors.Join(resumeErr, fmt.Errorf("resume mod watcher %q: %w", path, err))
					continue
				}
				restored.onChange(saved.previous.changed)
				*saved.current = restored
			}

			// An import leaves ShaderFixes behind, so shaders that enabled mods copied there are missing from the
			// importer folder now in use. A rolled-back import changes no importer folder and finds nothing to do.
			for _, game := range m.shaders.games() {
				root := xxmi.RelocateUserDataPath(game.ModFolderPath, moved)
				if err := m.shaders.reapplyRelocated(root); err != nil {
					resumeErr = errors.Join(resumeErr, fmt.Errorf("reapply shader fixes under %q: %w", root, err))
				}
			}
			c.opMu.Unlock()
			c.requestMu.Unlock()
			c.stopped.Store(wasStopped)
			if loaded && !wasStopped {
				resumeErr = errors.Join(resumeErr, c.replaceWatcher(context.WithoutCancel(ctx)))
				c.schedule()
			}
		})
		return resumeErr
	}
	if stopErr != nil {
		return nil, errors.Join(stopErr, resume(nil))
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, resume(nil))
	}
	return resume, nil
}
