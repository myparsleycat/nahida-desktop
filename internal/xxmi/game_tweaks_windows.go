//go:build windows

package xxmi

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

func initializeGameLaunch(ctx context.Context, key string, cfg ImporterConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cfg.ConfigureGame {
		return nil
	}
	switch key {
	case "GIMI":
		if cfg.GIMI != nil && cfg.GIMI.EnableHDR {
			key, err := openGenshinSettingsKey(registry.SET_VALUE)
			if err != nil {
				return err
			}
			defer func() { _ = key.Close() }()
			return key.SetDWordValue("WINDOWS_HDR_ON_h3132281285", 1)
		}
	case "SRMI":
		if cfg.SRMI != nil && cfg.SRMI.UnlockFPS {
			return editRegistryJSON(registry.CURRENT_USER, []string{`Software\Cognosphere\Star Rail`},
				"GraphicsSettings_Model_h2986158309", func(value map[string]any) error {
					if _, ok := value["FPS"]; !ok {
						return errors.New("star rail graphics settings are missing FPS")
					}
					value["FPS"] = 120
					return nil
				})
		}
	case "HIMI":
		if cfg.HIMI != nil && cfg.HIMI.UnlockFPS {
			fps := cfg.HIMI.UnlockFPSValue
			if fps < 30 || fps > 1000 {
				return fmt.Errorf("invalid Honkai Impact FPS target %d", fps)
			}
			return editRegistryJSON(registry.CURRENT_USER, []string{`Software\miHoYo\Honkai Impact 3rd`},
				"GENERAL_DATA_V2_PersonalGraphicsSettingV2_h3480068519", func(value map[string]any) error {
					if _, ok := value["TargetFrameRateForInLevel"]; !ok {
						return errors.New("honkai impact graphics settings are missing TargetFrameRateForInLevel")
					}
					value["TargetFrameRateForInLevel"] = fps
					value["TargetFrameRateForOthers"] = fps
					return nil
				})
		}
	}
	return nil
}
