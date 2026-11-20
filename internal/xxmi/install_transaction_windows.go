//go:build windows

package xxmi

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	installStatePrepared      = "prepared"
	installStateTreeBackedUp  = "tree-backed-up"
	installStateTreeInstalled = "tree-installed"
	installStateCommitted     = "committed"
)

type importerInstallJournal struct {
	State     string   `json:"state"`
	HadLive   bool     `json:"hadLive"`
	Preserved []string `json:"preserved,omitempty"`
}

type importerInstallTransaction struct {
	parent       *installRoot
	configRoot   *installRoot
	liveName     string
	stageName    string
	backupName   string
	journalName  string
	configBackup string
	configName   string
	hadLive      bool
	liveIdentity os.FileInfo
	configRaw    []byte
	configInfo   *installFileSnapshot
	state        string
	// preserved names the live entries, relative to the importer folder, that commit moves into the new tree.
	preserved []string
	// dropped names the auto_update.xcmd delete targets, which the new tree does not take over from live.
	dropped []string
	// restoreAccess, when set, gives the account back the live folders it may not list and the entries it may not
	// move or remove, so commit and cleanup can go on.
	restoreAccess func(ctx context.Context, paths []string) error
}

func beginImporterInstallTransaction(
	ctx context.Context,
	importerFolder string,
	configPath string,
	importerKey string,
	restoreAccess func(context.Context, []string) error,
) (*importerInstallTransaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	importerAbsolute, err := filepath.Abs(importerFolder)
	if err != nil {
		return nil, err
	}
	importerAbsolute = filepath.Clean(importerAbsolute)
	if info, err := os.Lstat(importerAbsolute); err == nil && isInstallReparsePoint(info) {
		// Replace the link's target rather than the link, so a relocated importer folder stays where the user put it.
		if importerAbsolute, err = filepath.EvalSymlinks(importerAbsolute); err != nil {
			return nil, err
		}
	}
	if filepath.Dir(importerAbsolute) == importerAbsolute {
		return nil, errors.New("importer folder cannot be a volume root")
	}
	parent, err := openInstallRoot(filepath.Dir(importerAbsolute))
	if err != nil {
		return nil, err
	}

	var configRoot *installRoot
	configAbsolute := ""
	if configPath != "" {
		configAbsolute, err = filepath.Abs(configPath)
		if err != nil {
			_ = parent.Close()
			return nil, err
		}
		configAbsolute = filepath.Clean(configAbsolute)
		configRoot, err = openInstallRoot(filepath.Dir(configAbsolute))
		if err != nil {
			_ = parent.Close()
			return nil, err
		}
	}

	prefix := ".nahida-" + strings.ToLower(importerKey) + "-install"
	transaction := &importerInstallTransaction{
		parent:       parent,
		configRoot:   configRoot,
		liveName:     filepath.Base(importerAbsolute),
		stageName:    prefix + ".stage",
		backupName:   prefix + ".backup",
		journalName:  prefix + ".json",
		configBackup: prefix + ".config-backup",
		configName:   filepath.Base(configAbsolute),
		// Recovery may have to remove a replaced tree this account cannot delete.
		restoreAccess: restoreAccess,
	}
	if err := transaction.recoverInterrupted(); err != nil {
		_ = transaction.Close()
		return nil, fmt.Errorf("recover interrupted importer installation: %w", err)
	}

	if configRoot != nil {
		configRaw, configInfo, err := transaction.configRoot.readFile(transaction.configName)
		if err != nil {
			_ = transaction.Close()
			return nil, err
		}
		transaction.configRaw = configRaw
		transaction.configInfo = configInfo
	}

	liveInfo, err := transaction.parent.childInfo(transaction.liveName)
	switch {
	case err == nil:
		if !liveInfo.IsDir() {
			_ = transaction.Close()
			return nil, fmt.Errorf("importer folder is not a directory: %q", importerAbsolute)
		}
		transaction.hadLive = true
		transaction.liveIdentity = liveInfo
	case errors.Is(err, os.ErrNotExist):
	default:
		_ = transaction.Close()
		return nil, err
	}
	return transaction, nil
}

func (t *importerInstallTransaction) Close() error {
	if t.configRoot == nil {
		return t.parent.Close()
	}
	return errors.Join(t.configRoot.Close(), t.parent.Close())
}

func (t *importerInstallTransaction) prepare(ctx context.Context) (*installRoot, error) {
	if err := t.writeJournal(ctx, installStatePrepared); err != nil {
		return nil, err
	}
	if t.configRoot != nil {
		if err := t.parent.writeFileAtomic(
			ctx, t.configBackup, bytes.NewReader(t.configRaw), 0o600, nil,
		); err != nil {
			return nil, err
		}
	}
	if err := t.parent.root.Mkdir(t.stageName, 0o755); err != nil {
		return nil, err
	}
	return t.parent.openChild(t.stageName)
}

// dropDeletes keeps the targets of one auto_update.xcmd section read from commandRoot out of the live entries the
// new tree takes over, and returns them.
func (t *importerInstallTransaction) dropDeletes(commandRoot *installRoot, section string) ([]string, error) {
	targets, err := xcmdDeleteTargets(commandRoot, section)
	if err != nil {
		return nil, err
	}
	t.dropped = append(t.dropped, targets...)
	return targets, nil
}

// applyDeletes drops the targets of one auto_update.xcmd section like dropDeletes and removes them from the stage.
func (t *importerInstallTransaction) applyDeletes(
	ctx context.Context, commandRoot, stage *installRoot, section string,
) error {
	targets, err := t.dropDeletes(commandRoot, section)
	if err != nil {
		return err
	}
	_, err = removeXcmdTargets(ctx, stage, targets)
	return err
}

// readLiveFile reads a top-level file of the live importer folder. A linked file reads as missing, because the
// new tree keeps the link instead of a package copy.
func (t *importerInstallTransaction) readLiveFile(name string) ([]byte, error) {
	if !t.hadLive {
		return nil, os.ErrNotExist
	}
	live, err := t.parent.openChild(t.liveName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = live.Close() }()
	data, _, err := live.readFile(name)
	if errors.Is(err, errInstallReparsePoint) {
		return nil, os.ErrNotExist
	}
	return data, err
}

// carryLive sets the live entries the new tree takes over at commit. Folders this account may not list are
// restored in rounds, so the caller's helper lease asks for consent once; restoring a folder can reveal denied
// folders below it. A folder that stays denied fails the installation: it may hold user data.
func (t *importerInstallTransaction) carryLive(ctx context.Context) error {
	live, err := t.parent.openChild(t.liveName)
	if err != nil {
		return err
	}
	defer func() { _ = live.Close() }()
	liveInfo, err := live.root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(t.liveIdentity, liveInfo) {
		return errors.New("importer folder identity changed before commit")
	}
	stage, err := t.parent.openChild(t.stageName)
	if err != nil {
		return err
	}
	defer func() { _ = stage.Close() }()

	restored := map[string]bool{}
	for {
		carried, denied, err := carriedEntries(ctx, live, stage, t.dropped)
		if err != nil {
			return err
		}
		if len(denied) == 0 {
			t.preserved = carried
			return nil
		}

		var paths []string
		for _, entry := range denied {
			if !restored[entry.path] {
				paths = append(paths, entry.path)
			}
		}
		if len(paths) == 0 || t.restoreAccess == nil {
			return importerFileAccessDeniedError(denied[0])
		}
		if restoreErr := t.restoreAccess(ctx, paths); restoreErr != nil {
			// A cancelled restore is not a denied folder, so it must not turn into the access error.
			if ctx.Err() != nil || infra.IsCancellationError(restoreErr) {
				return cmp.Or(ctx.Err(), restoreErr)
			}
			return infra.WithCause(importerFileAccessDeniedError(denied[0]), restoreErr)
		}
		for _, path := range paths {
			restored[path] = true
		}
	}
}

// carriedEntries lists, relative to live, the entries the new tree takes over by rename: the Mods folder, every
// top-level link, nested links the package does not replace, and every other entry the package neither replaces
// nor deletes. It descends only into folders the stage also has or that hold a delete target, so its cost
// follows the package rather than the user's data. The folders it may not list are returned apart.
func carriedEntries(
	ctx context.Context, live, stage *installRoot, dropped []string,
) ([]string, []*installSourceError, error) {
	droppedPaths := make([]string, len(dropped))
	for i, target := range dropped {
		droppedPaths[i] = filepath.Join(live.path, target)
	}
	isDropped := func(relative string) bool {
		path := filepath.Join(live.path, relative)
		return slices.ContainsFunc(droppedPaths, func(target string) bool {
			return platform.SameOrChildPath(target, path)
		})
	}
	holdsDropped := func(relative string) bool {
		path := filepath.Join(live.path, relative)
		return slices.ContainsFunc(droppedPaths, func(target string) bool {
			return platform.SameOrChildPath(path, target)
		})
	}

	var (
		carried []string
		denied  []*installSourceError
	)
	var walk func(dir string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := fs.ReadDir(live.root.FS(), filepath.ToSlash(dir))
		if errors.Is(err, fs.ErrPermission) && dir != "." {
			denied = append(denied, &installSourceError{path: filepath.Join(live.path, dir), err: err})
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			relative := filepath.Join(dir, entry.Name())
			// The listing's own information, since opening a folder this account may not list fails.
			info, err := entry.Info()
			if err != nil {
				return err
			}
			staged, err := stage.root.Lstat(relative)
			inStage := err == nil
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}

			switch {
			case isInstallReparsePoint(info) || (dir == "." && strings.EqualFold(entry.Name(), "Mods")):
				// Top-level user entries are merged with a staged copy at commit; a nested link the package
				// replaces is left behind.
				if dir == "." || !inStage {
					carried = append(carried, relative)
				}
			case isDropped(relative):
			case !inStage:
				if info.IsDir() && holdsDropped(relative) {
					if err := walk(relative); err != nil {
						return err
					}
					continue
				}
				carried = append(carried, relative)
			case info.IsDir() != staged.IsDir():
				// Neither side can replace the other without dropping a folder or a file.
				return fmt.Errorf("XXMI_IMPORTER_ENTRY_CONFLICT: %s", filepath.Join(live.path, relative))
			case info.IsDir():
				if !holdsDropped(relative) {
					stagedEntries, err := fs.ReadDir(stage.root.FS(), filepath.ToSlash(relative))
					if err != nil {
						return err
					}
					// movePreserved replaces an empty staged folder with the live one.
					if len(stagedEntries) == 0 {
						carried = append(carried, relative)
						continue
					}
				}
				if err := walk(relative); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := walk(".")
	return carried, denied, err
}

func isInstallSourceDenied(err error) bool {
	var source *installSourceError
	return errors.As(err, &source) && errors.Is(source, fs.ErrPermission)
}

// importerAccessRestorer returns the access restore for an installation transaction and the release of the
// helper lease its rounds share. A supplied launch lease stays held until the launch releases it.
func (x *XXMI) importerAccessRestorer(
	lease *elevated.FileLease,
) (restore func(context.Context, []string) error, release func()) {
	release = func() {}
	if lease == nil {
		lease = elevated.NewFileLease(x.elevated)
		release = lease.Release
	}
	return func(ctx context.Context, paths []string) error {
		return x.restoreImporterAccess(ctx, lease, paths)
	}, release
}

// restoreImporterAccess hands importer entries this account may not read back to it through the elevated
// helper under lease.
func (x *XXMI) restoreImporterAccess(ctx context.Context, lease *elevated.FileLease, paths []string) error {
	ops := make([]elevated.FileOp, 0, len(paths))
	for _, path := range paths {
		// The helper accepts only the path Windows resolves the entry to. The folder above an unreadable
		// entry was listed, so it resolves even when the entry itself does not open.
		folder, err := platform.FinalPath(filepath.Dir(path))
		if err != nil {
			return err
		}
		ops = append(ops, elevated.FileOp{
			Kind: elevated.FileOpResetAccess, Target: filepath.Join(folder, filepath.Base(path)),
		})
	}

	for batch := range slices.Chunk(ops, 50) {
		if err := lease.Apply(ctx, batch); err != nil {
			return err
		}
	}
	x.log.Warn(map[string]any{"paths": paths}, "XXMI.restoreImporterAccess")
	return nil
}

// importerFileAccessDeniedError names a live folder the current account cannot list, such as a leftover written by
// an elevated launcher, once restoring its access failed or was declined. The folder is not skipped: it may hold
// user data that is not part of the package.
func importerFileAccessDeniedError(err error) error {
	var source *installSourceError
	if !isInstallSourceDenied(err) || !errors.As(err, &source) {
		return err
	}
	return infra.WithCause(fmt.Errorf("XXMI_IMPORTER_FILE_ACCESS_DENIED: %s", source.path), err)
}

// movePreserved moves preserved entries from the backed-up tree into the stage. When the package staged
// content under the same name, a directory's content is written into the preserved entry, following a link as
// the reference launcher's in-place update does, and the staged copy is discarded.
func (t *importerInstallTransaction) movePreserved(ctx context.Context) error {
	for _, name := range t.preserved {
		if err := ctx.Err(); err != nil {
			return err
		}
		from := filepath.Join(t.backupName, name)
		to := filepath.Join(t.stageName, name)
		if _, err := t.parent.root.Lstat(from); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		// A nested entry can sit below a folder only the live tree has, which was kept for a delete target in it.
		if filepath.Dir(name) != "." {
			if err := t.parent.mkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
		}

		staged, err := t.parent.root.Lstat(to)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return err
		case staged.IsDir() && t.parent.root.Remove(to) == nil:
		default:
			target, statErr := os.Stat(filepath.Join(t.parent.path, from))
			if statErr == nil && target.IsDir() && staged.IsDir() {
				if err := copyTreeContext(
					ctx, filepath.Join(t.parent.path, to), filepath.Join(t.parent.path, from),
				); err != nil {
					return fmt.Errorf("merge staged %s into preserved entry: %w", name, err)
				}
			}
			if err := t.parent.root.RemoveAll(to); err != nil {
				return err
			}
		}

		// The backup goes back to the live name on rollback, so an error names the entry there.
		reported := filepath.Join(t.parent.path, t.liveName, name)
		if err := t.renameRestoringAccess(ctx, from, reported, func() error {
			return t.parent.root.Rename(from, to)
		}); err != nil {
			return fmt.Errorf("move preserved %s: %w", name, err)
		}
	}
	return nil
}

// restorePreserved moves preserved entries back into the backup before the new tree is discarded, so a
// rollback never deletes a user's Mods folder.
func (t *importerInstallTransaction) restorePreserved() error {
	for _, name := range t.preserved {
		backup := filepath.Join(t.backupName, name)
		if _, err := t.parent.root.Lstat(backup); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, container := range []string{t.liveName, t.stageName} {
			moved := filepath.Join(container, name)
			if _, err := t.parent.root.Lstat(moved); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
			if err := t.parent.root.Rename(moved, backup); err != nil {
				return fmt.Errorf("restore preserved %s: %w", name, err)
			}
			break
		}
	}
	return nil
}

func (t *importerInstallTransaction) commit(
	ctx context.Context,
	configJSON []byte,
) (map[string]any, parsedConfig, error) {
	if t.configRoot != nil {
		currentConfig, currentInfo, err := t.configRoot.readFile(t.configName)
		if err != nil {
			return nil, parsedConfig{}, err
		}
		if !os.SameFile(t.configInfo.info, currentInfo.info) || !bytes.Equal(t.configRaw, currentConfig) {
			return nil, parsedConfig{}, errors.New("XXMI configuration changed during installation")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, parsedConfig{}, err
	}

	if t.hadLive {
		if err := t.carryLive(ctx); err != nil {
			return nil, parsedConfig{}, err
		}
		if err := t.backUpLive(ctx); err != nil {
			return nil, parsedConfig{}, err
		}
	}
	if err := t.writeJournal(context.Background(), installStateTreeBackedUp); err != nil {
		return nil, parsedConfig{}, err
	}
	if err := t.movePreserved(ctx); err != nil {
		return nil, parsedConfig{}, err
	}
	if err := t.parent.renameChild(t.stageName, t.liveName); err != nil {
		return nil, parsedConfig{}, importerFolderInUseError(err)
	}
	if err := t.writeJournal(context.Background(), installStateTreeInstalled); err != nil {
		return nil, parsedConfig{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, parsedConfig{}, err
	}

	var loaded map[string]any
	var parsed parsedConfig
	if t.configRoot != nil {
		if err := t.configRoot.writeFileAtomic(
			context.Background(), t.configName, bytes.NewReader(configJSON), 0o644, t.configInfo,
		); err != nil {
			return nil, parsedConfig{}, err
		}
		written, _, err := t.configRoot.readFile(t.configName)
		if err != nil {
			return nil, parsedConfig{}, err
		}
		loaded, parsed, err = parseAndValidateConfig(written)
		if err != nil {
			return nil, parsedConfig{}, err
		}
	}
	if err := t.writeJournal(context.Background(), installStateCommitted); err != nil {
		return nil, parsedConfig{}, err
	}
	t.state = installStateCommitted
	return loaded, parsed, nil
}

func (t *importerInstallTransaction) backUpLive(ctx context.Context) error {
	return t.renameRestoringAccess(ctx, t.liveName, filepath.Join(t.parent.path, t.liveName), func() error {
		currentLive, err := t.parent.childInfo(t.liveName)
		if err != nil {
			return err
		}
		if !os.SameFile(t.liveIdentity, currentLive) {
			return errors.New("importer folder identity changed before replacement")
		}
		return t.parent.renameChild(t.liveName, t.backupName)
	})
}

// renameRestoringAccess runs rename, which moves the entry at relative below the parent. A rename refused by
// the entry's own permissions rather than by a file open below it has them restored once and runs again; the
// restore is skipped for a link, which it refuses. Errors name the entry as reported.
func (t *importerInstallTransaction) renameRestoringAccess(
	ctx context.Context, relative, reported string, rename func() error,
) error {
	err := rename()
	if err == nil {
		return nil
	}
	path := filepath.Join(t.parent.path, relative)
	if info, statErr := t.parent.root.Lstat(relative); statErr != nil || isInstallReparsePoint(info) ||
		!errors.Is(err, windows.ERROR_ACCESS_DENIED) || !deleteAccessDenied(path) {
		return importerFolderInUseError(err)
	}

	if t.restoreAccess == nil {
		return importerFolderAccessDeniedError(reported, err)
	}
	if restoreErr := t.restoreAccess(ctx, []string{path}); restoreErr != nil {
		if ctx.Err() != nil || infra.IsCancellationError(restoreErr) {
			return cmp.Or(ctx.Err(), restoreErr)
		}
		return infra.WithCause(importerFolderAccessDeniedError(reported, err), restoreErr)
	}

	// Restored permissions inherit from the parent, which may itself still refuse the rename.
	err = rename()
	if err != nil && errors.Is(err, windows.ERROR_ACCESS_DENIED) && deleteAccessDenied(path) {
		return importerFolderAccessDeniedError(reported, err)
	}
	return importerFolderInUseError(err)
}

// deleteAccessDenied reports whether the folder's permissions refuse this account DELETE. Opening for DELETE
// is checked against the folder and its parent only, so unlike a rename it still succeeds while a file below
// the folder is open.
func deleteAccessDenied(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	handle, err := windows.CreateFile(
		name, windows.DELETE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0,
	)
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	_ = windows.CloseHandle(handle)
	return false
}

// importerFolderAccessDeniedError names an importer folder this account may not move, once restoring its
// permissions failed or was declined.
func importerFolderAccessDeniedError(path string, err error) error {
	return infra.WithCause(fmt.Errorf("XXMI_IMPORTER_FOLDER_ACCESS_DENIED: %s", path), err)
}

// importerFolderInUseError names a denied importer folder swap. Windows refuses to rename a directory while any
// file below it is open and reports that as an access error, which otherwise reads like a permission problem.
func importerFolderInUseError(err error) error {
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return fmt.Errorf("XXMI_IMPORTER_FOLDER_IN_USE: %w", err)
	}
	return err
}

func (t *importerInstallTransaction) rollback() error {
	journal := importerInstallJournal{State: t.state, HadLive: t.hadLive}
	return t.rollbackJournal(context.Background(), journal)
}

func (t *importerInstallTransaction) finish() error {
	var result error
	for _, name := range []string{t.backupName, t.stageName, t.configBackup} {
		result = errors.Join(result, t.removeRestoringAccess(context.Background(), name))
	}
	if result != nil {
		return result
	}
	return t.parent.removeAll(t.journalName)
}

// removeRestoringAccess removes the parent's child name. Entries left behind because this account may not
// delete them, such as a replaced tree whose files kept another account's permissions, have their access
// restored and the removal runs again. A restore can expose entries that inherited delete access from a
// folder it changed, so rounds repeat until no new denied entry is found.
func (t *importerInstallTransaction) removeRestoringAccess(ctx context.Context, name string) error {
	err := t.parent.removeAll(name)
	if err == nil || t.restoreAccess == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}

	restored := map[string]bool{}
	for {
		denied, scanErr := undeletableInstallEntries(ctx, filepath.Join(t.parent.path, name))
		if scanErr != nil {
			return infra.WithCause(err, scanErr)
		}
		denied = slices.DeleteFunc(denied, func(path string) bool { return restored[path] })
		if len(denied) == 0 {
			return importerFolderInUseError(err)
		}
		if restoreErr := t.restoreAccess(ctx, denied); restoreErr != nil {
			if ctx.Err() != nil || infra.IsCancellationError(restoreErr) {
				return cmp.Or(ctx.Err(), restoreErr)
			}
			return infra.WithCause(importerFolderAccessDeniedError(denied[0], err), restoreErr)
		}
		for _, path := range denied {
			restored[path] = true
		}
		if err = t.parent.removeAll(name); err == nil || !errors.Is(err, fs.ErrPermission) {
			return err
		}
	}
}

// undeletableInstallEntries lists, parents first, the entries at and below root that this account may not
// delete and the folders it may not list. Links are left out: removal deletes the link, not its target.
func undeletableInstallEntries(ctx context.Context, root string) ([]string, error) {
	var denied []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if entry == nil || !errors.Is(walkErr, fs.ErrPermission) {
				return walkErr
			}
			// A folder that cannot be listed is reported again after its own visit.
			if !slices.Contains(denied, path) {
				denied = append(denied, path)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if isInstallReparsePoint(info) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if deleteAccessDenied(path) {
			denied = append(denied, path)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return denied, nil
	}
	return denied, err
}

func (t *importerInstallTransaction) recoverInterrupted() error {
	raw, _, err := t.parent.readFile(t.journalName)
	if errors.Is(err, os.ErrNotExist) {
		for _, name := range []string{t.stageName, t.backupName, t.configBackup} {
			if _, childErr := t.parent.childInfo(name); childErr == nil {
				return fmt.Errorf("installation artifact %q exists without a recovery journal", name)
			} else if !errors.Is(childErr, os.ErrNotExist) {
				return childErr
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	var journal importerInstallJournal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return err
	}
	t.preserved = journal.Preserved
	switch journal.State {
	case installStatePrepared, installStateTreeBackedUp, installStateTreeInstalled:
		return t.rollbackJournal(context.Background(), journal)
	case installStateCommitted:
		return t.finish()
	default:
		return fmt.Errorf("invalid importer installation journal state %q", journal.State)
	}
}

func (t *importerInstallTransaction) rollbackJournal(ctx context.Context, journal importerInstallJournal) error {
	var result error
	backupExists := false
	if _, err := t.parent.childInfo(t.backupName); err == nil {
		backupExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		result = errors.Join(result, err)
	}
	if backupExists {
		if err := t.restorePreserved(); err != nil {
			// Keep every tree in place; discarding the new tree now could delete preserved user data.
			return errors.Join(result, err)
		}
		result = errors.Join(result, t.parent.removeAll(t.liveName))
		if result == nil {
			result = errors.Join(result, t.parent.renameChild(t.backupName, t.liveName))
		}
	} else if !journal.HadLive && journal.State != installStatePrepared {
		result = errors.Join(result, t.parent.removeAll(t.liveName))
	}

	if journal.State != installStatePrepared && t.configRoot != nil {
		backupConfig, _, configErr := t.parent.readFile(t.configBackup)
		if configErr == nil {
			result = errors.Join(
				result,
				t.configRoot.writeFileAtomic(ctx, t.configName, bytes.NewReader(backupConfig), 0o644, nil),
			)
		} else if !errors.Is(configErr, os.ErrNotExist) {
			result = errors.Join(result, configErr)
		}
	}
	if result != nil {
		return result
	}
	return t.finish()
}

func (t *importerInstallTransaction) writeJournal(ctx context.Context, state string) error {
	raw, err := json.Marshal(importerInstallJournal{State: state, HadLive: t.hadLive, Preserved: t.preserved})
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := t.parent.writeFileAtomic(ctx, t.journalName, bytes.NewReader(raw), 0o600, nil); err != nil {
		return err
	}
	t.state = state
	return nil
}
