package drive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func writeUploadNZST(t *testing.T, path string, content []byte) {
	t.Helper()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = encoder.Close() }()
	writeUploadFile(t, path, string(encoder.EncodeAll(content, nil)))
}

func TestPrepareUploadNZSTUsesOriginalNamesAndSizes(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "Character", "texture.dds.NZST")
	content := bytes.Repeat([]byte("texture"), 1000)
	writeUploadNZST(t, archive, content)
	writeUploadFile(t, filepath.Join(root, "Character", "mod.ini"), "ini")
	writeUploadFile(t, filepath.Join(root, "desktop.ini.nzst"), "invalid but excluded")
	writeUploadFile(t, filepath.Join(root, "notes.exe.nzst"), "invalid but denied")
	// Collection resolves paths, which can expand Windows short names or fix casing.
	resolvedArchive, err := filepath.EvalSymlinks(archive)
	if err != nil {
		t.Fatal(err)
	}
	collected, err := collectUploadPaths([]string{root}, testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(collected.Files) != 2 || collected.SkippedCount != 1 || !reflect.DeepEqual(collected.SkippedExtensions, []string{".exe"}) {
		t.Fatalf("collected = %+v", collected)
	}
	for _, file := range collected.Files {
		if file.Name == "texture.dds" && (file.Size != int64(len(content)) || strings.HasSuffix(file.Path, ".NZST") || file.FullPath != filepath.ToSlash(resolvedArchive)) {
			t.Fatalf("file = %+v", file)
		}
	}
	for _, strategy := range []UploadConflictStrategy{UploadConflictSkip, UploadConflictSuffix} {
		prepared, err := PrepareUpload([]string{archive}, []string{"texture.dds"}, strategy, testUploadRules(), nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if strategy == UploadConflictSkip {
			if len(prepared.Files) != 0 {
				t.Fatal("conflicting original was not skipped")
			}
		} else if len(prepared.Files) != 1 || prepared.Files[0].Name == "texture.dds" || prepared.TotalSize != int64(len(content)) {
			t.Fatalf("prepared = %+v", prepared)
		}
	}
}

func TestUploadNZSTOriginalWinsRegardlessOfOrder(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "TEXTURE.dds")
	archive := filepath.Join(root, "texture.dds.nzst")
	writeUploadFile(t, original, "original")
	writeUploadFile(t, archive, "corrupt stale archive")
	resolvedOriginal, err := filepath.EvalSymlinks(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{{archive, original}, {original, archive}, {root}} {
		prepared, err := PrepareUpload(paths, nil, UploadConflictSuffix, testUploadRules(), nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(prepared.Files) != 1 || prepared.Files[0].FullPath != filepath.ToSlash(resolvedOriginal) {
			t.Fatalf("prepared = %+v", prepared)
		}
	}
}

func TestUploadNZSTLimitsAndAdditionalExtensions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.xyz.nzst")
	writeUploadNZST(t, path, []byte("12345"))
	rules := testUploadRules()
	rules.MaxFileSize = 5
	for _, allowAll := range []bool{false, true} {
		prepared, err := PrepareUpload([]string{path}, nil, "", rules, []string{"xyz"}, allowAll)
		if err != nil || len(prepared.Files) != 1 || prepared.TotalSize != 5 {
			t.Fatalf("prepared = %+v, err = %v", prepared, err)
		}
	}
	rules.Extensions = append(rules.Extensions, UploadExtensionRule{Ext: ".xyz", MaxSize: 4})
	prepared, err := PrepareUpload([]string{path}, nil, "", rules, nil, true)
	if err != nil || len(prepared.Files) != 0 {
		t.Fatalf("oversized = %+v, err = %v", prepared, err)
	}
	var output bytes.Buffer
	size, err := copyUploadNZST(t.Context(), path, &output, 2)
	if err != nil || size != 3 || output.Len() != 3 {
		t.Fatalf("bounded decode = %d, %v", size, err)
	}
}

func TestUploadNZSTCorruptionAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.ini.nzst")
	writeUploadFile(t, path, "")
	if _, err := collectUploadPaths([]string{path}, testUploadRules(), nil, false); err == nil {
		t.Fatal("empty archive accepted")
	}
	writeUploadFile(t, path, "broken")
	if _, err := collectUploadPaths([]string{path}, testUploadRules(), nil, false); err == nil {
		t.Fatal("corrupt archive accepted")
	}
	writeUploadNZST(t, path, bytes.Repeat([]byte("payload"), 20000))
	ctx, cancel := context.WithCancel(t.Context())
	writer := &cancelUploadNZSTWriter{cancel: cancel}
	if _, err := copyUploadNZST(ctx, path, writer, uploadNZSTMaxSize); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if _, err := collectUploadPathsContext(ctx, []string{path}, testUploadRules(), nil, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("collection error = %v", err)
	}
	compressed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	compressed[len(compressed)-1] ^= 1
	writeUploadFile(t, path, string(compressed))
	if _, err := copyUploadNZST(t.Context(), path, io.Discard, uploadNZSTMaxSize); err == nil {
		t.Fatal("checksum corruption accepted")
	}
}

func TestUploadNZSTPreservesNTEGroupingAndOrdinaryZST(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Game.pak.nzst", "Game.utoc.nzst", "Game_s1.ucas.nzst"} {
		writeUploadNZST(t, filepath.Join(root, name), []byte("bundle"))
	}
	writeUploadFile(t, filepath.Join(root, "ordinary.zst"), "opaque zst contents")
	prepared, err := PrepareUpload([]string{root}, nil, "", testUploadRules(), []string{"zst"}, false)
	if err != nil || len(prepared.Files) != 4 {
		t.Fatalf("prepared = %+v, %v", prepared, err)
	}
	for _, file := range prepared.Files {
		if file.Name == "ordinary.zst" {
			if file.Size != int64(len("opaque zst contents")) {
				t.Fatal("ordinary zst was changed")
			}
			continue
		}
		if got := nteGroupKey(FinalUploadFile{UploadFile: file, ParentID: "parent"}); got != "parent\x00game" {
			t.Fatalf("group key = %q for %s", got, file.Name)
		}
	}
}

type cancelUploadNZSTWriter struct{ cancel context.CancelFunc }

func (w *cancelUploadNZSTWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }

func TestRestoreUploadNZSTPreservesSourcesAndUsesUniqueTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"a", "b"} {
		writeUploadNZST(t, filepath.Join(root, dir, "file.ini.nzst"), []byte(dir))
	}
	prepared, err := PrepareUpload([]string{root}, nil, "", testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	tempDir := ""
	defer func() {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
	}()
	files, restored, err := restoreUploadSources(t.Context(), prepared.Files, &tempDir)
	if err != nil || !restored || len(files) != 2 || files[0].FullPath == files[1].FullPath {
		t.Fatalf("restore = %+v, %v", files, err)
	}
	hashed, err := HashUploadFiles(t.Context(), files, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, file := range hashed {
		content := []byte(filepath.Base(filepath.Dir(prepared.Files[index].FullPath)))
		if file.SHA256 != fmt.Sprintf("%x", sha256.Sum256(content)) || file.Name != "file.ini" || file.FID != prepared.Files[index].FID {
			t.Fatalf("hashed = %+v", file)
		}
		if _, err := os.Stat(prepared.Files[index].FullPath); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(strings.TrimSuffix(prepared.Files[index].FullPath, ".nzst")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("local original created: %v", err)
		}
	}
	writeUploadNZST(t, prepared.Files[0].FullPath, []byte("changed size"))
	if _, _, err := restoreUploadSources(t.Context(), prepared.Files, &tempDir); err == nil {
		t.Fatal("changed source size accepted")
	}
	badTemp := filepath.Join(root, "not-a-directory")
	writeUploadFile(t, badTemp, "file")
	if _, _, err := restoreUploadSources(t.Context(), prepared.Files, &badTemp); err == nil {
		t.Fatal("invalid temp directory accepted")
	}
}

func TestUploadNZSTDecodesOnlyOneLayer(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "payload.ini.nzst")
	writeUploadNZST(t, inner, []byte("payload"))
	compressed, err := os.ReadFile(inner)
	if err != nil {
		t.Fatal(err)
	}
	outer := inner + ".nzst"
	writeUploadNZST(t, outer, compressed)
	prepared, err := PrepareUpload([]string{outer}, nil, "", testUploadRules(), nil, true)
	if err != nil || len(prepared.Files) != 1 || prepared.Files[0].Name != "payload.ini.nzst" || prepared.TotalSize != int64(len(compressed)) {
		t.Fatalf("prepared = %+v, %v", prepared, err)
	}
	plain, err := PrepareUpload([]string{inner}, nil, "", testUploadRules(), nil, false)
	if err != nil || plain.TotalSize != 7 {
		t.Fatalf("plain = %+v, %v", plain, err)
	}
}
