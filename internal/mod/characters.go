package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod/character"
	"nahida.live/desktop/internal/mod/metadata"
)

const characterMetadataKey = "character"

// CharacterAssignment names a catalog character: Game is the importer, such as GIMI, and ID the catalog entry.
type CharacterAssignment struct {
	Game string `json:"game"`
	ID   string `json:"id"`
}

// ModCharacter is the character assigned to the nearest folder holding a mod.
type ModCharacter struct {
	CharacterAssignment
	Folder string `json:"folder"`
}

// SetFolderCharacter assigns a catalog character to a folder in a managed mod root, or clears the assignment when
// assignment is nil. The assignment lives in the folder's nhd.json, so it survives renames and toggles.
func (m *Mod) SetFolderCharacter(ctx context.Context, folderPath string, assignment *CharacterAssignment) (err error) {
	stage := "validate"
	defer func() {
		if err != nil {
			err = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
				Operation: "set-folder-character", Stage: stage,
				Fields: map[string]any{"folderPath": folderPath, "assignment": assignment},
			})
		}
	}()
	owned, err := m.ownedRoot(ctx, folderPath)
	if err != nil {
		return infra.WithCause(errors.New("INVALID_CHARACTER_PATH"), err)
	}
	if owned.relative == "" || owned.relative == "." {
		return errors.New("INVALID_CHARACTER_PATH")
	}
	info, err := os.Stat(folderPath)
	if err != nil || !info.IsDir() {
		return infra.WithCause(errors.New("INVALID_CHARACTER_PATH"), err)
	}
	if assignment != nil {
		if _, ok := character.Lookup(assignment.Game, assignment.ID); !ok {
			return errors.New("CHARACTER_NOT_FOUND")
		}
	}

	stage = "metadata"
	change := func(raw []byte) ([]byte, error) {
		document := make(map[string]json.RawMessage)
		if raw != nil {
			if err := json.Unmarshal(raw, &document); err != nil {
				return nil, fmt.Errorf("decode folder metadata: %w", err)
			}
			if document == nil {
				return nil, errors.New("invalid folder metadata object")
			}
		}
		if assignment == nil {
			delete(document, characterMetadataKey)
			return json.Marshal(document)
		}
		encoded, err := json.Marshal(assignment)
		if err != nil {
			return nil, err
		}
		document[characterMetadataKey] = encoded
		return json.Marshal(document)
	}
	if assignment != nil {
		return metadata.Upsert(folderPath, change, m.reportMetadataCleanup)
	}
	// Clearing an assignment must not create metadata for a folder that never had any.
	if err := metadata.Update(folderPath, change, m.reportMetadataCleanup); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// FolderCharacter returns the character assigned to the folder itself, or nil.
func (m *Mod) FolderCharacter(ctx context.Context, folderPath string) (assignment *CharacterAssignment, err error) {
	stage := "validate"
	defer func() {
		if err != nil {
			err = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
				Operation: "get-folder-character", Stage: stage, Fields: map[string]any{"folderPath": folderPath},
			})
		}
	}()
	if _, err := m.ownedPath(ctx, folderPath); err != nil {
		return nil, err
	}
	stage = "metadata"
	return readFolderCharacter(folderPath)
}

// ResolveModCharacter returns the assignment of the nearest folder from modPath up to, but not including, its mod
// root, or nil when there is none or modPath lies outside every managed root.
func (m *Mod) ResolveModCharacter(ctx context.Context, modPath string) (*ModCharacter, error) {
	owned, err := m.ownedRoot(ctx, modPath)
	if err != nil {
		if err.Error() == "MOD_PATH_OUTSIDE_MANAGED_ROOT" {
			return nil, nil
		}
		return nil, infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
			Operation: "resolve-mod-character", Stage: "match-root", Fields: map[string]any{"modPath": modPath},
		})
	}
	for relative := owned.relative; relative != "." && relative != ""; relative = filepath.Dir(relative) {
		if escapesRoot(filepath.ToSlash(relative)) {
			break
		}
		folder := filepath.Join(owned.root.path, relative)
		assignment, err := readFolderCharacter(folder)
		if err != nil {
			m.pathDiagnostic.Report(m.log, err, "Mod", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "resolve-mod-character", Stage: "read-metadata",
				Fields: map[string]any{"path": folder, "modPath": modPath},
			})
			continue
		}
		if assignment != nil {
			return &ModCharacter{CharacterAssignment: *assignment, Folder: folder}, nil
		}
	}
	return nil, nil
}

func readFolderCharacter(folderPath string) (*CharacterAssignment, error) {
	raw, err := metadata.Read(folderPath)
	if errors.Is(err, os.ErrNotExist) || raw == nil && err == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode folder metadata: %w", err)
	}
	encoded, ok := document[characterMetadataKey]
	if !ok {
		return nil, nil
	}
	var assignment CharacterAssignment
	if err := json.Unmarshal(encoded, &assignment); err != nil {
		return nil, fmt.Errorf("decode folder character: %w", err)
	}
	if assignment.Game == "" || assignment.ID == "" {
		return nil, nil
	}
	return &assignment, nil
}
