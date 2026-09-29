//go:build windows

package xxmi

import (
	"bytes"
	"encoding/json"
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
	if fps := value.field("FPS"); fps == nil || fps.scalar != json.Number("60") {
		t.Fatalf("FPS = %#v", fps)
	}
	if _, err := parseRegistryJSON([]byte("[]")); err == nil {
		t.Fatal("array accepted as registry settings")
	}
}

func TestEditRegistryJSONWritesCompactNullTerminatedValueOnlyWhenChanged(t *testing.T) {
	t.Parallel()
	value := &fakeRegistryJSONValue{value: []byte("{\"FPS\":60,\"Mode\":\"High\"}\x00")}
	if err := editRegistryJSONValue(value, "Graphics", func(settings *sleepyJSONValue) error {
		settings.setField("FPS", sleepyJSONValue{scalar: json.Number("120")})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []byte("{\"FPS\":120,\"Mode\":\"High\"}\x00")
	if len(value.writes) != 1 || !bytes.Equal(value.writes[0], want) {
		t.Fatalf("registry writes = %q; want %q", value.writes, want)
	}

	value.value = bytes.Clone(want)
	if err := editRegistryJSONValue(value, "Graphics", func(*sleepyJSONValue) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(value.writes) != 1 {
		t.Fatalf("unchanged registry value was written %d times", len(value.writes))
	}
}

func TestEditRegistryJSONPreservesOrderAndNumberText(t *testing.T) {
	t.Parallel()
	value := &fakeRegistryJSONValue{value: []byte("{ \"Zoom\": 1.50, \"FPS\": 60, \"Name\": \"<a&b>\" }\x00")}
	if err := editRegistryJSONValue(value, "Graphics", func(settings *sleepyJSONValue) error {
		settings.setField("FPS", sleepyJSONValue{scalar: json.Number("120")})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []byte("{\"Zoom\":1.50,\"FPS\":120,\"Name\":\"<a&b>\"}\x00")
	if len(value.writes) != 1 || !bytes.Equal(value.writes[0], want) {
		t.Fatalf("registry writes = %q; want %q", value.writes, want)
	}
}

func TestCompactSleepyJSONEscapesLikePythonEnsureASCII(t *testing.T) {
	t.Parallel()
	value, err := parseSleepyJSON([]byte("{\"text\":\"é\U0001F600\\n\\u007f\\\\\"}"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := compactSleepyJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"text\":\"\\u00e9\\ud83d\\ude00\\n\\u007f\\\\\"}"
	if string(got) != want {
		t.Fatalf("compact = %s; want %s", got, want)
	}
}
