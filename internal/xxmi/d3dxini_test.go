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

func TestINIEditorPreservesInlineComments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		line    string
		value   string
		want    string
		changed bool
	}{
		{"semicolon", "target = old.exe ; keep this", "game.exe", "target = game.exe ; keep this", true},
		{"hash", "target = old.exe\t# keep this", "game.exe", "target = game.exe\t# keep this", true},
		{"quoted separator", `target = "old ; value" ; keep this`, `"new ; value"`,
			`target = "new ; value" ; keep this`, true},
		{"unchanged", "target = game.exe ; keep this", "game.exe", "target = game.exe ; keep this", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := parseINI([]byte("[Loader]\r\n" + tc.line + "\r\n"))
			doc.SetOption("Loader", "target", tc.value, true)
			if got := string(doc.Bytes()); got != "[Loader]\r\n"+tc.want+"\r\n" {
				t.Fatalf("INI = %q; want %q", got, tc.want)
			}
			if doc.Changed() != tc.changed {
				t.Fatalf("changed = %t; want %t", doc.Changed(), tc.changed)
			}
		})
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

func TestINIEditorAddsMissingTemplateOptions(t *testing.T) {
	t.Parallel()
	template := parseINI([]byte("[Loader]\ntarget = template.exe\n" +
		"[System]\n;upscaling = 0\n; how long to wait\ndelay = 5\nkept = 0\n;off = 1\ndisabled = 1\n" +
		"[SmoothMotion]\n\n; fork option\nenabled = 1\nenabled = 2\n" +
		"[Constants]\nglobal $x = 1\n[KeyToggle]\nkey = VK_F1\n[ShaderOverrideFoo]\nhash = abc\n" +
		"[ClearRenderTargetView]\nrun = CommandListA\nrun = CommandListB\n"))
	input := "[System]\r\nKEPT = 7\r\n; disabled = 0\r\n\r\n[Mods]\r\nuser = 1\r\n"
	doc := parseINI([]byte(input))
	doc.AddMissingOptions(template)
	want := "[System]\r\nKEPT = 7\r\n; disabled = 0\r\n; how long to wait\r\ndelay = 5\r\n\r\n" +
		"[Mods]\r\nuser = 1\r\n\r\n[SmoothMotion]\r\n; fork option\r\nenabled = 1\r\n"
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("INI mismatch\n got %q\nwant %q", got, want)
	}

	again := parseINI([]byte(want))
	again.AddMissingOptions(template)
	if again.Changed() {
		t.Fatalf("second merge changed the INI: %q", again.Bytes())
	}
}
