package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/infra"
)

//wails:ignore
func (x *XXMI) AdoptUserRuntime(ctx context.Context, importer string) error {
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return err
	}
	if external {
		return x.enableExternalUnsafeMode(ctx, importer)
	}
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

// RestoreOfficialDLL replaces a user-provided d3d11.dll with the signed XXMI library and turns unsafe mode
// back off. The replaced DLL is kept in the XXMI backup folder by the runtime deployment.
func (x *XXMI) RestoreOfficialDLL(ctx context.Context, importer string) (warnings []string, returnErr error) {
	stage := "acquire"
	folder := ""
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.RestoreOfficialDLL", infra.Diagnostic{
			Operation: "restore-official-dll", Stage: stage,
			Fields: map[string]any{"importer": importer, "importer_folder": folder},
		})
	}()
	if !x.acquireImporter(importer) {
		return nil, errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(importer)

	stage = "load-config"
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return nil, err
	}
	if external {
		return nil, errors.New("XXMI_EXTERNAL_LAUNCHER_UNSUPPORTED")
	}
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return nil, err
	}
	folder = cfg.ImporterFolder
	if !cfg.Enabled {
		return nil, errors.New("XXMI_NOT_CONFIGURED")
	}
	if cfg.Mode != RuntimeXXMI {
		return nil, errors.New("XXMI_LEGACY_RUNTIME_UNSUPPORTED")
	}

	// Without unsafe mode the deployment no longer preserves user-managed files, so it backs up the custom
	// DLL and writes the signed one in its place.
	stage = "deploy-runtime"
	cfg.Migoto.UnsafeMode = false
	warnings, err = x.deployRuntime(ctx, importer, cfg, false)
	if err != nil {
		return nil, err
	}

	stage = "save-config"
	if err := x.SaveImporterConfig(ctx, importer, cfg); err != nil {
		return nil, err
	}
	if x.log != nil {
		x.log.Info(map[string]any{"importer": importer, "importer_folder": folder, "warnings": warnings},
			"XXMI.RestoreOfficialDLL")
	}
	return warnings, nil
}

func usesCustomDLL(folder string) bool {
	data, err := os.ReadFile(filepath.Join(folder, runtimeManifestName))
	if err != nil {
		return false
	}
	var manifest runtimeManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Mode != RuntimeXXMI {
		return false
	}
	return manifest.UserManaged["d3d11.dll"] != ""
}
