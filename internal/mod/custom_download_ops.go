package mod

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/infra"
)

// stagedMove is fse.move({ overwrite: true }). Tests replace it the same way
// Electron spies on fse.move.
var stagedMove = movePathOverwrite

var (
	renameMovePath = os.Rename
	copyMovePath   = copyPathForMove
)

func movePathOverwrite(sourcePath, destinationPath string) error {
	if _, err := os.Stat(destinationPath); err == nil {
		if err := os.RemoveAll(destinationPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := renameMovePath(sourcePath, destinationPath); err != nil {
		if !isCrossDeviceMoveError(err) {
			return err
		}
		if err := copyMovePath(sourcePath, destinationPath); err != nil {
			cleanupErr := os.RemoveAll(destinationPath)
			return errors.Join(err, cleanupErr)
		}
		if err := os.RemoveAll(sourcePath); err != nil {
			return err
		}
	}
	return nil
}

func copyPathForMove(sourcePath, destinationPath string) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(sourcePath)
		if err != nil {
			return err
		}
		return os.Symlink(target, destinationPath)
	}
	if info.IsDir() {
		return copyDirectoryForMove(sourcePath, destinationPath, info)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file type: %s", sourcePath)
	}
	return copyFileForMove(sourcePath, destinationPath, info)
}

func copyDirectoryForMove(sourcePath, destinationPath string, info os.FileInfo) error {
	if err := os.Mkdir(destinationPath, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyPathForMove(
			filepath.Join(sourcePath, entry.Name()),
			filepath.Join(destinationPath, entry.Name()),
		); err != nil {
			return err
		}
	}
	if err := os.Chmod(destinationPath, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(destinationPath, info.ModTime(), info.ModTime())
}

func copyFileForMove(sourcePath, destinationPath string, info os.FileInfo) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		_ = source.Close()
		return err
	}
	_, copyErr := io.Copy(destination, source)
	sourceCloseErr := source.Close()
	destinationCloseErr := destination.Close()
	if err := errors.Join(copyErr, sourceCloseErr, destinationCloseErr); err != nil {
		return err
	}
	if err := os.Chmod(destinationPath, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(destinationPath, info.ModTime(), info.ModTime())
}

type stagedDownloadHandle struct {
	DestinationPaths []string
	commit           func() error
	restore          func() error
}

func (h stagedDownloadHandle) Commit() error {
	if h.commit == nil {
		return nil
	}
	return h.commit()
}

func (h stagedDownloadHandle) Restore() error {
	if h.restore == nil {
		return nil
	}
	return h.restore()
}

type stagedBackup struct {
	destinationPath string
	backupPath      string
	restored        bool
}

func finalizeStagedDownload(stagingPath, destinationDir string) (stagedDownloadHandle, error) {
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return stagedDownloadHandle{}, err
	}
	entries, err := os.ReadDir(stagingPath)
	if err != nil {
		return stagedDownloadHandle{}, err
	}
	if len(entries) == 0 {
		return stagedDownloadHandle{}, errors.New("downloaded file did not produce staged content")
	}

	destinationPaths := make([]string, 0, len(entries))
	backups := make([]stagedBackup, 0, len(entries))

	rollbackPartial := func(err error) error {
		restoreErrors := []error{}
		for _, entry := range backups {
			wasMoved := false
			for _, path := range destinationPaths {
				if path == entry.destinationPath {
					wasMoved = true
					break
				}
			}
			isFailedEntry := !wasMoved && entry.backupPath != ""
			if wasMoved || isFailedEntry {
				if removeErr := os.RemoveAll(entry.destinationPath); removeErr != nil && !os.IsNotExist(removeErr) {
					restoreErrors = append(restoreErrors, infra.AnnotateError(removeErr, infra.Diagnostic{Stage: "rollback-remove"}))
				}
			}
			if entry.backupPath != "" {
				if _, statErr := os.Stat(entry.backupPath); statErr == nil {
					if moveErr := stagedMove(entry.backupPath, entry.destinationPath); moveErr != nil {
						restoreErrors = append(restoreErrors, infra.AnnotateError(moveErr, infra.Diagnostic{Stage: "rollback-restore"}))
					}
				}
			}
		}
		if len(restoreErrors) > 0 {
			return infra.WithCause(fmt.Errorf("%w", err), errors.Join(restoreErrors...))
		}
		return err
	}

	for _, entry := range entries {
		sourcePath := filepath.Join(stagingPath, entry.Name())
		destinationPath := filepath.Join(destinationDir, entry.Name())
		backupPath := ""
		if _, statErr := os.Stat(destinationPath); statErr == nil {
			backupPath = destinationPath + ".nhd-backup-" + uuid.NewString()
			if err := stagedMove(destinationPath, backupPath); err != nil {
				return stagedDownloadHandle{}, rollbackPartial(err)
			}
		} else if !os.IsNotExist(statErr) {
			return stagedDownloadHandle{}, rollbackPartial(statErr)
		}
		backups = append(backups, stagedBackup{destinationPath: destinationPath, backupPath: backupPath})
		if err := stagedMove(sourcePath, destinationPath); err != nil {
			return stagedDownloadHandle{}, rollbackPartial(err)
		}
		destinationPaths = append(destinationPaths, destinationPath)
	}

	committed := false
	return stagedDownloadHandle{
		DestinationPaths: destinationPaths,
		commit: func() error {
			committed = true
			var cleanupErrors []error
			for _, entry := range backups {
				if entry.backupPath != "" {
					cleanupErrors = append(cleanupErrors, infra.AnnotateError(os.RemoveAll(entry.backupPath), infra.Diagnostic{Stage: "commit-cleanup", Fields: map[string]any{"backupPath": entry.backupPath}}))
				}
			}
			return errors.Join(cleanupErrors...)
		},
		restore: func() error {
			if committed {
				return nil
			}
			var restoreErrors []string
			for index := range backups {
				entry := &backups[index]
				if entry.restored {
					continue
				}
				if err := os.RemoveAll(entry.destinationPath); err != nil && !os.IsNotExist(err) {
					restoreErrors = append(restoreErrors, err.Error())
					continue
				}
				if entry.backupPath != "" {
					if _, err := os.Stat(entry.backupPath); err == nil {
						if err := stagedMove(entry.backupPath, entry.destinationPath); err != nil {
							restoreErrors = append(restoreErrors, err.Error())
							continue
						}
					}
				}
				entry.restored = true
			}
			if len(restoreErrors) > 0 {
				return errors.New(strings.Join(restoreErrors, "; "))
			}
			return nil
		},
	}, nil
}

func applySelectedExtractedName(
	extractedPath, stagingPath, requestedFileName, originalSuggestedFileName string,
	sanitize func(string) string,
) (string, error) {
	if requestedFileName == originalSuggestedFileName || extractedPath == stagingPath {
		return extractedPath, nil
	}
	info, err := os.Stat(extractedPath)
	if err != nil {
		return "", err
	}
	desiredName := requestedFileName
	if info.IsDir() {
		desiredName = archiveRootName(requestedFileName, sanitize)
	}
	if desiredName == "" || filepath.Base(extractedPath) == desiredName {
		return extractedPath, nil
	}
	renamedPath := filepath.Join(filepath.Dir(extractedPath), desiredName)
	if err := movePathOverwrite(extractedPath, renamedPath); err != nil {
		return "", err
	}
	return renamedPath, nil
}
