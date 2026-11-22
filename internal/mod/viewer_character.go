package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod/character"
	"nahida.live/desktop/internal/mod/metadata"
	"nahida.live/desktop/internal/platform"
)

const viewerCharacterMetadataKey = "viewerCharacter"

type ViewerCharacter struct {
	DirectoryMatched bool
	Override         *CharacterAssignment
	HasOverride      bool
	Inherited        *ModCharacter
}

func (m *Mod) GetViewerCharacter(ctx context.Context, modPath, openedPath string) (ViewerCharacter, error) {
	var selection ViewerCharacter
	if err := ctx.Err(); err != nil {
		return selection, err
	}
	if openedPath == "" {
		inherited, err := m.ResolveModCharacter(ctx, modPath)
		selection.Inherited = inherited
		return selection, err
	}
	resolved, err := viewerCharacterDirectory(modPath, openedPath)
	if err != nil {
		return selection, infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
			Operation: "get-viewer-character", Stage: "resolve-directory",
			Fields: map[string]any{"modPath": modPath, "openedPath": openedPath, "resolvedPath": resolved},
		})
	}
	selection.DirectoryMatched = true

	document, readErr := readViewerCharacterMetadata(resolved)
	if encoded, ok := document[viewerCharacterMetadataKey]; ok {
		selection.HasOverride = true
		var assignment CharacterAssignment
		if json.Unmarshal(encoded, &assignment) == nil {
			if _, ok := character.Lookup(assignment.Game, assignment.ID); ok {
				selection.Override = &assignment
				return selection, nil
			}
		}
	}
	if readErr != nil {
		readErr = infra.ReportError(m.log, readErr, "Mod", infra.Diagnostic{
			Operation: "get-viewer-character", Stage: "read-metadata",
			Fields: map[string]any{"modPath": modPath, "resolvedPath": resolved},
		})
	}
	selection.Inherited, err = m.ResolveModCharacter(ctx, modPath)
	return selection, errors.Join(readErr, err)
}

// SetViewerCharacter updates the loaded folder's direct assignment. A nil assignment restores inheritance.
func (m *Mod) SetViewerCharacter(
	ctx context.Context,
	modPath, openedPath string,
	assignment *CharacterAssignment,
) (err error) {
	stage := "validate"
	var resolved string
	defer func() {
		if err != nil && !infra.IsCancellationError(err) {
			err = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
				Operation: "set-viewer-character", Stage: stage,
				Fields: map[string]any{
					"modPath": modPath, "openedPath": openedPath, "resolvedPath": resolved, "assignment": assignment,
				},
			})
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved, err = viewerCharacterDirectory(modPath, openedPath)
	if err != nil {
		return err
	}
	if assignment != nil {
		if _, ok := character.Lookup(assignment.Game, assignment.ID); !ok {
			return errors.New("CHARACTER_NOT_FOUND")
		}
	}

	stage = "update-metadata"
	err = metadata.UpdateLocal(resolved, assignment != nil, func(raw []byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		document, err := decodeViewerCharacterMetadata(raw)
		if err != nil {
			return nil, err
		}
		if assignment == nil {
			delete(document, viewerCharacterMetadataKey)
		} else {
			encoded, err := json.Marshal(assignment)
			if err != nil {
				return nil, err
			}
			document[viewerCharacterMetadataKey] = encoded
		}
		return json.Marshal(document)
	}, m.reportMetadataCleanup)
	if assignment == nil && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func viewerCharacterDirectory(path, openedPath string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", infra.WithCause(errors.New("INVALID_CHARACTER_PATH"), err)
	}
	resolved, err := platform.FinalPath(path)
	if err != nil || !platform.SamePathFold(resolved, openedPath) {
		return resolved, infra.WithCause(errors.New("INVALID_CHARACTER_PATH"), err)
	}
	return resolved, nil
}

func readViewerCharacterMetadata(path string) (map[string]json.RawMessage, error) {
	raw, err := metadata.ReadLocal(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeViewerCharacterMetadata(raw)
}

func decodeViewerCharacterMetadata(raw []byte) (map[string]json.RawMessage, error) {
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
