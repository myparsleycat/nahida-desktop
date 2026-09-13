package texture

// contractError preserves user-facing Electron error text, including its
// original capitalisation and punctuation.
type contractError string

func (e contractError) Error() string { return string(e) }

func stringPointer(value string) *string { return &value }
