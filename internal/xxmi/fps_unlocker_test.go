package xxmi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceCorruptFPSCacheBacksUpAndRestores(t *testing.T) {
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
			destination := filepath.Join(root, "packages", "gi-fps-unlocker", "1.2.3")
			if err := os.MkdirAll(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, "old.txt"), []byte("old"), 0o600); err != nil {
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
			err := replaceCorruptFPSCache(root, staging, destination, "1.2.3")
			if (err == nil) != tc.newExists {
				t.Fatalf("replacement error = %v", err)
			}
			name := "old.txt"
			if tc.newExists {
				name = "new.txt"
			}
			if _, err := os.Stat(filepath.Join(destination, name)); err != nil {
				t.Fatalf("destination %s: %v", name, err)
			}
			backups, err := filepath.Glob(filepath.Join(root, "backups", "gi-fps-unlocker-1.2.3-corrupt-*"))
			if err != nil || (len(backups) == 1) != tc.wantBackup {
				t.Fatalf("backups = %v, err = %v", backups, err)
			}
			if tc.wantBackup {
				if _, err := os.Stat(filepath.Join(backups[0], "old.txt")); err != nil {
					t.Fatalf("backup lost old cache: %v", err)
				}
			}
		})
	}
}

func TestImportExternalFPSUnlockerRejectsDotVersion(t *testing.T) {
	t.Parallel()
	external := t.TempDir()
	source := filepath.Join(external, "Resources", "Packages", "GI-FPS-Unlocker")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Manifest.json"), []byte(`{"version":".."}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := importExternalFPSUnlocker(external); err == nil {
		t.Fatal("external GI FPS Unlocker accepted a dot version")
	}
}
