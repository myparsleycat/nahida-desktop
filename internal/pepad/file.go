package pepad

import (
	"os"
	"path/filepath"
)

func DiversifyFile(inputPath, outputPath string) (Report, error) {
	input, err := os.ReadFile(inputPath)
	if err != nil {
		return Report{}, err
	}
	opts := DefaultOptions()
	opts.AllowInvalidSignature = true
	result, err := Transform(input, opts)
	if err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil && filepath.Dir(outputPath) != "" && filepath.Dir(outputPath) != "." {
		return Report{}, err
	}
	if err := os.WriteFile(outputPath, result.Output, 0o600); err != nil {
		return Report{}, err
	}
	return result.Report, nil
}
