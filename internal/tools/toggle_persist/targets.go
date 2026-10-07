package togglepersist

import (
	"context"
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
)

var globalDeclarationRE = regexp.MustCompile(`(?i)^global\s+(persist\s+)?\$([^\s=\\]+)(?:\s*=\s*.*)?$`)

type persistTarget struct {
	iniPath     string
	varName     string
	key         string
	importer    string
	info        os.FileInfo
	fingerprint string
	persistent  bool
	timeDriven  bool
}

// Sections 3DMigoto runs on its own every frame or draw call, as opposed to
// [Key*] sections that only run on user input.
var frameSectionPrefixes = []string{
	"present",
	"textureoverride",
	"shaderoverride",
	"shaderregex",
	"clearrendertargetview",
	"cleardepthstencilview",
	"clearunorderedaccessview",
}

var commandTokenRE = regexp.MustCompile(`[$\w\\.]+`)

type commandAssignment struct {
	variable string
	inputs   []string
}

type commandSection struct {
	perFrame bool
	assigns  []commandAssignment
	runs     []string
}

var callableSectionPrefixes = []string{"commandlist", "customshader"}

// commandGraph holds the command lists of every INI an importer loads, because
// a mod split over several files assigns and runs across them.
type commandGraph map[string]*commandSection

type persistTargetIndex map[string][]persistTarget

// Index declarations rather than interpreting namespaces as filesystem paths.
// 3DMigoto loads nothing below a DISABLED name, so a disabled copy neither owns
// a persisted value nor makes the enabled copy of its namespace ambiguous.
func indexPersistTargets(importerFolder string) (persistTargetIndex, error) {
	root, err := filepath.Abs(importerFolder)
	if err != nil {
		return nil, fmt.Errorf("resolve persist importer %q: %w", importerFolder, err)
	}
	index := persistTargetIndex{}
	graph := commandGraph{}
	visit := func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") ||
				strings.HasPrefix(strings.ToLower(entry.Name()), "disabled")) {
				return fs.SkipDir
			}
			return nil
		}
		lowerName := strings.ToLower(entry.Name())
		if !entry.Type().IsRegular() || filepath.Ext(lowerName) != ".ini" ||
			strings.HasPrefix(lowerName, "disabled") || lowerName == "d3dx_user.ini" {
			return nil
		}
		content, info, err := readPersistINI(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		namespace := strings.ReplaceAll(relative, string(os.PathSeparator), `\`)
		if strings.EqualFold(relative, "d3dx.ini") {
			namespace = ""
		}
		targets, namespace := parsePersistTargets(string(content), namespace)
		graph.add(string(content), namespace)
		fingerprint := ""
		if len(targets) > 0 {
			fingerprint = fingerprintTogglePersistINI(string(content))
		}
		for _, target := range targets {
			target.iniPath = path
			target.importer = root
			target.info = info
			target.fingerprint = fingerprint
			index[target.key] = append(index[target.key], target)
		}
		return nil
	}
	if err := filepath.WalkDir(root, visit); err != nil {
		return nil, fmt.Errorf("index persist declarations in %q: %w", root, err)
	}

	// XXMI may link its Mods directory to the user's external mod collection.
	// Windows junctions have ModeIrregular rather than ModeSymlink. Directory
	// reparse points already visited by the initial walk must not be indexed twice.
	// Follow only this explicit root; nested links and linked INI files are not
	// traversed, so a namespace cannot redirect writes through an arbitrary link.
	mods := filepath.Join(root, "Mods")
	if info, err := os.Lstat(mods); err == nil && !info.IsDir() &&
		info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		if err := filepath.WalkDir(mods+string(os.PathSeparator), visit); err != nil {
			return nil, fmt.Errorf("index linked persist mods %q: %w", mods, err)
		}
	}

	for key := range graph.timeDriven() {
		for i := range index[key] {
			index[key][i].timeDriven = true
		}
	}
	return index, nil
}

func parsePersistTargets(content, namespace string) ([]persistTarget, string) {
	targets := []persistTarget{}
	preamble := true
	inConstants := false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			preamble = false
			inConstants = strings.EqualFold(trimmed, "[Constants]")
			continue
		}
		if preamble {
			key, value, found := strings.Cut(trimmed, "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "namespace") {
				if comment := strings.IndexAny(value, ";#"); comment >= 0 {
					value = value[:comment]
				}
				namespace = strings.TrimSpace(value)
			}
			continue
		}
		if !inConstants {
			continue
		}
		match := globalDeclarationRE.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		key := "$" + match[2]
		if namespace != "" {
			key = `$\` + namespace + `\` + match[2]
		}
		targets = append(targets, persistTarget{
			key: strings.ToLower(key), varName: match[2], persistent: match[1] != "",
		})
	}
	return targets, namespace
}

// add records the command lists of one INI under the names other files use to
// reach them: 3DMigoto exposes `$var` and `[CommandListName]` of a namespaced
// file as `$\namespace\var` and `CommandList\namespace\Name`.
func (graph commandGraph) add(content, namespace string) {
	namespace = strings.ToLower(namespace)
	variable := func(token string) string {
		unqualified := strings.HasPrefix(token, "$") && !strings.HasPrefix(token, `$\`)
		if namespace == "" || !unqualified {
			return token
		}
		return `$\` + namespace + `\` + token[1:]
	}
	callable := func(name string) (string, bool) {
		for _, prefix := range callableSectionPrefixes {
			if rest, ok := strings.CutPrefix(name, prefix); ok {
				if namespace == "" || strings.HasPrefix(rest, `\`) {
					return name, true
				}
				return prefix + `\` + namespace + `\` + rest, true
			}
		}
		return name, false
	}
	tokens := func(text string) []string {
		found := commandTokenRE.FindAllString(text, -1)
		for i, token := range found {
			found[i] = variable(token)
		}
		return found
	}

	var section *commandSection
	var conditions [][]string
	lower := strings.ToLower(strings.TrimPrefix(content, "\uFEFF"))
	for _, line := range strings.Split(strings.ReplaceAll(lower, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			name := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			key, shared := callable(name)
			if !shared {
				key = namespace + "|" + name
			}
			section = graph[key]
			if section == nil {
				section = &commandSection{}
				graph[key] = section
			}
			section.perFrame = section.perFrame || slices.ContainsFunc(frameSectionPrefixes, func(prefix string) bool {
				return strings.HasPrefix(name, prefix)
			})
			conditions = nil
			continue
		}
		if section == nil {
			continue
		}
		for _, modifier := range []string{"pre ", "post "} {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, modifier))
		}

		switch {
		case strings.HasPrefix(trimmed, "if "):
			conditions = append(conditions, tokens(trimmed))
		case strings.HasPrefix(trimmed, "elif "), strings.HasPrefix(trimmed, "else if "):
			// A later branch also depends on every earlier condition being false.
			if last := len(conditions) - 1; last >= 0 {
				conditions[last] = append(conditions[last], tokens(trimmed)...)
			}
		case trimmed == "endif":
			if last := len(conditions) - 1; last >= 0 {
				conditions = conditions[:last]
			}
		default:
			key, value, found := strings.Cut(trimmed, "=")
			if !found {
				continue
			}
			key = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(key), "local "))
			if key == "run" {
				target, _ := callable(strings.TrimSpace(value))
				section.runs = append(section.runs, target)
			} else if strings.HasPrefix(key, "$") {
				inputs := tokens(value)
				for _, condition := range conditions {
					inputs = append(inputs, condition...)
				}
				section.assigns = append(section.assigns, commandAssignment{variable: variable(key), inputs: inputs})
			}
		}
	}
}

// A variable is time driven when a frame section, or a command list it runs,
// assigns it a value that depends on the `time` builtin, either in the
// expression or in an enclosing condition, directly or through other variables.
// Only such a variable can change without user input.
//
// Being assigned from a frame section is not enough: GUI mods handle cursor
// clicks, sliders and presets in [Present]. Time dependence is not sufficient
// either, because a slider that eases toward the value the user picked depends
// on time too, so this only marks candidates and the learner holds them back
// until they stop changing.
func (graph commandGraph) timeDriven() map[string]struct{} {
	var queue []string
	for name, section := range graph {
		if section.perFrame {
			queue = append(queue, name)
		}
	}
	var assigns []commandAssignment
	visited := map[string]struct{}{}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		section := graph[name]
		if _, seen := visited[name]; seen || section == nil {
			continue
		}
		visited[name] = struct{}{}
		assigns = append(assigns, section.assigns...)
		queue = append(queue, section.runs...)
	}

	driven := map[string]struct{}{}
	for changed := true; changed; {
		changed = false
		for _, assign := range assigns {
			if _, done := driven[assign.variable]; done {
				continue
			}
			if slices.ContainsFunc(assign.inputs, func(input string) bool {
				_, dependent := driven[input]
				return dependent || input == "time"
			}) {
				driven[assign.variable] = struct{}{}
				changed = true
			}
		}
	}
	return driven
}

func (index persistTargetIndex) resolve(key string) (*persistTarget, error) {
	targets := index[strings.ToLower(key)]
	if len(targets) > 1 {
		paths := make([]string, len(targets))
		for i, target := range targets {
			paths[i] = target.iniPath
		}
		return nil, fmt.Errorf("ambiguous persist variable %s declared in %s", key, strings.Join(paths, ", "))
	}
	if len(targets) == 0 || !targets[0].persistent {
		return nil, nil
	}
	target := targets[0]
	return &target, nil
}

func readPersistINI(path string) ([]byte, os.FileInfo, error) {
	// The walk that indexes declarations is sequential, but it overlaps scans and
	// uploads on the same disk. A background context never fails the wait.
	release, _ := diskio.Acquire(context.Background(), path)
	defer release()

	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	// File.Stat captures Windows file identity from the open handle. os.Stat
	// defers that lookup until SameFile, when the path may belong to another mod.
	info, statErr := file.Stat()
	content, readErr := io.ReadAll(file)
	if err := errors.Join(statErr, readErr, file.Close()); err != nil {
		return nil, nil, fmt.Errorf("read persist ini %q: %w", path, err)
	}
	return content, info, nil
}
