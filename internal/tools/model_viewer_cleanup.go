package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const legacyModelViewerTempPrefix = "nhd-model-viewer-"

// CleanupStaleModelViewerDirs removes directories left by the Electron static
// GLB viewer. The Go viewer no longer creates them, but existing installations
// can retain old directories in the Windows temp root.
//
//wails:ignore
func (t *Tools) CleanupStaleModelViewerDirs() error {
	return cleanupStaleModelViewerDirs(os.TempDir())
}

func cleanupStaleModelViewerDirs(tempRoot string) error {
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return fmt.Errorf("read model viewer temp root: %w", err)
	}
	var cleanupErrs []error
	for _, entry := range entries {
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
