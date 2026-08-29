package drive

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func sourceURL(hostname, pathname, protocol string) string {
	if protocol == "" {
		protocol = "https:"
	}
	return protocol + "//" + hostname + pathname
}

func stdBase64(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

func urlSafeBase64(value string) string {
	return strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(stdBase64(value)), "=")
}

func encodeBase64Layers(value string, layers int) string {
	encoded := value
	for range layers {
		encoded = stdBase64(encoded)
	}
	return encoded
}

func TestEncodeNahidaPassword(t *testing.T) {
	t.Parallel()

	if got := EncodeNahidaPassword("gayshin"); got != "Z2F5c2hpbg" {
		t.Fatalf("gayshin = %q", got)
	}
	if got := EncodeNahidaPassword("비밀번호"); got != "67mE67CA67KI7Zi4" {
		t.Fatalf("비밀번호 = %q", got)
	}
}

func TestParseDriveSourceUrlFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		label string
		url   string
		want  DriveSource
	}{
		{"public folder", "https://nahida.live/akasha/link/qjsEdvLpcAxr", DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}},
		{"private folder", "https://nahida.live/akasha/link/ZwgSTtFUXZGu", DriveSource{Type: "link", ID: "ZwgSTtFUXZGu"}},
		{"public collection", "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj", DriveSource{Type: "mod", ID: "WmVWMjAzthuFpKZiE-AKj"}},
		{"private collection", "https://nahida.live/akasha/mod/-fpnEyi_nPNB-Mf97p5_k", DriveSource{Type: "mod", ID: "-fpnEyi_nPNB-Mf97p5_k"}},
		{"Base64-encoded public folder", "aHR0cHM6Ly9uYWhpZGEubGl2ZS9ha2FzaGEvbGluay9xanNFZHZMcGNBeHI=", DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}},
		{"multi-encoded public folder", stdBase64(stdBase64("https://nahida.live/akasha/link/qjsEdvLpcAxr")), DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}},
		{"Base64-encoded Nahida host", stdBase64("nahida.live/akasha/link/qjsEdvLpcAxr"), DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}},
		{"Base64-encoded www Nahida host", stdBase64("www.nahida.live/akasha/link/qjsEdvLpcAxr"), DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDriveSourceUrl(tc.url)
			if err != nil {
				t.Fatalf("ParseDriveSourceUrl: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseDriveSourceUrlAddsHTTPSToHost(t *testing.T) {
	t.Parallel()

	got, err := ParseDriveSourceUrl("nahida.live/akasha/link/qjsEdvLpcAxr")
	if err != nil || got != (DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}) {
		t.Fatalf("host-only = %+v, %v", got, err)
	}
	got, err = ParseDriveSourceUrl("www.nahida.live/akasha/link/qjsEdvLpcAxr")
	if err != nil || got != (DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}) {
		t.Fatalf("www host-only = %+v, %v", got, err)
	}
}

func TestParseDriveSourceUrlUnpaddedURLSafeBase64(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"https://www.nahida.live/akasha/link/abc?x=😀",
		"https://www.nahida.live/akasha/link/abc#😀",
	} {
		if !strings.ContainsAny(stdBase64(value), "+/") {
			t.Fatalf("fixture %q is not a +/ Base64 source", value)
		}
		got, err := ParseDriveSourceUrl(urlSafeBase64(value))
		if err != nil || got != (DriveSource{Type: "link", ID: "abc"}) {
			t.Fatalf("%q => %+v, %v", value, got, err)
		}
	}
}

func TestParseDriveSourceUrlIgnoresWhitespaceAroundUnpaddedBase64(t *testing.T) {
	t.Parallel()

	value := "https://nahida.live/akasha/link/qjsEdvLpcAxr"
	got, err := ParseDriveSourceUrl("  \n" + urlSafeBase64(value) + "\t ")
	if err != nil || got != (DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestParseDriveSourceUrlDecodingDepth(t *testing.T) {
	t.Parallel()

	got, err := ParseDriveSourceUrl(encodeBase64Layers("https://nahida.live/akasha/link/qjsEdvLpcAxr", 10))
	if err != nil || got != (DriveSource{Type: "link", ID: "qjsEdvLpcAxr"}) {
		t.Fatalf("10 layers = %+v, %v", got, err)
	}
	_, err = ParseDriveSourceUrl(encodeBase64Layers("https://nahida.live/akasha/link/qjsEdvLpcAxr", 11))
	assertInvalidSource(t, err)
}

func TestParseDriveSourceUrlHostPathVariants(t *testing.T) {
	t.Parallel()

	got, err := ParseDriveSourceUrl(sourceURL(nahidaSourceHostnames[0], "/akasha/link/link_123", "https:"))
	if err != nil || got != (DriveSource{Type: "link", ID: "link_123"}) {
		t.Fatalf("link_123 = %+v, %v", got, err)
	}
	got, err = ParseDriveSourceUrl(sourceURL(nahidaSourceHostnames[1], "/akasha/mod/mod-456/", "https:"))
	if err != nil || got != (DriveSource{Type: "mod", ID: "mod-456"}) {
		t.Fatalf("mod-456 = %+v, %v", got, err)
	}
}

func TestParseDriveSourceUrlRejectsUnsupported(t *testing.T) {
	t.Parallel()

	for _, value := range []any{
		"",
		"nahida://link/abc",
		sourceURL(nahidaSourceHostnames[0], "/akasha/link/abc", "http:"),
		sourceURL("example.invalid", "/akasha/link/abc", "https:"),
		sourceURL(nahidaSourceHostnames[0], "/akasha/link/", "https:"),
		sourceURL(nahidaSourceHostnames[0], "/akasha/unknown/abc", "https:"),
		sourceURL(nahidaSourceHostnames[0], "/akasha/mod/abc/extra", "https:"),
		"invalid-base64*",
		nil,
		42,
	} {
		_, err := ParseDriveSourceUrl(value)
		assertInvalidSource(t, err)
	}
}

func assertInvalidSource(t *testing.T, err error) {
	t.Helper()
	var api *DriveAPIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v, want DriveAPIError", err)
	}
	if api.Code != codeInvalidSourceURL {
		t.Fatalf("code = %q", api.Code)
	}
	if !strings.Contains(err.Error(), "DRIVE_INVALID_SOURCE_URL") {
		t.Fatalf("error = %q", err)
	}
}
