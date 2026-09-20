package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"nahida.live/desktop/internal/setting"
)

// registerSettingsActions registers the allowlisted application setting actions.
func registerSettingsActions(registry *Registry, deps Dependencies) {
	if deps.Settings == nil {
		return
	}
	registry.add(
		simpleAction("settings.get", "Read an allowlisted local application setting.", "settings", RiskRead,
			objectSchema(map[string]any{"key": enumSchema(allowedAgentSettingKeys()...)}, "key"),
			func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Key string `json:"key"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				if !isAllowedAgentSetting(input.Key) {
					return nil, fmt.Errorf("setting %q is not available to the agent", input.Key)
				}
				value, err := deps.Settings.Get(ctx, input.Key)
				return map[string]any{"key": input.Key, "value": value}, err
			}),
	)
	settingAction := simpleAction(
		"settings.set",
		"Change an allowlisted local application setting.",
		"settings",
		RiskWrite,
		objectSchema(
			map[string]any{"key": enumSchema(allowedAgentSettingKeys()...), "value": map[string]any{}},
			"key",
			"value",
		),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			if !isAllowedAgentSetting(input.Key) {
				return nil, fmt.Errorf("setting %q is not available to the agent", input.Key)
			}
			if err := deps.Settings.Set(ctx, input.Key, input.Value); err != nil {
				return nil, err
			}
			return map[string]any{"key": input.Key, "value": input.Value}, nil
		},
	)
	settingAction.risk = func(raw json.RawMessage) Risk {
		var input struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(raw, &input) == nil && agentConfirmSettingKeys[input.Key] {
			return RiskConfirm
		}
		return RiskWrite
	}
	settingAction.validate = func(raw json.RawMessage) error {
		var input struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := decodeActionArguments(raw, &input); err != nil {
			return err
		}
		return validateAgentSettingValue(input.Key, input.Value)
	}
	registry.add(settingAction)
}

// Agent setting allowlist. Only these keys are readable and writable through the settings actions,
// so the model never reaches local configuration outside the list.
var agentSettingKeys = map[string]bool{
	setting.KeyGeneralLanguage: true, setting.KeyGeneralRunOnStartup: true, setting.KeyGeneralRunInBackground: true,
	setting.KeyGeneralDefaultStartPage: true, setting.KeyGeneralLogLevel: true,
	setting.KeyGeneralMoveTransferPageWhenStartTransfer: true, setting.KeyGeneralPowerSaveBlockInTransfer: true,
	setting.KeyGeneralBisectPreserveD3dx: true, setting.KeyModArchiveExtractPathMode: true,
	setting.KeyModDeleteArchiveAfterExtract: true, setting.KeyModMoveFolderInsteadOfCopy: true,
	setting.KeyModSearchModPreview: true, setting.KeyModCopyShaderFixesOnEnable: true,
	setting.KeyModSidebarLayout: true, setting.KeyModCharacterSidebarWidth: true, setting.KeyModGridLayoutMode: true,
	setting.KeyModGridModelPreview: true, setting.KeyModGridResponsiveBaseWidth: true,
	setting.KeyModGridFixedCardWidth: true, setting.KeyModGridFixedColumnCount: true,
	setting.KeyModDisabledPrefixStyle: true, setting.KeyModAutoInspectFix: true,
	setting.KeyTransferDownloadConcurrency: true, setting.KeyTransferDownloadBandwidthLimitMibps: true,
	setting.KeyTransferUploadConcurrency: true, setting.KeyModelViewerToneMapping: true,
	setting.KeyModelViewerEnvironment: true, setting.KeyModelViewerExposure: true,
	setting.KeyModelViewerToonShadows: true, setting.KeyXXMIPersistToggles: true,
}

var agentConfirmSettingKeys = map[string]bool{
	setting.KeyGeneralRunOnStartup:    true,
	setting.KeyModelViewerEnvironment: true,
}

func allowedAgentSettingKeys() []string {
	keys := make([]string, 0, len(agentSettingKeys))
	for key := range agentSettingKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isAllowedAgentSetting(key string) bool { return agentSettingKeys[key] }

func validateAgentSettingValue(key string, value any) error {
	if !isAllowedAgentSetting(key) {
		return fmt.Errorf("setting %q is not available to the agent", key)
	}
	if agentBooleanSettingKeys[key] {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("setting %q requires a boolean value", key)
		}
		return nil
	}
	if agentIntegerSettingKeys[key] {
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
			return fmt.Errorf("setting %q requires an integer value", key)
		}
		return nil
	}
	if key == setting.KeyModelViewerExposure {
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("setting %q requires a finite number", key)
		}
		return nil
	}
	if stringValue, ok := value.(string); !ok || strings.TrimSpace(stringValue) == "" {
		return fmt.Errorf("setting %q requires a non-empty string value", key)
	}
	return nil
}

var agentBooleanSettingKeys = map[string]bool{
	setting.KeyGeneralRunOnStartup: true, setting.KeyGeneralRunInBackground: true,
	setting.KeyGeneralMoveTransferPageWhenStartTransfer: true,
	setting.KeyGeneralPowerSaveBlockInTransfer:          true,
	setting.KeyGeneralBisectPreserveD3dx:                true,
	setting.KeyModDeleteArchiveAfterExtract:             true,
	setting.KeyModMoveFolderInsteadOfCopy:               true,
	setting.KeyModSearchModPreview:                      true,
	setting.KeyModCopyShaderFixesOnEnable:               true,
	setting.KeyModGridModelPreview:                      true,
	setting.KeyModAutoInspectFix:                        true,
	setting.KeyModelViewerToonShadows:                   true,
	setting.KeyXXMIPersistToggles:                       true,
}

var agentIntegerSettingKeys = map[string]bool{
	setting.KeyModCharacterSidebarWidth: true, setting.KeyModGridResponsiveBaseWidth: true,
	setting.KeyModGridFixedCardWidth: true, setting.KeyModGridFixedColumnCount: true,
	setting.KeyTransferDownloadConcurrency: true, setting.KeyTransferDownloadBandwidthLimitMibps: true,
	setting.KeyTransferUploadConcurrency: true,
}
