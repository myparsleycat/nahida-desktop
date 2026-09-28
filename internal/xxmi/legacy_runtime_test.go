package xxmi

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractLegacyRuntimeUsesOnlyRuntimeFiles(t *testing.T) {
	t.Parallel()
	zipPath := filepath.Join(t.TempDir(), "legacy.zip")
	output, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	for name, content := range map[string]string{
		"3dmigoto/3DMigoto Loader.exe": "loader",
		"3dmigoto/d3d11.dll":           "module",
		"3dmigoto/d3dcompiler_46.dll":  "compiler",
		"3dmigoto/nvapi64.dll":         "nvapi",
		"3dmigoto/d3dx.ini":            "must not deploy",
		"3dmigoto/Mods/example.ini":    "must not deploy",
	} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zipFile, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zipFile.Close() }()
	digest := sha256.Sum256(data)
	zipHash := hex.EncodeToString(digest[:])
	root := t.TempDir()
	id, err := extractLegacyRuntime(context.Background(), zipFile, int64(len(data)), zipHash, "", root)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "packages", "legacy-3dmigoto", id)
	if err := verifyLegacyRuntimeCache(folder, zipHash); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "d3dcompiler_46.dll")); err != nil {
		t.Fatalf("legacy compiler DLL was not extracted: %v", err)
	}
	for _, name := range []string{"d3dx.ini", "Mods"} {
		if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("content file %s was extracted: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(folder, "d3d11.dll"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyLegacyRuntimeCache(folder, zipHash); err == nil {
		t.Fatal("modified cache passed verification")
	}
}
