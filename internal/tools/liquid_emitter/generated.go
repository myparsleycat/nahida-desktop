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
//
// The copy is moved aside, out of the game's reach, before the original is
// enabled, and its profile is released only after that. A failure up to the
// enable moves the copy back with its marker and profile untouched, so the
// restore can be retried; nothing is written to the marker on the way, and the
// original is never disabled again, which could pick another folder name.
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

		var key, aside string
		_, err = s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
			if key, _, err = s.ownedKey(sc, runtimeLayout{root: rt.root}); err != nil {
				return Inspection{}, err
			}
			aside, err = s.moveAside(sc)
			return Inspection{}, err
		})
		if err != nil {
			return Inspection{}, err
		}

		restored := marker.Source
		if marker.ReenableSource {
			// Enabling renames through the mod service, which takes its own disk slot.
			sc.stage = stageEnable
			if restored, err = s.mod.Enable(ctx, marker.Source); err != nil {
				renameErr := os.Rename(aside, sc.root)
				return Inspection{}, infra.AnnotateError(errors.Join(err, renameErr), infra.Diagnostic{
					Fields: map[string]any{"copyRestored": renameErr == nil},
				})
			}
			sc.fields["restoredSource"] = restored
		}

		// The original is back, so a failure from here leaves the copy
		// disabled under its aside name, still holding its slots.
		generatedRoot := sc.root
		sc.root = restored
		return s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
			if key != "" {
				if err := s.dropKey(sc, rt.root, key); err != nil {
					return Inspection{}, err
				}
			}
			sc.stage = stageDelete
			if err := os.RemoveAll(aside); err != nil {
				_ = infra.ReportError(s.log, err, diagnosticWhere, infra.Diagnostic{
					Severity:  infra.DiagnosticWarn,
					Operation: operationPrefix + "restore-source",
					Stage:     sc.stage,
					Fields:    map[string]any{"modPath": generatedRoot, "aside": aside},
				})
			}
			return s.inspection(sc, rt)
		})
	})
}

// moveAside renames the mod at sc.root to a disabled name next to it. A file
// locked in the folder fails the rename before anything else changes.
func (s *Service) moveAside(sc *scope) (string, error) {
	sc.stage = stageDelete
	parent := filepath.Dir(sc.root)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	aside := filepath.Join(parent, s.fs.GetUniqueName("DISABLED_"+filepath.Base(sc.root)+".nhd-restore", names))
	sc.fields["aside"] = aside
	return aside, os.Rename(sc.root, aside)
}

// applyAsNewMod copies the original next to itself, applies the emitter to
// the copy and then disables the original. Disabling schedules compression
// of the original's files, so it waits until the copy is complete. A failure
// deletes the copy and leaves the original enabled.
func (s *Service) applyAsNewMod(
	ctx context.Context,
	sc *scope,
	source modelviewer.DrawSource,
	triangle anchorTriangle,
	anchor EmitterAnchor,
	emitterPID *int,
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

	origin := &modMarker{Source: sourceRoot, ReenableSource: !disabledPrefixRE.MatchString(filepath.Base(sourceRoot))}
	created := false
	inspection, err := s.withRuntime(ctx, sc, true, func(rt runtimeState) (Inspection, error) {
		sc.stage = stageCopy
		if err := os.Mkdir(target, 0o755); err != nil {
			return Inspection{}, err
		}
		created = true
		if err := copyModTree(ctx, sourceRoot, target); err != nil {
			return Inspection{}, err
		}
		rel, err := filepath.Rel(sourceRoot, source.INIPath)
		if err != nil || !platform.SameOrChildPath(sourceRoot, source.INIPath) {
			return Inspection{}, fmt.Errorf("draw INI %q is outside %q", source.INIPath, sourceRoot)
		}
		source.INIPath = filepath.Join(target, rel)
		sc.root, sc.origin = target, origin
		if err := s.apply(sc, rt.root, source, triangle, anchor, emitterPID); err != nil {
			return Inspection{}, err
		}
		return s.inspection(sc, rt)
	})
	switch {
	case err != nil && !created:
		return Inspection{}, err
	case err != nil:
		return Inspection{}, s.undoNewMod(ctx, sc, err, "")
	case !origin.ReenableSource:
		return inspection, nil
	}

	sc.stage = stageDisable
	disabled, err := s.mod.Disable(ctx, sourceRoot)
	if err != nil {
		return Inspection{}, s.undoNewMod(ctx, sc, err, "")
	}
	sc.fields["disabledSource"] = disabled

	// The marker written with the emitter names the original by the path it
	// had until now.
	origin.Source = disabled
	if err := s.rewriteMarker(sc, origin); err != nil {
		return Inspection{}, s.undoNewMod(ctx, sc, err, disabled)
	}
	inspection.SourcePath = disabled
	return inspection, nil
}

func (s *Service) rewriteMarker(sc *scope, origin *modMarker) error {
	marker, err := readMarker(sc.root)
	if err != nil {
		return err
	}
	changes, err := markerChange(sc.root, marker.ModKey, origin)
	if err != nil {
		return err
	}
	return s.commit(sc, changes)
}

// undoNewMod deletes the copy of a new-mod apply that failed with cause and,
// when disabled is set, enables the original again.
func (s *Service) undoNewMod(ctx context.Context, sc *scope, cause error, disabled string) error {
	ctx = context.WithoutCancel(ctx)
	target, _ := sc.fields["target"].(string)
	state := map[string]any{"copyRemoved": false, "sourceEnabled": disabled == ""}
	var undoErrs []error
	// The copy's profile and its split from the original are already
	// committed, unless the apply itself failed part way.
	if err := s.dropCopyProfile(ctx, sc, target); err != nil {
		undoErrs = append(undoErrs, err)
	}
	if err := os.RemoveAll(target); err != nil {
		undoErrs = append(undoErrs, err)
	} else {
		state["copyRemoved"] = true
	}
	if disabled != "" {
		if _, err := s.mod.Enable(ctx, disabled); err != nil {
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
	copies, err := markersElsewhere(sc.modsRoot, sc.root, marker.ModKey)
	return len(copies) == 0, err
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
