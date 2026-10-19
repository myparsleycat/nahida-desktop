package modruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	SettingKey    = "key"
	SettingBool   = "bool"
	SettingInt    = "int"
	SettingNumber = "number"
	// SettingColor is "r g b", each channel from 0 to 1. A color has three
	// targets, and target i receives channel i.
	SettingColor = "color"

	settingsFile = ".nhd-runtime-settings.json"
)

var (
	keyTokenRE = regexp.MustCompile(`^[!-~]+$`)
	// decimalRE is the plain notation the importer's INI parser reads;
	// strconv.ParseFloat alone would also take exponents, hex and Inf.
	decimalRE = regexp.MustCompile(`^-?(\d+(\.\d*)?|\.\d+)$`)
)

// Setting is a value the user can change in the runtime's bundled INI files.
// Each target is an assignment inside a section; an install writes the value
// after its "=". Default must match the bundled files, so an install with
// default settings writes them unchanged.
type Setting struct {
	ID      string
	Kind    string
	Default string
	// Min and Max bound an int or number; Step is the renderer's increment.
	Min     float64
	Max     float64
	Step    float64
	Targets []Target
}

// Target is the assignment `Name = value` in [Section] of File, a slash path
// in Spec.Files. Section and Name compare case-insensitively; a section
// that appears several times, such as Constants, is searched in every copy.
type Target struct {
	File    string
	Section string
	Name    string
}

type SettingValue struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Value   string  `json:"value"`
	Default string  `json:"default"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Step    float64 `json:"step"`
}

type storedSettings struct {
	Values map[string]string `json:"values"`
}

// settingValues reads the values stored in root. A missing or invalid value
// falls back to its default, so a hand-edited file cannot break an install.
func settingValues(root string, spec runtimeSpec) (map[string]string, error) {
	values := make(map[string]string, len(spec.Settings))
	var stored storedSettings
	data, err := os.ReadFile(filepath.Join(root, settingsFile))
	switch {
	case err == nil:
		if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &stored); err != nil {
			stored = storedSettings{}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	for _, setting := range spec.Settings {
		value := setting.Default
		if candidate, ok := stored.Values[setting.ID]; ok && setting.valid(candidate) == nil {
			value = candidate
		}
		values[setting.ID] = value
	}
	return values, nil
}

func settingStatus(spec runtimeSpec, values map[string]string) []SettingValue {
	settings := make([]SettingValue, 0, len(spec.Settings))
	for _, setting := range spec.Settings {
		value, ok := values[setting.ID]
		if !ok {
			value = setting.Default
		}
		settings = append(settings, SettingValue{
			ID: setting.ID, Kind: setting.Kind, Value: value, Default: setting.Default,
			Min: setting.Min, Max: setting.Max, Step: setting.Step,
		})
	}
	return settings
}

// normalizeSettings validates the values the renderer sends; a setting it
// leaves out goes back to its default.
func normalizeSettings(spec runtimeSpec, values map[string]string) (map[string]string, error) {
	normalized := make(map[string]string, len(spec.Settings))
	for _, setting := range spec.Settings {
		value, ok := values[setting.ID]
		if !ok {
			normalized[setting.ID] = setting.Default
			continue
		}
		value = strings.Join(strings.Fields(value), " ")
		if err := setting.valid(value); err != nil {
			return nil, fmt.Errorf("%s: %w", setting.ID, err)
		}
		normalized[setting.ID] = value
	}
	return normalized, nil
}

func (s Setting) valid(value string) error {
	switch s.Kind {
	case SettingKey:
		tokens := strings.Split(value, " ")
		if value == "" || len(tokens) > 8 {
			return errors.New("invalid key")
		}
		for _, token := range tokens {
			if !keyTokenRE.MatchString(token) {
				return errors.New("invalid key")
			}
		}
	case SettingBool:
		if value != "0" && value != "1" {
			return errors.New("invalid switch value")
		}
	case SettingInt:
		number, err := strconv.Atoi(value)
		if err != nil || float64(number) < s.Min || float64(number) > s.Max {
			return errors.New("value out of range")
		}
	case SettingNumber:
		if !s.inRange(value) {
			return errors.New("value out of range")
		}
	case SettingColor:
		channels := strings.Split(value, " ")
		unit := Setting{Max: 1}
		if len(channels) != 3 ||
			slices.ContainsFunc(channels, func(channel string) bool { return !unit.inRange(channel) }) {
			return errors.New("invalid color")
		}
	default:
		return fmt.Errorf("unknown setting kind %q", s.Kind)
	}
	return nil
}

func (s Setting) inRange(value string) bool {
	if !decimalRE.MatchString(value) {
		return false
	}
	number, err := strconv.ParseFloat(value, 64)
	return err == nil && number >= s.Min && number <= s.Max
}

// targetValue is what the setting writes into its target at index.
func (s Setting) targetValue(value string, index int) string {
	if s.Kind != SettingColor {
		return value
	}
	return strings.Split(value, " ")[index]
}

// renderSettings writes each target of the settings that apply to file name.
// Every target must be found, so a bundle update that drops one fails the
// install instead of silently ignoring the user's value.
func renderSettings(name string, data []byte, spec runtimeSpec, values map[string]string) ([]byte, error) {
	var targets []Target
	var targetValues []string
	for _, setting := range spec.Settings {
		for index, target := range setting.Targets {
			if target.File == name {
				targets = append(targets, target)
				targetValues = append(targetValues, setting.targetValue(values[setting.ID], index))
			}
		}
	}
	if len(targets) == 0 {
		return data, nil
	}

	lines := bytes.SplitAfter(data, []byte("\n"))
	found := make([]bool, len(targets))
	section := ""
	for index, line := range lines {
		body := strings.TrimRight(string(line), "\r\n")
		trimmed := strings.TrimSpace(body)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			continue
		}
		left, right, ok := strings.Cut(body, "=")
		if !ok {
			continue
		}
		assigned := strings.Join(strings.Fields(left), " ")
		spacing := right[:len(right)-len(strings.TrimLeft(right, " \t"))]
		for target, candidate := range targets {
			if !strings.EqualFold(candidate.Section, section) || !strings.EqualFold(candidate.Name, assigned) {
				continue
			}
			found[target] = true
			lines[index] = []byte(left + "=" + spacing + targetValues[target] + string(line[len(body):]))
		}
	}
	for target, ok := range found {
		if !ok {
			return nil, fmt.Errorf(
				"setting target [%s] %s not found in %s",
				targets[target].Section,
				targets[target].Name,
				name,
			)
		}
	}
	return bytes.Join(lines, nil), nil
}

func writeSettings(write func(string, []byte) error, root string, values map[string]string) error {
	encoded, err := encodeJSON(storedSettings{Values: values})
	if err != nil {
		return err
	}
	return write(filepath.Join(root, settingsFile), encoded)
}
