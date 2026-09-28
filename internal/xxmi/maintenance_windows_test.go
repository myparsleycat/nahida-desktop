//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRepairLibsCacheRestoresCorruptCacheWhenDownloadFails(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "packages", "xxmi-libs", "1.0.0")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(folder, "Manifest.json")
	if err := os.WriteFile(manifest, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New().repairLibsCache(context.Background(), "1.0.0"); err == nil {
		t.Fatal("expected download failure")
	}
	if data, err := os.ReadFile(manifest); err != nil || string(data) != "corrupt" {
		t.Fatalf("original cache was not restored: data = %q, err = %v", data, err)
	}
}
