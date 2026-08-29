//go:build windows

package mod

import (
	"nahida.live/desktop/internal/platform"
)

func newLocaleLess() func(string, string) bool {
	return platform.NewLocaleLess()
}

func newLocaleLessFor(locale string) func(string, string) bool {
	return platform.NewLocaleLessFor(locale)
}
