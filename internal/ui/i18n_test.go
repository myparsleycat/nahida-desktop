package ui

import "testing"

func TestCatalogKoreanParticles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, word, pair, want string
	}{
		{name: "topic consonant", word: "설정", pair: "은/는", want: "설정은"},
		{name: "topic vowel", word: "모드", pair: "은/는", want: "모드는"},
		{name: "subject", word: "게임", pair: "이/가", want: "게임이"},
		{name: "object", word: "파일", pair: "을/를", want: "파일을"},
		{name: "conjunction", word: "파일", pair: "과/와", want: "파일과"},
		{name: "reversed conjunction", word: "파일", pair: "와/과", want: "파일과"},
		{name: "predicate", word: "게임", pair: "이다/다", want: "게임이다"},
		{name: "direction consonant", word: "설정", pair: "으로/로", want: "설정으로"},
		{name: "direction vowel", word: "모드", pair: "으로/로", want: "모드로"},
		{name: "direction rieul", word: "파일", pair: "으로/로", want: "파일로"},
		{name: "rate", word: "성공", pair: "률/율", want: "성공률"},
		{name: "non Hangul", word: "XXMI", pair: "을/를", want: "XXMI를"},
		{name: "empty word", pair: "으로/로", want: ""},
		{name: "no pair", word: "모드", want: "모드"},
		{name: "other pair", word: "설정", pair: "이나/나", want: "설정이나"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := loadCatalog()
			c.SetLanguage("ko")
			c.Load("ko", map[string]string{"test.particle": "{{word, josa(pair: " + tc.pair + ")}}"})
			if got := c.T("test.particle", "word", tc.word); got != tc.want {
				t.Errorf("translated particle = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCatalogTogglePresetMigrationParticle(t *testing.T) {
	t.Parallel()

	c := loadCatalog()
	c.SetLanguage("ko-KR")
	for _, tc := range []struct {
		to, want string
	}{
		{to: "저장 공간", want: "모든 토글 프리셋을 원래 위치에서 저장 공간으로 옮깁니다."},
		{to: "앱 데이터베이스", want: "모든 토글 프리셋을 원래 위치에서 앱 데이터베이스로 옮깁니다."},
		{to: "파일", want: "모든 토글 프리셋을 원래 위치에서 파일로 옮깁니다."},
	} {
		t.Run(tc.to, func(t *testing.T) {
			t.Parallel()

			got := c.T("page.mod.toggle-presets.migrate.description", "from", "원래 위치", "to", tc.to)
			if got != tc.want {
				t.Errorf("migration description = %q, want %q", got, tc.want)
			}
		})
	}
}
