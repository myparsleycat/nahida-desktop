package db

import (
	"bytes"
	"context"
	"testing"
)

func TestTableAccessorsRoundTrip(t *testing.T) {
	t.Parallel()

	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	theme := "dark"
	if err := client.Settings.Insert(ctx, SettingRow{Key: "theme", Value: &theme}); err != nil {
		t.Fatalf("settings insert: %v", err)
	}
	if err := client.Settings.InsertIfMissing(ctx, "theme", ptr("ignored")); err != nil {
		t.Fatalf("insert if missing: %v", err)
	}
	if err := client.Settings.UpdateValue(ctx, "theme", ptr("light")); err != nil {
		t.Fatalf("update value: %v", err)
	}
	if err := client.Settings.Upsert(ctx, "locale", ptr("ko")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	listed, err := client.Settings.List(ctx)
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("settings list = %d, want 2", len(listed))
	}
	gotTheme, err := client.Settings.GetValue(ctx, "theme")
	if err != nil || gotTheme == nil || *gotTheme != "light" {
		t.Fatalf("theme = %v %v", gotTheme, err)
	}
	if err := client.Settings.Delete(ctx, "locale"); err != nil {
		t.Fatalf("delete setting: %v", err)
	}
	if remaining, err := client.Settings.List(ctx); err != nil || len(remaining) != 1 {
		t.Fatalf("after delete: %v %v", remaining, err)
	}

	if err := client.AppState.Upsert(ctx, "nav:home", "1", "t1"); err != nil {
		t.Fatalf("app state upsert: %v", err)
	}
	if err := client.AppState.Upsert(ctx, "nav:other", "2", "t2"); err != nil {
		t.Fatalf("app state upsert 2: %v", err)
	}
	if err := client.AppState.Upsert(ctx, "cache:x", "3", "t3"); err != nil {
		t.Fatalf("app state upsert 3: %v", err)
	}
	prefixed, err := client.AppState.ListByPrefix(ctx, "nav:")
	if err != nil || len(prefixed) != 2 {
		t.Fatalf("prefix list: %v %v", prefixed, err)
	}
	if all, err := client.AppState.List(ctx); err != nil || len(all) != 3 {
		t.Fatalf("app state list: %v %v", all, err)
	}
	if err := client.AppState.Delete(ctx, "cache:x"); err != nil {
		t.Fatalf("delete app state: %v", err)
	}

	if err := client.GamePaths.Insert(ctx, GamePathRow{
		Game:          "GI",
		ModFolderPath: "C:/gi/mods",
		Importer:      ptr("XXMI"),
	}); err != nil {
		t.Fatalf("insert game: %v", err)
	}
	if err := client.GamePaths.Insert(ctx, GamePathRow{
		Game:          "HSR",
		ModFolderPath: "C:/hsr/mods",
	}); err != nil {
		t.Fatalf("insert game 2: %v", err)
	}
	if err := client.GamePaths.Update(ctx, "GI", GamePathUpdates{
		ModFolderPath:      "C:/gi/mods2",
		Importer:           ptr("XXMI"),
		GameInstallPath:    ptr("C:/gi"),
		GameExecutablePath: ptr("C:/gi/GenshinImpact.exe"),
	}); err != nil {
		t.Fatalf("update game: %v", err)
	}
	if err := client.GamePaths.SetNteLauncherPath(ctx, "GI", "C:/gi/launcher.exe"); err != nil {
		t.Fatalf("set nte: %v", err)
	}
	if err := client.GamePaths.Reorder(ctx, []string{"HSR", "GI"}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	games, err := client.GamePaths.List(ctx)
	if err != nil || len(games) != 2 || games[0].Game != "HSR" || games[1].Game != "GI" {
		t.Fatalf("game list order: %+v %v", games, err)
	}
	found, err := client.GamePaths.FindByGameOrModFolderPath(ctx, "nope", "C:/gi/mods2")
	if err != nil || found == nil || found.Game != "GI" {
		t.Fatalf("find by folder: %+v %v", found, err)
	}
	other, err := client.GamePaths.FindByModFolderPathOtherGame(ctx, "HSR", "C:/gi/mods2")
	if err != nil || other == nil || other.Game != "GI" {
		t.Fatalf("find other game: %+v %v", other, err)
	}

	if err := client.ModPresets.Insert(ctx, ModPresetRow{
		ID: "p1", Game: "GI", Name: "Default", ItemCount: 1, CreatedAt: "c", UpdatedAt: "u", Version: 1,
	}); err != nil {
		t.Fatalf("insert preset: %v", err)
	}
	if err := client.ModPresets.UpdateName(ctx, "p1", "Renamed", "u2"); err != nil {
		t.Fatalf("rename preset: %v", err)
	}
	byName, err := client.ModPresets.FindByGameAndName(ctx, "GI", "Renamed")
	if err != nil || byName == nil {
		t.Fatalf("find preset: %v", err)
	}
	if listedPresets, err := client.ModPresets.ListByGame(ctx, "GI"); err != nil || len(listedPresets) != 1 {
		t.Fatalf("list presets: %v %v", listedPresets, err)
	}

	if err := client.ModPresetItems.InsertMany(ctx, []ModPresetItemRow{{
		PresetID: "p1", ModKey: "mod-a", RelativePath: "a", GroupRelativePath: "g", FolderName: "A", IsEnabled: true, ItemOrder: 1,
	}, {
		PresetID: "p1", ModKey: "mod-b", RelativePath: "b", GroupRelativePath: "g", FolderName: "B", IsEnabled: false, ItemOrder: 2,
	}}); err != nil {
		t.Fatalf("insert items: %v", err)
	}
	items, err := client.ModPresetItems.ListByPresetID(ctx, "p1")
	if err != nil || len(items) != 2 || !items[0].IsEnabled || items[1].IsEnabled {
		t.Fatalf("boolean items: %+v %v", items, err)
	}

	blob := []byte{0, 1, 2, 3, 255}
	if err := client.ImageCache.InsertIgnore(ctx, ImageCacheRow{Hash: "abc", Image: blob, Size: int64(len(blob))}); err != nil {
		t.Fatalf("image insert: %v", err)
	}
	if err := client.ImageCache.InsertIgnore(ctx, ImageCacheRow{Hash: "abc", Image: []byte{9}, Size: 1}); err != nil {
		t.Fatalf("image insert ignore: %v", err)
	}
	cached, err := client.ImageCache.GetByHash(ctx, "abc")
	if err != nil || cached == nil || !bytes.Equal(cached.Image, blob) || cached.Size != int64(len(blob)) {
		t.Fatalf("blob round-trip: %+v %v", cached, err)
	}
	if sum, err := client.ImageCache.SumSize(ctx); err != nil || sum != int64(len(blob)) {
		t.Fatalf("sum size: %d %v", sum, err)
	}
	if err := client.ImageCache.DeleteAll(ctx); err != nil {
		t.Fatalf("delete images: %v", err)
	}

	if err := client.TouchProfileVisionCache.Upsert(ctx, TouchProfileVisionCacheRow{CacheKey: "k", Result: "{}", UpdatedAt: "now"}); err != nil {
		t.Fatalf("touch upsert: %v", err)
	}
	if touch, err := client.TouchProfileVisionCache.Get(ctx, "k"); err != nil || touch == nil || touch.Result != "{}" {
		t.Fatalf("touch get: %v %v", touch, err)
	}
	if err := client.TouchProfileVisionCache.DeleteAll(ctx); err != nil {
		t.Fatalf("touch delete: %v", err)
	}

	if err := client.ModScanCache.Upsert(ctx, ModScanCacheRow{
		Path: "C:/mods/a", Mtime: 123, Payload: `[]`, UpdatedAt: "now",
	}); err != nil {
		t.Fatalf("mod scan upsert: %v", err)
	}
	if err := client.ModScanCache.UpsertMany(ctx, []ModScanCacheRow{{
		Path: "C:/mods/b", Mtime: 456, Payload: `[{"name":"mod.ini"}]`, UpdatedAt: "later",
	}}); err != nil {
		t.Fatalf("mod scan upsert many: %v", err)
	}
	scanRow, err := client.ModScanCache.Get(ctx, "C:/mods/a")
	if err != nil || scanRow == nil || scanRow.Mtime != 123 || scanRow.Payload != `[]` {
		t.Fatalf("mod scan get: %+v %v", scanRow, err)
	}
	many, err := client.ModScanCache.GetMany(ctx, []string{"C:/mods/a", "C:/mods/b", "C:/mods/missing"})
	if err != nil || len(many) != 2 || many["C:/mods/b"].Mtime != 456 {
		t.Fatalf("mod scan get many: %+v %v", many, err)
	}

	src := []byte("print('ok')")
	if err := client.Scripts.Insert(ctx, ScriptRow{
		ID: "s1", Name: "hello", Source: src, IsSrcZstd: false, Type: ScriptTypePython, Size: int64(len(src)), SHA256: "deadbeef",
	}); err != nil {
		t.Fatalf("script insert: %v", err)
	}
	compressed := []byte{0x28, 0xb5, 0x2f, 0xfd}
	if err := client.Scripts.UpdateCompressedSource(ctx, "s1", compressed, "zstdhash", int64(len(compressed))); err != nil {
		t.Fatalf("compress script: %v", err)
	}
	script, err := client.Scripts.FindBySHA256OrName(ctx, "deadbeef", "nope")
	if err != nil || script == nil || !script.IsSrcZstd || !bytes.Equal(script.Source, compressed) {
		t.Fatalf("script blob/bool: %+v %v", script, err)
	}
	if basic, err := client.Scripts.ListBasic(ctx); err != nil || len(basic) != 1 {
		t.Fatalf("list basic: %v %v", basic, err)
	}

	if err := client.ScriptPresets.Insert(ctx, ScriptPresetRow{ID: "sp1", Name: "bundle"}); err != nil {
		t.Fatalf("script preset: %v", err)
	}
	if err := client.ScriptPresetItems.InsertMany(ctx, []ScriptPresetItemRow{{PresetID: "sp1", ScriptID: "s1", Order: 1}}); err != nil {
		t.Fatalf("script items: %v", err)
	}
	withScripts, err := client.ScriptPresets.ListWithScripts(ctx)
	if err != nil || len(withScripts) != 1 || len(withScripts[0].Scripts) != 1 {
		t.Fatalf("list with scripts: %+v %v", withScripts, err)
	}
	usage, err := client.ScriptPresetItems.FindUsageByScriptID(ctx, "s1")
	if err != nil || usage == nil || usage.PresetName != "bundle" {
		t.Fatalf("usage: %+v %v", usage, err)
	}
	if foundPreset, err := client.ScriptPresets.FindByName(ctx, "bundle"); err != nil || foundPreset == nil {
		t.Fatalf("find preset name: %v", err)
	}
	if with, err := client.ScriptPresets.FindByIDWithScripts(ctx, "sp1"); err != nil || with == nil || len(with.Scripts) != 1 {
		t.Fatalf("find with scripts: %v %v", with, err)
	}

	if err := client.ToggleViewerArtifacts.Upsert(ctx, ToggleViewerArtifactRow{
		ID: "tv1", TargetIniPath: "a.ini", ToggleTxtPath: "a.txt", ToggleIniPath: "t.ini",
		ToggleTxtHash: "h1", ToggleIniHash: "h2", UpdatedAt: "t",
	}); err != nil {
		t.Fatalf("toggle upsert: %v", err)
	}
	if err := client.ToggleViewerArtifacts.UpdateHashes(ctx, "tv1", "h3", "t2"); err != nil {
		t.Fatalf("toggle hashes: %v", err)
	}
	toggles, err := client.ToggleViewerArtifacts.ListByTargetIniPath(ctx, "a.ini")
	if err != nil || len(toggles) != 1 || toggles[0].ToggleIniHash != "h3" {
		t.Fatalf("toggle list: %+v %v", toggles, err)
	}
	if all, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(all) != 1 {
		t.Fatalf("toggle all: %v %v", all, err)
	}
	if err := client.ToggleViewerArtifacts.DeleteByIDAndTargetIniPath(ctx, "tv1", "a.ini"); err != nil {
		t.Fatalf("toggle delete: %v", err)
	}

	if err := client.SchemaState.Upsert(ctx, "manual", "x", "now"); err != nil {
		t.Fatalf("schema upsert: %v", err)
	}
	if row, err := client.SchemaState.Get(ctx, "manual"); err != nil || row == nil || row.Value != "x" {
		t.Fatalf("schema get: %v %v", row, err)
	}

	if err := client.ScriptPresets.Delete(ctx, "sp1"); err != nil {
		t.Fatalf("delete script preset: %v", err)
	}
	if err := client.Scripts.Delete(ctx, "s1"); err != nil {
		t.Fatalf("delete script: %v", err)
	}
	if err := client.ModPresets.Delete(ctx, "p1"); err != nil {
		t.Fatalf("delete mod preset: %v", err)
	}
	if leftover, err := client.ModPresetItems.ListByPresetID(ctx, "p1"); err != nil || len(leftover) != 0 {
		t.Fatalf("cascade items: %v %v", leftover, err)
	}
	if err := client.GamePaths.Delete(ctx, "HSR"); err != nil {
		t.Fatalf("delete game: %v", err)
	}
}

func ptr(v string) *string { return &v }
