// Package efmioutline keeps chosen EFMI mod parts out of the outline pass so
// that Vulkan draws them like DX11 does. DX11 users load a community hash list
// that forces the outline vertex shader to filter_index 201; Vulkan shader
// hashes differ, so there the pass stays 203 and draws in "vs == 202 || vs ==
// 203" blocks run a second time as an opaque hull.
package efmioutline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	efmiImporter = "EFMI"
	// The loader reads every .ini below Mods, so the backup must not end in .ini.
	backupSuffix = ".nhd-outline-orig"

	ErrorNotEFMI      = "EFMI_OUTLINE_NOT_EFMI"
	ErrorModified     = "EFMI_OUTLINE_MODIFIED"
	ErrorStale        = "EFMI_OUTLINE_STALE"
	ErrorUnsupported  = "EFMI_OUTLINE_UNSUPPORTED"
	ErrorInvalidPath  = "EFMI_OUTLINE_INVALID_PATH"
	ErrorNotModified  = "EFMI_OUTLINE_NOT_MODIFIED"
	diagnosticWhere   = "EFMIOutline"
	operationPrefix   = "efmi-outline-"
	stageResolve      = "resolve"
	stageLoad         = "load"
	stageWriteBackup  = "write-backup"
	stageWriteFile    = "write-file"
	stageRestore      = "restore"
	stageRemoveBackup = "remove-backup"
)

var disabledNameRE = regexp.MustCompile(`(?i)^disabled`)

type GamePaths interface {
	List(context.Context) ([]db.GamePathRow, error)
}

type Options struct {
	Log       *infra.Log
	GamePaths GamePaths
}

type Service struct {
	log   *infra.Log
	games GamePaths
	mu    sync.Mutex
}

func NewWithOptions(opts Options) *Service {
	return &Service{log: opts.Log, games: opts.GamePaths}
}

//wails:ignore
func (s *Service) UseClient(client *db.Client) {
	if client != nil {
		s.games = client.GamePaths
	}
}

func (s *Service) Inspect(ctx context.Context, modPath string) (OutlineInspection, error) {
	return s.run(ctx, "inspect", modPath, "", func(string, *string) error { return nil })
}

// Apply recomputes every listed file from its original, so applying again
// replaces the previous selection instead of adding to it. An empty selection
// restores the original.
func (s *Service) Apply(ctx context.Context, input OutlineApplyInput) (OutlineInspection, error) {
	return s.run(ctx, "apply", input.ModPath, "", func(root string, stage *string) error {
		type plan struct {
			file   *loadedFile
			output []byte
		}
		var plans []plan
		for _, selection := range input.Files {
			file, err := loadValidated(root, selection.Path)
			if err != nil {
				return err
			}
			if err := file.requireEditable(); err != nil {
				return err
			}
			selected, err := file.selection(selection.PartIDs)
			if err != nil {
				return err
			}
			if len(selected) == 0 {
				plans = append(plans, plan{file: file})
				continue
			}
			output, err := applyExclusions(file.baseDoc, selected)
			if err != nil {
				return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": file.rel}})
			}
			plans = append(plans, plan{file: file, output: output})
		}

		// One apply covers every file of the mod, so a failed write puts back the
		// files this call already changed instead of leaving the mod half applied.
		var written []*loadedFile
		for _, p := range plans {
			err := p.file.write(p.output, stage)
			if err == nil {
				if p.file.written {
					written = append(written, p.file)
				}
				continue
			}

			rollback := "none"
			if len(written) > 0 {
				rollback = "restored"
				if rollbackErr := rollbackFiles(written); rollbackErr != nil {
					rollback = "failed"
					err = errors.Join(err, rollbackErr)
				}
			}
			rolledBack := make([]string, 0, len(written))
			for _, file := range written {
				rolledBack = append(rolledBack, file.rel)
			}
			return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{
				"file":       p.file.rel,
				"rollback":   rollback,
				"rolledBack": rolledBack,
			}})
		}
		return nil
	})
}

func rollbackFiles(files []*loadedFile) error {
	var errs []error
	for _, file := range slices.Backward(files) {
		if err := file.rollback(); err != nil {
			errs = append(errs, fmt.Errorf("roll back %q: %w", file.rel, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) Revert(ctx context.Context, modPath, file string) (OutlineInspection, error) {
	return s.run(ctx, "revert", modPath, file, func(root string, stage *string) error {
		loaded, err := loadValidated(root, file)
		if err != nil || !loaded.hasBackup {
			return err
		}
		if err := loaded.requireEditable(); err != nil {
			return err
		}
		return loaded.write(nil, stage)
	})
}

// Adopt drops the backup of a file edited after the tool applied it, making
// the file as it is now the new original.
func (s *Service) Adopt(ctx context.Context, modPath, file string) (OutlineInspection, error) {
	return s.run(ctx, "adopt", modPath, file, func(root string, stage *string) error {
		loaded, err := loadValidated(root, file)
		if err != nil || !loaded.hasBackup {
			return err
		}
		if loaded.state != StateModified {
			return infra.ContractError(ErrorNotModified + ":" + loaded.rel)
		}
		*stage = stageRemoveBackup
		return os.Remove(loaded.backup)
	})
}

func (s *Service) run(
	ctx context.Context,
	operation, modPath, file string,
	action func(root string, stage *string) error,
) (OutlineInspection, error) {
	stage := stageResolve
	report := func(err error) error {
		if err == nil || infra.IsCancellationError(err) {
			return err
		}
		var contract infra.ContractError
		if errors.As(err, &contract) {
			return err
		}
		return infra.ReportError(s.log, err, diagnosticWhere, infra.Diagnostic{
			Operation: operationPrefix + operation,
			Stage:     stage,
			Fields:    map[string]any{"modPath": modPath, "file": file},
		})
	}

	root, err := s.requireEFMIMod(ctx, modPath)
	if err != nil {
		return OutlineInspection{}, report(err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := diskio.AcquireDir(ctx, root)
	if err != nil {
		return OutlineInspection{}, report(err)
	}
	defer release()

	stage = stageLoad
	if err := action(root, &stage); err != nil {
		return OutlineInspection{}, report(err)
	}
	stage = stageLoad
	inspection, err := inspect(root)
	return inspection, report(err)
}

func (s *Service) requireEFMIMod(ctx context.Context, modPath string) (string, error) {
	if s.games == nil {
		return "", errors.New("efmi outline is not bound to a database")
	}
	root, _, ok, err := ResolveEFMIMod(ctx, s.games, modPath)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", infra.ContractError(ErrorNotEFMI + ":" + root)
	}
	return root, nil
}

// ResolveEFMIMod returns the absolute mod folder and the mod root of the game
// holding it. ok is false for any folder that is not a mod inside an EFMI mod
// folder. Mods folders are often junctions, so roots are compared both as
// written and as resolved by Windows.
func ResolveEFMIMod(ctx context.Context, games GamePaths, modPath string) (root, modsRoot string, ok bool, err error) {
	if strings.TrimSpace(modPath) == "" {
		return "", "", false, nil
	}
	root, err = filepath.Abs(modPath)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve mod path %q: %w", modPath, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return root, "", false, fmt.Errorf("open mod folder %q: %w", root, err)
	}
	if !info.IsDir() {
		return root, "", false, nil
	}
	physical := finalOr(root)

	rows, err := games.List(ctx)
	if err != nil {
		return root, "", false, fmt.Errorf("list game paths: %w", err)
	}
	importer, longest := "", -1
	for _, row := range rows {
		roots := []string{row.ModFolderPath}
		if row.LinkedModFolderPath != nil {
			roots = append(roots, *row.LinkedModFolderPath)
		}
		for _, gameRoot := range roots {
			if strings.TrimSpace(gameRoot) == "" {
				continue
			}
			for _, pair := range [][2]string{{gameRoot, root}, {finalOr(gameRoot), physical}} {
				if !platform.SameOrChildPath(pair[0], pair[1]) || platform.SamePathFold(pair[0], pair[1]) ||
					len(pair[0]) <= longest {
					continue
				}
				importer, longest, modsRoot = "", len(pair[0]), row.ModFolderPath
				if row.Importer != nil {
					importer = *row.Importer
				}
			}
		}
	}
	if !strings.EqualFold(importer, efmiImporter) {
		return root, "", false, nil
	}
	return root, modsRoot, true, nil
}

func finalOr(path string) string {
	if resolved, err := platform.FinalPath(path); err == nil {
		return resolved
	}
	return path
}

func inspect(root string) (OutlineInspection, error) {
	inspection := OutlineInspection{ModPath: root, Files: []OutlineFile{}}
	files, err := iniFiles(root)
	if err != nil {
		return inspection, err
	}
	for _, rel := range files {
		loaded, err := loadFile(root, rel)
		if err != nil {
			return inspection, err
		}
		if file, ok := loaded.outline(); ok {
			inspection.Files = append(inspection.Files, file)
		}
	}
	return inspection, nil
}

// iniFiles lists the .ini files the loader would read, skipping DISABLED
// entries below the mod folder.
func iniFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if path == root {
			return err
		}
		if err != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if disabledNameRE.MatchString(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), ".ini") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list ini files in %q: %w", root, err)
	}
	less := platform.NewLocaleLess()
	slices.SortFunc(files, func(a, b string) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		default:
			return 0
		}
	})
	return files, nil
}

type loadedFile struct {
	rel, path, backup string
	current           []byte
	original          []byte
	hasBackup         bool
	// written records that write changed the file or its backup.
	written bool
	state   OutlineFileState
	// baseDoc is the original: the backup when one exists, else the file itself.
	baseDoc    *document
	currentDoc *document
}

func loadValidated(root, rel string) (*loadedFile, error) {
	full := filepath.Join(root, rel)
	if rel == "" || filepath.IsAbs(rel) || !platform.SameOrChildPath(root, full) || platform.SamePathFold(root, full) ||
		!strings.EqualFold(filepath.Ext(full), ".ini") {
		return nil, infra.ContractError(ErrorInvalidPath + ":" + rel)
	}
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return nil, err
	}
	return loadFile(root, rel)
}

func loadFile(root, rel string) (*loadedFile, error) {
	path := filepath.Join(root, rel)
	file := &loadedFile{rel: rel, path: path, backup: path + backupSuffix}
	current, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	file.current = current
	original, err := os.ReadFile(file.backup)
	switch {
	case err == nil:
		file.hasBackup, file.original = true, original
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("read backup %q: %w", file.backup, err)
	}

	if isUTF16(current) || (file.hasBackup && isUTF16(original)) {
		file.state = StateUnsupported
		return file, nil
	}
	file.currentDoc = parseDocument(current)
	if !file.hasBackup {
		file.state, file.baseDoc = StateOriginal, file.currentDoc
		return file, nil
	}

	// The tool leaves no marker in the file, so the applied selection is read
	// back from it; any byte that the selection does not explain is a user edit.
	file.baseDoc = parseDocument(original)
	_, currentParts := file.currentDoc.parts()
	baseParts, _ := file.baseDoc.parts()
	selected := map[string]bool{}
	for _, p := range baseParts {
		if current := currentParts[p.id]; p.candidates > 0 && current != nil && current.isApplied() {
			selected[p.id] = true
		}
	}
	expected := original
	if len(selected) > 0 {
		expected, err = applyExclusions(file.baseDoc, selected)
	}
	file.state = StateModified
	if err == nil && bytes.Equal(expected, current) {
		file.state = StateApplied
	}
	return file, nil
}

func isUTF16(data []byte) bool {
	return bytes.HasPrefix(data, []byte{0xFF, 0xFE}) || bytes.HasPrefix(data, []byte{0xFE, 0xFF})
}

func (f *loadedFile) requireEditable() error {
	switch f.state {
	case StateModified:
		return infra.ContractError(ErrorModified + ":" + f.rel)
	case StateUnsupported:
		return infra.ContractError(ErrorUnsupported + ":" + f.rel)
	default:
		return nil
	}
}

func (f *loadedFile) selection(ids []string) (map[string]bool, error) {
	_, parts := f.baseDoc.parts()
	selected := map[string]bool{}
	for _, id := range ids {
		p := parts[id]
		if p == nil {
			return nil, infra.ContractError(ErrorStale + ":" + f.rel)
		}
		if p.candidates > 0 {
			selected[id] = true
		}
	}
	return selected, nil
}

// write replaces the file with output, or restores the original when output
// is nil. The original is saved before the first edit and never overwritten.
func (f *loadedFile) write(output []byte, stage *string) error {
	if output == nil {
		if !f.hasBackup {
			return nil
		}
		*stage = stageRestore
		if err := platform.ReplaceAtomic(f.backup, f.path); err != nil {
			return err
		}
		f.written = true
		return nil
	}
	if bytes.Equal(output, f.current) {
		return nil
	}

	createdBackup := false
	if !f.hasBackup {
		*stage = stageWriteBackup
		if err := writeAtomic(f.backup, f.current); err != nil {
			return err
		}
		createdBackup = true
	}
	*stage = stageWriteFile
	if err := writeAtomic(f.path, output); err != nil {
		if createdBackup {
			err = errors.Join(err, os.Remove(f.backup))
		}
		return err
	}
	f.written = true
	return nil
}

// rollback puts the file and its backup back as they were loaded. The
// original is restored before anything that held it is removed.
func (f *loadedFile) rollback() error {
	if f.hasBackup {
		if _, err := os.Stat(f.backup); errors.Is(err, fs.ErrNotExist) {
			if err := writeAtomic(f.backup, f.original); err != nil {
				return err
			}
		}
	}
	if err := writeAtomic(f.path, f.current); err != nil {
		return err
	}
	if !f.hasBackup {
		return removeIfExists(f.backup)
	}
	return nil
}

func writeAtomic(path string, data []byte) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".nhd-efmi-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, removeIfExists(temp.Name()))
		}
	}()
	if _, err = temp.Write(data); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err = temp.Sync(); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return platform.ReplaceAtomic(temp.Name(), path)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (f *loadedFile) outline() (OutlineFile, bool) {
	file := OutlineFile{
		Path:      f.rel,
		State:     f.state,
		HasBackup: f.hasBackup,
		Parts:     []OutlinePart{},
		Manual:    []OutlineManualItem{},
	}
	if f.state == StateUnsupported {
		return file, true
	}

	_, baseParts := f.baseDoc.parts()
	currentParts, _ := f.currentDoc.parts()
	for _, p := range currentParts {
		base := baseParts[p.id]
		file.Parts = append(file.Parts, OutlinePart{
			ID:       p.id,
			Section:  p.section,
			Label:    p.label,
			Sections: p.sections,
			Lines:    p.lines,
			Applied:  p.isApplied(),
			Locked:   base != nil && base.isApplied(),
		})
	}
	for _, item := range f.currentDoc.manualItems() {
		file.Manual = append(file.Manual, OutlineManualItem{
			Section: item.section,
			Line:    item.line,
			Text:    item.text,
			Reason:  string(item.reason),
		})
	}
	return file, len(file.Parts) > 0 || len(file.Manual) > 0 || f.hasBackup
}
