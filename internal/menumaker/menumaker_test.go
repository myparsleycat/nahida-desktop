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
	mustWrite(t, filepath.Join(root, "DISABLED old", "old.ini"), []byte("disabled"))
	mustWrite(t, filepath.Join(root, "sub", "extra.ini"), []byte("ini"))

	withoutTXT, err := New().ScanFolder(context.Background(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := relativePaths(
		withoutTXT.Files,
	); !slices.Equal(
		got,
		[]string{"main.ini", filepath.Join("sub", "extra.ini")},
	) {
		t.Fatalf("unexpected files without txt: %v", got)
	}

	withTXT, err := New().ScanFolder(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := relativePaths(withTXT.Files); !slices.Equal(
		got,
		[]string{"main.ini", "notes.txt", filepath.Join("sub", "extra.ini")},
	) {
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
		{
			name:     "utf8 bom crlf",
			data:     append([]byte{0xef, 0xbb, 0xbf}, []byte("[KeySwap]\r\nkey = 5\r\n$x = 0\r\n")...),
			encoding: "utf8",
			bom:      true,
			newline:  "crlf",
			text:     "[KeySwap]\r\nkey = 5\r\n$x = 0\r\n",
		},
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
			t.Parallel()
			path := filepath.Join(t.TempDir(), "source.ini")
			mustWrite(t, path, test.data)
			source, loadErr := New().LoadSource(context.Background(), path)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if source.Text != test.text || source.Encoding != test.encoding || source.HasBOM != test.bom ||
				source.Newline != test.newline {
				t.Fatalf("unexpected source metadata: %+v", source)
			}
		})
	}
}

func TestLoadSourceAllowsMenuINIUnlessItIsNahidaSidecar(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "menu.ini")
	mustWrite(t, path, []byte(sidecarFixture))
	if _, err := New().LoadSource(context.Background(), path); err != nil {
		t.Fatalf("ordinary menu.ini was rejected: %v", err)
	}
	mustWrite(t, path, []byte(sidecarMarker+"Example.ini\n"+sidecarFixture))
	if _, err := New().LoadSource(context.Background(), path); err == nil {
		t.Fatal("Nahida sidecar was accepted as an original")
	}
	otherPath := filepath.Join(root, "Example.ini")
	mustWrite(t, otherPath, []byte(sidecarMarker+"Example.ini\n"+sidecarFixture))
	if _, err := New().LoadSource(context.Background(), otherPath); err != nil {
		t.Fatalf("non-menu source containing a legacy marker was rejected: %v", err)
	}
}

func TestApplyBundleReplacesINIAndCreatesBackup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Example.ini")
	original := []byte(sidecarFixture)
	mustWrite(t, sourcePath, original)

	result := applyFixture(t, sourcePath, original)
	if result.OutputINIPath != sourcePath || result.BackupPath != filepath.Join(root, "Example.txt") {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertFile(t, result.BackupPath, original)
	written := mustRead(t, sourcePath)
	if result.SourceSHA256 != sha256Hex(written) || !bytes.Contains(written, []byte(generatedBegin)) {
		t.Fatal("single generated INI was not written")
	}
}

func TestApplyBundlePreservesTXTAndWritesMatchingINI(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Example.txt")
	original := []byte(sidecarFixture)
	mustWrite(t, sourcePath, original)

	result := applyFixture(t, sourcePath, original)
	assertFile(t, sourcePath, original)
	if result.OutputINIPath != filepath.Join(root, "Example.ini") || result.BackupPath != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.SourceSHA256 != sha256Hex(mustRead(t, result.OutputINIPath)) {
		t.Fatal("output hash was not refreshed")
	}
}

func TestApplyBundleUsesTimestampedBackupWhenTXTExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Example.ini")
	original := []byte(sidecarFixture)
	mustWrite(t, sourcePath, original)
	mustWrite(t, filepath.Join(root, "Example.txt"), []byte("existing"))

	result := applyFixture(t, sourcePath, original)
	if !strings.HasPrefix(filepath.Base(result.BackupPath), "Example.backup-") ||
		!strings.HasSuffix(result.BackupPath, ".txt") {
		t.Fatalf("unexpected backup path: %s", result.BackupPath)
	}
	assertFile(t, filepath.Join(root, "Example.txt"), []byte("existing"))
}

func TestApplyBundleRejectsChangedSourceAndAssetTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Example.ini")
	mustWrite(t, sourcePath, []byte(sidecarFixture))
	_, err := New().ApplyBundle(context.Background(), MenuMakerApplyRequest{
		SourcePath: sourcePath, SourceSHA256: sha256Hex([]byte("old")),
		Slots: parseDocument(sidecarFixture).Slots, Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
	})
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("expected source changed error, got %v", err)
	}
	_, err = New().writeGenerated(context.Background(), applyGeneratedRequest{
		sourcePath: sourcePath, original: []byte(sidecarFixture), iniText: "generated", encoding: "utf8", newline: "lf",
		assets: []MenuMakerGeneratedAsset{{RelativePath: "res_gui/../evil.png", Data: []byte("evil")}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid menu maker asset path") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestApplyBundleRollsBackINIResourcesAndPreservesSidecarOnFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "Example.ini")
	original := []byte(sidecarFixture)
	sidecar := []byte(sidecarMarker + "Example.ini\nlegacy")
	mustWrite(t, sourcePath, original)
	mustWrite(t, filepath.Join(root, menuININame), sidecar)
	mustWrite(t, filepath.Join(root, "res_gui"), []byte("directory blocker"))

	result, err := New().ApplyBundle(context.Background(), MenuMakerApplyRequest{
		SourcePath: sourcePath, SourceSHA256: sha256Hex(original), Slots: parseDocument(sidecarFixture).Slots,
		Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
		Assets: []MenuMakerGeneratedAsset{{RelativePath: "res_gui/bg.png", Data: []byte("png")}},
	})
	if err == nil || !result.RolledBack {
		t.Fatalf("expected rolled back failure: result=%+v err=%v", result, err)
	}
	assertFile(t, sourcePath, original)
	assertFile(t, filepath.Join(root, menuININame), sidecar)
	if _, statErr := os.Stat(filepath.Join(root, "Example.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rollback left a backup behind: %v", statErr)
	}
}

func TestApplyBundleRemovesOnlyMatchingNahidaSidecar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		menu      string
		isRemoved bool
	}{
		{name: "matching", menu: sidecarMarker + "Example.ini\nlegacy", isRemoved: true},
		{name: "different source", menu: sidecarMarker + "Other.ini\nlegacy"},
		{name: "user menu", menu: "[Present]\nrun = CommandListUser"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			sourcePath := filepath.Join(root, "Example.ini")
			original := []byte(sidecarFixture)
			menuPath := filepath.Join(root, menuININame)
			mustWrite(t, sourcePath, original)
			mustWrite(t, menuPath, []byte(test.menu))
			applyFixture(t, sourcePath, original)
			_, err := os.Stat(menuPath)
			if test.isRemoved && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("matching sidecar was not removed: %v", err)
			}
			if !test.isRemoved {
				assertFile(t, menuPath, []byte(test.menu))
			}
		})
	}
}

func TestSaveZIPContainsOneINIAndAssets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	archive := filepath.Join(root, "bundle.zip")
	_, err := New().SaveZIP(context.Background(), MenuMakerSaveZIPRequest{
		SourcePath: filepath.Join(root, "Example.txt"), DestinationPath: archive, SourceText: sidecarFixture,
		Slots: parseDocument(sidecarFixture).Slots, Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
		Assets: []MenuMakerGeneratedAsset{{RelativePath: "res_gui/bg.png", Data: []byte("png")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	if !slices.Equal(names, []string{"Example.ini", "res_gui/bg.png"}) {
		t.Fatalf("unexpected ZIP entries: %v", names)
	}
}

func applyFixture(t *testing.T, sourcePath string, original []byte) MenuMakerWriteResult {
	t.Helper()
	result, err := New().ApplyBundle(context.Background(), MenuMakerApplyRequest{
		SourcePath: sourcePath, SourceSHA256: sha256Hex(original), Slots: parseDocument(string(original)).Slots,
		Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func relativePaths(files []MenuMakerScanFile) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.RelativePath)
	}
	return paths
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

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertFile(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual := mustRead(t, path)
	if !bytes.Equal(actual, expected) {
		t.Fatalf("unexpected contents for %s: %q", path, actual)
	}
}
