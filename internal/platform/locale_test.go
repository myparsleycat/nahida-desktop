package platform

import "testing"

func TestUsableEnvLocale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"C", ""},
		{"POSIX", ""},
		{"C.UTF-8", ""},
		{"ko_KR.UTF-8", "ko_KR.UTF-8"},
		{"  ja_JP  ", "ja_JP"},
	}
	for _, tc := range cases {
		if got := usableEnvLocale(tc.in); got != tc.want {
			t.Fatalf("usableEnvLocale(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSystemLocaleDoesNotPanic(t *testing.T) {
	t.Parallel()

	_ = SystemLocale()
}
