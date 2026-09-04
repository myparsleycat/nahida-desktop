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

func TestZstdRoundTripRestoresMetadata(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED_Test")
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

	if err := compressZstdFile(context.Background(), folder, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original should be removed, err=%v", err)
	}
	if _, err := os.Stat(path + ".zst"); err != nil {
		t.Fatal(err)
	}
	if err := restoreZstdFolder(context.Background(), folder, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("restored data differs")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != originalInfo.Mode().Perm() || !info.ModTime().Equal(mtime) {
		t.Fatalf("metadata = %v %v", info.Mode().Perm(), info.ModTime())
	}
	if _, err := os.Stat(path + ".zst"); !errors.Is(err, os.ErrNotExist) {
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
	if err := compressZstdFile(context.Background(), folder, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".zst"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected zstd output, err=%v", err)
	}
}

func TestZstdRefusesDestinationCollision(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Collision")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 8192), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".zst", []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), folder, path, ignoreCompressionMutations); err == nil {
		t.Fatal("expected collision error")
	}
	got, err := os.ReadFile(path)
	if err != nil || len(got) != 8192 {
		t.Fatalf("original was not preserved: len=%d err=%v", len(got), err)
	}
}

func TestZstdManifestPathsCannotEscapeManagedFolder(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Unsafe")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(folder), "victim.bin")
	if err := os.WriteFile(outside, []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, relative := range []string{
		`..\victim.bin`, `../victim.bin`, `nested\..\..\victim.bin`, outside, `C:victim.bin`,
	} {
		t.Run(relative, func(t *testing.T) {
			entry := &zstdManifestEntry{
				OriginalPath: relative, ZstdPath: relative + ".zst", State: "compressed",
			}
			manifest := &zstdManifest{Version: 1, Entries: map[string]*zstdManifestEntry{relative: entry}}
			if err := recoverZstdEntry(context.Background(), folder, manifest, relative, entry, ignoreCompressionMutations); err == nil {
				t.Fatal("expected unsafe manifest path to fail")
			}
			if got, err := os.ReadFile(outside); err != nil || !bytes.Equal(got, []byte("preserve")) {
				t.Fatalf("outside file changed: %q, err=%v", got, err)
			}
		})
	}
}

func TestZstdManifestRequiresMatchingKeyAndDestination(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Unsafe")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := &zstdManifestEntry{OriginalPath: "payload.bin", ZstdPath: "other.zst", State: "compressed"}
	manifest := &zstdManifest{Version: 1, Entries: map[string]*zstdManifestEntry{"wrong.bin": entry}}
	if err := recoverZstdEntry(context.Background(), folder, manifest, "wrong.bin", entry, ignoreCompressionMutations); err == nil {
		t.Fatal("expected mismatched manifest key to fail")
	}
	manifest.Entries = map[string]*zstdManifestEntry{entry.OriginalPath: entry}
	if err := recoverZstdEntry(context.Background(), folder, manifest, entry.OriginalPath, entry, ignoreCompressionMutations); err == nil {
		t.Fatal("expected mismatched zstd destination to fail")
	}
}

func TestZstdRestoreClearsCanceledCompressionIntent(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Canceled")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	want := bytes.Repeat([]byte("original"), 1024)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := &zstdManifestEntry{
		OriginalPath: "payload.bin", ZstdPath: "payload.bin.zst", OriginalSize: int64(len(want)),
		State: "compressing",
	}
	manifest := &zstdManifest{Version: 1, Entries: map[string]*zstdManifestEntry{entry.OriginalPath: entry}}
	if err := writeZstdManifest(folder, manifest); err != nil {
		t.Fatal(err)
	}
	tempPath := path + ".zst" + compressionTempMarker
	if err := os.WriteFile(tempPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreZstdFolder(context.Background(), folder, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("original changed: err=%v", err)
	}
	for _, removed := range []string{tempPath, filepath.Join(folder, compressionManifestName)} {
		if _, err := os.Stat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovery artifact remains: %s, err=%v", removed, err)
		}
	}
}

func TestZstdValidationHonorsCancellationAndCleansTemp(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Validation")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("compress-me"), 32*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), folder, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	manifest, err := readZstdManifest(folder)
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entries["payload.bin"]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validateZstd(ctx, path+".zst", entry); !errors.Is(err, context.Canceled) {
		t.Fatalf("validation error = %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(folder, "*"+compressionTempMarker+".verify-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("verification temps = %v, err=%v", matches, err)
	}
}

func TestZstdRestoreRejectsArchiveOverMaximumAndCleansTemp(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Oversized")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("valid-zstd"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), folder, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	manifest, err := readZstdManifest(folder)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries["payload.bin"].OriginalSize = maxZstdRestoreSize + 1
	if err := writeZstdManifest(folder, manifest); err != nil {
		t.Fatal(err)
	}

	err = restoreZstdFolder(context.Background(), folder, ignoreCompressionMutations)
	if err == nil || !strings.Contains(err.Error(), "exceeds restore limit") {
		t.Fatalf("restore error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary original unexpectedly remains: %v", err)
	}
	if _, err := os.Stat(path + compressionTempMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore temporary output remains: %v", err)
	}
	if _, err := os.Stat(path + ".zst"); err != nil {
		t.Fatalf("valid archive was removed: %v", err)
	}
}

func TestZstdRejectsReparseManifestPath(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Reparse")
	outside := t.TempDir()
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(folder, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	relative := filepath.Join("link", "payload.bin")
	entry := &zstdManifestEntry{OriginalPath: relative, ZstdPath: relative + ".zst", State: "compressed"}
	manifest := &zstdManifest{Version: 1, Entries: map[string]*zstdManifestEntry{relative: entry}}
	if err := recoverZstdEntry(context.Background(), folder, manifest, relative, entry, ignoreCompressionMutations); err == nil {
		t.Fatal("expected reparse manifest path to fail")
	}
}

func TestCorruptZstdIsPreservedAndRestoreFails(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "DISABLED Corrupt")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "payload.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("compress-me"), 64*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), folder, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	compressedPath := path + ".zst"
	if err := os.WriteFile(compressedPath, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreZstdFolder(context.Background(), folder, ignoreCompressionMutations); err == nil {
		t.Fatal("expected corrupt zstd restore to fail")
	}
	if _, err := os.Stat(compressedPath); err != nil {
		t.Fatalf("corrupt compressed file was removed: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unverified original unexpectedly exists: %v", err)
	}
}

func TestEnabledFolderManifestIsRestoredDuringReconciliation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Mods")
	disabled := filepath.Join(root, "DISABLED Test")
	if err := os.MkdirAll(disabled, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(disabled, "payload.bin")
	want := bytes.Repeat([]byte("restore-on-rename"), 32*1024)
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), disabled, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	enabled := filepath.Join(root, "Test")
	if err := os.Rename(disabled, enabled); err != nil {
		t.Fatal(err)
	}
	if err := restoreEnabledZstd(context.Background(), []string{root}, func(int, int64) {}, func(string, int64, bool) {}, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(enabled, "payload.bin"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("restored file mismatch: err=%v", err)
	}
}

func TestEnableStopsBeforeRenameWhenZstdRestoreFails(t *testing.T) {
	root := t.TempDir()
	disabled := filepath.Join(root, "DISABLED Unsafe")
	if err := os.MkdirAll(disabled, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(disabled, "payload.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte("compress-me"), 32*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(context.Background(), disabled, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".zst", []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New()
	m.compression.mu.Lock()
	m.compression.state = CompressionState{Enabled: true, Method: "zstd", ThresholdMiB: 1, Status: "idle"}
	m.compression.mu.Unlock()
	if _, err := m.enableWithShaders(context.Background(), disabled); err == nil {
		t.Fatal("expected activation to stop on restore failure")
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatalf("disabled folder was renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Unsafe")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("enabled folder unexpectedly exists: %v", err)
	}
}
