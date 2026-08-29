package platform

import (
	"testing"
)

func TestCryptoRoundTrip(t *testing.T) {
	c := NewCrypto()
	const plain = "session-token-value"
	enc, err := c.EncryptString(plain)
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	if enc == "" || enc == plain {
		t.Fatalf("ciphertext = %q", enc)
	}
	got, err := c.DecryptString(enc)
	if err != nil {
		t.Fatalf("DecryptString: %v", err)
	}
	if got != plain {
		t.Fatalf("DecryptString = %q, want %q", got, plain)
	}
}

func TestCryptoRejectsGarbage(t *testing.T) {
	c := NewCrypto()
	if _, err := c.DecryptString("not-base64!!"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := c.DecryptString("YWJjZA=="); err == nil {
		t.Fatal("expected dpapi error")
	}
}
