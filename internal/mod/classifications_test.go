package mod

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/mod/metadata"
	"nahida.live/desktop/internal/watcher"
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

func classifiedSubGroups(t *testing.T, service *Mod, game string) map[string][]FolderGroup {
	t.Helper()
	characters, err := service.GetCharacters(context.Background(), game, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string][]FolderGroup{}
	for _, character := range characters {
		result[character.Name] = character.ClassifiedSubGroups
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

func TestCharacterClassificationInSubGroups(t *testing.T) {
	t.Parallel()
	for _, manual := range []bool{false, true} {
		name := "expanded"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			service, root := newClassificationFixture(t)
			parent := filepath.Join(root, "mods", "Diluc")
			child := filepath.Join(parent, "Costume")
			if manual {
				if err := service.SetManualSubGroup(ctx, child, true); err != nil {
					t.Fatal(err)
				}
			}
			classification, err := service.SaveClassification(
				ctx, "Game", nil, "Element", []ClassificationGroup{{Name: "Pyro"}},
			)
			if err != nil {
				t.Fatal(err)
			}
			groupID := classification.Groups[0].ID
			if err := service.SetCharacterClassification(ctx, child, classification.ID, &groupID); err != nil {
				t.Fatalf("assign nested folder: %v", err)
			}

			read := service.GetSubGroups
			if manual {
				read = service.GetManualSubGroups
			}
			groups, err := read(ctx, parent, nil)
			if err != nil || len(groups) != 1 || groups[0].Classifications[classification.ID] != groupID {
				t.Fatalf("nested classifications = %#v, err = %v", groups, err)
			}
			if got := characterClassifications(t, service, "Game"); got["Diluc"] != nil {
				t.Fatalf("child assignment changed parent: %#v", got)
			}
			if got := classifiedSubGroups(t, service, "Game"); len(got["Diluc"]) != 1 ||
				got["Diluc"][0].Path != child || got["Diluc"][0].Classifications[classification.ID] != groupID ||
				got["Diluc"][0].IsManualSubGroup != manual || len(got["Furina"]) != 0 {
				t.Fatalf("classified sub groups = %#v", got)
			}

			if err := service.SetCharacterClassification(ctx, child, classification.ID, nil); err != nil {
				t.Fatal(err)
			}
			if got := classifiedSubGroups(t, service, "Game"); len(got["Diluc"]) != 0 {
				t.Fatalf("cleared classified sub groups = %#v", got)
			}
			groups, err = read(ctx, parent, nil)
			if err != nil || len(groups) != 1 || groups[0].Classifications != nil {
				t.Fatalf("cleared nested classifications = %#v, err = %v", groups, err)
			}
		})
	}
}

func TestCharacterClassificationPreservesModDetection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		content bool
	}{
		{name: "empty folder"},
		{name: "folder with mod content", content: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			service, root := newClassificationFixture(t)
			parent := filepath.Join(root, "mods", "Diluc")
			child := filepath.Join(parent, "Costume")
			want := 0
			if test.content {
				writeModFile(t, child, "nested/buffer.buf", "mod content")
				want = 1
			}
			classification, err := service.SaveClassification(
				ctx, "Game", nil, "Element", []ClassificationGroup{{Name: "Pyro"}},
			)
			if err != nil {
				t.Fatal(err)
			}

			check := func(stage string) {
				t.Helper()
				characters, err := service.GetCharacters(ctx, "Game", nil)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, character := range characters {
					if character.Path != parent {
						continue
					}
					found = true
					if character.ModCount != want || character.EnabledModCount != want {
						t.Errorf("%s: character counts = %d/%d, want %d/%d",
							stage, character.ModCount, character.EnabledModCount, want, want)
					}
				}
				if !found {
					t.Fatalf("%s: parent folder missing from characters", stage)
				}
				for _, read := range []func(context.Context, string) (FolderGroup, error){
					service.GetMods, service.GetModsLight,
				} {
					group, err := read(ctx, parent)
					if err != nil {
						t.Fatal(err)
					}
					if len(group.Mods) != want || group.ModCount != want || group.EnabledModCount != want {
						t.Errorf("%s: mod list = %#v, want %d mods", stage, group, want)
					}
				}
			}
			check("before assignment")
			groupID := classification.Groups[0].ID
			if err := service.SetCharacterClassification(ctx, child, classification.ID, &groupID); err != nil {
				t.Fatal(err)
			}
			check("assigned")
			if err := service.SetCharacterClassification(ctx, child, classification.ID, nil); err != nil {
				t.Fatal(err)
			}
			check("cleared")
			if raw, err := metadata.Read(child); err != nil || string(raw) != `{}` {
				t.Fatalf("cleared metadata = %s, err = %v", raw, err)
			}
		})
	}
}

func TestClassifiedSubGroupsReachTheDepthLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newClassificationFixture(t)
	costume := filepath.Join(root, "mods", "Diluc", "Costume")
	deepest := filepath.Join(costume, "Red")
	tooDeep := filepath.Join(deepest, "Textures")
	if err := os.MkdirAll(tooDeep, 0o755); err != nil {
		t.Fatal(err)
	}
	// Assignments left in folders before any classification exists must not be listed.
	if err := metadata.Write(costume, []byte(`{"classifications":{"stale":"group"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := classifiedSubGroups(t, service, "Game"); len(got["Diluc"]) != 0 {
		t.Fatalf("sub groups without definitions = %#v", got)
	}

	element, err := service.SaveClassification(ctx, "Game", nil, "Element", []ClassificationGroup{{Name: "Pyro"}})
	if err != nil {
		t.Fatal(err)
	}
	pyro := element.Groups[0].ID
	if err := service.SetCharacterClassification(ctx, deepest, element.ID, &pyro); err != nil {
		t.Fatal(err)
	}
	if err := service.SetCharacterClassification(ctx, tooDeep, element.ID, &pyro); err == nil ||
		err.Error() != "INVALID_CLASSIFICATION_PATH" {
		t.Fatalf("assignment below the depth limit: %v", err)
	}
	if err := service.SetCharacterClassification(ctx, tooDeep, element.ID, nil); err != nil {
		t.Fatalf("clear below the depth limit: %v", err)
	}

	got := classifiedSubGroups(t, service, "Game")["Diluc"]
	paths := map[string]string{}
	for _, group := range got {
		paths[group.Path] = group.Classifications[element.ID]
	}
	if len(got) != 2 || paths[costume] != "" || paths[deepest] != pyro {
		t.Fatalf("classified sub groups = %#v", got)
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

func TestClassifiedSubGroupsFollowFolderChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newClassificationFixture(t)
	parent := filepath.Join(root, "mods", "Diluc")
	child := filepath.Join(parent, "Costume")
	plain := filepath.Join(parent, "Plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	element, err := service.SaveClassification(ctx, "Game", nil, "Element", []ClassificationGroup{{Name: "Pyro"}})
	if err != nil {
		t.Fatal(err)
	}
	pyro := element.Groups[0].ID
	paths := func() []string {
		t.Helper()
		found := []string{}
		for _, group := range classifiedSubGroups(t, service, "Game")["Diluc"] {
			found = append(found, group.Path)
		}
		slices.Sort(found)
		return found
	}

	// The first listing builds the index, so every later step runs against indexed folders.
	if got := paths(); len(got) != 0 {
		t.Fatalf("sub groups before assignment = %v", got)
	}
	if err := service.SetCharacterClassification(ctx, child, element.ID, &pyro); err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Equal(got, []string{child}) {
		t.Fatalf("assigned sub groups = %v", got)
	}

	disabled := filepath.Join(parent, "DISABLED Costume")
	if err := os.Rename(child, disabled); err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Equal(got, []string{disabled}) {
		t.Fatalf("sub groups after disabling = %v", got)
	}

	renamed := filepath.Join(parent, "Outfit")
	if err := os.Rename(disabled, renamed); err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Equal(got, []string{renamed}) {
		t.Fatalf("sub groups after renaming = %v", got)
	}

	// An assignment that arrives outside the service is indexed once its parent is listed.
	if err := metadata.Write(plain, []byte(`{"classifications":{"`+element.ID+`":"`+pyro+`"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetSubGroups(ctx, parent, nil); err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Equal(got, []string{renamed, plain}) {
		t.Fatalf("sub groups after an external assignment = %v", got)
	}

	if err := service.SetCharacterClassification(ctx, renamed, element.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Equal(got, []string{plain}) {
		t.Fatalf("sub groups after clearing = %v", got)
	}
}

func TestListGroupsWhereSkipsRejectedFolders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"Costume", "Plain"} {
		writeModFile(t, filepath.Join(root, name), "mod/mod.ini", "")
	}

	groups := listGroupsWhere(root, false, func(name string) bool { return name == "Costume" })
	if len(groups) != 1 || groups[0].Name != "Costume" || groups[0].ModCount != 1 {
		t.Fatalf("filtered groups = %#v", groups)
	}
	if all := listGroups(root, false); len(all) != 2 {
		t.Fatalf("unfiltered groups = %#v", all)
	}
}

// newIndexedClassificationFixture lists the characters once, so every later step runs against a built index.
func newIndexedClassificationFixture(t *testing.T) (*Mod, string, Classification, func() []string) {
	t.Helper()
	service, root := newClassificationFixture(t)
	element, err := service.SaveClassification(
		context.Background(), "Game", nil, "Element", []ClassificationGroup{{Name: "Pyro"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	paths := func() []string {
		t.Helper()
		found := []string{}
		for _, groups := range classifiedSubGroups(t, service, "Game") {
			for _, group := range groups {
				found = append(found, group.Path)
			}
		}
		slices.Sort(found)
		return found
	}
	if got := paths(); len(got) != 0 {
		t.Fatalf("sub groups before assignment = %v", got)
	}
	return service, root, element, paths
}

func TestClassifiedSubGroupsFollowWatchedFolderChanges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root, element, paths := newIndexedClassificationFixture(t)
	pyro := element.Groups[0].ID
	furina := filepath.Join(root, "mods", "Furina")
	if err := service.SetCharacterClassification(ctx, furina, element.ID, &pyro); err != nil {
		t.Fatal(err)
	}
	if err := service.WatchGame(ctx, "Game"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.watchMu.Lock()
		defer service.watchMu.Unlock()
		if err := service.replaceWatcher(true, nil); err != nil {
			t.Error(err)
		}
	})

	// A top-level folder has no place in the index, so only the watcher can announce that it became nested.
	moved := filepath.Join(root, "mods", "Diluc", "Furina")
	if err := os.Rename(furina, moved); err != nil {
		t.Fatal(err)
	}
	service.gameWatcher.schedule(watcher.Event{Path: furina, Op: watcher.Remove})
	if got := paths(); !slices.Equal(got, []string{moved}) {
		t.Fatalf("sub groups after moving a classified folder = %v", got)
	}

	// The search that followed rebuilt the index, so an unrelated listing does not lose the folder.
	if got := paths(); !slices.Equal(got, []string{moved}) {
		t.Fatalf("sub groups from the rebuilt index = %v", got)
	}
}

func TestClassifiedFolderIndexKeepsAssignmentsMadeDuringSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root, element, paths := newIndexedClassificationFixture(t)
	pyro := element.Groups[0].ID
	child := filepath.Join(root, "mods", "Diluc", "Costume")
	game := classificationGame(t, service)

	// The search read the folders before the assignment landed and finishes after it.
	_, since, _ := service.classifiedFolderKeys(game)
	if err := service.SetCharacterClassification(ctx, child, element.ID, &pyro); err != nil {
		t.Fatal(err)
	}
	service.finishClassifiedSearch(game, since)
	if got := paths(); !slices.Equal(got, []string{child}) {
		t.Fatalf("sub groups after an assignment during a search = %v", got)
	}

	// A change announced during a search leaves the index unbuilt, so the next listing searches again.
	_, since, _ = service.classifiedFolderKeys(game)
	service.invalidateClassifiedFolders("Game")
	service.finishClassifiedSearch(game, since)
	if _, _, indexed := service.classifiedFolderKeys(game); indexed {
		t.Fatal("index was built from a search that a folder change overtook")
	}
}

func TestClassifiedSubGroupsKeepFolderNamedByDisabledPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root, element, paths := newIndexedClassificationFixture(t)
	pyro := element.Groups[0].ID
	parent := filepath.Join(root, "mods", "Diluc")
	child := filepath.Join(parent, "DISABLED_")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{parent, child} {
		if err := service.SetCharacterClassification(ctx, path, element.ID, &pyro); err != nil {
			t.Fatal(err)
		}
	}

	// The second listing resolves the folder from its index key instead of finding it by search.
	for range 2 {
		if got := paths(); !slices.Equal(got, []string{child}) {
			t.Fatalf("sub groups for a folder named by the prefix = %v", got)
		}
	}
}

func TestClearingLastClassificationForgetsIndexedFolder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root, _, paths := newIndexedClassificationFixture(t)
	weapon, err := service.SaveClassification(ctx, "Game", nil, "Weapon", []ClassificationGroup{{Name: "Claymore"}})
	if err != nil {
		t.Fatal(err)
	}
	role, err := service.SaveClassification(ctx, "Game", nil, "Role", []ClassificationGroup{{Name: "DPS"}})
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "mods", "Diluc", "Costume")
	game := classificationGame(t, service)
	for _, classification := range []Classification{weapon, role} {
		if err := service.SetCharacterClassification(
			ctx, child, classification.ID, &classification.Groups[0].ID,
		); err != nil {
			t.Fatal(err)
		}
	}
	if got := paths(); !slices.Equal(got, []string{child}) {
		t.Fatalf("assigned sub groups = %v", got)
	}

	if err := service.SetCharacterClassification(ctx, child, weapon.ID, nil); err != nil {
		t.Fatal(err)
	}
	if keys, _, indexed := service.classifiedFolderKeys(game); !indexed || len(keys) != 1 {
		t.Fatalf("index after clearing one of two assignments = %v, indexed = %v", keys, indexed)
	}
	if err := service.SetCharacterClassification(ctx, child, role.ID, nil); err != nil {
		t.Fatal(err)
	}
	if keys, _, indexed := service.classifiedFolderKeys(game); !indexed || len(keys) != 0 {
		t.Fatalf("index after clearing the last assignment = %v, indexed = %v", keys, indexed)
	}
	if got := paths(); len(got) != 0 {
		t.Fatalf("sub groups after clearing = %v", got)
	}
}

func TestManualSubGroupListingIndexesClassifiedFolders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root, element, paths := newIndexedClassificationFixture(t)
	pyro := element.Groups[0].ID
	parent := filepath.Join(root, "mods", "Diluc")
	child := filepath.Join(parent, "Costume")
	if err := service.SetManualSubGroup(ctx, child, true); err != nil {
		t.Fatal(err)
	}

	// The assignment arrives outside the service, so only a listing that reads the folder can index it.
	if err := metadata.Write(child, []byte(`{"classifications":{"`+element.ID+`":"`+pyro+`"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetManualSubGroups(ctx, parent, nil); err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Equal(got, []string{child}) {
		t.Fatalf("sub groups after listing a classified manual sub group = %v", got)
	}
}

func classificationGame(t *testing.T, service *Mod) GameConfig {
	t.Helper()
	games, err := service.GetGames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, game := range games {
		if game.Game == "Game" {
			return game
		}
	}
	t.Fatal("fixture game is missing")
	return GameConfig{}
}
