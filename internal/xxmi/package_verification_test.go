package xxmi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/github"
)

func TestVerifyReleaseDigestReportsWhetherDigestWasPresent(t *testing.T) {
	t.Parallel()
	data := []byte("archive")
	digest := sha256.Sum256(data)
	release := github.Release{
		Assets: []github.Asset{{Name: "package.zip", Digest: "sha256:" + hex.EncodeToString(digest[:])}},
	}
	if verified, err := verifyReleaseDigest(release, "package.zip", data); err != nil || !verified {
		t.Fatalf("matching digest: verified = %t, err = %v", verified, err)
	}
	if verified, err := verifyReleaseDigest(release, "package.zip", []byte("modified")); err == nil || verified {
		t.Fatalf("modified archive: verified = %t, err = %v", verified, err)
	}
	release.Assets[0].Digest = ""
	if verified, err := verifyReleaseDigest(release, "package.zip", data); err != nil || verified {
		t.Fatalf("absent digest: verified = %t, err = %v", verified, err)
	}
}

func TestImporterPackageVerificationOnlyReportsCurrentInstalledVersion(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), "GIMI")
	cfg, err := DefaultImporterConfig("GIMI", filepath.Dir(folder))
	if err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	versionFile := filepath.Join(folder, "Core", "GIMI", "main.ini")
	if err := os.MkdirAll(filepath.Dir(versionFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(versionFile, []byte("global $version = 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(PackageVerification{Version: "1.2.3", Method: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, packageVerificationName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := service.GetImporterPackageVerification(ctx, "GIMI")
	if err != nil || record == nil || record.Version != "1.2.3" || record.Method != "digest" {
		t.Fatalf("installed verification = %+v, err = %v", record, err)
	}
	if err := os.WriteFile(versionFile, []byte("global $version = 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if record, err := service.GetImporterPackageVerification(ctx, "GIMI"); err != nil || record != nil {
		t.Fatalf("stale verification = %+v, err = %v", record, err)
	}
}
