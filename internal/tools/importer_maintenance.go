package tools

import (
	"context"
	"errors"

	"nahida.live/desktop/internal/xxmi"
)

// SuspendImporterWatchers pauses tools that keep handles or pending writes under importer folders.
//
//wails:ignore
func (t *Tools) SuspendImporterWatchers(ctx context.Context) (func([]xxmi.ImportedImporter) error, error) {
	resumeInspection, err := t.fixInspection.SuspendImporterWatchers(ctx)
	if err != nil {
		return nil, err
	}
	resumePersist, err := t.persist.SuspendImporterWatcher(ctx)
	if err != nil {
		return nil, errors.Join(err, resumeInspection(nil))
	}
	resumeToggleViewer, err := t.toggleViewer.SuspendImporterWatcher(ctx)
	if err != nil {
		return nil, errors.Join(err, resumePersist(), resumeInspection(nil))
	}
	return func(moved []xxmi.ImportedImporter) error {
		return errors.Join(resumeToggleViewer(), resumePersist(), resumeInspection(moved))
	}, nil
}
