package texture

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func copyRegularFile(source, target string) (returnErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return infra.WithCause(errors.New("backup is not a regular file"), err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".wuwa-rollback-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		cleanupErr := os.Remove(tempPath)
		if !errors.Is(cleanupErr, os.ErrNotExist) {
			returnErr = infra.WithCause(returnErr, infra.AnnotateError(cleanupErr, infra.Diagnostic{Stage: "cleanup"}))
		}
	}()
	if _, err = io.Copy(temp, input); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return platform.ReplaceAtomic(tempPath, target)
}
