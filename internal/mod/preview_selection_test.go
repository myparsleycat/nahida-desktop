package mod

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"nahida.live/desktop/internal/mod/metadata"
)

func TestPreviewSelectionScansAndPersists(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "group")
	modPath := filepath.Join(group, "mod")
	cover := writePreviewFile(t, modPath, "cover.jpg")
	nested := writePreviewFile(t, modPath, "images/shot.png")
	writePreviewFile(t, modPath, "textures/normal.png")
	writePreviewFile(t, modPath, "preview.mp4")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	full, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	previewEqual(t, full.Mods[0].Preview, filepath.Join(modPath, "preview.mp4"))
	if got := full.Mods[0].PreviewImages; !reflect.DeepEqual(got, []string{cover, nested}) {
		t.Fatalf("images = %v", got)
	}
	light, err := service.GetModsLight(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if got := light.Mods[0].PreviewImages; !reflect.DeepEqual(got, []string{cover, nested}) {
		t.Fatalf("light images = %v", got)
	}

	if err := service.SetDefaultPreview(ctx, modPath, nested); err != nil {
		t.Fatal(err)
	}
	raw, err := metadata.Read(modPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["preview"]) != `"images/shot.png"` {
		t.Fatalf("saved preview = %s", document["preview"])
	}
	for _, scan := range []func(string) *ModInfo{
		func(path string) *ModInfo { return scanMod(group, path) },
		func(path string) *ModInfo { return scanModLight(group, path) },
	} {
		previewEqual(t, scan(modPath).Preview, nested)
	}
	if err := os.Remove(nested); err != nil {
		t.Fatal(err)
	}
	previewEqual(t, scanMod(group, modPath).Preview, filepath.Join(modPath, "preview.mp4"))
}

func TestSetDefaultPreviewPreservesMetadataAndRejectsInvalidPaths(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "group", "mod")
	cover := writePreviewFile(t, modPath, "cover.jpg")
	texture := writePreviewFile(t, modPath, "normal.png")
	other := writePreviewFile(t, root, "outside.png")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"id":"download","number":1234567890123456789,"nested":{"keep":true}}`)
	if err := metadata.Initialize(modPath, original); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{texture, other, filepath.Join(modPath, "missing.jpg")} {
		if err := service.SetDefaultPreview(ctx, modPath, path); err == nil {
			t.Fatalf("accepted invalid image %s", path)
		}
	}
	if raw, err := metadata.Read(modPath); err != nil || string(raw) != string(original) {
		t.Fatalf("metadata after rejected selection = %s, %v", raw, err)
	}
	if err := service.SetDefaultPreview(ctx, modPath, cover); err != nil {
		t.Fatal(err)
	}
	raw, err := metadata.Read(modPath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["id"]) != `"download"` || string(fields["number"]) != `1234567890123456789` ||
		string(fields["nested"]) != `{"keep":true}` {
		t.Fatalf("lost metadata fields: %s", raw)
	}
	if err := metadata.Write(modPath, []byte("{")); err == nil {
		t.Fatal("invalid test setup accepted")
	}
	if err := os.WriteFile(filepath.Join(modPath, "nhd.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := service.SetDefaultPreview(ctx, modPath, cover); err == nil {
		t.Fatal("accepted corrupt metadata")
	}
	previewEqual(t, scanMod(filepath.Dir(modPath), modPath).Preview, cover)
}

func TestNtePreviewSelection(t *testing.T) {
	root := t.TempDir()
	modPath := filepath.Join(root, "mod")
	writePak(t, modPath, "mod.pak")
	first := writePreviewFile(t, modPath, "first.jpg")
	second := writePreviewFile(t, modPath, "images/second.png")
	if err := updateSelectedPreview(modPath, second); err != nil {
		t.Fatal(err)
	}
	entry := nteModEntry{path: modPath, name: "mod"}
	for _, info := range []ModInfo{nteModInfo(entry), nteModInfoLight(entry)} {
		previewEqual(t, info.Preview, second)
		if !reflect.DeepEqual(info.PreviewImages, []string{first, second}) {
			t.Fatalf("NTE images = %v", info.PreviewImages)
		}
	}
}

func TestPastePreviewUpdatesPreferredImage(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "group", "mod")
	original := writePreviewFile(t, modPath, "images/cover.jpg")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.SetDefaultPreview(ctx, modPath, original); err != nil {
		t.Fatal(err)
	}
	pasted, err := service.PastePreview(ctx, modPath, "data:image/png;base64,aW1hZ2U=", "base64", nil)
	if err != nil {
		t.Fatal(err)
	}
	previewEqual(t, scanMod(filepath.Dir(modPath), modPath).Preview, pasted)
	raw, err := metadata.Read(modPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Preview string `json:"preview"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Preview != "preview.png" {
		t.Fatalf("pasted selection = %s, %v", raw, err)
	}
}

func TestPasteVideoPreservesPreferredImage(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "group", "mod")
	selected := writePreviewFile(t, modPath, "images/cover.jpg")
	video := writePreviewFile(t, root, "clip.mp4")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.SetDefaultPreview(ctx, modPath, selected); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PastePreview(ctx, modPath, video, "path", nil); err != nil {
		t.Fatal(err)
	}

	previewEqual(t, scanMod(filepath.Dir(modPath), modPath).Preview, selected)
	raw, err := metadata.Read(modPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Preview string `json:"preview"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Preview != "images/cover.jpg" {
		t.Fatalf("selected image after video paste = %s, %v", raw, err)
	}
}

func TestPreviewSelectionConcurrentUpdates(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	modPath := filepath.Join(modsRoot, "group", "mod")
	cover := writePreviewFile(t, modPath, "cover.jpg")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		if err := service.SetDefaultPreview(ctx, modPath, cover); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer workers.Done()
		if err := metadata.Upsert(modPath, func(raw []byte) ([]byte, error) {
			fields := map[string]json.RawMessage{}
			if raw != nil {
				if err := json.Unmarshal(raw, &fields); err != nil {
					return nil, err
				}
			}
			fields["id"] = json.RawMessage(`"other"`)
			return json.Marshal(fields)
		}); err != nil {
			t.Error(err)
		}
	}()
	workers.Wait()
	raw, err := metadata.Read(modPath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || string(fields["id"]) != `"other"` ||
		string(fields["preview"]) != `"cover.jpg"` {
		t.Fatalf("concurrent update lost fields: %s, %v", raw, err)
	}
}

func TestRelocatedPreviewPathUsesRenameResult(t *testing.T) {
	t.Parallel()
	ancestor := filepath.Join("mods", "Outfit")
	oldPath := filepath.Join(ancestor, "DISABLED foo")
	newPath := filepath.Join(ancestor, "foo (2)")
	got, ok := relocatedPreviewPath("disabled foo/shot.jpg", ancestor, oldPath, newPath)
	if !ok || got != "foo (2)/shot.jpg" {
		t.Fatalf("relocated = %q, %v", got, ok)
	}
	if _, ok := relocatedPreviewPath("foo/shot.jpg", ancestor, oldPath, newPath); ok {
		t.Fatal("rewrote enabled duplicate")
	}
	parent := filepath.Dir(ancestor)
	got, ok = relocatedPreviewPath("Outfit/DISABLED foo/shot.jpg", parent, oldPath, newPath)
	if !ok || got != "Outfit/foo (2)/shot.jpg" {
		t.Fatalf("ancestor relocated = %q, %v", got, ok)
	}
}

func TestToggleRewritesNestedPreviewToRenamedPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	modsRoot := filepath.Join(root, "mods")
	character := filepath.Join(modsRoot, "Character")
	outfit := filepath.Join(character, "Outfit")
	disabled := filepath.Join(outfit, "DISABLED foo")
	selected := writePreviewFile(t, disabled, "shot.jpg")
	duplicate := writePreviewFile(t, filepath.Join(outfit, "foo"), "shot.jpg")
	writePreviewFile(t, outfit, "cover.jpg")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"id":"download","keep":true}`)
	for _, dir := range []string{outfit, character} {
		if err := metadata.Initialize(dir, original); err != nil {
			t.Fatal(err)
		}
	}
	for _, modPath := range []string{outfit, character} {
		if err := service.SetDefaultPreview(ctx, modPath, selected); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(root, "nhd.json")
	relative, err := filepath.Rel(root, selected)
	if err != nil {
		t.Fatal(err)
	}
	outsidePreview := []byte(`{"preview":"` + filepath.ToSlash(relative) + `","id":"outside"}`)
	if err := os.WriteFile(outside, outsidePreview, 0o644); err != nil {
		t.Fatal(err)
	}

	renamed, err := service.Toggle(ctx, disabled)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(renamed) != "foo (2)" {
		t.Fatalf("renamed = %q", renamed)
	}
	assertSavedPreview(t, outfit, "foo (2)/shot.jpg")
	assertSavedPreview(t, character, "Outfit/foo (2)/shot.jpg")
	previewEqual(t, scanMod(character, outfit).Preview, filepath.Join(renamed, "shot.jpg"))
	previewEqual(t, scanModLight(modsRoot, character).Preview, filepath.Join(renamed, "shot.jpg"))
	if _, err := os.Stat(duplicate); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != string(outsidePreview) {
		t.Fatalf("outside preview = %s, %v", raw, err)
	}

	disabledAgain, err := service.Toggle(ctx, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(disabledAgain) != "DISABLED foo (2)" {
		t.Fatalf("disabled = %q", disabledAgain)
	}
	assertSavedPreview(t, outfit, "DISABLED foo (2)/shot.jpg")
	assertSavedPreview(t, character, "Outfit/DISABLED foo (2)/shot.jpg")
	previewEqual(t, scanMod(character, outfit).Preview, filepath.Join(disabledAgain, "shot.jpg"))
}

func TestToggleEnableLeavesDuplicatePreviewUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	modsRoot := filepath.Join(root, "mods")
	character := filepath.Join(modsRoot, "Character")
	outfit := filepath.Join(character, "Outfit")
	writePreviewFile(t, filepath.Join(outfit, "DISABLED foo"), "shot.jpg")
	selected := writePreviewFile(t, filepath.Join(outfit, "foo"), "shot.jpg")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, modPath := range []string{outfit, character} {
		if err := service.SetDefaultPreview(ctx, modPath, selected); err != nil {
			t.Fatal(err)
		}
	}

	renamed, err := service.Toggle(ctx, filepath.Join(outfit, "DISABLED foo"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(renamed) != "foo (2)" {
		t.Fatalf("renamed = %q", renamed)
	}
	assertSavedPreview(t, outfit, "foo/shot.jpg")
	assertSavedPreview(t, character, "Outfit/foo/shot.jpg")
	previewEqual(t, scanMod(character, outfit).Preview, selected)
}

func TestTogglePreservesPreviewInsideRenamedMod(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{style: "space"})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Character")
	modPath := filepath.Join(group, "Outfit")
	selected := writePreviewFile(t, modPath, "images/cover.jpg")
	if err := service.AddGame(ctx, "game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.SetDefaultPreview(ctx, modPath, selected); err != nil {
		t.Fatal(err)
	}

	renamed, err := service.Toggle(ctx, modPath)
	if err != nil {
		t.Fatal(err)
	}
	assertSavedPreview(t, renamed, "images/cover.jpg")
	previewEqual(t, scanMod(group, renamed).Preview, filepath.Join(renamed, "images", "cover.jpg"))
}

func assertSavedPreview(t *testing.T, modPath, want string) {
	t.Helper()
	raw, err := metadata.Read(modPath)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	var preview string
	if err := json.Unmarshal(fields["preview"], &preview); err != nil || preview != want {
		t.Fatalf("preview = %q, want %q (%s)", preview, want, raw)
	}
	if id, ok := fields["id"]; ok && string(id) != `"download"` && string(id) != `"outside"` {
		t.Fatalf("lost id: %s", raw)
	}
	if keep, ok := fields["keep"]; ok && string(keep) != `true` {
		t.Fatalf("lost keep: %s", raw)
	}
}
