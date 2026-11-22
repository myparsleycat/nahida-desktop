package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
)

func TestImporterFolderMessageShowsDetectedProcesses(t *testing.T) {
	t.Parallel()
	const key = "page.setting.xxmi.fn.installImporterPackage.errors.XXMI_IMPORTER_FOLDER_IN_USE"

	inUse := &xxmi.ImporterFolderInUseError{Err: errors.New("Access is denied")}
	err := fmt.Errorf("install importer:WWMI update: %w", inUse)
	fallback := importerFolderMessage(err)
	if fallback != T(key) || strings.Contains(fallback, "Access is denied") {
		t.Fatalf("no-process message = %q", fallback)
	}

	inUse.Processes = []platform.ProcessInfo{{Name: "Game", PID: 1234}, {PID: 5678}}
	const processes = "Game (1234), PID 5678"
	message := importerFolderMessage(err)
	if message != T(key+"_WITH_PROCESSES", "processes", processes) || !strings.Contains(message, processes) {
		t.Fatalf("message with processes = %q", message)
	}
}
