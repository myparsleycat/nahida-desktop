package modmesh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveResourceRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-body.buf")
	if err := os.WriteFile(outside, make([]byte, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if _, err := ResolveResource(root, "../outside-body.buf"); err == nil {
		t.Fatal("resource traversal was accepted")
	}
}
