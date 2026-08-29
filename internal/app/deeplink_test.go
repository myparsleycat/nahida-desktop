package app

import "testing"

func TestParseNahidaDeepLink(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"nahida://gamebanana/mods/123":                                           "/gamebanana?mod=123",
		"nahida://gamebanana/mod/456":                                            "/gamebanana?mod=456",
		"nahida://gamebanana?id=789":                                             "/gamebanana?mod=789",
		"nahida://gamebanana/open?url=https%3A%2F%2Fgamebanana.com%2Fmods%2F321": "/gamebanana?mod=321",
		"NAHIDA://GAMEBANANA/MODS/42/":                                           "/gamebanana?mod=42",
	}
	for input, want := range valid {
		if got := parseNahidaDeepLink(input); got != want {
			t.Errorf("parseNahidaDeepLink(%q) = %q, want %q", input, got, want)
		}
	}

	invalid := []string{
		"nahida://auth",
		"nahida://gamebanana/mods/not-a-number",
		"nahida://gamebanana?id=0",
		"nahida://gamebanana?id=9007199254740992",
		"nahida://gamebanana/open?url=https%3A%2F%2Fexample.com%2Fmods%2F123",
		"nahida://gamebanana/open?url=file%3A%2F%2Fgamebanana.com%2Fmods%2F123",
		"https://gamebanana.com/mods/123",
	}
	for _, input := range invalid {
		if got := parseNahidaDeepLink(input); got != "" {
			t.Errorf("parseNahidaDeepLink(%q) = %q, want empty", input, got)
		}
	}
}

func TestNahidaDeepLinkRouteUsesFirstValidArgument(t *testing.T) {
	t.Parallel()
	got := nahidaDeepLinkRoute([]string{"Nahida Desktop.exe", "--flag", "nahida://gamebanana/mods/42", "nahida://gamebanana/mods/43"})
	if got != "/gamebanana?mod=42" {
		t.Fatalf("nahidaDeepLinkRoute = %q", got)
	}
}
