package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"nahida.live/desktop/internal/db"
)

const (
	maxPromotedRootsPerMessage        = 16
	maxPromotedRootsPerSession        = 64
	maxPathCandidatesPerMessage       = 64
	maxStoredRootCandidatesPerSession = 256
	maxWindowsPathCandidate           = 4096
	maxWindowsPathVariants            = 32
)

// promotedSandboxRoots finds local absolute Windows paths that the user explicitly included in a
// message. Existing directories become roots directly, existing files expose their parent, and a
// missing final path component exposes its existing parent so the agent can create that target.
// Drive roots are never promoted.
func promotedSandboxRoots(text string) []SandboxRoot {
	roots := make([]SandboxRoot, 0)
	candidates := 0
	for index := 0; index < len(text) && len(roots) < maxPromotedRootsPerMessage; index++ {
		if !isWindowsPathStart(text, index) {
			continue
		}
		candidates++
		if candidates > maxPathCandidatesPerMessage {
			break
		}
		candidate := windowsPathCandidate(text, index)
		rootPath, ok := promotedRootPath(candidate)
		if !ok {
			continue
		}
		name := sandboxRootName(rootPath)
		roots = mergeSandboxRoots(roots, []SandboxRoot{{
			ID: rootID(name, rootPath), Name: name, Path: rootPath,
		}})
	}
	return roots
}

// sandboxRootsFromEvents restores the explicit capabilities recorded on user turns. Rebuilding
// them from the effective event slice makes staged and committed conversation reverts remove the
// corresponding filesystem access as well.
func sandboxRootsFromEvents(events []db.AgentEventRow) []SandboxRoot {
	roots := make([]SandboxRoot, 0)
	seenCandidates := make(map[string]struct{})
	for _, event := range events {
		if event.EventType != "turn/start" {
			continue
		}
		var payload struct {
			SandboxRoots []SandboxRoot `json:"sandboxRoots"`
		}
		if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
			continue
		}
		for _, stored := range payload.SandboxRoots {
			clean := filepath.Clean(stored.Path)
			key := strings.ToLower(clean)
			if _, ok := seenCandidates[key]; ok {
				continue
			}
			if len(seenCandidates) >= maxStoredRootCandidatesPerSession {
				return roots
			}
			seenCandidates[key] = struct{}{}
			if !isLocalAbsoluteWindowsPath(clean) || isWindowsVolumeRoot(clean) || isVolumeRootFile(clean) {
				continue
			}
			canonical, err := canonicalExistingDir(clean)
			if err != nil || isWindowsVolumeRoot(canonical) {
				continue
			}
			if len(roots) >= maxPromotedRootsPerSession {
				return roots
			}
			name := strings.TrimSpace(stored.Name)
			if name == "" {
				name = sandboxRootName(canonical)
			}
			roots = mergeSandboxRoots(roots, []SandboxRoot{{
				ID: rootID(name, canonical), Name: name, Path: canonical,
			}})
		}
	}
	return roots
}

func mergeSandboxRoots(base, extra []SandboxRoot) []SandboxRoot {
	merged := append([]SandboxRoot(nil), base...)
	for _, candidate := range extra {
		covered := false
		for _, existing := range merged {
			if pathWithin(existing.Path, candidate.Path) {
				covered = true
				break
			}
		}
		if !covered {
			merged = append(merged, candidate)
		}
	}
	return merged
}

func isWindowsPathStart(text string, index int) bool {
	if index > 0 && (isASCIIAlphaNumeric(text[index-1]) || isWindowsSeparator(text[index-1])) {
		return false
	}
	if index+2 < len(text) && isASCIIAlpha(text[index]) && text[index+1] == ':' &&
		isWindowsSeparator(text[index+2]) {
		return true
	}
	if index+2 >= len(text) || text[index] != '\\' || text[index+1] != '\\' {
		return false
	}
	// Device namespaces can bypass ordinary Win32 path interpretation and are never promoted.
	return text[index+2] != '?' && text[index+2] != '.' && text[index+2] != '\\'
}

func windowsPathCandidate(text string, start int) string {
	end := len(text)
	if newline := strings.IndexAny(text[start:], "\r\n"); newline >= 0 {
		end = start + newline
	}
	if start > 0 {
		var closer byte
		switch text[start-1] {
		case '`', '\'', '"':
			closer = text[start-1]
		case '<':
			closer = '>'
		case '(':
			closer = ')'
		}
		if closer != 0 {
			if offset := strings.IndexByte(text[start:end], closer); offset >= 0 {
				end = start + offset
			}
		}
	}
	if end-start > maxWindowsPathCandidate {
		end = start + maxWindowsPathCandidate
	}
	return text[start:end]
}

func promotedRootPath(candidate string) (string, bool) {
	variants := windowsPathVariants(candidate)
	for _, variant := range variants {
		if root, ok := existingSandboxRoot(variant); ok {
			return root, true
		}
	}
	for _, variant := range variants {
		clean := filepath.Clean(variant)
		if !isLocalAbsoluteWindowsPath(clean) {
			continue
		}
		// Parent promotion is only for a missing final component. An existing path, including a
		// symlink, was already considered and must not expose its parent after being rejected.
		if _, err := os.Lstat(clean); err == nil {
			continue
		}
		parent := filepath.Dir(clean)
		if parent == clean || isWindowsVolumeRoot(parent) {
			continue
		}
		if root, ok := existingSandboxRoot(parent); ok {
			return root, true
		}
	}
	return "", false
}

func windowsPathVariants(candidate string) []string {
	remaining := strings.TrimSpace(candidate)
	variants := make([]string, 0, maxWindowsPathVariants)
	seen := make(map[string]struct{})
	appendVariant := func(variant string) bool {
		variant = strings.TrimSpace(variant)
		key := strings.ToLower(variant)
		if variant == "" {
			return true
		}
		if _, ok := seen[key]; ok {
			return true
		}
		if len(variants) >= maxWindowsPathVariants {
			return false
		}
		seen[key] = struct{}{}
		variants = append(variants, variant)
		return true
	}
	for remaining != "" {
		if !appendVariant(remaining) || !appendVariant(strings.TrimRightFunc(remaining, func(character rune) bool {
			return unicode.IsPunct(character) || unicode.IsSpace(character)
		})) {
			break
		}
		suffix := remaining
		for range 4 {
			character, size := utf8.DecodeLastRuneInString(suffix)
			if character <= unicode.MaxASCII || character == utf8.RuneError && size == 0 {
				break
			}
			suffix = strings.TrimSpace(suffix[:len(suffix)-size])
			if !appendVariant(suffix) {
				break
			}
		}
		if len(variants) >= maxWindowsPathVariants {
			break
		}
		space := strings.LastIndexAny(remaining, " \t")
		if space < 0 {
			break
		}
		remaining = strings.TrimSpace(remaining[:space])
	}
	return variants
}

func existingSandboxRoot(path string) (string, bool) {
	clean := filepath.Clean(path)
	if !isLocalAbsoluteWindowsPath(clean) || isWindowsVolumeRoot(clean) || isVolumeRootFile(clean) {
		return "", false
	}
	info, err := os.Stat(clean)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		clean = filepath.Dir(clean)
	}
	canonical, err := canonicalExistingDir(clean)
	if err != nil || isWindowsVolumeRoot(canonical) {
		return "", false
	}
	return canonical, true
}

// isWindowsVolumeRoot reports whether path is a drive root such as C:\.
// Promoting one would grant the agent the whole volume.
func isWindowsVolumeRoot(path string) bool {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if volume == "" {
		return false
	}
	return strings.EqualFold(clean, volume+string(filepath.Separator))
}

// isVolumeRootFile reports whether path is an existing file directly on a drive root.
// Its parent must not be promoted as a sandbox root.
func isVolumeRootFile(path string) bool {
	if !isWindowsVolumeRoot(filepath.Dir(path)) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isLocalAbsoluteWindowsPath(path string) bool {
	if strings.HasPrefix(path, `\\`) {
		return false
	}
	return filepath.IsAbs(path) && filepath.VolumeName(path) != ""
}

func sandboxRootName(path string) string {
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) {
		name = filepath.VolumeName(path)
	}
	if strings.TrimSpace(name) == "" {
		return path
	}
	return name
}

func isASCIIAlpha(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func isASCIIAlphaNumeric(character byte) bool {
	return isASCIIAlpha(character) || character >= '0' && character <= '9'
}

func isWindowsSeparator(character byte) bool {
	return character == '\\' || character == '/'
}
