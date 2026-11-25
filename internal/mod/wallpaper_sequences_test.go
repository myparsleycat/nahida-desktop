package mod

import (
	"encoding/json"
	"testing"
)

func TestUpdateMetadataWallpaperSequencesKeepsOtherKeys(t *testing.T) {
	raw := []byte(`{"id":"mod-1","viewerTextureChoices":{"A":"B"}}`)

	set, err := updateMetadataWallpaperSequences(raw, json.RawMessage(`[{"id":"s","name":"Loop","shots":[]}]`))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(set, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["id"]) != `"mod-1"` || string(document["viewerTextureChoices"]) != `{"A":"B"}` {
		t.Fatalf("other keys changed: %s", set)
	}
	if string(document[wallpaperSequencesMetadataKey]) != `[{"id":"s","name":"Loop","shots":[]}]` {
		t.Fatalf("sequences = %s", document[wallpaperSequencesMetadataKey])
	}

	if _, err := updateMetadataWallpaperSequences(set, json.RawMessage(`{"id":"s"}`)); err == nil {
		t.Fatal("an object replaced the sequence array")
	}

	cleared, err := updateMetadataWallpaperSequences(set, json.RawMessage(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	document = nil
	if err := json.Unmarshal(cleared, &document); err != nil {
		t.Fatal(err)
	}
	if _, ok := document[wallpaperSequencesMetadataKey]; ok || len(document) != 2 {
		t.Fatalf("cleared = %s", cleared)
	}
}
