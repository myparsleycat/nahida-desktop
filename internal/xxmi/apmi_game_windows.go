package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/platform"
)

// ErrAPMIGameProcessSelectionRequired means APMI needs an explicit client before launch.
var ErrAPMIGameProcessSelectionRequired = errors.New(
	"APMI_GAME_PROCESS_SELECTION_REQUIRED: set the Game process name in APMI settings",
)

// APMIGameProcessNames returns the executables offered when an APMI client must be selected.
func APMIGameProcessNames() []string {
	return slices.Clone(importerPackages["APMI"].gameExeNames)
}

type apmiGameTarget struct {
	processName string
	executable  string
}

func (x *XXMI) resolveAPMIGame(ctx context.Context, cfg ImporterConfig) (apmiGameTarget, error) {
	if err := ctx.Err(); err != nil {
		return apmiGameTarget{}, err
	}
	folder := cfg.GameFolder
	if validateLocalFolder("game folder", folder, true) != nil {
		folder = ""
	}
	// An existing game folder the user configured is not swapped for an installation found in the shell
	// records; the records then only name the client.
	keepFolder := false
	if info, err := os.Stat(folder); folder != "" && err == nil && info.IsDir() {
		keepFolder = true
	}

	if name := strings.TrimSpace(cfg.GameProcessExe); name != "" {
		if !platform.NewFS().IsValidWindowsFilename(name) || !strings.EqualFold(filepath.Ext(name), ".exe") {
			return apmiGameTarget{}, fmt.Errorf("invalid APMI game process executable %q", name)
		}
		target := apmiGameTarget{processName: name, executable: apmiExecutableInFolder(folder, name)}
		if target.executable != "" || keepFolder {
			return target, nil
		}
		registered, err := x.registeredAPMIGame(ctx, []string{name}, "")
		if err != nil {
			return apmiGameTarget{}, err
		}
		target.executable = registered.executable
		return target, nil
	}

	names := importerPackages["APMI"].gameExeNames
	var installed []apmiGameTarget
	for _, name := range names {
		if executable := apmiExecutableInFolder(folder, name); executable != "" {
			installed = append(installed, apmiGameTarget{processName: name, executable: executable})
		}
	}
	if len(installed) == 1 {
		return installed[0], nil
	}

	var selectedFolder string
	if len(installed) > 1 {
		selectedFolder = apmiGameFolderIdentity(folder)
	}
	target, err := x.registeredAPMIGame(ctx, names, selectedFolder)
	if err != nil {
		return apmiGameTarget{}, err
	}
	switch {
	case target.processName == "":
		return apmiGameTarget{}, ErrAPMIGameProcessSelectionRequired
	case selectedFolder != "":
		target.executable = filepath.Join(folder, target.processName)
	case keepFolder:
		target.executable = ""
	}
	return target, nil
}

// registeredAPMIGame picks the client that the shell records of existing executables name, limited to folder
// when it is set. It fails when the records name different clients and leaves the executable empty when one
// client is recorded in several folders.
func (x *XXMI) registeredAPMIGame(ctx context.Context, names []string, folder string) (apmiGameTarget, error) {
	registered, err := x.registryGameExecutables(ctx, names)
	if err != nil {
		return apmiGameTarget{}, err
	}
	var target apmiGameTarget
	var targetFolder string
	for _, executable := range registered {
		if err := ctx.Err(); err != nil {
			return apmiGameTarget{}, err
		}
		index := slices.IndexFunc(
			names,
			func(name string) bool { return strings.EqualFold(name, filepath.Base(executable)) },
		)
		if index < 0 || validateLocalFolder("game executable", executable, true) != nil || !fileExists(executable) {
			continue
		}
		parent := apmiGameFolderIdentity(filepath.Dir(executable))
		if folder != "" && !platform.SamePathFold(folder, parent) {
			continue
		}
		name := names[index]
		switch {
		case target.processName == "":
			target, targetFolder = apmiGameTarget{processName: name, executable: executable}, parent
		case target.processName != name:
			return apmiGameTarget{}, ErrAPMIGameProcessSelectionRequired
		case !platform.SamePathFold(targetFolder, parent):
			target.executable = ""
		}
	}
	return target, nil
}

func apmiExecutableInFolder(folder, name string) string {
	if folder != "" {
		executable := filepath.Join(folder, name)
		if fileExists(executable) {
			return executable
		}
	}
	return ""
}

func apmiGameFolderIdentity(folder string) string {
	if physical, err := platform.FinalPath(folder); err == nil {
		return physical
	}
	return folder
}
