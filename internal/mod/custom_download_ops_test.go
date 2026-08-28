package mod

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalizeStagedDownloadPreservesSuccessfullyRestoredEntriesWhenALaterRestoreFailsAndIsRetried(t *testing.T) {
	root := t.TempDir()
	stagingPath := filepath.Join(root, "staging")
	destinationDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(stagingPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destinationDir, "a.txt"), []byte("original-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destinationDir, "b.txt"), []byte("original-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, "a.txt"), []byte("new-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, "b.txt"), []byte("new-b"), 0o600); err != nil {
		t.Fatal(err)
	}

	finalized, err := finalizeStagedDownload(stagingPath, destinationDir)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(destinationDir, "a.txt")); string(raw) != "new-a" {
		t.Fatalf("a.txt = %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(destinationDir, "b.txt")); string(raw) != "new-b" {
		t.Fatalf("b.txt = %q", raw)
	}

	realMove := stagedMove
	backupRestores := 0
	t.Cleanup(func() { stagedMove = realMove })
	stagedMove = func(src, dest string) error {
		if strings.Contains(src, ".nhd-backup-") && filepath.Base(dest) == "b.txt" {
			backupRestores++
			if backupRestores == 1 {
				return errors.New("simulated restore failure for b.txt")
			}
		}
		return realMove(src, dest)
	}

	if err := finalized.Restore(); err == nil || !strings.Contains(err.Error(), "simulated restore failure for b.txt") {
		t.Fatalf("first restore error = %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(destinationDir, "a.txt")); string(raw) != "original-a" {
		t.Fatalf("restored a.txt = %q", raw)
	}
	if _, err := os.Stat(filepath.Join(destinationDir, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("b.txt should be missing, stat = %v", err)
	}

	if err := finalized.Restore(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(destinationDir, "a.txt")); string(raw) != "original-a" {
		t.Fatalf("final a.txt = %q", raw)
	}
	if raw, _ := os.ReadFile(filepath.Join(destinationDir, "b.txt")); string(raw) != "original-b" {
		t.Fatalf("final b.txt = %q", raw)
	}
}

func TestFinalizeStagedDownloadNoOpsRestoreAfterCommit(t *testing.T) {
	root := t.TempDir()
	stagingPath := filepath.Join(root, "staging")
	destinationDir := filepath.Join(root, "dest")
	if err := os.MkdirAll(stagingPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destinationDir, "a.txt"), []byte("original-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingPath, "a.txt"), []byte("new-a"), 0o600); err != nil {
		t.Fatal(err)
	}

	finalized, err := finalizeStagedDownload(stagingPath, destinationDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := finalized.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := finalized.Restore(); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(destinationDir, "a.txt")); string(raw) != "new-a" {
		t.Fatalf("committed a.txt = %q", raw)
	}
}

func TestParseDownloadFileName(t *testing.T) {
	sanitize := func(name string) string { return name }
	tests := []struct {
		name        string
		rawURL      string
		disposition string
		want        string
	}{
		{name: "quoted filename", rawURL: "https://example.test/fallback.bin", disposition: `attachment; filename="mod.zip"`, want: "mod.zip"},
		{name: "unquoted filename", rawURL: "https://example.test/fallback.bin", disposition: `attachment; filename=mod.zip`, want: "mod.zip"},
		{name: "filename star", rawURL: "https://example.test/fallback.bin", disposition: `attachment; filename*=UTF-8''%E6%A8%A1.zip`, want: "模.zip"},
		{name: "url fallback", rawURL: "https://example.test/path/mod.zip", want: "mod.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDownloadFileName(tt.rawURL, sanitize, tt.disposition)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
