package xxmi

import "strings"

// GameMatchCase is one Electron GAME_MATCH_CASES entry. Order matches the
// TypeScript object insertion order used by getMatchingImporter.
type GameMatchCase struct {
	Importer string
	Keywords []string
}

var GameMatchCases = []GameMatchCase{
	{Importer: "GIMI", Keywords: []string{"원신", "genshin", "gimi"}},
	{Importer: "SRMI", Keywords: []string{"스타레일", "붕스", "열차", "starrail", "srmi"}},
	{Importer: "ZZMI", Keywords: []string{"젠레스", "젠존제", "찢", "zzz", "zenless", "zzmi"}},
	{Importer: "WWMI", Keywords: []string{"명조", "묑조", "wuwa", "wuthering", "wwmi", "ww"}},
	{Importer: "EFMI", Keywords: []string{"엔드필드", "엔필", "endfield", "efmi"}},
	{Importer: "HIMI", Keywords: []string{"붕괴", "붕괴3", "붕괴3rd", "himi", "honkai", "hi3rd"}},
	{Importer: "NTE", Keywords: []string{"이환", "异环", "nte", "neverness", "everness", "htgame"}},
}

func GetMatchingImporter(gameName string, enabledImporters []string) *string {
	if gameName == "" || len(enabledImporters) == 0 {
		return nil
	}
	lowerName := strings.ToLower(gameName)
	enabled := make(map[string]struct{}, len(enabledImporters))
	for _, importer := range enabledImporters {
		enabled[importer] = struct{}{}
	}
	for _, candidate := range GameMatchCases {
		if _, ok := enabled[candidate.Importer]; !ok {
			continue
		}
		for _, keyword := range candidate.Keywords {
			if strings.Contains(lowerName, keyword) {
				importer := candidate.Importer
				return &importer
			}
		}
	}
	return nil
}
