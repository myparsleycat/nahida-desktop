//go:build windows

package xxmi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

func editRegistryJSON(hive registry.Key, subkeys []string, valueName string, edit func(map[string]any) error) error {
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
	before, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := edit(value); err != nil {
		return err
	}
	after, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		return nil
	}
	return key.SetBinaryValue(valueName, append(after, 0))
}

func parseRegistryJSON(raw []byte) (map[string]any, error) {
	text := raw
	if index := bytes.IndexByte(raw, 0); index >= 0 {
		text = raw[:index]
	}
	if !json.Valid(text) {
		return nil, errors.New("invalid graphics settings JSON")
	}
	var value map[string]any
	if err := json.Unmarshal(text, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("graphics settings JSON is not an object")
	}
	return value, nil
}
