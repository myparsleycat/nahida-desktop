package xxmi

import (
	"bytes"
	"testing"
)

func TestINIEditorPreservesFormatting(t *testing.T) {
	t.Parallel()
	input := append(
		[]byte{0xef, 0xbb, 0xbf},
		[]byte("; comment\r\n[Loader]\r\nTARGET=old.exe\r\n; target=ignored\r\n\r\n[System]\r\n")...)
	doc := parseINI(input)
	doc.SetOption("loader", "target", "game.exe", true)
	doc.SetOption("Loader", "module", "d3d11.dll", true)
	doc.SetOption("System", "dll_initialization_delay", "500", false)
	want := append(
		[]byte{0xef, 0xbb, 0xbf},
		[]byte(
			"; comment\r\n[Loader]\r\nTARGET=game.exe\r\n; target=ignored\r\nmodule = d3d11.dll\r\n\r\n[System]\r\ndll_initialization_delay=500\r\n",
		)...)
	if got := doc.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("INI mismatch\n got %q\nwant %q", got, want)
	}
	if !doc.Changed() {
		t.Fatal("modified INI not marked changed")
	}
	unchanged := parseINI(want)
	unchanged.SetOption("loader", "target", "game.exe", true)
	if unchanged.Changed() {
		t.Fatal("same option marked changed")
	}
}

func TestINIEditorRemoveDuplicateOptions(t *testing.T) {
	t.Parallel()
	doc := parseINI([]byte("[ConsoleVariables]\nkey=1\n;key=comment\nKEY=2\n"))
	doc.RemoveOption("consolevariables", "key")
	if got := string(doc.Bytes()); got != "[ConsoleVariables]\n;key=comment\n" {
		t.Fatalf("INI = %q", got)
	}
}

func TestINIEditorEditsDuplicateSections(t *testing.T) {
	t.Parallel()
	doc := parseINI(
		[]byte(
			"[Loader]\r\ntarget = old.exe\r\n[System]\r\nkeep=1\r\n[loader]\r\nTARGET=stale.exe\r\nloader=old.exe\r\n",
		),
	)
	doc.SetOption("Loader", "target", "game.exe", true)
	doc.RemoveOption("Loader", "loader")
	want := "[Loader]\r\ntarget = game.exe\r\n[System]\r\nkeep=1\r\n[loader]\r\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("INI = %q; want %q", got, want)
	}
}

func TestINIEditorCreatesMissingSection(t *testing.T) {
	t.Parallel()
	doc := parseINI([]byte("[System]\nkeep=1\n"))
	doc.SetOption("Loader", "target", "game.exe", true)
	want := "[System]\nkeep=1\n\n[Loader]\ntarget = game.exe\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("INI = %q; want %q", got, want)
	}
}
