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
var removeBackup = os.Remove

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
func Write(modPath string, data []byte, reports ...func(error)) error {
	return WriteBatch([]WriteEntry{{Dir: modPath, Data: data}}, reports...)
}

// Update reads and replaces nhd.json under one queue reservation, preventing lost read-modify-write updates.
// The callback must only transform the supplied contents; it must not call metadata operations on the same path.
func Update(modPath string, change func([]byte) ([]byte, error), reports ...func(error)) error {
	return update(modPath, change, false, reports...)
}

// Upsert reads and replaces nhd.json under one queue reservation, passing nil to change when absent.
// The callback must not call metadata operations on the same directory.
func Upsert(modPath string, change func([]byte) ([]byte, error), reports ...func(error)) error {
	return update(modPath, change, true, reports...)
}

func update(
	modPath string,
	change func([]byte) ([]byte, error),
	allowMissing bool,
	reports ...func(error),
) error {
	release, err := reserve(modPath)
	if err != nil {
		return err
	}
	defer release()

	current, err := os.ReadFile(filepath.Join(modPath, fileName))
	if err != nil && (!allowMissing || !errors.Is(err, os.ErrNotExist)) {
		return err
	}
	next, err := change(current)
	if err != nil {
		return err
	}
	return writeBatch([]WriteEntry{{Dir: modPath, Data: next}}, reports...)
}

// WriteBatch writes complete JSON documents to multiple mod directories and restores all files on failure.
// Each directory must appear only once. Callers must validate paths at their service boundary. reports
// receives cleanup failures that do not fail the write, such as a backup left after a committed batch.
func WriteBatch(entries []WriteEntry, reports ...func(error)) error {
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		dirs = append(dirs, entry.Dir)
	}
	release, err := reserve(dirs...)
	if err != nil {
		return err
	}
	defer release()

	return writeBatch(entries, reports...)
}

// UpdateBatch transforms existing nhd.json files while holding all directory reservations.
// Missing files are skipped; returning changed=false leaves a file untouched. The callback must not
// call metadata operations on any of the reserved directories.
func UpdateBatch(dirs []string, change func(string, []byte) ([]byte, bool, error), reports ...func(error)) error {
	release, err := reserve(dirs...)
	if err != nil {
		return err
	}
	defer release()

	entries := make([]WriteEntry, 0, len(dirs))
	for _, dir := range dirs {
		current, err := os.ReadFile(filepath.Join(dir, fileName))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		next, changed, err := change(dir, current)
		if err != nil {
			return err
		}
		if changed {
			entries = append(entries, WriteEntry{Dir: dir, Data: next})
		}
	}
	return writeBatch(entries, reports...)
}

// writeBatch runs with every affected directory reserved by the caller.
func writeBatch(entries []WriteEntry, reports ...func(error)) error {
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
		var err error
		if file.backupPath != "" {
			// A read-only nhd.json would refuse the replacement. The backup link shares the attribute, so a
			// rollback restores a writable file.
			err = os.Chmod(file.metadataPath, 0o644)
		}
		if err == nil {
			err = platform.WriteFileAtomic(file.metadataPath, entries[i].Data, 0o644, reports...)
		}
		if err == nil {
			err = hideFile(file.metadataPath)
		}
		if err != nil {
			return restoreFiles(backups, err)
		}
	}

	for _, file := range backups {
		if file.backupPath == "" {
			continue
		}
		if err := removeBackup(file.backupPath); err != nil && !os.IsNotExist(err) {
			// The new metadata has committed; a cleanup failure must not trigger the caller's rollback.
			for _, report := range reports {
				if report == nil {
					continue
				}
				report(fmt.Errorf("remove committed mod metadata backup %s: %w", file.backupPath, err))
			}
		}
	}
	return nil
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

// backupFiles keeps the current content of each nhd.json under a backup name while leaving nhd.json in
// place, so a crash during the batch never leaves a mod without its metadata.
func backupFiles(dirs []string) ([]backup, error) {
	backups := make([]backup, 0, len(dirs))
	for _, dir := range dirs {
		metadataPath := filepath.Join(dir, fileName)
		file := backup{metadataPath: metadataPath}
		if _, err := os.Stat(metadataPath); err == nil {
			file.backupPath = metadataPath + ".backup-" + uuid.NewString()
			if err := copyForBackup(metadataPath, file.backupPath); err != nil {
				return nil, removeBackups(backups, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, removeBackups(backups, err)
		}
		backups = append(backups, file)
	}
	return backups, nil
}

// copyForBackup links the backup to the current file, which costs no copy. Replacing nhd.json later
// swaps the directory entry, so the link keeps the old content. Volumes without hard links get a copy.
func copyForBackup(source, backupPath string) error {
	if err := os.Link(source, backupPath); err == nil {
		return nil
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	// The backup replaces nhd.json on rollback, so it must carry the hidden attribute a link would share.
	err = os.WriteFile(backupPath, data, 0o644)
	if err == nil {
		err = hideFile(backupPath)
	}
	if err != nil {
		if removeErr := os.Remove(backupPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return errors.Join(err, removeErr)
		}
	}
	return err
}

func removeBackups(backups []backup, cause error) error {
	result := cause
	for _, file := range backups {
		if file.backupPath == "" {
			continue
		}
		if err := os.Remove(file.backupPath); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, fmt.Errorf("remove backup %s: %w", file.backupPath, err))
		}
	}
	return result
}

func restoreFiles(backups []backup, cause error) error {
	result := cause
	for _, file := range backups {
		if file.backupPath == "" {
			if err := os.Remove(file.metadataPath); err != nil && !os.IsNotExist(err) {
				result = errors.Join(result, fmt.Errorf("remove incomplete %s: %w", file.metadataPath, err))
			}
			continue
		}
		if err := platform.ReplaceAtomic(file.backupPath, file.metadataPath); err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", file.metadataPath, err))
		}
	}
	return result
}
