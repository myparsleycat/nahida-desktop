package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathComparison(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "Mods")
	child := filepath.Join(root, "Nested", "file.ini")
	sibling := root + "-other"
	if !SamePathFold(root, strings.ToUpper(root)) {
		t.Fatal("same path with different case was not recognized")
	}
	if SamePathFold("", "") {
		t.Fatal("empty paths must not compare equal")
	}
	if !SameOrChildPath(root, child) || !SameOrChildPath(root, root) {
		t.Fatal("root or descendant was not recognized")
	}
	if !SameOrChildPath(strings.ToUpper(root), child) {
		t.Fatal("descendant with different path case was not recognized")
	}
	if SameOrChildPath(root, sibling) || SameOrChildPath(root, filepath.Dir(root)) {
		t.Fatal("sibling or parent was treated as a descendant")
	}
	if SameOrChildPath("", child) || SameOrChildPath(root, "") {
		t.Fatal("empty path was treated as a descendant")
	}
}
