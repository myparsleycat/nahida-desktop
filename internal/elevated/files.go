package elevated

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

type FileOpKind string

const (
	FileOpCopy   FileOpKind = "copy"
	FileOpRemove FileOpKind = "remove"
)

// FileOp changes one file in a location the unelevated application cannot write to. A copy
// replaces Target atomically with the contents of Source, which must hash to SHA256; the hash
// binds the request to the bytes the application staged.
type FileOp struct {
	Kind   FileOpKind `json:"kind"`
	Source string     `json:"source,omitempty"`
	Target string     `json:"target"`
	SHA256 string     `json:"sha256,omitempty"`
}

// NewCopyOp describes replacing target with source, bound to the bytes source holds now.
func NewCopyOp(source, target string) (FileOp, error) {
	file, err := os.Open(source)
	if err != nil {
		return FileOp{}, err
	}
	defer func() { _ = file.Close() }()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return FileOp{}, err
	}
	return FileOp{
		Kind: FileOpCopy, Source: source, Target: target, SHA256: hex.EncodeToString(digest.Sum(nil)),
	}, nil
}
