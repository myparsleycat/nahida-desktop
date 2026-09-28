package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

//wails:ignore
func (x *XXMI) DeployedLibsVersion(ctx context.Context, importer string) (string, bool) {
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil || !cfg.Enabled || cfg.Mode != RuntimeXXMI {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName))
	if err != nil {
		return "", false
	}
	var manifest runtimeManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Mode != RuntimeXXMI {
		return "", false
	}
	version, ok := strings.CutPrefix(manifest.Source, "xxmi-libs@")
	return version, ok && version != ""
}
