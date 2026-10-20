package toggleviewer

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateToggleViewerArtifact(t *testing.T) {
	iniPath := filepath.Join(`C:\Mods`, "Character", "mod.ini")
	content := strings.Join([]string{
		"[KeySwap]", "type = cycle", "key = ctrl vk_up", "back = no_ctrl vk_down", "$swapvar = 0, 1", "",
		"[TextureOverrideBodyPosition]", "hash = aabbccdd",
	}, "\n")
	artifact := generateArtifact(iniPath, content, "ctrl H")
	if artifact == nil {
		t.Fatal("artifact was not generated")
	}
	if !strings.Contains(artifact.txtContent, "Key: Ctrl + Up") ||
		!strings.Contains(artifact.txtContent, "Back: Down") {
		t.Fatalf("txt = %q", artifact.txtContent)
	}
	if !strings.Contains(artifact.iniContent, "hash = aabbccdd\nmatch_priority = 0\n") ||
		!strings.Contains(artifact.iniContent, "key = ctrl H") {
		t.Fatalf("ini = %q", artifact.iniContent)
	}
	if len(artifact.toggleINIHash) != 64 || len(artifact.toggleTXTHash) != 64 {
		t.Fatalf("hashes = %q %q", artifact.toggleINIHash, artifact.toggleTXTHash)
	}
}

func TestGenerateToggleViewerArtifactRequiresCycleAndPositionHash(t *testing.T) {
	if artifact := generateArtifact(
		"mod.ini",
		"[Key]\ntype = hold\nkey = H\n$x = 0,1\n[TextureOverrideBodyPosition]\nhash=1",
		"H",
	); artifact != nil {
		t.Fatal("non-cycle key generated an artifact")
	}
	// Only the missing hash is reported, because the scan lists those INIs as skipped.
	if source, missingHash := parseToggleViewerSource(
		"mod.ini",
		"[Key]\ntype = cycle\nkey = H\n$x = 0,1",
	); source != nil ||
		!missingHash {
		t.Fatalf("missing position hash = %#v, %v", source, missingHash)
	}
	if _, missingHash := parseToggleViewerSource("mod.ini", "[Key]\ntype = hold\nkey = H\n$x = 0,1"); missingHash {
		t.Fatal("INI without toggle keys reported a missing position hash")
	}
}

func TestGenerateToggleViewerArtifactListsToggleKeys(t *testing.T) {
	artifact := generateArtifact(
		"mod.ini",
		"[KeyHat]\ntype = toggle\nkey = VK_F\n$hat = 1\n[TextureOverrideBodyPosition]\nhash = 1234abcd",
		"ctrl H",
	)
	if artifact == nil || !strings.Contains(artifact.txtContent, "KeyHat:\n    Key: F") {
		t.Fatalf("toggle key artifact = %#v", artifact)
	}
}

func TestGenerateToggleViewerTXTKeepsPrintableASCII(t *testing.T) {
	artifact := generateArtifact(filepath.Join(`C:\Mods`, "나히다 Skin", "mod.ini"), cycleModINI, "ctrl H")
	if artifact == nil || !strings.HasPrefix(artifact.txtContent, "Mod: ??? Skin\n") {
		t.Fatalf("artifact = %#v", artifact)
	}
	for _, r := range artifact.txtContent {
		if r != '\n' && (r < ' ' || r > '~') {
			t.Fatalf("txt contains %q: %q", r, artifact.txtContent)
		}
	}
}

func TestGenerateToggleViewerArtifactAfterBOM(t *testing.T) {
	if artifact := generateArtifact("mod.ini", utf8BOM+cycleModINI, "ctrl H"); artifact == nil {
		t.Fatal("BOM hid the first key section")
	}
}

func TestReplaceToggleViewerHotkeyPreservesNewlines(t *testing.T) {
	original := "[Constants]\r\nx=1\r\n[Key]\r\nkey = ctrl H\r\ntype=cycle\r\n"
	updated := replaceToggleViewerHotkey(original, "alt J")
	if !strings.Contains(updated, "key = alt J\r\n") ||
		strings.Contains(strings.ReplaceAll(updated, "\r\n", ""), "\n") {
		t.Fatalf("updated = %q", updated)
	}
}

func TestResolveTogglePositionHashThroughResourceReference(t *testing.T) {
	sections := parseToggleINI(
		"[ResourceBodyPosition]\nfilename=x.buf\n[TextureOverrideBody]\nhash=deadbeef\nvb0=ResourceBodyPosition",
	)
	if got := resolveTogglePositionHash(sections); got != "deadbeef" {
		t.Fatalf("hash = %q", got)
	}
}

func TestParseToggleINIPreservesSemicolonKeyValues(t *testing.T) {
	t.Run("preserves a bare semicolon key value", func(t *testing.T) {
		sections := parseToggleINI("[KeyOne]\nkey = ;\n")
		if len(sections) != 1 || len(sections[0].entries) != 1 {
			t.Fatalf("sections = %#v", sections)
		}
		if sections[0].entries[0].key != "key" || sections[0].entries[0].value != ";" {
			t.Fatalf("entry = %#v", sections[0].entries[0])
		}
	})
	t.Run("preserves a modifier + semicolon key value", func(t *testing.T) {
		sections := parseToggleINI("[KeyTwo]\nkey = ctrl ;\n")
		if sections[0].entries[0].value != "ctrl ;" {
			t.Fatalf("value = %q", sections[0].entries[0].value)
		}
	})
	t.Run("skips a leading-semicolon comment line", func(t *testing.T) {
		sections := parseToggleINI("; full line comment\n[KeyThree]\nkey = x\n")
		if len(sections) != 1 || sections[0].entries[0].value != "x" {
			t.Fatalf("sections = %#v", sections)
		}
	})
	t.Run("preserves inline comment text in a value", func(t *testing.T) {
		sections := parseToggleINI("[KeyFour]\nkey = 0 ; note\n")
		if sections[0].entries[0].value != "0 ; note" {
			t.Fatalf("value = %q", sections[0].entries[0].value)
		}
	})
}

func TestBuildToggleViewerArtifactMergesFolderSources(t *testing.T) {
	dir := filepath.Join(`C:\Mods`, "Character")
	first, _ := parseToggleViewerSource(
		filepath.Join(dir, "a.ini"),
		"[KeyA]\ntype = cycle\nkey = A\n$a = 0,1\n[TextureOverrideBodyPosition]\nhash = 11111111",
	)
	second, _ := parseToggleViewerSource(
		filepath.Join(dir, "b.ini"),
		"[KeyB]\ntype = cycle\nkey = B\n$b = 0,1\n[TextureOverrideBodyPosition]\nhash = 22222222",
	)
	if first == nil || second == nil {
		t.Fatal("sources were not parsed")
	}

	artifact := buildToggleViewerArtifact([]toggleViewerSource{*first, *second}, "ctrl H")
	for _, want := range []string{"Ini: a.ini", "KeyA:", "Ini: b.ini", "KeyB:"} {
		if !strings.Contains(artifact.txtContent, want) {
			t.Fatalf("txt misses %q: %q", want, artifact.txtContent)
		}
	}
	for _, want := range []string{"hash = 11111111", "hash = 22222222"} {
		if !strings.Contains(artifact.iniContent, want) {
			t.Fatalf("ini misses %q: %q", want, artifact.iniContent)
		}
	}
	if len(artifact.targetINIPaths) != 2 {
		t.Fatalf("targets = %v", artifact.targetINIPaths)
	}
}

func generateArtifact(iniPath, content, hotkey string) *toggleViewerArtifact {
	source, _ := parseToggleViewerSource(iniPath, content)
	if source == nil {
		return nil
	}
	artifact := buildToggleViewerArtifact([]toggleViewerSource{*source}, hotkey)
	return &artifact
}
