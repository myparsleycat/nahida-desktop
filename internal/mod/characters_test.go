package mod

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/mod/metadata"
	"nahida.live/desktop/internal/platform"
)

// The viewer finds a mod's character by walking up from the mod, so the nearest assigned folder must win even when
// folders in between are toggled off, and clearing must leave the folder's other metadata alone.
func TestFolderCharacterResolvesNearestAssignment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newClassificationFixture(t)
	character := filepath.Join(root, "mods", "DISABLED Diluc")
	costume := filepath.Join(character, "Costume")
	mod := filepath.Join(costume, "DISABLED Mod")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := metadata.Write(character, []byte(`{"classifications":{"a":"b"}}`)); err != nil {
		t.Fatal(err)
	}

	diluc := &CharacterAssignment{Game: "GIMI", ID: "diluc"}
	if err := service.SetFolderCharacter(ctx, character, diluc); err != nil {
		t.Fatal(err)
	}
	if got, err := service.ResolveModCharacter(
		ctx,
		mod,
	); err != nil || got == nil ||
		got.CharacterAssignment != *diluc ||
		!platform.SamePathFold(got.Folder, character) {
		t.Fatalf("resolved %+v, %v; want diluc from %s", got, err, character)
	}

	nearer := &CharacterAssignment{Game: "GIMI", ID: "furina"}
	if err := service.SetFolderCharacter(ctx, costume, nearer); err != nil {
		t.Fatal(err)
	}
	if got, err := service.ResolveModCharacter(
		ctx,
		mod,
	); err != nil || got == nil ||
		got.CharacterAssignment != *nearer {
		t.Fatalf("resolved %+v, %v; want the nearer furina", got, err)
	}

	if err := service.SetFolderCharacter(ctx, character, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := metadata.Read(character)
	var document map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(raw, &document)
	}
	if _, kept := document["classifications"]; err != nil || !kept || document["character"] != nil {
		t.Fatalf("cleared metadata = %s, %v; want only classifications kept", raw, err)
	}
	furina := filepath.Join(root, "mods", "Furina")
	if err := service.SetFolderCharacter(ctx, furina, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(furina, "nhd.json")); !os.IsNotExist(err) {
		t.Fatalf("clearing an unassigned folder created nhd.json: %v", err)
	}
}

func TestSetFolderCharacterRejectsInvalidTargets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newClassificationFixture(t)
	modsRoot := filepath.Join(root, "mods")
	diluc := &CharacterAssignment{Game: "GIMI", ID: "diluc"}

	for _, test := range []struct {
		name, path string
		assignment *CharacterAssignment
		want       string
	}{
		{"game root", modsRoot, diluc, "INVALID_CHARACTER_PATH"},
		{"outside managed roots", root, diluc, "INVALID_CHARACTER_PATH"},
		{"unknown character", filepath.Join(modsRoot, "Diluc"), &CharacterAssignment{Game: "GIMI", ID: "nobody"},
			"CHARACTER_NOT_FOUND"},
		{"unknown game", filepath.Join(modsRoot, "Diluc"), &CharacterAssignment{Game: "Game", ID: "diluc"},
			"CHARACTER_NOT_FOUND"},
	} {
		err := service.SetFolderCharacter(ctx, test.path, test.assignment)
		if err == nil || err.Error() != test.want {
			t.Errorf("%s: error = %v, want %s", test.name, err, test.want)
		}
	}
	if got, err := service.ResolveModCharacter(
		ctx,
		filepath.Join(modsRoot, "Diluc", "Costume"),
	); got != nil ||
		err != nil {
		t.Fatalf("rejected assignment resolved as %+v, %v", got, err)
	}
	if got, err := service.ResolveModCharacter(ctx, root); got != nil || err != nil {
		t.Fatalf("path outside managed roots resolved as %+v, %v; want nil", got, err)
	}
}
