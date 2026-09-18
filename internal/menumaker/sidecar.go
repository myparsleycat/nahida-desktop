package menumaker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const sidecarMarker = "; NAHIDA MENU SIDECAR SOURCE="

var ErrNoVisibility = errors.New("MENU_MAKER_NO_VISIBILITY")

func generateSingleINI(
	text string,
	slots []MenuMakerSlot,
	settings MenuMakerSettings,
) (MenuMakerGenerateResult, error) {
	generated := generatePreview(text, slots, settings)
	if !generatedMenuHasVisibility(generated.INIText) {
		return MenuMakerGenerateResult{}, ErrNoVisibility
	}
	return generated, nil
}

func generatedMenuHasVisibility(text string) bool {
	start := strings.LastIndex(text, generatedBegin)
	end := strings.LastIndex(text, generatedEnd)
	if start < 0 || end < start {
		return false
	}
	for _, section := range parseSections(text[start:end]) {
		if section.Name == nil || !strings.EqualFold(*section.Name, "KeyGuiMenu") {
			continue
		}
		for _, line := range section.Lines {
			key, value, found := strings.Cut(stripComment(line), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "condition") {
				return strings.TrimSpace(value) != "0"
			}
		}
	}
	return false
}

func isOwnedSidecar(text, outputPath, sourceText string) bool {
	line, _, _ := strings.Cut(text, "\n")
	marker := strings.TrimSuffix(line, "\r")
	if marker == sidecarMarker+filepath.Base(outputPath) {
		return true
	}
	namespace, ok := resolvedSourceNamespace(outputPath, sourceText)
	return ok && marker == sidecarMarker+namespace
}

func resolvedSourceNamespace(sourcePath, text string) (string, bool) {
	for _, section := range parseSections(text) {
		if section.Name != nil {
			break
		}
		for _, line := range section.Lines {
			key, value, found := strings.Cut(stripComment(line), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "namespace") && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value), true
			}
		}
	}
	for root := filepath.Dir(sourcePath); ; root = filepath.Dir(root) {
		if info, err := os.Stat(filepath.Join(root, "d3dx.ini")); err == nil && !info.IsDir() {
			relative, err := filepath.Rel(root, sourcePath)
			if err != nil {
				return "", false
			}
			return strings.ReplaceAll(relative, "/", "\\"), true
		}
		if filepath.Dir(root) == root {
			return "", false
		}
	}
}
