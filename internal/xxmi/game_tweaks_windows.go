//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

func initializeGameLaunch(ctx context.Context, key string, cfg ImporterConfig, migotoDLLUsed bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "WWMI" && cfg.WWMI != nil {
		if err := configureWWMIGame(ctx, cfg, migotoDLLUsed); err != nil {
			return fmt.Errorf("WWMI_GAME_CONFIG_FAILED: %w", err)
		}
		return nil
	}
	switch key {
	case "GIMI":
		if cfg.GIMI != nil && cfg.GIMI.EnableHDR {
			key, err := openGenshinSettingsKey(registry.SET_VALUE)
			if err != nil {
				return fmt.Errorf("GIMI_HDR_CONFIG_FAILED: %w", err)
			}
			defer func() { _ = key.Close() }()
			if err := key.SetDWordValue("WINDOWS_HDR_ON_h3132281285", 1); err != nil {
				return fmt.Errorf("GIMI_HDR_CONFIG_FAILED: %w", err)
			}
			return nil
		}
	case "SRMI":
		if cfg.SRMI != nil && cfg.SRMI.UnlockFPS {
			err := editRegistryJSON(registry.CURRENT_USER, []string{`Software\Cognosphere\Star Rail`},
				"GraphicsSettings_Model_h2986158309", func(value map[string]any) error {
					if _, ok := value["FPS"]; !ok {
						return errors.New("star rail graphics settings are missing FPS")
					}
					value["FPS"] = 120
					return nil
				})
			if err != nil {
				return fmt.Errorf("SRMI_FPS_UNLOCK_FAILED: %w", err)
			}
			return nil
		}
	case "HIMI":
		if cfg.HIMI != nil && cfg.HIMI.UnlockFPS {
			fps := cfg.HIMI.UnlockFPSValue
			if fps < 30 || fps > 1000 {
				return fmt.Errorf("HIMI_FPS_UNLOCK_FAILED: invalid FPS target %d", fps)
			}
			err := editRegistryJSON(registry.CURRENT_USER, []string{`Software\miHoYo\Honkai Impact 3rd`},
				"GENERAL_DATA_V2_PersonalGraphicsSettingV2_h3480068519", func(value map[string]any) error {
					if _, ok := value["TargetFrameRateForInLevel"]; !ok {
						return errors.New("honkai impact graphics settings are missing TargetFrameRateForInLevel")
					}
					value["TargetFrameRateForInLevel"] = fps
					value["TargetFrameRateForOthers"] = fps
					return nil
				})
			if err != nil {
				return fmt.Errorf("HIMI_FPS_UNLOCK_FAILED: %w", err)
			}
			return nil
		}
	case "ZZMI":
		if cfg.ConfigureGame && migotoDLLUsed {
			if err := configureZZMIGame(ctx, cfg.GameFolder); err != nil {
				return fmt.Errorf("ZZMI_GAME_CONFIG_FAILED: %w", err)
			}
		}
	}
	return nil
}
