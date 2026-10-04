package mod

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/mod/metadata"
)

func newClassificationFixture(t *testing.T) (*Mod, string) {
	t.Helper()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	for _, path := range []string{
		filepath.Join(modsRoot, "Diluc", "Costume"),
		filepath.Join(modsRoot, "Furina"),
		filepath.Join(root, "other-mods", "Acheron"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Other", filepath.Join(root, "other-mods"), nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	return service, root
}

func characterClassifications(t *testing.T, service *Mod, game string) map[string]map[string]string {
	t.Helper()
	characters, err := service.GetCharacters(context.Background(), game, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]map[string]string{}
	for _, character := range characters {
		result[character.Name] = character.Classifications
	}
	return result
}

func TestSaveClassificationValidatesAndReplacesGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, _ := newClassificationFixture(t)

	element, err := service.SaveClassification(ctx, "Game", nil, " Element ", []ClassificationGroup{
		{Name: "Pyro"}, {Name: " Hydro "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if element.Name != "Element" || len(element.Groups) != 2 || element.Groups[1].Name != "Hydro" ||
		element.Groups[0].ID == "" || element.Active {
		t.Fatalf("element = %#v", element)
	}

	for name, call := range map[string]func() error{
		"INVALID_CLASSIFICATION_NAME": func() error {
			_, err := service.SaveClassification(ctx, "Game", nil, "  ", nil)
			return err
		},
		"CLASSIFICATION_NAME_EXISTS": func() error {
			_, err := service.SaveClassification(ctx, "Game", nil, "Element", nil)
			return err
		},
		"CLASSIFICATION_GROUP_NAME_EXISTS": func() error {
			_, err := service.SaveClassification(
				ctx, "Game", &element.ID, "Element", []ClassificationGroup{{Name: "Pyro"}, {Name: "Pyro"}},
			)
			return err
		},
		"CLASSIFICATION_NOT_FOUND": func() error {
			_, err := service.SaveClassification(
				ctx, "Game", &element.ID, "Element", []ClassificationGroup{{ID: "unknown", Name: "Pyro"}},
			)
			return err
		},
	} {
		if err := call(); err == nil || err.Error() != name {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	if _, err := service.SaveClassification(ctx, "Other", &element.ID, "Element", nil); err == nil ||
		err.Error() != "CLASSIFICATION_NOT_FOUND" {
		t.Fatalf("cross-game save error = %v", err)
	}
	if _, err := service.SaveClassification(ctx, "Other", nil, "Element", nil); err != nil {
		t.Fatalf("same name in another game: %v", err)
	}

	updated, err := service.SaveClassification(ctx, "Game", &element.ID, "Elements", []ClassificationGroup{
		{Name: "Cryo"}, {ID: element.Groups[0].ID, Name: "Fire"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != element.ID || updated.Name != "Elements" || len(updated.Groups) != 2 ||
		updated.Groups[0].Name != "Cryo" || updated.Groups[1] != (ClassificationGroup{element.Groups[0].ID, "Fire"}) {
		t.Fatalf("updated = %#v", updated)
	}

	if err := service.SetActiveClassification(ctx, "Other", &element.ID); err == nil ||
		err.Error() != "CLASSIFICATION_NOT_FOUND" {
		t.Fatalf("cross-game activate error = %v", err)
	}
	if err := service.SetActiveClassification(ctx, "Game", &element.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := service.GetClassifications(ctx, "Game")
	if err != nil || len(listed) != 1 || !listed[0].Active {
		t.Fatalf("listed = %#v, err = %v", listed, err)
	}
	if err := service.DeleteClassification(ctx, element.ID); err != nil {
		t.Fatal(err)
	}
	if listed, err := service.GetClassifications(ctx, "Game"); err != nil || len(listed) != 0 {
		t.Fatalf("after delete = %#v, err = %v", listed, err)
	}
}

func TestCharacterClassificationFollowsRenamedFolder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newClassificationFixture(t)
	modsRoot := filepath.Join(root, "mods")
	diluc := filepath.Join(modsRoot, "Diluc")
	furina := filepath.Join(modsRoot, "Furina")

	// Folders written before any classification exists must not be read or reported.
	if err := metadata.Write(furina, []byte(`{"preview":"cover.png","classifications":"broken"}`)); err != nil {
		t.Fatal(err)
	}
	if got := characterClassifications(t, service, "Game"); got["Furina"] != nil {
		t.Fatalf("classifications without definitions = %#v", got)
	}

	element, err := service.SaveClassification(
		ctx, "Game", nil, "Element", []ClassificationGroup{{Name: "Pyro"}, {Name: "Hydro"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	weapon, err := service.SaveClassification(ctx, "Game", nil, "Weapon", []ClassificationGroup{{Name: "Claymore"}})
	if err != nil {
		t.Fatal(err)
	}
	pyro, hydro, claymore := element.Groups[0].ID, element.Groups[1].ID, weapon.Groups[0].ID

	if err := service.SetCharacterClassification(ctx, furina, element.ID, nil); err != nil {
		t.Fatalf("clear without assignment: %v", err)
	}
	if err := service.SetCharacterClassification(ctx, diluc, element.ID, nil); err != nil {
		t.Fatalf("clear without metadata: %v", err)
	}
	if _, err := os.Stat(filepath.Join(diluc, "nhd.json")); !os.IsNotExist(err) {
		t.Fatalf("clearing created metadata: %v", err)
	}
	for _, assignment := range []struct{ path, classification, group string }{
		{diluc, element.ID, pyro}, {diluc, weapon.ID, claymore}, {furina, element.ID, hydro},
	} {
		if err := service.SetCharacterClassification(
			ctx, assignment.path, assignment.classification, &assignment.group,
		); err != nil {
			t.Fatal(err)
		}
	}

	renamed := filepath.Join(modsRoot, "Diluc Ragnvindr")
	if err := os.Rename(diluc, renamed); err != nil {
		t.Fatal(err)
	}
	got := characterClassifications(t, service, "Game")
	if len(got) != 2 || got["Diluc Ragnvindr"][element.ID] != pyro || got["Diluc Ragnvindr"][weapon.ID] != claymore ||
		len(got["Furina"]) != 1 || got["Furina"][element.ID] != hydro {
		t.Fatalf("classifications = %#v", got)
	}

	raw, err := metadata.Read(furina)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Preview         string            `json:"preview"`
		Classifications map[string]string `json:"classifications"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Preview != "cover.png" {
		t.Fatalf("metadata = %s, err = %v", raw, err)
	}

	if err := service.SetCharacterClassification(ctx, renamed, weapon.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.SetCharacterClassification(ctx, furina, element.ID, nil); err != nil {
		t.Fatal(err)
	}
	got = characterClassifications(t, service, "Game")
	if len(got["Diluc Ragnvindr"]) != 1 || got["Diluc Ragnvindr"][element.ID] != pyro || got["Furina"] != nil {
		t.Fatalf("classifications after clear = %#v", got)
	}
	if raw, err := metadata.Read(furina); err != nil || string(raw) != `{"preview":"cover.png"}` {
		t.Fatalf("metadata after clear = %s, err = %v", raw, err)
	}
}

func TestSetCharacterClassificationRejectsInvalidTargets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newClassificationFixture(t)
	modsRoot := filepath.Join(root, "mods")
	element, err := service.SaveClassification(ctx, "Game", nil, "Element", []ClassificationGroup{{Name: "Pyro"}})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := service.SaveClassification(ctx, "Other", nil, "Path", []ClassificationGroup{{Name: "Nihility"}})
	if err != nil {
		t.Fatal(err)
	}
	pyro := element.Groups[0].ID

	for _, test := range []struct {
		name, path, classification, group, want string
	}{
		{"nested folder", filepath.Join(modsRoot, "Diluc", "Costume"), element.ID, pyro, "INVALID_CLASSIFICATION_PATH"},
		{"game root", modsRoot, element.ID, pyro, "INVALID_CLASSIFICATION_PATH"},
		{"outside managed roots", root, element.ID, pyro, "INVALID_CLASSIFICATION_PATH"},
		{"missing folder", filepath.Join(modsRoot, "Missing"), element.ID, pyro, "INVALID_CLASSIFICATION_PATH"},
		{"other game's classification", filepath.Join(modsRoot, "Diluc"), foreign.ID, foreign.Groups[0].ID,
			"CLASSIFICATION_NOT_FOUND"},
		{"other classification's group", filepath.Join(modsRoot, "Diluc"), element.ID, foreign.Groups[0].ID,
			"CLASSIFICATION_NOT_FOUND"},
		{"unknown classification", filepath.Join(modsRoot, "Diluc"), "unknown", pyro, "CLASSIFICATION_NOT_FOUND"},
	} {
		err := service.SetCharacterClassification(ctx, test.path, test.classification, &test.group)
		if err == nil || err.Error() != test.want {
			t.Fatalf("%s: error = %v, want %s", test.name, err, test.want)
		}
	}
	if got := characterClassifications(t, service, "Game"); got["Diluc"] != nil || got["Furina"] != nil {
		t.Fatalf("rejected assignments were written: %#v", got)
	}
}
