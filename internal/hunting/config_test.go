package hunting

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/platform"
)

func TestReadHuntingConfigFollowsSafeIncludesAndParsesConditions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mainPath := filepath.Join(root, "d3dx.ini")
	writeTestINI(t, mainPath, `[Include]
include = keys.ini

[Hunting]
hunting = 2
marking_mode = skip
marking_actions = clipboard
toggle_hunting = no_modifiers NO_VK_DECIMAL VK_NUMPAD0
`)
	writeTestINI(t, filepath.Join(root, "keys.ini"), `[Hunting]
next_indexbuffer = no_modifiers NO_VK_DECIMAL VK_NUMPAD8
mark_indexbuffer = no_modifiers NO_VK_DECIMAL VK_NUMPAD9
`)

	config, err := readHuntingConfig(root, mainPath)
	if err != nil {
		t.Fatalf("readHuntingConfig = %v", err)
	}
	if config.toggle.chord != "vk_numpad0" {
		t.Fatalf("toggle chord = %q", config.toggle.chord)
	}
	if !slices.Contains(config.toggle.forbidden, "vk_decimal") ||
		!slices.Contains(config.toggle.forbidden, "ctrl") {
		t.Fatalf("toggle forbidden = %v", config.toggle.forbidden)
	}
	bindings, ok := config.categories[CategoryIndexBuffer]
	if !ok || bindings.next.chord != "vk_numpad8" || bindings.mark.chord != "vk_numpad9" {
		t.Fatalf("index-buffer bindings = %#v", bindings)
	}
}

func TestReadHuntingConfigRejectsDisabledAndEscapingInclude(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		path := filepath.Join(root, "d3dx.ini")
		writeTestINI(t, path, `[Hunting]
hunting = 0
marking_mode = skip
marking_actions = clipboard
toggle_hunting = VK_NUMPAD0
next_indexbuffer = VK_NUMPAD8
mark_indexbuffer = VK_NUMPAD9
`)
		_, err := readHuntingConfig(root, path)
		if !errors.Is(err, ErrDisabled) {
			t.Fatalf("readHuntingConfig = %v, want %v", err, ErrDisabled)
		}
	})

	t.Run("escape", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.ini")
		writeTestINI(t, outside, "[Hunting]\nhunting = 2\n")
		path := filepath.Join(root, "d3dx.ini")
		writeTestINI(t, path, "[Include]\ninclude = "+outside+"\n")
		_, err := readHuntingConfig(root, path)
		if !errors.Is(err, ErrConfigUnsupported) {
			t.Fatalf("readHuntingConfig = %v, want %v", err, ErrConfigUnsupported)
		}
	})
}

func TestReadHuntingConfigRejectsMissingHuntingAndIncludeCycle(t *testing.T) {
	t.Parallel()

	t.Run("missing hunting", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		path := filepath.Join(root, "d3dx.ini")
		writeTestINI(t, path, `[Hunting]
marking_mode = skip
marking_actions = clipboard
toggle_hunting = VK_NUMPAD0
next_indexbuffer = VK_NUMPAD8
mark_indexbuffer = VK_NUMPAD9
`)
		_, err := readHuntingConfig(root, path)
		if !errors.Is(err, ErrConfigUnsupported) || errors.Is(err, ErrDisabled) {
			t.Fatalf("readHuntingConfig = %v, want only %v", err, ErrConfigUnsupported)
		}
	})

	t.Run("include cycle", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		mainPath := filepath.Join(root, "d3dx.ini")
		includedPath := filepath.Join(root, "included.ini")
		writeTestINI(t, mainPath, "[Include]\ninclude = included.ini\n")
		writeTestINI(t, includedPath, "[Include]\ninclude = d3dx.ini\n")
		_, err := readHuntingConfig(root, mainPath)
		if !errors.Is(err, ErrConfigUnsupported) {
			t.Fatalf("readHuntingConfig = %v, want %v", err, ErrConfigUnsupported)
		}
	})
}

func TestParseBindingRejectsGamepad(t *testing.T) {
	t.Parallel()
	if _, err := parseBinding("XB_A"); err == nil {
		t.Fatal("parseBinding accepted a gamepad binding")
	}
}

func TestValidateConfigBindingsAcceptsZZMIMultiKeyChords(t *testing.T) {
	t.Parallel()

	config := huntingConfig{
		toggle: binding{chord: "vk_numpad0"},
		categories: map[Category]categoryBindings{
			CategoryPixelShader: {
				next: binding{chord: "vk_numpad2", forbidden: []string{"vk_decimal"}},
				mark: binding{chord: "vk_numpad3", forbidden: []string{"vk_decimal"}},
			},
			CategoryComputeShader: {
				next: binding{chord: "vk_decimal vk_numpad2"},
				mark: binding{chord: "vk_decimal vk_numpad3"},
			},
		},
	}

	if err := validateConfigBindings(platform.NewInput(), config); err != nil {
		t.Fatalf("validateConfigBindings(ZZMI bindings) = %v", err)
	}
}

func writeTestINI(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
