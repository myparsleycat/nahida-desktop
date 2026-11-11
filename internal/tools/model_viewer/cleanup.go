package modelviewer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/appdata"
)

const legacyModelViewerTempPrefix = "nhd-model-viewer-"

// CleanupStaleModelViewerDirs removes directories left by the Electron static
// GLB viewer, which existing installations can retain in the Windows temp root,
// and the mesh spools of viewer sessions a previous run did not close.
func (t *Service) CleanupStaleModelViewerDirs(ctx context.Context) error {
	err := cleanupStaleModelViewerDirs(ctx, os.TempDir())
	if t.appData == nil {
		return err
	}
	dir, resolveErr := t.appData.Resolve(appdata.ModelViewerSpoolDir)
	if resolveErr != nil {
		return errors.Join(err, resolveErr)
	}
	return errors.Join(err, cleanupModelViewerSpools(ctx, dir))
}

func cleanupStaleModelViewerDirs(ctx context.Context, tempRoot string) error {
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return fmt.Errorf("read model viewer temp root: %w", err)
	}
	var cleanupErrs []error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), errors.Join(cleanupErrs...))
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), legacyModelViewerTempPrefix) {
			continue
		}
		path := filepath.Join(tempRoot, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("remove stale model viewer directory %q: %w", path, err))
		}
	}
	return errors.Join(cleanupErrs...)
}
