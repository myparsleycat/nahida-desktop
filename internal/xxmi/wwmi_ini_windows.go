//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// wwmiRetiredEngineOptions are performance tweaks earlier versions wrote to Engine.ini.
// XXMI Launcher 2.3 dropped them, so they are removed once from a configuration that still has them.
var wwmiRetiredEngineOptions = []string{
	"r.Streaming.HLODStrategy", "r.Streaming.PoolSizeForMeshes", "r.XGEShaderCompile",
	"FX.BatchAsync", "FX.EarlyScheduleAsync", "fx.Niagara.ForceAutoPooling",
	"wp.Runtime.KuroRuntimeStreamingRangeOverallScale",
	"tick.AllowAsyncTickCleanup", "tick.AllowAsyncTickDispatch",
}

// configureWWMIINIFiles edits the game's INI files. The engine files only matter to mods, so they
// are left alone when the XXMI DLL is not loaded.
func configureWWMIINIFiles(
	ctx context.Context,
	game string,
	options WWMIOptions,
	migotoDLLUsed bool,
	publish *gameFilePublisher,
) error {
	if options.UnlockFPS {
		if err := editWWMIINI(ctx,
			filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "GameUserSettings.ini"), publish,
			func(doc *iniDocument) {
				doc.SetOptionUnique("/Script/Engine.GameUserSettings", "FrameRateLimit", "120.000000", false)
			}); err != nil {
			return fmt.Errorf("edit WWMI GameUserSettings.ini: %w", err)
		}
	}
	if !migotoDLLUsed {
		return nil
	}

	if err := editWWMIINI(ctx,
		filepath.Join(game, "Client", "Saved", "Config", "WindowsNoEditor", "Engine.ini"), publish,
		func(doc *iniDocument) {
			doc.RemoveOption("ConsoleVariables", "r.Kuro.SkeletalMesh.DistanceLODBaseFOV")
			if !options.RetiredEngineOptionsPending {
				return
			}
			for _, key := range wwmiRetiredEngineOptions {
				doc.RemoveOption("SystemSettings", key)
			}
		}); err != nil {
		return fmt.Errorf("edit WWMI Engine.ini: %w", err)
	}
	if err := editWWMIINI(ctx, filepath.Join(game, "Client", "Config", "UserEngine.ini"), publish,
		func(doc *iniDocument) {
			doc.SetOptionUnique(
				"ConsoleVariables",
				"r.Kuro.SkeletalMesh.DistanceLODBaseFOV",
				strconv.Itoa(options.MeshLODDistanceBaseFOV),
				false,
			)
		}); err != nil {
		return fmt.Errorf("edit WWMI UserEngine.ini: %w", err)
	}
	return nil
}

func editWWMIINI(ctx context.Context, path string, publish *gameFilePublisher, edit func(*iniDocument)) error {
	name := filepath.Base(path)
	owns := func(candidate string) bool { return strings.EqualFold(candidate, name) }
	return editGameFolder(ctx, filepath.Dir(path), owns, publish, func(folder string) error {
		return editINIFile(ctx, folder, name, edit)
	})
}

func editINIFile(ctx context.Context, folder, name string, edit func(*iniDocument)) error {
	root, err := ensureInstallRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	data, info, err := root.readFile(name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > 8<<20 {
		return errors.New("WWMI INI file exceeds size limit")
	}
	doc := parseINI(data)
	edit(doc)
	if !doc.Changed() {
		return nil
	}
	return root.writeFileAtomic(ctx, name, bytes.NewReader(doc.Bytes()), 0o600, info)
}
