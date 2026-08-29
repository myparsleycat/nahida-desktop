//go:build windows

package xxmi

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const genshinGeneralDataValue = "GENERAL_DATA_h2389025596"

var genshinRegistryKeys = []string{
	`SOFTWARE\miHoYo\Genshin Impact`,
	`SOFTWARE\miHoYo\原神`,
}

func readGenshinRegistryGeneralData() ([]byte, error) {
	key, err := openGenshinSettingsKey(registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer func() { _ = key.Close() }()
	raw, valueType, err := key.GetBinaryValue(genshinGeneralDataValue)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, errors.New("graphics settings record is not found in genshin impact registry")
		}
		if errors.Is(err, registry.ErrUnexpectedType) {
			return nil, fmt.Errorf("unknown settings format: data type %d is not REG_BINARY", valueType)
		}
		return nil, fmt.Errorf("read Genshin Impact graphics settings: %w", err)
	}
	return raw, nil
}

func writeGenshinRegistryGeneralData(raw []byte) error {
	key, err := openGenshinSettingsKey(registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()
	if err := key.SetBinaryValue(genshinGeneralDataValue, raw); err != nil {
		return fmt.Errorf("write Genshin Impact graphics settings: %w", err)
	}
	return nil
}

func openGenshinSettingsKey(access uint32) (registry.Key, error) {
	var lastErr error
	for _, path := range genshinRegistryKeys {
		key, err := registry.OpenKey(registry.CURRENT_USER, path, access)
		if err == nil {
			return key, nil
		}
		lastErr = err
	}
	if lastErr != nil && errors.Is(lastErr, registry.ErrNotExist) {
		return 0, errors.New("genshin impact registry key is not found")
	}
	if lastErr != nil {
		return 0, fmt.Errorf("open Genshin Impact registry key: %w", lastErr)
	}
	return 0, errors.New("genshin impact registry key is not found")
}
