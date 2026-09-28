package xxmi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceCorruptLibsCacheBacksUpAndRestores(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		newExists  bool
		wantBackup bool
	}{
		{"replacement", true, true},
		{"rollback", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			destination := filepath.Join(root, "packages", "xxmi-libs", "1.2.3")
			if err := os.MkdirAll(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, "Manifest.json"), []byte("corrupt"), 0o600); err != nil {
				t.Fatal(err)
			}
			staging := filepath.Join(root, "staging")
			if tc.newExists {
				if err := os.Mkdir(staging, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(staging, "new.txt"), []byte("new"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := replaceCorruptLibsCache(root, staging, destination, "1.2.3")
			if (err == nil) != tc.newExists {
				t.Fatalf("replacement error = %v", err)
			}
			name := "Manifest.json"
			if tc.newExists {
				name = "new.txt"
			}
			if _, err := os.Stat(filepath.Join(destination, name)); err != nil {
				t.Fatalf("destination %s: %v", name, err)
			}
			backups, err := filepath.Glob(filepath.Join(root, "backups", "xxmi-libs-1.2.3-corrupt-*"))
			if err != nil || (len(backups) == 1) != tc.wantBackup {
				t.Fatalf("backups = %v, err = %v", backups, err)
			}
			if tc.wantBackup {
				if data, err := os.ReadFile(filepath.Join(backups[0], "Manifest.json")); err != nil ||
					string(data) != "corrupt" {
					t.Fatalf("backup lost old cache: data = %q, err = %v", data, err)
				}
			}
		})
	}
}
