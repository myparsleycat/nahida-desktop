package mod

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/iostore"
)

// NteKey is one input key an NTE mod compiles into a Blueprint as an FKey literal.
type NteKey struct {
	// Container is the .utoc file name inside the mod folder.
	Container string `json:"container"`
	Chunk     string `json:"chunk"`
	Package   string `json:"package"`
	// Key is the Unreal key name, such as NumPadZero.
	Key string `json:"key"`
}

// nteKeyNames maps the 3DMigoto spelling the key recorder produces to the Unreal EKeys name.
var nteKeyNames = func() map[string]string {
	names := map[string]string{
		"vk_up": "Up", "vk_down": "Down", "vk_left": "Left", "vk_right": "Right",
		"vk_return": "Enter", "vk_space": "SpaceBar", "vk_tab": "Tab", "vk_escape": "Escape", "vk_back": "BackSpace",
		"vk_delete": "Delete", "vk_insert": "Insert", "vk_home": "Home", "vk_end": "End",
		"vk_prior": "PageUp", "vk_next": "PageDown", "vk_pause": "Pause",
		"vk_multiply": "Multiply", "vk_add": "Add", "vk_subtract": "Subtract", "vk_decimal": "Decimal",
		"vk_divide": "Divide",
		";":         "Semicolon", "=": "Equals", ",": "Comma", "_": "Hyphen", ".": "Period", "/": "Slash", "`": "Tilde",
		"[": "LeftBracket", "\\": "Backslash", "]": "RightBracket", "'": "Apostrophe",
	}
	digits := []string{"Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine"}
	for i, digit := range digits {
		names[strconv.Itoa(i)] = digit
		names["vk_numpad"+strconv.Itoa(i)] = "NumPad" + digit
	}
	for c := 'a'; c <= 'z'; c++ {
		names[string(c)] = strings.ToUpper(string(c))
	}
	for i := 1; i <= 12; i++ {
		names["vk_f"+strconv.Itoa(i)] = "F" + strconv.Itoa(i)
	}
	return names
}()

var nteKeyBindings = func() map[string]string {
	bindings := make(map[string]string, len(nteKeyNames))
	for binding, name := range nteKeyNames {
		bindings[name] = binding
	}
	return bindings
}()

func isNteKeyName(name string) bool {
	_, ok := nteKeyBindings[name]
	return ok
}

// NteKeyBinding returns the 3DMigoto spelling of an Unreal key name, for display with the INI key labels.
func NteKeyBinding(name string) string {
	return nteKeyBindings[name]
}

// NteKeyFromBinding returns the Unreal key name of a binding of one key without modifiers.
func NteKeyFromBinding(binding string) (string, bool) {
	fields := strings.Fields(strings.ToLower(binding))
	if len(fields) != 1 {
		return "", false
	}
	key := fields[0]
	switch key {
	case "up", "down", "left", "right":
		key = "vk_" + key
	}
	name, ok := nteKeyNames[key]
	return name, ok
}

func scanNteKeys(modPath string, reports ...func(error)) []NteKey {
	entries, err := os.ReadDir(modPath)
	if err != nil {
		reportScanFailure(err, reports)
		return nil
	}
	var keys []NteKey
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(name), ".utoc") {
			continue
		}
		found, err := scanNteContainer(modPath, name)
		if err != nil && !errors.Is(err, iostore.ErrUnsupported) {
			reportScanFailure(fmt.Errorf("read keys of %s: %w", filepath.Join(modPath, name), err), reports)
		}
		keys = append(keys, found...)
	}
	return keys
}

func scanNteContainer(modPath, tocName string) ([]NteKey, error) {
	tocPath := filepath.Join(modPath, tocName)
	casPath := strings.TrimSuffix(tocPath, filepath.Ext(tocPath)) + ".ucas"
	container, err := iostore.Open(tocPath, casPath)
	if err != nil {
		return nil, err
	}
	cas, err := os.Open(casPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cas.Close() }()

	var keys []NteKey
	for i := range container.ChunkCount() {
		id := container.ChunkID(i)
		if id.Type() != iostore.ChunkTypeExportBundleData {
			continue
		}
		prefix, err := container.ReadChunk(cas, i, iostore.PackagePrefixSize)
		switch {
		case errors.Is(err, iostore.ErrUnsupported):
			continue
		case err != nil:
			return keys, err
		}
		end, err := iostore.NameBatchEnd(prefix)
		if err != nil {
			continue
		}
		data, err := container.ReadChunk(cas, i, end)
		switch {
		case errors.Is(err, iostore.ErrUnsupported):
			continue
		case err != nil:
			return keys, err
		}
		if names, err := iostore.PackageNames(data); err != nil || !slices.ContainsFunc(names, isNteKeyName) {
			continue
		}
		if len(data) < container.ChunkLength(i) {
			data, err = container.ReadChunk(cas, i, -1)
			switch {
			case errors.Is(err, iostore.ErrUnsupported):
				continue
			case err != nil:
				return keys, err
			}
		}
		pkg, err := iostore.ParsePackage(data)
		if err != nil {
			continue
		}
		for _, key := range pkg.InputKeys(isNteKeyName) {
			keys = append(
				keys,
				NteKey{Container: tocName, Chunk: hex.EncodeToString(id[:]), Package: pkg.Name(), Key: key},
			)
		}
	}
	return keys, nil
}

// UpdateNteKey changes every FKey literal of oldKey in one package of an NTE mod container to newKey.
func (m *Mod) UpdateNteKey(ctx context.Context, modPath, container, chunk, oldKey, newKey string) error {
	if oldKey == newKey {
		return nil
	}
	if !isNteKeyName(newKey) {
		return fmt.Errorf("unsupported key %q", newKey)
	}
	if filepath.Base(container) != container || !strings.EqualFold(filepath.Ext(container), ".utoc") {
		return fmt.Errorf("invalid container name %q", container)
	}
	tocPath := filepath.Join(modPath, container)
	if _, err := m.ownedPath(ctx, tocPath); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := diskio.Acquire(ctx, tocPath)
	if err != nil {
		return err
	}
	defer release()

	stage, err := updateNteContainerKey(tocPath, chunk, oldKey, newKey)
	if err == nil {
		return nil
	}
	if locked := m.lockedFolderError(err, modPath); errors.Is(locked, ErrModFolderLocked) {
		return locked
	}
	return infra.AnnotateError(err, infra.Diagnostic{
		Operation: "update-nte-key",
		Stage:     stage,
		Fields: map[string]any{
			"modPath": modPath, "container": container, "chunk": chunk, "oldKey": oldKey, "newKey": newKey,
		},
	})
}

func updateNteContainerKey(tocPath, chunk, oldKey, newKey string) (string, error) {
	var id iostore.ChunkID
	raw, err := hex.DecodeString(chunk)
	if err != nil || len(raw) != len(id) {
		return "validate", fmt.Errorf("invalid chunk id %q", chunk)
	}
	copy(id[:], raw)
	casPath := strings.TrimSuffix(tocPath, filepath.Ext(tocPath)) + ".ucas"
	container, err := iostore.Open(tocPath, casPath)
	if err != nil {
		return "open", err
	}
	index := container.FindChunk(id)
	if index < 0 {
		return "open", fmt.Errorf("chunk %s not found", chunk)
	}

	cas, err := os.Open(casPath)
	if err != nil {
		return "read", err
	}
	data, err := container.ReadChunk(cas, index, -1)
	if closeErr := cas.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "read", err
	}
	pkg, err := iostore.ParsePackage(data)
	if err != nil {
		return "parse", err
	}
	edited, err := pkg.ReplaceInputKey(oldKey, newKey, isNteKeyName)
	if err != nil {
		return "edit", err
	}
	// Read-only files would refuse the append and the replacement, so the attribute is cleared first.
	for _, path := range []string{tocPath, casPath} {
		if err := os.Chmod(path, 0o666); err != nil {
			return "write", err
		}
	}
	if err := container.ReplaceChunk(index, edited); err != nil {
		return "write", err
	}
	return "", nil
}
