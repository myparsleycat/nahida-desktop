package transfer

import (
	"path/filepath"
	"slices"
	"strings"
)

//wails:ignore
func (t *Transfer) IsActiveDownloadDestination(path string) bool {
	t.destinationMu.RLock()
	defer t.destinationMu.RUnlock()
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isActiveDownloadDestinationLocked(path)
}

// GuardDownloadDestinations takes a stable snapshot of active destination
// conflicts and prevents transfer registration or state changes until release.
// Callers must invoke release after the protected filesystem operations finish.
//
//wails:ignore
func (t *Transfer) GuardDownloadDestinations(paths []string) (blocked []bool, release func()) {
	if t == nil {
		return make([]bool, len(paths)), func() {}
	}
	t.destinationMu.RLock()
	t.mu.RLock()
	blocked = make([]bool, len(paths))
	for index, path := range paths {
		blocked[index] = t.isActiveDownloadDestinationLocked(path)
	}
	t.mu.RUnlock()
	return blocked, t.destinationMu.RUnlock
}

func (t *Transfer) isActiveDownloadDestinationLocked(path string) bool {
	for _, item := range t.entries {
		if item.record.Type != "download" ||
			(!isOpen(item.record.Status) && item.record.Status != StatusPaused && item.cancel == nil) {
			continue
		}
		if slices.ContainsFunc(item.record.DestinationPaths, func(destinationPath string) bool {
			return transferPathsOverlap(path, destinationPath)
		}) {
			return true
		}
	}
	return false
}

func destinationTargetPaths(targets []DestinationTarget) []string {
	paths := make([]string, len(targets))
	for index, target := range targets {
		paths[index] = target.Path
	}
	return paths
}

func transferPathsOverlap(first, second string) bool {
	first = cleanTransferPath(first)
	second = cleanTransferPath(second)
	return pathContains(first, second) || pathContains(second, first)
}

func cleanTransferPath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return filepath.Clean(path)
}

func pathContains(parent, child string) bool {
	if strings.EqualFold(parent, child) {
		return true
	}
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}
	return strings.HasPrefix(strings.ToLower(child), strings.ToLower(parent))
}
