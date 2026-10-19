package modruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

var (
	disabledPrefixRE = regexp.MustCompile(`(?i)^disabled[\s_-]*`)
	// includeRE matches a quoted HLSL include, which resolves beside the file
	// that has it.
	includeRE = regexp.MustCompile(`(?m)^[[:blank:]]*#[[:blank:]]*include[[:blank:]]*"([^"]+)"`)
)

type manifest struct {
	Name           string            `json:"name,omitempty"`
	RuntimeVersion string            `json:"runtimeVersion"`
	Variant        string            `json:"variant"`
	Files          map[string]string `json:"files"`
	// PendingShaders are the shaders, by slash path, whose compiled caches an
	// unfinished install has yet to remove. A retry may find their changed
	// sources already written and could not tell otherwise.
	PendingShaders []string `json:"pendingShaders,omitempty"`
}

func installRoot(modsRoot, name string) string {
	return filepath.Join(modsRoot, GroupDir, name)
}

// inspect reports the runtime under modsRoot. A second copy declaring the
// same namespace wins over every other state, because the importer would
// load both.
func (m *Manager) inspect(modsRoot string, sel selection) (Status, error) {
	spec := sel.runtimeSpec
	root := installRoot(modsRoot, spec.Name)
	status, err := inspectAt(root, sel)
	if err != nil {
		return status, err
	}
	if status.State == StateMissing {
		if disabled := findDisabled(filepath.Join(modsRoot, GroupDir), spec.Name, manifestFile); disabled != "" {
			status.State, status.Path = StateDisabled, disabled
			if found, err := readManifest(filepath.Join(disabled, manifestFile)); err == nil && found != nil {
				status.Version = found.RuntimeVersion
			}
		}
	}
	if status.State != StateForeign {
		foreign, err := findForeign(modsRoot, root, spec)
		if err != nil {
			return status, err
		}
		if foreign != "" {
			status = Status{Name: spec.Name, State: StateForeign, Path: foreign, BundledVersion: spec.Version}
		}
	}

	dataRoot := root
	if status.State == StateDisabled {
		dataRoot = status.Path
	}
	values, err := settingValues(dataRoot, spec)
	if err != nil {
		return status, err
	}
	status.Settings = settingStatus(spec, values)
	return status, nil
}

func inspectAt(root string, sel selection) (Status, error) {
	spec := sel.runtimeSpec
	status := Status{Name: spec.Name, State: StateMissing, Path: root, BundledVersion: spec.Version}
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return status, nil
	} else if err != nil {
		return status, err
	}
	found, err := readManifest(filepath.Join(root, manifestFile))
	if err != nil {
		return status, err
	}
	if found == nil {
		// Data a feature kept after its runtime files were deleted is
		// installed over; runtime files without a manifest are someone else's.
		if declaresBelow(root, spec) {
			status.State = StateForeign
		}
		return status, nil
	}
	status.Version = found.RuntimeVersion
	if found.RuntimeVersion != spec.Version || found.Variant != sel.variant {
		status.State = StateOutdated
		return status, nil
	}
	status.State = StateCurrent
	if len(found.PendingShaders) > 0 {
		status.State = StateModified
	}
	for name, hash := range found.Files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || digest(data) != hash {
			status.State = StateModified
			break
		}
	}
	for name := range spec.Seed {
		if status.State != StateCurrent {
			break
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); errors.Is(err, fs.ErrNotExist) {
			status.State = StateModified
		}
	}
	return status, nil
}

func readManifest(name string) (*manifest, error) {
	data, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found manifest
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &found); err != nil {
		return nil, fmt.Errorf("parse %q: %w", name, err)
	}
	return &found, nil
}

// findDisabled returns the disabled spelling of base inside parent that
// carries the manifest file.
func findDisabled(parent, base, manifestName string) string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !disabledPrefixRE.MatchString(name) ||
			!strings.EqualFold(disabledPrefixRE.ReplaceAllString(name, ""), base) {
			continue
		}
		candidate := filepath.Join(parent, name)
		if _, err := os.Stat(filepath.Join(candidate, manifestName)); err == nil {
			return candidate
		}
	}
	return ""
}

// findForeign looks for an enabled INI declaring the runtime's namespace
// outside its install folder. The importer skips DISABLED folders and files.
func findForeign(modsRoot, ownRoot string, spec runtimeSpec) (string, error) {
	const maxDepth = 4
	modsRoot, ownRoot = physicalPath(modsRoot), physicalPath(ownRoot)
	found := ""
	err := filepath.WalkDir(modsRoot, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if name == modsRoot {
				return err
			}
			return nil
		}
		base := entry.Name()
		if entry.IsDir() {
			if name == modsRoot {
				return nil
			}
			depth := strings.Count(strings.TrimPrefix(name, modsRoot), string(filepath.Separator))
			if disabledPrefixRE.MatchString(base) || strings.HasPrefix(base, ".") || depth > maxDepth ||
				platform.SamePathFold(name, ownRoot) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(base), ".ini") || disabledPrefixRE.MatchString(base) {
			return nil
		}
		if declares(name, spec) {
			found = filepath.Dir(name)
			return filepath.SkipAll
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return found, err
}

func declaresBelow(root string, spec runtimeSpec) bool {
	found := false
	_ = filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(name), ".ini") && declares(name, spec) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func declares(name string, spec runtimeSpec) bool {
	file, err := os.Open(name)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	head := make([]byte, 4096)
	count, _ := io.ReadFull(file, head)
	return spec.namespace.Match(bytes.TrimPrefix(head[:count], []byte{0xef, 0xbb, 0xbf}))
}

// physicalPath resolves junctions and links. The Mods folder is often a
// junction, which filepath.WalkDir does not descend into.
func physicalPath(name string) string {
	if resolved, err := platform.FinalPath(name); err == nil {
		return resolved
	}
	return filepath.Clean(name)
}

// installAt writes into root the runtime files, with the stored settings
// applied, that differ from what is there, and the seed files that are
// missing, then removes the files of the last install the variant no longer
// has and the compiled shader caches those changes left stale. The manifest
// goes last so an interrupted update still reads as outdated. Before any
// runtime file lands, the manifest records the shader caches to remove, so a
// retry removes them too; a first install also claims the folder with it,
// without which those files would read as a foreign copy.
func (m *Manager) installAt(root, modsRoot string, sel selection) error {
	spec := sel.runtimeSpec
	values, err := settingValues(root, spec)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(root, manifestFile)
	previous, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	writes, stale, shaders, err := installPlan(root, modsRoot, sel, values, previous)
	if err != nil {
		return err
	}
	if previous == nil || !slices.Equal(previous.PendingShaders, shaders) {
		progress := manifest{Name: spec.Name}
		if previous != nil {
			progress = *previous
		}
		progress.PendingShaders = shaders
		pending, err := encodeJSON(progress)
		if err == nil {
			err = m.writeFile(manifestPath, pending)
		}
		if err != nil {
			return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": manifestPath}})
		}
	}
	names := make([]string, 0, len(writes))
	for name := range writes {
		if name != manifestPath {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if err := m.writeFile(name, writes[name]); err != nil {
			return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": name}})
		}
	}
	for _, name := range stale {
		if err := removeStale(root, name); err != nil {
			return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": name}})
		}
	}
	for _, name := range shaders {
		if err := removeShaderCache(root, name); err != nil {
			return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"shader": name}})
		}
	}
	if err := m.writeFile(manifestPath, writes[manifestPath]); err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": manifestPath}})
	}
	return nil
}

// installPlan returns the files to write, by path, the paths of the files
// the previous install wrote that the variant does not have, and the slash
// paths of the shaders whose compiled caches the changes, or the previous
// unfinished install, leave stale.
func installPlan(
	root, modsRoot string,
	sel selection,
	values map[string]string,
	previous *manifest,
) (writes map[string][]byte, stale, shaders []string, err error) {
	spec := sel.runtimeSpec
	next := manifest{Name: spec.Name, RuntimeVersion: spec.Version, Variant: sel.variant, Files: map[string]string{}}
	writes = make(map[string][]byte)
	files, err := variantFiles(sel)
	if err != nil {
		return nil, nil, nil, err
	}
	changed := make(map[string]bool)
	for name, data := range files {
		if data, err = renderSettings(name, data, spec, values); err != nil {
			return nil, nil, nil, err
		}
		next.Files[name] = digest(data)
		target := filepath.Join(root, filepath.FromSlash(name))
		if current, err := os.ReadFile(target); err == nil && bytes.Equal(current, data) {
			continue
		}
		writes[target] = data
		changed[strings.ToLower(name)] = true
	}
	encoded, err := encodeJSON(next)
	if err != nil {
		return nil, nil, nil, err
	}
	writes[filepath.Join(root, manifestFile)] = encoded
	if previous != nil {
		for _, name := range previous.PendingShaders {
			changed[strings.ToLower(name)] = true
		}
		for name := range previous.Files {
			if _, kept := next.Files[name]; kept {
				continue
			}
			if target, ok := installedPath(root, name); ok {
				stale = append(stale, target)
				changed[strings.ToLower(name)] = true
			}
		}
		slices.Sort(stale)
	}
	shaders = staleShaders(files, changed)

	for name, seed := range spec.Seed {
		target := filepath.Join(root, filepath.FromSlash(name))
		if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		data, err := seed(root, modsRoot)
		if err != nil {
			return nil, nil, nil, err
		}
		writes[target] = data
	}
	return writes, stale, shaders, nil
}

// staleShaders returns the HLSL files among files and changed, by slash path,
// that changed or include one that did at any depth. 3DMigoto keeps a compiled
// custom shader in a .bin beside its source and uses it while the source is
// not newer, so a change to an included file alone leaves the cache stale.
// changed holds lowercase slash paths.
func staleShaders(files map[string][]byte, changed map[string]bool) []string {
	isShader := func(name string) bool { return strings.EqualFold(path.Ext(name), ".hlsl") }
	affected := maps.Clone(changed)
	for grew := true; grew; {
		grew = false
		for name, data := range files {
			key := strings.ToLower(name)
			if affected[key] || !isShader(name) {
				continue
			}
			for _, match := range includeRE.FindAllSubmatch(data, -1) {
				included := path.Join(path.Dir(name), strings.ReplaceAll(string(match[1]), `\`, "/"))
				if affected[strings.ToLower(included)] {
					affected[key], grew = true, true
					break
				}
			}
		}
	}
	var shaders []string
	for name := range affected {
		if isShader(name) {
			shaders = append(shaders, name)
		}
	}
	slices.Sort(shaders)
	return shaders
}

// removeShaderCache deletes the caches 3DMigoto compiled from the shader at
// slash path name below root, such as name.ps_5_0.8000.bin. Like removeStale,
// it deletes only what physically lies below root, so a junction inside the
// folder cannot lead it to another folder's files.
func removeShaderCache(root, name string) error {
	source, ok := installedPath(root, name)
	if !ok {
		return nil
	}
	physicalRoot := physicalPath(root)
	dir, prefix := filepath.Dir(source), strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))+"."
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		base := entry.Name()
		if !entry.Type().IsRegular() || len(base) <= len(prefix) ||
			!strings.EqualFold(base[:len(prefix)], prefix) || !strings.EqualFold(filepath.Ext(base), ".bin") {
			continue
		}
		cache := filepath.Join(dir, base)
		physical, err := platform.FinalPath(cache)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if platform.SamePathFold(physicalRoot, physical) || !platform.SameOrChildPath(physicalRoot, physical) {
			continue
		}
		if err := os.Remove(cache); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// variantFiles reads the files the variant installs, by slash path: the base
// files outside every variant directory, then the selected variant's own
// over them.
func variantFiles(sel selection) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := fs.WalkDir(sel.Files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != "." && slices.ContainsFunc(sel.Variants, func(v Variant) bool { return v.Dir == name }) {
				return fs.SkipDir
			}
			return nil
		}
		data, err := fs.ReadFile(sel.Files, name)
		if err != nil {
			return err
		}
		files[name] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(sel.Variants, func(v Variant) bool { return v.Name == sel.variant })
	if index < 0 {
		return files, nil
	}
	dir := sel.Variants[index].Dir
	err = fs.WalkDir(sel.Files, dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if name == dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(sel.Files, name)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(name, dir+"/")] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// removeStale deletes a file of an earlier install and the directories it
// leaves empty below root.
func removeStale(root, name string) error {
	if _, err := os.Lstat(name); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	// A manifest someone edited, or a junction placed inside the folder,
	// must not lead the removal outside the install: delete only what
	// physically lies below the root.
	physical, err := platform.FinalPath(name)
	if err != nil {
		return err
	}
	physicalRoot := physicalPath(root)
	if platform.SamePathFold(physicalRoot, physical) || !platform.SameOrChildPath(physicalRoot, physical) {
		return nil
	}
	if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	removeEmptyParents(root, filepath.Dir(name))
	return nil
}

// installedPath resolves a manifest entry to its path below root, refusing
// one that names a parent, a drive or an absolute path. fs.ValidPath knows no
// backslash, which filepath.Join would still follow out of the root.
func installedPath(root, name string) (string, bool) {
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, `\:`) {
		return "", false
	}
	target := filepath.Join(root, filepath.FromSlash(name))
	if platform.SamePathFold(root, target) || !platform.SameOrChildPath(root, target) {
		return "", false
	}
	return target, true
}

// removeEmptyParents removes dir and its parents below root until one still
// holds something, such as the data a feature keeps beside its runtime.
func removeEmptyParents(root, dir string) {
	for ; !platform.SamePathFold(dir, root) && platform.SameOrChildPath(root, dir); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			return
		}
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func encodeJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writeAtomic(name string, data []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(name), ".nhd-runtime-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if removeErr := os.Remove(temp.Name()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
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
	if err = platform.ReplaceAtomic(temp.Name(), name); err != nil {
		return fmt.Errorf("replace %q: %w", name, err)
	}
	return nil
}
