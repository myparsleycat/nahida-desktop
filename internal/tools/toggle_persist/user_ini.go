package togglepersist

import (
	"errors"
	"os"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// updateUserINI sets the [Constants] entries d3dx_user.ini already has for the lowercase keys in values and
// reports how many lines changed. It never adds an entry: without one, 3DMigoto keeps the value the mod INI
// declares, which the caller writes first. Every other byte of the file is kept.
func updateUserINI(path string, values map[string]string) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	raw, info, err := readPersistINI(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	content := string(raw)
	lineEnding := "\n"
	if strings.Contains(content, "\r\n") {
		lineEnding = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	inConstants := false
	changed := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if strings.HasPrefix(trimmed, "[") {
			inConstants = strings.EqualFold(trimmed, "[Constants]")
			continue
		}
		if !inConstants || !strings.HasPrefix(trimmed, "$") {
			continue
		}
		keyPart, current, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		next, ok := values[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(keyPart, "\uFEFF")))]
		if !ok || strings.TrimSpace(current) == strings.TrimSpace(next) {
			continue
		}
		lines[index] = strings.TrimRight(keyPart, " \t") + " = " + strings.TrimSpace(next)
		changed++
	}
	if changed == 0 {
		return 0, nil
	}

	if err := platform.WriteFileAtomic(path, []byte(strings.Join(lines, lineEnding)), info.Mode().Perm()); err != nil {
		return 0, infra.AnnotateError(err, infra.Diagnostic{
			Operation: "toggle-preset.apply",
			Stage:     "user-ini",
			Fields:    map[string]any{"userIniPath": path, "entries": changed},
		})
	}
	return changed, nil
}

// lastUserValue returns the value of the last [Constants] line in d3dx_user.ini that sets one of the lowercase
// keys. 3DMigoto runs the section top to bottom, so when a variable is saved under both its declared namespace
// and its path, the later line is the one the game ends up with.
func lastUserValue(content string, keys []string) (string, bool) {
	if content == "" || len(keys) == 0 {
		return "", false
	}
	value, found := "", false
	inConstants := false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if strings.HasPrefix(trimmed, "[") {
			inConstants = strings.EqualFold(trimmed, "[Constants]")
			continue
		}
		if !inConstants || !strings.HasPrefix(trimmed, "$") {
			continue
		}
		key, current, ok := strings.Cut(trimmed, "=")
		if ok && containsFold(keys, strings.TrimSpace(key)) {
			value, found = strings.TrimSpace(current), true
		}
	}
	return value, found
}

func containsFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}
