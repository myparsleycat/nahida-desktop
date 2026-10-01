package xxmi

import (
	"context"
	"path/filepath"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func (x *XXMI) beginImporterMaintenance(ctx context.Context) (func([]ImportedImporter), error) {
	x.mu.RLock()
	begin := x.importerMaintenance
	x.mu.RUnlock()
	if begin == nil {
		return func([]ImportedImporter) {}, nil
	}
	resume, err := begin(ctx)
	if err != nil {
		return nil, err
	}
	return func(moved []ImportedImporter) {
		if err := resume(moved); err != nil {
			// Installation may already be committed; a watcher failure must not trigger a data rollback.
			_ = infra.ReportError(x.log, err, "XXMI.resumeImporterWatchers", infra.Diagnostic{
				Operation: "resume-importer-watchers", Stage: "cleanup", Fields: map[string]any{"moved": moved},
			})
		}
	}, nil
}

// RelocateUserDataPath follows only user data moved by ImportExternalLauncher; package files stay in source.
func RelocateUserDataPath(path string, moved []ImportedImporter) string {
	for _, importer := range moved {
		for _, name := range importerUserData {
			source := filepath.Join(importer.PreviousFolder, name)
			if !platform.SameOrChildPath(source, path) {
				continue
			}
			relative, err := filepath.Rel(source, path)
			if err == nil {
				return filepath.Join(importer.ImporterFolder, name, relative)
			}
		}
	}
	return path
}
