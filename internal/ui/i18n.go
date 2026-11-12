package ui

import (
	"embed"
	"fmt"
	"strings"

	"github.com/LoperLee/golang-hangul-toolkit/hangul"
	"github.com/myparsleycat/fairy/i18n"
	"github.com/myparsleycat/fairy/kit"
)

// The locale files started as copies of the React frontend's i18next resources, so screens ported from it
// keep their keys and translations.
//
//go:embed locales/*.json
var localeFiles embed.FS

var languages = []string{"en", "ja", "ko", "zh"}

var catalog = loadCatalog()

func loadCatalog() *i18n.Catalog {
	c := i18n.New("en")
	c.Format("josa", formatJosa)
	for _, lang := range languages {
		data, err := localeFiles.ReadFile("locales/" + lang + ".json")
		if err == nil {
			err = c.LoadJSON(lang, data)
		}
		if err != nil {
			panic(fmt.Sprintf("ui: load locale %s: %v", lang, err))
		}
	}
	return c
}

func formatJosa(value any, _ string, opts map[string]string) string {
	word := fmt.Sprint(value)
	pair := opts["pair"]
	// GetJosa indexes the last character, so empty values must not reach it.
	if word == "" || pair == "" {
		return word
	}

	var particle hangul.Josa
	switch pair {
	case "은/는":
		particle = hangul.EUN_NEUN
	case "이/가":
		particle = hangul.I_GA
	case "을/를":
		particle = hangul.EUL_REUL
	case "과/와", "와/과":
		particle = hangul.GWA_WA
	case "이다/다":
		particle = hangul.IDA_DA
	case "으로/로":
		particle = hangul.EURO_RO
	case "률/율":
		particle = hangul.RYUL_YUL
	default:
		return i18n.Josa(word, pair)
	}
	return hangul.GetJosa(word, particle)
}

// T returns the message for key in the current language; args are name/value pairs.
func T(key string, args ...any) string { return catalog.T(key, args...) }

// SetLanguage switches the messages of the UI and of the component kit. Call it on the UI thread.
func SetLanguage(lang string) {
	catalog.SetLanguage(lang)
	switch base, _, _ := strings.Cut(lang, "-"); base {
	case "ko":
		kit.Messages = kit.KoreanStrings
	case "ja":
		kit.Messages = kit.JapaneseStrings
	case "zh":
		kit.Messages = kit.ChineseStrings
	default:
		kit.Messages = kit.EnglishStrings
	}
}
