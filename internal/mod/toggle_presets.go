package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod/metadata"
	togglepersist "nahida.live/desktop/internal/tools/toggle_persist"
)

const (
	TogglePresetStoreMetadata = "metadata"
	TogglePresetStoreDatabase = "database"

	// TogglePresetSourceViewer marks a preset saved from the model viewer's state.
	TogglePresetSourceViewer = "viewer"

	togglePresetsMetadataKey = "togglePresets"
)

var (
	ErrTogglePresetNotFound      = errors.New("TOGGLE_PRESET_NOT_FOUND")
	ErrTogglePresetStoreUnset    = errors.New("TOGGLE_PRESET_STORE_UNSET")
	ErrTogglePresetUnavailable   = errors.New("TOGGLE_PRESET_UNAVAILABLE")
	ErrInvalidTogglePresetStore  = errors.New("INVALID_TOGGLE_PRESET_STORE")
	errInvalidPresetName         = errors.New("INVALID_PRESET_NAME")
	errPresetNameExists          = errors.New("PRESET_NAME_EXISTS")
	errToggleModIsRoot           = errors.New("INVALID_MOD_FOLDER_PATH")
	togglePresetValidationErrors = []error{
		ErrTogglePresetNotFound, ErrTogglePresetStoreUnset, ErrTogglePresetUnavailable, ErrInvalidTogglePresetStore,
		errInvalidPresetName, errPresetNameExists, togglepersist.ErrNoPersistVariables,
	}
)

// ToggleState reads and writes the persist variables of a mod's INIs and of the running games.
type ToggleState interface {
	CaptureModToggleState(ctx context.Context, modPath string) (togglepersist.CapturedState, error)
	PersistModToggleValues(modPath string, inis map[string]map[string]any) (togglepersist.INIValues, error)
	ApplyModToggleState(
		ctx context.Context,
		modPath string,
		values togglepersist.INIValues,
	) (togglepersist.StateApplyResult, error)
}

type togglePresetSettings interface {
	GetTogglePresetStore(context.Context) (string, error)
	SetTogglePresetStore(context.Context, string) error
}

// TogglePreset is a named set of persist variable values of one mod. INIs maps the path of each INI relative
// to the mod folder to declared variable names and values.
type TogglePreset struct {
	ID        string                       `json:"id"`
	Name      string                       `json:"name"`
	Source    string                       `json:"source"`
	INIs      map[string]map[string]string `json:"inis"`
	CreatedAt string                       `json:"createdAt"`
	UpdatedAt string                       `json:"updatedAt"`
}

// TogglePresetList holds a mod's presets and the store they come from. An empty Store means the user has not
// chosen one yet.
type TogglePresetList struct {
	Store   string         `json:"store"`
	Presets []TogglePreset `json:"presets"`
}

func (m *Mod) UseToggleState(state ToggleState) { m.toggleState = state }

// toggleModRef identifies a mod for toggle presets. key is its path below the game's mod folder, lowercased and
// without DISABLED prefixes, so enabling or disabling the mod keeps its presets in the database.
type toggleModRef struct {
	game string
	path string
	key  string
	name string
}

func (m *Mod) GetTogglePresets(ctx context.Context, modPath string) (TogglePresetList, error) {
	m.togglePresetMu.Lock()
	defer m.togglePresetMu.Unlock()

	store, err := m.togglePresetStore(ctx)
	if err != nil || store == "" {
		return TogglePresetList{Store: store, Presets: []TogglePreset{}}, err
	}
	ref, err := m.toggleModRef(ctx, modPath)
	if err != nil {
		return TogglePresetList{}, err
	}
	presets, err := m.loadTogglePresets(ctx, store, ref)
	if err != nil {
		return TogglePresetList{}, m.reportTogglePresetError(err, "toggle-preset.list", ref, nil)
	}
	return TogglePresetList{Store: store, Presets: sortTogglePresets(presets)}, nil
}

// CreateTogglePreset saves the current state of the mod's persist variables, read from the running game when it
// loads the mod through the desktop channel.
func (m *Mod) CreateTogglePreset(ctx context.Context, modPath, name string) (TogglePreset, error) {
	return m.createTogglePreset(ctx, modPath, name, func(path string) (string, togglepersist.INIValues, error) {
		captured, err := m.toggleState.CaptureModToggleState(ctx, path)
		return captured.Source, captured.INIs, err
	})
}

// CreateTogglePresetFromState saves the model viewer's toggle state, given per INI by absolute path.
func (m *Mod) CreateTogglePresetFromState(
	ctx context.Context,
	modPath, name string,
	inis map[string]map[string]any,
) (TogglePreset, error) {
	return m.createTogglePreset(ctx, modPath, name, func(path string) (string, togglepersist.INIValues, error) {
		values, err := m.toggleState.PersistModToggleValues(path, inis)
		return TogglePresetSourceViewer, values, err
	})
}

func (m *Mod) createTogglePreset(
	ctx context.Context,
	modPath, name string,
	capture func(path string) (string, togglepersist.INIValues, error),
) (TogglePreset, error) {
	m.togglePresetMu.Lock()
	defer m.togglePresetMu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return TogglePreset{}, errInvalidPresetName
	}
	store, ref, err := m.togglePresetTarget(ctx, modPath)
	if err != nil {
		return TogglePreset{}, err
	}
	fields := map[string]any{"name": name, "store": store}
	source, values, err := capture(ref.path)
	if err != nil {
		return TogglePreset{}, m.reportTogglePresetError(err, "toggle-preset.create", ref, fields)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	preset := TogglePreset{ID: newPresetID(), Name: name, Source: source, INIs: values, CreatedAt: now, UpdatedAt: now}
	err = m.updateTogglePresets(ctx, store, ref, func(presets []TogglePreset) ([]TogglePreset, error) {
		if togglePresetNameTaken(presets, name, "") {
			return nil, errPresetNameExists
		}
		return append(presets, preset), nil
	})
	if err != nil {
		return TogglePreset{}, m.reportTogglePresetError(err, "toggle-preset.create", ref, fields)
	}
	return preset, nil
}

func (m *Mod) RenameTogglePreset(ctx context.Context, modPath, id, name string) error {
	m.togglePresetMu.Lock()
	defer m.togglePresetMu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return errInvalidPresetName
	}
	store, ref, err := m.togglePresetTarget(ctx, modPath)
	if err != nil {
		return err
	}
	err = m.updateTogglePresets(ctx, store, ref, func(presets []TogglePreset) ([]TogglePreset, error) {
		index := slices.IndexFunc(presets, func(preset TogglePreset) bool { return preset.ID == id })
		if index < 0 {
			return nil, ErrTogglePresetNotFound
		}
		if togglePresetNameTaken(presets, name, id) {
			return nil, errPresetNameExists
		}
		presets[index].Name = name
		presets[index].UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return presets, nil
	})
	return m.reportTogglePresetError(err, "toggle-preset.rename", ref, map[string]any{"presetId": id, "name": name})
}

func (m *Mod) DeleteTogglePreset(ctx context.Context, modPath, id string) error {
	m.togglePresetMu.Lock()
	defer m.togglePresetMu.Unlock()

	store, ref, err := m.togglePresetTarget(ctx, modPath)
	if err != nil {
		return err
	}
	err = m.updateTogglePresets(ctx, store, ref, func(presets []TogglePreset) ([]TogglePreset, error) {
		next := slices.DeleteFunc(slices.Clone(presets), func(preset TogglePreset) bool { return preset.ID == id })
		if len(next) == len(presets) {
			return nil, ErrTogglePresetNotFound
		}
		return next, nil
	})
	return m.reportTogglePresetError(err, "toggle-preset.delete", ref, map[string]any{"presetId": id})
}

// ApplyTogglePreset writes the preset to the mod's INIs and d3dx_user.ini and sets it in the running games.
func (m *Mod) ApplyTogglePreset(ctx context.Context, modPath, id string) (togglepersist.StateApplyResult, error) {
	m.togglePresetMu.Lock()
	defer m.togglePresetMu.Unlock()

	store, ref, err := m.togglePresetTarget(ctx, modPath)
	if err != nil {
		return togglepersist.StateApplyResult{}, err
	}
	fields := map[string]any{"presetId": id, "store": store}
	presets, err := m.loadTogglePresets(ctx, store, ref)
	if err != nil {
		return togglepersist.StateApplyResult{}, m.reportTogglePresetError(err, "toggle-preset.apply", ref, fields)
	}
	index := slices.IndexFunc(presets, func(preset TogglePreset) bool { return preset.ID == id })
	if index < 0 {
		return togglepersist.StateApplyResult{}, ErrTogglePresetNotFound
	}
	fields["name"] = presets[index].Name
	result, err := m.toggleState.ApplyModToggleState(ctx, ref.path, presets[index].INIs)
	return result, m.reportTogglePresetError(err, "toggle-preset.apply", ref, fields)
}

// togglePresetTarget resolves the store and the mod for an operation that needs both.
func (m *Mod) togglePresetTarget(ctx context.Context, modPath string) (string, toggleModRef, error) {
	if m.toggleState == nil {
		return "", toggleModRef{}, ErrTogglePresetUnavailable
	}
	store, err := m.togglePresetStore(ctx)
	if err != nil {
		return "", toggleModRef{}, err
	}
	if store == "" {
		return "", toggleModRef{}, ErrTogglePresetStoreUnset
	}
	ref, err := m.toggleModRef(ctx, modPath)
	return store, ref, err
}

func (m *Mod) togglePresetStore(ctx context.Context) (string, error) {
	settings, ok := m.settings.(togglePresetSettings)
	if !ok {
		return "", ErrTogglePresetUnavailable
	}
	return settings.GetTogglePresetStore(ctx)
}

func (m *Mod) toggleModRef(ctx context.Context, modPath string) (toggleModRef, error) {
	input := strings.TrimSpace(modPath)
	resolved, err := resolveForCompare(input, m.reportUnresolvedPath)
	if err != nil {
		return toggleModRef{}, err
	}
	games, err := m.listGames(ctx)
	if err != nil {
		return toggleModRef{}, err
	}
	root, relative, ok := owningRoot(m.managedRoots(games), resolved)
	if !ok {
		return toggleModRef{}, infra.AnnotateError(errors.New("MOD_PATH_OUTSIDE_MANAGED_ROOT"), infra.Diagnostic{
			Operation: "toggle-preset", Stage: "match-root", Fields: map[string]any{"modPath": input},
		})
	}
	key := normalizeRelativePath(relative)
	if key == "" || key == "." {
		return toggleModRef{}, errToggleModIsRoot
	}
	path := input
	if !pathWithin(root.resolved, resolved) {
		path = filepath.Join(root.path, relative)
	}
	if path, err = validDirectory(path); err != nil {
		return toggleModRef{}, err
	}
	return toggleModRef{
		game: games[root.game].Game, path: path, key: key, name: stripDisabled(filepath.Base(path)),
	}, nil
}

func (m *Mod) loadTogglePresets(ctx context.Context, store string, ref toggleModRef) ([]TogglePreset, error) {
	if store == TogglePresetStoreDatabase {
		client, err := m.requireClient()
		if err != nil {
			return nil, err
		}
		rows, err := client.ModTogglePresets.ListByMod(ctx, ref.game, ref.key)
		if err != nil {
			return nil, err
		}
		presets := make([]TogglePreset, 0, len(rows))
		for _, row := range rows {
			preset, err := togglePresetFromRow(row)
			if err != nil {
				return nil, err
			}
			presets = append(presets, preset)
		}
		return presets, nil
	}

	raw, err := metadata.Read(ref.path)
	if errors.Is(err, os.ErrNotExist) {
		return []TogglePreset{}, nil
	}
	if err != nil {
		return nil, err
	}
	_, presets, err := decodeMetadataTogglePresets(raw)
	return presets, err
}

// updateTogglePresets replaces the presets of one mod with what change makes of them. An error from change
// leaves the store untouched.
func (m *Mod) updateTogglePresets(
	ctx context.Context,
	store string,
	ref toggleModRef,
	change func([]TogglePreset) ([]TogglePreset, error),
) error {
	if store == TogglePresetStoreDatabase {
		client, err := m.requireClient()
		if err != nil {
			return err
		}
		current, err := m.loadTogglePresets(ctx, store, ref)
		if err != nil {
			return err
		}
		next, err := change(current)
		if err != nil {
			return err
		}
		rows := make([]db.ModTogglePresetRow, len(next))
		for i, preset := range next {
			inis, err := json.Marshal(preset.INIs)
			if err != nil {
				return err
			}
			rows[i] = db.ModTogglePresetRow{
				ID: preset.ID, ModName: ref.name, Name: preset.Name, Source: preset.Source, INIs: string(inis),
				CreatedAt: preset.CreatedAt, UpdatedAt: preset.UpdatedAt,
			}
		}
		return client.ModTogglePresets.ReplaceMod(ctx, ref.game, ref.key, rows)
	}

	return metadata.Upsert(ref.path, func(raw []byte) ([]byte, error) {
		document, current, err := decodeMetadataTogglePresets(raw)
		if err != nil {
			return nil, err
		}
		next, err := change(current)
		if err != nil {
			return nil, err
		}
		return encodeMetadataTogglePresets(document, next)
	}, m.reportMetadataCleanup)
}

func togglePresetFromRow(row db.ModTogglePresetRow) (TogglePreset, error) {
	preset := TogglePreset{
		ID: row.ID, Name: row.Name, Source: row.Source, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := json.Unmarshal([]byte(row.INIs), &preset.INIs); err != nil {
		return TogglePreset{}, fmt.Errorf("decode toggle preset %s: %w", row.ID, err)
	}
	return preset, nil
}

// encodeMetadataTogglePresets puts presets back into the decoded nhd.json document and drops the key when none
// remain.
func encodeMetadataTogglePresets(document map[string]json.RawMessage, presets []TogglePreset) ([]byte, error) {
	if len(presets) == 0 {
		delete(document, togglePresetsMetadataKey)
	} else {
		encoded, err := json.Marshal(presets)
		if err != nil {
			return nil, err
		}
		document[togglePresetsMetadataKey] = encoded
	}
	return json.Marshal(document)
}

// decodeMetadataTogglePresets decodes nhd.json, keeping its other keys for the write back. raw may be nil.
func decodeMetadataTogglePresets(raw []byte) (map[string]json.RawMessage, []TogglePreset, error) {
	document := map[string]json.RawMessage{}
	if raw != nil {
		if err := json.Unmarshal(raw, &document); err != nil {
			return nil, nil, fmt.Errorf("decode mod metadata: %w", err)
		}
		if document == nil {
			return nil, nil, errors.New("invalid mod metadata object")
		}
	}
	presets := []TogglePreset{}
	if encoded, ok := document[togglePresetsMetadataKey]; ok {
		if err := json.Unmarshal(encoded, &presets); err != nil {
			return nil, nil, fmt.Errorf("decode toggle presets: %w", err)
		}
	}
	return document, presets, nil
}

func togglePresetNameTaken(presets []TogglePreset, name, exceptID string) bool {
	return slices.ContainsFunc(presets, func(preset TogglePreset) bool {
		return preset.ID != exceptID && strings.EqualFold(preset.Name, name)
	})
}

func sortTogglePresets(presets []TogglePreset) []TogglePreset {
	less := newLocaleLess()
	sort.SliceStable(presets, func(i, j int) bool { return less(presets[i].Name, presets[j].Name) })
	return presets
}

// reportTogglePresetError logs a failure the UI shows only as a generic message. Validation results and
// cancellation are expected outcomes and are returned as they are.
func (m *Mod) reportTogglePresetError(err error, operation string, ref toggleModRef, fields map[string]any) error {
	if err == nil || infra.IsCancellationError(err) || slices.ContainsFunc(togglePresetValidationErrors,
		func(target error) bool { return errors.Is(err, target) }) {
		return err
	}
	diagnostic := map[string]any{"modPath": ref.path, "game": ref.game, "modKey": ref.key}
	for key, value := range fields {
		diagnostic[key] = value
	}
	return infra.ReportError(m.log, err, "Mod", infra.Diagnostic{Operation: operation, Fields: diagnostic})
}
