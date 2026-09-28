package xxmi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
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
