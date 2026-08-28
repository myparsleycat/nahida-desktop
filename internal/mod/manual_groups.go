package mod

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type manualSubGroups map[string][]string

func (m *Mod) SetManualSubGroup(ctx context.Context, modPath string, enabled bool) error {
	game, err := m.ownedPath(ctx, modPath)
	if err != nil {
		return errors.New("INVALID_MANUAL_SUBGROUP_PATH")
	}
	relative := manualRelativePath(gameRelativePath(game.ModFolderPath, modPath))
	if relative == "" {
		return errors.New("INVALID_MANUAL_SUBGROUP_PATH")
	}
	info, err := os.Stat(modPath)
	if err != nil || !info.IsDir() {
		return errors.New("INVALID_MANUAL_SUBGROUP_PATH")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	groups, err := m.loadManualSubGroups(ctx)
	if err != nil {
		return err
	}
	current := make(map[string]struct{}, len(groups[game.Game]))
	for _, value := range groups[game.Game] {
		current[value] = struct{}{}
	}
	if enabled {
		current[relative] = struct{}{}
	} else {
		delete(current, relative)
	}
	next := make([]string, 0, len(current))
	for value := range current {
		next = append(next, value)
	}
	sortLocaleStrings(next)
	if len(next) == 0 {
		delete(groups, game.Game)
	} else {
		groups[game.Game] = next
	}
	return m.saveManualSubGroups(ctx, groups)
}

func (m *Mod) GetManualSubGroups(
	ctx context.Context,
	folderPath string,
	searchModPreview *bool,
) ([]FolderGroup, error) {
	game, err := m.ownedPath(ctx, folderPath)
	if err != nil {
		return nil, err
	}
	search, err := m.resolvePreviewSetting(ctx, searchModPreview)
	if err != nil {
		return nil, err
	}
	parentRelative := manualRelativePath(gameRelativePath(game.ModFolderPath, folderPath))
	children, err := m.manualChildPaths(ctx, *game, parentRelative)
	if err != nil || len(children) == 0 {
		return []FolderGroup{}, err
	}
	groups := listGroups(folderPath, search)
	result := make([]FolderGroup, 0, len(groups))
	for _, group := range groups {
		relative := joinManualPath(parentRelative, filepath.Base(group.Path))
		if _, ok := children[relative]; ok {
			group.IsManualSubGroup = true
			result = append(result, group)
		}
	}
	return result, nil
}

func (m *Mod) decorateGroups(
	ctx context.Context,
	gameName, parentRelative string,
	groups []FolderGroup,
) []FolderGroup {
	games, err := m.GetGames(ctx)
	if err != nil {
		return groups
	}
	var game *GameConfig
	for i := range games {
		if games[i].Game == gameName {
			game = &games[i]
			break
		}
	}
	if game == nil {
		return groups
	}
	children, _ := m.manualChildPaths(ctx, *game, parentRelative)
	for i := range groups {
		relative := joinManualPath(parentRelative, filepath.Base(groups[i].Path))
		_, groups[i].IsManualSubGroup = children[relative]
		ownChildren, _ := m.manualChildPaths(ctx, *game, relative)
		groups[i].HasManualSubGroups = len(ownChildren) > 0
		for manualPath := range ownChildren {
			for _, diskPath := range resolveManualDiskPaths(game.ModFolderPath, manualPath) {
				if !hasAnyFile(diskPath) {
					continue
				}
				if groups[i].ModCount > 0 {
					groups[i].ModCount--
				}
				if !isDisabled(filepath.Base(diskPath)) && groups[i].EnabledModCount > 0 {
					groups[i].EnabledModCount--
				}
			}
		}
	}
	return groups
}

func (m *Mod) filterManualMods(
	ctx context.Context,
	game GameConfig,
	groupPath string,
	group FolderGroup,
) FolderGroup {
	relative := manualRelativePath(gameRelativePath(game.ModFolderPath, groupPath))
	children, _ := m.manualChildPaths(ctx, game, relative)
	group.HasManualSubGroups = len(children) > 0
	if len(children) == 0 {
		return group
	}
	mods := make([]ModInfo, 0, len(group.Mods))
	for _, info := range group.Mods {
		fullRelative := joinManualPath(relative, filepath.Base(info.Path))
		if _, hidden := children[fullRelative]; !hidden {
			mods = append(mods, info)
		}
	}
	group.Mods = mods
	group.ModCount = len(mods)
	group.EnabledModCount = 0
	for _, info := range mods {
		if info.IsEnabled {
			group.EnabledModCount++
		}
	}
	return group
}

func (m *Mod) manualChildPaths(
	ctx context.Context,
	game GameConfig,
	parentRelative string,
) (map[string]struct{}, error) {
	groups, err := m.loadManualSubGroups(ctx)
	if err != nil {
		return nil, err
	}
	parentRelative = manualRelativePath(parentRelative)
	prefix := ""
	if parentRelative != "" {
		prefix = parentRelative + "/"
	}
	result := map[string]struct{}{}
	for _, candidate := range groups[game.Game] {
		if !strings.HasPrefix(candidate, prefix) || strings.Contains(candidate[len(prefix):], "/") {
			continue
		}
		if len(resolveManualDiskPaths(game.ModFolderPath, candidate)) > 0 {
			result[candidate] = struct{}{}
		}
	}
	return result, nil
}

func (m *Mod) loadManualSubGroups(ctx context.Context) (manualSubGroups, error) {
	value, err := m.settingValue(ctx, manualSubGroupsSettingKey)
	if err != nil || value == nil {
		return manualSubGroups{}, err
	}
	return decodeManualSubGroups(*value), nil
}

func decodeManualSubGroups(value string) manualSubGroups {
	var stored map[string][]string
	if err := json.Unmarshal([]byte(value), &stored); err != nil {
		return manualSubGroups{}
	}
	result := manualSubGroups{}
	for game, paths := range stored {
		seen := map[string]struct{}{}
		for _, path := range paths {
			normalized := manualRelativePath(path)
			if normalized != "" {
				seen[normalized] = struct{}{}
			}
		}
		for path := range seen {
			result[game] = append(result[game], path)
		}
		sortLocaleStrings(result[game])
	}
	return result
}

func (m *Mod) saveManualSubGroups(ctx context.Context, groups manualSubGroups) error {
	client, err := m.requireClient()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	value := string(raw)
	return client.Settings.Upsert(ctx, manualSubGroupsSettingKey, &value)
}

func resolveManualDiskPaths(root, relative string) []string {
	current := []string{filepath.Clean(root)}
	for _, storedSegment := range strings.Split(manualRelativePath(relative), "/") {
		if storedSegment == "" {
			continue
		}
		next := []string{}
		for _, parent := range current {
			entries, err := os.ReadDir(parent)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() && manualSegmentMatches(entry.Name(), storedSegment) {
					next = append(next, filepath.Join(parent, entry.Name()))
				}
			}
		}
		current = next
	}
	return current
}

func manualSegmentMatches(entryName, storedSegment string) bool {
	return strings.EqualFold(entryName, storedSegment) || strings.EqualFold(stripDisabled(entryName), storedSegment)
}

func gameRelativePath(root, target string) string {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(relative)
}

func normalizedGameRelativePath(root, target string) string {
	return normalizeRelativePath(gameRelativePath(root, target))
}

func manualRelativePath(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	return strings.Join(parts, "/")
}

func joinManualPath(parent, child string) string {
	if parent == "" {
		return manualRelativePath(child)
	}
	return manualRelativePath(parent + "/" + child)
}
