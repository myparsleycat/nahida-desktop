package menumaker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const sidecarMarker = "; NAHIDA MENU SIDECAR SOURCE="

var ErrNoVisibility = errors.New("MENU_MAKER_NO_VISIBILITY")

func generateSidecar(
	sourcePath, text string,
	slots []MenuMakerSlot,
	settings MenuMakerSettings,
) (MenuMakerGenerateResult, error) {
	if strings.EqualFold(filepath.Base(sourcePath), "menu.ini") || strings.Contains(text, sidecarMarker) {
		return MenuMakerGenerateResult{}, errors.New("select the original mod INI, not menu.ini")
	}
	runtimePath := strings.TrimSuffix(sourcePath, filepath.Ext(sourcePath)) + ".ini"
	namespace, err := sourceNamespace(runtimePath, text)
	if err != nil {
		return MenuMakerGenerateResult{}, err
	}
	generated := generatePreview(text, slots, settings)
	if strings.Contains(generated.INIText, "condition = 0\n") {
		return MenuMakerGenerateResult{}, ErrNoVisibility
	}
	owned := map[string]bool{}
	for _, section := range parseSections(generated.INIText) {
		if section.Name == nil {
			continue
		}
		if !strings.EqualFold(*section.Name, "constants") && !strings.EqualFold(*section.Name, "present") {
			owned[strings.ToLower(*section.Name)] = true
		}
		for _, line := range section.Lines {
			if globalPrefixRe.MatchString(line) {
				if variable := variablePrefixRe.FindString(
					strings.TrimSpace(globalPrefixRe.ReplaceAllString(line, "")),
				); variable != "" {
					owned[strings.ToLower(variable)] = true
				}
			}
		}
	}
	for _, section := range parseSections(generated.SourceINIText) {
		if section.Name != nil && owned[strings.ToLower(*section.Name)] {
			return MenuMakerGenerateResult{}, fmt.Errorf("menu section conflicts with original: %s", *section.Name)
		}
		for _, line := range section.Lines {
			if globalPrefixRe.MatchString(line) {
				variable := variablePrefixRe.FindString(strings.TrimSpace(globalPrefixRe.ReplaceAllString(line, "")))
				if owned[strings.ToLower(variable)] {
					return MenuMakerGenerateResult{}, fmt.Errorf("menu variable conflicts with original: %s", variable)
				}
			}
		}
	}
	// Sharing the original namespace preserves local variables, run targets and
	// raw handler expressions without copying the mod's resources or command lists.
	generated.INIText = sidecarMarker + filepath.Base(
		runtimePath,
	) + "\nnamespace = " + namespace + "\n\n" + generated.INIText
	return generated, nil
}

// Exported INIs may be installed at a different path. Pin the source namespace
// as well so its variables and command lists remain shared with the menu.
func generateExportSidecar(
	sourcePath, text string,
	slots []MenuMakerSlot,
	settings MenuMakerSettings,
) (MenuMakerGenerateResult, error) {
	generated, err := generateSidecar(sourcePath, text, slots, settings)
	if err != nil {
		return generated, err
	}
	if _, err := sourceNamespace("", generated.SourceINIText); err != nil {
		namespace, err := sourceNamespace("", generated.INIText)
		if err != nil {
			return MenuMakerGenerateResult{}, err
		}
		generated.SourceINIText = "namespace = " + namespace + "\n\n" + generated.SourceINIText
	}
	return generated, nil
}

func sourceNamespace(sourcePath, text string) (string, error) {
	for _, section := range parseSections(text) {
		if section.Name != nil {
			break
		}
		for _, line := range section.Lines {
			key, value, found := strings.Cut(stripComment(line), "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "namespace") && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value), nil
			}
		}
	}
	if !filepath.IsAbs(sourcePath) {
		return "", errors.New("original INI path is required to resolve its namespace")
	}
	for root := filepath.Dir(sourcePath); ; root = filepath.Dir(root) {
		if info, err := os.Stat(filepath.Join(root, "d3dx.ini")); err == nil && !info.IsDir() {
			relative, err := filepath.Rel(root, sourcePath)
			if err != nil {
				return "", err
			}
			return strings.ReplaceAll(relative, "/", "\\"), nil
		}
		if filepath.Dir(root) == root {
			return "", fmt.Errorf("cannot resolve original INI namespace: d3dx.ini not found above %s", sourcePath)
		}
	}
}
