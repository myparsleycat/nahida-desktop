package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod/metadata"
)

const classificationsMetadataKey = "classifications"

// ClassificationGroup is one bucket of a classification. An empty ID in a save request creates the group.
type ClassificationGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Classification is a user-defined way to group a game's character folders in the sidebar.
type Classification struct {
	ID     string                `json:"id"`
	Game   string                `json:"game"`
	Name   string                `json:"name"`
	Active bool                  `json:"active"`
	Groups []ClassificationGroup `json:"groups"`
}

func (m *Mod) GetClassifications(ctx context.Context, game string) ([]Classification, error) {
	client, err := m.requireClient()
	if err != nil {
		return nil, err
	}
	rows, err := client.ModClassifications.ListByGame(ctx, game)
	if err != nil {
		return nil, err
	}
	return lo.Map(rows, func(row db.ModClassificationWithGroups, _ int) Classification {
		return classificationFromRow(row)
	}), nil
}

// SaveClassification creates a classification when id is nil, otherwise renames it. The supplied groups
// replace the stored ones in order: groups with a known ID are kept, the rest are created or deleted.
func (m *Mod) SaveClassification(
	ctx context.Context,
	game string,
	id *string,
	name string,
	groups []ClassificationGroup,
) (Classification, error) {
	client, err := m.requireClient()
	if err != nil {
		return Classification{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Classification{}, errors.New("INVALID_CLASSIFICATION_NAME")
	}
	gameRow, err := client.GamePaths.GetByGame(ctx, game)
	if err != nil {
		return Classification{}, err
	}
	if gameRow == nil {
		return Classification{}, fmt.Errorf("no mod folder path set for %s", game)
	}
	existing, err := client.ModClassifications.ListByGame(ctx, game)
	if err != nil {
		return Classification{}, err
	}

	row := db.ModClassificationRow{Game: game, Name: name}
	knownGroups := map[string]struct{}{}
	for _, other := range existing {
		if id != nil && other.ID == *id {
			row.ID = other.ID
			for _, group := range other.Groups {
				knownGroups[group.ID] = struct{}{}
			}
			continue
		}
		if other.Name == name {
			return Classification{}, errors.New("CLASSIFICATION_NAME_EXISTS")
		}
		row.ItemOrder = max(row.ItemOrder, other.ItemOrder+1)
	}
	if id != nil && row.ID == "" {
		return Classification{}, errors.New("CLASSIFICATION_NOT_FOUND")
	}
	if row.ID == "" {
		if row.ID, err = newPresetID(); err != nil {
			return Classification{}, err
		}
	}

	groupRows := make([]db.ModClassificationGroupRow, 0, len(groups))
	names := map[string]struct{}{}
	for index, group := range groups {
		groupName := strings.TrimSpace(group.Name)
		if groupName == "" {
			return Classification{}, errors.New("INVALID_CLASSIFICATION_NAME")
		}
		if _, duplicate := names[groupName]; duplicate {
			return Classification{}, errors.New("CLASSIFICATION_GROUP_NAME_EXISTS")
		}
		names[groupName] = struct{}{}
		groupID := group.ID
		if groupID == "" {
			if groupID, err = newPresetID(); err != nil {
				return Classification{}, err
			}
		} else if _, known := knownGroups[groupID]; !known {
			return Classification{}, errors.New("CLASSIFICATION_NOT_FOUND")
		}
		delete(knownGroups, groupID)
		groupRows = append(groupRows, db.ModClassificationGroupRow{
			ID: groupID, ClassificationID: row.ID, Name: groupName, ItemOrder: int64(index),
		})
	}

	if err := client.ModClassifications.Save(ctx, row, groupRows); err != nil {
		return Classification{}, err
	}
	saved, err := client.ModClassifications.FindByID(ctx, row.ID)
	if err != nil {
		return Classification{}, err
	}
	if saved == nil {
		return Classification{}, errors.New("CLASSIFICATION_NOT_FOUND")
	}
	return classificationFromRow(*saved), nil
}

func (m *Mod) DeleteClassification(ctx context.Context, id string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	return client.ModClassifications.Delete(ctx, id)
}

// SetActiveClassification selects the classification the sidebar groups by; nil restores the plain folder list.
func (m *Mod) SetActiveClassification(ctx context.Context, game string, id *string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	if id != nil {
		classification, err := client.ModClassifications.FindByID(ctx, *id)
		if err != nil {
			return err
		}
		if classification == nil || classification.Game != game {
			return errors.New("CLASSIFICATION_NOT_FOUND")
		}
	}
	return client.ModClassifications.SetActive(ctx, game, id)
}

// SetCharacterClassification assigns a top-level character folder to a group, or clears the assignment
// when groupID is nil. The assignment lives in the folder's nhd.json so it survives renames.
func (m *Mod) SetCharacterClassification(
	ctx context.Context,
	folderPath, classificationID string,
	groupID *string,
) (err error) {
	stage := "validate"
	gameName := ""
	defer func() {
		if err != nil {
			err = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
				Operation: "set-character-classification", Stage: stage,
				Fields: map[string]any{
					"folderPath": folderPath, "game": gameName,
					"classificationId": classificationID, "groupId": lo.FromPtr(groupID),
				},
			})
		}
	}()
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	game, err := m.ownedPath(ctx, folderPath)
	if err != nil {
		return infra.WithCause(errors.New("INVALID_CLASSIFICATION_PATH"), err)
	}
	gameName = game.Game
	relative := manualRelativePath(gameRelativePath(game.ModFolderPath, folderPath))
	if relative == "" || relative == "." || relative == ".." || strings.Contains(relative, "/") {
		return errors.New("INVALID_CLASSIFICATION_PATH")
	}
	info, err := os.Stat(folderPath)
	if err != nil || !info.IsDir() {
		return infra.WithCause(errors.New("INVALID_CLASSIFICATION_PATH"), err)
	}

	classification, err := client.ModClassifications.FindByID(ctx, classificationID)
	if err != nil {
		return err
	}
	if classification == nil || classification.Game != game.Game {
		return errors.New("CLASSIFICATION_NOT_FOUND")
	}
	if groupID != nil && !lo.ContainsBy(classification.Groups, func(group db.ModClassificationGroupRow) bool {
		return group.ID == *groupID
	}) {
		return errors.New("CLASSIFICATION_NOT_FOUND")
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
		assignments := decodeClassificationAssignments(document[classificationsMetadataKey])
		if groupID == nil {
			delete(assignments, classificationID)
		} else {
			assignments[classificationID] = *groupID
		}
		if len(assignments) == 0 {
			delete(document, classificationsMetadataKey)
			return json.Marshal(document)
		}
		encoded, err := json.Marshal(assignments)
		if err != nil {
			return nil, err
		}
		document[classificationsMetadataKey] = encoded
		return json.Marshal(document)
	}
	if groupID != nil {
		return metadata.Upsert(folderPath, change)
	}
	// Clearing an assignment must not create metadata for a folder that never had any.
	if err := metadata.Update(folderPath, change); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// loadCharacterClassifications fills each top-level folder's assignments from its nhd.json.
// Games without classifications skip the per-folder reads entirely.
func (m *Mod) loadCharacterClassifications(
	ctx context.Context,
	client *db.Client,
	game string,
	groups []FolderGroup,
	reports ...func(error),
) []FolderGroup {
	classifications, err := client.ModClassifications.ListByGame(ctx, game)
	if err != nil {
		reportScanFailure(fmt.Errorf("list classifications for %s: %w", game, err), reports)
		return groups
	}
	if len(classifications) == 0 {
		return groups
	}
	assignments := mapParallel(groups, func(group FolderGroup) map[string]string {
		raw, err := metadata.Read(group.Path)
		if err != nil {
			reportScanFailure(fmt.Errorf("read classifications for %s: %w", group.Path, err), reports)
			return nil
		}
		var document map[string]json.RawMessage
		if err := json.Unmarshal(raw, &document); err != nil {
			reportScanFailure(fmt.Errorf("decode classifications for %s: %w", group.Path, err), reports)
			return nil
		}
		if found := decodeClassificationAssignments(document[classificationsMetadataKey]); len(found) > 0 {
			return found
		}
		return nil
	})
	for i := range groups {
		groups[i].Classifications = assignments[i]
	}
	return groups
}

func decodeClassificationAssignments(raw json.RawMessage) map[string]string {
	assignments := map[string]string{}
	if len(raw) == 0 {
		return assignments
	}
	// A malformed value is replaced on the next write rather than blocking assignment forever.
	if err := json.Unmarshal(raw, &assignments); err != nil || assignments == nil {
		return map[string]string{}
	}
	return assignments
}

func classificationFromRow(row db.ModClassificationWithGroups) Classification {
	return Classification{
		ID: row.ID, Game: row.Game, Name: row.Name, Active: row.IsActive,
		Groups: lo.Map(row.Groups, func(group db.ModClassificationGroupRow, _ int) ClassificationGroup {
			return ClassificationGroup{ID: group.ID, Name: group.Name}
		}),
	}
}
