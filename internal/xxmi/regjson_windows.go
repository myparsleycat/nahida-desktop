//go:build windows

package xxmi

import (
	"bytes"
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// editRegistryJSON rewrites a null-terminated JSON record like the reference launcher: compact, ASCII-only,
// in the original key order, and only when the edit changed something.
func editRegistryJSON(
	hive registry.Key,
	subkeys []string,
	valueName string,
	edit func(*sleepyJSONValue) error,
) error {
	var key registry.Key
	var err error
	for _, path := range subkeys {
		key, err = registry.OpenKey(hive, path, registry.QUERY_VALUE|registry.SET_VALUE)
		if err == nil {
			break
		}
	}
	if key == 0 {
		return fmt.Errorf("game registry key is not found: %w", err)
	}
	defer func() { _ = key.Close() }()
	return editRegistryJSONValue(key, valueName, edit)
}

type registryJSONValue interface {
	GetBinaryValue(string) ([]byte, uint32, error)
	SetBinaryValue(string, []byte) error
}

func editRegistryJSONValue(key registryJSONValue, valueName string, edit func(*sleepyJSONValue) error) error {
	raw, valueType, err := key.GetBinaryValue(valueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("graphics settings record %s is not found: %w", valueName, err)
		}
		if errors.Is(err, registry.ErrUnexpectedType) {
			return fmt.Errorf("graphics settings record %s is not REG_BINARY (type %d)", valueName, valueType)
		}
		return err
	}
	value, err := parseRegistryJSON(raw)
	if err != nil {
		return err
	}
	before, err := serializeRegistryJSON(value)
	if err != nil {
		return err
	}
	if err := edit(&value); err != nil {
		return err
	}
	after, err := serializeRegistryJSON(value)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	return key.SetBinaryValue(valueName, after)
}

func serializeRegistryJSON(value sleepyJSONValue) ([]byte, error) {
	data, err := compactSleepyJSON(value)
	if err != nil {
		return nil, err
	}
	return append(data, 0), nil
}

func parseRegistryJSON(raw []byte) (sleepyJSONValue, error) {
	value, err := parseSleepyJSON(stripNullTerminator(raw))
	if err != nil {
		return sleepyJSONValue{}, fmt.Errorf("invalid graphics settings JSON: %w", err)
	}
	if value.kind != '{' {
		return sleepyJSONValue{}, errors.New("graphics settings JSON is not an object")
	}
	return value, nil
}
