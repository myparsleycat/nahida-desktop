package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod/metadata"
)

const classificationsMetadataKey = "classifications"

// classifiedSubGroupDepth is how many levels below a top-level folder can carry an assignment.
// The bound keeps the character listing from walking into mod contents.
const classifiedSubGroupDepth = 2

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

// SetCharacterClassification assigns a character folder to a group, or clears the assignment
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
	if relative == "" || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return errors.New("INVALID_CLASSIFICATION_PATH")
	}
	// A deeper assignment would never be found by the listing, so it is refused rather than silently lost.
	if groupID != nil && strings.Count(relative, "/") > classifiedSubGroupDepth {
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
		if err := metadata.Upsert(folderPath, change); err != nil {
			return err
		}
		m.rememberClassifiedFolder(*game, folderPath)
		return nil
	}
	// Clearing an assignment must not create metadata for a folder that never had any.
	if err := metadata.Update(folderPath, change); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// loadCharacterClassifications fills each folder's assignments from its nhd.json.
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
	return attachClassifications(groups, reports...)
}

func attachClassifications(groups []FolderGroup, reports ...func(error)) []FolderGroup {
	assignments := mapParallel(groups, func(group FolderGroup) map[string]string {
		return readClassificationAssignments(group.Path, reports...)
	})
	for i := range groups {
		groups[i].Classifications = assignments[i]
	}
	return groups
}

// classifiedFolderIndex remembers which nested folders of a game carry an assignment, so listing the
// characters does not search every folder for them again. It lives for the session only; nhd.json stays
// the source of truth.
type classifiedFolderIndex struct {
	root string
	keys map[string]struct{}
}

// classifiedFolderKey identifies a nested folder across case changes and the DISABLED prefix.
// Top-level folders and folders below the depth limit have no key.
func classifiedFolderKey(root, folderPath string) string {
	parts := strings.Split(manualRelativePath(gameRelativePath(root, folderPath)), "/")
	if len(parts) < 2 || len(parts) > classifiedSubGroupDepth+1 || parts[0] == ".." {
		return ""
	}
	for i := range parts {
		parts[i] = stripDisabled(parts[i])
	}
	return strings.Join(parts, "/")
}

func (m *Mod) classifiedFolderKeys(game GameConfig) ([]string, bool) {
	m.classifiedMu.Lock()
	defer m.classifiedMu.Unlock()
	index, ok := m.classifiedFolders[game.Game]
	if !ok || index.root != game.ModFolderPath {
		return nil, false
	}
	return lo.Keys(index.keys), true
}

func (m *Mod) storeClassifiedFolders(game GameConfig, folders []FolderGroup) {
	keys := make(map[string]struct{}, len(folders))
	for _, folder := range folders {
		if key := classifiedFolderKey(game.ModFolderPath, folder.Path); key != "" {
			keys[key] = struct{}{}
		}
	}

	m.classifiedMu.Lock()
	defer m.classifiedMu.Unlock()
	if m.classifiedFolders == nil {
		m.classifiedFolders = map[string]classifiedFolderIndex{}
	}
	m.classifiedFolders[game.Game] = classifiedFolderIndex{root: game.ModFolderPath, keys: keys}
}

// rememberClassifiedFolder adds a folder seen with an assignment to an index that was already built.
// Without an index the next character listing searches the folders anyway.
func (m *Mod) rememberClassifiedFolder(game GameConfig, folderPath string) {
	key := classifiedFolderKey(game.ModFolderPath, folderPath)
	if key == "" {
		return
	}

	m.classifiedMu.Lock()
	defer m.classifiedMu.Unlock()
	if index, ok := m.classifiedFolders[game.Game]; ok && index.root == game.ModFolderPath {
		index.keys[key] = struct{}{}
	}
}

// loadClassifiedSubGroups attaches to each top-level folder the nested folders that carry an assignment,
// so the sidebar can list them under their classification group without expanding the parent.
func (m *Mod) loadClassifiedSubGroups(
	listing groupListing,
	groups []FolderGroup,
	reports ...func(error),
) []FolderGroup {
	if !listing.classified {
		return groups
	}
	game := listing.game

	keys, indexed := m.classifiedFolderKeys(game)
	candidates := lo.FlatMap(keys, func(key string, _ int) []string {
		return resolveManualDiskPaths(game.ModFolderPath, key)
	})
	folders := m.listClassifiedFolders(listing, candidates, reports...)
	live := lo.UniqBy(folders, func(folder FolderGroup) string {
		return classifiedFolderKey(game.ModFolderPath, folder.Path)
	})

	// An indexed folder without an assignment was renamed, moved or cleared since it was indexed,
	// so the assignment may now live in a folder the index does not know.
	if !indexed || len(live) < len(keys) {
		folders = m.listClassifiedFolders(listing, findClassifiedFolders(groups, reports...), reports...)
		m.storeClassifiedFolders(game, folders)
	}

	topLevel := make(map[string]int, len(groups))
	for i := range groups {
		topLevel[strings.ToLower(filepath.Base(groups[i].Path))] = i
	}
	for _, folder := range folders {
		name, _, _ := strings.Cut(gameRelativePath(game.ModFolderPath, folder.Path), "/")
		if i, ok := topLevel[strings.ToLower(name)]; ok {
			groups[i].ClassifiedSubGroups = append(groups[i].ClassifiedSubGroups, folder)
		}
	}
	return groups
}

// findClassifiedFolders searches the folders below each group for assignments, reading only nhd.json.
func findClassifiedFolders(groups []FolderGroup, reports ...func(error)) []string {
	return lo.Flatten(mapParallel(groups, func(group FolderGroup) []string {
		var found []string
		var walk func(dir string, remaining int)
		walk = func(dir string, remaining int) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				reportScanFailure(err, reports)
				return
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				child := filepath.Join(dir, entry.Name())
				if len(readClassificationAssignments(child, reports...)) > 0 {
					found = append(found, child)
				}
				if remaining > 1 {
					walk(child, remaining-1)
				}
			}
		}
		walk(group.Path, classifiedSubGroupDepth)
		return found
	}))
}

// listClassifiedFolders builds the sidebar entries for the given folders only, leaving their siblings unread,
// and drops the ones that carry no assignment.
func (m *Mod) listClassifiedFolders(listing groupListing, paths []string, reports ...func(error)) []FolderGroup {
	names := map[string]map[string]struct{}{}
	for _, path := range paths {
		parent := filepath.Dir(path)
		if names[parent] == nil {
			names[parent] = map[string]struct{}{}
		}
		names[parent][strings.ToLower(filepath.Base(path))] = struct{}{}
	}
	parents := lo.Keys(names)
	slices.Sort(parents)

	return lo.Flatten(mapParallel(parents, func(parent string) []FolderGroup {
		listed := m.listSubGroups(listing, parent, func(name string) bool {
			_, ok := names[parent][strings.ToLower(name)]
			return ok
		}, reports...)
		return lo.Filter(listed, func(folder FolderGroup, _ int) bool {
			return len(folder.Classifications) > 0
		})
	}))
}

func readClassificationAssignments(folderPath string, reports ...func(error)) map[string]string {
	raw, err := metadata.Read(folderPath)
	if err != nil {
		reportScanFailure(fmt.Errorf("read classifications for %s: %w", folderPath, err), reports)
		return nil
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		reportScanFailure(fmt.Errorf("decode classifications for %s: %w", folderPath, err), reports)
		return nil
	}
	if found := decodeClassificationAssignments(document[classificationsMetadataKey]); len(found) > 0 {
		return found
	}
	return nil
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
