package xxmi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/platform"
)

// ImportUserDataMode decides what happens to an importer's user data when an external launcher is imported: the
// built-in importer folder either links back to the external folders or takes them over.
type ImportUserDataMode string

const (
	ImportUserDataKeep ImportUserDataMode = "keep"
	ImportUserDataMove ImportUserDataMode = "move"
)

// importerUserData lists the importer folder entries that belong to the user rather than the package. Packages and
// XXMI libraries are installed fresh; these are carried over from the external launcher.
var importerUserData = []string{"Mods", "ShaderFixes", "d3dx_user.ini"}

var (
	errImportFolderConflict    = errors.New("XXMI_IMPORT_FOLDER_CONFLICT")
	errImportTargetNotEmpty    = errors.New("XXMI_IMPORT_TARGET_NOT_EMPTY")
	errImportMoveAcrossVolumes = errors.New("XXMI_IMPORT_MOVE_CROSS_VOLUME")
)

type importerFolderMove struct {
	from string
	to   string
}

// importerFolderMigration records every filesystem change made while importing so a failed import can be undone.
type importerFolderMigration struct {
	mode    ImportUserDataMode
	created []string
	links   []string
	moves   []importerFolderMove
}

func checkImporterFolderMigration(mode ImportUserDataMode, key, source, target string) error {
	if platform.SameOrChildPath(source, target) || platform.SameOrChildPath(target, source) {
		return fmt.Errorf("%w: %s folder %q overlaps %q", errImportFolderConflict, key, target, source)
	}

	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case !info.IsDir() || isInstallReparsePoint(info):
		return fmt.Errorf("%w: %s folder %q is not a directory", errImportTargetNotEmpty, key, target)
	default:
		entries, err := os.ReadDir(target)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("%w: %s folder %q", errImportTargetNotEmpty, key, target)
		}
	}

	if mode != ImportUserDataMove || strings.EqualFold(filepath.VolumeName(source), filepath.VolumeName(target)) {
		return nil
	}
	for _, name := range importerUserData {
		if _, err := os.Lstat(filepath.Join(source, name)); err == nil {
			return fmt.Errorf("%w: %s %q to %q", errImportMoveAcrossVolumes, key, source, target)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// prepare creates target and carries the user data over from source before the package is installed into it. The
// installer keeps Mods and top-level links, so the prepared entries survive installation and later updates.
func (m *importerFolderMigration) prepare(source, target string, overwriteINI bool) error {
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	m.created = append(m.created, target)

	// The installer restores an existing d3dx.ini over the packaged one when overwriting is disabled.
	names := importerUserData
	if !overwriteINI {
		names = append([]string{"d3dx.ini"}, importerUserData...)
	}
	for _, name := range names {
		from := filepath.Join(source, name)
		to := filepath.Join(target, name)
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		info, err := os.Stat(from)
		if err != nil {
			return err
		}

		switch {
		case m.mode == ImportUserDataMove && name != "d3dx.ini":
			if err := os.Rename(from, to); err != nil {
				return fmt.Errorf("move %q to %q: %w", from, to, err)
			}
			m.moves = append(m.moves, importerFolderMove{from: from, to: to})
		case info.IsDir():
			if err := createJunction(to, from); err != nil {
				return err
			}
			m.links = append(m.links, to)
		default:
			data, err := os.ReadFile(from)
			if err != nil {
				return err
			}
			if err := os.WriteFile(to, data, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

// rollback unlinks and moves the user data back before removing the folders the import created. A folder that
// still holds moved user data is never removed.
func (m *importerFolderMigration) rollback() error {
	var errs []error
	for _, link := range slices.Backward(m.links) {
		if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	for _, move := range slices.Backward(m.moves) {
		if err := os.Rename(move.to, move.from); err != nil {
			errs = append(errs, fmt.Errorf("restore %q to %q: %w", move.to, move.from, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	for _, folder := range slices.Backward(m.created) {
		if err := os.RemoveAll(folder); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
