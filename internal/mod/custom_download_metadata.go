package mod

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/platform"
)

const modDownloadMetadataFileName = "nhd.json"

var hideModDownloadMetadataFile = platform.HideFile

type modDownloadMetadataBackup struct {
	metadataPath string
	backupPath   string
}

func writeModDownloadMetadataToDirectories(paths []string, metadata map[string]any) error {
	dirs := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		dir := path
		if !info.IsDir() {
			dir = filepath.Dir(path)
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	return writeModDownloadMetadataFiles(dirs, metadata)
}

func writeModDownloadMetadataFiles(dirs []string, metadata map[string]any) error {
	backups, err := backupModDownloadMetadata(dirs)
	if err != nil {
		return err
	}

	for _, backup := range backups {
		data := map[string]any{"id": uuid.NewString()}
		for key, value := range metadata {
			data[key] = value
		}
		raw, err := json.MarshalIndent(data, "", "  ")
		if err == nil {
			err = os.WriteFile(backup.metadataPath, append(raw, '\n'), 0o644)
		}
		if err == nil {
			err = hideModDownloadMetadataFile(backup.metadataPath)
		}
		if err != nil {
			return restoreModDownloadMetadata(backups, err)
		}
	}

	var cleanupErr error
	for _, backup := range backups {
		if backup.backupPath == "" {
			continue
		}
		if err := os.Remove(backup.backupPath); err != nil && !os.IsNotExist(err) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func backupModDownloadMetadata(dirs []string) ([]modDownloadMetadataBackup, error) {
	backups := make([]modDownloadMetadataBackup, 0, len(dirs))
	for _, dir := range dirs {
		metadataPath := filepath.Join(dir, modDownloadMetadataFileName)
		backup := modDownloadMetadataBackup{metadataPath: metadataPath}
		if _, err := os.Stat(metadataPath); err == nil {
			backup.backupPath = metadataPath + ".backup-" + uuid.NewString()
			if err := os.Rename(metadataPath, backup.backupPath); err != nil {
				return nil, restorePreparedModDownloadMetadata(backups, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, restorePreparedModDownloadMetadata(backups, err)
		}
		backups = append(backups, backup)
	}
	return backups, nil
}

func restorePreparedModDownloadMetadata(backups []modDownloadMetadataBackup, cause error) error {
	result := cause
	for i := len(backups) - 1; i >= 0; i-- {
		backup := backups[i]
		if backup.backupPath == "" {
			continue
		}
		if err := os.Rename(backup.backupPath, backup.metadataPath); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", backup.metadataPath, err))
		}
	}
	return result
}

func restoreModDownloadMetadata(backups []modDownloadMetadataBackup, cause error) error {
	result := cause
	for _, backup := range backups {
		if err := os.Remove(backup.metadataPath); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, fmt.Errorf("remove incomplete %s: %w", backup.metadataPath, err))
			continue
		}
		if backup.backupPath == "" {
			continue
		}
		if err := os.Rename(backup.backupPath, backup.metadataPath); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", backup.metadataPath, err))
		}
	}
	return result
}
