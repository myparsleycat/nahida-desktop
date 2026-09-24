package hunting

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	maxINIFileCount = 64
	maxINIBytes     = 4 << 20
)

type binding struct {
	chord     string
	forbidden []string
}

type categoryBindings struct {
	next binding
	mark binding
}

type huntingConfig struct {
	toggle     binding
	categories map[Category]categoryBindings
}

var categoryINIKeys = []struct {
	category Category
	name     string
}{
	{CategoryIndexBuffer, "indexbuffer"},
	{CategoryVertexBuffer, "vertexbuffer"},
	{CategoryVertexShader, "vertexshader"},
	{CategoryPixelShader, "pixelshader"},
	{CategoryComputeShader, "computeshader"},
	{CategoryGeometryShader, "geometryshader"},
	{CategoryDomainShader, "domainshader"},
	{CategoryHullShader, "hullshader"},
}

func readHuntingConfig(root, iniPath string) (huntingConfig, error) {
	values := make(map[string]string)
	seen := make(map[string]struct{})
	totalBytes, fileCount := 0, 0
	if err := readINI(root, iniPath, seen, &fileCount, &totalBytes, values); err != nil {
		return huntingConfig{}, fmt.Errorf("%w: read %s: %w", ErrConfigUnsupported, iniPath, err)
	}

	huntingRaw, exists := values["hunting"]
	if !exists || strings.TrimSpace(huntingRaw) == "" {
		return huntingConfig{}, fmt.Errorf("%w: missing hunting setting", ErrConfigUnsupported)
	}
	huntingValue, err := strconv.Atoi(strings.TrimSpace(huntingRaw))
	if err != nil {
		return huntingConfig{}, fmt.Errorf("%w: invalid hunting setting %q", ErrConfigUnsupported, huntingRaw)
	}
	if huntingValue == 0 {
		return huntingConfig{}, fmt.Errorf(
			"%w: turn on Enable Hunting in XXMI and relaunch the game",
			ErrDisabled,
		)
	}
	if !strings.EqualFold(strings.TrimSpace(values["marking_mode"]), "skip") {
		return huntingConfig{}, fmt.Errorf("%w: marking_mode must be skip", ErrConfigUnsupported)
	}
	if !containsWord(values["marking_actions"], "clipboard") {
		return huntingConfig{}, fmt.Errorf("%w: marking_actions must include clipboard", ErrConfigUnsupported)
	}

	toggle, err := parseBinding(values["toggle_hunting"])
	if err != nil {
		return huntingConfig{}, fmt.Errorf("%w: toggle_hunting: %w", ErrConfigUnsupported, err)
	}
	config := huntingConfig{toggle: toggle, categories: make(map[Category]categoryBindings)}
	for _, item := range categoryINIKeys {
		nextRaw, markRaw := values["next_"+item.name], values["mark_"+item.name]
		if strings.TrimSpace(nextRaw) == "" || strings.TrimSpace(markRaw) == "" {
			continue
		}
		next, nextErr := parseBinding(nextRaw)
		mark, markErr := parseBinding(markRaw)
		if nextErr != nil || markErr != nil {
			return huntingConfig{}, fmt.Errorf(
				"%w: %s bindings: %w",
				ErrConfigUnsupported,
				item.category,
				errors.Join(nextErr, markErr),
			)
		}
		config.categories[item.category] = categoryBindings{next: next, mark: mark}
	}
	if len(config.categories) == 0 {
		return huntingConfig{}, fmt.Errorf("%w: no complete next/mark binding pair", ErrConfigUnsupported)
	}
	return config, nil
}

func readINI(
	root,
	path string,
	seen map[string]struct{},
	fileCount,
	totalBytes *int,
	hunting map[string]string,
) error {
	resolved, err := secureINIPath(root, path)
	if err != nil {
		return err
	}
	key := strings.ToLower(resolved)
	if _, ok := seen[key]; ok {
		return fmt.Errorf("cyclic include %s", resolved)
	}
	(*fileCount)++
	if *fileCount > maxINIFileCount {
		return fmt.Errorf("more than %d INI files", maxINIFileCount)
	}
	seen[key] = struct{}{}
	defer delete(seen, key)

	raw, err := os.ReadFile(resolved)
	if err != nil {
		return err
	}
	*totalBytes += len(raw)
	if *totalBytes > maxINIBytes {
		return fmt.Errorf("INI input exceeds %d bytes", maxINIBytes)
	}

	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		name, value = strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(value)
		if section == "include" && name == "include_recursive" {
			// Importer configs conventionally point this at the entire Mods tree.
			// Walking user mods is both unbounded and unnecessary for the base
			// [Hunting] controls, but still validate that the configured root does
			// not escape the importer before ignoring it.
			if err := validateINIRootReference(
				root,
				filepath.Join(filepath.Dir(resolved), trimINIQuotes(value)),
			); err != nil {
				return err
			}
			continue
		}
		if section == "include" && name == "include" {
			matches := []string{filepath.Join(filepath.Dir(resolved), trimINIQuotes(value))}
			if strings.ContainsAny(value, "*?") {
				matches, err = filepath.Glob(matches[0])
				if err != nil {
					return err
				}
				slices.Sort(matches)
			}
			for _, include := range matches {
				if err := readINI(root, include, seen, fileCount, totalBytes, hunting); err != nil {
					return err
				}
			}
			continue
		}
		if section == "hunting" {
			hunting[name] = value
		}
	}
	return scanner.Err()
}

func secureINIPath(root, path string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("include escapes importer root: %s", path)
	}
	return filepath.Clean(path), nil
}

func validateINIRootReference(root, path string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("include escapes importer root: %s", path)
	}
	return nil
}

func trimINIQuotes(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"'")
}

func containsWord(value, wanted string) bool {
	for _, word := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	}) {
		if strings.EqualFold(word, wanted) {
			return true
		}
	}
	return false
}

func parseBinding(raw string) (binding, error) {
	positive := make([]string, 0, 4)
	forbidden := make([]string, 0, 8)
	for _, original := range strings.Fields(raw) {
		token := strings.ToLower(original)
		switch {
		case token == "no_modifiers":
			forbidden = append(forbidden, "ctrl", "alt", "shift", "vk_lwin", "vk_rwin")
		case strings.HasPrefix(token, "no_"):
			forbidden = append(forbidden, strings.TrimPrefix(token, "no_"))
		case strings.HasPrefix(token, "xb_"):
			return binding{}, fmt.Errorf("gamepad binding %q cannot be injected", original)
		default:
			positive = append(positive, token)
		}
	}
	if len(positive) == 0 {
		return binding{}, errors.New("binding names no key")
	}
	slices.Sort(forbidden)
	forbidden = slices.Compact(forbidden)
	return binding{chord: strings.Join(positive, " "), forbidden: forbidden}, nil
}

func categories(config huntingConfig) []Category {
	available := make([]Category, 0, len(config.categories))
	for category := range config.categories {
		available = append(available, category)
	}
	slices.Sort(available)
	return available
}

func validateConfigBindings(input WindowInput, config huntingConfig) error {
	bindings := []binding{config.toggle}
	for _, pair := range config.categories {
		bindings = append(bindings, pair.next, pair.mark)
	}
	for _, value := range bindings {
		if err := input.ValidateKeys([]string{value.chord}, value.forbidden); err != nil {
			return fmt.Errorf("%w: binding %q: %w", ErrConfigUnsupported, value.chord, err)
		}
	}
	return nil
}
