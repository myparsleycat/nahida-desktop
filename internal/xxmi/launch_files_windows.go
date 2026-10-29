package xxmi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi/inject"
)

func protectLaunchFiles(ctx context.Context, spec *inject.LaunchSpec) (func(), error) {
	var files []*platform.LockedExecutable
	closeFiles := func() {
		for _, file := range files {
			_ = file.Close()
		}
	}
	for _, input := range []*inject.VerifiedFile{&spec.LoaderDLL, &spec.LegacyLoader} {
		if input.Path == "" {
			continue
		}
		path := input.Path
		file, err := inject.LockVerifiedFile(ctx, *input)
		if err == nil {
			files = append(files, file)
			input.Path = file.Path
			if input == &spec.LoaderDLL {
				err = verifyInjectorSignature(file.Path, input.SHA256)
			}
		}
		if err != nil {
			closeFiles()
			if infra.IsCancellationError(err) {
				return nil, err
			}
			return nil, infra.AnnotateError(
				fmt.Errorf("XXMI_RUNTIME_CORRUPTED: protect launch file: %w", err),
				infra.Diagnostic{
					Stage: "protect-launch-files", Fields: map[string]any{
						"inputPath": path, "resolvedPath": input.Path, "cleanup": "released",
					},
				},
			)
		}
	}
	return closeFiles, nil
}

func verifyInjectorSignature(path, hash string) error {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "Manifest.json"))
	if err != nil {
		return err
	}
	var manifest xxmiLibraryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	raw, err := hex.DecodeString(hash)
	if err != nil || len(raw) != sha256.Size {
		return fmt.Errorf("invalid injector SHA-256")
	}
	var digest [sha256.Size]byte
	copy(digest[:], raw)
	return verifyPackageDigestSignature(spectrumPublicKey, manifest.Signatures["3dmloader.dll"], digest)
}
