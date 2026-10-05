//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func configureZZMIGame(ctx context.Context, gameFolder string, publish *gameFilePublisher) error {
	folder := filepath.Join(gameFolder, "ZenlessZoneZero_Data", "Persistent", "LocalStorage")
	owns := func(name string) bool { return strings.EqualFold(name, "GENERAL_DATA.bin") }
	return editGameFolder(ctx, folder, owns, publish, func(folder string) error {
		return editZZMISettings(ctx, folder)
	})
}

func editZZMISettings(ctx context.Context, folder string) error {
	root, err := ensureInstallRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	data, info, err := root.readFile("GENERAL_DATA.bin")
	var settings sleepyJSONValue
	if errors.Is(err, os.ErrNotExist) {
		settings = sleepyJSONValue{kind: '{', fields: []sleepyJSONField{
			{key: "$Type", value: sleepyJSONValue{scalar: "MoleMole.GeneralLocalDataItem"}},
			{key: "userLocalDataVersionId", value: sleepyJSONValue{scalar: "0.0.1"}},
		}}
	} else if err != nil {
		return err
	} else {
		if len(data) > 16<<20 {
			return errors.New("ZZMI GENERAL_DATA.bin exceeds size limit")
		}
		decoded, err := decodeSleepy(data, zzmiSleepyMagic)
		if err != nil {
			return err
		}
		settings, err = parseSleepyJSON(decoded)
		if err != nil {
			return err
		}
	}
	if settings.kind != '{' {
		return errors.New("ZZMI GENERAL_DATA.bin must contain a JSON object")
	}
	system := settings.field("SystemSettingDataMap")
	if system == nil {
		settings.setField("SystemSettingDataMap", sleepyJSONValue{kind: '{'})
		system = settings.field("SystemSettingDataMap")
	}
	if system.kind != '{' {
		return errors.New("ZZMI SystemSettingDataMap must be a JSON object")
	}
	changed := false
	for _, setting := range []struct {
		id    string
		value int
	}{{"3", 3}, {"13162", 0}, {"99", 1}} {
		entry := system.field(setting.id)
		if entry == nil {
			system.setField(setting.id, sleepyJSONValue{kind: '{', fields: []sleepyJSONField{
				{key: "$Type", value: sleepyJSONValue{scalar: "MoleMole.SystemSettingLocalData"}},
				{key: "Version", value: sleepyJSONValue{scalar: 0}},
				{key: "Data", value: sleepyJSONValue{scalar: setting.value}},
			}})
			changed = true
			continue
		}
		if entry.kind != '{' {
			return fmt.Errorf("ZZMI system setting %s has unknown format", setting.id)
		}
		value := entry.field("Data")
		if value == nil {
			return fmt.Errorf("ZZMI system setting %s has no Data field", setting.id)
		}
		if value.scalar != json.Number(fmt.Sprint(setting.value)) {
			value.scalar = setting.value
			changed = true
		}
	}
	if !changed {
		return nil
	}
	content, err := serializeSleepyJSON(settings)
	if err != nil {
		return err
	}
	encoded, err := encodeSleepy(content, zzmiSleepyMagic)
	if err != nil {
		return err
	}
	return root.writeFileAtomic(ctx, "GENERAL_DATA.bin", bytes.NewReader(encoded), 0o600, info)
}
