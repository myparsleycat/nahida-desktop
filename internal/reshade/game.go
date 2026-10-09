package reshade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// PrepareLaunch readies ReShade for one importer's game and returns the module to inject before the
// game creates its graphics device.
//
//wails:ignore
func (r *ReShade) PrepareLaunch(ctx context.Context, importer string) (string, error) {
	module, err := r.prepareLaunch(ctx, importer)
	if err != nil {
		return "", infra.AnnotateError(err, infra.Diagnostic{
			Operation: "reshade", Stage: "prepare-launch", Fields: map[string]any{"importer": importer},
		})
	}
	return module, nil
}

func (r *ReShade) prepareLaunch(ctx context.Context, importer string) (string, error) {
	layout, err := r.layout(ctx)
	if err != nil {
		return "", err
	}
	game, err := layout.game(importer)
	if err != nil {
		return "", err
	}
	cache, err := r.launchCache(ctx)
	if err != nil {
		return "", err
	}
	if err := layout.ensureShared(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(game, 0o700); err != nil {
		return "", err
	}

	module := filepath.Join(game, gameModuleName)
	r.binaryMu.Lock()
	err = syncModule(filepath.Join(cache, moduleName), module)
	r.binaryMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("copy ReShade module: %w", err)
	}
	if err := writeDefaultConfig(filepath.Join(game, "ReShade.ini")); err != nil {
		return "", fmt.Errorf("write ReShade.ini: %w", err)
	}
	return module, nil
}

// launchCache returns the cache folder to launch from. A launch keeps working offline: when the
// followed version cannot be downloaded, the newest installed one is used instead.
func (r *ReShade) launchCache(ctx context.Context) (string, error) {
	pinned, err := r.pinnedVersion(ctx)
	if err != nil {
		return "", err
	}
	version, err := r.targetVersion(ctx, false)
	if err == nil {
		var cache string
		if cache, err = r.ensureVersion(ctx, version); err == nil {
			return cache, nil
		}
	}
	if infra.IsCancellationError(err) {
		return "", err
	}
	if pinned == "" {
		if installed, listErr := r.installedVersions(); listErr == nil && len(installed) > 0 {
			root, rootErr := r.cacheRoot()
			if rootErr != nil {
				return "", rootErr
			}
			return filepath.Join(root, installed[0]), nil
		}
	}
	return "", fmt.Errorf("RESHADE_NOT_INSTALLED: %w", err)
}

// syncModule copies the cached module beside the game's settings unless that copy is already current.
func syncModule(source, destination string) error {
	want, err := fileSHA256(source)
	if err != nil {
		return err
	}
	if have, err := fileSHA256(destination); err == nil && strings.EqualFold(have, want) {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	temporary, err := os.CreateTemp(filepath.Dir(destination), gameModuleName+".tmp-")
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(temporary, input)
	if err := errors.Join(copyErr, temporary.Close()); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	if err := platform.ReplaceAtomic(temporary.Name(), destination); err != nil {
		_ = os.Remove(temporary.Name())
		return err
	}
	return nil
}

// writeDefaultConfig creates the game's ReShade.ini once. ReShade rewrites the file itself, so an
// existing one is the user's and is left alone.
func writeDefaultConfig(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(defaultConfig())
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func defaultConfig() string {
	// The game's folder sits beside the shared one, and ReShade resolves relative paths against the
	// module's folder, so the file stays valid when the user moves the data folder.
	shared := `..\` + effectsDirName + `\`
	lines := []string{
		"[ADDON]",
		"AddonPath=" + shared + "Addons",
		"",
		"[GENERAL]",
		"EffectSearchPaths=" + shared + `Shaders\**`,
		"PreprocessorDefinitions=RESHADE_DEPTH_INPUT_IS_UPSIDE_DOWN=1,RESHADE_DEPTH_INPUT_IS_REVERSED=1",
		`PresetPath=.\ReShadePreset.ini`,
		"TextureSearchPaths=" + shared + `Textures\**`,
		"",
		"[INPUT]",
		"KeyOverlay=36,0,0,0",
		"",
		"[OVERLAY]",
		"TutorialProgress=4",
		"",
	}
	return strings.Join(lines, "\r\n")
}
