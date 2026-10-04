package app

import (
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/drive"
)

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

func TestParseAuthDeepLink(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"nahida://auth?state=st&code=cd",
		// Browsers may add a trailing slash when opening a URL with an empty path.
		"nahida://auth/?state=st&code=cd",
		"NAHIDA://AUTH?code=cd&state=st",
	} {
		if state, code, ok := parseAuthDeepLink(input); !ok || state != "st" || code != "cd" {
			t.Errorf("parseAuthDeepLink(%q) = %q, %q, %v", input, state, code, ok)
		}
	}

	for _, input := range []string{
		"nahida://auth",
		"nahida://auth?state=st",
		"nahida://auth?code=cd",
		"nahida://auth?state=&code=cd",
		"nahida://auth?state=st&code=" + strings.Repeat("a", maxAuthDeepLinkValueLength+1),
		"nahida://download?state=st&code=cd",
		"https://auth?state=st&code=cd",
	} {
		if state, code, ok := parseAuthDeepLink(input); ok {
			t.Errorf("parseAuthDeepLink(%q) = %q, %q, want rejected", input, state, code)
		}
	}
	if route := parseNahidaDeepLink("nahida://auth?state=st&code=cd"); route != "" {
		t.Errorf("auth link produced route %q", route)
	}
}

func TestNahidaDeepLinkRouteUsesFirstValidArgument(t *testing.T) {
	t.Parallel()
	got := nahidaDeepLinkRoute(
		[]string{"Nahida Desktop.exe", "--flag", "nahida://gamebanana/mods/42", "nahida://gamebanana/mods/43"},
	)
	if got != "/gamebanana?mod=42" {
		t.Fatalf("nahidaDeepLinkRoute = %q", got)
	}
}

func TestParseDownloadDeepLink(t *testing.T) {
	t.Parallel()
	valid := map[string]deepLinkDownload{
		"nahida://download?v=2&ticket=" + strings.Repeat("A", 43): {
			Kind: "mod-ticket", Ticket: strings.Repeat("A", 43), IsDir: true,
		},
		"nahida://download/?v=2&ticket=" + strings.Repeat("A", 43): {
			Kind: "mod-ticket", Ticket: strings.Repeat("A", 43), IsDir: true,
		},
		"nahida://download?v=1&kind=drive&id=abc&dir=0&name=a%20b.zip": {
			Kind: "drive", ID: "abc", IsDir: false, Name: "a b.zip",
		},
		"nahida://download?v=1&kind=drive&id=abc": {Kind: "drive", ID: "abc", IsDir: true},
		"nahida://download?v=1&kind=link&id=abc&dir=1&name=Folder&linkId=l&linkToken=t%2B%2F": {
			Kind: "link", ID: "abc", IsDir: true, Name: "Folder",
			Link: &drive.DownloadLink{LinkID: "l", Token: "t+/"},
		},
		"nahida://download?v=1&kind=mod&id=abc&dir=0&name=Mod&token=tok&sig=s": {
			Kind: "mod", ID: "abc", IsDir: true, Name: "Mod",
			Mod: &drive.DownloadModAccess{Token: "tok", Sig: "s"},
		},
	}
	for input, want := range valid {
		got := parseDownloadDeepLink(input)
		if got == nil || !reflect.DeepEqual(*got, want) {
			t.Errorf("parseDownloadDeepLink(%q) = %+v, want %+v", input, got, want)
		}
	}

	invalid := []string{
		"nahida://download?kind=drive&id=abc",
		"nahida://download?v=2&kind=drive&id=abc",
		"nahida://download?v=2&ticket=short",
		"nahida://download?v=2&ticket=" + strings.Repeat("A", 43) + "&id=abc",
		"nahida://download?v=2&ticket=" + strings.Repeat("A", 43) + "&ticket=" + strings.Repeat("B", 43),
		"nahida://download?v=2&ticket=" + strings.Repeat("/", 43),
		"nahida://download/other?v=2&ticket=" + strings.Repeat("A", 43),
		"nahida://download//?v=2&ticket=" + strings.Repeat("A", 43),
		"nahida://download/?v=2&ticket=" + strings.Repeat("A", 43) + "#extra",
		"nahida://download?v=1&kind=drive",
		"nahida://download?v=1&kind=unknown&id=abc",
		"nahida://download?v=1&kind=link&id=abc&linkId=l",
		"nahida://download?v=1&kind=drive&id=abc&name=" + strings.Repeat("a", maxDeepLinkValueLength+1),
		"nahida://gamebanana/mods/1",
		"https://download?v=1&kind=drive&id=abc",
	}
	for _, input := range invalid {
		if got := parseDownloadDeepLink(input); got != nil {
			t.Errorf("parseDownloadDeepLink(%q) = %+v, want nil", input, got)
		}
	}
	if route := parseNahidaDeepLink("nahida://download?v=1&kind=drive&id=abc"); route != "" {
		t.Errorf("download link produced route %q", route)
	}
}
