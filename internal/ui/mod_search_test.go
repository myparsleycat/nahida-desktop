package ui

import "testing"

func TestSearchScore(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"Furina", "furina", 100},
		{"Furina Swimsuit", "furina", 90},
		{"New Furina", "furina", 80},
		{"푸리나 수영복", "ㅍㄹㄴ", 75},
		{"푸리나 수영복", "ㅍㄹㄴ ㅅ", 75},
		{"푸리나", "풀", 70},
		{"닭갈비", "닭ㄱ", 70},
		{"ㅋㅋ 모드", "ㅋㄱ", 60},
		{"Furina", "nahida", 0},
	} {
		if got := searchScore(newSearchIndex(tc.name), tc.query); got != tc.want {
			t.Errorf("searchScore(%q, %q) = %d, want %d", tc.name, tc.query, got, tc.want)
		}
	}
}
