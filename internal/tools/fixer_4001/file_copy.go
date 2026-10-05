package fixer4001

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type fileCopy struct {
	Source string
	Target string
}

type elevatedFileCopyError struct{ err error }

func (e elevatedFileCopyError) Error() string { return e.err.Error() }

func (e elevatedFileCopyError) Unwrap() error { return e.err }

// installFileCopies and removeFilePaths take the lease of the user action they belong to, so every
// change of that action that needs administrator rights shares one UAC prompt.
func installFileCopies(ctx context.Context, lease *elevated.FileLease, copies []fileCopy, useElevated bool) error {
	if len(copies) == 0 {
		return nil
	}
	if useElevated {
		if err := elevatedCopyFiles(ctx, lease, copies); err != nil {
			return elevatedFileCopyError{err: err}
		}
		return nil
	}
	for _, item := range copies {
		if err := copyFileOverwrite(item.Source, item.Target); err != nil {
			if errors.Is(err, os.ErrPermission) {
				if elevatedErr := elevatedCopyFiles(ctx, lease, copies); elevatedErr != nil {
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
	return platform.ReplaceAtomic(tempPath, target)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func removeFilePaths(ctx context.Context, lease *elevated.FileLease, paths []string, useElevated bool) error {
	if len(paths) == 0 {
		return nil
	}
	if useElevated {
		return elevatedRemoveFiles(ctx, lease, paths)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if errors.Is(err, os.ErrPermission) {
				return elevatedRemoveFiles(ctx, lease, paths)
			}
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}
