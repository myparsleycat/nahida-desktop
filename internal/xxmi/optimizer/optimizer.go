package optimizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Options struct {
	Importer       string
	ImporterFolder string
	CachePath      string
	Exclude        []string
	Prefix         string
	DryRun         bool
	ResetCache     bool
}

type Change struct {
	Path   string `json:"path"`
	Action string `json:"action"`
	Reason string `json:"reason"`
	Line   int    `json:"line,omitempty"`
}

type Report struct {
	Changes       []Change `json:"changes"`
	DisabledFiles int      `json:"disabledFiles"`
	DisabledMods  int      `json:"disabledMods"`
	EditedFiles   int      `json:"editedFiles"`
	EditedLines   int      `json:"editedLines"`
}

type iniFile struct {
	path    string
	data    []byte
	lines   []string
	newline string
	bom     bool
	final   bool
}

func Optimize(ctx context.Context, options Options) (Report, error) {
	if options.Prefix != "DISABLED " && options.Prefix != "DISABLED_" {
		return Report{}, errors.New("invalid disabled prefix")
	}
	if options.ImporterFolder == "" || !filepath.IsAbs(options.ImporterFolder) {
		return Report{}, errors.New("invalid importer folder")
	}
	if len(options.Exclude) == 0 {
		options.Exclude = []string{"DISABLED*"}
	}
	cache := map[string]int64{}
	if !options.ResetCache {
		if data, err := os.ReadFile(options.CachePath); err == nil {
			_ = json.Unmarshal(data, &cache)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Report{}, err
		}
	}
	mods := filepath.Join(options.ImporterFolder, "Mods")
	shaderFixes := filepath.Join(options.ImporterFolder, "ShaderFixes")
	packagedNamespaces := map[string]bool{}
	if options.Importer == "GIMI" || options.Importer == "ZZMI" {
		library := filepath.Join(options.ImporterFolder, "Core", options.Importer, "Libraries")
		if err := walkINI(ctx, library, options.Exclude, func(path string, info fs.FileInfo) error {
			file, err := readINI(path)
			if err != nil {
				return err
			}
			if namespace := iniNamespace(file); namespace != "" {
				packagedNamespaces[namespace] = true
			}
			return nil
		}); err != nil {
			return Report{}, err
		}
	}
	type pendingINI struct {
		file    iniFile
		changes []Change
	}
	pending := []pendingINI{}
	modDisables := map[string]Change{}
	for _, folder := range []string{mods, shaderFixes} {
		if err := walkINI(ctx, folder, options.Exclude, func(path string, info fs.FileInfo) error {
			if !options.DryRun && cache[path] == info.ModTime().UnixNano() {
				return nil
			}
			file, err := readINI(path)
			if err != nil {
				return err
			}
			changes := inspect(file, options.Importer, folder, packagedNamespaces)
			if len(changes) == 1 && changes[0].Action == "disable-mod" {
				modDisables[changes[0].Path] = changes[0]
				changes = nil
			}
			pending = append(pending, pendingINI{file: file, changes: changes})
			return nil
		}); err != nil {
			return Report{}, err
		}
	}
	report := Report{Changes: []Change{}}
	for _, item := range pending {
		skipped := false
		for path := range modDisables {
			if strings.HasPrefix(strings.ToLower(item.file.path), strings.ToLower(path+string(filepath.Separator))) {
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		report.Changes = append(report.Changes, item.changes...)
		if options.DryRun {
			continue
		}
		if err := apply(item.file, item.changes, options.Prefix); err != nil {
			return Report{}, err
		}
		if updated, err := os.Stat(item.file.path); err == nil {
			cache[item.file.path] = updated.ModTime().UnixNano()
		} else if errors.Is(err, os.ErrNotExist) {
			delete(cache, item.file.path)
		} else {
			return Report{}, err
		}
	}
	for _, change := range modDisables {
		report.Changes = append(report.Changes, change)
		if !options.DryRun {
			if err := disablePath(change.Path, options.Prefix); err != nil {
				return Report{}, err
			}
		}
	}
	for _, change := range report.Changes {
		switch change.Action {
		case "disable-file":
			report.DisabledFiles++
		case "disable-mod":
			report.DisabledMods++
		case "edit-line":
			report.EditedLines++
		}
	}
	seen := map[string]bool{}
	for _, change := range report.Changes {
		if change.Action == "edit-line" && !seen[change.Path] {
			report.EditedFiles++
			seen[change.Path] = true
		}
	}
	slices.SortFunc(report.Changes, func(a, b Change) int {
		if order := strings.Compare(a.Path, b.Path); order != 0 {
			return order
		}
		return a.Line - b.Line
	})
	if !options.DryRun {
		if err := os.MkdirAll(filepath.Dir(options.CachePath), 0o700); err != nil {
			return Report{}, err
		}
		data, err := json.MarshalIndent(cache, "", "  ")
		if err != nil {
			return Report{}, err
		}
		if err := os.WriteFile(options.CachePath, data, 0o600); err != nil {
			return Report{}, err
		}
	}
	return report, nil
}

// walkINI visits the INI files under folder. Like the reference launcher's os.walk(followlinks=True), it
// follows symbolic links and junctions, including a linked folder itself, since users commonly relocate Mods
// or individual mods that way. A link back into one of its own ancestors is skipped to stop cycles.
func walkINI(ctx context.Context, folder string, exclude []string, visit func(string, fs.FileInfo) error) error {
	info, err := os.Stat(folder)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return walkINIDir(ctx, folder, []os.FileInfo{info}, exclude, visit)
}

func walkINIDir(
	ctx context.Context,
	folder string,
	ancestors []os.FileInfo,
	exclude []string,
	visit func(string, fs.FileInfo) error,
) error {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if excludedININame(entry.Name(), exclude) {
			continue
		}
		path := filepath.Join(folder, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			// A dangling link is ignored, as os.walk does.
			if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}

		if info.IsDir() {
			if slices.ContainsFunc(ancestors, func(ancestor os.FileInfo) bool { return os.SameFile(ancestor, info) }) {
				continue
			}
			if err := walkINIDir(ctx, path, append(ancestors, info), exclude, visit); err != nil {
				return err
			}
			continue
		}
		if info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(path), ".ini") {
			if err := visit(path, info); err != nil {
				return err
			}
		}
	}
	return nil
}

func excludedININame(name string, exclude []string) bool {
	for _, pattern := range exclude {
		if matched, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(name)); matched {
			return true
		}
	}
	return false
}

func readINI(path string) (iniFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return iniFile{}, err
	}
	if len(data) > 16<<20 {
		return iniFile{}, fmt.Errorf("INI %q exceeds 16 MiB", path)
	}
	file := iniFile{path: path, data: data, newline: "\n", final: bytes.HasSuffix(data, []byte("\n"))}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		file.bom = true
		data = data[3:]
	}
	if bytes.Contains(data, []byte("\r\n")) {
		file.newline = "\r\n"
	}
	text := strings.TrimSuffix(string(data), "\n")
	text = strings.TrimSuffix(text, "\r")
	if text != "" {
		file.lines = strings.Split(text, file.newline)
	}
	return file, nil
}

// iniNamespace returns the namespace an INI file declares. 3DMigoto honors the declaration
// only on the first line that is neither blank nor a comment.
func iniNamespace(file iniFile) string {
	for _, line := range file.lines {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if name, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(name) == "namespace" {
			return strings.TrimSpace(value)
		}
		return ""
	}
	return ""
}

type section struct {
	name     string
	triggers []int
	runs     []string
}

func inspect(file iniFile, importer, folder string, packagedNamespaces map[string]bool) []Change {
	shaderFixes := strings.EqualFold(filepath.Base(folder), "ShaderFixes")
	add := func(action, reason string, line int) []Change {
		return []Change{{Path: file.path, Action: action, Reason: reason, Line: line}}
	}
	name := strings.ToLower(filepath.Base(file.path))
	if name == "3dvision2sbs.ini" || name == "help.ini" || name == "mouse.ini" || name == "upscale.ini" {
		if shaderFixes || strings.EqualFold(filepath.Base(filepath.Dir(file.path)), "ShaderFixes") {
			return add("disable-file", "unwanted ShaderFixes file", 0)
		}
	}
	if !shaderFixes && importer == "EFMI" && name == "vscheck.ini" {
		return add("disable-file", "unwanted VSCheck file", 0)
	}
	if !shaderFixes && packagedNamespaces[iniNamespace(file)] && iniNamespace(file) != "" {
		return add("disable-file", "duplicate packaged library namespace", 0)
	}
	sections := map[string]*section{}
	var current *section
	changes := []Change{}
	for index, raw := range file.lines {
		line := strings.ToLower(strings.TrimSpace(raw))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !shaderFixes {
				for _, prefix := range []string{"[loader", "[system", "[stereo", "[commandlistunbindallrendertargets"} {
					if strings.HasPrefix(line, prefix) {
						return add("disable-file", "rogue d3dx.ini section", 0)
					}
				}
			}
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			current = sections[name]
			if current == nil {
				current = &section{name: name}
				sections[name] = current
			}
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !shaderFixes && current.name == "include" &&
			(key == "include_recursive" && value == "mods" || key == "exclude_recursive" && value == "disabled*") {
			return add("disable-file", "rogue d3dx.ini include", 0)
		}
		if key == "run" && value != "" {
			current.runs = append(current.runs, value)
		}
		if key != "checktextureoverride" || value == "" {
			continue
		}
		current.triggers = append(current.triggers, index)
		if !shaderFixes && (importer == "EFMI" && value == "ib" ||
			importer == "WWMI" && (value == "ib" || value == "vb0")) {
			reason := value
			if importer == "WWMI" && value == "ib" {
				reason = "wwmi ib"
			}
			changes = append(changes, Change{Path: file.path, Action: "edit-line", Reason: reason, Line: index + 1})
		}
	}
	if !shaderFixes {
		globalTrigger := false
		for name, section := range sections {
			if !strings.HasPrefix(name, "shaderregex") || sections[name+".pattern"] != nil {
				continue
			}
			globalTrigger = globalTrigger || len(section.triggers) > 0
			for _, run := range section.runs {
				if called := sections[run]; called != nil {
					globalTrigger = globalTrigger || len(called.triggers) > 0
				}
			}
		}
		if globalTrigger {
			relative, err := filepath.Rel(folder, file.path)
			if err == nil {
				parts := strings.Split(relative, string(filepath.Separator))
				if len(parts) > 1 {
					return []Change{
						{
							Path:   filepath.Join(folder, parts[0]),
							Action: "disable-mod",
							Reason: "global ShaderRegex trigger",
						},
					}
				}
			}
			return add("disable-file", "global ShaderRegex trigger", 0)
		}
	}
	slices.SortFunc(changes, func(a, b Change) int { return a.Line - b.Line })
	return slices.CompactFunc(changes, func(a, b Change) bool { return a.Line == b.Line })
}

func apply(file iniFile, changes []Change, prefix string) error {
	if len(changes) == 0 {
		return nil
	}
	if changes[0].Action == "disable-file" {
		return disablePath(file.path, prefix)
	}
	for _, change := range changes {
		index := change.Line - 1
		if index < 0 || index >= len(file.lines) {
			return fmt.Errorf("invalid INI line %d", change.Line)
		}
		line := file.lines[index]
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if change.Reason == "wwmi ib" {
			file.lines[index] = indent + `$\WWMIv1\enable_ib_callbacks = 1`
		} else {
			file.lines[index] = indent + ";" + strings.TrimSpace(line)
		}
	}
	for index := 0; ; index++ {
		backup := file.path + ".xxmi_bak"
		if index > 0 {
			backup = fmt.Sprintf("%s.%d", backup, index)
		}
		if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(backup, file.data, 0o600); err != nil {
				return err
			}
			break
		} else if err != nil {
			return err
		}
	}
	result := strings.Join(file.lines, file.newline)
	if file.final {
		result += file.newline
	}
	if file.bom {
		result = "\xef\xbb\xbf" + result
	}
	return os.WriteFile(file.path, []byte(result), 0o600)
}

func disablePath(path, prefix string) error {
	name := filepath.Base(path)
	if strings.HasPrefix(strings.ToUpper(name), "DISABLED ") || strings.HasPrefix(strings.ToUpper(name), "DISABLED_") {
		return nil
	}
	for index := 0; ; index++ {
		target := filepath.Join(filepath.Dir(path), prefix+name)
		if index > 0 {
			target = fmt.Sprintf("%s.%d", target, index)
		}
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			return os.Rename(path, target)
		} else if err != nil {
			return err
		}
	}
}
