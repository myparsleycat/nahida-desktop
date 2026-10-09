package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/tools/texture"
)

const xxmiLaunchTexturesWhere = "XXMI.launchTextures"

// LaunchUncompressedTextures returns the uncompressed textures of the mods a launch of importer
// loads. It is empty while the guard is off or the launch loads no mods.
func (x *XXMI) LaunchUncompressedTextures(ctx context.Context, importer string) ([]texture.UncompressedTexture, error) {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	none := []texture.UncompressedTexture{}
	guards, err := x.launchGuards(ctx)
	if err != nil || !guards.textures {
		return none, err
	}
	mods, err := x.launchModsFolder(ctx, importer)
	if err != nil || mods == "" {
		return none, err
	}

	textures, err := texture.FindUncompressed(ctx, mods)
	if err != nil {
		return none, x.reportLaunchTextures(err, "scan", importer, mods)
	}
	return textures, nil
}

// CompressLaunchTextures compresses the textures at paths, which are the ones the user was shown. It
// only takes them from a new scan, so it never rewrites a file outside the Mods folder or one that
// LaunchUncompressedTextures would not report.
func (x *XXMI) CompressLaunchTextures(
	ctx context.Context,
	importer string,
	paths []string,
) (texture.TextureResizeResult, error) {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	if !x.acquireImporter(importer) {
		return texture.TextureResizeResult{}, errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(importer)

	mods, err := x.launchModsFolder(ctx, importer)
	if err != nil || mods == "" {
		return texture.TextureResizeResult{Files: []texture.TextureResizeFileResult{}}, err
	}
	client, err := x.settingsClient()
	if err != nil {
		return texture.TextureResizeResult{}, err
	}
	backup, err := texture.ResizeBackupEnabled(ctx, client)
	if err != nil {
		return texture.TextureResizeResult{}, x.reportLaunchTextures(err, "read-settings", importer, mods)
	}
	textures, err := texture.FindUncompressed(ctx, mods)
	if err != nil {
		return texture.TextureResizeResult{}, x.reportLaunchTextures(err, "scan", importer, mods)
	}
	textures = lo.Filter(textures, func(found texture.UncompressedTexture, _ int) bool {
		return lo.ContainsBy(paths, func(path string) bool { return platform.SamePathFold(path, found.Path) })
	})

	result, err := texture.CompressUncompressed(ctx, textures, backup, func(done, total int, path string) {
		if x.eventEmit != nil {
			x.eventEmit("xxmi:texture-compress-progress", map[string]any{
				"importer": importer, "done": done, "total": total, "file": path,
			})
		}
	})
	result.TargetPath = mods
	failures := &infra.DiagnosticBatch{}
	for _, file := range result.Files {
		if file.Status == "failed" && file.Message != nil {
			failures.Add(fmt.Errorf("%s: %s", file.FilePath, *file.Message))
		}
	}
	failures.Report(x.log, xxmiLaunchTexturesWhere, "compress-launch-textures")
	if err != nil {
		return result, x.reportLaunchTextures(err, "compress", importer, mods)
	}
	if x.log != nil {
		x.log.Info(map[string]any{
			"importer": importer, "modsFolder": mods, "backup": backup,
			"updated": result.Updated, "skipped": result.Skipped, "failed": result.Failed,
		}, xxmiLaunchTexturesWhere)
	}
	return result, nil
}

// launchModsFolder returns the Mods folder a launch of importer loads, or "" when it loads none.
func (x *XXMI) launchModsFolder(ctx context.Context, importer string) (string, error) {
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return "", err
	}
	folder := ""
	if external {
		launcher, err := x.loadExternalLauncher(ctx)
		if err != nil || launcher == nil {
			return "", err
		}
		if _, ok := launcher.parsed.Importers[importer]; !ok {
			return "", nil
		}
		folder = launcher.importerFolder(importer)
	} else {
		cfg, err := x.GetImporterConfig(ctx, importer)
		if err != nil || cfg.ImporterFolder == "" {
			return "", err
		}
		cfg, _ = withoutReplacedReShadeLibraries(cfg)
		used, err := x.migotoDLLUsed(ctx, cfg)
		if err != nil || !used {
			return "", err
		}
		folder = cfg.ImporterFolder
	}

	mods := filepath.Join(folder, "Mods")
	if info, err := os.Stat(mods); err != nil || !info.IsDir() {
		return "", nil //nolint:nilerr // A launch without a Mods folder has no textures to report.
	}
	return mods, nil
}

func (x *XXMI) reportLaunchTextures(err error, stage, importer, mods string) error {
	if infra.IsCancellationError(err) {
		return err
	}
	return infra.ReportError(x.log, err, xxmiLaunchTexturesWhere, infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: "launch-textures", Stage: stage,
		Fields: map[string]any{"importer": importer, "modsFolder": mods},
	})
}
