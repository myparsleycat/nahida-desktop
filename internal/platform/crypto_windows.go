package platform

import (
	"encoding/base64"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const cryptProtectUIForbidden uint32 = 0x1

func (c *Crypto) EncryptString(s string) (string, error) {
	blob, err := cryptProtect([]byte(s))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(blob), nil
}

func (c *Crypto) DecryptString(s string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("dpapi ciphertext: %w", err)
	}
	plain, err := cryptUnprotect(raw)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func cryptProtect(plain []byte) ([]byte, error) {
	in := bytesBlob(plain)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("dpapi protect: %w", err)
	}
	defer localFreeBlob(out)
	return copyBlob(out), nil
}

func cryptUnprotect(cipher []byte) ([]byte, error) {
	in := bytesBlob(cipher)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("dpapi unprotect: %w", err)
	}
	defer localFreeBlob(out)
	return copyBlob(out), nil
}

func bytesBlob(b []byte) windows.DataBlob {
	if len(b) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func copyBlob(blob windows.DataBlob) []byte {
	if blob.Size == 0 || blob.Data == nil {
		return []byte{}
	}
	src := unsafe.Slice(blob.Data, blob.Size)
	out := make([]byte, len(src))
	copy(out, src)
	return out
}

func localFreeBlob(blob windows.DataBlob) {
	if blob.Data == nil {
		return
	}
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
}
