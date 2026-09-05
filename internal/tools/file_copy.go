package tools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/infra"
)

type fileCopy struct {
	Source string
	Target string
}

type elevatedFileCopyError struct{ err error }

func (e elevatedFileCopyError) Error() string { return e.err.Error() }
func (e elevatedFileCopyError) Unwrap() error { return e.err }

func installFileCopies(copies []fileCopy, elevated bool) error {
	if len(copies) == 0 {
		return nil
	}
	if elevated {
		if err := elevatedCopyFiles(copies); err != nil {
			return elevatedFileCopyError{err: err}
		}
		return nil
	}
	for _, item := range copies {
		if err := copyFileOverwrite(item.Source, item.Target); err != nil {
			if errors.Is(err, os.ErrPermission) {
				if elevatedErr := elevatedCopyFiles(copies); elevatedErr != nil {
					return elevatedFileCopyError{err: elevatedErr}
				}
				return nil
			}
			return err
		}
	}
	return nil
}

func copyFileOverwrite(source, target string) (returnErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(target), ".nhd-copy-*")
	if err != nil {
		return err
	}
	tempPath := output.Name()
	defer func() {
		cleanupErr := os.Remove(tempPath)
		if !errors.Is(cleanupErr, os.ErrNotExist) {
			returnErr = infra.WithCause(returnErr, infra.AnnotateError(cleanupErr, infra.Diagnostic{Stage: "cleanup"}))
		}
	}()
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceAtomic(tempPath, target)
}

func removeFilePaths(paths []string, elevated bool) error {
	if len(paths) == 0 {
		return nil
	}
	if elevated {
		return elevatedRemoveFiles(paths)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, os.ErrPermission) {
				return elevatedRemoveFiles(paths)
			}
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}
