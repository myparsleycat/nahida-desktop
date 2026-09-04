package mod

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/xxmi"
)

type compressionImporterSource []xxmi.EnabledImporter

func (source compressionImporterSource) GetEnabledImporters(context.Context) ([]xxmi.EnabledImporter, error) {
	return source, nil
}

func TestCompressionRootsNormalizeDeduplicateAndSkipMissing(t *testing.T) {
	base := t.TempDir()
	importer := filepath.Join(base, "Importer")
	mods := filepath.Join(importer, "Mods")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewWithOptions(Options{XXMI: compressionImporterSource{
		{Key: "A", ImporterFolder: importer},
		{Key: "B", ImporterFolder: filepath.Join(importer, ".")},
		{Key: "Missing", ImporterFolder: filepath.Join(base, "Missing")},
	}})
	roots, err := m.compression.roots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] != mods {
		t.Fatalf("roots = %v, want [%s]", roots, mods)
	}
}

func TestCompressionRootsResolveConfiguredSymlinkAndSkipNestedLinks(t *testing.T) {
	base := t.TempDir()
	importer := filepath.Join(base, "Importer")
	target := filepath.Join(base, "Library", "Mods")
	outside := filepath.Join(base, "Outside")
	for _, path := range []string{importer, target, outside} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "payload.bin"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "outside.bin"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(importer, "Mods")); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(target, "NestedLink")); err != nil {
		t.Skipf("nested directory symlink unavailable: %v", err)
	}

	m := NewWithOptions(Options{XXMI: compressionImporterSource{
		{Key: "Linked", ImporterFolder: importer},
		{Key: "Direct", ImporterFolder: filepath.Dir(target)},
	}})
	roots, err := m.compression.roots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || !samePath(roots[0], target) {
		t.Fatalf("roots = %v, want resolved target %s", roots, target)
	}

	files, err := walkCompressionFiles(roots, func(string, os.FileInfo) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0].path) != "payload.bin" {
		t.Fatalf("files = %#v; nested symlink target must be excluded", files)
	}
}

func TestDisabledModFoldersAndZstdThreshold(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Mods")
	disabled := filepath.Join(root, "DISABLED Test")
	enabled := filepath.Join(root, "Enabled")
	for _, path := range []string{disabled, enabled} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(disabled, "edge.bin"), bytes.Repeat([]byte("a"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabled, "small.bin"), bytes.Repeat([]byte("b"), 1023), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(enabled, "large.bin"), bytes.Repeat([]byte("c"), 2048), 0o644); err != nil {
		t.Fatal(err)
	}

	folders, err := disabledModFolders([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0] != disabled {
		t.Fatalf("folders = %v", folders)
	}
	files, err := filesForZstd(disabled, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0].path) != "edge.bin" {
		t.Fatalf("files = %#v", files)
	}
}

func TestZstdRoundTripRestoresMetadataWithoutManifest(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Test")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	want := bytes.Repeat([]byte("nahida-live-zstd"), 32*1024)
	if err := os.WriteFile(path, want, 0o640); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_700_000_000, 123_000_000)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := compressZstdFile(context.Background(), path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(folder, legacyCompressionManifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest was created: %v", err)
	}
	if err := restoreZstdFolder(context.Background(), folder, ignoreCompressionMutations, ignoreCompressionFileErrors); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored data differs: err=%v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != originalInfo.Mode().Perm() || !info.ModTime().Equal(mtime) {
		t.Fatalf("metadata = %v %v", info.Mode().Perm(), info.ModTime())
	}
	if _, err := os.Stat(path + managedZstdExtension); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("compressed file should be removed, err=%v", err)
	}
}

func TestZstdKeepsOriginalWhenCompressionIsNotBeneficial(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Random")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "random.bin")
	data := make([]byte, 256*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + managedZstdExtension); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected zstd output, err=%v", err)
	}
}

func TestZstdCompressionUsesSourceWhenDestinationExists(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Collision")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	want := bytes.Repeat([]byte("source-wins"), 8192)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+managedZstdExtension, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if err := restoreZstdFile(context.Background(), path+managedZstdExtension, maxZstdRestoreSize, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("source did not replace stale target: err=%v", err)
	}
}

func TestZstdRestoreUsesExistingSourceAndRemovesArchive(t *testing.T) {
	folder := t.TempDir()
	sourcePath := filepath.Join(folder, "payload.bin")
	targetPath := sourcePath + managedZstdExtension
	want := []byte("source-is-canonical")
	if err := os.WriteFile(sourcePath, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("not-even-zstd"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreZstdFile(context.Background(), targetPath, maxZstdRestoreSize, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(sourcePath)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("existing source changed: err=%v", err)
	}
	if _, err := os.Stat(targetPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive remains: %v", err)
	}
}

func TestZstdPassRemovesLegacyManifestAndTemps(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Legacy")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	artifacts := []string{
		filepath.Join(folder, legacyCompressionManifestName),
		filepath.Join(folder, legacyCompressionManifestName+compressionTempMarker+"123456789"),
		filepath.Join(folder, "payload.bin"+managedZstdExtension+compressionTempMarker),
		filepath.Join(folder, "payload.bin"+compressionTempMarker),
	}
	for _, path := range artifacts {
		if err := os.WriteFile(path, []byte("obsolete"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := restoreZstdFolder(context.Background(), folder, ignoreCompressionMutations, ignoreCompressionFileErrors); err != nil {
		t.Fatal(err)
	}
	for _, path := range artifacts {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy artifact remains: %s, err=%v", path, err)
		}
	}
}

func TestZstdRestoreLimitPreservesArchiveAndCleansTemp(t *testing.T) {
	folder := t.TempDir()
	sourcePath := filepath.Join(folder, "payload.bin")
	targetPath := sourcePath + managedZstdExtension
	if err := os.WriteFile(sourcePath, bytes.Repeat([]byte("too-large"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := streamCompressZstd(context.Background(), sourcePath, targetPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}
	tempPath := sourcePath + compressionTempMarker
	err := restoreZstdFile(context.Background(), targetPath, 1024, ignoreCompressionMutations)
	if err == nil || !strings.Contains(err.Error(), "exceeds restore limit") {
		t.Fatalf("restore error = %v", err)
	}
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("archive was removed: %v", err)
	}
	if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary output remains: %v", err)
	}
}

func TestCorruptZstdIsPreservedAndRestoreContinues(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "Enabled")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	validPath := filepath.Join(folder, "valid.bin")
	corruptPath := filepath.Join(folder, "corrupt.bin")
	validData := bytes.Repeat([]byte("valid"), 16*1024)
	if err := os.WriteFile(validPath, validData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), validPath, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corruptPath+managedZstdExtension, []byte("not-zstd"), 0o644); err != nil {
		t.Fatal(err)
	}
	var fileErrors int
	if err := restoreZstdFolder(context.Background(), folder, ignoreCompressionMutations, func(string, error) {
		fileErrors++
	}); err != nil {
		t.Fatal(err)
	}
	if fileErrors != 1 {
		t.Fatalf("file errors = %d, want 1", fileErrors)
	}
	got, err := os.ReadFile(validPath)
	if err != nil || !bytes.Equal(got, validData) {
		t.Fatalf("valid sibling was not restored: err=%v", err)
	}
	if _, err := os.Stat(corruptPath + managedZstdExtension); err != nil {
		t.Fatalf("corrupt archive was not preserved: %v", err)
	}
	if _, err := os.Stat(corruptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt output unexpectedly exists: %v", err)
	}
}

func TestRestoreEnabledZstdSkipsDisabledFolders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Mods")
	disabled := filepath.Join(root, "DISABLED Test")
	enabled := filepath.Join(root, "Enabled")
	for _, folder := range []string{disabled, enabled} {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(folder, "payload.bin")
		if err := os.WriteFile(path, bytes.Repeat([]byte(filepath.Base(folder)), 16*1024), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := compressZstdFile(context.Background(), path, ignoreCompressionMutations); err != nil {
			t.Fatal(err)
		}
	}
	if err := restoreEnabledZstd(context.Background(), []string{root}, func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations, ignoreCompressionFileErrors); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(enabled, "payload.bin")); err != nil {
		t.Fatalf("enabled archive was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(disabled, "payload.bin"+managedZstdExtension)); err != nil {
		t.Fatalf("disabled archive was unexpectedly restored: %v", err)
	}
}

func TestEveryNZstdFileIsTreatedAsManaged(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "Enabled")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(folder, "external.dat")
	targetPath := sourcePath + managedZstdExtension
	want := bytes.Repeat([]byte("external-zstd"), 4096)
	if err := os.WriteFile(sourcePath, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := streamCompressZstd(context.Background(), sourcePath, targetPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}
	if err := restoreAllZstd(context.Background(), []string{folder}, func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations, ignoreCompressionFileErrors); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(sourcePath)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("untracked zstd was not restored: err=%v", err)
	}
}

func TestOrdinaryZstdArchiveIsIgnored(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Archive")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(folder, "external.dat.zst")
	if err := os.WriteFile(archivePath, []byte("ordinary-zstd"), 0o644); err != nil {
		t.Fatal(err)
	}
	var fileErrors int
	if err := restoreAllZstd(context.Background(), []string{folder}, func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations, func(string, error) {
		fileErrors++
	}); err != nil {
		t.Fatal(err)
	}
	if fileErrors != 0 {
		t.Fatalf("ordinary .zst was processed: errors=%d", fileErrors)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("ordinary .zst was removed: %v", err)
	}
	files, err := filesForZstd(folder, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("ordinary .zst became a compression candidate: %#v", files)
	}
}

func TestEnableContinuesAfterIndividualZstdRestoreFailure(t *testing.T) {
	root := t.TempDir()
	disabled := filepath.Join(root, "DISABLED Unsafe")
	if err := os.MkdirAll(disabled, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabled, "payload.bin"+managedZstdExtension), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New()
	m.compression.mu.Lock()
	m.compression.state = CompressionState{Enabled: true, Method: "zstd", ThresholdMiB: 1, Status: "idle"}
	m.compression.mu.Unlock()
	if _, err := m.enableWithShaders(context.Background(), disabled); err != nil {
		t.Fatalf("activation stopped on individual restore failure: %v", err)
	}
	if _, err := os.Stat(disabled); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled folder still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Unsafe")); err != nil {
		t.Fatalf("enabled folder was not created: %v", err)
	}
}

func ignoreCompressionFileErrors(string, error) {}
