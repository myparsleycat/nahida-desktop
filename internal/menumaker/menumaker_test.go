package menumaker

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestScanFolderFiltersDisabledAndTXT(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "main.ini"), []byte("[KeySwap]"))
	mustWrite(t, filepath.Join(root, "notes.txt"), []byte("text"))
	mustWrite(t, filepath.Join(root, "image.png"), []byte("png"))
	mustWrite(t, filepath.Join(root, "DISABLED old", "old.ini"), []byte("disabled"))
	mustWrite(t, filepath.Join(root, "sub", "disabled-copy.ini"), []byte("disabled"))
	mustWrite(t, filepath.Join(root, "sub", "extra.ini"), []byte("ini"))

	withoutTXT, err := New().ScanFolder(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := relativePaths(withoutTXT.Files); !slices.Equal(got, []string{"main.ini", filepath.Join("sub", "extra.ini")}) {
		t.Fatalf("unexpected files without txt: %v", got)
	}
	if withoutTXT.Stats.INI != 2 || withoutTXT.Stats.TXT != 1 || withoutTXT.Stats.Disabled != 2 {
		t.Fatalf("unexpected stats: %+v", withoutTXT.Stats)
	}

	withTXT, err := New().ScanFolder(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := relativePaths(withTXT.Files); !slices.Equal(got, []string{"main.ini", "notes.txt", filepath.Join("sub", "extra.ini")}) {
		t.Fatalf("unexpected files with txt: %v", got)
	}
}

func TestLoadSourcePreservesEncodingBOMAndNewline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		data     []byte
		encoding string
		bom      bool
		newline  string
		text     string
	}{
		{name: "utf8 lf", data: []byte("[KeySwap]\nkey = 5\n$x = 0\n"), encoding: "utf8", newline: "lf", text: "[KeySwap]\nkey = 5\n$x = 0\n"},
		{name: "utf8 bom crlf", data: append([]byte{0xef, 0xbb, 0xbf}, []byte("[KeySwap]\r\nkey = 5\r\n$x = 0\r\n")...), encoding: "utf8", bom: true, newline: "crlf", text: "[KeySwap]\r\nkey = 5\r\n$x = 0\r\n"},
	}
	gbkText := "[KeySwap]\r\n; 中文\r\n$x = 0\r\n"
	gbkData, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(gbkText))
	if err != nil {
		t.Fatal(err)
	}
	tests = append(tests, struct {
		name     string
		data     []byte
		encoding string
		bom      bool
		newline  string
		text     string
	}{name: "gbk crlf", data: gbkData, encoding: "gbk", newline: "crlf", text: gbkText})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.ini")
			mustWrite(t, path, test.data)
			source, loadErr := New().LoadSource(context.Background(), path)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if source.Text != test.text || source.Encoding != test.encoding || source.HasBOM != test.bom || source.Newline != test.newline {
				t.Fatalf("unexpected source metadata: %+v", source)
			}
			encoded, encodeErr := encodeText(source.Text, textEncoding{name: source.Encoding, bom: source.HasBOM, newline: source.Newline})
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if !bytes.Equal(encoded, test.data) {
				t.Fatalf("round trip changed bytes: %x != %x", encoded, test.data)
			}
		})
	}
}

func TestApplyBundleOriginalNameBacksUpAndPreservesUnmanagedResources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "mod.ini")
	original := []byte("[KeySwap]\r\nkey = 5\r\n")
	mustWrite(t, sourcePath, original)
	mustWrite(t, filepath.Join(root, "res_gui", "keep.dat"), []byte("keep"))

	result, err := New().writeGenerated(context.Background(), applyGeneratedRequest{
		sourcePath: sourcePath, original: original, outputININame: "mod.ini",
		iniText: "[Present]\nrun = CommandListGuiMenu\n", encoding: "utf8", newline: "crlf",
		assets: []MenuMakerGeneratedAsset{
			{RelativePath: "res_gui/draw_2d.hlsl", Data: []byte("shader")},
			{RelativePath: "res_gui/slot_01.png", Data: []byte("png")},
		},
		useOriginalININame: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OutputINIPath != sourcePath || result.BackupPath != filepath.Join(root, "mod.txt") || result.RolledBack {
		t.Fatalf("unexpected result: %+v", result)
	}
	written := []byte("[Present]\r\nrun = CommandListGuiMenu\r\n")
	if result.SourceSHA256 != sha256Hex(written) {
		t.Fatalf("unexpected overwrite sha256: %q", result.SourceSHA256)
	}
	assertFile(t, result.BackupPath, original)
	assertFile(t, sourcePath, written)
	assertFile(t, filepath.Join(root, "res_gui", "keep.dat"), []byte("keep"))
	assertFile(t, filepath.Join(root, "res_gui", "draw_2d.hlsl"), []byte("shader"))
}

func TestApplyBundleNewNameDisablesINIOnlyAfterOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "mod.ini")
	original := []byte("original")
	mustWrite(t, sourcePath, original)

	result, err := New().writeGenerated(context.Background(), applyGeneratedRequest{
		sourcePath: sourcePath, original: original, outputININame: "mod_gui.ini",
		iniText: "generated", encoding: "utf8", newline: "lf", useOriginalININame: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(sourcePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("source INI was not disabled: %v", statErr)
	}
	assertFile(t, result.BackupPath, original)
	assertFile(t, filepath.Join(root, "mod_gui.ini"), []byte("generated"))
	if result.SourceSHA256 != "" {
		t.Fatalf("new-name apply should not refresh source sha256: %+v", result)
	}
}

func TestApplyBundleUsesTimestampedBackupWhenTXTNameExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "mod.ini")
	original := []byte("original")
	mustWrite(t, sourcePath, original)
	mustWrite(t, filepath.Join(root, "mod.txt"), []byte("existing"))

	result, err := New().writeGenerated(context.Background(), applyGeneratedRequest{
		sourcePath: sourcePath, original: original, outputININame: "mod.ini",
		iniText: "generated", encoding: "utf8", newline: "lf", useOriginalININame: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(result.BackupPath), "mod.backup-") || !strings.HasSuffix(result.BackupPath, ".txt") {
		t.Fatalf("unexpected collision backup path: %s", result.BackupPath)
	}
	assertFile(t, filepath.Join(root, "mod.txt"), []byte("existing"))
	assertFile(t, result.BackupPath, original)
}

func TestApplyBundleTXTAlwaysPreservesSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "mod.txt")
	original := []byte("original")
	mustWrite(t, sourcePath, original)

	result, err := New().writeGenerated(context.Background(), applyGeneratedRequest{
		sourcePath: sourcePath, original: original, outputININame: "mod.ini",
		iniText: "generated", encoding: "utf8", newline: "lf", useOriginalININame: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, sourcePath, original)
	assertFile(t, result.OutputINIPath, []byte("generated"))
	if result.SourceSHA256 != "" {
		t.Fatalf("txt apply should not refresh source sha256: %+v", result)
	}
}

func TestApplyBundleRejectsChangedSourceAndAssetTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "mod.ini")
	mustWrite(t, sourcePath, []byte("changed"))
	_, err := New().ApplyBundle(context.Background(), MenuMakerApplyRequest{
		SourcePath: sourcePath, SourceSHA256: sha256Hex([]byte("old")), OutputININame: "mod.ini",
		Encoding: "utf8", Newline: "lf", UseOriginalININame: true,
	})
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("expected source changed error, got %v", err)
	}

	_, err = New().writeGenerated(context.Background(), applyGeneratedRequest{
		sourcePath: sourcePath, original: []byte("changed"), outputININame: "mod.ini",
		iniText: "generated", encoding: "utf8", newline: "lf", useOriginalININame: true,
		assets: []MenuMakerGeneratedAsset{{RelativePath: "res_gui/../evil.png", Data: []byte("evil")}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid menu maker asset path") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestApplyBundleRollsBackPromotedINIWhenResourceCommitFails(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "mod.ini")
	original := []byte("original")
	mustWrite(t, sourcePath, original)
	mustWrite(t, filepath.Join(root, "res_gui"), []byte("directory blocker"))

	result, err := New().writeGenerated(context.Background(), applyGeneratedRequest{
		sourcePath: sourcePath, original: original, outputININame: "mod.ini",
		iniText: "generated", encoding: "utf8", newline: "lf", useOriginalININame: true,
		assets: []MenuMakerGeneratedAsset{{RelativePath: "res_gui/bg.png", Data: []byte("png")}},
	})
	if err == nil {
		t.Fatal("expected resource promotion failure")
	}
	if !result.RolledBack {
		t.Fatalf("expected rollback result: %+v", result)
	}
	assertFile(t, sourcePath, original)
	if _, statErr := os.Stat(filepath.Join(root, "mod.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed transaction left a backup behind: %v", statErr)
	}
}

func TestApplyBundleOverwriteAllowsImmediateReapply(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "mod.ini")
	original := []byte("[KeySwap]\nkey = 5\n$x = 0,1\n")
	mustWrite(t, sourcePath, original)
	svc := New()
	first, err := svc.ApplyBundle(context.Background(), MenuMakerApplyRequest{
		SourcePath: sourcePath, SourceSHA256: sha256Hex(original), OutputININame: "mod.ini",
		Slots: parseDocument(string(original)).Slots, Settings: defaultSettings(),
		Encoding: "utf8", Newline: "lf", UseOriginalININame: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceSHA256 == "" || first.SourceSHA256 == sha256Hex(original) || first.SourceSHA256 != sha256Hex(written) {
		t.Fatalf("unexpected overwrite sha256: got %q original %q file %q", first.SourceSHA256, sha256Hex(original), sha256Hex(written))
	}
	if _, err = svc.ApplyBundle(context.Background(), MenuMakerApplyRequest{
		SourcePath: sourcePath, SourceSHA256: first.SourceSHA256, OutputININame: "mod.ini",
		Slots: parseDocument(string(original)).Slots, Settings: defaultSettings(),
		Encoding: "utf8", Newline: "lf", UseOriginalININame: true,
	}); err != nil {
		t.Fatalf("reapply with returned hash failed: %v", err)
	}
}

func TestSaveZIPUsesUTF8NamesAndContainsBundle(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "menu.zip")
	_, err := saveZIPBytes(path, "메뉴.ini", "ini", textEncoding{name: "utf8", newline: "lf"}, []MenuMakerGeneratedAsset{{RelativePath: "res_gui/title.png", Data: []byte("png")}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	if !slices.Equal(names, []string{"메뉴.ini", "res_gui/title.png"}) {
		t.Fatalf("unexpected zip entries: %v", names)
	}
}

func relativePaths(files []MenuMakerScanFile) []string {
	return slices.Collect(func(yield func(string) bool) {
		for _, file := range files {
			if !yield(file.RelativePath) {
				return
			}
		}
	})
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("unexpected contents for %s: %q", path, actual)
	}
}
