package mod

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ModDependency is a shared 3DMigoto library that a mod references without shipping it.
// Installed reports whether the importer currently loads the library.
type ModDependency struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type modLibrary struct {
	name      string
	namespace string
}

// modLibraries lists the shared libraries mods reach through a namespace, in display order.
// Importer cores such as ZZMI or WWMIv1 are left out: every mod of that importer needs them.
var modLibraries = []modLibrary{
	{name: "RabbitFX", namespace: "rabbitfx"},
	{name: "TexFx", namespace: "texfx"},
	{name: "ORFix", namespace: `global\orfix`},
	{name: "Offset", namespace: `global\offset`},
	{name: "HealthBar", namespace: `global\healthbar`},
	{name: "Tracking", namespace: `global\tracking`},
}

// libraryMask holds one bit per modLibraries entry.
type libraryMask uint8

// libraryIndex is the installed libraries of one mod folder and importer as of a
// libraryChanges count.
type libraryIndex struct {
	installed libraryMask
	changes   uint64
}

// libraryReferencePrefixes are the lowercase words that qualify a namespaced symbol,
// as in CommandList\RabbitFX\Run or $\TexFx\glow.
var libraryReferencePrefixes = map[string]bool{
	"$": true, "resource": true, "commandlist": true, "customshader": true, "textureoverride": true,
	"shaderoverride": true, "shaderregex": true, "preset": true, "key": true, "constants": true, "present": true,
}

// libraryHeadSize bounds how much of an INI is read to find its namespace declaration,
// which precedes the first section.
const libraryHeadSize = 4096

// referencedLibraries reports the libraries a lowercase INI line refers to. The prefix
// check keeps file paths such as Textures\RabbitFX\map.dds from counting as references.
func referencedLibraries(line string) libraryMask {
	var mask libraryMask
	for index, library := range modLibraries {
		needle := `\` + library.namespace + `\`
		for start := 0; ; {
			at := strings.Index(line[start:], needle)
			if at < 0 {
				break
			}
			at += start
			prefix := at
			for prefix > 0 && (line[prefix-1] == '$' || line[prefix-1] >= 'a' && line[prefix-1] <= 'z') {
				prefix--
			}
			if libraryReferencePrefixes[line[prefix:at]] {
				mask |= 1 << index
				break
			}
			start = at + 1
		}
	}
	return mask
}

func declaredLibrary(namespace string) libraryMask {
	for index, library := range modLibraries {
		if strings.EqualFold(namespace, library.namespace) {
			return 1 << index
		}
	}
	return 0
}

// iniCode returns line without its trailing comment, with each quoted string replaced by
// a space. It follows the namespace package: ; and # start a comment anywhere outside
// quotes, except in WWMI's #Pool resource queries.
func iniCode(line string) string {
	if !strings.ContainsAny(line, `;#"'`) {
		return line
	}
	var code strings.Builder
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '#' && len(line)-i >= 5 && strings.EqualFold(line[i:i+5], "#pool"):
			code.WriteByte(c)
		case c == ';' || c == '#':
			return code.String()
		case c == '"' || c == '\'':
			end := strings.IndexByte(line[i+1:], c)
			if end < 0 {
				return code.String()
			}
			i += end + 1
			code.WriteByte(' ')
		default:
			code.WriteByte(c)
		}
	}
	return code.String()
}

func iniNamespace(line string) (string, bool) {
	key, value, found := strings.Cut(iniCode(line), "=")
	if !found || !strings.EqualFold(strings.TrimSpace(key), "namespace") {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// requiredLibraries reports the libraries a mod references but does not declare itself.
// INIs below a DISABLED folder inside the mod are never loaded, so they count for neither.
func requiredLibraries(info ModInfo) libraryMask {
	var references, declares libraryMask
	for _, ini := range info.Inis {
		if libraryDisabledPath(info.Path, ini.Path) {
			continue
		}
		references |= ini.references
		declares |= ini.declares
	}
	return references &^ declares
}

// attachDependencies fills the dependencies of every mod in group. The installed
// libraries are looked up only when a mod needs one.
func (m *Mod) attachDependencies(ctx context.Context, game GameConfig, group *FolderGroup, reports ...func(error)) {
	var installed libraryMask
	scanned := false
	for index := range group.Mods {
		required := requiredLibraries(group.Mods[index])
		if required == 0 {
			continue
		}
		if !scanned {
			installed, scanned = m.installedLibraries(ctx, game, reports...), true
		}

		dependencies := []ModDependency{}
		for bit, library := range modLibraries {
			if required&(1<<bit) != 0 {
				dependencies = append(dependencies, ModDependency{
					Name: library.name, Installed: installed&(1<<bit) != 0,
				})
			}
		}
		group.Mods[index].Dependencies = dependencies
	}
}

// installedLibraries returns the libraries the game's importer loads, scanning its
// folders again only after a mod operation or a watched change.
func (m *Mod) installedLibraries(ctx context.Context, game GameConfig, reports ...func(error)) libraryMask {
	// The roots follow the mod folder and the importer, so a game whose settings change
	// must not reuse the index of its previous folders.
	key := strings.ToLower(game.ModFolderPath)
	if game.Importer != nil {
		key += "\x00" + strings.ToLower(*game.Importer)
	}
	m.libraryMu.Lock()
	changes := m.libraryChanges
	cached, ok := m.libraryIndexes[key]
	m.libraryMu.Unlock()
	if ok && cached.changes == changes {
		return cached.installed
	}

	installed := scanInstalledLibraries(m.libraryRoots(ctx, game), reports...)

	// An invalidation during the scan leaves the result unstored, so the next lookup scans again.
	m.libraryMu.Lock()
	if m.libraryChanges == changes {
		if m.libraryIndexes == nil {
			m.libraryIndexes = map[string]libraryIndex{}
		}
		m.libraryIndexes[key] = libraryIndex{installed: installed, changes: changes}
	}
	m.libraryMu.Unlock()
	return installed
}

// InvalidateLibraries drops the installed-library indexes after mod or importer files change.
//
//wails:ignore
func (m *Mod) InvalidateLibraries() {
	m.libraryMu.Lock()
	m.libraryChanges++
	m.libraryMu.Unlock()
}

// libraryRoots lists the folders whose INIs the game's importer loads: the game's mod
// folder, and Mods, Core and ShaderFixes of each importer folder that may serve it.
func (m *Mod) libraryRoots(ctx context.Context, game GameConfig) []string {
	folders := []string{}
	if m.xxmi != nil && game.Importer != nil {
		importers, err := m.xxmi.GetEnabledImporters(ctx)
		m.logShaderError(err, "libraries:importers")
		for _, importer := range importers {
			if strings.EqualFold(importer.Key, *game.Importer) {
				folders = append(folders, importer.ImporterFolder)
			}
		}
	}

	// A mod folder kept inside a 3DMigoto install is served by that install's d3dx.ini.
	for dir := game.ModFolderPath; ; {
		if _, err := os.Stat(filepath.Join(dir, "d3dx.ini")); err == nil {
			folders = append(folders, dir)
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	candidates := []string{game.ModFolderPath}
	for _, folder := range folders {
		for _, name := range []string{"Mods", "Core", "ShaderFixes"} {
			candidates = append(candidates, filepath.Join(folder, name))
		}
	}
	// An importer's Mods folder is commonly a junction to the game's mod folder, which
	// WalkDir would not enter and which must not be scanned twice.
	physical := []string{}
	for _, candidate := range candidates {
		if path, err := libraryPhysicalPath(candidate); err == nil {
			physical = append(physical, path)
		}
	}
	roots := []string{}
	for index, path := range physical {
		nested := slices.ContainsFunc(physical[:index], func(other string) bool { return pathWithin(other, path) }) ||
			slices.ContainsFunc(physical[index+1:], func(other string) bool {
				return pathWithin(other, path) && !pathWithin(path, other)
			})
		if !nested {
			roots = append(roots, path)
		}
	}
	return roots
}

// scanInstalledLibraries reads the namespace of every INI below roots that 3DMigoto loads.
func scanInstalledLibraries(roots []string, reports ...func(error)) libraryMask {
	var installed libraryMask
	for _, root := range roots {
		release := holdScanDisk(root)
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				reportScanFailure(err, reports)
				return fs.SkipDir
			}
			if path != root && strings.HasPrefix(strings.ToLower(entry.Name()), "disabled") {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ".ini") {
				installed |= declaredLibrary(readININamespace(path, reports...))
			}
			return nil
		})
		release()
	}
	return installed
}

func readININamespace(path string, reports ...func(error)) string {
	file, err := os.Open(path)
	if err != nil {
		reportScanFailure(err, reports)
		return ""
	}
	defer func() { _ = file.Close() }()
	head, err := io.ReadAll(io.LimitReader(file, libraryHeadSize))
	if err != nil {
		reportScanFailure(err, reports)
		return ""
	}

	// Dropping the zero bytes of a UTF-16 head leaves its ASCII text, which is all a
	// library namespace contains.
	if bytes.HasPrefix(head, []byte{0xff, 0xfe}) || bytes.HasPrefix(head, []byte{0xfe, 0xff}) {
		head = bytes.ReplaceAll(head[2:], []byte{0}, nil)
	}
	head = bytes.TrimPrefix(head, []byte{0xef, 0xbb, 0xbf})
	for line := range strings.Lines(string(head)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			break
		}
		if namespace, ok := iniNamespace(line); ok {
			return namespace
		}
	}
	return ""
}

// libraryDisabledPath reports whether a folder below root carries the DISABLED prefix.
// 3DMigoto loads nothing below such a folder.
func libraryDisabledPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(strings.Split(relative, string(os.PathSeparator)), func(part string) bool {
		return strings.HasPrefix(strings.ToLower(part), "disabled")
	})
}

// libraryPhysicalPath resolves symbolic links and junctions. filepath.EvalSymlinks
// leaves a Windows junction in place and WalkDir does not descend into one.
func libraryPhysicalPath(path string) (string, error) {
	for range 32 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}

		// Substitute the junction nearest the volume root, then resolve the result again.
		link, target := "", ""
		for current := resolved; ; current = filepath.Dir(current) {
			info, err := os.Lstat(current)
			if err != nil {
				return "", err
			}
			// Reparse points without a link target, such as cloud placeholders, are physical.
			if isReparsePoint(info) {
				if next, err := os.Readlink(current); err == nil {
					link, target = current, next
				}
			}
			if filepath.Dir(current) == current {
				break
			}
		}
		if link == "" {
			return resolved, nil
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(link), target)
		}
		relative, err := filepath.Rel(link, resolved)
		if err != nil {
			return "", err
		}
		path = filepath.Join(target, relative)
	}
	return "", fmt.Errorf("too many links in %q", path)
}
