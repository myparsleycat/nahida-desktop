package mod

import (
	"encoding/json"
	"testing"
)

func TestUpdateMetadataTextureChoiceKeepsOtherKeys(t *testing.T) {
	raw := []byte(`{"id":"mod-1","togglePresets":[{"id":"p"}]}`)

	set, err := updateMetadataTextureChoice(raw, "KafkaBodyB", "KafkaBodyC")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(set, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["id"]) != `"mod-1"` || string(document["togglePresets"]) != `[{"id":"p"}]` {
		t.Fatalf("other keys changed: %s", set)
	}
	if string(document[viewerTextureChoicesMetadataKey]) != `{"KafkaBodyB":"KafkaBodyC"}` {
		t.Fatalf("choices = %s", document[viewerTextureChoicesMetadataKey])
	}

	cleared, err := updateMetadataTextureChoice(set, "KafkaBodyB", "")
	if err != nil {
		t.Fatal(err)
	}
	document = nil
	if err := json.Unmarshal(cleared, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document[viewerTextureChoicesMetadataKey]; ok || len(document) != 2 {
		t.Fatalf("cleared = %s", cleared)
	}
}
