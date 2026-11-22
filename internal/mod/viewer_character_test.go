package mod

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/mod/metadata"
	"nahida.live/desktop/internal/platform"
)

func TestViewerCharacterOverridesOnlyTheOpenedMod(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	service, root := newClassificationFixture(t)
	parent := filepath.Join(root, "mods", "Diluc")
	modPath := filepath.Join(parent, "Costume")
	sibling := filepath.Join(parent, "Other Costume")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := metadata.Write(modPath, []byte(`{"viewerTextureChoices":{"face":"game"}}`)); err != nil {
		t.Fatal(err)
	}
	diluc := CharacterAssignment{Game: "GIMI", ID: "diluc"}
	if err := service.SetFolderCharacter(ctx, parent, &diluc); err != nil {
		t.Fatal(err)
	}
	openedPath := openedCharacterPath(t, modPath)
	before, err := service.GetViewerCharacter(ctx, modPath, openedPath)
	if err != nil || before.HasOverride || before.Inherited == nil || before.Inherited.CharacterAssignment != diluc {
		t.Fatalf("initial character = %+v, %v; want inherited Diluc", before, err)
	}

	for _, id := range []string{"furina", "amber"} {
		assignment := CharacterAssignment{Game: "GIMI", ID: id}
		if err := service.SetViewerCharacter(ctx, modPath, openedPath, &assignment); err != nil {
			t.Fatal(err)
		}
		got, err := service.GetViewerCharacter(ctx, modPath, openedPath)
		if err != nil || !got.HasOverride || got.Override == nil || *got.Override != assignment {
			t.Fatalf("reopened character = %+v, %v; want direct %s", got, err, id)
		}
	}
	other, err := service.GetViewerCharacter(ctx, sibling, openedCharacterPath(t, sibling))
	if err != nil || other.HasOverride || other.Inherited == nil || other.Inherited.CharacterAssignment != diluc {
		t.Fatalf("sibling character = %+v, %v; want inherited Diluc", other, err)
	}

	if err := service.SetViewerCharacter(ctx, modPath, openedPath, nil); err != nil {
		t.Fatal(err)
	}
	after, err := service.GetViewerCharacter(ctx, modPath, openedPath)
	if err != nil || after.HasOverride || after.Inherited == nil || after.Inherited.CharacterAssignment != diluc {
		t.Fatalf("cleared character = %+v, %v; want inherited Diluc", after, err)
	}
	raw, err := metadata.Read(modPath)
	if err != nil {
		t.Fatal(err)
	}
	var remaining map[string]map[string]string
	if err := json.Unmarshal(raw, &remaining); err != nil || len(remaining) != 1 ||
		remaining["viewerTextureChoices"]["face"] != "game" {
		t.Fatalf("remaining metadata = %s, %v; want original texture choice", raw, err)
	}
}

func TestViewerCharacterPersistsOutsideManagedRoots(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	service, _ := newClassificationFixture(t)
	root := t.TempDir()
	modPath := filepath.Join(root, "External Mod")
	if err := os.Mkdir(modPath, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "Explorer Alias")
	createJunction(t, modPath, link)
	openedPath := openedCharacterPath(t, link)
	before, err := service.GetViewerCharacter(ctx, link, openedPath)
	if err != nil || before.Inherited != nil || before.HasOverride {
		t.Fatalf("external folder = %+v, %v; want no assignment", before, err)
	}
	assignment := CharacterAssignment{Game: "GIMI", ID: "furina"}
	if err := service.SetViewerCharacter(ctx, link, openedPath, &assignment); err != nil {
		t.Fatal(err)
	}
	got, err := service.GetViewerCharacter(ctx, modPath, openedPath)
	if err != nil || !got.DirectoryMatched || got.Override == nil ||
		*got.Override != assignment {
		t.Fatalf("external folder reopened = %+v, %v; want direct Furina", got, err)
	}
	other := filepath.Join(root, "Other Mod")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	otherPath := openedCharacterPath(t, other)
	amber := CharacterAssignment{Game: "GIMI", ID: "amber"}
	if err := service.SetViewerCharacter(ctx, other, otherPath, &amber); err != nil {
		t.Fatal(err)
	}
	beforeRetarget := make(map[string][]byte, 2)
	for _, path := range []string{modPath, other} {
		raw, err := metadata.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		beforeRetarget[path] = raw
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	createJunction(t, other, link)
	if got, err := service.GetViewerCharacter(ctx, link, openedPath); err == nil || got.DirectoryMatched ||
		got.Override != nil {
		t.Fatalf("character for stale scene = %+v, %v; want path mismatch", got, err)
	}
	for _, value := range []*CharacterAssignment{nil, &assignment} {
		if err := service.SetViewerCharacter(ctx, link, openedPath, value); err == nil {
			t.Fatalf("updated character with stale scene: %+v", value)
		}
	}
	for path, want := range beforeRetarget {
		if raw, err := metadata.Read(path); err != nil || !bytes.Equal(raw, want) {
			t.Fatalf("metadata changed for %s: %s, %v", path, raw, err)
		}
	}
	for path, want := range map[string]CharacterAssignment{modPath: assignment, link: amber} {
		got, err := service.GetViewerCharacter(ctx, path, openedCharacterPath(t, path))
		if err != nil || got.Override == nil || *got.Override != want {
			t.Fatalf("reopened %s = %+v, %v; want %+v", path, got, err, want)
		}
	}
}

func TestViewerCharacterRejectsUnsafeMetadata(t *testing.T) {
	t.Parallel()
	assignment := CharacterAssignment{Game: "GIMI", ID: "furina"}
	for _, tc := range []struct {
		name    string
		content string
		link    bool
	}{
		{"invalid JSON", `{"viewerCharacter":`, false},
		{"redirected file", `{"viewerCharacter":{"game":"GIMI","id":"diluc"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service, _ := newClassificationFixture(t)
			modPath := t.TempDir()
			metadataPath := filepath.Join(modPath, "nhd.json")
			target := metadataPath
			if tc.link {
				target = filepath.Join(t.TempDir(), "external.json")
			}
			if err := os.WriteFile(target, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.link {
				if err := os.Symlink(target, metadataPath); err != nil {
					if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
						t.Skipf("file symlinks are unavailable: %v", err)
					}
					t.Fatal(err)
				}
			}
			openedPath := openedCharacterPath(t, modPath)
			before, err := service.GetViewerCharacter(t.Context(), modPath, openedPath)
			if err == nil || before.Override != nil {
				t.Fatalf("unsafe metadata was accepted: %+v, %v", before, err)
			}
			if err := service.SetViewerCharacter(t.Context(), modPath, openedPath, &assignment); err == nil {
				t.Fatal("unsafe metadata was replaced")
			}
			if got, err := os.ReadFile(metadataPath); err != nil || !bytes.Equal(got, []byte(tc.content)) {
				t.Fatalf("metadata after failed save = %q, %v; want %s", got, err, tc.content)
			}
		})
	}
}

func openedCharacterPath(t *testing.T, path string) string {
	t.Helper()
	openedPath, err := platform.FinalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return openedPath
}
