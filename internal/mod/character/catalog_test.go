package character

import (
	"regexp"
	"slices"
	"testing"
)

// The catalog is generated data that folders and settings point into by ID, so a malformed file would panic at
// startup and a duplicate or reshaped ID would attach a folder to the wrong character.
func TestCatalogData(t *testing.T) {
	loaded, err := parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"EFMI", "GIMI", "SRMI", "WWMI", "ZZMI"}; !slices.Equal(loaded.games, want) {
		t.Fatalf("games = %v, want %v", loaded.games, want)
	}

	idRE := regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	avatarRE := regexp.MustCompile(`^[A-Za-z0-9]+$`)
	bodies := []string{"boy", "girl", "lady", "loli", "male"}
	for game, characters := range loaded.byGame {
		if len(characters) == 0 {
			t.Errorf("%s has no characters", game)
		}
		seen := map[string]bool{}
		for _, c := range characters {
			if !idRE.MatchString(c.ID) || seen[c.ID] {
				t.Errorf("%s: id %q is malformed or repeated", game, c.ID)
			}
			seen[c.ID] = true
			if c.Names["en"] == "" {
				t.Errorf("%s %s: no English name", game, c.ID)
			}
			switch {
			case game != "GIMI" && c.Genshin != nil:
				t.Errorf("%s %s: Genshin data outside GIMI", game, c.ID)
			case game == "GIMI" && (c.Genshin == nil || !slices.Contains(bodies, c.Genshin.Body)):
				t.Errorf("%s %s: Genshin body type missing or unknown: %+v", game, c.ID, c.Genshin)
			case game == "GIMI" && c.Genshin.Avatar != "" && !avatarRE.MatchString(c.Genshin.Avatar):
				t.Errorf("%s %s: avatar name %q is not one ramp file name part", game, c.ID, c.Genshin.Avatar)
			}
		}
	}
}
