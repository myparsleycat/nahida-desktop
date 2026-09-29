//go:build windows

package xxmi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func configureWWMIGame(ctx context.Context, cfg ImporterConfig, migotoDLLUsed bool) error {
	game := cfg.GameFolder
	if !fileExists(filepath.Join(game, "Wuthering Waves.exe")) {
		return fmt.Errorf("WWMI game folder is missing Wuthering Waves.exe")
	}
	for _, folder := range []string{"Client", "Engine"} {
		info, err := os.Stat(filepath.Join(game, folder))
		if err != nil || !info.IsDir() {
			return fmt.Errorf("WWMI game folder is missing %s", folder)
		}
	}
	if cfg.ConfigureGame || cfg.WWMI.UnlockFPS {
		if err := configureWWMILocalStorage(ctx, game, cfg, migotoDLLUsed); err != nil {
			return err
		}
	}
	if err := configureWWMIINIFiles(ctx, game, *cfg.WWMI); err != nil {
		return err
	}
	return nil
}
