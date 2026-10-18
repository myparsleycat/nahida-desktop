//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// ImporterModFolder describes the Mods folder a built-in importer loads. XXMI always reads <importer folder>\Mods,
// so mods kept elsewhere are reached through a junction at that path.
type ImporterModFolder struct {
	Path string `json:"path"`
	// LinkTarget is the folder the Mods junction points to, or empty while Mods is an ordinary folder.
	LinkTarget string `json:"linkTarget"`
	// TargetMissing reports a junction whose target no longer exists, such as one on a removed drive.
	TargetMissing bool `json:"targetMissing"`
	// Warnings lists cleanup that a committed change left undone.
	Warnings []string `json:"warnings,omitempty"`
}

var (
	errModFolderTargetNotEmpty = errors.New("XXMI_MOD_FOLDER_TARGET_NOT_EMPTY")
	errModFolderInvalidTarget  = errors.New("XXMI_MOD_FOLDER_INVALID_TARGET")
)

func (x *XXMI) GetImporterModFolder(ctx context.Context, key string) (ImporterModFolder, error) {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return ImporterModFolder{}, err
	}
	return readImporterModFolder(cfg.ImporterFolder)
}

// SetImporterModFolder makes the importer load mods from target, or from an ordinary Mods folder when target is
// empty. Mods kept in an ordinary Mods folder move to target, which must then be empty. A linked folder keeps its
// mods both when the link moves to another target and when it is removed.
func (x *XXMI) SetImporterModFolder(
	ctx context.Context,
	key, target string,
) (folder ImporterModFolder, returnErr error) {
	stage := "acquire"
	fields := map[string]any{"importer": key, "target": target}
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.SetImporterModFolder", infra.Diagnostic{
			Operation: "set-importer-mod-folder", Stage: stage, Fields: fields,
		})
	}()
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return folder, errors.New("unknown XXMI importer")
	}
	if !x.acquireImporter(spec.key) {
		return folder, errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(spec.key)

	stage = "load-config"
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return folder, err
	}
	if external {
		return folder, errors.New("XXMI_EXTERNAL_LAUNCHER_UNSUPPORTED")
	}
	cfg, err := x.GetImporterConfig(ctx, spec.key)
	if err != nil {
		return folder, err
	}
	fields["importer_folder"] = cfg.ImporterFolder
	if err := validateLocalFolder("importer folder", cfg.ImporterFolder, true); err != nil {
		return folder, err
	}
	if info, err := os.Stat(cfg.ImporterFolder); err != nil || !info.IsDir() {
		return folder, infra.WithCause(errors.New("XXMI_NOT_CONFIGURED"), err)
	}

	stage = "check-running"
	running, err := x.runningImporters(ctx, []string{spec.key})
	if err != nil {
		return folder, err
	}
	if running[spec.key] {
		return folder, errors.New("XXMI_GAME_RUNNING")
	}

	// Watchers hold the Mods folder open, which would block renaming or unlinking it.
	stage = "pause-importer-watchers"
	resume, err := x.beginImporterMaintenance(ctx)
	if err != nil {
		return folder, err
	}
	stage = "relink"
	warnings, err := relinkImporterMods(ctx, spec.key, cfg.ImporterFolder, target, x.renameUserData)
	resume(nil)
	if err != nil {
		return folder, err
	}
	x.notifyImportersChanged(ctx)

	stage = "read-result"
	folder, err = readImporterModFolder(cfg.ImporterFolder)
	folder.Warnings = warnings
	return folder, err
}

func readImporterModFolder(importerFolder string) (ImporterModFolder, error) {
	folder := ImporterModFolder{Path: filepath.Join(filepath.Clean(importerFolder), "Mods")}
	info, err := os.Lstat(folder.Path)
	if errors.Is(err, os.ErrNotExist) {
		return folder, nil
	}
	if err != nil || !isInstallReparsePoint(info) {
		return folder, err
	}
	target, err := os.Readlink(folder.Path)
	if err != nil {
		return folder, fmt.Errorf("read Mods link %q: %w", folder.Path, err)
	}
	folder.LinkTarget = filepath.Clean(target)
	if _, err := os.Stat(folder.Path); errors.Is(err, os.ErrNotExist) {
		folder.TargetMissing = true
	} else if err != nil {
		return folder, err
	}
	return folder, nil
}

// relinkImporterMods points importerFolder\Mods at target, or turns it back into an ordinary folder when target is
// empty or names Mods itself. rename moves an ordinary Mods folder to target.
func relinkImporterMods(
	ctx context.Context,
	key, importerFolder, target string,
	rename func(from, to string) error,
) ([]string, error) {
	importerFolder = filepath.Clean(importerFolder)
	mods := filepath.Join(importerFolder, "Mods")
	unlink := strings.TrimSpace(target) == "" || platform.SamePathFold(filepath.Clean(target), mods)
	if !unlink {
		var err error
		if target, err = validModFolderTarget(importerFolder, target); err != nil {
			return nil, err
		}
	}

	info, err := os.Lstat(mods)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if unlink {
			return nil, os.Mkdir(mods, 0o755)
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, err
		}
		return nil, createJunction(mods, target)
	case err != nil:
		return nil, err
	case isInstallReparsePoint(info):
		return nil, retargetModsLink(mods, target, unlink)
	case !info.IsDir():
		return nil, fmt.Errorf("%w: %q is not a folder", errModFolderInvalidTarget, mods)
	case unlink:
		return nil, nil
	}
	return moveImporterMods(ctx, key, mods, target, rename)
}

// retargetModsLink replaces the Mods junction. The folder it pointed to keeps its mods.
func retargetModsLink(mods, target string, unlink bool) error {
	previous, err := os.Readlink(mods)
	if err != nil {
		return fmt.Errorf("read Mods link %q: %w", mods, err)
	}
	if !unlink && platform.SamePathFold(filepath.Clean(previous), target) {
		return nil
	}
	if !unlink {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
	}

	if err := os.Remove(mods); err != nil {
		return fmt.Errorf("remove Mods link %q: %w", mods, err)
	}
	if unlink {
		err = os.Mkdir(mods, 0o755)
	} else {
		err = createJunction(mods, target)
	}
	if err != nil {
		return errors.Join(err, writeJunction(mods, previous))
	}
	return nil
}

// moveImporterMods carries an ordinary Mods folder over to target and leaves a junction to it in its place.
func moveImporterMods(
	ctx context.Context,
	key, mods, target string,
	rename func(from, to string) error,
) ([]string, error) {
	entries, err := os.ReadDir(mods)
	if err != nil {
		return nil, err
	}
	targetEntries, err := os.ReadDir(target)
	targetExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// An empty Mods folder has nothing to carry over, so it links to target as it is, mods included.
	if len(entries) == 0 {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, err
		}
		if err := os.Remove(mods); err != nil {
			return nil, err
		}
		if err := createJunction(mods, target); err != nil {
			return nil, errors.Join(err, os.Mkdir(mods, 0o755))
		}
		return nil, nil
	}
	if len(targetEntries) > 0 {
		return nil, fmt.Errorf("%w: %q", errModFolderTargetNotEmpty, target)
	}

	// The move recreates target, so an empty one is cleared out of the way and restored on failure.
	if targetExists {
		if err := os.Remove(target); err != nil {
			return nil, err
		}
	} else if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}
	restoreTarget := func() error {
		if !targetExists {
			return nil
		}
		return os.MkdirAll(target, 0o755)
	}

	migration := &importerFolderMigration{mode: ImportUserDataMove, rename: rename}
	if err := migration.move(ctx, key, "Mods", mods, target); err != nil {
		return nil, errors.Join(err, migration.rollback(), restoreTarget())
	}
	copied := migration.moves[len(migration.moves)-1].copied

	// A copy across volumes leaves the source in place, which must make way for the junction.
	staging := ""
	if copied {
		staging = mods + ".nhd-moving-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		if err := os.Rename(mods, staging); err != nil {
			return nil, errors.Join(err, migration.rollback(), restoreTarget())
		}
	}
	if err := createJunction(mods, target); err != nil {
		var restore error
		if copied {
			restore = os.Rename(staging, mods)
		}
		return nil, errors.Join(err, restore, migration.rollback(), restoreTarget())
	}

	// The mods are already safe in target, so a leftover original is reported instead of undoing the move.
	if copied {
		if err := removeUserDataTree(staging); err != nil {
			return []string{fmt.Sprintf("Mods were copied to %s, but the original %s could not be removed: %v",
				target, staging, err)}, nil
		}
	}
	return nil, nil
}

// validModFolderTarget accepts an absolute local folder that neither contains nor lies inside the importer folder,
// and returns where it physically is. A junction to a path through a link, such as one leading back through Mods
// itself, would break once Mods is replaced, so the link is made to the resolved path.
func validModFolderTarget(importerFolder, target string) (string, error) {
	target = strings.TrimSpace(target)
	if validateLocalFolder("mod folder", target, true) != nil || len(filepath.VolumeName(target)) != 2 {
		return "", fmt.Errorf("%w: %q is not a local drive folder", errModFolderInvalidTarget, target)
	}
	target = filepath.Clean(target)

	overlaps := func(folder, other string) bool {
		return platform.SameOrChildPath(folder, other) || platform.SameOrChildPath(other, folder)
	}
	if overlaps(importerFolder, target) {
		return "", fmt.Errorf("%w: %q overlaps the importer folder", errModFolderInvalidTarget, target)
	}
	if info, err := os.Stat(target); err == nil && !info.IsDir() {
		return "", fmt.Errorf("%w: %q is not a folder", errModFolderInvalidTarget, target)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	// A target reached through a link may still resolve into the importer folder.
	physicalTarget, err := platform.PhysicalPath(target)
	if err != nil {
		return "", err
	}
	physicalImporter, err := platform.FinalPath(importerFolder)
	if err != nil {
		return "", err
	}
	if overlaps(physicalImporter, physicalTarget) || len(filepath.VolumeName(physicalTarget)) != 2 {
		return "", fmt.Errorf("%w: %q resolves to %q", errModFolderInvalidTarget, target, physicalTarget)
	}
	return physicalTarget, nil
}

// removeUserDataTree deletes a folder tree without following links: a junction or symbolic link inside it is
// removed itself, never the folder it points to.
func removeUserDataTree(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() && !isInstallReparsePoint(info) {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := removeUserDataTree(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
	}
	return os.Remove(path)
}
