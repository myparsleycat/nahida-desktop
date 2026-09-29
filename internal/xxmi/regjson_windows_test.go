//go:build windows

package xxmi

import (
	"bytes"
	"testing"
)

type fakeRegistryJSONValue struct {
	value  []byte
	writes [][]byte
}

func (f *fakeRegistryJSONValue) GetBinaryValue(string) ([]byte, uint32, error) {
	return f.value, 3, nil
}

func (f *fakeRegistryJSONValue) SetBinaryValue(_ string, value []byte) error {
	f.writes = append(f.writes, bytes.Clone(value))
	return nil
}

func TestParseRegistryJSONIgnoresNullTerminator(t *testing.T) {
	value, err := parseRegistryJSON([]byte("{\"FPS\":60}\x00discarded"))
	if err != nil {
		t.Fatal(err)
	}
	if value["FPS"] != float64(60) {
		t.Fatalf("FPS = %#v", value["FPS"])
	}
	if _, err := parseRegistryJSON([]byte("[]")); err == nil {
		t.Fatal("array accepted as registry settings")
	}
}

func TestEditRegistryJSONWritesCompactNullTerminatedValueOnlyWhenChanged(t *testing.T) {
	t.Parallel()
	value := &fakeRegistryJSONValue{value: []byte("{\"FPS\":60,\"Mode\":\"High\"}\x00")}
	if err := editRegistryJSONValue(value, "Graphics", func(settings map[string]any) error {
		settings["FPS"] = 120
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []byte("{\"FPS\":120,\"Mode\":\"High\"}\x00")
	if len(value.writes) != 1 || !bytes.Equal(value.writes[0], want) {
		t.Fatalf("registry writes = %q; want %q", value.writes, want)
	}

	value.value = bytes.Clone(want)
	if err := editRegistryJSONValue(value, "Graphics", func(map[string]any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(value.writes) != 1 {
		t.Fatalf("unchanged registry value was written %d times", len(value.writes))
	}
}
