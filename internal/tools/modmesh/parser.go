// Package modmesh reads the mesh sources of a mod: the INI bundle that declares
// its buffer resources, and the position, index, and blend buffers behind them.
package modmesh

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type Section struct {
	Header string
	Name   string
	Lines  []string
	Values map[string]string
}

type BufferResource struct {
	Name     string
	Filename string
	Stride   int
	Format   string
}

var (
	modINIHeaderRE  = regexp.MustCompile(`^\[([^\]]+)\]$`)
	modINIMergedRE  = regexp.MustCompile(`(?i)^\s*;\s*(?:merged mods?|合并mod)\s*:\s*(.+)$`)
	modINISectionRE = regexp.MustCompile(
		`(?i)^(TextureOverride|ShaderOverride|Resource|Constants|Present|CommandList|CustomShader)(.*)$`,
	)
	modLOResourceRE  = regexp.MustCompile(`(?i)(?:_LOD$|_VB\d+_LOD)`)
	positionCSNameRE = regexp.MustCompile(`(?i)position(?:\.\d+)?cs$`)
	componentVB0RE   = regexp.MustCompile(`(?i)component\d+_vb0$`)
	componentVB2RE   = regexp.MustCompile(`(?i)component\d+_vb2$`)
	resourceRefRE    = regexp.MustCompile(`(?i)^(?:ref\s+)?Resource(.+)$`)
	nonAlphaNumRE    = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

func LoadINIBundle(input string) (string, []Section, error) {
	iniPath, sections, _, err := LoadINIBundleWithSources(input)
	return iniPath, sections, err
}

func LoadINIBundleWithSources(input string) (string, []Section, []string, error) {
	iniPath, err := FindPrimaryINI(input)
	if err != nil {
		return "", nil, nil, err
	}
	text, err := os.ReadFile(iniPath)
	if err != nil {
		return "", nil, nil, err
	}
	sections := parseINI(string(text))
	sourcePaths := []string{iniPath}
	base := filepath.Dir(iniPath)
	refs := extractMergedINIRefs(string(text))
	for _, ref := range refs {
		refPath, resolveErr := resolveMergedINIRef(base, ref)
		if resolveErr != nil || platform.SamePathFold(refPath, iniPath) {
			continue
		}
		refText, readErr := os.ReadFile(refPath)
		if readErr != nil {
			return "", nil, nil, readErr
		}
		sections = append(sections, parseINI(string(refText))...)
		sourcePaths = append(sourcePaths, refPath)
	}
	return iniPath, sections, sourcePaths, nil
}

func FindPrimaryINI(input string) (string, error) {
	resolved, err := filepath.Abs(input)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.Mode().IsRegular() {
		return resolved, nil
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a file or directory: %s", resolved)
	}
	type candidate struct {
		path  string
		score int
	}
	var candidates []candidate
	err = filepath.WalkDir(resolved, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ini") ||
			strings.HasPrefix(strings.ToLower(entry.Name()), "disabled") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		candidates = append(candidates, candidate{path: path, score: scoreModINI(path, string(raw))})
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", infra.ContractError(fmt.Sprintf("No .ini found in %s", input))
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].path < candidates[j].path
	})
	return candidates[0].path, nil
}

func scoreModINI(path, text string) int {
	base := strings.ToLower(filepath.Base(path))
	score := 0
	overrideCount, resourceCount := 0, 0
	if base == "merged.ini" {
		score += 120
	}
	if strings.HasPrefix(base, "master") && strings.HasSuffix(base, ".ini") {
		score += 140
	}
	if len(extractMergedINIRefs(text)) > 0 {
		score += 80
	}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "namespace") && strings.Contains(line, "="):
			score += 60
		case strings.HasPrefix(lower, "global persist $"):
			score += 15
		case lower == "type = cycle" || lower == "type=cycle":
			score += 10
		case strings.HasPrefix(lower, "[textureoverride"):
			overrideCount++
		case strings.HasPrefix(lower, "[resource"):
			resourceCount++
		}
	}
	score += min(overrideCount, 50) + min(resourceCount, 50)
	if strings.Contains(strings.ToLower(text), "[keyhelp]") {
		score -= 25
	}
	if strings.HasPrefix(base, "disabled") && len(extractMergedINIRefs(text)) == 0 {
		score -= 10
	}
	return score
}

func parseINI(text string) []Section {
	sections := make([]Section, 0)
	current := -1
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if match := modINIHeaderRE.FindStringSubmatch(line); len(match) == 2 {
			full := strings.TrimSpace(match[1])
			header, name := full, full
			if kind := modINISectionRE.FindStringSubmatch(full); len(kind) == 3 {
				header, name = kind[1], kind[2]
			}
			sections = append(sections, Section{Header: header, Name: name, Values: make(map[string]string)})
			current = len(sections) - 1
			continue
		}
		if current < 0 {
			continue
		}
		sections[current].Lines = append(sections[current].Lines, strings.TrimSpace(line))
		if index := strings.Index(line, "="); index >= 0 {
			sections[current].Values[strings.TrimSpace(line[:index])] = strings.TrimSpace(line[index+1:])
		}
	}
	return sections
}

func extractMergedINIRefs(text string) []string {
	var rawMatches []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[;") {
			break
		}
		if match := modINIMergedRE.FindStringSubmatch(raw); len(match) == 2 {
			rawMatches = append(rawMatches, strings.TrimSpace(match[1]))
		}
	}
	var out []string
	for _, raw := range rawMatches {
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			var list []string
			if json.Unmarshal([]byte(raw), &list) == nil {
				for _, entry := range list {
					if entry = strings.TrimSpace(entry); entry != "" {
						out = append(out, entry)
					}
				}
				continue
			}
		}
		if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
			var value string
			if json.Unmarshal([]byte(raw), &value) == nil && strings.TrimSpace(value) != "" {
				out = append(out, strings.TrimSpace(value))
				continue
			}
		}
		parts := strings.Split(raw, ",")
		if len(parts) > 1 {
			allINI := true
			for i := range parts {
				parts[i] = strings.Trim(strings.TrimSpace(parts[i]), `"'`)
				allINI = allINI && strings.EqualFold(filepath.Ext(parts[i]), ".ini")
			}
			if allINI {
				out = append(out, parts...)
				continue
			}
		}
		value := strings.Trim(strings.TrimSpace(raw), `"'`)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveMergedINIRef(baseDir, entry string) (string, error) {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	candidate := filepath.FromSlash(strings.TrimSpace(entry))
	var resolved string
	if filepath.IsAbs(candidate) {
		resolved, err = filepath.Abs(candidate)
	} else {
		resolved, err = filepath.Abs(filepath.Join(base, candidate))
	}
	if err != nil || !platform.SameOrChildPath(base, resolved) || platform.SamePathFold(base, resolved) {
		return "", infra.WithCause(errors.New("merged INI path is outside mod root"), err)
	}
	// EvalSymlinks can fail on Windows temp junctions even for in-tree files.
	// Keep the logical path when evaluation fails; successful evaluation still
	// rejects symlink escapes like Electron loadIniBundle.
	if realBase, evalErr := filepath.EvalSymlinks(base); evalErr == nil {
		if realPath, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
			if !platform.SameOrChildPath(realBase, realPath) || platform.SamePathFold(realBase, realPath) {
				return "", errors.New("merged INI path is outside mod root")
			}
			resolved = realPath
		}
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", infra.WithCause(errors.New("merged INI is not a regular file"), err)
	}
	return filepath.Clean(resolved), nil
}
