package platform

import (
	"path/filepath"
	"strings"
)

// SamePathFold reports whether two paths point at the same file, comparing
// case-insensitively because Windows path comparisons are case-insensitive.
func SamePathFold(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

// SameOrChildPath reports whether target is root itself or lives below it.
// The comparison is lexical, so it does not resolve symlinks.
func SameOrChildPath(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	rootAbs, rootErr := filepath.Abs(root)
	targetAbs, targetErr := filepath.Abs(target)
	if rootErr != nil || targetErr != nil {
		return false
	}
	relative, err := filepath.Rel(rootAbs, targetAbs)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}
