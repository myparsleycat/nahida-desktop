package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectInstallerAssetPrefersV3Installer(t *testing.T) {
	t.Parallel()

	asset, err := selectInstallerAsset([]releaseAsset{
		{Name: "SHA256SUMS"},
		{Name: "nahida-desktop-windows-amd64.exe"},
		{Name: "Nahida Desktop Latest Installer.exe"},
		{Name: "nahida-desktop-windows-amd64-installer.exe"},
	})
	if err != nil {
		t.Fatalf("selectInstallerAsset returned error: %v", err)
	}

	if asset.Name != "nahida-desktop-windows-amd64-installer.exe" {
		t.Fatalf("unexpected asset selected: %q", asset.Name)
	}
}

func TestSelectInstallerAssetPrefersV3OverV2Setup(t *testing.T) {
	t.Parallel()

	asset, err := selectInstallerAsset([]releaseAsset{
		{Name: "Nahida-Desktop-Setup-2.35.2.exe"},
		{Name: "nahida-desktop-windows-amd64-installer.exe"},
	})
	if err != nil {
		t.Fatalf("selectInstallerAsset returned error: %v", err)
	}

	if asset.Name != "nahida-desktop-windows-amd64-installer.exe" {
		t.Fatalf("unexpected asset selected: %q", asset.Name)
	}
}

func TestSelectInstallerAssetFallsBackToV2Setup(t *testing.T) {
	t.Parallel()

	asset, err := selectInstallerAsset([]releaseAsset{
		{Name: "latest.yml"},
		{Name: "Nahida-Desktop-Setup-2.35.2.exe.blockmap"},
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

func TestSelectInstallerAssetSupportsSpacedV2SetupName(t *testing.T) {
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

func TestSelectInstallerAssetFailsWithoutInstaller(t *testing.T) {
	t.Parallel()

	if _, err := selectInstallerAsset([]releaseAsset{
		{Name: "SHA256SUMS"},
		{Name: "nahida-desktop-windows-amd64.exe"},
		{Name: "Nahida Desktop Latest Installer.exe"},
		{Name: "latest.yml"},
	}); err == nil {
		t.Fatal("expected error when no installer asset exists")
	}
}

func TestParseSHA256Digest(t *testing.T) {
	t.Parallel()

	expected := sha256.Sum256([]byte("nahida"))
	decoded, err := parseSHA256Digest("sha256:" + hex.EncodeToString(expected[:]))
	if err != nil {
		t.Fatalf("parseSHA256Digest returned error: %v", err)
	}

	if hex.EncodeToString(decoded) != hex.EncodeToString(expected[:]) {
		t.Fatalf("unexpected decoded digest: %x", decoded)
	}
}

func TestVerifyDigestRejectsMissingDigest(t *testing.T) {
	t.Parallel()

	if err := verifyDigest("", sha256.New().Sum(nil)); err == nil {
		t.Fatal("expected error for missing digest")
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
	keepName := "nahida-desktop-windows-amd64-installer.exe"
	removeV2 := "Nahida-Desktop-Setup-2.35.1.exe"
	portable := "nahida-desktop-windows-amd64.exe"
	otherName := "notes.txt"

	for _, name := range []string{keepName, removeV2, portable, otherName} {
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

	if _, err := os.Stat(filepath.Join(cacheDir, portable)); err != nil {
		t.Fatalf("expected portable exe to remain: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, removeV2)); !os.IsNotExist(err) {
		t.Fatalf("expected old v2 installer to be removed, got err=%v", err)
	}
}
