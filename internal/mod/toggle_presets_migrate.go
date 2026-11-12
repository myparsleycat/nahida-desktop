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

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod/metadata"
	"nahida.live/desktop/internal/platform"
)

// TogglePresetMigration reports a move of toggle presets to another store. Presets listed in Orphaned,
// Ambiguous or Failed stay in the old store, and Leftover counts mods whose copies could not be removed from it.
type TogglePresetMigration struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Mods      int      `json:"mods"`
	Moved     int      `json:"moved"`
	Renamed   int      `json:"renamed"`
	Leftover  int      `json:"leftover"`
	Orphaned  []string `json:"orphaned"`
	Ambiguous []string `json:"ambiguous"`
	Failed    []string `json:"failed"`
}

type togglePresetGroup struct {
	ref     toggleModRef
	presets []TogglePreset
}

type togglePresetCopy struct {
	sourceIDs []string
	presets   []TogglePreset
	renamed   int
	remapped  int
}

// SetTogglePresetStore chooses where toggle presets are stored. Choosing for the first time only records the
// choice; switching copies every preset to the new store, verifies the copies, records the choice, and then
// removes the presets from the old store. A copy that fails leaves the choice unchanged.
func (m *Mod) SetTogglePresetStore(ctx context.Context, store string) (TogglePresetMigration, error) {
	m.togglePresetMu.Lock()
	defer m.togglePresetMu.Unlock()

	if store != TogglePresetStoreMetadata && store != TogglePresetStoreDatabase {
		return TogglePresetMigration{}, ErrInvalidTogglePresetStore
	}
	settings, ok := m.settings.(togglePresetSettings)
	if !ok {
		return TogglePresetMigration{}, ErrTogglePresetUnavailable
	}
	current, err := settings.GetTogglePresetStore(ctx)
	if err != nil {
		return TogglePresetMigration{}, err
	}
	migration := TogglePresetMigration{
		From: current, To: store, Orphaned: []string{}, Ambiguous: []string{}, Failed: []string{},
	}
	if current == store {
		return migration, nil
	}
	if current == "" {
		return migration, settings.SetTogglePresetStore(ctx, store)
	}

	stage := "list"
	report := func(err error) error {
		return infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
			Operation: "toggle-preset.set-store", Stage: stage,
			Fields: map[string]any{"from": current, "to": store, "moved": migration.Moved,
				"failed": len(migration.Failed)},
		})
	}
	groups, err := m.togglePresetGroups(ctx, current, &migration)
	if err != nil {
		return migration, report(err)
	}

	stage = "copy"
	copies := make([]togglePresetCopy, len(groups))
	remapped := 0
	for i, group := range groups {
		if err := ctx.Err(); err != nil {
			return migration, err
		}
		copied, err := m.copyTogglePresets(ctx, store, group)
		if err != nil {
			if infra.IsCancellationError(err) {
				return migration, err
			}
			migration.Failed = append(migration.Failed, group.ref.path)
			_ = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "toggle-preset.set-store", Stage: "copy",
				Fields: map[string]any{"modPath": group.ref.path, "game": group.ref.game, "modKey": group.ref.key,
					"from": current, "to": store, "presets": len(group.presets)},
			})
			continue
		}
		copies[i] = copied
		migration.Renamed += copied.renamed
		remapped += copied.remapped
	}
	if len(migration.Failed) > 0 {
		return migration, fmt.Errorf("TOGGLE_PRESET_MIGRATION_FAILED:%d", len(migration.Failed))
	}

	stage = "verify"
	for i, group := range groups {
		if err := m.verifyTogglePresetCopy(ctx, store, group.ref, copies[i].presets); err != nil {
			if infra.IsCancellationError(err) {
				return migration, err
			}
			migration.Failed = append(migration.Failed, group.ref.path)
			failure := fmt.Errorf("TOGGLE_PRESET_MIGRATION_FAILED:%d: %w", len(migration.Failed), err)
			return migration, report(infra.AnnotateError(failure, infra.Diagnostic{
				Fields: map[string]any{"modPath": group.ref.path, "game": group.ref.game, "modKey": group.ref.key},
			}))
		}
	}

	stage = "switch"
	if err := settings.SetTogglePresetStore(ctx, store); err != nil {
		return migration, report(err)
	}

	// The new store now holds every preset; a copy left in the old one is unused.
	cleanup := &infra.DiagnosticBatch{}
	for i, group := range groups {
		migration.Mods++
		migration.Moved += len(copies[i].sourceIDs)
		if err := m.removeTogglePresets(
			context.WithoutCancel(ctx),
			current,
			group.ref,
			copies[i].sourceIDs,
		); err != nil {
			migration.Leftover++
			cleanup.Add(infra.AnnotateError(err, infra.Diagnostic{
				Fields: map[string]any{"modPath": group.ref.path, "from": current},
			}))
		}
	}
	cleanup.Report(m.log, "Mod", "toggle-preset.set-store.cleanup")
	m.log.Info(fmt.Sprintf(
		"Moved %d toggle presets of %d mods from %s to %s (renamed %d, remapped %d, orphaned %d, ambiguous %d, leftover %d)",
		migration.Moved,
		migration.Mods,
		current,
		store,
		migration.Renamed,
		remapped,
		len(migration.Orphaned),
		len(migration.Ambiguous),
		migration.Leftover,
	), "Mod")
	return migration, nil
}

// togglePresetGroups lists the presets the store holds, by mod.
func (m *Mod) togglePresetGroups(
	ctx context.Context,
	store string,
	migration *TogglePresetMigration,
) ([]togglePresetGroup, error) {
	games, err := m.listGames(ctx)
	if err != nil {
		return nil, err
	}
	roots, err := uniqueToggleRoots(ctx, games)
	if err != nil {
		return nil, err
	}
	var groups []togglePresetGroup

	if store == TogglePresetStoreDatabase {
		client, err := m.requireClient()
		if err != nil {
			return nil, err
		}
		rows, err := client.ModTogglePresets.ListAll(ctx)
		if err != nil {
			return nil, err
		}
		byMod := map[[2]string][]TogglePreset{}
		var order [][2]string
		for _, row := range rows {
			key := [2]string{row.Game, row.ModKey}
			if _, seen := byMod[key]; !seen {
				order = append(order, key)
			}
			preset, err := togglePresetFromRow(row)
			if err != nil {
				return nil, err
			}
			byMod[key] = append(byMod[key], preset)
		}
		for _, key := range order {
			label := key[0] + "/" + key[1]
			var found []string
			for _, root := range roots {
				if games[root.game].Game != key[0] {
					continue
				}
				paths, err := resolveToggleDiskPaths(ctx, root.path, key[1])
				if err != nil {
					return nil, err
				}
				for _, path := range paths {
					if !slices.ContainsFunc(
						found,
						func(other string) bool { return platform.SamePathFold(other, path) },
					) {
						found = append(found, path)
					}
				}
			}
			switch len(found) {
			case 0:
				migration.Orphaned = append(migration.Orphaned, label)
			case 1:
				groups = append(groups, togglePresetGroup{
					ref: toggleModRef{
						game: key[0], path: found[0], key: key[1], name: stripDisabled(filepath.Base(found[0])),
					},
					presets: byMod[key],
				})
			default:
				migration.Ambiguous = append(migration.Ambiguous, label)
			}
		}
		return groups, validateTogglePresetTargets(ctx, groups)
	}

	for _, root := range roots {
		dirs, err := metadataDirs(ctx, root.path)
		if err != nil {
			return nil, err
		}
		for _, dir := range dirs {
			raw, err := metadata.Read(dir)
			if err != nil {
				return nil, fmt.Errorf("read metadata of %s: %w", dir, err)
			}
			_, presets, err := decodeMetadataTogglePresets(raw)
			if err != nil {
				return nil, fmt.Errorf("read metadata of %s: %w", dir, err)
			}
			if len(presets) == 0 {
				continue
			}
			relative, err := filepath.Rel(root.path, dir)
			if err != nil {
				return nil, err
			}
			groups = append(groups, togglePresetGroup{
				ref: toggleModRef{
					game: games[root.game].Game, path: dir, key: normalizeRelativePath(relative),
					name: stripDisabled(filepath.Base(dir)),
				},
				presets: presets,
			})
		}
	}
	return groups, validateTogglePresetTargets(ctx, groups)
}

// uniqueToggleRoots drops a root that is another root's physical location, so a linked mod folder is walked once.
func uniqueToggleRoots(ctx context.Context, games []GameConfig) ([]managedRoot, error) {
	var unique []managedRoot
	var physical []string
	for i, game := range games {
		paths := []string{game.ModFolderPath}
		if game.LinkedModFolderPath != nil {
			paths = append(paths, *game.LinkedModFolderPath)
		}
		for _, path := range paths {
			location, _, _, err := readTogglePresetDir(ctx, path)
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(
				physical,
				func(other string) bool { return platform.SamePathFold(other, location) },
			) {
				continue
			}
			physical = append(physical, location)
			unique = append(unique, managedRoot{game: i, path: path, resolved: location})
		}
	}
	return unique, nil
}

// metadataDirs lists the folders below root that hold an nhd.json. Folders whose name starts with a dot are
// skipped.
func metadataDirs(ctx context.Context, root string) ([]string, error) {
	var dirs []string
	var walk func(string, []string) error
	walk = func(path string, ancestors []string) error {
		physical, children, metadata, err := readTogglePresetDir(ctx, path)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(ancestors, func(other string) bool { return platform.SamePathFold(other, physical) }) {
			return nil
		}
		if metadata && !platform.SamePathFold(path, root) {
			dirs = append(dirs, path)
		}
		ancestors = append(ancestors, physical)
		for _, child := range children {
			if err := walk(child, ancestors); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, nil); err != nil {
		return nil, err
	}
	return dirs, nil
}

func readTogglePresetDir(ctx context.Context, path string) (string, []string, bool, error) {
	release, err := diskio.AcquireDir(ctx, path)
	if err != nil {
		return "", nil, false, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return "", nil, false, err
	}
	physical, err := platform.FinalPath(path)
	if err != nil {
		return "", nil, false, fmt.Errorf("resolve toggle preset directory %s: %w", path, err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", nil, false, fmt.Errorf("list toggle preset directory %s: %w", path, err)
	}
	var children []string
	metadata := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", nil, false, err
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		child := filepath.Join(path, entry.Name())
		isDir := entry.IsDir()
		regular := entry.Type().IsRegular()
		if !isDir && !regular {
			info, err := os.Stat(child)
			if err != nil {
				return "", nil, false, fmt.Errorf("inspect toggle preset entry %s: %w", child, err)
			}
			isDir, regular = info.IsDir(), info.Mode().IsRegular()
		}
		if isDir {
			children = append(children, child)
		} else if regular && strings.EqualFold(entry.Name(), "nhd.json") {
			metadata = true
		}
	}
	return physical, children, metadata, nil
}

func resolveToggleDiskPaths(ctx context.Context, root, relative string) ([]string, error) {
	current := []string{root}
	for _, segment := range strings.Split(manualRelativePath(relative), "/") {
		if segment == "" {
			continue
		}
		var next []string
		for _, parent := range current {
			_, children, _, err := readTogglePresetDir(ctx, parent)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if manualSegmentMatches(filepath.Base(child), segment) {
					next = append(next, child)
				}
			}
		}
		current = next
	}
	return current, nil
}

func validateTogglePresetTargets(ctx context.Context, groups []togglePresetGroup) error {
	physical := make([]string, 0, len(groups))
	for i, group := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(group.ref.path, "nhd.json")
		location, err := func() (string, error) {
			release, err := diskio.AcquireDir(ctx, group.ref.path)
			if err != nil {
				return "", err
			}
			defer release()
			location, err := platform.FinalPath(path)
			if errors.Is(err, os.ErrNotExist) {
				dir, resolveErr := platform.FinalPath(group.ref.path)
				if resolveErr != nil {
					return "", resolveErr
				}
				return filepath.Join(dir, "nhd.json"), nil
			}
			return location, err
		}()
		if err != nil {
			return fmt.Errorf("resolve toggle preset metadata %s: %w", path, err)
		}
		for j, other := range physical {
			if platform.SamePathFold(other, location) {
				return fmt.Errorf(
					"toggle preset metadata is shared by %s and %s",
					groups[j].ref.path,
					groups[i].ref.path,
				)
			}
		}
		physical = append(physical, location)
	}
	return nil
}

// Source IDs remain separate from the destination IDs so cleanup removes the original metadata after remapping.
func (m *Mod) copyTogglePresets(ctx context.Context, store string, group togglePresetGroup) (togglePresetCopy, error) {
	copied := togglePresetCopy{sourceIDs: make([]string, len(group.presets))}
	for i, preset := range group.presets {
		copied.sourceIDs[i] = preset.ID
	}
	ids := map[string]string{}
	if store == TogglePresetStoreDatabase {
		client, err := m.requireClient()
		if err != nil {
			return copied, err
		}
		ids, err = client.ModTogglePresets.ResolveImportIDs(ctx, group.ref.game, group.ref.key, copied.sourceIDs)
		if err != nil {
			return copied, err
		}
	}
	err := m.updateTogglePresets(ctx, store, group.ref, func(presets []TogglePreset) ([]TogglePreset, error) {
		copied.renamed, copied.remapped, copied.presets = 0, 0, nil
		for _, preset := range group.presets {
			if id, ok := ids[preset.ID]; ok && id != preset.ID {
				preset.ID = id
				copied.remapped++
			}
			presets = slices.DeleteFunc(presets, func(other TogglePreset) bool { return other.ID == preset.ID })
			if togglePresetNameTaken(presets, preset.Name, preset.ID) {
				base := preset.Name
				for n := 2; togglePresetNameTaken(presets, preset.Name, preset.ID); n++ {
					preset.Name = fmt.Sprintf("%s (%d)", base, n)
				}
				copied.renamed++
			}
			presets = append(presets, preset)
			copied.presets = append(copied.presets, preset)
		}
		return presets, nil
	})
	if err != nil {
		return copied, err
	}
	return copied, m.verifyTogglePresetCopy(ctx, store, group.ref, copied.presets)
}

func (m *Mod) verifyTogglePresetCopy(
	ctx context.Context,
	store string,
	ref toggleModRef,
	written []TogglePreset,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stored, err := m.loadTogglePresets(ctx, store, ref)
	if err != nil {
		return err
	}
	for _, want := range written {
		index := slices.IndexFunc(stored, func(preset TogglePreset) bool { return preset.ID == want.ID })
		if index < 0 || !sameTogglePreset(stored[index], want) {
			return fmt.Errorf("toggle preset %s did not read back from %s", want.ID, store)
		}
	}
	return nil
}

func sameTogglePreset(a, b TogglePreset) bool {
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && string(left) == string(right)
}

func (m *Mod) removeTogglePresets(ctx context.Context, store string, ref toggleModRef, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if store == TogglePresetStoreDatabase {
		client, err := m.requireClient()
		if err != nil {
			return err
		}
		return client.ModTogglePresets.DeleteByModIDs(ctx, ref.game, ref.key, ids)
	}
	return metadata.Update(ref.path, func(raw []byte) ([]byte, error) {
		document, presets, err := decodeMetadataTogglePresets(raw)
		if err != nil {
			return nil, err
		}
		presets = slices.DeleteFunc(presets, func(preset TogglePreset) bool { return slices.Contains(ids, preset.ID) })
		return encodeMetadataTogglePresets(document, presets)
	}, m.reportMetadataCleanup)
}
