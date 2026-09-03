package drive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeUploadFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUploadHashConcurrencyMatchesPiscinaPool(t *testing.T) {
	t.Parallel()
	want := max(2, int(math.Ceil(float64(runtime.GOMAXPROCS(0))*1.5)))
	if got := uploadHashConcurrency(); got != want {
		t.Fatalf("uploadHashConcurrency = %d, want %d", got, want)
	}
}

func TestHashUploadFilesPreservesOrderAndReportsProgress(t *testing.T) {
	base := t.TempDir()
	firstPath := filepath.Join(base, "first.ini")
	secondPath := filepath.Join(base, "second.ini")
	writeUploadFile(t, firstPath, "first")
	writeUploadFile(t, secondPath, "second")
	files := []UploadFile{
		{Name: "first.ini", FullPath: filepath.ToSlash(firstPath)},
		{Name: "second.ini", FullPath: filepath.ToSlash(secondPath)},
	}
	progress := make([]int, 0, 2)
	hashed, err := HashUploadFiles(context.Background(), files, 2, func(count int) {
		progress = append(progress, count)
	})
	if err != nil {
		t.Fatal(err)
	}
	firstHash := sha256.Sum256([]byte("first"))
	secondHash := sha256.Sum256([]byte("second"))
	if len(hashed) != 2 || hashed[0].SHA256 != hex.EncodeToString(firstHash[:]) || hashed[1].SHA256 != hex.EncodeToString(secondHash[:]) {
		t.Fatalf("hashes = %#v", hashed)
	}
	if len(progress) != 2 || progress[0] != 1 || progress[1] != 2 {
		t.Fatalf("progress = %v", progress)
	}
}

func TestPrepareUploadCollectsAllowedFilesAndSkipsSystemEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "My Mod")
	writeUploadFile(t, filepath.Join(root, "Character", "mod.ini"), "ini")
	writeUploadFile(t, filepath.Join(root, "Character", "texture.dds"), "texture")
	writeUploadFile(t, filepath.Join(root, "Character", "ignored.exe"), "binary")
	writeUploadFile(t, filepath.Join(root, "__MACOSX", "metadata.ini"), "metadata")
	writeUploadFile(t, filepath.Join(root, "desktop.ini"), "metadata")

	prepared, err := PrepareUpload([]string{root}, nil, UploadConflictSuffix, testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Files) != 2 {
		t.Fatalf("files = %#v", prepared.Files)
	}
	if len(prepared.Directories) != 2 {
		t.Fatalf("directories = %#v", prepared.Directories)
	}
	if prepared.TotalSize != int64(len("ini")+len("texture")) {
		t.Fatalf("TotalSize = %d", prepared.TotalSize)
	}
	if prepared.ProcessName != "My Mod" || prepared.PID == "" {
		t.Fatalf("preparation = %#v", prepared)
	}
	for _, file := range prepared.Files {
		if file.FID == "" {
			t.Fatalf("missing stable ID: %#v", file)
		}
	}
}

func TestPrepareUploadStableIDsAreDeterministic(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Mod")
	writeUploadFile(t, filepath.Join(root, "file.ini"), "value")
	first, err := PrepareUpload([]string{root}, nil, UploadConflictSuffix, testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareUpload([]string{root}, nil, UploadConflictSuffix, testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != 1 || len(second.Files) != 1 || first.Files[0].FID != second.Files[0].FID {
		t.Fatalf("stable IDs differ: %#v, %#v", first.Files, second.Files)
	}
}

func TestPrepareUploadSuffixesRootConflicts(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Mod")
	writeUploadFile(t, filepath.Join(root, "file.ini"), "value")
	rootFile := filepath.Join(base, "preview.png")
	writeUploadFile(t, rootFile, "preview")
	prepared, err := PrepareUpload(
		[]string{root, rootFile},
		[]string{"Mod", "preview.png", "Mod (2)"},
		UploadConflictSuffix,
		testUploadRules(),
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Directories[0].Name != "Mod (3)" {
		t.Fatalf("root directory name = %q", prepared.Directories[0].Name)
	}
	var rootFileName string
	for _, file := range prepared.Files {
		if file.ParentPath == "" {
			rootFileName = file.Name
		}
	}
	if rootFileName != "preview.png (2)" {
		t.Fatalf("root file name = %q", rootFileName)
	}
}

func TestPrepareUploadSkipDropsConflictingRootTree(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Mod")
	writeUploadFile(t, filepath.Join(root, "nested", "file.ini"), "value")
	other := filepath.Join(base, "other.ini")
	writeUploadFile(t, other, "other")
	prepared, err := PrepareUpload(
		[]string{root, other},
		[]string{"Mod"},
		UploadConflictSkip,
		testUploadRules(),
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Directories) != 0 {
		t.Fatalf("directories = %#v", prepared.Directories)
	}
	if len(prepared.Files) != 1 || prepared.Files[0].Name != "other.ini" {
		t.Fatalf("files = %#v", prepared.Files)
	}
}

func TestPrepareUploadAllowsAdditionalExtensionAndAllFiles(t *testing.T) {
	base := t.TempDir()
	custom := filepath.Join(base, "custom.xyz")
	executable := filepath.Join(base, "tool.exe")
	writeUploadFile(t, custom, "custom")
	writeUploadFile(t, executable, "tool")
	withExtension, err := PrepareUpload([]string{custom}, nil, UploadConflictSuffix, testUploadRules(), []string{"xyz"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(withExtension.Files) != 1 {
		t.Fatalf("additional extension files = %#v", withExtension.Files)
	}
	all, err := PrepareUpload([]string{executable}, nil, UploadConflictSuffix, testUploadRules(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Files) != 1 {
		t.Fatalf("allow-all files = %#v", all.Files)
	}
}

func TestPrepareUploadSkipsOversizedFiles(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "ok.ini")
	oversized := filepath.Join(root, "huge.png")
	writeUploadFile(t, allowed, "ini")
	if err := os.WriteFile(oversized, make([]byte, 100*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareUpload([]string{root}, nil, UploadConflictSuffix, testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Files) != 1 || prepared.Files[0].Name != "ok.ini" {
		t.Fatalf("files = %#v", prepared.Files)
	}
}
