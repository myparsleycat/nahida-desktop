package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"nahida.live/desktop/internal/mod/metadata"
)

const viewerTextureChoicesMetadataKey = "viewerTextureChoices"

// GetViewerTextureChoices returns the texture sources the user picked in the model viewer, keyed by the part
// whose INI does not say which of its siblings' textures it uses.
func (m *Mod) GetViewerTextureChoices(ctx context.Context, modPath string) (map[string]string, error) {
	ref, err := m.toggleModRef(ctx, modPath)
	if err != nil {
		return nil, err
	}
	raw, err := metadata.Read(ref.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err == nil {
		var choices map[string]string
		_, choices, err = decodeMetadataTextureChoices(raw)
		if err == nil {
			return choices, nil
		}
	}
	return nil, m.reportTogglePresetError(err, "viewer-texture-choice.get", ref, nil)
}

// SetViewerTextureChoice records source as the texture set of part in the mod's nhd.json, or removes the part's
// entry when source is empty.
func (m *Mod) SetViewerTextureChoice(ctx context.Context, modPath, part, source string) error {
	ref, err := m.toggleModRef(ctx, modPath)
	if err != nil {
		return err
	}
	if source == "" {
		if _, err := metadata.Read(ref.path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	err = metadata.Upsert(ref.path, func(raw []byte) ([]byte, error) {
		return updateMetadataTextureChoice(raw, part, source)
	}, m.reportMetadataCleanup)
	return m.reportTogglePresetError(err, "viewer-texture-choice.set", ref,
		map[string]any{"part": part, "source": source})
}

// updateMetadataTextureChoice sets part's source in the nhd.json document raw, which may be nil, keeping its other
// keys. It drops the key when no choice remains.
func updateMetadataTextureChoice(raw []byte, part, source string) ([]byte, error) {
	document, choices, err := decodeMetadataTextureChoices(raw)
	if err != nil {
		return nil, err
	}
	if source == "" {
		delete(choices, part)
	} else {
		choices[part] = source
	}
	if len(choices) == 0 {
		delete(document, viewerTextureChoicesMetadataKey)
		return json.Marshal(document)
	}
	encoded, err := json.Marshal(choices)
	if err != nil {
		return nil, err
	}
	document[viewerTextureChoicesMetadataKey] = encoded
	return json.Marshal(document)
}

func decodeMetadataTextureChoices(raw []byte) (map[string]json.RawMessage, map[string]string, error) {
	document := map[string]json.RawMessage{}
	if raw != nil {
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, nil, fmt.Errorf("decode mod metadata: %w", err)
		}
		if document == nil {
			return nil, nil, errors.New("invalid mod metadata object")
		}
	}
	choices := map[string]string{}
	if encoded, ok := document[viewerTextureChoicesMetadataKey]; ok {
		if err := json.Unmarshal(encoded, &choices); err != nil {
			return nil, nil, fmt.Errorf("decode viewer texture choices: %w", err)
		}
		if choices == nil {
			choices = map[string]string{}
		}
	}
	return document, choices, nil
}
