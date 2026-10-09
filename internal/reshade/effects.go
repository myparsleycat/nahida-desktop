package reshade

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	// The official setup reads the same list to offer its effect packages.
	effectListURL  = "https://raw.githubusercontent.com/crosire/reshade-shaders/list/EffectPackages.ini"
	maxEffectList  = 1 << 20
	maxEffectPack  = 1 << 30
	effectsDirName = "reshade-shaders"
)

var (
	effectListRepo = github.Repo{Owner: "crosire", Name: "reshade-shaders"}
	effectPackRE   = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/archive/[^?#]+\.zip$`)
)

type EffectPackage struct {
	// ID is the package's download URL, the one value the list keeps unique.
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Repository  string `json:"repository"`
	// Recommended marks the packages the official setup selects by default.
	Recommended bool `json:"recommended"`
	Installed   bool `json:"installed"`
	// Supported is false for a package this app cannot download or place safely.
	Supported bool `json:"supported"`

	installPath        string
	textureInstallPath string
	denied             []string
}

type effectRecord struct {
	Name        string   `json:"name"`
	InstalledAt string   `json:"installedAt"`
	Files       []string `json:"files"`
}

func (r *ReShade) ListEffectPackages(ctx context.Context) ([]EffectPackage, error) {
	packages, layout, err := r.effectPackages(ctx)
	if err != nil {
		return nil, r.report(err, "ListEffectPackages", "list", nil)
	}
	records, err := readEffectRecords(layout.record())
	if err != nil {
		return nil, r.report(err, "ListEffectPackages", "read-record", map[string]any{"path": layout.record()})
	}
	for i := range packages {
		_, packages[i].Installed = records[packages[i].ID]
	}
	return packages, nil
}

// InstallEffectPackages downloads the listed packages into the shared effect folders, replacing
// their earlier files. It returns the IDs installed before any failure.
func (r *ReShade) InstallEffectPackages(ctx context.Context, ids []string) ([]string, error) {
	packages, layout, err := r.effectPackages(ctx)
	if err != nil {
		return nil, r.report(err, "InstallEffectPackages", "list", nil)
	}
	if err := layout.ensureShared(); err != nil {
		return nil, r.report(err, "InstallEffectPackages", "create-folders", map[string]any{"root": layout.root})
	}

	installed := make([]string, 0, len(ids))
	defer r.emitStatus()
	for _, id := range ids {
		index := slices.IndexFunc(packages, func(pkg EffectPackage) bool { return pkg.ID == id })
		if index < 0 || !packages[index].Supported {
			return installed, errors.New("RESHADE_EFFECT_PACKAGE_UNSUPPORTED")
		}
		pkg := packages[index]
		if err := r.installEffectPackage(ctx, layout, pkg); err != nil {
			return installed, r.report(err, "InstallEffectPackages", "install", map[string]any{
				"package": pkg.Name, "url": infra.SanitizeLogURL(pkg.ID), "root": layout.effects(),
			})
		}
		installed = append(installed, id)
	}
	return installed, nil
}

func (r *ReShade) RemoveEffectPackage(ctx context.Context, id string) error {
	layout, err := r.layout(ctx)
	if err != nil {
		return r.report(err, "RemoveEffectPackage", "resolve-root", nil)
	}
	r.effectsMu.Lock()
	defer r.effectsMu.Unlock()

	records, err := readEffectRecords(layout.record())
	if err != nil {
		return r.report(err, "RemoveEffectPackage", "read-record", map[string]any{"path": layout.record()})
	}
	record, ok := records[id]
	if !ok {
		return nil
	}
	delete(records, id)
	if err := removeEffectFiles(ctx, layout, record.Files, records); err != nil {
		return r.report(err, "RemoveEffectPackage", "remove-files", map[string]any{"package": record.Name})
	}
	if err := writeEffectRecords(layout.record(), records); err != nil {
		return r.report(err, "RemoveEffectPackage", "write-record", map[string]any{"path": layout.record()})
	}
	r.emitStatus()
	return nil
}

func (r *ReShade) effectPackages(ctx context.Context) ([]EffectPackage, layout, error) {
	layout, err := r.layout(ctx)
	if err != nil {
		return nil, layout, err
	}
	if r.github == nil {
		return nil, layout, errors.New("GitHub client is not configured")
	}
	data, err := r.github.FetchFile(ctx, effectListRepo, effectListURL, maxEffectList)
	if err != nil {
		return nil, layout, err
	}
	return parseEffectPackages(data, layout), layout, nil
}

func (r *ReShade) installEffectPackage(ctx context.Context, layout layout, pkg EffectPackage) error {
	return r.runTransfer(ctx, pkg.Name, layout.effects(), pkg.ID,
		func(ctx context.Context, progress func(int64, int64)) error {
			r.effectsMu.Lock()
			defer r.effectsMu.Unlock()
			return r.installEffectPackageFiles(ctx, layout, pkg, progress)
		})
}

func (r *ReShade) installEffectPackageFiles(
	ctx context.Context,
	layout layout,
	pkg EffectPackage,
	progress func(int64, int64),
) error {
	match := effectPackRE.FindStringSubmatch(pkg.ID)
	if match == nil {
		return errors.New("RESHADE_EFFECT_PACKAGE_UNSUPPORTED")
	}
	if r.archive == nil {
		return errors.New("archive extractor is not configured")
	}
	staging, err := os.MkdirTemp(layout.root, ".effects-")
	if err != nil {
		return err
	}
	retainBackup := false
	defer func() {
		if !retainBackup {
			_ = os.RemoveAll(staging)
		}
	}()

	archivePath := filepath.Join(staging, "package.zip")
	err = r.github.DownloadFile(ctx, github.FileRequest{
		Repo: github.Repo{Owner: match[1], Name: match[2]}, URL: pkg.ID, Destination: archivePath,
		MaxSize: maxEffectPack,
		Progress: func(downloaded, total int64) {
			progress(downloaded, total)
			r.emitProgress("effects", pkg.Name, "download", downloaded, total)
		},
	})
	r.emitProgress("effects", pkg.Name, "done", 0, 0)
	if err != nil {
		return fmt.Errorf("download effect package: %w", err)
	}
	extracted, err := r.archive.Extract(ctx, archivePath, filepath.Join(staging, "files"),
		infra.ExtractOptions{FlattenSingleRoot: new(bool)}, nil)
	if err != nil {
		return fmt.Errorf("extract effect package: %w", err)
	}

	records, err := readEffectRecords(layout.record())
	if err != nil {
		return err
	}
	release, err := diskio.AcquireDir(ctx, layout.effects())
	if err != nil {
		return err
	}
	var files []string
	prepared := filepath.Join(staging, "prepared")
	for _, part := range []struct{ name, destination string }{
		{"Shaders", pkg.installPath}, {"Textures", pkg.textureInstallPath},
	} {
		source := findDirectory(extracted, part.name)
		if source == "" {
			continue
		}
		relative, relErr := filepath.Rel(layout.effects(), part.destination)
		if relErr != nil {
			err = relErr
			break
		}
		copied, copyErr := copyEffectFiles(ctx, source, filepath.Join(prepared, relative), prepared, pkg.denied)
		files = append(files, copied...)
		if copyErr != nil {
			err = copyErr
			break
		}
	}
	release()
	if err != nil {
		return err
	}
	retainBackup, err = commitEffectPackage(ctx, layout, pkg, staging, files, records)
	return err
}

// parseEffectPackages reads the official package list. A package is supported only when it comes
// from a GitHub archive and installs below the shared effect folders.
func parseEffectPackages(data []byte, layout layout) []EffectPackage {
	var packages []EffectPackage
	var fields map[string]string
	flush := func() {
		if fields == nil || fields["DownloadUrl"] == "" || fields["PackageName"] == "" {
			return
		}
		pkg := EffectPackage{
			ID: fields["DownloadUrl"], Name: fields["PackageName"], Description: fields["PackageDescription"],
			Repository:  fields["RepositoryUrl"],
			Recommended: fields["Enabled"] == "1" || fields["Required"] == "1",
		}
		for _, name := range strings.Split(fields["DenyEffectFiles"], ",") {
			if name = strings.TrimSpace(name); name != "" {
				pkg.denied = append(pkg.denied, name)
			}
		}
		var shadersOK, texturesOK bool
		pkg.installPath, shadersOK = effectInstallPath(layout, fields["InstallPath"])
		pkg.textureInstallPath, texturesOK = effectInstallPath(layout, fields["TextureInstallPath"])
		pkg.Supported = shadersOK && texturesOK && effectPackRE.MatchString(pkg.ID)
		packages = append(packages, pkg)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64<<10), maxEffectList)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			fields = map[string]string{}
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && fields != nil {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	flush()
	return packages
}

// effectInstallPath maps a list path such as `.\reshade-shaders\Shaders\Name` into the shared
// effect folders and rejects one that would land anywhere else.
func effectInstallPath(layout layout, listed string) (string, bool) {
	relative := strings.TrimPrefix(strings.ReplaceAll(listed, "/", `\`), `.\`)
	rest, ok := strings.CutPrefix(relative, effectsDirName+`\`)
	if !ok || rest == "" || filepath.IsAbs(rest) || filepath.VolumeName(rest) != "" {
		return "", false
	}
	target := filepath.Join(layout.effects(), rest)
	if platform.SamePathFold(layout.effects(), target) || !platform.SameOrChildPath(layout.effects(), target) {
		return "", false
	}
	return target, true
}

// effectFilePath returns where a package file lives below the shared effect folders. It rejects a
// file that a junction or link would place elsewhere, which the lexical check cannot see.
func effectFilePath(layout layout, file string) (string, error) {
	target := filepath.Join(layout.effects(), file)
	if platform.SamePathFold(target, layout.effects()) || !platform.SameOrChildPath(layout.effects(), target) {
		return "", fmt.Errorf("effect path escapes shared folders: %s", file)
	}
	root, err := platform.FinalPath(layout.effects())
	if err != nil {
		return "", err
	}

	// A file yet to be created is judged by the deepest folder that exists above it.
	for current := target; ; {
		resolved, err := platform.FinalPath(current)
		if err == nil {
			if !platform.SameOrChildPath(root, resolved) {
				return "", fmt.Errorf("effect path leaves shared folders through a link: %s", file)
			}
			return target, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		// A link whose target is missing still redirects whatever is created through it.
		if _, statErr := os.Lstat(current); statErr == nil {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		current = parent
	}
}

// findDirectory returns the shallowest directory called name below root, or "".
func findDirectory(root, name string) string {
	for level := []string{root}; len(level) > 0; {
		var next []string
		for _, dir := range level {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				if strings.EqualFold(entry.Name(), name) {
					return path
				}
				next = append(next, path)
			}
		}
		level = next
	}
	return ""
}

// copyEffectFiles copies a package folder into destination and returns the copied files relative to
// the shared effect root, including those copied before an error.
func copyEffectFiles(ctx context.Context, source, destination, effectsRoot string, denied []string) ([]string, error) {
	var copied []string
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		if slices.ContainsFunc(denied, func(name string) bool { return strings.EqualFold(name, entry.Name()) }) {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if err := copyFile(path, target); err != nil {
			return fmt.Errorf("copy %s: %w", relative, err)
		}
		recorded, err := filepath.Rel(effectsRoot, target)
		if err != nil {
			return err
		}
		copied = append(copied, recorded)
		return nil
	})
	return copied, err
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".effect-")
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	if err := errors.Join(copyErr, temporary.Close()); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	if err := platform.ReplaceAtomic(temporary.Name(), destination); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	return nil
}

// removeEffectFiles deletes a package's files except those another installed package also provides,
// then drops the folders left empty.
func removeEffectFiles(ctx context.Context, layout layout, files []string, remaining map[string]effectRecord) error {
	if len(files) == 0 {
		return nil
	}
	if _, err := os.Stat(layout.effects()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	shared := map[string]bool{}
	for _, record := range remaining {
		for _, file := range record.Files {
			shared[strings.ToLower(file)] = true
		}
	}
	release, err := diskio.AcquireDir(ctx, layout.effects())
	if err != nil {
		return err
	}
	defer release()
	kept := []string{layout.shaders(), layout.textures(), layout.addons(), layout.presets()}
	for _, file := range files {
		if shared[strings.ToLower(file)] {
			continue
		}
		target, err := effectFilePath(layout, file)
		if err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for dir := filepath.Dir(target); platform.SameOrChildPath(layout.effects(), dir); dir = filepath.Dir(dir) {
			if platform.SamePathFold(dir, layout.effects()) ||
				slices.ContainsFunc(kept, func(keep string) bool { return platform.SamePathFold(keep, dir) }) ||
				os.Remove(dir) != nil {
				break
			}
		}
	}
	return nil
}

func readEffectRecords(path string) (map[string]effectRecord, error) {
	records := map[string]effectRecord{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode effect package record: %w", err)
	}
	return records, nil
}

func writeEffectRecords(path string, records map[string]effectRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".packages-")
	if err != nil {
		return err
	}
	_, writeErr := temporary.Write(data)
	if err := errors.Join(writeErr, temporary.Close()); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	if err := platform.ReplaceAtomic(temporary.Name(), path); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	return nil
}
