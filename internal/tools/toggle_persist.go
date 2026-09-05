package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

var persistDeclarationRE = regexp.MustCompile(`(?i)^global\s+persist\s+\$(.+?)\s*=\s*(.+)$`)

type PersistModelViewerResult struct {
	UpdatedVariables []string `json:"updatedVariables"`
}

func (t *Tools) PersistModelViewerToggleState(iniPath string, state map[string]any) (PersistModelViewerResult, error) {
	updates := persistUpdatesFromState(state)
	if len(updates) == 0 {
		return PersistModelViewerResult{UpdatedVariables: []string{}}, nil
	}
	t.persistMu.Lock()
	defer t.persistMu.Unlock()
	updated, err := applyPersistUpdates(iniPath, updates)
	if err != nil {
		return PersistModelViewerResult{}, err
	}
	return PersistModelViewerResult{UpdatedVariables: updated}, nil
}

func (t *Tools) GetPersistLogs() []string {
	if t == nil || t.persist == nil {
		return []string{}
	}
	return t.persist.GetLogs()
}

func (t *Tools) StartPersistWatcher(ctx context.Context) error {
	if t == nil || t.persist == nil {
		return nil
	}
	enabled := false
	if getter, ok := t.settings.(interface {
		GetPersistToggles(context.Context) (bool, error)
	}); ok {
		value, err := getter.GetPersistToggles(ctx)
		if err != nil {
			t.persist.Stop()
			return err
		}
		enabled = value
	}
	if !enabled || t.xxmi == nil {
		t.persist.Stop()
		return nil
	}
	data, err := t.xxmi.GetXXMIData(ctx)
	if err != nil || data.XXMIPath == nil {
		t.persist.Stop()
		return err
	}
	importers, err := t.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		t.persist.Stop()
		return err
	}
	converted := make([]persistImporter, len(importers))
	for i, importer := range importers {
		converted[i] = persistImporter{Key: importer.Key, Folder: importer.ImporterFolder}
	}
	return t.persist.Start(converted, watchPersistFile)
}

func (t *Tools) StopPersistWatcher() bool {
	if t != nil && t.persist != nil {
		t.persist.Stop()
	}
	return true
}

func (t *Tools) shutdownPersistWatcher() error {
	t.StopPersistWatcher()
	return nil
}

func persistUpdatesFromState(state map[string]any) map[string]string {
	updates := make(map[string]string, len(state))
	for name, value := range state {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		switch typed := value.(type) {
		case string:
			updates[key] = typed
		case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			updates[key] = fmt.Sprint(typed)
		}
	}
	return updates
}

func applyPersistUpdates(iniPath string, updates map[string]string) (result []string, returnErr error) {
	info, err := os.Stat(iniPath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("target INI path is not a regular file")
	}
	raw, err := os.ReadFile(iniPath)
	if err != nil {
		return nil, err
	}
	content := string(raw)
	lineEnding := "\n"
	if strings.Contains(content, "\r\n") {
		lineEnding = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inConstants := false
	updated := make([]string, 0, len(updates))
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inConstants = strings.EqualFold(trimmed, "[Constants]")
			continue
		}
		if !inConstants || !strings.HasPrefix(strings.ToLower(trimmed), "global persist $") {
			continue
		}
		match := persistDeclarationRE.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		name := strings.TrimSpace(match[1])
		next, exists := updates[strings.ToLower(name)]
		if !exists || strings.TrimSpace(match[2]) == strings.TrimSpace(next) {
			continue
		}
		lines[index] = "global persist $" + name + " = " + strings.TrimSpace(next)
		updated = append(updated, name)
	}
	if len(updated) == 0 {
		return []string{}, nil
	}
	tempPath := filepath.Join(filepath.Dir(iniPath), fmt.Sprintf(".%s.%d.%d.tmp", filepath.Base(iniPath), os.Getpid(), time.Now().UnixNano()))
	if err := os.WriteFile(tempPath, []byte(strings.Join(lines, lineEnding)), info.Mode().Perm()); err != nil {
		return nil, err
	}
	defer func() {
		cleanupErr := os.Remove(tempPath)
		if !errors.Is(cleanupErr, os.ErrNotExist) {
			returnErr = infra.WithCause(returnErr, infra.AnnotateError(cleanupErr, infra.Diagnostic{Stage: "cleanup"}))
		}
	}()
	if err := replaceAtomic(tempPath, iniPath); err != nil {
		return nil, err
	}
	return updated, nil
}
