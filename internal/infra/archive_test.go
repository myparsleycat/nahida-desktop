package infra

import (
	"archive/tar"
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIsArchiveOfKeepsCustomDownloadSniffToElectronFormats(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "payload.bin")
	writeZip(t, zipPath, map[string]string{"mod.ini": "[Constants]"})

	tarPath := filepath.Join(root, "payload.dat")
	tarFile, err := os.Create(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	tarWriter := tar.NewWriter(tarFile)
	body := []byte("[Constants]")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "mod.ini", Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tarFile.Close(); err != nil {
		t.Fatal(err)
	}

	archive := NewArchive()
	if !archive.IsArchiveOf(context.Background(), zipPath, "zip", "7z", "rar") {
		t.Fatal("ZIP magic was not accepted")
	}
	if archive.IsArchiveOf(context.Background(), tarPath, "zip", "7z", "rar") {
		t.Fatal("TAR magic exceeded Electron's download archive contract")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if archive.IsArchiveOf(canceled, zipPath, "zip", "7z", "rar") {
		t.Fatal("canceled archive sniff should stop")
	}
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveHasSingleTopLevelDirectory(t *testing.T) {
	directory := t.TempDir()
	single := filepath.Join(directory, "single.zip")
	writeZip(t, single, map[string]string{
		"Root/file.txt": "value",
		"desktop.ini":   "ignored",
	})
	archive := NewArchive()
	got, err := archive.HasSingleTopLevelDirectory(context.Background(), single)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("HasSingleTopLevelDirectory() = false")
	}

	multiple := filepath.Join(directory, "multiple.zip")
	writeZip(t, multiple, map[string]string{
		"Root/file.txt":  "value",
		"Other/file.txt": "value",
	})
	got, err = archive.HasSingleTopLevelDirectory(context.Background(), multiple)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("HasSingleTopLevelDirectory() = true")
	}
}

func TestArchiveExtractFlattensSingleRootAndUsesUniqueName(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "archive.zip")
	writeZip(t, archivePath, map[string]string{
		"Root/nested/file.txt": "contents",
	})
	target := filepath.Join(directory, "target")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	var finalPercent int
	extracted, err := NewArchive().Extract(
		context.Background(),
		archivePath,
		target,
		ExtractOptions{},
		func(percent int, _ string) { finalPercent = percent },
	)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(extracted) != "nested (2)" {
		t.Fatalf("extracted path = %q", extracted)
	}
	content, err := os.ReadFile(filepath.Join(extracted, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "contents" {
		t.Fatalf("content = %q", content)
	}
	if finalPercent != 100 {
		t.Fatalf("final progress = %d", finalPercent)
	}
}

func TestArchiveExtractCanKeepArchiveNamedRoot(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "pack.zip")
	writeZip(t, archivePath, map[string]string{"Root/file.txt": "contents"})
	flatten := false
	extracted, err := NewArchive().Extract(
		context.Background(),
		archivePath,
		filepath.Join(directory, "target"),
		ExtractOptions{FlattenSingleRoot: &flatten},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(extracted) != "pack" {
		t.Fatalf("extracted path = %q", extracted)
	}
	if _, err := os.Stat(filepath.Join(extracted, "Root", "file.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveExtractRejectsTraversalAndCleansTemp(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "unsafe.zip")
	writeZip(t, archivePath, map[string]string{"../escape.txt": "forbidden"})
	target := filepath.Join(directory, "target")
	_, err := NewArchive().Extract(context.Background(), archivePath, target, ExtractOptions{}, nil)
	if err == nil {
		t.Fatal("Extract() error = nil")
	}
	if _, statErr := os.Stat(filepath.Join(directory, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("escape file stat error = %v", statErr)
	}
	entries, readErr := os.ReadDir(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("target contains leftover entries: %v", entries)
	}
}
