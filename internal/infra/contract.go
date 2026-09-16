package infra

// ContractError preserves user-facing Electron error text, including its
// original capitalisation and punctuation.
type ContractError string

func (e ContractError) Error() string { return string(e) }
