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
		grades  []volatileGrade
		items   []saveItem
		enabled bool
	}{
		{
			name:    "enabled in both records",
			grades:  []volatileGrade{{Key: 21, Value: 2}},
			items:   []saveItem{{EntryType: 21, Index: 1, ItemVersion: "OSRELWin5.0.0"}},
			enabled: true,
		},
		{
			name:    "enabled only in volatile grades",
			grades:  []volatileGrade{{Key: 1, Value: 1}, {Key: 21, Value: 2}},
			items:   []saveItem{{EntryType: 21, Index: 0, ItemVersion: "OSRELWin5.0.0"}},
			enabled: true,
		},
		{
			name:    "enabled only in save items",
			grades:  []volatileGrade{{Key: 21, Value: 1}},
			items:   []saveItem{{EntryType: 7, Index: 0}, {EntryType: 21, Index: 1}},
			enabled: true,
		},
		{
			name:    "already disabled",
			grades:  []volatileGrade{{Key: 21, Value: 1}},
			items:   []saveItem{{EntryType: 21, Index: 0, ItemVersion: "OSRELWin5.0.0"}},
			enabled: false,
		},
		{
			name:    "dcr keys absent",
			grades:  []volatileGrade{{Key: 1, Value: 1}},
			items:   []saveItem{{EntryType: 7, Index: 0}},
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
		[]volatileGrade{{Key: 21, Value: 1}},
		[]saveItem{{EntryType: 21, Index: 0}},
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
		[]volatileGrade{{Key: 21, Value: 2}, {Key: 3, Value: 4}},
		[]saveItem{{EntryType: 21, Index: 1, ItemVersion: "old"}},
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
	if len(roundTrip.grades) != 2 || roundTrip.grades[0].Value != genshinDCRDisabledValue {
		t.Fatalf("grades = %+v", roundTrip.grades)
	}
	if roundTrip.saveItems[0].Index != genshinDCRDisabledIndex || roundTrip.saveItems[0].ItemVersion != genshinDCRItemVersion {
		t.Fatalf("save item = %+v", roundTrip.saveItems[0])
	}
}

func TestDisableDCRAppendsMissingEntries(t *testing.T) {
	t.Parallel()
	data, err := parseGenshinGeneralData(mustEncodeGeneralData(t,
		[]volatileGrade{{Key: 1, Value: 1}},
		[]saveItem{{EntryType: 7, Index: 0}},
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
	roundTrip, err := parseGenshinGeneralData(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.grades) != 2 {
		t.Fatalf("grades = %+v", roundTrip.grades)
	}
	last := roundTrip.grades[1]
	if last.Key != genshinDCRSettingKey || last.Value != genshinDCRDisabledValue {
		t.Fatalf("appended grade = %+v", last)
	}
}

func TestDisableDCRSkipsAlreadyDisabledSettings(t *testing.T) {
	t.Parallel()
	data, err := parseGenshinGeneralData(mustEncodeGeneralData(t,
		[]volatileGrade{{Key: 21, Value: 1}},
		[]saveItem{{EntryType: 21, Index: 0, ItemVersion: genshinDCRItemVersion}},
	))
	if err != nil {
		t.Fatal(err)
	}
	if updated := data.disableDCR(); updated {
		t.Fatal("disableDCR = true, want false")
	}
}

func TestRejectEnabledGimiDCRSkipsNonGIMI(t *testing.T) {
	t.Parallel()
	if err := New().rejectEnabledGimiDCR(t.Context(), "WWMI"); err != nil {
		t.Fatal(err)
	}
}

func mustEncodeGeneralData(t *testing.T, grades []volatileGrade, items []saveItem) []byte {
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
