package platform

import (
	"path/filepath"
	"strings"
)

// SamePathFold reports whether two paths point at the same file, comparing
// case-insensitively because Windows path comparisons are case-insensitive.
func SamePathFold(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

// SameOrChildPath reports whether target is root itself or lives below it.
// The comparison is lexical, so it does not resolve symlinks.
func SameOrChildPath(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}
