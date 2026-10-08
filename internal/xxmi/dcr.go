package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"nahida.live/desktop/internal/infra"
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
	xxmiDisableDCRWhere      = "XXMI.disableDCR"
	gimiImporterKey          = "GIMI"
)

// errGimiDCRUnreadable marks a Genshin graphics record that is missing or has an unknown shape. Genshin either
// never saved graphics settings in this Windows account or changed their format, and neither can be fixed
// here, so a launch warns about it instead of failing.
var errGimiDCRUnreadable = errors.New("genshin dynamic character resolution setting is unreadable")

// genshinGeneralData keeps the registry record as ordered JSON so a rewrite preserves key order, number
// text, and fields this code does not know about, like the reference launcher's dict round trip.
type genshinGeneralData struct {
	settings       sleepyJSONValue
	graphicsData   sleepyJSONValue
	globalPerfData sleepyJSONValue
}

func (x *XXMI) DisableGenshinDynamicCharacterResolution(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := readGenshinRegistryGeneralData()
	if err != nil {
		return x.reportDCRFailure(fmt.Errorf("%w: %w", errGimiDCRUnreadable, err), "read")
	}
	data, err := parseGenshinGeneralData(raw)
	if err != nil {
		return x.reportDCRFailure(fmt.Errorf("%w: %w", errGimiDCRUnreadable, err), "decode")
	}
	if !data.disableDCR() {
		return nil
	}
	encoded, err := data.encode()
	if err != nil {
		return x.reportDCRFailure(err, "encode")
	}
	if x.log != nil {
		x.log.Info("Disabling Genshin Impact Dynamic Character Resolution", xxmiDisableDCRWhere)
	}
	if err := writeGenshinRegistryGeneralData(encoded); err != nil {
		return x.reportDCRFailure(err, "write")
	}
	return nil
}

func readGenshinDCR(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	raw, err := readGenshinRegistryGeneralData()
	if err != nil {
		return false, err
	}
	data, err := parseGenshinGeneralData(raw)
	if err != nil {
		return false, err
	}
	return data.dcrEnabled(), nil
}

func (x *XXMI) reportDCRFailure(err error, stage string) error {
	severity := infra.DiagnosticError
	if errors.Is(err, errGimiDCRUnreadable) {
		severity = infra.DiagnosticWarn
	}
	return infra.ReportError(x.log, err, "XXMI", infra.Diagnostic{
		Severity: severity, Operation: "disable-dcr", Stage: stage,
		Fields: map[string]any{"importer": gimiImporterKey},
	})
}

func parseGenshinGeneralData(raw []byte) (*genshinGeneralData, error) {
	payload := stripNullTerminator(raw)
	if len(payload) == 0 {
		return nil, errors.New("genshin impact graphics settings are empty")
	}
	if !isASCII(payload) {
		return nil, errors.New("genshin impact graphics settings are not ASCII")
	}
	settings, err := parseSleepyJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("genshin impact graphics settings are not JSON: %w", err)
	}
	if settings.kind != '{' {
		return nil, errors.New("genshin impact graphics settings are not a JSON object")
	}

	graphicsData, err := parseNestedObject(&settings, genshinGraphicsDataKey)
	if err != nil {
		return nil, err
	}
	if _, err := objectArray(&graphicsData, genshinGraphicsDataKey, genshinVolatileGradesKey); err != nil {
		return nil, err
	}
	globalPerfData, err := parseNestedObject(&settings, genshinGlobalPerfDataKey)
	if err != nil {
		return nil, err
	}
	if _, err := objectArray(&globalPerfData, genshinGlobalPerfDataKey, genshinSaveItemsKey); err != nil {
		return nil, err
	}
	return &genshinGeneralData{settings: settings, graphicsData: graphicsData, globalPerfData: globalPerfData}, nil
}

func (d *genshinGeneralData) dcrEnabled() bool {
	for _, grade := range d.graphicsData.field(genshinVolatileGradesKey).items {
		if jsonIntField(&grade, "key") == genshinDCRSettingKey &&
			jsonIntField(&grade, "value") == genshinDCREnabledValue {
			return true
		}
	}
	for _, item := range d.globalPerfData.field(genshinSaveItemsKey).items {
		if jsonIntField(&item, "entryType") == genshinDCRSettingKey &&
			jsonIntField(&item, "index") == genshinDCREnabledIndex {
			return true
		}
	}
	return false
}

func (d *genshinGeneralData) disableDCR() bool {
	updated := false

	grades := d.graphicsData.field(genshinVolatileGradesKey)
	foundGrade := false
	for i := range grades.items {
		grade := &grades.items[i]
		if jsonIntField(grade, "key") != genshinDCRSettingKey {
			continue
		}
		foundGrade = true
		if jsonIntField(grade, "value") == genshinDCREnabledValue {
			grade.setField("value", jsonInt(genshinDCRDisabledValue))
			updated = true
		}
	}
	if !foundGrade {
		grades.items = append(grades.items, sleepyJSONValue{kind: '{', fields: []sleepyJSONField{
			{key: "key", value: jsonInt(genshinDCRSettingKey)},
			{key: "value", value: jsonInt(genshinDCRDisabledValue)},
		}})
		updated = true
	}

	items := d.globalPerfData.field(genshinSaveItemsKey)
	foundItem := false
	for i := range items.items {
		item := &items.items[i]
		if jsonIntField(item, "entryType") != genshinDCRSettingKey {
			continue
		}
		foundItem = true
		if jsonIntField(item, "index") == genshinDCREnabledIndex {
			item.setField("index", jsonInt(genshinDCRDisabledIndex))
			item.setField("itemVersion", sleepyJSONValue{scalar: genshinDCRItemVersion})
			updated = true
		}
	}
	if !foundItem {
		items.items = append(items.items, sleepyJSONValue{kind: '{', fields: []sleepyJSONField{
			{key: "entryType", value: jsonInt(genshinDCRSettingKey)},
			{key: "index", value: jsonInt(genshinDCRDisabledIndex)},
			{key: "itemVersion", value: sleepyJSONValue{scalar: genshinDCRItemVersion}},
		}})
		updated = true
	}
	return updated
}

func (d *genshinGeneralData) encode() ([]byte, error) {
	graphicsJSON, err := compactSleepyJSON(d.graphicsData)
	if err != nil {
		return nil, err
	}
	perfJSON, err := compactSleepyJSON(d.globalPerfData)
	if err != nil {
		return nil, err
	}
	d.settings.setField(genshinGraphicsDataKey, sleepyJSONValue{scalar: string(graphicsJSON)})
	d.settings.setField(genshinGlobalPerfDataKey, sleepyJSONValue{scalar: string(perfJSON)})
	outer, err := compactSleepyJSON(d.settings)
	if err != nil {
		return nil, err
	}
	return append(outer, 0), nil
}

func parseNestedObject(settings *sleepyJSONValue, key string) (sleepyJSONValue, error) {
	field := settings.field(key)
	if field == nil {
		return sleepyJSONValue{}, fmt.Errorf("unknown graphics settings format: %q key not found", key)
	}
	encoded, ok := field.scalar.(string)
	if field.kind != 0 || !ok {
		return sleepyJSONValue{}, fmt.Errorf("unknown graphics settings format: %q is not a JSON string", key)
	}
	object, err := parseSleepyJSON([]byte(encoded))
	if err != nil {
		return sleepyJSONValue{}, fmt.Errorf("unknown graphics settings format: %q is not JSON: %w", key, err)
	}
	if object.kind != '{' {
		return sleepyJSONValue{}, fmt.Errorf("unknown graphics settings format: %q is not a JSON object", key)
	}
	return object, nil
}

func objectArray(object *sleepyJSONValue, parent, key string) (*sleepyJSONValue, error) {
	array := object.field(key)
	if array == nil {
		return nil, fmt.Errorf("unknown graphics settings format: %q.%s key not found", parent, key)
	}
	if array.kind != '[' {
		return nil, fmt.Errorf("unknown graphics settings format: %q.%s is not an array", parent, key)
	}
	for _, item := range array.items {
		if item.kind != '{' {
			return nil, fmt.Errorf("unknown graphics settings format: %q.%s has a non-object entry", parent, key)
		}
	}
	return array, nil
}

// jsonIntField returns an integer field, or -1 when it is missing or not an integer so it matches no setting.
func jsonIntField(object *sleepyJSONValue, key string) int {
	field := object.field(key)
	if field == nil {
		return -1
	}
	number, ok := field.scalar.(json.Number)
	if !ok {
		return -1
	}
	value, err := strconv.Atoi(number.String())
	if err != nil {
		return -1
	}
	return value
}

func jsonInt(value int) sleepyJSONValue {
	return sleepyJSONValue{scalar: json.Number(strconv.Itoa(value))}
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
