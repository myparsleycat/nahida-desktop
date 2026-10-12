package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const packageVerificationName = ".nahida-package-verification.json"

type PackageVerification struct {
	Version string `json:"version"`
	Method  string `json:"method"`
}

func (x *XXMI) GetImporterPackageVerification(ctx context.Context, key string) (*PackageVerification, error) {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return nil, err
	}
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return nil, fmt.Errorf("unknown importer %q", key)
	}
	record, err := readPackageVerification(cfg.ImporterFolder)
	if err != nil || record == nil {
		return nil, err
	}
	installed := readImporterVersion(cfg.ImporterFolder, spec)
	if installed == nil || *installed != record.Version {
		return nil, nil
	}
	return record, nil
}

// readPackageVerification returns the record the last installation into the importer folder left, or nil when
// there is none. It may be of another version than the package files now in the folder.
func readPackageVerification(importerFolder string) (*PackageVerification, error) {
	root, err := openInstallRoot(importerFolder)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	data, _, err := root.readFile(packageVerificationName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 4096 {
		return nil, errors.New("package verification record exceeds size limit")
	}
	var record PackageVerification
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("decode package verification record: %w", err)
	}
	if record.Method != "ecdsa" && record.Method != "digest" && record.Method != "none" {
		return nil, errors.New("invalid package verification method")
	}
	return &record, nil
}
