package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

//wails:ignore
func (x *XXMI) AdoptUserRuntime(ctx context.Context, importer string) error {
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return errors.New("XXMI_NOT_CONFIGURED")
	}
	if cfg.Mode != RuntimeXXMI {
		return errors.New("XXMI_LEGACY_RUNTIME_UNSUPPORTED")
	}
	root, err := openInstallRoot(cfg.ImporterFolder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	dll, _, err := root.readFile("d3d11.dll")
	if err != nil {
		return err
	}
	manifest := runtimeManifest{Mode: RuntimeXXMI, Files: map[string]string{}}
	data, info, err := root.readFile(runtimeManifestName)
	if err == nil {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if manifest.UserManaged == nil {
		manifest.UserManaged = map[string]string{}
	}
	manifest.UserManaged["d3d11.dll"] = hashBytes(dll)
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := root.writeFileAtomic(ctx, runtimeManifestName, bytes.NewReader(encoded), 0o600, info); err != nil {
		return err
	}
	cfg.Migoto.UnsafeMode = true
	return x.SaveImporterConfig(ctx, importer, cfg)
}
