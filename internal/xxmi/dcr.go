package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	genshinDCRSettingKey     = 21
	genshinDCREnabledValue   = 2
	genshinDCRDisabledValue  = 1
	genshinDCREnabledIndex   = 1
	genshinDCRDisabledIndex  = 0
	genshinDCRItemVersion    = "OSRELWin5.0.0"
	genshinGraphicsDataKey   = "graphicsData"
	genshinGlobalPerfDataKey = "globalPerfData"
	genshinVolatileGradesKey = "customVolatileGrades"
	genshinSaveItemsKey      = "saveItems"
	xxmiCheckDCRWhere        = "XXMI.checkDCR"
	xxmiDisableDCRWhere      = "XXMI.disableDCR"
	gimiImporterKey          = "GIMI"
)

var errGimiDCREnabled = errors.New("GIMI_DCR_ENABLED")

type volatileGrade struct {
	Key   int `json:"key"`
	Value int `json:"value"`
}

type saveItem struct {
	EntryType   int    `json:"entryType"`
	Index       int    `json:"index"`
	ItemVersion string `json:"itemVersion"`
}

type genshinGeneralData struct {
	settings       map[string]json.RawMessage
	graphicsData   map[string]json.RawMessage
	globalPerfData map[string]json.RawMessage
	grades         []volatileGrade
	saveItems      []saveItem
}

func (x *XXMI) DisableGenshinDynamicCharacterResolution(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := readGenshinRegistryGeneralData()
	if err != nil {
		x.logDCRError(fmt.Sprintf("Failed to read Genshin Impact graphics settings: %v", err), xxmiDisableDCRWhere)
		return err
	}
	data, err := parseGenshinGeneralData(raw)
	if err != nil {
		x.logDCRError(fmt.Sprintf("Failed to parse Genshin Impact graphics settings: %v", err), xxmiDisableDCRWhere)
		return err
	}
	if !data.disableDCR() {
		return nil
	}
	encoded, err := data.encode()
	if err != nil {
		x.logDCRError(fmt.Sprintf("Failed to encode Genshin Impact graphics settings: %v", err), xxmiDisableDCRWhere)
		return err
	}
	if x.log != nil {
		x.log.Info("Disabling Genshin Impact Dynamic Character Resolution", xxmiDisableDCRWhere)
	}
	if err := writeGenshinRegistryGeneralData(encoded); err != nil {
		x.logDCRError(fmt.Sprintf("Failed to write Genshin Impact graphics settings: %v", err), xxmiDisableDCRWhere)
		return err
	}
	return nil
}

func (x *XXMI) rejectEnabledGimiDCR(ctx context.Context, importer string) error {
	if !strings.EqualFold(importer, gimiImporterKey) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := readGenshinRegistryGeneralData()
	if err != nil {
		x.logDCRError(fmt.Sprintf("Failed to read Genshin Impact graphics settings: %v", err), xxmiCheckDCRWhere)
		return err
	}
	data, err := parseGenshinGeneralData(raw)
	if err != nil {
		x.logDCRError(fmt.Sprintf("Failed to parse Genshin Impact graphics settings: %v", err), xxmiCheckDCRWhere)
		return err
	}
	if data.dcrEnabled() {
		if x.log != nil {
			x.log.Info(fmt.Sprintf("Rejected StartGame at DCR check for importer %s", importer), xxmiCheckDCRWhere)
		}
		return errGimiDCREnabled
	}
	return nil
}

func (x *XXMI) logDCRError(message, where string) {
	if x != nil && x.log != nil {
		x.log.Error(message, where)
	}
}

func parseGenshinGeneralData(raw []byte) (*genshinGeneralData, error) {
	payload := stripNullTerminator(raw)
	if len(payload) == 0 {
		return nil, errors.New("genshin impact graphics settings are empty")
	}
	if !isASCII(payload) {
		return nil, errors.New("genshin impact graphics settings are not ASCII")
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(payload, &settings); err != nil {
		return nil, fmt.Errorf("genshin impact graphics settings are not JSON: %w", err)
	}
	graphicsData, err := unmarshalNestedObject(settings, genshinGraphicsDataKey)
	if err != nil {
		return nil, err
	}
	grades, err := unmarshalObjectSlice[volatileGrade](graphicsData, genshinGraphicsDataKey, genshinVolatileGradesKey)
	if err != nil {
		return nil, err
	}
	globalPerfData, err := unmarshalNestedObject(settings, genshinGlobalPerfDataKey)
	if err != nil {
		return nil, err
	}
	saveItems, err := unmarshalObjectSlice[saveItem](globalPerfData, genshinGlobalPerfDataKey, genshinSaveItemsKey)
	if err != nil {
		return nil, err
	}
	return &genshinGeneralData{
		settings:       settings,
		graphicsData:   graphicsData,
		globalPerfData: globalPerfData,
		grades:         grades,
		saveItems:      saveItems,
	}, nil
}

func (d *genshinGeneralData) dcrEnabled() bool {
	for _, entry := range d.grades {
		if entry.Key == genshinDCRSettingKey && entry.Value == genshinDCREnabledValue {
			return true
		}
	}
	for _, entry := range d.saveItems {
		if entry.EntryType == genshinDCRSettingKey && entry.Index == genshinDCREnabledIndex {
			return true
		}
	}
	return false
}

func (d *genshinGeneralData) disableDCR() bool {
	updated := false
	foundGrade := false
	for i, entry := range d.grades {
		if entry.Key != genshinDCRSettingKey {
			continue
		}
		foundGrade = true
		if entry.Value == genshinDCREnabledValue {
			d.grades[i].Value = genshinDCRDisabledValue
			updated = true
		}
	}
	if !foundGrade {
		d.grades = append(d.grades, volatileGrade{Key: genshinDCRSettingKey, Value: genshinDCRDisabledValue})
		updated = true
	}
	foundItem := false
	for i, entry := range d.saveItems {
		if entry.EntryType != genshinDCRSettingKey {
			continue
		}
		foundItem = true
		if entry.Index == genshinDCREnabledIndex {
			d.saveItems[i].Index = genshinDCRDisabledIndex
			d.saveItems[i].ItemVersion = genshinDCRItemVersion
			updated = true
		}
	}
	if !foundItem {
		d.saveItems = append(d.saveItems, saveItem{
			EntryType:   genshinDCRSettingKey,
			Index:       genshinDCRDisabledIndex,
			ItemVersion: genshinDCRItemVersion,
		})
		updated = true
	}
	return updated
}

func (d *genshinGeneralData) encode() ([]byte, error) {
	gradesRaw, err := marshalCompact(d.grades)
	if err != nil {
		return nil, err
	}
	d.graphicsData[genshinVolatileGradesKey] = gradesRaw
	itemsRaw, err := marshalCompact(d.saveItems)
	if err != nil {
		return nil, err
	}
	d.globalPerfData[genshinSaveItemsKey] = itemsRaw
	graphicsJSON, err := marshalCompact(d.graphicsData)
	if err != nil {
		return nil, err
	}
	perfJSON, err := marshalCompact(d.globalPerfData)
	if err != nil {
		return nil, err
	}
	graphicsString, err := json.Marshal(string(graphicsJSON))
	if err != nil {
		return nil, err
	}
	perfString, err := json.Marshal(string(perfJSON))
	if err != nil {
		return nil, err
	}
	d.settings[genshinGraphicsDataKey] = graphicsString
	d.settings[genshinGlobalPerfDataKey] = perfString
	outer, err := marshalCompact(d.settings)
	if err != nil {
		return nil, err
	}
	return append(outer, 0), nil
}

func unmarshalNestedObject(settings map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	raw, ok := settings[key]
	if !ok {
		return nil, fmt.Errorf("unknown graphics settings format: %q key not found", key)
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("unknown graphics settings format: %q is not a JSON string", key)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(encoded), &object); err != nil {
		return nil, fmt.Errorf("unknown graphics settings format: %q is not JSON: %w", key, err)
	}
	return object, nil
}

func unmarshalObjectSlice[T any](object map[string]json.RawMessage, parent, key string) ([]T, error) {
	raw, ok := object[key]
	if !ok {
		return nil, fmt.Errorf("unknown graphics settings format: %q.%s key not found", parent, key)
	}
	var items []T
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("unknown graphics settings format: %q.%s is not an array", parent, key)
	}
	return items, nil
}

func stripNullTerminator(raw []byte) []byte {
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		return raw[:i]
	}
	return raw
}

func isASCII(raw []byte) bool {
	for _, b := range raw {
		if b > 127 {
			return false
		}
	}
	return true
}

func marshalCompact(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
