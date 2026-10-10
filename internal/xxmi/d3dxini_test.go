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

func TestRebuildINICarriesPreviousFile(t *testing.T) {
	t.Parallel()
	template := []byte("; head\n[Loader]\ntarget = Game.exe\n; loader notes\n;launch = x\n\n; includes\n" +
		"[Include]\ninclude_recursive = Mods\nexclude_recursive = DISABLED*\n\n; system\n" +
		"[System]\n; delay help\ndelay = 5 ; seconds\nkept = 0\ndisabled = 1\nfresh = 2\n\n; constants\n" +
		"[Constants]\n; declare globals\n\n; present\n[Present]\nrun = CommandListNew\n")
	previous := []byte(
		"; old head\r\n[System]\r\nKEPT = 7 ; mine\r\n; disabled = 0\r\n;gone = 1\r\nlegacy = 3\r\n\r\n" +
			"[Include]\r\ninclude = Core\\main.ini\r\nexclude_recursive = DISABLED*\r\n; old tail\r\n\r\n" +
			"[Constants]\r\n; old doc\r\nglobal $x = 1\r\n; old doc\r\n\r\n" +
			"[Loader]\r\ntarget = Old.exe\r\nlaunch = old.exe\r\n\r\n[Stereo]\r\nautomatic_mode = 0\r\n;unlock = 1\r\n; next banner\r\n",
	)
	want := "; head\n[Loader]\ntarget = Old.exe\nlaunch = old.exe\n; loader notes\n;launch = x\n\n; includes\n" +
		"[Include]\ninclude = Core\\main.ini\nexclude_recursive = DISABLED*\n\n; system\n" +
		"[System]\n; delay help\ndelay = 5 ; seconds\nkept = 7\n;disabled = 1\nfresh = 2\n;gone = 1\nlegacy = 3\n\n; constants\n" +
		"[Constants]\n; declare globals\nglobal $x = 1\n\n; present\n[Present]\nrun = CommandListNew\n\n[Stereo]\nautomatic_mode = 0\n;unlock = 1\n"

	doc, lost := rebuildINI(template, previous)
	if got := string(doc.Bytes()); got != want {
		t.Fatalf("INI mismatch\n got %q\nwant %q", got, want)
	}
	if !lost {
		t.Fatal("dropped comments of the previous file were not reported")
	}

	again, lost := rebuildINI(template, []byte(want))
	if got := string(again.Bytes()); got != want || lost {
		t.Fatalf("second rebuild changed the INI (lost = %t): %q", lost, got)
	}

	// An option that was off before the template knew it stays off once a later template sets it.
	later := bytes.Replace(template, []byte("fresh = 2\n"), []byte("fresh = 2\ngone = 5\n"), 1)
	later = append(later, "[Stereo]\nunlock = 9\n"...)
	updated, _ := rebuildINI(later, []byte(want))
	for _, option := range [][2]string{{"System", "gone"}, {"Stereo", "unlock"}} {
		value, ok := updated.Option(option[0], option[1])
		if ok || !updated.mentionsOption(option[0], option[1]) {
			t.Fatalf("a later template turned %s on (%q): %q", option[1], value, updated.Bytes())
		}
	}
}

func TestRebuildINIKeepsOnlyFirstVerbatimSection(t *testing.T) {
	t.Parallel()
	previous := []byte("[CommandListUser]\nrun = CommandListFirst\n" +
		"[System]\nfirst = 1\n[commandlistuser]\nrun = CommandListIgnored\n" +
		"[system]\nsecond = 2\n[Loader]\ntarget = Old.exe\n[loader]\ncustom = keep\n" +
		"[CommandListOther]\nrun = CommandListOther\n")
	for _, tc := range []struct {
		name     string
		template string
		previous []byte
		want     string
	}{
		{
			name:     "section in template",
			template: "[CommandListUser]\nrun = CommandListDefault\n[System]\nfirst = 0\n[Loader]\ntarget = Game.exe\n",
			want: "[CommandListUser]\nrun = CommandListFirst\n[System]\nfirst = 1\nsecond = 2\n" +
				"[Loader]\ntarget = Old.exe\ncustom = keep\n\n[CommandListOther]\nrun = CommandListOther\n",
		},
		{
			name:     "section absent from template",
			template: "[System]\nfirst = 0\n[Loader]\ntarget = Game.exe\n",
			want: "[System]\nfirst = 1\nsecond = 2\n[Loader]\ntarget = Old.exe\ncustom = keep\n\n" +
				"[CommandListUser]\nrun = CommandListFirst\n\n[CommandListOther]\nrun = CommandListOther\n",
		},
		{
			name:     "duplicate lines also in first section",
			template: "[CommandListUser]\nrun = CommandListDefault\n",
			previous: []byte("[CommandListUser]\nrun = CommandListFirst\n[CommandListUser]\nrun = CommandListFirst\n"),
			want:     "[CommandListUser]\nrun = CommandListFirst\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			old := previous
			if tc.previous != nil {
				old = tc.previous
			}
			doc, lost := rebuildINI([]byte(tc.template), old)
			if got := string(doc.Bytes()); got != tc.want {
				t.Fatalf("INI mismatch\n got %q\nwant %q", got, tc.want)
			}
			if !lost {
				t.Fatal("discarded duplicate commands were not reported")
			}
			again, lost := rebuildINI([]byte(tc.template), doc.Bytes())
			if got := string(again.Bytes()); got != tc.want || lost {
				t.Fatalf("second rebuild changed the INI (lost = %t): %q", lost, got)
			}
		})
	}
}
