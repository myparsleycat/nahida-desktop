package metadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/platform"
)

const fileName = "nhd.json"

var hideFile = platform.HideFile

// WriteEntry is one mod directory and its complete nhd.json contents.
type WriteEntry struct {
	Dir  string
	Data []byte
}

type backup struct {
	metadataPath string
	backupPath   string
}

// Initialize creates nhd.json only if it does not already exist.
// The caller supplies the complete JSON contents.
func Initialize(modPath string, data []byte) error {
	if !json.Valid(data) {
		return errors.New("invalid mod metadata JSON")
	}
	release, err := reserve(modPath)
	if err != nil {
		return err
	}
	defer release()

	if err := requireDirectory(modPath); err != nil {
		return err
	}
	path := filepath.Join(modPath, fileName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	if err := hideFile(path); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

// Read reads a mod directory's nhd.json. A missing file returns an error matching os.ErrNotExist.
// Callers decode the JSON into the fields they need.
func Read(modPath string) ([]byte, error) {
	release, err := reserve(modPath)
	if err != nil {
		return nil, err
	}
	defer release()

	return os.ReadFile(filepath.Join(modPath, fileName))
}

// Write replaces nhd.json with the caller's complete JSON contents.
// Callers must validate the mod path at their service boundary.
func Write(modPath string, data []byte) error {
	return WriteBatch([]WriteEntry{{Dir: modPath, Data: data}})
}

// Update reads and replaces nhd.json under one queue reservation, preventing lost read-modify-write updates.
// The callback must only transform the supplied contents; it must not call metadata operations on the same path.
func Update(modPath string, change func([]byte) ([]byte, error)) error {
	if change == nil {
		return errors.New("mod metadata update callback is nil")
	}
	release, err := reserve(modPath)
	if err != nil {
		return err
	}
	defer release()

	current, err := os.ReadFile(filepath.Join(modPath, fileName))
	if err != nil {
		return err
	}
	next, err := change(current)
	if err != nil {
		return err
	}
	return writeBatch([]WriteEntry{{Dir: modPath, Data: next}})
}

// WriteBatch writes complete JSON documents to multiple mod directories and restores all files on failure.
// Each directory must appear only once. Callers must validate paths at their service boundary.
func WriteBatch(entries []WriteEntry) error {
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		dirs = append(dirs, entry.Dir)
	}
	release, err := reserve(dirs...)
	if err != nil {
		return err
	}
	defer release()

	return writeBatch(entries)
}

// writeBatch runs with every affected directory reserved by the caller.
func writeBatch(entries []WriteEntry) error {
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !json.Valid(entry.Data) {
			return errors.New("invalid mod metadata JSON")
		}
		if err := requireDirectory(entry.Dir); err != nil {
			return err
		}
		dirs = append(dirs, entry.Dir)
	}

	backups, err := backupFiles(dirs)
	if err != nil {
		return err
	}
	for i, file := range backups {
		err := os.WriteFile(file.metadataPath, entries[i].Data, 0o644)
		if err == nil {
			err = hideFile(file.metadataPath)
		}
		if err != nil {
			return restoreFiles(backups, err)
		}
	}

	var cleanupErr error
	for _, file := range backups {
		if file.backupPath == "" {
			continue
		}
		if err := os.Remove(file.backupPath); err != nil && !os.IsNotExist(err) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func requireDirectory(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("mod path is not a directory: %s", dir)
	}
	return nil
}

func backupFiles(dirs []string) ([]backup, error) {
	backups := make([]backup, 0, len(dirs))
	for _, dir := range dirs {
		metadataPath := filepath.Join(dir, fileName)
		file := backup{metadataPath: metadataPath}
		if _, err := os.Stat(metadataPath); err == nil {
			file.backupPath = metadataPath + ".backup-" + uuid.NewString()
			if err := os.Rename(metadataPath, file.backupPath); err != nil {
				return nil, restorePreparedFiles(backups, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, restorePreparedFiles(backups, err)
		}
		backups = append(backups, file)
	}
	return backups, nil
}

func restorePreparedFiles(backups []backup, cause error) error {
	result := cause
	for i := len(backups) - 1; i >= 0; i-- {
		file := backups[i]
		if file.backupPath == "" {
			continue
		}
		if err := os.Rename(file.backupPath, file.metadataPath); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", file.metadataPath, err))
		}
	}
	return result
}

func restoreFiles(backups []backup, cause error) error {
	result := cause
	for _, file := range backups {
		if err := os.Remove(file.metadataPath); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, fmt.Errorf("remove incomplete %s: %w", file.metadataPath, err))
			continue
		}
		if file.backupPath == "" {
			continue
		}
		if err := os.Rename(file.backupPath, file.metadataPath); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", file.metadataPath, err))
		}
	}
	return result
}
