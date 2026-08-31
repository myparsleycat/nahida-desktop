package mod

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
)

type testSettings struct {
	preview bool
	style   string
	move    bool
}

func (s testSettings) GetSearchModPreview(context.Context) (bool, error) { return s.preview, nil }

func (s testSettings) GetDisabledPrefixStyle(context.Context) (string, error) {
	return s.style, nil
}

func (s testSettings) GetArchiveExtractPathMode(context.Context) (string, error) {
	return "flatten_single_root", nil
}

func (s testSettings) GetDeleteArchiveAfterExtract(context.Context) (bool, error) {
	return false, nil
}

func (s testSettings) GetMoveFolderInsteadOfCopy(context.Context) (bool, error) {
	return s.move, nil
}

func (s testSettings) GetCopyShaderFixesOnEnable(context.Context) (bool, error) {
	return false, nil
}

func newTestMod(t *testing.T, settings testSettings) (*Mod, string) {
	t.Helper()
	root := t.TempDir()
	client, err := db.New(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	service := NewWithOptions(Options{Settings: settings})
	service.UseClient(client)
	return service, root
}

func TestGameCRUDRejectsDuplicateRootsAndInvalidOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsA := filepath.Join(root, "mods-a")
	modsB := filepath.Join(root, "mods-b")
	if err := os.MkdirAll(modsA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(modsB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Genshin", modsA, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "ZZZ", modsB, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Duplicate", modsA, nil, nil, nil, nil); err == nil || err.Error() != "DUPLICATE_MOD_FOLDER_PATH" {
		t.Fatalf("duplicate root error = %v", err)
	}
	if err := service.ReorderGames(ctx, []string{"ZZZ", "ZZZ"}); err == nil || err.Error() != "INVALID_GAME_ORDER" {
		t.Fatalf("invalid order error = %v", err)
	}
	if err := service.ReorderGames(ctx, []string{"ZZZ", "Genshin"}); err != nil {
		t.Fatal(err)
	}
	games, err := service.GetGames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].Game != "ZZZ" || games[1].Game != "Genshin" {
		t.Fatalf("games = %#v", games)
	}
}

func TestAddAndUpdateGameDoNotRequireExistingConfiguredDirectory(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	initial := filepath.Join(root, "not-created-yet")
	if err := service.AddGame(ctx, "Game", initial, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(initial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("AddGame created configured directory: %v", err)
	}
	updated := filepath.Join(root, "also-not-created")
	if err := service.UpdateGame(ctx, "Game", GameUpdates{ModFolderPath: updated}); err != nil {
		t.Fatal(err)
	}
	path, err := service.GetGamePath(ctx, "Game")
	if err != nil || path == nil || *path != updated {
		t.Fatalf("updated path = %v, %v", path, err)
	}
}

func TestAddGameClassifiesCaseVariantNameOnSamePathAsPathDuplicate(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", first, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "game", first, nil, nil, nil, nil); err == nil || err.Error() != "DUPLICATE_MOD_FOLDER_PATH" {
		t.Fatalf("case-variant same-path error = %v", err)
	}
	if err := service.AddGame(ctx, "game", second, nil, nil, nil, nil); err != nil {
		t.Fatalf("case-variant name on distinct path = %v", err)
	}
}

func TestGetGamesDerivesMissingNteInstallPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	importer := "NTE"
	linked := filepath.Join(root, "Neverness To Everness", "Client", "WindowsNoEditor", "HT", "Content", "Paks", "Mods")
	modRoot := filepath.Join(root, "custom-mods")
	if err := service.client.GamePaths.Insert(ctx, db.GamePathRow{
		Game: "NTE", ModFolderPath: modRoot, Importer: &importer, LinkedModFolderPath: &linked,
	}); err != nil {
		t.Fatal(err)
	}

	games, err := service.GetGames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "Neverness To Everness", "Client")
	if len(games) != 1 || games[0].GameInstallPath == nil || !samePath(*games[0].GameInstallPath, want) {
		t.Fatalf("games = %#v, want derived install path %q", games, want)
	}
	row, err := service.client.GamePaths.GetByGame(ctx, "NTE")
	if err != nil || row == nil || row.GameInstallPath != nil {
		t.Fatalf("stored row = %#v, %v; derived value must not be persisted", row, err)
	}
}

func TestUpdateAndRemoveGameCleanupNteJunctions(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	importer := "NTE"

	oldTarget := filepath.Join(root, "nte-custom-old")
	oldLink := filepath.Join(root, "nte-game-old", "Content", "Paks", "Mods")
	if err := configureNteModFolder(oldTarget, &oldLink); err != nil {
		t.Fatal(err)
	}
	writePak(t, filepath.Join(oldTarget, "Character", "Old"), "old.pak")
	if err := service.client.GamePaths.Insert(ctx, db.GamePathRow{
		Game: "NTE Update", ModFolderPath: oldTarget, Importer: &importer, LinkedModFolderPath: &oldLink,
	}); err != nil {
		t.Fatal(err)
	}
	normalRoot := filepath.Join(root, "normal-mods")
	if err := os.MkdirAll(normalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateGame(ctx, "NTE Update", GameUpdates{ModFolderPath: normalRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(oldLink); err == nil {
		t.Fatal("old NTE junction remains after switching importer")
	}
	if !fileExists(filepath.Join(oldLink, "Character", "Old", "old.pak")) {
		t.Fatal("old NTE mod was not restored into the game folder")
	}
	updated, err := service.client.GamePaths.GetByGame(ctx, "NTE Update")
	if err != nil || updated == nil || updated.Importer != nil || updated.LinkedModFolderPath != nil {
		t.Fatalf("updated game = %#v, %v", updated, err)
	}

	removeTarget := filepath.Join(root, "nte-custom-remove")
	removeLink := filepath.Join(root, "nte-game-remove", "Content", "Paks", "Mods")
	if err := configureNteModFolder(removeTarget, &removeLink); err != nil {
		t.Fatal(err)
	}
	writePak(t, filepath.Join(removeTarget, "UI", "Remove"), "remove.pak")
	if err := service.client.GamePaths.Insert(ctx, db.GamePathRow{
		Game: "NTE Remove", ModFolderPath: removeTarget, Importer: &importer, LinkedModFolderPath: &removeLink,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.RemoveGame(ctx, "NTE Remove"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(removeLink); err == nil {
		t.Fatal("NTE junction remains after removing game")
	}
	if !fileExists(filepath.Join(removeLink, "UI", "Remove", "remove.pak")) {
		t.Fatal("removed game's mod was not restored into the game folder")
	}
	removed, err := service.client.GamePaths.GetByGame(ctx, "NTE Remove")
	if err != nil || removed != nil {
		t.Fatalf("removed game = %#v, %v", removed, err)
	}
}

func TestScanAndTogglePreserveStableID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{preview: true, style: "underscore"})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character 2")
	modPath := filepath.Join(group, "Costume 10")
	if err := os.MkdirAll(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := `[KeyToggle]
key = F1
$swap = 0, 1

[NotAKey]
$ignored = 0, 1
`
	if err := os.WriteFile(filepath.Join(modPath, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modPath, "preview.png"), []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	before, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if before.ModCount != 1 || before.EnabledModCount != 1 || len(before.Mods[0].Inis) != 1 {
		t.Fatalf("before = %#v", before)
	}
	if got := before.Mods[0].Inis[0].ToggleKeys; len(got) != 1 || got[0].Variable != "$swap" || got[0].Key == nil || *got[0].Key != "F1" {
		t.Fatalf("toggle keys = %#v", got)
	}
	disabledPath, err := service.Toggle(ctx, modPath)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(disabledPath) != "DISABLED_Costume 10" {
		t.Fatalf("disabled path = %q", disabledPath)
	}
	after, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if after.EnabledModCount != 0 || after.Mods[0].ID != before.Mods[0].ID {
		t.Fatalf("after = %#v", after)
	}
}

func TestActionsRejectPathsOutsideManagedRoots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(modsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Toggle(ctx, outside); err == nil || err.Error() != "MOD_PATH_OUTSIDE_MANAGED_ROOT" {
		t.Fatalf("outside error = %v", err)
	}
}

func TestActionsRejectSymlinkEscape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(modsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(modsRoot, "escaped")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Toggle(ctx, link); err == nil || err.Error() != "MOD_PATH_OUTSIDE_MANAGED_ROOT" {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestExclusiveToggleDisablesSiblings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	enabled := filepath.Join(group, "Enabled")
	disabled := filepath.Join(group, "DISABLED Target")
	for _, path := range []string{enabled, disabled} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.ExclusiveToggle(ctx, disabled)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result) != "Target" {
		t.Fatalf("result = %q", result)
	}
	if _, err := os.Stat(filepath.Join(group, "DISABLED Enabled")); err != nil {
		t.Fatalf("sibling was not disabled: %v", err)
	}
}

func TestManualSubGroupIsDecoratedAndHiddenFromParentMods(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	manual := filepath.Join(group, "Variants")
	regular := filepath.Join(group, "Regular")
	for _, path := range []string{filepath.Join(manual, "Blue"), regular} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.SetManualSubGroup(ctx, manual, true); err != nil {
		t.Fatal(err)
	}
	characters, err := service.GetCharacters(ctx, "Game", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(characters) != 1 || !characters[0].HasManualSubGroups || characters[0].ModCount != 1 {
		t.Fatalf("characters = %#v", characters)
	}
	mods, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods.Mods) != 1 || mods.Mods[0].Name != "Regular" || !mods.HasManualSubGroups {
		t.Fatalf("mods = %#v", mods)
	}
	manualGroups, err := service.GetManualSubGroups(ctx, group, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(manualGroups) != 1 || manualGroups[0].Name != "Variants" || !manualGroups[0].IsManualSubGroup {
		t.Fatalf("manual groups = %#v", manualGroups)
	}
}

func TestPresetCreateAndApplyRestoresEnabledState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	modPath := filepath.Join(group, "Costume")
	if err := os.MkdirAll(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modPath, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	description := "enabled state"
	preset, err := service.CreatePreset(ctx, "Game", "Default", &description, false)
	if err != nil {
		t.Fatal(err)
	}
	disabledPath, err := service.Toggle(ctx, modPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyPreset(ctx, preset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 1 || len(result.Missing) != 0 {
		t.Fatalf("apply result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(group, "Costume")); err != nil {
		t.Fatalf("enabled folder missing after apply: %v (disabled was %q)", err, disabledPath)
	}
	items, err := service.GetPresets(ctx, "Game")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Description == nil || *items[0].Description != description {
		t.Fatalf("presets = %#v", items)
	}
}

func TestApplyPresetSkipsActiveDownloadDestination(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	modPath := filepath.Join(group, "Costume")
	if err := os.MkdirAll(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modPath, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	disabledPath, err := service.Toggle(ctx, modPath)
	if err != nil {
		t.Fatal(err)
	}
	preset, err := service.CreatePreset(ctx, "Game", "Disabled", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	enabledPath, err := service.Toggle(ctx, disabledPath)
	if err != nil {
		t.Fatal(err)
	}
	registerActiveModDownload(t, service, "preset-active", enabledPath)

	result, err := service.ApplyPreset(ctx, preset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 0 || len(result.Skipped) != 1 ||
		!strings.EqualFold(result.Skipped[0], filepath.ToSlash(filepath.Join("Character", "Costume"))) {
		t.Fatalf("apply result = %#v", result)
	}
	if _, err := os.Stat(enabledPath); err != nil {
		t.Fatalf("active download folder changed: %v", err)
	}
}

func TestApplyPresetRoutesNteModsThroughPakToggle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "Character", "Costume")
	writePak(t, modPath, "costume.pak")
	if err := os.WriteFile(filepath.Join(modPath, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
		t.Fatal(err)
	}
	importer := "NTE"
	if err := service.client.GamePaths.Insert(ctx, db.GamePathRow{
		Game: "NTE", ModFolderPath: modsRoot, Importer: &importer,
	}); err != nil {
		t.Fatal(err)
	}
	preset, err := service.CreatePreset(ctx, "NTE", "Enabled", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	disabledPath, err := service.Toggle(ctx, modPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(disabledPath, "costume.pak.disabled")); err != nil {
		t.Fatalf("disabled pak missing before apply: %v", err)
	}

	result, err := service.ApplyPreset(ctx, preset.ID)
	if err != nil {
		t.Fatal(err)
	}
	enabledPath := filepath.Join(filepath.Dir(disabledPath), "Costume")
	if len(result.Applied) != 1 || len(result.Missing) != 0 {
		t.Fatalf("apply result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(enabledPath, "costume.pak")); err != nil {
		t.Fatalf("enabled pak missing after apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(enabledPath, "costume.pak.disabled")); !os.IsNotExist(err) {
		t.Fatalf("disabled pak remains after apply: %v", err)
	}
}

func TestPresetConflictMustBeResolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	for _, name := range []string{"Costume", "DISABLED Costume"} {
		path := filepath.Join(group, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	conflicts, err := service.GetPresetCreateConflicts(ctx, "Game")
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || len(conflicts[0].Candidates) != 2 {
		t.Fatalf("conflicts = %#v", conflicts)
	}
	if _, err := service.CreatePreset(ctx, "Game", "Blocked", nil, false); err == nil || err.Error() != "PRESET_CONFLICTS_EXIST" {
		t.Fatalf("create without resolve error = %v", err)
	}
	if _, err := service.CreatePreset(ctx, "Game", "Resolved", nil, true); err != nil {
		t.Fatal(err)
	}
	remaining, err := service.GetPresetCreateConflicts(ctx, "Game")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining conflicts = %#v", remaining)
	}
}

func TestUpdateToggleKeyOnlyTouchesRequestedSection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "Group", "Costume")
	if err := os.MkdirAll(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	iniPath := filepath.Join(modPath, "mod.ini")
	content := "[KeyFirst]\r\n$swap = 0, 1\r\n\r\n[KeySecond]\r\nkey = F2\r\n"
	if err := os.WriteFile(iniPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateToggleKey(ctx, modPath, "mod.ini", "keysecond", "$swap", "1, 0"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(iniPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if got != "[KeyFirst]\r\n$swap = 0, 1\r\n\r\n[KeySecond]\r\nkey = F2\r\n$swap = 1, 0\n" {
		t.Fatalf("updated ini = %q", got)
	}
	if err := service.UpdateToggleKey(ctx, modPath, iniPath, "KeySecond", "$swap", ""); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(iniPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "[KeyFirst]\r\n$swap = 0, 1\r\n\r\n[KeySecond]\r\nkey = F2\r\n" {
		t.Fatalf("removed ini variable = %q", got)
	}
}

func TestPastePreviewReplacesStalePreview(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "Group", "Costume")
	if err := os.MkdirAll(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(modPath, "preview.jpg")
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	payload := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("new-image"))
	result, err := service.PastePreview(ctx, modPath, payload, "base64", &stale)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result) != "preview.png" {
		t.Fatalf("result = %q", result)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale preview still exists: %v", err)
	}
	raw, err := os.ReadFile(result)
	if err != nil || string(raw) != "new-image" {
		t.Fatalf("preview = %q, %v", raw, err)
	}
}

func TestCopyFolderAndReadGameBananaMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	source := filepath.Join(root, "Downloaded")
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := `{"source":"gamebanana","mod":{"id":1234}}`
	if err := os.WriteFile(filepath.Join(source, "nhd.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	target, err := service.CopyFolderToGroup(ctx, source, group)
	if err != nil {
		t.Fatal(err)
	}
	id, err := service.GetGameBananaModID(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if id == nil || *id != 1234 {
		t.Fatalf("gamebanana id = %v", id)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("copy removed source: %v", err)
	}
}

func TestCopyFolderToGroupMovesSourceWhenConfigured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{move: true})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	source := filepath.Join(root, "Downloaded")
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	target, err := service.CopyFolderToGroup(ctx, source, group)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("moved source still exists: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(target, "mod.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "[Constants]" {
		t.Fatalf("moved content = %q", content)
	}
}

func TestResolveDownloadTargetUsesElectronFuzzyScoringAndRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	firstRoot := filepath.Join(root, "first")
	secondRoot := filepath.Join(root, "second")
	for _, path := range []string{
		filepath.Join(firstRoot, "Veltrionna"),
		filepath.Join(firstRoot, "CharF"),
		filepath.Join(secondRoot, "CharF"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Genshin", firstRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Other", secondRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	match, err := service.ResolveDownloadTarget(ctx, "veltronna mod", nil)
	if err != nil {
		t.Fatal(err)
	}
	if match == nil || match.Game != "Genshin" || match.Group.Name != "Veltrionna" || match.Score < 0.85 {
		t.Fatalf("match = %#v", match)
	}
	ambiguous, err := service.ResolveDownloadTarget(ctx, "CharF", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous != nil {
		t.Fatalf("ambiguous match = %#v", ambiguous)
	}
	filter := "Genshin"
	filtered, err := service.ResolveDownloadTarget(ctx, "CharF", &filter)
	if err != nil {
		t.Fatal(err)
	}
	if filtered == nil || filtered.Game != filter {
		t.Fatalf("filtered match = %#v", filtered)
	}
}

func TestResolveNteInstallPathAndNteListing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	installRoot := filepath.Join(root, "install")
	executable := filepath.Join(
		installRoot, "Neverness To Everness", "Client", "WindowsNoEditor",
		"HT", "Binaries", "Win64", "HTGame.exe",
	)
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range nteBootstrapRequiredFiles {
		if err := os.WriteFile(filepath.Join(filepath.Dir(executable), name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	htRoot := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", ".."))
	if err := os.MkdirAll(filepath.Join(htRoot, "Content", "Paks"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolution, err := service.ResolveNteInstallPath(ctx, installRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resolution == nil || !samePath(resolution.ExecutablePath, executable) || resolution.RequiresElevation {
		t.Fatalf("resolution = %#v", resolution)
	}
	modsRoot := resolution.ModFolderPath
	direct := filepath.Join(modsRoot, "Character", "CharF", "Direct")
	wrapped := filepath.Join(modsRoot, "Character", "CharF", "Pack", "Variant")
	for _, path := range []string{direct, wrapped} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "mod.pak"), []byte("pak"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	importer := "NTE"
	if err := service.AddGame(ctx, "NTE", modsRoot, &importer, nil, &installRoot, &executable); err != nil {
		t.Fatal(err)
	}
	characters, err := service.GetCharacters(ctx, "NTE", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(characters) != 4 {
		t.Fatalf("NTE characters = %#v", characters)
	}
	subgroups, err := service.GetSubGroups(ctx, filepath.Join(modsRoot, "Character"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(subgroups) != 1 || subgroups[0].Name != "CharF" || subgroups[0].ModCount != 2 {
		t.Fatalf("NTE subgroups = %#v", subgroups)
	}
	mods, err := service.GetMods(ctx, filepath.Join(modsRoot, "Character", "CharF"))
	if err != nil {
		t.Fatal(err)
	}
	if mods.ModCount != 2 || mods.EnabledModCount != 2 {
		t.Fatalf("NTE mods = %#v", mods)
	}
	disabledPath, err := service.Toggle(ctx, direct)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(disabledPath) != "DISABLED Direct" {
		t.Fatalf("NTE disabled path = %q", disabledPath)
	}
	if _, err := os.Stat(filepath.Join(disabledPath, "mod.pak.disabled")); err != nil {
		t.Fatalf("disabled pak missing: %v", err)
	}
}

func TestClassifyMergePacksMatchesFamiliesAndHashOverlap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	ordinary := filepath.Join(group, "Ordinary")
	namespace := filepath.Join(group, "Namespace")
	for _, path := range []string{ordinary, namespace} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ordinaryINI := "[TextureOverrideBodyPosition]\nhash = aabbccdd\nrun = CommandListSkin\n"
	namespaceINI := "namespace = Foo\\Master\n\n[Constants]\nglobal persist $swapvar = 0\n\n[KeySwap]\ntype = cycle\n$swapvar = 0,1\n"
	if err := os.WriteFile(filepath.Join(ordinary, "mod.ini"), []byte(ordinaryINI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(namespace, "Master.ini"), []byte(namespaceINI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.ClassifyMergePacks(ctx, []string{ordinary, namespace})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Packs) != 2 || result.Packs[0].Family != "ordinary" ||
		result.Packs[0].Dialect != "gimi" || !result.Packs[0].AllowsClassic {
		t.Fatalf("ordinary classification = %#v", result.Packs)
	}
	if result.Packs[1].Family != "namespace_merge" || result.Packs[1].PrimaryIniPath == nil {
		t.Fatalf("namespace classification = %#v", result.Packs[1])
	}
}

func TestClassicMergeCreatesOutputAndDisablesOriginals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	first := filepath.Join(group, "First")
	second := filepath.Join(group, "Second")
	for index, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		ini := "[TextureOverrideBodyPosition]\nhash = aabbccdd\nvb0 = ResourceBody\n\n" +
			"[ResourceBody]\ntype = Buffer\nfilename = body.buf\n"
		if err := os.WriteFile(filepath.Join(path, "mod.ini"), []byte(ini), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "body.buf"), []byte{byte(index)}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	request := MergeModsRequest{
		GroupPath: group, Placement: "new_folder", PackName: "Combined",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "classic", Name: "Combined",
			ForwardKey: "F1", BackKey: "", IncludeVanilla: false,
			Children: []MergePlanNode{{Kind: "leaf", Path: first}, {Kind: "leaf", Path: second}},
		},
	}
	result, err := service.MergeMods(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	mergedINI := filepath.Join(result.OutputPath, "merged.ini")
	raw, err := os.ReadFile(mergedINI)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "$swapvar = 0,1") ||
		!strings.Contains(text, "vb0 = ResourceBody.0") ||
		!strings.Contains(text, "vb0 = ResourceBody.1") {
		t.Fatalf("merged ini = %s", text)
	}
	for _, name := range []string{"DISABLED First", "DISABLED Second"} {
		if _, err := os.Stat(filepath.Join(group, name)); err != nil {
			t.Fatalf("disabled original %q missing: %v", name, err)
		}
	}
}

func TestClassicMergeRollsBackCreatedFolderOnCopyFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	first := filepath.Join(group, "First")
	second := filepath.Join(group, "Second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		ini := "[TextureOverrideBodyPosition]\nhash = aabbccdd\nvb0 = ResourceBody\n"
		if err := os.WriteFile(filepath.Join(path, "mod.ini"), []byte(ini), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	linkTarget := filepath.Join(root, "outside.bin")
	if err := os.WriteFile(linkTarget, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkTarget, filepath.Join(second, "escape.bin")); err != nil {
		t.Skipf("file symlink is unavailable: %v", err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	request := MergeModsRequest{
		GroupPath: group, Placement: "new_folder", PackName: "WillRollback",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "classic", Name: "WillRollback", ForwardKey: "F1",
			Children: []MergePlanNode{{Kind: "leaf", Path: first}, {Kind: "leaf", Path: second}},
		},
	}
	if _, err := service.MergeMods(ctx, request); err == nil {
		t.Fatal("merge unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(group, "WillRollback")); !os.IsNotExist(err) {
		t.Fatalf("rollback folder remains: %v", err)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("original missing after rollback: %s: %v", path, err)
		}
	}
}

func TestNamespaceMergeWrapsChildrenAndKeepsBackups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	first := filepath.Join(group, "First")
	second := filepath.Join(group, "Second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		ini := "[TextureOverrideBodyPosition]\nhash = aabbccdd\nvb0 = ResourceBody\n"
		if err := os.WriteFile(filepath.Join(path, "mod.ini"), []byte(ini), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.MergeMods(ctx, MergeModsRequest{
		GroupPath: group, Placement: "new_folder", PackName: "Wardrobe",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Wardrobe",
			ForwardKey: "F1", BackKey: "F2", IncludeVanilla: true,
			Children: []MergePlanNode{{Kind: "leaf", Path: first}, {Kind: "leaf", Path: second}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	master, err := os.ReadFile(filepath.Join(result.OutputPath, "MasterWardrobe.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(master), "namespace = Wardrobe\\Master") ||
		!strings.Contains(string(master), "$swapvar = 0,1,2") {
		t.Fatalf("master = %s", master)
	}
	for index, name := range []string{"First", "Second"} {
		childRoot := filepath.Join(result.OutputPath, name)
		wrapped, err := os.ReadFile(filepath.Join(childRoot, "mod.ini"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(wrapped), fmt.Sprintf("if $\\Wardrobe\\Master\\swapvar==%d", index+1)) {
			t.Fatalf("wrapped %s = %s", name, wrapped)
		}
		if _, err := os.Stat(filepath.Join(childRoot, "DISABLED_BACKUP_mod.ini")); err != nil {
			t.Fatalf("namespace backup missing: %v", err)
		}
	}
}

func TestWatchGameDebouncesFilesystemEventsAndCloses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	if err := os.MkdirAll(modsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	events := make(chan string, 4)
	service.emit = func(name string, _ ...any) { events <- name }
	if err := service.WatchGame(ctx, "Game"); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(modsRoot, "Character")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(child, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event != "mod:update-game" {
			t.Fatalf("event = %q", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watcher event")
	}
	select {
	case duplicate := <-events:
		t.Fatalf("watcher did not debounce: %q", duplicate)
	case <-time.After(250 * time.Millisecond):
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestDisableINIForMergeKeepsUserDisabledFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	active := filepath.Join(root, "MasterBeta.ini")
	disabled := filepath.Join(root, "DISABLEDMasterBeta.ini")
	if err := os.WriteFile(active, []byte("active-master"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disabled, []byte("stale-backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	var created []mergeRollback
	if err := disableINIForMerge(active, &created); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Fatal("active INI should have been moved")
	}
	userDisabled, err := os.ReadFile(disabled)
	if err != nil || string(userDisabled) != "stale-backup" {
		t.Fatalf("user DISABLED file = %q, %v", userDisabled, err)
	}
	backup, err := os.ReadFile(filepath.Join(root, "DISABLED_BACKUP_MasterBeta.ini"))
	if err != nil || string(backup) != "active-master" {
		t.Fatalf("backup = %q, %v", backup, err)
	}
}
