package xxmi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestVerifyPackageSignature(t *testing.T) {
	t.Parallel()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("package bytes")
	digest := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	publicKey := base64.StdEncoding.EncodeToString(der)
	signature := base64.StdEncoding.EncodeToString(sig)
	if err := verifyPackageSignature(publicKey, signature, data); err != nil {
		t.Fatal(err)
	}
	if err := verifyPackageSignature(publicKey, signature, []byte("modified")); err == nil {
		t.Fatal("modified package passed signature verification")
	}
}

func TestReleaseSignatureAndNotes(t *testing.T) {
	t.Parallel()
	body := "## Warning\nCheck requirements\n## Changes\nUpdated DLL\n\n## Signature\n- YWJjZA=="
	if got := releaseSignature(body); got != "YWJjZA==" {
		t.Fatalf("signature = %q", got)
	}
	if got := releaseNotes(body); got != "## Changes\nUpdated DLL" {
		t.Fatalf("notes = %q", got)
	}
	if got := releaseSignature("## Signature\n- invalid!"); got != "" {
		t.Fatalf("invalid signature = %q", got)
	}
}

func TestVerifyOfficialXXMILibsReleaseSignature(t *testing.T) {
	// SpectrumQT/XXMI-Libs-Package v1.1.7: the release body's Signature
	// and the SHA-256 of its XXMI-PACKAGE-v1.1.7.zip asset.
	const signature = "MGUCMQCtCbykMitok4F69EtooclmVjbM3zRR8N/qRcUAhBgGxzNvwZFhNf8oB2COkoPoYaACME+TH8XhMbDw6l3I3zHN40XE+IIY2f8jFrNBKO8rqcTRbLSy+IkNBrCtYtuwDVaT9Q=="
	const zipSHA256 = "6ba40887a2d1ccd6221e23d06929d4687371aea9e51b01a7400850f811d54c06"
	if got := releaseSignature("## Signature\n- " + signature); got != signature {
		t.Fatalf("official release signature parsed as %q", got)
	}

	raw, err := hex.DecodeString(zipSHA256)
	if err != nil {
		t.Fatal(err)
	}
	var digest [sha256.Size]byte
	copy(digest[:], raw)
	if err := verifyPackageDigestSignature(spectrumPublicKey, signature, digest); err != nil {
		t.Fatalf("official release signature failed: %v", err)
	}
	digest[0] ^= 1
	if err := verifyPackageDigestSignature(spectrumPublicKey, signature, digest); err == nil {
		t.Fatal("modified release digest passed signature verification")
	}
}
