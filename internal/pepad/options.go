package pepad

// programVersion is part of candidateSeed and must stay byte-for-byte aligned
// with native/pe-padding-diversifier/Cargo.toml in the Electron source tree.
const programVersion = "0.1.0"

type Options struct {
	Seed                  uint64 `json:"seed"`
	MinimumSledLength     int    `json:"minimum_sled_length"`
	MaximumMutations      *int   `json:"maximum_mutations"`
	DryRun                bool   `json:"dry_run"`
	AllowInvalidSignature bool   `json:"allow_invalid_signature"`
	AllowZeroPadding      bool   `json:"allow_zero_padding"`
	DLLOnly               bool   `json:"dll_only"`
}

func DefaultOptions() Options {
	return Options{MinimumSledLength: 8, DLLOnly: true}
}
