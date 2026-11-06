package mod

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"nahida.live/desktop/internal/infra"
)

const downloadTargetMatchThreshold = 0.85

type DownloadTarget struct {
	Game  string      `json:"game"`
	Group FolderGroup `json:"group"`
	Score float64     `json:"score"`
}

// ResolveDownloadTarget ranks folder names alone and builds only the groups it may return,
// because building every group of every game reads each mod's files.
func (m *Mod) ResolveDownloadTarget(
	ctx context.Context,
	input string,
	gameFilter *string,
) (*DownloadTarget, error) {
	diagnostics := &infra.DiagnosticBatch{}
	defer diagnostics.Report(m.log, "Mod", "ResolveDownloadTarget")

	client, err := m.requireClient()
	if err != nil {
		return nil, err
	}
	rows, err := client.GamePaths.List(ctx)
	if err != nil {
		return nil, err
	}
	search, err := m.resolvePreviewSetting(ctx, nil)
	if err != nil {
		return nil, err
	}

	type candidate struct {
		listing groupListing
		name    string
		score   float64
		length  int
	}
	normalizedInput := normalizeFuzzyString(input)
	tokens := tokenizeFuzzyInput(input)
	var candidates []candidate
	for _, row := range rows {
		if (gameFilter != nil && row.Game != *gameFilter) || row.ModFolderPath == "" {
			continue
		}
		listing := m.newGroupListing(ctx, client, gameConfig(row), search, diagnostics.Add)
		m.listSubGroups(listing, row.ModFolderPath, func(name string) bool {
			normalized := normalizeFuzzyString(name)
			candidates = append(candidates, candidate{
				listing: listing, name: name, length: len([]rune(normalized)),
				score: scoreFuzzyCandidate(normalized, normalizedInput, tokens),
			})
			return false
		}, diagnostics.Add)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].length < candidates[j].length
	})

	// A listed folder can still be left out of the groups, such as an NTE pak wrapper.
	list := func(c candidate) []FolderGroup {
		return m.listSubGroups(c.listing, c.listing.game.ModFolderPath, func(name string) bool {
			return name == c.name
		}, diagnostics.Add)
	}
	for i, best := range candidates {
		if best.score < downloadTargetMatchThreshold {
			return nil, nil
		}
		groups := list(best)
		if len(groups) == 0 {
			continue
		}
		for _, other := range candidates[i+1:] {
			if other.name != best.name {
				continue
			}
			if len(list(other)) > 0 {
				return nil, nil
			}
		}
		group := m.loadClassifiedSubGroups(best.listing, groups, false, diagnostics.Add)[0]
		return &DownloadTarget{Game: best.listing.game.Game, Group: group, Score: best.score}, nil
	}
	return nil, nil
}

func normalizeFuzzyString(value string) string {
	value = strings.ToLower(strings.TrimSpace(norm.NFKD.String(value)))
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return -1
	}, value)
}

func tokenizeFuzzyInput(value string) []string {
	runes := []rune(strings.TrimSpace(value))
	var builder strings.Builder
	for i, current := range runes {
		if i > 0 && isASCIILowerOrDigit(runes[i-1]) && current >= 'A' && current <= 'Z' {
			builder.WriteRune(' ')
		}
		builder.WriteRune(current)
	}
	fields := strings.FieldsFunc(builder.String(), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if normalized := normalizeFuzzyString(field); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func scoreFuzzyCandidate(candidate, input string, tokens []string) float64 {
	if candidate == "" || input == "" {
		return 0
	}
	if candidate == input {
		return 1
	}
	for _, token := range tokens {
		if candidate == token {
			return 0.99
		}
	}
	if len([]rune(candidate)) < 4 {
		return 0
	}
	if strings.HasPrefix(input, candidate) {
		return 0.97
	}
	if strings.Contains(input, candidate) {
		return 0.92
	}
	best := levenshteinSimilarity(candidate, input) * 0.8
	for _, token := range tokens {
		best = max(best, scoreFuzzyToken(candidate, token))
	}
	return min(1, max(0, best))
}

func scoreFuzzyToken(candidate, token string) float64 {
	if token == "" {
		return 0
	}
	a, b := []rune(candidate), []rune(token)
	shorter, longer := min(len(a), len(b)), max(len(a), len(b))
	prefix := commonPrefixLength(a, b)
	transposed := singleAdjacentTransposition(a, b)
	distance := levenshteinDistanceRunes(a, b)
	if transposed {
		distance = 1
	}
	// A partial shared prefix such as "lucy"/"lucia" names a different character, so only an
	// abbreviation, where one side is a whole prefix of the other, earns the prefix score.
	strongPrefix := 0.0
	if shorter >= 4 && prefix == shorter {
		strongPrefix = 0.85 + 0.1*float64(prefix)/float64(longer)
	}
	transpositionScore := 0.0
	if transposed {
		transpositionScore = 0.88
	}
	singleEdit := 0.0
	if shorter >= 4 && prefix >= 2 && distance == 1 {
		singleEdit = 0.88
	}
	return max(strongPrefix, transpositionScore, singleEdit,
		(1-float64(distance)/float64(longer))*0.9)
}

func commonPrefixLength(a, b []rune) int {
	limit := min(len(a), len(b))
	for i := range limit {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}

func levenshteinSimilarity(a, b string) float64 {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 && len(br) == 0 {
		return 1
	}
	if len(ar) == 0 || len(br) == 0 {
		return 0
	}
	return 1 - float64(levenshteinDistanceRunes(ar, br))/float64(max(len(ar), len(br)))
}

func levenshteinDistanceRunes(a, b []rune) int {
	previous := make([]int, len(b)+1)
	for i := range previous {
		previous[i] = i
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				current[j] = previous[j-1]
			} else {
				current[j] = min(previous[j-1], previous[j], current[j-1]) + 1
			}
		}
		previous = current
	}
	return previous[len(b)]
}

func singleAdjacentTransposition(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	first := -1
	for i := range a {
		if a[i] != b[i] {
			first = i
			break
		}
	}
	if first < 0 || first+1 >= len(a) || a[first] != b[first+1] || a[first+1] != b[first] {
		return false
	}
	return string(a[first+2:]) == string(b[first+2:])
}

func isASCIILowerOrDigit(value rune) bool {
	return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9')
}
