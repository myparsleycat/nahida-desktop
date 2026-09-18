package menumaker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nahida.live/desktop/internal/infra"
)

var sidecarRepairMu sync.Mutex

type sidecarRepair struct {
	path     string
	original []byte
	updated  []byte
}

// RepairRelocatedSidecars updates generated menu namespaces after a mod folder
// is copied, moved, or renamed. Files without the Nahida sidecar marker are
// never changed.
func RepairRelocatedSidecars(rootPath string) error {
	root, err := requireDirectory(rootPath)
	if err != nil {
		return err
	}

	sidecarRepairMu.Lock()
	defer sidecarRepairMu.Unlock()

	repairs, err := collectSidecarRepairs(root)
	if err != nil {
		return err
	}
	completed := make([]sidecarRepair, 0, len(repairs))
	for _, repair := range repairs {
		if err := writeAtomic(repair.path, repair.updated); err != nil {
			rollbackErrors := make([]error, 0, len(completed))
			for index := len(completed) - 1; index >= 0; index-- {
				previous := completed[index]
				rollbackErrors = append(
					rollbackErrors,
					infra.AnnotateError(writeAtomic(previous.path, previous.original), infra.Diagnostic{
						Stage:  "rollback",
						Fields: map[string]any{"path": previous.path},
					}),
				)
			}
			return errors.Join(
				fmt.Errorf("repair menu maker sidecar %s: %w", repair.path, err),
				errors.Join(rollbackErrors...),
			)
		}
		completed = append(completed, repair)
	}
	return nil
}

func collectSidecarRepairs(root string) ([]sidecarRepair, error) {
	repairs := []sidecarRepair{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != root && (entry.Type()&os.ModeSymlink != 0 || isReparsePoint(path)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(entry.Name(), menuININame) {
			return nil
		}
		repair, changed, err := relocatedSidecarRepair(path)
		if err != nil {
			return err
		}
		if changed {
			repairs = append(repairs, repair)
		}
		return nil
	})
	return repairs, err
}

func relocatedSidecarRepair(menuPath string) (sidecarRepair, bool, error) {
	raw, err := readLimited(menuPath, maxSourceBytes)
	if err != nil {
		return sidecarRepair{}, false, err
	}
	text, encoding, err := decodeText(raw)
	if err != nil {
		return sidecarRepair{}, false, err
	}
	sourceName, ok := sidecarSourceName(text)
	if !ok {
		return sidecarRepair{}, false, nil
	}
	sourcePath := filepath.Join(filepath.Dir(menuPath), sourceName)
	sourceRaw, err := readLimited(sourcePath, maxSourceBytes)
	if errors.Is(err, os.ErrNotExist) {
		return sidecarRepair{}, false, nil
	}
	if err != nil {
		return sidecarRepair{}, false, err
	}
	sourceText, _, err := decodeText(sourceRaw)
	if err != nil {
		return sidecarRepair{}, false, err
	}
	namespace, err := sourceNamespace(sourcePath, sourceText)
	if err != nil {
		return sidecarRepair{}, false, err
	}
	updatedText, changed, err := replaceTopLevelNamespace(text, namespace)
	if err != nil || !changed {
		return sidecarRepair{}, false, err
	}
	updated, err := encodeText(updatedText, encoding)
	if err != nil {
		return sidecarRepair{}, false, err
	}
	return sidecarRepair{path: menuPath, original: raw, updated: updated}, true, nil
}

func sidecarSourceName(text string) (string, bool) {
	first, _, _ := strings.Cut(strings.TrimPrefix(text, "\ufeff"), "\n")
	first = strings.TrimSuffix(first, "\r")
	if !strings.HasPrefix(first, sidecarMarker) {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(first, sidecarMarker))
	if name == "" || filepath.Base(name) != name || !strings.EqualFold(filepath.Ext(name), ".ini") {
		return "", false
	}
	return name, true
}

func replaceTopLevelNamespace(text, namespace string) (string, bool, error) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		key, value, found := strings.Cut(stripComment(line), "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "namespace") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(value), namespace) {
			return text, false, nil
		}
		lines[index] = "namespace = " + namespace
		return strings.Join(lines, "\n"), true, nil
	}
	return "", false, errors.New("generated menu sidecar has no top-level namespace")
}
