package mod

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const downloadTargetMatchThreshold = 0.85

type DownloadTarget struct {
	Game  string      `json:"game"`
	Group FolderGroup `json:"group"`
	Score float64     `json:"score"`
}

func (m *Mod) ResolveDownloadTarget(
	ctx context.Context,
	input string,
	gameFilter *string,
) (*DownloadTarget, error) {
	games, err := m.GetGames(ctx)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		game  string
		group FolderGroup
	}
	candidates := []candidate{}
	for _, game := range games {
		if gameFilter != nil && game.Game != *gameFilter {
			continue
		}
		groups, groupErr := m.GetCharacters(ctx, game.Game, nil)
		if groupErr != nil {
			continue
		}
		for _, group := range groups {
			candidates = append(candidates, candidate{game: game.Game, group: group})
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	type ranked struct {
		index  int
		score  float64
		length int
	}
	ranks := make([]ranked, len(candidates))
	for i := range candidates {
		normalized := normalizeFuzzyString(candidates[i].group.Name)
		ranks[i] = ranked{
			index: i, length: len([]rune(normalized)),
			score: scoreFuzzyCandidate(normalized, normalizeFuzzyString(input), tokenizeFuzzyInput(input)),
		}
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].score != ranks[j].score {
			return ranks[i].score > ranks[j].score
		}
		return ranks[i].length < ranks[j].length
	})
	best := ranks[0]
	if best.score < downloadTargetMatchThreshold {
		return nil, nil
	}
	name := candidates[best.index].group.Name
	matches := 0
	for _, candidate := range candidates {
		if candidate.group.Name == name {
			matches++
		}
	}
	if matches != 1 {
		return nil, nil
	}
	return &DownloadTarget{
		Game: candidates[best.index].game, Group: candidates[best.index].group, Score: best.score,
	}, nil
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
	strongPrefix := 0.0
	if prefix >= max(2, int(math.Ceil(float64(shorter)*0.6))) {
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
