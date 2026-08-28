package mod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUniqueMergeDisabledNamePrefersBackupAndNumbersCollisions(t *testing.T) {
	used := map[string]struct{}{"disabled_backup_chara.ini": {}}
	first, err := uniqueMergeDisabledName("CharA.ini", used)
	if err != nil {
		t.Fatal(err)
	}
	if first != "DISABLED_BACKUP_2_CharA.ini" {
		t.Fatalf("first = %q", first)
	}
	second, err := uniqueMergeDisabledName("CharA.ini", used)
	if err != nil {
		t.Fatal(err)
	}
	if second != "DISABLED_BACKUP_3_CharA.ini" {
		t.Fatalf("second = %q", second)
	}
	pack, err := uniqueMergeDisabledName("Pack", used)
	if err != nil {
		t.Fatal(err)
	}
	if pack != "DISABLED Pack" {
		t.Fatalf("pack = %q", pack)
	}
	used["disabled pack"] = struct{}{}
	pack2, err := uniqueMergeDisabledName("Pack", used)
	if err != nil {
		t.Fatal(err)
	}
	if pack2 != "DISABLED Pack (2)" {
		t.Fatalf("pack2 = %q", pack2)
	}
}

func TestEnsureMergeBackupIgnoresUnrelatedDisabledFile(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "CharA.ini")
	disabled := filepath.Join(root, "DISABLEDCharA.ini")
	backup := filepath.Join(root, "DISABLED_BACKUP_CharA.ini")
	if err := os.WriteFile(active, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disabled, []byte("user-disabled"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := []mergeRollback{}
	if err := ensureMergeBackup(active, &created); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(disabled)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user-disabled" {
		t.Fatalf("disabled = %s", got)
	}
	backupText, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(backupText) != "original" {
		t.Fatalf("backup = %s", backupText)
	}
	if err := ensureMergeBackup(active, &created); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	backups := 0
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Name()), "backup") {
			backups++
		}
	}
	if backups != 1 {
		t.Fatalf("backup count = %d", backups)
	}
}

func TestEnsureMergeBackupRejectsNonNumericBackupPrefix(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	active := filepath.Join(root, "CharA.ini")
	if err := os.WriteFile(active, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DISABLED_BACKUP_OLD_CharA.ini"), []byte("unrelated"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := []mergeRollback{}
	if err := ensureMergeBackup(active, &created); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "DISABLED_BACKUP_CharA.ini")); err != nil {
		t.Fatalf("exact backup was not created: %v", err)
	}
}
