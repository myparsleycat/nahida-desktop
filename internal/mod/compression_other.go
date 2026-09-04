//go:build !windows

package mod

import (
	"context"
	"errors"
	"io/fs"
)

var errXpress4KUnsupported = errors.New("XPRESS4K compression is only supported on Windows")

func applyXpress4K(
	context.Context,
	[]string,
	func(int, int64),
	func(string, int64, bool),
	*compressionFileOwnership,
	compressionMutationMarker,
	func(string, error),
) error {
	return errXpress4KUnsupported
}

func restoreWOF(
	context.Context,
	[]string,
	func(int, int64),
	func(string, int64, bool),
	*compressionFileOwnership,
	compressionMutationMarker,
	func(string, error),
) error {
	return errXpress4KUnsupported
}

func isReparsePoint(fs.FileInfo) bool { return false }
