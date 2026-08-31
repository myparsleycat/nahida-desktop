package mod

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nahida.live/desktop/internal/db"
)

const modPresetVersion int64 = 2

type Preset struct {
	ID          string  `json:"id"`
	Game        string  `json:"game"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	Version     int64   `json:"version"`
	IsLegacy    bool    `json:"isLegacy"`
}

type PresetConflictCandidate struct {
	ActualPath   string `json:"actualPath"`
	RelativePath string `json:"relativePath"`
	FolderName   string `json:"folderName"`
	IsEnabled    bool   `json:"isEnabled"`
}

type PresetConflict struct {
	ModKey     string                    `json:"modKey"`
	Candidates []PresetConflictCandidate `json:"candidates"`
}

type MissingPresetItem struct {
	ModKey               string `json:"modKey"`
	ExpectedFolderName   string `json:"expectedFolderName"`
	ExpectedRelativePath string `json:"expectedRelativePath"`
}

type ApplyPresetResult struct {
	PresetID string              `json:"presetId"`
	Applied  []string            `json:"applied"`
	Skipped  []string            `json:"skipped"`
	Missing  []MissingPresetItem `json:"missing"`
}

type presetSnapshotItem struct {
	ModKey            string
	RelativePath      string
	GroupRelativePath string
	FolderName        string
	IsEnabled         bool
	ActualPath        string
}

func (m *Mod) GetPresets(ctx context.Context, game string) ([]Preset, error) {
	client, err := m.requireClient()
	if err != nil {
		return nil, err
	}
	rows, err := client.ModPresets.ListByGame(ctx, game)
	if err != nil {
		return nil, err
	}
	result := make([]Preset, len(rows))
	for i := range rows {
		result[i] = presetFromRow(rows[i])
	}
	less := newLocaleLess()
	sort.SliceStable(result, func(i, j int) bool { return less(result[i].Name, result[j].Name) })
	return result, nil
}

func (m *Mod) GetPresetCreateConflicts(ctx context.Context, game string) ([]PresetConflict, error) {
	items, err := m.presetSnapshot(ctx, game)
	if err != nil {
		return nil, err
	}
	return snapshotConflicts(items), nil
}

func (m *Mod) CreatePreset(
	ctx context.Context,
	game, name string,
	description *string,
	resolveConflicts bool,
) (Preset, error) {
	client, err := m.requireClient()
	if err != nil {
		return Preset{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Preset{}, errors.New("INVALID_PRESET_NAME")
	}
	description = cleanOptional(description)
	existing, err := client.ModPresets.FindByGameAndName(ctx, game, name)
	if err != nil {
		return Preset{}, err
	}
	if existing != nil {
		return Preset{}, errors.New("PRESET_NAME_EXISTS")
	}
	items, err := m.presetSnapshot(ctx, game)
	if err != nil {
		return Preset{}, err
	}
	conflicts := snapshotConflicts(items)
	if len(conflicts) > 0 && !resolveConflicts {
		return Preset{}, errors.New("PRESET_CONFLICTS_EXIST")
	}
	if len(conflicts) > 0 {
		if err := resolveSnapshotConflicts(conflicts); err != nil {
			return Preset{}, err
		}
		items, err = m.presetSnapshot(ctx, game)
		if err != nil {
			return Preset{}, err
		}
		if len(snapshotConflicts(items)) > 0 {
			return Preset{}, errors.New("PRESET_CONFLICT_RESOLUTION_FAILED")
		}
	}
	id, err := newPresetID()
	if err != nil {
		return Preset{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := db.ModPresetRow{
		ID: id, Game: game, Name: name, Description: description, ItemCount: int64(len(items)),
		CreatedAt: now, UpdatedAt: now, Version: modPresetVersion,
	}
	dbItems := make([]db.ModPresetItemRow, len(items))
	for i, item := range items {
		dbItems[i] = db.ModPresetItemRow{
			PresetID: id, ModKey: item.ModKey, RelativePath: item.RelativePath,
			GroupRelativePath: item.GroupRelativePath, FolderName: item.FolderName,
			IsEnabled: item.IsEnabled, ItemOrder: int64(i),
		}
	}
	if err := client.ModPresets.InsertSnapshot(ctx, row, dbItems); err != nil {
		return Preset{}, err
	}
	return presetFromRow(row), nil
}

func (m *Mod) ApplyPreset(ctx context.Context, presetID string) (ApplyPresetResult, error) {
	client, err := m.requireClient()
	if err != nil {
		return ApplyPresetResult{}, err
	}
	preset, err := client.ModPresets.FindByID(ctx, presetID)
	if err != nil {
		return ApplyPresetResult{}, err
	}
	if preset == nil {
		return ApplyPresetResult{}, fmt.Errorf("preset %s not found", presetID)
	}
	if preset.Version < modPresetVersion {
		return ApplyPresetResult{}, errors.New("LEGACY_PRESET_NOT_SUPPORTED")
	}
	stored, err := client.ModPresetItems.ListByPresetID(ctx, presetID)
	if err != nil {
		return ApplyPresetResult{}, err
	}
	current, err := m.presetSnapshot(ctx, preset.Game)
	if err != nil {
		return ApplyPresetResult{}, err
	}
	byKey := make(map[string]presetSnapshotItem, len(current))
	byRelative := make(map[string]presetSnapshotItem, len(current))
	for _, item := range current {
		byKey[item.ModKey] = item
		byRelative[strings.ToLower(item.RelativePath)] = item
	}
	result := ApplyPresetResult{
		PresetID: presetID, Applied: []string{}, Skipped: []string{}, Missing: []MissingPresetItem{},
	}
	sort.Slice(stored, func(i, j int) bool { return stored[i].ItemOrder < stored[j].ItemOrder })
	for _, wanted := range stored {
		item, ok := byKey[wanted.ModKey]
		if !ok {
			item, ok = byRelative[strings.ToLower(wanted.RelativePath)]
		}
		if !ok {
			result.Missing = append(result.Missing, MissingPresetItem{
				ModKey: wanted.ModKey, ExpectedFolderName: wanted.FolderName,
				ExpectedRelativePath: wanted.RelativePath,
			})
			continue
		}
		if item.IsEnabled == wanted.IsEnabled {
			result.Skipped = append(result.Skipped, item.RelativePath)
			continue
		}
		if m.isActiveDownloadDestination(item.ActualPath) {
			result.Skipped = append(result.Skipped, item.RelativePath)
			continue
		}
		var actionErr error
		if wanted.IsEnabled {
			_, actionErr = m.Enable(ctx, item.ActualPath)
		} else {
			_, actionErr = m.Disable(ctx, item.ActualPath)
		}
		if actionErr != nil {
			if actionErr.Error() == "MOD_DOWNLOAD_IN_PROGRESS" {
				result.Skipped = append(result.Skipped, item.RelativePath)
			}
			continue
		}
		result.Applied = append(result.Applied, item.RelativePath)
	}
	return result, nil
}

func (m *Mod) DeletePreset(ctx context.Context, presetID string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	return client.ModPresets.Delete(ctx, presetID)
}

func (m *Mod) UpdatePresetName(ctx context.Context, presetID, newName string) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	preset, err := client.ModPresets.FindByID(ctx, presetID)
	if err != nil {
		return err
	}
	if preset == nil {
		return fmt.Errorf("preset %s not found", presetID)
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("INVALID_PRESET_NAME")
	}
	existing, err := client.ModPresets.FindByGameAndName(ctx, preset.Game, newName)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != presetID {
		return errors.New("PRESET_NAME_EXISTS")
	}
	return client.ModPresets.UpdateName(ctx, presetID, newName, time.Now().UTC().Format(time.RFC3339Nano))
}

func (m *Mod) presetSnapshot(ctx context.Context, game string) ([]presetSnapshotItem, error) {
	gamePath, err := m.GetGamePath(ctx, game)
	if err != nil {
		return nil, err
	}
	if gamePath == nil {
		return nil, fmt.Errorf("no mod folder path set for %s", game)
	}
	paths := map[string]string{}
	err = filepath.WalkDir(*gamePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ".ini") {
			parent := filepath.Dir(path)
			key := strings.ToLower(filepath.Clean(parent))
			if _, exists := paths[key]; !exists {
				paths[key] = parent
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	items := make([]presetSnapshotItem, 0, len(paths))
	for _, modPath := range paths {
		groupPath := filepath.Dir(modPath)
		groupRelative := normalizedGameRelativePath(*gamePath, groupPath)
		relative := normalizedGameRelativePath(*gamePath, modPath)
		items = append(items, presetSnapshotItem{
			ModKey: groupRelative + "::" + relative, RelativePath: relative,
			GroupRelativePath: groupRelative, FolderName: stripDisabled(filepath.Base(modPath)),
			IsEnabled: !isDisabled(filepath.Base(modPath)), ActualPath: modPath,
		})
	}
	less := newLocaleLess()
	sort.SliceStable(items, func(i, j int) bool { return less(items[i].RelativePath, items[j].RelativePath) })
	return items, nil
}

func snapshotConflicts(items []presetSnapshotItem) []PresetConflict {
	byKey := map[string][]presetSnapshotItem{}
	for _, item := range items {
		byKey[item.ModKey] = append(byKey[item.ModKey], item)
	}
	result := []PresetConflict{}
	for key, values := range byKey {
		if len(values) < 2 {
			continue
		}
		less := newLocaleLess()
		sort.SliceStable(values, func(i, j int) bool { return less(values[i].RelativePath, values[j].RelativePath) })
		conflict := PresetConflict{ModKey: key, Candidates: make([]PresetConflictCandidate, len(values))}
		for i, item := range values {
			conflict.Candidates[i] = PresetConflictCandidate{
				ActualPath: item.ActualPath, RelativePath: item.RelativePath,
				FolderName: item.FolderName, IsEnabled: item.IsEnabled,
			}
		}
		result = append(result, conflict)
	}
	less := newLocaleLess()
	sort.SliceStable(result, func(i, j int) bool { return less(result[i].ModKey, result[j].ModKey) })
	return result
}

func resolveSnapshotConflicts(conflicts []PresetConflict) error {
	for _, conflict := range conflicts {
		enabled := 0
		for _, candidate := range conflict.Candidates {
			if candidate.IsEnabled {
				enabled++
			}
		}
		disabledSeen := 0
		for _, candidate := range conflict.Candidates {
			if candidate.IsEnabled {
				continue
			}
			disabledSeen++
			if enabled == 0 && disabledSeen == 1 {
				continue
			}
			baseName := restoreDisabledPrefix(filepath.Base(candidate.ActualPath), candidate.FolderName)
			if _, err := renameUnique(candidate.ActualPath, baseName); err != nil {
				return err
			}
		}
	}
	return nil
}

func presetFromRow(row db.ModPresetRow) Preset {
	return Preset{
		ID: row.ID, Game: row.Game, Name: row.Name, Description: row.Description,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Version: row.Version,
		IsLegacy: row.Version < modPresetVersion,
	}
}

func newPresetID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
