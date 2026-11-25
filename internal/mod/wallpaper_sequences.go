package mod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"nahida.live/desktop/internal/mod/metadata"
)

const wallpaperSequencesMetadataKey = "wallpaperSequences"

// GetWallpaperSequences returns the wallpaper sequences saved in the mod's nhd.json as the JSON array the model
// viewer wrote, or nil when there are none. Their content belongs to the viewer.
func (m *Mod) GetWallpaperSequences(ctx context.Context, modPath string) (json.RawMessage, error) {
	ref, err := m.toggleModRef(ctx, modPath)
	if err != nil {
		return nil, err
	}
	raw, err := metadata.Read(ref.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err == nil {
		var document map[string]json.RawMessage
		if document, err = decodeMetadataDocument(raw); err == nil {
			return document[wallpaperSequencesMetadataKey], nil
		}
	}
	return nil, m.reportTogglePresetError(err, "wallpaper-sequence.get", ref, nil)
}

// SetWallpaperSequences replaces the wallpaper sequences in the mod's nhd.json with sequences, a JSON array, or
// removes them when it is empty.
func (m *Mod) SetWallpaperSequences(ctx context.Context, modPath string, sequences json.RawMessage) error {
	ref, err := m.toggleModRef(ctx, modPath)
	if err != nil {
		return err
	}
	if wallpaperSequencesEmpty(sequences) {
		if _, err := metadata.Read(ref.path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	err = metadata.Upsert(ref.path, func(raw []byte) ([]byte, error) {
		return updateMetadataWallpaperSequences(raw, sequences)
	}, m.reportMetadataCleanup)
	return m.reportTogglePresetError(err, "wallpaper-sequence.set", ref, nil)
}

// updateMetadataWallpaperSequences sets the sequences in the nhd.json document raw, which may be nil, keeping its
// other keys.
func updateMetadataWallpaperSequences(raw []byte, sequences json.RawMessage) ([]byte, error) {
	document, err := decodeMetadataDocument(raw)
	if err != nil {
		return nil, err
	}
	if wallpaperSequencesEmpty(sequences) {
		delete(document, wallpaperSequencesMetadataKey)
		return json.Marshal(document)
	}
	if trimmed := bytes.TrimSpace(sequences); len(trimmed) == 0 || trimmed[0] != '[' || !json.Valid(trimmed) {
		return nil, errors.New("wallpaper sequences are not a JSON array")
	}
	document[wallpaperSequencesMetadataKey] = sequences
	return json.Marshal(document)
}

func wallpaperSequencesEmpty(sequences json.RawMessage) bool {
	trimmed := bytes.TrimSpace(sequences)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]"))
}

func decodeMetadataDocument(raw []byte) (map[string]json.RawMessage, error) {
	document := map[string]json.RawMessage{}
	if raw == nil {
		return document, nil
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode mod metadata: %w", err)
	}
	if document == nil {
		return nil, errors.New("invalid mod metadata object")
	}
	return document, nil
}
