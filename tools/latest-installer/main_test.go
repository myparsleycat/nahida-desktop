package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectInstallerAsset(t *testing.T) {
	t.Parallel()

	asset, err := selectInstallerAsset([]releaseAsset{
		{Name: "latest.yml"},
		{Name: "Nahida-Desktop-Setup-2.35.2.exe.blockmap"},
		{Name: "Nahida-Desktop-Setup-2.35.2.exe"},
	})
	if err != nil {
		t.Fatalf("selectInstallerAsset returned error: %v", err)
	}

	if asset.Name != "Nahida-Desktop-Setup-2.35.2.exe" {
		t.Fatalf("unexpected asset selected: %q", asset.Name)
	}
}

func TestSelectInstallerAssetSkipsLatestInstallerExecutable(t *testing.T) {
	t.Parallel()

	asset, err := selectInstallerAsset([]releaseAsset{
		{Name: "Nahida Desktop Latest Installer.exe"},
		{Name: "Nahida Desktop Setup 2.35.2.exe"},
	})
	if err != nil {
		t.Fatalf("selectInstallerAsset returned error: %v", err)
	}

	if asset.Name != "Nahida Desktop Setup 2.35.2.exe" {
		t.Fatalf("unexpected asset selected: %q", asset.Name)
	}
}

func TestSelectInstallerAssetSupportsHyphenatedSetupName(t *testing.T) {
	t.Parallel()

	asset, err := selectInstallerAsset([]releaseAsset{
		{Name: "Nahida Desktop Latest Installer.exe"},
		{Name: "Nahida-Desktop-Setup-2.35.2.exe"},
	})
	if err != nil {
		t.Fatalf("selectInstallerAsset returned error: %v", err)
	}

	if asset.Name != "Nahida-Desktop-Setup-2.35.2.exe" {
		t.Fatalf("unexpected asset selected: %q", asset.Name)
	}
}

func TestSelectInstallerAssetFailsWithoutExe(t *testing.T) {
	t.Parallel()

	if _, err := selectInstallerAsset([]releaseAsset{
		{Name: "latest.yml"},
		{Name: "Nahida-Desktop-Setup-2.35.2.exe.blockmap"},
	}); err == nil {
		t.Fatal("expected error when no installer asset exists")
	}
}

func TestParseSHA256Digest(t *testing.T) {
	t.Parallel()

	expected := sha256.Sum256([]byte("nahida"))
	decoded, ok, err := parseSHA256Digest("sha256:" + hex.EncodeToString(expected[:]))
	if err != nil {
		t.Fatalf("parseSHA256Digest returned error: %v", err)
	}

	if !ok {
		t.Fatal("expected digest to be present")
	}

	if hex.EncodeToString(decoded) != hex.EncodeToString(expected[:]) {
		t.Fatalf("unexpected decoded digest: %x", decoded)
	}
}

func TestVerifyDigestAllowsMissingDigest(t *testing.T) {
	t.Parallel()

	if err := verifyDigest("", sha256.New().Sum(nil)); err != nil {
		t.Fatalf("verifyDigest returned error for missing digest: %v", err)
	}
}

func TestVerifyDigestRejectsMismatch(t *testing.T) {
	t.Parallel()

	expected := sha256.Sum256([]byte("expected"))
	actual := sha256.Sum256([]byte("actual"))
	err := verifyDigest("sha256:"+hex.EncodeToString(expected[:]), actual[:])
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
}

func TestResolveCacheDirUsesUserCacheDir(t *testing.T) {
	t.Parallel()

	cacheDir, err := resolveCacheDir()
	if err != nil {
		t.Fatalf("resolveCacheDir returned error: %v", err)
	}

	if filepath.Base(cacheDir) != "InstallerCache" {
		t.Fatalf("unexpected cache dir: %q", cacheDir)
	}
}

func TestPruneInstallerCacheRemovesOldInstallersOnly(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	keepName := "Nahida Desktop Setup 2.35.2.exe"
	removeName := "Nahida Desktop Setup 2.35.1.exe"
	otherName := "notes.txt"

	for _, name := range []string{keepName, removeName, otherName} {
		if err := os.WriteFile(filepath.Join(cacheDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) failed: %v", name, err)
		}
	}

	if err := pruneInstallerCache(cacheDir, keepName); err != nil {
		t.Fatalf("pruneInstallerCache returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, keepName)); err != nil {
		t.Fatalf("expected keep file to remain: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, otherName)); err != nil {
		t.Fatalf("expected non-installer file to remain: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, removeName)); !os.IsNotExist(err) {
		t.Fatalf("expected old installer to be removed, got err=%v", err)
	}
}
