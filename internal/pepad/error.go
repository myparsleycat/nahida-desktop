package pepad

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidPE        = errors.New("invalid PE input")
	ErrUnsupported      = errors.New("unsupported input")
	ErrOverflow         = errors.New("integer overflow")
	ErrAddress          = errors.New("address conversion failed")
	ErrAuthenticode     = errors.New("input contains an Authenticode certificate table; pass the explicit allow option to produce an invalidated signature")
	ErrValidation       = errors.New("transformation validation failed")
	ErrNotDeterministic = errors.New("patch planning was not deterministic")
)

func invalidPE(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidPE, msg)
}

func unsupported(msg string) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, msg)
}

func overflow(context string) error {
	return fmt.Errorf("%w while processing %s", ErrOverflow, context)
}

func addressErr(msg string) error {
	return fmt.Errorf("%w: %s", ErrAddress, msg)
}

func validationErr(msg string) error {
	return fmt.Errorf("%w: %s", ErrValidation, msg)
}

func addU32(a, b uint32, context string) (uint32, error) {
	sum := uint64(a) + uint64(b)
	if sum > uint64(^uint32(0)) {
		return 0, overflow(context)
	}
	return uint32(sum), nil
}

func addUsize(a, b int, context string) (int, error) {
	if a < 0 || b < 0 {
		return 0, overflow(context)
	}
	sum := a + b
	if sum < a {
		return 0, overflow(context)
	}
	return sum, nil
}
