package platform

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseClipboardURIListMatchesElectronFallback(t *testing.T) {
	t.Parallel()
	text := "# copied files\r\nfile:///C:/Mods/Furina%20Blue\r\nhttps://example.test/not-a-file\n file:///D:/Mods/Encore%23One \x00"
	want := []string{
		filepath.FromSlash("C:/Mods/Furina Blue"),
		filepath.FromSlash("D:/Mods/Encore#One"),
	}
	if got := parseClipboardURIList(trimTrailingNUL(text)); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseClipboardURIList() = %#v, want %#v", got, want)
	}
}

func TestParseClipboardURIListIgnoresMalformedAndPlainPaths(t *testing.T) {
	t.Parallel()
	text := "C:\\Mods\\Plain\nfile://%zz\n# comment\n"
	if got := parseClipboardURIList(text); len(got) != 0 {
		t.Fatalf("parseClipboardURIList() = %#v, want empty", got)
	}
}
