package platform

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCryptoEnvelopeRoundTripAndAuthentication(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, cryptoMasterKeySize)
	envelope, err := encryptCryptoEnvelope("secret-token", key)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptCryptoEnvelope(envelope, key)
	if err != nil || plain != "secret-token" {
		t.Fatalf("decrypt = %q, %v", plain, err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(envelope, keyringEnvelopePrefix))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 1
	tampered := keyringEnvelopePrefix + base64.RawStdEncoding.EncodeToString(raw)
	if _, err := decryptCryptoEnvelope(tampered, key); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestBasicTextEnvelopeRoundTrip(t *testing.T) {
	envelope := encodeBasicText("fallback-secret")
	plain, err := decodeBasicText(envelope)
	if err != nil || plain != "fallback-secret" {
		t.Fatalf("decode = %q, %v", plain, err)
	}
}
