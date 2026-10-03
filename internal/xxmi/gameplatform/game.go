// Package gameplatform locates games installed through Steam and the Epic Games Launcher
// and builds the requests that start them through those clients.
package gameplatform

import (
	"strings"
	"unicode"
)

// Game identifies a game independently of the store it was installed from.
type Game struct {
	Name string
	// Keywords match the install folder or store title when no application ID is known.
	Keywords    []string
	SteamAppIDs []string
}

var games = map[string]Game{
	"EFMI": {Name: "Arknights Endfield", Keywords: []string{"endfield"}, SteamAppIDs: []string{"4732690"}},
	"GIMI": {Name: "Genshin Impact", Keywords: []string{"genshin"}},
	"HIMI": {Name: "Honkai Impact", Keywords: []string{"honkai impact"}, SteamAppIDs: []string{"1671200"}},
	"SRMI": {Name: "Honkai: Star Rail", Keywords: []string{"starrail", "star rail"}},
	"WWMI": {Name: "Wuthering Waves", Keywords: []string{"wuthering"}, SteamAppIDs: []string{"3513350"}},
	"ZZMI": {Name: "Zenless Zone Zero", Keywords: []string{"zenless"}, SteamAppIDs: []string{"4162040"}},
}

// GameFor returns the game an importer mods.
func GameFor(importer string) (Game, bool) {
	game, ok := games[strings.ToUpper(strings.TrimSpace(importer))]
	return game, ok
}

// matches reports whether any keyword of the game occurs in one of the names,
// ignoring case, spacing, and punctuation.
func (g Game) matches(names ...string) bool {
	for _, name := range names {
		normalized := normalizeName(name)
		for _, keyword := range g.Keywords {
			if strings.Contains(normalized, normalizeName(keyword)) {
				return true
			}
		}
	}
	return false
}

func normalizeName(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
