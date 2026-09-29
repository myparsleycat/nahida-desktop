package xxmi

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestParseGenshinGeneralDataDetectsDCR(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		grades  []testGrade
		items   []testSaveItem
		enabled bool
	}{
		{
			name:    "enabled in both records",
			grades:  []testGrade{{Key: 21, Value: 2}},
			items:   []testSaveItem{{EntryType: 21, Index: 1, ItemVersion: "OSRELWin5.0.0"}},
			enabled: true,
		},
		{
			name:    "enabled only in volatile grades",
			grades:  []testGrade{{Key: 1, Value: 1}, {Key: 21, Value: 2}},
			items:   []testSaveItem{{EntryType: 21, Index: 0, ItemVersion: "OSRELWin5.0.0"}},
			enabled: true,
		},
		{
			name:    "enabled only in save items",
			grades:  []testGrade{{Key: 21, Value: 1}},
			items:   []testSaveItem{{EntryType: 7, Index: 0}, {EntryType: 21, Index: 1}},
			enabled: true,
		},
		{
			name:    "already disabled",
			grades:  []testGrade{{Key: 21, Value: 1}},
			items:   []testSaveItem{{EntryType: 21, Index: 0, ItemVersion: "OSRELWin5.0.0"}},
			enabled: false,
		},
		{
			name:    "dcr keys absent",
			grades:  []testGrade{{Key: 1, Value: 1}},
			items:   []testSaveItem{{EntryType: 7, Index: 0}},
			enabled: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := parseGenshinGeneralData(mustEncodeGeneralData(t, tc.grades, tc.items))
			if err != nil {
				t.Fatal(err)
			}
			if enabled := data.dcrEnabled(); enabled != tc.enabled {
				t.Fatalf("dcrEnabled = %v, want %v", enabled, tc.enabled)
			}
		})
	}
}

func TestParseGenshinGeneralDataStripsNullTerminator(t *testing.T) {
	t.Parallel()
	raw := mustEncodeGeneralData(t,
		[]testGrade{{Key: 21, Value: 1}},
		[]testSaveItem{{EntryType: 21, Index: 0}},
	)
	if !bytes.HasSuffix(raw, []byte{0}) {
		t.Fatal("fixture is not null-terminated")
	}
	if _, err := parseGenshinGeneralData(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := parseGenshinGeneralData(bytes.TrimSuffix(raw, []byte{0})); err != nil {
		t.Fatal(err)
	}
}

func TestParseGenshinGeneralDataRejectsUnknownShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "empty", raw: nil},
		{name: "non ascii", raw: []byte("{\"graphicsData\":\"\xff\"}\x00")},
		{name: "not json", raw: []byte("not-json\x00")},
		{name: "missing graphicsData", raw: []byte(`{"globalPerfData":"{}"}`)},
		{name: "missing globalPerfData", raw: []byte(`{"graphicsData":"{}"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseGenshinGeneralData(tc.raw); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDisableDCRMutatesEnabledSettings(t *testing.T) {
	t.Parallel()
	data, err := parseGenshinGeneralData(mustEncodeGeneralData(t,
		[]testGrade{{Key: 21, Value: 2}, {Key: 3, Value: 4}},
		[]testSaveItem{{EntryType: 21, Index: 1, ItemVersion: "old"}},
	))
	if err != nil {
		t.Fatal(err)
	}
	if updated := data.disableDCR(); !updated {
		t.Fatal("disableDCR = false, want true")
	}
	if data.dcrEnabled() {
		t.Fatal("dcrEnabled after disable")
	}
	encoded, err := data.encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(encoded, []byte{0}) {
		t.Fatal("encoded settings are not null-terminated")
	}
	roundTrip, err := parseGenshinGeneralData(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip.dcrEnabled() {
		t.Fatal("round-trip dcrEnabled")
	}
	grades, items := decodeGeneralData(t, encoded)
	if len(grades) != 2 || grades[0].Value != genshinDCRDisabledValue {
		t.Fatalf("grades = %+v", grades)
	}
	if items[0].Index != genshinDCRDisabledIndex || items[0].ItemVersion != genshinDCRItemVersion {
		t.Fatalf("save item = %+v", items[0])
	}
}

func TestDisableDCRAppendsMissingEntries(t *testing.T) {
	t.Parallel()
	data, err := parseGenshinGeneralData(mustEncodeGeneralData(t,
		[]testGrade{{Key: 1, Value: 1}},
		[]testSaveItem{{EntryType: 7, Index: 0}},
	))
	if err != nil {
		t.Fatal(err)
	}
	if updated := data.disableDCR(); !updated {
		t.Fatal("disableDCR = false, want true")
	}
	if data.dcrEnabled() {
		t.Fatal("dcrEnabled after append")
	}
	encoded, err := data.encode()
	if err != nil {
		t.Fatal(err)
	}
	grades, _ := decodeGeneralData(t, encoded)
	if len(grades) != 2 {
		t.Fatalf("grades = %+v", grades)
	}
	last := grades[1]
	if last.Key != genshinDCRSettingKey || last.Value != genshinDCRDisabledValue {
		t.Fatalf("appended grade = %+v", last)
	}
}

func TestDisableDCRSkipsAlreadyDisabledSettings(t *testing.T) {
	t.Parallel()
	data, err := parseGenshinGeneralData(mustEncodeGeneralData(t,
		[]testGrade{{Key: 21, Value: 1}},
		[]testSaveItem{{EntryType: 21, Index: 0, ItemVersion: genshinDCRItemVersion}},
	))
	if err != nil {
		t.Fatal(err)
	}
	if updated := data.disableDCR(); updated {
		t.Fatal("disableDCR = true, want false")
	}
}

func TestDisableDCRPreservesUnknownFieldsAndOrder(t *testing.T) {
	t.Parallel()
	graphics := `{"volatileVersion":"x","customVolatileGrades":[{"key":21,"value":2,"extra":1.50}]}`
	perf := `{"saveItems":[{"entryType":21,"index":1,"itemVersion":"old","flag":true}],"z":"\u00e9"}`
	raw, err := json.Marshal(map[string]string{genshinGraphicsDataKey: graphics, genshinGlobalPerfDataKey: perf})
	if err != nil {
		t.Fatal(err)
	}
	// Put a later key first to prove the outer order is kept rather than sorted.
	raw = append([]byte(`{"zOuter":1,`), raw[1:]...)
	data, err := parseGenshinGeneralData(append(raw, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !data.disableDCR() {
		t.Fatal("disableDCR = false, want true")
	}
	encoded, err := data.encode()
	if err != nil {
		t.Fatal(err)
	}

	want := `{"zOuter":1,` +
		`"globalPerfData":"{\"saveItems\":[{\"entryType\":21,\"index\":0,\"itemVersion\":\"OSRELWin5.0.0\",` +
		`\"flag\":true}],\"z\":\"\\u00e9\"}",` +
		`"graphicsData":"{\"volatileVersion\":\"x\",\"customVolatileGrades\":[{\"key\":21,\"value\":1,` +
		`\"extra\":1.50}]}"}` + "\x00"
	if string(encoded) != want {
		t.Fatalf("encoded = %s\nwant    = %s", encoded, want)
	}
}

func mustEncodeGeneralData(t *testing.T, grades []testGrade, items []testSaveItem) []byte {
	t.Helper()
	graphics, err := json.Marshal(map[string]any{genshinVolatileGradesKey: grades})
	if err != nil {
		t.Fatal(err)
	}
	perf, err := json.Marshal(map[string]any{genshinSaveItemsKey: items})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := json.Marshal(map[string]any{
		genshinGraphicsDataKey:   string(graphics),
		genshinGlobalPerfDataKey: string(perf),
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(outer, 0)
}

type testGrade struct {
	Key   int `json:"key"`
	Value int `json:"value"`
}

type testSaveItem struct {
	EntryType   int    `json:"entryType"`
	Index       int    `json:"index"`
	ItemVersion string `json:"itemVersion"`
}

func decodeGeneralData(t *testing.T, encoded []byte) ([]testGrade, []testSaveItem) {
	t.Helper()
	var outer map[string]string
	if err := json.Unmarshal(bytes.TrimSuffix(encoded, []byte{0}), &outer); err != nil {
		t.Fatal(err)
	}
	var graphics struct {
		Grades []testGrade `json:"customVolatileGrades"`
	}
	if err := json.Unmarshal([]byte(outer[genshinGraphicsDataKey]), &graphics); err != nil {
		t.Fatal(err)
	}
	var perf struct {
		Items []testSaveItem `json:"saveItems"`
	}
	if err := json.Unmarshal([]byte(outer[genshinGlobalPerfDataKey]), &perf); err != nil {
		t.Fatal(err)
	}
	return graphics.Grades, perf.Items
}
