//go:build windows

package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type chainedFolderLink struct {
	importer string
	path     string
	// target is the link's own target as stored, which a failed relink restores.
	target   string
	symlink  bool
	physical string
}

// FlattenImporterLinks repoints each folder link at the top of an importer folder that reaches its folder through
// another link, such as a Mods junction an earlier import made to an external launcher's Mods symbolic link,
// straight at the physical folder. An importer that is running or busy is left for the next start.
//
//wails:ignore
func (x *XXMI) FlattenImporterLinks(ctx context.Context) error {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return err
	}

	// An importer that cannot be read is reported and skipped, so it does not hold back the others' repair.
	var links []chainedFolderLink
	var errs []error
	watched := map[string][]string{}
	for _, row := range rows {
		var cfg ImporterConfig
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			errs = append(errs, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "flatten-importer-links", Stage: "read-config", Fields: map[string]any{"importer": row.Key},
			}))
			continue
		}
		if strings.TrimSpace(cfg.ImporterFolder) == "" {
			continue
		}
		entries, err := os.ReadDir(cfg.ImporterFolder)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			errs = append(errs, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "flatten-importer-links", Stage: "read-importer-folder",
				Fields: map[string]any{"importer": row.Key, "importer_folder": cfg.ImporterFolder},
			}))
			continue
		}
		found := false
		for _, entry := range entries {
			if link, ok := chainedLink(filepath.Join(cfg.ImporterFolder, entry.Name())); ok {
				link.importer = row.Key
				links = append(links, link)
				found = true
			}
		}
		if found {
			watched[row.Key] = cfg.gameProcessNames(row.Key)
		}
	}
	if len(links) == 0 {
		return errors.Join(errs...)
	}

	running, err := x.runningImporters(ctx, watched)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	// Watchers hold the linked folders open, which would block replacing the links.
	resume, err := x.beginImporterMaintenance(ctx)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	defer resume(nil)

	for _, link := range links {
		if running[link.importer] || !x.acquireImporter(link.importer) {
			continue
		}
		err := relinkFolder(link)
		x.releaseImporter(link.importer)
		fields := map[string]any{
			"importer": link.importer, "link": link.path, "previousTarget": link.target, "target": link.physical,
		}
		if err != nil {
			errs = append(errs, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "flatten-importer-links", Stage: "relink", Fields: fields,
			}))
			continue
		}
		if x.log != nil {
			x.log.Info(fields, "XXMI.FlattenImporterLinks")
		}
	}
	return errors.Join(errs...)
}

// chainedLink reports a folder link whose stored target is not where the folder physically is, which means the
// target is itself reached through a link.
func chainedLink(path string) (chainedFolderLink, bool) {
	info, err := os.Lstat(path)
	if err != nil || !isInstallReparsePoint(info) {
		return chainedFolderLink{}, false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return chainedFolderLink{}, false
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(path), resolved)
	}
	physical, err := platform.FinalPath(path)
	if err != nil || !isLocalDrivePath(physical) {
		return chainedFolderLink{}, false
	}
	if folder, err := os.Stat(physical); err != nil || !folder.IsDir() {
		return chainedFolderLink{}, false
	}
	link := chainedFolderLink{
		path: path, target: target, symlink: info.Mode()&os.ModeSymlink != 0, physical: physical,
	}
	return link, !platform.SamePathFold(filepath.Clean(resolved), physical)
}

func relinkFolder(link chainedFolderLink) error {
	if err := os.Remove(link.path); err != nil {
		return err
	}
	err := writeJunction(link.path, link.physical)
	if err == nil {
		return nil
	}
	if link.symlink {
		return errors.Join(err, os.Symlink(link.target, link.path))
	}
	return errors.Join(err, writeJunction(link.path, link.target))
}
