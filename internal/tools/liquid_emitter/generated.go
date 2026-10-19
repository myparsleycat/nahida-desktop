package liquidemitter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/platform"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const (
	generatedSuffix = " (Liquid)"

	stageDisable = "disable-source"
	stageCopy    = "copy-mod"
	stageDelete  = "delete-copy"
	stageEnable  = "enable-source"
)

// RestoreSource deletes a mod made by a new-mod apply, together with its
// emitter profiles, and enables the original again.
func (s *Service) RestoreSource(ctx context.Context, modPath string) (Inspection, error) {
	return s.run(ctx, "restore-source", modPath, func(sc *scope) (Inspection, error) {
		if s.mod == nil {
			return Inspection{}, errors.New("mod service is unavailable")
		}
		marker, err := readMarker(sc.root)
		if err != nil {
			return Inspection{}, err
		}
		generated, err := generatedHere(sc, marker)
		if err != nil {
			return Inspection{}, err
		}
		if !generated {
			return Inspection{}, infra.ContractError(ErrorNotGenerated + ":" + sc.root)
		}
		sc.fields["source"] = marker.Source
		if info, err := os.Stat(
			marker.Source,
		); err != nil || !info.IsDir() ||
			samePhysicalPath(marker.Source, sc.root) {
			return Inspection{}, infra.ContractError(ErrorSourceMissing + ":" + marker.Source)
		}

		_, err = s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
			if err := s.dropKey(sc, rt.root, marker.ModKey); err != nil {
				return Inspection{}, err
			}
			sc.stage = stageDelete
			return Inspection{}, os.RemoveAll(sc.root)
		})
		if err != nil {
			return Inspection{}, err
		}

		restored := marker.Source
		if marker.ReenableSource {
			// Enabling renames through the mod service, which takes its own disk slot.
			sc.stage = stageEnable
			if restored, err = s.mod.Enable(ctx, marker.Source); err != nil {
				return Inspection{}, err
			}
		}
		sc.root = restored
		return s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
			return s.inspection(sc, rt)
		})
	})
}

// applyAsNewMod disables the original, copies it next to itself and applies
// the emitter to the copy. A failure deletes the copy and enables the
// original again.
func (s *Service) applyAsNewMod(
	ctx context.Context,
	sc *scope,
	source modelviewer.DrawSource,
	triangle anchorTriangle,
	anchor EmitterAnchor,
) (Inspection, error) {
	if s.mod == nil {
		return Inspection{}, errors.New("mod service is unavailable")
	}
	marker, err := readMarker(sc.root)
	if err != nil {
		return Inspection{}, err
	}
	if generated, err := generatedHere(sc, marker); err != nil {
		return Inspection{}, err
	} else if generated {
		return Inspection{}, infra.ContractError(ErrorGeneratedMod + ":" + sc.root)
	}
	// A foreign runtime would fail the apply only after the original was disabled.
	_, err = s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
		if rt.status.State == modruntime.StateForeign {
			return Inspection{}, infra.ContractError(ErrorRuntimeForeign + ":" + rt.status.Path)
		}
		return Inspection{}, nil
	})
	if err != nil {
		return Inspection{}, err
	}

	sourceRoot := sc.root
	parent := filepath.Dir(sourceRoot)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return Inspection{}, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	base := strings.TrimSpace(disabledPrefixRE.ReplaceAllString(filepath.Base(sourceRoot), ""))
	target := filepath.Join(parent, s.fs.GetUniqueName(base+generatedSuffix, names))
	sc.fields["target"] = target

	origin := &modMarker{Source: sourceRoot}
	if !disabledPrefixRE.MatchString(filepath.Base(sourceRoot)) {
		sc.stage = stageDisable
		disabled, err := s.mod.Disable(ctx, sourceRoot)
		if err != nil {
			return Inspection{}, err
		}
		origin.Source, origin.ReenableSource = disabled, true
	}
	sc.fields["disabledSource"] = origin.Source

	created := false
	inspection, err := s.withRuntime(ctx, sc, true, func(rt runtimeState) (Inspection, error) {
		sc.stage = stageCopy
		if err := os.Mkdir(target, 0o755); err != nil {
			return Inspection{}, err
		}
		created = true
		if err := copyModTree(ctx, origin.Source, target); err != nil {
			return Inspection{}, err
		}
		rel, err := filepath.Rel(sourceRoot, source.INIPath)
		if err != nil || !platform.SameOrChildPath(sourceRoot, source.INIPath) {
			return Inspection{}, fmt.Errorf("draw INI %q is outside %q", source.INIPath, sourceRoot)
		}
		source.INIPath = filepath.Join(target, rel)
		sc.root, sc.origin = target, origin
		if err := s.apply(sc, rt.root, source, triangle, anchor); err != nil {
			return Inspection{}, err
		}
		return s.inspection(sc, rt)
	})
	if err != nil {
		return Inspection{}, s.undoNewMod(ctx, sc, err, created, origin)
	}
	return inspection, nil
}

func (s *Service) undoNewMod(ctx context.Context, sc *scope, cause error, created bool, origin *modMarker) error {
	ctx = context.WithoutCancel(ctx)
	target, _ := sc.fields["target"].(string)
	state := map[string]any{"copyRemoved": false, "sourceEnabled": !origin.ReenableSource}
	var undoErrs []error
	if created {
		// Splitting the copy from the original commits on its own, before
		// the apply that failed.
		if err := s.dropCopyProfile(ctx, sc, target); err != nil {
			undoErrs = append(undoErrs, err)
		}
		if err := os.RemoveAll(target); err != nil {
			undoErrs = append(undoErrs, err)
		} else {
			state["copyRemoved"] = true
		}
	}
	if origin.ReenableSource {
		if _, err := s.mod.Enable(ctx, origin.Source); err != nil {
			undoErrs = append(undoErrs, err)
		} else {
			state["sourceEnabled"] = true
		}
	}
	return infra.AnnotateError(errors.Join(append([]error{cause}, undoErrs...)...), infra.Diagnostic{Fields: state})
}

func (s *Service) dropCopyProfile(ctx context.Context, sc *scope, target string) error {
	marker, err := readMarker(target)
	if err != nil || marker.ModKey == "" || marker.ModPath == "" || !samePhysicalPath(marker.ModPath, target) {
		return err
	}
	_, err = s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
		return Inspection{}, s.dropKey(sc, rt.root, marker.ModKey)
	})
	return err
}

// dropKey releases every slot and the profile file of one mod key.
func (s *Service) dropKey(sc *scope, runtimeRoot, key string) error {
	sc.stage = stageLoad
	layout := runtimeLayout{root: runtimeRoot}
	index, err := readIndex(layout, sc.modsRoot)
	if err != nil {
		return err
	}
	delete(index.Owners, key)
	changes, err := s.profileChanges(layout, index, key, profileDocument{})
	if err != nil {
		return err
	}
	return s.commit(sc, changes)
}

// generatedHere reports whether marker records the mod at sc.root as a copy
// made by a new-mod apply. A marker copied along with that folder names
// another folder that still carries the same key.
func generatedHere(sc *scope, marker modMarker) (bool, error) {
	if marker.Source == "" {
		return false, nil
	}
	if marker.ModPath != "" && samePhysicalPath(marker.ModPath, sc.root) {
		return true, nil
	}
	shared, err := markerHeldElsewhere(sc.modsRoot, sc.root, marker.ModKey)
	return !shared, err
}

// copyModTree copies a mod folder into the existing empty target.
func copyModTree(ctx context.Context, source, target string) error {
	return filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil || rel == "." {
			return err
		}
		destination := filepath.Join(target, rel)
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not supported in copied mods: %s", name)
		}
		if entry.IsDir() {
			return os.Mkdir(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported mod entry: %s", name)
		}
		return copyModFile(name, destination, info.Mode().Perm())
	})
}

func copyModFile(source, target string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}
