package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	keyringEnvelopePrefix = "nhd-keyring-v1:"
	basicTextPrefix       = "nhd-basic-text-v1:"
	cryptoMasterKeySize   = 32
)

func encryptCryptoEnvelope(plain string, key []byte) (string, error) {
	aead, err := newCryptoAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("safe storage nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), nil)
	return keyringEnvelopePrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func decryptCryptoEnvelope(envelope string, key []byte) (string, error) {
	if !strings.HasPrefix(envelope, keyringEnvelopePrefix) {
		return "", errors.New("safe storage ciphertext has an unknown format")
	}
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(envelope, keyringEnvelopePrefix))
	if err != nil {
		return "", fmt.Errorf("safe storage ciphertext: %w", err)
	}
	aead, err := newCryptoAEAD(key)
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", errors.New("safe storage ciphertext is truncated")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("safe storage decrypt: %w", err)
	}
	return string(plain), nil
}

func newCryptoAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != cryptoMasterKeySize {
		return nil, fmt.Errorf("safe storage key length is %d, want %d", len(key), cryptoMasterKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("safe storage cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("safe storage GCM: %w", err)
	}
	return aead, nil
}

func encodeBasicText(plain string) string {
	return basicTextPrefix + base64.RawStdEncoding.EncodeToString([]byte(plain))
}

func decodeBasicText(envelope string) (string, error) {
	if !strings.HasPrefix(envelope, basicTextPrefix) {
		return "", errors.New("safe storage plaintext has an unknown format")
	}
	plain, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(envelope, basicTextPrefix))
	if err != nil {
		return "", fmt.Errorf("safe storage plaintext: %w", err)
	}
	return string(plain), nil
}
