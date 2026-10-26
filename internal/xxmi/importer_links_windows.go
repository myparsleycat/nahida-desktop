//go:build windows

package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type importerFolderLink struct {
	importer string
	path     string
	// target is the link's own target as stored, which a failed repair restores.
	target   string
	symlink  bool
	physical string
	// replace turns the link into a real folder holding a copy of its target instead of repointing it.
	replace bool
}

// RepairImporterLinks repairs the folder links at the top of an importer folder that earlier imports left. A link
// that reaches its folder through another link, such as a Mods junction to an external launcher's Mods symbolic
// link, is repointed straight at the physical folder. A ShaderFixes link, which an earlier import made to the
// external launcher's ShaderFixes, becomes a real folder: the package owns ShaderFixes, so a shared one lets
// both launchers overwrite each other's shaders and blocks auto_update.xcmd deletes. An importer that is running
// or busy is left for the next start.
//
//wails:ignore
func (x *XXMI) RepairImporterLinks(ctx context.Context) error {
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
	var links []importerFolderLink
	var errs []error
	watched := map[string][]string{}
	for _, row := range rows {
		var cfg ImporterConfig
		if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
			errs = append(errs, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "repair-importer-links", Stage: "read-config", Fields: map[string]any{"importer": row.Key},
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
				Operation: "repair-importer-links", Stage: "read-importer-folder",
				Fields: map[string]any{"importer": row.Key, "importer_folder": cfg.ImporterFolder},
			}))
			continue
		}
		found := false
		for _, entry := range entries {
			link, ok := folderLink(filepath.Join(cfg.ImporterFolder, entry.Name()))
			switch {
			case !ok:
				continue
			case strings.EqualFold(entry.Name(), "ShaderFixes"):
				link.replace = true
			case !link.chained():
				continue
			}
			link.importer = row.Key
			links = append(links, link)
			found = true
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
		stage, repair := "relink", relinkFolder
		if link.replace {
			stage, repair = "replace-link", replaceFolderLink
		}
		err := repair(ctx, link)
		x.releaseImporter(link.importer)
		fields := map[string]any{
			"importer": link.importer, "link": link.path, "previousTarget": link.target, "target": link.physical,
			"replaced": link.replace,
		}
		if err != nil {
			errs = append(errs, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "repair-importer-links", Stage: stage, Fields: fields,
			}))
			continue
		}
		if x.log != nil {
			x.log.Info(fields, "XXMI.RepairImporterLinks")
		}
	}
	return errors.Join(errs...)
}

// chainedLink reports a folder link whose stored target is not where the folder physically is, which means the
// target is itself reached through a link.
func chainedLink(path string) (importerFolderLink, bool) {
	link, ok := folderLink(path)
	return link, ok && link.chained()
}

// folderLink reports a link to a folder on a local drive.
func folderLink(path string) (importerFolderLink, bool) {
	info, err := os.Lstat(path)
	if err != nil || !isInstallReparsePoint(info) {
		return importerFolderLink{}, false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return importerFolderLink{}, false
	}
	physical, err := platform.FinalPath(path)
	if err != nil || !isLocalDrivePath(physical) {
		return importerFolderLink{}, false
	}
	if folder, err := os.Stat(physical); err != nil || !folder.IsDir() {
		return importerFolderLink{}, false
	}
	return importerFolderLink{
		path: path, target: target, symlink: info.Mode()&os.ModeSymlink != 0, physical: physical,
	}, true
}

func (l importerFolderLink) chained() bool {
	resolved := l.target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(l.path), resolved)
	}
	return !platform.SamePathFold(filepath.Clean(resolved), l.physical)
}

func relinkFolder(_ context.Context, link importerFolderLink) error {
	if err := os.Remove(link.path); err != nil {
		return err
	}
	err := writeJunction(link.path, link.physical)
	if err == nil {
		return nil
	}
	return errors.Join(err, restoreFolderLink(link))
}

// replaceFolderLink swaps the link for a real folder holding a copy of its target. The target stays as it was,
// since it still belongs to whoever made it, such as an external launcher.
func replaceFolderLink(ctx context.Context, link importerFolderLink) error {
	release, err := diskio.AcquireDir(ctx, link.physical, filepath.Dir(link.path))
	if err != nil {
		return err
	}
	defer release()

	// The copy drops links and special files, so a target holding one keeps its link rather than losing what is
	// reachable through it.
	err = filepath.WalkDir(link.physical, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == link.physical {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if isInstallReparsePoint(info) || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("folder link target holds an entry a copy would drop: %q", path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	temporary, err := os.MkdirTemp(filepath.Dir(link.path), filepath.Base(link.path)+".nahida-")
	if err != nil {
		return err
	}
	if err := copyTreeContext(ctx, link.physical, temporary); err != nil {
		return errors.Join(err, os.RemoveAll(temporary))
	}
	if err := os.Remove(link.path); err != nil {
		return errors.Join(err, os.RemoveAll(temporary))
	}
	if err := os.Rename(temporary, link.path); err != nil {
		return errors.Join(err, restoreFolderLink(link), os.RemoveAll(temporary))
	}
	return nil
}

func restoreFolderLink(link importerFolderLink) error {
	if link.symlink {
		return os.Symlink(link.target, link.path)
	}
	return writeJunction(link.path, link.target)
}
