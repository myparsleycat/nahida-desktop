//go:build windows

package xxmi

import "testing"

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
