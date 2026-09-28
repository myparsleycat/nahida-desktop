package xxmi

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	spectrumPublicKey = "MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAEYac352uRGKZh6LOwK0fVDW/TpyECEfnRtUp+bP2PJPP63SWOkJ3a/d9pAnPfYezRVJ1hWjZtpRTT8HEAN/b4mWpJvqO43SAEV/1Q6vz9Rk/VvRV3jZ6B/tmqVnIeHKEb"
	gimiPublicKey     = "MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAET5SWORxEdlJ3RXWIFiuwMX6oyZedz+DgaxtsbpWyxNQJDgIDj4uKLLJlvhRNpnkFEuQntgJKzJs0SpASBEguPOTE7VSnmp+x5uyDmsQsWzsRSAZip++a02jqR/K2j18H"
	zzmiPublicKey     = "MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAEb11GjbKQS6SmRe8TcIc5VMu5Ob3moo5v2YeD+s53xEe4bVPGcToUNLu3Jgqo0OwWZ4RsNy1nR0HId6pR09HedyEMifxebsyPT3T5PH82QozEXHQlTDySklWUfGItoOdf"
	himiPublicKey     = "MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAEeigvK7REsX3f/vb+RRuFkZt/6VRbykI2oQcEU3IiI3N9s6jWqKkxAE2cTC9wKXDlkeSzlHjPxgzrTrKdqwkFzROMjw5T2LixFB5BYaT633aU/cCiHDbArIJ46+GrqemG"
)

func verifyPackageSignature(publicKey, signature string, data []byte) error {
	digest := sha256.Sum256(data)
	return verifyPackageDigestSignature(publicKey, signature, digest)
}

func verifyPackageDigestSignature(publicKey, signature string, digest [sha256.Size]byte) error {
	der, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return fmt.Errorf("decode package public key: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return fmt.Errorf("parse package public key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve.Params().BitSize != 384 {
		return errors.New("package public key is not ECDSA P-384")
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("decode package signature: %w", err)
	}
	if !ecdsa.VerifyASN1(key, digest[:], sig) {
		return errors.New("package signature verification failed")
	}
	return nil
}
