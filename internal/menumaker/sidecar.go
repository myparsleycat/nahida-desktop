package menumaker

import (
	"errors"
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
	if strings.Contains(generated.INIText, "condition = 0\n") {
		return MenuMakerGenerateResult{}, ErrNoVisibility
	}
	return generated, nil
}

func isOwnedSidecar(text, outputPath string) bool {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSuffix(line, "\r") == sidecarMarker+filepath.Base(outputPath)
}
