package tools

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
	artifact := generateToggleViewerArtifact(iniPath, content, "ctrl H")
	if artifact == nil {
		t.Fatal("artifact was not generated")
	}
	if !strings.Contains(artifact.txtContent, "Key: Ctrl + Up") || !strings.Contains(artifact.txtContent, "Back: Down") {
		t.Fatalf("txt = %q", artifact.txtContent)
	}
	if !strings.Contains(artifact.iniContent, "hash = aabbccdd") || !strings.Contains(artifact.iniContent, "key = ctrl H") {
		t.Fatalf("ini = %q", artifact.iniContent)
	}
	if len(artifact.toggleINIHash) != 64 || len(artifact.toggleTXTHash) != 64 {
		t.Fatalf("hashes = %q %q", artifact.toggleINIHash, artifact.toggleTXTHash)
	}
}

func TestGenerateToggleViewerArtifactRequiresCycleAndPositionHash(t *testing.T) {
	if artifact := generateToggleViewerArtifact("mod.ini", "[Key]\ntype = hold\nkey = H\n$x = 0,1\n[TextureOverrideBodyPosition]\nhash=1", "H"); artifact != nil {
		t.Fatal("non-cycle key generated an artifact")
	}
	if artifact := generateToggleViewerArtifact("mod.ini", "[Key]\ntype = cycle\nkey = H\n$x = 0,1", "H"); artifact != nil {
		t.Fatal("missing position hash generated an artifact")
	}
}

func TestReplaceToggleViewerHotkeyPreservesNewlines(t *testing.T) {
	original := "[Constants]\r\nx=1\r\n[Key]\r\nkey = ctrl H\r\ntype=cycle\r\n"
	updated := replaceToggleViewerHotkey(original, "alt J")
	if !strings.Contains(updated, "key = alt J\r\n") || strings.Contains(strings.ReplaceAll(updated, "\r\n", ""), "\n") {
		t.Fatalf("updated = %q", updated)
	}
}

func TestResolveTogglePositionHashThroughResourceReference(t *testing.T) {
	sections := parseToggleINI("[ResourceBodyPosition]\nfilename=x.buf\n[TextureOverrideBody]\nhash=deadbeef\nvb0=ResourceBodyPosition")
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
