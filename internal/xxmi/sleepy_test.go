package xxmi

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestSleepyMatchesReferenceBytes(t *testing.T) {
	fixture, err := hex.DecodeString("0001000000ffffffff0100000000000000060100000004340cd0b50b")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeSleepy([]byte("abc"), []byte{85, 110, 209, 150})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, fixture) {
		t.Fatalf("encoded = %x, want %x", encoded, fixture)
	}
	decoded, err := decodeSleepy(fixture, []byte{85, 110, 209, 150})
	if err != nil || string(decoded) != "abc" {
		t.Fatalf("decoded = %q, error = %v", decoded, err)
	}
	fixture[len(fixture)-1] = 0
	if _, err := decodeSleepy(fixture, []byte{85, 110, 209, 150}); err == nil {
		t.Fatal("corrupted Sleepy footer was accepted")
	}
}

func TestSleepyJSONMatchesReferenceLayout(t *testing.T) {
	value, err := parseSleepyJSON([]byte(`{"alpha":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := serializeSleepyJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := "\r\n{\r\n    \"alpha\" : 1,\r\n    \"b\"     : 2\r\n}"
	if string(encoded) != want {
		t.Fatalf("serialized = %q, want %q", encoded, want)
	}
}
