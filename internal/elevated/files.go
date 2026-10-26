package elevated

type FileOpKind string

const (
	FileOpCopy        FileOpKind = "copy"
	FileOpRemove      FileOpKind = "remove"
	FileOpResetAccess FileOpKind = "reset-access"
)

// FileOp changes one file in a location the unelevated application cannot write to. A copy
// replaces Target atomically with the contents of Source, which must hash to SHA256; the hash
// binds the request to the bytes the application staged. A reset-access hands Target, a file or
// folder, to the account the application runs as and replaces its permissions with the ones it
// inherits from its folder, as icacls /reset does.
type FileOp struct {
	Kind   FileOpKind `json:"kind"`
	Source string     `json:"source,omitempty"`
	Target string     `json:"target"`
	SHA256 string     `json:"sha256,omitempty"`
}
