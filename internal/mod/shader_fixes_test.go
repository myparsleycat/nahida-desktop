package mod

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShaderFixesUsesOwnerIndexWithoutScanningWhenDisabling(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	modPath := filepath.Join(h.modsPath, "Group", "Mod")
	for _, file := range []string{"a.ini", filepath.Join("nested", "b.ini"), filepath.Join("nested", "c.ini")} {
		writeFile(t, filepath.Join(modPath, "ShaderFixes", file), filepath.Base(file))
	}
	if _, err := h.service.HandleShaders(modPath, true); err != nil {
		t.Fatal(err)
	}
	if h.service.ownerIndexWrites != 2 {
		t.Fatalf("owner index writes = %d, want 2", h.service.ownerIndexWrites)
	}
	if err := os.RemoveAll(filepath.Join(modPath, "ShaderFixes")); err != nil {
		t.Fatal(err)
	}
	h.service.globCalls = nil

	if _, err := h.service.HandleShaders(modPath, false); err != nil {
		t.Fatal(err)
	}
	if len(h.service.globCalls) != 0 {
		t.Fatalf("glob calls = %#v, want none", h.service.globCalls)
	}
	if exists(filepath.Join(h.importerPath, "ShaderFixes", "a.ini")) {
		t.Fatal("copied shader should be removed")
	}
	if exists(filepath.Join(modPath, shaderFixesModMarkerFile)) {
		t.Fatal("mod marker should be removed")
	}
	assertOwnerIndex(t, filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile), map[string]any{
		"version": float64(1), "targets": map[string]any{},
	})
}

func TestNormalizeShaderFixesOwnerTargetKeyOnlyRejectsParentSegments(t *testing.T) {
	t.Parallel()

	service := &ShaderFixes{}
	if got := service.normalizeShaderFixesOwnerTargetKey("nested/x..y.ini"); got == nil || *got != "nested/x..y.ini" {
		t.Fatalf("ordinary double-dot filename = %#v", got)
	}
	for _, path := range []string{"../escape.ini", "nested/../escape.ini", `nested\..\escape.ini`} {
		if got := service.normalizeShaderFixesOwnerTargetKey(path); got != nil {
			t.Fatalf("parent path %q normalized to %q", path, *got)
		}
	}
}

func TestShaderFixesPreservesShaderWhileAnotherModOwnsIt(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	first := filepath.Join(h.modsPath, "Group", "First")
	second := filepath.Join(h.modsPath, "Group", "Second")
	for _, modPath := range []string{first, second} {
		writeFile(t, filepath.Join(modPath, "ShaderFixes", "shared.ini"), "shared")
		if _, err := h.service.HandleShaders(modPath, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.service.HandleShaders(first, false); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(h.importerPath, "ShaderFixes", "shared.ini")) {
		t.Fatal("shared shader should remain")
	}
	if _, err := h.service.HandleShaders(second, false); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(h.importerPath, "ShaderFixes", "shared.ini")) {
		t.Fatal("shared shader should be removed")
	}
}

func TestShaderFixesPreservesChangedShader(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	modPath := filepath.Join(h.modsPath, "Group", "Mod")
	target := filepath.Join(h.importerPath, "ShaderFixes", "changed.ini")
	writeFile(t, filepath.Join(modPath, "ShaderFixes", "changed.ini"), "original")
	if _, err := h.service.HandleShaders(modPath, true); err != nil {
		t.Fatal(err)
	}
	writeFile(t, target, "changed")
	if _, err := h.service.HandleShaders(modPath, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "changed" {
		t.Fatalf("preserved content = %q %v", raw, err)
	}
	if exists(filepath.Join(modPath, shaderFixesModMarkerFile)) {
		t.Fatal("mod marker should be removed")
	}
}

func TestShaderFixesMigratesManifestsByScanningCurrentImporterMods(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	first := filepath.Join(h.modsPath, "Group", "First")
	second := filepath.Join(h.modsPath, "Group", "Second")
	target := filepath.Join(h.importerPath, "ShaderFixes", "shared.ini")
	hash := sha256Hex("shared")
	writeFile(t, target, "shared")
	for _, item := range []struct {
		modPath, modKey string
	}{{first, "first"}, {second, "second"}} {
		writeJSON(t, filepath.Join(item.modPath, shaderFixesModMarkerFile), map[string]any{
			"version": 1,
			"modKey":  item.modKey,
			"files": []map[string]any{{
				"file": "shared.ini", "targetPath": target, "targetKey": "legacy-target-key", "hash": hash,
			}},
		})
	}
	otherImporter := filepath.Join(h.root, "OtherImporter")
	otherMods := filepath.Join(otherImporter, "Mods")
	if err := os.MkdirAll(otherMods, 0o755); err != nil {
		t.Fatal(err)
	}
	h.importers = append(h.importers, shaderImporter{Key: "OtherImporter", ImporterFolder: otherImporter})
	h.games = append(h.games, shaderGame{Game: "Other", ModFolderPath: otherMods, Importer: "OtherImporter"})
	h.service.globCalls = nil

	if _, err := h.service.HandleShaders(first, false); err != nil {
		t.Fatal(err)
	}
	assertGlobCalls(t, h.service.globCalls, []shaderGlobCall{{Pattern: "**/" + shaderFixesModMarkerFile, Cwd: h.modsPath}})
	if !exists(target) {
		t.Fatal("shared shader should remain")
	}
	assertOwnerIndex(t, filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile), map[string]any{
		"version": float64(1),
		"targets": map[string]any{"shared.ini": map[string]any{"hash": hash, "owners": []any{"second"}}},
	})

	h.service.globCalls = nil
	if _, err := h.service.HandleShaders(second, false); err != nil {
		t.Fatal(err)
	}
	if len(h.service.globCalls) != 0 {
		t.Fatalf("second disable glob calls = %#v", h.service.globCalls)
	}
	if exists(target) {
		t.Fatal("shared shader should be removed")
	}
}

func TestShaderFixesCleansOriginalImporterAfterMove(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	original := filepath.Join(h.modsPath, "Group", "Mod")
	originalTarget := filepath.Join(h.importerPath, "ShaderFixes", "moved.ini")
	writeFile(t, filepath.Join(original, "ShaderFixes", "moved.ini"), "moved")
	if _, err := h.service.HandleShaders(original, true); err != nil {
		t.Fatal(err)
	}
	otherImporter := filepath.Join(h.root, "OtherImporter")
	otherMods := filepath.Join(otherImporter, "Mods")
	moved := filepath.Join(otherMods, "Group", "Mod")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	h.importers = append(h.importers, shaderImporter{Key: "OtherImporter", ImporterFolder: otherImporter})
	h.games = append(h.games, shaderGame{Game: "Other", ModFolderPath: otherMods, Importer: "OtherImporter"})
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile))
	h.service.globCalls = nil

	if _, err := h.service.HandleShaders(moved, false); err != nil {
		t.Fatal(err)
	}
	assertGlobCalls(t, h.service.globCalls, []shaderGlobCall{{Pattern: "**/" + shaderFixesModMarkerFile, Cwd: h.modsPath}})
	if exists(originalTarget) {
		t.Fatal("original shader should be removed")
	}
	if exists(filepath.Join(moved, shaderFixesModMarkerFile)) {
		t.Fatal("moved marker should be removed")
	}
	assertOwnerIndex(t, filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile), map[string]any{
		"version": float64(1), "targets": map[string]any{},
	})
	if exists(filepath.Join(otherImporter, "ShaderFixes", shaderFixesModMarkerFile)) {
		t.Fatal("new importer should not receive an owner index")
	}
}

func TestShaderFixesRebuildsCorruptedOwnerIndex(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	modPath := filepath.Join(h.modsPath, "Group", "Mod")
	target := filepath.Join(h.importerPath, "ShaderFixes", "legacy.ini")
	hash := sha256Hex("legacy")
	writeFile(t, target, "legacy")
	writeJSON(t, filepath.Join(modPath, shaderFixesModMarkerFile), map[string]any{
		"version": 1,
		"modKey":  "legacy",
		"files": []map[string]any{{
			"file": "legacy.ini", "targetPath": target, "targetKey": "legacy-target-key", "hash": hash,
		}},
	})
	writeFile(t, filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile), "invalid")
	h.service.globCalls = nil

	if _, err := h.service.HandleShaders(modPath, false); err != nil {
		t.Fatal(err)
	}
	assertGlobCalls(t, h.service.globCalls, []shaderGlobCall{{Pattern: "**/" + shaderFixesModMarkerFile, Cwd: h.modsPath}})
	if exists(target) {
		t.Fatal("legacy shader should be removed")
	}
}

func TestShaderFixesRollbackRemovesOwnerState(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	modPath := filepath.Join(h.modsPath, "Group", "Mod")
	target := filepath.Join(h.importerPath, "ShaderFixes", "rollback.ini")
	writeFile(t, filepath.Join(modPath, "ShaderFixes", "rollback.ini"), "rollback")
	processed, err := h.service.HandleShaders(modPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.service.RollbackEnabledShaders(modPath, processed); err != nil {
		t.Fatal(err)
	}
	if exists(target) {
		t.Fatal("rolled back shader should be removed")
	}
	assertOwnerIndex(t, filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile), map[string]any{
		"version": float64(1), "targets": map[string]any{},
	})
	if exists(filepath.Join(modPath, shaderFixesModMarkerFile)) {
		t.Fatal("mod marker should be removed")
	}
}

func TestShaderFixesRollbackAfterOwnerIndexWriteFailure(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	modPath := filepath.Join(h.modsPath, "Group", "Mod")
	target := filepath.Join(h.importerPath, "ShaderFixes", "rollback.ini")
	writeFile(t, filepath.Join(modPath, "ShaderFixes", "rollback.ini"), "rollback")
	h.service.failOwnerIndexWriteOn = 2
	_, err := h.service.HandleShaders(modPath, true)
	if err == nil {
		t.Fatal("expected owner index write failure")
	}
	processed := processedFilesFromError(err)
	if len(processed) != 1 {
		t.Fatalf("processedFiles = %#v", processed)
	}
	h.service.failOwnerIndexWriteOn = 0
	if err := h.service.RollbackEnabledShaders(modPath, processed); err != nil {
		t.Fatal(err)
	}
	if exists(target) {
		t.Fatal("rolled back shader should be removed")
	}
	assertOwnerIndex(t, filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile), map[string]any{
		"version": float64(1), "targets": map[string]any{},
	})
}

func TestShaderFixesDoesNotCopyReservedOwnerIndex(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	modPath := filepath.Join(h.modsPath, "Group", "Mod")
	writeFile(t, filepath.Join(modPath, "ShaderFixes", "actual.ini"), "actual")
	writeFile(t, filepath.Join(modPath, "ShaderFixes", shaderFixesModMarkerFile), "reserved")
	if _, err := h.service.HandleShaders(modPath, true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(modPath, shaderFixesModMarkerFile))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	assertOwnerIndex(t, filepath.Join(h.importerPath, "ShaderFixes", shaderFixesModMarkerFile), map[string]any{
		"version": float64(1),
		"targets": map[string]any{
			"actual.ini": map[string]any{
				"hash":   sha256Hex("actual"),
				"owners": []any{manifest["modKey"]},
			},
		},
	})
}

func TestShaderFixesDoesNotScanWhenNoManifestExists(t *testing.T) {
	t.Parallel()
	h := newShaderFixesHarness(t)
	modPath := filepath.Join(h.modsPath, "Group", "Mod")
	writeFile(t, filepath.Join(modPath, "ShaderFixes", "unused.ini"), "unused")
	h.service.globCalls = nil
	if _, err := h.service.HandleShaders(modPath, false); err != nil {
		t.Fatal(err)
	}
	if len(h.service.globCalls) != 0 {
		t.Fatalf("glob calls = %#v, want none", h.service.globCalls)
	}
}

type shaderFixesHarness struct {
	root         string
	importerPath string
	modsPath     string
	importers    []shaderImporter
	games        []shaderGame
	service      *ShaderFixes
}

func newShaderFixesHarness(t *testing.T) *shaderFixesHarness {
	t.Helper()
	root := t.TempDir()
	importerPath := filepath.Join(root, "Importer")
	modsPath := filepath.Join(importerPath, "Mods")
	if err := os.MkdirAll(modsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &shaderFixesHarness{
		root: root, importerPath: importerPath, modsPath: modsPath,
		importers: []shaderImporter{{Key: "Importer", ImporterFolder: importerPath}},
		games:     []shaderGame{{Game: "Test", ModFolderPath: modsPath, Importer: "Importer"}},
	}
	h.service = newShaderFixes()
	h.service.getImporters = func() []shaderImporter { return h.importers }
	h.service.getGames = func() []shaderGame { return h.games }
	return h
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(raw)+"\n")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func assertOwnerIndex(t *testing.T, path string, want map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owner index = %#v, want %#v", got, want)
	}
}

func assertGlobCalls(t *testing.T, got, want []shaderGlobCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("glob calls = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i].Pattern != want[i].Pattern || filepath.Clean(got[i].Cwd) != filepath.Clean(want[i].Cwd) {
			t.Fatalf("glob calls = %#v, want %#v", got, want)
		}
	}
}
