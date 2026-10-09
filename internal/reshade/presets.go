package reshade

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	maxPreset     = 8 << 20
	defaultPreset = "ReShadePreset.ini"
)

// PresetEffects is what a preset turns on that the shared effect folders lack.
type PresetEffects struct {
	// Packages are the listed packages that provide the missing effects.
	Packages []EffectPackage `json:"packages"`
	// Unknown names the missing effect files no installable package provides.
	Unknown []string `json:"unknown"`
}

// AddPresets copies preset files into the shared preset folder, keeping a preset that already has
// the name, and returns the effects the added presets need and lack.
func (r *ReShade) AddPresets(ctx context.Context, sources []string) (PresetEffects, error) {
	layout, err := r.layout(ctx)
	if err != nil {
		return PresetEffects{}, r.report(err, "AddPresets", "resolve-root", nil)
	}
	if err := layout.ensureShared(); err != nil {
		return PresetEffects{}, r.report(err, "AddPresets", "create-folders", map[string]any{"root": layout.root})
	}

	added := make([]string, 0, len(sources))
	for _, source := range sources {
		target, err := addPreset(layout.presets(), source)
		if err != nil {
			if errors.Is(err, errPresetUnsupported) {
				return PresetEffects{}, err
			}
			return PresetEffects{}, r.report(err, "AddPresets", "copy", map[string]any{
				"source": source, "destination": layout.presets(), "added": len(added),
			})
		}
		added = append(added, target)
	}

	effects, err := r.presetEffects(ctx, layout, added)
	if err != nil {
		return PresetEffects{}, r.report(err, "AddPresets", "check-effects", map[string]any{"presets": added})
	}
	return effects, nil
}

// LaunchPresetEffects returns the effects the importer's current preset needs and lacks.
//
//wails:ignore
func (r *ReShade) LaunchPresetEffects(ctx context.Context, importer string) (PresetEffects, error) {
	effects, err := r.launchPresetEffects(ctx, importer)
	if err != nil {
		return PresetEffects{}, infra.AnnotateError(err, infra.Diagnostic{
			Operation: "reshade", Stage: "check-preset-effects", Fields: map[string]any{"importer": importer},
		})
	}
	return effects, nil
}

func (r *ReShade) launchPresetEffects(ctx context.Context, importer string) (PresetEffects, error) {
	layout, err := r.layout(ctx)
	if err != nil {
		return PresetEffects{}, err
	}
	game, err := layout.game(importer)
	if err != nil {
		return PresetEffects{}, err
	}

	preset := defaultPreset
	if config, err := os.ReadFile(filepath.Join(game, "ReShade.ini")); err == nil {
		if configured := iniValue(config, "GENERAL", "PresetPath"); configured != "" {
			preset = configured
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return PresetEffects{}, err
	}
	// ReShade resolves a relative preset path against its module's folder.
	if !filepath.IsAbs(preset) {
		preset = filepath.Join(game, preset)
	}
	return r.presetEffects(ctx, layout, []string{preset})
}

func (r *ReShade) presetEffects(ctx context.Context, layout layout, presets []string) (PresetEffects, error) {
	effects := PresetEffects{Packages: []EffectPackage{}, Unknown: []string{}}
	var wanted []string
	for _, preset := range presets {
		data, err := readPreset(preset)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return effects, err
		}
		wanted = append(wanted, presetEffectFiles(data)...)
	}
	if len(wanted) == 0 {
		return effects, nil
	}

	present, err := shaderFiles(ctx, layout)
	if err != nil {
		return effects, err
	}
	var missing []string
	for _, file := range wanted {
		key := strings.ToLower(file)
		if !present[key] {
			present[key] = true
			missing = append(missing, file)
		}
	}
	// The package list is fetched only for a preset that lacks something, so most checks stay offline.
	if len(missing) == 0 {
		return effects, nil
	}

	packages, _, err := r.effectPackages(ctx)
	if err != nil {
		return effects, err
	}
	effects.Packages, effects.Unknown = packagesForEffects(packages, missing)
	return effects, nil
}

// packagesForEffects picks the packages that provide files. A file several packages provide goes to
// one already picked for another file, or else to the first that lists it.
func packagesForEffects(packages []EffectPackage, files []string) ([]EffectPackage, []string) {
	candidates := make([][]int, len(files))
	picked := map[int]bool{}
	for i, file := range files {
		for index, pkg := range packages {
			if pkg.Supported && slices.ContainsFunc(pkg.effectFiles, func(name string) bool {
				return strings.EqualFold(name, file)
			}) {
				candidates[i] = append(candidates[i], index)
			}
		}
		if len(candidates[i]) == 1 {
			picked[candidates[i][0]] = true
		}
	}

	unknown := []string{}
	for i, file := range files {
		switch {
		case len(candidates[i]) == 0:
			unknown = append(unknown, file)
		case !slices.ContainsFunc(candidates[i], func(index int) bool { return picked[index] }):
			picked[candidates[i][0]] = true
		}
	}
	needed := []EffectPackage{}
	for index, pkg := range packages {
		if picked[index] {
			needed = append(needed, pkg)
		}
	}
	return needed, unknown
}

var errPresetUnsupported = errors.New("RESHADE_PRESET_UNSUPPORTED")

func addPreset(directory, source string) (string, error) {
	extension := filepath.Ext(source)
	if !strings.EqualFold(extension, ".ini") && !strings.EqualFold(extension, ".txt") {
		return "", errPresetUnsupported
	}
	data, err := readPreset(source)
	if err != nil {
		return "", err
	}
	files := platform.NewFS()
	stem := files.SanitizeWindowsFilename(strings.TrimSuffix(filepath.Base(source), extension), "_")
	if stem == "" || platform.IsUnaddressableName(stem+extension) {
		return "", errPresetUnsupported
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	var taken []string
	for _, entry := range entries {
		if name := entry.Name(); strings.EqualFold(filepath.Ext(name), extension) {
			taken = append(taken, strings.TrimSuffix(name, filepath.Ext(name)))
		}
	}
	target := filepath.Join(directory, files.GetUniqueName(stem, taken)+extension)

	temporary, err := os.CreateTemp(directory, ".preset-")
	if err != nil {
		return "", err
	}
	_, writeErr := temporary.Write(data)
	if err := errors.Join(writeErr, temporary.Close()); err != nil {
		_ = os.Remove(temporary.Name())
		return "", err
	}
	if err := platform.ReplaceAtomic(temporary.Name(), target); err != nil {
		_ = os.Remove(temporary.Name())
		return "", err
	}
	return target, nil
}

func readPreset(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPreset {
		return nil, errPresetUnsupported
	}
	return os.ReadFile(path)
}

// presetEffectFiles returns the effect files of the techniques a preset turns on, such as
// `Bloom.fx` from `Techniques=Bloom@Bloom.fx`. Techniques listed without a file are skipped.
func presetEffectFiles(preset []byte) []string {
	var files []string
	for _, technique := range strings.Split(iniValue(preset, "", "Techniques"), ",") {
		_, file, ok := strings.Cut(strings.TrimSpace(technique), "@")
		if file = strings.TrimSpace(file); !ok || file == "" || file != filepath.Base(file) {
			continue
		}
		if !slices.ContainsFunc(files, func(known string) bool { return strings.EqualFold(known, file) }) {
			files = append(files, file)
		}
	}
	return files
}

// iniValue returns the last value of key in section; the empty section is the part before any header.
func iniValue(data []byte, section, key string) string {
	value, current := "", ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64<<10), maxPreset)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = line[1 : len(line)-1]
			continue
		}
		if !strings.EqualFold(current, section) {
			continue
		}
		if name, found, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(name), key) {
			value = strings.TrimSpace(found)
		}
	}
	return value
}

// shaderFiles returns the lower-cased names of the effect files below the shared shader folder,
// which ReShade searches recursively.
func shaderFiles(ctx context.Context, layout layout) (map[string]bool, error) {
	present := map[string]bool{}
	if _, err := os.Stat(layout.shaders()); errors.Is(err, os.ErrNotExist) {
		return present, nil
	}
	release, err := diskio.AcquireDir(ctx, layout.shaders())
	if err != nil {
		return nil, err
	}
	defer release()

	err = filepath.WalkDir(layout.shaders(), func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".fx") {
			present[strings.ToLower(entry.Name())] = true
		}
		return nil
	})
	return present, err
}
